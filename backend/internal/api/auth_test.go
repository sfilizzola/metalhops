package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metalhops/metalhops/backend/internal/auth"
)

type fakeAuth struct {
	calls       int
	token       string
	caller      auth.Caller
	err         error
	operator    bool
	operatorErr error
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (auth.Caller, error) {
	f.calls++
	f.token = token
	return f.caller, f.err
}

func (f *fakeAuth) IsOperator(context.Context, string) (bool, error) {
	return f.operator, f.operatorErr
}

func TestAuthMiddleware(t *testing.T) {
	caller := auth.Caller{UserID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", SessionID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}
	authenticator := &fakeAuth{caller: caller}
	var seen auth.Caller
	protected := requireAuth(authenticator, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seen, ok = CallerFrom(r.Context())
		if !ok {
			t.Fatal("missing caller")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	handler := New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Profiles:    rejectingProfiles{},
	})

	for _, header := range []string{"", "Basic token", "Bearer", "Bearer "} {
		rec := call(protected, http.MethodGet, "/v1/me", header)
		if rec.Code != http.StatusUnauthorized || authenticator.calls != 0 {
			t.Fatalf("%q status %d calls %d", header, rec.Code, authenticator.calls)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	req.Header.Add("Authorization", "Bearer one")
	req.Header.Add("Authorization", "Bearer two")
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || authenticator.calls != 0 {
		t.Fatalf("duplicate %d calls %d", rec.Code, authenticator.calls)
	}

	rec = call(protected, http.MethodGet, "/v1/me", "Bearer token")
	if rec.Code != http.StatusNoContent || seen.UserID != caller.UserID || authenticator.token != "token" {
		t.Fatalf("accepted %d %#v token %q", rec.Code, seen, authenticator.token)
	}

	for _, tc := range []struct {
		err    error
		status int
	}{
		{auth.ErrUnauthorized, http.StatusUnauthorized},
		{auth.ErrProvider, http.StatusBadGateway},
		{errors.New("db down"), http.StatusInternalServerError},
	} {
		authenticator.err = tc.err
		rec = call(protected, http.MethodGet, "/v1/me", "Bearer token")
		if rec.Code != tc.status {
			t.Fatalf("%v status %d body %s", tc.err, rec.Code, rec.Body.String())
		}
	}

	before := authenticator.calls
	for _, path := range []string{"/healthz", "/readyz", "/openapi.yaml", "/docs"} {
		if get(handler, path).Code == 0 {
			t.Fatal(path)
		}
	}
	if authenticator.calls != before {
		t.Fatalf("operational routes authenticated: %d", authenticator.calls-before)
	}
}

func call(handler http.Handler, method, target, authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
