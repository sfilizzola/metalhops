# Session 9 Venue Submissions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a signed-in User submit a Venue, read their own submissions, and correct a pending submission, without strong duplicates and within 10 new submissions per rolling 24 hours.

**Architecture:** `internal/venue` owns validation, the caller's reads, duplicate detection, the quota, and both write transactions. Before each write it commits an expired-suspension restoration through `(*profile.Service).NormalizeExpiry`, then opens one venue transaction that locks the Profile through `profile.RequireContributor`, checks Genres, and, inside `idempotency.Execute` for `POST`, takes the quota lock, the normalized-name lock, checks matches within 100 metres, reserves a write token, and writes the Venue and its Genre links. `internal/api` adds a second interface, `Submissions`, with the PostgreSQL service and an HTTP-test fake as its two adapters. Migration `0007` adds indexes only.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL 16, PostGIS 3.5, `unaccent`, `golang.org/x/text/unicode/norm`, OpenAPI 3.1, `httptest`, and `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-20-session-9-venue-submissions-design.md`

## Global Constraints

- Follow DevSteps and `docs/architecture/backend.md`.
- Keep `cmd/api` as composition only. Extend `internal/venue`, `internal/profile`, `internal/api`, and `internal/database`.
- Do not edit migrations `0001` through `0006`. Add `infra/supabase/migrations/0007_venue_submissions.sql`, indexes only.
- Add no Go dependency.
- The API process never applies migrations. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- `GET /v1/genres`, `GET /v1/venues`, and `GET /v1/venues/{venueID}` keep their contracts. `PublicVenue.verification_status` stays `pending` or `verified`.
- Duplicate detection calls the SQL function `venue_name_key`. No stored name-key column.
- Venue writes and Profile writes share the one `*ratelimit.UserLimiter` built in `cmd/api`.
- Idempotency operation name: `venue_submission`. Only `POST /v1/venues` requires `Idempotency-Key`; `PUT /v1/me/venues/{venueID}` ignores it.
- Quota: 10 rows per `submitted_by` with `created_at` strictly after `now - 24h`, every status. Message `The submission limit has been reached.`
- Duplicate: equal `venue_name_key`, status `pending` or `verified`, `ST_DWithin(..., 100)` on geography. Message `A matching venue already exists.`
- Not pending: `409 venue_not_pending`. A verified Venue uses `This venue can no longer be corrected.` A rejected Venue Submission uses `Only a pending venue can be corrected.` Session 10 replaces the rejected branch with a correction that sets the submission back to pending and clears Rejection Feedback.
- Advisory lock keys: `hashtextextended('venue_submissions' || chr(31) || userID, 0)` and `hashtextextended('venue_name' || chr(31) || nameKey, 0)`.
- Reads and the venue transaction each use a two-second timeout.
- Work on `feature/session-9-venue-submissions`. Merge into `dev` by pull request.
- Commands run from `backend/` with plain `go` and `git` unless a step says otherwise.

## Review Focus

1. **Contract guard trips on the new `POST`:** `backend/contract_test.go` asserts that no `security:` appears before the first `/v1/me` in `openapi.yaml`. `/v1/venues` sits before `/v1/me`, so `POST /v1/venues` with `bearerAuth` fails that guard even though the operational routes stay public. Task 9 moves the guard to the text before `/v1/genres`, which still covers `/healthz` and `/readyz`, and checks bearer security on each new operation.
2. **`Error.details` rejects duplicate details:** the schema has `additionalProperties: false` and `required: [suspended_until]`, so `duplicate_venue` details would not validate. Task 9 adds `venue_id` and `verification_status` and drops the `required` list. `profile_suspended` without an expiry already omits `details`.
3. **`NULL` self id in the duplicate query:** `POST` passes no venue to exclude. `v.id <> NULL` is `NULL`, which would drop every candidate and let every duplicate through. Task 5 uses `v.id is distinct from $5::uuid`, and `TestSubmitDuplicateRules` fails if any `POST` duplicate passes.
4. **Lock and read in one statement:** under read committed, a statement's snapshot is taken when it starts. If the advisory lock and the match query were one statement, the second writer could read a snapshot from before the first writer's commit. Task 5 runs the lock and the read as separate statements, and `TestSubmitConcurrentSameName` pins one winner.
5. **Two clocks on one row:** insert sets `created_at` and `updated_at` from the Go clock, while a correction's `updated_at` comes from the `venues_set_updated_at` trigger (`now()` in the database). A fixed test clock later than the container's real time makes "correction increases `updated_at`" fail. Tasks 4 to 6 use `2020-01-15T12:00:00Z`.
6. **Replay after a correction:** a repeated `POST` must return the stored creation body, not the corrected row. Task 6 `TestCorrectThenReplay` pins it.
7. **Missing latitude is not zero:** an omitted `latitude` must be a field error, not a point at `0`. Task 3 decodes coordinates as pointers and pins the missing case.

---

## File Structure

New files:

```text
infra/supabase/migrations/0007_venue_submissions.sql
backend/internal/venue/submission.go        # RawSubmission, Draft, ParseSubmission, Submission, commands, errors, pure helpers
backend/internal/venue/submission_test.go   # unit tests for validation and helpers
backend/internal/venue/own.go               # ListOwn, GetOwn, submission columns, own cursor
backend/internal/venue/own_test.go          # cursor unit tests and PostGIS read tests
backend/internal/venue/submit.go            # Submit, Correct, and their SQL
backend/internal/venue/submit_test.go       # PostGIS write tests and shared harness
backend/internal/api/submission.go          # Submissions interface, routes, error mapping
backend/internal/api/submission_test.go     # HTTP tests with a fake adapter
```

Focused modifications:

```text
backend/internal/database/pool.go
backend/internal/database/postgres_test.go
backend/internal/profile/postgres.go
backend/internal/profile/access.go
backend/internal/profile/report.go
backend/internal/profile/delete.go
backend/internal/venue/postgres.go
backend/internal/venue/postgres_test.go
backend/internal/venue/discovery_test.go
backend/internal/venue/index_test.go
backend/internal/api/server.go
backend/internal/api/profile.go
backend/cmd/api/main.go
backend/openapi.yaml
backend/contract_test.go
backend/AGENTS.md
DevSteps.md
```

`pgtest.MigratedURL` applies every SQL file in order, so `0007` loads in every integration test without a harness change. `internal/database/access_test.go` already proves Data API roles have no privilege on `venues`; `0007` grants nothing, so that test needs no change and must keep passing.

---

### Task 1: Submission Indexes and Readiness

**Files:**
- Create: `infra/supabase/migrations/0007_venue_submissions.sql`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go` (the `0006` block at the end of `TestMigrationVersions`)

**Interfaces:**
- Consumes: migrations `0001`–`0006`, `database.Pool.Check`, test helpers `execSQL` and `readMigration`.
- Produces: indexes `venues_submitted_by_created_at_idx` on `(submitted_by, created_at)` and `venues_submitted_by_updated_at_idx` on `(submitted_by, updated_at desc, id desc)`; readiness version `0007`.

- [ ] **Step 1: Write the failing readiness test**

In `backend/internal/database/postgres_test.go`, inside `TestMigrationVersions`, replace:

```go
	execSQL(t, pool, readMigration(t, "0006_venue_discovery.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0006')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0006 ready %v reason %s", ready, reason)
	}
```

with:

```go
	execSQL(t, pool, readMigration(t, "0006_venue_discovery.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0006')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0006 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0007_venue_submissions.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0007')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0007 ready %v reason %s", ready, reason)
	}
```

Leave the `Old Bar` backfill assertions after it unchanged.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/database -run TestMigrationVersions -v`
Expected: FAIL with `0006 ready true reason ok`. If Docker is unavailable locally the test skips; CI treats it as blocking.

- [ ] **Step 3: Add the migration**

Create `infra/supabase/migrations/0007_venue_submissions.sql`:

```sql
-- Venue submissions: quota count and the Submitter's list.
create index venues_submitted_by_created_at_idx on venues (submitted_by, created_at);
create index venues_submitted_by_updated_at_idx on venues (submitted_by, updated_at desc, id desc);
```

- [ ] **Step 4: Raise readiness**

In `backend/internal/database/pool.go`, change:

```go
const requiredMigrationVersion = "0006"
```

to:

```go
const requiredMigrationVersion = "0007"
```

- [ ] **Step 5: Run the database tests**

Run: `go test ./internal/database -v`
Expected: PASS, including `TestMigrationVersions` and `TestDataAPIClosed`.

- [ ] **Step 6: Commit**

```bash
git add infra/supabase/migrations/0007_venue_submissions.sql backend/internal/database/pool.go backend/internal/database/postgres_test.go
git commit -m "feat: add venue submission indexes"
```

---

### Task 2: Profile Contribution Helpers

**Files:**
- Modify: `backend/internal/profile/postgres.go:35,54,169`
- Modify: `backend/internal/profile/delete.go:11`
- Modify: `backend/internal/profile/report.go:59-83`
- Modify: `backend/internal/profile/access.go`

**Interfaces:**
- Consumes: `CommunityAccess`, `normalizeExpiry`.
- Produces:
  - `func (s *Service) NormalizeExpiry(ctx context.Context, userID string) error`: the former private `commitExpiry`, unchanged. It commits an expired-suspension restoration in its own transaction. A missing Profile is a nil no-op.
  - `func RequireContributor(ctx context.Context, tx pgx.Tx, userID string) error`: locks the Profile row with `for update` for the rest of `tx` and returns `ErrProfileRequired`, `ErrRenameRequired`, `*SuspendedError`, a wrapped database error, or nil.

This task changes no behavior. The existing Profile tests are its test surface.

- [ ] **Step 1: Export the expiry commit**

In `backend/internal/profile/postgres.go`, rename the method declaration:

```go
func (s *Service) commitExpiry(ctx context.Context, userID string) error {
```

to:

```go
func (s *Service) NormalizeExpiry(ctx context.Context, userID string) error {
```

Replace each call `s.commitExpiry(` with `s.NormalizeExpiry(` in `postgres.go` (lines 35 and 54), `delete.go` (line 11), and `report.go` (line 59). Leave the private `normalizeExpiry(ctx, tx, userID)` unchanged.

- [ ] **Step 2: Add `RequireContributor`**

In `backend/internal/profile/access.go`, replace the import block with:

```go
import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)
```

and append:

```go
// RequireContributor locks the Profile row for the rest of tx.
func RequireContributor(ctx context.Context, tx pgx.Tx, userID string) error {
	var status string
	var until *time.Time
	err := tx.QueryRow(ctx, `
		select status, suspended_until
		from profiles
		where id = $1::uuid
		for update`, userID).Scan(&status, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return CommunityAccess(false, "", nil)
	}
	if err != nil {
		return fmt.Errorf("profile: read: %w", err)
	}
	return CommunityAccess(true, status, until)
}
```

- [ ] **Step 3: Use it in `Report`**

In `backend/internal/profile/report.go`, replace:

```go
	var status string
	var until *time.Time
	err = tx.QueryRow(txCtx, `
		select status, suspended_until
		from profiles
		where id = $1::uuid
		for update`, cmd.ReporterID).Scan(&status, &until)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, CommunityAccess(false, "", nil)
	}
	if err != nil {
		return Report{}, fmt.Errorf("profile: read: %w", err)
	}
	if err := CommunityAccess(true, status, until); err != nil {
		return Report{}, err
	}
```

with:

```go
	if err := RequireContributor(txCtx, tx, cmd.ReporterID); err != nil {
		return Report{}, err
	}
```

The next statement assigns `err` with `=`; keep it. If `go vet` reports an unused import, remove only that import.

- [ ] **Step 4: Run the Profile tests**

Run: `go test ./internal/profile ./internal/api -v`
Expected: PASS. `TestReport` still covers the rename-required, suspended, expired, and missing-Profile reporter.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile
git commit -m "refactor: share the profile contribution check"
```

---

### Task 3: Submission Validation and Pure Rules

**Files:**
- Create: `backend/internal/venue/submission.go`
- Create: `backend/internal/venue/submission_test.go`

**Interfaces:**
- Consumes: `countryCodes`, `parseText`, `ValidationError` from `internal/venue`.
- Produces:

```go
type RawSubmission struct {
	Name        *string
	Description *string
	Latitude    *float64
	Longitude   *float64
	Address     *string
	City        *string
	CountryCode *string
	VenueType   *string
	GenreIDs    []int64
}

type Draft struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Address     string  `json:"address"`
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	VenueType   string  `json:"venue_type"`
	GenreIDs    []int   `json:"genre_ids"`
}

