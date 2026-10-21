# Session 11: Reviews and ratings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Android can show, create, edit, and delete one Review per User and Verified Venue, and the venue catalogue shows real rating aggregates.

**Architecture:** Migration `0009` adds `reviews`. A new capability package `internal/review` owns validation, visibility, the write transaction, the public list, and the caller's own Review. `internal/api` adds a `Reviews` interface (PostgreSQL service plus HTTP-test fake) and four routes. The venue catalogue select reads `reviews` for `average_rating` and `review_count` in the same query; `internal/venue` never imports `internal/review`.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL/PostGIS via `testcontainers-go` (`internal/pgtest`), OpenAPI 3.1.

**Spec:** `docs/superpowers/specs/2026-10-21-session-11-reviews-ratings-design.md`

## Global Constraints

- Run every command from `backend/`. Plain `go`, `gofmt`; no wrappers.
- Do not edit migrations `0001`–`0008`. Add no Go dependency.
- `cmd/api` stays composition only.
- No `Idempotency-Key` on any review route; a sent key is ignored.
- Out of scope (Session 12): `commentary_removed`, `409 commentary_locked`, commentary restrictions, Review Reports, review audit.
- `check_in_count` stays `0`.
- Every DB statement uses a 2-second `context.WithTimeout`, like `internal/venue`.
- Exact copy: `Venue not found.`, `Review not found.`, `Reviews open after this venue is verified.`, `rating: must be between 1 and 5`, `commentary: must be text`, `commentary: must be at most 2000 characters`, `limit: must be from 1 to 100`, `cursor: invalid`, `venue_id: must be a canonical uuid`.
- Error codes: `venue_not_found` (404), `review_not_found` (404), `venue_not_verified` (409), plus the existing `profile_required`, `profile_rename_required`, `profile_suspended`, `rate_limited`, `validation_error`.
- Integration tests skip locally without Docker through `pgtest`; CI treats them as blocking.
- Definition of done: `gofmt -l .` prints nothing; `go vet ./...` and `go test ./...` pass.

## Review Focus

- `rating` sent as `4.0`, `"4"`, `true`, or `4e0` must be a `rating` field error, not a fieldless malformed-body error. Pinned in Task 2 (`TestParseDraft`) and Task 6 (`TestReviewRoutes`).
- A User whose suspension already expired must be able to save a Review on the first request. Pinned in Task 4 (`TestSaveAfterExpiredSuspension`).
- A Review written while verified, then the Venue is delisted: list, own read, `PUT`, and `DELETE` must all hide it and leave the row intact. Pinned in Task 4 (`TestDelistedVenueKeepsReview`).
- A cursor copied from one Venue's list and replayed on another Venue must be `400 cursor: invalid`, not a silently wrong page. Pinned in Task 2 (`TestCursorRoundTrip`) and Task 3 (`TestListPagination`).
- Commentary follows the venue-description text rule: a null byte or non-whitespace control is `commentary: must be text`; trim then Unicode NFC; length is counted after that; whitespace-only, including `"\u00a0 \n"`, stores null. The same words in decomposed form are not an edit. Pinned in Task 2 (`TestParseDraft`) and Task 4 (`TestSaveNormalizedCommentaryNoop`).

---

## File map

| File | Responsibility |
| --- | --- |
| `infra/supabase/migrations/0009_reviews.sql` | `reviews` table, index, RLS, revokes |
| `backend/internal/database/pool.go` | `requiredMigrationVersion = "0009"` |
| `backend/internal/database/postgres_test.go` | readiness chain through `0009` |
| `backend/internal/database/access_test.go` | Data API roles denied on `reviews` |
| `backend/internal/database/review_test.go` | `reviews` constraint checks |
| `backend/internal/review/review.go` | types, errors, `ParseDraft`, cursor, canonical UUID |
| `backend/internal/review/postgres.go` | `Service`, `List`, `GetOwn`, `Save`, `Delete`, SQL |
| `backend/internal/review/*_test.go` | unit + integration tests |
| `backend/internal/venue/postgres.go` | aggregate subqueries in `venueSelect` |
| `backend/internal/venue/postgres_test.go` | aggregate tests |
| `backend/internal/api/review.go` | `Reviews` interface, handlers, JSON views, error mapping |
| `backend/internal/api/review_test.go` | HTTP tests with `fakeReviews` |
| `backend/internal/api/server.go` | `Options.Reviews`, four routes |
| `backend/cmd/api/main.go` | construct `review.New`, pass `Reviews` |
| `backend/openapi.yaml`, `backend/contract_test.go` | contract |
| `backend/AGENTS.md`, `docs/architecture/backend.md`, `DevSteps.md` | docs |

