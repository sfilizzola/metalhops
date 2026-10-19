# Session 7 Venue Catalogue Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve the public Genre list, the unfiltered Venue catalogue, and Venue detail from the Go API.

**Architecture:** `internal/venue` owns catalogue membership, `venue_name_key` ordering, and the three reads. `internal/api` defines a `Catalogue` interface and maps results to HTTP. HTTP tests use a fake. PostgreSQL is the production adapter. Migration `0005` makes the catalogue columns required and adds the shared name key.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL 16, PostGIS, `unaccent`, OpenAPI 3.1, `httptest`, and `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-19-session-7-venue-catalogue-design.md`

## Global Constraints

- Follow DevSteps and `docs/architecture/backend.md`.
- Keep `cmd/api` as composition only. Add `internal/venue`. Extend `internal/api` and `internal/database`.
- Do not edit migrations `0001` through `0004`. Add `infra/supabase/migrations/0005_venue_catalogue.sql`.
- Add no dependency.
- The API process never applies migrations. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Public reads do not resolve a bearer token and do not consult `internal/auth` or `internal/profile`.
- Discovery filters, Venue submissions, delisting, rejection feedback, and `status_changed_at` stay in later sessions.
- Review and Check-in tables stay in later sessions. This session does not create them.
- Aggregates are SQL literals: `average_rating` null, `review_count` 0, `check_in_count` 0.
- Continue on `feature/session-7-venue-catalogue`. Merge into `dev` by pull request.
- Commands in this plan use `go` and `git`, run from `backend/` unless a step says otherwise.

## Review Focus

1. **Two genres, one venue:** a venue linked to two genres must occupy one catalogue slot. A join that multiplies rows makes `limit=1` return that venue twice or skip the next venue. Task 3 pins this with `TestVenueWithTwoGenresCountsOnce`.
2. **Empty name key:** a punctuation-only name produces an empty `venue_name_key`, and its cursor starts with the encoded form of `|` plus the id. A decoder that rejects an empty key cannot page past `!!!`. Task 3 pins this with `TestEmptyNameKeyCursorSurvivesDeletion`.
3. **Supplied `updated_at`:** an `UPDATE` that sets `updated_at` to `2000-01-01` must still store the trigger time. A trigger that only fills a null leaves the supplied value. Task 1 pins this with `TestUpdatedAtTriggerOverwritesSuppliedTime`.
4. **Uppercase venue id:** `AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA` is not a canonical id. The read must return validation, not `ErrNotFound`, or HTTP maps it to 404. Task 3 pins this with `TestUppercaseVenueIDIsInvalid`.
5. **Data API execute:** `anon` must receive SQLSTATE `42501` for `select venue_name_key('Bar!')`. A function left granted to `PUBLIC` lets the Data API fold names. Task 1 pins this with the new statement in `TestDataAPIClosed`.

---

## File Structure

New files:

```text
infra/supabase/migrations/0005_venue_catalogue.sql
backend/internal/database/catalogue_test.go
backend/internal/venue/venue.go          # values, cursor, validation
backend/internal/venue/venue_test.go
backend/internal/venue/postgres.go       # genre, list, and detail SQL
backend/internal/venue/postgres_test.go
backend/internal/api/venue.go            # public HTTP adapters
backend/internal/api/venue_test.go
```

Focused modifications:

```text
backend/internal/database/pool.go
backend/internal/database/postgres_test.go
backend/internal/database/access_test.go
backend/internal/profile/delete_test.go
backend/internal/api/server.go
backend/cmd/api/main.go
backend/openapi.yaml
backend/contract_test.go
backend/AGENTS.md
DevSteps.md
```

`pgtest.MigratedURL` applies every SQL file in order. No test-harness edit is required for `0005` to load.

### Task 1: Catalogue Migration and Readiness

**Files:**
- Create: `infra/supabase/migrations/0005_venue_catalogue.sql`
- Create: `backend/internal/database/catalogue_test.go`
- Modify: `backend/internal/database/pool.go`
- Modify: `backend/internal/database/postgres_test.go`
- Modify: `backend/internal/database/access_test.go`
- Modify: `backend/internal/profile/delete_test.go`
- Test: `backend/internal/database/postgres_test.go`
- Test: `backend/internal/database/catalogue_test.go`
- Test: `backend/internal/database/access_test.go`
- Test: `backend/internal/profile/delete_test.go`

**Interfaces:**
- Consumes: migrations `0001` through `0004`, `database.Pool.Check`, `pgtest.MigratedURL`.
- Produces: `venues.country_code`, required `address` and `city`, `venues.updated_at`, trigger `venues_set_updated_at`, and `venue_name_key(text) returns text`. Readiness version `0005`.

- [ ] **Step 1: Write the failing readiness, constraint, and fixture tests**

In `TestMigrationVersions`, replace the final `0004` assertion. After applying `0004` and inserting version `0004`, expect `(false, "migrations_behind")`. Then apply `0005_venue_catalogue.sql`, insert version `0005`, and expect `(true, "ok")`.

```go
execSQL(t, pool, readMigration(t, "0004_profile_safety.sql"))
execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0004')`)
ready, reason = pool.Check(ctx)
if ready || reason != "migrations_behind" {
	t.Fatalf("0004 ready %v reason %s", ready, reason)
}

