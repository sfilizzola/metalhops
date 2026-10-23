# Session 12: Review safety and moderation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Users can report public Review commentary, and operators can remove that commentary and impose a Commentary restriction or delete the Review, without retaining the removed text.

**Architecture:** Migration `0010` adds the commentary-removal marker, `review_reports`, `review_moderation_actions`, and `review_commentary_restrictions`. `internal/review` owns reports, the Commentary restriction, the audit, and the Session 11 write changes. `internal/api` gains a `ReviewModeration` interface (the PostgreSQL service plus an HTTP fake) and five routes. Venue delist runs one `update` on `review_reports` inside its existing transaction and does not import `internal/review`.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL/PostGIS via `testcontainers-go` (`internal/pgtest`), OpenAPI 3.1.

**Spec:** `docs/superpowers/specs/2026-10-21-session-12-review-safety-moderation-design.md`

## Global Constraints

- Run every command from `backend/`. Plain `go`, `gofmt`; no wrappers.
- Do not edit migrations `0001`–`0009`. Add no Go dependency. No new package. `cmd/api` stays composition only.
- No operator-provisioning route. Operators are the existing `platform_operators` row through `requireOperator`.
- `POST` report and moderation routes require `Idempotency-Key`. `PATCH` dismiss does not. A key on `PUT` or `DELETE /v1/venues/{venueID}/reviews/me` stays ignored.
- Operations: `review_report`, `review_moderation_action`. Keys follow the existing helper: missing key is `400 validation_error` with `idempotency_key: required`; a reused key with a different command is `409 idempotency_key_reused`. Only a stored `2xx` replays.
- Quota reuses `profile.CheckReportQuota` and `profile.RecordReportEvent`. Return `*profile.QuotaError` unwrapped.
- Every DB statement uses a 2-second `context.WithTimeout`.
- `internal/review` does not import `internal/venue`. `internal/venue` does not import `internal/review`.
- Out of scope: Hidden Commentary, the Session 14 moderator operation that lifts a Commentary restriction, report history, check-ins, the admin app. A restriction is indefinite until a Platform Moderator lifts it; Session 12 removes one only through User or Venue deletion cascades.
- Exact copy: `You cannot report your own review.`, `This review has no public commentary.`, `You already reported this review.`, `Commentary on this review is locked.`, `Report not found.`, `That report is already resolved.`, `The report limit has been reached.`, `Venue not found.`, `Review not found.`, `Reviews open after this venue is verified.`, `Create a profile first.`, `Rename this profile before continuing.`, `This profile is suspended.`
- Field copy: `reason: must be a known reason`, `note: required`, `note: must be 1 to 1000 characters`, `action: must be remove_commentary or delete_review`, `must be a canonical uuid` on `venue_id`, `review_id`, or `report_id`, `limit: must be from 1 to 100`, `cursor: invalid`.
- Error codes: `self_report` (400), `commentary_not_public` (400 while filing a report; 409 when a moderator tries to remove absent commentary), `review_already_reported` (409), `commentary_locked` (409), `review_report_not_found` (404), `report_already_resolved` (409), `quota_exceeded` (429), `rate_limited` (429), plus the existing venue and profile codes named above.
- A moderation write that appends an audit row is `201`. Replay returns that stored `201`. A `remove_commentary` no-op is `200` and is not stored.
- A moderation action's reason and note become the affected User's Restriction explanation. Reporter notes stay moderator-only.
- Integration tests skip locally without Docker through `pgtest`; CI treats them as blocking.
- Definition of done: `gofmt -l .` prints nothing; `go vet ./...` and `go test ./...` pass.

## Review Focus

- A suspended User reporting a hidden Venue gets `403 profile_suspended`, not `404`. Report filing checks the Profile before the Venue. Pinned in Task 4 (`TestReportProfileBeforeVenue`).
- Reusing the successful report key after the commentary is removed returns `400 commentary_not_public` and does not replay the `201`. Pinned in Task 4 (`TestReportReplayStopsWhenCommentaryGone`).
- Reusing the successful report key after the author replaces commentary replays the historical `201`; reporting the replacement requires a new key. Pinned in Task 4 (`TestReportReplayAfterAuthorReplacement`).
- Replaying `delete_review` after the row is gone returns the stored `201`. A different key returns `404 review_not_found`. Lookup runs before the row lock. Pinned in Task 5 (`TestDeleteReviewReplay`).
- A restricted User who sends the same non-null commentary again gets `409 commentary_locked` before the unchanged-save short-circuit. Pinned in Task 3 (`TestLockedSameCommentaryIsConflict`).
- An author commentary change updates only `status = 'pending'`. A report already `actioned` by delist stays `actioned`. Pinned in Task 3 (`TestAuthorChangeDoesNotReopenActioned`).

---

## File map

