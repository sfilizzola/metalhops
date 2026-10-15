# Backend (Go API) agent guide

REST API for MetalHops, hosted on Render and backed by Supabase (Postgres +
PostGIS). Run commands from `backend/`.

## Commands

    cp .env.example .env   # first time only
    make run               # http://localhost:8080/healthz
    make test              # go test ./...
    make vet               # go vet ./...
    make build             # bin/api

## Layout

- `cmd/api/`: composition root only (configuration, adapters, and server
  lifecycle).
- `internal/`: all domain code.
- `internal/auth`: Supabase JWT verification, JWKS cache, and live-session lookup.
- `internal/profile`: active Profile rules, persistence, and account deletion.
- `internal/idempotency`: in-transaction retry helper. No HTTP route.
- `internal/ratelimit`: public per-IP token buckets, client-IP walk, and the per-User write limiter.
- `internal/pgtest`: PostGIS test harness. Production code does not import it.
- `openapi.yaml`: contract for implemented HTTP routes.

Before adding backend packages or seams, read
[`docs/architecture/backend.md`](../docs/architecture/backend.md). Capability
modules are the primary shape. Handler, service, and repository roles do not
require separate packages, and a Go interface needs real variation rather than
layering ceremony.

## Runtime

Go 1.24. `make run` from `backend/` reads `.env` in the current directory
when that file exists. Variables already in the environment win.

`GET /healthz` reports process health and does not check the database.
`GET /readyz` returns 200 when the database ping succeeds and migration
`0003` or newer is recorded. Otherwise it returns 503.
`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

`TRUSTED_PROXIES` is a comma-separated CIDR list. Empty or unset means the
TCP peer is the client. `GET /healthz` and `GET /readyz` are not rate
limited. Every other route uses a token bucket with burst capacity 120 and
a sustained rate of 2 requests per second per resolved client IP. A refusal
is `429` `rate_limited` with `Retry-After`.

HTTP behavior lives in `internal/api`. The contract embed remains
`backend/contract.go`. `docs.html` lives beside the HTTP package.

## Planning a session

Every backend session plan follows the
[architecture guide](../docs/architecture/backend.md) and
[DevSteps](../DevSteps.md). The guide sets the module shape, and DevSteps sets
the scope. Each plan must state:

- which capability module owns each new behavior, and where its transactions
  are owned;
- any new seam or Go interface, plus the second adapter or real variation
  that justifies it (otherwise leave it out);
- the test surface: capability, HTTP, and persistence integration tests;
- the migrations, `openapi.yaml`, and `AGENTS.md` updates the session needs.

If a plan has to break a rule in the guide, update the guide in the same pull
request, or record an ADR under `docs/adr/` when the reason is load-bearing.
Never drift from it silently.

## Definition of done

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass; CI runs the same checks.
- Every new endpoint ships with a test.
- The change passes the review checks in the architecture guide.

## Dependencies

- Ask before adding any dependency to `go.mod`. The Go standard library needs
  no approval.
- Libraries suggested in the root README are candidates to propose, not
  defaults: `chi`, `pgx` + `sqlc`, `goose`, OpenAPI tooling,
  `testcontainers-go`.

## Shared paths backend work may change

- `infra/supabase/migrations/`: add a new numbered file (`0002_<name>.sql`,
  and so on). Never edit a migration that is already on `dev` or `main`, or
  whose version is already recorded on the hosted project. A failed push does
  not count. The file freezes only when
  `supabase_migrations.schema_migrations` contains that version. A normal
  failed push leaves neither the objects nor the version row, so the file may
  be corrected and pushed again. If objects from that migration exist without
  the version row, stop and report that state. Do not edit the file to paper
  over it. Add a new file instead. `0002` becomes immutable when the Session 4
  operator push records it, even if that branch is not merged yet. Every new
  table enables row-level security and revokes
  `PUBLIC`, `anon`, `authenticated`, and `service_role` in the same migration.
  A sequence created in that migration gets the same revoke after it is
  created. A table that uses `gen_random_uuid()` adds no sequence revoke. Do
  not try to clear Supabase default privileges owned by another role. A table
  with no Data API grant needs no policy.
- `infra/render.yaml`: Render blueprint for this service.
- `.github/workflows/backend.yml`: backend CI.

## Auth model

- The apps sign in with Supabase Auth and send the JWT to this API.
- `SUPABASE_URL` derives the issuer, JWKS, and Admin URL. `SUPABASE_JWT_AUDIENCE`
  is the exact accepted audience. `SUPABASE_SERVICE_ROLE_KEY` is server-only
  and is used for hard User deletion. Never put it in a client app.
- Accepted algorithms are `RS256` and `ES256` (P-256). A symmetric JWT secret
  is not used. JWKS is cached for ten minutes. An unknown `kid` forces one
  refresh. Every protected request then reads `auth.sessions` for that session
  and User.
- `GET`, `PATCH`, and `DELETE /v1/me` are the protected routes. JSON bodies
  are at most 64 KiB, valid UTF-8, one object, and reject unknown fields.
- Profile writes and account deletion reserve a per-User bucket of burst 60
  and one token per second. The reservation is committed only after the state
  change succeeds and cancelled when it does not. `GET` does not reserve.
- Deleting a User removes the Profile and idempotency rows. Venue submissions
  remain with `submitted_by` null.
