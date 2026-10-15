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
}

func (s *scriptProfiles) Get(context.Context, string) (profile.Profile, error) {
	return s.got, s.getErr
}
func (s *scriptProfiles) Patch(_ context.Context, _ string, patch profile.Patch) (profile.Profile, error) {
	s.patch = patch
	return s.patched, s.patchErr
}
func (s *scriptProfiles) Delete(_ context.Context, cmd profile.DeleteCommand) error {
	s.command = cmd
	return s.deleteErr
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
	if ok.Body.String() != `{"data":{"id":"`+caller.UserID+`","username":"metal_fan","display_name":"Metal Fan","status":"active"}}`+"\n" {
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
