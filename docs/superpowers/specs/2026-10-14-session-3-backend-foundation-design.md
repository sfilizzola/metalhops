# Session 3: Backend foundation

**Date:** 2026-10-14
**Status:** Approved design, not yet implemented

## Goal

Give the Go API a validated configuration, a database pool, operational
routes, and the shared HTTP infrastructure later feature slices call.
Session 3 of [DevSteps](../../../DevSteps.md) moves HTTP behavior out of
`cmd/api` and aligns the module on Go 1.23.

## Constraints

- Require a parseable `DATABASE_URL`, and still listen when Postgres is down.
  `/healthz` and `/docs` stay available. `/readyz` returns 503 until the ping
  and the migration-version check succeed.
- Use the Supabase session pooler: port 5432 on the pooler host, prepared
  statements left at the `pgx` default, and a pool of at most 5 connections.
- The local Supabase project is empty. Startup does not apply SQL. `/readyz`
  stays `migrations_unavailable` until `0001` is applied with the Supabase CLI.
- Add only the readiness seam. Production passes the pool check function.
  Tests pass a fake checker.
- `openapi.yaml` stays at `backend/openapi.yaml`. `go:embed` cannot see a
  parent directory, so `backend/contract.go` keeps embedding it and `cmd/api`
  passes the bytes into `internal/api`.
- Document `/readyz` only. `/openapi.yaml`, `/docs`, and the bearer scheme
  stay out of the contract.
- This spec is committed on `dev`. Implementation uses a `feature/*` branch
  when coding starts.

## Approach

Add `internal/config`, `internal/database`, and `internal/api`. `cmd/api`
loads configuration, opens the pool, builds the server, and shuts it down.

Rejected alternatives:

- **A separate migrate command.** A Go migrator would write the same history
  table as the Supabase CLI and the later release job.
- **Leave routing in `cmd/api`.** The next feature session would start by
  moving routes this session is already assigned.
- **Apply migrations on startup.** DevSteps reserves migration application
  for the release job. The process only observes the history table.

## Packages

```text
cmd/api
  -> internal/config
  -> internal/database
  -> internal/api
```

`internal/config` turns the environment into one validated value.
`internal/database` owns the pool, ping, migration-version check, and pool
close. `internal/api` owns routing, middleware, response envelopes,
`/healthz`, `/readyz`, `/openapi.yaml`, and `/docs`.

No Profile, Venue, Review, or Check-in package is created. The database
package does not import the HTTP package. The HTTP package does not import
`pgx`.

`internal/api` defines the readiness seam:

```go
type Readiness interface {
    Check(context.Context) (ready bool, reason string)
}

type ReadinessFunc func(context.Context) (bool, string)

func (f ReadinessFunc) Check(ctx context.Context) (bool, string) {
    return f(ctx)
}
```

`cmd/api` passes `api.ReadinessFunc(pool.Check)`. Tests pass another
`ReadinessFunc`. `docs.html` moves to `internal/api` and is embedded there.
Its markup, CSP, and docs switch stay as Session 2 defined them.

`go.mod` uses Go 1.23, matching CI and `backend/Dockerfile`. This session
adds `github.com/jackc/pgx/v5` and `github.com/testcontainers/testcontainers-go`.
Both are already on the DevSteps approved list.

## Configuration

`config.Load` reads the process environment, then fills any missing variable
from a `.env` file in the current working directory. `make run` from
`backend/` therefore sees a URL placed in `backend/.env`. A missing file is
ignored. An unreadable file stops startup with the path and not the file
contents.

`.env` rules:

- Blank lines, and lines whose first non-whitespace character is `#`, are
  ignored.
- An optional `export ` prefix is stripped.
- The key is trimmed. The value is everything after the first `=` and is not
  trimmed or unquoted.
- A line with no `=` or an empty key stops startup with the line number and
  not the line contents.
- A variable already set in the environment wins.
- Values are not expanded.