| File | Responsibility |
| --- | --- |
| `infra/supabase/migrations/0010_review_safety.sql` | column, three tables, triggers, RLS |
| `backend/internal/database/pool.go` | readiness `0010` |
| `backend/internal/database/postgres_test.go` | chain through `0010` |
| `backend/internal/database/access_test.go` | Data API denial |
| `backend/internal/database/review_safety_test.go` | checks, append-only, no commentary columns |
| `backend/internal/review/review.go` | errors, report and moderation types |
| `backend/internal/review/validation.go` | reason, note, and action normalization |
| `backend/internal/review/postgres.go` | `commentary_removed` and User-visible restriction explanation on reads; restriction enforcement and author-change handling inside `Save` |
| `backend/internal/review/report.go` | `ReportReview`, list, dismiss |
| `backend/internal/review/moderate.go` | `ModerateReview`, `GetModerationReview` |
| `backend/internal/review/report_test.go`, `moderate_test.go`, `index_test.go` | capability tests |
| `backend/internal/venue/moderate.go` | delist `update review_reports` |
| `backend/internal/venue/moderate_test.go` | delist side effect |
| `backend/internal/api/review.go` | `commentary_removed`, `commentary_locked` |
| `backend/internal/api/review_moderation.go` | `ReviewModeration`, five handlers |
| `backend/internal/api/review_moderation_test.go` | HTTP fake |
| `backend/internal/api/server.go`, `backend/cmd/api/main.go` | routes and composition |
| `backend/openapi.yaml`, `backend/contract_test.go` | contract |
| `backend/AGENTS.md`, `docs/architecture/backend.md`, `DevSteps.md` | docs |

---

### Task 1: Migration `0010` and readiness

**Files:**
- Create: `infra/supabase/migrations/0010_review_safety.sql`
- Create: `backend/internal/database/review_safety_test.go`
- Modify: `backend/internal/database/pool.go` (`requiredMigrationVersion`)
- Modify: `backend/internal/database/postgres_test.go` (readiness chain)
- Modify: `backend/internal/database/access_test.go` (`TestDataAPIClosed`)

**Interfaces:**
- Produces: `reviews.commentary_removed_at`; tables `review_reports`, `review_moderation_actions`, `review_commentary_restrictions`; indexes `review_reports_pending_pair_key`, `review_reports_pending_list_key`, `review_reports_pending_review_key`, `review_moderation_actions_review_key`, `review_commentary_restrictions_pair_key`. `review_reports` stores no duplicate `venue_id`; reads and Venue delist derive it through `reviews`. Later tasks use these names.

- [ ] **Step 1: Write the failing tests**

`review_safety_test.go` uses `pgtest.MigratedURL` and `vsCode`, same setup as `TestReviewConstraints` (one auth user, one venue, one review).

`TestReviewSafetyConstraints`:
- commentary non-null and `commentary_removed_at` set → `"23514"`; both null, and commentary null with the timestamp set, succeed.
- report reason `'nope'` → `"23514"`; `other` with null note → `"23514"`; `spam` with null note succeeds; note of 1001 characters → `"23514"`.
- a second pending report for the same reporter and review → `"23505"`; after the first is `dismissed` (with `resolved_at`), a new pending row succeeds.
- `author_resolved` with `resolved_at` and `resolved_by_user_id` succeeds; without `resolved_at` it fails with `"23514"`.
- pending row with `resolved_at` set → `"23514"`.
- `information_schema.columns` has zero rows whose `table_name` is `review_reports` or `review_moderation_actions` and whose `column_name` contains `commentary`.
- deleting the review leaves `select count(*) from review_reports` = 0 and leaves the `report_quota_events` row.
- deleting the reporter's auth user leaves that user's `report_quota_events` at 0.
- deleting the restricted user's auth user leaves `select count(*) from review_commentary_restrictions` = 0.
- inserting a restriction with an unknown `source_action_id` fails with `"23503"`; deleting the source Review leaves the audit, restriction, and source reference intact.

`TestReviewAuditAppendOnly`:
- insert with null `operator_user_id` → `"23514"`.
- `update ... set reason = 'spam'` → `"55000"`.
- `update ... set operator_user_id = null` succeeds.
- `delete from review_moderation_actions` → `"55000"`.
- `delete from reviews` leaves the audit row, and its `review_id` is unchanged.

In `postgres_test.go`, after the `0009` block, `Check` is `migrations_behind`. Applying `0010_review_safety.sql` and inserting version `0010` makes `Check` ready with reason `ok`.

