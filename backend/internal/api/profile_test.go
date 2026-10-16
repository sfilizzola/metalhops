package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type scriptProfiles struct {
	getErr    error
	got       profile.Profile
	patchErr  error
	patched   profile.Profile
	deleteErr error
	patch     profile.Patch
	command   profile.DeleteCommand
	reportErr error
	report    profile.Report
	reportCmd profile.ReportCommand
	gets      int
	patches   int
	deletes   int
	reports   int
}

func (s *scriptProfiles) Get(context.Context, string) (profile.Profile, error) {
	s.gets++
	return s.got, s.getErr
}
func (s *scriptProfiles) Patch(_ context.Context, _ string, patch profile.Patch) (profile.Profile, error) {
	s.patches++
	s.patch = patch
	return s.patched, s.patchErr
}
func (s *scriptProfiles) Delete(_ context.Context, cmd profile.DeleteCommand) error {
	s.deletes++
	s.command = cmd
	return s.deleteErr
}
func (s *scriptProfiles) Report(_ context.Context, cmd profile.ReportCommand) (profile.Report, error) {
	s.reports++
	s.reportCmd = cmd
	return s.report, s.reportErr
}

type rejectingProfiles struct{}

func (rejectingProfiles) Get(context.Context, string) (profile.Profile, error) {
	return profile.Profile{}, errors.New("unexpected")
}
func (rejectingProfiles) Patch(context.Context, string, profile.Patch) (profile.Profile, error) {
	return profile.Profile{}, errors.New("unexpected")
}
func (rejectingProfiles) Delete(context.Context, profile.DeleteCommand) error {
	return errors.New("unexpected")
}
func (rejectingProfiles) Report(context.Context, profile.ReportCommand) (profile.Report, error) {
	return profile.Report{}, errors.New("unexpected")
}

