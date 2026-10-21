package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/review"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type Reviews interface {
	List(context.Context, review.ListQuery) (review.Page, error)
	GetOwn(ctx context.Context, userID, venueID string) (review.Own, error)
	Save(context.Context, review.SaveCommand) (review.SaveResult, error)
	Delete(ctx context.Context, userID, venueID string) error
}

type reviewAuthorView struct {
	Username    *string `json:"username"`
	DisplayName string  `json:"display_name"`
}

type reviewView struct {
	ID         string           `json:"id"`
	Rating     int              `json:"rating"`
	Commentary *string          `json:"commentary"`
	Author     reviewAuthorView `json:"author"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

type ownReviewView struct {
	ID         string           `json:"id"`
	UserID     string           `json:"user_id"`
	Rating     int              `json:"rating"`
	Commentary *string          `json:"commentary"`
	Author     reviewAuthorView `json:"author"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}

// reviewRequest keeps raw values so review.ParseDraft owns every rating and commentary field error.
type reviewRequest struct {
	Rating     json.RawMessage `json:"rating"`
	Commentary json.RawMessage `json:"commentary"`
}

func reviewJSON(got review.Review) reviewView {
	return reviewView{
		ID: got.ID, Rating: got.Rating, Commentary: got.Commentary,
		Author:    reviewAuthorView{Username: got.Author.Username, DisplayName: got.Author.DisplayName},
		CreatedAt: got.CreatedAt.UTC(), UpdatedAt: got.UpdatedAt.UTC(),
	}
}

func ownReviewJSON(got review.Own) ownReviewView {
	username := got.Username
	return ownReviewView{
		ID: got.ID, UserID: got.UserID, Rating: got.Rating, Commentary: got.Commentary,
		Author:    reviewAuthorView{Username: &username, DisplayName: got.DisplayName},
		CreatedAt: got.CreatedAt.UTC(), UpdatedAt: got.UpdatedAt.UTC(),
	}
}

var reviewListParams = map[string]bool{"limit": false, "cursor": false}

func listReviews(w http.ResponseWriter, r *http.Request, reviews Reviews, logger *slog.Logger) {
	if fields := rejectedQuery(r, reviewListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	var validation *venue.ValidationError
	if errors.As(err, &validation) {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), validation.Fields)
		return
	}
	page, err := reviews.List(r.Context(), review.ListQuery{VenueID: r.PathValue("venueID"), Limit: limit, Cursor: values.Get("cursor")})
	if err != nil {
		writeReviewError(logger, w, r, err)
		return
	}
	views := make([]reviewView, 0, len(page.Reviews))
	for _, got := range page.Reviews {
		views = append(views, reviewJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}

func getOwnReview(w http.ResponseWriter, r *http.Request, reviews Reviews, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewError(logger, w, r, errors.New("missing caller"))
		return
	}
	got, err := reviews.GetOwn(r.Context(), caller.UserID, r.PathValue("venueID"))
	if err != nil {
		writeReviewError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, ownReviewJSON(got))
}

func saveReview(w http.ResponseWriter, r *http.Request, reviews Reviews, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req reviewRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	got, err := reviews.Save(r.Context(), review.SaveCommand{
		UserID: caller.UserID, VenueID: r.PathValue("venueID"), Rating: req.Rating, Commentary: req.Commentary,
	})
	if err != nil {
		writeReviewError(logger, w, r, err)
		return
	}
	status := http.StatusOK
	if got.Created {
		status = http.StatusCreated
	}
	WriteData(w, status, ownReviewJSON(got.Review))
}

func deleteReview(w http.ResponseWriter, r *http.Request, reviews Reviews, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewError(logger, w, r, errors.New("missing caller"))
		return
	}
	if err := reviews.Delete(r.Context(), caller.UserID, r.PathValue("venueID")); err != nil {
		writeReviewError(logger, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeReviewError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	if writeContributionError(w, id, err) {
		return
	}
	var validation *review.ValidationError
	var limited *review.RateLimitError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, review.ErrVenueNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", id, nil)
	case errors.Is(err, review.ErrReviewNotFound):
		WriteError(w, http.StatusNotFound, "review_not_found", "Review not found.", id, nil)
	case errors.Is(err, review.ErrVenueNotVerified):
		WriteError(w, http.StatusConflict, "venue_not_verified", "Reviews open after this venue is verified.", id, nil)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	default:
		logger.Error("review", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