| Setting | Rule |
| --- | --- |
| `PORT` | Empty means `8080`. Any other value must be an integer from 1 through 65535, with no surrounding whitespace. A rejected value returns `PORT must be an integer from 1 through 65535`. |
| `DATABASE_URL` | Required. An empty value returns `DATABASE_URL is required`. Otherwise `pgxpool.ParseConfig` must accept it. A parse failure returns `DATABASE_URL must be a valid PostgreSQL URL` and does not wrap the driver error. |
| `DOCS_UI_ENABLED` | Unchanged. Empty or `true` enables `/docs`. `false` leaves it unregistered. Any other value, including `TRUE`, `1`, and ` true`, stops startup with `DOCS_UI_ENABLED must be true, false, or empty`. |

`SUPABASE_JWT_SECRET` stays in `backend/.env.example` and `infra/render.yaml`.
This session does not read it.

Pool size and timeouts are fixed in code. They are not environment settings.

`backend/.env.example` comments become:

```text
PORT=8080
# Supabase session pooler URL (port 5432 on the pooler host).
DATABASE_URL=
# Unused until the authentication session. Session 3 does not read this.
SUPABASE_JWT_SECRET=
# Scalar UI at /docs. Unset or true enables it. Render sets false.
DOCS_UI_ENABLED=true
```

## Database

`internal/database` opens one `pgxpool` from `DATABASE_URL` with `MaxConns`
5 and `MinConns` 0. Prepared statements stay at the driver default.
`Open` returns successfully when the database host refuses connections.
Background pool errors must not exit the process. A failure to build the
pool stops startup.

`Pool.Check(context.Context) (bool, string)` pings with a 2-second timeout,
then reads the latest `version` from `supabase_migrations.schema_migrations`. The required version is the
string `0001`, the Supabase CLI version for
`infra/supabase/migrations/0001_init.sql`. The filename stays
`0001_init.sql` even if the CLI prefers timestamped names. A CLI
rejection is reported and fixed in a follow-up, and that follow-up
changes the required version in the same change. The latest stored version must be
greater than or equal to `0001` in text order. The session that adds a
migration bumps this constant in the same change.

The version query uses its own 2-second timeout. A canceled ping returns
`database_unreachable`. A canceled or failed version query returns
`migrations_unavailable`.

| Reason | Meaning |
| --- | --- |
| `ok` | Ping succeeded and version `0001` or newer is recorded. `Check` returns `true, "ok"`. |
| `database_unreachable` | The ping failed. The version query is skipped. `Check` returns `false` and this reason. |
| `migrations_unavailable` | The history table is missing, unreadable, or the query failed. `Check` returns `false` and this reason. |
| `migrations_behind` | The history table is readable and every recorded version is older than `0001`. An empty history table is this case. `Check` returns `false` and this reason. |

`Check` logs the reason and the driver error for a failed check. The log
does not include the database URL. The application does not create
`supabase_migrations` and does not apply `0001_init.sql`.

## HTTP

`/healthz`, `/openapi.yaml`, and `/docs` keep their Session 2 behavior,
including `{"status":"ok"}` for liveness.

`GET /readyz` calls `Readiness.Check` and returns:

| Outcome | Status | Body |
| --- | --- | --- |
| `ready == true` | 200 | `{"status":"ok"}` |
| any other result | 503 | `{"status":"not_ready","reason":"<reason>"}` |

`reason` is `database_unreachable`, `migrations_unavailable`, or
`migrations_behind`. `Content-Type` is `application/json`. The driver error
stays in the log.

Add `/readyz` to `backend/openapi.yaml`. The 200 body matches `/healthz`.
The 503 body requires `status` of `not_ready` and a `reason` enum of those
three strings. `additionalProperties` is false on both bodies.

Every response passes through three middleware layers, outermost first:

1. Access log. JSON on stdout through `log/slog`, written after the inner
   handlers return, with method, path, status, duration, and request ID. The
   path has no query string. The log omits headers, bodies, and the database
   URL.
