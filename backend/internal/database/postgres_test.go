package database

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// shortcut: harness stays in database tests until a second package needs it.

func TestMigrationVersions(t *testing.T) {
	url := startPostGIS(t)
	ctx := context.Background()

	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	ready, reason := pool.Check(ctx)
	if ready || reason != "migrations_unavailable" {
		t.Fatalf("empty ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		create schema if not exists supabase_migrations;
		create table supabase_migrations.schema_migrations (
			version text primary key
		)`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("empty history ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0000')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0000 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		do $$
		begin
			if not exists (select 1 from pg_roles where rolname = 'authenticated') then
				create role authenticated;
			end if;
		end
		$$;
		create schema if not exists auth;
		create table auth.users (
			id uuid primary key
		);
		create function auth.uid() returns uuid as $$
			select null::uuid
		$$ language sql stable`)
	execSQL(t, pool, readMigration(t))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0001')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0001 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0002')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0002 ready %v reason %s", ready, reason)
	}
}

func startPostGIS(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, err := postgres.Run(ctx, "postgis/postgis:16-3.5",
		postgres.WithDatabase("metalhops"),
		postgres.WithUsername("metalhops"),
		postgres.WithPassword("metalhops"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		if os.Getenv("CI") == "" && (strings.Contains(err.Error(), "docker") || strings.Contains(err.Error(), "Docker")) {
			t.Skip("Docker is unavailable: " + err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func execSQL(t *testing.T, pool *Pool, sql string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	results := conn.Conn().PgConn().Exec(ctx, sql)
	if err := results.Close(); err != nil {
		t.Fatal(err)
	}
}

func readMigration(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations", "0001_init.sql")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