type Submission struct {
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
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SubmitCommand struct {
	UserID string
	Key    string
	Fields RawSubmission
}

type CorrectCommand struct {
	UserID  string
	VenueID string
	Fields  RawSubmission
}

type OwnQuery struct {
	UserID string
	Limit  int
	Cursor string
}

type OwnPage struct {
	Submissions []Submission
	NextCursor  *string
}

type NotPendingError struct{ Status string }

type DuplicateMatch struct {
	VenueID            string
	VerificationStatus string
}

type DuplicateError struct{ Match *DuplicateMatch }
type QuotaError struct{ RetryAfter int }
type RateLimitError struct{ RetryAfter int }

func ParseSubmission(raw RawSubmission) (Draft, error)
```

and private `duplicateCandidate`, `disclose`, `quotaRetryAfter`, `sameSubmission`, `submissionBody`, `bodyOf`, and `(submissionBody).submission`.

`Draft`'s JSON is the idempotency command. Field order and tags are fixed; do not reorder them.

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/venue/submission_test.go`:

```go
package venue

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func validRaw() RawSubmission {
	name, address, city, country, venueType := "  Iron Maiden  ", "1 High Street", "Oslo", "no", "pub"
	lat, lon := 59.91, 10.75
	return RawSubmission{
		Name: &name, Latitude: &lat, Longitude: &lon, Address: &address, City: &city,
		CountryCode: &country, VenueType: &venueType, GenreIDs: []int64{4, 1, 4},
	}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("err %v", err)
	}
	return validation.Fields
}

func TestParseSubmissionNormalizes(t *testing.T) {
	got, err := ParseSubmission(validRaw())
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Iron Maiden" || got.CountryCode != "NO" || got.Description != nil || len(got.GenreIDs) != 2 || got.GenreIDs[0] != 1 || got.GenreIDs[1] != 4 {
		t.Fatalf("draft %+v", got)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"name":"Iron Maiden","description":null,"latitude":59.91,"longitude":10.75,"address":"1 High Street","city":"Oslo","country_code":"NO","venue_type":"pub","genre_ids":[1,4]}`
	if string(body) != want {
		t.Fatalf("command %s", body)
	}
	for _, code := range []string{"gb", " XK ", "Gb"} {
		raw := validRaw()
		raw.CountryCode = &code
		if got, err := ParseSubmission(raw); err != nil || (got.CountryCode != "GB" && got.CountryCode != "XK") {
			t.Fatalf("%q %+v %v", code, got, err)
		}
	}
	description := " Loud.\nEvery night. "
	raw := validRaw()
	raw.Description = &description
	if got, err := ParseSubmission(raw); err != nil || *got.Description != "Loud.\nEvery night." {
		t.Fatalf("description %+v %v", got, err)
	}
	decomposed := "Cafe\u0301"
	raw = validRaw()
	raw.Name = &decomposed
	if got, err := ParseSubmission(raw); err != nil || got.Name != "Caf\u00e9" {
		t.Fatalf("nfc %q %v", got.Name, err)
	}
}

func TestParseSubmissionMissing(t *testing.T) {
	fields := fieldsOf(t, func() error { _, err := ParseSubmission(RawSubmission{}); return err }())
	want := map[string]string{
		"name":         "must be 1 to 150 characters",
		"address":      "must be 1 to 500 characters",
		"city":         "must be 1 to 120 characters",
		"country_code": "must be an ISO 3166-1 alpha-2 code",
		"venue_type":   "must be bar, pub, or club",
		"latitude":     "must be from -90 to 90",
		"longitude":    "must be from -180 to 180",
		"genre_ids":    "must include a genre",
	}
	if len(fields) != len(want) {
		t.Fatalf("fields %v", fields)
	}
	for name, message := range want {
		if fields[name] != message {
			t.Fatalf("%s %q", name, fields[name])
		}
	}
}

func TestParseSubmissionBoundaries(t *testing.T) {
	str := func(s string) *string { return &s }
	num := func(f float64) *float64 { return &f }
	cases := []struct {
		name    string
		edit    func(*RawSubmission)
		field   string
		message string
	}{
		{"blank name", func(r *RawSubmission) { r.Name = str("   ") }, "name", "must be 1 to 150 characters"},
		{"long name", func(r *RawSubmission) { r.Name = str(strings.Repeat("a", 151)) }, "name", "must be 1 to 150 characters"},
		{"name newline", func(r *RawSubmission) { r.Name = str("Iron\nMaiden") }, "name", "must be a single line of text"},
		{"address cr", func(r *RawSubmission) { r.Address = str("1\rHigh") }, "address", "must be a single line of text"},
		{"long address", func(r *RawSubmission) { r.Address = str(strings.Repeat("a", 501)) }, "address", "must be 1 to 500 characters"},
		{"long city", func(r *RawSubmission) { r.City = str(strings.Repeat("a", 121)) }, "city", "must be 1 to 120 characters"},
		{"blank description", func(r *RawSubmission) { r.Description = str("  ") }, "description", "must be 1 to 2000 characters"},
		{"long description", func(r *RawSubmission) { r.Description = str(strings.Repeat("a", 2001)) }, "description", "must be 1 to 2000 characters"},
		{"null byte description", func(r *RawSubmission) { r.Description = str("a\x00b") }, "description", "must be text"},
		{"bell description", func(r *RawSubmission) { r.Description = str("a\x07b") }, "description", "must be text"},
		{"uk", func(r *RawSubmission) { r.CountryCode = str("UK") }, "country_code", "must be an ISO 3166-1 alpha-2 code"},
		{"type case", func(r *RawSubmission) { r.VenueType = str("Pub") }, "venue_type", "must be bar, pub, or club"},
		{"lat high", func(r *RawSubmission) { r.Latitude = num(90.0001) }, "latitude", "must be from -90 to 90"},
		{"lat low", func(r *RawSubmission) { r.Latitude = num(-90.0001) }, "latitude", "must be from -90 to 90"},
		{"lon high", func(r *RawSubmission) { r.Longitude = num(180.0001) }, "longitude", "must be from -180 to 180"},
		{"empty genres", func(r *RawSubmission) { r.GenreIDs = []int64{} }, "genre_ids", "must include a genre"},
		{"zero genre", func(r *RawSubmission) { r.GenreIDs = []int64{0} }, "genre_ids", "must be a positive integer"},
		{"int4 overflow", func(r *RawSubmission) { r.GenreIDs = []int64{2147483648} }, "genre_ids", "must be a positive integer"},
	}
	for _, c := range cases {
		raw := validRaw()
		c.edit(&raw)
		_, err := ParseSubmission(raw)
		if got := fieldsOf(t, err)[c.field]; got != c.message {
			t.Fatalf("%s: %q", c.name, got)
		}
	}
	for _, edit := range []func(*RawSubmission){
		func(r *RawSubmission) { r.Name = str(strings.Repeat("é", 150)) },
		func(r *RawSubmission) { r.Description = str(strings.Repeat("a", 2000)) },
		func(r *RawSubmission) { r.Latitude, r.Longitude = num(-90), num(180) },
		func(r *RawSubmission) { r.GenreIDs = []int64{2147483647} },
	} {
		raw := validRaw()
		edit(&raw)
		if _, err := ParseSubmission(raw); err != nil {
			t.Fatalf("boundary rejected %v", err)
		}
	}
}

func TestDisclose(t *testing.T) {
	if got := disclose([]duplicateCandidate{{ID: "b", Status: "pending"}}); got != nil {
		t.Fatalf("private pending disclosed %+v", got)
	}
	got := disclose([]duplicateCandidate{{ID: "a", Status: "pending"}, {ID: "b", Status: "verified"}})
	if got == nil || got.VenueID != "b" || got.VerificationStatus != "verified" {
		t.Fatalf("verified %+v", got)
	}
	got = disclose([]duplicateCandidate{{ID: "a", Status: "pending"}, {ID: "c", Status: "pending", Own: true}, {ID: "d", Status: "verified"}})
	if got == nil || got.VenueID != "c" || got.VerificationStatus != "pending" {
		t.Fatalf("own %+v", got)
	}
}

func TestQuotaRetryAfter(t *testing.T) {
	now := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	if got := quotaRetryAfter(now.Add(-23*time.Hour), now); got != 3600 {
		t.Fatalf("hour %d", got)
	}
	if got := quotaRetryAfter(now.Add(-24*time.Hour+500*time.Millisecond), now); got != 1 {
		t.Fatalf("round up %d", got)
	}
	if got := quotaRetryAfter(now.Add(-25*time.Hour), now); got != 1 {
		t.Fatalf("minimum %d", got)
	}
}

func TestSameSubmission(t *testing.T) {
	draft, err := ParseSubmission(validRaw())
	if err != nil {
		t.Fatal(err)
	}
	current := Submission{
		Name: draft.Name, Latitude: draft.Latitude, Longitude: draft.Longitude,
		Address: draft.Address, City: draft.City, CountryCode: draft.CountryCode, VenueType: draft.VenueType,
		Genres: []Genre{{ID: 4, Name: "A"}, {ID: 1, Name: "B"}},
	}
	if !sameSubmission(current, draft) {
		t.Fatal("equal submission differs")
	}
	note := "x"
	for _, edit := range []func(*Submission){
		func(s *Submission) { s.Name = "Other" },
		func(s *Submission) { s.Latitude = 59.9100001 },
		func(s *Submission) { s.Description = &note },
		func(s *Submission) { s.Genres = s.Genres[:1] },
		func(s *Submission) { s.Genres = []Genre{{ID: 4}, {ID: 2}} },
	} {
		changed := current
		changed.Genres = append([]Genre(nil), current.Genres...)
		edit(&changed)
		if sameSubmission(changed, draft) {
			t.Fatalf("change not detected %+v", changed)
		}
	}
}