2. Request ID. 16 bytes from `crypto/rand`, hex-encoded, stored for the rest
   of the request and returned in `X-Request-ID`. If `crypto/rand` fails, the
   response is the 500 envelope below with `request_id` set to `unavailable`,
   and the access log still records that response.
3. Panic recovery. It runs inside the access log so a recovered panic is
   logged with status 500.

A recovered panic is logged with its stack and request ID. The response is
status 500:

```json
{
  "error": {
    "code": "internal_error",
    "message": "The server could not complete the request.",
    "request_id": "request-id"
  }
}
```

`fields` is omitted. The stack is not in the body.

Shared helpers, tested directly and unused by a product route in this
session, set `Content-Type: application/json` and the status they are given:

- A single-resource writer emits `{"data": <value>}`.
- A collection writer emits `{"data": <items>, "meta": {"next_cursor": <cursor-or-null>}}`. A missing cursor is JSON `null`.
- An error writer emits the envelope above. It omits `fields` when the field
  map is empty.

The first route that reads a JSON body adds decoding, the 64 KiB limit, and
unknown-field rejection.

## Startup and shutdown

`cmd/api` runs this sequence:

1. Create the JSON logger on stdout.
2. Load configuration. An invalid setting logs the validation error and
   exits before listen. The log line does not contain `DATABASE_URL`.
3. Open the pool. A failure to build the pool exits the same way.
4. Build the HTTP server with `ReadinessFunc(pool.Check)`.
5. Listen, then wait for `SIGINT` or `SIGTERM`. The wait, the 15-second
   drain, and the pool close are `api.Serve`. `cmd/api` calls it and
   gains no new test.

Server timeouts are 5 seconds for headers, 10 seconds for reading a request,
10 seconds for writing a response, and 60 seconds of idle keep-alive.
Shutdown allows 15 seconds for `http.Server.Shutdown` to finish, then closes
the pool. A listen error, or a shutdown that passes that deadline, still
closes the pool and exits non-zero.

Render stays on `healthCheckPath: /healthz`. An empty database can still
pass a deploy. On the current Supabase project, `/readyz` returns 503 with
`migrations_unavailable` until the CLI applies `0001`.

## Errors

| Failure | What happens |
| --- | --- |
| Invalid configuration | Exit before listen. The message names the variable and the rule, and omits `DATABASE_URL`. |
| Pool cannot be built | Same exit, before listen. |
| Postgres refuses connections | The process stays up. `/readyz` returns 503 with `database_unreachable`. |
| Migration history missing or unreadable | `/readyz` returns 503 with `migrations_unavailable`. |
| History older than `0001`, or an empty history table | `/readyz` returns 503 with `migrations_behind`. |
| Panic in a handler | 500 error envelope. The stack is logged with the request ID and is absent from the body. |
| Listen fails, or shutdown exceeds 15 seconds | The pool is closed and the process exits non-zero. |

## Guide updates

Replace the Documentation routes section in `backend/AGENTS.md` with:

```markdown
## Runtime

Go 1.23. `make run` from `backend/` reads `.env` in the current directory
when that file exists. Variables already in the environment win.

`GET /healthz` reports process health and does not check the database.
`GET /readyz` returns 200 when the database ping succeeds and migration
`0001` or newer is recorded. Otherwise it returns 503.
`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

