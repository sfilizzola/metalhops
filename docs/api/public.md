# Public API

No sign-in, no `Authorization` header. This is what a website visitor, a map
view, or an app screen shown before sign-in can use to let someone discover
rock and metal venues. Only the per-IP rate limit applies.

## Quick reference

| Method & path | Business purpose | `operationId` |
| --- | --- | --- |
| `GET /v1/genres` | List every rock/metal Genre, for filter menus | `listGenres` |
| `GET /v1/venues` | Browse or search the Public catalogue | `listVenues` |
| `GET /v1/venues/{venueID}` | Read one venue's public profile | `getVenue` |
| `GET /v1/venues/{venueID}/reviews` | Read a venue's public reviews | `listVenueReviews` |

Two operational routes also need no auth, but exist for infrastructure, not
for the app's business — see [Operational routes](#operational-routes) below.

## `GET /v1/genres` — List genres

**Business purpose:** populates the Genre filter (black metal, classic rock,
punk, …) that Discovery and the submission form use.

**Key rules:**
- Returns every Genre, ordered deterministically by name. No pagination, no
  filters — it's a small, fixed list.

## `GET /v1/venues` — Browse or search the Public catalogue

**Business purpose:** this is Discovery — the main "find a bar" screen. The
**Public catalogue** is the same for every visitor: Verified Venues plus
pending Venue Submissions that look complete, marked as not yet verified. It
never includes rejected or Delisted Venues.

**Key rules:**
- Filter by free-text `query` (matches venue name, address, or city — accent-
  and punctuation-insensitive, no typo tolerance), `genre_id` (repeatable),
  `city`, `country_code` (ISO 3166-1 alpha-2, plus `XK` for Kosovo; `GB` not
  `UK`), `venue_type` (`bar`, `pub`, `club`), or a location (`latitude` +
  `longitude` + `radius_km`, all three or none).
- Without a query or location, results sort alphabetically by name. With a
  location, by distance. With a text query, by relevance (exact name match
  first, then prefix, then substring, then city, then address) before
  distance.
- Each result includes `average_rating`, `review_count`, and
  `check_in_count` so a list view doesn't need a second call per venue.
- Paginated: `limit` (1–100, default 20) and `cursor`. A cursor is only valid
  with the filters that produced it.

**Typical errors:** `400 validation_error` for a bad filter, an incomplete
location group (e.g. `latitude` without `radius_km`), or an invalid cursor.

## `GET /v1/venues/{venueID}` — Read one public venue

**Business purpose:** the venue detail screen — name, location, genres,
average rating, review count, and check-in count.

**Key rules:**
- Only venues in the Public catalogue are visible here. A rejected,
  Delisted, or unknown venue is `404 venue_not_found` — both look identical,
  so a visitor can't tell a hidden venue from one that never existed.

## `GET /v1/venues/{venueID}/reviews` — List a venue's reviews

**Business purpose:** the reviews tab on a venue page.

**Key rules:**
- Newest first. Editing a Review doesn't move it in the order.
- A pending (not yet verified) Venue always returns an empty page — Reviews
  only exist once a Venue is Verified.
- An author under a rename requirement or Community Suspension shows as
  "MetalHops User" with no username, for every reader. This is the one place
  anonymous visibility is explicitly undecided for a future redesign (see
  [`CONTEXT.md`](../../CONTEXT.md), "Flagged ambiguities") — today it behaves
  the same as it always has.

## Operational routes

These exist for load balancers and deploy tooling, not for the app's
business logic:

- `GET /healthz` — the process is running. Never rate limited.
- `GET /readyz` — the database is reachable and migrations are current.
  Returns `503 not_ready` otherwise. Never rate limited.
- `GET /openapi.yaml` — the raw contract.
- `GET /docs` — the Scalar browsable version of the contract (disabled on
  Render via `DOCS_UI_ENABLED=false`).
