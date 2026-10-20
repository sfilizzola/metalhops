# DevSteps

MetalHops MVP API roadmap. Refer to this document as **DevSteps** across every implementation session.

**Date:** 2026-10-14
**Status:** Sessions 1–8 are done. Session 9 is next. Sessions 10–14 are not started.

## Goal

Deliver the complete Go API, including the user-facing contract and the
permanent privileged moderation endpoints, through a sequence of small,
independently reviewable sessions. Each session must leave the API
working, tested, and documented so a new session can continue without relying
on unstated context.

This roadmap ends as an admin-ready API. The admin app is the next project,
and the Android client starts only after that app exists. Certifying a
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
- Public genre and venue discovery. The public catalogue contains Verified
  Venues and pending Venue Submissions whose required fields are valid.
  Pending Venues are visibly not verified. Rejected and Delisted Venues stay
  hidden.
- Text, genre, radius, city, country, and venue-type filters.
- Authenticated venue submissions that start in `pending` status.
- Authenticated Venue reporting for incorrect or outdated listings.
- One editable Review per user and Venue, with a required rating and optional
  commentary.
- Authenticated Review reporting for harmful commentary.
- Privileged endpoints for Profile, Venue, and Review moderation decisions.
- Repeatable check-ins protected by a server-side cooldown.
- A browsable OpenAPI contract with an interactive web UI.

### Deferred

- Venue and review photos.
- Owner claims and owner-managed listings. In the future, a Verified Venue
  Owner may hide Review commentary until a Platform Moderator reviews it,
  but cannot hide or change its rating or make a final moderation decision.
- Rankings, leaderboards, badges, and achievements.
- Social graphs and activity feeds.
- Advanced venue attributes such as vibe, drinks, food, darts, pool, live
  music, and outdoor seating.
- The moderation UI, which belongs to the admin app.
- OpenStreetMap catalogue import, which remains a separate post-API project.
- Generated Kotlin or Swift clients; each mobile design cycle chooses its
  client strategy from the published OpenAPI contract.
- Custom backups and restore drills; the MVP relies on the backup capability
  available in its Supabase plan and revisits this before public beta.
- Beta rollout, production launch certification, and store submission.

Moderation is a separate basic web application under `admin/`. This roadmap
does not build that app or choose its framework. The app calls privileged
endpoints on this Go API, not direct Supabase access or a separate backend.
Operator actions are those privileged endpoints. A caller who is not an
operator receives the existing `403`. Each session that adds one of these
endpoints names how that rule recognizes an operator, and this roadmap does
not add an operator-provisioning model.

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
Backend source, CI, and container builds use Go 1.24 consistently.

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

Authenticated `POST` routes that create submissions, Reports, Check-ins, or
moderation/audit effects require an `Idempotency-Key` header containing 8–128
printable ASCII characters. Reads, state-setting `PUT` and `PATCH` routes,
and `DELETE` routes do not require the header because repeating them does not
create another resource or audit effect. The key is scoped to the User and
operation for 24 hours. Reusing it with the same normalized validated command
replays the original status and response; reusing it with different input
returns `409 idempotency_key_reused`. The write route normalizes the command
before the helper hashes it.

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

Public read routes do not resolve caller identity and expose the public
catalogue. A user accesses their own submissions, including those hidden from
the catalogue, through the protected `GET /v1/me/venues` route.

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

The database connection is server-owned and may bypass RLS, so capability
modules and their persistence implementation must enforce API visibility
independently. The Render `DATABASE_URL` user owns the product tables. Session
4 does not grant a second API role. RLS remains enabled on every table as
defense in depth.
Supabase's Data API does not provide product reads or direct product writes
for `anon`, `authenticated`, or `service_role`. The Go API is the sole public
product contract, including author-only reads.

## Data model

Existing `genres`, `venues`, and `venue_genres` tables remain the foundation.
Later migrations may add indexes or constraints but never edit migration
`0001_init.sql`.

### Venue fields

A Venue requires a trimmed name (1–150 characters), valid latitude and
longitude, a trimmed street address (1–500 characters), a city (1–120
characters), a Country code as defined in the glossary, one of the existing
Venue types, and at least one valid Genre. Description is optional, trimmed,
and limited to 2,000 characters. Genre IDs are deduplicated before writing.

Migration `0005` strengthens the nullable initial columns and country-code
constraint without editing `0001_init.sql`, and it adds `updated_at`. Every
Venue-row change updates `updated_at`; a Venue transaction that changes
Genre links explicitly touches the Venue row. The moderation lifecycle
migration adds `status_changed_at`; every moderation status transition
updates both timestamps. Country codes use official ISO 3166-1 alpha-2 codes
plus `XK`; `GB`, not `UK`, identifies the United Kingdom.

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
back to `verified`. Reopening restores that same Venue, including its past
Check-ins and Reviews, rather than creating a new Venue Submission. A Delisted
Venue is retained but hidden from public discovery. Moderation transitions are performed through privileged moderation
endpoints.
A rejected submission carries:

- `rejection_reason`, constrained to `duplicate`, `not_eligible`,
  `inaccurate_details`, `insufficient_information`, `cannot_verify`, or
  `other`.
- An optional `rejection_note` limited to 1,000 characters. The note is
  required when the reason is `other`.