execSQL(t, pool, readMigration(t, "0005_venue_catalogue.sql"))
execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0005')`)
ready, reason = pool.Check(ctx)
if !ready || reason != "ok" {
	t.Fatalf("0005 ready %v reason %s", ready, reason)
}
```

Add `select venue_name_key('Bar!')` to the statement list in `TestDataAPIClosed`, beside the other venue statements. Leave this statement unchanged:

```sql
insert into venues (name, location) values ('nope', ST_MakePoint(0, 0)::geography)
```

PostgreSQL checks `INSERT` privilege before `NOT NULL`, so that statement keeps failing with `42501`. If it fails with `23502`, stop and report that result. Do not fill in the missing columns on that statement.

Replace the successful owner insert in `access_test.go` with:

```sql
insert into venues (name, location, address, city, country_code, submitted_by)
values ('Access', ST_MakePoint(0, 0)::geography, '1 Metal Street', 'Oslo', 'NO', 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa')
```

Replace the successful insert in `delete_test.go` with:

```sql
insert into venues (name, location, address, city, country_code, submitted_by)
values ('Room', ST_MakePoint(0, 0)::geography, '1 Metal Street', 'Oslo', 'NO', $1)
```

Create `backend/internal/database/catalogue_test.go`:

```go
package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestVenueCatalogueConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city)
		values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo')`)
	if err == nil {
		t.Fatal("missing country_code accepted")
	}
	for _, code := range []string{"de", "XK", "UK", "ZZ"} {
		_, err = pool.Exec(ctx, `
			insert into venues (name, location, address, city, country_code)
			values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', $1)`, code)
		if err == nil {
			t.Fatalf("country %s accepted", code)
		}
	}
	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code)
		values (' Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')`)
	if err == nil {
		t.Fatal("untrimmed name accepted")
	}
	_, err = pool.Exec(ctx, `
		insert into venues (name, location, address, city, country_code)
		values (E'Rock\ner', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')`)
	if err == nil {
		t.Fatal("name line break accepted")
	}

	var id string
	err = pool.QueryRow(ctx, `
		insert into venues (name, description, location, address, city, country_code)
		values ('Rocker', E'line\nbreak', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')
		returning id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	var genres int
	if err = pool.QueryRow(ctx, `select count(*) from venue_genres where venue_id = $1`, id).Scan(&genres); err != nil || genres != 0 {
		t.Fatalf("genre-less venue %d %v", genres, err)
	}
}

func TestVenueNameKey(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cases := map[string]string{
		"Bar!":        "bar",
		"bar":         "bar",
		"The Bar":     "the bar",
		"Röcker":      "rocker",
		"O'Connor":    "oconnor",
		"black-metal": "blackmetal",
		"bar  one":    "bar one",
		"!!!":         "",
	}
	for input, want := range cases {
		var got string
		if err := pool.QueryRow(ctx, `select venue_name_key($1)`, input).Scan(&got); err != nil || got != want {
			t.Fatalf("%q got %q want %q err %v", input, got, want, err)
		}
	}
}

func TestUpdatedAtTriggerOverwritesSuppliedTime(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var id string
	var created time.Time
	if err := pool.QueryRow(ctx, `
		insert into venues (name, location, address, city, country_code)
		values ('Rocker', ST_MakePoint(10.75, 59.91)::geography, '1 Metal Street', 'Oslo', 'DE')
		returning id::text, updated_at`).Scan(&id, &created); err != nil {
		t.Fatal(err)
	}
	var punk int
	if err := pool.QueryRow(ctx, `select id from genres where name = 'Punk'`).Scan(&punk); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, id, punk); err != nil {
		t.Fatal(err)
	}
	var afterGenre time.Time
	if err := pool.QueryRow(ctx, `select updated_at from venues where id = $1`, id).Scan(&afterGenre); err != nil {
		t.Fatal(err)
	}
	if !afterGenre.Equal(created) {
		t.Fatalf("genre link moved updated_at from %s to %s", created, afterGenre)
	}
	var renamed time.Time
	if err := pool.QueryRow(ctx, `
		update venues set name = 'Renamed' where id = $1 returning updated_at`, id).Scan(&renamed); err != nil {
		t.Fatal(err)
	}
	if !renamed.After(created) {
		t.Fatalf("rename left updated_at at %s", renamed)
	}
	var forced time.Time
	if err := pool.QueryRow(ctx, `
		update venues set updated_at = '2000-01-01T00:00:00Z' where id = $1 returning updated_at`, id).Scan(&forced); err != nil {
		t.Fatal(err)
	}
	if forced.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || !forced.After(renamed) {
		t.Fatalf("supplied updated_at stored as %s", forced)
	}
}

func TestDataAPIStillDenied(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "reset role")
		conn.Release()
	}()
	if _, err = conn.Exec(ctx, `grant usage on schema public to anon`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, `set role anon`); err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, `select venue_name_key('Bar!')`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("name key err %v", err)
	}
}
```

`TestDataAPIClosed` also runs the new `select venue_name_key('Bar!')` statement for `anon`, `authenticated`, and `service_role`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/database ./internal/profile -run 'TestMigrationVersions|TestVenueCatalogueConstraints|TestVenueNameKey|TestUpdatedAtTriggerOverwritesSuppliedTime|TestDataAPIStillDenied|TestDataAPIClosed|TestDeleteUser' -count=1`

Expected: FAIL. `0005_venue_catalogue.sql` is missing, or `venue_name_key` and `country_code` do not exist.

- [ ] **Step 3: Add the migration and raise readiness**

Set `requiredMigrationVersion` in `backend/internal/database/pool.go` to `"0005"`.

Create `infra/supabase/migrations/0005_venue_catalogue.sql` with this SQL. The country list is the 249 officially assigned ISO 3166-1 alpha-2 codes. It excludes `XK`, `UK`, and every reserved code.

```sql
alter table venues rename column country to country_code;

alter table venues add column updated_at timestamptz;
update venues set updated_at = created_at;
alter table venues alter column updated_at set not null;
alter table venues alter column updated_at set default now();

alter table venues alter column address set not null;
alter table venues alter column city set not null;
alter table venues alter column country_code set not null;

alter table venues add constraint venues_name_check check (
  char_length(name) between 1 and 150
  and name = btrim(name)
  and name !~ E'[\n\r]'
);

alter table venues add constraint venues_address_check check (
  char_length(address) between 1 and 500
  and address = btrim(address)
  and address !~ E'[\n\r]'
);

alter table venues add constraint venues_city_check check (
  char_length(city) between 1 and 120
  and city = btrim(city)
  and city !~ E'[\n\r]'
);

alter table venues add constraint venues_description_check check (
  description is null
  or (
    char_length(description) between 1 and 2000
    and description = btrim(description)
  )
);

alter table venues add constraint venues_country_code_check check (country_code in (
  'AD', 'AE', 'AF', 'AG', 'AI', 'AL', 'AM', 'AO', 'AQ', 'AR',
  'AS', 'AT', 'AU', 'AW', 'AX', 'AZ', 'BA', 'BB', 'BD', 'BE',
  'BF', 'BG', 'BH', 'BI', 'BJ', 'BL', 'BM', 'BN', 'BO', 'BQ',
  'BR', 'BS', 'BT', 'BV', 'BW', 'BY', 'BZ', 'CA', 'CC', 'CD',
  'CF', 'CG', 'CH', 'CI', 'CK', 'CL', 'CM', 'CN', 'CO', 'CR',
  'CU', 'CV', 'CW', 'CX', 'CY', 'CZ', 'DE', 'DJ', 'DK', 'DM',
  'DO', 'DZ', 'EC', 'EE', 'EG', 'EH', 'ER', 'ES', 'ET', 'FI',
  'FJ', 'FK', 'FM', 'FO', 'FR', 'GA', 'GB', 'GD', 'GE', 'GF',
  'GG', 'GH', 'GI', 'GL', 'GM', 'GN', 'GP', 'GQ', 'GR', 'GS',
  'GT', 'GU', 'GW', 'GY', 'HK', 'HM', 'HN', 'HR', 'HT', 'HU',
  'ID', 'IE', 'IL', 'IM', 'IN', 'IO', 'IQ', 'IR', 'IS', 'IT',
  'JE', 'JM', 'JO', 'JP', 'KE', 'KG', 'KH', 'KI', 'KM', 'KN',
  'KP', 'KR', 'KW', 'KY', 'KZ', 'LA', 'LB', 'LC', 'LI', 'LK',
  'LR', 'LS', 'LT', 'LU', 'LV', 'LY', 'MA', 'MC', 'MD', 'ME',
  'MF', 'MG', 'MH', 'MK', 'ML', 'MM', 'MN', 'MO', 'MP', 'MQ',
  'MR', 'MS', 'MT', 'MU', 'MV', 'MW', 'MX', 'MY', 'MZ', 'NA',
  'NC', 'NE', 'NF', 'NG', 'NI', 'NL', 'NO', 'NP', 'NR', 'NU',
  'NZ', 'OM', 'PA', 'PE', 'PF', 'PG', 'PH', 'PK', 'PL', 'PM',
  'PN', 'PR', 'PS', 'PT', 'PW', 'PY', 'QA', 'RE', 'RO', 'RS',
  'RU', 'RW', 'SA', 'SB', 'SC', 'SD', 'SE', 'SG', 'SH', 'SI',
  'SJ', 'SK', 'SL', 'SM', 'SN', 'SO', 'SR', 'SS', 'ST', 'SV',
  'SX', 'SY', 'SZ', 'TC', 'TD', 'TF', 'TG', 'TH', 'TJ', 'TK',
  'TL', 'TM', 'TN', 'TO', 'TR', 'TT', 'TV', 'TW', 'TZ', 'UA',
  'UG', 'UM', 'US', 'UY', 'UZ', 'VA', 'VC', 'VE', 'VG', 'VI',
  'VN', 'VU', 'WF', 'WS', 'YE', 'YT', 'ZA', 'ZM', 'ZW'
));

create or replace function venues_set_updated_at() returns trigger
language plpgsql
as $$
begin
  new.updated_at = now();
  return new;
end
$$;

create trigger venues_set_updated_at
before update on venues
for each row
execute function venues_set_updated_at();

create extension if not exists unaccent;

create function venue_name_key(input text) returns text
language sql
stable
set search_path = public
as $$
  select btrim(
    regexp_replace(
      regexp_replace(lower(unaccent(input)), '\p{P}+', '', 'g'),
      '\s+', ' ', 'g'
    )
  )
$$;

revoke execute on function venue_name_key(text) from public, anon, authenticated, service_role;
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/database ./internal/profile -run 'TestMigrationVersions|TestVenueCatalogueConstraints|TestVenueNameKey|TestUpdatedAtTriggerOverwritesSuppliedTime|TestDataAPIStillDenied|TestDataAPIClosed|TestDeleteUser' -count=1`

Expected: PASS. `TestDeleteUser` still retains one venue and clears `submitted_by`.

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0005_venue_catalogue.sql backend/internal/database/pool.go backend/internal/database/postgres_test.go backend/internal/database/catalogue_test.go backend/internal/database/access_test.go backend/internal/profile/delete_test.go
git commit -m "$(cat <<'EOF'
feat: require catalogue venue columns

The public catalogue needs finished address, country, and name-key columns before it can page venues.

EOF
)"
```

### Task 2: Venue Values and Cursors

**Files:**
- Create: `backend/internal/venue/venue.go`
- Create: `backend/internal/venue/venue_test.go`
- Test: `backend/internal/venue/venue_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 at runtime. The cursor key is the text `venue_name_key` returns.
- Produces:

```go
type Genre struct {
	ID   int
	Name string
}

type Venue struct {
	ID                 string
	Name               string
	Description        *string
	Latitude           float64
	Longitude          float64
	Address            string
	City               string
	CountryCode        string
	VenueType          string
	VerificationStatus string
	Genres             []Genre
	AverageRating      *float64
	ReviewCount        int
	CheckInCount       int
}

type Page struct {
	Venues     []Venue
	NextCursor *string
}

var ErrNotFound = errors.New("venue: not found")

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string

func ValidateLimit(limit int) error
func EncodeCursor(nameKey, id string) string
func DecodeCursor(cursor string) (string, string, error)
func CanonicalVenueID(id string) bool
```

`DecodeCursor` returns `&ValidationError{Fields: map[string]string{"cursor": "invalid"}}` for a bad cursor. `ValidateLimit` uses the field message `must be from 1 to 100`. `CanonicalVenueID` accepts only `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`.

- [ ] **Step 1: Write the failing cursor tests**

Create `backend/internal/venue/venue_test.go`:

```go
package venue

import (
	"errors"
	"testing"
)

func TestCursorRoundTrip(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	encoded := EncodeCursor("the bar", id)
	nameKey, gotID, err := DecodeCursor(encoded)
	if err != nil || nameKey != "the bar" || gotID != id {
		t.Fatalf("key %q id %q err %v", nameKey, gotID, err)
	}
	empty := EncodeCursor("", id)
	nameKey, gotID, err = DecodeCursor(empty)
	if err != nil || nameKey != "" || gotID != id {
		t.Fatalf("empty key %q id %q err %v", nameKey, gotID, err)
	}
}

func TestCursorRejectsMalformedValues(t *testing.T) {
	id := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, cursor := range []string{
		"",
		"abc",
		EncodeCursor("the|bar", id),
		EncodeCursor("the bar", "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"),
		EncodeCursor("the bar", "not-a-uuid"),
	} {
		if _, _, err := DecodeCursor(cursor); !errors.As(err, new(*ValidationError)) {
			t.Fatalf("cursor %q err %v", cursor, err)
		}
	}
}

func TestValidateLimit(t *testing.T) {
	if err := ValidateLimit(1); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLimit(100); err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, 101} {
		var validation *ValidationError
		if err := ValidateLimit(limit); !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
			t.Fatalf("limit %d err %v", limit, err)
		}
	}
}

