# DevSteps

MetalHops MVP API roadmap. Refer to this document as **DevSteps** across every implementation session.

**Date:** 2026-10-13
**Status:** Approved refined design, ready for per-session planning

## Goal

Deliver the complete Go API needed by the first Android MVP through a sequence
of small, independently reviewable sessions. Each session must leave the API
working, tested, and documented so a new session can continue without relying
on unstated context.

This roadmap ends at a developer-ready API and Android handoff. Certifying a
Google Play internal, closed, or public beta is a later cross-stack milestone.

The MVP lets a person discover verified rock and metal venues, maintain a
minimal profile, submit a venue for later moderation, review and rate venues,
and record check-ins.

In MetalHops, a **Venue** is a publicly accessible bar, pub, or club where
rock or metal is a recurring part of the normal experience. A concert hall
that only hosts occasional relevant events is outside the MVP catalogue.

## Product boundary

### Included

- Supabase authentication, with JWTs verified by the Go API.
- Self-service User deletion with defined contribution retention.
- A Profile containing a unique Username and non-unique display name.
- Authenticated Profile reporting for abuse or impersonation.
- Public genre and verified-venue discovery.
- Text, genre, radius, city, country, and venue-type filters.
- Authenticated venue submissions that start in `pending` status.
- Authenticated Venue reporting for incorrect or outdated listings.
- One editable Review per user and Venue, with a required rating and optional
  commentary.
- Authenticated Review reporting for harmful commentary.
- Repeatable check-ins protected by a server-side cooldown.
- A browsable OpenAPI contract with an interactive web UI.

### Deferred

- Venue and review photos.
- Owner claims and owner-managed listings. In the future, a Verified Venue
  Owner may temporarily hide Review commentary for Platform Moderator review,
  but cannot hide or change its rating or make a final moderation decision.
- Rankings, leaderboards, badges, and achievements.
- Social graphs and activity feeds.
- Advanced venue attributes such as vibe, drinks, food, darts, pool, live
  music, and outdoor seating.
- Moderation decision endpoints and the moderation UI. Venue- and
  Review-report intake are included, but operators process both queues through
  the temporary SQL runbook.
- OpenStreetMap catalogue import, which remains a separate post-API project.
- Generated Kotlin or Swift clients; each mobile design cycle chooses its
  client strategy from the published OpenAPI contract.
- Custom backups and restore drills; the MVP relies on the backup capability
  available in its Supabase plan and revisits this before public beta.
- Beta rollout, production launch certification, and store submission.

Moderation is a separate basic web application under a future `admin/` stack.
This roadmap creates only `admin/README.md` and `admin/AGENTS.md` so the
boundary is visible. Its framework, API needs, and implementation require a
separate design cycle. The future admin frontend will use privileged endpoints
in this Go API, not direct Supabase access or a separate backend. Until that
project exists, operator-only SQL runbooks cover moderation without adding
temporary HTTP endpoints; the admin project retires those runbooks as each
workflow moves behind the API. The version-controlled bridge lives at
`docs/operations/moderation.md`.

## Architecture

Android signs users in directly with Supabase Auth. It sends the resulting
bearer token to the Go API, and all product data flows through that API.
Android does not query Supabase tables directly.
This boundary is recorded in
[ADR 0001](docs/adr/0001-product-data-through-go-api.md).

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
Backend source, CI, and container builds use Go 1.23 consistently.

The approved dependencies are:

- `pgx/v5` for PostgreSQL access and connection pooling.
- `golang-jwt/jwt/v5` for JWT parsing and claim validation.
- `testcontainers-go` for integration tests against a real PostGIS database.
- `golang.org/x/time/rate` for in-process token buckets.

The interactive documentation does not add a Go dependency. The API serves a
small HTML page that loads an exact, pinned Scalar release from a CDN.

## HTTP conventions

Product routes use the `/v1` prefix. Operational and documentation routes stay
unversioned:

- `GET /healthz` reports process health.
- `GET /readyz` reports whether required dependencies are ready.
- `GET /openapi.yaml` returns the OpenAPI 3.1 contract.
- `GET /docs` returns the Scalar API explorer.

JSON fields use `snake_case`. UUIDs use canonical hyphenated strings.
Timestamps use RFC 3339 with UTC output; inputs with explicit offsets are
accepted and normalized to UTC.
User-entered text must be valid UTF-8, is normalized to Unicode NFC, and
rejects null bytes and non-whitespace control characters. Multiline fields may
contain line feeds; single-line names and addresses may not.

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

Paginated collection endpoints accept `limit` and `cursor`. The default limit
is 20 and the maximum is 100. Cursors are opaque, URL-safe values containing
the endpoint's stable sort key and resource ID. Clients must not construct or
interpret them. The small, controlled Genre catalogue is returned in full and
is not paginated.

