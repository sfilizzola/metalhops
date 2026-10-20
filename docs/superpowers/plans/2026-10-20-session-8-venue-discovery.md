# Session 8 Venue Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add every approved discovery filter, four pinned sort orders, filter-bound cursors, and `distance_meters` to `GET /v1/venues`.

**Architecture:** `internal/venue` owns discovery. `ParseQuery` validates raw parameters into `venue.Query`, `Service.ListVenues` folds text through SQL, selects one of four sort modes, decodes a cursor bound to that mode and filter hash, and runs one statement built by a private parameterized SQL builder. `internal/api` keeps the one `Catalogue` seam; only the list signature changes. Migration `0006` adds trigger-maintained search keys, `pg_trgm`, and plan-justified indexes.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL 16, PostGIS 3.5, `unaccent`, `pg_trgm`, `golang.org/x/text/unicode/norm`, OpenAPI 3.1, `httptest`, and `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-20-session-8-venue-discovery-design.md`

## Global Constraints

- Follow DevSteps and `docs/architecture/backend.md`.
- Keep `cmd/api` as composition only. Extend `internal/venue`, `internal/api`, and `internal/database`.
- Do not edit migrations `0001` through `0005`. Add `infra/supabase/migrations/0006_venue_discovery.sql`.
- Add no Go dependency. `golang.org/x/text` is already in `go.mod`.
- The API process never applies migrations. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Public reads do not resolve a bearer token and do not consult `internal/auth` or `internal/profile`.
- `GET /v1/venues/{venueID}` and `GET /v1/genres` keep their Session 7 contract. Detail never returns `distance_meters`.
- `venue_name_key` stays unchanged. The `search_*` columns serve discovery only.
- Values are always bound SQL parameters. Never interpolate a request value into SQL text.
- Reads stay single statements on the pool under one two-second context. No transaction.
- Session 7 `name-v1` cursors are no longer issued or accepted.
- No new error code. Every input failure is `400 validation_error`, message `The request is invalid.`
- Work on `feature/session-8-venue-discovery`. Merge into `dev` by pull request.
- Commands run from `backend/` with plain `go` and `git` unless a step says otherwise.

## Review Focus

1. **Backfill moves `updated_at`:** `0006` backfills search keys with an `UPDATE`, which fires the Session 7 `updated_at` trigger and would stamp every existing Venue with the migration time. Session 9 orders a Submitter's list by `updated_at`. Task 1 disables that trigger around the backfill and pins it in `TestMigrationVersions` with a Venue stored at `2001-01-01`.
2. **Genre id beyond int4:** `genre_id=3000000000` passes `strconv.Atoi` but cannot encode into `int[]`, so pgx fails and the request becomes `500`. Task 2 parses with `strconv.ParseInt(value, 10, 32)` and pins `2147483648` as `must be a positive integer`.
3. **Whitespace-only text:** `query=%20%20` is not empty, so it is not "absent", but it trims to nothing. It must be `must be 1 to 100 characters`, not an unfiltered page. Task 2 pins it.
4. **Punctuation in LIKE:** the search key holds only alphanumerics, so `%` and `_` from a request can never reach a `LIKE` pattern as wildcards. Task 1 pins `venue_search_key('100%_off')` as `100off`, and Task 4 pins a punctuation-only query as `400`.
5. **Distance equal to the radius:** `ST_DWithin` is inclusive. A venue measured at exactly the radius must be returned; one just outside must not. Task 4 `TestRadiusBoundary` asserts inclusion matches `ST_Distance <= radius` for a venue projected to 1,000 m.
6. **Equal distances and equal tiers:** two venues on the same point, or two names with the same tier, must still page without a gap or repeat. Task 4 `TestWalkEveryMode` walks each mode at page size 2 across full ties.
7. **Cursor replayed with other filters:** a client that changes `country_code` but resends the cursor must get `400 cursor invalid`, while changing only `limit` must keep working. Task 4 `TestCursorBoundToFilters` pins both.

---

## File Structure

New files:

```text
infra/supabase/migrations/0006_venue_discovery.sql
backend/internal/database/discovery_test.go
backend/internal/venue/query.go              # RawQuery, Query, ParseQuery, sort mode
backend/internal/venue/query_test.go
backend/internal/venue/countries.go          # Go copy of the 0005 allowlist
backend/internal/venue/countries_test.go
backend/internal/venue/cursor.go             # position, cursor codec, filter hash
backend/internal/venue/cursor_test.go
backend/internal/venue/discovery_test.go     # PostGIS filter, order, cursor tests
backend/internal/venue/index_test.go         # EXPLAIN plan tests
```

Focused modifications:

```text
backend/internal/database/pool.go
backend/internal/database/postgres_test.go
backend/internal/database/access_test.go
backend/internal/venue/venue.go
backend/internal/venue/venue_test.go
backend/internal/venue/postgres.go
backend/internal/venue/postgres_test.go
backend/internal/api/venue.go
backend/internal/api/venue_test.go
backend/openapi.yaml
backend/contract_test.go
backend/AGENTS.md
DevSteps.md
```

`pgtest.MigratedURL` applies every SQL file in order, so `0006` loads in every integration test without a harness change. `cmd/api/main.go` does not change: `*venue.Service` still satisfies `api.Catalogue` after Task 4.

---

### Task 1: Discovery Migration and Readiness

