# Session 8: Venue discovery

**Date:** 2026-10-20
**Status:** Approved

## Goal

Add every approved discovery filter to `GET /v1/venues`. Android can build
map and list discovery with the MVP filters from this contract.

The session adds:

- `query`, repeated `genre_id`, `city`, `country_code`, `venue_type`,
  `latitude`, `longitude`, and `radius_km` on `GET /v1/venues`;
- four server-selected sort modes with pinned relevance and tie-breaking;
- cursors bound to their sort mode and filter set;
- `distance_meters` on list items when coordinates are supplied;
- migration `0006`, which adds stored search keys, `pg_trgm`, and the indexes
  that captured query plans use.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. Extend `internal/venue`,
  `internal/api`, and `internal/database`.
- Do not edit migrations `0001` through `0005`.
- Add no Go dependency. `golang.org/x/text` is already required.
- The API process never applies migrations.
- Public reads do not resolve a bearer token and do not consult
  `internal/auth` or `internal/profile`.
- `GET /v1/venues/{venueID}` and `GET /v1/genres` keep their Session 7
  contract.
- `venue_name_key` stays unchanged. Session 9 duplicate detection uses it.

## Approach

`internal/venue` owns discovery: input validation, sort-mode selection, the
cursor codec, the filter hash, and a private SQL builder. The builder appends
`and ...` clauses and `$n` arguments to a fixed base query. Values are always
bound parameters and never interpolated into SQL text.

The existing `Catalogue` seam in `internal/api` keeps one interface and two
adapters, the HTTP-test fake and the PostgreSQL implementation. Only the list
signature changes:

```go
ListVenues(context.Context, venue.Query) (venue.Page, error)
```

```text
cmd/api
  -> venue
  -> api

venue
  -> ParseQuery: domain validation into venue.Query
  -> sort mode, cursor codec, filter hash
  -> private SQL builder and PostgreSQL reads

api
  -> GET /v1/venues: parameter presence, repetition, number parsing
```

Reads stay single statements on the pool with the existing two-second
statement timeout. The session adds no transaction.

## Data model

Migration `0006_venue_discovery.sql`:

1. Installs `pg_trgm` in the `extensions` schema, and moves it there when it
   already exists elsewhere, using the same pattern `0005` uses for
   `unaccent`.
2. Creates `venue_search_key(input text) returns text`, a `stable` SQL
   function with `set search_path = pg_catalog, extensions`. Its result is
   `venue_name_key(input)` with every space removed. It revokes `EXECUTE`
   from `PUBLIC`, `anon`, `authenticated`, and `service_role`.
3. Adds `search_name`, `search_address`, and `search_city` to `venues` as
   `text`, backfills them with `venue_search_key` of `name`, `address`, and
   `city`, then sets them `not null`. The backfill runs with
   `venues_set_updated_at` disabled, so existing `updated_at` values do not
   move.
4. Creates `venues_set_search_keys()` and a `before insert or update` row
   trigger that always recomputes the three columns from the row. A caller
   cannot supply its own key values. The Session 7 `updated_at` trigger is
   unchanged.
5. Creates candidate indexes. Only indexes that a captured plan uses ship in
   the merged migration:
   - GIN `extensions.gin_trgm_ops` on `search_name`, `search_address`, and
     `search_city`, for substring text search;
   - btree on `search_city`, for the exact city filter;
   - btree on `country_code`;
   - btree on `venue_genres (genre_id)`, because the primary key leads with
     `venue_id`.

   `venue_type` gets no index; it has three values. The existing
   `venues_location_idx` GiST index serves the radius filter.

Examples of the search key:

| Input | `venue_search_key` |
| --- | --- |
| `Saint-Étienne` | `saintetienne` |
| `Saint Etienne` | `saintetienne` |
| `Iron Maiden Pub` | `ironmaidenpub` |
| `O'Connor's` | `oconnors` |
| `!!!` | empty string |