---

### Task 1: Migration `0009` and readiness

**Files:**
- Create: `infra/supabase/migrations/0009_reviews.sql`
- Create: `backend/internal/database/review_test.go`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go:124-129`
- Modify: `backend/internal/database/access_test.go:24-74`

**Interfaces:**
- Produces: table `reviews(id, venue_id, user_id, rating, commentary, created_at, updated_at)`, unique `reviews_venue_user_key (venue_id, user_id)`, index `reviews_venue_list_idx (venue_id, created_at desc, id desc)`. Later tasks query these names.

- [ ] **Step 1: Write the failing tests**

In `review_test.go`, `TestReviewConstraints` (same setup as `TestVenueSafetyConstraints`: one `auth.users` row, one venue). Using `vsCode(err)`:
- `rating` 0 and 6 → `"23514"`;
- `commentary = ''` → `"23514"`;
- `commentary = repeat('a', 2001)` → `"23514"`; `repeat('a', 2000)` succeeds;
- a second row for the same `(venue_id, user_id)` → `"23505"`;
- deleting the `auth.users` row leaves `select count(*) from reviews` = 0;
- deleting the venue row removes its reviews.

In `access_test.go`, append to `statements`:
```go
`select * from reviews`,
`insert into reviews (venue_id, user_id, rating, created_at, updated_at) values ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-8000-000000000001', 3, now(), now())`,
`update reviews set rating = 3 where false`,
`delete from reviews where false`,
```
Also assert `select relrowsecurity from pg_class where relname = 'reviews'` is `true`.

In `postgres_test.go`, the `0008` step now expects `ready == false, reason == "migrations_behind"`, then apply `0009_reviews.sql`, insert version `'0009'`, and expect `ready && reason == "ok"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/database/ -run 'TestReviewConstraints|TestDataAPIClosed|TestMigrationVersions' -v`
Expected: FAIL (`relation "reviews" does not exist`, and the readiness chain fails at `0008`).

- [ ] **Step 3: Write `0009_reviews.sql` and bump readiness**

Columns per the spec's Data section: `id uuid primary key default gen_random_uuid()`; `venue_id uuid not null references venues (id) on delete cascade`; `user_id uuid not null references auth.users (id) on delete cascade`; `rating integer not null` with check `between 1 and 5`; `commentary text` with check `commentary is null or char_length(commentary) between 1 and 2000`; `created_at`/`updated_at timestamptz not null` (no default, no trigger); `constraint reviews_venue_user_key unique (venue_id, user_id)`. Then `create index reviews_venue_list_idx on reviews (venue_id, created_at desc, id desc)`, `alter table reviews enable row level security`, `revoke all on table reviews from public, anon, authenticated, service_role`. No sequence revoke, no policy. Header comment one line, like `0008`.

`pool.go`: `const requiredMigrationVersion = "0009"`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/database/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0009_reviews.sql backend/internal/database
git commit -m "feat: add the reviews table and require migration 0009"
```

---

### Task 2: Review domain values, validation, and cursor

**Files:**
- Create: `backend/internal/review/review.go`
- Create: `backend/internal/review/review_test.go`

