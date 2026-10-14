package database

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestMigrationVersions(t *testing.T) {
	url := pgtest.EmptyURL(t)
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
			if not exists (select 1 from pg_roles where rolname = 'anon') then
				create role anon nologin nosuperuser nobypassrls;
			end if;
			if not exists (select 1 from pg_roles where rolname = 'authenticated') then
				create role authenticated nologin nosuperuser nobypassrls;
			end if;
			if not exists (select 1 from pg_roles where rolname = 'service_role') then
				create role service_role nologin nosuperuser bypassrls;
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
	execSQL(t, pool, readMigration(t, "0001_init.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0001')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0001 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0002_api_safety.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0002')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0002 ready %v reason %s", ready, reason)
	}
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

func readMigration(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