func TestCanonicalVenueID(t *testing.T) {
	if !CanonicalVenueID("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa") {
		t.Fatal("lowercase rejected")
	}
	if CanonicalVenueID("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA") {
		t.Fatal("uppercase accepted")
	}
}
```

The `"the|bar"` case still encodes. `DecodeCursor` must reject it because `strings.Cut` would hide the extra pipe inside the key only when the pipe is first. Encode does not strip pipes. Decode rejects a payload whose remainder after the first `|` is not a canonical UUID, and rejects a key that itself contains `|`. For `"the|bar"`, `Cut` produces key `the` and id `bar|<uuid>`, which fails the UUID check. Assert that this cursor returns `cursor: invalid`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/venue -run 'TestCursorRoundTrip|TestCursorRejectsMalformedValues|TestValidateLimit|TestCanonicalVenueID' -count=1`

Expected: FAIL with `venue` package or functions undefined.

- [ ] **Step 3: Implement the values and cursor**

Create `backend/internal/venue/venue.go`:

```go
package venue

import (
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

type Genre struct {
	ID   int
	Name string
}

type Venue struct {
	ID                 string
	Name               string
	Description        *string
	Latitude           float64
	Longitude          float64
	Address            string
	City               string
	CountryCode        string
	VenueType          string
	VerificationStatus string
	Genres             []Genre
	AverageRating      *float64
	ReviewCount        int
	CheckInCount       int
}

type Page struct {
	Venues     []Venue
	NextCursor *string
}

var ErrNotFound = errors.New("venue: not found")

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string { return "venue: validation failed" }

var canonicalVenueID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func CanonicalVenueID(id string) bool {
	return canonicalVenueID.MatchString(id)
}

func ValidateLimit(limit int) error {
	if limit < 1 || limit > 100 {
		return &ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return nil
}

func EncodeCursor(nameKey, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(nameKey + "|" + id))
}

func DecodeCursor(cursor string) (string, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return "", "", invalid
	}
	nameKey, id, ok := strings.Cut(string(raw), "|")
	if !ok || strings.Contains(nameKey, "|") || strings.Contains(id, "|") || !CanonicalVenueID(id) {
		return "", "", invalid
	}
	return nameKey, id, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/venue -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/venue.go backend/internal/venue/venue_test.go
git commit -m "$(cat <<'EOF'
feat: define venue catalogue values

List and detail need one cursor and one venue value before the SQL reads exist.

EOF
)"
```