In `access_test.go`, add select/insert/update/delete for `review_reports`, `review_moderation_actions`, and `review_commentary_restrictions` to the statement list, and assert `relrowsecurity` on all three.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/database/ -count=1 -run 'TestReviewSafety|TestReviewAudit|TestMigrations|TestDataAPIClosed'`
Expected: FAIL. The migration file does not exist, or `0010` is not required.

- [ ] **Step 3: Write `0010_review_safety.sql` and set readiness to `0010`**

Follow the spec's Data section. Copy the Venue append-only and actor-required triggers from `infra/supabase/migrations/0008_venue_safety.sql`. Name them `reject_review_moderation_action_mutation` and `reject_review_moderation_action_without_actor`. The update allow-list permits only nulling `reviewer_user_id` or `operator_user_id`. Insert rejects a null `operator_user_id`. Revoke execute on both functions from `public`, `anon`, `authenticated`, and `service_role`.

`review_moderation_actions.review_id` has no foreign key. `venue_id` references `venues (id)` with no `ON DELETE` clause. `review_reports.review_id` and the restriction's User and Venue foreign keys cascade. `review_commentary_restrictions.source_action_id` references the append-only audit table with no `ON DELETE` clause. Report and audit `created_at` default `now()`.

`requiredMigrationVersion` becomes `"0010"`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/database/ -count=1 -run 'TestReviewSafety|TestReviewAudit|TestMigrations|TestDataAPIClosed'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0010_review_safety.sql backend/internal/database
git commit -m "feat: add review safety tables and require migration 0010"
```

---

### Task 2: Report and action validation

**Files:**
- Create: `backend/internal/review/validation.go`
- Create: `backend/internal/review/validation_test.go`
- Modify: `backend/internal/review/review.go`

**Interfaces:**
- Consumes: `profile.NormalizeNote`.
- Produces:

```go
var (
    ErrSelfReport          = errors.New("review: self report")
    ErrCommentaryNotPublic = errors.New("review: commentary not public")
    ErrReportPending       = errors.New("review: report pending")
    ErrCommentaryLocked    = errors.New("review: commentary locked")
    ErrReportNotFound      = errors.New("review: report not found")
    ErrReportResolved      = errors.New("review: report resolved")
)

func NormalizeReport(reason string, note *string) (string, *string, error)
func NormalizeAction(action, reason string, note *string) (string, string, *string, error)
```

`NormalizeReport` returns `*ValidationError`. Reasons: `spam`, `harassment`, `hate_or_discrimination`, `personal_information`, `illegal_content`, `off_topic`, `other`. `NormalizeAction` uses those reasons and actions `remove_commentary`, `delete_review`. A `*profile.ValidationError` from `NormalizeNote` becomes `*review.ValidationError` with the same `Fields`.

- [ ] **Step 1: Write the failing test**

`TestNormalizeReport` in `validation_test.go`:
- `spam` and a nil note returns `("spam", nil, nil)`.
- `other` and nil note → field `note` = `required`.
- `nope` → field `reason` = `must be a known reason`.
- note `"  kept  "` returns `"kept"`.
- note `""` → field `note` = `must be 1 to 1000 characters`.

`TestNormalizeAction`:
- action `nope` → field `action` = `must be remove_commentary or delete_review`.
- `remove_commentary` + `spam` returns those strings and a nil note.
- `delete_review` + `other` and nil note → `note` = `required`.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/review/ -count=1 -run 'TestNormalize'`
Expected: FAIL with `NormalizeReport` undefined.

- [ ] **Step 3: Implement the two functions and the error variables**

Map note failures the way `internal/venue/moderation.go` `normalizeNote` maps `profile.NormalizeNote`.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/review/ -count=1 -run 'TestNormalize'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/validation.go backend/internal/review/validation_test.go backend/internal/review/review.go
git commit -m "feat: validate review report reasons and moderation actions"
```

---

### Task 3: Commentary flag, restriction, and author report resolution

**Files:**
- Modify: `backend/internal/review/review.go` (`Review`, `Own`)
- Modify: `backend/internal/review/postgres.go` (`Save`, list and own selects)
- Modify: `backend/internal/review/postgres_test.go`
- Modify: `backend/internal/review/write_test.go`
- Modify: `backend/internal/venue/postgres_test.go` (`TestRatingAggregates`)

**Interfaces:**
- Consumes: `ErrCommentaryLocked` from Task 2. Tables from Task 1.
- Produces: `Review.CommentaryRemoved bool`, `Own.CommentaryRemoved bool`, and `Own.CommentaryRestriction *CommentaryRestriction`, where `CommentaryRestriction` contains `Reason string`, `Note *string`, and `CreatedAt time.Time`. `CommentaryRestrictedError` carries that same value and satisfies `errors.Is(err, ErrCommentaryLocked)`. `Save` returns it before the unchanged-save short-circuit when normalized commentary is non-null and `review_commentary_restrictions` has `(user_id, venue_id)`. A save that changes stored non-null commentary, whether replacing or clearing it, sets that Review's pending reports to `author_resolved` with `resolved_by_user_id` = the author and `resolved_at` = the service clock, predicate `status = 'pending'`. It does not insert a restriction and does not set `commentary_removed_at`. `Delete` is unchanged; the foreign key cascades reports.