**Interfaces:**
- Produces (package `review`):
```go
type Author struct {
	Username    *string // nil when public identity is redacted
	DisplayName string
}
type Review struct { // public
	ID         string
	Rating     int
	Commentary *string
	Author     Author
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
type Own struct {
	ID          string
	UserID      string
	Rating      int
	Commentary  *string
	Username    string
	DisplayName string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
type Page struct {
	Reviews    []Review
	NextCursor *string
}
type ListQuery struct {
	VenueID string
	Limit   int
	Cursor  string
}
type SaveCommand struct {
	UserID     string
	VenueID    string
	Rating     json.RawMessage // nil when omitted
	Commentary json.RawMessage // nil when omitted
}
type SaveResult struct {
	Review  Own
	Created bool
}
type Draft struct {
	Rating     int
	Commentary *string
}
type ValidationError struct{ Fields map[string]string }
type RateLimitError struct{ RetryAfter int }
var ErrVenueNotFound   = errors.New("review: venue not found")
var ErrReviewNotFound  = errors.New("review: not found")
var ErrVenueNotVerified = errors.New("review: venue not verified")

func ParseDraft(rating, commentary json.RawMessage) (Draft, error)
func CanonicalID(id string) bool
func ValidateLimit(limit int) error
func encodeCursor(venueID string, createdAt time.Time, id string) string
func decodeCursor(cursor, venueID string) (time.Time, string, error)
```
`ValidationError.Error()` returns `"review: validation failed"`; `RateLimitError.Error()` returns `"review: rate limited"`.

- [ ] **Step 1: Write the failing tests**

`TestParseDraft`, table-driven, asserting either the `Draft` or exactly the `Fields` map:
- `rating` `1`, `5` → OK; `0`, `6`, `-1`, `4.0`, `4e0`, `"4"`, `true`, `null`, omitted (nil) → `{"rating": "must be between 1 and 5"}`;
- `commentary` omitted, `null`, `"   "`, `"\u00a0 \n"` → `Commentary == nil`;
- `"  loud  "` → `"loud"`;
- `"e\u0301"` → `"é"` (NFC); a null byte and a bell (`"\u0007"`) → `{"commentary": "must be text"}`; a line feed inside the text is kept;
- `strings.Repeat("é", 2000)` → OK; 2001 NFC runes → `{"commentary": "must be at most 2000 characters"}`; 2001 decomposed `e` + combining-acute pairs normalize under the cap and succeed;
- `5`, `{}`, `[]` as commentary → `{"commentary": "must be text"}`;
- bad rating and bad commentary together → both keys.

`TestCursorRoundTrip`:
- `decodeCursor(encodeCursor(v, t, id), v)` returns `t` (nanosecond equal, UTC) and `id`;
- the same cursor with another venue id → `*ValidationError` with `{"cursor": "invalid"}`;
- `"abc"`, base64 of `review-v2|…`, a non-canonical review id, a bad timestamp → same error.

`TestValidateLimit`: 0 and 101 → `{"limit": "must be from 1 to 100"}`; 1 and 100 → nil.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestParseDraft|TestCursorRoundTrip|TestValidateLimit' -v`
Expected: FAIL (package does not compile).

- [ ] **Step 3: Implement `review.go`**