### Task 3: Catalogue Reads

**Files:**
- Create: `backend/internal/venue/postgres.go`
- Create: `backend/internal/venue/postgres_test.go`
- Test: `backend/internal/venue/postgres_test.go`

**Interfaces:**
- Consumes: `ValidateLimit`, `EncodeCursor`, `DecodeCursor`, `CanonicalVenueID`, `ErrNotFound`, `ValidationError`, `Genre`, `Venue`, and `Page` from Task 2. `venue_name_key(text)` and the strengthened `venues` columns from Task 1.
- Produces:

```go
func New(pool *pgxpool.Pool) *Service
func (s *Service) ListGenres(ctx context.Context) ([]Genre, error)
func (s *Service) ListVenues(ctx context.Context, limit int, cursor string) (Page, error)
func (s *Service) GetVenue(ctx context.Context, venueID string) (Venue, error)
```

An empty cursor is the first page and is not decoded. A non-empty cursor is decoded. `ListVenues` calls `ValidateLimit` before querying. `GetVenue` returns `venue_id: must be a canonical uuid` for a non-canonical id and `ErrNotFound` when the canonical id is absent, rejected, or has no genre. Each statement uses a two-second timeout. `Genres` on a scanned venue is an empty slice when the aggregate is `[]`, never nil.

- [ ] **Step 1: Write the failing catalogue tests**

Create `backend/internal/venue/postgres_test.go`:

```go
package venue

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func TestListGenresOrderedByName(t *testing.T) {
	service := New(testPool(t))
	genres, err := service.ListGenres(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Black metal", "Classic rock", "Death metal", "Folk metal", "Grunge",
		"Hard rock", "Heavy metal", "Punk", "Thrash metal",
	}
	if len(genres) != len(want) {
		t.Fatalf("genres %d", len(genres))
	}
	for i, name := range want {
		if genres[i].Name != name || genres[i].ID == 0 {
			t.Fatalf("genre %d %+v", i, genres[i])
		}
	}
}

func TestCatalogueMembershipAndAggregates(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	verified := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	pending := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	rejected := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	bare := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	insertVenue(t, pool, verified, "Alpha", "verified", classic, punk)
	insertVenue(t, pool, pending, "Beta", "pending", punk)
	insertVenue(t, pool, rejected, "Gamma", "rejected", punk)
	insertVenue(t, pool, bare, "Delta", "verified")

	service := New(pool)
	page, err := service.ListVenues(ctx, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Venues) != 2 || page.NextCursor != nil {
		t.Fatalf("page %+v", page.Venues)
	}
	if page.Venues[0].ID != verified || page.Venues[1].ID != pending {
		t.Fatalf("order %s %s", page.Venues[0].ID, page.Venues[1].ID)
	}
	if page.Venues[0].Latitude != 60 || page.Venues[0].Longitude != 10 {
		t.Fatalf("point %v %v", page.Venues[0].Latitude, page.Venues[0].Longitude)
	}
	if len(page.Venues[0].Genres) != 2 || page.Venues[0].Genres[0].Name != "Classic rock" || page.Venues[0].Genres[1].Name != "Punk" {
		t.Fatalf("genres %+v", page.Venues[0].Genres)
	}
	for _, got := range page.Venues {
		if got.AverageRating != nil || got.ReviewCount != 0 || got.CheckInCount != 0 {
			t.Fatalf("aggregates %+v", got)
		}
		if got.CountryCode != "DE" || got.Address != "1 Metal Street" {
			t.Fatalf("place %+v", got)
		}
	}
	got, err := service.GetVenue(ctx, verified)
	if err != nil || got.ID != verified || got.VerificationStatus != "verified" {
		t.Fatalf("detail %+v %v", got, err)
	}
	for _, hidden := range []string{rejected, bare, "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"} {
		_, err = service.GetVenue(ctx, hidden)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("hidden %s err %v", hidden, err)
		}
	}
}

func TestVenueWithTwoGenresCountsOnce(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	classic := genreID(t, pool, "Classic rock")
	first := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	second := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	insertVenue(t, pool, first, "Alpha", "verified", classic, punk)
	insertVenue(t, pool, second, "Beta", "pending", punk)
	page, err := New(pool).ListVenues(ctx, 1, "")
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || len(page.Venues[0].Genres) != 2 || page.NextCursor == nil {
		t.Fatalf("page %+v err %v", page, err)
	}
	next, err := New(pool).ListVenues(ctx, 1, *page.NextCursor)
	if err != nil || len(next.Venues) != 1 || next.Venues[0].ID != second || next.NextCursor != nil {
		t.Fatalf("next %+v err %v", next, err)
	}
}

func TestEmptyNameKeyCursorSurvivesDeletion(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	first := "00000000-0000-4000-8000-000000000001"
	second := "00000000-0000-4000-8000-000000000002"
	third := "00000000-0000-4000-8000-000000000003"
	insertVenue(t, pool, first, "!!!", "verified", punk)
	insertVenue(t, pool, second, "???", "verified", punk)
	insertVenue(t, pool, third, "Alpha", "verified", punk)
	service := New(pool)
	page, err := service.ListVenues(ctx, 1, "")
	if err != nil || len(page.Venues) != 1 || page.Venues[0].ID != first || page.NextCursor == nil {
		t.Fatalf("first %+v err %v", page, err)
	}
	if _, err = pool.Exec(ctx, `delete from venues where id = $1`, first); err != nil {
		t.Fatal(err)
	}
	next, err := service.ListVenues(ctx, 1, *page.NextCursor)
	if err != nil || len(next.Venues) != 1 || next.Venues[0].ID != second || next.NextCursor == nil {
		t.Fatalf("second %+v err %v", next, err)
	}
	last, err := service.ListVenues(ctx, 1, *next.NextCursor)
	if err != nil || len(last.Venues) != 1 || last.Venues[0].ID != third || last.NextCursor != nil {
		t.Fatalf("third %+v err %v", last, err)
	}
}

func TestNameSortAndTieBreak(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	punk := genreID(t, pool, "Punk")
	low := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	high := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	article := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	insertVenue(t, pool, high, "Bar!", "verified", punk)
	insertVenue(t, pool, low, "bar", "pending", punk)
	insertVenue(t, pool, article, "The Bar", "verified", punk)
	page, err := New(pool).ListVenues(ctx, 20, "")
	if err != nil || len(page.Venues) != 3 {
		t.Fatal(err)
	}
	if page.Venues[0].ID != low || page.Venues[1].ID != high || page.Venues[2].ID != article {
		t.Fatalf("order %s %s %s", page.Venues[0].ID, page.Venues[1].ID, page.Venues[2].ID)
	}
}

func TestUppercaseVenueIDIsInvalid(t *testing.T) {
	service := New(testPool(t))
	_, err := service.GetVenue(context.Background(), "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA")
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" || errors.Is(err, ErrNotFound) {
		t.Fatalf("err %v", err)
	}
	_, err = service.ListVenues(context.Background(), 0, "")
	if !errors.As(err, &validation) || validation.Fields["limit"] != "must be from 1 to 100" {
		t.Fatalf("limit err %v", err)
	}
	_, err = service.ListVenues(context.Background(), 20, "abc")
	if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
		t.Fatalf("cursor err %v", err)
	}
}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func genreID(t *testing.T, pool *pgxpool.Pool, name string) int {
	t.Helper()
	var id int
	if err := pool.QueryRow(context.Background(), `select id from genres where name = $1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func insertVenue(t *testing.T, pool *pgxpool.Pool, id, name, status string, genreIDs ...int) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type, verification_status
		) values (
			$1, $2, 'A metal bar', ST_MakePoint(10, 60)::geography,
			'1 Metal Street', 'Oslo', 'DE', 'bar', $3
		)`, id, name, status); err != nil {
		t.Fatal(err)
	}
	for _, genreID := range genreIDs {
		if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, id, genreID); err != nil {
			t.Fatal(err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue -run 'TestListGenresOrderedByName|TestCatalogueMembershipAndAggregates|TestVenueWithTwoGenresCountsOnce|TestEmptyNameKeyCursorSurvivesDeletion|TestNameSortAndTieBreak|TestUppercaseVenueIDIsInvalid' -count=1`

Expected: FAIL with `New` undefined.

- [ ] **Step 3: Implement the reads**

Create `backend/internal/venue/postgres.go`:

```go
package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) ListGenres(ctx context.Context) ([]Genre, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, `select id, name from genres order by name, id`)
	if err != nil {
		return nil, fmt.Errorf("venue: genres: %w", err)
	}
	defer rows.Close()
	genres := []Genre{}
	for rows.Next() {
		var genre Genre
		if err := rows.Scan(&genre.ID, &genre.Name); err != nil {
			return nil, fmt.Errorf("venue: genres: %w", err)
		}
		genres = append(genres, genre)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("venue: genres: %w", err)
	}
	return genres, nil
}

func (s *Service) ListVenues(ctx context.Context, limit int, cursor string) (Page, error) {
	if err := ValidateLimit(limit); err != nil {
		return Page{}, err
	}
	nameKey := ""
	id := "00000000-0000-4000-8000-000000000000"
	firstPage := cursor == ""
	if !firstPage {
		var err error
		nameKey, id, err = DecodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, venueSelect+`
		where v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres link where link.venue_id = v.id)
		  and ($1::bool or (venue_name_key(v.name), v.id) > ($2::text, $3::uuid))
		order by venue_name_key(v.name), v.id
		limit $4`, firstPage, nameKey, id, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	defer rows.Close()
	venues := []Venue{}
	keys := []string{}
	for rows.Next() {
		got, key, err := scanVenue(rows)
		if err != nil {
			return Page{}, err
		}
		venues = append(venues, got)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("venue: list: %w", err)
	}
	var next *string
	if len(venues) > limit {
		venues = venues[:limit]
		keys = keys[:limit]
		encoded := EncodeCursor(keys[len(keys)-1], venues[len(venues)-1].ID)
		next = &encoded
	}
	return Page{Venues: venues, NextCursor: next}, nil
}

func (s *Service) GetVenue(ctx context.Context, venueID string) (Venue, error) {
	if !CanonicalVenueID(venueID) {
		return Venue{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	row := s.pool.QueryRow(queryCtx, venueSelect+`
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres link where link.venue_id = v.id)`, venueID)
	got, _, err := scanVenue(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Venue{}, ErrNotFound
	}
	if err != nil {
		return Venue{}, err
	}
	return got, nil
}

const venueSelect = `
	select v.id::text, v.name, v.description,
		ST_Y(v.location::geometry), ST_X(v.location::geometry),
		v.address, v.city, v.country_code, v.venue_type, v.verification_status,
		coalesce((
			select json_agg(json_build_object('id', g.id, 'name', g.name) order by g.name, g.id)
			from venue_genres vg
			join genres g on g.id = vg.genre_id
			where vg.venue_id = v.id
		), '[]'::json),
		null::float8, 0, 0,
		venue_name_key(v.name)
	from venues v`

type venueScanner interface {
	Scan(dest ...any) error
}

func scanVenue(row venueScanner) (Venue, string, error) {
	var got Venue
	var genres []byte
	var nameKey string
	err := row.Scan(
		&got.ID, &got.Name, &got.Description,
		&got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.AverageRating, &got.ReviewCount, &got.CheckInCount, &nameKey,
	)
	if err != nil {
		return Venue{}, "", err
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Venue{}, "", fmt.Errorf("venue: genres: %w", err)
	}
	return got, nameKey, nil
}
```

