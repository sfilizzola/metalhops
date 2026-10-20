package database

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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
		create table auth.sessions (
			id         uuid primary key,
			user_id    uuid not null references auth.users (id) on delete cascade,
			created_at timestamptz not null
		);
		create index auth_sessions_user_id_idx on auth.sessions (user_id);
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
	if ready || reason != "migrations_behind" {
		t.Fatalf("0002 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0003_auth_profiles.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0003')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0003 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0004_profile_safety.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0004')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0004 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0005_venue_catalogue.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0005')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0005 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		insert into venues (name, location, address, city, country_code, updated_at)
		values ('Old Bar', ST_MakePoint(0, 0)::geography, '1 Main Street', 'Oslo', 'NO', '2001-01-01T00:00:00Z')`)
	execSQL(t, pool, readMigration(t, "0006_venue_discovery.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0006')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0006 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0007_venue_submissions.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0007')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0007 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0008_venue_safety.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0008')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0008 ready %v reason %s", ready, reason)
	}
	var searchName, searchCity string
	var updated time.Time
	if err := pool.pool.QueryRow(ctx, `
		select search_name, search_city, updated_at from venues where name = 'Old Bar'`).Scan(&searchName, &searchCity, &updated); err != nil {
		t.Fatal(err)
	}
	if searchName != "oldbar" || searchCity != "oslo" {
		t.Fatalf("backfill %q %q", searchName, searchCity)
	}
	if !updated.Equal(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("backfill moved updated_at to %s", updated)
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
