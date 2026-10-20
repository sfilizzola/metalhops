package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/metalhops/metalhops/backend/internal/venue"
)

func TestVenueRoutes(t *testing.T) {
	cursor := "next-page"
	description := "A metal bar"
	catalogue := &fakeCatalogue{
		genres: []venue.Genre{{ID: 2, Name: "Punk"}, {ID: 1, Name: "Classic rock"}},
		page: venue.Page{Venues: []venue.Venue{{
			ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Description: &description,
			Latitude: 60, Longitude: 10, Address: "1 Metal Street", City: "Oslo",
			CountryCode: "DE", VenueType: "bar", VerificationStatus: "pending",
			Genres: []venue.Genre{{ID: 1, Name: "Classic rock"}},
		}}, NextCursor: &cursor},
		venue: venue.Venue{
			ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Description: &description,
			Latitude: 60, Longitude: 10, Address: "1 Metal Street", City: "Oslo",
			CountryCode: "DE", VenueType: "bar", VerificationStatus: "verified",
			Genres: []venue.Genre{},
		},
	}
	handler := venueHandler(catalogue)
	genres := requestVenue(handler, http.MethodGet, "/v1/genres", "Bearer secret")
	if genres.Code != http.StatusOK || catalogue.genreCalls != 1 || bytes.Contains(genres.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("genres %d %s", genres.Code, genres.Body.String())
	}
	if !bytes.Contains(genres.Body.Bytes(), []byte(`"Punk"`)) || bytes.Index(genres.Body.Bytes(), []byte(`"Punk"`)) > bytes.Index(genres.Body.Bytes(), []byte(`"Classic rock"`)) {
		t.Fatal("handler reordered genres")
	}
	list := requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if list.Code != http.StatusOK || catalogue.query.Limit != 20 || catalogue.query.Cursor != "" || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next-page"`)) {
		t.Fatalf("list %d query %+v body %s", list.Code, catalogue.query, list.Body.String())
	}
	if bytes.Contains(list.Body.Bytes(), []byte(`"distance_meters"`)) {
		t.Fatalf("distance without location %s", list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"average_rating":null`)) || bytes.Contains(list.Body.Bytes(), []byte(`"submitted_by"`)) {
		t.Fatalf("document %s", list.Body.String())
	}
	detail := requestVenue(handler, http.MethodGet, "/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "")
	if detail.Code != http.StatusOK || catalogue.id != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || bytes.Contains(detail.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
}

func TestEmptyVenueCollections(t *testing.T) {
	handler := venueHandler(&fakeCatalogue{})
	genres := requestVenue(handler, http.MethodGet, "/v1/genres", "")
	if genres.Code != http.StatusOK || !bytes.Contains(genres.Body.Bytes(), []byte(`"data":[]`)) || bytes.Contains(genres.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("genres %d %s", genres.Code, genres.Body.String())
	}
	list := requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"data":[]`)) || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":null`)) {
		t.Fatalf("list %d %s", list.Code, list.Body.String())
	}
}

