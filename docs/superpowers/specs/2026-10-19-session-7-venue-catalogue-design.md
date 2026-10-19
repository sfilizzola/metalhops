# Session 7: Venue catalogue

**Date:** 2026-10-19
**Status:** Approved

## Goal

Let any caller list Genres and browse the public Venue catalogue, including
one Venue at a time. Android can render a paginated catalogue and a venue
detail screen from this contract.

The session adds:

- `GET /v1/genres`, `GET /v1/venues`, and `GET /v1/venues/{venueID}`;
- one public Venue document for the list and the detail route;
- cursor pagination ordered by normalized Venue name, then Venue id;
- literal rating and check-in aggregates;
- migration `0005`, which makes the catalogue columns required and adds
  `updated_at` and `venue_name_key`.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. Add `internal/venue`. Extend
  `internal/api` and `internal/database`.
- Do not edit migrations `0001` through `0004`.
- Add no dependency.
- The API process never applies migrations.
- Public reads do not resolve a bearer token and do not consult
  `internal/auth` or `internal/profile`.
- Discovery filters, Venue submissions, delisting, rejection feedback, and
  `status_changed_at` stay in later sessions.
- Review and Check-in tables stay in later sessions. This session does not
  create them.

## Approach

`internal/venue` owns catalogue membership, the name sort, and the three reads.
`internal/api` defines a `Catalogue` interface with `ListGenres`,
`ListVenues`, and `GetVenue`, and maps domain results onto HTTP. HTTP tests
use a fake. The production adapter is the PostgreSQL implementation inside
`internal/venue`. That is one seam and two adapters.

```text
cmd/api
  -> venue
  -> api

venue
  -> genre list, catalogue page, venue detail
  -> membership and venue_name_key ordering
  -> private PostgreSQL queries

api
  -> GET /v1/genres
  -> GET /v1/venues
  -> GET /v1/venues/{venueID}
```

The existing per-IP limit already wraps every product route. These three
routes are registered without the app or operator gate. Readiness requires
migration `0005`.

## Data model

Migration `0005_venue_catalogue.sql` changes `venues` in place.

- Rename `country` to `country_code`.
- `address`, `city`, and `country_code` are `not null`.
- Add `updated_at timestamptz not null default now()`. Existing rows receive
  `created_at` before the trigger exists, so the backfill is not overwritten
  by the current time.
- A `before update` trigger sets `updated_at` to `now()` on every venue-row
  update, including an update that tries to supply its own timestamp. A
  change that only rewrites `venue_genres` does not fire this trigger. The
  submission session sets `updated_at` in that transaction.

Column checks:

- `name` is 1–150 characters, equal to `btrim(name)`, and contains no `\n`
  or `\r`.
- `address` is 1–500 characters under those same trim and line-break rules.
- `city` is 1–120 characters under those same rules.
- `description` is null, or 1–2,000 characters and equal to
  `btrim(description)`. Line breaks are allowed.
- `country_code` is an explicit `in (...)` allowlist of officially assigned
  ISO 3166-1 alpha-2 codes, stored uppercase, plus the product exception
  `XK` for Kosovo. A two-letter pattern is not enough. Other exceptionally
  reserved, transitionally reserved, indeterminately reserved, and
  user-assigned codes are rejected. The United Kingdom is `GB`; `UK` is
  rejected.
- `venue_type` stays `bar`, `pub`, or `club`.
- `verification_status` stays `pending`, `verified`, or `rejected`.

Lengths use `char_length`. Trim uses PostgreSQL's default `btrim`, which
strips the space character. The migration aborts when a stored venue violates
a new check. It does not invent an address, city, or country code.

Before applying `0005`, the operator runs the read-only preflight from the
implementation plan. It reports each offending Venue id and the failed
requirements for null or invalid catalogue fields. The preflight changes no
row. Offending data is corrected deliberately before another migration
attempt; the migration never repairs it automatically.

A venue with no genre link remains stored. There is no genre-count
constraint, so a later submission can insert the venue and its genre links in
one transaction. The catalogue query is what requires a genre.

`0005` also creates the `extensions` schema when needed, installs `unaccent`
there, and creates one pure SQL function,
`venue_name_key(text) returns text`, marked `stable`. The function
schema-qualifies `extensions.unaccent`; it does not depend on the caller's
search path. Revoke `EXECUTE` on the name-key function from `PUBLIC`, `anon`,
`authenticated`, and `service_role`. The owner role keeps it. The function
reads no table. Its result, in order, is:

