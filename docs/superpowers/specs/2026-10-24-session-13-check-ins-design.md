# Session 13: Check-ins

**Date:** 2026-10-24
**Status:** Approved in brainstorming; pending spec review

## Goal

Android can record a visit, show the caller's history, and remove an incorrect
Check-in. A Check-in is one User's visit to one Venue. The same User may check
in again after the cooldown. The visit time and the Venue are never edited in
place. A correction deletes the Check-in and creates another with a new
`Idempotency-Key`.

The session adds:

- migration `0011`, which creates only `check_ins`;
- the `internal/checkin` capability;
- three routes;
- a real `check_in_count` on the existing venue list and detail.

**Exit condition:** Android can record visits and show the user's history
without duplicate Check-in spam, and a User can remove an incorrect Check-in.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Do not edit migrations `0001` through `0010`. Add no Go dependency.
- `cmd/api` stays composition only.
- `DELETE` does not take an `Idempotency-Key`. A sent key is ignored.
- No check-in moderation, public activity feed, operator read, photos, device
  coordinates, daily quota, stored counter, or schema constraint for the
  cooldown. Session 14 owns the admin handoff.

## Modules

`internal/checkin` owns visit validation, the cooldown, authorship, the write
transaction, personal history, and deletion. Its constructor takes the pool,
the `*profile.Service`, the per-User write limiter, the clock, and the cooldown
duration, matching `review.New` plus that duration. `profiles` and `writes` may
be nil for read-only use; a write then fails. Writes call
`profiles.NormalizeExpiry` before the transaction, as Review writes do. Create
and delete call `profile.RequireContributor`. The package reads `venues` in its
own SQL for eligibility and the history summary. It imports `profile`,
`ratelimit`, and `idempotency`. It does not import `internal/venue`.

HTTP adapters stay in `internal/api`. That package gains a `CheckIns` interface
with two adapters: the PostgreSQL service, and a fake used by HTTP tests. The
interface lives with the caller. The check-in package exposes a concrete
service and no extra seam. The plan chooses the file split inside
`internal/checkin`.

`internal/config` parses `CHECKIN_COOLDOWN` and passes the duration to the
constructor. The value is trimmed. Empty or unset means six hours. Any other
value must be a Go duration greater than zero. Zero, a negative duration, and
an unparseable value stop startup with
`CHECKIN_COOLDOWN must be a positive duration`. Changing the duration keeps the
error code and the response shape. No migration encodes the interval.

The Venue catalogue query replaces the literal `check_in_count` of `0` with a
count of `check_ins`. `internal/venue` does not import `internal/checkin`.
List and detail stay one round trip.

`cmd/api` wires config, the service, and the three routes onto the existing
authenticated app group. An operator on those routes receives the existing
`403 forbidden`. A missing or invalid token is `401`. Reads and writes use a
2-second timeout. The public per-IP bucket still applies to all three routes,
before the handler.

| Route | Caller |
| --- | --- |
| `POST /v1/venues/{venueID}/check-ins` | Active Profile. Records one visit. |
| `GET /v1/me/check-ins` | Signed-in caller, including one who must rename or is suspended. |
| `DELETE /v1/me/check-ins/{checkInID}` | Active Profile. Removes one of their Check-ins. |

## Data

Migration `infra/supabase/migrations/0011_check_ins.sql` creates `check_ins`.
Readiness requires `0011`.

The table has:

- `id uuid` primary key, default `gen_random_uuid()`;
- `venue_id uuid` not null, references `venues (id)` on delete cascade;
- `user_id uuid` not null, references `auth.users (id)` on delete cascade;
- `visited_at timestamptz` not null;
- `recorded_at timestamptz` not null.

There is no `updated_at`, no device coordinates, and no status column.
Rejecting or delisting a Venue leaves its Check-ins in place. Deleting the
User or the Venue row cascades them away.

The cooldown and the 24-hour backdating limit are not check constraints. Both
are compared to the request clock.

Three indexes:

- `check_ins_user_history_idx` on `(user_id, visited_at desc, id desc)` serves
  personal history. The integration test fails if that plan does not use the
  index.
- `check_ins_user_venue_visit_idx` on `(user_id, venue_id, visited_at)` serves
  the cooldown lookup.
- `check_ins_venue_idx` on `(venue_id)` serves the catalogue count. The history
  index leads with `user_id`, so it does not serve a per-Venue count.

`gen_random_uuid()` adds no sequence revoke. The migration enables row-level
security and revokes `public`, `anon`, `authenticated`, and `service_role`.
There is no policy. Idempotency stays on `idempotency_records`. The operation
name is `check_in`.

## Recording a visit

