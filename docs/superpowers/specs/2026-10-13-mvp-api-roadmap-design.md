# MetalHops MVP API roadmap

**Date:** 2026-10-13
**Status:** Approved design, ready for implementation planning

## Goal

Deliver the complete Go API needed by the first Android MVP through a sequence
of small, independently reviewable sessions. Each session must leave the API
working, tested, and documented so a new session can continue without relying
on unstated context.

The MVP lets a person discover verified rock and metal venues, maintain a
minimal profile, submit a venue for later moderation, review and rate venues,
and record check-ins.

## Product boundary

### Included

- Supabase authentication, with JWTs verified by the Go API.
- A minimal profile containing a display name and optional avatar URL.
- Public genre and verified-venue discovery.
- Text, genre, radius, city, country, and venue-type filters.
- Authenticated venue submissions that start in `pending` status.
- One editable rating and review per user and venue.
- Repeatable check-ins protected by a server-side cooldown.
- A browsable OpenAPI contract with an interactive web UI.

### Deferred

- Venue and review photos.
- Owner claims and owner-managed listings.
- Rankings, leaderboards, badges, and achievements.
- Social graphs and activity feeds.
- Advanced venue attributes such as vibe, drinks, food, darts, pool, live
  music, and outdoor seating.
- Moderation endpoints and the moderation UI.

Moderation is a separate basic web application under a future `admin/` stack.
This roadmap creates only `admin/README.md` and `admin/AGENTS.md` so the
boundary is visible. Its framework, API needs, and implementation require a
separate design cycle.

## Architecture

Android signs users in directly with Supabase Auth. It sends the resulting
bearer token to the Go API, and all product data flows through that API.
Android does not query Supabase tables directly.

```text
Android app
    |
    | Supabase JWT + JSON over HTTPS
    v
Go REST API
    |
    | pgx
    v
Supabase PostgreSQL + PostGIS
```

The service continues to use `net/http`. Code under `cmd/api` only loads
configuration and composes the server. Capability-focused packages under
`backend/internal/` own HTTP handling, business rules, and persistence.

The approved dependencies are:

- `pgx/v5` for PostgreSQL access and connection pooling.
- `golang-jwt/jwt/v5` for JWT parsing and claim validation.
- `testcontainers-go` for integration tests against a real PostGIS database.

The interactive documentation does not add a Go dependency. The API serves a
small HTML page that loads an exact, pinned Scalar release from a CDN.

## HTTP conventions

Product routes use the `/v1` prefix. Operational and documentation routes stay
unversioned:

- `GET /healthz` reports process health.
- `GET /readyz` reports whether required dependencies are ready.
- `GET /openapi.yaml` returns the OpenAPI 3.1 contract.
- `GET /docs` returns the Scalar API explorer.

Successful single-resource responses use:

```json
{"data": {}}
```

Successful collection responses use:

```json
{
  "data": [],
  "meta": {
    "next_cursor": "opaque-or-null"
  }
}
```

Collection endpoints accept `limit` and `cursor`. The default limit is 20 and
the maximum is 100. Cursors are opaque, URL-safe values containing the
endpoint's stable sort key and resource ID. Clients must not construct or
interpret them.

Errors use one envelope:

```json
{
  "error": {
    "code": "validation_error",
    "message": "The request is invalid.",
    "request_id": "request-id",
    "fields": {
      "rating": "must be between 1 and 5"
    }
  }
}
```

`fields` is omitted when an error is not field-specific. Database errors,
token contents, and stack details are logged with the request ID but never
returned.

Status semantics are consistent:

- `400` for malformed input or invalid filters.
- `401` for a missing, invalid, or expired token on a protected route.
- `403` when the caller is authenticated but does not own the resource.
- `404` when a visible resource does not exist.
- `409` for a check-in cooldown or another domain conflict.
- `500` for an unexpected internal failure.

Create operations return `201`. Updates and reads return `200`. Successful
deletes return `204`.

## Authentication and authorization

Public read routes do not resolve caller identity and expose verified venues
only. A user accesses pending submissions through the protected
`GET /v1/me/venues` route.

