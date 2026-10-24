# Session 13: Check-ins Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A signed-in User can record a visit to a public Venue, read their own Check-in history, and delete one Check-in, and the venue catalogue shows a real `check_in_count`.

**Architecture:** Migration `0011` adds `check_ins`. A new capability package `internal/checkin` owns visit validation, the cooldown, authorship, the write transaction, history, and deletion. `internal/api` adds a `CheckIns` interface (PostgreSQL service plus HTTP-test fake) and three routes. The venue catalogue select counts `check_ins` in the same query; `internal/venue` never imports `internal/checkin`. `internal/config` parses `CHECKIN_COOLDOWN`.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL/PostGIS via `testcontainers-go` (`internal/pgtest`), OpenAPI 3.1.

**Spec:** `docs/superpowers/specs/2026-10-24-session-13-check-ins-design.md`

## Global Constraints

- Run Go commands from `backend/`. Plain `go`, `gofmt`; no wrappers.
- Do not edit migrations `0001`–`0010`. Add no Go dependency.
- `cmd/api` stays composition only.
- `POST` requires `Idempotency-Key`; `DELETE` ignores a sent key. Operation name `check_in`.
- Every DB statement uses a 2-second `context.WithTimeout`.
- `internal/checkin` imports `profile`, `ratelimit`, `idempotency`; never `internal/venue`. `internal/venue` never imports `internal/checkin`.
- PostgreSQL stores microseconds. The service truncates the visit time and `recorded_at` to `time.Microsecond` before insert, so a `201` body equals the stored row.
- Exact copy: `Venue not found.`, `Check-in not found.`, `Another check-in at this venue is too close to this visit.`, `visited_at: must be a timestamp`, `visited_at: must be within the last 24 hours`, `visited_at: must not be more than 5 minutes ahead`, `check_in_id: must be a canonical uuid`, `venue_id: must be a canonical uuid`, `limit: must be from 1 to 100`, `cursor: invalid`, `idempotency_key: required`, `CHECKIN_COOLDOWN must be a positive duration`.
- Error codes: `venue_not_found` (404), `check_in_not_found` (404), `check_in_cooldown` (409, `details.check_in_id`, `details.visited_at`), plus the existing `profile_required`, `profile_rename_required`, `profile_suspended`, `idempotency_key_reused`, `rate_limited`, `validation_error`.
- Out of scope: public check-in lists, operator reads, check-in reports, photos, coordinates, quotas, stored counters.
- Integration tests skip locally without Docker through `pgtest`; CI treats failures as blocking.
- Definition of done: `gofmt -l .` prints nothing; `go vet ./...` and `go test ./...` pass.

## Review Focus

- A client retries a backdated Check-in with the same key after that visit has aged past 24 hours. It must replay the stored `201`, not fail the window check. The stored-key lookup therefore runs before the window check. Pinned in Task 4 (`TestCreateReplayAfterWindow`).
- A client sends `visited_at` with nanoseconds (`2026-10-24T09:00:00.123456789Z`). The `201` body and the history row must both show `…00.123456Z`, and the history cursor built from that row must page correctly. Pinned in Task 3 (`TestResolveVisit`) and Task 5 (`TestHistoryPagination`).
- A Check-in made before a Venue was delisted must still block another Check-in after the Venue is verified again, inside the cooldown. Pinned in Task 4 (`TestCooldownSurvivesDelisting`).
- Another User's Check-in at the same Venue and time must not block the caller. Pinned in Task 4 (`TestCooldownIsPerUser`).
- `visited_at` without a zone (`"2026-10-24T10:00:00"`), a date only, a number, or an object must be the `visited_at` field error, not a fieldless malformed-body error. Pinned in Task 3 (`TestParseVisitedAt`) and Task 7 (`TestCheckInRoutes`).

---

## File map