`POST /v1/venues/{venueID}/check-ins` requires `Content-Type: application/json`
and an `Idempotency-Key`. The body is one JSON object. `{}` checks in at the
server time. Unknown fields are rejected. Query parameters use the existing
`rejectedQuery` rules: an unknown parameter is `is not accepted`, and a
repeated parameter is `must be provided once`. A venue id that is not a
canonical UUID, using the same rule as other Venue ids, is `400` with
`venue_id: must be a canonical uuid`. A missing key is `400` with
`idempotency_key: required`. A key the existing helper rejects is `400
validation_error` with no fields.

`visited_at` may be omitted or null. Both mean the server time. A present
value is a JSON string that parses as RFC 3339. Any other JSON type, or a
string that does not parse, is `visited_at: must be a timestamp`. The service
parses the instant and formats it with Go's `RFC3339Nano` in UTC before
hashing, so `10:00Z` and `12:00+02:00` are the same command. A time before
`serverNow - 24h` is `visited_at: must be within the last 24 hours`. A time
exactly 24 hours before is accepted. A time after `serverNow + 5m` is
`visited_at: must not be more than 5 minutes ahead`. A time up to five minutes
ahead, including exactly five minutes, is stored as the server time.

The idempotency command is compact JSON with keys in this order:
`{"venue_id":"<uuid>","visited_at":"<utc>"|"null"}`. No extra whitespace. The
operation name is `check_in`. The hash uses the normalized input. Clamping
changes the stored `visited_at` and does not change the command.

The idempotency record stores the Check-in document, the object inside `data`,
not the HTTP envelope. The handler wraps that document on the original
response and on replay. The same key and command return that stored snapshot.
A later Venue rename does not change it. Replay also returns it after the row
was deleted and when the Venue is no longer in the catalogue. Replay does not
insert again and does not spend a write token. A key that already succeeded
stays a replay for 24 hours, including after the cooldown has passed. After
the record expires, the helper treats the key as unused. The next visit uses
a new key. A different command with a live key is
`409 idempotency_key_reused` with `That idempotency key was already used.`

`NormalizeExpiry` runs before the transaction. A new attempt then runs in one
transaction:

1. `idempotency.Lookup` for this User, operation `check_in`, key, and command.
   A stored match commits and returns the `201` before any other check. A
   different live hash rolls back with `idempotency_key_reused`. An invalid
   key is `400 validation_error`.
2. Load the Venue `for share`. The visible Venue is the catalogue predicate:
   `pending` or `verified`, with at least one Genre. Anything else, including
   rejected, Delisted, unknown, and genre-less rows, is `404 venue_not_found`,
   `Venue not found.` A pending Venue is allowed. The Submitter is not
   special. A hidden Venue is `404` for a suspended caller as well.
3. `RequireContributor` locks the Profile `for update`. The failures are the
   existing ones: `409 profile_required`, `Create a profile first.`;
   `403 profile_rename_required`, `Rename this profile before continuing.`;
   `403 profile_suspended`, `This profile is suspended.`, with
   `suspended_until` when the Profile has one.
4. Take `pg_advisory_xact_lock(hashtextextended('check_in' || chr(31) || userID || chr(31) || venueID, 0))`.
5. `idempotency.Execute` runs the callback under its own key lock. The
   callback looks for this User's Check-in at this Venue whose stored
   `visited_at` is strictly inside the cooldown of the visit time about to be
   stored. The predicate is `existing > requested - cooldown` and
   `existing < requested + cooldown`. A visit exactly one cooldown away is
   allowed. The comparison uses the configured duration and the time that will
   be stored, after clamping. The nearest `visited_at` wins. An equal distance
   uses the lexicographically smaller canonical UUID. A hit returns
   `409 check_in_cooldown`, `Another check-in at this venue is too close to
   this visit.`, with `details.check_in_id` and `details.visited_at`.
   `check_in_id` is the canonical UUID string. `visited_at` uses the same
   timestamp encoding as Profile. That error rolls the transaction back,
   stores no idempotency row, and spends no write token. A `{}` command that
   failed this way can succeed with the same key once the clock has moved to
   exactly one cooldown after the conflicting visit. The same explicit
   `visited_at` conflicts again while it stays inside the window. Because no
   row was stored, that key with a different `visited_at` is a new attempt.
6. Reserve a write token only after the cooldown check passes. A refusal is
   `429 rate_limited` with `Retry-After`. Nothing is inserted.
7. Insert `visited_at` and `recorded_at` from the service clock. `recorded_at`
   is the server time. A backdated visit keeps the supplied `visited_at` and
   still records `recorded_at` as the server time. A clamped visit stores both
   at the server time. The caller commits only when the helper returns a nil
   error and a `201`.

The `201` body is the Check-in. Timestamps use the same JSON encoding as
Profile. `verification_status` uses the catalogue name. Counts, the address,
coordinates, genres, and moderator text stay off this object.

