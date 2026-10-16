package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
)

func TestModerationReads(t *testing.T) {
	now := time.Date(2026, 10, 16, 13, 0, 0, 0, time.UTC)
	reason := "spam"
	note := "watch"
	until := now.Add(time.Hour)
	reporter := profile.Profile{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Username: "reporter", DisplayName: "Reporter", Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	target := profile.Profile{
		ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Username: "target", DisplayName: "Target", Status: "suspended",
		ModerationReason: &reason, ModerationNote: &note, SuspendedUntil: &until, CreatedAt: now, UpdatedAt: now,
	}
	cursor := "next-page"
	profiles := &readProfiles{page: profile.ReportPage{
		Items: []profile.ReportItem{{
			ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Reason: "spam", Status: "pending", CreatedAt: now,
			Reporter: reporter, Target: target,
		}},
		NextCursor: &cursor,
	}, got: target}
	caller := auth.Caller{UserID: reporter.ID, SessionID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
	authenticator := &fakeAuth{caller: caller}
	var logs bytes.Buffer
	handler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    profiles,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	})

	for _, path := range []string{"/v1/moderation/profile-reports", "/v1/moderation/profiles/" + target.ID} {
		rec := authed(handler, http.MethodGet, path, "", nil)
		assertCode(t, rec, http.StatusForbidden, "forbidden")
		if !bytes.Contains(rec.Body.Bytes(), []byte(`This action requires a moderator account.`)) {
			t.Fatalf("message %s", rec.Body.String())
		}
	}
	if profiles.lists != 0 || profiles.gets != 0 {
		t.Fatalf("app user reached reads lists %d gets %d", profiles.lists, profiles.gets)
	}

	authenticator.operatorErr = errors.New("db down")
	assertCode(t, authed(handler, http.MethodGet, "/v1/moderation/profile-reports", "", nil), http.StatusInternalServerError, "internal_error")
	if !bytes.Contains(logs.Bytes(), []byte("db down")) || bytes.Contains(logs.Bytes(), []byte("test-token")) {
		t.Fatalf("log %s", logs.String())
	}
	authenticator.operatorErr = nil
	authenticator.operator = true

	ok := authed(handler, http.MethodGet, "/v1/moderation/profile-reports", "", nil)
	if ok.Code != http.StatusOK || profiles.limit != 20 || profiles.cursor != "" {
		t.Fatalf("list %d limit %d cursor %q body %s", ok.Code, profiles.limit, profiles.cursor, ok.Body.String())
	}
	var body struct {
		Data []struct {
			ID        string         `json:"id"`
			Reason    string         `json:"reason"`
			Status    string         `json:"status"`
			CreatedAt time.Time      `json:"created_at"`
			Reporter  map[string]any `json:"reporter"`
			Target    map[string]any `json:"target"`
		} `json:"data"`
		Meta struct {
			NextCursor *string `json:"next_cursor"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].ID != profiles.page.Items[0].ID || body.Data[0].Reason != "spam" || body.Data[0].Status != "pending" || body.Meta.NextCursor == nil || *body.Meta.NextCursor != cursor || !bytes.Contains(ok.Body.Bytes(), []byte(`"next_cursor"`)) {
		t.Fatalf("envelope %s", ok.Body.String())
	}
	assertProfileFields(t, body.Data[0].Reporter, reporter.ID, reporter.Username)
	assertProfileFields(t, body.Data[0].Target, target.ID, target.Username)
	if body.Data[0].Reporter["moderation_reason"] != nil || body.Data[0].Target["moderation_reason"] != reason || body.Data[0].Target["moderation_note"] != note {
		t.Fatalf("moderation fields %s", ok.Body.String())
	}

	authed(handler, http.MethodGet, "/v1/moderation/profile-reports?limit=2&cursor=abc", "", nil)
	if profiles.limit != 2 || profiles.cursor != "abc" {
		t.Fatalf("query limit %d cursor %q", profiles.limit, profiles.cursor)
	}
	lists := profiles.lists
	badLimit := authed(handler, http.MethodGet, "/v1/moderation/profile-reports?limit=0", "", nil)
	assertCode(t, badLimit, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(badLimit.Body.Bytes(), []byte(`"limit"`)) || profiles.lists != lists+1 || profiles.limit != 0 {
		t.Fatalf("limit %d %s", profiles.limit, badLimit.Body.String())
	}
	lists = profiles.lists
	badText := authed(handler, http.MethodGet, "/v1/moderation/profile-reports?limit=nope", "", nil)
	assertCode(t, badText, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(badText.Body.Bytes(), []byte(`"limit"`)) || profiles.lists != lists {
		t.Fatalf("non-numeric limit %s", badText.Body.String())
	}

	got := authed(handler, http.MethodGet, "/v1/moderation/profiles/"+target.ID, "", nil)
	if got.Code != http.StatusOK || profiles.userID != target.ID {
		t.Fatalf("profile %d %s", got.Code, got.Body.String())
	}
	var profileBody struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &profileBody); err != nil {
		t.Fatal(err)
	}
	assertProfileFields(t, profileBody.Data, target.ID, target.Username)

	profiles.profileErr = &profile.ValidationError{Fields: map[string]string{"user_id": "must be a canonical uuid"}}
	badID := authed(handler, http.MethodGet, "/v1/moderation/profiles/abc", "", nil)
	assertCode(t, badID, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(badID.Body.Bytes(), []byte(`"user_id"`)) || profiles.userID != "abc" {
		t.Fatalf("user id %s", badID.Body.String())
	}
	profiles.profileErr = profile.ErrNotFound
	assertCode(t, authed(handler, http.MethodGet, "/v1/moderation/profiles/99999999-9999-4999-8999-999999999999", "", nil), http.StatusNotFound, "profile_not_found")
}

func assertProfileFields(t *testing.T, raw map[string]any, id, username string) {
	t.Helper()
	for _, key := range []string{"id", "username", "display_name", "status", "moderation_reason", "moderation_note", "suspended_until", "created_at", "updated_at"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("missing %s in %#v", key, raw)
		}
	}
	if raw["id"] != id || raw["username"] != username {
		t.Fatalf("profile %#v", raw)
	}
}

type readProfiles struct {
	page       profile.ReportPage
	pageErr    error
	limit      int
	cursor     string
	lists      int
	got        profile.Profile
	profileErr error
	userID     string
	gets       int
}

func (p *readProfiles) Get(context.Context, string) (profile.Profile, error) {
	return profile.Profile{}, errors.New("unexpected")
}
func (p *readProfiles) Patch(context.Context, string, profile.Patch) (profile.Profile, error) {
	return profile.Profile{}, errors.New("unexpected")
}
func (p *readProfiles) Delete(context.Context, profile.DeleteCommand) error {
	return errors.New("unexpected")
}
func (p *readProfiles) Report(context.Context, profile.ReportCommand) (profile.Report, error) {
	return profile.Report{}, errors.New("unexpected")
}
func (p *readProfiles) ListReports(_ context.Context, limit int, cursor string) (profile.ReportPage, error) {
	p.lists++
	p.limit = limit
	p.cursor = cursor
	if limit < 1 || limit > 100 {
		return profile.ReportPage{}, &profile.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return p.page, p.pageErr
}
func (p *readProfiles) GetModerationProfile(_ context.Context, userID string) (profile.Profile, error) {
	p.gets++
	p.userID = userID
	return p.got, p.profileErr
}
func (p *readProfiles) Moderate(context.Context, profile.ModerationCommand) (profile.ModerationResult, error) {
	return profile.ModerationResult{}, errors.New("unexpected")
}
func (p *readProfiles) DismissReport(context.Context, string, string) (profile.ReportItem, error) {
	return profile.ReportItem{}, errors.New("unexpected")
}

func TestModerationWrites(t *testing.T) {
	now := time.Date(2026, 10, 16, 17, 0, 0, 0, time.UTC)
	until := now.Add(2 * time.Hour)
	actionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	reporter := profile.Profile{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Username: "reporter", DisplayName: "Reporter", Status: "active",
		CreatedAt: now, UpdatedAt: now,
	}
	target := profile.Profile{
		ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Username: "target", DisplayName: "Target", Status: "suspended",
		CreatedAt: now, UpdatedAt: now,
	}
	reportID := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	profiles := &actionProfiles{
		result: profile.ModerationResult{Created: true, ActionID: &actionID, Profile: target},
		item: profile.ReportItem{
			ID: reportID, Reason: "spam", Status: "dismissed", CreatedAt: now,
			Reporter: reporter, Target: target,
		},
	}
	caller := auth.Caller{UserID: reporter.ID, SessionID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
	authenticator := &fakeAuth{caller: caller}
	handler := New(Options{
		Readiness: readyOK(), PublicLimit: allowAll, Auth: authenticator, Profiles: profiles,
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
	actionPath := "/v1/moderation/profiles/" + target.ID + "/actions"
	dismissPath := "/v1/moderation/profile-reports/" + reportID
	actionBody := []byte(`{"action":"suspended","reason":"spam","note":"see","suspended_until":"` + until.Format(time.RFC3339) + `"}`)

	for _, call := range []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, actionPath, actionBody},
		{http.MethodPatch, dismissPath, []byte(`{"status":"dismissed"}`)},
	} {
		rec := requestWith(handler, call.method, call.path, "Bearer test-token", call.body, map[string]string{
			"Content-Type": "application/json", "Idempotency-Key": "action-key",
		})
		assertCode(t, rec, http.StatusForbidden, "forbidden")
		if !bytes.Contains(rec.Body.Bytes(), []byte(`This action requires a moderator account.`)) {
			t.Fatalf("message %s", rec.Body.String())
		}
	}
	if profiles.moderates != 0 || profiles.dismisses != 0 {
		t.Fatalf("app user reached writes moderates %d dismisses %d", profiles.moderates, profiles.dismisses)
	}
	authenticator.operator = true

	missing := requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", actionBody, map[string]string{
		"Content-Type": "application/json",
	})
	assertCode(t, missing, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(missing.Body.Bytes(), []byte(`"idempotency_key"`)) || profiles.moderates != 0 {
		t.Fatalf("missing key %s", missing.Body.String())
	}

	created := requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", actionBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "action-key",
	})
	if created.Code != http.StatusCreated || profiles.moderates != 1 || profiles.cmd.ActorID != caller.UserID || profiles.cmd.TargetID != target.ID || profiles.cmd.Action != "suspended" || !profiles.cmd.ReasonSet || profiles.cmd.Reason != "spam" || !profiles.cmd.NoteSet || profiles.cmd.Note == nil || *profiles.cmd.Note != "see" || !profiles.cmd.UntilSet || profiles.cmd.SuspendedUntil == nil || !profiles.cmd.SuspendedUntil.Equal(until) || profiles.cmd.Key != "action-key" {
		t.Fatalf("created %d cmd %#v body %s", created.Code, profiles.cmd, created.Body.String())
	}
	if !bytes.Contains(created.Body.Bytes(), []byte(`"action_id":"`+actionID+`"`)) || !bytes.Contains(created.Body.Bytes(), []byte(`"id":"`+target.ID+`"`)) {
		t.Fatalf("created body %s", created.Body.String())
	}

	nullUntil := requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", []byte(`{"action":"rename_required","suspended_until":null}`), map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "null-until",
	})
	if nullUntil.Code != http.StatusCreated || !profiles.cmd.UntilSet || profiles.cmd.SuspendedUntil != nil || profiles.cmd.ReasonSet || profiles.cmd.NoteSet || profiles.cmd.Action != "rename_required" {
		t.Fatalf("null until %d %#v %s", nullUntil.Code, profiles.cmd, nullUntil.Body.String())
	}

	profiles.result = profile.ModerationResult{Created: false, Profile: target}
	unchanged := requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", []byte(`{"action":"suspended","reason":"spam"}`), map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "noop-key",
	})
	if unchanged.Code != http.StatusOK || !bytes.Contains(unchanged.Body.Bytes(), []byte(`"action_id":null`)) {
		t.Fatalf("noop %d %s", unchanged.Code, unchanged.Body.String())
	}

	profiles.moderateErr = idempotency.ErrReused
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", actionBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "action-key",
	}), http.StatusConflict, "idempotency_key_reused")
	profiles.moderateErr = idempotency.ErrInvalid
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", actionBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "short",
	}), http.StatusBadRequest, "validation_error")
	profiles.moderateErr = &profile.RateLimitError{RetryAfter: 3}
	limited := requestWith(handler, http.MethodPost, actionPath, "Bearer test-token", actionBody, map[string]string{
		"Content-Type": "application/json", "Idempotency-Key": "action-key",
	})
	assertCode(t, limited, http.StatusTooManyRequests, "rate_limited")
	if limited.Header().Get("Retry-After") != "3" {
		t.Fatalf("retry %q", limited.Header().Get("Retry-After"))
	}
	profiles.moderateErr = nil

	dismisses := profiles.dismisses
	for _, body := range []string{`{"status":"pending"}`, `{}`, `{"status":"dismissed","note":"x"}`} {
		assertCode(t, authed(handler, http.MethodPatch, dismissPath, "application/json", []byte(body)), http.StatusBadRequest, "validation_error")
	}
	if profiles.dismisses != dismisses {
		t.Fatalf("invalid dismiss called service %d", profiles.dismisses)
	}
	dismissed := authed(handler, http.MethodPatch, dismissPath, "application/json", []byte(`{"status":"dismissed"}`))
	if dismissed.Code != http.StatusOK || profiles.dismissActor != caller.UserID || profiles.reportID != reportID || !bytes.Contains(dismissed.Body.Bytes(), []byte(`"status":"dismissed"`)) || !bytes.Contains(dismissed.Body.Bytes(), []byte(`"id":"`+reportID+`"`)) {
		t.Fatalf("dismiss %d actor %s report %s body %s", dismissed.Code, profiles.dismissActor, profiles.reportID, dismissed.Body.String())
	}
	profiles.dismissErr = profile.ErrReportNotFound
	assertCode(t, authed(handler, http.MethodPatch, dismissPath, "application/json", []byte(`{"status":"dismissed"}`)), http.StatusNotFound, "profile_report_not_found")
	profiles.dismissErr = profile.ErrReportResolved
	assertCode(t, authed(handler, http.MethodPatch, dismissPath, "application/json", []byte(`{"status":"dismissed"}`)), http.StatusConflict, "report_already_resolved")
	profiles.dismissErr = &profile.ValidationError{Fields: map[string]string{"report_id": "must be a canonical uuid"}}
	badID := authed(handler, http.MethodPatch, "/v1/moderation/profile-reports/abc", "application/json", []byte(`{"status":"dismissed"}`))
	assertCode(t, badID, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(badID.Body.Bytes(), []byte(`"report_id"`)) {
		t.Fatalf("report id %s", badID.Body.String())
	}
}

type actionProfiles struct {
	readProfiles
	cmd          profile.ModerationCommand
	result       profile.ModerationResult
	moderateErr  error
	moderates    int
	dismissActor string
	reportID     string
	item         profile.ReportItem
	dismissErr   error
	dismisses    int
}

func (p *actionProfiles) Moderate(_ context.Context, cmd profile.ModerationCommand) (profile.ModerationResult, error) {
	p.moderates++
	p.cmd = cmd
	return p.result, p.moderateErr
}

func (p *actionProfiles) DismissReport(_ context.Context, actorID, reportID string) (profile.ReportItem, error) {
	p.dismisses++
	p.dismissActor = actorID
	p.reportID = reportID
	return p.item, p.dismissErr
}