Authenticated POST routes that create resources require an `Idempotency-Key`
header containing 8–128 printable ASCII characters. The key is scoped to the
User and operation for 24 hours. Reusing it with the same validated command
replays the original status and response; reusing it with different input
returns `409 idempotency_key_reused`.

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

`fields` is omitted when an error is not field-specific. `details` is omitted
unless a documented domain error returns safe structured context such as a
duplicate public Venue or conflicting Check-in. Database errors, token
contents, and stack details are logged with the request ID but never returned.

Status semantics are consistent:

- `400` for malformed input or invalid filters.
- `401` for a missing, invalid, or expired token on a protected route.
- `403` when the caller is authenticated and the visible operation is
  forbidden for their role.
- `404` when a resource does not exist or is private to another User; errors
  do not disclose private resource existence.
- `409` for a check-in cooldown or another domain conflict.
- `413` when a request body exceeds 64 KiB.
- `415` when a JSON endpoint receives an unsupported content type.
- `429` for a rate or daily quota limit, with `Retry-After`.
- `500` for an unexpected internal failure.
- `502` when the upstream authentication provider fails.

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
- A non-empty `session_id` whose Supabase `auth.sessions` row still exists.

JWKS values are cached and refreshed when an unknown key ID is encountered.
Tests use a local JWKS server and generated keys; they never contact Supabase.
Every protected request performs the indexed live-session lookup, so logout
and hard User deletion revoke API access immediately rather than waiting for
JWT expiry. The API performs authorship checks before updates or deletes.

Configuration uses the Supabase project URL, expected issuer, and audience to
derive or validate the JWKS endpoint. Session 5 replaces the legacy
`SUPABASE_JWT_SECRET` example with these asymmetric-verification settings; no
JWT signing secret is required by the API. The Supabase service-role key is
configured only for self-service identity deletion and never exposed or
logged.

Every community write except `PATCH /v1/me` requires the caller to have a
Profile. A caller without one receives `409` with code `profile_required`.
Reading public data and creating the Profile remain available immediately
after authentication.
Community writes from a `rename_required` Profile return
`403 profile_rename_required`; writes from a suspended Profile return
`403 profile_suspended` with the expiry when one exists. `DELETE /v1/me`
remains available in every Profile state.

The database connection is server-owned and may bypass RLS, so service and
repository queries must enforce API visibility independently. RLS remains
enabled on every table as defense in depth. Supabase's Data API does not
provide anonymous product reads or direct product writes; authenticated Users
may read only their own private rows. The Go API is the sole public product
contract.

## Data model

Existing `genres`, `venues`, and `venue_genres` tables remain the foundation.
Later migrations may add indexes or constraints but never edit migration
`0001_init.sql`.

### Venue fields

A Venue requires a trimmed name (1–150 characters), valid latitude and
longitude, a trimmed street address (1–500 characters), a city (1–120
characters), a valid ISO 3166-1 alpha-2 country code, one of the existing
Venue types, and at least one valid Genre. Description is optional, trimmed,
and limited to 2,000 characters. Genre IDs are deduplicated before writing.

A new migration strengthens the nullable initial columns and country-code
constraint without editing `0001_init.sql`. It also adds `updated_at` and
`status_changed_at`; every content change updates the former, and every
moderation status transition updates both.

When a Supabase User is deleted, their Profile, Reviews, and Check-ins cascade
delete. Their Venue Submissions remain, but `submitted_by` becomes null. The
strengthening migration replaces the initial submitter foreign key with
`on delete set null`. The self-service deletion endpoint invokes Supabase
Auth's server-side admin API after validating explicit confirmation and a
live, recently created Supabase session.
Moderation audit rows remain but set deleted reporter, subject, reviewer, and
operator User references to null.

### Venue submission lifecycle

A new Venue Submission starts as `pending`. Moderation can move it to
`verified` or `rejected`. A Verified Venue can later move to `delisted` and
back to `verified`; a Delisted Venue is retained but hidden from public
discovery. Moderation transitions are performed through the temporary runbook
until the admin project exists.
A rejected submission carries:

- `rejection_reason`, constrained to `duplicate`, `not_eligible`,
  `inaccurate_details`, `insufficient_information`, `cannot_verify`, or
  `other`.
- An optional `rejection_note` limited to 1,000 characters. The note is
  required when the reason is `other`.

A Delisted Venue carries `delisting_reason` (`permanently_closed`,
`duplicate`, `no_longer_eligible`, or `other`) and an optional
`delisting_note`; `other` requires a note.

