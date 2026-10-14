# Session 4 API Safety Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close direct Data API access, add the in-transaction idempotency helper, and limit public traffic to 120 requests per minute per client IP.

**Architecture:** Migration `0002_api_safety.sql` drops the four `0001` policies, revokes Data API grants, and creates `idempotency_records`. `internal/idempotency.Execute` runs inside the caller's `pgx` transaction and has no HTTP route. `internal/ratelimit` owns the token buckets and client-IP walk. `cmd/api` passes its middleware into `internal/api`, which runs it after the request ID and inside panic recovery. `internal/pgtest` is the shared PostGIS harness.

**Tech Stack:** Go 1.24, `net/http`, `github.com/jackc/pgx/v5`, `golang.org/x/time/rate`, `github.com/testcontainers/testcontainers-go/modules/postgres`, image `postgis/postgis:16-3.5`.

**Spec:** `docs/superpowers/specs/2026-10-14-session-4-api-safety-design.md`

## Global Constraints

- Leave `infra/supabase/migrations/0001_init.sql` unchanged.
- Add no `/v1` route, no `Idempotency-Key` parsing, and no OpenAPI path.
- `GET /healthz` and `GET /readyz` gain no `429` response. They are exempt.
- Add `golang.org/x/time/rate`. DevSteps already approves it.
- `requiredMigrationVersion` becomes the string `0002`.
- Idempotency primary key is `(user_id, operation, key)`. `ErrInvalid` and `ErrReused` are sentinels compared with `errors.Is`.
- A row is live while `expires_at` is greater than the supplied `now`. Inserts set `expires_at` to `now` plus 24 hours.
- `Execute` does not commit or roll back.
- Each bucket is `rate.NewLimiter(rate.Limit(2), 120)`. The map holds at most 10,000 keys. Idle means last use 3 minutes or more ago. A full map refuses a new key with `Retry-After: 60`.
- `TRUSTED_PROXIES` empty or unset means no proxies. Any other failure returns exactly `TRUSTED_PROXIES must be a comma-separated list of CIDRs`.
- Refusal envelope: status `429`, code `rate_limited`, message `Too many requests.`, no `fields`, `json.Encoder` trailing newline, existing `X-Request-ID` kept.
- `render.yaml` pins the Cloudflare lists from 14 October 2026 plus `10.0.0.0/8`. `172.16.0.0/12` and `192.168.0.0/16` are absent.
- Do not run `supabase db push`. Do not mark Session 4 done in `DevSteps.md` before Task 8.
- Work on `feature/session-4-api-safety`, targeting `dev` by pull request.
- Commands in this plan use `git` and `go`. Run `go` commands from `backend/`.

## Review Focus

- The same idempotency key for two users replays the first user's response to the second. Expected: both callbacks run. Pinned in Task 4.
- The same user and key under two operations share one stored response. Expected: both callbacks run. Pinned in Task 4.
- An unparseable `X-Forwarded-For` hop becomes the client or fails the request. Expected: that hop is skipped and the next untrusted address is the key. Pinned in Task 6.
- An IPv6 client behind a Cloudflare v6 hop is stored as the Cloudflare address. Expected: the client address is the key. Pinned in Task 6.
- Different idempotency keys share one advisory lock, so one creation blocks the other. Expected: both callbacks are inside `Execute` at the same time. Pinned in Task 4.

## File map

- `backend/internal/pgtest/pgtest.go` — `EmptyURL`, `MigratedURL`.
- `backend/internal/pgtest/pgtest_test.go` — migrated genre count.
- `backend/internal/database/postgres_test.go` — readiness uses `EmptyURL`; version `0002`.
- `backend/internal/database/pool.go` — `requiredMigrationVersion`.
- `backend/internal/database/access_test.go` — Data API denial.
- `infra/supabase/migrations/0002_api_safety.sql` — policies, grants, idempotency table.
- `backend/internal/idempotency/idempotency.go` — `Execute`.
- `backend/internal/idempotency/idempotency_test.go` — helper behavior.
- `backend/internal/config/config.go` — `TrustedProxies`.
- `backend/internal/config/config_test.go` — proxy parsing.
- `backend/.env.example` — empty `TRUSTED_PROXIES`.
- `backend/internal/ratelimit/limit.go` — buckets and client IP.
- `backend/internal/ratelimit/middleware.go` — HTTP middleware and `429` body.
- `backend/internal/ratelimit/limit_test.go` — clock and header cases.
- `backend/internal/api/middleware.go` — limiter slot in `wrap`.
- `backend/internal/api/server.go` — `New` accepts the middleware.
- `backend/internal/api/server_test.go` — identity middleware and mounted `429`.
- `backend/cmd/api/main.go` — compose the limiter.
- `backend/go.mod`, `backend/go.sum` — `golang.org/x/time/rate`.
- `infra/render.yaml` — pinned proxy list.
- `backend/AGENTS.md`, `docs/architecture/backend.md`, `DevSteps.md` — guidance after the behavior exists.

## Setup

Local `dev` contains the spec commit. Start the feature branch from the current `dev` tip. Do not reset `dev`.

```bash
git checkout -b feature/session-4-api-safety
git add docs/superpowers/plans/2026-10-14-session-4-api-safety.md
git commit -m "$(cat <<'EOF'
docs: plan session 4 API safety

Give the data-boundary work a task list before the migration and limiter land.

EOF
)"
```

Expected: the branch exists and `infra/supabase/migrations/0002_api_safety.sql` does not exist yet.

---

### Task 1: Shared PostGIS harness

**Files:**
- Create: `backend/internal/pgtest/pgtest.go`
- Create: `backend/internal/pgtest/pgtest_test.go`
- Modify: `backend/internal/database/postgres_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func EmptyURL(t *testing.T) string` and `func MigratedURL(t *testing.T) string`. `MigratedURL` creates `anon` and `authenticated`, schema `auth`, applies every migration file in lexical order, and gives those roles default table and sequence grants so a later `REVOKE` is visible. Both skip with a message that starts `Docker is unavailable:` when Docker fails and `CI` is empty.