**Files:**
- Create: `infra/supabase/migrations/0006_venue_discovery.sql`
- Create: `backend/internal/database/discovery_test.go`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go` (the `0005` block at the end of `TestMigrationVersions`)
- Modify: `backend/internal/database/access_test.go:33`

**Interfaces:**
- Consumes: migrations `0001`–`0005`, `venue_name_key(text)`, trigger `venues_set_updated_at`, `database.Pool.Check`, `pgtest.MigratedURL`, test helpers `execSQL` and `readMigration`.
- Produces: `venue_search_key(text) returns text`; `venues.search_name`, `venues.search_address`, `venues.search_city` (`text not null`, trigger-maintained); trigger `venues_set_search_keys`; indexes `venues_search_name_trgm_idx`, `venues_search_address_trgm_idx`, `venues_search_city_trgm_idx`, `venues_search_city_idx`, `venues_country_code_idx`, `venue_genres_genre_id_idx`; readiness version `0006`.

- [ ] **Step 1: Write the failing readiness and backfill test**

In `backend/internal/database/postgres_test.go`, replace the final block of `TestMigrationVersions`:

```go
	execSQL(t, pool, readMigration(t, "0005_venue_catalogue.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0005')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0005 ready %v reason %s", ready, reason)
	}
}
```

with:

```go
	execSQL(t, pool, readMigration(t, "0005_venue_catalogue.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0005')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0005 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		insert into venues (name, location, address, city, country_code, updated_at)
		values ('Old Bar', ST_MakePoint(0, 0)::geography, '1 Main Street', 'Oslo', 'NO', '2001-01-01T00:00:00Z')`)
	execSQL(t, pool, readMigration(t, "0006_venue_discovery.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0006')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0006 ready %v reason %s", ready, reason)
	}
	var searchName, searchCity string
	var updated time.Time
	if err := pool.pool.QueryRow(ctx, `
		select search_name, search_city, updated_at from venues where name = 'Old Bar'`).Scan(&searchName, &searchCity, &updated); err != nil {
		t.Fatal(err)
	}
	if searchName != "oldbar" || searchCity != "oslo" {
		t.Fatalf("backfill %q %q", searchName, searchCity)
	}
	if !updated.Equal(time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("backfill moved updated_at to %s", updated)
	}
}
```

Add `"time"` to the import block of `postgres_test.go` if it is not already imported.

- [ ] **Step 2: Write the failing search-key, trigger, and Data API tests**

In `backend/internal/database/access_test.go`, directly after the line `` `select venue_name_key('Bar!')`, `` add:

```go
		`select venue_search_key('Bar!')`,
```

Create `backend/internal/database/discovery_test.go`:

```go
package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestVenueSearchKey(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cases := map[string]string{
		"Saint-Étienne":   "saintetienne",
		"Saint Etienne":   "saintetienne",
		"Iron Maiden Pub": "ironmaidenpub",
		"O'Connor's":      "oconnors",
		"Bar  One":        "barone",
		"Бар №1":          "барno1",
		"100%_off":        "100off",
		"!!!":             "",
	}
	for input, want := range cases {
		var got string
		if err := pool.QueryRow(ctx, `select venue_search_key($1)`, input).Scan(&got); err != nil || got != want {
			t.Fatalf("%q got %q want %q err %v", input, got, want, err)
		}
	}
}

func TestSearchKeyTriggerOwnsColumns(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var id, name, address, city string
	if err := pool.QueryRow(ctx, `
		insert into venues (name, location, address, city, country_code, search_name, search_address, search_city)
		values ('Iron-Maiden Pub', ST_MakePoint(4.39, 45.43)::geography, '12 Rue du Clocher', 'Saint-Étienne', 'FR', 'forged', 'forged', 'forged')
		returning id::text, search_name, search_address, search_city`).Scan(&id, &name, &address, &city); err != nil {
		t.Fatal(err)
	}
	if name != "ironmaidenpub" || address != "12rueduclocher" || city != "saintetienne" {
		t.Fatalf("insert keys %q %q %q", name, address, city)
	}
	if err := pool.QueryRow(ctx, `
		update venues set name = 'Black Hole' where id = $1 returning search_name`, id).Scan(&name); err != nil || name != "blackhole" {
		t.Fatalf("rename key %q err %v", name, err)
	}
	if err := pool.QueryRow(ctx, `
		update venues set search_city = 'forged' where id = $1 returning search_city`, id).Scan(&city); err != nil || city != "saintetienne" {
		t.Fatalf("forged city key %q err %v", city, err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/database/ -run 'TestMigrationVersions|TestVenueSearchKey|TestSearchKeyTriggerOwnsColumns|TestDataAPIClosed' -v`
Expected: FAIL. `TestMigrationVersions` fails reading `0006_venue_discovery.sql` (or reports `0005 ready true reason ok`). The search-key tests fail with `function venue_search_key(unknown) does not exist`. `TestDataAPIClosed` fails because the new statement errors with `42883`, not `42501`.

- [ ] **Step 4: Write the migration**

Create `infra/supabase/migrations/0006_venue_discovery.sql`:

```sql
create schema if not exists extensions;
create extension if not exists pg_trgm with schema extensions;

do $$
begin
  if (
    select n.nspname <> 'extensions'
    from pg_extension e
    join pg_namespace n on n.oid = e.extnamespace
    where e.extname = 'pg_trgm'
  ) then
    alter extension pg_trgm set schema extensions;
  end if;
end
$$;

create function venue_search_key(input text) returns text
language sql
stable
set search_path = pg_catalog, public
as $$
  select replace(public.venue_name_key(input), ' ', '')
$$;

revoke execute on function venue_search_key(text) from public, anon, authenticated, service_role;

alter table venues
  add column search_name text,
  add column search_address text,
  add column search_city text;

-- The backfill is not a Venue change, so it must not move updated_at.
alter table venues disable trigger venues_set_updated_at;
update venues set
  search_name = venue_search_key(name),
  search_address = venue_search_key(address),
  search_city = venue_search_key(city);
alter table venues enable trigger venues_set_updated_at;

alter table venues
  alter column search_name set not null,
  alter column search_address set not null,
  alter column search_city set not null;

create function venues_set_search_keys() returns trigger
language plpgsql
set search_path = pg_catalog, public
as $$
begin
  new.search_name = public.venue_search_key(new.name);
  new.search_address = public.venue_search_key(new.address);
  new.search_city = public.venue_search_key(new.city);
  return new;
end
$$;

create trigger venues_set_search_keys
before insert or update on venues
for each row
execute function venues_set_search_keys();

create index venues_search_name_trgm_idx on venues using gin (search_name extensions.gin_trgm_ops);
create index venues_search_address_trgm_idx on venues using gin (search_address extensions.gin_trgm_ops);
create index venues_search_city_trgm_idx on venues using gin (search_city extensions.gin_trgm_ops);
create index venues_search_city_idx on venues (search_city);
create index venues_country_code_idx on venues (country_code);
create index venue_genres_genre_id_idx on venue_genres (genre_id);
```

`venue_name_key` collapses every whitespace run to one space and trims, so removing `' '` removes all whitespace. Task 5 may delete index lines that no captured plan uses.

- [ ] **Step 5: Raise the readiness version**

In `backend/internal/database/pool.go`, change:

```go
const requiredMigrationVersion = "0005"
```

to:

```go
const requiredMigrationVersion = "0006"
```

- [ ] **Step 6: Run the database package**

Run: `go test ./internal/database/ -v`
Expected: PASS, including `TestMigrationVersions`, `TestVenueSearchKey`, `TestSearchKeyTriggerOwnsColumns`, `TestDataAPIClosed`, `TestDataAPIStillDenied`, `TestVenueCatalogueConstraints`, and `TestUpdatedAtTriggerOverwritesSuppliedTime`. If `TestDataAPIClosed` reports `23502` for the incomplete venue insert, stop and report it; do not complete that statement.

- [ ] **Step 7: Run the whole module**

Run: `go test ./...`
Expected: PASS. The profile deletion and access fixtures insert venues without search columns; the trigger fills them.

- [ ] **Step 8: Commit**

```bash
git add ../infra/supabase/migrations/0006_venue_discovery.sql internal/database/pool.go internal/database/postgres_test.go internal/database/access_test.go internal/database/discovery_test.go
git commit -m "feat: add venue discovery search keys"
```

---

### Task 2: Discovery Query Parsing

**Files:**
- Create: `backend/internal/venue/query.go`
- Create: `backend/internal/venue/query_test.go`
- Create: `backend/internal/venue/countries.go`
- Create: `backend/internal/venue/countries_test.go`

**Interfaces:**
- Consumes: `ValidationError`, `ValidateLimit(int) error` from `venue.go`.
- Produces:
  - `type RawQuery struct { Limit int; Cursor, Query string; GenreIDs []string; City, CountryCode, VenueType, Latitude, Longitude, RadiusKM string }`
  - `type Location struct { Latitude, Longitude float64; RadiusKM int }`
  - `type Query struct { Limit int; Cursor, Text string; GenreIDs []int; City, CountryCode, VenueType string; Location *Location }`
  - `func ParseQuery(raw RawQuery) (Query, error)`
  - `type sortMode int` with `modeName`, `modeDistance`, `modeRelevanceName`, `modeRelevanceDistance`, and `func (q Query) mode() sortMode`
  - `var countryCodes map[string]struct{}`

`Query.Text` and `Query.City` hold NFC, trimmed request text. Search keys are computed later in SQL. `GenreIDs` is ascending and distinct, or nil when absent.

- [ ] **Step 1: Write the failing parser tests**

Create `backend/internal/venue/query_test.go`:

```go
package venue

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func parseFields(t *testing.T, raw RawQuery) map[string]string {
	t.Helper()
	_, err := ParseQuery(raw)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("raw %+v err %v", raw, err)
	}
	return validation.Fields
}

func TestParseQueryAcceptsEveryFilter(t *testing.T) {
	got, err := ParseQuery(RawQuery{
		Limit: 10, Cursor: "abc", Query: "  Cafe\u0301 Iron  ",
		GenreIDs: []string{"3", "", "1", "3", "01"},
		City:     "Oslo", CountryCode: "XK", VenueType: "club",
		Latitude: "-90", Longitude: "180", RadiusKM: "100",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Query{
		Limit: 10, Cursor: "abc", Text: "Caf\u00e9 Iron", GenreIDs: []int{1, 3},
		City: "Oslo", CountryCode: "XK", VenueType: "club",
		Location: &Location{Latitude: -90, Longitude: 180, RadiusKM: 100},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestParseQueryEmptyMeansAbsent(t *testing.T) {
	got, err := ParseQuery(RawQuery{Limit: 20, GenreIDs: []string{""}})
	if err != nil || !reflect.DeepEqual(got, Query{Limit: 20}) {
		t.Fatalf("got %+v err %v", got, err)
	}
}

func TestParseQueryBoundaries(t *testing.T) {
	twenty := []string{}
	for i := 1; i <= 20; i++ {
		twenty = append(twenty, strconv.Itoa(i), strconv.Itoa(i))
	}
	for _, raw := range []RawQuery{
		{Limit: 1},
		{Limit: 100},
		{Limit: 20, Query: strings.Repeat("a", 100)},
		{Limit: 20, City: strings.Repeat("a", 120)},
		{Limit: 20, Query: "iron\tmaiden"},
		{Limit: 20, GenreIDs: twenty},
		{Limit: 20, GenreIDs: []string{"2147483647"}},
		{Limit: 20, CountryCode: "GB"},
		{Limit: 20, VenueType: "bar"},
		{Limit: 20, VenueType: "pub"},
		{Limit: 20, Latitude: "90", Longitude: "-180", RadiusKM: "1"},
	} {
		if _, err := ParseQuery(raw); err != nil {
			t.Fatalf("raw %+v err %v", raw, err)
		}
	}
}

func TestParseQueryRejects(t *testing.T) {
	twentyOne := []string{}
	for i := 1; i <= 21; i++ {
		twentyOne = append(twentyOne, strconv.Itoa(i))
	}
	at := func(lat, lon, radius string) RawQuery {
		return RawQuery{Limit: 20, Latitude: lat, Longitude: lon, RadiusKM: radius}
	}
	cases := []struct {
		raw     RawQuery
		field   string
		message string
	}{
		{RawQuery{Limit: 0}, "limit", "must be from 1 to 100"},
		{RawQuery{Limit: 101}, "limit", "must be from 1 to 100"},
		{RawQuery{Limit: 20, Query: "   "}, "query", "must be 1 to 100 characters"},
		{RawQuery{Limit: 20, Query: strings.Repeat("a", 101)}, "query", "must be 1 to 100 characters"},
		{RawQuery{Limit: 20, Query: "iron\nmaiden"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, Query: "iron\x00"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, Query: "\xff"}, "query", "must be a single line of text"},
		{RawQuery{Limit: 20, City: strings.Repeat("a", 121)}, "city", "must be 1 to 120 characters"},
		{RawQuery{Limit: 20, City: "Os\rlo"}, "city", "must be a single line of text"},
		{RawQuery{Limit: 20, GenreIDs: []string{"0"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"-1"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"x"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"1.5"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: []string{"2147483648"}}, "genre_id", "must be a positive integer"},
		{RawQuery{Limit: 20, GenreIDs: twentyOne}, "genre_id", "must be provided at most 20 times"},
		{RawQuery{Limit: 20, CountryCode: "gb"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, CountryCode: "UK"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, CountryCode: "ZZ"}, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{RawQuery{Limit: 20, VenueType: "tavern"}, "venue_type", "must be bar, pub, or club"},
		{at("90.0001", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("NaN", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("Inf", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("north", "0", "5"), "latitude", "must be from -90 to 90"},
		{at("0", "-180.5", "5"), "longitude", "must be from -180 to 180"},
		{at("0", "0", "0"), "radius_km", "must be from 1 to 100"},
		{at("0", "0", "101"), "radius_km", "must be from 1 to 100"},
		{at("0", "0", "1.5"), "radius_km", "must be from 1 to 100"},
	}
	for _, c := range cases {
		fields := parseFields(t, c.raw)
		if fields[c.field] != c.message {
			t.Fatalf("raw %+v fields %+v want %s=%q", c.raw, fields, c.field, c.message)
		}
	}
}

func TestParseQueryIncompleteLocation(t *testing.T) {
	fields := parseFields(t, RawQuery{Limit: 20, Latitude: "59.9"})
	if len(fields) != 2 || fields["longitude"] != locationGroup || fields["radius_km"] != locationGroup {
		t.Fatalf("fields %+v", fields)
	}
	fields = parseFields(t, RawQuery{Limit: 20, RadiusKM: "5"})
	if len(fields) != 2 || fields["latitude"] != locationGroup || fields["longitude"] != locationGroup {
		t.Fatalf("fields %+v", fields)
	}
}

func TestParseQueryReportsEveryField(t *testing.T) {
	fields := parseFields(t, RawQuery{Limit: 0, VenueType: "tavern", CountryCode: "UK"})
	if len(fields) != 3 {
		t.Fatalf("fields %+v", fields)
	}
}

func TestSortMode(t *testing.T) {
	location := &Location{Latitude: 0, Longitude: 0, RadiusKM: 5}
	cases := map[sortMode]Query{
		modeName:              {Limit: 20, City: "Oslo", GenreIDs: []int{1}, CountryCode: "NO", VenueType: "bar"},
		modeDistance:          {Limit: 20, Location: location},
		modeRelevanceName:     {Limit: 20, Text: "iron"},
		modeRelevanceDistance: {Limit: 20, Text: "iron", Location: location},
	}
	for want, q := range cases {
		if got := q.mode(); got != want {
			t.Fatalf("query %+v mode %d want %d", q, got, want)
		}
	}
}
```

Create `backend/internal/venue/countries_test.go`:

```go
package venue

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

func TestCountryCodesMatchMigration(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations", "0005_venue_catalogue.sql"))
	if err != nil {
		t.Fatal(err)
	}
	start := bytes.Index(body, []byte("venues_country_code_check"))
	if start < 0 {
		t.Fatal("country constraint not found")
	}
	end := bytes.Index(body[start:], []byte("));"))
	if end < 0 {
		t.Fatal("country constraint end not found")
	}
	want := map[string]struct{}{}
	for _, match := range regexp.MustCompile(`'([A-Z]{2})'`).FindAllSubmatch(body[start:start+end], -1) {
		want[string(match[1])] = struct{}{}
	}
	if len(want) != len(countryCodes) {
		t.Fatalf("migration %d codes, Go %d codes", len(want), len(countryCodes))
	}
	for code := range want {
		if _, ok := countryCodes[code]; !ok {
			t.Fatalf("Go set misses %s", code)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue/ -run 'TestParseQuery|TestSortMode|TestCountryCodes' -v`
Expected: FAIL to compile with `undefined: RawQuery`, `undefined: ParseQuery`, `undefined: locationGroup`, `undefined: sortMode`, and `undefined: countryCodes`.

- [ ] **Step 3: Write the country set**

Create `backend/internal/venue/countries.go`:

```go
package venue

import "strings"

// Must equal the venues_country_code_check list in migration 0005.
var countryCodes = func() map[string]struct{} {
	set := map[string]struct{}{}
	for _, code := range strings.Fields(countryList) {
		set[code] = struct{}{}
	}
	return set
}()

const countryList = `
AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE
BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD
CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM
DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF
GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU
ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN
KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME
MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA
NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM
PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI
SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK
TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI
VN VU WF WS XK YE YT ZA ZM ZW
`
```

- [ ] **Step 4: Write the parser**

Create `backend/internal/venue/query.go`:

```go
package venue

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type RawQuery struct {
	Limit       int
	Cursor      string
	Query       string
	GenreIDs    []string
	City        string
	CountryCode string
	VenueType   string
	Latitude    string
	Longitude   string
	RadiusKM    string
}

type Location struct {
	Latitude  float64
	Longitude float64
	RadiusKM  int
}

type Query struct {
	Limit       int
	Cursor      string
	Text        string
	GenreIDs    []int
	City        string
	CountryCode string
	VenueType   string
	Location    *Location
}

type sortMode int

const (
	modeName sortMode = iota
	modeDistance
	modeRelevanceName
	modeRelevanceDistance
)

func (q Query) mode() sortMode {
	switch {
	case q.Text != "" && q.Location != nil:
		return modeRelevanceDistance
	case q.Text != "":
		return modeRelevanceName
	case q.Location != nil:
		return modeDistance
	}
	return modeName
}

const locationGroup = "must be provided with latitude, longitude, and radius_km"

func ParseQuery(raw RawQuery) (Query, error) {
	fields := map[string]string{}
	q := Query{Limit: raw.Limit, Cursor: raw.Cursor}
	if ValidateLimit(raw.Limit) != nil {
		fields["limit"] = "must be from 1 to 100"
	}
	q.Text = parseText(fields, "query", raw.Query, 100)
	q.City = parseText(fields, "city", raw.City, 120)
	q.GenreIDs = parseGenres(fields, raw.GenreIDs)
	if raw.CountryCode != "" {
		if _, ok := countryCodes[raw.CountryCode]; !ok {
			fields["country_code"] = "must be an ISO 3166-1 alpha-2 code"
		}
		q.CountryCode = raw.CountryCode
	}
	if raw.VenueType != "" {
		if raw.VenueType != "bar" && raw.VenueType != "pub" && raw.VenueType != "club" {
			fields["venue_type"] = "must be bar, pub, or club"
		}
		q.VenueType = raw.VenueType
	}
	q.Location = parseLocation(fields, raw)
	if len(fields) > 0 {
		return Query{}, &ValidationError{Fields: fields}
	}
	return q, nil
}

func parseText(fields map[string]string, name, raw string, max int) string {
	if raw == "" {
		return ""
	}
	if !utf8.ValidString(raw) || disallowedText(raw) {
		fields[name] = "must be a single line of text"
		return ""
	}
	value := norm.NFC.String(strings.TrimSpace(raw))
	if n := utf8.RuneCountInString(value); n < 1 || n > max {
		fields[name] = "must be 1 to " + strconv.Itoa(max) + " characters"
		return ""
	}
	return value
}

func disallowedText(value string) bool {
	for _, r := range value {
		if r == '\n' || r == '\r' || r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return true
		}
	}
	return false
}

func parseGenres(fields map[string]string, raw []string) []int {
	seen := map[int]bool{}
	var ids []int
	for _, value := range raw {
		if value == "" {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 32)
		if err != nil || id < 1 {
			fields["genre_id"] = "must be a positive integer"
			return nil
		}
		if !seen[int(id)] {
			seen[int(id)] = true
			ids = append(ids, int(id))
		}
	}
	if len(ids) > 20 {
		fields["genre_id"] = "must be provided at most 20 times"
		return nil
	}
	sort.Ints(ids)
	return ids
}

func parseLocation(fields map[string]string, raw RawQuery) *Location {
	present := map[string]bool{
		"latitude":  raw.Latitude != "",
		"longitude": raw.Longitude != "",
		"radius_km": raw.RadiusKM != "",
	}
	if !present["latitude"] && !present["longitude"] && !present["radius_km"] {
		return nil
	}
	if !present["latitude"] || !present["longitude"] || !present["radius_km"] {
		for name, ok := range present {
			if !ok {
				fields[name] = locationGroup
			}
		}
		return nil
	}
	latitude, latitudeOK := parseCoordinate(raw.Latitude, 90)
	if !latitudeOK {
		fields["latitude"] = "must be from -90 to 90"
	}
	longitude, longitudeOK := parseCoordinate(raw.Longitude, 180)
	if !longitudeOK {
		fields["longitude"] = "must be from -180 to 180"
	}
	radius, err := strconv.Atoi(raw.RadiusKM)
	radiusOK := err == nil && radius >= 1 && radius <= 100
	if !radiusOK {
		fields["radius_km"] = "must be from 1 to 100"
	}
	if !latitudeOK || !longitudeOK || !radiusOK {
		return nil
	}
	return &Location{Latitude: latitude, Longitude: longitude, RadiusKM: radius}
}

func parseCoordinate(raw string, bound float64) (float64, bool) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || value < -bound || value > bound {
		return 0, false
	}
	return value, true
}
```

`strconv.ParseFloat("Inf")` returns `+Inf` with a nil error; the bound check rejects it. `ParseInt(value, 10, 32)` rejects ids above 2147483647, because `genres.id` is a `serial` int4.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/venue/ -run 'TestParseQuery|TestSortMode|TestCountryCodes' -v`
Expected: PASS.

- [ ] **Step 6: Run vet and format**

Run: `gofmt -l . && go vet ./...`
Expected: no output from `gofmt`, and `go vet` passes.

- [ ] **Step 7: Commit**

```bash
git add internal/venue/query.go internal/venue/query_test.go internal/venue/countries.go internal/venue/countries_test.go
git commit -m "feat: parse venue discovery filters"
```

---

### Task 3: Filter-Bound Cursors

**Files:**
- Create: `backend/internal/venue/cursor.go`
- Create: `backend/internal/venue/cursor_test.go`

**Interfaces:**
- Consumes: `Query`, `sortMode` and its four constants, `ParseQuery`, `RawQuery` (Task 2); `CanonicalVenueID`, `ValidationError` (`venue.go`).
- Produces:
  - `type position struct { Tier int; NameKey string; Distance float64; ID string }`
  - `var discriminators map[sortMode]string`
  - `func filterHash(mode sortMode, q Query, textKey, cityKey string) string` — 16 lowercase hex characters
  - `func encodeCursor(mode sortMode, hash string, p position) string`
  - `func decodeCursor(cursor string, mode sortMode, hash string) (position, error)` — any failure is `*ValidationError{"cursor": "invalid"}`

The Session 7 exported `EncodeCursor` and `DecodeCursor` stay in `venue.go` until Task 4 removes them, so the package keeps compiling.

- [ ] **Step 1: Write the failing cursor tests**

Create `backend/internal/venue/cursor_test.go`:

```go
package venue

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testHash = "0123456789abcdef"

func TestCursorRoundTripEveryMode(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	cases := []struct {
		mode sortMode
		p    position
	}{
		{modeName, position{NameKey: "the bar", ID: id}},
		{modeName, position{NameKey: "", ID: id}},
		{modeDistance, position{Distance: 0.1 + 0.2, ID: id}},
		{modeDistance, position{Distance: 0, ID: id}},
		{modeDistance, position{Distance: 1e6, ID: id}},
		{modeRelevanceName, position{Tier: 4, NameKey: "бар no1", ID: id}},
		{modeRelevanceName, position{Tier: 0, NameKey: "", ID: id}},
		{modeRelevanceDistance, position{Tier: 3, Distance: 123456.78901234567, ID: id}},
	}
	for _, c := range cases {
		got, err := decodeCursor(encodeCursor(c.mode, testHash, c.p), c.mode, testHash)
		if err != nil || got != c.p {
			t.Fatalf("mode %d got %+v want %+v err %v", c.mode, got, c.p, err)
		}
	}
}

func TestCursorRejects(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	raw := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	cases := []struct {
		name   string
		cursor string
		mode   sortMode
	}{
		{"not base64", "!!!", modeName},
		{"other hash", encodeCursor(modeName, "fedcba9876543210", position{NameKey: "bar", ID: id}), modeName},
		{"other mode", encodeCursor(modeDistance, testHash, position{Distance: 1, ID: id}), modeName},
		{"session 7", raw("name-v1|bar|" + id), modeName},
		{"too few fields", raw("name-v2|" + testHash + "|bar"), modeName},
		{"too many fields", raw("name-v2|" + testHash + "|bar|baz|" + id), modeName},
		{"uppercase id", raw("name-v2|" + testHash + "|bar|" + strings.ToUpper(id)), modeName},
		{"bad utf8", raw("name-v2|" + testHash + "|\xff|" + id), modeName},
		{"null byte", raw("name-v2|" + testHash + "|b\x00r|" + id), modeName},
		{"negative distance", raw("distance-v1|" + testHash + "|-1|" + id), modeDistance},
		{"nan distance", raw("distance-v1|" + testHash + "|NaN|" + id), modeDistance},
		{"inf distance", raw("distance-v1|" + testHash + "|+Inf|" + id), modeDistance},
		{"word distance", raw("distance-v1|" + testHash + "|far|" + id), modeDistance},
		{"tier 5", raw("relevance-name-v1|" + testHash + "|5|bar|" + id), modeRelevanceName},
		{"tier 10", raw("relevance-name-v1|" + testHash + "|10|bar|" + id), modeRelevanceName},
		{"tier word", raw("relevance-distance-v1|" + testHash + "|x|1|" + id), modeRelevanceDistance},
	}
	for _, c := range cases {
		_, err := decodeCursor(c.cursor, c.mode, testHash)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("%s err %v", c.name, err)
		}
	}
}

func TestFilterHash(t *testing.T) {
	parse := func(raw RawQuery) Query {
		t.Helper()
		raw.Limit = 20
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		return q
	}
	base := parse(RawQuery{GenreIDs: []string{"3", "1"}, CountryCode: "NO"})
	same := parse(RawQuery{GenreIDs: []string{"1", "3", "1"}, CountryCode: "NO"})
	same.Limit = 50
	same.Cursor = "ignored"
	hash := filterHash(modeName, base, "", "")
	if len(hash) != 16 || strings.ToLower(hash) != hash || filterHash(modeName, same, "", "") != hash {
		t.Fatalf("hash %q same %q", hash, filterHash(modeName, same, "", ""))
	}
	location := RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "NO", Latitude: "1", Longitude: "2", RadiusKM: "5"}
	wider := location
	wider.RadiusKM = "6"
	others := []string{
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1"}, CountryCode: "NO"}), "", ""),
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "DE"}), "", ""),
		filterHash(modeName, parse(RawQuery{GenreIDs: []string{"1", "3"}, CountryCode: "NO", VenueType: "pub"}), "", ""),
		filterHash(modeName, base, "", "oslo"),
		filterHash(modeRelevanceName, base, "iron", ""),
		filterHash(modeDistance, parse(location), "", ""),
		filterHash(modeDistance, parse(wider), "", ""),
	}
	seen := map[string]bool{hash: true}
	for i, other := range others {
		if seen[other] {
			t.Fatalf("hash %d collides: %s", i, other)
		}
		seen[other] = true
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue/ -run 'TestCursorRoundTripEveryMode|TestCursorRejects|TestFilterHash' -v`
Expected: FAIL to compile with `undefined: position`, `undefined: encodeCursor`, `undefined: decodeCursor`, and `undefined: filterHash`.

- [ ] **Step 3: Write the cursor codec**

Create `backend/internal/venue/cursor.go`:

```go
package venue

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

type position struct {
	Tier     int
	NameKey  string
	Distance float64
	ID       string
}

var discriminators = map[sortMode]string{
	modeName:              "name-v2",
	modeDistance:          "distance-v1",
	modeRelevanceName:     "relevance-name-v1",
	modeRelevanceDistance: "relevance-distance-v1",
}

func filterHash(mode sortMode, q Query, textKey, cityKey string) string {
	genres := make([]string, len(q.GenreIDs))
	for i, id := range q.GenreIDs {
		genres[i] = strconv.Itoa(id)
	}
	latitude, longitude, radius := "", "", ""
	if q.Location != nil {
		latitude = formatFloat(q.Location.Latitude)
		longitude = formatFloat(q.Location.Longitude)
		radius = strconv.Itoa(q.Location.RadiusKM)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		discriminators[mode], textKey, cityKey, strings.Join(genres, ","),
		q.CountryCode, q.VenueType, latitude, longitude, radius,
	}, "\n")))
	return hex.EncodeToString(sum[:8])
}

