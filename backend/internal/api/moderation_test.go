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