A Delisted Venue carries `delisting_reason` (`permanently_closed`,
`duplicate`, `no_longer_eligible`, or `other`) and an optional
`delisting_note`; `other` requires a note.

Delisting retains Reviews and Check-ins. Reviews disappear from public APIs,
including the author's own Review read, and reappear if the Venue returns to
`verified`. There is no personal Review list. The author cannot update or
delete that Review while the Venue is Delisted; both return not found, and
the Review stays unchanged until the Venue is verified again. Private Check-in history
continues to show a minimal Venue summary marked `delisted`. That summary is
the Venue name, city, country code, and status, and it omits the delisting
reason and note. When the Venue returns to verified, that Check-in shows the
public Venue summary again. No new Review, Check-in, or Venue Report can target
a Delisted Venue. Delisting marks every
pending Review Report for that Venue actioned. Those reports stay resolved if
the Venue returns to verified, and a User may file a new report once the
commentary is public again.

Rejecting a Venue retains existing Check-ins. Private Check-in history
continues to show a minimal Venue summary marked `rejected`. That summary is
the Venue name, city, country code, and status, and it omits the rejection
reason and note. No new Check-in
can target a rejected Venue. If the Submitter corrects it back to pending, the
same Check-in remains. When that Venue returns to the public catalogue, the
Check-in shows the public Venue summary again, and its Check-in count includes
those Check-ins.

When two Verified Venues are found to be duplicates, the operator delists the
duplicate but does not move its community history. Venue merging belongs to
the future admin project.

Rejection fields are non-null only for rejected submissions, and delisting
fields are non-null only for Delisted Venues. When the Submitter corrects a
pending or rejected submission, the update atomically sets its status to
`pending`, clears rejection feedback, and returns it to the public catalogue
as visibly not verified. A rejected correction leaves the Venue rejected and
hidden. A Submitter cannot change a verified or Delisted Venue.

`venue_moderation_actions` is an append-only audit log containing the Venue
Submission, prior and new status, rejection reason and note when applicable,
delisting reason and note when applicable, operator User ID, and timestamp.
It never stores a copy of the full Venue Submission.

`venue_reports` stores the reporter User, Venue, reason, optional note, status
(`pending`, `actioned`, or `dismissed`), creation time, and resolution
metadata. Reasons are `permanently_closed`, `moved`, `duplicate`,
`incorrect_details`, `no_longer_eligible`, or `other`; `incorrect_details`
and `other` require a note. The same reasons apply to a Verified Venue and a
valid pending Venue. A partial unique constraint permits one pending
report per User and Venue. A resolved report does not block a later report by
the same User. A Submitter correction of a pending Venue leaves its pending
Venue Reports open. The Platform Moderator assesses the Venue as it is when
they open it. Rejecting a Venue leaves its reports resolved, and correcting
that Venue back into the public catalogue does not reopen them. Reports are
private and never alter Venue visibility automatically. The MVP acknowledges submission but exposes no
report-history or resolution-status endpoint.

Trusted operators perform the `pending` to `verified` or `rejected` transition
through a privileged moderation endpoint. The endpoint uses a transaction,
enforces the same rejection invariants, appends the moderation action, and
requires the operator and one submission to be identified explicitly. It also
lists pending Venue Reports in one list, whether the reported Venue is
verified or a valid pending submission, and shows that Venue's current status.
The moderator sees the full Profile of each involved User who still has one,
including the reporter and the Submitter. Public redaction does not apply. It supports correcting a Verified Venue or moving it to or from `delisted`,
and atomically resolves each handled report. A
moderation action marks all pending Reports for that Venue as `actioned`;
reports without an action are dismissed individually.

Creation and correction reject a strong Duplicate Venue Submission before
writing. A strong match has the same normalized name and lies within 100
metres of a verified Venue or pending Venue Submission. Name normalization
uses Unicode case and accent folding, trims and collapses whitespace, and
removes punctuation and symbols; it does not remove articles or words such as
`bar`, `pub`, or `club`. Rejected submissions do not block a new submission.

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

A Profile is created by the first authenticated update, and that creation
requires both a Username and a display name. A sign-in without a Profile is
not yet a User.
`username` is required, 3–30 characters long, and contains only ASCII letters,
digits, and underscore. It stores the casing the User entered. Identity and
uniqueness use case-insensitive comparison, so the same User may correct
`sfilizZola` to `sfilizzola`, while another User cannot take either casing. It
is editable when the new value is available; a uniqueness conflict returns
`409 username_taken`.
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
read public data but cannot modify its Profile or contribute. Both can read
their own Check-ins, Venue Submissions, including rejection feedback, and
their own Review while its Venue is verified. Creating, correcting, or
deleting a Check-in, Venue Submission, or Review stays blocked until the
Profile can contribute again. Suspension may
have an explicit expiry or be indefinite; an expired suspension is treated as
active and normalized on the next authenticated request.

Public attribution for a `rename_required` or suspended Profile uses the
generic display name `MetalHops User` and no Username. That public Review list
is the same for every viewer, including the author. The author's own Review
read shows that User's current Username and display name. Existing ratings remain
in aggregates; content-specific removal uses the Review moderation workflow.

