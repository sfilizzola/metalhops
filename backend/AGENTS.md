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
- `internal/auth`: Supabase JWT verification, JWKS cache, live-session lookup, and operator lookup.
- `internal/profile`: Profile, report, and moderation behavior.
- `internal/idempotency`: in-transaction retry helper. No HTTP route.
- `internal/ratelimit`: public per-IP token buckets, client-IP walk, and the per-User write limiter.
- `internal/venue`: Genre list, public catalogue, discovery filters, sort modes,
  filter-bound cursors, Venue submissions, Venue reports, and operator
  moderation (verify, reject, correct, delist, reopen, and the append-only
  audit).
- `internal/review`: Review validation, public list, the caller's own Review,
  save and delete, Review Reports, operator moderation, the Commentary
  restriction, and its lift through `lift_commentary_restriction` on the
  existing Review action route.
- `internal/checkin`: Check-in validation, the spacing rule, creation, personal
  history, the public per-Venue feed, and deletion.
- `internal/pgtest`: PostGIS test harness. Production code does not import it.
- `openapi.yaml`: contract for implemented HTTP routes.

[`docs/api/`](../docs/api/README.md) explains those routes in business
terms, grouped as public, app, and moderation. Update it in the same pull
request as a route change, alongside `openapi.yaml`.

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
`0012` or newer is recorded. Otherwise it returns 503. The 200 body adds
`commit` when `RENDER_GIT_COMMIT` is a full 40-character lowercase hex SHA.
`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

`TRUSTED_PROXIES` is a comma-separated CIDR list. Empty or unset means the
TCP peer is the client. `GET /healthz` and `GET /readyz` are not rate
limited. Every other route uses a token bucket with burst capacity 120 and
a sustained rate of 2 requests per second per resolved client IP. A refusal
is `429` `rate_limited` with `Retry-After`.

`ALLOWED_ORIGINS` is a comma-separated list of exact browser origins (scheme
and host, no path) allowed to call this API cross-origin. Empty or unset
sends no `Access-Control-Allow-Origin` header and answers every `OPTIONS`
through the normal router, which is `405` for a route that does not register
it; this never affects native app clients, which do not send `Origin`. A
listed origin gets `Access-Control-Allow-Origin` on every response and a
direct `204` preflight reply, bypassing routing, for `OPTIONS` requests that
carry `Access-Control-Request-Method`. `openapi.yaml`'s one `servers` entry
is the relative URL `/`, so Scalar's "Send" button targets whatever host
served `/docs`, local or deployed, without per-environment edits.

`CHECKIN_SPACING` is the minimum time between one User's Check-ins at one
Venue. Unset means one hour. Any other value must be a positive Go duration;
an invalid value stops the process at startup.

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
- `.github/workflows/release.yml`: the only deployer. It runs on push to
  `main`.

## Auth model

- The apps sign in with Supabase Auth and send the JWT to this API.
- `SUPABASE_URL` derives the issuer, JWKS, and Admin URL. `SUPABASE_JWT_AUDIENCE`
  is the exact accepted audience. `SUPABASE_SERVICE_ROLE_KEY` is server-only
  and is used for hard User deletion. Never put it in a client app.
- Accepted algorithms are `RS256` and `ES256` (P-256). A symmetric JWT secret
  is not used. JWKS is cached for ten minutes. An unknown `kid` forces one
  refresh. Every protected request then reads `auth.sessions` for that session
  and User.
- App routes are `GET`, `PATCH`, and `DELETE /v1/me`, `POST /v1/venues`,
  `GET /v1/me/venues`, `GET` and `PUT /v1/me/venues/{venueID}`,
  `POST /v1/profiles/{username}/reports`,
  `POST /v1/venues/{venueID}/reports`,
  `POST /v1/venues/{venueID}/reviews/{reviewID}/reports`, `GET`, `PUT`, and
  `DELETE /v1/venues/{venueID}/reviews/me`, `POST` and
  `GET /v1/venues/{venueID}/check-ins`, `GET /v1/me/check-ins`, and
  `DELETE /v1/me/check-ins/{checkInID}`. `GET /v1/venues/{venueID}/check-ins`
  requires only a Profile, not an Active one, and returns `403 profile_required`
  to a signed-in caller with none. Privileged routes are
  `GET /v1/moderation/profile-reports`,
  `GET /v1/moderation/profiles/{userID}`,
  `POST /v1/moderation/profiles/{userID}/actions`,
  `PATCH /v1/moderation/profile-reports/{reportID}`,
  `GET /v1/moderation/venue-reports`,
  `GET` and `PUT /v1/moderation/venues/{venueID}`,
  `POST /v1/moderation/venues/{venueID}/actions`,
  `PATCH /v1/moderation/venue-reports/{reportID}`,
  `GET /v1/moderation/review-reports`,
  `GET /v1/moderation/reviews/{reviewID}`,
  `POST /v1/moderation/reviews/{reviewID}/actions`, and
  `PATCH /v1/moderation/review-reports/{reportID}`. A `platform_operators` row
  marks an operator. An operator on an app route, or an app User on a
  privileged route, receives `403 forbidden`.
  JSON bodies are at most 64 KiB, valid UTF-8, one object, and reject unknown
  fields.
- `GET /v1/genres`, `GET /v1/venues`, `GET /v1/venues/{venueID}`, and
  `GET /v1/venues/{venueID}/reviews` are public: they do not read `Authorization`, and only the per-IP limit
  applies.
- Profile writes, profile and Venue reports, review reports, Venue submissions,
  changed Venue corrections (by a Submitter or an operator), changed Review
  saves and Review deletes, Check-in creation and deletion, moderation actions including review moderation
  actions, report dismissals including review-report dismissals, and account
  deletion reserve a per-User bucket of burst 60 and one token per second. The
  reservation is committed only after the state change succeeds and cancelled
  when it does not. `GET` does not reserve.
- Deleting a User removes the Profile, Check-ins, and idempotency rows. Venue submissions
  remain with `submitted_by` null.