| File | Responsibility |
| --- | --- |
| `infra/supabase/migrations/0011_check_ins.sql` | `check_ins` table, three indexes, RLS, revokes |
| `backend/internal/database/pool.go` | `requiredMigrationVersion = "0011"` |
| `backend/internal/database/postgres_test.go` | readiness chain through `0011` |
| `backend/internal/database/access_test.go` | Data API roles denied on `check_ins`; User deletion removes Check-ins |
| `backend/internal/config/config.go`, `config_test.go` | `CHECKIN_COOLDOWN` |
| `backend/.env.example` | documents `CHECKIN_COOLDOWN` |
| `backend/internal/checkin/checkin.go` | types, errors, visit parsing and resolution, cursor |
| `backend/internal/checkin/postgres.go` | `Service`, `New`, `Create`, `ListOwn`, `Delete`, SQL |
| `backend/internal/checkin/*_test.go` | unit and integration tests |
| `backend/internal/venue/postgres.go`, `postgres_test.go` | `check_in_count` subquery and tests |
| `backend/internal/api/checkin.go`, `checkin_test.go` | `CheckIns` interface, handlers, views, error mapping, HTTP tests |
| `backend/internal/api/server.go` | `Options.CheckIns`, three routes |
| `backend/cmd/api/main.go` | construct `checkin.New`, pass `CheckIns` |
| `backend/openapi.yaml`, `backend/contract_test.go` | contract |
| `backend/AGENTS.md`, `docs/architecture/backend.md`, `DevSteps.md` | docs |

---

### Task 1: Migration `0011` and readiness

**Files:**
- Create: `infra/supabase/migrations/0011_check_ins.sql`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go:138-143`
- Modify: `backend/internal/database/access_test.go`

**Interfaces:**
- Produces: table `check_ins(id, venue_id, user_id, visited_at, recorded_at)`; indexes `check_ins_user_history_idx`, `check_ins_user_venue_visit_idx`, `check_ins_venue_idx`. Later tasks query these names.

- [ ] **Step 1: Write the failing tests**

In `access_test.go`, append to `statements`:
```go
`select * from check_ins`,
`insert into check_ins (venue_id, user_id, visited_at, recorded_at) values ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', now(), now())`,
`update check_ins set visited_at = now() where false`,
`delete from check_ins where false`,
```
Add `"check_ins"` to the `relrowsecurity` table list.

In the User-deletion block of the same test, after the `profiles` insert and before `delete from auth.users`, insert one Check-in:
```sql
insert into check_ins (venue_id, user_id, visited_at, recorded_at)
select id, 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa', now(), now() from venues where name = 'Access'
```
After the delete, assert `select count(*) from check_ins` is `0`. The existing `venues` assertion stays (one row, `submitted_by` null).

In `postgres_test.go`, the `0010` step now expects `ready == false, reason == "migrations_behind"`. Then apply `0011_check_ins.sql`, insert version `'0011'`, and expect `ready && reason == "ok"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/database/ -run 'TestDataAPIClosed|TestMigrationVersions' -v`
Expected: FAIL (`relation "check_ins" does not exist`).

- [ ] **Step 3: Write `0011_check_ins.sql` and bump readiness**

One-line header comment, like `0009`. Columns per the spec's Data section: `id uuid primary key default gen_random_uuid()`; `venue_id uuid not null references venues (id) on delete cascade`; `user_id uuid not null references auth.users (id) on delete cascade`; `visited_at timestamptz not null`; `recorded_at timestamptz not null`. No defaults on the timestamps, no check constraints. Then:
```sql
create index check_ins_user_history_idx on check_ins (user_id, visited_at desc, id desc);
create index check_ins_user_venue_visit_idx on check_ins (user_id, venue_id, visited_at);
create index check_ins_venue_idx on check_ins (venue_id);
alter table check_ins enable row level security;
revoke all on table check_ins from public, anon, authenticated, service_role;
```
No sequence revoke, no policy.

`pool.go`: `const requiredMigrationVersion = "0011"`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/database/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0011_check_ins.sql backend/internal/database
git commit -m "feat: add the check_ins table and require migration 0011"
```

---

### Task 2: `CHECKIN_COOLDOWN` configuration

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/.env.example`

**Interfaces:**
- Produces: `Config.CheckInCooldown time.Duration`. Task 7 passes it to `checkin.New`.

- [ ] **Step 1: Write the failing tests**

In `TestLoadEnvironment`, after the first `load`: `cfg.CheckInCooldown == 6*time.Hour`. Then set `lookup["CHECKIN_COOLDOWN"] = " 90m "` and assert `90*time.Minute`; then `""` and assert `6*time.Hour`.

In `TestLoadRejectsEnvironment`, add three cases built on a fully valid environment (`DATABASE_URL`, `SUPABASE_URL`, `SUPABASE_JWT_AUDIENCE`, `SUPABASE_SERVICE_ROLE_KEY` all valid), each with `errText: "CHECKIN_COOLDOWN must be a positive duration"`:
- `"CHECKIN_COOLDOWN": "0s"`;
- `"CHECKIN_COOLDOWN": "-1h"`;
- `"CHECKIN_COOLDOWN": "six hours"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ -v`
Expected: FAIL (`cfg.CheckInCooldown undefined`).

- [ ] **Step 3: Implement `parseCheckInCooldown(value string) (time.Duration, error)`**

Trim the value. Empty returns `6 * time.Hour`. Otherwise `time.ParseDuration`; an error or a result `<= 0` returns `fmt.Errorf("CHECKIN_COOLDOWN must be a positive duration")`. `load` calls it after `SUPABASE_SERVICE_ROLE_KEY` and sets `CheckInCooldown`.

In `.env.example`, append:
```text
# Minimum gap between one User's check-ins at one Venue. Unset also means 6h.
CHECKIN_COOLDOWN=6h
```
Leave `infra/render.yaml` unchanged.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config backend/.env.example
git commit -m "feat: configure the check-in cooldown"
```