- [ ] **Step 1: Write the failing tests**

`TestCommentaryRemovedFlag` in `postgres_test.go`: a review with `commentary_removed_at` set and commentary null lists `CommentaryRemoved == true` and `Commentary == nil`. A rating-only review without the timestamp lists `CommentaryRemoved == false`. `GetOwn` matches. Public list items have no restriction field. `GetOwn` on a restricted pair returns the source action's reason and note plus the restriction's `created_at`; an unrestricted pair returns nil.

`TestLockedSameCommentaryIsConflict` in `write_test.go`: seed a review with commentary `"loud"` and a restriction row. `Save` of rating `4` and `"loud"` returns a `*CommentaryRestrictedError` that matches `ErrCommentaryLocked` and carries the source reason, note, and restriction start. The row's commentary is still `"loud"`. Tokens stay 60.

`TestLockedClearAndRecreate` : `Save` with commentary null changes the rating, returns nil error, leaves `commentary_removed_at` null, and does not delete the restriction. `Delete` then `Save` of rating `2` and commentary null succeeds with `CommentaryRemoved == false`. `Save` of commentary `"again"` returns `ErrCommentaryLocked`.

`TestAuthorChangeResolvesReports`: commentary is `"old"` and one report is `pending`. `Save` with commentary null sets that report `author_resolved`, `resolved_by_user_id` = the author, and inserts no restriction. Restore commentary to `"old"`, insert another `pending` report, then `Save` commentary `"new"`. That report also becomes `author_resolved`; the replacement can receive a new pending report.

`TestAuthorChangeDoesNotReopenActioned`: one `actioned` report (with `resolved_at`) and one `pending` report. Changing commentary marks only the pending row `author_resolved`.

`TestRatingAggregates`: after the four-review assertion, `update reviews set commentary = null, commentary_removed_at = now()`. `assertAggregate` still wants `1.3` and count `4`.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/review/ ./internal/venue/ -count=1 -run 'TestCommentaryRemovedFlag|TestLocked|TestAuthorChange|TestRatingAggregates'`
Expected: FAIL. `CommentaryRemoved` is missing, or the restriction is not enforced.

- [ ] **Step 3: Implement the flag, the restriction check, and the resolution update**

Add `commentary_removed_at is not null` to the list and own selects. The own select left-joins the restriction and its source action for the User-visible explanation. In `Save`, after `RequireContributor` and the current-row read, query that explanation when `draft.Commentary != nil`. On a hit, return `*CommentaryRestrictedError` with no token. On any update that changes stored non-null commentary, run the pending-only `author_resolved` update in that same transaction before commit. Do not add a commentary filter to the venue aggregate subquery.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/review/ ./internal/venue/ -count=1 -run 'TestCommentaryRemovedFlag|TestLocked|TestAuthorChange|TestRatingAggregates|TestSave|TestList|TestDelete'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review backend/internal/venue/postgres_test.go
git commit -m "feat: restrict review commentary and resolve author-changed reports"
```

---

### Task 4: File a Review Report

**Files:**
- Create: `backend/internal/review/report.go`
- Create: `backend/internal/review/report_test.go`
- Modify: `backend/internal/review/review.go` (command and result types)

**Interfaces:**
- Consumes: `NormalizeReport`, `ErrSelfReport`, `ErrCommentaryNotPublic`, `ErrReportPending`, `profile.CheckReportQuota`, `profile.RecordReportEvent`, `idempotency.Execute`.
- Produces:

```go
type ReportCommand struct {
    ReporterID string
    VenueID    string
    ReviewID   string
    Reason     string
    Note       *string
    Key        string
}

type Report struct {
    ID        string
    ReviewID  string
    VenueID   string
    Reason    string
    Note      *string
    Status    string
    CreatedAt time.Time
}

func (s *Service) ReportReview(ctx context.Context, cmd ReportCommand) (Report, error)
```

Idempotency command JSON is `review_id`, `venue_id`, `reason`, `note`. Operation `review_report`. Check order is the spec's filing list: contributor, venue `for share`, review on that venue, self, null commentary, then inside `Execute` the pending pair, the quota, the write token, the insert, and `RecordReportEvent`. Steps before `Execute` are not replayed.

- [ ] **Step 1: Write the failing tests**

`TestReportCheckOrder` in `report_test.go`, using `newWriteHarness` and `insertReview`:
- venue id `abc` → `*ValidationError` field `venue_id` = `must be a canonical uuid`.
- review id `abc` on a canonical venue → field `review_id` = `must be a canonical uuid`.
- hidden venue → `ErrVenueNotFound`.
- pending public venue → `ErrVenueNotVerified`.
- verified venue, unknown review id → `ErrReviewNotFound`.
- own review, including rating-only → `ErrSelfReport`.
- another user's rating-only review → `ErrCommentaryNotPublic`.
- commentary present → `Report` with `Status == "pending"` and one `report_quota_events` row. The review commentary is unchanged. Tokens for the reporter are 59.
- same pair, new key → `ErrReportPending`, still one quota row, tokens 59.
- twenty existing quota events, new pair → `*profile.QuotaError`, zero new report rows.

