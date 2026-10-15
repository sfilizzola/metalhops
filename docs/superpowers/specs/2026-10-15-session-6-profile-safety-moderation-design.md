# Session 6: Profile safety and moderation API

**Date:** 2026-10-15
**Status:** Pending review of this spec

## Goal

Let an app User report another Profile, and let a Platform Moderator require a
rename, suspend community participation, or restore the Profile. The admin app
does not exist yet. This session makes the Go contract those two callers will
use when it does.

The session adds:

- Profile Reports and the combined report quota;
- rename-required and suspended behavior on the caller's own Profile;
- suspension expiry and the moderation audit;
- privileged list, dismissal, and moderation-action routes;
- public-identity redaction as a Profile rule, with no new public route.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. Extend `internal/profile`,
  `internal/auth`, and `internal/api`. Add no moderation package.
- Do not edit migrations `0001`, `0002`, or `0003`.
- Add no operator-provisioning route and no admin app.
- One Supabase auth user is either an app User or a Platform Moderator.
- The API process never applies migrations.

## Approach

`internal/profile` owns reports, state transitions, expiry, audit rows, the
report quota, and their transactions. `internal/auth` answers whether the
authenticated Supabase user has a `platform_operators` row. Profile, Venue,
and Review can reuse that fact later without importing one another.
`internal/api` keeps routing and translates domain results. Privileged routes
use the existing authenticator seam; HTTP tests fake `IsOperator`, and the
production verifier reads the table.

```text
cmd/api
  -> auth
  -> profile
  -> api

auth
  -> existing JWT and live-session checks
  -> platform_operators lookup

profile
  -> reports, transitions, expiry, audit, quota
  -> private PostgreSQL transaction
```

Migration `0004_profile_safety.sql` adds the new tables and the Profile
constraints Session 5 left open. Readiness requires version `0004`.

## Data model

### `platform_operators`

- `user_id uuid primary key references auth.users(id) on delete cascade`
- `created_at timestamptz not null default now()`

Rows are inserted by a trusted database operation outside this API. Tests
insert them with SQL. Enable RLS, revoke `PUBLIC`, `anon`, `authenticated`,
and `service_role`, and add no policy.

Triggers reject an operator insert or update when `profiles.id` already
exists, and reject a Profile insert or update when `platform_operators.user_id`
already exists.

### `profile_reports`

- `id uuid primary key default gen_random_uuid()`
- `reporter_user_id uuid not null references auth.users(id) on delete cascade`
- `target_user_id uuid not null references auth.users(id) on delete cascade`
- `reason text not null`
- `note text null`
- `status text not null default 'pending'`
- `created_at timestamptz not null default now()`
- `resolved_at timestamptz null`
- `resolved_by_user_id uuid null references auth.users(id) on delete set null`

Reasons are `abusive_name`, `hate_or_discrimination`, `impersonation`,
`personal_information`, `spam`, and `other`. Status is `pending`, `actioned`,
or `dismissed`. A note is 1–1,000 characters. `impersonation`,
`personal_information`, and `other` require one. The reporter and target
differ. A partial unique index allows one pending report per reporter and
target. Resolved reports do not block a later report.

Deleting either auth user deletes the report row. Audit rows are separate and
remain. The same RLS revoke applies. `gen_random_uuid()` adds no sequence
revoke. The pending-list index is `(status, created_at, id)`.

### `report_quota_events`

- `id uuid primary key default gen_random_uuid()`
- `user_id uuid not null references auth.users(id) on delete cascade`
- `created_at timestamptz not null default now()`

One row is inserted in the same transaction as each Profile Report. Deleting
the report does not delete the event. Deleting the reporter deletes the
events. Index `(user_id, created_at)`. The same RLS revoke applies.

Later Venue and Review reports insert into this table and do not add another
quota store. The check lives in `internal/profile` while Profile Reports are
the only report type. `shortcut: move the check when the second report type
arrives, because Venue and Review must not import Profile to count the same
rows.`

### `profile_moderation_actions`

Append-only:

- `id uuid primary key default gen_random_uuid()`
- `subject_user_id uuid null references auth.users(id) on delete set null`
- `action text not null`
- `actor_type text not null`
- `actor_user_id uuid null references auth.users(id) on delete set null`
- `reason text null`
- `note text null`
- `suspended_until timestamptz null`
- `created_at timestamptz not null default now()`

`action` is `rename_required`, `suspended`, or `restored`. `actor_type` is
`operator`, `user`, or `system`. Operator and user actors have an actor id. A
system actor does not. Restrictive actions require a taxonomy reason. A
restoration copies the reason and note being cleared. `suspended_until` is
present only on a `suspended` action. A note is required when the reason is
`other`. A trigger rejects updates and deletes. User deletion nulls the
subject and actor references, and the row remains. The same RLS revoke
applies.

