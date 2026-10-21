# Session 12: Review safety and moderation API

**Date:** 2026-10-21
**Status:** Approved in planning; pending spec review

## Goal

Users can report unsafe Review commentary. Operators can resolve those
reports, clear and lock commentary while keeping the rating, or delete the
Review. Removed commentary is never stored in a report or an audit row.

The session adds:

- migration `0010`: `commentary_removed_at`, `review_reports`,
  `review_moderation_actions`, and `review_commentary_restrictions`;
- five routes (below);
- the commentary lock and report dismissal on the Session 11 Review writes;
- the delist side effect that actions pending Review Reports for that Venue.

**Exit condition:** Users can report unsafe commentary, and trusted operators
can resolve reports through those endpoints without retaining removed text.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. Behavior goes in `internal/review`,
  `internal/api`, and one update inside the existing Venue delist transaction.
  No new package.
- Do not edit migrations `0001` through `0009`. Add no Go dependency.
- No operator-provisioning model. Operators are recognized by the existing
  `platform_operators` row through `requireOperator`. An operator on an app
  route, or an app User on a privileged route, receives `403 forbidden`.
- `POST` routes that file a report or append an audit row require
  `Idempotency-Key`. `PATCH` dismiss does not. A key sent on
  `PUT` or `DELETE /v1/venues/{venueID}/reviews/me` stays ignored.
- Report quota reuses `report_quota_events` and
  `profile.CheckReportQuota` / `profile.RecordReportEvent`. The combined limit
  stays 20 Profile, Venue, and Review Reports per rolling 24 hours.
- Reads and writes use a 2-second timeout, matching Review and Venue.
- Hidden Commentary, appeals that clear a lock, a report-history endpoint,
  check-ins, Venue merging, and the admin app are out of scope.

## Modules

`internal/review` owns report validation, the commentary lock, the audit, and
the Review write transaction, in `report.go` and `moderate.go`, mirroring
`internal/venue`. It already imports `internal/profile`. It does not import
`internal/venue`.

`internal/api` gains a `ReviewModeration` interface with two adapters: the
PostgreSQL `*review.Service`, and a fake used by HTTP tests. The existing
`Reviews` interface keeps the four Session 11 methods. `PUT` maps
`409 commentary_locked` on that existing seam.

Delist stays in `internal/venue`. The capability that owns the delist
transaction owns the invariant "delisting actions every pending Review Report
for that Venue." The delist callback runs one `update` on `review_reports`.
Venue does not import Review. A database trigger is not used. The composition
root does not share the transaction.

## Data (migration `0010_review_safety.sql`)

### `reviews`

Add nullable `commentary_removed_at timestamptz`. Check
`reviews_commentary_removed_check`: the timestamp is null, or `commentary` is
null. A save never clears the timestamp.

Public and own-Review documents gain `commentary_removed` (boolean), true only
when the timestamp is set. An author who never wrote commentary, or who
cleared it themselves, stays `commentary_removed: false`. There is no second
lock field. A later rating-only Review under an existing restriction looks
like any rating-only Review until a commentary write returns
`409 commentary_locked`.

Removing commentary leaves the rating in `average_rating` and `review_count`.
Deleting the Review drops it from those aggregates because the row is gone.
`check_in_count` stays `0`.

### `review_reports`

Mirrors `venue_reports`:

- `id uuid` primary key, default `gen_random_uuid()`;
- `reporter_user_id uuid` not null, references `auth.users (id)` on delete
  cascade;
- `review_id uuid` not null, references `reviews (id)` on delete cascade;
- `venue_id uuid` not null, references `venues (id)` on delete cascade;
- `reason text` not null, one of `spam`, `harassment`,
  `hate_or_discrimination`, `personal_information`, `illegal_content`,
  `off_topic`, `other`;
- `note text` null, or 1–1,000 characters; `other` requires a note;
- `status text` not null, default `pending`, one of `pending`, `actioned`,
  `dismissed`;
- `created_at timestamptz` not null default `now()`; the insert passes the
  service clock, matching `venue_reports`;
- `resolved_at timestamptz` null;
- `resolved_by_user_id uuid` null, references `auth.users (id)` on delete set
  null.

Resolution check, matching Venue Reports: pending has null `resolved_at` and
null `resolved_by_user_id`; `actioned` and `dismissed` have a non-null
`resolved_at`. `resolved_by_user_id` may become null when that User is
deleted.

Indexes:

- unique `review_reports_pending_pair_key` on `(reporter_user_id, review_id)`
  where `status = 'pending'`;