The default grants mirror Supabase. Without them, `anon` already lacks `SELECT`, and the Task 2 denial test would pass before migration `0002` exists.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/pgtest/pgtest_test.go`:

```go
package pgtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigratedGenres(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	var n int
	if err := pool.QueryRow(ctx, `select count(*) from genres`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 9 {
		t.Fatalf("genres %d", n)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pgtest/ -count=1`

Expected: FAIL because `MigratedURL` is undefined.

- [ ] **Step 3: Write the harness and point readiness at EmptyURL**

Create `backend/internal/pgtest/pgtest.go`:

```go
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
		end
		$$;
		create schema if not exists auth;
		create table if not exists auth.users (
			id uuid primary key
		);
		create or replace function auth.uid() returns uuid as $$
			select null::uuid
		$$ language sql stable;
		grant usage on schema public to anon, authenticated;
		alter default privileges in schema public grant all on tables to anon, authenticated;
		alter default privileges in schema public grant all on sequences to anon, authenticated`)
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
```

In `backend/internal/database/postgres_test.go`, delete the `shortcut:` comment. Add import `github.com/metalhops/metalhops/backend/internal/pgtest`. Replace `url := startPostGIS(t)` with `url := pgtest.EmptyURL(t)`. Delete `startPostGIS`. Keep `execSQL` and `readMigration`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/pgtest/pgtest.go internal/pgtest/pgtest_test.go internal/database/postgres_test.go`

Run: `go test ./internal/pgtest/ ./internal/database/ -count=1`

Expected: PASS. A machine without Docker skips with `Docker is unavailable:`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/pgtest backend/internal/database/postgres_test.go
git commit -m "$(cat <<'EOF'
test: share the PostGIS harness

A second package needs the container, and Supabase-style default grants make a later revoke observable.

EOF
)"
```

---

### Task 2: Close the Data API

**Files:**
- Create: `infra/supabase/migrations/0002_api_safety.sql`
- Create: `backend/internal/database/access_test.go`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go`

**Interfaces:**
- Consumes: `pgtest.EmptyURL`, `pgtest.MigratedURL`.
- Produces: table `idempotency_records` with primary key `(user_id, operation, key)`, and `requiredMigrationVersion == "0002"`. No Go API for the table yet.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/database/access_test.go`:

```go
package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestDataAPIClosed(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `grant usage on schema public to anon, authenticated`); err != nil {
		t.Fatal(err)
	}

	statements := []string{
		`select * from genres`,
		`insert into genres (name) values ('nope')`,
		`update genres set name = 'nope' where false`,
		`delete from genres where false`,
		`select * from venues`,
		`insert into venues (name, location) values ('nope', ST_MakePoint(0, 0)::geography)`,
		`update venues set name = 'nope' where false`,
		`delete from venues where false`,
		`select * from venue_genres`,
		`insert into venue_genres (venue_id, genre_id) values ('00000000-0000-4000-8000-000000000001', 1)`,
		`update venue_genres set genre_id = 1 where false`,
		`delete from venue_genres where false`,
		`select * from idempotency_records`,
		`insert into idempotency_records (user_id, operation, key, request_hash, response_status, response_body, expires_at) values ('00000000-0000-4000-8000-000000000001', 'access_test', 'access-key', repeat('ab', 32), 200, '{}', now())`,
		`update idempotency_records set operation = 'access_test' where false`,
		`delete from idempotency_records where false`,
		`select nextval('genres_id_seq')`,
	}
	for _, role := range []string{"anon", "authenticated"} {
		for _, sql := range statements {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					_, _ = conn.Exec(ctx, "reset role")
					conn.Release()
				}()
				if _, err := conn.Exec(ctx, "set role "+role); err != nil {
					t.Fatal(err)
				}
				_, err = conn.Exec(ctx, sql)
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
					t.Fatalf("%s %s err %v", role, sql, err)
				}
			}()
		}
	}

	if _, err := pool.Exec(ctx, `insert into genres (name) values ('Access test')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venues (name, location) values ('Access', ST_MakePoint(0, 0)::geography)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values (
			'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', 'access_test', 'access-key',
			repeat('ab', 32), 200, '{"ok":true}', now() + interval '1 hour'
		)`); err != nil {
		t.Fatal(err)
	}
}
```

In `postgres_test.go`, change `readMigration` to take a filename:

```go
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
```

Update its only call site to `readMigration(t, "0001_init.sql")`. Replace the role SQL inside `TestMigrationVersions` with:

```go
	execSQL(t, pool, `
		do $$
		begin
			if not exists (select 1 from pg_roles where rolname = 'anon') then
				create role anon nologin nosuperuser nobypassrls;
			end if;
			if not exists (select 1 from pg_roles where rolname = 'authenticated') then
				create role authenticated nologin nosuperuser nobypassrls;
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
```

After the `0001` insert, delete the assertion that recorded `0001` is ready and delete the later insert of version `0002`. Put this block in their place:

```go
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
```

The `0001` insert stays where it is. Only the expectation after that insert changes from ready to `migrations_behind`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/database/ -count=1 -run 'TestDataAPIClosed|TestMigrationVersions'`

Expected: FAIL. `TestDataAPIClosed` reads genres as `anon`, or `TestMigrationVersions` still treats recorded `0001` as ready. A missing `0002` file also fails the readiness test.

- [ ] **Step 3: Add the migration and require version 0002**

Create `infra/supabase/migrations/0002_api_safety.sql`:

```sql
-- Close direct Data API access and add idempotency records.

drop policy "genres are public" on genres;
drop policy "verified venues are public" on venues;
drop policy "users submit pending venues" on venues;
drop policy "venue genres follow venue visibility" on venue_genres;

revoke all on table genres from public, anon, authenticated;
revoke all on table venues from public, anon, authenticated;
revoke all on table venue_genres from public, anon, authenticated;
revoke all on sequence genres_id_seq from public, anon, authenticated;

create table idempotency_records (
  user_id uuid not null references auth.users (id) on delete cascade,
  operation text not null check (operation ~ '^[a-z0-9_]{1,64}$'),
  key text not null check (key ~ '^[ -~]{8,128}$'),
  request_hash text not null check (request_hash ~ '^[0-9a-f]{64}$'),
  response_status smallint not null check (response_status between 200 and 299),
  response_body jsonb not null,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  primary key (user_id, operation, key)
);

create index idempotency_records_expires_at_idx on idempotency_records (expires_at);

alter table idempotency_records enable row level security;

revoke all on table idempotency_records from public, anon, authenticated;
```

The key check `[ -~]` is ASCII `0x20` through `0x7E`. Do not add `FORCE ROW LEVEL SECURITY` and do not add a policy.

In `pool.go`, set `requiredMigrationVersion = "0002"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/database/access_test.go internal/database/postgres_test.go internal/database/pool.go`

Run: `go test ./internal/database/ ./internal/pgtest/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0002_api_safety.sql backend/internal/database
git commit -m "$(cat <<'EOF'
feat: close direct Data API access

Catalogue policies and grants no longer let anon or authenticated read or write product rows.

EOF
)"
```

---

### Task 3: Idempotency record and replay

**Files:**
- Create: `backend/internal/idempotency/idempotency.go`
- Create: `backend/internal/idempotency/idempotency_test.go`

**Interfaces:**
- Consumes: `pgtest.MigratedURL` and table `idempotency_records`.
- Produces:

```go
var ErrInvalid = errors.New("idempotency: invalid request")

type Request struct {
	UserID    string
	Operation string
	Key       string
	Command   []byte
}

type Result struct {
	Status int
	Body   []byte
}

func Execute(ctx context.Context, tx pgx.Tx, now time.Time, req Request, fn func(context.Context) (Result, error)) (Result, error)
```

- [ ] **Step 1: Write the failing test**

Create `backend/internal/idempotency/idempotency_test.go`:

```go
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestExecuteReplayAndValidation(t *testing.T) {
	pool := openPool(t)
	userA := insertUser(t, pool, "11111111-1111-4111-8111-111111111111")
	userB := insertUser(t, pool, "22222222-2222-4222-8222-222222222222")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var calls int
	req := Request{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"b":1,"a":2}`)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}

	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 500, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first.Status != 201 || string(first.Body) != string(second.Body) {
		t.Fatalf("calls %d first %#v second %#v", calls, first, second)
	}

	var stored string
	var hash string
	if err := pool.QueryRow(context.Background(), `
		select response_body::text, request_hash
		from idempotency_records
		where user_id = $1 and operation = $2 and key = $3`,
		userA, req.Operation, req.Key).Scan(&stored, &hash); err != nil {
		t.Fatal(err)
	}
	if stored != string(first.Body) {
		t.Fatalf("stored %s body %s", stored, first.Body)
	}
	sum := sha256.Sum256(req.Command)
	if hash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash %s", hash)
	}

	otherUser := req
	otherUser.UserID = userB
	runOnce(t, pool, now, otherUser)
	otherOp := req
	otherOp.Operation = "review_create"
	runOnce(t, pool, now, otherOp)

	wide := req
	wide.Key = "wide-key1"
	wide.Command = make([]byte, 65536)
	for i := range wide.Command {
		wide.Command[i] = 'x'
	}
	runOnce(t, pool, now, wide)

	rolled, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := rolled.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Request{
		{UserID: "BAD", Operation: "venue_submission", Key: "same-key1", Command: []byte("x")},
		{UserID: userA, Operation: "Venue", Key: "same-key1", Command: []byte("x")},
		{UserID: userA, Operation: "venue_submission", Key: "short", Command: []byte("x")},
		{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: nil},
		{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: make([]byte, 65537)},
	} {
		_, err := Execute(context.Background(), rolled, now, bad, func(context.Context) (Result, error) {
			t.Fatal("callback")
			return Result{}, nil
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%#v err %v", bad, err)
		}
	}
}