Delisting retains Reviews and Check-ins. Reviews disappear from public APIs
and reappear if the Venue returns to `verified`; private Check-in history
continues to show a minimal Venue summary marked `delisted`. No new Review,
Check-in, or Venue Report can target a Delisted Venue.

When two Verified Venues are found to be duplicates, the operator delists the
duplicate but does not move its community history. Venue merging belongs to
the future admin project.

Rejection fields are non-null only for rejected submissions, and delisting
fields are non-null only for Delisted Venues. When the Submitter corrects a
pending or rejected submission, the update atomically sets its status to
`pending` and clears rejection feedback. A Submitter cannot change a verified
or Delisted Venue.

`venue_moderation_actions` is an append-only audit log containing the Venue
Submission, prior and new status, rejection reason and note when applicable,
delisting reason and note when applicable, operator User ID, and timestamp.
It never stores a copy of the full Venue Submission.

`venue_reports` stores the reporter User, Venue, reason, optional note, status
(`pending`, `actioned`, or `dismissed`), creation time, and resolution
metadata. Reasons are `permanently_closed`, `moved`, `duplicate`,
`incorrect_details`, `no_longer_eligible`, or `other`; `incorrect_details`
and `other` require a note. A partial unique constraint permits one pending
report per User and Venue. Reports are private and never alter Venue
visibility automatically. The MVP acknowledges submission but exposes no
report-history or resolution-status endpoint.

Before the admin project is available, trusted operators perform the
`pending` to `verified` or `rejected` transition through a version-controlled
SQL runbook. The runbook uses a transaction, enforces the same rejection
invariants, appends the moderation action, and requires the operator and one
submission to be identified explicitly. It also lists pending Venue Reports,
supports correcting a Verified Venue or moving it to/from `delisted`, and
atomically resolves each handled report. A moderation action marks all pending
Reports for that Venue as `actioned`; reports without an action are dismissed
individually.

Creation and correction reject a strong Duplicate Venue Submission before
writing. A strong match has the same normalized name and lies within 100
metres of a verified Venue or pending Venue Submission. Name normalization
uses Unicode case and accent folding, trims and collapses whitespace, and
removes punctuation; it does not remove articles or words such as `bar`,
`pub`, or `club`. Rejected submissions do not block a new submission.

Duplicate detection and writing occur in one transaction. A
transaction-scoped advisory lock derived from the normalized name prevents
concurrent matching submissions from both passing the check. A conflict
returns `409` with code `duplicate_venue`. It includes the matched resource ID
and status only when the match is a Verified Venue or belongs to the same
Submitter; another User's pending resource remains private.

### Profiles

`profiles` has one row per Supabase user:

- `id uuid primary key references auth.users(id) on delete cascade`
- `username text not null unique`
- `display_name text not null`
- `status text not null` constrained to `active`, `rename_required`, or
  `suspended`
- Nullable moderation reason, note, and suspension expiry
- `created_at timestamptz not null`
- `updated_at timestamptz not null`

A profile is created lazily on the first authenticated profile update.
`username` is required, stored lowercase, 3–30 characters long, and contains
only ASCII letters, digits, and underscore. It is editable when the new value
is available; a uniqueness conflict returns `409 username_taken`.
`display_name` is required, non-unique, trimmed, and limited to 80 characters.
Creating a Profile is a prerequisite for Venue Submissions, Reviews, and
Check-ins.

Self-service registration rejects reserved platform and role names with
`400 username_reserved`. The initial protected skeletons are `admin`,
`administrator`, `mod`, `moderator`, `metalhops`, `support`, `staff`, and
`official`. Reservation comparison removes underscores and maps common ASCII
digit lookalikes (`0→o`, `1→i` or `l`, `3→e`, `4→a`, `5→s`, `7→t`).
Usernames whose skeleton contains `metalhops` are also reserved. Platform
identities, if needed later, are provisioned outside self-service flows.

Only an active Profile can perform community writes. A `rename_required`
Profile may use `PATCH /v1/me` but cannot contribute until it changes its
Username or display name, which returns it to active. A suspended Profile can
read public data but cannot modify its Profile or contribute. Suspension may
have an explicit expiry or be indefinite; an expired suspension is treated as
active and normalized on the next authenticated request.

Public attribution for a `rename_required` or suspended Profile uses the
generic display name `MetalHops User` and no Username. Existing ratings remain
in aggregates; content-specific removal uses the Review moderation workflow.

`profile_reports` stores reporter and target Users, a reason, optional note,
status, creation time, and resolution metadata. Reasons are `abusive_name`,
`hate_or_discrimination`, `impersonation`, `personal_information`, `spam`, or
`other`; `impersonation`, `personal_information`, and `other` require a note.
A partial unique constraint permits one pending report per reporter and target.
Users cannot report themselves.