- `review_reports_pending_list_key` on `(created_at, id)` where pending;
- `review_reports_pending_review_key` on `(review_id)` where pending;
- `review_reports_pending_venue_key` on `(venue_id)` where pending.

The table has no commentary column. Deleting the Review cascades its reports.
That is how an author delete and an operator delete end pending reports.
Those two deletes leave `report_quota_events` in place. User deletion of the
author cascades the Review and its reports; User deletion of the reporter
cascades that reporter's reports and, through the existing quota table, that
reporter's quota events.

### `review_moderation_actions`

Append-only. The mutation trigger matches Venue audit: update and delete
raise `55000`, except an update that only nulls `reviewer_user_id` or
`operator_user_id`. Insert requires `operator_user_id` (the Venue actor
trigger, `23514`).

Columns:

- `id uuid` primary key, default `gen_random_uuid()`;
- `review_id uuid` not null, no foreign key, so the id survives deletion;
- `reviewer_user_id uuid` null, references `auth.users (id)` on delete set
  null;
- `venue_id uuid` not null, references `venues (id)` with no `ON DELETE`
  clause, matching `venue_moderation_actions`;
- `action text` not null, `commentary_removed` or `review_deleted`;
- `reason text` not null, the same seven values as reports;
- `note text` null, or 1–1,000 characters; `other` requires a note;
- `operator_user_id uuid` null, references `auth.users (id)` on delete set
  null;
- `created_at timestamptz` not null default `now()`; the insert passes the
  service clock.

Index `review_moderation_actions_review_key` on
`(review_id, created_at desc, id desc)`. The table has no commentary column.
A persistence test fails if `review_reports` or `review_moderation_actions`
has a column whose name contains `commentary`.

### `review_commentary_restrictions`

One row per User and Venue:

- `user_id uuid` not null, references `auth.users (id)` on delete cascade;
- `venue_id uuid` not null, references `venues (id)` on delete cascade;
- `created_at timestamptz` not null default `now()`; the upsert passes the
  service clock;
- unique `review_commentary_restrictions_pair_key` on `(user_id, venue_id)`.

Both moderation actions upsert this row. User deletion removes it with the
User. This lock is not the Profile restriction record in
[ADR 0003](../../adr/0003-keep-restriction-records-after-user-deletion.md).
Nothing in this session deletes a restriction except those cascades.

Every new table enables row-level security and revokes `public`, `anon`,
`authenticated`, and `service_role`. Trigger functions get the same execute
revoke as Venue audit. `gen_random_uuid()` adds no sequence revoke. There is
no policy. `requiredMigrationVersion` becomes `0010`.

## Routes

| Route | Caller | Notes |
| --- | --- | --- |
| `POST /v1/venues/{venueID}/reviews/{reviewID}/reports` | Active Profile | `Idempotency-Key`, operation `review_report` |
| `GET /v1/moderation/review-reports` | Operator | pending, oldest first |
| `GET /v1/moderation/reviews/{reviewID}` | Operator | any Venue status |
| `POST /v1/moderation/reviews/{reviewID}/actions` | Operator | `Idempotency-Key`, operation `review_moderation_action` |
| `PATCH /v1/moderation/review-reports/{reportID}` | Operator | dismiss; no key |

A missing or invalid token is `401 unauthorized`. Path ids that are not
canonical UUIDs are `400` with `venue_id`, `review_id`, or `report_id`:
`must be a canonical uuid`. Unknown JSON fields are rejected. Bodies stay
within the existing 64 KiB limit.

Privileged Profiles are `profile.Profile` through the existing moderation
shape: User id, Username, display name, status, moderation reason, note,
suspension expiry, and the Profile timestamps. No email and no raw auth
metadata. Public redaction does not apply. A deleted User is JSON null.

### Filing a report

`POST` accepts `reason` and optional `note`. `reason` must be one of the seven
report reasons (`reason: must be a known reason`). A note is trimmed and
normalized with `profile.NormalizeNote`. A blank or over-long note is
`note: must be 1 to 1000 characters`. `other` with no note is
`note: required`. A missing key is the existing `400 validation_error` with
field `idempotency_key: required`. The same key with a different command is
`409 idempotency_key_reused`. Only a stored `2xx` replays. Validation,
conflict, quota, and rate-limit errors are not recorded.

Checks, in order:

1. `NormalizeExpiry`, then `RequireContributor`. Outcomes are
   `409 profile_required` (`Create a profile first.`),
   `403 profile_rename_required` (`Rename this profile before continuing.`),
   and `403 profile_suspended` (`This profile is suspended.`), with
   `suspended_until` when the Profile has one.
2. Venue lookup under `for share`. A hidden, rejected, Delisted, genre-less,
   or unknown Venue is `404 venue_not_found` (`Venue not found.`). A pending
   public Venue is `409 venue_not_verified` (`Reviews open after this venue
   is verified.`).
3. The Review must belong to that Venue. Anything else is
   `404 review_not_found` (`Review not found.`).
4. The caller's own Review is `400 self_report` (`You cannot report your own
   review.`). This wins over a rating-only own Review.
5. Null commentary, including commentary a moderator already removed, is
   `400 commentary_not_public` (`This review has no public commentary.`).
6. Inside `idempotency.Execute`: a pending report for this User and Review is
   `409 review_already_reported` (`You already reported this review.`). Quota
   exhaustion is `429 quota_exceeded` (`The report limit has been reached.`)
   with `Retry-After`. Then reserve a write token. A refusal is `429
   rate_limited` with `Retry-After`. Then insert the report and
   `RecordReportEvent`.

Steps 1–5 run before idempotency lookup, matching Venue reports. A retry
after the Venue is Delisted, or after the commentary is gone, returns that
current error and does not replay the stored `201`. A replay while the
Review is still reportable returns the stored `201` and does not take another
quota slot or write token.

Success is `201`:

```json
{
  "id": "uuid",
  "review_id": "uuid",
  "venue_id": "uuid",
  "reason": "harassment",
  "note": null,
  "status": "pending",
  "created_at": "2026-10-21T09:00:00Z"
}
```

The Review is unchanged. The response has no commentary.

### Operator list

`GET /v1/moderation/review-reports` accepts only `limit` and `cursor`. `limit`
defaults to 20 and accepts 1 through 100. Outside that range, the field error
is `limit: must be from 1 to 100`. Any other query parameter is `400`.

Pending reports only, ordered by `created_at` then `id` ascending. The
implementation fetches one extra row. The cursor payload, before
`base64.RawURLEncoding`, is `review-report-v1|{created_at}|{reportID}`.
`created_at` is UTC RFC3339Nano. A cursor that does not parse is `400` with
`cursor: invalid`. The page envelope is `data` and `meta.next_cursor`.

Each item has `id`, `reason`, `note`, `status` (`pending`), `created_at`,
`review_id`, `venue_id`, `rating`, `commentary`, `commentary_removed`,
`reporter`, and `author`. `commentary` is the text stored on the Review at
read time, not a snapshot from when the report was filed. Resolved reports
are omitted. A report never stores commentary.

### Operator detail

`GET /v1/moderation/reviews/{reviewID}` returns the Review for any stored
row, including when the Venue is pending, rejected, or Delisted. A missing
Review is `404 review_not_found`. The body is the operator Review document,
each pending report for that Review with its reporter Profile (oldest first),
and the newest 20 audit rows (`created_at` descending, `id` descending).
Resolved reports are omitted. History is not paginated.

The operator Review document is:

```json
{
  "id": "uuid",
  "venue_id": "uuid",
  "user_id": "uuid",
  "rating": 4,
  "commentary": null,
  "commentary_removed": true,
  "commentary_removed_at": "2026-10-21T10:00:00Z",
  "author": { "id": "uuid", "username": "metal_fan", "display_name": "Metal Fan", "status": "active", "moderation_reason": null, "moderation_note": null, "suspended_until": null, "created_at": "2026-10-21T08:00:00Z", "updated_at": "2026-10-21T08:00:00Z" },
  "created_at": "2026-10-21T09:00:00Z",
  "updated_at": "2026-10-21T10:00:00Z"
}
```

`author` is null when that User has no Profile. `commentary_removed_at` is
null when the timestamp is null. An audit row is `id`, `action`, `reason`,
`note`, `review_id`, `reviewer_user_id`, `venue_id`, `operator_user_id`, and
`created_at`. It has no commentary property.

### Operator actions

`POST` accepts `action`, `reason`, and optional `note`. `action` is
`remove_commentary` or `delete_review`
(`action: must be remove_commentary or delete_review`). Reason and note use
the report rules, and both actions require a reason. The idempotency key
rules match report filing.

The transaction locks the Review `for update` only after idempotency lookup,
so a replay of a delete still returns the stored response after the row is
gone. A lookup miss and a missing Review is `404 review_not_found`.

Both actions apply on a pending, verified, rejected, or Delisted Venue, with
or without a pending report. A write that appends an audit row is `201`,
matching Venue moderation, and stores that status. Replay returns the stored
`201` and body. The stored body never contains commentary.

`remove_commentary`:

- Commentary is non-null: set commentary null, set `commentary_removed_at` and
  `updated_at` to the request clock, leave `created_at` and the rating,
  upsert the restriction, append `commentary_removed` with the author as
  `reviewer_user_id`, and mark that Review's pending reports `actioned` with
  the operator as `resolved_by_user_id`. `201`.
- Commentary is null and a restriction row exists: no-op. `200`, `action_id`
  null, no audit row, no write token, reports unchanged. The no-op is not
  stored, so the same key no-ops again.
- Commentary is null and no restriction exists: upsert the restriction, append
  the audit row, and mark any pending reports for that Review `actioned`.
  Leave `commentary_removed_at` null. `201`. The report update matches zero
  rows when the author already cleared the text.

The `201` and `200` bodies are `action_id`, `review_id`, and `review` (the
operator Review document). The no-op sends `action_id: null`.

`delete_review` appends `review_deleted` with the author as
`reviewer_user_id`, upserts the restriction, then deletes the Review. The
delete cascades reports in the same transaction. There is no status update of
those reports before they disappear. `201` with `action_id`, `review_id`, and
`review: null`. A second key, after the row is gone, is `404
review_not_found`.

`other` without a note is `400` before a token is reserved. A no-op spends no
write token. A validation failure, a `404`, and a `409` spend none. A write
that reserves a token and finds the bucket empty is `429 rate_limited` with
`Retry-After`, and the transaction rolls back.

### Dismiss

`PATCH /v1/moderation/review-reports/{reportID}` matches Venue report
dismissal. A pending report becomes `dismissed` with `resolved_at` set and
`resolved_by_user_id` the operator, and spends a write token. An already
dismissed report returns unchanged and spends no token. An actioned report is
`409 report_already_resolved` (`That report is already resolved.`). A missing
report is `404 review_report_not_found` (`Report not found.`). The Review is
unchanged. Success is `200` and returns the list-item fields with
`status: dismissed`.

## Session 11 writes

`PUT /v1/venues/{venueID}/reviews/me` keeps its venue-then-contributor order
and still takes no idempotency key. After `RequireContributor` and the
current-row read:

- Normalized commentary is non-null and `review_commentary_restrictions` has
  this User and Venue: `409 commentary_locked` (`Commentary on this review is
  locked.`). This runs before the unchanged-save short-circuit and before a
  write token. The row stays unchanged. The same check applies when the
  Review row is new.
- Omitted, null, or whitespace-only commentary is a clear. It does not create
  a restriction and does not set `commentary_removed_at`. When this save
  changes stored commentary from non-null to null, it sets pending reports
  for that Review to `dismissed`, `resolved_at` to the save clock, and
  `resolved_by_user_id` to the author. The update's predicate is
  `status = 'pending'`, so an already actioned report stays actioned.
- Replacing non-null commentary with different non-null commentary leaves
  pending reports pending.
- An unchanged rating and commentary stays the Session 11 no-op: `200`, no
  token, reports unchanged. The lock check has already run, so this no-op
  applies when the normalized commentary is null or when no restriction
  exists.
- A rating change while commentary is omitted or null still returns `200`
  when the User is restricted.

`DELETE /v1/venues/{venueID}/reviews/me` still deletes that one row. Pending
reports cascade with it. A restriction, when one exists, remains. A later
Review is a new row. It can be reported when it has public commentary and no
restriction. A commentary write under the restriction returns
`409 commentary_locked`.

Public list and own-Review documents gain the boolean. A removed Review is:

```json
{
  "id": "uuid",
  "rating": 4,
  "commentary": null,
  "commentary_removed": true,
  "author": {"username": "metal_fan", "display_name": "Metal Fan"},
  "created_at": "2026-10-21T09:00:00Z",
  "updated_at": "2026-10-21T10:00:00Z"
}
```

The own-Review document adds `user_id` and `commentary_removed` to the
Session 11 shape and still shows the stored Username and display name.

## Delist

Inside the existing Venue moderation callback, when `action` is `delist`, one
statement marks pending `review_reports` for that `venue_id` `actioned`, with
`resolved_at` set and `resolved_by_user_id` the operator. It runs for the
first delist and for a feedback-only delist, next to the existing Venue-report
resolution. An identical re-delist returns before that callback and does not
touch Review Reports. `verify`, `reject`, `reopen`, and operator correction do
not touch them.

Delist does not clear commentary, does not set `commentary_removed_at`, does
not write `review_moderation_actions`, and does not upsert a restriction.
Reopen leaves actioned Review Reports actioned. Public reads stay hidden
while the Venue is Delisted, and the commentary is public again when it
returns to verified, unless an operator removed it. A User may file a new
report once that commentary is public.

The author-clear update and this delist update both change report rows. The
author clear only updates `status = 'pending'`. A delist that commits first
leaves nothing for the clear to dismiss.

## Testing

Capability tests in `internal/review`, against PostGIS, skipping locally when
Docker is unavailable:

- report check order: hidden Venue `404`, pending Venue `409`, own Review
  `400 self_report` even when rating-only, rating-only `400
  commentary_not_public`, second pending report `409` before quota, quota
  `429`, replay without a second quota row;
- a retry after delist or after commentary removal does not replay the
  `201`;
- `remove_commentary` clears text, sets the timestamp, locks, actions
  reports, and leaves the rating;
- the already-locked no-op, and the lock written onto a rating-only Review
  without setting `commentary_removed_at`;
- `delete_review` cascades reports, keeps the quota event, upserts the
  restriction, and replays `201` after the row is gone;
- a later Review under that restriction accepts a rating and rejects
  commentary;
- author clear dismisses pending reports and does not lock;
- author replace leaves pending reports open;
- an unchanged save leaves reports pending and spends no token;
- audit rows and report rows contain no commentary text.

Venue moderation tests: a real delist and a feedback-only delist action
pending Review Reports; an identical re-delist, a reopen, a verify, and a
reject do not.

HTTP tests map each new route and `409 commentary_locked` on `PUT`, using the
fakes. Persistence tests in `internal/database` cover the checks, the partial
unique index, the append-only trigger, the actor-required insert, and RLS
closed to `anon`, `authenticated`, and `service_role`. `internal/review` gains
a query-plan check that the pending-list query uses
`review_reports_pending_list_key`.

`contract_test.go` drops the assertion that the document lacks
`commentary_locked` and `commentary_removed`. It requires these paths:
`/v1/venues/{venueID}/reviews/{reviewID}/reports`,
`/v1/moderation/review-reports`, `/v1/moderation/reviews/{reviewID}`,
`/v1/moderation/reviews/{reviewID}/actions`, and
`/v1/moderation/review-reports/{reportID}`. It also requires the codes
`review_already_reported`, `commentary_not_public`, `commentary_locked`, and
`review_report_not_found`, and `commentary_removed` on the public and
own-Review schemas.

## Docs that land with the code

- `backend/openapi.yaml` documents the five routes, the new fields, and the
  error codes above. The audit schema has no commentary property.
- `backend/AGENTS.md` raises readiness to `0010` and lists the five routes
  and the lock on `PUT`.
- `docs/architecture/backend.md` extends the Review paragraph: the Review
  module also owns reports, the commentary lock, and the audit; Venue delist
  updates pending `review_reports` in its own transaction and still does not
  import `internal/review`.
- `DevSteps.md` marks Session 12 done only when this work lands: the five
  routes, migration `0010`, readiness `0010`, and delist now actions pending
  Review Reports. The header moves to Session 13 next.

## Confirmed decisions

- A `remove_commentary` that writes marks pending reports for that Review
  `actioned` and keeps them. The no-op leaves reports unchanged.
  `delete_review`, author delete, and User deletion cascade the report rows
  instead. DevSteps says a moderation action marks pending reports actioned,
  and also says an author delete closes reports with the Review. The cascade
  is the close. The actioned status is the path that keeps the Review.
- Author clear is `dismissed` with the author as resolver, and it does not
  lock the User. Moderator removal is what locks.
- A moderation write that appends an audit row is `201`, and replay returns
  that stored `201`, matching
  `POST /v1/moderation/venues/{venueID}/actions`. The no-op stays `200`.
- Filing checks the Profile before the Venue, matching Venue reports. Review
  saves still check the Venue before the Profile.
- The operator list and detail show the commentary stored on the Review at
  read time. Replacing commentary leaves a pending report open, and the
  moderator assesses that current text.

## Out of scope

- Hidden Commentary and any Verified Venue Owner route.
- A route that clears `review_commentary_restrictions`.
- A report-history or resolution-status endpoint.
- Check-ins (Session 13), operator provisioning, Venue merging, and the admin
  app.