func runOnce(t *testing.T, pool *pgxpool.Pool, now time.Time, req Request) {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := Execute(context.Background(), tx, now, req, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func openPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertUser(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	return id
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/idempotency/ -count=1`

Expected: FAIL because `Execute` is undefined.

- [ ] **Step 3: Write the minimal helper**

Create `backend/internal/idempotency/idempotency.go`:

```go
package idempotency

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrInvalid = errors.New("idempotency: invalid request")

var (
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	operationPattern = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
)

type Request struct {
	UserID    string
	Operation string
	Key       string
	Command   []byte
}

type Result struct {
	Status int
	Body   []byte
}

func Execute(ctx context.Context, tx pgx.Tx, now time.Time, req Request, fn func(context.Context) (Result, error)) (Result, error) {
	if !uuidPattern.MatchString(req.UserID) || !operationPattern.MatchString(req.Operation) || !validKey(req.Key) || len(req.Command) == 0 || len(req.Command) > 65536 {
		return Result{}, ErrInvalid
	}
	sum := sha256.Sum256(req.Command)
	hash := hex.EncodeToString(sum[:])

	var status int
	var body []byte
	var expires time.Time
	err := tx.QueryRow(ctx, `
		select response_status, response_body, expires_at
		from idempotency_records
		where user_id = $1 and operation = $2 and key = $3 and request_hash = $4`,
		req.UserID, req.Operation, req.Key, hash).Scan(&status, &body, &expires)
	if err == nil && expires.After(now) {
		return Result{Status: status, Body: body}, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}

	result, err := fn(ctx)
	if err != nil {
		return Result{}, err
	}
	if result.Status < 200 || result.Status > 299 {
		return result, nil
	}
	if !json.Valid(result.Body) {
		return Result{}, ErrInvalid
	}
	var stored []byte
	if err := tx.QueryRow(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, $2, $3, $4, $5, $6, $7)
		returning response_body`,
		req.UserID, req.Operation, req.Key, hash, result.Status, result.Body, now.Add(24*time.Hour),
	).Scan(&stored); err != nil {
		return Result{}, err
	}
	return Result{Status: result.Status, Body: stored}, nil
}

func validKey(key string) bool {
	if len(key) < 8 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] > 0x7E {
			return false
		}
	}
	return true
}
```

This lookup matches only the same hash. Task 4 replaces it with the conflict, expiry, cleanup, and lock behavior.

- [ ] **Step 4: Run the test to verify it passes**

Run: `gofmt -w internal/idempotency/idempotency.go internal/idempotency/idempotency_test.go`

Run: `go test ./internal/idempotency/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/idempotency
git commit -m "$(cat <<'EOF'
feat: replay a matching idempotency key

A creation can store its response and return those same bytes on a retry.

EOF
)"
```

---

### Task 4: Idempotency conflicts, expiry, and locks

**Files:**
- Modify: `backend/internal/idempotency/idempotency.go`
- Modify: `backend/internal/idempotency/idempotency_test.go`

**Interfaces:**
- Consumes: `Execute`, `ErrInvalid`, `Request`, `Result`, `openPool`, `insertUser` from Task 3.
- Produces: `var ErrReused = errors.New("idempotency: key reused")`. `Execute` deletes at most 100 expired rows, takes `pg_advisory_xact_lock(hashtextextended($1, 0))`, and deletes the expired row for the key under the lock.

- [ ] **Step 1: Write the failing tests**

Append these tests to `idempotency_test.go`. Add `"fmt"` and `"sync/atomic"` to the imports. `errors`, `time`, and `pgxpool` are already imported.

```go
func TestExecuteConflictExpiryAndRollback(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "33333333-3333-4333-8333-333333333333")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	req := Request{UserID: user, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}
	var calls int
	fn := func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}
	commitCall(t, pool, now, req, fn)

	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	changed := req
	changed.Command = []byte(`{"name":"b"}`)
	_, err = Execute(context.Background(), tx, now, changed, func(context.Context) (Result, error) {
		t.Fatal("callback")
		return Result{}, nil
	})
	if !errors.Is(err, ErrReused) || calls != 1 {
		t.Fatalf("reused err %v calls %d", err, calls)
	}
	_ = tx.Rollback(context.Background())

	conflict, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	status, err := Execute(context.Background(), conflict, now, req, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 409, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil || status.Status != 201 || calls != 1 {
		t.Fatalf("live replay status %#v err %v calls %d", status, err, calls)
	}
	_ = conflict.Rollback(context.Background())

	fresh := req
	fresh.Key = "status-key"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got, err := Execute(context.Background(), tx, now, fresh, func(context.Context) (Result, error) {
		calls++
		return Result{Status: 409, Body: []byte(`{"no":true}`)}, nil
	})
	if err != nil || got.Status != 409 {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where key = 'status-key'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("409 stored %d", n)
	}
	commitCall(t, pool, now, fresh, fn)

	errKey := req
	errKey.Key = "error-key1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), tx, now, errKey, func(context.Context) (Result, error) {
		return Result{}, errors.New("boom")
	})
	if err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("callback err %v", err)
	}
	_ = tx.Rollback(context.Background())
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where key = 'error-key1'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("error stored %d", n)
	}

	badJSON := req
	badJSON.Key = "bad-json1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = Execute(context.Background(), tx, now, badJSON, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte("not-json")}, nil
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("json err %v", err)
	}
	_ = tx.Rollback(context.Background())

	if _, err := pool.Exec(context.Background(), `create table probe (n int)`); err != nil {
		t.Fatal(err)
	}
	probe := req
	probe.Key = "probe-key1"
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), tx, now, probe, func(context.Context) (Result, error) {
		if _, err := tx.Exec(context.Background(), `insert into probe (n) values (1)`); err != nil {
			return Result{}, err
		}
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	commitCall(t, pool, now, probe, func(ctx context.Context) (Result, error) {
		if _, err := pool.Exec(ctx, `insert into probe (n) values (1)`); err != nil {
			return Result{}, err
		}
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	if err := pool.QueryRow(context.Background(), `select count(*) from probe`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("probe %d", n)
	}

	early := now.Add(24*time.Hour - time.Nanosecond)
	tx, err = pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), tx, early, req, func(context.Context) (Result, error) {
		t.Fatal("early callback")
		return Result{}, nil
	}); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(context.Background())
	before := calls
	commitCall(t, pool, now.Add(24*time.Hour), req, fn)
	if calls != before+1 {
		t.Fatalf("expiry calls %d before %d", calls, before)
	}
}

func TestExecuteCleanupBatch(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "44444444-4444-4444-8444-444444444444")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 101; i++ {
		key := fmt.Sprintf("expired-%03d", i)
		if _, err := pool.Exec(context.Background(), `
			insert into idempotency_records (
				user_id, operation, key, request_hash, response_status, response_body, expires_at
			) values ($1, 'cleanup_row', $2, repeat('ab', 32), 200, '{}', $3)`,
			user, key, now.Add(-2*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(context.Background(), `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, 'venue_submission', 'subject-key', repeat('cd', 32), 200, '{}', $2)`,
		user, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	commitCall(t, pool, now, Request{
		UserID: user, Operation: "venue_submission", Key: "subject-key", Command: []byte(`{"name":"new"}`),
	}, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	var expired, live int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where expires_at <= $1`, now).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `
		select count(*) from idempotency_records
		where user_id = $1 and operation = 'venue_submission' and key = 'subject-key' and expires_at > $2`,
		user, now).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if expired != 1 || live != 1 {
		t.Fatalf("expired %d live %d", expired, live)
	}
}