`profile_moderation_actions` is an append-only log of rename requirements,
suspensions, renames, restorations, actor type (`operator`, `user`, or
`system`), nullable actor User ID, reason/note, and timestamp. A User's
compliant rename and automatic suspension expiry append restoration actions.
The operator runbook lists pending Profile Reports and resolves them
atomically with any moderation action. Report submission does not alter the
target Profile, and the MVP exposes no report-history endpoint.
A moderation action marks all pending Reports for that Profile as `actioned`;
reports without an action are dismissed individually.

### Idempotency records

`idempotency_records` stores the User, operation, key, hash of the normalized
validated command, response status and JSON, creation time, and expiration
time. A unique constraint covers `(user_id, operation, key)`, and User deletion
cascades records.

Creation and idempotency recording occur in the same transaction. A
transaction-scoped advisory lock serializes concurrent requests for the same
User, operation, and key. Expired records are removed opportunistically by
creation requests in indexed batches of at most 100 rows.
Only successful `2xx` creation responses are recorded; validation, conflict,
rate-limit, and server errors may be retried with the same key.

### Reviews

`reviews` stores:

- UUID primary key.
- Venue foreign key with cascade delete and User foreign key with cascade
  delete.
- Integer rating constrained to 1 through 5.
- Optional trimmed commentary limited to 2,000 characters.
- Nullable `commentary_removed_at`; when set, commentary is null and the
  Review is permanently rating-only in this MVP.
- Created and updated timestamps.
- A unique constraint on `(venue_id, user_id)`.

A user can upsert or delete only their own review. Public review responses show
the author's Username and display name, never an email address or raw auth
metadata. Responses include creation and update timestamps and indicate
whether commentary was removed. After removal, the reviewer may still update
the rating but cannot add replacement commentary; an attempt returns
`409 commentary_locked`.

`review_moderation_actions` is an append-only audit log containing the target
Review ID, reviewer User ID, Venue ID, action (`commentary_removed` or
`review_deleted`), reason, optional note, operator User ID, and timestamp.
Reasons are `spam`, `harassment`, `hate_or_discrimination`,
`personal_information`, `illegal_content`, `off_topic`, or `other`; `other`
requires a note. Removed commentary is never copied into the audit log.

`review_commentary_restrictions` has one row per restricted User–Venue pair.
Both moderation actions upsert this row, so deleting and recreating a Review
cannot bypass the commentary lock. A restricted User may create or update a
rating-only Review for that Venue. User or Venue deletion cascades the
restriction. Clearing restrictions belongs to a future appeal workflow.

`review_reports` stores the reporter User, target Review and Venue, reason,
optional note, status (`pending`, `actioned`, or `dismissed`), creation time,
and resolution metadata. Report reasons use the same safety taxonomy as
moderation actions, and `other` requires a note. A partial unique constraint
permits one pending report per User and Review; a resolved report does not
block a later report about changed commentary. Reports never copy Review
commentary, do not change public visibility, and are private to the reporter
and Platform Moderators.
The MVP exposes no report-history or resolution-status endpoint.

Until the admin project exists, the operator SQL runbook supports two atomic
actions: clear and lock commentary while preserving the rating, or record the
action and delete the entire Review. Both actions require a reason and
operator User ID and create the User–Venue commentary restriction. The
runbook also lists pending Review Reports and atomically marks each handled
report as `actioned` or `dismissed`. A moderation action marks all pending
Reports for that Review as `actioned`; reports without an action are dismissed
individually.

### Check-ins

`check_ins` stores:

- UUID primary key.
- Venue and User foreign keys with cascade delete.
- `visited_at`, supplied by the client or defaulted to the current server
  time.
- `recorded_at`, always set by the server.

Check-ins do not store device coordinates. The API rejects another check-in by
the same user at the same venue when the two `visited_at` values are less than
six hours apart. `visited_at` cannot be more than 24 hours before the request's
server time or beyond the allowed clock-skew window. The six-hour cooldown is
configurable as a Go duration through `CHECKIN_COOLDOWN`, without changing API
behavior or schema. A cooldown conflict identifies the conflicting Check-in.

To tolerate device clock skew, a supplied `visited_at` up to five minutes
ahead is accepted and stored as the current server time. A value further in
the future is rejected.

The cooldown check and insert are atomic. The repository transaction acquires
a transaction-scoped advisory lock derived from the user and venue IDs,
checks for any existing `visited_at` within the cooldown interval before or
after the requested visit time, and inserts only when none exists. Concurrent
requests therefore cannot create duplicate Check-ins.

### Aggregates

Venue list and detail responses include:

- Average rating rounded half up to one decimal place, or `null` when no
  Reviews exist.
- Review count.
- Check-in count.

These values are computed in SQL for the MVP. Persisted counters or
materialized views are deferred until measurements justify them.

### RLS policy intent

Every new table enables RLS in the migration that creates it:

- Anonymous and authenticated Data API roles have no direct access to genres
  or the public Venue catalogue.
- Authenticated Users may read their own Profile, Profile Reports, Venue
  Submissions, Venue Reports, Reviews, Review Reports, and Check-ins through
  RLS as defense in depth.
- Data API roles cannot insert, update, or delete product rows; all product
  writes go through the Go API.
- Public Profile fields, Reviews, and aggregate counts are exposed only by the
  Go API in their documented response shapes.

The API does not rely on these policies for authorization because its database
connection is server-owned. A new migration drops the initial public genre,
verified-Venue, Venue-genre, and direct-submission policies without editing
`0001_init.sql`.

## Endpoint contract

### Public endpoints

`GET /v1/genres`

- Returns all genres ordered by name.

`GET /v1/venues`

- Returns verified venues only.
- Supports `query`, repeated `genre_id`, `city`, `country_code`, `venue_type`,
  `latitude`, `longitude`, `radius_km`, `limit`, and `cursor`.
- Multiple `genre_id` values use OR semantics: a Venue matches when it has any
  selected Genre.
- Different filter groups combine with AND. City matches exactly after
  case/accent normalization; country code and Venue type match exactly.
- Latitude, longitude, and radius must be supplied together.
- `radius_km` accepts values from 1 through 100 inclusive.
- When coordinates are supplied, each result includes rounded integer
  `distance_meters`; clients format distance in local units.
- Text search is case- and accent-insensitive across Venue name, address, and
  city. It does not perform typo-tolerant matching.
- Radius search uses PostGIS geography distance.
- Radius-only results sort by distance ascending and UUID ascending.
- Text results sort by deterministic relevance: exact Venue name, Venue-name
  prefix, Venue-name substring, exact city, then other city/address matches.
  Without radius, ties sort by normalized Venue name and UUID ascending. With
  radius, ties sort by distance and UUID ascending.
- Results without radius or text search sort by normalized Venue name and UUID
  ascending.

`GET /v1/venues/{venueID}`

- Returns one verified venue, its genres, and review/check-in aggregates.
- Returns `404` for pending, rejected, or Delisted Venues.

`GET /v1/venues/{venueID}/reviews`

- Returns reviews by original creation time newest first with opaque cursor
  pagination; editing does not move a Review to the top.
- Returns author Username and display name.

### Authenticated endpoints

`GET /v1/me`

- Returns the caller's profile.
- Returns `404` until the caller creates a profile.
- Returns moderation status and feedback to that User.

`PATCH /v1/me`

- Creates or updates the caller's Profile from `username` and `display_name`.
- Creation requires both fields; updates require at least one and leave
  omitted fields unchanged.
- A `rename_required` User must change at least one moderated identity field
  to return to active; a suspended User cannot update the Profile.

`DELETE /v1/me`

- Is available to every authenticated User, including Users without a Profile
  and suspended Users.
- Requires a JWT whose `session_id` identifies a live Supabase session created
  within the previous 10 minutes, plus JSON confirmation value
  `DELETE MY ACCOUNT`. A refreshed JWT for an older session is not sufficient.
- Deletes the Supabase Auth identity through the server-side admin API.
  Deletion is hard, not Supabase soft deletion.
  Database cascades delete the Profile, Reviews, Check-ins, Reports,
  restrictions, and idempotency records; Venue Submissions remain with a null
  Submitter, and moderation audit records retain no deleted User identity.
- Returns `204` only after Supabase confirms deletion. Authentication-provider
  failures return a safe `502 auth_provider_error` and do not claim success.

`POST /v1/profiles/{username}/reports`

- Requires an active Profile and `Idempotency-Key`.
- Resolves an active target Profile by its current Username.
- Accepts a Profile-report reason and optional note limited to 1,000
  characters; impersonation, personal-information, and other reports require
  a note.
- Returns `400` for self-reporting, `201` without changing the target Profile,
  and `409 profile_already_reported` for an existing pending report.
- Counts toward the combined 20 moderation Reports per User per rolling
  24-hour quota.

`POST /v1/venues`

- Requires `Idempotency-Key`.
- Accepts name, description, latitude, longitude, address, city,
  `country_code`, venue type, and a non-empty set of genre IDs.
- Name, coordinates, address, city, country code, Venue type, and Genres are
  required; description is optional.