`profile_reports` stores reporter and target Users, a reason, optional note,
status, creation time, and resolution metadata. Reasons are `abusive_name`,
`hate_or_discrimination`, `impersonation`, `personal_information`, `spam`, or
`other`; `impersonation`, `personal_information`, and `other` require a note.
A partial unique constraint permits one pending report per reporter and target.
A resolved report does not block a later report by the same User. A change to
the target User's Username or display name leaves a pending Profile Report
open. The Platform Moderator assesses the Profile as it is when they open it.
On every privileged moderation view, the moderator sees the full Profile of
each involved User who still has one: User id, Username, display name, status,
moderation reason, note, suspension expiry, and the Profile's created and
updated times. That includes reports, Venue eligibility decisions, Review
moderation, and Profile moderation. The list omits email and raw auth
metadata. Public redaction does not apply. A deleted User has no Profile to
show. These views belong to the later moderation sessions and the admin
service. Session 5 does not add them. Users cannot report themselves.

`profile_moderation_actions` is an append-only log of rename requirements,
suspensions, renames, restorations, actor type (`operator`, `user`, or
`system`), nullable actor User ID, reason/note, and timestamp. A User's
compliant rename and automatic suspension expiry append restoration actions.
A privileged moderation endpoint lists pending Profile Reports and resolves
them atomically with any moderation action. Report submission does not alter
the target Profile, and the MVP exposes no report-history endpoint.
A moderation action marks all pending Reports for that Profile as `actioned`;
reports without an action are dismissed individually.

### Idempotency records

`idempotency_records` stores the User, operation, key, and the lowercase hex
SHA-256 of the command bytes the write route passed in. That route passes the
normalized validated command. The helper does not normalize those bytes.
The record also stores the response status and JSON, creation time, and
expiration time. A unique constraint covers `(user_id, operation, key)`, and
User deletion cascades records. An operation is one idempotent `POST` action,
not one database outcome. Each `POST` route that requires a key uses one
stable operation name matching `^[a-z0-9_]{1,64}$`. A corrected Check-in uses
a new key under the same Check-in operation; reusing the original key replays
the original Check-in. Session 4 assigns no operation names.

The write and idempotency recording occur in the same transaction. A
transaction-scoped advisory lock serializes concurrent requests for the same
User, operation, and key. Expired records, including other Users' records, are
removed opportunistically by write requests in indexed batches of at most 100
rows. That deletion stays in the caller's transaction, so a rollback puts the
rows back. There is no separate cleanup transaction.
Only successful `2xx` write responses are recorded; validation, conflict,
rate-limit, and server errors may be retried with the same key. The caller
commits only when the helper returns a nil error and a 2xx result. A callback
error, a non-2xx result, an invalid request, and a reused key roll the
transaction back. A replay returns the stored 2xx result, so the caller may
commit that transaction. Each write route tests a callback that writes and
then returns an error: neither the domain write nor an idempotency row
remains.

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
metadata. A `rename_required` or suspended author is shown as `MetalHops User`
with no Username on that public response, including to that author. Public
review responses omit the User id. Responses include creation and update timestamps and indicate
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
permits one pending report per User and Review. A resolved report does not
block a later report by the same User, even when the commentary is unchanged.
A User cannot report their own Review. A Review Report requires commentary
that is currently public. A rating-only Review, including one whose commentary
a Platform Moderator already removed, cannot be reported. When the author
deletes the Review, its pending Review Reports close with it. Clearing the
commentary dismisses them. Replacing the commentary leaves a pending Review
Report open. The Platform Moderator assesses the commentary that is public
when they open it. A Review created
later is a different Review and can be reported on its own. Delisting a Venue
marks every pending Review Report for that Venue actioned. Those reports stay
resolved if the Venue returns to verified.
Reports never copy Review
commentary, do not change public visibility, and are private to the reporter
and Platform Moderators.
The MVP exposes no report-history or resolution-status endpoint.

Privileged moderation endpoints support two atomic actions: clear and lock
commentary while preserving the rating, or record the action and delete the
entire Review. Both actions require a reason and operator User ID and create
the User–Venue commentary restriction. The endpoints also list pending Review
Reports and atomically mark each handled report as `actioned` or `dismissed`.
The moderator sees the full Profile of each involved User who still has one,
including the reporter and the Review author, on the report list and on a
Review moderation action. Public redaction does not apply. A moderation action
marks all pending Reports for that Review as `actioned`;
reports without an action are dismissed individually.

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
Rejection or delisting does not reset that cooldown. A Check-in retained from
before the Venue left the public catalogue still blocks another Check-in
within six hours after the Venue returns.

To tolerate device clock skew, a supplied `visited_at` up to five minutes
ahead is accepted and stored as the current server time. A value further in
the future is rejected.

The cooldown check and insert are atomic. The Check-in module's transaction
acquires a transaction-scoped advisory lock derived from the user and venue
IDs, checks for any existing `visited_at` within the cooldown interval before
or after the requested visit time, and inserts only when none exists.
Concurrent requests therefore cannot create duplicate Check-ins.

### Aggregates

Venue list and detail responses include:

- Average rating rounded half up to one decimal place, or `null` when no
  Reviews exist. A pending Venue has no rating.
- Review count. A pending Venue has a review count of zero.
- Check-in count, including Check-ins on a pending Venue. Check-ins made
  before a Venue was rejected count again when it returns to the public
  catalogue.

These values are computed in SQL for the MVP. Persisted counters or
materialized views are deferred until measurements justify them.

### RLS policy intent