func TestSubmissionBodyRoundTrip(t *testing.T) {
	description := "Loud"
	in := Submission{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Iron", Description: &description,
		Latitude: 1.5, Longitude: -2.25, Address: "1", City: "Oslo", CountryCode: "NO", VenueType: "pub",
		VerificationStatus: "pending", Genres: []Genre{{ID: 1, Name: "Punk"}},
		CreatedAt: time.Date(2020, 1, 15, 12, 0, 0, 123456000, time.UTC),
		UpdatedAt: time.Date(2020, 1, 15, 12, 0, 1, 0, time.UTC),
	}
	body, err := json.Marshal(bodyOf(in))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"genres":[{"id":1,"name":"Punk"}]`) || !strings.Contains(string(body), `"created_at":"2020-01-15T12:00:00.123456Z"`) {
		t.Fatalf("body %s", body)
	}
	var stored submissionBody
	if err := json.Unmarshal(body, &stored); err != nil {
		t.Fatal(err)
	}
	out := stored.submission()
	if out.ID != in.ID || *out.Description != description || !out.CreatedAt.Equal(in.CreatedAt) || out.Genres[0] != in.Genres[0] {
		t.Fatalf("round trip %+v", out)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue -run 'TestParseSubmission|TestDisclose|TestQuotaRetryAfter|TestSameSubmission|TestSubmissionBodyRoundTrip' -v`
Expected: FAIL to compile with `undefined: RawSubmission`.

- [ ] **Step 3: Implement `submission.go`**

Create `backend/internal/venue/submission.go`:

```go
package venue

import (
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type RawSubmission struct {
	Name        *string
	Description *string
	Latitude    *float64
	Longitude   *float64
	Address     *string
	City        *string
	CountryCode *string
	VenueType   *string
	GenreIDs    []int64
}

// Draft's JSON is the idempotency command for venue_submission.
type Draft struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Address     string  `json:"address"`
	City        string  `json:"city"`
	CountryCode string  `json:"country_code"`
	VenueType   string  `json:"venue_type"`
	GenreIDs    []int   `json:"genre_ids"`
}

type Submission struct {
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
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SubmitCommand struct {
	UserID string
	Key    string
	Fields RawSubmission
}

type CorrectCommand struct {
	UserID  string
	VenueID string
	Fields  RawSubmission
}

type OwnQuery struct {
	UserID string
	Limit  int
	Cursor string
}

type OwnPage struct {
	Submissions []Submission
	NextCursor  *string
}

type NotPendingError struct{ Status string }

func (e *NotPendingError) Error() string { return "venue: not pending" }

var errWritesUnavailable = errors.New("venue: writes are not configured")

type DuplicateMatch struct {
	VenueID            string
	VerificationStatus string
}

type DuplicateError struct{ Match *DuplicateMatch }

func (e *DuplicateError) Error() string { return "venue: duplicate" }

type QuotaError struct{ RetryAfter int }

func (e *QuotaError) Error() string { return "venue: quota exceeded" }

type RateLimitError struct{ RetryAfter int }

func (e *RateLimitError) Error() string { return "venue: rate limited" }

func ParseSubmission(raw RawSubmission) (Draft, error) {
	fields := map[string]string{}
	d := Draft{
		Name:    requiredText(fields, "name", raw.Name, 150),
		Address: requiredText(fields, "address", raw.Address, 500),
		City:    requiredText(fields, "city", raw.City, 120),
	}
	if raw.Description != nil {
		value, failure := parseDescription(*raw.Description)
		if failure != "" {
			fields["description"] = failure
		} else {
			d.Description = &value
		}
	}
	if raw.CountryCode != nil {
		d.CountryCode = strings.ToUpper(strings.TrimSpace(*raw.CountryCode))
	}
	if _, ok := countryCodes[d.CountryCode]; !ok {
		fields["country_code"] = "must be an ISO 3166-1 alpha-2 code"
	}
	if raw.VenueType == nil || (*raw.VenueType != "bar" && *raw.VenueType != "pub" && *raw.VenueType != "club") {
		fields["venue_type"] = "must be bar, pub, or club"
	} else {
		d.VenueType = *raw.VenueType
	}
	if raw.Latitude == nil || *raw.Latitude < -90 || *raw.Latitude > 90 {
		fields["latitude"] = "must be from -90 to 90"
	} else {
		d.Latitude = *raw.Latitude
	}
	if raw.Longitude == nil || *raw.Longitude < -180 || *raw.Longitude > 180 {
		fields["longitude"] = "must be from -180 to 180"
	} else {
		d.Longitude = *raw.Longitude
	}
	d.GenreIDs = parseGenreIDs(fields, raw.GenreIDs)
	if len(fields) > 0 {
		return Draft{}, &ValidationError{Fields: fields}
	}
	return d, nil
}

func requiredText(fields map[string]string, name string, raw *string, max int) string {
	if raw == nil || *raw == "" {
		fields[name] = "must be 1 to " + strconv.Itoa(max) + " characters"
		return ""
	}
	return parseText(fields, name, *raw, max)
}

func parseDescription(raw string) (string, string) {
	if !utf8.ValidString(raw) {
		return "", "must be text"
	}
	for _, r := range raw {
		if r == 0 || (unicode.IsControl(r) && !unicode.IsSpace(r)) {
			return "", "must be text"
		}
	}
	value := norm.NFC.String(strings.TrimSpace(raw))
	if n := utf8.RuneCountInString(value); n < 1 || n > 2000 {
		return "", "must be 1 to 2000 characters"
	}
	return value, ""
}

func parseGenreIDs(fields map[string]string, raw []int64) []int {
	if len(raw) == 0 {
		fields["genre_ids"] = "must include a genre"
		return nil
	}
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id < 1 || id > math.MaxInt32 {
			fields["genre_ids"] = "must be a positive integer"
			return nil
		}
		ids = append(ids, int(id))
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

type duplicateCandidate struct {
	ID     string
	Status string
	Own    bool
}

// disclose expects candidates ordered by distance, then id.
func disclose(candidates []duplicateCandidate) *DuplicateMatch {
	for _, c := range candidates {
		if c.Own || c.Status == "verified" {
			return &DuplicateMatch{VenueID: c.ID, VerificationStatus: c.Status}
		}
	}
	return nil
}

func quotaRetryAfter(oldest, now time.Time) int {
	return max(1, int(math.Ceil(oldest.Add(24*time.Hour).Sub(now).Seconds())))
}

func sameSubmission(current Submission, d Draft) bool {
	if current.Name != d.Name || current.Latitude != d.Latitude || current.Longitude != d.Longitude ||
		current.Address != d.Address || current.City != d.City || current.CountryCode != d.CountryCode ||
		current.VenueType != d.VenueType || (current.Description == nil) != (d.Description == nil) ||
		(current.Description != nil && *current.Description != *d.Description) {
		return false
	}
	ids := make([]int, len(current.Genres))
	for i, genre := range current.Genres {
		ids[i] = genre.ID
	}
	slices.Sort(ids)
	return slices.Equal(ids, d.GenreIDs)
}

type genreBody struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type submissionBody struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	Genres             []genreBody `json:"genres"`
	VerificationStatus string      `json:"verification_status"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

func bodyOf(s Submission) submissionBody {
	genres := make([]genreBody, len(s.Genres))
	for i, genre := range s.Genres {
		genres[i] = genreBody{ID: genre.ID, Name: genre.Name}
	}
	return submissionBody{
		ID: s.ID, Name: s.Name, Description: s.Description, Latitude: s.Latitude, Longitude: s.Longitude,
		Address: s.Address, City: s.City, CountryCode: s.CountryCode, VenueType: s.VenueType,
		Genres: genres, VerificationStatus: s.VerificationStatus,
		CreatedAt: s.CreatedAt.UTC(), UpdatedAt: s.UpdatedAt.UTC(),
	}
}

func (b submissionBody) submission() Submission {
	genres := make([]Genre, len(b.Genres))
	for i, genre := range b.Genres {
		genres[i] = Genre{ID: genre.ID, Name: genre.Name}
	}
	return Submission{
		ID: b.ID, Name: b.Name, Description: b.Description, Latitude: b.Latitude, Longitude: b.Longitude,
		Address: b.Address, City: b.City, CountryCode: b.CountryCode, VenueType: b.VenueType,
		VerificationStatus: b.VerificationStatus, Genres: genres,
		CreatedAt: b.CreatedAt.UTC(), UpdatedAt: b.UpdatedAt.UTC(),
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/venue -run 'TestParseSubmission|TestDisclose|TestQuotaRetryAfter|TestSameSubmission|TestSubmissionBodyRoundTrip|TestParseQuery' -v`
Expected: PASS. `TestParseQuery` still passes because `parseText` is unchanged.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/submission.go backend/internal/venue/submission_test.go
git commit -m "feat: validate venue submissions"
```

---

### Task 4: Submitter Reads

**Files:**
- Create: `backend/internal/venue/own.go`
- Create: `backend/internal/venue/own_test.go`
- Create: `backend/internal/venue/submit_test.go` (shared harness only in this task)
- Modify: `backend/internal/venue/postgres.go:17-23` and `venueSelect`
- Modify: `backend/internal/venue/postgres_test.go`, `discovery_test.go`, `index_test.go` (every `New(pool)`)
- Modify: `backend/cmd/api/main.go:44`

**Interfaces:**
- Consumes: `Submission`, `OwnQuery`, `OwnPage`, `ValidateLimit`, `CanonicalVenueID`, `ErrNotFound`, `venueScanner`; `profile.New`; `ratelimit.NewUserLimiter`.
- Produces:

```go
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time) *Service
func (s *Service) ListOwn(ctx context.Context, q OwnQuery) (OwnPage, error)
func (s *Service) GetOwn(ctx context.Context, userID, venueID string) (Submission, error)

const genresJSON string           // shared Genre aggregate, used by venueSelect and submissionColumns
const submissionColumns string
type queryRower interface{ QueryRow(context.Context, string, ...any) pgx.Row }
func ownSubmission(ctx context.Context, q queryRower, userID, venueID string) (Submission, error)
func scanSubmission(row venueScanner) (Submission, error)
func ownListSQL(after bool) string
func encodeOwnCursor(updated time.Time, id string) string
func decodeOwnCursor(cursor string) (time.Time, string, error)
```

Test helpers in `submit_test.go`, used by Tasks 5 to 7: `submitNow`, `harness`, `newHarness`, `insertUser`, `seedVenue`, `rawVenue`, `tokensLeft`, `userA`, `userB`.

- [ ] **Step 1: Change `New` and every caller**

In `backend/internal/venue/postgres.go`, replace:

```go
type Service struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}
```

with:

```go
type Service struct {
	pool     *pgxpool.Pool
	profiles *profile.Service
	writes   *ratelimit.UserLimiter
	clock    func() time.Time
}

// profiles and writes may be nil for catalogue-only use; writes then fail.
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time) *Service {
	return &Service{pool: pool, profiles: profiles, writes: writes, clock: clock}
}
```

and add imports `"github.com/metalhops/metalhops/backend/internal/profile"` and `"github.com/metalhops/metalhops/backend/internal/ratelimit"`.

In `postgres_test.go`, `discovery_test.go`, and `index_test.go`, replace every `New(pool)` with `New(pool, nil, nil, time.Now)` and add `"time"` to each file's imports.

In `backend/cmd/api/main.go`, replace:

```go
	venues := venue.New(pool.Raw())
```

with:

```go
	venues := venue.New(pool.Raw(), profiles, userLimiter, time.Now)
```

- [ ] **Step 2: Share the Genre aggregate**

In `backend/internal/venue/postgres.go`, add:

```go
const genresJSON = `coalesce((
			select json_agg(
				json_build_object('id', g.id, 'name', g.name)
				order by g.name collate "C", g.id
			)
			from venue_genres vg
			join genres g on g.id = vg.genre_id
			where vg.venue_id = v.id
		), '[]'::json)`
```

and change `venueSelect` so that the aggregate is `genresJSON`:

```go
func venueSelect(distance, tier string) string {
	return `
	select v.id::text, v.name, v.description,
		ST_Y(v.location::geometry), ST_X(v.location::geometry),
		v.address, v.city, v.country_code, v.venue_type, v.verification_status,
		` + genresJSON + `,
		null::float8, 0, 0,
		venue_name_key(v.name), ` + distance + `, ` + tier + `
	from venues v`
}
```

Run: `go test ./internal/venue -v`
Expected: PASS. Catalogue and discovery output is unchanged.

- [ ] **Step 3: Write the shared harness**

Create `backend/internal/venue/submit_test.go`:

```go
package venue

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

// Earlier than any container clock, so trigger-set updated_at is always later.
var submitNow = time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)

const (
	userA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	userB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type harness struct {
	pool    *pgxpool.Pool
	service *Service
	limiter *ratelimit.UserLimiter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := testPool(t)
	clock := func() time.Time { return submitNow }
	limiter := ratelimit.NewUserLimiter(clock)
	return &harness{
		pool:    pool,
		service: New(pool, profile.New(pool, limiter, nil, clock), limiter, clock),
		limiter: limiter,
	}
}

func insertUser(t *testing.T, pool *pgxpool.Pool, id, status string, until *time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, id); err != nil {
		t.Fatal(err)
	}
	if status == "" {
		return
	}
	var reason *string
	if status != "active" {
		value := "spam"
		reason = &value
	}
	if _, err := pool.Exec(ctx, `
		insert into profiles (
			id, username, display_name, status, moderation_reason, suspended_until, created_at, updated_at
		) values ($1, $2, 'Tester', $3, $4, $5, $6, $6)`,
		id, "u"+id[len(id)-12:], status, reason, until, submitNow); err != nil {
		t.Fatal(err)
	}
}

type seed struct {
	id        string
	name      string
	status    string
	submitter *string
	lat, lon  float64
	created   time.Time
	updated   time.Time
	genre     int
}

func seedVenue(t *testing.T, pool *pgxpool.Pool, s seed) {
	t.Helper()
	ctx := context.Background()
	if s.status == "" {
		s.status = "verified"
	}
	if s.created.IsZero() {
		s.created = submitNow.Add(-48 * time.Hour)
	}
	if s.updated.IsZero() {
		s.updated = s.created
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, location, address, city, country_code, venue_type,
			verification_status, submitted_by, created_at, updated_at
		) values (
			$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
			'1 Metal Street', 'Oslo', 'NO', 'pub', $5, $6::uuid, $7, $8
		)`, s.id, s.name, s.lon, s.lat, s.status, s.submitter, s.created, s.updated); err != nil {
		t.Fatal(err)
	}
	if s.genre == 0 {
		s.genre = genreID(t, pool, "Punk")
	}
	if _, err := pool.Exec(ctx, `insert into venue_genres (venue_id, genre_id) values ($1, $2)`, s.id, s.genre); err != nil {
		t.Fatal(err)
	}
}

func rawVenue(name string, lat, lon float64, genres ...int) RawSubmission {
	address, city, country, venueType := "1 Metal Street", "Oslo", "NO", "pub"
	ids := make([]int64, len(genres))
	for i, genre := range genres {
		ids[i] = int64(genre)
	}
	return RawSubmission{
		Name: &name, Latitude: &lat, Longitude: &lon, Address: &address, City: &city,
		CountryCode: &country, VenueType: &venueType, GenreIDs: ids,
	}
}

func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int {
	held := make([]*ratelimit.Reservation, 0, 60)
	for len(held) < 60 {
		reservation, _, ok := limiter.Reserve(userID)
		if !ok {
			break
		}
		held = append(held, reservation)
	}
	for _, reservation := range held {
		reservation.Cancel()
	}
	return len(held)
}

func ptr(s string) *string { return &s }
```

- [ ] **Step 4: Write the failing read tests**

Create `backend/internal/venue/own_test.go`:

```go
package venue

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestOwnCursorRoundTrip(t *testing.T) {
	updated := time.Date(2020, 1, 15, 12, 0, 0, 123456000, time.UTC)
	cursor := encodeOwnCursor(updated, "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	gotTime, gotID, err := decodeOwnCursor(cursor)
	if err != nil || !gotTime.Equal(updated) || gotID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("%s %s %v", gotTime, gotID, err)
	}
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	for _, bad := range []string{
		"",
		"!!!",
		enc("submission-v1|2020-01-15T12:00:00Z"),
		enc("submission-v2|2020-01-15T12:00:00Z|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		enc("submission-v1|yesterday|aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"),
		enc("submission-v1|2020-01-15T12:00:00Z|AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"),
		encodeCursor(modeName, "0123456789abcdef", position{NameKey: "x", ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}),
	} {
		_, _, err := decodeOwnCursor(bad)
		var validation *ValidationError
		if !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
			t.Fatalf("%q err %v", bad, err)
		}
	}
}

func TestListAndGetOwn(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "rename_required", nil)
	insertUser(t, h.pool, userB, "active", nil)
	tie := submitNow.Add(-2 * time.Hour)
	seedVenue(t, h.pool, seed{id: vid(1), name: "Oldest", status: "rejected", submitter: ptr(userA), updated: submitNow.Add(-5 * time.Hour)})
	seedVenue(t, h.pool, seed{id: vid(2), name: "Tie Low", status: "pending", submitter: ptr(userA), updated: tie})
	seedVenue(t, h.pool, seed{id: vid(3), name: "Tie High", status: "verified", submitter: ptr(userA), updated: tie})
	seedVenue(t, h.pool, seed{id: vid(4), name: "Newest", status: "pending", submitter: ptr(userA), updated: submitNow})
	seedVenue(t, h.pool, seed{id: vid(5), name: "Other", status: "pending", submitter: ptr(userB), updated: submitNow})
	seedVenue(t, h.pool, seed{id: vid(6), name: "Orphan", status: "pending", updated: submitNow})

	var seen []string
	cursor := ""
	for {
		page, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 1, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, got := range page.Submissions {
			seen = append(seen, got.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	want := []string{vid(4), vid(3), vid(2), vid(1)}
	if len(seen) != len(want) {
		t.Fatalf("seen %v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("order %v", seen)
		}
	}

	full, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 20})
	if err != nil || len(full.Submissions) != 4 || full.NextCursor != nil {
		t.Fatalf("full %+v %v", full, err)
	}
	first := full.Submissions[0]
	if first.CreatedAt.IsZero() || !first.UpdatedAt.Equal(submitNow) || len(first.Genres) != 1 || first.Genres[0].Name != "Punk" {
		t.Fatalf("document %+v", first)
	}

	none, err := h.service.ListOwn(ctx, OwnQuery{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Limit: 20})
	if err != nil || len(none.Submissions) != 0 {
		t.Fatalf("no profile %+v %v", none, err)
	}
	var validation *ValidationError
	if _, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 0}); !errors.As(err, &validation) || validation.Fields["limit"] == "" {
		t.Fatalf("limit %v", err)
	}
	if _, err := h.service.ListOwn(ctx, OwnQuery{UserID: userA, Limit: 20, Cursor: "abc"}); !errors.As(err, &validation) || validation.Fields["cursor"] != "invalid" {
		t.Fatalf("cursor %v", err)
	}

	rejected, err := h.service.GetOwn(ctx, userA, vid(1))
	if err != nil || rejected.VerificationStatus != "rejected" {
		t.Fatalf("rejected %+v %v", rejected, err)
	}
	for _, id := range []string{vid(5), vid(6), vid(99)} {
		if _, err := h.service.GetOwn(ctx, userA, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s err %v", id, err)
		}
	}
	if _, err := h.service.GetOwn(ctx, userA, "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"); !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("id %v", err)
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/venue -run 'TestOwnCursorRoundTrip|TestListAndGetOwn' -v`
Expected: FAIL to compile with `undefined: encodeOwnCursor`.

- [ ] **Step 6: Implement `own.go`**

Create `backend/internal/venue/own.go`:

```go
package venue

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const ownDiscriminator = "submission-v1"