### Profile constraints

`0004` adds checks without editing `0003`:

- `moderation_reason` is null or one of the six report reasons;
- `rename_required` has a reason and a null `suspended_until`;
- `suspended` has a reason, and `suspended_until` may be null;
- `active` already requires every moderation field to be null;
- a stored note is 1–1,000 characters, and `other` requires one.

## Authorization

Every protected route verifies the existing bearer token and live session.

An operator row makes the caller a Platform Moderator. App routes reject that
caller with `403 forbidden` before Profile creation or deletion. Privileged
routes reject an authenticated caller with no operator row with
`403 forbidden`. A missing or invalid token remains `401 unauthorized`.

## App-user behavior

`GET /v1/me` returns the caller's real Username and display name, plus
`moderation_reason`, `moderation_note`, and `suspended_until`. Those three
values are JSON null while the Profile is active.

`DELETE /v1/me` remains available in every Profile state. An operator receives
`403 forbidden`.

Before `GET`, `PATCH`, `DELETE`, or a report, the Profile transaction locks
the caller. When `suspended_until` is at or before the request time, it clears
the moderation fields, stores `active`, and appends `restored` with actor type
`system`. That write spends no User write token. An indefinite suspension has
a null expiry and does not clear itself.

`PATCH /v1/me` still creates an active Profile when the caller has none. A
`rename_required` Profile returns to `active` when the Username or display
name changes, including a casing-only Username correction. The audit row uses
actor type `user` and keeps the cleared reason and note. An unchanged patch
leaves `rename_required` in place and spends no token. A `suspended` Profile
can repeat its stored values and receive `200`. Any actual change returns
`403 profile_suspended`. `details.suspended_until` is included only when an
expiry exists. An expired suspension is normalized before the patch, so the
patch then follows the active rules.

`POST /v1/profiles/{username}/reports` requires an active Profile and
`Idempotency-Key`. The operation name is `profile_report`. The path Username
matches the current Username without regard to case, in any Profile status.
The stored response uses the stored casing and includes the report id, reason,
note, `pending` status, and creation time. It includes no User id.

The body is `reason` and an optional `note`. The existing JSON decoder rejects
unknown fields, a second JSON value, and explicit null for a required field.
Text follows the DevSteps rules: UTF-8, NFC, trimmed, no null bytes, and no
non-whitespace control characters. A note may contain line feeds. Omitted and
JSON null both mean no note.

Outcomes:

- `201` when the report and one quota event are stored. The target Profile
  does not change.
- `400 self_report` when the path resolves to the caller.
- `400 validation_error` for a bad reason, note, or idempotency key.
- `404 profile_not_found` when the Username resolves to no Profile.
- `409 profile_already_reported` when that caller already has a pending report
  for that target.
- `409 profile_required` when the caller has no Profile.
- `403 profile_rename_required` or `403 profile_suspended` when the caller
  cannot contribute. Expiry normalization runs first.
- `429 quota_exceeded` when the caller already has 20 report-quota events in
  the rolling 24 hours.

The Profile module owns the contribution decision: no Profile, rename
required, suspended, or allowed. This route uses it. Later community writes
call the same decision after expiry normalization.

`PublicIdentity` returns the display name `MetalHops User` and an empty
Username for `rename_required` and `suspended`. An active Profile returns its
stored Username and display name. The caller's own routes do not use it. This
session adds no public route that shows another person.

## Moderator behavior

Privileged routes identify a Profile by User id.

`GET /v1/moderation/profile-reports` lists pending reports, oldest first by
`created_at` and report id. `limit` and `cursor` follow the existing rules:
default 20, maximum 100, opaque and URL-safe. A malformed cursor is `400
validation_error`. Each item contains the report id, reason, note, status,
creation time, and the full Profile of the reporter and the target: User id,
Username, display name, status, moderation reason, note, suspension expiry,
and both timestamps. Email and auth metadata are absent. Because report rows
cascade with either user, a listed report always has both Profiles.

`GET /v1/moderation/profiles/{userID}` returns that Profile view. A canonical
UUID with no Profile is `404 profile_not_found`. Any other path value is
`400 validation_error`.

`POST /v1/moderation/profiles/{userID}/actions` requires `Idempotency-Key`.
The operation name is `profile_moderation_action`. The body uses the same
JSON decoder and is one action:

- `rename_required` requires a reason. `suspended_until` is omitted or null.
- `suspended` requires a reason. An omitted or null `suspended_until` is
  indefinite. A timestamp must be after the request time and is stored in UTC.
- `restored` sends neither reason, note, nor expiry.