Every new table enables RLS in the migration that creates it. That migration
also revokes `PUBLIC`, `anon`, `authenticated`, and `service_role` on the new
table. A migration that creates a sequence revokes those same roles on that
sequence after creating it. A table that uses `gen_random_uuid()` adds no sequence
revoke. It does not try to clear Supabase default privileges owned by another
role. A table with no Data API grant needs no policy. The Go API serves
author-only reads.

- Anonymous, authenticated, and service-role Data API roles have no direct
  access to genres, the public Venue catalogue, or later product tables.
- Authenticated Users read their own Profile, Profile Reports, Venue
  Submissions, Venue Reports, Reviews, Review Reports, and Check-ins through
  the Go API.
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

- Returns Verified Venues and pending Venue Submissions that have the required
  creation fields. A submission missing one of those fields is never stored.
  A pending result is visibly not verified. Rejected and Delisted
  Venues are excluded. Public results do not include the Submitter or a
  caller-specific submission flag.
- Supports `query`, repeated `genre_id`, `city`, `country_code`, `venue_type`,
  `latitude`, `longitude`, `radius_km`, `limit`, and `cursor`.
- Multiple `genre_id` values use OR semantics: a Venue matches when it has any
  selected Genre.
- Different filter groups combine with AND. City matches by exact Discovery
  text. Country code and Venue type match exactly.
- Latitude, longitude, and radius must be supplied together.
- `radius_km` accepts values from 1 through 100 inclusive.
- When coordinates are supplied, each result includes rounded integer
  `distance_meters`; clients format distance in local units.
- A query matches Discovery text of the Venue name, address, or city. It does
  not tolerate typos.
- Radius search uses PostGIS geography distance.
- Radius-only results sort by distance ascending and UUID ascending.
- Text results sort by deterministic relevance: exact Venue name, Venue-name
  prefix, Venue-name substring, exact city, then other city/address matches.
  Without a location, ties sort by Normalized name and UUID ascending. With
  a location, ties sort by distance and UUID ascending.
- Results without a location or text search sort by Normalized name and UUID
  ascending.

`GET /v1/venues/{venueID}`

- Returns one Verified Venue or one pending Venue Submission whose required
  fields are valid, plus its genres and review/check-in aggregates. A pending
  Venue is visibly not verified. The response does not include the Submitter
  or a caller-specific submission flag. The app hides the report control when
  the Venue id is in the signed-in User's own submission list.
- Returns `404` for a rejected or Delisted Venue, and for a pending Venue
  whose required fields are not valid.

`GET /v1/venues/{venueID}/reviews`

- Returns reviews by original creation time newest first with opaque cursor
  pagination; editing does not move a Review to the top.
- Returns author Username and display name. A `rename_required` or suspended
  author is `MetalHops User` with no Username, for every viewer, including
  that author. The response omits the User id.

### Authenticated endpoints

`GET /v1/me`

- Returns the caller's profile.
- Returns `404` until the caller creates a profile.
- Returns moderation status and feedback to that User. Session 5 returns the
  stored status, including a non-active status already in the row, and omits
  moderation reason, note, and suspension expiry.

`PATCH /v1/me`

- Creates or updates the caller's Profile from `username` and `display_name`.
- Does not require `Idempotency-Key`; repeating it sets the same Profile
  fields and cannot create a second Profile.
- Creation requires both fields; updates require at least one and leave
  omitted fields unchanged.
- A `rename_required` User must change at least one moderated identity field
  to return to active; a suspended User cannot update the Profile.
- Changing the Username or display name leaves a pending Profile Report open.
  The Platform Moderator assesses the Profile as it is when they open it.

`DELETE /v1/me`

- Is available to every authenticated caller, including an abandoned sign-in
  with no Profile and a suspended User. A sign-in without a Profile is not yet
  a User.
- Does not require `Idempotency-Key`. Successful deletion removes the User's
  idempotency rows and invalidates subsequent authenticated retries.
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
  required; description is optional. A submission missing any required field
  is rejected and no Venue is stored.
- The API field is `country_code`, stored as the canonical uppercase Country
  code; clients localize the country display name. `XK` is accepted for
  Kosovo, `GB` identifies the United Kingdom, and `UK` is rejected.
- Creates the venue with `pending` status and the caller as `submitted_by`.
- Creates the venue and genre links in one transaction.
- Never accepts verification status from the client.
- Returns `409 duplicate_venue` for a normalized-name match within 100 metres
  of a verified or pending Venue.
- Enforces 10 new Venue Submissions per User in a rolling 24-hour window.
  The slot is taken when the row is created, using `created_at`. A correction
  must not change `created_at`. Rejection, correction, verification, or
  delisting does not return the slot.

`GET /v1/me/venues`

- Returns the caller's submissions in every status ordered by `updated_at`
  descending and UUID descending, with cursor pagination. Moderation activity
  moves a submission to the top. A `rename_required` or suspended caller can
  read this list, including rejection feedback. Creating or correcting a
  submission stays blocked until that Profile can contribute again.
- Includes rejection reason and note for rejected submissions.

`GET /v1/me/venues/{venueID}`

- Returns the caller's complete Venue Submission, its Genres, current status,
  and applicable rejection or delisting feedback. A `rename_required` or
  suspended caller can read it.
- Returns `404` when the Venue Submission does not exist or belongs to another
  User.

`PUT /v1/me/venues/{venueID}`