- `ParseDraft`: rating is valid only when `bytes.TrimSpace(rating)` matches `^[1-5]$` (rejects fractions, exponents, strings, booleans, null in one rule). Commentary: nil or `null` → nil; otherwise `json.Unmarshal` into `string`, failure → `must be text`. On the raw string, a null byte or a non-whitespace control (`unicode.IsControl` and not `unicode.IsSpace`, the venue-description rule) → `must be text`. Then `norm.NFC.String(strings.TrimSpace(...))`; empty → nil; `utf8.RuneCountInString > 2000` → length error. `golang.org/x/text/unicode/norm` is already a dependency. Collect both fields before returning.
- `CanonicalID`: same regexp as `venue.CanonicalVenueID` (lowercase canonical UUID). Duplicated on purpose; review must not import venue.
- Cursor payload: `review-v1|{venueID}|{createdAt.UTC().Format(time.RFC3339Nano)}|{id}`, `base64.RawURLEncoding`. Decode rejects anything other than exactly four parts, discriminator `review-v1`, venue id equal to the argument, parseable time, canonical id.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review
git commit -m "feat: validate reviews and encode review cursors"
```

---

### Task 3: Public list and own Review reads

**Files:**
- Create: `backend/internal/review/postgres.go`
- Create: `backend/internal/review/postgres_test.go`

**Interfaces:**
- Consumes: Task 1 schema; Task 2 types.
- Produces:
```go
type Service struct { /* pool, profiles, writes, clock */ }
func New(pool *pgxpool.Pool, profiles *profile.Service, writes *ratelimit.UserLimiter, clock func() time.Time) *Service
func (s *Service) List(ctx context.Context, q ListQuery) (Page, error)
func (s *Service) GetOwn(ctx context.Context, userID, venueID string) (Own, error)
const listSQL = `…` // unexported; the index test explains it
```

- [ ] **Step 1: Write the failing tests**

Test helpers in `postgres_test.go` (package-local copies of the venue helpers; do not import venue test code): `testPool`, `insertUser(t, pool, id, status string, until *time.Time)` (same shape as venue's, username `"u"+id[len(id)-12:]`, display name `"Tester"`), `insertVenue(t, pool, id, status string)` (one Punk genre link unless status is `"bare"`, which inserts a verified venue with no genre), `insertReview(t, pool, venueID, userID string, rating int, commentary *string, created time.Time) string`.

`TestListVisibility`:
- verified venue with 2 reviews → 2 items, newest `created_at` first;
- pending venue with a seeded review row → `Page{Reviews: []Review{}, NextCursor: nil}`, no error;
- rejected, delisted, bare, unknown venue → `ErrVenueNotFound`;
- `"AAAA…"` uppercase id → `*ValidationError{"venue_id": "must be a canonical uuid"}`;
- `Limit: 0` → limit error.

`TestListPagination`: 3 reviews where two share `created_at`. `Limit: 2` returns the newest plus the higher id of the tie, `NextCursor != nil`; following the cursor returns the third with `NextCursor == nil`. The page-1 cursor used with another verified venue id → `{"cursor": "invalid"}`.

`TestListRedactsAuthor`: an active author → `Author{Username: &"u…", DisplayName: "Tester"}`; a `suspended` author and a `rename_required` author → `Author{Username: nil, DisplayName: "MetalHops User"}`. No `UserID` exists on `Review` (compile-time).

`TestGetOwn`:
- own review on verified venue → `Own` with `UserID`, stored `Username`, `DisplayName: "Tester"`, even when the caller is suspended;
- no review, pending venue (with a seeded row), delisted venue (with a seeded row), unknown venue, caller with no Profile → `ErrReviewNotFound`.

`TestReviewListIndex` (same pattern as `TestVenueReportIndexes`): seed 1,000 verified venues and 5,000 reviews across 500 users with unique `(venue, user)` pairs, `vacuum analyze reviews`, `explain listSQL` with `(venueID, nil, nil, 21)` → plan contains `reviews_venue_list_idx`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestList|TestGetOwn|TestReviewListIndex' -v`
Expected: FAIL (`New` undefined).

- [ ] **Step 3: Implement `New`, `List`, `GetOwn`**

