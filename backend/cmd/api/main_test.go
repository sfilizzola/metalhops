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

	newMux().ServeHTTP(rec, req)

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

	newMux().ServeHTTP(rec, req)

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