- Allows the original submitter to correct a pending or rejected submission.
- Does not require `Idempotency-Key`; repeating the complete replacement
  cannot create another Venue Submission.
- Requires the complete editable Venue representation accepted by creation.
  A correction missing a required field is rejected and the stored Venue
  remains unchanged. Description may be cleared.
- Resets the submission to `pending` in the same transaction as its venue
  genre changes.
- Returns `404` for a different Submitter and `409` for a verified or Delisted
  Venue.
- Re-runs strong duplicate detection while excluding the submission itself.
- A correction of a pending Venue leaves pending Venue Reports open. The
  Platform Moderator assesses the Venue as it is when they open it.

`POST /v1/venues/{venueID}/reports`

- Requires a Profile and `Idempotency-Key`.
- Accepts Reports for any Venue in the public catalogue, including a valid
  pending Venue. The app shows the report control only to a User who did not
  submit that Venue. If a report is submitted anyway, the API returns `400`. A missing
  token returns `401`, and an authenticated caller without a Profile returns
  `409 profile_required`. A public Venue with no Submitter can be reported by
  any User. Rejected and Delisted Venues cannot be reported.
- Accepts a Venue-report reason and optional note limited to 1,000 characters;
  `incorrect_details` and `other` require a note.
- Returns `201` without changing Venue visibility.
- Returns `409 venue_already_reported` when the User already has a pending
  report for that Venue.
- Counts toward the combined 20 moderation Reports per User per rolling
  24-hour quota.

`GET /v1/venues/{venueID}/reviews/me`

- Returns the caller's Review for a Verified Venue without scanning the public
  Review list. A `rename_required` or suspended caller can read it. Creating,
  updating, or deleting that Review stays blocked until the Profile can
  contribute again. That read shows the caller's current Username and display
  name, including when public attribution uses `MetalHops User`. It includes
  the caller's User id.
- Returns `404` when the caller has no Review or the Venue is not verified,
  including while it is Delisted. The Review reappears when the Venue is
  verified again. There is no personal Review list.

`PUT /v1/venues/{venueID}/reviews/me`

- Creates or replaces the caller's review.
- Does not require `Idempotency-Key`; the route sets the caller's single
  Review for that Venue and repeating it cannot create another Review.
- Requires `rating`; omitted or null `commentary` clears existing commentary.
  Clearing the commentary dismisses pending Review Reports for that Review. If
  the author adds commentary again later, a User may file a new report.
  Replacing the commentary leaves a pending Review Report open.
- Reviews are allowed only for verified venues. A Delisted Venue returns
  `404`, and an existing Review stays unchanged.
- Allows rating updates after commentary moderation but rejects replacement
  commentary with `409 commentary_locked`.
- Applies the same restriction if the moderated Review was deleted and later
  recreated.

`DELETE /v1/venues/{venueID}/reviews/me`

- Deletes the caller's review. It does not require an `Idempotency-Key`;
  repeating the delete does not create a second Review. A Delisted Venue
  returns `404`, and the Review stays unchanged. Pending Review Reports
  for that Review end with it. A Review created later is a different Review
  and can be reported on its own.

`POST /v1/venues/{venueID}/reviews/{reviewID}/reports`

- Requires a Profile and `Idempotency-Key`.
- Requires another User's Review with currently public commentary on that
  Verified Venue. The app shows the report control only on that Review. A
  report of the caller's own Review, or of a rating-only Review, returns
  `400`.
- Accepts a safety reason and optional note limited to 1,000 characters;
  `other` requires a note.
- Returns `201` without changing Review visibility.
- Returns `409 review_already_reported` when the User already has a pending
  report for that Review.
- Counts toward the combined 20 moderation Reports per User per rolling
  24-hour quota.

`POST /v1/venues/{venueID}/check-ins`

- Requires `Idempotency-Key`. A corrected visit uses a new key under this
  same operation. Reusing the original key replays the original Check-in and
  does not record the corrected visit.
- Records a Check-in for a Verified Venue or a pending Venue Submission whose
  required fields are valid.
- Accepts optional `visited_at`; omission means the current server time.
- Accepts a past visit time no more than 24 hours old. Values up to five
  minutes ahead are clamped to server time; later values are rejected.
- Returns `409` when another Check-in at that Venue falls within six hours of
  the requested visit time. A Check-in retained through rejection or delisting
  still counts.

`GET /v1/me/check-ins`

- Returns the caller's check-ins by visit time newest first with cursor
  pagination. A `rename_required` or suspended caller can read this history.
  Creating or deleting a Check-in stays blocked until that Profile can
  contribute again.
- A pending or verified Venue uses the public Check-in summary: name, city,
  country code, Venue type, and status. Status is `pending` or `verified`, and
  a pending Venue is visibly not verified. Rating, review count, and Check-in
  count stay on the Venue detail.
- A rejected or Delisted Venue uses a summary of name, city, country code, and
  status `rejected` or `delisted`. It omits the moderator's reason and note
  and does not open a Venue detail page. When the Venue is pending or verified
  again, the Check-in shows the public summary and can open that Venue. The
  visit remains a memory if the Venue is rejected, closes, or later returns to
  the public catalogue.

`DELETE /v1/me/check-ins/{checkInID}`

