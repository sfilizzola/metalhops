package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

func allowAll(next http.Handler) http.Handler { return next }

func testServer(docs bool, spec []byte, ready Readiness, limit func(http.Handler) http.Handler, logger *slog.Logger) http.Handler {
	return New(Options{DocsEnabled: docs, OpenAPI: spec, Readiness: ready, PublicLimit: limit, Logger: logger})
}

func TestOperationalRoutes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := testServer(true, contract.OpenAPI, ReadinessFunc(func(context.Context) (bool, string) {
		return true, "ok"
	}), allowAll, logger)

	health := get(handler, "/healthz?token=secret")
	if health.Code != http.StatusOK || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health %d %s", health.Code, health.Body.String())
	}
	assertRequestID(t, health)

	spec := get(handler, "/openapi.yaml")
	if spec.Code != http.StatusOK || spec.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("spec %d %q", spec.Code, spec.Header().Get("Content-Type"))
	}
	if spec.Body.String() != string(contract.OpenAPI) {
		t.Fatal("spec bytes differ")
	}
	for _, banned := range []string{`{"data":`} {
		if strings.Contains(spec.Body.String(), banned) {
			t.Fatalf("contract contains %q", banned)
		}
	}

	docs := get(handler, "/docs")
	if docs.Code != http.StatusOK || docs.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("docs %d %q", docs.Code, docs.Header().Get("Content-Type"))
	}
	const csp = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if docs.Header().Get("Content-Security-Policy") != csp {
		t.Fatalf("csp %q", docs.Header().Get("Content-Security-Policy"))
	}
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js",
		"sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6",
		`data-url="/openapi.yaml"`,
		`crossorigin="anonymous"`,
	} {
		if !strings.Contains(docs.Body.String(), want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"proxy", "registry.scalar.com"} {
		if strings.Contains(docs.Body.String(), banned) {
			t.Fatalf("docs page contains %q", banned)
		}
	}

	disabled := testServer(false, contract.OpenAPI, readyOK(), allowAll, logger)
	missing := get(disabled, "/docs")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("disabled docs %d", missing.Code)
	}

	logText := buf.String()
	if !strings.Contains(logText, `"path":"/healthz"`) || strings.Contains(logText, "token=secret") || strings.Contains(logText, "postgres://") {
		t.Fatalf("access log %s", logText)
	}
	if !strings.Contains(logText, `"method":"GET"`) || !strings.Contains(logText, `"status":200`) || !strings.Contains(logText, `"duration_ms"`) || !strings.Contains(logText, `"request_id"`) {
		t.Fatalf("access log fields %s", logText)
	}
}

