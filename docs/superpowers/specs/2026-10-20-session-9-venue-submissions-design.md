# Session 9: Venue submissions

**Date:** 2026-10-20
**Status:** Pending review of this spec

## Goal

A signed-in User can submit a Venue, read their submission history, and
correct that Venue while it is pending. The write rejects a strong duplicate
and stops at 10 new Venue Submissions in a rolling 24 hours.

The session adds:

- `POST /v1/venues`, with `Idempotency-Key`;
- `GET /v1/me/venues` and `GET /v1/me/venues/{venueID}`;
- `PUT /v1/me/venues/{venueID}` while the Venue is pending;
- duplicate detection through the existing `venue_name_key` function;
- the submission quota, counted from `venues.created_at`;
- migration `0007`, limited to the indexes a captured plan uses.

Session 10 owns rejection feedback, correction of a rejected Venue, Venue
Reports, delisting, and the moderation routes.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. Extend `internal/venue`,
  `internal/profile`, `internal/api`, and `internal/database`.
- Do not edit migrations `0001` through `0006`.
- Add no Go dependency.
- The API process never applies migrations.
- `GET /v1/genres`, `GET /v1/venues`, and `GET /v1/venues/{venueID}` keep
  their Session 7 and Session 8 contracts. `PublicVenue.verification_status`
  stays `pending` or `verified`.
- Duplicate detection calls `venue_name_key`. This session does not add a
  stored name-key column.
- Venue writes and Profile writes share the one per-User write limiter
  created in `cmd/api`.

## Approach

`internal/venue` owns submission validation, the caller's list and detail,
duplicate detection, the quota, and both write transactions. Catalogue reads
stay on the existing `Catalogue` interface.

HTTP stays in `internal/api` behind a second interface with two adapters, the
PostgreSQL service and an HTTP-test fake:

```go
type Submissions interface {
    Submit(context.Context, venue.SubmitCommand) (venue.Submission, error)
    Correct(context.Context, venue.CorrectCommand) (venue.Submission, error)
    ListOwn(context.Context, venue.OwnQuery) (venue.OwnPage, error)
    GetOwn(context.Context, string, string) (venue.Submission, error)
}
```

`GetOwn` takes the caller id and the venue id. `OwnQuery` carries the caller
id, the limit, and the cursor.

```text
cmd/api
  -> one UserLimiter, passed to profile.New and venue.New
  -> venue.New(pool, profiles, writes, clock)

venue.Submit / venue.Correct
  -> profile.NormalizeExpiry, its own transaction
  -> one venue transaction:
       lock the profile row
       profile.CommunityAccess
       quota lock, quota count, name lock, duplicate check
       write the venue and genre links
       idempotency.Execute around the POST write

venue.ListOwn / venue.GetOwn
  -> one read statement

api
  -> requireApp
  -> JSON and query syntax
  -> Submissions
```

`venue.New` gains the profile service, the shared limiter, and the clock.
Catalogue tests pass a nil profile service and a nil limiter because those
tests never write. A write with either dependency missing is an internal
error. Production always wires both. The clock is `time.Now` in `cmd/api`
and a fixed clock in tests.

There is no new contributor interface. Venue calls
`(*profile.Service).NormalizeExpiry`, which is the current private expiry
commit exported under that name. `Report` calls the exported method.
`NormalizeExpiry` locks the profile, restores an expired suspension, and
commits that restoration even when the later venue write fails. A missing
profile is a successful no-op for that method. The venue transaction then
locks the same row and applies `CommunityAccess`, so a suspension placed
between the two transactions still blocks the write.

`POST` records idempotency inside the venue transaction with operation
`venue_submission`. The callback returns domain errors. The caller commits
only when `Execute` returns a 2xx result and a nil error. A replay commits
that transaction, skips the callback, and does not take a quota slot or a
write token.

The write token is reserved after the quota and duplicate checks pass.
`Correct` reserves one only when the stored row changes. The reservation is
committed after the venue transaction commits, and any rollback cancels it.
List and detail do not reserve one, and they do not call `NormalizeExpiry`.

Reads and the venue transaction each use a two-second timeout.

## Data model

