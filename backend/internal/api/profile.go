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
)

type Profiles interface {
	Get(context.Context, string) (profile.Profile, error)
	Patch(context.Context, string, profile.Patch) (profile.Profile, error)
	Delete(context.Context, profile.DeleteCommand) error
	Report(context.Context, profile.ReportCommand) (profile.Report, error)
	ListReports(context.Context, int, string) (profile.ReportPage, error)
	GetModerationProfile(context.Context, string) (profile.Profile, error)
}

type profileView struct {
	ID               string     `json:"id"`
	Username         string     `json:"username"`
	DisplayName      string     `json:"display_name"`
	Status           string     `json:"status"`
	ModerationReason *string    `json:"moderation_reason"`
	ModerationNote   *string    `json:"moderation_note"`
	SuspendedUntil   *time.Time `json:"suspended_until"`
}

type profilePatchRequest struct {
	Username    optionalString `json:"username"`
	DisplayName optionalString `json:"display_name"`
}

type deleteRequest struct {
	Confirmation optionalString `json:"confirmation"`
}

func profileRoutes(profiles Profiles, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getMe(w, r, profiles, logger)
		case http.MethodPatch:
			patchMe(w, r, profiles, logger)
		case http.MethodDelete:
			deleteMe(w, r, profiles, logger)
		default:
			http.NotFound(w, r)
		}
	})
}

func getMe(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
		return
	}
	got, err := profiles.Get(r.Context(), caller.UserID)
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, view(got))
}

func patchMe(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req profilePatchRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	fields := map[string]string{}
	if req.Username.Null {
		fields["username"] = "must not be null"
	}
	if req.DisplayName.Null {
		fields["display_name"] = "must not be null"
	}
	if len(fields) > 0 {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	var patch profile.Patch
	if req.Username.Set {
		patch.Username = &req.Username.Value
	}
	if req.DisplayName.Set {
		patch.DisplayName = &req.DisplayName.Value
	}
	got, err := profiles.Patch(r.Context(), caller.UserID, patch)
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, view(got))
}

func deleteMe(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req deleteRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if req.Confirmation.Null {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
			"confirmation": "must not be null",
		})
		return
	}
	err := profiles.Delete(r.Context(), profile.DeleteCommand{
		UserID:           caller.UserID,
		SessionCreatedAt: caller.SessionCreatedAt,
		Confirmation:     req.Confirmation.Value,
	})
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reportRequest struct {
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

type reportView struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func createReport(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req reportRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	id := requestID(r.Context())
	if req.Reason.Null {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, map[string]string{
			"reason": "must not be null",
		})
		return
	}
	if r.Header.Get("Idempotency-Key") == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, map[string]string{
			"idempotency_key": "required",
		})
		return
	}
	var note *string
	if req.Note.Set && !req.Note.Null {
		note = &req.Note.Value
	}
	got, err := profiles.Report(r.Context(), profile.ReportCommand{
		ReporterID: caller.UserID,
		Username:   r.PathValue("username"),
		Reason:     req.Reason.Value,
		Note:       note,
		Key:        r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, reportView{
		ID: got.ID, Username: got.Username, Reason: got.Reason, Note: got.Note, Status: got.Status, CreatedAt: got.CreatedAt,
	})
}

func view(got profile.Profile) profileView {
	return profileView{
		ID: got.ID, Username: got.Username, DisplayName: got.DisplayName, Status: got.Status,
		ModerationReason: got.ModerationReason, ModerationNote: got.ModerationNote, SuspendedUntil: got.SuspendedUntil,
	}
}

func writeDecodeError(w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	switch {
	case errors.Is(err, ErrUnsupportedMediaType):
		WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.", id, nil)
	case errors.Is(err, ErrBodyTooLarge):
		WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large", "The request body is too large.", id, nil)
	default:
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, nil)
	}
}

func writeProfileError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	var validation *profile.ValidationError
	var limited *profile.RateLimitError
	var suspended *profile.SuspendedError
	var quota *profile.QuotaError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, profile.ErrUsernameReserved):
		WriteError(w, http.StatusBadRequest, "username_reserved", "That username is reserved.", id, nil)
	case errors.Is(err, profile.ErrNotFound):
		WriteError(w, http.StatusNotFound, "profile_not_found", "Profile not found.", id, nil)
	case errors.Is(err, profile.ErrSelfReport):
		WriteError(w, http.StatusBadRequest, "self_report", "You cannot report yourself.", id, nil)
	case errors.Is(err, profile.ErrReportPending):
		WriteError(w, http.StatusConflict, "profile_already_reported", "You already reported this profile.", id, nil)
	case errors.Is(err, profile.ErrProfileRequired):
		WriteError(w, http.StatusConflict, "profile_required", "Create a profile first.", id, nil)
	case errors.Is(err, profile.ErrRenameRequired):
		WriteError(w, http.StatusForbidden, "profile_rename_required", "Rename this profile before continuing.", id, nil)
	case errors.Is(err, idempotency.ErrInvalid):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, nil)
	case errors.Is(err, idempotency.ErrReused):
		WriteError(w, http.StatusConflict, "idempotency_key_reused", "That idempotency key was already used.", id, nil)
	case errors.Is(err, profile.ErrUsernameTaken):
		WriteError(w, http.StatusConflict, "username_taken", "That username is taken.", id, nil)
	case errors.Is(err, profile.ErrRecentAuthenticationRequired):
		WriteError(w, http.StatusForbidden, "recent_authentication_required", "Sign in again to delete your account.", id, nil)
	case errors.As(err, &suspended):
		var details map[string]any
		if suspended.Until != nil {
			details = map[string]any{"suspended_until": suspended.Until.UTC().Format(time.RFC3339)}
		}
		WriteErrorDetails(w, http.StatusForbidden, "profile_suspended", "This profile is suspended.", id, nil, details)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The report limit has been reached.", id, nil)
	case errors.Is(err, profile.ErrAuthProvider):
		logger.Error("profile", "request_id", id, "err", err)
		WriteError(w, http.StatusBadGateway, "auth_provider_error", "The authentication provider is unavailable.", id, nil)
	default:
		logger.Error("profile", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
