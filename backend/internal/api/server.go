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

type Options struct {
	DocsEnabled bool
	OpenAPI     []byte
	Readiness   Readiness
	PublicLimit func(http.Handler) http.Handler
	Auth        Authenticator
	Profiles    Profiles
	Logger      *slog.Logger
}

func New(options Options) http.Handler {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.PublicLimit == nil {
		options.PublicLimit = func(next http.Handler) http.Handler { return next }
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler(options.Readiness))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(options.OpenAPI)
	})
	if options.DocsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	protected := requireApp(options.Auth, options.Logger, profileRoutes(options.Profiles, options.Logger))
	mux.Handle("GET /v1/me", protected)
	mux.Handle("PATCH /v1/me", protected)
	mux.Handle("DELETE /v1/me", protected)
	mux.Handle("POST /v1/profiles/{username}/reports", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createReport(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/profile-reports", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listProfileReports(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/profiles/{userID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getModerationProfile(w, r, options.Profiles, options.Logger)
	})))
	return wrap(options.Logger, options.PublicLimit, mux)
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
