package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metalhops/metalhops/backend"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	newMux(false).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestOpenAPI(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)

	newMux(false).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("content type %q", got)
	}
	body := rec.Body.String()
	if body != string(contract.OpenAPI) {
		t.Fatalf("body does not match the embedded contract: %q", body)
	}
	for _, want := range []string{"openapi: 3.1.0", "/healthz", "http://localhost:8080"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"/v1", "bearer", `{"data":`} {
		if strings.Contains(body, banned) {
			t.Fatalf("contract contains %q", banned)
		}
	}
}

func TestDocsEnabled(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)

	newMux(true).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type %q", got)
	}
	const csp = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if got := rec.Header().Get("Content-Security-Policy"); got != csp {
		t.Fatalf("csp %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js",
		"sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6",
		`data-url="/openapi.yaml"`,
		`crossorigin="anonymous"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"proxy", "registry.scalar.com"} {
		if strings.Contains(body, banned) {
			t.Fatalf("docs page contains %q", banned)
		}
	}
}

func TestDocsDisabled(t *testing.T) {
	mux := newMux(false)

	docs := httptest.NewRecorder()
	mux.ServeHTTP(docs, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docs.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", docs.Code)
	}

	spec := httptest.NewRecorder()
	mux.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if spec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", spec.Code)
	}
}

func TestDocsUIEnabled(t *testing.T) {
	ok, err := docsUIEnabled("")
	if err != nil || !ok {
		t.Fatalf("empty: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("true")
	if err != nil || !ok {
		t.Fatalf("true: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("false")
	if err != nil || ok {
		t.Fatalf("false: enabled=%v err=%v", ok, err)
	}
	for _, value := range []string{"TRUE", "1", " true"} {
		enabled, gotErr := docsUIEnabled(value)
		if gotErr == nil || enabled {
			t.Fatalf("%q: enabled=%v err=%v", value, enabled, gotErr)
		}
		if gotErr.Error() != "DOCS_UI_ENABLED must be true, false, or empty" {
			t.Fatalf("%q error %q", value, gotErr.Error())
		}
	}
}