const submissionColumns = `
	v.id::text, v.name, v.description,
	ST_Y(v.location::geometry), ST_X(v.location::geometry),
	v.address, v.city, v.country_code, v.venue_type, v.verification_status,
	` + genresJSON + `,
	v.created_at, v.updated_at`

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) ListOwn(ctx context.Context, q OwnQuery) (OwnPage, error) {
	if err := ValidateLimit(q.Limit); err != nil {
		return OwnPage{}, err
	}
	args := []any{q.UserID}
	after := q.Cursor != ""
	if after {
		updated, id, err := decodeOwnCursor(q.Cursor)
		if err != nil {
			return OwnPage{}, err
		}
		args = append(args, updated, id)
	}
	args = append(args, q.Limit+1)
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, ownListSQL(after), args...)
	if err != nil {
		return OwnPage{}, fmt.Errorf("venue: own list: %w", err)
	}
	defer rows.Close()
	items := []Submission{}
	for rows.Next() {
		got, err := scanSubmission(rows)
		if err != nil {
			return OwnPage{}, err
		}
		items = append(items, got)
	}
	if err := rows.Err(); err != nil {
		return OwnPage{}, fmt.Errorf("venue: own list: %w", err)
	}
	var next *string
	if len(items) > q.Limit {
		items = items[:q.Limit]
		last := items[q.Limit-1]
		encoded := encodeOwnCursor(last.UpdatedAt, last.ID)
		next = &encoded
	}
	return OwnPage{Submissions: items, NextCursor: next}, nil
}