`TestReportProfileBeforeVenue`: caller `suspended`, venue delisted → `errors.As` into `*profile.SuspendedError`, not `ErrVenueNotFound`.

`TestReportReplayStopsWhenCommentaryGone`: first call `201` path returns a report. Set commentary null. The same key returns `ErrCommentaryNotPublic`. Quota row count stays 1.

`TestReportReplayWhilePublic`: the same key and command returns the same `Report.ID` and does not insert a second quota row.

`TestReportReplayAfterAuthorReplacement`: file a report, replace the
commentary through `Save`, then reuse the original report key. It returns the
same stored `Report.ID` and historical `pending` response without another
quota event or token. A new key files a new pending report against the
replacement.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestReport'`
Expected: FAIL with `ReportReview` undefined.

- [ ] **Step 3: Implement `ReportReview`**

Follow `internal/venue/report.go` `ReportVenue` for the transaction shape, including the canonical UUID checks before the transaction. The venue predicate is `verification_status = 'verified'` plus a genre link. A pending public venue (the Session 11 writable predicate, status `pending`) returns `ErrVenueNotVerified`. Self-check uses the review's `user_id`. Null `commentary` returns `ErrCommentaryNotPublic`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestReport'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/report.go backend/internal/review/report_test.go backend/internal/review/review.go
git commit -m "feat: accept a review report against public commentary"
```

---

### Task 5: Operator remove and delete

**Files:**
- Create: `backend/internal/review/moderate.go`
- Create: `backend/internal/review/moderate_test.go`
- Modify: `backend/internal/review/review.go`

**Interfaces:**
- Consumes: `NormalizeAction`, `idempotency.Execute`.
- Produces:

```go
type ModerationCommand struct {
    ActorID  string
    ReviewID string
    Action   string
    Reason   string
    Note     *string
    Key      string
}

type OperatorReview struct {
    ID                   string
    VenueID              string
    UserID               string
    Rating               int
    Commentary           *string
    CommentaryRemoved    bool
    CommentaryRemovedAt  *time.Time
    Author               *profile.Profile
    CreatedAt            time.Time
    UpdatedAt            time.Time
}

type ModerationResult struct {
    Created  bool
    ActionID *string
    ReviewID string
    Review   *OperatorReview // nil after delete_review
}

func (s *Service) ModerateReview(ctx context.Context, cmd ModerationCommand) (ModerationResult, error)
```

Idempotency JSON is `review_id`, `action`, `reason`, `note`. Operation `review_moderation_action`. Stored status `201`. The body has no commentary field: `action_id`, `review_id`, and for `remove_commentary` the operator review fields except the commentary string is allowed on the live `Review` result but the stored idempotency JSON must not contain a `commentary` key. Lookup runs before `select ... for update`. A no-op (`commentary` null and a restriction exists) returns `Created: false`, `ActionID: nil`, and is not passed to `Execute`.

- [ ] **Step 1: Write the failing tests**

`TestRemoveCommentary`: commentary `"loud"`, one pending report. Result `Created == true`, commentary null, `CommentaryRemoved == true`, rating unchanged, `created_at` unchanged, `updated_at` equal to the harness clock, restriction row present, report `actioned` with `resolved_by_user_id` = the operator. The audit `action` is `commentary_removed` and `reviewer_user_id` is the author; its id is the restriction's `source_action_id`. The idempotency `response_body` does not contain `commentary`. Review id `abc` → `*ValidationError` field `review_id` = `must be a canonical uuid`.

`TestRemoveCommentaryNoop`: call it again. `Created == false`, `ActionID == nil`, audit count stays 1, tokens unchanged.

`TestRejectRatingOnlyRemoval`: commentary null, no restriction.
`remove_commentary` returns `ErrCommentaryNotPublic`; no restriction, audit
row, report update, or write token is created.

`TestDeleteReviewReplay`: `delete_review` returns `Created == true`, `Review == nil`, audit `review_deleted`, reports count 0, quota events still 1, restriction exists and points to that audit. If the restriction already existed, its `created_at` and `source_action_id` are unchanged. The same key returns `Created == true` and the same `ActionID`. A new key returns `ErrReviewNotFound`.

`TestModerateOnDelistedVenue`: the review's venue is `delisted`. `remove_commentary` still returns `Created == true`.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestRemove|TestRejectRatingOnlyRemoval|TestDeleteReview|TestModerateOn'`
Expected: FAIL with `ModerateReview` undefined.

- [ ] **Step 3: Implement `ModerateReview`**