Migration `0007_venue_submissions.sql` adds indexes only. It creates no
table, sequence, policy, or grant, and it does not replace `venue_name_key`.
Data API roles stay revoked on `venues`. A table with no grant needs no
policy.

Duplicate lookup calls `venue_name_key(name)` in SQL. The function is
`stable`, so this session does not expression-index it. The Session 1 GiST
index on `location` serves the distance filter. The Session 8 search-key
trigger runs on these inserts and updates. The write does not set
`search_name`, `search_address`, or `search_city` itself.

Candidate indexes, shipped only when a captured `EXPLAIN` uses them:

- btree on `(submitted_by, created_at)`, for the quota count;
- btree on `(submitted_by, updated_at desc, id desc)`, for the caller's
  list.

An index no plan uses is removed before merge. Readiness requires `0007`.

## Request contract

All four routes use `requireApp`. An operator receives `403 forbidden`. A
missing or invalid token receives `401`. `POST` and `PUT` use the shared
JSON decoder: one object, at most 64 KiB, UTF-8, and no unknown fields. A
structural JSON failure is `400 validation_error` with no field map.

### Submission document

List, detail, create, and correction share one document:

| Field | Rule |
| --- | --- |
| `id` | Canonical UUID |
| `name`, `address`, `city`, `country_code`, `venue_type` | Stored text |
| `description` | String or null |
| `latitude`, `longitude` | `ST_Y` and `ST_X` of the stored point |
| `genres` | `{id, name}`, ordered by name with `C` collation, then id |
| `verification_status` | `pending`, `verified`, or `rejected` |
| `created_at`, `updated_at` | UTC timestamps, encoded like Profile timestamps |

The document has no aggregates, submitter, rejection reason, or rejection
note. The list body is `{data, meta.next_cursor}`. The other three responses
are `{data}`.

`GET /v1/me/venues` accepts only `limit` and `cursor`, with the Session 7
rules: default 20, maximum 100, and an unknown or repeated parameter
rejected. Order is `updated_at` descending, then venue id descending. The
cursor is `submission-v1`, the row's `updated_at` in RFC3339Nano UTC, and
the canonical id, joined with `|` and encoded with base64 raw URL encoding.
The next page contains rows with an earlier `updated_at`, or the same
`updated_at` and a smaller id. An unusable cursor is `400` on `cursor` with
message `invalid`, including a catalogue cursor.

`GET /v1/me/venues/{venueID}` accepts no query parameter. `venueID` uses the
Session 7 canonical-UUID check.

Both reads filter on `submitted_by` equal to the caller. A caller with no
Profile receives an empty list. A missing venue, a venue with a null
submitter, and another User's venue are `404 venue_not_found` with message
`Venue not found.` A `rename_required` or suspended caller can read, including
a `verified` or `rejected` venue they submitted.

### Create and correction body

`POST /v1/venues` and `PUT /v1/me/venues/{venueID}` accept the same object:
`name`, `description`, `latitude`, `longitude`, `address`, `city`,
`country_code`, `venue_type`, and `genre_ids`. `verification_status` is an
unknown field and is rejected. `PUT` ignores `Idempotency-Key`.

Text uses the Session 8 rules: `strings.TrimSpace`, then `norm.NFC`, valid
UTF-8, no null byte, and no non-whitespace control character. Lengths are
rune counts after that normalization. Name, address, and city also reject
line breaks. Description allows line feed and carriage return, and it rejects
a null byte and any other control character; that failure is `must be text`.
Omitting `description` or sending JSON null stores null, and `PUT` may clear
a stored description. A JSON string that trims to empty is invalid.

`country_code` is trimmed and folded to uppercase, then checked against the
`0005` allowlist copied into Go. `gb` becomes `GB`. `UK` fails. `XK` is
accepted. `venue_type` must already be `bar`, `pub`, or `club`.

Coordinates are finite. Latitude is −90 through 90. Longitude is −180 through
180. The point is stored as
`ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography`.

`genre_ids` is a JSON array of integers. Each value fits in a 32-bit
integer and is at least 1. Duplicates are removed. The stored set is those
ids. There is no separate maximum count; a body over 64 KiB still returns
`413`. After the profile lock, and after the venue lock on `PUT`, the
transaction loads those ids from `genres`. An unknown id is `400` with
`must be an existing genre`. That check runs before `Execute` and before the
unchanged-correction return, so a bad id leaves no idempotency row and is
not treated as an unchanged venue. The foreign key remains the backstop if a
genre disappears before the insert.

