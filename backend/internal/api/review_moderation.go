package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/review"
)

type ReviewModeration interface {
	ReportReview(context.Context, review.ReportCommand) (review.Report, error)
	ListReviewReports(context.Context, int, string) (review.ReportPage, error)
	GetModerationReview(context.Context, string) (review.ModerationDetail, error)
	ModerateReview(context.Context, review.ModerationCommand) (review.ModerationResult, error)
	DismissReviewReport(context.Context, string, string) (review.ReportItem, error)
}

type reviewReportRequest struct {
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

type reviewActionRequest struct {
	Action optionalString `json:"action"`
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

type reviewReportView struct {
	ID        string    `json:"id"`
	ReviewID  string    `json:"review_id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type reviewReportItemView struct {
	ID                string           `json:"id"`
	Reason            string           `json:"reason"`
	Note              *string          `json:"note"`
	Status            string           `json:"status"`
	ReviewID          string           `json:"review_id"`
	VenueID           string           `json:"venue_id"`
	Rating            int              `json:"rating"`
	Commentary        *string          `json:"commentary"`
	CommentaryRemoved bool             `json:"commentary_removed"`
	CreatedAt         time.Time        `json:"created_at"`
	Reporter          *profile.Profile `json:"reporter"`
	Author            *profile.Profile `json:"author"`
}

type operatorReviewView struct {
	ID                  string           `json:"id"`
	VenueID             string           `json:"venue_id"`
	UserID              string           `json:"user_id"`
	Rating              int              `json:"rating"`
	Commentary          *string          `json:"commentary"`
	CommentaryRemoved   bool             `json:"commentary_removed"`
	CommentaryRemovedAt *time.Time       `json:"commentary_removed_at"`
	Author              *profile.Profile `json:"author"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type reviewAuditView struct {
	ID             string    `json:"id"`
	Action         string    `json:"action"`
	Reason         string    `json:"reason"`
	Note           *string   `json:"note"`
	ReviewID       string    `json:"review_id"`
	ReviewerUserID *string   `json:"reviewer_user_id"`
	VenueID        string    `json:"venue_id"`
	OperatorUserID *string   `json:"operator_user_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type operatorRestrictionView struct {
	CreatedAt time.Time       `json:"created_at"`
	Source    reviewAuditView `json:"source"`
}

type moderationReviewView struct {
	Review      operatorReviewView       `json:"review"`
	Reports     []pendingReportView      `json:"reports"`
	History     []reviewAuditView        `json:"history"`
	Restriction *operatorRestrictionView `json:"commentary_restriction"`
}

type reviewActionView struct {
	ActionID *string             `json:"action_id"`
	ReviewID string              `json:"review_id"`
	Review   *operatorReviewView `json:"review"`
}

var reviewReportListParams = map[string]bool{"limit": false, "cursor": false}

func reportReview(w http.ResponseWriter, r *http.Request, moderation ReviewModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req reviewReportRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := review.ReportCommand{
		ReporterID: caller.UserID, VenueID: r.PathValue("venueID"), ReviewID: r.PathValue("reviewID"),
		Key: r.Header.Get("Idempotency-Key"),
	}
	if req.Reason.Set && !req.Reason.Null {
		cmd.Reason = req.Reason.Value
	}
	if req.Note.Set && !req.Note.Null {
		note := req.Note.Value
		cmd.Note = &note
	}
	got, err := moderation.ReportReview(r.Context(), cmd)
	if err != nil {
		writeReviewError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, reviewReportView{
		ID: got.ID, ReviewID: got.ReviewID, VenueID: got.VenueID, Reason: got.Reason,
		Note: got.Note, Status: got.Status, CreatedAt: got.CreatedAt.UTC(),
	})
}

func listReviewReports(w http.ResponseWriter, r *http.Request, moderation ReviewModeration, logger *slog.Logger) {
	if fields := rejectedQuery(r, reviewReportListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeReviewModerationError(logger, w, r, &review.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}})
		return
	}
	if err := review.ValidateLimit(limit); err != nil {
		writeReviewModerationError(logger, w, r, err)
		return
	}
	page, err := moderation.ListReviewReports(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeReviewModerationError(logger, w, r, err)
		return
	}
	items := make([]reviewReportItemView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reviewReportItemJSON(item))
	}
	WriteList(w, http.StatusOK, items, page.NextCursor)
}

func getModerationReview(w http.ResponseWriter, r *http.Request, moderation ReviewModeration, logger *slog.Logger) {
	got, err := moderation.GetModerationReview(r.Context(), r.PathValue("reviewID"))
	if err != nil {
		writeReviewModerationError(logger, w, r, err)
		return
	}
	reports := make([]pendingReportView, 0, len(got.Reports))
	for _, item := range got.Reports {
		reports = append(reports, pendingReportView{
			ID: item.ID, Reason: item.Reason, Note: item.Note, CreatedAt: item.CreatedAt.UTC(), Reporter: profilePtr(item.Reporter),
		})
	}
	history := make([]reviewAuditView, 0, len(got.History))
	for _, a := range got.History {
		history = append(history, auditJSON(a))
	}
	var restriction *operatorRestrictionView
	if got.Restriction != nil {
		restriction = &operatorRestrictionView{
			CreatedAt: got.Restriction.CreatedAt.UTC(),
			Source:    auditJSON(got.Restriction.Source),
		}
	}
	WriteData(w, http.StatusOK, moderationReviewView{
		Review: operatorReviewJSON(got.Review), Reports: reports, History: history, Restriction: restriction,
	})
}

func applyReviewAction(w http.ResponseWriter, r *http.Request, moderation ReviewModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req reviewActionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := review.ModerationCommand{
		ActorID: caller.UserID, ReviewID: r.PathValue("reviewID"), Action: req.Action.Value,
		Key: r.Header.Get("Idempotency-Key"),
	}
	if req.Reason.Set && !req.Reason.Null {
		cmd.Reason = req.Reason.Value
	}
	if req.Note.Set && !req.Note.Null {
		note := req.Note.Value
		cmd.Note = &note
	}
	result, err := moderation.ModerateReview(r.Context(), cmd)
	if err != nil {
		writeReviewModerationError(logger, w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	var bodyReview *operatorReviewView
	if result.Review != nil {
		view := operatorReviewJSON(*result.Review)
		bodyReview = &view
	}
	WriteData(w, status, reviewActionView{ActionID: result.ActionID, ReviewID: result.ReviewID, Review: bodyReview})
}

func dismissReviewReport(w http.ResponseWriter, r *http.Request, moderation ReviewModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeReviewModerationError(logger, w, r, errors.New("missing caller"))
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
	item, err := moderation.DismissReviewReport(r.Context(), caller.UserID, r.PathValue("reportID"))
	if err != nil {
		writeReviewModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, reviewReportItemJSON(item))
}

func reviewReportItemJSON(item review.ReportItem) reviewReportItemView {
	return reviewReportItemView{
		ID: item.ID, Reason: item.Reason, Note: item.Note, Status: item.Status,
		ReviewID: item.ReviewID, VenueID: item.VenueID, Rating: item.Rating,
		Commentary: item.Commentary, CommentaryRemoved: item.CommentaryRemoved,
		CreatedAt: item.CreatedAt.UTC(), Reporter: profilePtr(item.Reporter), Author: profilePtr(item.Author),
	}
}

func operatorReviewJSON(got review.OperatorReview) operatorReviewView {
	var removedAt *time.Time
	if got.CommentaryRemovedAt != nil {
		at := got.CommentaryRemovedAt.UTC()
		removedAt = &at
	}
	return operatorReviewView{
		ID: got.ID, VenueID: got.VenueID, UserID: got.UserID, Rating: got.Rating,
		Commentary: got.Commentary, CommentaryRemoved: got.CommentaryRemoved, CommentaryRemovedAt: removedAt,
		Author: profilePtr(got.Author), CreatedAt: got.CreatedAt.UTC(), UpdatedAt: got.UpdatedAt.UTC(),
	}
}

func auditJSON(a review.AuditEntry) reviewAuditView {
	return reviewAuditView{
		ID: a.ID, Action: a.Action, Reason: a.Reason, Note: a.Note, ReviewID: a.ReviewID,
		ReviewerUserID: a.ReviewerUserID, VenueID: a.VenueID, OperatorUserID: a.OperatorUserID,
		CreatedAt: a.CreatedAt.UTC(),
	}
}

func writeReviewModerationError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	if writeContributionError(w, id, err) {
		return
	}
	var validation *review.ValidationError
	var limited *review.RateLimitError
	var quota *profile.QuotaError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, review.ErrReviewNotFound):
		WriteError(w, http.StatusNotFound, "review_not_found", "Review not found.", id, nil)
	case errors.Is(err, review.ErrCommentaryNotPublic):
		WriteError(w, http.StatusConflict, "commentary_not_public", "This review has no public commentary.", id, nil)
	case errors.Is(err, review.ErrReportNotFound):
		WriteError(w, http.StatusNotFound, "review_report_not_found", "Report not found.", id, nil)
	case errors.Is(err, review.ErrReportResolved):
		WriteError(w, http.StatusConflict, "report_already_resolved", "That report is already resolved.", id, nil)
	case errors.Is(err, idempotency.ErrReused):
		WriteError(w, http.StatusConflict, "idempotency_key_reused", "That idempotency key was already used.", id, nil)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The report limit has been reached.", id, nil)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	default:
		logger.Error("review moderation", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