func encodeCursor(mode sortMode, hash string, p position) string {
	fields := []string{discriminators[mode], hash}
	switch mode {
	case modeName:
		fields = append(fields, p.NameKey)
	case modeDistance:
		fields = append(fields, formatFloat(p.Distance))
	case modeRelevanceName:
		fields = append(fields, strconv.Itoa(p.Tier), p.NameKey)
	case modeRelevanceDistance:
		fields = append(fields, strconv.Itoa(p.Tier), formatFloat(p.Distance))
	}
	fields = append(fields, p.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(fields, "|")))
}

func decodeCursor(cursor string, mode sortMode, hash string) (position, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return position{}, invalid
	}
	parts := strings.Split(string(raw), "|")
	keys := 1
	if mode == modeRelevanceName || mode == modeRelevanceDistance {
		keys = 2
	}
	if len(parts) != 3+keys || parts[0] != discriminators[mode] || parts[1] != hash || !CanonicalVenueID(parts[len(parts)-1]) {
		return position{}, invalid
	}
	p := position{ID: parts[len(parts)-1]}
	sortKeys := parts[2 : 2+keys]
	if keys == 2 {
		if len(sortKeys[0]) != 1 || sortKeys[0][0] < '0' || sortKeys[0][0] > '4' {
			return position{}, invalid
		}
		p.Tier = int(sortKeys[0][0] - '0')
		sortKeys = sortKeys[1:]
	}
	switch mode {
	case modeName, modeRelevanceName:
		if !utf8.ValidString(sortKeys[0]) || strings.ContainsRune(sortKeys[0], 0) {
			return position{}, invalid
		}
		p.NameKey = sortKeys[0]
	default:
		distance, err := strconv.ParseFloat(sortKeys[0], 64)
		if err != nil || math.IsNaN(distance) || math.IsInf(distance, 0) || distance < 0 {
			return position{}, invalid
		}
		p.Distance = distance
	}
	return p, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