func (s *Service) GetOwn(ctx context.Context, userID, venueID string) (Submission, error) {
	if !CanonicalVenueID(venueID) {
		return Submission{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return ownSubmission(queryCtx, s.pool, userID, venueID)
}

func ownSubmission(ctx context.Context, q queryRower, userID, venueID string) (Submission, error) {
	got, err := scanSubmission(q.QueryRow(ctx, `select `+submissionColumns+`
	from venues v
	where v.id = $1::uuid and v.submitted_by = $2::uuid`, venueID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, ErrNotFound
	}
	return got, err
}

func ownListSQL(after bool) string {
	sql := `select ` + submissionColumns + `
	from venues v
	where v.submitted_by = $1::uuid`
	limit := "$2"
	if after {
		sql += `
	  and (v.updated_at, v.id) < ($2::timestamptz, $3::uuid)`
		limit = "$4"
	}
	return sql + `
	order by v.updated_at desc, v.id desc
	limit ` + limit
}

func scanSubmission(row venueScanner) (Submission, error) {
	var got Submission
	var genres []byte
	if err := row.Scan(
		&got.ID, &got.Name, &got.Description, &got.Latitude, &got.Longitude,
		&got.Address, &got.City, &got.CountryCode, &got.VenueType, &got.VerificationStatus,
		&genres, &got.CreatedAt, &got.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Submission{}, err
		}
		return Submission{}, fmt.Errorf("venue: submission: %w", err)
	}
	got.Genres = []Genre{}
	if err := json.Unmarshal(genres, &got.Genres); err != nil {
		return Submission{}, fmt.Errorf("venue: genres: %w", err)
	}
	got.CreatedAt = got.CreatedAt.UTC()
	got.UpdatedAt = got.UpdatedAt.UTC()
	return got, nil
}

func encodeOwnCursor(updated time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(ownDiscriminator + "|" + updated.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeOwnCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || cursor == "" {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != ownDiscriminator || !CanonicalVenueID(parts[2]) {
		return time.Time{}, "", invalid
	}
	updated, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return updated, parts[2], nil
}
```

- [ ] **Step 7: Run the venue tests and build**

Run: `go test ./internal/venue -v && go build ./...`
Expected: PASS, and `cmd/api` builds with the new `venue.New` arguments.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/venue backend/cmd/api/main.go
git commit -m "feat: read the caller's venue submissions"
```

---

### Task 5: Venue Creation

**Files:**
- Create: `backend/internal/venue/submit.go`
- Modify: `backend/internal/venue/submit_test.go`

**Interfaces:**
- Consumes: `ParseSubmission`, `Draft`, `SubmitCommand`, `DuplicateError`, `QuotaError`, `RateLimitError`, `disclose`, `quotaRetryAfter`, `bodyOf`, `ownSubmission`, `errWritesUnavailable` (Task 3–4); `profile.RequireContributor`, `(*profile.Service).NormalizeExpiry` (Task 2); `idempotency.Execute`.
- Produces:

```go
func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (Submission, error)

const quotaSQL string
const duplicateSQL string
func checkGenres(ctx context.Context, tx pgx.Tx, ids []int) error
func checkQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error
func checkDuplicate(ctx context.Context, tx pgx.Tx, userID string, self *string, d Draft) error
func replaceGenres(ctx context.Context, tx pgx.Tx, venueID string, ids []int) error
```

- [ ] **Step 1: Write the failing creation tests**

Append to `backend/internal/venue/submit_test.go`, and add `"errors"`, `"sync"`, and `"github.com/metalhops/metalhops/backend/internal/idempotency"` to its imports:

```go
func submit(h *harness, user, key string, raw RawSubmission) (Submission, error) {
	return h.service.Submit(context.Background(), SubmitCommand{UserID: user, Key: key, Fields: raw})
}

func countRows(t *testing.T, h *harness, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSubmitCreatesPublicPendingVenue(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	got, err := submit(h, userA, "create-key1", rawVenue("Iron Maiden Pub", 59.91, 10.75, punk, punk))
	if err != nil {
		t.Fatal(err)
	}
	if got.VerificationStatus != "pending" || got.Name != "Iron Maiden Pub" || len(got.Genres) != 1 ||
		!got.CreatedAt.Equal(submitNow) || !got.UpdatedAt.Equal(submitNow) || got.Latitude != 59.91 || got.Longitude != 10.75 {
		t.Fatalf("submission %+v", got)
	}
	public, err := h.service.GetVenue(ctx, got.ID)
	if err != nil || public.VerificationStatus != "pending" {
		t.Fatalf("public %+v %v", public, err)
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("create did not take one token")
	}
	if countRows(t, h, `select count(*) from idempotency_records where user_id = $1 and operation = 'venue_submission'`, userA) != 1 {
		t.Fatal("idempotency row")
	}
	var submitter string
	if err := h.pool.QueryRow(ctx, `select submitted_by::text from venues where id = $1`, got.ID).Scan(&submitter); err != nil || submitter != userA {
		t.Fatalf("submitter %s %v", submitter, err)
	}
}

func TestSubmitDuplicateRules(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	at := func(n int) float64 { return float64(n) }
	seedVenue(t, h.pool, seed{id: vid(1), name: "Iron Maiden", status: "pending", submitter: ptr(userB), lat: at(1), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(2), name: "Black Hole", status: "pending", submitter: ptr(userB), lat: at(2), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(3), name: "Black Hole", status: "verified", lat: at(2) + 0.00045, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(4), name: "Own Place", status: "pending", submitter: ptr(userA), lat: at(3), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(6), name: "Twin", status: "verified", lat: at(4), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(5), name: "Twin", status: "verified", lat: at(4), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(7), name: "Gone", status: "rejected", lat: at(5), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(8), name: "Iron Maiden", status: "verified", lat: at(6), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(9), name: "!!!", status: "verified", lat: at(7), lon: 10})
	seedVenue(t, h.pool, seed{id: vid(10), name: "Orphan", status: "pending", lat: at(8), lon: 10})
	punk := genreID(t, h.pool, "Punk")

	cases := []struct {
		name  string
		raw   RawSubmission
		match *DuplicateMatch
		dup   bool
	}{
		{"private pending", rawVenue("iron maiden!", at(1), 10, punk), nil, true},
		{"verified beats closer private", rawVenue("Black  Hole", at(2), 10, punk), &DuplicateMatch{vid(3), "verified"}, true},
		{"own pending", rawVenue("Own Place", at(3), 10, punk), &DuplicateMatch{vid(4), "pending"}, true},
		{"tie smallest id", rawVenue("Twin", at(4), 10, punk), &DuplicateMatch{vid(5), "verified"}, true},
		{"rejected ignored", rawVenue("Gone", at(5), 10, punk), nil, false},
		{"space counts", rawVenue("IronMaiden", at(6), 10, punk), nil, false},
		{"punctuation only", rawVenue("???", at(7), 10, punk), &DuplicateMatch{vid(9), "verified"}, true},
		{"deleted submitter stays private", rawVenue("Orphan", at(8), 10, punk), nil, true},
		{"far away", rawVenue("Iron Maiden", at(20), 10, punk), nil, false},
	}
	for i, c := range cases {
		before := tokensLeft(h.limiter, userA)
		_, err := submit(h, userA, "dup-key-"+string(rune('a'+i)), c.raw)
		var duplicate *DuplicateError
		switch {
		case c.dup && !errors.As(err, &duplicate):
			t.Fatalf("%s: err %v", c.name, err)
		case !c.dup && err != nil:
			t.Fatalf("%s: err %v", c.name, err)
		}
		if c.dup {
			if (c.match == nil) != (duplicate.Match == nil) || (c.match != nil && *c.match != *duplicate.Match) {
				t.Fatalf("%s: match %+v", c.name, duplicate.Match)
			}
			if tokensLeft(h.limiter, userA) != before {
				t.Fatalf("%s: duplicate spent a token", c.name)
			}
		}
	}
}

func TestSubmitDistanceBoundary(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	for i, metres := range []float64{99, 100, 101} {
		name := "Edge " + string(rune('A'+i))
		lat := float64(10 + i)
		seedVenue(t, h.pool, seed{id: vid(100 + i), name: name, lat: lat, lon: 20})
		var probeLat, probeLon, distance float64
		if err := h.pool.QueryRow(ctx, `
			with origin as (select ST_SetSRID(ST_MakePoint(20, $1::float8), 4326)::geography as o),
			     probe as (select ST_Project(o, $2::float8, radians(90)) as p, o from origin)
			select ST_Y(p::geometry), ST_X(p::geometry), ST_Distance(p, o) from probe`, lat, metres).Scan(&probeLat, &probeLon, &distance); err != nil {
			t.Fatal(err)
		}
		_, err := submit(h, userA, "edge-key-"+string(rune('a'+i)), rawVenue(name, probeLat, probeLon, punk))
		var duplicate *DuplicateError
		if got, want := errors.As(err, &duplicate), distance <= 100; got != want {
			t.Fatalf("%.0f m (measured %.9f): duplicate %v err %v", metres, distance, got, err)
		}
	}
}

func TestSubmitConcurrentSameName(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, user := range []string{userA, userB} {
		wg.Add(1)
		go func(i int, user string) {
			defer wg.Done()
			_, errs[i] = submit(h, user, "race-key-1", rawVenue("Race Bar", 30, 30, punk))
		}(i, user)
	}
	wg.Wait()
	var duplicate *DuplicateError
	wins := 0
	for _, err := range errs {
		switch {
		case err == nil:
			wins++
		case !errors.As(err, &duplicate):
			t.Fatalf("err %v", err)
		}
	}
	if wins != 1 || countRows(t, h, `select count(*) from venues where name = 'Race Bar'`) != 1 {
		t.Fatalf("wins %d errs %v", wins, errs)
	}
}

func TestSubmitQuota(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	statuses := []string{"pending", "verified", "rejected"}
	for i := 0; i < 9; i++ {
		seedVenue(t, h.pool, seed{
			id: vid(200 + i), name: "Quota " + string(rune('A'+i)), status: statuses[i%3], submitter: ptr(userA),
			lat: float64(i), lon: 40, created: submitNow.Add(-time.Duration(23-i) * time.Hour),
		})
	}
	seedVenue(t, h.pool, seed{id: vid(220), name: "Expired Slot", status: "pending", submitter: ptr(userA), lat: 50, lon: 40, created: submitNow.Add(-24 * time.Hour)})

	tenth, err := submit(h, userA, "quota-key-10", rawVenue("Tenth", 60, 40, punk))
	if err != nil {
		t.Fatalf("tenth %v", err)
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("tenth token")
	}
	_, err = submit(h, userA, "quota-key-11", rawVenue("Eleventh", 61, 40, punk))
	var quota *QuotaError
	if !errors.As(err, &quota) || quota.RetryAfter != 3600 {
		t.Fatalf("eleventh %v", err)
	}
	if tokensLeft(h.limiter, userA) != 59 || countRows(t, h, `select count(*) from venues where name = 'Eleventh'`) != 0 {
		t.Fatal("quota failure spent a token or wrote a row")
	}
	if countRows(t, h, `select count(*) from idempotency_records where key = 'quota-key-11'`) != 0 {
		t.Fatal("quota failure recorded idempotency")
	}
	replay, err := submit(h, userA, "quota-key-10", rawVenue("Tenth", 60, 40, punk))
	if err != nil || replay.ID != tenth.ID || tokensLeft(h.limiter, userA) != 59 {
		t.Fatalf("replay %+v %v", replay, err)
	}
}

func TestSubmitIdempotency(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	first, err := submit(h, userA, "same-key-1", rawVenue("Echo Bar", 70, 10, punk))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := submit(h, userA, "same-key-1", rawVenue("Echo Bar", 70, 10, punk))
	if err != nil || replay.ID != first.ID || !replay.UpdatedAt.Equal(first.UpdatedAt) || !replay.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := submit(h, userA, "same-key-1", rawVenue("Other Bar", 71, 10, punk)); !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused %v", err)
	}
	if _, err := submit(h, userA, "short", rawVenue("Key Bar", 72, 10, punk)); !errors.Is(err, idempotency.ErrInvalid) {
		t.Fatalf("invalid key %v", err)
	}
	bad := rawVenue("", 73, 10, punk)
	var validation *ValidationError
	if _, err := submit(h, userA, "bad-key-01", bad); !errors.As(err, &validation) {
		t.Fatalf("validation %v", err)
	}
	if _, err := submit(h, userA, "bad-key-02", rawVenue("Ghost Genre", 74, 10, 999999)); !errors.As(err, &validation) || validation.Fields["genre_ids"] != "must be an existing genre" {
		t.Fatalf("genre %v", err)
	}
	if countRows(t, h, `select count(*) from idempotency_records where key like 'bad-key-%'`) != 0 || countRows(t, h, `select count(*) from venues where submitted_by = $1`, userA) != 1 {
		t.Fatal("failed request left rows")
	}
	if tokensLeft(h.limiter, userA) != 59 {
		t.Fatal("replay or failure spent a token")
	}
}

func TestSubmitContributionRules(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	punk := genreID(t, h.pool, "Punk")
	noProfile := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	renamed := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	suspended := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	expired := "ffffffff-ffff-4fff-8fff-ffffffffffff"
	until := submitNow.Add(time.Hour)
	past := submitNow.Add(-time.Hour)
	insertUser(t, h.pool, noProfile, "", nil)
	insertUser(t, h.pool, renamed, "rename_required", nil)
	insertUser(t, h.pool, suspended, "suspended", &until)
	insertUser(t, h.pool, expired, "suspended", &past)

	if _, err := submit(h, noProfile, "role-key-1", rawVenue("A", 80, 1, punk)); !errors.Is(err, profile.ErrProfileRequired) {
		t.Fatalf("no profile %v", err)
	}
	if _, err := submit(h, renamed, "role-key-2", rawVenue("B", 80, 2, punk)); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("renamed %v", err)
	}
	var blocked *profile.SuspendedError
	if _, err := submit(h, suspended, "role-key-3", rawVenue("C", 80, 3, punk)); !errors.As(err, &blocked) || blocked.Until == nil || !blocked.Until.Equal(until) {
		t.Fatalf("suspended %v", err)
	}
	if _, err := submit(h, expired, "role-key-4", rawVenue("D", 80, 4, punk)); err != nil {
		t.Fatalf("expired suspension %v", err)
	}
	var status string
	if err := h.pool.QueryRow(ctx, `select status from profiles where id = $1`, expired).Scan(&status); err != nil || status != "active" {
		t.Fatalf("restored %s %v", status, err)
	}
	if countRows(t, h, `select count(*) from venues`) != 1 {
		t.Fatal("blocked caller wrote a venue")
	}
	catalogueOnly := New(h.pool, nil, nil, time.Now)
	if _, err := catalogueOnly.Submit(ctx, SubmitCommand{UserID: expired, Key: "role-key-5", Fields: rawVenue("E", 80, 5, punk)}); !errors.Is(err, errWritesUnavailable) {
		t.Fatalf("catalogue-only service %v", err)
	}
}
```

Correction under a full quota is pinned in Task 6 `TestCorrectDoesNotTakeQuota`, because `Correct` does not exist yet.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue -run 'TestSubmit' -v`
Expected: FAIL to compile with `h.service.Submit undefined`.

- [ ] **Step 3: Implement `submit.go`**

Create `backend/internal/venue/submit.go`:

```go
package venue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

const quotaSQL = `
	select count(*), min(created_at)
	from venues
	where submitted_by = $1::uuid and created_at > $2`

const duplicateSQL = `
	select v.id::text, v.verification_status, coalesce(v.submitted_by = $2::uuid, false)
	from venues v
	where venue_name_key(v.name) = $1
	  and v.verification_status in ('pending', 'verified')
	  and ST_DWithin(v.location, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography, 100)
	  and v.id is distinct from $5::uuid
	order by ST_Distance(v.location, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography), v.id`

func (s *Service) Submit(ctx context.Context, cmd SubmitCommand) (Submission, error) {
	draft, err := ParseSubmission(cmd.Fields)
	if err != nil {
		return Submission{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return Submission{}, errWritesUnavailable
	}
	command, err := json.Marshal(draft)
	if err != nil {
		return Submission{}, fmt.Errorf("venue: submit: %w", err)
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Submission{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	if err := checkGenres(txCtx, tx, draft.GenreIDs); err != nil {
		return Submission{}, err
	}
	now := s.clock()
	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	result, err := idempotency.Execute(txCtx, tx, now, idempotency.Request{
		UserID: cmd.UserID, Operation: "venue_submission", Key: cmd.Key, Command: command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		if err := checkQuota(ctx, tx, cmd.UserID, now); err != nil {
			return idempotency.Result{}, err
		}
		if err := checkDuplicate(ctx, tx, cmd.UserID, nil, draft); err != nil {
			return idempotency.Result{}, err
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.UserID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var id string
		if err := tx.QueryRow(ctx, `
			insert into venues (
				name, description, location, address, city, country_code, venue_type,
				verification_status, submitted_by, created_at, updated_at
			) values (
				$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
				$5, $6, $7, $8, 'pending', $9::uuid, $10, $10
			)
			returning id::text`,
			draft.Name, draft.Description, draft.Longitude, draft.Latitude,
			draft.Address, draft.City, draft.CountryCode, draft.VenueType, cmd.UserID, now,
		).Scan(&id); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: submit: %w", err)
		}
		if err := replaceGenres(ctx, tx, id, draft.GenreIDs); err != nil {
			return idempotency.Result{}, err
		}
		got, err := ownSubmission(ctx, tx, cmd.UserID, id)
		if err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(bodyOf(got))
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: submit: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return Submission{}, err
	}
	var stored submissionBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return Submission{}, fmt.Errorf("venue: submit: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return Submission{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return stored.submission(), nil
}

func checkGenres(ctx context.Context, tx pgx.Tx, ids []int) error {
	var found int
	if err := tx.QueryRow(ctx, `select count(*) from genres where id = any($1::int[])`, ids).Scan(&found); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	if found != len(ids) {
		return &ValidationError{Fields: map[string]string{"genre_ids": "must be an existing genre"}}
	}
	return nil
}

func checkQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('venue_submissions' || chr(31) || $1, 0))`, userID); err != nil {
		return fmt.Errorf("venue: quota lock: %w", err)
	}
	var count int
	var oldest *time.Time
	if err := tx.QueryRow(ctx, quotaSQL, userID, now.Add(-24*time.Hour)).Scan(&count, &oldest); err != nil {
		return fmt.Errorf("venue: quota: %w", err)
	}
	if count < 10 {
		return nil
	}
	retryAfter := 1
	if oldest != nil {
		retryAfter = quotaRetryAfter(*oldest, now)
	}
	return &QuotaError{RetryAfter: retryAfter}
}

// The lock and the match query must be separate statements: a statement's snapshot
// starts with the statement, so only a later statement sees the other writer's commit.
func checkDuplicate(ctx context.Context, tx pgx.Tx, userID string, self *string, d Draft) error {
	var key string
	if err := tx.QueryRow(ctx, `select venue_name_key($1)`, d.Name).Scan(&key); err != nil {
		return fmt.Errorf("venue: name key: %w", err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('venue_name' || chr(31) || $1, 0))`, key); err != nil {
		return fmt.Errorf("venue: name lock: %w", err)
	}
	rows, err := tx.Query(ctx, duplicateSQL, key, userID, d.Longitude, d.Latitude, self)
	if err != nil {
		return fmt.Errorf("venue: duplicates: %w", err)
	}
	defer rows.Close()
	var candidates []duplicateCandidate
	for rows.Next() {
		var c duplicateCandidate
		if err := rows.Scan(&c.ID, &c.Status, &c.Own); err != nil {
			return fmt.Errorf("venue: duplicates: %w", err)
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("venue: duplicates: %w", err)
	}
	if len(candidates) == 0 {
		return nil
	}
	return &DuplicateError{Match: disclose(candidates)}
}

func replaceGenres(ctx context.Context, tx pgx.Tx, venueID string, ids []int) error {
	if _, err := tx.Exec(ctx, `delete from venue_genres where venue_id = $1::uuid`, venueID); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into venue_genres (venue_id, genre_id)
		select $1::uuid, unnest($2::int[])`, venueID, ids); err != nil {
		return fmt.Errorf("venue: genres: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the creation tests**

Run: `go test ./internal/venue -run 'TestSubmit' -v -race`
Expected: PASS for `TestSubmitCreatesPublicPendingVenue`, `TestSubmitDuplicateRules`, `TestSubmitDistanceBoundary`, `TestSubmitConcurrentSameName`, `TestSubmitQuota`, `TestSubmitIdempotency`, and `TestSubmitContributionRules`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/submit.go backend/internal/venue/submit_test.go
git commit -m "feat: submit venues with duplicate and quota checks"
```

---

### Task 6: Pending Correction

**Files:**
- Modify: `backend/internal/venue/submit.go`
- Modify: `backend/internal/venue/submit_test.go`

**Interfaces:**
- Consumes: `CorrectCommand`, `NotPendingError`, `sameSubmission`, `checkGenres`, `checkDuplicate`, `replaceGenres`, `ownSubmission`, `RateLimitError`.
- Produces: `func (s *Service) Correct(ctx context.Context, cmd CorrectCommand) (Submission, error)`.

- [ ] **Step 1: Write the failing correction tests**

Append to `backend/internal/venue/submit_test.go`:

```go
func correct(h *harness, user, venueID string, raw RawSubmission) (Submission, error) {
	return h.service.Correct(context.Background(), CorrectCommand{UserID: user, VenueID: venueID, Fields: raw})
}

func TestCorrectPendingSubmission(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	classic := genreID(t, h.pool, "Classic rock")
	created, err := submit(h, userA, "fix-key-01", rawVenue("Rust Bar", 15, 15, punk))
	if err != nil {
		t.Fatal(err)
	}

	renamed, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, classic))
	if err != nil || renamed.Name != "Rust Pub" || renamed.VerificationStatus != "pending" || len(renamed.Genres) != 1 || renamed.Genres[0].ID != classic ||
		!renamed.CreatedAt.Equal(submitNow) || !renamed.UpdatedAt.After(submitNow) {
		t.Fatalf("renamed %+v %v", renamed, err)
	}
	var searchName string
	if err := h.pool.QueryRow(ctx, `select search_name from venues where id = $1`, created.ID).Scan(&searchName); err != nil || searchName != "rustpub" {
		t.Fatalf("search key %s %v", searchName, err)
	}
	if public, err := h.service.GetVenue(ctx, created.ID); err != nil || public.Name != "Rust Pub" {
		t.Fatalf("public %+v %v", public, err)
	}

	genresOnly, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, classic, punk))
	if err != nil || len(genresOnly.Genres) != 2 || !genresOnly.UpdatedAt.After(renamed.UpdatedAt) {
		t.Fatalf("genre-only %+v %v", genresOnly, err)
	}
	tokens := tokensLeft(h.limiter, userA)
	same, err := correct(h, userA, created.ID, rawVenue("Rust Pub", genresOnly.Latitude, genresOnly.Longitude, punk, classic))
	if err != nil || !same.UpdatedAt.Equal(genresOnly.UpdatedAt) || tokensLeft(h.limiter, userA) != tokens {
		t.Fatalf("unchanged %+v %v", same, err)
	}
	if tokens != 57 {
		t.Fatalf("tokens %d", tokens)
	}

	seedVenue(t, h.pool, seed{id: vid(30), name: "Taken", lat: 15.0002, lon: 15})
	var duplicate *DuplicateError
	if _, err := correct(h, userA, created.ID, rawVenue("Taken", 15, 15, punk)); !errors.As(err, &duplicate) || duplicate.Match == nil || duplicate.Match.VenueID != vid(30) {
		t.Fatalf("duplicate %v", err)
	}
	var validation *ValidationError
	if _, err := correct(h, userA, created.ID, rawVenue("Rust Pub", 15, 15, 999999)); !errors.As(err, &validation) || validation.Fields["genre_ids"] != "must be an existing genre" {
		t.Fatalf("unknown genre %v", err)
	}

	seedVenue(t, h.pool, seed{id: vid(31), name: "Verified Own", status: "verified", submitter: ptr(userA), lat: 16, lon: 15})
	seedVenue(t, h.pool, seed{id: vid(32), name: "Rejected Own", status: "rejected", submitter: ptr(userA), lat: 17, lon: 15})
	seedVenue(t, h.pool, seed{id: vid(33), name: "Orphan", status: "pending", lat: 18, lon: 15})
	for _, c := range []struct{ id, status string }{{vid(31), "verified"}, {vid(32), "rejected"}} {
		var notPending *NotPendingError
		if _, err := correct(h, userA, c.id, rawVenue("Changed", 16, 15, 999999)); !errors.As(err, &notPending) || notPending.Status != c.status {
			t.Fatalf("%s err %v", c.id, err)
		}
	}
	for _, c := range []struct{ user, id string }{{userB, created.ID}, {userA, vid(33)}, {userA, vid(99)}} {
		if _, err := correct(h, c.user, c.id, rawVenue("Changed", 19, 15, punk)); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%+v err %v", c, err)
		}
	}
	if _, err := correct(h, userA, "not-a-uuid", rawVenue("Changed", 19, 15, punk)); !errors.As(err, &validation) || validation.Fields["venue_id"] != "must be a canonical uuid" {
		t.Fatalf("id %v", err)
	}
}

func TestCorrectThenReplay(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	first, err := submit(h, userA, "keep-key-1", rawVenue("Original", 25, 25, punk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := correct(h, userA, first.ID, rawVenue("Corrected", 25, 25, punk)); err != nil {
		t.Fatal(err)
	}
	replay, err := submit(h, userA, "keep-key-1", rawVenue("Original", 25, 25, punk))
	if err != nil || replay.Name != "Original" || !replay.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("replay %+v %v", replay, err)
	}
}

func TestCorrectDoesNotTakeQuota(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	punk := genreID(t, h.pool, "Punk")
	for i := 0; i < 10; i++ {
		seedVenue(t, h.pool, seed{
			id: vid(300 + i), name: "Slot " + string(rune('A'+i)), status: "pending", submitter: ptr(userA),
			lat: float64(i), lon: 45, created: submitNow.Add(-time.Hour),
		})
	}
	if _, err := submit(h, userA, "slot-key-11", rawVenue("Over", 50, 45, punk)); err == nil {
		t.Fatal("quota not full")
	}
	var created time.Time
	if err := h.pool.QueryRow(ctx, `select created_at from venues where id = $1`, vid(300)).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if _, err := correct(h, userA, vid(300), rawVenue("Slot Fixed", 0, 45, punk)); err != nil {
		t.Fatalf("correction under full quota %v", err)
	}
	var after time.Time
	if err := h.pool.QueryRow(ctx, `select created_at from venues where id = $1`, vid(300)).Scan(&after); err != nil || !after.Equal(created) {
		t.Fatalf("correction moved created_at %s %v", after, err)
	}
}

func TestCorrectContributionRules(t *testing.T) {
	h := newHarness(t)
	renamed := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	insertUser(t, h.pool, renamed, "rename_required", nil)
	seedVenue(t, h.pool, seed{id: vid(40), name: "Mine", status: "pending", submitter: ptr(renamed), lat: 5, lon: 5})
	if _, err := correct(h, renamed, vid(40), rawVenue("Mine Too", 5, 5, genreID(t, h.pool, "Punk"))); !errors.Is(err, profile.ErrRenameRequired) {
		t.Fatalf("renamed %v", err)
	}
	if got, err := h.service.GetOwn(context.Background(), renamed, vid(40)); err != nil || got.Name != "Mine" {
		t.Fatalf("read while renamed %+v %v", got, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/venue -run 'TestCorrect' -v`
Expected: FAIL to compile with `h.service.Correct undefined`.

- [ ] **Step 3: Implement `Correct`**

Append to `backend/internal/venue/submit.go`, and add `"errors"` to its imports:

```go
func (s *Service) Correct(ctx context.Context, cmd CorrectCommand) (Submission, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return Submission{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	draft, err := ParseSubmission(cmd.Fields)
	if err != nil {
		return Submission{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return Submission{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Submission{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.UserID); err != nil {
		return Submission{}, err
	}
	var status string
	err = tx.QueryRow(txCtx, `
		select verification_status from venues
		where id = $1::uuid and submitted_by = $2::uuid
		for update`, cmd.VenueID, cmd.UserID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, ErrNotFound
	}
	if err != nil {
		return Submission{}, fmt.Errorf("venue: correct: %w", err)
	}
	if status != "pending" {
		return Submission{}, &NotPendingError{Status: status}
	}
	if err := checkGenres(txCtx, tx, draft.GenreIDs); err != nil {
		return Submission{}, err
	}
	current, err := ownSubmission(txCtx, tx, cmd.UserID, cmd.VenueID)
	if err != nil {
		return Submission{}, err
	}
	if sameSubmission(current, draft) {
		if err := tx.Commit(txCtx); err != nil {
			return Submission{}, fmt.Errorf("venue: commit: %w", err)
		}
		return current, nil
	}
	if err := checkDuplicate(txCtx, tx, cmd.UserID, &cmd.VenueID, draft); err != nil {
		return Submission{}, err
	}
	reservation, retryAfter, ok := s.writes.Reserve(cmd.UserID)
	if !ok {
		return Submission{}, &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	if _, err := tx.Exec(txCtx, `
		update venues set
			name = $2, description = $3,
			location = ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
			address = $6, city = $7, country_code = $8, venue_type = $9
		where id = $1::uuid`,
		cmd.VenueID, draft.Name, draft.Description, draft.Longitude, draft.Latitude,
		draft.Address, draft.City, draft.CountryCode, draft.VenueType); err != nil {
		return Submission{}, fmt.Errorf("venue: correct: %w", err)
	}
	if err := replaceGenres(txCtx, tx, cmd.VenueID, draft.GenreIDs); err != nil {
		return Submission{}, err
	}
	got, err := ownSubmission(txCtx, tx, cmd.UserID, cmd.VenueID)
	if err != nil {
		return Submission{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return Submission{}, fmt.Errorf("venue: commit: %w", err)
	}
	reservation.Commit()
	committed = true
	return got, nil
}
```

The `update` always runs for a changed correction, so `venues_set_updated_at` fires even when only Genre links change.

- [ ] **Step 4: Run every venue test**

Run: `go test ./internal/venue -v -race`
Expected: PASS, including Tasks 4 to 6 and the Session 7 and Session 8 tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/submit.go backend/internal/venue/submit_test.go
git commit -m "feat: correct pending venue submissions"
```

---

### Task 7: Submission Index Plans

**Files:**
- Modify: `backend/internal/venue/index_test.go`
- Modify (only if a plan rejects an index): `infra/supabase/migrations/0007_venue_submissions.sql`

**Interfaces:**
- Consumes: `quotaSQL`, `ownListSQL`, `testPool`, indexes from Task 1.
- Produces: `TestSubmissionIndexes`.

- [ ] **Step 1: Write the plan test**

Append to `backend/internal/venue/index_test.go`, and add `"time"` to its imports if Task 4 did not already:

```go
func TestSubmissionIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) as n from auth.users)
		insert into venues (name, location, address, city, country_code, verification_status, submitted_by, created_at, updated_at)
		select 'Venue ' || i,
		       ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street', 'City', 'DE', 'pending', users.id,
		       now() - (i || ' minutes')::interval, now() - (i || ' minutes')::interval
		from generate_series(1, 5000) as i
		join users on users.n = (i % 500) + 1`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze venues"); err != nil {
		t.Fatal(err)
	}
	var user string
	if err := pool.QueryRow(ctx, `select id::text from auth.users order by id limit 1`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		sql   string
		args  []any
		index string
	}{
		{"quota", quotaSQL, []any{user, time.Now().Add(-24 * time.Hour)}, "venues_submitted_by_created_at_idx"},
		{"own list", ownListSQL(false), []any{user, 21}, "venues_submitted_by_updated_at_idx"},
		{"own list after", ownListSQL(true), []any{user, time.Now(), "ffffffff-ffff-4fff-8fff-ffffffffffff", 21}, "venues_submitted_by_updated_at_idx"},
	}
	for _, c := range cases {
		rows, err := pool.Query(ctx, "explain "+c.sql, c.args...)
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
		if plan := strings.Join(lines, "\n"); !strings.Contains(plan, c.index) {
			t.Fatalf("%s plan does not use %s:\n%s", c.name, c.index, plan)
		}
	}
}
```

- [ ] **Step 2: Run the plan test**

Run: `go test ./internal/venue -run TestSubmissionIndexes -v`
Expected: PASS. If a case uses a different index, keep only the indexes that some captured plan uses, edit `0007_venue_submissions.sql` to drop the unused one, and change that case's expected index to the one the plan chose. `0007` is not yet on `dev` or recorded on the hosted project, so it may still change.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/venue/index_test.go infra/supabase/migrations/0007_venue_submissions.sql
git commit -m "test: justify venue submission indexes with query plans"
```

---

### Task 8: HTTP Routes

**Files:**
- Create: `backend/internal/api/submission.go`
- Create: `backend/internal/api/submission_test.go`
- Modify: `backend/internal/api/server.go:26-35,63`
- Modify: `backend/internal/api/profile.go` (`writeProfileError`)
- Modify: `backend/cmd/api/main.go:54`

**Interfaces:**
- Consumes: `venue.SubmitCommand`, `venue.CorrectCommand`, `venue.OwnQuery`, `venue.OwnPage`, `venue.Submission`, `venue.RawSubmission`, `venue.DuplicateError`, `venue.QuotaError`, `venue.RateLimitError`, `venue.ErrNotFound`, `venue.NotPendingError`, `venue.ValidationError`; `requireApp`, `DecodeJSON`, `writeDecodeError`, `rejectedQuery`, `parseVenueLimit`, `genreView`, `WriteData`, `WriteList`, `WriteError`, `WriteErrorDetails`.
- Produces:

```go
type Submissions interface {
	Submit(context.Context, venue.SubmitCommand) (venue.Submission, error)
	Correct(context.Context, venue.CorrectCommand) (venue.Submission, error)
	ListOwn(context.Context, venue.OwnQuery) (venue.OwnPage, error)
	GetOwn(context.Context, string, string) (venue.Submission, error)
}

// Options gains:
Submissions Submissions

func writeContributionError(w http.ResponseWriter, id string, err error) bool
```

- [ ] **Step 1: Write the failing HTTP tests**

Create `backend/internal/api/submission_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/metalhops/metalhops/backend/internal/auth"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type fakeSubmissions struct {
	got      venue.Submission
	page     venue.OwnPage
	err      error
	submit   venue.SubmitCommand
	correct  venue.CorrectCommand
	query    venue.OwnQuery
	userID   string
	venueID  string
	calls    int
}

func (f *fakeSubmissions) Submit(_ context.Context, cmd venue.SubmitCommand) (venue.Submission, error) {
	f.calls++
	f.submit = cmd
	return f.got, f.err
}

func (f *fakeSubmissions) Correct(_ context.Context, cmd venue.CorrectCommand) (venue.Submission, error) {
	f.calls++
	f.correct = cmd
	return f.got, f.err
}

func (f *fakeSubmissions) ListOwn(_ context.Context, q venue.OwnQuery) (venue.OwnPage, error) {
	f.calls++
	f.query = q
	return f.page, f.err
}

func (f *fakeSubmissions) GetOwn(_ context.Context, userID, venueID string) (venue.Submission, error) {
	f.calls++
	f.userID, f.venueID = userID, venueID
	return f.got, f.err
}

const submissionBodyJSON = `{"name":"Iron","latitude":59.9,"longitude":10.7,"address":"1 Street","city":"Oslo","country_code":"no","venue_type":"pub","genre_ids":[3,1]}`

func submissionHandler(authenticator *fakeAuth, submissions *fakeSubmissions) http.Handler {
	return New(Options{
		Readiness:   readyOK(),
		PublicLimit: allowAll,
		Auth:        authenticator,
		Submissions: submissions,
		Logger:      slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func sampleSubmission() venue.Submission {
	at := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)
	return venue.Submission{
		ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", Name: "Iron", Latitude: 59.9, Longitude: 10.7,
		Address: "1 Street", City: "Oslo", CountryCode: "NO", VenueType: "pub", VerificationStatus: "pending",
		Genres: []venue.Genre{{ID: 1, Name: "Classic rock"}}, CreatedAt: at, UpdatedAt: at,
	}
}

func TestSubmissionRoutes(t *testing.T) {
	caller := auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}
	submissions := &fakeSubmissions{got: sampleSubmission()}
	handler := submissionHandler(&fakeAuth{caller: caller}, submissions)
	plain := map[string]string{"Content-Type": "application/json"}
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}

	created := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), keyed)
	if created.Code != http.StatusCreated || submissions.submit.Key != "submit-key-1" || submissions.submit.UserID != caller.UserID {
		t.Fatalf("create %d %s %+v", created.Code, created.Body.String(), submissions.submit)
	}
	if f := submissions.submit.Fields; f.Name == nil || *f.Name != "Iron" || *f.CountryCode != "no" || f.Description != nil || len(f.GenreIDs) != 2 || f.GenreIDs[0] != 3 {
		t.Fatalf("fields %+v", f)
	}
	for _, want := range []string{`"data":{`, `"created_at":"2020-01-15T12:00:00Z"`, `"updated_at":"2020-01-15T12:00:00Z"`, `"verification_status":"pending"`, `"genres":[{"id":1,"name":"Classic rock"}]`} {
		if !bytes.Contains(created.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, created.Body.String())
		}
	}
	for _, unwanted := range []string{`average_rating`, `review_count`, `submitted_by`} {
		if bytes.Contains(created.Body.Bytes(), []byte(unwanted)) {
			t.Fatalf("leaked %s", unwanted)
		}
	}

	before := submissions.calls
	missingKey := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), plain)
	assertCode(t, missingKey, http.StatusBadRequest, "validation_error")
	if !bytes.Contains(missingKey.Body.Bytes(), []byte(`"idempotency_key":"required"`)) || submissions.calls != before {
		t.Fatalf("missing key %s", missingKey.Body.String())
	}
	for _, body := range []string{`{"verification_status":"verified"}`, `{"latitude":"north"}`, `{"genre_ids":[1.5]}`, `not json`} {
		rec := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(body), keyed)
		assertCode(t, rec, http.StatusBadRequest, "validation_error")
		if bytes.Contains(rec.Body.Bytes(), []byte(`"fields"`)) || submissions.calls != before {
			t.Fatalf("%s %s", body, rec.Body.String())
		}
	}

	put := requestWith(handler, http.MethodPut, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Bearer t", []byte(submissionBodyJSON), plain)
	if put.Code != http.StatusOK || submissions.correct.VenueID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || submissions.correct.UserID != caller.UserID {
		t.Fatalf("put %d %s", put.Code, put.Body.String())
	}

	cursor := "next"
	submissions.page = venue.OwnPage{Submissions: []venue.Submission{sampleSubmission()}, NextCursor: &cursor}
	list := requestWith(handler, http.MethodGet, "/v1/me/venues", "Bearer t", nil, nil)
	if list.Code != http.StatusOK || submissions.query.Limit != 20 || submissions.query.UserID != caller.UserID || !bytes.Contains(list.Body.Bytes(), []byte(`"next_cursor":"next"`)) {
		t.Fatalf("list %d %s %+v", list.Code, list.Body.String(), submissions.query)
	}
	requestWith(handler, http.MethodGet, "/v1/me/venues?limit=5&cursor=abc", "Bearer t", nil, nil)
	if submissions.query.Limit != 5 || submissions.query.Cursor != "abc" {
		t.Fatalf("query %+v", submissions.query)
	}
	before = submissions.calls
	for _, path := range []string{"/v1/me/venues?sort=new", "/v1/me/venues?limit=1&limit=2", "/v1/me/venues?limit=x", "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa?x=1"} {
		assertCode(t, requestWith(handler, http.MethodGet, path, "Bearer t", nil, nil), http.StatusBadRequest, "validation_error")
	}
	if submissions.calls != before {
		t.Fatal("bad query reached the service")
	}
	detail := requestWith(handler, http.MethodGet, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "Bearer t", nil, nil)
	if detail.Code != http.StatusOK || submissions.venueID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || submissions.userID != caller.UserID || bytes.Contains(detail.Body.Bytes(), []byte(`"meta"`)) {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
}

func TestSubmissionAuth(t *testing.T) {
	routes := []struct{ method, path string }{
		{http.MethodPost, "/v1/venues"},
		{http.MethodGet, "/v1/me/venues"},
		{http.MethodGet, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{http.MethodPut, "/v1/me/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
	}
	submissions := &fakeSubmissions{}
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}
	anonymous := submissionHandler(&fakeAuth{}, submissions)
	operator := submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}, operator: true}, submissions)
	for _, route := range routes {
		assertCode(t, requestWith(anonymous, route.method, route.path, "", []byte(submissionBodyJSON), headers), http.StatusUnauthorized, "unauthorized")
		assertForbidden(t, requestWith(operator, route.method, route.path, "Bearer t", []byte(submissionBodyJSON), headers))
	}
	if submissions.calls != 0 {
		t.Fatal("rejected caller reached the service")
	}
}