A note is required when the reason is `other`. The transaction locks the
target Profile, then loads any stored idempotency record. A matching unexpired
record returns its stored `201` without reading the command as a new action.
When no record exists, an unchanged status, reason, note, and expiry returns
`200` with `action_id` null, writes nothing, and stores nothing. A later
different Profile state can still accept that key. A change returns `201`
with the new action id and the updated Profile, appends one audit row, and
marks every report that is still pending for that Profile `actioned`,
including one filed after the moderator opened the list. The stored
idempotent response is that `201`. The same key with a different normalized
command returns `409 idempotency_key_reused`.

The normalized command contains the target User id, action, reason, note, and
UTC expiry. The report command contains the resolved target User id, reason,
and note, so Username casing does not change the hash.

`PATCH /v1/moderation/profile-reports/{reportID}` dismisses one pending
report. The body is exactly `{"status":"dismissed"}`. A non-canonical report
id is `400 validation_error`. It records the operator and resolution time,
does not change the Profile, and does not require `Idempotency-Key`. The
response is `200` and uses the report-list item shape. A repeated dismissal
returns `200` and spends no write token. An actioned report returns
`409 report_already_resolved`. An unknown canonical report id returns
`404 profile_report_not_found`.

Moderator writes use the existing per-User bucket, keyed by the operator's
auth User id. Reads do not spend a token.

## Quota, tokens, and idempotency

The public per-IP bucket runs first. A replay spends one public token and no
User write token.

A User write token is spent only when a report is created, a moderation action
appends an audit row, or a dismissal changes a pending report. Normalization,
no-op patches, no-op actions, repeated dismissals, validation failures, and
conflicts spend none.

The report quota is 20 combined events per reporter per rolling 24 hours,
counted from `report_quota_events.created_at`. Resolving, dismissing, or
deleting a report does not return the slot. A request that creates no report
creates no event. An idempotent replay creates neither. The quota check and
insert share the report transaction and a transaction-scoped advisory lock for
that reporter and the `reports` class. `Retry-After` is the whole seconds
until the oldest counted event leaves the window, and at least 1.

The report route calls the existing idempotency helper inside the Profile
transaction. The action route uses that helper's lookup and insert rules, and
inserts a record only for a `201`. The helper's existing 24-hour scope applies.

## Errors

| Failure | Result |
| --- | --- |
| Missing or invalid token, or absent live session | `401 unauthorized` |
| Operator on an app route, or app User on a privileged route | `403 forbidden` |
| Caller cannot contribute while rename is required | `403 profile_rename_required` |
| Caller cannot contribute or cannot change a suspended Profile | `403 profile_suspended`, with `details.suspended_until` when an expiry exists |
| Self-report | `400 self_report` |
| Invalid body, cursor, User id, or idempotency key | `400 validation_error` |
| Unknown Profile or report | `404 profile_not_found` or `404 profile_report_not_found` |
| Pending report already exists for the pair | `409 profile_already_reported` |
| Dismissal targets an actioned report | `409 report_already_resolved` |
| Idempotency key reused with a different command | `409 idempotency_key_reused` |
| Report quota is full | `429 quota_exceeded` with `Retry-After` |
| User write bucket is empty | `429 rate_limited` with `Retry-After` |
| Profile transaction fails | Roll back; store no report, action, event, or idempotency row |

Inherited body limits still return `413` and `415`. Logs keep the wrapped
cause and request ID. Responses omit SQL, tokens, and stack details.

## Tests

Profile tests cover user restoration, system expiry, operator transitions in
both directions, an unchanged action, one pending report per pair,
self-reporting, dismissal, actioning every pending report, and the quota lock.
Two concurrent reports cannot create the 21st event. A replay does not add an
event. Deleting the target removes the report and leaves the reporter's quota
event.

HTTP tests cover both `403 forbidden` directions, suspended-profile details,
list order, idempotency replay and conflict, and the unstored no-op action.
Migration tests apply `0004` after `0003` and check the new constraints,
revokes, exclusion triggers, append-only trigger, and readiness at `0004`.
`PublicIdentity` has a direct test.

## Documentation

`backend/openapi.yaml` documents the new routes, the three added Profile
fields, the error codes, and bearer security on the privileged routes.
`/docs` serves that contract without a new dependency.

The implementation updates DevSteps with the privileged route names, the
`platform_operators` rule, and Session 6 marked done. It raises the readiness
version in `backend/internal/database` and `backend/AGENTS.md` to `0004`.

## Out of scope

- An endpoint that creates or deletes operator rows.
- The admin UI and its login screen.
- A report-history endpoint for the reporter.
- Venue, Review, and Check-in routes.
- A public response that shows another person's identity.