Protected routes require a bearer token issued by the configured Supabase
project. Verification checks:

- The signature against the project's JWKS.
- An explicit algorithm allowlist.
- Issuer and audience.
- Expiration and not-before claims.
- A non-empty UUID subject identifying the user.

JWKS values are cached and refreshed when an unknown key ID is encountered.
Tests use a local JWKS server and generated keys; they never contact Supabase.
The API performs ownership checks before updates or deletes.

The database connection is server-owned and may bypass RLS, so service and
repository queries must enforce API visibility independently. RLS remains
enabled on every table as defense in depth against direct Supabase access.

## Data model

Existing `genres`, `venues`, and `venue_genres` tables remain the foundation.
Later migrations may add indexes or constraints but never edit migration
`0001_init.sql`.

### Profiles

`profiles` has one row per Supabase user:

- `id uuid primary key references auth.users(id) on delete cascade`
- `display_name text not null`
- `avatar_url text null`
- `created_at timestamptz not null`
- `updated_at timestamptz not null`

A profile is created lazily on the first authenticated profile update.
`display_name` is required, trimmed, and limited to 80 characters. `avatar_url`
is optional and must be an HTTPS URL no longer than 2,048 characters.

### Reviews

`reviews` stores:

- UUID primary key.
- Venue and user foreign keys.
- Integer rating constrained to 1 through 5.
- Optional trimmed body limited to 2,000 characters.
- Created and updated timestamps.
- A unique constraint on `(venue_id, user_id)`.

A user can upsert or delete only their own review. Public review responses show
the author's profile fields, never an email address or raw auth metadata.

### Check-ins

`check_ins` stores:

- UUID primary key.
- Venue and user foreign keys.
- Creation timestamp.

Check-ins do not store device coordinates. The API rejects another check-in by
the same user at the same venue during the cooldown. The default cooldown is
six hours and is configurable as a Go duration through
`CHECKIN_COOLDOWN`, without changing API behavior or schema. A cooldown
conflict includes `retry_after_seconds` in the error metadata.

The cooldown check and insert are atomic. The repository transaction acquires
a transaction-scoped advisory lock derived from the user and venue IDs,
checks the latest check-in, and inserts only when the cooldown has elapsed.
Concurrent requests therefore cannot create duplicate check-ins.

### Aggregates

Venue list and detail responses include:

- Average rating, or `null` when no reviews exist.
- Review count.
- Check-in count.

These values are computed in SQL for the MVP. Persisted counters or
materialized views are deferred until measurements justify them.

### RLS policy intent

Every new table enables RLS in the migration that creates it:

- Genres are publicly readable.
- Verified venues and their genre links are publicly readable; authenticated
  users can insert their own pending submissions and read their own
  submissions.
- Profile display names and avatar URLs are publicly readable; users can
  insert and update only their own profile.
- Reviews for verified venues are publicly readable; users can insert,
  update, and delete only their own review.
- Check-ins are readable and insertable only by their owner. Public aggregate
  counts are returned by the server-owned API connection, not by exposing
  individual check-ins.

The API does not rely on these policies for authorization because its database
connection is server-owned, but the policies prevent broader access through
Supabase's data API.

## Endpoint contract

### Public endpoints

`GET /v1/genres`

- Returns all genres ordered by name.

`GET /v1/venues`

- Returns verified venues only.
- Supports `query`, `genre_id`, `city`, `country`, `venue_type`, `latitude`,
  `longitude`, `radius_km`, `limit`, and `cursor`.
- Latitude, longitude, and radius must be supplied together.
- Text search is case-insensitive across venue name, address, and city.
- Radius search uses PostGIS geography distance.
- Radius results sort by distance ascending and UUID ascending.
- Other results sort by creation time descending and UUID descending.

`GET /v1/venues/{venueID}`

- Returns one verified venue, its genres, and review/check-in aggregates.
- Returns `404` for pending or rejected venues on an anonymous request.

`GET /v1/venues/{venueID}/reviews`

- Returns reviews newest first with opaque cursor pagination.
- Returns author display name and avatar URL.

### Authenticated endpoints