The list predicate uses the venue as the limited row. The genre aggregate is a scalar subquery, so two genre links do not multiply the page.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/venue -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/postgres.go backend/internal/venue/postgres_test.go
git commit -m "$(cat <<'EOF'
feat: read the public venue catalogue

Android can page verified and pending venues only when membership, sort, and detail share one query.

EOF
)"
```

### Task 4: Public HTTP Routes

**Files:**
- Create: `backend/internal/api/venue.go`
- Create: `backend/internal/api/venue_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/api/venue_test.go`

**Interfaces:**
- Consumes: `venue.New`, `venue.Genre`, `venue.Venue`, `venue.Page`, `venue.ErrNotFound`, and `venue.ValidationError`.
- Produces: `Catalogue` with `ListGenres`, `ListVenues`, and `GetVenue`. Routes `GET /v1/genres`, `GET /v1/venues`, and `GET /v1/venues/{venueID}`, registered without `requireApp` or `requireOperator`. `cmd/api` passes `venue.New(pool.Raw())` as `Options.Catalogue`.

- [ ] **Step 1: Write the failing HTTP tests**

Create `backend/internal/api/venue_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/metalhops/metalhops/backend/internal/venue"
)

func TestVenueRoutes(t *testing.T) {
	cursor := "next-page"
	description := "A metal bar"
	catalogue := &fakeCatalogue{
		genres: []venue.Genre{{ID: 2, Name: "Punk"}, {ID: 1, Name: "Classic rock"}},
		page: venue.Page{Venues: []venue.Venue{{
			ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Description: &description,
			Latitude: 60, Longitude: 10, Address: "1 Metal Street", City: "Oslo",
			CountryCode: "DE", VenueType: "bar", VerificationStatus: "pending",
			Genres: []venue.Genre{{ID: 1, Name: "Classic rock"}},
		}}, NextCursor: &cursor},
		venue: venue.Venue{
			ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Alpha", Description: &description,
			Latitude: 60, Longitude: 10, Address: "1 Metal Street", City: "Oslo",
			CountryCode: "DE", VenueType: "bar", VerificationStatus: "verified",
			Genres: []venue.Genre{},
		},
	}
	handler := venueHandler(catalogue)
	genres := requestVenue(handler, http.MethodGet, "/v1/genres", "Bearer secret")
	if genres.Code != http.StatusOK || catalogue.genreCalls != 1 || bytes.Contains(genres.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("genres %d %s", genres.Code, genres.Body.String())
	}
	if !bytes.Contains(genres.Body.Bytes(), []byte(`"Punk"`)) || bytes.Index(genres.Body.Bytes(), []byte(`"Punk"`)) > bytes.Index(genres.Body.Bytes(), []byte(`"Classic rock"`)) {
		t.Fatal("handler reordered genres")
	}
	list := requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if list.Code != http.StatusOK || catalogue.limit != 20 || catalogue.cursor != "" || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next-page"`)) {
		t.Fatalf("list %d limit %d cursor %q body %s", list.Code, catalogue.limit, catalogue.cursor, list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"average_rating":null`)) || bytes.Contains(list.Body.Bytes(), []byte(`"submitted_by"`)) {
		t.Fatalf("document %s", list.Body.String())
	}
	detail := requestVenue(handler, http.MethodGet, "/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "")
	if detail.Code != http.StatusOK || catalogue.id != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || bytes.Contains(detail.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
}

func TestVenueQueryValidation(t *testing.T) {
	catalogue := &fakeCatalogue{}
	handler := venueHandler(catalogue)
	cases := []string{
		"/v1/venues?limit=nope",
		"/v1/venues?limit=0",
		"/v1/venues?limit=1&limit=2",
		"/v1/venues?country_code=DE",
		"/v1/genres?limit=20",
		"/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?cursor=abc",
	}
	for _, path := range cases {
		before := catalogue.listCalls + catalogue.getCalls + catalogue.genreCalls
		rec := requestVenue(handler, http.MethodGet, path, "")
		if rec.Code != http.StatusBadRequest || !bytes.Contains(rec.Body.Bytes(), []byte(`"validation_error"`)) {
			t.Fatalf("%s %d %s", path, rec.Code, rec.Body.String())
		}
		if path == "/v1/venues?limit=0" {
			if catalogue.listCalls != before+1 {
				t.Fatal("range limit did not reach the catalogue")
			}
			continue
		}
		if catalogue.listCalls+catalogue.getCalls+catalogue.genreCalls != before {
			t.Fatalf("%s reached the catalogue", path)
		}
	}
	badID := requestVenue(handler, http.MethodGet, "/v1/venues/AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "")
	if badID.Code != http.StatusBadRequest || !bytes.Contains(badID.Body.Bytes(), []byte(`"venue_id"`)) || catalogue.getCalls != 1 {
		t.Fatalf("uppercase %d calls %d %s", badID.Code, catalogue.getCalls, badID.Body.String())
	}
	catalogue.getErr = venue.ErrNotFound
	missing := requestVenue(handler, http.MethodGet, "/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "")
	if missing.Code != http.StatusNotFound || !bytes.Contains(missing.Body.Bytes(), []byte(`"venue_not_found"`)) {
		t.Fatalf("missing %d %s", missing.Code, missing.Body.String())
	}
	catalogue.listErr = errors.New("db down")
	failed := requestVenue(handler, http.MethodGet, "/v1/venues", "")
	if failed.Code != http.StatusInternalServerError || !bytes.Contains(failed.Body.Bytes(), []byte(`"internal_error"`)) {
		t.Fatalf("internal %d %s", failed.Code, failed.Body.String())
	}
}

func venueHandler(catalogue *fakeCatalogue) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Catalogue:   catalogue,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func requestVenue(handler http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type fakeCatalogue struct {
	genres     []venue.Genre
	page       venue.Page
	venue      venue.Venue
	limit      int
	cursor     string
	id         string
	genreCalls int
	listCalls  int
	getCalls   int
	listErr    error
	getErr     error
}

func (f *fakeCatalogue) ListGenres(context.Context) ([]venue.Genre, error) {
	f.genreCalls++
	return f.genres, nil
}

func (f *fakeCatalogue) ListVenues(_ context.Context, limit int, cursor string) (venue.Page, error) {
	f.listCalls++
	f.limit = limit
	f.cursor = cursor
	if limit < 1 || limit > 100 {
		return venue.Page{}, &venue.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	if f.listErr != nil {
		return venue.Page{}, f.listErr
	}
	return f.page, nil
}

func (f *fakeCatalogue) GetVenue(_ context.Context, id string) (venue.Venue, error) {
	f.getCalls++
	f.id = id
	if !venue.CanonicalVenueID(id) {
		return venue.Venue{}, &venue.ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	if f.getErr != nil {
		return venue.Venue{}, f.getErr
	}
	return f.venue, nil
}

```

`limit=0` must call `ListVenues`. Every other validation path in that table must not call the catalogue. The uppercase id must call `GetVenue`, and the fake applies `CanonicalVenueID`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api -run 'TestVenueRoutes|TestVenueQueryValidation' -count=1`

Expected: FAIL with `Catalogue` or the routes missing.

- [ ] **Step 3: Implement the adapters and register the routes**

Create `backend/internal/api/venue.go`:

```go
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/metalhops/metalhops/backend/internal/venue"
)

type Catalogue interface {
	ListGenres(context.Context) ([]venue.Genre, error)
	ListVenues(context.Context, int, string) (venue.Page, error)
	GetVenue(context.Context, string) (venue.Venue, error)
}

type genreView struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type venueView struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	VerificationStatus string      `json:"verification_status"`
	Genres             []genreView `json:"genres"`
	AverageRating      *float64    `json:"average_rating"`
	ReviewCount        int         `json:"review_count"`
	CheckInCount       int         `json:"check_in_count"`
}

