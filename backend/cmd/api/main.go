package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/metalhops/metalhops/backend"
	"github.com/metalhops/metalhops/backend/internal/api"
	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/checkin"
	"github.com/metalhops/metalhops/backend/internal/config"
	"github.com/metalhops/metalhops/backend/internal/database"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
	"github.com/metalhops/metalhops/backend/internal/review"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

const version = "v0.1.0-dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(1)
	}

	pool, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("database pool", "err", database.Redact(cfg.DatabaseURL, err.Error()))
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	userLimiter := ratelimit.NewUserLimiter(time.Now)
	authenticator := auth.New(cfg.SupabaseURL, cfg.SupabaseJWTAudience, pool.Raw(), httpClient, time.Now)
	admin := profile.NewAdminClient(cfg.SupabaseURL, cfg.SupabaseServiceRoleKey, httpClient)
	profiles := profile.New(pool.Raw(), userLimiter, admin, time.Now, cfg.SupabaseURL)
	venues := venue.New(pool.Raw(), profiles, userLimiter, time.Now)
	reviews := review.New(pool.Raw(), profiles, userLimiter, time.Now)
	checkIns := checkin.New(pool.Raw(), profiles, userLimiter, time.Now, cfg.CheckInSpacing)
	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.New(api.Options{
			DocsEnabled:      cfg.DocsUIEnabled,
			OpenAPI:          contract.OpenAPI,
			Readiness:        api.ReadinessFunc(pool.Check),
			PublicLimit:      ratelimit.Middleware(ratelimit.New(time.Now, cfg.TrustedProxies)),
			AllowedOrigins:   cfg.AllowedOrigins,
			Auth:             authenticator,
			Profiles:         profiles,
			Catalogue:        venues,
			Submissions:      venues,
			VenueModeration:  venues,
			Reviews:          reviews,
			ReviewModeration: reviews,
			CheckIns:         checkIns,
			Commit:           cfg.Commit,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := api.Serve(ctx, srv, version, cfg.DocsUIEnabled, 15*time.Second, pool.Close); err != nil {
		slog.Error("server", "err", database.Redact(cfg.DatabaseURL, err.Error()))
		os.Exit(1)
	}
}
