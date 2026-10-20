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
	DocsEnabled     bool
	OpenAPI         []byte
	Readiness       Readiness
	PublicLimit     func(http.Handler) http.Handler
	Auth            Authenticator
	Profiles        Profiles
	Logger          *slog.Logger
	Catalogue       Catalogue
	Submissions     Submissions
	VenueModeration VenueModeration
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
	mux.HandleFunc("GET /v1/genres", func(w http.ResponseWriter, r *http.Request) {
		listGenres(w, r, options.Catalogue, options.Logger)
	})
	mux.HandleFunc("GET /v1/venues", func(w http.ResponseWriter, r *http.Request) {
		listVenues(w, r, options.Catalogue, options.Logger)
	})
	mux.HandleFunc("GET /v1/venues/{venueID}", func(w http.ResponseWriter, r *http.Request) {
		getVenue(w, r, options.Catalogue, options.Logger)
	})
	protected := requireApp(options.Auth, options.Logger, profileRoutes(options.Profiles, options.Logger))
	mux.Handle("GET /v1/me", protected)
	mux.Handle("PATCH /v1/me", protected)
	mux.Handle("DELETE /v1/me", protected)
	mux.Handle("POST /v1/profiles/{username}/reports", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createReport(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("POST /v1/venues", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createSubmission(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("GET /v1/me/venues", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listSubmissions(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("GET /v1/me/venues/{venueID}", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getSubmission(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("PUT /v1/me/venues/{venueID}", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correctSubmission(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/profile-reports", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listProfileReports(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/profiles/{userID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getModerationProfile(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("POST /v1/moderation/profiles/{userID}/actions", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyModeration(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("PATCH /v1/moderation/profile-reports/{reportID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dismissProfileReport(w, r, options.Profiles, options.Logger)
	})))
	mux.Handle("POST /v1/venues/{venueID}/reports", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reportVenue(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/venue-reports", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listVenueReports(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/venues/{venueID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getModerationVenue(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("POST /v1/moderation/venues/{venueID}/actions", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyVenueAction(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("PUT /v1/moderation/venues/{venueID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correctVenueAsOperator(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("PATCH /v1/moderation/venue-reports/{reportID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dismissVenueReport(w, r, options.VenueModeration, options.Logger)
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
