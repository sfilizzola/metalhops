package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type VenueModeration interface {
	ReportVenue(context.Context, venue.ReportCommand) (venue.VenueReport, error)
	ListVenueReports(context.Context, int, string) (venue.ReportPage, error)
	GetModerationVenue(context.Context, string) (venue.ModerationVenue, error)
	ModerateVenue(context.Context, venue.ModerationCommand) (venue.ModerationResult, error)
	CorrectVenue(context.Context, venue.OperatorCorrectCommand) (venue.Moderated, error)
	DismissVenueReport(context.Context, string, string) (venue.ReportItem, error)
}

type venueReportRequest struct {
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

type venueReportView struct {
	ID        string    `json:"id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type venueSummaryView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	City               string `json:"city"`
	CountryCode        string `json:"country_code"`
	VerificationStatus string `json:"verification_status"`
}

type venueReportItemView struct {
	ID        string           `json:"id"`
	Reason    string           `json:"reason"`
	Note      *string          `json:"note"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	Reporter  *profile.Profile `json:"reporter"`
	Venue     venueSummaryView `json:"venue"`
	Submitter *profile.Profile `json:"submitter"`
}

type pendingReportView struct {
	ID        string           `json:"id"`
	Reason    string           `json:"reason"`
	Note      *string          `json:"note"`
	CreatedAt time.Time        `json:"created_at"`
	Reporter  *profile.Profile `json:"reporter"`
}

type moderatedView struct {
	submissionView
	DelistingReason *string `json:"delisting_reason"`
	DelistingNote   *string `json:"delisting_note"`
}

type moderationVenueView struct {
	moderatedView
	Submitter *profile.Profile    `json:"submitter"`
	Reports   []pendingReportView `json:"reports"`
}

type venueActionView struct {
	ActionID *string       `json:"action_id"`
	Venue    moderatedView `json:"venue"`
}

type venueActionRequest struct {
	Action optionalString `json:"action"`
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

func profilePtr(p *profile.Profile) *profile.Profile {
	if p == nil {
		return nil
	}
	got := moderationProfile(*p)
	return &got
}

func moderatedJSON(m venue.Moderated) moderatedView {
	return moderatedView{submissionView: submissionJSON(m.Submission), DelistingReason: m.DelistingReason, DelistingNote: m.DelistingNote}
}

func reportItemJSON(item venue.ReportItem) venueReportItemView {
	return venueReportItemView{
		ID: item.ID, Reason: item.Reason, Note: item.Note, Status: item.Status, CreatedAt: item.CreatedAt.UTC(),
		Reporter: profilePtr(item.Reporter), Submitter: profilePtr(item.Submitter),
		Venue: venueSummaryView{
			ID: item.Venue.ID, Name: item.Venue.Name, City: item.Venue.City,
			CountryCode: item.Venue.CountryCode, VerificationStatus: item.Venue.VerificationStatus,
		},
	}
}

func missingKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return false
	}
	WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
		"idempotency_key": "required",
	})
	return true
}

func reportVenue(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req venueReportRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := venue.ReportCommand{ReporterID: caller.UserID, VenueID: r.PathValue("venueID"), Key: r.Header.Get("Idempotency-Key")}
	if req.Reason.Set && !req.Reason.Null {
		cmd.Reason = req.Reason.Value
	}
	if req.Note.Set && !req.Note.Null {
		note := req.Note.Value
		cmd.Note = &note
	}
	got, err := moderation.ReportVenue(r.Context(), cmd)
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, venueReportView{
		ID: got.ID, VenueID: got.VenueID, Reason: got.Reason, Note: got.Note, Status: got.Status, CreatedAt: got.CreatedAt.UTC(),
	})
}

func listVenueReports(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeVenueModerationError(logger, w, r, &venue.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}})
		return
	}
	page, err := moderation.ListVenueReports(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	items := make([]venueReportItemView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reportItemJSON(item))
	}
	WriteList(w, http.StatusOK, items, page.NextCursor)
}

func getModerationVenue(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	got, err := moderation.GetModerationVenue(r.Context(), r.PathValue("venueID"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	reports := make([]pendingReportView, 0, len(got.Reports))
	for _, item := range got.Reports {
		reports = append(reports, pendingReportView{
			ID: item.ID, Reason: item.Reason, Note: item.Note, CreatedAt: item.CreatedAt.UTC(), Reporter: profilePtr(item.Reporter),
		})
	}
	WriteData(w, http.StatusOK, moderationVenueView{moderatedView: moderatedJSON(got.Moderated), Submitter: profilePtr(got.Submitter), Reports: reports})
}

func applyVenueAction(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req venueActionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := venue.ModerationCommand{
		ActorID: caller.UserID, VenueID: r.PathValue("venueID"), Action: req.Action.Value, Key: r.Header.Get("Idempotency-Key"),
	}
	if req.Reason.Set {
		cmd.ReasonSet = true
		if !req.Reason.Null {
			cmd.Reason = req.Reason.Value
		}
	}
	if req.Note.Set {
		cmd.NoteSet = true
		if !req.Note.Null {
			note := req.Note.Value
			cmd.Note = &note
		}
	}
	result, err := moderation.ModerateVenue(r.Context(), cmd)
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	WriteData(w, status, venueActionView{ActionID: result.ActionID, Venue: moderatedJSON(result.Venue)})
}

func correctVenueAsOperator(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	got, err := moderation.CorrectVenue(r.Context(), venue.OperatorCorrectCommand{
		ActorID: caller.UserID, VenueID: r.PathValue("venueID"), Fields: req.raw(),
	})
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, moderatedJSON(got))
}

func dismissVenueReport(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req dismissRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if !req.Status.Set || req.Status.Null || req.Status.Value != "dismissed" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
			"status": "must be dismissed",
		})
		return
	}
	item, err := moderation.DismissVenueReport(r.Context(), caller.UserID, r.PathValue("reportID"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, reportItemJSON(item))
}

func writeVenueModerationError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	var transition *venue.InvalidTransitionError
	var quota *profile.QuotaError
	switch {
	case errors.Is(err, venue.ErrSelfReport):
		WriteError(w, http.StatusBadRequest, "self_report", "You cannot report your own venue.", id, nil)
	case errors.Is(err, venue.ErrReportPending):
		WriteError(w, http.StatusConflict, "venue_already_reported", "You already reported this venue.", id, nil)
	case errors.Is(err, venue.ErrReportNotFound):
		WriteError(w, http.StatusNotFound, "venue_report_not_found", "Report not found.", id, nil)
	case errors.Is(err, venue.ErrReportResolved):
		WriteError(w, http.StatusConflict, "report_already_resolved", "That report is already resolved.", id, nil)
	case errors.As(err, &transition):
		WriteError(w, http.StatusConflict, "invalid_venue_transition", "This action is not available for a venue with that status.", id, nil)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The report limit has been reached.", id, nil)
	default:
		writeSubmissionError(logger, w, r, err)
	}
}
