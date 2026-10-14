package pgtest

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func EmptyURL(t *testing.T) string {
	t.Helper()
	return start(t)
}

func MigratedURL(t *testing.T) string {
	t.Helper()
	url := start(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	exec(t, pool, `
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
		create table if not exists auth.users (
			id uuid primary key
		);
		create or replace function auth.uid() returns uuid as $$
			select null::uuid
		$$ language sql stable;
		grant usage on schema public to anon, authenticated, service_role;
		alter default privileges in schema public grant all on tables to anon, authenticated, service_role;
		alter default privileges in schema public grant all on sequences to anon, authenticated, service_role`)
	for _, path := range migrationFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, pool, string(body))
	}
	return url
}

func start(t *testing.T) string {
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

func exec(t *testing.T, pool *pgxpool.Pool, sql string) {
	t.Helper()
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	results := conn.Conn().PgConn().Exec(context.Background(), sql)
	if err := results.Close(); err != nil {
		t.Fatal(err)
	}
}

func migrationFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(migrationsDir(t))
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			paths = append(paths, filepath.Join(migrationsDir(t), entry.Name()))
		}
	}
	sort.Strings(paths)
	return paths
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations")
}