HTTP behavior lives in `internal/api`. The contract embed remains
`backend/contract.go`. `docs.html` lives beside the HTTP package.
```

In `docs/architecture/backend.md`, delete the transitional bullets for
`cmd/api` owning `/healthz` and for `go.mod` lagging Go 1.23. Replace the
`contract.go` bullet with this sentence under the module map:

```markdown
`openapi.yaml` stays at `backend/openapi.yaml` and is embedded by
`backend/contract.go`, because `go:embed` cannot reference a parent
directory. The composition root passes those bytes into `internal/api`.
`docs.html` is embedded from `internal/api`.
```

`infra/render.yaml` gains no variables and keeps `/healthz`. DevSteps stays
the session list. Neither this spec nor a later `{"status":"ok"}` from
`/readyz` marks Session 3 done. That status update waits for a later docs
commit.

## Tests

`internal/config` covers empty, valid, and rejected `PORT` values, including
`0`, `65536`, and ` 8080`. It covers a missing `DATABASE_URL`, an unparseable
URL, and an error string that does not contain the password. It covers every
`DOCS_UI_ENABLED` value listed above. `.env` fills a missing variable, an
existing environment variable wins, a missing file is ignored, and a
malformed line reports its number and not its contents.

`internal/api` uses `httptest` and a fake `ReadinessFunc`. The tests cover
`/healthz`, `/openapi.yaml`, `/docs` enabled and disabled, `/readyz` at 200,
and `/readyz` at 503 for each not-ready reason. Every response has an
`X-Request-ID` of 32 hex characters. A panic returns the 500 envelope with
that same ID and no stack in the body. Helper tests cover the single-resource,
collection, and error envelopes, including omission of empty `fields`. The
access log contains method, path, status, duration, and request ID, and it
does not contain a database URL or a query string.

`internal/database` covers `database_unreachable` with a PostgreSQL URL aimed
at a closed local port. The PostGIS cases use `testcontainers-go` and the
pinned image `postgis/postgis:16-3.5`. The harness creates schema `auth` and the minimal
`auth.users` table (`id uuid primary key`) that `0001` references, applies
`infra/supabase/migrations/0001_init.sql`, and creates
`supabase_migrations.schema_migrations` (`version text primary key`) itself.
The cases are an empty database (`migrations_unavailable`), a history row of
`0000` (`migrations_behind`), and version `0001` recorded after the migration
file applies (`ok`). The harness stays in the database package tests.
Implementation marks that limit with `shortcut: harness stays in database tests until a second package needs it`.

When Docker is unavailable and `CI` is empty, those PostGIS cases skip with
an explicit message. When `CI` is non-empty, a Docker failure fails the test.

Route tests move from `cmd/api` with the handlers. `cmd/api` gains no new
test.

From `backend/`:

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass.

## Out of scope

- Applying SQL, including a migrate command and any new migration file.
- Reading `SUPABASE_JWT_SECRET`.
- Rate limits, idempotency, and JWT verification.
- JSON body decoding, the 64 KiB limit, and unknown-field rejection.
- Profile, Venue, Review, and Check-in packages.
- Changing the Render health check to `/readyz`.

## Operator handoff

The API process does not apply SQL. Session 3 is still incomplete until
migration `0001` is recorded on the hosted Supabase project and a local
`GET /readyz` returns 200.

Commit `infra/supabase/config.toml` from `supabase init --workdir infra`.
Also commit `infra/supabase/.gitignore` when init creates it. Leave every
other file init creates untracked, including `seed.sql`. Do not commit
`.temp`, `.branches`, the database password, or the linked project
credentials. The implementation plan lists the commands the operator runs
for install, login, link, and `db push`. `db push` uses the CLI's linked
direct connection. If that host is unreachable, the operator reruns the
push with `--db-url` set to the session-pooler URL on port 5432. The
transaction pooler on port 6543 is not used for the push. `DATABASE_URL`
for the API remains that same session-pooler URL. This session does not run `supabase start`. Tests keep using the
temporary PostGIS database. `major_version` stays as `supabase init`
writes it unless `link` or `db push` reports a mismatch. `project_id` is
`metalhops`. Ports and enabled services stay as init writes them.

## Exit condition

The service starts with validated configuration, serves `/healthz`,
`/readyz`, `/openapi.yaml`, and `/docs`, and exposes the envelope helpers,
middleware, pool, and readiness check that later sessions call. On an empty
database the process stays up and `/readyz` returns 503 with
`migrations_unavailable`. After the operator handoff, the hosted project
returns 200.
