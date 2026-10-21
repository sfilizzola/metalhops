# Session 11: Reviews and ratings

**Date:** 2026-10-21
**Status:** Approved in brainstorming; pending spec review

## Goal

Android can show, create, edit, and delete a Review. A Review is one User's
rating of 1 through 5 and optional commentary on one Verified Venue. Five is
the strongest assessment and one is the weakest. One User
has at most one Review for a Venue.

The session adds:

- migration `0009`, which creates only `reviews`;
- the `internal/review` capability;
- four routes;
- real `average_rating` and `review_count` on the existing venue list and
  detail.

**Exit condition:** Android can display, create, edit, and delete ratings and
reviews.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Do not edit migrations `0001` through `0008`. Add no Go dependency.
- `cmd/api` stays composition only.
- `PUT` and `DELETE` do not take an `Idempotency-Key`. A sent key is ignored.
- Session 12 owns commentary removal, the commentary lock, Review Reports,
  and the moderation audit. This session does not add those tables, a
  `commentary_removed` field, or `409 commentary_locked`. Delisting does not
  action Review Reports, because those reports do not exist yet.
- The venue list and detail document keeps its current fields. `check_in_count`
  stays `0`.

## Modules

`internal/review` owns validation, visibility, authorship, the write
transaction, the public list, and the caller's own Review. Its constructor
takes the pool, the `*profile.Service`, the per-User write limiter, and the
clock, matching `venue.New`. Writes call `profiles.NormalizeExpiry` before the
transaction, as Venue submissions and reports do, so an expired suspension
does not block a Review. It calls `profile.RequireContributor` and
`profile.PublicIdentity`. It does not import `internal/venue`.

HTTP adapters stay in `internal/api`. That package gains a `Reviews` interface
with two adapters: the PostgreSQL service, and a fake used by HTTP tests. The
interface lives with the caller. The review package exposes a concrete service
and no extra seam.

The venue catalogue query stays one round trip. It reads `reviews` for the two
rating columns. `internal/venue` does not import `internal/review`.

Reads and writes use a 2-second timeout, matching venue queries.

## Data (migration `0009`)

`reviews` has:

- `id uuid` primary key, default `gen_random_uuid()`;
- `venue_id uuid` not null, references `venues (id)` on delete cascade;
- `user_id uuid` not null, references `auth.users (id)` on delete cascade;
- `rating integer` not null, check 1 through 5;
- `commentary text` null, check null or 1–2,000 characters;
- `created_at timestamptz` not null;
- `updated_at timestamptz` not null;
- unique `(venue_id, user_id)`, named `reviews_venue_user_key`.

Commentary follows the venue-description text rule. The service rejects a null
byte or a non-whitespace control character before storing. It trims, normalizes
the rest to Unicode NFC, and stores null when that value is empty. The length
check runs after normalization. The check never sees `""`. An unchanged save
compares this normalized commentary, so the same words in decomposed form are
not an edit. The service writes both timestamps from its clock.
There is no `updated_at` trigger. An unchanged save issues no `UPDATE`.

User deletion and deletion of the Venue row cascade the Review away. Rejecting
or delisting a Venue leaves the row in place.

`reviews_venue_list_idx` is on `(venue_id, created_at desc, id desc)` and
serves the public list. The integration test fails if that plan does not use
the index. The unique key serves the own-Review lookup. `gen_random_uuid()`
adds no sequence revoke. The migration enables row-level security and revokes
`public`, `anon`, `authenticated`, and `service_role`. There is no policy.
Readiness requires `0009`.

## Aggregates

The venue select replaces the leading `null::float8, 0` in
`null::float8, 0, 0`. The last `0` stays `check_in_count`.

For `verification_status = 'verified'`:

- `round(avg(rating)::numeric, 1)::float8`, which is null when that Venue has
  no Reviews, because `avg` of an empty set is null and the scalar subquery
  still returns one row;
- `count(*)::int`, which is 0 when that Venue has no Reviews.

PostgreSQL rounds `numeric` half away from zero. Ratings are positive, so that
is half up. Ratings 1, 1, 1, and 2 average 1.25 and become 1.3. A pending
Venue is forced to `average_rating: null` and `review_count: 0` even if a row
were present. Rejected and Delisted Venues stay out of the catalogue query, so
their stored Reviews do not affect a public average. Reopening a Delisted
Venue makes those Reviews count again. The JSON field stays a number, so a
whole value such as 2.0 may encode as `2`.

## Routes

| Route | Caller |
| --- | --- |
| `GET /v1/venues/{venueID}/reviews` | Public. `Authorization` is not read. |
| `GET /v1/venues/{venueID}/reviews/me` | Signed-in caller, including one who must rename or is suspended. |
| `PUT /v1/venues/{venueID}/reviews/me` | Active Profile. Sets that caller's one Review. |
| `DELETE /v1/venues/{venueID}/reviews/me` | Active Profile. Removes that caller's Review. |