| Field | Failure message |
| --- | --- |
| `name` | `must be 1 to 150 characters` or `must be a single line of text` |
| `address` | `must be 1 to 500 characters` or `must be a single line of text` |
| `city` | `must be 1 to 120 characters` or `must be a single line of text` |
| `description` | `must be 1 to 2000 characters` or `must be text` |
| `country_code` | `must be an ISO 3166-1 alpha-2 code` |
| `venue_type` | `must be bar, pub, or club` |
| `latitude` | `must be from -90 to 90` |
| `longitude` | `must be from -180 to 180` |
| `genre_ids` | `must include a genre`, `must be a positive integer`, or `must be an existing genre` |
| `venue_id` | `must be a canonical uuid` |
| `idempotency_key` | `required` when `POST` omits the header |

A missing required field uses that field's constraint message. An unusable
`Idempotency-Key` that is present but fails the Session 4 key rule is `400`
with no field map. Validation runs before `idempotency.Execute`, so a `400`
leaves no venue row and no idempotency row.

The idempotency command is this JSON object, with struct field order, sorted
genre ids, uppercase country code, and `description` null when omitted:

```json
{
  "name": "Iron Maiden",
  "description": null,
  "latitude": 59.91,
  "longitude": 10.75,
  "address": "1 High Street",
  "city": "Oslo",
  "country_code": "NO",
  "venue_type": "pub",
  "genre_ids": [1, 4]
}
```

`POST` returns `201`. The stored idempotency body is the Submission object.
The HTTP adapter wraps that object in `data` for the original response and
for a replay, both with status `201`. A replay returns the timestamps from
the original insert. The same key with a different command returns
`409 idempotency_key_reused`.

`PUT` returns `200`. It is unchanged when the normalized fields and the genre
id set equal the locked row, and the submitted latitude and longitude are
equal with Go `==` to the stored `ST_Y` and `ST_X` values. An unchanged
correction does not update the row, does not change `updated_at`, and does
not reserve a write token. Echoing the returned coordinates with the same
other fields is unchanged.

A changed correction replaces the editable columns and the genre links and
leaves `created_at` unchanged. The existing `venues_set_updated_at` trigger
sets `updated_at` to the database time. A genre-only change still updates the
venue row, so that trigger runs. Insert sets both timestamps from the Go
clock. The update trigger does not fire on insert.

A committed pending create or correction has at least one genre, so
`GET /v1/venues` can return it. The public response stays the Session 7
document.

## Write transaction

Field validation of the body happens before either transaction. A `400` from
those rules leaves the database unchanged. `NormalizeExpiry` runs next, in
its own transaction. The venue transaction then does the following. A
non-pending venue returns `409` before the genre lookup, so an otherwise
valid body that names an unknown genre still receives `409` when that venue
cannot be corrected.

1. Lock the caller's profile with `for update`. Apply `CommunityAccess`.
2. On `PUT`, lock the venue with `for update`. A missing row, a null
   submitter, or another submitter is `404`. The caller's verified venue
   is `409 venue_not_pending` with message
   `This venue can no longer be corrected.` The caller's rejected venue
   is `409 venue_not_pending` with message
   `Only a pending venue can be corrected.`
3. Resolve `genre_ids`. An unknown id returns `400` and rolls the
   transaction back.
4. On an unchanged `PUT`, return the locked submission and commit.
5. On `POST`, call `idempotency.Execute`. The steps below that change state
   run inside its callback. `PUT` runs those steps directly, skipping the
   quota lock and the quota count.
6. Take `pg_advisory_xact_lock(hashtextextended('venue_submissions' || chr(31) || userID, 0))`.
   Count rows with that `submitted_by` and `created_at` strictly after the
   clock minus 24 hours, in every status. At 10 or more, return
   `429 quota_exceeded` with message `The submission limit has been reached.`
   `Retry-After` is the number of seconds until the oldest counted
   `created_at` leaves the window, rounded up, and at least 1. A correction
   does not count and does not change `created_at`.