`remove_commentary` with text sets commentary null, sets `commentary_removed_at` and `updated_at`, inserts the audit, inserts the restriction with that action id and `on conflict do nothing`, and actions pending reports. With no commentary and no existing restriction, it returns `ErrCommentaryNotPublic` before reserving a write token. `delete_review` inserts the audit, inserts the restriction with that action id and `on conflict do nothing`, then deletes the review. Read the author id before the delete. Repeated actions preserve the restriction's original `created_at` and `source_action_id`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestRemove|TestRejectRatingOnlyRemoval|TestDeleteReview|TestModerateOn'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/moderate.go backend/internal/review/moderate_test.go backend/internal/review/review.go
git commit -m "feat: remove review commentary or delete the review"
```

---

### Task 6: Operator list, detail, and dismiss

**Files:**
- Modify: `backend/internal/review/report.go`
- Modify: `backend/internal/review/moderate.go`
- Modify: `backend/internal/review/report_test.go`
- Modify: `backend/internal/review/moderate_test.go`
- Create: `backend/internal/review/index_test.go`

**Interfaces:**
- Produces:

```go
type ReportItem struct {
    ID                 string
    Reason             string
    Note               *string
    Status             string
    ReviewID           string
    VenueID            string
    Rating             int
    Commentary         *string
    CommentaryRemoved  bool
    CreatedAt          time.Time
    Reporter           *profile.Profile
    Author             *profile.Profile
}

type ReportPage struct {
    Items      []ReportItem
    NextCursor *string
}

type DetailReport struct {
    ID        string
    Reason    string
    Note      *string
    CreatedAt time.Time
    Reporter  *profile.Profile
}

type AuditEntry struct {
    ID             string
    Action         string
    Reason         string
    Note           *string
    ReviewID       string
    ReviewerUserID *string
    VenueID        string
    OperatorUserID *string
    CreatedAt      time.Time
}

type OperatorCommentaryRestriction struct {
    CreatedAt time.Time
    Source    AuditEntry
}

type ModerationDetail struct {
    Review      OperatorReview
    Reports     []DetailReport
    History     []AuditEntry
    Restriction *OperatorCommentaryRestriction
}

func (s *Service) ListReviewReports(ctx context.Context, limit int, cursor string) (ReportPage, error)
func (s *Service) GetModerationReview(ctx context.Context, reviewID string) (ModerationDetail, error)
func (s *Service) DismissReviewReport(ctx context.Context, actorID, reportID string) (ReportItem, error)
```

Cursor payload is `review-report-v1|{created_at RFC3339Nano}|{reportID}`, `base64.RawURLEncoding`. List order is `created_at`, `id` ascending, pending only, `limit+1`. Detail history is the newest 20, `created_at desc, id desc`. Copy the profile column list from `venue.profileColumns` into this package; do not import `venue`. A missing profile scans as nil.

- [ ] **Step 1: Write the failing tests**

`TestListReviewReports`: three pending reports and one dismissed. The author's profile status is `suspended` and the item's `Author.Username` is the stored username. Page of limit 2 returns the two oldest and a cursor. The next page returns the third. A cursor with the wrong discriminator is `*ValidationError` field `cursor` = `invalid`. An `actioned` report is absent.

`TestGetModerationReview`: a delisted venue's review is returned with its pending report and one audit row. Resolved reports are absent. History is capped at 20. An unrestricted pair returns `Restriction == nil`. For a later Review whose restriction source belongs to a deleted earlier Review, `Restriction` contains the original `created_at` and source audit row even though that source is outside the later Review's `History`. Unknown id is `ErrReviewNotFound`.

`TestDismissReviewReport`: pending becomes `dismissed` and spends a token. A second dismiss returns the same id, `Status == "dismissed"`, and does not spend a token. An `actioned` or `author_resolved` report is `ErrReportResolved`. Unknown id is `ErrReportNotFound`. Report id `abc` → field `report_id` = `must be a canonical uuid`. The review commentary is unchanged.

`TestReviewReportListPlan` in `index_test.go`: seed enough pending rows that `explain` of the list query uses `review_reports_pending_list_key`. Follow `venue/index_test.go`.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestListReviewReports|TestGetModerationReview|TestDismissReviewReport|TestReviewReportListPlan'`
Expected: FAIL. The methods are undefined.

- [ ] **Step 3: Implement list, detail, and dismiss**

Dismiss matches `DismissVenueReport`: lock the report, return the stored row when already `dismissed`, and return `ErrReportResolved` when `actioned` or `author_resolved`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/review/ -count=1 -run 'TestListReviewReports|TestGetModerationReview|TestDismissReviewReport|TestReviewReportListPlan'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review
git commit -m "feat: list and dismiss review reports"
```

---

### Task 7: Delist actions pending Review Reports

**Files:**
- Modify: `backend/internal/venue/moderate.go`
- Modify: `backend/internal/venue/moderate_test.go`

**Interfaces:**
- Consumes: table `review_reports` from Task 1. No import of `internal/review`.
- Produces: `resolveReviewReports(ctx, tx, venueID, actorID string, now time.Time) error`, called from the existing moderation callback only when `cmd.Action == "delist"`, next to `resolveReports`.

```sql
update review_reports
set status = 'actioned', resolved_at = $3, resolved_by_user_id = $2::uuid
from reviews
where review_reports.review_id = reviews.id
  and reviews.venue_id = $1::uuid
  and review_reports.status = 'pending'
