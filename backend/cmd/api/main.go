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
	"github.com/metalhops/metalhops/backend/internal/config"
	"github.com/metalhops/metalhops/backend/internal/database"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

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

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: api.New(
			cfg.DocsUIEnabled,
			contract.OpenAPI,
			api.ReadinessFunc(pool.Check),
			ratelimit.Middleware(ratelimit.New(time.Now, cfg.TrustedProxies)),
		),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := api.Serve(ctx, srv, 15*time.Second, pool.Close); err != nil {
		slog.Error("server", "err", database.Redact(cfg.DatabaseURL, err.Error()))
		os.Exit(1)
	}
}