The three protected routes sit on the authenticated app group used by Venue
submissions. An operator receives the existing `403 forbidden`. A missing or
invalid token is `401`.

A Venue id that is not a canonical UUID is `400` with
`venue_id: must be a canonical uuid`, on every route.

Public visibility is the `GetVenue` predicate: status `pending` or `verified`,
and at least one Genre link. Anything else is hidden. That includes rejected,
Delisted, unknown, and genre-less rows.

### Public list

`GET /v1/venues/{venueID}/reviews` accepts only `limit` and `cursor`. Any other
query parameter is `400`. `limit` defaults to 20 and accepts 1 through 100.
Outside that range, or not an integer, the field error is
`limit: must be from 1 to 100`.

A verified public Venue returns Reviews ordered by `created_at` descending,
then `id` descending. The query joins `profiles` on `user_id`.
`profile.PublicIdentity` is applied in Go. A `rename_required` or suspended
author is `display_name: "MetalHops User"` and `username: null`, for every
viewer, including that author. The page envelope matches the venue list:
`data` and `meta.next_cursor`. The implementation fetches one extra row. When
that row exists, `next_cursor` is the position of the last returned Review.

A pending public Venue returns `200` with `data: []` and `next_cursor: null`,
even if a Review row were present, so the page matches the zero review count.
A hidden Venue returns `404 venue_not_found` with `Venue not found.`

The cursor is opaque. Its payload, before `base64.RawURLEncoding`, is
`review-v1|{venueID}|{created_at}|{reviewID}`. `created_at` is UTC RFC3339Nano.
The next page is `(created_at, id) < (cursor time, cursor id)` for the Venue
in the path. A cursor is `400` with `cursor: invalid` when the payload does
not parse, or when its Venue id is not the path Venue id. Clients must not
build or read the payload.

Editing does not change `created_at`, so a Review stays in place.

A public Review is:

```json
{
  "id": "uuid",
  "rating": 4,
  "commentary": "loud and cold",
  "author": {"username": "metal_fan", "display_name": "Metal Fan"},
  "created_at": "2026-10-21T09:00:00Z",
  "updated_at": "2026-10-21T09:00:00Z"
}
```

`commentary` is a string or null. There is no User id and no
`commentary_removed` field. Timestamps use the same JSON encoding as Profile.

### Own Review

`GET /v1/venues/{venueID}/reviews/me` returns the caller's Review on a verified
public Venue. The document adds `user_id` and the stored Username and display
name. It does not use `PublicIdentity`. A caller who must rename, or who is
suspended, can read it. A sign-in with no Profile has no Review.

`404 review_not_found` with `Review not found.` covers every miss on this
route: no Review, a pending Venue, and every hidden Venue.

`PUT` replaces the caller's whole Review. The JSON object has `rating` and
optional `commentary`. Unknown fields are rejected. The body limit, content
type, and malformed-body errors match the existing JSON rules.

`rating` must be a JSON integer from 1 through 5. A fraction, including `4.0`,
an exponent, a string, null, or omission is
`rating: must be between 1 and 5`. `commentary`, when present, is a string or
null. Any other JSON type, a null byte, or a non-whitespace control character
is `commentary: must be text`. Line feeds are allowed. The service trims and
normalizes to Unicode NFC. Omitted, null, or whitespace-only commentary is
stored as null. Longer than 2,000 characters after that is
`commentary: must be at most 2000 characters`. Several field errors return
together.

The first insert returns `201` and the own-Review document. A later change
returns `200` and that document. An unchanged rating and normalized commentary returns
`200` with the stored Review, leaves `updated_at` at the earlier timestamp,
and spends no write token. The clock moving between the two calls is how the
test shows the second call did not write.

`DELETE` reads no body. Success is `204` with an empty body. A second delete,
and a delete where that caller has no Review on a verified public Venue, is
`404 review_not_found`.

### Write outcomes

Writes resolve the Venue before the Profile. A hidden Venue is `404
venue_not_found` for a suspended caller as well, and a stored Review stays
unchanged. A pending public Venue is `409 venue_not_verified` with
`Reviews open after this venue is verified.` The row, if one were present,
stays unchanged.

The Submitter may Review that Venue once it is verified. The write does not
look up `submitted_by`.

After the Venue check, an active Profile is required:

- `409 profile_required`, `Create a profile first.`
- `403 profile_rename_required`, `Rename this profile before continuing.`
- `403 profile_suspended`, `This profile is suspended.`, with
  `suspended_until` when the Profile has one.

`PUT` and `DELETE` call `profiles.NormalizeExpiry` and then
`profile.RequireContributor`, the existing pair used by Venue writes. This
session does not add a new suspension-expiry path.