7. Read `venue_name_key` for the submitted name in SQL. Take
   `pg_advisory_xact_lock(hashtextextended('venue_name' || chr(31) || nameKey, 0))`.
   An empty key still takes that lock.
8. Select matches whose `venue_name_key` equals that key, whose status is
   `pending` or `verified`, and whose geography is within 100 metres under
   `ST_DWithin`. The distance is inclusive. A correction excludes its own
   id. Rejected venues are excluded. `The Bar` and `Bar` stay different
   keys, and `Iron Maiden` and `IronMaiden` stay different keys.
9. When the match set is empty, continue. Otherwise return
   `409 duplicate_venue` with message `A matching venue already exists.`
   When any match is `verified`, or has this caller's `submitted_by`,
   `details.venue_id` and `details.verification_status` name the nearest of
   those matches. Distance is `ST_Distance` on geography. The smallest
   canonical id wins a tie. When every match is pending and has a different
   submitter, or a null submitter, the response has no `details`. A
   `verified` venue with a null submitter is disclosable because it is
   verified.
10. Reserve the write token. If the limiter refuses, return
    `429 rate_limited` with `Retry-After` and message `Too many requests.`
11. Insert the pending venue with the caller as `submitted_by`, or update the
    pending venue. Replace genre links in the same transaction. Return the
    submission through the same genre ordering as the catalogue select.

The lock order is the profile row, the idempotency key lock taken inside
`Execute`, the quota lock, then the name lock. `PUT` takes the profile row,
the venue row, and, when the row changes, the name lock. Concurrent creates
with the same normalized name and coordinates within 100 metres produce one
venue. The other request receives `duplicate_venue`. The same name beyond
100 metres can produce two venues; the name lock only serializes the check.

A duplicate, a full quota, a rate limit, and a validation failure leave the
write bucket unchanged and leave no new idempotency row. A successful create
or a changed correction takes one token.

User deletion already sets `submitted_by` null. Those rows leave this User's
quota count and this User's list. This session does not change deletion.

## Errors

| Condition | Result |
| --- | --- |
| Malformed JSON, unknown field, or unusable idempotency key | `400 validation_error`, no field map |
| Field validation, bad cursor, bad `venueID` | `400 validation_error` and the field message above |
| Missing token | `401 unauthorized` |
| Operator on any of these routes | `403 forbidden` |
| `rename_required` on `POST` or `PUT` | `403 profile_rename_required` |
| Suspended on `POST` or `PUT` | `403 profile_suspended`, with `details.suspended_until` when an expiry is set |
| No Profile on `POST` or `PUT` | `409 profile_required` |
| Missing venue, or a venue the caller does not own | `404 venue_not_found` |
| Own verified venue | `409 venue_not_pending`, `This venue can no longer be corrected.` |
| Own rejected venue | `409 venue_not_pending`, `Only a pending venue can be corrected.` |
| Strong duplicate | `409 duplicate_venue` |
| Idempotency key reused with a different command | `409 idempotency_key_reused` |
| Ten creates in the rolling 24 hours | `429 quota_exceeded` |
| Per-User write bucket | `429 rate_limited` |
| Body larger than 64 KiB | `413 body_too_large` |
| Unsupported content type | `415 unsupported_media_type` |
| Unexpected database error | `500 internal_error` |

Profile contribution errors use the same codes and messages as Profile
writes. Reads do not return `profile_required`, `profile_rename_required`,
or `profile_suspended`.

## Tests

Capability tests in `internal/venue`:

- each field rule at its length and coordinate boundaries, including NFC,
  trimming, line breaks, `UK`, `gb`, and `XK`;
- genre-id deduplication, an empty list, a non-integer, and an unknown id;
- a `submission-v1` cursor round trip, and rejection of a catalogue cursor,
  a bad timestamp, and a non-canonical id;
- duplicate disclosure: the nearest disclosable match, the smallest id on a
  tie, a closer private pending match that does not hide a farther verified
  match, and no details when every match is another User's pending venue;
- quota `Retry-After` from the oldest `created_at` inside the window, with
  a minimum of 1, and a row created exactly 24 hours ago excluded;
- unchanged-correction detection before a write.

HTTP tests with a fake submissions adapter:

- `401`, and `403 forbidden` for an operator, on every new route;
- a missing `Idempotency-Key` reports `idempotency_key`, and `PUT` accepts a
  request that omits it;
- each error in the table maps to its status and code, including suspension
  details;
- the document includes both timestamps and omits aggregates;
- the default limit is 20, and an unknown query parameter is rejected;
- a replay body from the fake is wrapped in `data` with status `201`.

PostGIS integration tests, from an empty database through `0007`:

- a pending create with genres is visible on `GET /v1/venues` after commit,
  and the public document still omits the submitter and the timestamps;
- `ST_Project` places a second venue at exactly 100 metres and just beyond
  it. The same normalized name conflicts at 100 metres against a pending
  venue and a verified venue. The point just beyond does not conflict. A
  rejected venue does not conflict. `Iron Maiden` and `IronMaiden` do not
  conflict. `!!!` and `???` share an empty key and conflict inside 100
  metres;
- another User's pending match has no `details`. The caller's own pending
  match, and a verified match, include `venue_id` and
  `verification_status`. A pending venue whose submitter was deleted still
  conflicts and stays undisclosed;
- two concurrent creates with the same normalized name and close coordinates
  leave one venue;
- the 10th create in the window succeeds and the 11th returns `429`. The
  count includes a verified or rejected row from that window. A correction
  does not take a slot and does not change `created_at`. A replay takes no
  slot and no write token. A duplicate or a full quota leaves the write
  bucket unchanged. A successful create, and a changed correction, each take
  one token from the shared limiter;
- a changed correction replaces genres, stays `pending`, stays on the public
  list, and increases `updated_at`. A genre-only change does too. An
  identical `PUT` leaves `updated_at` unchanged. The search-key columns
  follow the new name, address, and city;
- the owner can read a `verified` or `rejected` venue. `PUT` on the
  verified venue returns `409 venue_not_pending` with message
  `This venue can no longer be corrected.` `PUT` on the rejected venue
  returns `409 venue_not_pending` with message
  `Only a pending venue can be corrected.` Another User's venue, a null
  submitter, and a missing id are `404`. A non-canonical id is `400`;
- a `rename_required` caller and a suspended caller can list and read, and
  both writes return the contribution error. An expired suspension is
  restored by the write, and that caller can then submit. A caller with no
  Profile receives an empty list and `409 profile_required` on `POST`;
- the same idempotency key and command replay the stored `201`, including
  the original timestamps after a later correction. A different command
  returns `409 idempotency_key_reused`. A validation failure leaves no venue
  and no idempotency row;
- a page size of one walks the caller's list once, ordered by `updated_at`
  descending and id descending, and does not include another User's venues;
- after `ANALYZE` on a few thousand venues, `EXPLAIN` uses each shipped
  index for the quota count and the caller's list. An unused candidate is
  removed from `0007` before merge;
- readiness is `migrations_behind` when `0006` is the newest recorded
  version and ready after `0007`. `PUBLIC`, `anon`, `authenticated`, and
  `service_role` still have no privilege on `venues`.

The OpenAPI contract test requires the four paths, bearer security on each,
`Submission`, `duplicate_venue`, `venue_not_pending`,
`Only a pending venue can be corrected.`, and `quota_exceeded`.

## Documentation

`backend/openapi.yaml` documents the `Submission` schema, the four routes,
the field rules, the cursor, and the error codes above. `/docs` serves that
contract without a new dependency.

`backend/AGENTS.md` raises readiness from `0006` to `0007`, describes
`internal/venue` as catalogue, discovery, and submissions, and adds the four
routes to the app-route list. The shared write limiter sentence covers venue
creates and corrections.

DevSteps marks Session 9 done and moves the header to Sessions 1–9 done,
Session 10 next.

## Out of scope

- Rejection reason, rejection note, and correcting a rejected Venue.
  Session 10 replaces the rejected `PUT` refusal with a correction that
  sets the submission back to pending and clears Rejection Feedback. The
  message `Only a pending venue can be corrected.` is this session's
  slice, not a domain rule.
- `delisted`, delisting fields, and `status_changed_at`.
- Venue Reports and venue moderation routes.
- Real rating and check-in aggregates.
- Operator provisioning and the admin app.
- A stored `venue_name_key` column.