```json
{
  "data": {
    "id": "uuid",
    "venue_id": "uuid",
    "visited_at": "2026-10-24T10:00:00Z",
    "recorded_at": "2026-10-24T10:04:00Z",
    "venue": {
      "name": "The Riff",
      "city": "Berlin",
      "country_code": "DE",
      "venue_type": "bar",
      "verification_status": "verified"
    }
  }
}
```

A create succeeds only for a public Venue, so this summary always includes
`venue_type`. `verification_status` is `pending` or `verified`.

## Personal history and deletion

`GET /v1/me/check-ins` accepts only `limit` and `cursor`, through the same
`rejectedQuery` rules. `limit` defaults to 20 and accepts 1 through 100.
Outside that range, or not an integer, the field error is
`limit: must be from 1 to 100`.

The page is that caller's Check-ins, `visited_at` descending, then `id`
descending. A caller who must rename, or who is suspended, can read it. A
sign-in with no Profile gets `200` with `data: []` and `next_cursor: null`.
The list does not call `RequireContributor`, does not spend a write token, and
does not change a suspension.

The implementation fetches one extra row. When that row exists, `next_cursor`
is the position of the last returned Check-in. The cursor is opaque. Its
payload, before `base64.RawURLEncoding`, is
`check-in-v1|{visited_at}|{checkInID}`. `visited_at` is UTC `RFC3339Nano`. The
next page is `(visited_at, id) < (cursor time, cursor id)`. A cursor that does
not parse is `400` with `cursor: invalid`. Clients must not build or read the
payload.

Each item uses the same Check-in document as the `201`. The joined Venue
supplies the current summary, so a later name change appears here even though
an idempotent replay keeps the old snapshot. `pending` and `verified` include
`venue_type`. `rejected` and `delisted` omit the `venue_type` key, the
moderator reason, and the note. The omitted key is absent, not null.
`venue_id` stays on every item.

```json
{
  "name": "The Riff",
  "city": "Berlin",
  "country_code": "DE",
  "verification_status": "delisted"
}
```

When that Venue is `pending` or `verified` again, the same row includes
`venue_type` and the current `verification_status`.

`DELETE /v1/me/check-ins/{checkInID}` reads no body. A sent `Idempotency-Key`
is ignored, including a key the helper would reject. Query parameters use
`rejectedQuery`. An id that is not a canonical UUID is `400` with
`check_in_id: must be a canonical uuid`.

The delete runs in one transaction:

1. `NormalizeExpiry` for the caller.
2. Load the Check-in `for update` where the id and the caller match. No row is
   `404 check_in_not_found`, `Check-in not found.` That covers a missing id and
   another User's id, including when the caller is suspended or has no
   Profile. The other User's row stays.
3. `RequireContributor` locks the Profile `for update`. A row that is theirs
   stays in place when the Profile cannot contribute, with the same Profile
   errors as create.
4. Reserve a write token. A refusal is `429 rate_limited` with `Retry-After`,
   and the row stays.
5. Delete and commit. Success is `204` with an empty body. Only that committed
   delete keeps the token. A `404` and a Profile failure spend no token.

The caller can delete their Check-in while its Venue is `rejected` or
`delisted`. That removes the visit and its cooldown block. The Venue row stays
as it is, and a new Check-in still cannot target it while it is `rejected` or
`delisted`. A second delete of the same id is `404`.

## Public count

`venueSelect` replaces the literal `0` with:

```sql
(select count(*)::int from check_ins c where c.venue_id = v.id)
```

List and detail both use that select. A `pending` Venue reports that count.
Its `average_rating` stays null and its `review_count` stays `0`. A `verified`
Venue reports the same Check-in count beside its rating figures. The count is
every User's Check-ins for that Venue, with no time window. Zero encodes as
`0`.

Rejected and Delisted Venues stay out of the catalogue query. When a rejected
Venue returns to `pending`, or a Delisted Venue is verified again, those
retained Check-ins are in the count. Deleting a Check-in lowers the next
catalogue read. User deletion removes that User's rows, so they leave the
count with the cascade.

`check_ins_venue_idx` serves the subquery. There is no stored counter.

## Testing

HTTP tests in `internal/api` use the fake `CheckIns` adapter and cover
transport only:

- `401` on all three routes without a token, and `403 forbidden` for an
  operator;
- `POST` requires `Idempotency-Key`, rejects an unknown field, a bad content
  type, a body over 64 KiB, and any query parameter;
- `POST` maps `400` field errors, `404 venue_not_found`, `409 profile_required`,
  `403 profile_rename_required`, `403 profile_suspended` with `suspended_until`,
  `409 check_in_cooldown` with `check_in_id` and `visited_at`,
  `409 idempotency_key_reused`, and `429` with `Retry-After`;