func TestCORS(t *testing.T) {
	handler := New(Options{Readiness: readyOK(), PublicLimit: allowAll, AllowedOrigins: []string{"https://metalhops.app"}})

	preflight := httptest.NewRequest(http.MethodOptions, "/v1/genres", nil)
	preflight.Header.Set("Origin", "https://metalhops.app")
	preflight.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, preflight)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://metalhops.app" {
		t.Fatalf("allow-origin %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" || rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("missing preflight headers")
	}

	trusted := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	trusted.Header.Set("Origin", "https://metalhops.app")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, trusted)
	if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "https://metalhops.app" {
		t.Fatalf("trusted origin %d %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
	}

	untrusted := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	untrusted.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, untrusted)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("leaked header for untrusted origin %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}

	disabled := New(Options{Readiness: readyOK(), PublicLimit: allowAll})
	rec = httptest.NewRecorder()
	disabled.ServeHTTP(rec, trusted)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("CORS active with no AllowedOrigins: %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestReadyz(t *testing.T) {
	sha := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	ok := get(New(Options{Readiness: readyOK(), PublicLimit: allowAll, Commit: sha}), "/readyz")
	if ok.Code != http.StatusOK || ok.Body.String() != `{"status":"ok","commit":"`+sha+"\"}\n" {
		t.Fatalf("ready %d %s", ok.Code, ok.Body.String())
	}
	for _, commit := range []string{"", "abc1234", strings.ToUpper(sha), sha + "0"} {
		rec := get(New(Options{Readiness: readyOK(), PublicLimit: allowAll, Commit: commit}), "/readyz")
		if rec.Code != http.StatusOK || rec.Body.String() != "{\"status\":\"ok\"}\n" {
			t.Fatalf("%q ready %d %s", commit, rec.Code, rec.Body.String())
		}
	}
	for _, reason := range []string{"database_unreachable", "migrations_unavailable", "migrations_behind"} {
		handler := New(Options{Readiness: ReadinessFunc(func(context.Context) (bool, string) {
			return false, reason
		}), PublicLimit: allowAll, Commit: sha})
		rec := get(handler, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status %d", reason, rec.Code)
		}
		if rec.Body.String() != `{"status":"not_ready","reason":"`+reason+"\"}\n" {
			t.Fatalf("%s body %s", reason, rec.Body.String())
		}
	}
}

func TestPanicEnvelope(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := wrap(logger, allowAll, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := get(handler, "/boom")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "goroutine") || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("body leaked panic: %s", rec.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "internal_error" || got.Error.Message != "The server could not complete the request." {
		t.Fatalf("error %#v", got.Error)
	}
	if rec.Header().Get("X-Request-ID") != got.Error.RequestID {
		t.Fatalf("header %q body %q", rec.Header().Get("X-Request-ID"), got.Error.RequestID)
	}
	if !strings.Contains(buf.String(), `"status":500`) || !strings.Contains(buf.String(), got.Error.RequestID) {
		t.Fatalf("panic log %s", buf.String())
	}
}

func TestRandomFailure(t *testing.T) {
	previous := readRandom
	readRandom = func([]byte) (int, error) { return 0, context.Canceled }
	t.Cleanup(func() { readRandom = previous })

	var buf bytes.Buffer
	handler := testServer(false, nil, readyOK(), allowAll, slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := get(handler, "/healthz")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"unavailable"`) {
		t.Fatalf("body %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), `"status":500`) {
		t.Fatalf("log %s", buf.String())
	}
}

func TestPublicRateLimit(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	limiter := ratelimit.New(func() time.Time { return now }, nil)
	handler := testServer(false, contract.OpenAPI, ReadinessFunc(func(context.Context) (bool, string) {
		return false, "migrations_behind"
	}), ratelimit.Middleware(limiter), slog.Default())

	for i := 0; i < 120; i++ {
		rec := get(handler, "/openapi.yaml")
		if rec.Code != http.StatusOK {
			t.Fatalf("allow %d status %d", i, rec.Code)
		}
	}
	denied := get(handler, "/openapi.yaml")
	if denied.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", denied.Code)
	}
	if !strings.HasSuffix(denied.Body.String(), "\n") || strings.Contains(denied.Body.String(), `"fields"`) {
		t.Fatalf("body %s", denied.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(denied.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "rate_limited" || got.Error.Message != "Too many requests." {
		t.Fatalf("error %#v", got.Error)
	}
	if denied.Header().Get("X-Request-ID") != got.Error.RequestID || len(got.Error.RequestID) != 32 {
		t.Fatalf("request id %q", got.Error.RequestID)
	}
	retry, err := strconv.Atoi(denied.Header().Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Fatalf("retry %q", denied.Header().Get("Retry-After"))
	}
	if get(handler, "/healthz").Code != http.StatusOK {
		t.Fatal("healthz was limited")
	}
	ready := get(handler, "/readyz")
	if ready.Code != http.StatusServiceUnavailable || ready.Body.String() != "{\"status\":\"not_ready\",\"reason\":\"migrations_behind\"}\n" {
		t.Fatalf("ready %d %s", ready.Code, ready.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusTooManyRequests {
		t.Fatalf("post health %d", postRec.Code)
	}
	missing := get(handler, "/missing")
	if missing.Code != http.StatusTooManyRequests {
		t.Fatalf("missing %d", missing.Code)
	}
}

func get(handler http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func readyOK() Readiness {
	return ReadinessFunc(func(context.Context) (bool, string) { return true, "ok" })
}

func assertRequestID(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatalf("request id %q", id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("request id %q", id)
		}
	}
}