- Venue visibility for both reads is the `GetVenue` predicate: `verification_status in ('pending','verified') and exists (select 1 from venue_genres g where g.venue_id = v.id)`. Read the status first in one query.
- `List`: validate id and limit, decode cursor (with path venue id) before any query, hidden → `ErrVenueNotFound`, pending → empty page, verified → `listSQL` with params `$1 venue_id, $2 cursor time (nullable), $3 cursor id (nullable), $4 limit+1`: `join profiles p on p.id = r.user_id`, `where r.venue_id = $1 and ($2::timestamptz is null or (r.created_at, r.id) < ($2, $3::uuid))`, `order by r.created_at desc, r.id desc`. Map author through `profile.PublicIdentity(status, username, displayName)`; empty username → nil. Return UTC timestamps. Fetch-one-extra for `NextCursor` (same as `ListVenueReports`).
- `GetOwn`: a non-canonical id returns the `venue_id` validation error, like every other route. Any other miss → `ErrReviewNotFound`. Only a `verified` public venue returns a row.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review
git commit -m "feat: list venue reviews and read the caller's review"
```

---

### Task 4: Save and delete the caller's Review

**Files:**
- Modify: `backend/internal/review/postgres.go`
- Create: `backend/internal/review/write_test.go`

**Interfaces:**
- Consumes: Task 3 `Service`; `profile.RequireContributor(ctx, tx, userID) error`; `(*profile.Service).NormalizeExpiry(ctx, userID) error`; `(*ratelimit.UserLimiter).Reserve(userID) (*ratelimit.Reservation, int, bool)`.
- Produces:
```go
func (s *Service) Save(ctx context.Context, cmd SaveCommand) (SaveResult, error)
func (s *Service) Delete(ctx context.Context, userID, venueID string) error
```

- [ ] **Step 1: Write the failing tests**

Harness: mutable clock (`now := t0; clock := func() time.Time { return now }`), `limiter := ratelimit.NewUserLimiter(clock)`, `New(pool, profile.New(pool, limiter, nil, clock), limiter, clock)`. Copy venue's `tokensLeft`. Use `t0 := time.Date(2020, 1, 15, 12, 0, 0, 0, time.UTC)`.

`TestSaveCreateReplaceNoop`:
Advance the clock by 1 ms before each later call, so refill stays below one token and `updated_at` movement is visible.
- first `Save` rating `4`, commentary `"  loud "` → `Created: true`, `Commentary == "loud"`, `CreatedAt == UpdatedAt == t0`, `tokensLeft == 59`;
- save rating `4`, commentary `"loud"` → `Created: false`, `UpdatedAt == t0`, `tokensLeft == 59`;
- save rating `2`, commentary omitted → `Created: false`, `Commentary == nil`, `UpdatedAt == t0+2ms`, `CreatedAt == t0`, `tokensLeft == 58`;
- `select count(*) from reviews` == 1.

`TestSaveNormalizedCommentaryNoop`: save commentary `"e\u0301"` → stored `"é"`, one token spent; save `"é"` after the clock advances → `Created: false`, `UpdatedAt` unchanged, token count unchanged.

`TestSaveValidation`: invalid rating → `*ValidationError`, no row, no token. Also a null byte in commentary → `*ValidationError`, no row, no token.

`TestSaveOutcomes`:
- pending venue → `ErrVenueNotVerified`, no row, no token;
- rejected, delisted, bare, unknown → `ErrVenueNotFound`;
- caller with no Profile → `profile.ErrProfileRequired`; `rename_required` → `profile.ErrRenameRequired`; suspended with future `until` → `*profile.SuspendedError`;
- the Venue's Submitter saves a Review → `Created: true`. The test inserts `submitted_by` and does not assert on it;
- suspended caller on a delisted venue → `ErrVenueNotFound` (Venue before Profile).

`TestSaveAfterExpiredSuspension`: caller `suspended` with `until` = `t0 - 1h` → `Save` succeeds, `Created: true`.

`TestSaveRateLimited`: drain 60 tokens with `limiter.Reserve`, `Save` → `*RateLimitError` with `RetryAfter >= 1`, no row.

`TestConcurrentFirstSave`: two goroutines `Save` the same user and venue (ratings 3 and 5) behind a `sync.WaitGroup` start gate → both `err == nil`, exactly one row, and exactly one result has `Created: true`.

`TestDelete`: delete existing → nil, row gone, one token spent; second delete → `ErrReviewNotFound`, no token; pending venue → `ErrVenueNotVerified`; unknown venue → `ErrVenueNotFound`.

`TestDelistedVenueKeepsReview`: save on verified venue, then `update venues set verification_status = 'delisted', delisting_reason = 'duplicate'`; `List` → `ErrVenueNotFound`; `GetOwn` → `ErrReviewNotFound`; `Save` and `Delete` → `ErrVenueNotFound`; row still present with its rating. Reopen (`verified`, delisting fields null) → `GetOwn` returns it again.

`TestUserDeletionRemovesReview`: `delete from auth.users` → `select count(*) from reviews` == 0.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestSave|TestConcurrentFirstSave|TestDelete|TestDelistedVenueKeepsReview|TestUserDeletionRemovesReview' -v`
Expected: FAIL (`Save` undefined).