A search key contains only alphanumeric characters. It never contains `%`,
`_`, or `\`, so `LIKE` patterns built from it need no escaping.

Readiness requires migration `0006`.

## Request contract

`GET /v1/venues` accepts `limit`, `cursor`, `query`, `genre_id`, `city`,
`country_code`, `venue_type`, `latitude`, `longitude`, and `radius_km`.
`genre_id` may repeat. Every other parameter may appear at most once. Any
other parameter is `is not accepted`. An empty value is treated as absent, as
Session 7 treats `limit` and `cursor`.

Text inputs are normalized as Profile text is: `strings.TrimSpace`, then
`norm.NFC`. They must be valid UTF-8 and contain no null byte, line feed,
carriage return, or other non-whitespace control character. Lengths use
character counts after normalization.

| Parameter | Rule | Failure message |
| --- | --- | --- |
| `query` | text rules; 1–100 characters; non-empty search key | `must be 1 to 100 characters`, `must be a single line of text`, or `must contain a letter or digit` |
| `city` | text rules; 1–120 characters; non-empty search key | same pattern, with `must be 1 to 120 characters` |
| `genre_id` | each value parses with `strconv.ParseInt(value, 10, 32)` and is at least 1, matching the `serial` Genre id; duplicates are removed; at most 20 distinct values | `must be a positive integer` or `must be provided at most 20 times` |
| `country_code` | exactly a code in the `0005` allowlist, uppercase | `must be an ISO 3166-1 alpha-2 code` |
| `venue_type` | `bar`, `pub`, or `club` | `must be bar, pub, or club` |
| `latitude` | `strconv.ParseFloat` 64-bit, finite, −90 to 90 inclusive | `must be from -90 to 90` |
| `longitude` | `strconv.ParseFloat` 64-bit, finite, −180 to 180 inclusive | `must be from -180 to 180` |
| `radius_km` | `strconv.Atoi`, 1 to 100 inclusive | `must be from 1 to 100` |

`latitude`, `longitude`, and `radius_km` form one location group. When some
but not all are present, each missing one fails with
`must be provided with latitude, longitude, and radius_km`.

The HTTP adapter rejects unknown and repeated parameters, parses `limit` as
Session 7 does, and passes the limit and raw strings to `venue.ParseQuery`.
`ParseQuery` owns every rule above, including the 1–100 limit range, and
returns `*ValidationError` with all failing fields. An out-of-range limit no
longer reaches the catalogue. The `0005`
country allowlist is copied into a Go set in `internal/venue`; a test reads
the migration file and asserts the two lists are identical.

When `query` or `city` is present, the service runs one
`select venue_search_key($1), venue_search_key($2)` before the list query.
SQL stays the single definition of normalization. An empty key fails that
field with `must contain a letter or digit`.

## Matching

Every result still satisfies the Session 7 catalogue predicate: status
`pending` or `verified` and at least one `venue_genres` row. Filter groups
combine with AND.

| Filter | Predicate, with `q` the query key and `c` the city key |
| --- | --- |
| `query` | `search_name like '%' || q || '%'` or `search_address like '%' || q || '%'` or `search_city like '%' || q || '%'` |
| `city` | `search_city = c` |
| `genre_id` | `exists` a `venue_genres` row for the venue with `genre_id = any($n)`; unknown ids match nothing |
| `country_code` | `country_code = $n` |
| `venue_type` | `venue_type = $n` |
| location | `ST_DWithin(location, point, radius_km * 1000)`, inclusive at the boundary |

`point` is `ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)::geography`.
Text matching is case-, accent-, punctuation-, and whitespace-insensitive and
does not tolerate typos.

## Sorting

The service selects the mode from the filters. Clients cannot choose a sort.

| Mode | When | Order |
| --- | --- | --- |
| `name` | no `query`, no location | `venue_name_key(name) collate "C"`, `id` |
| `distance` | location, no `query` | `distance`, `id` |
| `relevance-name` | `query`, no location | `tier`, `venue_name_key(name) collate "C"`, `id` |
| `relevance-distance` | `query` and location | `tier`, `distance`, `id` |

All keys sort ascending. `distance` is the float8
`ST_Distance(location, point)` on geography. `tier` is the first matching row
of:

| Tier | Condition |
| --- | --- |
| 0 | `search_name = q` |
| 1 | `search_name like q || '%'` |
| 2 | `search_name like '%' || q || '%'` |
| 3 | `search_city = q` |
| 4 | any other `query` match, on city or address |

## Cursors

A cursor is `base64.RawURLEncoding` of `|`-joined fields:

```text
<discriminator>|<filter hash>|<sort keys...>|<venue id>
```

| Mode | Discriminator | Sort keys |
| --- | --- | --- |
| `name` | `name-v2` | name key |
| `distance` | `distance-v1` | distance |
| `relevance-name` | `relevance-name-v1` | tier, name key |
| `relevance-distance` | `relevance-distance-v1` | tier, distance |

- The name key is `venue_name_key`, which cannot contain `|`. It may be
  empty.
- The distance is `strconv.FormatFloat(d, 'g', -1, 64)` of the exact float8
  read from SQL, and parses back to the same value. The keyset predicate
  compares against the same SQL expression.
- The tier is one digit, `0` to `4`.
- The filter hash is the first 16 lowercase hex characters of SHA-256 over a
  canonical serialization of: mode, query key, city key, ascending
  deduplicated genre ids, `country_code`, `venue_type`, latitude and
  longitude as `FormatFloat(v, 'g', -1, 64)`, and `radius_km`. Absent values
  serialize as empty fields. `limit` is excluded, so a client may change page
  size during a traversal.

A non-empty cursor decodes only when the field count matches its
discriminator, every sort key parses (tier 0–4, distance finite and
non-negative, name key valid UTF-8 without a null byte), the id is canonical,
the discriminator matches the mode the current filters select, and the hash
matches the current filters. Any other cursor is `400` on `cursor` with
message `invalid`.

Session 7 `name-v1` cursors are no longer issued or accepted. No client has
shipped, and DevSteps requires clients to treat cursors as opaque.

The query reads `limit + 1` rows. On a full page, `next_cursor` encodes the
sort keys and id of the last returned venue; otherwise it is null. The
Session 7 best-effort rule still applies: with unchanged membership and sort
keys, traversal has no gap or repeat, and a concurrent change may move a
venue across the cursor.

## Response

List items keep the Session 7 Venue document. When the location group is
supplied, each item adds `distance_meters`, an integer computed in Go as
`math.Floor(distance + 0.5)` from the exact distance. Without the location
group the field is omitted, not null. `GET /v1/venues/{venueID}` never
returns it.

`venue.Venue` gains `DistanceMeters *int`. `venueView` uses
`*int` with `json:"distance_meters,omitempty"`.

## Errors

The session adds no error code. All input failures are
`400 validation_error`, message `The request is invalid.`, with the field
messages above. A repeated non-`genre_id` parameter stays
`must be provided once`. Database failures stay `500 internal_error`. The
per-IP limit stays `429 rate_limited` with `Retry-After`. No list request
returns 401 or 403.

## Tests

Unit tests in `internal/venue`:

- each validation rule at its boundaries, NFC and trimming, and the
  single-line rule;
- the incomplete location group, reporting each missing field;
- `genre_id` deduplication and the 20-value cap;
- sort-mode selection for every filter combination that changes the mode;
- a round trip for each cursor discriminator, including an empty name key
  and an exact float distance;
- cursor rejection for a hash mismatch, a mode mismatch, a tier outside 0–4,
  a negative or non-finite distance, a wrong field count, a non-canonical
  id, and a Session 7 `name-v1` cursor;
- the Go country set equals the `0005` allowlist read from the migration.

HTTP tests with a fake catalogue:

- repeated `genre_id` reaches the fake as a list; any other repeated
  parameter is `400`;
- unknown parameters and number parse failures are `400`;
- the fake receives the expected `venue.Query`;
- `distance_meters` is present when the fake returns it and absent
  otherwise;
- `next_cursor` is copied through.

PostGIS integration tests:

- the trigger computes search keys on insert and update and overwrites
  supplied values; the backfill fills existing rows;
- each filter alone and in combination; genre OR semantics; unknown genre id
  returns an empty page;
- radius boundaries: exactly at the radius, just inside, and just outside;
- distance ordering, and equal distances ordered by id;
- every relevance tier in its pinned order, with the name tie-break and the
  distance tie-break;
- folding for `query` and `city`: case, accents, punctuation, and
  whitespace, including `Saint-Étienne` and `Saint Etienne`;
- a punctuation-only `query` or `city` is `400`;
- walking a small page size in each of the four modes visits every match
  once, including tied tiers and tied distances;
- a cursor reused with a changed filter is `400`;
- on a seeded catalogue of a few thousand venues after `ANALYZE`, `EXPLAIN`
  uses each shipped index for its filter; an index no plan uses is removed
  from the migration before merge;
- readiness is `migrations_behind` when `0005` is the newest version and
  ready after `0006`.

The OpenAPI contract test also requires `query`, `genre_id`, `radius_km`,
and `distance_meters`.

## Documentation

- `backend/openapi.yaml` documents every new parameter and its validation,
  `distance_meters` and when it appears, the four orderings, and that a
  cursor is valid only with the filters that produced it.
- `backend/AGENTS.md` raises the readiness version from `0005` to `0006` and
  adds `internal/venue` to the layout as Genre, catalogue, and discovery
  behavior.
- DevSteps marks Session 8 done, moves the header to Sessions 1–8 done and
  Session 9 next, and notes that the `search_*` columns serve discovery only;
  Session 9 duplicate detection keeps using `venue_name_key`.

## Out of scope

- Typo-tolerant or ranked full-text search.
- Client-selectable sort order.
- Distance on the detail route.
- Compatibility with Session 7 `name-v1` cursors.
- Persisted aggregates, real ratings, and check-in counts.
- Venue submissions, duplicate detection, and quotas.