func TestSubmissionErrors(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		err    error
		status int
		code   string
		want   string
		header string
	}{
		{&venue.ValidationError{Fields: map[string]string{"genre_ids": "must be an existing genre"}}, 400, "validation_error", `"genre_ids":"must be an existing genre"`, ""},
		{idempotency.ErrInvalid, 400, "validation_error", "", ""},
		{idempotency.ErrReused, 409, "idempotency_key_reused", "", ""},
		{profile.ErrProfileRequired, 409, "profile_required", "Create a profile first.", ""},
		{profile.ErrRenameRequired, 403, "profile_rename_required", "", ""},
		{&profile.SuspendedError{Until: &until}, 403, "profile_suspended", `"suspended_until":"2026-11-01T00:00:00Z"`, ""},
		{venue.ErrNotFound, 404, "venue_not_found", "Venue not found.", ""},
		{&venue.NotPendingError{Status: "verified"}, 409, "venue_not_pending", "This venue can no longer be corrected.", ""},
		{&venue.NotPendingError{Status: "rejected"}, 409, "venue_not_pending", "Only a pending venue can be corrected.", ""},
		{&venue.DuplicateError{Match: &venue.DuplicateMatch{VenueID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", VerificationStatus: "verified"}}, 409, "duplicate_venue", `"details":{"venue_id":"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb","verification_status":"verified"}`, ""},
		{&venue.QuotaError{RetryAfter: 3600}, 429, "quota_exceeded", "The submission limit has been reached.", "3600"},
		{&venue.RateLimitError{RetryAfter: 2}, 429, "rate_limited", "Too many requests.", "2"},
		{errors.New("db down"), 500, "internal_error", "", ""},
	}
	headers := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "submit-key-1"}
	for _, c := range cases {
		submissions := &fakeSubmissions{err: c.err}
		handler := submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}, submissions)
		rec := requestWith(handler, http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), headers)
		assertCode(t, rec, c.status, c.code)
		if c.want != "" && !bytes.Contains(rec.Body.Bytes(), []byte(c.want)) {
			t.Fatalf("%s body %s", c.code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") != c.header {
			t.Fatalf("%s retry %q", c.code, rec.Header().Get("Retry-After"))
		}
	}
	private := &fakeSubmissions{err: &venue.DuplicateError{}}
	rec := requestWith(submissionHandler(&fakeAuth{caller: auth.Caller{UserID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc"}}, private), http.MethodPost, "/v1/venues", "Bearer t", []byte(submissionBodyJSON), headers)
	assertCode(t, rec, http.StatusConflict, "duplicate_venue")
	if bytes.Contains(rec.Body.Bytes(), []byte(`"details"`)) {
		t.Fatalf("private duplicate leaked %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api -run 'TestSubmission' -v`
Expected: FAIL to compile with `unknown field Submissions in struct literal`.

- [ ] **Step 3: Extract the contribution errors**

In `backend/internal/api/profile.go`, add:

```go
func writeContributionError(w http.ResponseWriter, id string, err error) bool {
	var suspended *profile.SuspendedError
	switch {
	case errors.Is(err, profile.ErrProfileRequired):
		WriteError(w, http.StatusConflict, "profile_required", "Create a profile first.", id, nil)
	case errors.Is(err, profile.ErrRenameRequired):
		WriteError(w, http.StatusForbidden, "profile_rename_required", "Rename this profile before continuing.", id, nil)
	case errors.As(err, &suspended):
		var details map[string]any
		if suspended.Until != nil {
			details = map[string]any{"suspended_until": suspended.Until.UTC().Format(time.RFC3339)}
		}
		WriteErrorDetails(w, http.StatusForbidden, "profile_suspended", "This profile is suspended.", id, nil, details)
	default:
		return false
	}
	return true
}
```

In `writeProfileError`, delete `var suspended *profile.SuspendedError` and the three cases for `profile.ErrProfileRequired`, `profile.ErrRenameRequired`, and `errors.As(err, &suspended)`. Insert, directly after `id := requestID(r.Context())`:

```go
	if writeContributionError(w, id, err) {
		return
	}
```

Run: `go test ./internal/api -run 'TestProfile|TestModeration' -v`
Expected: PASS. Profile responses are unchanged.

- [ ] **Step 4: Implement `submission.go`**

Create `backend/internal/api/submission.go`:

```go
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type Submissions interface {
	Submit(context.Context, venue.SubmitCommand) (venue.Submission, error)
	Correct(context.Context, venue.CorrectCommand) (venue.Submission, error)
	ListOwn(context.Context, venue.OwnQuery) (venue.OwnPage, error)
	GetOwn(context.Context, string, string) (venue.Submission, error)
}

type submissionRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	Latitude    *float64 `json:"latitude"`
	Longitude   *float64 `json:"longitude"`
	Address     *string  `json:"address"`
	City        *string  `json:"city"`
	CountryCode *string  `json:"country_code"`
	VenueType   *string  `json:"venue_type"`
	GenreIDs    []int64  `json:"genre_ids"`
}

func (r submissionRequest) raw() venue.RawSubmission {
	return venue.RawSubmission{
		Name: r.Name, Description: r.Description, Latitude: r.Latitude, Longitude: r.Longitude,
		Address: r.Address, City: r.City, CountryCode: r.CountryCode, VenueType: r.VenueType, GenreIDs: r.GenreIDs,
	}
}

type submissionView struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Description        *string     `json:"description"`
	Latitude           float64     `json:"latitude"`
	Longitude          float64     `json:"longitude"`
	Address            string      `json:"address"`
	City               string      `json:"city"`
	CountryCode        string      `json:"country_code"`
	VenueType          string      `json:"venue_type"`
	Genres             []genreView `json:"genres"`
	VerificationStatus string      `json:"verification_status"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

func submissionJSON(got venue.Submission) submissionView {
	genres := make([]genreView, 0, len(got.Genres))
	for _, genre := range got.Genres {
		genres = append(genres, genreView{ID: genre.ID, Name: genre.Name})
	}
	return submissionView{
		ID: got.ID, Name: got.Name, Description: got.Description, Latitude: got.Latitude, Longitude: got.Longitude,
		Address: got.Address, City: got.City, CountryCode: got.CountryCode, VenueType: got.VenueType,
		Genres: genres, VerificationStatus: got.VerificationStatus,
		CreatedAt: got.CreatedAt.UTC(), UpdatedAt: got.UpdatedAt.UTC(),
	}
}

var ownListParams = map[string]bool{"limit": false, "cursor": false}

func createSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
			"idempotency_key": "required",
		})
		return
	}
	got, err := submissions.Submit(r.Context(), venue.SubmitCommand{UserID: caller.UserID, Key: key, Fields: req.raw()})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, submissionJSON(got))
}

func correctSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	got, err := submissions.Correct(r.Context(), venue.CorrectCommand{
		UserID: caller.UserID, VenueID: r.PathValue("venueID"), Fields: req.raw(),
	})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, submissionJSON(got))
}