---

### Task 3: Check-in domain values, visit parsing, and cursor

**Files:**
- Create: `backend/internal/checkin/checkin.go`
- Create: `backend/internal/checkin/checkin_test.go`

**Interfaces:**
- Produces (package `checkin`):
```go
type VenueSummary struct {
	Name               string
	City               string
	CountryCode        string
	VenueType          *string // nil when the Venue is rejected or delisted
	VerificationStatus string
}
type CheckIn struct {
	ID         string
	VenueID    string
	VisitedAt  time.Time
	RecordedAt time.Time
	Venue      VenueSummary
}
type CreateCommand struct {
	UserID    string
	VenueID   string
	Key       string
	VisitedAt json.RawMessage // nil when omitted
}
type ListQuery struct {
	UserID string
	Limit  int
	Cursor string
}
type Page struct {
	CheckIns   []CheckIn
	NextCursor *string
}
type ValidationError struct{ Fields map[string]string }
type RateLimitError struct{ RetryAfter int }
type CooldownError struct {
	CheckInID string
	VisitedAt time.Time
}
var ErrVenueNotFound   = errors.New("checkin: venue not found")
var ErrCheckInNotFound = errors.New("checkin: not found")

func CanonicalID(id string) bool
func ValidateLimit(limit int) error
func ParseVisitedAt(raw json.RawMessage) (*time.Time, error)
func resolveVisit(requested *time.Time, now time.Time) (time.Time, error)
func encodeCursor(visitedAt time.Time, id string) string
func decodeCursor(cursor string) (time.Time, string, error)
```
`Error()` strings: `"checkin: validation failed"`, `"checkin: rate limited"`, `"checkin: cooldown"`.

- [ ] **Step 1: Write the failing tests**

`TestParseVisitedAt`, table-driven:
- nil and `null` → `(nil, nil)`;
- `"2026-10-24T10:00:00Z"` and `"2026-10-24T12:00:00+02:00"` → the same instant, `Location() == time.UTC`;
- `"2026-10-24T10:00:00.123456789Z"` → nanoseconds kept;
- `"2026-10-24T10:00:00"`, `"2026-10-24"`, `"yesterday"`, `5`, `true`, `{}`, `[]` → `*ValidationError` with exactly `{"visited_at": "must be a timestamp"}`.

`TestResolveVisit` with `now := time.Date(2026, 10, 24, 10, 0, 0, 500, time.UTC)`:
- nil → `now.Truncate(time.Microsecond)`;
- `now - 24h` → that value truncated; `now - 24h - 1ns` → `{"visited_at": "must be within the last 24 hours"}`;
- `now + 5m` → `now` truncated; `now + 5m + 1ns` → `{"visited_at": "must not be more than 5 minutes ahead"}`;
- `now + 1s` → `now` truncated;
- `now - 1h + 123456789ns` → nanoseconds truncated to `123456000`.

`TestCursorRoundTrip`:
- `decodeCursor(encodeCursor(t, id))` returns `t` (equal, UTC) and `id`;
- `""`, `"abc"`, base64 of `check-in-v2|…`, a non-canonical id, a bad timestamp → `{"cursor": "invalid"}`.

`TestValidateLimit`: `0` and `101` → `{"limit": "must be from 1 to 100"}`; `1` and `100` → nil.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/checkin/ -v`
Expected: FAIL (package does not compile).

- [ ] **Step 3: Implement in `checkin.go`**

`CanonicalID` uses the same lowercase UUID regexp as `review.CanonicalID`. `ParseVisitedAt` unmarshals into a `string` and parses with `time.RFC3339Nano`, then returns `.UTC()`. `resolveVisit` checks the window against the untruncated `now`, clamps a future value to `now`, then returns `.Truncate(time.Microsecond)`. Cursor payload is `check-in-v1|{visited_at RFC3339Nano UTC}|{id}` in `base64.RawURLEncoding`, shaped like `review.encodeCursor` without the Venue part.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/checkin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/checkin
git commit -m "feat: parse check-in visit times and history cursors"
```

