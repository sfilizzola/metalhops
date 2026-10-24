package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/checkin"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type CheckIns interface {
	Create(context.Context, checkin.CreateCommand) (checkin.CheckIn, error)
	ListOwn(context.Context, checkin.ListQuery) (checkin.Page, error)
	ListVenueFeed(ctx context.Context, venueID, callerUserID string) ([]checkin.FeedItem, error)
	Delete(ctx context.Context, userID, checkInID string) error
}

type checkInVenueView struct {
	Name               string  `json:"name"`
	City               string  `json:"city"`
	CountryCode        string  `json:"country_code"`
	VenueType          *string `json:"venue_type,omitempty"`
	VerificationStatus string  `json:"verification_status"`
}

type checkInView struct {
	ID         string           `json:"id"`
	VenueID    string           `json:"venue_id"`
	VisitedAt  time.Time        `json:"visited_at"`
	RecordedAt time.Time        `json:"recorded_at"`
	Venue      checkInVenueView `json:"venue"`
}

type checkerView struct {
	Username    *string `json:"username"`
	DisplayName string  `json:"display_name"`
}

type feedItemView struct {
	ID         string      `json:"id"`
	VisitedAt  time.Time   `json:"visited_at"`
	RecordedAt time.Time   `json:"recorded_at"`
	Checker    checkerView `json:"checker"`
}

func checkInJSON(got checkin.CheckIn) checkInView {
	return checkInView{
		ID: got.ID, VenueID: got.VenueID, VisitedAt: got.VisitedAt.UTC(), RecordedAt: got.RecordedAt.UTC(),
		Venue: checkInVenueView{
			Name: got.Venue.Name, City: got.Venue.City, CountryCode: got.Venue.CountryCode,
			VenueType: got.Venue.VenueType, VerificationStatus: got.Venue.VerificationStatus,
		},
	}
}

func feedItemJSON(got checkin.FeedItem) feedItemView {
	var username *string
	if got.Checker.Username != "" {
		name := got.Checker.Username
		username = &name
	}
	return feedItemView{
		ID: got.ID, VisitedAt: got.VisitedAt.UTC(), RecordedAt: got.RecordedAt.UTC(),
		Checker: checkerView{Username: username, DisplayName: got.Checker.DisplayName},
	}
}

func createCheckIn(w http.ResponseWriter, r *http.Request, checkIns CheckIns, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeCheckInError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req struct {
		VisitedAt json.RawMessage `json:"visited_at"`
	}
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
	got, err := checkIns.Create(r.Context(), checkin.CreateCommand{
		UserID: caller.UserID, VenueID: r.PathValue("venueID"), Key: key, VisitedAt: req.VisitedAt,
	})
	if err != nil {
		writeCheckInError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, checkInJSON(got))
}

func listCheckIns(w http.ResponseWriter, r *http.Request, checkIns CheckIns, logger *slog.Logger) {
	if fields := rejectedQuery(r, map[string]bool{"limit": false, "cursor": false}); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeCheckInError(logger, w, r, errors.New("missing caller"))
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	var validation *venue.ValidationError
	if errors.As(err, &validation) {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), validation.Fields)
		return
	}
	page, err := checkIns.ListOwn(r.Context(), checkin.ListQuery{UserID: caller.UserID, Limit: limit, Cursor: values.Get("cursor")})
	if err != nil {
		writeCheckInError(logger, w, r, err)
		return
	}
	views := make([]checkInView, 0, len(page.CheckIns))
	for _, got := range page.CheckIns {
		views = append(views, checkInJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}

func listCheckInFeed(w http.ResponseWriter, r *http.Request, checkIns CheckIns, logger *slog.Logger) {
	id := requestID(r.Context())
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, fields)
		return
	}
	venueID := r.PathValue("venueID")
	if !checkin.CanonicalID(venueID) {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, map[string]string{"venue_id": "must be a canonical uuid"})
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		logger.Error("checkin", "request_id", id, "err", "missing caller")
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
		return
	}
	items, err := checkIns.ListVenueFeed(r.Context(), venueID, caller.UserID)
	switch {
	case err == nil:
	case errors.Is(err, checkin.ErrVenueNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", id, nil)
		return
	case errors.Is(err, profile.ErrProfileRequired):
		WriteError(w, http.StatusForbidden, "profile_required", "Create a profile first.", id, nil)
		return
	default:
		logger.Error("checkin", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
		return
	}
	views := make([]feedItemView, 0, len(items))
	for _, got := range items {
		views = append(views, feedItemJSON(got))
	}
	WriteData(w, http.StatusOK, views)
}

func deleteCheckIn(w http.ResponseWriter, r *http.Request, checkIns CheckIns, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeCheckInError(logger, w, r, errors.New("missing caller"))
		return
	}
	if err := checkIns.Delete(r.Context(), caller.UserID, r.PathValue("checkInID")); err != nil {
		writeCheckInError(logger, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeCheckInError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	if writeContributionError(w, id, err) {
		return
	}
	var validation *checkin.ValidationError
	var spacing *checkin.SpacingError
	var limited *checkin.RateLimitError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, checkin.ErrVenueNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", id, nil)
	case errors.Is(err, checkin.ErrCheckInNotFound):
		WriteError(w, http.StatusNotFound, "check_in_not_found", "Check-in not found.", id, nil)
	case errors.As(err, &spacing):
		WriteErrorDetails(w, http.StatusConflict, "check_in_spacing", "Another check-in at this venue is too close to this visit.", id, nil, map[string]any{
			"check_in_id": spacing.CheckInID, "visited_at": spacing.VisitedAt.UTC(),
		})
	case errors.Is(err, idempotency.ErrInvalid):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, nil)
	case errors.Is(err, idempotency.ErrReused):
		WriteError(w, http.StatusConflict, "idempotency_key_reused", "That idempotency key was already used.", id, nil)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	default:
		logger.Error("checkin", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