func TestProfileRoutes(t *testing.T) {
	caller := auth.Caller{
		UserID:           "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SessionID:        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		SessionCreatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}
	stored := profile.Profile{
		ID: caller.UserID, Username: "metal_fan", DisplayName: "Metal Fan", Status: "active",
		CreatedAt: caller.SessionCreatedAt, UpdatedAt: caller.SessionCreatedAt,
	}
	profiles := &scriptProfiles{got: stored, patched: stored}
	var logs bytes.Buffer
	authenticator := &fakeAuth{caller: caller}
	handler := New(Options{
		DocsEnabled: true,
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    profiles,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	})

	ok := authed(handler, http.MethodGet, "/v1/me", "", nil)
	if ok.Code != http.StatusOK || bytes.Contains(ok.Body.Bytes(), []byte("created_at")) {
		t.Fatalf("get %d %s", ok.Code, ok.Body.String())
	}
	if ok.Body.String() != `{"data":{"id":"`+caller.UserID+`","username":"metal_fan","display_name":"Metal Fan","status":"active","moderation_reason":null,"moderation_note":null,"suspended_until":null}}`+"\n" {
		t.Fatalf("body %s", ok.Body.String())
	}

	profiles.getErr = profile.ErrNotFound
	assertCode(t, authed(handler, http.MethodGet, "/v1/me", "", nil), http.StatusNotFound, "profile_not_found")

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		authenticator.err = auth.ErrUnauthorized
		assertCode(t, authed(handler, method, "/v1/me", "", []byte(`{}`)), http.StatusUnauthorized, "unauthorized")
		authenticator.err = errors.New("db down")
		assertCode(t, authed(handler, method, "/v1/me", "", []byte(`{}`)), http.StatusInternalServerError, "internal_error")
		authenticator.err = auth.ErrProvider
		assertCode(t, authed(handler, method, "/v1/me", "", []byte(`{}`)), http.StatusBadGateway, "auth_provider_error")
		authenticator.err = nil
	}
	if bytes.Contains(logs.Bytes(), []byte("test-token")) || bytes.Contains(logs.Bytes(), []byte("db down")) && bytes.Contains(logs.Bytes(), []byte("Bearer")) {
		t.Fatalf("log %s", logs.String())
	}

	username := "Metal_Fan"
	profiles.patchErr = nil
	rec := authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":"Metal_Fan"}`))
	if rec.Code != http.StatusOK || profiles.patch.Username == nil || *profiles.patch.Username != username || profiles.patch.DisplayName != nil {
		t.Fatalf("patch %d %#v", rec.Code, profiles.patch)
	}
	rec = requestWith(handler, http.MethodPatch, "/v1/me", "Bearer test-token", []byte(`{"display_name":"Metal Fan"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "not-required",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotency %d %s", rec.Code, rec.Body.String())
	}
	assertCode(t, authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":null}`)), http.StatusBadRequest, "validation_error")
	profiles.patchErr = profile.ErrUsernameReserved
	assertCode(t, authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":"admin"}`)), http.StatusBadRequest, "username_reserved")
	profiles.patchErr = profile.ErrUsernameTaken
	assertCode(t, authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":"taken"}`)), http.StatusConflict, "username_taken")
	profiles.patchErr = &profile.ValidationError{Fields: map[string]string{"username": "required"}}
	assertCode(t, authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{}`)), http.StatusBadRequest, "validation_error")
	profiles.patchErr = &profile.RateLimitError{RetryAfter: 3}
	limited := authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":"metal_fan"}`))
	assertCode(t, limited, http.StatusTooManyRequests, "rate_limited")
	if limited.Header().Get("Retry-After") != "3" {
		t.Fatalf("retry %q", limited.Header().Get("Retry-After"))
	}
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	profiles.patchErr = &profile.SuspendedError{Until: &until}
	suspended := authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"display_name":"Metal Fan"}`))
	assertCode(t, suspended, http.StatusForbidden, "profile_suspended")
	if !bytes.Contains(suspended.Body.Bytes(), []byte(`"details":{"suspended_until":"2026-11-01T00:00:00Z"}`)) {
		t.Fatalf("details %s", suspended.Body.String())
	}
	profiles.patchErr = &profile.SuspendedError{}
	open := authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"display_name":"Metal Fan"}`))
	assertCode(t, open, http.StatusForbidden, "profile_suspended")
	if bytes.Contains(open.Body.Bytes(), []byte(`"details"`)) {
		t.Fatalf("nil until %s", open.Body.String())
	}
	profiles.patchErr = nil
	plain := authed(handler, http.MethodPatch, "/v1/me", "text/plain", []byte(`{}`))
	assertCode(t, plain, http.StatusUnsupportedMediaType, "unsupported_media_type")
	huge := bytes.Repeat([]byte("a"), (1<<16)+1)
	assertCode(t, authed(handler, http.MethodPatch, "/v1/me", "", huge), http.StatusRequestEntityTooLarge, "body_too_large")

	rec = authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"DELETE MY ACCOUNT"}`))
	if rec.Code != http.StatusNoContent || profiles.command.Confirmation != "DELETE MY ACCOUNT" || profiles.command.UserID != caller.UserID {
		t.Fatalf("delete %d %#v", rec.Code, profiles.command)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("delete body %s", rec.Body.String())
	}
	profiles.deleteErr = &profile.ValidationError{Fields: map[string]string{"confirmation": "must equal DELETE MY ACCOUNT"}}
	assertCode(t, authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"no"}`)), http.StatusBadRequest, "validation_error")
	profiles.deleteErr = nil
	profiles.deleteErr = profile.ErrRecentAuthenticationRequired
	assertCode(t, authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"DELETE MY ACCOUNT"}`)), http.StatusForbidden, "recent_authentication_required")
	profiles.deleteErr = profile.ErrAuthProvider
	assertCode(t, authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"DELETE MY ACCOUNT"}`)), http.StatusBadGateway, "auth_provider_error")
	profiles.deleteErr = &profile.RateLimitError{RetryAfter: 2}
	limited = authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"DELETE MY ACCOUNT"}`))
	assertCode(t, limited, http.StatusTooManyRequests, "rate_limited")
	if limited.Header().Get("Retry-After") != "2" {
		t.Fatalf("delete retry %q", limited.Header().Get("Retry-After"))
	}

	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	limiter := ratelimit.New(func() time.Time { return now }, nil)
	authenticator.calls = 0
	limitedHandler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: ratelimit.Middleware(limiter),
		Auth:        authenticator,
		Profiles:    profiles,
	})
	for i := 0; i < 120; i++ {
		if authed(limitedHandler, http.MethodGet, "/v1/me", "", nil).Code == http.StatusTooManyRequests {
			t.Fatalf("public limit early %d", i)
		}
	}
	calls := authenticator.calls
	denied := authed(limitedHandler, http.MethodGet, "/v1/me", "", nil)
	assertCode(t, denied, http.StatusTooManyRequests, "rate_limited")
	if authenticator.calls != calls {
		t.Fatal("public limit ran after authentication")
	}
}

func TestOperatorCannotCreateProfile(t *testing.T) {
	caller := auth.Caller{
		UserID:           "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SessionID:        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		SessionCreatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}
	profiles := &scriptProfiles{}
	authenticator := &fakeAuth{caller: caller, operator: true}
	handler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    profiles,
	})

	patch := authed(handler, http.MethodPatch, "/v1/me", "", []byte(`{"username":"metal_fan","display_name":"Metal Fan"}`))
	assertForbidden(t, patch)
	if profiles.patches != 0 {
		t.Fatalf("patch calls %d", profiles.patches)
	}

	got := authed(handler, http.MethodGet, "/v1/me", "", nil)
	assertForbidden(t, got)
	if profiles.gets != 0 {
		t.Fatalf("get calls %d", profiles.gets)
	}

	deleted := authed(handler, http.MethodDelete, "/v1/me", "", []byte(`{"confirmation":"DELETE MY ACCOUNT"}`))
	assertForbidden(t, deleted)
	if profiles.deletes != 0 {
		t.Fatalf("delete calls %d", profiles.deletes)
	}
}

func TestOperatorLookupError(t *testing.T) {
	profiles := &scriptProfiles{}
	authenticator := &fakeAuth{
		caller:      auth.Caller{UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		operatorErr: errors.New("auth: operator lookup: db down"),
	}
	var logs bytes.Buffer
	handler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    profiles,
		Logger:      slog.New(slog.NewJSONHandler(&logs, nil)),
	})

	rec := authed(handler, http.MethodGet, "/v1/me", "", nil)
	assertCode(t, rec, http.StatusInternalServerError, "internal_error")
	if profiles.gets != 0 {
		t.Fatalf("get calls %d", profiles.gets)
	}
	if !bytes.Contains(logs.Bytes(), []byte("auth: operator lookup: db down")) || bytes.Contains(logs.Bytes(), []byte("test-token")) {
		t.Fatalf("log %s", logs.String())
	}
}

func TestReportRoute(t *testing.T) {
	caller := auth.Caller{
		UserID:           "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		SessionID:        "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		SessionCreatedAt: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
	}
	targetID := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	created := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	profiles := &scriptProfiles{report: profile.Report{
		ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Username: "metalfan", Reason: "spam", Status: "pending", CreatedAt: created,
	}}
	authenticator := &fakeAuth{caller: caller}
	handler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    profiles,
	})

	rec := requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	})
	want := `{"data":{"id":"cccccccc-cccc-4ccc-8ccc-cccccccccccc","username":"metalfan","reason":"spam","note":null,"status":"pending","created_at":"2026-10-15T12:00:00Z"}}` + "\n"
	if rec.Code != http.StatusCreated || rec.Body.String() != want {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(targetID)) || bytes.Contains(rec.Body.Bytes(), []byte(caller.UserID)) {
		t.Fatalf("user id %s", rec.Body.String())
	}
	if profiles.reports != 1 || profiles.reportCmd.Username != "metalfan" || profiles.reportCmd.Reason != "spam" || profiles.reportCmd.Note != nil || profiles.reportCmd.Key != "report-key" || profiles.reportCmd.ReporterID != caller.UserID {
		t.Fatalf("command %#v calls %d", profiles.reportCmd, profiles.reports)
	}

	missing := authed(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "", []byte(`{"reason":"spam"}`))
	assertCode(t, missing, http.StatusBadRequest, "validation_error")
	if profiles.reports != 1 {
		t.Fatalf("missing key calls %d", profiles.reports)
	}

	authenticator.operator = true
	forbidden := requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	})
	assertForbidden(t, forbidden)
	if profiles.reports != 1 {
		t.Fatalf("operator calls %d", profiles.reports)
	}
	authenticator.operator = false

	profiles.reportErr = profile.ErrReportPending
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusConflict, "profile_already_reported")
	profiles.reportErr = profile.ErrSelfReport
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusBadRequest, "self_report")
	profiles.reportErr = profile.ErrProfileRequired
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusConflict, "profile_required")
	profiles.reportErr = profile.ErrRenameRequired
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusForbidden, "profile_rename_required")
	profiles.reportErr = profile.ErrNotFound
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusNotFound, "profile_not_found")
	profiles.reportErr = idempotency.ErrReused
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusConflict, "idempotency_key_reused")
	profiles.reportErr = idempotency.ErrInvalid
	assertCode(t, requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	}), http.StatusBadRequest, "validation_error")
	profiles.reportErr = &profile.QuotaError{RetryAfter: 17}
	limited := requestWith(handler, http.MethodPost, "/v1/profiles/metalfan/reports", "Bearer test-token", []byte(`{"reason":"spam"}`), map[string]string{
		"Content-Type":    "application/json",
		"Idempotency-Key": "report-key",
	})
	assertCode(t, limited, http.StatusTooManyRequests, "quota_exceeded")
	if limited.Header().Get("Retry-After") != "17" {
		t.Fatalf("retry %q", limited.Header().Get("Retry-After"))
	}
}

func assertForbidden(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	assertCode(t, rec, http.StatusForbidden, "forbidden")
	if !bytes.Contains(rec.Body.Bytes(), []byte(`This action is unavailable for this account.`)) {
		t.Fatalf("message %s", rec.Body.String())
	}
}

func authed(handler http.Handler, method, target, media string, body []byte) *httptest.ResponseRecorder {
	if media == "" && body != nil {
		media = "application/json"
	}
	return requestWith(handler, method, target, "Bearer test-token", body, map[string]string{"Content-Type": media})
}

func requestWith(handler http.Handler, method, target, authz string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"`+code+`"`)) || rec.Header().Get("X-Request-ID") == "" {
		t.Fatalf("%s: %d %s", code, rec.Code, rec.Body.String())
	}
}