func TestVenueQueryValidation(t *testing.T) {
	catalogue := &fakeCatalogue{}
	handler := venueHandler(catalogue)
	cases := []string{
		"/v1/venues?limit=nope",
		"/v1/venues?limit=0",
		"/v1/venues?limit=101",
		"/v1/venues?limit=1&limit=2",
		"/v1/venues?cursor=a&cursor=b",
		"/v1/venues?city=Oslo&city=Bergen",
		"/v1/venues?sort=name",
		"/v1/genres?limit=20",
		"/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?cursor=abc",
	}
	for _, path := range cases {
		before := catalogue.listCalls + catalogue.getCalls + catalogue.genreCalls
		rec := requestVenue(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"validation_error"`)) {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body.String())
		}
		if catalogue.listCalls+catalogue.getCalls+catalogue.genreCalls != before {
			t.Fatalf("%s reached the catalogue", path)
		}
	}
	catalogue.listErr = &venue.ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	badCursor := requestVenue(handler, http.MethodGet, "/v1/venues?cursor=abc", "")
	if badCursor.Code != http.StatusBadRequest || !bytes.Contains(badCursor.Body.Bytes(), []byte(`"cursor":"invalid"`)) {
		t.Fatalf("cursor %d %s", badCursor.Code, badCursor.Body.String())
	}
	catalogue.listErr = nil
	badID := requestVenue(handler, http.MethodGet, "/v1/venues/AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "")
	if badID.Code != http.StatusBadRequest || !bytes.Contains(badID.Body.Bytes(), []byte(`"venue_id"`)) || catalogue.getCalls != 1 {
		t.Fatalf("uppercase %d calls %d %s", badID.Code, catalogue.getCalls, badID.Body.String())
	}
	catalogue.getErr = venue.ErrNotFound
	missing := requestVenue(handler, http.MethodGet, "/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "")
	if missing.Code != http.StatusNotFound || !bytes.Contains(missing.Body.Bytes(), []byte(`"venue_not_found"`)) {
		t.Fatalf("missing %d %s", missing.Code, missing.Body.String())
	}
	catalogue.listErr = errors.New("db down")
	failed := requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if failed.Code != http.StatusInternalServerError || !bytes.Contains(failed.Body.Bytes(), []byte(`"internal_error"`)) {
		t.Fatalf("internal %d %s", failed.Code, failed.Body.String())
	}
}

func venueHandler(catalogue *fakeCatalogue) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Catalogue:   catalogue,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func requestVenue(handler http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type fakeCatalogue struct {
	genres     []venue.Genre
	page       venue.Page
	venue      venue.Venue
	query      venue.Query
	id         string
	genreCalls int
	listCalls  int
	getCalls   int
	listErr    error
	getErr     error
}

func (f *fakeCatalogue) ListGenres(context.Context) ([]venue.Genre, error) {
	f.genreCalls++
	return f.genres, nil
}

func (f *fakeCatalogue) ListVenues(_ context.Context, q venue.Query) (venue.Page, error) {
	f.listCalls++
	f.query = q
	if f.listErr != nil {
		return venue.Page{}, f.listErr
	}
	return f.page, nil
}

func (f *fakeCatalogue) GetVenue(_ context.Context, id string) (venue.Venue, error) {
	f.getCalls++
	f.id = id
	if !venue.CanonicalVenueID(id) {
		return venue.Venue{}, &venue.ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	if f.getErr != nil {
		return venue.Venue{}, f.getErr
	}
	return f.venue, nil
}

func TestVenueDiscoveryParams(t *testing.T) {
	catalogue := &fakeCatalogue{}
	handler := venueHandler(catalogue)
	rec := requestVenue(handler, http.MethodGet, "/v1/venues?query=Iron&genre_id=3&genre_id=1&genre_id=3&city=Oslo&country_code=NO&venue_type=pub&latitude=59.9&longitude=10.75&radius_km=5&limit=10&cursor=abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	want := venue.Query{
		Limit: 10, Cursor: "abc", Text: "Iron", GenreIDs: []int{1, 3},
		City: "Oslo", CountryCode: "NO", VenueType: "pub",
		Location: &venue.Location{Latitude: 59.9, Longitude: 10.75, RadiusKM: 5},
	}
	if !reflect.DeepEqual(catalogue.query, want) {
		t.Fatalf("query %+v want %+v", catalogue.query, want)
	}
	for _, path := range []string{
		"/v1/venues?genre_id=x",
		"/v1/venues?latitude=1&longitude=2",
		"/v1/venues?latitude=91&longitude=0&radius_km=5",
		"/v1/venues?radius_km=1.5&latitude=0&longitude=0",
		"/v1/venues?country_code=UK",
		"/v1/venues?query=a&query=b",
	} {
		before := catalogue.listCalls
		rec := requestVenue(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"validation_error"`)) || catalogue.listCalls != before {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestDistanceMetersPresence(t *testing.T) {
	meters := 1234
	catalogue := &fakeCatalogue{page: venue.Page{Venues: []venue.Venue{{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Genres: []venue.Genre{}, DistanceMeters: &meters,
	}}}}
	handler := venueHandler(catalogue)
	rec := requestVenue(handler, http.MethodGet, "/v1/venues?latitude=0&longitude=0&radius_km=5", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"distance_meters":1234`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	catalogue.page.Venues[0].DistanceMeters = nil
	rec = requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte(`"distance_meters"`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
