package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type Submissions interface {
	Submit(context.Context, venue.SubmitCommand) (venue.Submission, error)
	Correct(context.Context, venue.CorrectCommand) (venue.Submission, error)
	ListOwn(context.Context, venue.OwnQuery) (venue.OwnPage, error)
	GetOwn(context.Context, string, string) (venue.Submission, error)
}

type submissionRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	Address     *string  `json:"address"`
	City        *string  `json:"city"`
	CountryCode *string  `json:"country_code"`
	VenueType   *string  `json:"venue_type"`
	GenreIDs    []int64  `json:"genre_ids"`
}

func (r submissionRequest) raw() venue.RawSubmission {
	return venue.RawSubmission{
		Name: r.Name, Description: r.Description, Latitude: r.Latitude, Longitude: r.Longitude,
		Address: r.Address, City: r.City, CountryCode: r.CountryCode, VenueType: r.VenueType, GenreIDs: r.GenreIDs,
	}
}

type submissionView struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	Genres             []genreView `json:"genres"`
	VerificationStatus string      `json:"verification_status"`
	RejectionReason    *string     `json:"rejection_reason"`
	RejectionNote      *string     `json:"rejection_note"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

func submissionJSON(got venue.Submission) submissionView {
	genres := make([]genreView, 0, len(got.Genres))
	for _, genre := range got.Genres {
		genres = append(genres, genreView{ID: genre.ID, Name: genre.Name})
	}
	return submissionView{
		ID: got.ID, Name: got.Name, Description: got.Description, Latitude: got.Latitude, Longitude: got.Longitude,
		Address: got.Address, City: got.City, CountryCode: got.CountryCode, VenueType: got.VenueType,
		Genres: genres, VerificationStatus: got.VerificationStatus,
		RejectionReason: got.RejectionReason, RejectionNote: got.RejectionNote,
		CreatedAt: got.CreatedAt.UTC(), UpdatedAt: got.UpdatedAt.UTC(),
	}
}

var ownListParams = map[string]bool{"limit": false, "cursor": false}

func createSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
			"idempotency_key": "required",
		})
		return
	}
	got, err := submissions.Submit(r.Context(), venue.SubmitCommand{UserID: caller.UserID, Key: key, Fields: req.raw()})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, submissionJSON(got))
}

func correctSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	got, err := submissions.Correct(r.Context(), venue.CorrectCommand{
		UserID: caller.UserID, VenueID: r.PathValue("venueID"), Fields: req.raw(),
	})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, submissionJSON(got))
}

func listSubmissions(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	if fields := rejectedQuery(r, ownListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	page, err := submissions.ListOwn(r.Context(), venue.OwnQuery{UserID: caller.UserID, Limit: limit, Cursor: values.Get("cursor")})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	views := make([]submissionView, 0, len(page.Submissions))
	for _, got := range page.Submissions {
		views = append(views, submissionJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}

func getSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	got, err := submissions.GetOwn(r.Context(), caller.UserID, r.PathValue("venueID"))
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, submissionJSON(got))
}

func writeSubmissionError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	if writeContributionError(w, id, err) {
		return
	}
	var validation *venue.ValidationError
	var duplicate *venue.DuplicateError
	var quota *venue.QuotaError
	var limited *venue.RateLimitError
	var notPending *venue.NotPendingError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, idempotency.ErrInvalid):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, nil)
	case errors.Is(err, idempotency.ErrReused):
		WriteError(w, http.StatusConflict, "idempotency_key_reused", "That idempotency key was already used.", id, nil)
	case errors.Is(err, venue.ErrNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", id, nil)
	case errors.As(err, &notPending):
		WriteError(w, http.StatusConflict, "venue_not_pending", "This venue can no longer be corrected.", id, nil)
	case errors.As(err, &duplicate):
		var details map[string]any
		if duplicate.Match != nil {
			details = map[string]any{
				"venue_id":            duplicate.Match.VenueID,
				"verification_status": duplicate.Match.VerificationStatus,
			}
		}
		WriteErrorDetails(w, http.StatusConflict, "duplicate_venue", "A matching venue already exists.", id, nil, details)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The submission limit has been reached.", id, nil)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	default:
		logger.Error("venue submission", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