func listSubmissions(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	if fields := rejectedQuery(r, ownListParams); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	values := r.URL.Query()
	limit, err := parseVenueLimit(values.Get("limit"))
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	page, err := submissions.ListOwn(r.Context(), venue.OwnQuery{UserID: caller.UserID, Limit: limit, Cursor: values.Get("cursor")})
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	views := make([]submissionView, 0, len(page.Submissions))
	for _, got := range page.Submissions {
		views = append(views, submissionJSON(got))
	}
	WriteList(w, http.StatusOK, views, page.NextCursor)
}

func getSubmission(w http.ResponseWriter, r *http.Request, submissions Submissions, logger *slog.Logger) {
	if fields := rejectedQuery(r, nil); fields != nil {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), fields)
		return
	}
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeSubmissionError(logger, w, r, errors.New("missing caller"))
		return
	}
	got, err := submissions.GetOwn(r.Context(), caller.UserID, r.PathValue("venueID"))
	if err != nil {
		writeSubmissionError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, submissionJSON(got))
}

func writeSubmissionError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	if writeContributionError(w, id, err) {
		return
	}
	var validation *venue.ValidationError
	var duplicate *venue.DuplicateError
	var quota *venue.QuotaError
	var limited *venue.RateLimitError
	var notPending *venue.NotPendingError
	switch {
	case errors.As(err, &validation):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, validation.Fields)
	case errors.Is(err, idempotency.ErrInvalid):
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", id, nil)
	case errors.Is(err, idempotency.ErrReused):
		WriteError(w, http.StatusConflict, "idempotency_key_reused", "That idempotency key was already used.", id, nil)
	case errors.Is(err, venue.ErrNotFound):
		WriteError(w, http.StatusNotFound, "venue_not_found", "Venue not found.", id, nil)
	case errors.As(err, &notPending):
		message := "This venue can no longer be corrected."
		if notPending.Status == "rejected" {
			message = "Only a pending venue can be corrected."
		}
		WriteError(w, http.StatusConflict, "venue_not_pending", message, id, nil)
	case errors.As(err, &duplicate):
		var details map[string]any
		if duplicate.Match != nil {
			details = map[string]any{
				"venue_id":            duplicate.Match.VenueID,
				"verification_status": duplicate.Match.VerificationStatus,
			}
		}
		WriteErrorDetails(w, http.StatusConflict, "duplicate_venue", "A matching venue already exists.", id, nil, details)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The submission limit has been reached.", id, nil)
	case errors.As(err, &limited):
		w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests.", id, nil)
	default:
		logger.Error("venue submission", "request_id", id, "err", err)
		WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
	}
}
```

- [ ] **Step 5: Register the routes and wire the service**

In `backend/internal/api/server.go`, add to `Options`:

```go
	Submissions Submissions
```

After the existing `POST /v1/profiles/{username}/reports` registration, add:

```go
	mux.Handle("POST /v1/venues", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		createSubmission(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("GET /v1/me/venues", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listSubmissions(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("GET /v1/me/venues/{venueID}", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getSubmission(w, r, options.Submissions, options.Logger)
	})))
	mux.Handle("PUT /v1/me/venues/{venueID}", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correctSubmission(w, r, options.Submissions, options.Logger)
	})))
```

In `backend/cmd/api/main.go`, add to the `api.Options` literal:

```go
			Submissions: venues,
```

- [ ] **Step 6: Run the API tests and build**

Run: `go test ./internal/api -v && go build ./... && go vet ./...`
Expected: PASS. The existing `GET /v1/venues` public tests still pass, since only `POST` on that path requires a token.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/api backend/cmd/api/main.go
git commit -m "feat: route venue submissions"
```

---

### Task 9: Contract and Guidance

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: routes, codes, and messages from Task 8.
- Produces: operation ids `createVenueSubmission`, `listMyVenues`, `getMyVenue`, `correctMyVenue`; schemas `SubmissionRequest`, `Submission`, `SubmissionResponse`, `SubmissionListResponse`; response `ContributionForbidden`.

- [ ] **Step 1: Write the failing contract test**

Replace `backend/contract_test.go` with:

```go
package contract

import (
	"bytes"
	"testing"
)

func TestOpenAPIDocument(t *testing.T) {
	for _, want := range []string{
		"openapi: 3.1.0", "/healthz", "/readyz", "http://localhost:8080",
		"/v1/me", "type: http", "scheme: bearer",
		"username_reserved", "recent_authentication_required", "auth_provider_error",
		"/v1/profiles/{username}/reports",
		"/v1/moderation/profile-reports",
		"/v1/moderation/profiles/{userID}",
		"/v1/moderation/profiles/{userID}/actions",
		"profile_report",
		"profile_moderation_action",
		"self_report",
		"profile_already_reported",
		"profile_required",
		"profile_rename_required",
		"profile_suspended",
		"quota_exceeded",
		"report_already_resolved",
		"profile_report_not_found",
		"moderation_reason",
		"suspended_until",
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
		"genre_id",
		"radius_km",
		"distance_meters",
		"must be provided with latitude, longitude, and radius_km",
		"XK",
		"^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$",
		"/v1/me/venues",
		"/v1/me/venues/{venueID}",
		"SubmissionRequest",
		"Submission:",
		"venue_submission",
		"duplicate_venue",
		"venue_not_pending",
		"Only a pending venue can be corrected.",
		"The submission limit has been reached.",
		"genre_ids",
	} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	genres := bytes.Index(OpenAPI, []byte("/v1/genres"))
	if genres < 0 || bytes.Contains(OpenAPI[:genres], []byte("security:")) {
		t.Fatal("operational route requires security")
	}
	for _, op := range []string{"createVenueSubmission", "listMyVenues", "getMyVenue", "correctMyVenue"} {
		start := bytes.Index(OpenAPI, []byte("operationId: "+op))
		if start < 0 {
			t.Fatalf("missing %s", op)
		}
		rest := OpenAPI[start+len("operationId: "+op):]
		if next := bytes.Index(rest, []byte("operationId:")); next >= 0 {
			rest = rest[:next]
		}
		if !bytes.Contains(rest, []byte("bearerAuth")) {
			t.Fatalf("%s has no bearer security", op)
		}
	}
	for _, op := range []string{"listGenres", "listVenues", "getVenue"} {
		start := bytes.Index(OpenAPI, []byte("operationId: "+op))
		rest := OpenAPI[start+len("operationId: "+op):]
		if next := bytes.Index(rest, []byte("operationId:")); next >= 0 {
			rest = rest[:next]
		}
		if bytes.Contains(rest, []byte("security:")) {
			t.Fatalf("%s became protected", op)
		}
	}
}
```

