package database

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestOpenDoesNotDialAndCheckReportsUnreachable(t *testing.T) {
	ctx := context.Background()
	const url = "postgres://metalhops:s3cret@127.0.0.1:1/metalhops"
	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if pool.pool.Stat().MaxConns() != 5 || pool.pool.Stat().TotalConns() != 0 {
		t.Fatalf("max %d total %d", pool.pool.Stat().MaxConns(), pool.pool.Stat().TotalConns())
	}

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ready, reason := pool.Check(ctx)
	if ready || reason != "database_unreachable" {
		t.Fatalf("ready %v reason %s", ready, reason)
	}
	if strings.Contains(buf.String(), "s3cret") || strings.Contains(buf.String(), "postgres://") {
		t.Fatalf("log contains url: %s", buf.String())
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	ready, reason = pool.Check(canceled)
	if ready || reason != "database_unreachable" {
		t.Fatalf("canceled ready %v reason %s", ready, reason)
	}
}

func TestRedactRemovesURL(t *testing.T) {
	got := Redact("postgres://user:s3cret@localhost/db", "failed postgres://user:s3cret@localhost/db")
	if got != "failed DATABASE_URL" || strings.Contains(got, "s3cret") {
		t.Fatalf("redacted %q", got)
	}
}