- The API field is `country_code`, stored uppercase as a valid ISO 3166-1
  alpha-2 code; clients localize the country display name.
- Creates the venue with `pending` status and the caller as `submitted_by`.
- Creates the venue and genre links in one transaction.
- Never accepts verification status from the client.
- Returns `409 duplicate_venue` for a normalized-name match within 100 metres
  of a verified or pending Venue.
- Enforces 10 new Venue Submissions per User in a rolling 24-hour window.

`GET /v1/me/venues`

- Returns the caller's submissions in every status ordered by `updated_at`
  descending and UUID descending, with cursor pagination. Moderation activity
  moves a submission to the top.
- Includes rejection reason and note for rejected submissions.

`GET /v1/me/venues/{venueID}`

- Returns the caller's complete Venue Submission, its Genres, current status,
  and applicable rejection or delisting feedback.
- Returns `404` when the Venue Submission does not exist or belongs to another
  User.

`PUT /v1/me/venues/{venueID}`

- Allows the original submitter to correct a pending or rejected submission.
- Requires the complete editable Venue representation accepted by creation.
- Resets the submission to `pending` in the same transaction as its venue
  genre changes.
- Returns `404` for a different Submitter and `409` for a verified or Delisted
  Venue.
- Re-runs strong duplicate detection while excluding the submission itself.

`POST /v1/venues/{venueID}/reports`

- Requires a Profile and `Idempotency-Key`.
- Accepts Reports for Verified Venues only.
- Accepts a Venue-report reason and optional note limited to 1,000 characters;
  `incorrect_details` and `other` require a note.
- Returns `201` without changing Venue visibility.
- Returns `409 venue_already_reported` when the User already has a pending
  report for that Venue.
- Counts toward the combined 20 moderation Reports per User per rolling
  24-hour quota.

`GET /v1/venues/{venueID}/reviews/me`

- Returns the caller's Review for a Verified Venue without scanning the public
  Review list.
- Returns `404` when the caller has no Review or the Venue is not verified.

`PUT /v1/venues/{venueID}/reviews/me`

- Creates or replaces the caller's review.
- Requires `rating`; omitted or null `commentary` clears existing commentary.
- Reviews are allowed only for verified venues.
- Allows rating updates after commentary moderation but rejects replacement
  commentary with `409 commentary_locked`.
- Applies the same restriction if the moderated Review was deleted and later
  recreated.

`DELETE /v1/venues/{venueID}/reviews/me`

- Deletes the caller's review.

`POST /v1/venues/{venueID}/reviews/{reviewID}/reports`

- Requires a Profile and `Idempotency-Key`.
- Requires a currently visible Review on that Verified Venue.
- Accepts a safety reason and optional note limited to 1,000 characters;
  `other` requires a note.
- Returns `201` without changing Review visibility.
- Returns `409 review_already_reported` when the User already has a pending
  report for that Review.
- Counts toward the combined 20 moderation Reports per User per rolling
  24-hour quota.

`POST /v1/venues/{venueID}/check-ins`

- Requires `Idempotency-Key`.
- Records a check-in for a verified venue.
- Accepts optional `visited_at`; omission means the current server time.
- Accepts a past visit time no more than 24 hours old. Values up to five
  minutes ahead are clamped to server time; later values are rejected.
- Returns `409` when another Check-in at that Venue falls within six hours of
  the requested visit time.

`GET /v1/me/check-ins`

- Returns the caller's check-ins by visit time newest first with venue summary
  data and cursor pagination.
- Retains history for Delisted Venues and marks their summary as `delisted`.

`DELETE /v1/me/check-ins/{checkInID}`

- Deletes one of the caller's Check-ins.
- Returns `404` when the Check-in does not exist or belongs to another User,
  avoiding disclosure of another User's private history.
- A correction is made by deleting and recreating a Check-in; visit time and
  Venue are never edited in place.

## Request flow and package boundaries

Requests pass through panic recovery, request-ID, and structured logging
middleware. Protected route groups additionally run JWT authentication and
require a resolved caller.

Handlers parse path, query, and JSON input and translate domain results into
HTTP responses. Services enforce validation, visibility, authorship, cooldown,
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
than a floating `latest` tag. The page pins an integrity hash and sends a
Content Security Policy limited to the required same-origin and CDN resources.

The OpenAPI security scheme accepts Supabase bearer tokens. Developers can
authorize once in the page and exercise protected endpoints. The contract
contains realistic request and response examples, but no real credentials or
production hostnames.

The API remains functional if the CDN is unavailable; only the interactive
page fails to load. `/openapi.yaml` remains locally available. Self-hosting
Scalar is deferred unless offline development becomes a demonstrated need.