```

- [ ] **Step 1: Write the failing test**

`TestDelistActionsReviewReports` in `moderate_test.go`:
- a pending `review_reports` row on a verified venue. `delist` sets it `actioned` and `resolved_by_user_id` to the operator. The review commentary is unchanged. `commentary_removed_at` is null. No `review_moderation_actions` row. No restriction row.
- a second `delist` with the same reason and note (identical re-delist) leaves a newly inserted pending report pending. Insert that report after the first delist by writing SQL, then call the identical action.
- `reopen` leaves an `actioned` report `actioned`.
- `verify` on a different pending venue, and `reject` on another, leave a pending review report pending.
- a feedback-only `delist` (different note, `other` plus a note), after inserting a fresh pending review report, actions that report.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/venue/ -count=1 -run TestDelistActionsReviewReports`
Expected: FAIL. The report stays `pending`.

- [ ] **Step 3: Call `resolveReviewReports` from the delist write**

Add the statement in `moderate.go`. Do not call it from `CorrectVenue` or from `verify`, `reject`, or `reopen`.

- [ ] **Step 4: Run the test**

Run: `go test ./internal/venue/ -count=1 -run 'TestDelistActionsReviewReports|TestModerate'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/venue/moderate.go backend/internal/venue/moderate_test.go
git commit -m "feat: action review reports when a venue is delisted"
```

---

### Task 8: HTTP routes

**Files:**
- Modify: `backend/internal/api/review.go` (`reviewView`, `ownReviewView`, `writeReviewError`)
- Create: `backend/internal/api/review_moderation.go`
- Create: `backend/internal/api/review_moderation_test.go`
- Modify: `backend/internal/api/review_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: the `*review.Service` methods from Tasks 3–6.
- Produces:

```go
type ReviewModeration interface {
    ReportReview(context.Context, review.ReportCommand) (review.Report, error)
    ListReviewReports(context.Context, int, string) (review.ReportPage, error)
    GetModerationReview(context.Context, string) (review.ModerationDetail, error)
    ModerateReview(context.Context, review.ModerationCommand) (review.ModerationResult, error)
    DismissReviewReport(context.Context, string, string) (review.ReportItem, error)
}
```

`Options.ReviewModeration`. `*review.Service` is the production adapter. `main.go` passes the existing `reviews` value. Routes:

- `POST /v1/venues/{venueID}/reviews/{reviewID}/reports` on `requireApp`
- `GET /v1/moderation/review-reports` on `requireOperator`
- `GET /v1/moderation/reviews/{reviewID}` on `requireOperator`
- `POST /v1/moderation/reviews/{reviewID}/actions` on `requireOperator`
- `PATCH /v1/moderation/review-reports/{reportID}` on `requireOperator`

`reviewView` and `ownReviewView` gain `commentary_removed`; only `ownReviewView` gains nullable `commentary_restriction` with `reason`, `note`, and `created_at`. `writeReviewError` maps `*review.CommentaryRestrictedError` (and its `ErrCommentaryLocked` classification) to `409 commentary_locked` / `Commentary on this review is locked.`, with the same `commentary_restriction` object in error details. Reporter notes and operator identity are absent. Report and moderation errors map to the Global Constraints codes. `*profile.QuotaError` maps like `writeVenueModerationError`. `ModerateReview` with `Created == true` writes `201`; otherwise `200`. The delete body is `action_id`, `review_id`, `review: null`. The remove body includes `review`. The idempotency record is the service's concern; the HTTP JSON for a live remove may include `commentary` on the operator review, and the service test in Task 5 already forbids that key in the stored body.

- [ ] **Step 1: Write the failing tests**

`TestReviewRoutes` gains a `PUT` case: fake `Save` returns `*CommentaryRestrictedError` → `409`; the body contains `commentary_locked`, `Commentary on this review is locked.`, and the source reason, note, and restriction start under `commentary_restriction`, with no operator or reporter identity. A listed review and the own-review response with `CommentaryRemoved: true` encode `"commentary_removed":true`; only the own response encodes `commentary_restriction`.

`TestReviewModerationRoutes` mirrors `venue_moderation_test.go`:
- no token on each new route → `401`.
- app user on a privileged route → `403 forbidden`.
- operator on the report `POST` → `403 forbidden`.
- table: report filing maps `ErrSelfReport` → `400 self_report`, `ErrCommentaryNotPublic` → `400 commentary_not_public`, and `ErrReportPending` → `409 review_already_reported`; moderation actions map `ErrCommentaryNotPublic` → `409 commentary_not_public`; dismissal maps `ErrReportNotFound` → `404 review_report_not_found` and `ErrReportResolved` → `409 report_already_resolved`; `*profile.QuotaError{RetryAfter: 30}` → `429 quota_exceeded` and `Retry-After: 30`.
- missing `Idempotency-Key` on the two `POST`s → `400` field `idempotency_key` = `required`. `PATCH` without a key still calls dismiss.
- `ModerateReview` `Created: true` → `201`. `Created: false` → `200` and `"action_id":null`. `Review: nil` encodes `"review":null`.
- `GET /v1/moderation/review-reports?limit=0` → `400` field `limit` = `must be from 1 to 100`. `?extra=1` → `400`. The handler uses the existing limit parser and `rejectedQuery`.
- `*review.RateLimitError{RetryAfter: 4}` on report `POST` → `429 rate_limited` and `Retry-After: 4`.

- [ ] **Step 2: Run the tests**

Run: `go test ./internal/api/ -count=1 -run 'TestReviewRoutes|TestReviewModerationRoutes'`
Expected: FAIL. The route or the error code is missing.

- [ ] **Step 3: Add the handlers, the interface, and the routes**

Decode report and action bodies with `optionalString`, matching `venueReportRequest` and `venueActionRequest`. Unknown fields stay rejected by `DecodeJSON`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/api/ -count=1 -run 'TestReview'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api/review.go backend/internal/api/review_moderation.go backend/internal/api/review_moderation_test.go backend/internal/api/review_test.go backend/internal/api/server.go backend/cmd/api/main.go
git commit -m "feat: route review reports and review moderation"
```