| Venue | GET list | GET me | PUT | DELETE |
| --- | --- | --- | --- | --- |
| Verified, caller has a Review | `200` page | `200` own document | `200` replace, or `200` no-op | `204` |
| Verified, caller has no Review | `200` page | `404 review_not_found` | `201` | `404 review_not_found` |
| Pending, public | `200` empty page | `404 review_not_found` | `409 venue_not_verified` | `409 venue_not_verified` |
| Hidden | `404 venue_not_found` | `404 review_not_found` | `404 venue_not_found` | `404 venue_not_found` |

## Transaction

`PUT` and `DELETE` run in one transaction. A hidden or pending Venue returns
from step 1, before the Profile lock and before a write token is reserved.

0. Before the transaction, `profiles.NormalizeExpiry` for the caller.
1. Load the Venue `for share` and apply the outcome table above. A concurrent
   moderation action either commits first or waits for this write.
2. `RequireContributor` locks the Profile row `for update`. That lock
   serializes every write by one User, so no advisory lock is needed.
3. Read the caller's Review. For `PUT`, an equal rating and commentary is a
   no-op: commit, spend no write token, return the stored Review, and stop.
   For `DELETE`, a missing row on a verified public Venue is
   `404 review_not_found` and stops before a token is reserved.
4. Reserve a write token. A refusal is `429 rate_limited` with `Retry-After`,
   the transaction rolls back, and the row stays unchanged.
5. Insert, update, or delete. The unique key remains the backstop.
6. Commit. Only a committed insert, update, or delete keeps the token. A
   failure cancels the reservation.

Two concurrent first saves for the same User and Venue leave one row. Both
calls return `2xx`. The later committed body wins, and that call is `200` when
it changes the row the first call inserted. `created_at` stays at the insert.
A validation failure, a `404`, a `409`, and a no-op spend no token.

The public per-IP bucket still applies to all four routes, before the handler.

## Testing

HTTP tests in `internal/api` use the fake `Reviews` adapter and cover transport
only:

- the public list ignores `Authorization`, defaults `limit` to 20, and passes
  the cursor through;
- `401` on the three protected routes without a token, and `403 forbidden` for
  an operator;
- `404 venue_not_found`, `404 review_not_found`, `409 venue_not_verified`,
  `409 profile_required`, `403 profile_rename_required`, `403 profile_suspended`
  with `suspended_until`, `400` field errors, and `429` with `Retry-After`;
- `PUT` is `201` on create and `200` on replace;
- `DELETE` is `204` with an empty body;
- unknown fields, a bad content type, and a body over 64 KiB use the existing
  errors.

`internal/review` tests the service against PostGIS. Local runs skip with the
existing Docker-unavailable reason. CI treats failures as blocking. Cover:

- create, replace, and the unchanged no-op, including a clock that moves while
  `updated_at` stays put and the token count stays 60;
- a token spent only on a committed insert, update, or delete;
- commentary trim, NFC, null, rejected control characters, 2,000 characters accepted, 2,001 rejected, and rating
  bounds;
- two concurrent first saves leaving one row;
- a pending write returning `409` and inserting nothing;
- a Review seeded in SQL, then a Delisted or rejected write returning `404`
  and leaving that Review in place;
- list order by `created_at` then `id`, including two Reviews with the same
  `created_at`;
- a cursor from another Venue rejected;
- a suspended author's public row showing `MetalHops User` and `username: null`,
  while `GET .../me` shows the stored name;
- delete, a second delete, and deletion of the auth user removing the Review.

Venue catalogue tests pin the subquery: no Reviews is null and `0`; ratings
1, 1, 1, and 2 become `1.3` with count 4; a pending Venue stays null and `0`;
deleting the last Review returns to null and `0`; a Delisted Venue stays out
of the catalogue and reopening counts its Reviews again; `check_in_count`
stays `0`. The list-plan test fails unless `reviews_venue_list_idx` is used.

`internal/database` gains the privilege checks used for `venue_reports`:
row-level security is on, and `anon`, `authenticated`, and `service_role`
cannot select, insert, update, or delete `reviews`. Constraint checks reject
rating 0 and 6, `''` commentary, commentary of 2,001 characters, and a second
row for the same User and Venue. Readiness moves from `0008` to `0009`,
including the migration chain in the database test.

## Docs that land with the code

- `backend/openapi.yaml` documents the four routes, both Review documents, and
  the error codes above. A verified venue example may show a non-null
  `average_rating`. The field types on the Venue document stay as they are.
- `backend/AGENTS.md` names `internal/review`, the four routes, and readiness
  `0009`.
- `docs/architecture/backend.md` gains one paragraph under Capability modules:
  the Venue catalogue query reads `reviews` for `average_rating` and
  `review_count` so list and detail stay one round trip; `internal/venue` does
  not import `internal/review`; the Review module owns Review validation,
  authorship, the write transaction, the public list, and the caller's own
  Review.
- `DevSteps.md` is marked done for Session 11 only when this work lands.