`/openapi.yaml` is public in every environment. The Scalar `/docs` route is
controlled by `DOCS_UI_ENABLED`: local development enables it, while the
production Render configuration disables it by default. When disabled,
`/docs` returns `404`.

## Testing strategy

Every endpoint ships with tests in the same session.

### Unit and handler tests

- `httptest` verifies status codes, headers, envelopes, validation, and
  authentication behavior.
- Service tests cover visibility, authorship, review constraints, transaction
  failure, 24-hour backdating limits, and cooldown boundaries around visit
  time.
- Idempotency tests cover replay, same-key/different-input conflicts,
  concurrent requests, expiry, and atomic rollback.
- Rate-limit tests cover IP/User isolation, refill, bounded-key eviction,
  trusted proxy handling, daily quotas, idempotent replay, and `Retry-After`.
- A controllable clock makes cooldown and timestamp tests deterministic.
- A local fake JWKS server covers valid, expired, malformed, wrong-audience,
  wrong-issuer, missing-session, revoked-session, and key-rotation cases.
- A fake Supabase Admin server and auth-session fixture cover recent-session
  confirmation, successful User deletion, upstream failure, and
  secret-redaction behavior.

### Database integration tests

`testcontainers-go` starts a pinned PostGIS image. The harness creates the
minimal `auth` schema required by migrations, applies every migration from an
empty database, and then runs repository tests.

Integration coverage includes:

- PostGIS radius boundaries and distance ordering.
- Every venue filter and stable pagination under tied sort values.
- Venue-plus-genres transaction rollback.
- Duplicate normalization, 100-metre boundaries, rejected-submission
  exclusion, and concurrent submission serialization.
- Profile-report privacy and uniqueness, rename-required transitions,
  suspension expiry, public identity redaction, and moderation audit.
- Venue-report uniqueness, privacy, idempotency, resolution, Verified-to-
  Delisted transitions, and append-only moderation audit records.
- Review uniqueness and aggregate updates.
- Commentary removal locks, full Review deletion, structured moderation
  reasons, and append-only audit records that never retain removed text.
- Review-report uniqueness, privacy, idempotency, and report resolution
  through the operator runbook.
- Check-in cooldown queries.
- User deletion cascades private contributions while retaining Venue
  Submissions with a null Submitter.
- RLS policies for anonymous users, row authors, and other authenticated
  users.

Integration tests must skip with an explicit message when Docker is
unavailable locally, while CI runs them with Docker and treats failures as
blocking.

## Security and operational behavior

- JSON request bodies are limited to 64 KiB.
- Unknown JSON fields are rejected.
- Unknown query parameters on documented endpoints are rejected.
- Client-controlled text is returned as data, never rendered as HTML by the
  API.
- Logs do not include bearer tokens or full request bodies.
- Database and HTTP timeouts are explicit.
- Server shutdown drains active requests before closing the database pool.
- Readiness fails when the database is unavailable; liveness does not depend
  on the database.
- Readiness also fails when the database migration version is older than the
  minimum version required by the running binary.
- Secrets stay in environment variables and `.env` remains untracked.

The API never applies migrations at startup. Pull requests validate all
migrations from an empty PostGIS database. A protected CI release job applies
committed Supabase migrations to the target environment before deploying the
API. Migrations are forward-only; incompatible destructive changes require
expand-and-contract releases rather than a down migration.

The API enforces baseline token-bucket limits of 120 requests per minute per
client IP for public traffic and 60 authenticated writes per minute per User.
Buckets live in memory because the MVP runs one API instance. Idle buckets are
evicted, and the implementation caps tracked keys so attacker-controlled IPs
cannot grow memory without bound. Proxy headers are trusted only when the
request came through the configured Render proxy path.

PostgreSQL enforces durable domain quotas of 10 new Venue Submissions per User
per rolling 24 hours and 20 combined Profile, Venue, and Review Reports per
User per rolling 24 hours.
Idempotent replays do not consume another quota. Limit responses use
`429 rate_limited` or `429 quota_exceeded` and include `Retry-After`.
Quota checks and inserts share a transaction-scoped advisory lock per User and
quota class so concurrent requests cannot exceed the limit.

The Check-in cooldown remains a separate domain rule and returns `409`.

## Single-session implementation sequence

### Session 1: Repository boundaries

Update the root project map and roadmap to identify `admin/` as a future basic
moderation web app. Add documentation-only `admin/README.md` and
`admin/AGENTS.md`. Do not choose or scaffold a web framework.

**Exit condition:** contributors and agents can distinguish Android, backend,
admin, and shared infrastructure scopes.

### Session 2: Browsable API contract