- [ ] **Step 3: Implement `Save` and `Delete`**

Order for both (spec's Transaction section):
1. Validate venue id (`venue_id` field error); `Save` also runs `ParseDraft` here.
2. `s.profiles.NormalizeExpiry(txCtx, userID)` before `Begin`.
3. In the transaction, read the venue status `for share of v` with the public predicate. Hidden → `ErrVenueNotFound`; `pending` → `ErrVenueNotVerified`.
4. `profile.RequireContributor(txCtx, tx, userID)`.
5. `select … from reviews where venue_id = $1 and user_id = $2` (no extra lock; the profile `for update` serializes this User).
6. `Save` no-op when rating and normalized commentary equal the stored row: commit, return the stored `Own`, no reservation. `Delete` with no row → `ErrReviewNotFound`.
7. `Reserve`; refusal → `&RateLimitError{RetryAfter: retryAfter}`.
8. Insert (`created_at = updated_at = s.clock()`), `update … set rating, commentary, updated_at = s.clock()`, or `delete`. Insert and update use `returning` plus a join to `profiles` for the `Own` fields (or a follow-up read in the same tx).
9. Commit, then `reservation.Commit()`; any earlier return cancels via the same deferred pattern as `venue.ReportVenue`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review
git commit -m "feat: save and delete the caller's review"
```

---

### Task 5: Rating aggregates in the venue catalogue

**Files:**
- Modify: `backend/internal/venue/postgres.go:232-241` (`venueSelect`)
- Modify: `backend/internal/venue/postgres_test.go`

**Interfaces:**
- Consumes: Task 1 `reviews` table. No Go import of `internal/review`.
- Produces: `venue.Venue.AverageRating` / `ReviewCount` filled for verified venues on `ListVenues` and `GetVenue`.

- [ ] **Step 1: Write the failing test**

`TestRatingAggregates` (use `insertVenue`; insert users and reviews with plain SQL):
- verified venue, no reviews → `AverageRating == nil`, `ReviewCount == 0`;
- add ratings 1, 1, 1, 2 from four users → list and detail both `*AverageRating == 1.3`, `ReviewCount == 4`, `CheckInCount == 0`;
- pending venue with one seeded review row → `nil`, `0`;
- delete all four reviews → back to `nil`, `0`;
- re-add one review of 5, delist the venue → absent from `ListVenues`, `GetVenue` → `ErrNotFound`; reopen → `*AverageRating == 5`, `ReviewCount == 1`.

The existing `TestCatalogueMembershipAndAggregates` must still pass unchanged.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue/ -run 'TestRatingAggregates|TestCatalogueMembershipAndAggregates' -v`
Expected: FAIL (`AverageRating` nil for the four-review venue).

- [ ] **Step 3: Replace the literal columns in `venueSelect`**

Replace `null::float8, 0, 0,` with:
```sql
case when v.verification_status = 'verified' then
	(select round(avg(r.rating)::numeric, 1)::float8 from reviews r where r.venue_id = v.id)
end,
case when v.verification_status = 'verified' then
	(select count(*)::int from reviews r where r.venue_id = v.id)
else 0 end,
0,
```
Scan order is unchanged.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/venue/ -v`
Expected: PASS, including the existing index tests.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue
git commit -m "feat: show review aggregates in the venue catalogue"
```

---

### Task 6: HTTP routes and composition

**Files:**
- Create: `backend/internal/api/review.go`
- Create: `backend/internal/api/review_test.go`
- Modify: `backend/internal/api/server.go` (`Options`, routes)
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: Task 3/4 `review.Service` methods and types.
- Produces:
```go
type Reviews interface {
	List(context.Context, review.ListQuery) (review.Page, error)
	GetOwn(ctx context.Context, userID, venueID string) (review.Own, error)
	Save(context.Context, review.SaveCommand) (review.SaveResult, error)
	Delete(ctx context.Context, userID, venueID string) error
}
// Options gains: Reviews Reviews
```
JSON views:
```go
type reviewAuthorView struct {
	Username    *string `json:"username"`
	DisplayName string  `json:"display_name"`
}
type reviewView struct {
	ID         string           `json:"id"`
	Rating     int              `json:"rating"`
	Commentary *string          `json:"commentary"`
	Author     reviewAuthorView `json:"author"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}