1. `unaccent` of the input;
2. `lower` of that text;
3. removal of every character that is neither alphanumeric nor whitespace,
   including punctuation and symbols, so `O'Connor` and `OConnor` share a
   key, `black-metal` and `blackmetal` share a key, and `C++` and `C` share a
   key;
4. collapse of each PostgreSQL `\s` run to one space, then `btrim`.

Words such as `bar`, `pub`, `club`, and `the` stay in the key. A
punctuation-only name is allowed by the name check and produces an empty
key. This session adds no catalogue index. The existing location index stays.

The successful owner inserts in `backend/internal/profile/delete_test.go` and
`backend/internal/database/access_test.go` gain address, city, and
`country_code` so user deletion still keeps the venue and clears
`submitted_by`. Those fixture venues have no genre, so they stay out of the
catalogue. The Data API statements that must fail with SQLSTATE `42501` stay
incomplete. Privilege denial remains the failure those statements assert.

## Catalogue rules

A venue is public when `verification_status` is `pending` or `verified` and
at least one `venue_genres` row exists. The list and the detail route use
that predicate. Each public venue appears once. The page limit counts venues,
so a venue with several genres is not repeated and does not consume extra
slots.

Reads use the same two-second statement timeout as Profile reads.

`venue_name_key(name) collate "C"` ascending, then the uuid value ascending,
is the only order. The explicit collation keeps ordering identical across
environments. The keyset position is that pair. A cursor is
`base64.RawURLEncoding("name-v1|" + name_key + "|" + id)`. `name-v1` is the
cursor version and sort discriminator. The delimiters are safe because the
key has had punctuation and symbols, including `|`, removed. An empty key
encodes as `name-v1||` followed by the id. A non-empty cursor decodes only
when it contains exactly those two delimiters, has the `name-v1` discriminator,
and has a canonical lowercase UUID. A decoded cursor is a position in that
order.
The venue it names may already have been renamed or deleted. The API does
not keep a record of issued cursors, and the cursor is not signed. Clients
are still required to treat it as opaque.

The catalogue treats an empty cursor as the first page and decodes any other
cursor. The next cursor is the key and id of the last venue on a full page.
The query reads one extra row to decide whether another page exists. The last
page uses `next_cursor` null. Latitude is `ST_Y` and longitude is `ST_X` of
`location::geometry`.

Pagination is best-effort while the catalogue changes. With unchanged
membership and sort keys, keyset traversal has no gap or repeat. A concurrent
rename, addition, removal, or status or Genre change may move a Venue across
the cursor or into or out of the catalogue, so a traversal spanning that
change may skip or repeat it. Session 7 does not add snapshot state.

Aggregates are SQL literals on every public venue, pending or verified:
`average_rating` is null, `review_count` is 0, and `check_in_count` is 0.
This session does not implement half-up rounding. A later review session
replaces the rating and review-count expressions. A later check-in session
replaces the check-in count.

## HTTP contract

All three routes are public. An `Authorization` header is ignored. The
per-IP limit still applies.

`GET /v1/genres` returns every genre as `{id, name}`, ordered by stored name
using the explicit `C` collation, then genre id. The body is
`{"data":[...]}` with no `meta`. It accepts no query parameter.

`GET /v1/venues` accepts only `limit` and `cursor`, each at most once. A
missing or empty `limit` means 20. A present `limit` parses with
`strconv.Atoi` and must be from 1 to 100. A missing or empty `cursor` is the
first page. The body is the collection envelope, and each item is the Venue
document below.

`GET /v1/venues/{venueID}` accepts no query parameter. The body is
`{"data":{...}}` with the same Venue document. `venueID` is validated by the
venue module. A non-canonical id is a validation error. Canonical path IDs are
lowercase UUID strings; OpenAPI adds the lowercase pattern in addition to
`format: uuid`. Any canonical id that fails the catalogue predicate,
including a rejected venue, a venue with no genre, and an unknown id, is
`404 venue_not_found` with message `Venue not found.`

The Venue document contains these fields, in this meaning:

| Field | JSON |
| --- | --- |
| `id` | canonical UUID string |
| `name` | string |
| `description` | string or null |
| `latitude` | number, WGS84 Y of the stored point |
| `longitude` | number, WGS84 X of the stored point |
| `address` | string |
| `city` | string |
| `country_code` | string |
| `venue_type` | `bar`, `pub`, or `club` |
| `verification_status` | `pending` or `verified` |
| `genres` | array of `{id, name}`, ordered by genre name under `C` collation, then genre id |
| `average_rating` | null, and the field is present |
| `review_count` | 0 |
| `check_in_count` | 0 |

The document leaves out the submitter, timestamps, and moderation feedback.
`internal/api` owns the JSON structs. Domain values stay free of response tags.