- `POST` success is `201` and the Check-in document;
- `GET` defaults `limit` to 20, passes the cursor through, and rejects a bad
  limit, a bad cursor, and any other query parameter;
- `DELETE` is `204` with an empty body, ignores `Idempotency-Key`, and maps a
  bad id, `404 check_in_not_found`, the Profile errors above, and `429`.

`internal/checkin` tests the service against PostGIS. Local runs skip with the
existing Docker-unavailable reason. CI treats failures as blocking. The service
clock is fixed. Cover:

- a `{}` create stores both timestamps at the server time and spends one write
  token;
- a visit exactly 24 hours old is stored, and one nanosecond older is `400`
  with nothing inserted;
- a visit exactly five minutes ahead is stored as the server time, and one
  nanosecond further is `400`;
- `10:00Z` and `12:00+02:00` share one idempotency command;
- a `{}` create strictly inside another visit's cooldown is `409`, stores no
  idempotency row, and spends no token. After the clock advances by exactly
  one cooldown, that same key and `{}` command inserts. One nanosecond earlier
  it conflicts again;
- an explicit `visited_at` whose first attempt conflicts stores no row. The
  same key and same `visited_at` conflict again while that visit stays inside
  the window. That key with a different `visited_at` is a new attempt;
- after a successful create, a different command on that key is
  `409 idempotency_key_reused` until the record expires;
- two rows inside the window return the nearest `visited_at`, and an equal
  distance returns the lexicographically smaller id;
- two concurrent creates with different keys and the same visit time leave one
  row. The loser receives `409 check_in_cooldown` and spends no token;
- two concurrent creates with the same key and command return the same `201`
  and leave one row;
- a replay returns the stored `201`, spends no token, and does not insert,
  including after that Check-in was deleted and including when the Venue has
  since left the catalogue;
- a hidden Venue is `404` before the Profile check, for a suspended caller as
  well;
- a `pending` Venue accepts a Check-in from a User who is not its Submitter;
- history order is `visited_at` then `id`, including two rows with the same
  `visited_at`, and the plan fails unless it uses
  `check_ins_user_history_idx`;
- a `rejected` or `delisted` summary omits the `venue_type` key, the rejection
  reason and note, and the delisting reason and note. The same row includes
  `venue_type` when the Venue is public again;
- delete removes the caller's row and its cooldown block, a second delete is
  `404`, and another User's id is `404` while that row remains;
- a token is spent only on a committed insert or delete.

Venue catalogue tests replace the old "`check_in_count` stays 0" pin. A
`pending` Venue and a `verified` Venue report their Check-in counts. Deleting
the last Check-in returns that count to `0`. A rejected Venue stays out of the
catalogue, and correcting it back to `pending` counts the retained Check-ins.
A Delisted Venue stays out, and verifying it again counts them too.

`internal/database` checks that row-level security is on and that `anon`,
`authenticated`, and `service_role` cannot select, insert, update, or delete
`check_ins`. Readiness moves from `0010` to `0011`, including the migration
chain. The existing User-deletion test gains a Check-in that disappears with
the User while the Venue Submission remains.

`internal/config` tests that an unset or empty `CHECKIN_COOLDOWN` is six
hours, a positive Go duration is kept, and zero, a negative duration, or an
unparseable value returns `CHECKIN_COOLDOWN must be a positive duration`.

## Docs that land with the code

- `backend/openapi.yaml` documents the three routes, the Check-in document,
  both Venue summaries, and the error codes above. The cooldown description
  states the six-hour default and the stable code `check_in_cooldown`. A
  catalogue example may show a non-zero `check_in_count`. The Venue field
  types stay as they are.
- `backend/.env.example` sets `CHECKIN_COOLDOWN=6h` and comments that unsetting
  it also means six hours. `infra/render.yaml` leaves the variable unset, so
  production keeps that default.
- `backend/AGENTS.md` names `internal/checkin`, the three routes, readiness
  `0011`, and `CHECKIN_COOLDOWN`.
- `docs/architecture/backend.md` adds one paragraph under Capability modules:
  the Check-in module owns visit validation, the cooldown, authorship, the
  write transaction, personal history, and deletion. The Venue catalogue query
  reads `check_ins` for `check_in_count`. Each package keeps its own SQL.
- `DevSteps.md` marks Session 13 done only when this work lands: migration
  `0011`, the three routes, the real public count, and readiness `0011`. The
  header moves to Session 14 next.

## Out of scope

- A public recent-check-ins list, a "who's here" view, and any operator read
  of Check-ins.
- Check-in reports, photos, and device coordinates.
- Editing `visited_at` or the Venue in place.
- A daily Check-in quota, a stored counter, and a schema constraint for the
  cooldown.
- The Session 14 handoff: cross-feature sweeps, the commentary-restriction
  lift, and deployment certification.