---

### Task 4: Create a Check-in

**Files:**
- Create: `backend/internal/checkin/postgres.go`
- Create: `backend/internal/checkin/helpers_test.go`
- Create: `backend/internal/checkin/create_test.go`

**Interfaces:**
- Consumes: Task 3 types; `profile.Service.NormalizeExpiry(ctx, userID) error`; `profile.RequireContributor(ctx, tx, userID) error`; `idempotency.Lookup`, `idempotency.Execute`; `ratelimit.UserLimiter.Reserve(userID) (*ratelimit.Reservation, int, bool)`.
- Produces:
```go
type Service struct { /* pool, profiles, writes, clock, cooldown */ }
// profiles and writes may be nil for read-only use; writes then fail.
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time, cooldown time.Duration) *Service
func (s *Service) Create(ctx context.Context, cmd CreateCommand) (CheckIn, error)
var errWritesUnavailable = errors.New("checkin: writes are not configured")
```

- [ ] **Step 1: Write the test helpers**

In `helpers_test.go`, copy the shapes from `internal/review/postgres_test.go`: `testPool`, `ptr`, `insertUser(t, pool, id, status string, until *time.Time)`, `insertVenue(t, pool, id, status string)` (status `bare` is verified with no Genre), and the UUID constants `verifiedVenue`, `pendingVenue`, `rejectedVenue`, `delistedVenue`, `bareVenue`, `unknownVenue`, `userA`, `userB`, `userC`. Add:
```go
func insertCheckIn(t *testing.T, pool *pgxpool.Pool, venueID, userID string, visited time.Time) string
func countCheckIns(t *testing.T, pool *pgxpool.Pool) int
func tokensLeft(limiter *ratelimit.UserLimiter, userID string) int // copy from review/write_test.go
```
A `createHarness` holds the pool, a mutable `now` (start `t0 = time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)`), the limiter, and `New(pool, profile.New(pool, limiter, nil, clock), limiter, clock, 6*time.Hour)`. Its `create(userID, venueID, key string, visitedAt json.RawMessage)` calls `Create`.

- [ ] **Step 2: Write the failing tests**