---

### Task 9: Contract and session docs

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the five routes and the error codes from Task 8. No new Go API.

- [ ] **Step 1: Write the failing contract assertions**

In `contract_test.go`, delete the loop that fails when the document contains `commentary_locked` or `commentary_removed`. Add to the required substrings:

- `/v1/venues/{venueID}/reviews/{reviewID}/reports`
- `/v1/moderation/review-reports`
- `/v1/moderation/reviews/{reviewID}`
- `/v1/moderation/reviews/{reviewID}/actions`
- `/v1/moderation/review-reports/{reportID}`
- `review_already_reported`, `commentary_not_public`, `commentary_locked`, `review_report_not_found`
- `commentary_removed` inside the `Review` and `OwnReview` schemas
- nullable User-visible `commentary_restriction` inside `OwnReview` and the
  `commentary_locked` error details, but not public `Review`

Add `reportReview`, `listReviewReports`, `getModerationReview`, `applyReviewAction`, and `dismissReviewReport` to the bearer-security loop. Leave `listVenueReviews` in the public loop.

- [ ] **Step 2: Run the contract test**

Run: `go test . -count=1 -run TestOpenAPIDocument`
Expected: FAIL with a missing path or code.

- [ ] **Step 3: Document the routes and close the session**

`openapi.yaml`: the five paths, `ReviewReport`, `ReviewReportRequest`, `ReviewModerationAction`, `OperatorReview` (includes `commentary_removed_at`), `ReviewAuditEntry`, `UserCommentaryRestriction` (reason, note, start time), and `OperatorCommentaryRestriction` (start time plus source audit action). `ReviewAuditEntry` has no `commentary` property. Operator Review detail requires nullable `commentary_restriction`. `OwnReview` requires nullable `commentary_restriction`; the `commentary_locked` response carries the same User-visible object. `Review` and `OwnReview` require `commentary_removed`, and public `Review` has no restriction field.

`backend/AGENTS.md`: readiness text says `0010`. App routes gain the report `POST`. Privileged routes gain the four moderation paths. The write-token sentence gains review reports, review moderation actions, and review-report dismissals.

`docs/architecture/backend.md`: extend the Review paragraph so the module also owns reports, the Commentary restriction, and the audit, and so Venue delist updates pending `review_reports` in its own transaction and still does not import `internal/review`.

`DevSteps.md`: status line becomes Sessions 1–12 done, Session 13 next, 13–14 not started. Sequence table: `| 12. Review safety and moderation API | Done |` and `| 13–14 | Not started |`. Session 12 `**Status:** Done.` plus one paragraph naming the five routes, migration `0010`, readiness `0010`, operator recognition through `requireOperator`, and that delist now actions pending Review Reports.

- [ ] **Step 4: Run the full backend tests**

Run: `gofmt -l . && go vet ./... && go test ./...`
Expected: `gofmt` prints nothing; vet and tests pass. Docker-unavailable skips are the existing message, not failures.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md docs/architecture/backend.md DevSteps.md
git commit -m "docs: publish the review safety contract and close session 12"
```