func listGenres(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	genres, err := catalogue.ListGenres(r.Context())
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	views := make([]genreView, 0, len(genres))
	for _, genre := range genres {
		views = append(views, genreView{ID: genre.ID, Name: genre.Name})
	}
	WriteData(w, http.StatusOK, views)
}

func listVenues(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, map[string]bool{"limit": true, "cursor": true}); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	limit, err := parseVenueLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	page, err := catalogue.ListVenues(r.Context(), limit, r.URL.Query().Get("cursor"))
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

func getVenue(w http.ResponseWriter, r *http.Request, catalogue Catalogue, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	got, err := catalogue.GetVenue(r.Context(), r.PathValue("venueID"))
	if err != nil {
		writeVenueError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, venueJSON(got))
}

func rejectedQuery(r *http.Request, allowed map[string]bool) map[string]string {
	fields := map[string]string{}
	for key, values := range r.URL.Query() {
		if allowed == nil || !allowed[key] {
			fields[key] = "is not accepted"
			continue
		}
		if len(values) > 1 {
			fields[key] = "must be provided once"
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func parseVenueLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, &venue.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}}
	}
	return n, nil
}

func venueJSON(got venue.Venue) venueView {
	genres := make([]genreView, 0, len(got.Genres))
	for _, genre := range got.Genres {
		genres = append(genres, genreView{ID: genre.ID, Name: genre.Name})
	}
	return venueView{
		ID: got.ID, Name: got.Name, Description: got.Description,
		Latitude: got.Latitude, Longitude: got.Longitude,
		Address: got.Address, City: got.City, CountryCode: got.CountryCode,
		VenueType: got.VenueType, VerificationStatus: got.VerificationStatus,
		Genres: genres, AverageRating: got.AverageRating,
		ReviewCount: got.ReviewCount, CheckInCount: got.CheckInCount,
	}
}

func writeVenueError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	var validation *venue.ValidationError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), validation.Fields)
	case errors.Is(err, venue.ErrNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", requestID(r.Context()), nil)
	default:
		logger.Error("venue", "err", err, "request_id", requestID(r.Context()))
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", requestID(r.Context()), nil)
	}
}
```

Add `Catalogue Catalogue` to `Options`. Inside `New`, before the protected profile routes, register:

```go
mux.HandleFunc("GET /v1/genres", func(w http.ResponseWriter, r *http.Request) {
	listGenres(w, r, options.Catalogue, options.Logger)
})
mux.HandleFunc("GET /v1/venues", func(w http.ResponseWriter, r *http.Request) {
	listVenues(w, r, options.Catalogue, options.Logger)
})
mux.HandleFunc("GET /v1/venues/{venueID}", func(w http.ResponseWriter, r *http.Request) {
	getVenue(w, r, options.Catalogue, options.Logger)
})
```

Do not wrap these handlers in `requireApp` or `requireOperator`.

In `backend/cmd/api/main.go`, import `github.com/metalhops/metalhops/backend/internal/venue`, construct `venues := venue.New(pool.Raw())`, and set `Catalogue: venues` on `api.Options`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api ./internal/venue ./cmd/api -count=1 && go build -o /dev/null ./cmd/api`

Expected: PASS, and the build succeeds.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/venue.go backend/internal/api/venue_test.go backend/internal/api/server.go backend/cmd/api/main.go
git commit -m "$(cat <<'EOF'
feat: expose public venue catalogue routes

The genre list, catalogue page, and venue detail are readable without a bearer token.