```

A name key never contains `|`: `venue_name_key` removes punctuation and symbols. A hex hash and a `'g'` float never contain `|` either.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/venue/ -run 'TestCursorRoundTripEveryMode|TestCursorRejects|TestFilterHash' -v`
Expected: PASS.

- [ ] **Step 5: Run vet and format**

Run: `gofmt -l . && go vet ./...`
Expected: no `gofmt` output; `go vet` passes.

- [ ] **Step 6: Commit**

```bash
git add internal/venue/cursor.go internal/venue/cursor_test.go
git commit -m "feat: bind venue cursors to their filters"
```

---

### Task 4: Discovery Reads and HTTP Adapter

**Files:**
- Modify: `backend/internal/venue/venue.go`
- Modify: `backend/internal/venue/venue_test.go`
- Modify: `backend/internal/venue/postgres.go`
- Modify: `backend/internal/venue/postgres_test.go`
- Create: `backend/internal/venue/discovery_test.go`
- Modify: `backend/internal/api/venue.go`
- Modify: `backend/internal/api/venue_test.go`

**Interfaces:**
- Consumes: `RawQuery`, `Query`, `Location`, `ParseQuery`, `sortMode` (Task 2); `position`, `filterHash`, `encodeCursor`, `decodeCursor` (Task 3); `venue_search_key` and the `search_*` columns (Task 1).
- Produces:
  - `Venue.DistanceMeters *int`
  - `func (s *Service) ListVenues(ctx context.Context, q Query) (Page, error)`
  - `func (s *Service) searchKeys(ctx context.Context, q Query) (string, string, error)`
  - `func listSQL(mode sortMode, q Query, textKey, cityKey string, after *position) (string, []any)`
  - `func scanVenue(row venueScanner) (Venue, position, error)`
  - `api.Catalogue.ListVenues(context.Context, venue.Query) (venue.Page, error)`
  - JSON field `distance_meters` on list items, omitted when nil.

This task changes the service and the adapter together, because `cmd/api` passes `*venue.Service` as `api.Catalogue`; splitting them would leave `go build ./...` broken between commits.

- [ ] **Step 1: Write the failing discovery integration tests**

Create `backend/internal/venue/discovery_test.go`:

```go
package venue

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func vid(n int) string {
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", n)
}

type place struct {
	id        string
	name      string
	address   string
	city      string
	country   string
	venueType string
	lon       float64
	lat       float64
	genres    []int
}

func insertPlace(t *testing.T, pool *pgxpool.Pool, p place) {
	t.Helper()
	ctx := context.Background()
	if p.address == "" {
		p.address = "1 Main Street"
	}
	if p.city == "" {
		p.city = "Oslo"
	}
	if p.country == "" {
		p.country = "NO"
	}
	if p.venueType == "" {
		p.venueType = "bar"
	}
	if len(p.genres) == 0 {
		p.genres = []int{genreID(t, pool, "Punk")}
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status)
		values ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5, $6, $7, $8, 'verified')`,
		p.id, p.name, p.lon, p.lat, p.address, p.city, p.country, p.venueType); err != nil {
		t.Fatal(err)
	}
	for _, genre := range p.genres {
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, p.id, genre); err != nil {
			t.Fatal(err)
		}
	}
}

func discover(t *testing.T, service *Service, raw RawQuery) Page {
	t.Helper()
	if raw.Limit == 0 {
		raw.Limit = 100
	}
	q, err := ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse %+v err %v", raw, err)
	}
	page, err := service.ListVenues(context.Background(), q)
	if err != nil {
		t.Fatalf("list %+v err %v", raw, err)
	}
	return page
}

func pageIDs(page Page) []string {
	ids := []string{}
	for _, got := range page.Venues {
		ids = append(ids, got.ID)
	}
	return ids
}

func assertIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func distanceFromOrigin(t *testing.T, pool *pgxpool.Pool, id string) float64 {
	t.Helper()
	var distance float64
	if err := pool.QueryRow(context.Background(), `
		select ST_Distance(location, ST_SetSRID(ST_MakePoint(0, 0), 4326)::geography)
		from venues where id = $1`, id).Scan(&distance); err != nil {
		t.Fatal(err)
	}
	return distance
}

func TestDiscoveryFilters(t *testing.T) {
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	heavy := genreID(t, pool, "Heavy metal")
	insertPlace(t, pool, place{id: vid(1), name: "Iron Pub", city: "Oslo", country: "NO", venueType: "pub", lon: 10.75, lat: 59.91, genres: []int{punk}})
	insertPlace(t, pool, place{id: vid(2), name: "Rust Bar", city: "Bergen", country: "NO", venueType: "bar", lon: 5.32, lat: 60.39, genres: []int{classic}})
	insertPlace(t, pool, place{id: vid(3), name: "Metal Club", city: "Berlin", country: "DE", venueType: "club", lon: 13.40, lat: 52.52, genres: []int{punk, classic}})
	insertPlace(t, pool, place{id: vid(4), name: "Black Hole", city: "Oslo", country: "NO", venueType: "bar", lon: 10.76, lat: 59.92, genres: []int{heavy}})
	service := New(pool)
	g := strconv.Itoa

	assertIDs(t, pageIDs(discover(t, service, RawQuery{})), vid(4), vid(1), vid(3), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(punk)}})), vid(1), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(punk), g(heavy)}})), vid(4), vid(1), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{"999999"}})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "oslo"})), vid(4), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{CountryCode: "DE"})), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{VenueType: "pub"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "Oslo", VenueType: "bar"})), vid(4))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{GenreIDs: []string{g(classic)}, CountryCode: "NO"})), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "metal"})), vid(3))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Latitude: "59.91", Longitude: "10.75", RadiusKM: "5"})), vid(1), vid(4))
}

func TestRadiusBoundary(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	for i, meters := range []float64{999.5, 1000, 1000.5} {
		if _, err := pool.Exec(ctx, `
			insert into venues (id, name, location, address, city, country_code, verification_status)
			values ($1, $2, ST_Project(ST_SetSRID(ST_MakePoint(0, 0), 4326)::geography, $3, radians(90)),
			        '1 Main Street', 'Oslo', 'NO', 'verified')`,
			vid(i+1), fmt.Sprintf("Ring %d", i), meters); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, vid(i+1), punk); err != nil {
			t.Fatal(err)
		}
	}
	page := discover(t, New(pool), RawQuery{Latitude: "0", Longitude: "0", RadiusKM: "1"})
	included := map[string]bool{}
	for _, got := range page.Venues {
		included[got.ID] = true
	}
	if !included[vid(1)] || included[vid(3)] {
		t.Fatalf("inside/outside included %v", included)
	}
	edge := distanceFromOrigin(t, pool, vid(2))
	if included[vid(2)] != (edge <= 1000) {
		t.Fatalf("venue at %v m included %v", edge, included[vid(2)])
	}
}

func TestDistanceOrderAndMeters(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(3), name: "Far", lon: 0.003, lat: 0})
	insertPlace(t, pool, place{id: vid(2), name: "Twin B", lon: 0, lat: 0.001})
	insertPlace(t, pool, place{id: vid(1), name: "Twin A", lon: 0, lat: 0.001})
	insertPlace(t, pool, place{id: vid(4), name: "Outside", lon: 0.02, lat: 0})
	service := New(pool)
	page := discover(t, service, RawQuery{Latitude: "0", Longitude: "0", RadiusKM: "1"})
	assertIDs(t, pageIDs(page), vid(1), vid(2), vid(3))
	for _, got := range page.Venues {
		want := int(math.Floor(distanceFromOrigin(t, pool, got.ID) + 0.5))
		if got.DistanceMeters == nil || *got.DistanceMeters != want {
			t.Fatalf("%s distance %v want %d", got.ID, got.DistanceMeters, want)
		}
	}
	for _, got := range discover(t, service, RawQuery{}).Venues {
		if got.DistanceMeters != nil {
			t.Fatalf("distance without location %+v", got)
		}
	}
	detail, err := service.GetVenue(ctx, vid(1))
	if err != nil || detail.DistanceMeters != nil {
		t.Fatalf("detail %+v err %v", detail, err)
	}
}

func TestRelevanceTiers(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron"})
	insertPlace(t, pool, place{id: vid(2), name: "Iron Bar"})
	insertPlace(t, pool, place{id: vid(3), name: "Ironbar"})
	insertPlace(t, pool, place{id: vid(4), name: "The Iron"})
	insertPlace(t, pool, place{id: vid(5), name: "Rust", city: "Iron"})
	insertPlace(t, pool, place{id: vid(6), name: "Pub", address: "1 Ironside Road"})
	insertPlace(t, pool, place{id: vid(7), name: "Bar", city: "Ironton"})
	insertPlace(t, pool, place{id: vid(8), name: "Rust Pub"})
	page := discover(t, New(pool), RawQuery{Query: "iron"})
	// Tiers 0, 1, 1, 2, 3, 4, 4. "iron bar" sorts before "ironbar" under C collation.
	assertIDs(t, pageIDs(page), vid(1), vid(2), vid(3), vid(4), vid(5), vid(7), vid(6))
}

func TestRelevanceDistanceTieBreak(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron", lon: 0.5})
	insertPlace(t, pool, place{id: vid(2), name: "Iron Bar", lon: 0.2})
	insertPlace(t, pool, place{id: vid(3), name: "Ironbar", lon: 0.1})
	page := discover(t, New(pool), RawQuery{Query: "iron", Latitude: "0", Longitude: "0", RadiusKM: "100"})
	assertIDs(t, pageIDs(page), vid(1), vid(3), vid(2))
}

func TestTextAndCityFolding(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron-Maiden Pub", city: "Saint-Étienne", country: "FR"})
	insertPlace(t, pool, place{id: vid(2), name: "Other", city: "Saint Etienne", country: "FR"})
	insertPlace(t, pool, place{id: vid(3), name: "Third", city: "Lyon", country: "FR"})
	service := New(pool)
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "saint etienne"})), vid(1), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "SAINT-ÉTIENNE"})), vid(1), vid(2))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{City: "Saint"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "iron maiden"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "IRON-MAIDEN"})), vid(1))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "ironmaidn"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "st etienne"})))
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Query: "étienne"})), vid(1), vid(2))
}

func TestPunctuationOnlyText(t *testing.T) {
	service := New(testPool(t))
	for field, raw := range map[string]RawQuery{
		"query": {Limit: 20, Query: "!!!"},
		"city":  {Limit: 20, City: "---"},
	} {
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ListVenues(context.Background(), q)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields[field] != "must contain a letter or digit" {
			t.Fatalf("%s err %v", field, err)
		}
	}
}

func TestWalkEveryMode(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Iron", lat: 0.001})
	insertPlace(t, pool, place{id: vid(2), name: "Iron", lat: 0.001})
	insertPlace(t, pool, place{id: vid(3), name: "Iron Bar", lon: 0.002})
	insertPlace(t, pool, place{id: vid(4), name: "Ironbar", lon: 0.002})
	insertPlace(t, pool, place{id: vid(5), name: "Pub", address: "1 Ironside Road", lon: 0.003})
	insertPlace(t, pool, place{id: vid(6), name: "Rust", city: "Iron", lon: 0.003})
	insertPlace(t, pool, place{id: vid(7), name: "!!!", lon: 0.004})
	service := New(pool)
	modes := map[string]RawQuery{
		"name":               {},
		"distance":           {Latitude: "0", Longitude: "0", RadiusKM: "100"},
		"relevance-name":     {Query: "iron"},
		"relevance-distance": {Query: "iron", Latitude: "0", Longitude: "0", RadiusKM: "100"},
	}
	for name, raw := range modes {
		full := pageIDs(discover(t, service, raw))
		if len(full) < 5 {
			t.Fatalf("%s full %v", name, full)
		}
		walked := []string{}
		cursor := ""
		for i := 0; i < 20; i++ {
			step := raw
			step.Limit = 2
			step.Cursor = cursor
			page := discover(t, service, step)
			walked = append(walked, pageIDs(page)...)
			if page.NextCursor == nil {
				break
			}
			cursor = *page.NextCursor
		}
		assertIDs(t, walked, full...)
	}
}