In `create_test.go`:
- `TestCreateNow`: `{}` on `verifiedVenue` → `VisitedAt == RecordedAt == t0`, `Venue` is `Venue`/`Oslo`/`DE`/`bar`/`verified`, one row, tokens `59`.
- `TestCreateBackdatedAndClamped`: `t0-24h` is stored with `RecordedAt == t0`; `t0+5m` is stored at `t0`. Neither `t0-24h-1µs` nor `t0+5m+1µs` inserts (`ValidationError`, row count unchanged, no token spent).
- `TestCreateEquivalentOffsets`: key `same-key-1` with `"2026-10-24T09:00:00Z"`, then the same key with `"2026-10-24T11:00:00+02:00"` → the second returns the first `ID`; one row.
- `TestCreateReplay`: replay with the same key returns the stored `CheckIn`, spends no token, inserts nothing. Then delete the row in SQL and set the Venue `delisted`; replay still returns the same `ID`.
- `TestCreateReplayAfterWindow`: create with `t0-23h` and key `back-key-1`; advance `now` by 2h; the same key and body replay the original (no `ValidationError`).
- `TestCreateKeyReused`: after success, same key with another `visited_at` → `errors.Is(err, idempotency.ErrReused)`.
- `TestCooldown`: seed a Check-in at `t0` for `userA`. A `{}` create at `t0+6h-1µs` → `*CooldownError` with that `CheckInID` and `VisitedAt == t0`, no idempotency row (`select count(*) from idempotency_records` = 0), tokens 60. Advance to `t0+6h`; the same key and `{}` inserts.
- `TestCooldownExplicitRetry`: a conflicting explicit `visited_at` on `retry-key-1` conflicts again with the same body; the same key with a `visited_at` outside the window then inserts.
- `TestCooldownNearestAndTie`: on `verifiedVenue`, seed `userA` rows at `t0-2h` and `t0+1h`; a create at `t0` returns the `t0+1h` row. On `pendingVenue`, insert two `userA` rows in SQL with explicit ids `00000000-0000-4000-8000-0000000000a2` at `t0-1h` and `00000000-0000-4000-8000-0000000000a1` at `t0+1h`; a create at `t0` returns `…a1`.
- `TestCooldownIsPerUser`: `userB` has a Check-in at `t0`; `userA` creates at `t0` successfully.
- `TestCooldownSurvivesDelisting`: seed `userA` at `t0` on `verifiedVenue`, set `delisted`, then `verified` again; a create at `t0+1h` → `*CooldownError`.
- `TestCreateConcurrent`: for `userA`, two goroutines with keys `race-key-a` and `race-key-b` and the same `{}` → one success, one `*CooldownError`, one row, `userA` tokens 59. Then for `userB`, two goroutines with key `race-key-c` and the same `{}` → the same `ID`, and one new row.
- `TestCreateVisibility`: `pendingVenue` succeeds for a User who is not its Submitter, and its summary is `pending` with `VenueType` set. `rejectedVenue`, `delistedVenue`, `bareVenue`, `unknownVenue` → `ErrVenueNotFound`. A suspended `userC` on `rejectedVenue` → `ErrVenueNotFound`, while on `verifiedVenue` → `*profile.SuspendedError`.
- `TestCreateProfileErrors`: no Profile → `profile.ErrProfileRequired`; `rename_required` → `profile.ErrRenameRequired`; an expired suspension (`until = t0-1h`) succeeds.
- `TestCreateRateLimited`: exhaust `userA`'s tokens by holding 60 reservations → `*RateLimitError`, no row.
- `TestCreateInvalid`: uppercase Venue id → `{"venue_id": "must be a canonical uuid"}`; key `short` → `idempotency.ErrInvalid`.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/checkin/ -run 'TestCreate|TestCooldown' -v`
Expected: FAIL (`New` undefined).

- [ ] **Step 4: Implement `New` and `Create` in `postgres.go`**

Order:
1. Canonical Venue id; `ParseVisitedAt`; nil `profiles`/`writes` → `errWritesUnavailable`.
2. `now := s.clock()`. Command is `json.Marshal` of `struct{VenueID string \`json:"venue_id"\`; VisitedAt *string \`json:"visited_at"\`}` with the parsed time formatted `time.RFC3339Nano` in UTC, or nil.
3. 2-second `txCtx`; `NormalizeExpiry`; begin.
4. `idempotency.Lookup` with operation `check_in`. Found → decode the stored body, commit, return it.
5. `resolveVisit(requested, now)`.
6. Load the Venue `for share of v` with the catalogue predicate, selecting `name, city, country_code, venue_type, verification_status`. No row → `ErrVenueNotFound`.
7. `profile.RequireContributor`.
8. `select pg_advisory_xact_lock(hashtextextended('check_in' || chr(31) || $1 || chr(31) || $2, 0))` with user then Venue id.
9. `idempotency.Execute`. Callback: the cooldown query below → `*CooldownError`; `Reserve` → `*RateLimitError`; insert with `visited_at = visit`, `recorded_at = now.Truncate(time.Microsecond)`, `returning id::text`; marshal the body; return `{Status: 201}`.
10. Decode, commit, commit the reservation. Cancel it on any other exit.

Cooldown query, with `$3 = visit - cooldown`, `$4 = visit + cooldown`, `$5 = visit`:
```sql
select id::text, visited_at from check_ins
where user_id = $1::uuid and venue_id = $2::uuid
  and visited_at > $3 and visited_at < $4
order by abs(extract(epoch from visited_at - $5::timestamptz)), id
limit 1
```

The stored body is the Check-in document without the `data` envelope. Its JSON keys are `id`, `venue_id`, `visited_at`, `recorded_at`, and `venue` (`name`, `city`, `country_code`, `venue_type` with `omitempty`, `verification_status`).

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/checkin/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/checkin
git commit -m "feat: record a check-in behind the cooldown"
```

---

### Task 5: History and deletion

**Files:**
- Modify: `backend/internal/checkin/postgres.go`
- Create: `backend/internal/checkin/history_test.go`

**Interfaces:**
- Produces:
```go
func (s *Service) ListOwn(ctx context.Context, q ListQuery) (Page, error)
func (s *Service) Delete(ctx context.Context, userID, checkInID string) error
// historySQL is used by ListOwn and the index plan test.
const historySQL = "…" // params: $1 user, $2 cursor time or null, $3 cursor id or null, $4 limit
```

- [ ] **Step 1: Write the failing tests**

- `TestHistoryPagination`: `userA` creates one Check-in on `verifiedVenue` through `Create` with `visited_at` `t0-1h+123456789ns`. Then seed two `userA` rows at `t0-2h` in SQL, one on `pendingVenue` and one on `delistedVenue`. Seed one `userB` row. `Limit: 1` returns the created row, which reads back as `…123456000`, with a cursor. `Limit: 2` with that cursor returns the two tied rows, id descending, and a `nil` cursor. `userB`'s row never appears. A User with an `auth.users` row and no Profile gets non-nil `CheckIns` with length 0.
- `TestHistorySummaries`: rows on `verifiedVenue`, `pendingVenue`, `rejectedVenue`, and `delistedVenue` (seeded in SQL). Rejected and delisted have `VenueType == nil` and status `rejected`/`delisted`. Set the delisted Venue back to `verified`: the same row now has `VenueType` `bar`.
- `TestHistoryReadableWhenRestricted`: a `suspended` and a `rename_required` caller each list their rows; tokens unchanged.
- `TestHistoryPlan`: seed enough rows (mirror `review` `TestListPlan`: 5,000 rows across 100 users), `vacuum analyze check_ins`, `explain` `historySQL` → contains `check_ins_user_history_idx`.
- `TestDelete`: delete own row → nil, row gone, tokens 59. A second delete → `ErrCheckInNotFound`. `userB` deleting `userA`'s row → `ErrCheckInNotFound`, row remains. Deleting a row on `delistedVenue` works and removes its cooldown block (a later `Create` at the same time on that Venue still returns `ErrVenueNotFound`).
- `TestDeleteProfileErrors`: a suspended owner → `*profile.SuspendedError`, row remains; a suspended caller with another User's id → `ErrCheckInNotFound`; no Profile with an unknown id → `ErrCheckInNotFound`.
- `TestDeleteRateLimited`: exhausted tokens → `*RateLimitError`, row remains.
- `TestDeleteInvalid`: `"abc"` → `{"check_in_id": "must be a canonical uuid"}`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/checkin/ -run 'TestHistory|TestDelete' -v`
Expected: FAIL (`ListOwn` undefined).

- [ ] **Step 3: Implement `ListOwn` and `Delete`**

`historySQL` joins `venues v on v.id = c.venue_id`, filters `c.user_id = $1::uuid` and `($2::timestamptz is null or (c.visited_at, c.id) < ($2, $3::uuid))`, orders `c.visited_at desc, c.id desc`, limit `$4`. Go sets `VenueType` to nil when status is `rejected` or `delisted`. `ListOwn` validates the limit, decodes the cursor, fetches `Limit+1`, and builds `next_cursor` from the last returned row.

`Delete`: canonical id; nil writes → `errWritesUnavailable`; `NormalizeExpiry`; begin; `select 1 from check_ins where id = $1::uuid and user_id = $2::uuid for update` (no row → `ErrCheckInNotFound`); `RequireContributor`; `Reserve`; delete; commit; commit the reservation.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/checkin/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/checkin
git commit -m "feat: list and delete the caller's check-ins"
```

---

### Task 6: Catalogue `check_in_count`

**Files:**
- Modify: `backend/internal/venue/postgres.go:244`
- Modify: `backend/internal/venue/postgres_test.go`

**Interfaces:**
- Consumes: table `check_ins` from Task 1.

- [ ] **Step 1: Write the failing test**

Remove the `CheckInCount != 0` assertion from `checkAggregate`. Add `TestCheckInCountAggregates` with a verified and a pending Venue and four users, and a helper `insertCheckIn(t, pool, venueID, userID string)`. Assert through both `ListVenues` and `GetVenue`:
- no Check-ins → `0` on both;
- three on verified, two on pending → `3` and `2`, while pending keeps `average_rating` nil and `review_count` 0;
- delete one verified row → `2`;
- set verified to `delisted` → absent from list and `ErrNotFound`; set back to `verified` → `2`;
- set pending to `rejected` → absent; set back to `pending` → `2`;
- delete the remaining verified rows → `0`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue/ -run TestCheckInCountAggregates -v`
Expected: FAIL (count `0`, want `3`).

- [ ] **Step 3: Replace the literal**

In `venueSelect`, replace the `0,` before `venue_name_key(v.name)` with:
```sql
(select count(*)::int from check_ins c where c.venue_id = v.id),
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/venue/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue
git commit -m "feat: count check-ins on the venue catalogue"
```

---

### Task 7: HTTP routes and wiring

**Files:**
- Create: `backend/internal/api/checkin.go`
- Create: `backend/internal/api/checkin_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: Tasks 3–5 (`checkin.CreateCommand`, `checkin.ListQuery`, `checkin.Page`, `checkin.CheckIn`, errors), Task 2 (`cfg.CheckInCooldown`).
- Produces:
```go
type CheckIns interface {
	Create(context.Context, checkin.CreateCommand) (checkin.CheckIn, error)
	ListOwn(context.Context, checkin.ListQuery) (checkin.Page, error)
	Delete(ctx context.Context, userID, checkInID string) error
}
// Options gains: CheckIns CheckIns
```

- [ ] **Step 1: Write the failing tests**

`fakeCheckIns` records the last command, query, user, id, and call count, and returns configured values, like `fakeReviews`. `checkInHandler(authenticator, checkIns)` mirrors `reviewHandler`. Paths: `/v1/venues/aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa/check-ins`, `/v1/me/check-ins`, `/v1/me/check-ins/dddddddd-dddd-4ddd-8ddd-dddddddddddd`.

`TestCheckInRoutes`:
- `POST` `{}` with key `checkin-key-1` → `201`. The fake saw that user, Venue, key, and nil `VisitedAt`. The body contains `"visited_at":"2026-10-24T10:00:00Z"`, `"recorded_at"`, `"venue_id"`, and `"venue":{"name":"The Riff","city":"Berlin","country_code":"DE","venue_type":"bar","verification_status":"verified"}`.
- `POST` `{"visited_at":5}` passes the raw `5` through. The fake returns `{"visited_at": "must be a timestamp"}`, and the response is `400` with that field.
- `POST` without a key → `400` with `"idempotency_key":"required"` and no service call. An unknown field, `not json`, `text/plain`, a body over 64 KiB, and `?x=1` are rejected with no service call.
- `GET` → `limit` 20, then `?limit=5&cursor=abc` passes through, and `next_cursor` is in the body. `?limit=x`, `?limit=1&limit=2`, and `?sort=x` → `400` with no call. A rejected-Venue item has no `"venue_type"` key and shows `"verification_status":"rejected"`.
- `DELETE` with header `Idempotency-Key: bad` → `204`, empty body, the fake saw the id. `?x=1` → `400`.

`TestCheckInAuth`: all three routes without a token → `401 unauthorized`; operator → `assertForbidden`; no service calls.

`TestCheckInErrors`, table-driven on `POST`:
- `checkin.ErrVenueNotFound` → `404 venue_not_found`, `Venue not found.`;
- `&checkin.CooldownError{CheckInID: "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", VisitedAt: <2026-10-24T09:00:00Z>}` → `409 check_in_cooldown`, body contains `"details":{"check_in_id":"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee","visited_at":"2026-10-24T09:00:00Z"}`;
- `idempotency.ErrReused` → `409 idempotency_key_reused`; `idempotency.ErrInvalid` → `400 validation_error`;
- `profile.ErrProfileRequired`, `profile.ErrRenameRequired`, and `&profile.SuspendedError{Until: …}` → the existing codes, with `suspended_until` on the last;
- `&checkin.RateLimitError{RetryAfter: 2}` → `429 rate_limited`, `Retry-After: 2`;
- `errors.New("db down")` → `500 internal_error`.
Then on `DELETE`: `checkin.ErrCheckInNotFound` → `404 check_in_not_found`, `Check-in not found.`; `&checkin.ValidationError{Fields: {"check_in_id": "must be a canonical uuid"}}` → `400` with that field.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestCheckIn' -v`
Expected: FAIL (`Options.CheckIns` undefined).

- [ ] **Step 3: Implement `checkin.go`, routes, and wiring**

Handlers `createCheckIn`, `listCheckIns`, `deleteCheckIn`, and `writeCheckInError` follow `review.go`. `POST` order: `rejectedQuery(r, nil)`, caller, `DecodeJSON` into `struct{ VisitedAt json.RawMessage \`json:"visited_at"\` }`, key check, service. `GET` uses `rejectedQuery(r, map[string]bool{"limit": false, "cursor": false})` and `parseVenueLimit`. `DELETE` uses `rejectedQuery(r, nil)` and never reads the key. `writeCheckInError` calls `writeContributionError` first. The cooldown uses `WriteErrorDetails` with `map[string]any{"check_in_id": e.CheckInID, "visited_at": e.VisitedAt.UTC()}`.

The view types mirror the stored body: `venue_type` is `*string` with `omitempty`, and timestamps are `time.Time` in UTC.

In `server.go`, add `CheckIns CheckIns` to `Options` and register `POST /v1/venues/{venueID}/check-ins`, `GET /v1/me/check-ins`, and `DELETE /v1/me/check-ins/{checkInID}` with `requireApp`.

In `main.go`, add `checkIns := checkin.New(pool.Raw(), profiles, userLimiter, time.Now, cfg.CheckInCooldown)` and `CheckIns: checkIns`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/api/ -v && go build ./...`
Expected: PASS; build succeeds.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api backend/cmd/api
git commit -m "feat: route check-in creation, history, and deletion"
```

---

### Task 8: Contract and docs

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the routes, documents, and error codes from Tasks 4–7.

- [ ] **Step 1: Write the failing contract test**

Add these strings to the `want` list: `/v1/venues/{venueID}/check-ins`, `/v1/me/check-ins`, `/v1/me/check-ins/{checkInID}`, `createCheckIn`, `listMyCheckIns`, `deleteMyCheckIn`, `check_in_cooldown`, `check_in_not_found`, `Another check-in at this venue is too close to this visit.`, `CheckIn:`, `CheckInVenue:`, `CheckInRequest:`, `check_in`. Add the three operation ids to the bearer-security list. Assert that the `CheckInVenue` schema block does not list `venue_type` under `required`.

- [ ] **Step 2: Run to verify failure**

Run: `go test . -run TestOpenAPIDocument -v`
Expected: FAIL (`missing "/v1/venues/{venueID}/check-ins"`).

- [ ] **Step 3: Write the contract and docs**

`openapi.yaml`:
- Add `/v1/venues/{venueID}/check-ins` after `/v1/venues/{venueID}/reviews/{reviewID}/reports`.
- Add `/v1/me/check-ins` and `/v1/me/check-ins/{checkInID}` after `/v1/me/venues/{venueID}`.
- `createCheckIn` documents the `Idempotency-Key` header, `CheckInRequest` (optional nullable `visited_at`, `format: date-time`), `201` `CheckInResponse`, `400` with the three `visited_at` messages and `idempotency_key`, `401`, `403` `ContributionForbidden`, `404 venue_not_found`, `409` examples for `check_in_cooldown` (with `details`), `profile_required`, and `idempotency_key_reused`, `413`, `415`, and `429`. The cooldown description states the six-hour default and that the code stays `check_in_cooldown` when it changes.
- `listMyCheckIns` documents `limit` and `cursor`, and a `200` `CheckInListResponse` with `data` and `meta.next_cursor`.
- `deleteMyCheckIn` documents `204`, `400` `check_in_id`, `401`, `403`, `404 check_in_not_found`, and `429`. Its description says it does not take an `Idempotency-Key`.
- Schemas: `CheckInRequest`, `CheckIn` (`id`, `venue_id`, `visited_at`, `recorded_at`, `venue`), `CheckInVenue` (`venue_type` optional; its description says it is absent for rejected and delisted Venues), `CheckInResponse`, `CheckInListResponse`.
- Give one `PublicVenue` example a non-zero `check_in_count`.

`backend/AGENTS.md`: add an `internal/checkin` line to Layout. Readiness becomes `0011`. Add the three routes to the app-route list. Add Check-in creation and deletion to the per-User bucket list. Add a Runtime line for `CHECKIN_COOLDOWN`: unset means six hours, any other value is a positive Go duration, and an invalid value stops startup.

`docs/architecture/backend.md`: after the Review paragraph under Capability modules, add one paragraph. The Check-in module owns visit validation, the cooldown, authorship, the write transaction, personal history, and deletion. The Venue catalogue query reads `check_ins` for `check_in_count`. Each package keeps its own SQL, and `internal/venue` does not import `internal/checkin`.

`DevSteps.md`: the status line becomes `Sessions 1–13 are done. Session 14 is next. Session 14 is not started.` The sequence table gains `| 13. Check-ins | Done |` and `| 14 | Not started |`. Session 13 becomes `**Status:** Done.` plus one paragraph naming the three routes, migration `0011`, readiness `0011`, `CHECKIN_COOLDOWN`, and the real `check_in_count`.

- [ ] **Step 4: Run the full gate**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt` prints nothing; vet and tests pass.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md docs/architecture/backend.md DevSteps.md
git commit -m "docs: publish the check-in contract and close session 13"
```
