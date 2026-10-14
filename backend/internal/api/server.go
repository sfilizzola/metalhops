package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
)

//go:embed docs.html
var docsHTML []byte

const docsCSP = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

type Readiness interface {
	Check(ctx context.Context) (ready bool, reason string)
}

type ReadinessFunc func(context.Context) (bool, string)

func (f ReadinessFunc) Check(ctx context.Context) (bool, string) {
	return f(ctx)
}

func New(docsEnabled bool, openAPI []byte, ready Readiness) http.Handler {
	return newServer(docsEnabled, openAPI, ready, slog.Default())
}

func newServer(docsEnabled bool, openAPI []byte, ready Readiness, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler(ready))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})
	if docsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	return wrap(logger, mux)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type readyBody struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func readyHandler(ready Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, reason := ready.Check(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if ok {
			_ = json.NewEncoder(w).Encode(readyBody{Status: "ok"})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(readyBody{Status: "not_ready", Reason: reason})
	}
}

func docsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", docsCSP)
	_, _ = w.Write(docsHTML)
}