func TestCursorBoundToFilters(t *testing.T) {
	pool := testPool(t)
	insertPlace(t, pool, place{id: vid(1), name: "Alpha", country: "NO"})
	insertPlace(t, pool, place{id: vid(2), name: "Beta", country: "NO"})
	insertPlace(t, pool, place{id: vid(3), name: "Gamma", country: "DE"})
	service := New(pool)
	first := discover(t, service, RawQuery{Limit: 1, CountryCode: "NO"})
	if first.NextCursor == nil {
		t.Fatal("no cursor")
	}
	cursor := *first.NextCursor
	assertIDs(t, pageIDs(discover(t, service, RawQuery{Limit: 5, CountryCode: "NO", Cursor: cursor})), vid(2))
	for _, raw := range []RawQuery{
		{Limit: 1, CountryCode: "DE", Cursor: cursor},
		{Limit: 1, Cursor: cursor},
		{Limit: 1, CountryCode: "NO", Query: "beta", Cursor: cursor},
	} {
		q, err := ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.ListVenues(context.Background(), q)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("raw %+v err %v", raw, err)
		}
	}
}
```

In `TestWalkEveryMode`, `vid(1)` and `vid(2)` tie on name, point, and tier; `vid(3)`/`vid(4)` and `vid(5)`/`vid(6)` tie on distance; `vid(7)` has an empty name key.

- [ ] **Step 2: Update the Session 7 integration tests to the new signature**

In `backend/internal/venue/postgres_test.go`, replace each list call:

| Old | New |
| --- | --- |
| `service.ListVenues(context.Background(), 20, "")` | `service.ListVenues(context.Background(), Query{Limit: 20})` |
| `service.ListVenues(ctx, 20, "")` | `service.ListVenues(ctx, Query{Limit: 20})` |
| `New(pool).ListVenues(ctx, 1, "")` | `New(pool).ListVenues(ctx, Query{Limit: 1})` |
| `New(pool).ListVenues(ctx, 1, *page.NextCursor)` | `New(pool).ListVenues(ctx, Query{Limit: 1, Cursor: *page.NextCursor})` |
| `service.ListVenues(ctx, 1, "")` | `service.ListVenues(ctx, Query{Limit: 1})` |
| `service.ListVenues(ctx, 1, *page.NextCursor)` | `service.ListVenues(ctx, Query{Limit: 1, Cursor: *page.NextCursor})` |
| `service.ListVenues(ctx, 1, *next.NextCursor)` | `service.ListVenues(ctx, Query{Limit: 1, Cursor: *next.NextCursor})` |
| `New(pool).ListVenues(ctx, 20, "")` | `New(pool).ListVenues(ctx, Query{Limit: 20})` |
| `service.ListVenues(context.Background(), 0, "")` | `service.ListVenues(context.Background(), Query{Limit: 0})` |
| `service.ListVenues(context.Background(), 20, "abc")` | `service.ListVenues(context.Background(), Query{Limit: 20, Cursor: "abc"})` |

In `backend/internal/venue/venue_test.go`, delete `TestCursorRoundTrip` and `TestCursorRejectsMalformedValues`, and remove the now-unused `"encoding/base64"` import. Task 3 covers cursors. Keep `TestValidateLimit` and `TestCanonicalVenueID`.

- [ ] **Step 3: Write the failing HTTP tests**

In `backend/internal/api/venue_test.go`:

Add `"reflect"` to the imports.

In `TestVenueRoutes`, replace:

```go
	if list.Code != http.StatusOK || catalogue.limit != 20 || catalogue.cursor != "" || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next-page"`)) {
		t.Fatalf("list %d limit %d cursor %q body %s", list.Code, catalogue.limit, catalogue.cursor, list.Body.String())
	}
```

with:

```go
	if list.Code != http.StatusOK || catalogue.query.Limit != 20 || catalogue.query.Cursor != "" || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next-page"`)) {
		t.Fatalf("list %d query %+v body %s", list.Code, catalogue.query, list.Body.String())
	}
	if bytes.Contains(list.Body.Bytes(), []byte(`"distance_meters"`)) {
		t.Fatalf("distance without location %s", list.Body.String())
	}