Add the initial OpenAPI 3.1 document, `/openapi.yaml`, and the Scalar `/docs`
page with its environment switch. Initially document only routes that are
implemented. Each later feature session adds its routes and schemas when the
implementation lands; the published contract never advertises unavailable
operations.

**Exit condition:** a browser can navigate the contract and use Scalar's
request builder against the running service.

### Session 3: Backend foundation

Introduce configuration, `pgx/v5`, database lifecycle, common envelopes,
middleware, graceful shutdown, and the handler/service/repository seams.
Preserve `/healthz`, add `/readyz`, and align `go.mod`, CI, container builds,
and backend guidance on Go 1.23.

**Exit condition:** the service starts with validated configuration, exposes
operational routes, and has reusable infrastructure for feature slices.

### Session 4: API safety and data boundary

Add the migration that closes direct public reads and writes through
Supabase's Data API. Add the shared idempotency-record schema and primitives
used by later authenticated creation routes. Add bounded in-memory
token-bucket primitives and public per-IP middleware with deterministic-clock
tests.

**Exit condition:** the Go API is the sole public data contract, retries are
safe, and baseline abuse limits apply before community writes are added.

### Session 5: Authentication and profiles

Add JWT/JWKS verification, protected-route middleware, the profile migration,
base RLS policies, authenticated per-User write limiting, and
`GET/PATCH/DELETE /v1/me`. This session supports active Profiles only and
implements the agreed contribution retention for User deletion.

**Exit condition:** valid Supabase identities can maintain a profile while
invalid tokens fail consistently.

### Session 6: Profile safety and moderation bridge

Add Profile Reports, rename-required and suspended states, public identity
redaction, moderation audit, expiry behavior, and the temporary operator SQL
runbook.

**Exit condition:** Users can report unsafe Profiles, operators can require a
rename or suspend community writes, and moderated identities are not exposed
publicly.

### Session 7: Venue catalogue

Implement genre listing, verified-venue listing, venue detail, aggregates,
and cursor pagination without the optional discovery filters.

**Exit condition:** Android can render a paginated venue catalogue and venue
detail screen.

### Session 8: Venue discovery

Add every approved text, classification, location, and radius filter to the
venue list endpoint. Add indexes justified by query plans and PostGIS
integration coverage. Text matching ignores case and accents but not typos;
tests pin the relevance and tie-breaking order.

**Exit condition:** Android can implement map/list discovery with all MVP
filters.

### Session 9: Venue submissions

Implement transactional pending submissions, correction of pending
submissions, strong duplicate prevention, daily quotas, and the caller's
submission history. Add any migration required to strengthen Venue constraints
and RLS read policies without editing the initial migration.

**Exit condition:** a signed-in User can submit a Venue, track its pending
status, and correct it without creating strong duplicates.

### Session 10: Venue safety and moderation bridge

Add rejection feedback, correction of rejected submissions, Venue Reports,
the Delisted Venue lifecycle, append-only moderation audit, and the temporary
operator SQL runbook. Do not add a moderation decision endpoint.

**Exit condition:** operators can verify, reject, correct, or delist Venues;
Users can report Verified Venue problems; catalogue history is retained.

### Session 11: Reviews and ratings

Add the review migration, RLS policies, public review list, authenticated
upsert/delete operations, and venue rating aggregates.

**Exit condition:** Android can display, create, edit, and delete ratings and
reviews.

### Session 12: Review safety and moderation bridge

Add Review Reports, commentary restrictions, the append-only moderation audit
schema, and the operator SQL runbook for queue processing, commentary removal,
or full Review deletion.

**Exit condition:** Users can report unsafe commentary, and trusted operators
can resolve reports without an admin UI or retaining removed text.

### Session 13: Check-ins

Add the check-in migration, RLS policies, cooldown rule, check-in creation,
personal history, User-only deletion, and venue check-in aggregates.

**Exit condition:** Android can record visits and show the user's history
without duplicate Check-in spam, and a User can remove an incorrect Check-in.

### Session 14: API completion and Android handoff

Run cross-feature integration scenarios, verify migration-from-empty and RLS,
close OpenAPI example gaps, confirm error and pagination edge cases, validate
the migration-before-deploy release workflow and Render configuration,
document Android request flows, and document the account-deletion flow needed
for Google Play disclosure and support pages.
Do not generate or commit mobile client code.

**Exit condition:** formatting, vet, unit tests, integration tests, migration
tests, and build all pass; the OpenAPI contract describes the runnable API,
and deployment configuration is validated without certifying a beta release.

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

After Session 14, the Android team can build every core MVP user journey
against the documented API without direct database access or undocumented
backend behavior. Each roadmap session is independently understandable,
testable, and reviewable, and moderation remains cleanly separated for its own
future design and implementation cycle.