func TestExecuteDifferentKeysOverlap(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "55555555-5555-4555-8555-555555555555")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	run := func(key string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		_, err = Execute(ctx, tx, now, Request{UserID: user, Operation: "venue_submission", Key: key, Command: []byte(`{"n":1}`)}, func(context.Context) (Result, error) {
			calls.Add(1)
			entered <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return Result{}, ctx.Err()
			}
			return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
		})
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	errCh := make(chan error, 2)
	go func() { errCh <- run("overlap-a") }()
	go func() { errCh <- run("overlap-b") }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("callbacks did not overlap")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestExecuteConcurrentReplay(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "66666666-6666-4666-8666-666666666666")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	req := Request{UserID: user, Operation: "venue_submission", Key: "race-key1", Command: []byte(`{"name":"a"}`)}
	var calls atomic.Int32
	holding := make(chan struct{})
	errCh := make(chan error, 2)
	resCh := make(chan Result, 2)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)
		result, err := Execute(ctx, tx, now, req, func(context.Context) (Result, error) {
			calls.Add(1)
			close(holding)
			for {
				var waiting int
				if err := pool.QueryRow(ctx, `select count(*) from pg_locks where locktype = 'advisory' and not granted`).Scan(&waiting); err != nil {
					return Result{}, err
				}
				if waiting >= 1 {
					break
				}
				if ctx.Err() != nil {
					return Result{}, ctx.Err()
				}
			}
			return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
		})
		if err != nil {
			errCh <- err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			errCh <- err
			return
		}
		resCh <- result
	}()
	go func() {
		select {
		case <-holding:
		case <-time.After(5 * time.Second):
			errCh <- errors.New("first callback did not start")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		tx, err := pool.Begin(ctx)
		if err != nil {
			errCh <- err
			return
		}
		defer tx.Rollback(ctx)
		result, err := Execute(ctx, tx, now, req, func(context.Context) (Result, error) {
			calls.Add(1)
			return Result{Status: 201, Body: []byte(`{"other":true}`)}, nil
		})
		if err != nil {
			errCh <- err
			return
		}
		if err := tx.Commit(ctx); err != nil {
			errCh <- err
			return
		}
		resCh <- result
	}()
	var results []Result
	deadline := time.After(5 * time.Second)
	for len(results) < 2 {
		select {
		case err := <-errCh:
			t.Fatal(err)
		case result := <-resCh:
			results = append(results, result)
		case <-deadline:
			t.Fatal("timed out")
		}
	}
	if calls.Load() != 1 || string(results[0].Body) != string(results[1].Body) {
		t.Fatalf("calls %d results %#v %#v", calls.Load(), results[0], results[1])
	}
}

func TestExecuteUserCascade(t *testing.T) {
	pool := openPool(t)
	user := insertUser(t, pool, "77777777-7777-4777-8777-777777777777")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	commitCall(t, pool, now, Request{UserID: user, Operation: "venue_submission", Key: "cascade1", Command: []byte(`{"n":1}`)}, func(context.Context) (Result, error) {
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	})
	if _, err := pool.Exec(context.Background(), `delete from auth.users where id = $1`, user); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `select count(*) from idempotency_records where user_id = $1`, user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows %d", n)
	}
}