`GET /v1/me`

- Returns the caller's profile.
- Returns `404` until the caller creates a profile.

`PATCH /v1/me`

- Creates or updates the caller's profile from `display_name` and
  `avatar_url`.

`POST /v1/venues`

- Accepts name, description, latitude, longitude, address, city, country,
  venue type, and a non-empty set of genre IDs.
- Creates the venue with `pending` status and the caller as `submitted_by`.
- Creates the venue and genre links in one transaction.
- Never accepts verification status from the client.

`GET /v1/me/venues`

- Returns the caller's submissions in every status, newest first, with cursor
  pagination.

`PUT /v1/venues/{venueID}/reviews/me`

- Creates or replaces the caller's review.
- Accepts `rating` and optional `body`.
- Reviews are allowed only for verified venues.

`DELETE /v1/venues/{venueID}/reviews/me`

- Deletes the caller's review.

`POST /v1/venues/{venueID}/check-ins`

- Records a check-in for a verified venue.
- Returns `409` while the caller is inside the configured cooldown.

`GET /v1/me/check-ins`

- Returns the caller's check-ins newest first with venue summary data and
  cursor pagination.

## Request flow and package boundaries

Requests pass through panic recovery, request-ID, and structured logging
middleware. Protected route groups additionally run JWT authentication and
require a resolved caller.

Handlers parse path, query, and JSON input and translate domain results into
HTTP responses. Services enforce validation, visibility, ownership, cooldown,
and transaction boundaries. Repositories contain SQL and map database records
to domain values. These layers communicate through narrow interfaces so
handlers and services can be tested without a database.

Expected package responsibilities:

```text
backend/
├── cmd/api/                 # configuration and composition only
├── internal/
│   ├── api/                 # server, routing, middleware, response helpers
│   ├── auth/                # JWT verification and caller identity
│   ├── config/              # environment parsing and validation
│   ├── database/            # pgx pool lifecycle
│   ├── profile/             # profile handler, service, repository
│   ├── venue/               # genre, venue discovery, and submissions
│   ├── review/              # reviews and rating aggregates
│   └── checkin/             # check-in rules and persistence
└── openapi.yaml             # source-controlled API contract
```

The exact file split inside each package belongs to its implementation plan,
but no package should combine unrelated capabilities.

## Browser API navigation

Session 2 introduces browser navigation before product endpoints are built.
`/docs` serves a small HTML shell for Scalar, configured to load
`/openapi.yaml`. The Scalar JavaScript URL contains an exact version rather
than a floating `latest` tag.

The OpenAPI security scheme accepts Supabase bearer tokens. Developers can
authorize once in the page and exercise protected endpoints. The contract
contains realistic request and response examples, but no real credentials or
production hostnames.

The API remains functional if the CDN is unavailable; only the interactive
page fails to load. `/openapi.yaml` remains locally available. Self-hosting
Scalar is deferred unless offline development becomes a demonstrated need.

## Testing strategy

Every endpoint ships with tests in the same session.

### Unit and handler tests

- `httptest` verifies status codes, headers, envelopes, validation, and
  authentication behavior.
- Service tests cover visibility, ownership, review constraints, transaction
  failure, and cooldown boundaries.
- A controllable clock makes cooldown and timestamp tests deterministic.
- A local fake JWKS server covers valid, expired, malformed, wrong-audience,
  wrong-issuer, and key-rotation cases.

### Database integration tests

`testcontainers-go` starts a pinned PostGIS image. The harness creates the
minimal `auth` schema required by migrations, applies every migration from an
empty database, and then runs repository tests.

Integration coverage includes:

- PostGIS radius boundaries and distance ordering.
- Every venue filter and stable pagination under tied sort values.
- Venue-plus-genres transaction rollback.
- Review uniqueness and aggregate updates.
- Check-in cooldown queries.
- RLS policies for anonymous users, authenticated owners, and other users.

Integration tests must skip with an explicit message when Docker is
unavailable locally, while CI runs them with Docker and treats failures as
blocking.

## Security and operational behavior