## Errors

Validation uses the existing message `The request is invalid.` Unexpected
failures use `internal_error` and `The server could not complete the request.`
Logs keep the wrapped cause and request ID. Responses omit SQL and stack
details.

| Failure | Result |
| --- | --- |
| `limit` is not an integer, or is outside 1–100 | `400 validation_error` on `limit`, message `must be from 1 to 100` |
| `limit` or `cursor` is repeated | `400 validation_error` on that parameter, message `must be provided once` |
| Any other query parameter on any of the three routes | `400 validation_error` on that parameter name, message `is not accepted` |
| Cursor does not decode | `400 validation_error` on `cursor`, message `invalid` |
| `venueID` is not a canonical UUID | `400 validation_error` on `venue_id`, message `must be a canonical uuid` |
| Unknown, rejected, or genre-less venue | `404 venue_not_found` |
| Database failure | `500 internal_error` |
| Per-IP bucket exhausted | `429 rate_limited` with `Retry-After` |

The HTTP adapter applies the default limit, passes an empty cursor through,
and rejects a non-integer `limit`, a repeated parameter, and an unknown
parameter before it calls the catalogue. The catalogue treats an empty cursor
as the first page, rejects a limit outside 1–100, and rejects a non-empty
cursor that does not decode. An empty catalogue is
`200` with `"data": []` and `next_cursor` null. No route on this slice
returns 401 or 403.

## Tests

Migration and catalogue tests use the PostGIS harness:

- Readiness is `migrations_behind` when `0004` is the newest recorded
  version, and ready after `0005` is recorded.
- A venue row rejects a missing address, a lowercase or unlisted country
  code, `UK`, an untrimmed name, and a line break in the name. `XK` is
  accepted. A description may contain a line break. `updated_at` changes
  when the row is updated.
- The list returns a verified venue and a pending venue that each have a
  genre, once each. It omits a rejected venue and a venue with no genre.
  Detail uses that same cut.
- `Bar!` and `bar` share a key. `The Bar` keeps `the`. Accent folding, extra
  spaces, international alphanumeric text, punctuation, and symbols fold as
  `venue_name_key` defines. Equal keys sort by venue id under explicit `C`
  collation. Walking a small page size visits every match once when the
  catalogue is unchanged, including an empty-key cursor and a cursor whose
  venue was removed after it was issued.
- Every returned venue has a null average rating, review count 0, and
  check-in count 0.
- The updated deletion fixtures still retain the venue and clear
  `submitted_by`.

HTTP tests use a fake catalogue and send no successful request that depends
on a bearer token. One request includes an `Authorization` header and still
receives the public response. The other cases are the default limit of 20, a
non-integer limit, both bounds outside 1–100, a repeated parameter, an unknown
query parameter, a list cursor the fake reports invalid, a non-UUID id,
`venue_not_found`, empty collection envelopes, the genre list without `meta`,
and a venue page whose `next_cursor` is copied through.

The OpenAPI contract test requires `/v1/genres`, `/v1/venues`,
`/v1/venues/{venueID}`, `listGenres`, `listVenues`, `getVenue`,
`venue_not_found`, `verification_status`, `country_code`, and
`average_rating`. The three operations declare no security requirement.

## Documentation

`backend/openapi.yaml` documents the three routes, the Venue document, the
genre document, and the error codes above. `/docs` serves that contract
without a new dependency.

The implementation raises the readiness version in
`backend/internal/database` and `backend/AGENTS.md` to `0005`, and describes
`internal/venue` as Genre and catalogue behavior.

The `venues` row trigger owns row-field changes. A future Venue transaction
that changes Genre links explicitly touches the Venue row in that same
transaction, so the public content change advances `updated_at` without a
cross-table trigger.

It marks Session 7 done in DevSteps and moves the header to Sessions 1–7
done, Session 8 next. The Venue-fields paragraph names migration `0005` for
the required columns, `country_code`, and `updated_at`. `status_changed_at`
stays with the moderation lifecycle migration, which updates both timestamps
on a status transition. Session 9 no longer repeats that column work.
Duplicate detection there calls `venue_name_key`.

## Out of scope

- `query`, `genre_id`, `city`, `country_code`, `venue_type`, `latitude`,
  `longitude`, and `radius_km` as catalogue filters.
- A catalogue sort index.
- `POST /v1/venues` and the caller's submission routes.
- `delisted`, rejection fields, delisting fields, and `status_changed_at`.
- Review and Check-in tables, real aggregate SQL, and half-up rounding.
- A signed cursor or a stored set of issued cursors.
