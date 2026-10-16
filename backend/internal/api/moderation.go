package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

type moderationReportView struct {
	ID        string          `json:"id"`
	Reason    string          `json:"reason"`
	Note      *string         `json:"note"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	Reporter  profile.Profile `json:"reporter"`
	Target    profile.Profile `json:"target"`
}

func listProfileReports(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	page, err := profiles.ListReports(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	items := make([]moderationReportView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, moderationReportItem(item))
	}
	WriteList(w, http.StatusOK, items, page.NextCursor)
}

func getModerationProfile(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	got, err := profiles.GetModerationProfile(r.Context(), r.PathValue("userID"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, moderationProfile(got))
}

type actionRequest struct {
	Action         optionalString `json:"action"`
	Reason         optionalString `json:"reason"`
	Note           optionalString `json:"note"`
	SuspendedUntil optionalTime   `json:"suspended_until"`
}

type actionView struct {
	ActionID *string         `json:"action_id"`
	Profile  profile.Profile `json:"profile"`
}

type dismissRequest struct {
	Status optionalString `json:"status"`
}

type optionalTime struct {
	Set   bool
	Null  bool
	Value time.Time
}

func (o *optionalTime) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(data, []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}

func applyModeration(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req actionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	id := requestID(r.Context())
	if r.Header.Get("Idempotency-Key") == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, map[string]string{
			"idempotency_key": "required",
		})
		return
	}
	cmd := profile.ModerationCommand{
		ActorID: caller.UserID, TargetID: r.PathValue("userID"), Action: req.Action.Value,
		Key: r.Header.Get("Idempotency-Key"),
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
	if req.SuspendedUntil.Set {
		cmd.UntilSet = true
		if !req.SuspendedUntil.Null {
			until := req.SuspendedUntil.Value
			cmd.SuspendedUntil = &until
		}
	}
	result, err := profiles.Moderate(r.Context(), cmd)
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	WriteData(w, status, actionView{ActionID: result.ActionID, Profile: moderationProfile(result.Profile)})
}

func dismissProfileReport(w http.ResponseWriter, r *http.Request, profiles Profiles, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeProfileError(logger, w, r, errors.New("missing caller"))
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
	item, err := profiles.DismissReport(r.Context(), caller.UserID, r.PathValue("reportID"))
	if err != nil {
		writeProfileError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, moderationReportItem(item))
}

func moderationReportItem(item profile.ReportItem) moderationReportView {
	return moderationReportView{
		ID: item.ID, Reason: item.Reason, Note: item.Note, Status: item.Status, CreatedAt: item.CreatedAt.UTC(),
		Reporter: moderationProfile(item.Reporter), Target: moderationProfile(item.Target),
	}
}

func parseReportLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &profile.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return n, nil
}

func moderationProfile(got profile.Profile) profile.Profile {
	got.CreatedAt = got.CreatedAt.UTC()
	got.UpdatedAt = got.UpdatedAt.UTC()
	if got.SuspendedUntil != nil {
		until := got.SuspendedUntil.UTC()
		got.SuspendedUntil = &until
	}
	return got
}