- JSON request bodies have a fixed size limit.
- Unknown JSON fields are rejected.
- Client-controlled text is returned as data, never rendered as HTML by the
  API.
- Logs do not include bearer tokens or full request bodies.
- Database and HTTP timeouts are explicit.
- Server shutdown drains active requests before closing the database pool.
- Readiness fails when the database is unavailable; liveness does not depend
  on the database.
- Secrets stay in environment variables and `.env` remains untracked.

Rate limiting for general endpoints is deferred to the hosting edge until
traffic requires application-level enforcement. The check-in cooldown remains
a domain rule and is enforced in the API.

## Single-session implementation sequence

### Session 1: Repository boundaries

Update the root project map and roadmap to identify `admin/` as a future basic
moderation web app. Add documentation-only `admin/README.md` and
`admin/AGENTS.md`. Do not choose or scaffold a web framework.

**Exit condition:** contributors and agents can distinguish Android, backend,
admin, and shared infrastructure scopes.

### Session 2: Browsable API contract

Add the initial OpenAPI 3.1 document, `/openapi.yaml`, and the Scalar `/docs`
page. Document all MVP routes and schemas even though unimplemented routes
will return `404` until their sessions land.

**Exit condition:** a browser can navigate the contract and use Scalar's
request builder against the running service.

### Session 3: Backend foundation

Introduce configuration, `pgx/v5`, database lifecycle, common envelopes,
middleware, graceful shutdown, and the handler/service/repository seams.
Preserve `/healthz` and add `/readyz`.

**Exit condition:** the service starts with validated configuration, exposes
operational routes, and has reusable infrastructure for feature slices.

### Session 4: Authentication and profiles

Add JWT/JWKS verification, protected-route middleware, the profile migration,
RLS policies, and `GET/PATCH /v1/me`.

**Exit condition:** valid Supabase identities can maintain a profile while
invalid tokens fail consistently.

### Session 5: Venue catalogue

Implement genre listing, verified-venue listing, venue detail, aggregates,
and cursor pagination without the optional discovery filters.

**Exit condition:** Android can render a paginated venue catalogue and venue
detail screen.

### Session 6: Venue discovery

Add every approved text, classification, location, and radius filter to the
venue list endpoint. Add indexes justified by query plans and PostGIS
integration coverage.

**Exit condition:** Android can implement map/list discovery with all MVP
filters.

### Session 7: Venue submissions

Implement transactional pending submissions and the caller's submission
history. Add any migration required to strengthen venue constraints without
editing the initial migration.

**Exit condition:** a signed-in user can submit a venue and track its status.

### Session 8: Reviews and ratings

Add the review migration, RLS policies, public review list, authenticated
upsert/delete operations, and venue rating aggregates.

**Exit condition:** Android can display, create, edit, and delete ratings and
reviews.

### Session 9: Check-ins

Add the check-in migration, RLS policies, cooldown rule, check-in creation,
personal history, and venue check-in aggregates.

**Exit condition:** Android can record visits and show the user's history
without duplicate check-in spam.

### Session 10: MVP hardening and Android handoff

Run cross-feature integration scenarios, verify migration-from-empty and RLS,
close OpenAPI example gaps, confirm error and pagination edge cases, validate
Render configuration, and document Android request flows.

**Exit condition:** formatting, vet, unit tests, integration tests, migration
tests, and build all pass; the OpenAPI contract describes the deployed API.

## Definition of done for every session

- Work occurs on a `feature/*` branch and targets `dev` by pull request.
- New schema changes use a new numbered migration with RLS and policies.
- New endpoints have handler, service, and relevant integration coverage.
- `backend/openapi.yaml` matches implemented behavior and `/docs` renders it.
- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass from `backend/`.
- Commands and paths changed by the session are reflected in the appropriate
  `AGENTS.md`.
- No secrets, local `.env`, generated binaries, or real tokens are committed.

## Success criteria

After Session 10, the Android team can build every core MVP user journey
against the documented API without direct database access or undocumented
backend behavior. Each roadmap session is independently understandable,
testable, and reviewable, and moderation remains cleanly separated for its own
future design and implementation cycle.