Run: `go test . -run TestOpenAPIDocument -v`
Expected: FAIL with `missing "/v1/me/venues"`.

- [ ] **Step 2: Document `POST /v1/venues`**

In `backend/openapi.yaml`, under `/v1/venues:`, after the `get:` operation's final `"500":` response and before `/v1/venues/{venueID}:`, insert:

```yaml
    post:
      operationId: createVenueSubmission
      summary: Submit a venue
      description: Creates a pending Venue whose Submitter is the caller. A pending Venue is in the public catalogue as not verified. Duplicate detection compares the normalized name, which folds case and accents, removes punctuation, and keeps single spaces, within 100 metres of a pending or verified Venue. A caller may create 10 Venue Submissions in a rolling 24 hours; corrections do not count.
      security:
        - bearerAuth: []
      parameters:
        - name: Idempotency-Key
          in: header
          required: true
          description: 8 to 128 printable ASCII characters. The idempotency operation is venue_submission. A replay returns the stored 201 body and takes no quota slot.
          schema:
            type: string
            minLength: 8
            maxLength: 128
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/SubmissionRequest"
            example:
              name: Iron Maiden Pub
              latitude: 59.91
              longitude: 10.75
              address: 1 Metal Street
              city: Oslo
              country_code: "NO"
              venue_type: pub
              genre_ids: [1, 4]
      responses:
        "201":
          description: The pending Venue was stored.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SubmissionResponse"
        "400":
          description: The body or idempotency key is invalid, or a Genre id does not exist.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              examples:
                field:
                  value:
                    error:
                      code: validation_error
                      message: The request is invalid.
                      request_id: abc
                      fields:
                        genre_ids: must be an existing genre
                key:
                  value:
                    error:
                      code: validation_error
                      message: The request is invalid.
                      request_id: abc
                      fields:
                        idempotency_key: required
        "401":
          $ref: "#/components/responses/Unauthorized"
        "403":
          $ref: "#/components/responses/ContributionForbidden"
        "409":
          description: The caller has no profile, a strong duplicate exists, or the idempotency key was reused for a different command. Duplicate details appear only when the nearest disclosable match is verified or belongs to the caller.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              examples:
                duplicate:
                  value:
                    error:
                      code: duplicate_venue
                      message: A matching venue already exists.
                      request_id: abc
                      details:
                        venue_id: bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb
                        verification_status: verified
                duplicate_private:
                  value:
                    error:
                      code: duplicate_venue
                      message: A matching venue already exists.
                      request_id: abc
                profile:
                  value:
                    error:
                      code: profile_required
                      message: Create a profile first.
                      request_id: abc
                idempotency:
                  value:
                    error:
                      code: idempotency_key_reused
                      message: That idempotency key was already used.
                      request_id: abc
        "413":
          $ref: "#/components/responses/BodyTooLarge"
        "415":
          $ref: "#/components/responses/UnsupportedMedia"
        "429":
          description: The submission quota or a write limit refused the request.
          headers:
            Retry-After:
              schema:
                type: integer
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              examples:
                quota:
                  value:
                    error:
                      code: quota_exceeded
                      message: The submission limit has been reached.
                      request_id: abc
                rate:
                  value:
                    error:
                      code: rate_limited
                      message: Too many requests.
                      request_id: abc
        "500":
          $ref: "#/components/responses/Internal"
        "502":
          $ref: "#/components/responses/AuthProvider"
```

- [ ] **Step 3: Document the caller's submission routes**

After the `/v1/me:` path block (its `delete:` operation) and before `/v1/profiles/{username}/reports:`, insert:

```yaml
  /v1/me/venues:
    get:
      operationId: listMyVenues
      summary: List the caller's venue submissions
      description: Every status, ordered by updated_at descending, then id descending. A rename_required or suspended caller can read this list. A caller without a profile receives an empty list.
      security:
        - bearerAuth: []
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
          description: Opaque next_cursor from a previous page of this route.
          schema:
            type: string
      responses:
        "200":
          description: One page of the caller's submissions.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SubmissionListResponse"
        "400":
          description: The limit or cursor is invalid, or a parameter is repeated or unknown.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
        "401":
          $ref: "#/components/responses/Unauthorized"
        "403":
          $ref: "#/components/responses/ContributionForbidden"
        "429":
          $ref: "#/components/responses/RateLimited"
        "500":
          $ref: "#/components/responses/Internal"
        "502":
          $ref: "#/components/responses/AuthProvider"
  /v1/me/venues/{venueID}:
    parameters:
      - name: venueID
        in: path
        required: true
        schema:
          type: string
          format: uuid
          pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
    get:
      operationId: getMyVenue
      summary: Read one of the caller's venue submissions
      security:
        - bearerAuth: []
      responses:
        "200":
          description: The caller's submission in any status.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SubmissionResponse"
        "400":
          description: The id is not a canonical UUID, or a query parameter was sent.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
        "401":
          $ref: "#/components/responses/Unauthorized"
        "403":
          $ref: "#/components/responses/ContributionForbidden"
        "404":
          description: The Venue does not exist or belongs to another User.
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
        "502":
          $ref: "#/components/responses/AuthProvider"
    put:
      operationId: correctMyVenue
      summary: Correct a pending venue submission
      description: Replaces every editable field and the Genre set of a pending Venue the caller submitted. It does not require Idempotency-Key. An unchanged body returns 200 and leaves updated_at unchanged. Duplicate detection excludes this Venue. A correction does not count toward the submission quota.
      security:
        - bearerAuth: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: "#/components/schemas/SubmissionRequest"
      responses:
        "200":
          description: The corrected submission, still pending.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/SubmissionResponse"
        "400":
          description: The id or body is invalid, or a Genre id does not exist.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
        "401":
          $ref: "#/components/responses/Unauthorized"
        "403":
          $ref: "#/components/responses/ContributionForbidden"
        "404":
          description: The Venue does not exist or belongs to another User.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
        "409":
          description: The caller has no profile, the Venue is verified or rejected, or a strong duplicate exists.
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Error"
              examples:
                not_pending:
                  value:
                    error:
                      code: venue_not_pending
                      message: This venue can no longer be corrected.
                      request_id: abc
                rejected:
                  value:
                    error:
                      code: venue_not_pending
                      message: Only a pending venue can be corrected.
                      request_id: abc
                duplicate:
                  value:
                    error:
                      code: duplicate_venue
                      message: A matching venue already exists.
                      request_id: abc
        "413":
          $ref: "#/components/responses/BodyTooLarge"
        "415":
          $ref: "#/components/responses/UnsupportedMedia"
        "429":
          $ref: "#/components/responses/RateLimited"
        "500":
          $ref: "#/components/responses/Internal"
        "502":
          $ref: "#/components/responses/AuthProvider"
```

- [ ] **Step 4: Add the schemas and the shared 403 response**

Under `components.schemas`, after `VenueListResponse`, insert:

```yaml
    SubmissionRequest:
      type: object
      additionalProperties: false
      required: [name, latitude, longitude, address, city, country_code, venue_type, genre_ids]
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 150
          description: Trimmed and NFC-normalized. A single line.
        description:
          type: [string, "null"]
          minLength: 1
          maxLength: 2000
          description: Trimmed and NFC-normalized. Line breaks are allowed. Omit or send null for none.
        latitude:
          type: number
          minimum: -90
          maximum: 90
        longitude:
          type: number
          minimum: -180
          maximum: 180
        address:
          type: string
          minLength: 1
          maxLength: 500
          description: Trimmed and NFC-normalized. A single line.
        city:
          type: string
          minLength: 1
          maxLength: 120
          description: Trimmed and NFC-normalized. A single line.
        country_code:
          type: string
          description: Official ISO 3166-1 alpha-2 code, plus XK for Kosovo. Letter case is folded to uppercase. The United Kingdom is GB; UK is rejected.
        venue_type:
          type: string
          enum: [bar, pub, club]
        genre_ids:
          type: array
          minItems: 1
          description: Existing Genre ids. Duplicates are removed.
          items:
            type: integer
            minimum: 1
            maximum: 2147483647
    Submission:
      type: object
      additionalProperties: false
      required: [id, name, description, latitude, longitude, address, city, country_code, venue_type, genres, verification_status, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
          pattern: "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
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
          pattern: "^[A-Z]{2}$"
        venue_type:
          type: string
          enum: [bar, pub, club]
        genres:
          type: array
          items:
            $ref: "#/components/schemas/Genre"
        verification_status:
          type: string
          enum: [pending, verified, rejected]
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    SubmissionResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data:
          $ref: "#/components/schemas/Submission"
    SubmissionListResponse:
      type: object
      additionalProperties: false
      required: [data, meta]
      properties:
        data:
          type: array
          items:
            $ref: "#/components/schemas/Submission"
        meta:
          type: object
          additionalProperties: false
          required: [next_cursor]
          properties:
            next_cursor:
              type: [string, "null"]
```

In the `Error` schema, replace the `details` property:

```yaml
            details:
              type: object
              additionalProperties: false
              required: [suspended_until]
              properties:
                suspended_until:
                  type: string
                  format: date-time
```

with:

```yaml
            details:
              type: object
              additionalProperties: false
              properties:
                suspended_until:
                  type: string
                  format: date-time
                venue_id:
                  type: string
                  format: uuid
                verification_status:
                  type: string
                  enum: [pending, verified]
```

Under `components.responses`, after `UnsupportedMedia`, insert:

```yaml
    ContributionForbidden:
      description: The caller is a platform moderator, or, on a write, the caller's profile cannot contribute.
      content:
        application/json:
          schema:
            $ref: "#/components/schemas/Error"
          examples:
            forbidden:
              value:
                error:
                  code: forbidden
                  message: This action is unavailable for this account.
                  request_id: abc
            rename:
              value:
                error:
                  code: profile_rename_required
                  message: Rename this profile before continuing.
                  request_id: abc
            suspended:
              value:
                error:
                  code: profile_suspended
                  message: This profile is suspended.
                  request_id: abc
                  details:
                    suspended_until: "2026-10-16T12:00:00Z"
```

- [ ] **Step 5: Run the contract test**

Run: `go test . -run TestOpenAPIDocument -v`
Expected: PASS.

- [ ] **Step 6: Update `backend/AGENTS.md`**

Make these replacements:

- `` `GET /readyz` returns 200 when the database ping succeeds and migration `0006` or newer is recorded. `` → `` `GET /readyz` returns 200 when the database ping succeeds and migration `0007` or newer is recorded. ``
- `` - `internal/venue`: Genre list, public catalogue, and discovery filters, sort modes, and filter-bound cursors. `` → `` - `internal/venue`: Genre list, public catalogue, discovery filters, sort modes, filter-bound cursors, and Venue submissions (create, correct while pending, the caller's list and detail, duplicates, and the quota). ``
- `` - App routes are `GET`, `PATCH`, and `DELETE /v1/me` and `` → `` - App routes are `GET`, `PATCH`, and `DELETE /v1/me`, `POST /v1/venues`, `GET /v1/me/venues`, `GET` and `PUT /v1/me/venues/{venueID}`, and ``
- `` - Profile writes, profile reports, moderation actions, report dismissals, and `` → `` - Profile writes, profile reports, Venue submissions, changed Venue corrections, moderation actions, report dismissals, and ``

Wrap the edited lines to match the surrounding line width.

- [ ] **Step 7: Update `DevSteps.md`**

- Line 6: `**Status:** Sessions 1–8 are done. Session 9 is next. Sessions 10–14 are not started.` → `**Status:** Sessions 1–9 are done. Session 10 is next. Sessions 11–14 are not started.`
- In the session table, replace `| 9–14 | Not started |` with:

```markdown
| 9. Venue submissions | Done |
| 10–14 | Not started |
```

- Under `### Session 9: Venue submissions`, replace `**Status:** Not started.` with `**Status:** Done.` and append after the existing paragraph:

```markdown
Migration `0007` adds the `(submitted_by, created_at)` and
`(submitted_by, updated_at desc, id desc)` indexes that captured plans use and
raises readiness to `0007`. Rejection feedback and correction of a rejected
submission remain in Session 10.
```

- [ ] **Step 8: Run the full suite**

Run: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: `gofmt -l .` prints nothing; vet, tests, and build pass.

- [ ] **Step 9: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md DevSteps.md
git commit -m "docs: publish the venue submission contract"
```

---

## Self-Review Notes

- Spec coverage: create (Task 5), list and detail (Task 4), pending correction and unchanged correction (Task 6), duplicate rules and disclosure (Tasks 3, 5, 6), quota and `Retry-After` (Tasks 3, 5, 6), contribution rules and expiry (Tasks 2, 5, 6), idempotency replay, reuse, and invalid key (Tasks 5, 6), write tokens (Tasks 5, 6), indexes and plans (Tasks 1, 7), readiness (Task 1), HTTP mapping (Task 8), OpenAPI, AGENTS, DevSteps (Task 9).
- The catalogue keeps its `Catalogue` interface; `*venue.Service` satisfies both `Catalogue` and `Submissions`.
- Data API denial on `venues` is already pinned by `TestDataAPIClosed`; `0007` grants nothing.
