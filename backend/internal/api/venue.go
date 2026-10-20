package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/metalhops/metalhops/backend/internal/venue"
)

type Catalogue interface {
	ListGenres(context.Context) ([]venue.Genre, error)
	ListVenues(context.Context, venue.Query) (venue.Page, error)
	GetVenue(context.Context, string) (venue.Venue, error)
}

type genreView struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type venueView struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	VerificationStatus string      `json:"verification_status"`
	Genres             []genreView `json:"genres"`
	AverageRating      *float64    `json:"average_rating"`
	ReviewCount        int         `json:"review_count"`
	CheckInCount       int         `json:"check_in_count"`
	DistanceMeters     *int        `json:"distance_meters,omitempty"`
}

func listGenres(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	genres, err := catalogue.ListGenres(r.Context())
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	views := make([]genreView, 0, len(genres))
	for _, genre := range genres {
		views = append(views, genreView{ID: genre.ID, Name: genre.Name})
	}
	WriteData(w, http.StatusOK, views)
}

var venueListParams = map[string]bool{
	"limit": false, "cursor": false, "query": false, "genre_id": true, "city": false,
	"country_code": false, "venue_type": false, "latitude": false, "longitude": false, "radius_km": false,
}

func listVenues(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, venueListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	query, err := venue.ParseQuery(venue.RawQuery{
		Limit: limit, Cursor: values.Get("cursor"), Query: values.Get("query"),
		GenreIDs: values["genre_id"], City: values.Get("city"),
		CountryCode: values.Get("country_code"), VenueType: values.Get("venue_type"),
		Latitude: values.Get("latitude"), Longitude: values.Get("longitude"),
		RadiusKM: values.Get("radius_km"),
	})
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	page, err := catalogue.ListVenues(r.Context(), query)
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	views := make([]venueView, 0, len(page.Venues))
	for _, got := range page.Venues {
		views = append(views, venueJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}

func getVenue(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	got, err := catalogue.GetVenue(r.Context(), r.PathValue("venueID"))
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, venueJSON(got))
}

func rejectedQuery(r *http.Request, allowed map[string]bool) map[string]string {
	fields := map[string]string{}
	for key, values := range r.URL.Query() {
		repeatable, ok := allowed[key]
		if !ok {
			fields[key] = "is not accepted"
			continue
		}
		if len(values) > 1 && !repeatable {
			fields[key] = "must be provided once"
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func parseVenueLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &venue.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return n, nil
}

func venueJSON(got venue.Venue) venueView {
	genres := make([]genreView, 0, len(got.Genres))
	for _, genre := range got.Genres {
		genres = append(genres, genreView{ID: genre.ID, Name: genre.Name})
	}
	return venueView{
		ID: got.ID, Name: got.Name, Description: got.Description,
		Latitude: got.Latitude, Longitude: got.Longitude,
		Address: got.Address, City: got.City, CountryCode: got.CountryCode,
		VenueType: got.VenueType, VerificationStatus: got.VerificationStatus,
		Genres: genres, AverageRating: got.AverageRating,
		ReviewCount: got.ReviewCount, CheckInCount: got.CheckInCount,
		DistanceMeters: got.DistanceMeters,
	}
}

func writeVenueError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	var validation *venue.ValidationError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), validation.Fields)
	case errors.Is(err, venue.ErrNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", requestID(r.Context()), nil)
	default:
		logger.Error("venue", "err", err, "request_id", requestID(r.Context()))
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", requestID(r.Context()), nil)
	}
}