```

In `TestVenueQueryValidation`, replace the `cases` slice and the loop body with:

```go
	cases := []string{
		"/v1/venues?limit=nope",
		"/v1/venues?limit=0",
		"/v1/venues?limit=101",
		"/v1/venues?limit=1&limit=2",
		"/v1/venues?cursor=a&cursor=b",
		"/v1/venues?city=Oslo&city=Bergen",
		"/v1/venues?sort=name",
		"/v1/genres?limit=20",
		"/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?cursor=abc",
	}
	for _, path := range cases {
		before := catalogue.listCalls + catalogue.getCalls + catalogue.genreCalls
		rec := requestVenue(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"validation_error"`)) {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body.String())
		}
		if catalogue.listCalls+catalogue.getCalls+catalogue.genreCalls != before {
			t.Fatalf("%s reached the catalogue", path)
		}
	}
```

Keep the rest of `TestVenueQueryValidation` unchanged.

Replace the fake's fields and `ListVenues`:

```go
type fakeCatalogue struct {
	genres     []venue.Genre
	page       venue.Page
	venue      venue.Venue
	query      venue.Query
	id         string
	genreCalls int
	listCalls  int
	getCalls   int
	listErr    error
	getErr     error
}
```

```go
func (f *fakeCatalogue) ListVenues(_ context.Context, q venue.Query) (venue.Page, error) {
	f.listCalls++
	f.query = q
	if f.listErr != nil {
		return venue.Page{}, f.listErr
	}
	return f.page, nil
}
```

Append these tests:

```go
func TestVenueDiscoveryParams(t *testing.T) {
	catalogue := &fakeCatalogue{}
	handler := venueHandler(catalogue)
	rec := requestVenue(handler, http.MethodGet, "/v1/venues?query=Iron&genre_id=3&genre_id=1&genre_id=3&city=Oslo&country_code=NO&venue_type=pub&latitude=59.9&longitude=10.75&radius_km=5&limit=10&cursor=abc", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	want := venue.Query{
		Limit: 10, Cursor: "abc", Text: "Iron", GenreIDs: []int{1, 3},
		City: "Oslo", CountryCode: "NO", VenueType: "pub",
		Location: &venue.Location{Latitude: 59.9, Longitude: 10.75, RadiusKM: 5},
	}
	if !reflect.DeepEqual(catalogue.query, want) {
		t.Fatalf("query %+v want %+v", catalogue.query, want)
	}
	for _, path := range []string{
		"/v1/venues?genre_id=x",
		"/v1/venues?latitude=1&longitude=2",
		"/v1/venues?latitude=91&longitude=0&radius_km=5",
		"/v1/venues?radius_km=1.5&latitude=0&longitude=0",
		"/v1/venues?country_code=UK",
		"/v1/venues?query=a&query=b",
	} {
		before := catalogue.listCalls
		rec := requestVenue(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"validation_error"`)) || catalogue.listCalls != before {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestDistanceMetersPresence(t *testing.T) {
	meters := 1234
	catalogue := &fakeCatalogue{page: venue.Page{Venues: []venue.Venue{{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Genres: []venue.Genre{}, DistanceMeters: &meters,
	}}}}
	handler := venueHandler(catalogue)
	rec := requestVenue(handler, http.MethodGet, "/v1/venues?latitude=0&longitude=0&radius_km=5", "")
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte(`"distance_meters":1234`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	catalogue.page.Venues[0].DistanceMeters = nil
	rec = requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if rec.Code != http.StatusOK || bytes.Contains(rec.Body.Bytes(), []byte(`"distance_meters"`)) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/venue/ ./internal/api/`
Expected: FAIL to compile. `internal/venue` reports `too many arguments`/`cannot use Query{...}` for `ListVenues` and `unknown field DistanceMeters`; `internal/api` reports that `*fakeCatalogue` does not implement `Catalogue` and `unknown field DistanceMeters`.

- [ ] **Step 5: Add `DistanceMeters` and remove the Session 7 cursor**

In `backend/internal/venue/venue.go`, add the field at the end of `Venue`:

```go
	CheckInCount       int
	DistanceMeters     *int
}
```

Delete `EncodeCursor` and `DecodeCursor`, and remove the imports they alone used (`encoding/base64`, `strings`, `unicode/utf8`). Keep `errors` and `regexp`.

- [ ] **Step 6: Replace the list read and the shared select**

In `backend/internal/venue/postgres.go`, replace `ListVenues`, the `venueSelect` constant, and `scanVenue` with the code below, and change the `GetVenue` query to use `venueSelect("null::float8", "null::int")`. Update imports to `context`, `encoding/json`, `errors`, `fmt`, `math`, `strconv`, `strings`, `time`, `github.com/jackc/pgx/v5`, and `github.com/jackc/pgx/v5/pgxpool`.

```go
func (s *Service) ListVenues(ctx context.Context, q Query) (Page, error) {
	if err := ValidateLimit(q.Limit); err != nil {
		return Page{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	textKey, cityKey, err := s.searchKeys(queryCtx, q)
	if err != nil {
		return Page{}, err
	}
	mode := q.mode()
	hash := filterHash(mode, q, textKey, cityKey)
	var after *position
	if q.Cursor != "" {
		p, err := decodeCursor(q.Cursor, mode, hash)
		if err != nil {
			return Page{}, err
		}
		after = &p
	}
	sql, args := listSQL(mode, q, textKey, cityKey, after)
	rows, err := s.pool.Query(queryCtx, sql, args...)
	if err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	defer rows.Close()
	venues := []Venue{}
	positions := []position{}
	for rows.Next() {
		got, p, err := scanVenue(rows)
		if err != nil {
			return Page{}, err
		}
		venues = append(venues, got)
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	var next *string
	if len(venues) > q.Limit {
		venues = venues[:q.Limit]
		encoded := encodeCursor(mode, hash, positions[q.Limit-1])
		next = &encoded
	}
	return Page{Venues: venues, NextCursor: next}, nil
}

func (s *Service) searchKeys(ctx context.Context, q Query) (string, string, error) {
	if q.Text == "" && q.City == "" {
		return "", "", nil
	}
	var textKey, cityKey string
	if err := s.pool.QueryRow(ctx, `select venue_search_key($1), venue_search_key($2)`, q.Text, q.City).Scan(&textKey, &cityKey); err != nil {
		return "", "", fmt.Errorf("venue: search keys: %w", err)
	}
	fields := map[string]string{}
	if q.Text != "" && textKey == "" {
		fields["query"] = "must contain a letter or digit"
	}
	if q.City != "" && cityKey == "" {
		fields["city"] = "must contain a letter or digit"
	}
	if len(fields) > 0 {
		return "", "", &ValidationError{Fields: fields}
	}
	return textKey, cityKey, nil
}

type sqlArgs []any

func (a *sqlArgs) add(value any) string {
	*a = append(*a, value)
	return "$" + strconv.Itoa(len(*a))
}

func listSQL(mode sortMode, q Query, textKey, cityKey string, after *position) (string, []any) {
	var args sqlArgs
	where := []string{
		`v.verification_status in ('pending', 'verified')`,
		`exists (select 1 from venue_genres link where link.venue_id = v.id)`,
	}
	distance, tier := "null::float8", "null::int"
	if q.Location != nil {
		point := fmt.Sprintf("ST_SetSRID(ST_MakePoint(%s::float8, %s::float8), 4326)::geography",
			args.add(q.Location.Longitude), args.add(q.Location.Latitude))
		distance = "ST_Distance(v.location, " + point + ")"
		where = append(where, fmt.Sprintf("ST_DWithin(v.location, %s, %s::float8)",
			point, args.add(float64(q.Location.RadiusKM)*1000)))
	}
	if textKey != "" {
		key := args.add(textKey) + "::text"
		contains := "'%' || " + key + " || '%'"
		tier = fmt.Sprintf(`case
			when v.search_name = %[1]s then 0
			when v.search_name like %[1]s || '%%' then 1
			when v.search_name like %[2]s then 2
			when v.search_city = %[1]s then 3
			else 4 end`, key, contains)
		where = append(where, fmt.Sprintf(
			"(v.search_name like %[1]s or v.search_address like %[1]s or v.search_city like %[1]s)", contains))
	}
	if cityKey != "" {
		where = append(where, "v.search_city = "+args.add(cityKey)+"::text")
	}
	if len(q.GenreIDs) > 0 {
		where = append(where, "exists (select 1 from venue_genres pick where pick.venue_id = v.id and pick.genre_id = any("+args.add(q.GenreIDs)+"::int[]))")
	}
	if q.CountryCode != "" {
		where = append(where, "v.country_code = "+args.add(q.CountryCode)+"::text")
	}
	if q.VenueType != "" {
		where = append(where, "v.venue_type = "+args.add(q.VenueType)+"::text")
	}

	nameKey := `venue_name_key(v.name) collate "C"`
	var order []string
	switch mode {
	case modeName:
		order = []string{nameKey, "v.id"}
	case modeDistance:
		order = []string{distance, "v.id"}
	case modeRelevanceName:
		order = []string{tier, nameKey, "v.id"}
	case modeRelevanceDistance:
		order = []string{tier, distance, "v.id"}
	}
	if after != nil {
		var bound []string
		switch mode {
		case modeName:
			bound = []string{args.add(after.NameKey) + `::text collate "C"`}
		case modeDistance:
			bound = []string{args.add(after.Distance) + "::float8"}
		case modeRelevanceName:
			bound = []string{args.add(after.Tier) + "::int", args.add(after.NameKey) + `::text collate "C"`}
		case modeRelevanceDistance:
			bound = []string{args.add(after.Tier) + "::int", args.add(after.Distance) + "::float8"}
		}
		bound = append(bound, args.add(after.ID)+"::uuid")
		where = append(where, "("+strings.Join(order, ", ")+") > ("+strings.Join(bound, ", ")+")")
	}
	sql := venueSelect(distance, tier) +
		"\n\twhere " + strings.Join(where, "\n\t  and ") +
		"\n\torder by " + strings.Join(order, ", ") +
		"\n\tlimit " + args.add(q.Limit+1)
	return sql, args
}

func venueSelect(distance, tier string) string {
	return `
	select v.id::text, v.name, v.description,
		ST_Y(v.location::geometry), ST_X(v.location::geometry),
		v.address, v.city, v.country_code, v.venue_type, v.verification_status,
		coalesce((
			select json_agg(
				json_build_object('id', g.id, 'name', g.name)
				order by g.name collate "C", g.id
			)
			from venue_genres vg
			join genres g on g.id = vg.genre_id
			where vg.venue_id = v.id
		), '[]'::json),
		null::float8, 0, 0,
		venue_name_key(v.name), ` + distance + `, ` + tier + `
	from venues v`
}

type venueScanner interface {
	Scan(dest ...any) error
}

func scanVenue(row venueScanner) (Venue, position, error) {
	var got Venue
	var genres []byte
	var p position
	var distance *float64
	var tier *int
	err := row.Scan(
		&got.ID, &got.Name, &got.Description,
		&got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.AverageRating, &got.ReviewCount, &got.CheckInCount,
		&p.NameKey, &distance, &tier,
	)
	if err != nil {
		return Venue{}, position{}, err
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Venue{}, position{}, fmt.Errorf("venue: genres: %w", err)
	}
	p.ID = got.ID
	if distance != nil {
		p.Distance = *distance
		meters := int(math.Floor(*distance + 0.5))
		got.DistanceMeters = &meters
	}
	if tier != nil {
		p.Tier = *tier
	}
	return got, p, nil
}
```

`GetVenue` becomes:

```go
	row := s.pool.QueryRow(queryCtx, venueSelect("null::float8", "null::int")+`
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres link where link.venue_id = v.id)`, venueID)
	got, _, err := scanVenue(row)
```

A search key holds only alphanumeric characters, so the `'%' || key || '%'` patterns never carry a request-supplied wildcard.

- [ ] **Step 7: Update the HTTP adapter**

In `backend/internal/api/venue.go`:

Change the interface method:

```go
	ListVenues(context.Context, venue.Query) (venue.Page, error)
```

Add `DistanceMeters` to `venueView`, after `CheckInCount`:

```go
	CheckInCount       int         `json:"check_in_count"`
	DistanceMeters     *int        `json:"distance_meters,omitempty"`
}
```

Add `DistanceMeters: got.DistanceMeters,` to the `venueView` literal in `venueJSON`.

Add the parameter set, where `true` means the parameter may repeat:

```go
var venueListParams = map[string]bool{
	"limit": false, "cursor": false, "query": false, "genre_id": true, "city": false,
	"country_code": false, "venue_type": false, "latitude": false, "longitude": false, "radius_km": false,
}
```

Replace `listVenues`:

```go
func listVenues(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, venueListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	query, err := venue.ParseQuery(venue.RawQuery{
		Limit: limit, Cursor: values.Get("cursor"), Query: values.Get("query"),
		GenreIDs: values["genre_id"], City: values.Get("city"),
		CountryCode: values.Get("country_code"), VenueType: values.Get("venue_type"),
		Latitude: values.Get("latitude"), Longitude: values.Get("longitude"),
		RadiusKM: values.Get("radius_km"),
	})
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	page, err := catalogue.ListVenues(r.Context(), query)
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	views := make([]venueView, 0, len(page.Venues))
	for _, got := range page.Venues {
		views = append(views, venueJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}
```

Replace `rejectedQuery`:

```go
func rejectedQuery(r *http.Request, allowed map[string]bool) map[string]string {
	fields := map[string]string{}
	for key, values := range r.URL.Query() {
		repeatable, ok := allowed[key]
		if !ok {
			fields[key] = "is not accepted"
			continue
		}
		if len(values) > 1 && !repeatable {
			fields[key] = "must be provided once"
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
```

`listGenres` and `getVenue` still pass `nil`, and a lookup in a nil map reports `ok == false`, so every parameter on those routes stays `is not accepted`.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/venue/ ./internal/api/ -v`
Expected: PASS, including every Session 7 catalogue test and every new discovery test.

- [ ] **Step 9: Run the whole module**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: no `gofmt` output; vet, tests, and build pass.

- [ ] **Step 10: Commit**

```bash
git add internal/venue/venue.go internal/venue/venue_test.go internal/venue/postgres.go internal/venue/postgres_test.go internal/venue/discovery_test.go internal/api/venue.go internal/api/venue_test.go
git commit -m "feat: filter and sort the public venue catalogue"
```

---

### Task 5: Plan-Justified Indexes

**Files:**
- Create: `backend/internal/venue/index_test.go`
- Modify (only if an assertion cannot pass): `infra/supabase/migrations/0006_venue_discovery.sql`

**Interfaces:**
- Consumes: `listSQL`, `Service.searchKeys`, `ParseQuery`, `RawQuery`, `testPool`, `genreID` (earlier tasks); the six indexes from Task 1 and `venues_location_idx` from `0001`.
- Produces: a test that proves each shipped index is chosen for its filter on a seeded catalogue.

- [ ] **Step 1: Write the plan test**

Create `backend/internal/venue/index_test.go`:

```go
package venue

import (
	"context"
	"strconv"
	"strings"
	"testing"
)

func TestDiscoveryIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	if _, err := pool.Exec(ctx, `insert into genres (name) values ('Rare test')`); err != nil {
		t.Fatal(err)
	}
	rare := genreID(t, pool, "Rare test")
	if _, err := pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code, verification_status)
		select 'Venue ' || i,
		       ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street',
		       'City ' || (i % 500),
		       case when i % 1000 = 0 then 'NO' else 'DE' end,
		       'verified'
		from generate_series(1, 5000) as i`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) select id, $1 from venues`, punk); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id)
		select id, $1 from venues where name in ('Venue 7', 'Venue 8', 'Venue 9')`, rare); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"venues", "venue_genres"} {
		if _, err := pool.Exec(ctx, "analyze "+table); err != nil {
			t.Fatal(err)
		}
	}
	service := New(pool)
	cases := []struct {
		name    string
		raw     RawQuery
		indexes []string
	}{
		{"text", RawQuery{Query: "venue4321"}, []string{"venues_search_name_trgm_idx", "venues_search_address_trgm_idx", "venues_search_city_trgm_idx"}},
		{"city", RawQuery{City: "City 7"}, []string{"venues_search_city_idx"}},
		{"country", RawQuery{CountryCode: "NO"}, []string{"venues_country_code_idx"}},
		{"genre", RawQuery{GenreIDs: []string{strconv.Itoa(rare)}}, []string{"venue_genres_genre_id_idx"}},
		{"radius", RawQuery{Latitude: "-85", Longitude: "0", RadiusKM: "5"}, []string{"venues_location_idx"}},
	}
	for _, c := range cases {
		c.raw.Limit = 20
		q, err := ParseQuery(c.raw)
		if err != nil {
			t.Fatal(err)
		}
		textKey, cityKey, err := service.searchKeys(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		sql, args := listSQL(q.mode(), q, textKey, cityKey, nil)
		rows, err := pool.Query(ctx, "explain "+sql, args...)
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, line)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(lines, "\n")
		for _, index := range c.indexes {
			if !strings.Contains(plan, index) {
				t.Fatalf("%s plan does not use %s:\n%s", c.name, index, plan)
			}
		}
	}
}
```

The planner sees default settings; the test does not disable sequential scans. Each case filters to a small fraction of 5,000 rows: one name, ten cities, five countries, three genre links, and a 5 km radius at latitude −85 that holds only a couple of seeded points.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/venue/ -run TestDiscoveryIndexes -v`
Expected: PASS. Each index name appears in its case's plan.

- [ ] **Step 3: Remove any index no plan uses**

If a case fails because the planner chose a different access path, do not change planner settings or the seed to force it. Delete that index's `create index` line from `0006_venue_discovery.sql`, delete that name from the case's `indexes` (delete the case if it becomes empty), and record the dropped index and the plan it chose in the pull request description. Then rerun:

Run: `go test ./internal/venue/ -run TestDiscoveryIndexes -v && go test ./internal/database/ -v`
Expected: PASS.

If nothing failed in Step 2, skip this step.

- [ ] **Step 4: Run vet and format**

Run: `gofmt -l . && go vet ./...`
Expected: no `gofmt` output; vet passes.

- [ ] **Step 5: Commit**

```bash
git add internal/venue/index_test.go ../infra/supabase/migrations/0006_venue_discovery.sql
git commit -m "test: justify venue discovery indexes with query plans"
```

---

### Task 6: Contract and Documentation

**Files:**
- Modify: `backend/openapi.yaml` (`/v1/venues` parameters and responses; `PublicVenue`)
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the behavior from Tasks 1–5.
- Produces: the published contract for discovery, and current agent and roadmap docs.

- [ ] **Step 1: Write the failing contract test**

In `backend/contract_test.go`, add these strings to the `want` list, after `"average_rating",`:

```go
		"genre_id",
		"radius_km",
		"distance_meters",
		"must be provided with latitude, longitude, and radius_km",
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestOpenAPIDocument -v`
Expected: FAIL with `missing "genre_id"`.

- [ ] **Step 3: Document the list parameters**

In `backend/openapi.yaml`, under `/v1/venues` → `get` → `parameters`, replace the existing `cursor` parameter:

```yaml
        - name: cursor
          in: query
          required: false
          schema:
            type: string
```

with:

```yaml
        - name: cursor
          in: query
          required: false
          description: Opaque next_cursor from a previous page. Valid only with the same filters that produced it; limit may change. Otherwise the request fails with cursor invalid.
          schema:
            type: string
        - name: query
          in: query
          required: false
          description: Substring match on Venue name, address, or city. Matching ignores case, accents, punctuation, and whitespace, and does not tolerate typos. Must contain a letter or digit.
          schema:
            type: string
            minLength: 1
            maxLength: 100
        - name: genre_id
          in: query
          required: false
          description: Repeat to match Venues with any of the listed Genres. Unknown ids match nothing.
          style: form
          explode: true
          schema:
            type: array
            maxItems: 20
            items:
              type: integer
              minimum: 1
              maximum: 2147483647
        - name: city
          in: query
          required: false
          description: Exact city match after the same folding as query, so Saint-Étienne matches Saint Etienne. Must contain a letter or digit.
          schema:
            type: string
            minLength: 1
            maxLength: 120
        - name: country_code
          in: query
          required: false
          description: Official ISO 3166-1 alpha-2 code, plus XK for Kosovo. The United Kingdom is GB; UK is rejected.
          schema:
            type: string
            pattern: "^[A-Z]{2}$"
        - name: venue_type
          in: query
          required: false
          schema:
            type: string
            enum: [bar, pub, club]
        - name: latitude
          in: query
          required: false
          description: WGS84 latitude. Send with longitude and radius_km.
          schema:
            type: number
            minimum: -90
            maximum: 90
        - name: longitude
          in: query
          required: false
          description: WGS84 longitude. Send with latitude and radius_km.
          schema:
            type: number
            minimum: -180
            maximum: 180
        - name: radius_km
          in: query
          required: false
          description: Inclusive radius around latitude and longitude. Send with both.
          schema:
            type: integer
            minimum: 1
            maximum: 100
```

Replace the `"200"` description line:

```yaml
          description: One page of public venues. Traversal is stable while catalogue membership and names are unchanged, and best-effort across concurrent changes.
```

with:

```yaml
          description: >-
            One page of public venues. Without query or location, results sort by
            folded name, then id. With a location only, by distance, then id. With
            query only, by relevance tier (exact name, name prefix, name substring,
            exact city, other city or address match), then folded name, then id.
            With query and a location, by relevance tier, then distance, then id.
            Traversal is stable while catalogue membership and sort keys are
            unchanged, and best-effort across concurrent changes.
```

Replace the `"400"` block's description and example fields:

```yaml
          description: The limit or cursor is invalid, or another parameter was sent.
```

with:

```yaml
          description: A filter, limit, or cursor is invalid, a parameter is repeated or unknown, or the location group is incomplete.
```

and:

```yaml
                  fields:
                    country_code: is not accepted
```

with:

```yaml
                  fields:
                    radius_km: must be provided with latitude, longitude, and radius_km
```

- [ ] **Step 4: Document `distance_meters`**

In `PublicVenue` → `properties`, after:

```yaml
        check_in_count:
          type: integer
```

add:

```yaml
        distance_meters:
          type: integer
          minimum: 0
          description: Metres from the supplied point, rounded half up. Present only on GET /v1/venues when latitude, longitude, and radius_km are supplied. Never present on GET /v1/venues/{venueID}.
```

Do not add `distance_meters` to `required`.

- [ ] **Step 5: Run the contract test**

Run: `go test . -run TestOpenAPIDocument -v`
Expected: PASS.

- [ ] **Step 6: Update `backend/AGENTS.md`**

Replace:

```text
`GET /readyz` returns 200 when the database ping succeeds and migration
`0005` or newer is recorded. Otherwise it returns 503.
```

with:

```text
`GET /readyz` returns 200 when the database ping succeeds and migration
`0006` or newer is recorded. Otherwise it returns 503.
```

In `## Layout`, after the `internal/ratelimit` line, add:

```text
- `internal/venue`: Genre list, public catalogue, and discovery filters, sort modes, and filter-bound cursors.
```

- [ ] **Step 7: Update `DevSteps.md`**

Replace line 6:

```text
**Status:** Sessions 1–7 are done. Session 8 is next. Sessions 9–14 are not started.
```

with:

```text
**Status:** Sessions 1–8 are done. Session 9 is next. Sessions 10–14 are not started.
```

Replace the sequence-table row:

```text
| 6–14 | Not started |
```

with:

```text
| 6. Profile safety and moderation API | Done |
| 7. Venue catalogue | Done |
| 8. Venue discovery | Done |
| 9–14 | Not started |
```

Under `### Session 8: Venue discovery`, replace `**Status:** Not started.` with `**Status:** Done.` and add this paragraph directly before that section's `**Exit condition:**` line:

```text
Migration `0006` adds `venue_search_key`, which is `venue_name_key` with all
whitespace removed, and trigger-maintained `search_name`, `search_address`,
and `search_city` columns with indexes that captured query plans use. These
columns serve discovery only; Session 9 duplicate detection keeps using
`venue_name_key`. The server chooses one of four orders from the filters,
cursors are bound to that order and the filter set, and `distance_meters`
appears on list items only when a location is supplied. Readiness requires
`0006`.
```

- [ ] **Step 8: Run the full definition of done**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: no `gofmt` output; vet, tests, and build pass.

- [ ] **Step 9: Check `/docs` renders the contract**

Run: `make run` in one terminal, then in another: `curl -s http://localhost:8080/openapi.yaml | grep -c distance_meters`
Expected: a count of at least 1. Open `http://localhost:8080/docs` and confirm the `listVenues` operation shows the new parameters. Stop the server. If no local `.env` with a reachable database exists, record that this manual check was skipped in the pull request.

- [ ] **Step 10: Commit**

```bash
git add openapi.yaml contract_test.go AGENTS.md ../DevSteps.md
git commit -m "docs: publish the venue discovery contract"
```