type ownReviewView struct {
	ID         string           `json:"id"`
	UserID     string           `json:"user_id"`
	Rating     int              `json:"rating"`
	Commentary *string          `json:"commentary"`
	Author     reviewAuthorView `json:"author"` // Username always set
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
}
type reviewRequest struct {
	Rating     json.RawMessage `json:"rating"`
	Commentary json.RawMessage `json:"commentary"`
}
```
Timestamps `.UTC()` like `submissionJSON`.

- [ ] **Step 1: Write the failing tests**

`fakeReviews` records the last `ListQuery`, `SaveCommand`, user and venue ids, call count, and returns configured values/errors (same style as `fakeSubmissions`). Handler helper `reviewHandler(auth *fakeAuth, reviews *fakeReviews)`.

`TestReviewRoutes`:
- `GET /v1/venues/{id}/reviews` with no `Authorization` and with `Bearer t` → `200`; `ListQuery.Limit == 20`; `?limit=5&cursor=abc` passes through; `?sort=x`, `?limit=1&limit=2`, `?limit=x` → `400` without calling the fake; body has `"author":{"username":null,"display_name":"MetalHops User"}` for a redacted fake item and no `user_id`;
- `GET …/reviews/me` → `200`, body contains `"user_id"`;
- `PUT …/reviews/me` body `{"rating":4,"commentary":"loud"}` → fake `Created: true` gives `201`, `Created: false` gives `200`; `SaveCommand.Rating` equals raw `4`; `{"rating":4.0}` reaches the fake as raw `4.0` (the fake returns the field error and the response is `400` with `"rating":"must be between 1 and 5"`);
- `{"rating":4,"extra":1}`, `not json`, wrong content type, body over 64 KiB → existing `400`/`415`/`413` without calling the fake;
- `DELETE …/reviews/me` → `204`, empty body.

`TestReviewAuth`: the three `/me` routes → `401 unauthorized` with no token; operator caller → `403 forbidden` (use `assertForbidden`); fake never called.

`TestReviewErrors` (table, on `PUT`): `review.ErrVenueNotFound` → `404 venue_not_found` `Venue not found.`; `review.ErrReviewNotFound` → `404 review_not_found` `Review not found.`; `review.ErrVenueNotVerified` → `409 venue_not_verified` `Reviews open after this venue is verified.`; `profile.ErrProfileRequired` → `409`; `profile.ErrRenameRequired` → `403`; `&profile.SuspendedError{Until: &until}` → `403` with `"suspended_until":"2026-11-01T00:00:00Z"`; `&review.ValidationError{…}` → `400` with fields; `&review.RateLimitError{RetryAfter: 2}` → `429 rate_limited`, header `Retry-After: 2`; `errors.New("db down")` → `500 internal_error`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api/ -run 'TestReview' -v`
Expected: FAIL (`Reviews` undefined).

- [ ] **Step 3: Implement `review.go`, register routes, wire `main.go`**

