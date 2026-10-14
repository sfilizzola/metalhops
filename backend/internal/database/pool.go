package database

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const requiredMigrationVersion = "0002"

type Pool struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 5
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Close() {
	p.pool.Close()
}

func (p *Pool) Check(ctx context.Context) (bool, string) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := p.pool.Ping(pingCtx); err != nil {
		return fail("database_unreachable", err)
	}

	queryCtx, cancelQuery := context.WithTimeout(ctx, 2*time.Second)
	defer cancelQuery()
	var version string
	err := p.pool.QueryRow(queryCtx, `
		select version
		from supabase_migrations.schema_migrations
		order by version desc
		limit 1`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail("migrations_behind", nil)
	}
	if err != nil {
		return fail("migrations_unavailable", err)
	}
	if version < requiredMigrationVersion {
		return fail("migrations_behind", nil)
	}
	return true, "ok"
}

func fail(reason string, err error) (bool, string) {
	if err != nil {
		slog.Error("database check", "reason", reason, "err", err)
	} else {
		slog.Error("database check", "reason", reason)
	}
	return false, reason
}

func Redact(databaseURL, text string) string {
	if databaseURL == "" {
		return text
	}
	return strings.ReplaceAll(text, databaseURL, "DATABASE_URL")
}