EOF
)"
```

### Task 5: Contract and Session Closeout

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `DevSteps.md`
- Test: `backend/contract_test.go`

**Interfaces:**
- Consumes: the three routes, `venue_not_found`, and the Venue document from Task 4. Readiness `0005` from Task 1.
- Produces: an OpenAPI document for those routes with no security requirement, and DevSteps Session 7 marked done.

- [ ] **Step 1: Write the failing contract test**

Add these strings to the `TestOpenAPIDocument` list:

```go
"/v1/genres",
"/v1/venues",
"/v1/venues/{venueID}",
"listGenres",
"listVenues",
"getVenue",
"venue_not_found",
"verification_status",
"country_code",
"average_rating",
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test . -run TestOpenAPIDocument -count=1`

Expected: FAIL with the first missing string.

- [ ] **Step 3: Update the contract and docs**

Insert these paths immediately before `/v1/me:` in `backend/openapi.yaml`. Do not add a `security` key. The existing contract test rejects `security:` anywhere before `/v1/me`.

```yaml
  /v1/genres:
    get:
      operationId: listGenres
      summary: List genres
      responses:
        "200":
          description: Every genre, ordered by name.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/GenreListResponse"
              example:
                data:
                  - id: 1
                    name: Black metal
        "400":
          description: The query has a parameter.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              example:
                error:
                  code: validation_error
                  message: The request is invalid.
                  request_id: abc
                  fields:
                    limit: is not accepted
        "429":
          $ref: "#/components/responses/RateLimited"
        "500":
          $ref: "#/components/responses/Internal"
  /v1/venues:
    get:
      operationId: listVenues
      summary: List the public venue catalogue
      parameters:
        - name: limit
          in: query
          required: false
          schema:
            type: integer
            minimum: 1
            maximum: 100
            default: 20
        - name: cursor
          in: query
          required: false
          schema:
            type: string
      responses:
        "200":
          description: One page of public venues.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/VenueListResponse"
              example:
                data:
                  - id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa
                    name: Alpha
                    description: A metal bar
                    latitude: 59.91
                    longitude: 10.75
                    address: 1 Metal Street
                    city: Oslo
                    country_code: DE
                    venue_type: bar
                    verification_status: pending
                    genres:
                      - id: 1
                        name: Classic rock
                    average_rating: null
                    review_count: 0
                    check_in_count: 0
                meta:
                  next_cursor: null
        "400":
          description: The limit or cursor is invalid, or another parameter was sent.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              example:
                error:
                  code: validation_error
                  message: The request is invalid.
                  request_id: abc
                  fields:
                    country_code: is not accepted
        "429":
          $ref: "#/components/responses/RateLimited"
        "500":
          $ref: "#/components/responses/Internal"
  /v1/venues/{venueID}:
    get:
      operationId: getVenue
      summary: Read one public venue
      parameters:
        - name: venueID
          in: path
          required: true
          schema:
            type: string
            format: uuid
      responses:
        "200":
          description: The public venue.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/VenueResponse"
        "400":
          description: The id is not a canonical UUID, or a query parameter was sent.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              example:
                error:
                  code: validation_error
                  message: The request is invalid.
                  request_id: abc
                  fields:
                    venue_id: must be a canonical uuid
        "404":
          description: The venue is not in the public catalogue.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              example:
                error:
                  code: venue_not_found
                  message: Venue not found.
                  request_id: abc
        "429":
          $ref: "#/components/responses/RateLimited"
        "500":
          $ref: "#/components/responses/Internal"
```

Add these schemas beside `Profile`:

```yaml
    Genre:
      type: object
      additionalProperties: false
      required: [id, name]
      properties:
        id:
          type: integer
        name:
          type: string
    GenreListResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data:
          type: array
          items:
            $ref: "#/components/schemas/Genre"
    PublicVenue:
      type: object
      additionalProperties: false
      required:
        - id
        - name
        - description
        - latitude
        - longitude
        - address
        - city
        - country_code
        - venue_type
        - verification_status
        - genres
        - average_rating
        - review_count
        - check_in_count
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        description:
          type: [string, "null"]
        latitude:
          type: number
        longitude:
          type: number
        address:
          type: string
        city:
          type: string
        country_code:
          type: string
        venue_type:
          type: string
          enum: [bar, pub, club]
        verification_status:
          type: string
          enum: [pending, verified]
        genres:
          type: array
          items:
            $ref: "#/components/schemas/Genre"
        average_rating:
          type: [number, "null"]
        review_count:
          type: integer
        check_in_count:
          type: integer
    VenueResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data:
          $ref: "#/components/schemas/PublicVenue"
    VenueListResponse:
      type: object
      additionalProperties: false
      required: [data, meta]
      properties:
        data:
          type: array
          items:
            $ref: "#/components/schemas/PublicVenue"
        meta:
          type: object
          additionalProperties: false
          required: [next_cursor]
          properties:
            next_cursor:
              type: [string, "null"]
```

In `backend/AGENTS.md`, add `- internal/venue: Genre list and public Venue catalogue reads.` to the layout list. Change the readiness sentence from migration `0004` to migration `0005`.

In `DevSteps.md`, change the header status to `Sessions 1–7 are done. Session 8 is next. Sessions 9–14 are not started.`

Replace the Venue-fields strengthening paragraph with:

```markdown
Migration `0005` strengthens the nullable initial columns and country-code
constraint without editing `0001_init.sql`, and it adds `updated_at`. Every
content change updates `updated_at`. The moderation lifecycle migration adds
`status_changed_at`; every moderation status transition updates both
timestamps.
```

Replace the Session 7 section with:

```markdown
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

Migration `0005` requires address, city, and `country_code`, adds
`updated_at` and `venue_name_key`, and raises readiness to `0005`.

**Exit condition:** Android can render a paginated venue catalogue and venue
detail screen.
```

Replace the Session 9 migration sentence with:

```markdown
Duplicate detection uses `venue_name_key` from migration `0005`. Add any
further migration required for submission quotas, duplicate locking, and RLS
read policies. Do not repeat the `0005` column, country-code, or `updated_at`
work.
```

- [ ] **Step 4: Run the full backend checks**

Run: `gofmt -l . && go vet ./... && go test ./...`

Expected: `gofmt` prints nothing. `go vet` and `go test` pass.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md DevSteps.md
git commit -m "$(cat <<'EOF'
docs: publish the venue catalogue contract

Session 7 is usable only when the OpenAPI document and roadmap name the public catalogue routes.

EOF
)"
```