- Public list: `rejectedQuery(r, map[string]bool{"limit": false, "cursor": false})`, then `parseVenueLimit`. It returns a `*venue.ValidationError`, which `writeReviewError` does not know, so write that error's `Fields` as `400 validation_error` directly in the handler. Then `reviews.List`; `WriteList`.
- `GET me`: `rejectedQuery(r, nil)`; `DELETE`: `w.WriteHeader(http.StatusNoContent)` like `deleteMe`.
- `writeReviewError`: `writeContributionError` first, then the table above; default logs `"review"` and writes `500`.
- `server.go`: `GET /v1/venues/{venueID}/reviews` unwrapped (public, like `getVenue`); `GET`, `PUT`, `DELETE /v1/venues/{venueID}/reviews/me` each through `requireApp`.
- `main.go`: `reviews := review.New(pool.Raw(), profiles, userLimiter, time.Now)`; `Reviews: reviews`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/api/ ./cmd/... -v` then `go build ./...`
Expected: PASS, build succeeds.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api backend/cmd/api/main.go
git commit -m "feat: route venue reviews"
```

---

### Task 7: Contract and docs

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md` (Capability modules section, after the Venue example near line 127)
- Modify: `DevSteps.md` (status line 6, sequence table, Session 11 section)

**Interfaces:**
- Consumes: everything above. Produces documentation only.

- [ ] **Step 1: Write the failing contract test**

Add to the `want` list in `TestOpenAPIDocument`: `"/v1/venues/{venueID}/reviews"`, `"/v1/venues/{venueID}/reviews/me"`, `"listVenueReviews"`, `"getMyReview"`, `"putMyReview"`, `"deleteMyReview"`, `"review_not_found"`, `"venue_not_verified"`, `"Reviews open after this venue is verified."`, `"must be between 1 and 5"`, `"Review:"`, `"OwnReview:"`, `"ReviewRequest:"`. Add `"getMyReview", "putMyReview", "deleteMyReview"` to the bearer-security loop and `"listVenueReviews"` to the must-not-be-secured loop. Assert the document does not contain `"commentary_locked"` or `"commentary_removed"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test . -run TestOpenAPIDocument -v`
Expected: FAIL (`missing "/v1/venues/{venueID}/reviews"`).

- [ ] **Step 3: Document**

- `openapi.yaml`: four operations with the IDs above; schemas `Review`, `OwnReview` (adds `user_id`, `author.username` non-null), `ReviewRequest` (`rating` integer 1–5 required, `commentary` string max 2000 nullable); `PUT` responses `200`, `201`, `400`, `401`, `403`, `404` (`venue_not_found`), `409` (`venue_not_verified`, `profile_required`), `429`; `GET me` `404 review_not_found`; `DELETE` `204`, `404` both codes, `409`; list `200`, `400`, `404`. No `Idempotency-Key` parameter. Add one verified-venue example with `average_rating: 4.3`, `review_count: 3`; leave the Venue schema types unchanged.
- `backend/AGENTS.md`: Layout gains `internal/review`: Review validation, public list, the caller's own Review, save and delete. App routes gain `GET`, `PUT`, `DELETE /v1/venues/{venueID}/reviews/me`; public routes gain `GET /v1/venues/{venueID}/reviews`; per-User bucket list gains "changed Review saves and Review deletes"; readiness text `0008` → `0009`.
- `docs/architecture/backend.md`: the one paragraph from the spec's "Docs that land with the code".
- `DevSteps.md`: status line → Sessions 1–11 done, Session 12 next, 12–14 not started; table row `| 11. Reviews and ratings | Done |` and `| 12–14 | Not started |`; Session 11 `**Status:** Done.` plus one paragraph: four routes, migration `0009`, readiness `0009`, aggregates computed in the catalogue query, pending Venues `409 venue_not_verified`, `commentary_removed` and the lock deferred to Session 12.

- [ ] **Step 4: Full verification**

Run: `gofmt -l .` → no output. `go vet ./...` → no output. `go test ./...` → PASS. `go build ./...` → success.
Then `make run` with a local `.env` and open `http://localhost:8080/docs`: the four review operations render.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md docs/architecture/backend.md DevSteps.md
git commit -m "docs: publish the review contract and close session 11"
```