func commitCall(t *testing.T, pool *pgxpool.Pool, now time.Time, req Request, fn func(context.Context) (Result, error)) Result {
	t.Helper()
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	result, err := Execute(context.Background(), tx, now, req, fn)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	return result
}
```

`expired-000` is 11 characters and matches `^[ -~]{8,128}$`.

Add this test in the same file:

```go
func TestExecuteScopesByUserAndOperation(t *testing.T) {
	pool := openPool(t)
	userA := insertUser(t, pool, "88888888-8888-4888-8888-888888888888")
	userB := insertUser(t, pool, "99999999-9999-4999-8999-999999999999")
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var calls int
	fn := func(context.Context) (Result, error) {
		calls++
		return Result{Status: 201, Body: []byte(`{"ok":true}`)}, nil
	}
	base := Request{UserID: userA, Operation: "venue_submission", Key: "same-key1", Command: []byte(`{"name":"a"}`)}
	commitCall(t, pool, now, base, fn)
	otherUser := base
	otherUser.UserID = userB
	commitCall(t, pool, now, otherUser, fn)
	otherOp := base
	otherOp.Operation = "review_create"
	commitCall(t, pool, now, otherOp, fn)
	if calls != 3 {
		t.Fatalf("calls %d", calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/idempotency/ -count=1`

Expected: FAIL. `ErrReused` is undefined, or the same-key conflict stores a second logical result instead of returning the sentinel. The overlap test times out if a single lock blocks both keys.

- [ ] **Step 3: Replace Execute with the full algorithm**

Add `var ErrReused = errors.New("idempotency: key reused")` next to `ErrInvalid`.

Replace `Execute` with:

```go
func Execute(ctx context.Context, tx pgx.Tx, now time.Time, req Request, fn func(context.Context) (Result, error)) (Result, error) {
	if !uuidPattern.MatchString(req.UserID) || !operationPattern.MatchString(req.Operation) || !validKey(req.Key) || len(req.Command) == 0 || len(req.Command) > 65536 {
		return Result{}, ErrInvalid
	}
	sum := sha256.Sum256(req.Command)
	hash := hex.EncodeToString(sum[:])

	if _, err := tx.Exec(ctx, `
		delete from idempotency_records
		where ctid in (
			select ctid from idempotency_records
			where expires_at <= $1
			order by expires_at
			limit 100
			for update skip locked
		)`, now); err != nil {
		return Result{}, err
	}
	lockKey := req.UserID + "\x00" + req.Operation + "\x00" + req.Key
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Result{}, err
	}

	var storedHash string
	var status int
	var body []byte
	var expires time.Time
	err := tx.QueryRow(ctx, `
		select request_hash, response_status, response_body, expires_at
		from idempotency_records
		where user_id = $1 and operation = $2 and key = $3`,
		req.UserID, req.Operation, req.Key).Scan(&storedHash, &status, &body, &expires)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Result{}, err
	}
	if err == nil {
		if expires.After(now) {
			if storedHash == hash {
				return Result{Status: status, Body: body}, nil
			}
			return Result{}, ErrReused
		}
		if _, err := tx.Exec(ctx, `
			delete from idempotency_records
			where user_id = $1 and operation = $2 and key = $3`,
			req.UserID, req.Operation, req.Key); err != nil {
			return Result{}, err
		}
	}

	result, err := fn(ctx)
	if err != nil {
		return Result{}, err
	}
	if result.Status < 200 || result.Status > 299 {
		return result, nil
	}
	if !json.Valid(result.Body) {
		return Result{}, ErrInvalid
	}
	var stored []byte
	if err := tx.QueryRow(ctx, `
		insert into idempotency_records (
			user_id, operation, key, request_hash, response_status, response_body, expires_at
		) values ($1, $2, $3, $4, $5, $6, $7)
		returning response_body`,
		req.UserID, req.Operation, req.Key, hash, result.Status, result.Body, now.Add(24*time.Hour),
	).Scan(&stored); err != nil {
		return Result{}, err
	}
	return Result{Status: result.Status, Body: stored}, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/idempotency/idempotency.go internal/idempotency/idempotency_test.go`

Run: `go test ./internal/idempotency/ -count=1`

Expected: PASS. `TestExecuteReplayAndValidation` still passes.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/idempotency
git commit -m "$(cat <<'EOF'
feat: serialize idempotency retries in one transaction

Conflicting keys, expiry, rollback, and concurrent creates follow the stored row.

EOF
)"
```

---

### Task 5: Trusted proxy configuration

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/.env.example`

**Interfaces:**
- Consumes: `config.Config` and `load`.
- Produces: `Config.TrustedProxies []net.IPNet`. Empty input yields a nil slice. `load` returns `TRUSTED_PROXIES must be a comma-separated list of CIDRs` for an empty entry or a bad CIDR.

- [ ] **Step 1: Write the failing test**

Append to `config_test.go`:

```go
func TestTrustedProxies(t *testing.T) {
	dir := t.TempDir()
	validURL := "postgres://user:s3cret@127.0.0.1:5432/metalhops"
	loadWith := func(value string, set bool) (Config, error) {
		return load(func(key string) (string, bool) {
			if key == "DATABASE_URL" {
				return validURL, true
			}
			if key == "TRUSTED_PROXIES" && set {
				return value, true
			}
			return "", false
		}, dir)
	}

	cfg, err := loadWith("", false)
	if err != nil || len(cfg.TrustedProxies) != 0 {
		t.Fatalf("unset %#v %v", cfg.TrustedProxies, err)
	}
	cfg, err = loadWith("  10.0.0.0/8 , 10.1.0.0/16 , 10.0.0.0/8 ", true)
	if err != nil || len(cfg.TrustedProxies) != 3 {
		t.Fatalf("parsed %#v %v", cfg.TrustedProxies, err)
	}
	if cfg.TrustedProxies[0].String() != "10.0.0.0/8" || cfg.TrustedProxies[1].String() != "10.1.0.0/16" || cfg.TrustedProxies[2].String() != "10.0.0.0/8" {
		t.Fatalf("nets %#v", cfg.TrustedProxies)
	}

	pinned := "173.245.48.0/20, 103.21.244.0/22, 103.22.200.0/22, 103.31.4.0/22, 141.101.64.0/18, 108.162.192.0/18, 190.93.240.0/20, 188.114.96.0/20, 197.234.240.0/22, 198.41.128.0/17, 162.158.0.0/15, 104.16.0.0/13, 104.24.0.0/14, 172.64.0.0/13, 131.0.72.0/22, 2400:cb00::/32, 2606:4700::/32, 2803:f800::/32, 2405:b500::/32, 2405:8100::/32, 2a06:98c0::/29, 2c0f:f248::/32, 10.0.0.0/8"
	cfg, err = loadWith(pinned, true)
	if err != nil || len(cfg.TrustedProxies) != 23 {
		t.Fatalf("pinned %d %v", len(cfg.TrustedProxies), err)
	}

	for _, bad := range []string{"10.0.0.0/8,", "not-a-cidr", "10.0.0.0/8,,10.1.0.0/16"} {
		_, err := loadWith(bad, true)
		if err == nil || err.Error() != "TRUSTED_PROXIES must be a comma-separated list of CIDRs" {
			t.Fatalf("%q err %v", bad, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/config/ -count=1 -run TestTrustedProxies`

Expected: FAIL because `TrustedProxies` is not a field.

- [ ] **Step 3: Parse the setting**

Add `"net"` to the imports in `config.go`. Add `TrustedProxies []net.IPNet` to `Config`. In `load`, after `parseDocs` succeeds:

```go
	proxies, err := parseTrustedProxies(value("TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, err
	}
	return Config{Port: port, DatabaseURL: databaseURL, DocsUIEnabled: docs, TrustedProxies: proxies}, nil
```

Add:

```go
func parseTrustedProxies(value string) ([]net.IPNet, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	nets := make([]net.IPNet, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of CIDRs")
		}
		_, network, err := net.ParseCIDR(part)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of CIDRs")
		}
		nets = append(nets, *network)
	}
	return nets, nil
}
```

Append to `backend/.env.example`:

```text
# Empty uses the TCP peer. Render sets the pinned Cloudflare ranges and 10.0.0.0/8.
TRUSTED_PROXIES=
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/config/config.go internal/config/config_test.go`

Run: `go test ./internal/config/ -count=1`

Expected: PASS, including `TestLoadEnvironment` and `TestLoadRejectsEnvironment`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config backend/.env.example
git commit -m "$(cat <<'EOF'
feat: parse trusted proxy CIDRs

Local config keeps an empty list, and a bad entry stops startup with one message.

EOF
)"
```

---

### Task 6: Public rate limiter

**Files:**
- Create: `backend/internal/ratelimit/limit.go`
- Create: `backend/internal/ratelimit/middleware.go`
- Create: `backend/internal/ratelimit/limit_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

**Interfaces:**
- Consumes: nothing from earlier tasks. The tests parse their own CIDRs.
- Produces:

```go
func New(clock func() time.Time, proxies []net.IPNet) *Limiter
func Middleware(l *Limiter) func(http.Handler) http.Handler
```

`Middleware` allows `GET /healthz` and `GET /readyz` through. Every other request calls the limiter. A refusal writes the `429` envelope.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/ratelimit/limit_test.go`:

```go
package ratelimit

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestLimiterBucketsAndClientIP(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	proxies := mustNets(t, "10.0.0.0/8", "104.16.0.0/13", "2606:4700::/32")
	limiter := New(clock, proxies)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "abc")
		Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})).ServeHTTP(w, r)
	})

	for i := 0; i < 120; i++ {
		rec := hit(handler, "203.0.113.9:1", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("allow %d status %d", i, rec.Code)
		}
	}
	denied := hit(handler, "203.0.113.9:1", "198.51.100.10")
	if denied.Code != http.StatusTooManyRequests || denied.Header().Get("Retry-After") != "1" {
		t.Fatalf("denied %d retry %q", denied.Code, denied.Header().Get("Retry-After"))
	}
	if denied.Body.String() != `{"error":{"code":"rate_limited","message":"Too many requests.","request_id":"abc"}}`+"\n" {
		t.Fatalf("body %s", denied.Body.String())
	}
	if denied.Header().Get("X-Request-ID") != "abc" {
		t.Fatalf("request id %q", denied.Header().Get("X-Request-ID"))
	}
	other := hit(handler, "203.0.113.10:1", "")
	if other.Code != http.StatusNoContent {
		t.Fatalf("other %d", other.Code)
	}

	now = now.Add(500 * time.Millisecond)
	if hit(handler, "203.0.113.9:1", "").Code != http.StatusNoContent {
		t.Fatal("refill")
	}

	mapped := New(clock, nil)
	mappedHandler := Middleware(mapped)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 120; i++ {
		if hit(mappedHandler, "[::ffff:203.0.113.9]:1", "").Code != http.StatusNoContent {
			t.Fatal("mapped allow")
		}
	}
	if hit(mappedHandler, "203.0.113.9:2", "").Code != http.StatusTooManyRequests {
		t.Fatal("mapped addresses did not share a bucket")
	}

	walk := New(clock, proxies)
	walkHandler := Middleware(walk)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	pinned := "203.0.113.5, 198.51.100.10, 104.16.0.1"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.21.157.68:443", pinned).Code != http.StatusNoContent {
			t.Fatal("pinned allow")
		}
	}
	if hit(walkHandler, "10.21.157.68:443", pinned).Code != http.StatusTooManyRequests {
		t.Fatal("pinned should be exhausted")
	}
	if hit(walkHandler, "10.21.157.68:443", "198.51.100.11, 104.16.0.1").Code != http.StatusNoContent {
		t.Fatal("leftmost forged address was used as the key")
	}
	if hit(walkHandler, "203.0.113.8:1234", "198.51.100.10").Code != http.StatusNoContent {
		t.Fatal("untrusted peer used the forwarded header")
	}

	spaced := "203.0.113.5, 198.51.100.10 , 104.16.0.1"
	if hit(walkHandler, "10.21.157.68:443", spaced).Code != http.StatusTooManyRequests {
		t.Fatal("spaces changed the client")
	}
	garbage := "198.51.100.20, not-an-ip, 104.16.0.1"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.21.157.68:443", garbage).Code != http.StatusNoContent {
			t.Fatal("garbage allow")
		}
	}
	if hit(walkHandler, "10.21.157.68:443", "198.51.100.20").Code != http.StatusTooManyRequests {
		t.Fatal("unparseable hop became the key")
	}

	v6 := "2001:db8::1, 2606:4700:4700::1111"
	for i := 0; i < 120; i++ {
		if hit(walkHandler, "10.0.0.1:443", v6).Code != http.StatusNoContent {
			t.Fatal("ipv6 allow")
		}
	}
	if hit(walkHandler, "10.0.0.1:443", "2001:db8::1").Code != http.StatusTooManyRequests {
		t.Fatal("cloudflare v6 hop was used as the key")
	}
	if hit(walkHandler, "10.0.0.1:443", "2001:db8::2").Code != http.StatusNoContent {
		t.Fatal("second ipv6 client")
	}

	peer := New(func() time.Time { return now }, proxies)
	peerHandler := Middleware(peer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 120; i++ {
		if hit(peerHandler, "10.1.1.1:10", "").Code != http.StatusNoContent {
			t.Fatal("empty header allow")
		}
	}
	if hit(peerHandler, "10.1.1.1:11", "104.16.0.1").Code != http.StatusTooManyRequests {
		t.Fatal("trusted-only header did not use the peer")
	}

	health := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	health.RemoteAddr = "203.0.113.9:1"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, health)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("health %d", rec.Code)
	}
}

func TestLimiterCapacity(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	limiter := New(clock, nil)
	handler := Middleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 10000; i++ {
		if hit(handler, "203.0.113."+strconv.Itoa(i%250)+":"+strconv.Itoa(i), "").Code != http.StatusNoContent {
			t.Fatalf("fill %d", i)
		}
	}
	full := hit(handler, "198.51.100.50:1", "")
	if full.Code != http.StatusTooManyRequests || full.Header().Get("Retry-After") != "60" {
		t.Fatalf("full %d retry %q", full.Code, full.Header().Get("Retry-After"))
	}
	if hit(handler, "203.0.113.0:0", "").Code != http.StatusNoContent {
		t.Fatal("existing key was evicted while fresh")
	}

	now = now.Add(3 * time.Minute)
	if hit(handler, "198.51.100.50:1", "").Code != http.StatusNoContent {
		t.Fatal("idle keys were not evicted")
	}
}

func hit(handler http.Handler, remote, forwarded string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.RemoteAddr = remote
	if forwarded != "" {
		req.Header.Set("X-Forwarded-For", forwarded)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func mustNets(t *testing.T, cidrs ...string) []net.IPNet {
	t.Helper()
	var nets []net.IPNet
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			t.Fatal(err)
		}
		nets = append(nets, *network)
	}
	return nets
}
```

The fill addresses `203.0.113.N:port` are unique because the port is the index. `203.0.113.0:0` is the first key.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/ratelimit/ -count=1`

Expected: FAIL because package `ratelimit` does not exist, or the import of `golang.org/x/time/rate` is missing once the files exist. Add the dependency before the first compile of the implementation:

Run: `go get golang.org/x/time/rate`

- [ ] **Step 3: Implement the limiter**

Create `backend/internal/ratelimit/limit.go`:

```go
package ratelimit

import (
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type bucket struct {
	lim  *rate.Limiter
	last time.Time
}

type Limiter struct {
	mu      sync.Mutex
	clock   func() time.Time
	proxies []net.IPNet
	keys    map[string]*bucket
}

func New(clock func() time.Time, proxies []net.IPNet) *Limiter {
	copied := append([]net.IPNet(nil), proxies...)
	return &Limiter{clock: clock, proxies: copied, keys: map[string]*bucket{}}
}

func (l *Limiter) allow(remoteAddr, forwarded string) (int, bool) {
	now := l.clock()
	key := l.clientKey(remoteAddr, forwarded)
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.keys[key]
	if b == nil {
		l.evictIdle(now)
		if len(l.keys) >= 10000 {
			return 60, false
		}
		b = &bucket{lim: rate.NewLimiter(rate.Limit(2), 120), last: now}
		l.keys[key] = b
	}
	b.last = now
	reservation := b.lim.ReserveN(now, 1)
	delay := reservation.DelayFrom(now)
	if delay > 0 {
		reservation.CancelAt(now)
		secs := int((delay + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		return secs, false
	}
	return 0, true
}

func (l *Limiter) evictIdle(now time.Time) {
	for key, b := range l.keys {
		if now.Sub(b.last) >= 3*time.Minute {
			delete(l.keys, key)
		}
	}
}

func (l *Limiter) clientKey(remoteAddr, forwarded string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return remoteAddr
	}
	if !l.trusted(ip) {
		return canonicalIP(ip)
	}
	if client, ok := l.forwardedClient(forwarded); ok {
		return client
	}
	return canonicalIP(ip)
}

func (l *Limiter) forwardedClient(header string) (string, bool) {
	parts := strings.Split(header, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		entry := strings.TrimSpace(parts[i])
		ip := net.ParseIP(entry)
		if ip == nil || l.trusted(ip) {
			continue
		}
		return canonicalIP(ip), true
	}
	return "", false
}

func (l *Limiter) trusted(ip net.IP) bool {
	for _, network := range l.proxies {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func canonicalIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}
```

Updating `last` on a refusal keeps a depleted client active so idle eviction does not hand it a new burst.

Create `backend/internal/ratelimit/middleware.go`:

```go
package ratelimit

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type limitedBody struct {
	Error limitedError `json:"error"`
}

type limitedError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

func Middleware(l *Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet && (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") {
				next.ServeHTTP(w, r)
				return
			}
			retryAfter, ok := l.allow(r.RemoteAddr, r.Header.Get("X-Forwarded-For"))
			if ok {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_ = json.NewEncoder(w).Encode(limitedBody{Error: limitedError{
				Code: "rate_limited", Message: "Too many requests.", RequestID: w.Header().Get("X-Request-ID"),
			}})
		})
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/ratelimit/limit.go internal/ratelimit/middleware.go internal/ratelimit/limit_test.go`

Run: `go test ./internal/ratelimit/ -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/ratelimit backend/go.mod backend/go.sum
git commit -m "$(cat <<'EOF'
feat: limit public requests per client IP

Trusted proxies walk X-Forwarded-For from the right, and idle buckets cannot grow the map.

EOF
)"
```

---

### Task 7: Mount the limiter

**Files:**
- Modify: `backend/internal/api/middleware.go:17-19`
- Modify: `backend/internal/api/server.go:26-41`
- Modify: `backend/internal/api/server_test.go`
- Modify: `backend/cmd/api/main.go:33-35`
- Modify: `infra/render.yaml`

**Interfaces:**
- Consumes: `ratelimit.New`, `ratelimit.Middleware`, `config.Config.TrustedProxies`.
- Produces: `func New(docsEnabled bool, openAPI []byte, ready Readiness, limit func(http.Handler) http.Handler) http.Handler`. `wrap(logger, limit, next)` places the limiter after the request ID and inside panic recovery. `cmd/api` passes `ratelimit.Middleware(ratelimit.New(time.Now, cfg.TrustedProxies))`.

- [ ] **Step 1: Write the failing server test**

In `server_test.go`, add:

```go
func allowAll(next http.Handler) http.Handler { return next }
```

Add the import `github.com/metalhops/metalhops/backend/internal/ratelimit` and `"time"` if it is not already imported.

```go
func TestPublicRateLimit(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	limiter := ratelimit.New(func() time.Time { return now }, nil)
	handler := newServer(false, contract.OpenAPI, ReadinessFunc(func(context.Context) (bool, string) {
		return false, "migrations_behind"
	}), ratelimit.Middleware(limiter), slog.Default())

	for i := 0; i < 120; i++ {
		rec := get(handler, "/openapi.yaml")
		if rec.Code != http.StatusOK {
			t.Fatalf("allow %d status %d", i, rec.Code)
		}
	}
	denied := get(handler, "/openapi.yaml")
	if denied.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d", denied.Code)
	}
	if !strings.HasSuffix(denied.Body.String(), "\n") || strings.Contains(denied.Body.String(), `"fields"`) {
		t.Fatalf("body %s", denied.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(denied.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "rate_limited" || got.Error.Message != "Too many requests." {
		t.Fatalf("error %#v", got.Error)
	}
	if denied.Header().Get("X-Request-ID") != got.Error.RequestID || len(got.Error.RequestID) != 32 {
		t.Fatalf("request id %q", got.Error.RequestID)
	}
	retry, err := strconv.Atoi(denied.Header().Get("Retry-After"))
	if err != nil || retry < 1 {
		t.Fatalf("retry %q", denied.Header().Get("Retry-After"))
	}
	if get(handler, "/healthz").Code != http.StatusOK {
		t.Fatal("healthz was limited")
	}
	ready := get(handler, "/readyz")
	if ready.Code != http.StatusServiceUnavailable || ready.Body.String() != "{\"status\":\"not_ready\",\"reason\":\"migrations_behind\"}\n" {
		t.Fatalf("ready %d %s", ready.Code, ready.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	postRec := httptest.NewRecorder()
	handler.ServeHTTP(postRec, post)
	if postRec.Code != http.StatusTooManyRequests {
		t.Fatalf("post health %d", postRec.Code)
	}
	missing := get(handler, "/missing")
	if missing.Code != http.StatusTooManyRequests {
		t.Fatalf("missing %d", missing.Code)
	}
}
```

`httptest.NewRequest` uses `RemoteAddr` `192.0.2.1:1234`, so these calls share one bucket. Import `net/http/httptest` and `strconv` if missing.

Pass `allowAll` into every existing `newServer` call, in the new parameter position immediately before the logger. Pass it to `wrap` as the second argument in `TestPanicEnvelope`:

```go
handler := wrap(logger, allowAll, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
	panic("boom")
}))
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -count=1`

Expected: FAIL to compile because `newServer` and `wrap` do not take the middleware argument.

- [ ] **Step 3: Mount the middleware and set Render's proxy list**

In `middleware.go`:

```go
func wrap(logger *slog.Logger, limit func(http.Handler) http.Handler, next http.Handler) http.Handler {
	return accessLog(logger, requestIDs(recoverPanic(logger, limit(next))))
}
```

In `server.go`:

```go
func New(docsEnabled bool, openAPI []byte, ready Readiness, limit func(http.Handler) http.Handler) http.Handler {
	return newServer(docsEnabled, openAPI, ready, limit, slog.Default())
}

func newServer(docsEnabled bool, openAPI []byte, ready Readiness, limit func(http.Handler) http.Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler(ready))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})
	if docsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	return wrap(logger, limit, mux)
}
```

In `cmd/api/main.go`, import `github.com/metalhops/metalhops/backend/internal/ratelimit` and set the handler to:

```go
Handler: api.New(
	cfg.DocsUIEnabled,
	contract.OpenAPI,
	api.ReadinessFunc(pool.Check),
	ratelimit.Middleware(ratelimit.New(time.Now, cfg.TrustedProxies)),
),
```

In `infra/render.yaml`, add this env var after `DOCS_UI_ENABLED`:

```yaml
      - key: TRUSTED_PROXIES
        value: "173.245.48.0/20, 103.21.244.0/22, 103.22.200.0/22, 103.31.4.0/22, 141.101.64.0/18, 108.162.192.0/18, 190.93.240.0/20, 188.114.96.0/20, 197.234.240.0/22, 198.41.128.0/17, 162.158.0.0/15, 104.16.0.0/13, 104.24.0.0/14, 172.64.0.0/13, 131.0.72.0/22, 2400:cb00::/32, 2606:4700::/32, 2803:f800::/32, 2405:b500::/32, 2405:8100::/32, 2a06:98c0::/29, 2c0f:f248::/32, 10.0.0.0/8"
```

Do not edit `backend/openapi.yaml`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -w internal/api/middleware.go internal/api/server.go internal/api/server_test.go cmd/api/main.go`

Run: `go test ./... -count=1`

Expected: PASS. `go vet ./...` prints nothing.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api backend/cmd/api/main.go infra/render.yaml
git commit -m "$(cat <<'EOF'
feat: apply the public IP limit to the running API

Health and readiness stay open, and Render trusts only the pinned proxy ranges.

EOF
)"
```

---

### Task 8: Document the boundary

**Files:**
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the packages and routes from Tasks 2, 4, and 7.
- Produces: guidance that matches the running process. No new Go behavior.

- [ ] **Step 1: Update the guides**

In `backend/AGENTS.md`, add under Layout:

```markdown
- `internal/idempotency`: in-transaction retry helper. No HTTP route.
- `internal/ratelimit`: public per-IP token buckets and client-IP walk.
- `internal/pgtest`: PostGIS test harness. Production code does not import it.
```

Replace the readiness sentence that names migration `0001` with:

```markdown
`GET /readyz` returns 200 when the database ping succeeds and migration
`0002` or newer is recorded. Otherwise it returns 503.
```

Add this paragraph after the docs-switch paragraph:

```markdown
`TRUSTED_PROXIES` is a comma-separated CIDR list. Empty or unset means the
TCP peer is the client. `GET /healthz` and `GET /readyz` are not rate
limited. Every other route allows 120 requests per minute per resolved
client IP. A refusal is `429` `rate_limited` with `Retry-After`.
```

In `docs/architecture/backend.md`, add these two lines to the module map after `internal/database`:

```text
│   ├── idempotency/         # shared creation-retry helper
│   ├── ratelimit/           # in-memory public IP limiter
```

Leave `internal/pgtest` out of that map. Delete the transitional bullet that says migration `0001_init.sql` still permits direct catalogue reads and Venue submissions. Replace it with:

```markdown
- migration `0002_api_safety.sql` closed direct Data API reads and writes
  of the catalogue. Creation routes call `idempotency.Execute` inside their
  own transaction. The public IP limiter lives in `internal/ratelimit`.
```

In `DevSteps.md`, set the status line to `Sessions 1–4 are done. Session 5 is next. Sessions 6–14 are not started.` Set the session table row for session 4 to `Done`. Set the Session 4 heading status to `Done`.

Do not run `supabase db push`. Hosted `/readyz` stays `migrations_behind` until an operator applies `0002`.

- [ ] **Step 2: Check formatting and the suite**

Run from `backend/`:

```bash
gofmt -l .
go vet ./...
go test ./... -count=1
```

Expected: `gofmt -l .` prints nothing. Vet and tests pass.

- [ ] **Step 3: Commit**

```bash
git add backend/AGENTS.md docs/architecture/backend.md DevSteps.md
git commit -m "$(cat <<'EOF'
docs: describe the API safety boundary

Readiness, proxy trust, and the closed Data API match the running service.

EOF
)"
```

## Plan self-check

- Spec coverage: Task 1 is the harness. Task 2 is the migration, revoke, RLS, and readiness constant. Tasks 3 and 4 are `Execute`. Task 5 is `TRUSTED_PROXIES`. Task 6 is the buckets and client-IP walk. Task 7 mounts the middleware and pins Render. Task 8 updates the guides and marks Session 4 done only after the behavior exists.
- The five Review Focus cases each have a test in Task 4 or Task 6.
- `Execute`, `ErrInvalid`, `ErrReused`, `New`, `Middleware`, and `Config.TrustedProxies` keep one signature from the task that produces them through the task that consumes them.