- Deletes one of the caller's Check-ins. It does not require an
  `Idempotency-Key`; repeating the delete does not create a second Check-in.
  The caller can delete their own Check-in while its Venue is rejected or
  Delisted. That removes the memory and its six-hour cooldown block. It does
  not change the Venue, and a new Check-in still cannot target that Venue
  while it is rejected or Delisted.
- Returns `404` when the Check-in does not exist or belongs to another User,
  avoiding disclosure of another User's private history.
- A correction is made by deleting and recreating a Check-in; visit time and
  Venue are never edited in place. The new Check-in uses a new
  `Idempotency-Key`.

## Request flow and package boundaries

Requests pass through panic recovery, request-ID, and structured logging
middleware. Protected route groups additionally run JWT authentication and
require a resolved caller.

HTTP adapters parse path, query, and JSON input and translate domain results
into HTTP responses. Capability modules enforce validation, visibility,
authorship, cooldown, and transaction boundaries. Their private persistence
implementation contains SQL and maps database records to domain values.

These are implementation roles, not mandatory package layers. A seam is added
only when behavior actually varies, and its interface stays with the caller.
The rules for module depth, dependencies, transaction ownership, adapters, and
testing are in
[the backend architecture guide](docs/architecture/backend.md).

Expected package responsibilities:

```text
backend/
├── cmd/api/                 # configuration and composition only
├── internal/
│   ├── api/                 # server, routing, middleware, response helpers
│   ├── auth/                # JWT verification and caller identity
│   ├── config/              # environment parsing and validation
│   ├── database/            # pgx pool lifecycle
│   ├── profile/             # Profile capability
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

The contract documents only implemented routes. Session 2 has no protected
routes, so it declares no bearer security scheme. The authentication session
adds that scheme when the first protected route exists. Examples contain no
real credentials or production hostnames.

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
empty database, and then runs persistence integration tests.

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
  through the privileged moderation API.
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
expand-and-contract releases rather than a down migration. A migration file
is immutable once its version is recorded on the hosted project. A failed
push does not freeze it. The file freezes only when
`supabase_migrations.schema_migrations` contains that version. A normal
failed push leaves neither the objects nor the version row, so the file may
be corrected and pushed again. If `0002` objects exist without that version
row, stop and report that state. Do not edit the file to paper over it.
`0002` freezes at the Session 4 operator push, even if the branch is not
merged yet. A later correction is a new migration. The Session 4
handoff starts the Session 4 binary before the operator push. `/readyz`
must return `migrations_behind`, then that same process must return 200
after the push. If the first response is already 200, the highest recorded
version decides. `0001` means the Session 3 binary is still running. `0002`
finishes the gate only for the process started from this session after the
old listener on port 8080 was stopped. The history row does not identify
the binary.
The handoff does not deploy Render. The release job still applies `0002` before it deploys the
binary that requires `0002`. Render's `healthCheckPath` stays `/healthz`.
Session 4 does not point it at `/readyz` and does not add the release job.
When Session 14 adds that job, a release is successful only after the
committed migrations are applied and `/readyz` returns 200. Render's
`/healthz` success is not that proof.

The API enforces a public token bucket with burst capacity 120 and a sustained
rate of 2 requests per second per client IP. Authenticated writes use a
separate token bucket with burst capacity 60 and a sustained rate of 1 write
per second per User. A replay consumes one public token, because that bucket
runs before the handler, and it does not consume a User write token. Session 4
implements only the public bucket. A public-limit refusal does not call
`Execute` and does not change a stored idempotency row.
Buckets live in memory because the MVP runs one API instance. Idle buckets are
evicted, and the implementation caps tracked keys so attacker-controlled IPs
cannot grow memory without bound. Proxy headers are trusted only when the
request came through the configured Render proxy path.

PostgreSQL enforces durable domain quotas of 10 new Venue Submissions per User
per rolling 24 hours and 20 combined Profile, Venue, and Review Reports per
User per rolling 24 hours. A Venue Submission takes its slot when the row is
created. Rejection, correction, verification, and delisting do not return that
slot or take another. The quota clock is the row's creation timestamp. For a
Venue that is `venues.created_at`, and a correction must not change it. A
Report takes its slot when the row is created. Resolving it, dismissing it,
or removing it because its Review or a User was deleted does not return that
slot or take another. The combined Report window counts Profile, Venue, and
Review Reports together, so `Retry-After` uses the oldest submission time still
inside the window, including a Report whose row was later removed. A request that creates no row does not
take a slot.
An idempotent replay does not consume a quota. The quota increment runs inside
the idempotency callback, which a replay skips. A User write token is likewise
consumed only when that callback runs, or when a write route that does not use
the helper actually changes state. A `429 quota_exceeded` response sets
`Retry-After` to the whole seconds until the oldest counted row in that
rolling 24-hour window falls outside it, and at least 1. Session 4 does not
implement quotas. `429 rate_limited` keeps the `Retry-After` value from the
token bucket.
Quota checks and inserts share a transaction-scoped advisory lock per User and
quota class so concurrent requests cannot exceed the limit.

The Check-in cooldown remains a separate domain rule and returns `409`.

## Single-session implementation sequence

| Session | Status |
| --- | --- |
| 1. Repository boundaries | Done |
| 2. Browsable API contract | Done |
| 3. Backend foundation | Done |
| 4. API safety and data boundary | Done |
| 5. Authentication and profiles | Done |
| 6. Profile safety and moderation API | Done |
| 7. Venue catalogue | Done |
| 8. Venue discovery | Done |
| 9–14 | Not started |

### Session 1: Repository boundaries

**Status:** Done.

Update the root project map and roadmap to identify `admin/` as a future basic
moderation web app. Add documentation-only `admin/README.md` and
`admin/AGENTS.md`. Do not choose or scaffold a web framework.

**Exit condition:** contributors and agents can distinguish Android, backend,
admin, and shared infrastructure scopes.

### Session 2: Browsable API contract

**Status:** Done.

Add the initial OpenAPI 3.1 document, `/openapi.yaml`, and the Scalar `/docs`
page with its environment switch. Initially document only routes that are
implemented. Each later feature session adds its routes and schemas when the
implementation lands; the published contract never advertises unavailable
operations.

**Exit condition:** a browser can navigate the contract and use Scalar's
request builder against the running service.

### Session 3: Backend foundation

**Status:** Done.

Introduce configuration, `pgx/v5`, database lifecycle, common envelopes,
middleware, graceful shutdown, capability-module composition, and only the
seams justified by real variation. Preserve `/healthz`, add `/readyz`, and
align `go.mod`, CI, container builds, and backend guidance on Go 1.24.

**Exit condition:** the service starts with validated configuration, exposes
operational routes, and has reusable infrastructure for feature slices.

### Session 4: API safety and data boundary

**Status:** Done.

Add the migration that closes direct public reads and writes through
Supabase's Data API. Add the shared idempotency-record schema and primitives
used by later authenticated `POST` routes with duplicate durable effects. Add
bounded in-memory token-bucket primitives and public per-IP middleware with
deterministic-clock tests.

**Exit condition:** the Go API is the sole public data contract, retries are
safe, and baseline abuse limits apply before community writes are added. The
hosted project records migration `0002`. A local `GET /readyz` returns 200
from the Session 4 process: `migrations_behind` then 200 after the push, or a
first 200 after port 8080 was cleared when the highest history version is
already `0002`. The API process does not apply that SQL.

### Session 5: Authentication and profiles

**Status:** Done.

Add JWT/JWKS verification, protected-route middleware, the profile migration,
base RLS policies, authenticated per-User write limiting, and
`GET/PATCH/DELETE /v1/me`. The write limit is a token bucket with burst
capacity 60 and a sustained rate of 1 write per second per User. This session
supports active Profiles only and implements the agreed contribution retention
for User deletion. `GET /v1/me` returns a stored non-active status and omits
moderation feedback. An unchanged `PATCH` leaves a stored non-active status
in place. A casing-only Username correction is a real update and stores
`active`. The OpenAPI document gains the bearer security scheme on
those protected routes. It does not add Venue, Review, Check-in, report, or
public-catalogue routes.

**Exit condition:** valid Supabase identities can maintain a profile while
invalid tokens fail consistently.

### Session 6: Profile safety and moderation API

**Status:** Done.

Add Profile Reports, rename-required and suspended states, public identity
redaction, moderation audit, expiry behavior, and privileged moderation routes.
A Platform Moderator is a Supabase auth user with a `platform_operators` row
and no Profile. App routes return `403 forbidden` to that caller. Privileged
routes return `403 forbidden` to an authenticated caller without that row.
No route provisions operators.

`POST /v1/profiles/{username}/reports` files a report and requires
`Idempotency-Key`. `GET /v1/moderation/profile-reports` lists pending reports.
`GET /v1/moderation/profiles/{userID}` reads one Profile.
`POST /v1/moderation/profiles/{userID}/actions` applies `rename_required`,
`suspended`, or `restored` and requires `Idempotency-Key`.
`PATCH /v1/moderation/profile-reports/{reportID}` dismisses one report and
does not require `Idempotency-Key`.

**Exit condition:** Users can report unsafe Profiles, operators can require a
rename or suspend community writes through those endpoints, and moderated
identities are not exposed publicly.

### Session 7: Venue catalogue

**Status:** Done.

Add genre listing, the public venue catalogue, venue detail, literal rating
and check-in aggregates, and cursor pagination. Discovery filters stay in
Session 8. The catalogue includes Verified Venues and pending Venue
Submissions that have at least one Genre. Pending Venues are visibly not
verified. Rejected Venues stay hidden.

`GET /v1/genres` returns every Genre ordered by name and is not paginated.
`GET /v1/venues` lists the catalogue with `limit` and `cursor`.
`GET /v1/venues/{venueID}` returns one catalogue Venue.
Both venue routes use one Venue document. A hidden or unknown Venue is
`404 venue_not_found`.
Venue and Genre name ordering uses explicit `C` collation. Venue cursors use
the `name-v1` discriminator. Traversal has no gap or repeat while membership
and sort keys are unchanged and is best-effort across concurrent changes.

Migration `0005` requires address, city, and `country_code`, adds
`updated_at` and `venue_name_key`, and raises readiness to `0005`.

**Exit condition:** Android can render a paginated venue catalogue and venue
detail screen.

### Session 8: Venue discovery

**Status:** Done.

Add every approved text, classification, location, and radius filter to the
venue list endpoint. Add indexes justified by query plans and PostGIS
integration coverage. Text matching ignores case, accents, punctuation, and
spaces, but not typos; tests pin the relevance and tie-breaking order.

Migration `0006` adds `venue_search_key`, which is `venue_name_key` with all
whitespace removed, and trigger-maintained `search_name`, `search_address`,
and `search_city` columns with indexes that captured query plans use. These
columns serve discovery only; Session 9 duplicate detection keeps using
`venue_name_key`. The server chooses one of four orders from the filters,
cursors are bound to that order and the filter set, and `distance_meters`
appears on list items only when a location is supplied. Readiness requires
`0006`.

**Exit condition:** Android can implement map/list discovery with all MVP
filters.

### Session 9: Venue submissions

**Status:** Not started.

Implement transactional pending submissions, correction of pending
submissions, strong duplicate prevention, daily quotas, and the caller's
submission history. `POST /v1/venues` requires `Idempotency-Key`;
`PUT /v1/me/venues/{venueID}` does not. A verified Venue returns
`409 venue_not_pending` with `This venue can no longer be corrected.`; a
rejected Venue Submission uses the same code with
`Only a pending venue can be corrected.`, which Session 10 replaces.
Duplicate detection uses `venue_name_key` from migration `0005`, which keeps
alphanumeric characters and whitespace and removes punctuation and symbols.
Add any further migration required for submission quotas, duplicate locking,
and RLS read policies. Do not repeat the `0005` column, country-code, or
`updated_at` work.

**Exit condition:** a signed-in User can submit a Venue, track its pending
status, and correct it without creating strong duplicates.

### Session 10: Venue safety and moderation API

**Status:** Not started.

Add rejection feedback, correction of rejected submissions, Venue Reports,
the Delisted Venue lifecycle, append-only moderation audit, and privileged
endpoints that verify, reject, correct, or delist Venues and resolve Venue
Reports. The session plan names the routes and how the existing `403` rule
recognizes an operator. Any `POST` moderation action that appends an audit
entry requires `Idempotency-Key`; state-setting `PUT`/`PATCH` and `DELETE`
routes do not. Do not add an operator-provisioning model.

**Exit condition:** operators can verify, reject, correct, or delist Venues
through those endpoints; Users can report a Venue in the public catalogue;
catalogue history is retained.

### Session 11: Reviews and ratings

**Status:** Not started.

Add the review migration, RLS policies, public review list, authenticated
upsert/delete operations, and venue rating aggregates.
Neither `PUT` nor `DELETE /v1/venues/{venueID}/reviews/me` requires an
`Idempotency-Key`; both target the caller's single Review.

**Exit condition:** Android can display, create, edit, and delete ratings and
reviews.

### Session 12: Review safety and moderation API

**Status:** Not started.

Add Review Reports, commentary restrictions, the append-only moderation audit
schema, and privileged endpoints for listing and resolving Review Reports,
clearing and locking commentary, or deleting the Review. The session plan
names the routes and how the existing `403` rule recognizes an operator. Any
`POST` moderation action that appends an audit entry requires
`Idempotency-Key`; state-setting `PUT`/`PATCH` and `DELETE` routes do not. Do
not add an operator-provisioning model.

**Exit condition:** Users can report unsafe commentary, and trusted operators
can resolve reports through those endpoints without retaining removed text.

### Session 13: Check-ins

**Status:** Not started.

Add the check-in migration, RLS policies, cooldown rule, check-in creation,
personal history, User-only deletion, and venue check-in aggregates. A
corrected Check-in uses a new `Idempotency-Key` under the same operation.
`DELETE /v1/me/check-ins/{checkInID}` does not require one.

**Exit condition:** Android can record visits and show the user's history
without duplicate Check-in spam, and a User can remove an incorrect Check-in.

### Session 14: API completion and admin handoff

**Status:** Not started.

Run cross-feature integration scenarios, verify migration-from-empty and RLS,
close OpenAPI example gaps, confirm error and pagination edge cases, validate
the migration-before-deploy release workflow and Render configuration. A
release is successful only after the committed migrations are applied and
`/readyz` returns 200. Render's `/healthz` success is not that proof.
Document the user-facing and moderation request flows, and document the
account-deletion flow needed for later Google Play disclosure and support
pages.
Do not generate or commit client code, and do not start the admin app.

**Exit condition:** formatting, vet, unit tests, integration tests, migration
tests, and build all pass; the OpenAPI contract describes the runnable user
and moderation routes, and deployment configuration is validated without
certifying a beta release.

## Definition of done for every session

- Work occurs on a `feature/*` branch and targets `dev` by pull request.
- The session plan and code follow
  [the backend architecture guide](docs/architecture/backend.md); any
  deviation updates the guide or adds an ADR in the same pull request.
- New schema changes use a new numbered migration with RLS. A new product
  table, and any sequence that migration creates, revokes `PUBLIC`, `anon`,
  `authenticated`, and `service_role` after the create. A table that uses
  `gen_random_uuid()` adds no sequence revoke.
- New endpoints have HTTP, capability, and relevant integration coverage.
- `backend/openapi.yaml` matches implemented behavior and `/docs` renders it.
- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass from `backend/`.
- Commands and paths changed by the session are reflected in the appropriate
  `AGENTS.md`.
- No secrets, local `.env`, generated binaries, or real tokens are committed.

## Success criteria

After Session 14, the admin project can be designed and built against the
documented API, including moderation. The Android client starts only after
the admin app exists. Each roadmap session is independently understandable,
testable, and reviewable. Moderation has no temporary path.
