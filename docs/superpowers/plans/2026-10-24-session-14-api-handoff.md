# Session 14: API Completion and Admin Handoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Platform Moderator can lift a Commentary restriction through the existing Review action route, a push to `main` applies migrations before it deploys and proves the new process through `/readyz`, and the admin project has a documented API to build against.

**Architecture:** Migration `0012` only widens `review_moderation_actions_action_check`. `internal/review` owns the lift inside `ModerateReview` and the marker clear inside `Save`; there is no new route, package, or Go interface. `/readyz` reports the build commit when `RENDER_GIT_COMMIT` is a full SHA, which lets `.github/workflows/release.yml` prove that the new process is the one answering. `docs/api-handoff.md` walks the sequences; OpenAPI stays the field contract.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL/PostGIS via `testcontainers-go` (`internal/pgtest`), OpenAPI 3.1, GitHub Actions, Supabase CLI `2.84.2`, Render deploy hooks.

**Spec:** `docs/superpowers/specs/2026-10-24-session-14-api-handoff-design.md`

## Resolved in review

A Commentary restriction is lifted through a current Review on a verified or
Delisted Venue. The Review stays rating-only, and the removal stays visible,
until the User writes commentary. The lift's Restriction record belongs to
that Review and carries the Restriction explanation that began the
restriction. A later removal is a new Commentary restriction. A lift assesses
no Review Report, and a lift when nothing stands leaves no Restriction record.

A release succeeds only when public readiness returns the pushed commit. The
release job is the only deployer. An applied migration stays applied when the
deploy fails. A newer push waits. Re-running the failed job retries that same
commit. A process that becomes ready after the 15-minute bound does not
rewrite a failed release.

The handoff states only the rules that change a sequence. Fields stay in
OpenAPI. On User deletion, the Profile and Review moderation decisions remain
as Restriction records without that User's identity. The standing Commentary
restriction ends. A Venue's eligibility decisions remain as that Venue's
history.

## Global Constraints

- Branch `feature/session-14-api-handoff`, merged into `dev` by pull request. Do not push `main`.
- Run Go commands from `backend/`. Plain `go`, `gofmt`; no wrappers.
- Do not edit migrations `0001`–`0011`. Add no Go dependency. Do not promote `gopkg.in/yaml.v3` to a direct dependency.
- `cmd/api` stays composition only. `internal/review` owns the lift and the marker clear. No new route, package, or Go interface.
- Every DB statement uses the existing 2-second `context.WithTimeout`.
- The implementer does not run `supabase login`, `supabase link`, `supabase db push`, or create the GitHub environment, the deploy hook, or the blueprint sync. Task 10 is the operator's step.
- Action string `lift_commentary_restriction`; audit action `commentary_restriction_lifted`; idempotency operation stays `review_moderation_action`.
- Exact copy: `must be omitted`, `must be remove_commentary, delete_review, or lift_commentary_restriction`, `Review not found.`, `SUPABASE_DB_URL must use port 5432`.
- `/readyz` `200` body: `{"status":"ok","commit":"<sha>"}\n` only when `RENDER_GIT_COMMIT` matches `^[0-9a-f]{40}$`; otherwise `{"status":"ok"}\n`. `503` bodies never include `commit`.
- Out of scope: running the release job, a Render API key, `workflow_dispatch`, a required reviewer, a pair route, an operator-supplied lift reason, changing `DELETE /v1/me` behavior, new pagination, the admin app.
- Definition of done: `gofmt -l .` prints nothing; `go vet ./...`, `go test ./...`, and `go build ./...` pass.

## Review Focus

- Two operators lift the same restriction at once with different keys. Exactly one gets `201`, the other `200` with `action_id` null, and there is one `commentary_restriction_lifted` row. Pinned in Task 3 (`TestLiftConcurrent`).
- A lift body with `"note":"   "` or `"reason":"spam"` is `400 must be omitted`, not silently normalized to null and accepted. Pinned in Task 2 (`TestNormalizeLift`).
- A lift key replayed after a later `remove_commentary` re-imposed the restriction returns the stored `201` and does not lift the new restriction. Pinned in Task 3 (`TestLiftReplayAndReuse`).
- A short SHA, an uppercase SHA, or any non-40-hex `RENDER_GIT_COMMIT` must leave `commit` out of `/readyz`, so the release poll can never match a wrong value. Pinned in Task 6 (`TestReadyz`).
- The stored lift idempotency body must not contain a `"commentary"` key, even though the live `201` response includes commentary. Pinned in Task 3 (`TestLiftCommentaryRestriction`).

---

## File map

| File | Responsibility |
| --- | --- |
| `infra/supabase/migrations/0012_commentary_restriction_lift.sql` | widen the audit action check |
| `backend/internal/database/pool.go`, `postgres_test.go`, `review_test.go` | readiness `0012`, constraint test |
| `backend/internal/review/validation.go`, `validation_test.go` | lift validation |
| `backend/internal/review/moderate.go`, `moderate_test.go` | lift transaction and tests |
| `backend/internal/review/postgres.go`, `write_test.go` | marker clear on save, lifecycle test |
| `backend/internal/api/review_moderation_test.go` | HTTP coverage of the lift |
| `backend/internal/config/config.go`, `config_test.go` | `Config.Commit` from `RENDER_GIT_COMMIT` |
| `backend/internal/api/server.go`, `server_test.go` | `Options.Commit`, `/readyz` commit |
| `backend/cmd/api/main.go` | pass `cfg.Commit` |
| `backend/Dockerfile` | keep `RENDER_GIT_COMMIT` in the image |
| `backend/openapi.yaml`, `backend/contract_test.go` | contract and the example rule |
| `.github/workflows/release.yml`, `infra/render.yaml`, `infra/README.md` | release job |
| `docs/api-handoff.md` | sequence guide |
| `backend/AGENTS.md`, `docs/architecture/backend.md`, `DevSteps.md` | docs |

---

### Task 1: Migration `0012` and readiness

**Files:**
- Create: `infra/supabase/migrations/0012_commentary_restriction_lift.sql`
- Modify: `backend/internal/database/pool.go:14`
- Modify: `backend/internal/database/postgres_test.go:145-150`
- Modify: `backend/internal/database/review_test.go`

**Interfaces:**
- Produces: audit action `commentary_restriction_lifted` accepted by `review_moderation_actions_action_check`. Task 3 inserts it.

- [ ] **Step 1: Write the failing tests**

In `postgres_test.go`, the `0011` step now expects `ready == false, reason == "migrations_behind"`. Then apply `0012_commentary_restriction_lift.sql`, insert version `'0012'`, and expect `ready && reason == "ok"`. Keep the backfill assertions after it.

In `review_test.go`, add `TestLiftAuditAction`. Reuse the setup style of `TestReviewConstraints` (one `auth.users` row, one Venue, one Review). Insert into `review_moderation_actions` with `action = 'commentary_restriction_lifted'`, `reason = 'spam'`, an operator id, and assert no error. Insert again with `action = 'restriction_lifted'` and assert a `*pgconn.PgError` with `Code == "23514"`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/database/ -run 'TestMigrationVersions|TestLiftAuditAction' -v`
Expected: FAIL (missing migration file; check violation on `commentary_restriction_lifted`).

- [ ] **Step 3: Write `0012` and bump readiness**

One-line header comment, like `0010`. Then:
```sql
alter table review_moderation_actions
  drop constraint review_moderation_actions_action_check;
alter table review_moderation_actions
  add constraint review_moderation_actions_action_check check (
    action in ('commentary_removed', 'review_deleted', 'commentary_restriction_lifted')
  );
```
No table, column, index, policy, or revoke. `pool.go`: `const requiredMigrationVersion = "0012"`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/database/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0012_commentary_restriction_lift.sql backend/internal/database
git commit -m "feat: allow the commentary restriction lift audit and require migration 0012"
```

---

### Task 2: Lift validation

**Files:**
- Modify: `backend/internal/review/validation.go`
- Modify: `backend/internal/review/validation_test.go`

**Interfaces:**
- Produces: `NormalizeAction(action, reason string, note *string) (string, string, *string, error)`, unchanged signature. For `lift_commentary_restriction` it returns `(action, "", nil, nil)` or a `*ValidationError`. Task 3 relies on the empty reason and nil note in the idempotency command.

- [ ] **Step 1: Write the failing tests**

In `TestNormalizeAction`, change the unknown-action expectation to `must be remove_commentary, delete_review, or lift_commentary_restriction`.

Add `TestNormalizeLift`:
```go
action, reason, note, err := NormalizeAction("lift_commentary_restriction", "", nil)
// want: action == "lift_commentary_restriction", reason == "", note == nil, err == nil
_, _, _, err = NormalizeAction("lift_commentary_restriction", "spam", nil)
// want: *ValidationError with Fields["reason"] == "must be omitted"
blank := "   "
_, _, _, err = NormalizeAction("lift_commentary_restriction", "", &blank)
// want: *ValidationError with Fields["note"] == "must be omitted"
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestNormalizeAction|TestNormalizeLift' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

Add `"lift_commentary_restriction": {}` to `moderationActions` and update the unknown-action message. In `NormalizeAction`, after the allow-list check, the lift branch returns `reason: must be omitted` when `reason != ""`, then `note: must be omitted` when `note != nil`, before any `NormalizeReport` call. Whitespace is not trimmed first. The other two actions keep the current path.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -run 'TestNormalizeAction|TestNormalizeLift' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/validation.go backend/internal/review/validation_test.go
git commit -m "feat: validate the commentary restriction lift action"
```

---

### Task 3: Lift transaction

**Files:**
- Modify: `backend/internal/review/moderate.go`
- Modify: `backend/internal/review/moderate_test.go`

**Interfaces:**
- Consumes: Task 1's audit action, Task 2's `NormalizeAction`, the existing `moderate(h, actor, reviewID, key, action, reason, note)` test helper, `seedRestriction`, `insertModerationAction`, `tokensLeft`, `insertPendingReport`.
- Produces: `ModerateReview` returns `ModerationResult{Created: true, ActionID, ReviewID, Review}` on a lift and `ModerationResult{Created: false, ActionID: nil, ReviewID, Review}` on the no-op. Task 4's lifecycle test and Task 5's HTTP test use these shapes.

- [ ] **Step 1: Write the failing tests**

All use `newWriteHarness`, `userA` as author, `userB` as reporter, and `userC` as operator.

`TestLiftCommentaryRestriction`: verified Venue; Review with `commentary = nil`, `commentary_removed_at` set to `t0`; `seedRestriction(t, h, reviewID, userA, verifiedVenue, "harassment", ptr("gone"))`. Lift with key `lift-1`. Assert: `Created`, non-nil `ActionID`, `Review.CommentaryRemoved` still true; audit row with `action = 'commentary_restriction_lifted'`, `reason = 'harassment'`, `note = 'gone'`, `reviewer_user_id = userA`, `operator_user_id = userC`, `created_at = h.now`; zero `review_commentary_restrictions` rows; the Review's `commentary_removed_at`, `updated_at`, and rating unchanged; `tokensLeft(h.limiter, userC) == 59`; the stored `response_body` for `lift-1` does not contain `"commentary"`.

`TestLiftNoop`: no restriction row. Lift twice with key `lift-noop`. Both return `!Created`, `ActionID == nil`. No audit row, `tokensLeft == 60`, no idempotency row for that key.

`TestLiftReplayAndReuse`: lift with `lift-r` (201). Re-impose by calling `moderate(..., "remove-after", "remove_commentary", "spam", nil)` after restoring commentary with a direct `update reviews set commentary = 'again', commentary_removed_at = null`. Replay `lift-r`: same `ActionID`, `Created`, restriction row still present, tokens unchanged by the replay. Call `moderate(..., "lift-r", "remove_commentary", "spam", nil)`: `errors.Is(err, idempotency.ErrReused)`.

`TestLiftThroughLaterReview`: restriction sourced from an earlier Review that is then deleted (pattern from `TestGetModerationReview`); a new rating-only Review for the same pair. Lift through the new Review id. Assert the new audit row's `review_id` is the new Review and the reason and note are copied from the source.

`TestLiftOnDelistedVenue`: delisted Venue with a restriction; lift is `Created`.

`TestLiftMissingReview`: `moderate(h, userC, unknownVenue, "lift-miss", "lift_commentary_restriction", "", nil)` is `ErrReviewNotFound`.

`TestLiftRateLimited`: drain `userC` to zero tokens; lift returns `*RateLimitError`; restriction row still present; no new audit row.

`TestLiftConcurrent`: one restriction. Two goroutines lift with keys `lift-a` and `lift-b`, released by a shared channel. Assert exactly one `Created` and one not, and exactly one `commentary_restriction_lifted` row.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestLift' -v`
Expected: FAIL (lift writes no audit row).

- [ ] **Step 3: Implement the lift branch in `ModerateReview`**

After the existing idempotency `Lookup` miss, add a lift branch before the `remove_commentary` no-op check. `Lookup` holds the key lock until commit, so a concurrent same-key request already waits there and then replays. Do not add a second lookup.

1. Lock the Review with the existing `select ... from reviews r where r.id = $1::uuid for update` scan. `pgx.ErrNoRows` is `ErrReviewNotFound`.
2. Lock the restriction and read its source:
   ```sql
   select a.reason, a.note
   from review_commentary_restrictions rest
   join review_moderation_actions a on a.id = rest.source_action_id
   where rest.user_id = $1::uuid and rest.venue_id = $2::uuid
   for update of rest
   ```
   No row: commit, then return `ModerationResult{Created: false, ReviewID: cmd.ReviewID, Review: &view}`. `view` comes from `loadOperatorReview` after the lock.
3. Otherwise run `idempotency.Execute`. In the callback: reserve the write token as the other actions do; insert the audit row with `commentary_restriction_lifted`, the copied reason and note, the locked Review's ids, `cmd.ActorID`, and `now`; `delete from review_commentary_restrictions where user_id = $1::uuid and venue_id = $2::uuid`; reload with `loadOperatorReview`; store `storedReviewModeration{ActionID, ReviewID, Review: storeOperatorReview(live)}` with status `201`.

Keep the reservation commit and cancel pattern that is already in place.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -v`
Expected: PASS, including the existing `TestRemoveCommentary*` and `TestDeleteReviewReplay`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/moderate.go backend/internal/review/moderate_test.go
git commit -m "feat: lift a commentary restriction through the review action route"
```

---

### Task 4: Save after a lift, plus the lifecycle test

**Files:**
- Modify: `backend/internal/review/postgres.go:262-266`
- Modify: `backend/internal/review/write_test.go`

**Interfaces:**
- Consumes: Task 3's lift through `moderate`.

- [ ] **Step 1: Write the failing tests**

`TestSaveAfterLiftClearsMarker`: a Review with commentary removed through `moderate(..., "remove_commentary", ...)`, then lifted. `h.now = t0.Add(time.Hour)`. Save `"4"` with `"back"`. Assert the commentary is `back`, `commentary_removed_at` is null, `updated_at == h.now`, `created_at` unchanged, and two audit rows remain (`commentary_removed`, `commentary_restriction_lifted`).

`TestRatingOnlySaveKeepsMarker`: same setup through the lift. Save `"2"` with nil commentary. `commentary_removed_at` is still set. An unchanged repeat is the no-op: `tokensLeft` unchanged.

`TestCommentaryRestrictionLifecycle`: verified Venue; `h.save(userA, verifiedVenue, "4", "loud")`; `moderate` with `remove_commentary`; `h.save` with `"again"` is `ErrCommentaryLocked`; `moderate` with `lift_commentary_restriction` is `Created`; `h.save` with `"again"` succeeds. Then: `commentary_removed_at` null, zero restriction rows, audit actions in `created_at` order are exactly `commentary_removed`, `commentary_restriction_lifted`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/review/ -run 'TestSaveAfterLift|TestRatingOnlySaveKeepsMarker|TestCommentaryRestrictionLifecycle' -v`
Expected: FAIL (the check constraint rejects the save, or the marker stays set).

- [ ] **Step 3: Implement**

In `Save`, change the existing-row update to clear the marker in the same statement:
```sql
update reviews
set rating = $3, commentary = $4, updated_at = $5,
    commentary_removed_at = case when $4::text is null then commentary_removed_at end
where venue_id = $1::uuid and user_id = $2::uuid
```
The insert path is unchanged.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/review/ -v`
Expected: PASS, including `TestLockedClearAndRecreate`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/review/postgres.go backend/internal/review/write_test.go
git commit -m "feat: clear the removal marker when commentary returns after a lift"
```

---

### Task 5: HTTP coverage of the lift

**Files:**
- Modify: `backend/internal/api/review_moderation_test.go`

**Interfaces:**
- Consumes: `fakeReviewModeration.modCmd`, `assertCode`, `requestWith`. The handler `applyReviewAction` already passes `reason` and `note` only when set and not null, so it needs no change.

- [ ] **Step 1: Write the test**

Add `TestReviewLiftRoute`, operator caller, using `reviewModerationHandler`:
- `{"action":"lift_commentary_restriction"}` with a key and a `Created` fake result: `201`, `fake.modCmd.Action == "lift_commentary_restriction"`, `fake.modCmd.Reason == ""`, `fake.modCmd.Note == nil`.
- The same body with `"reason":null,"note":null`: `fake.modCmd.Reason == ""` and `Note == nil`.
- A no-op fake result: `200`, body contains `"action_id":null`.
- The fake returns `&review.ValidationError{Fields: map[string]string{"note": "must be omitted"}}`: `400 validation_error`, body contains `must be omitted`.
- The fake returns `review.ErrReviewNotFound`: `404 review_not_found`.
- An app User (`authenticator.operator = false`): `403 forbidden`, the fake not called.

- [ ] **Step 2: Run**

Run: `go test ./internal/api/ -run 'TestReviewLiftRoute|TestReviewModerationRoutes' -v`
Expected: PASS. If a case fails, fix the handler mapping in `review_moderation.go`. Do not change the fake.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/api/review_moderation_test.go
git commit -m "test: cover the commentary restriction lift over HTTP"
```

---

### Task 6: `/readyz` reports the build commit

**Files:**
- Modify: `backend/internal/config/config.go`, `config_test.go`
- Modify: `backend/internal/api/server.go:26-40,164-180`
- Modify: `backend/internal/api/server_test.go`
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/Dockerfile`

**Interfaces:**
- Produces: `config.Config.Commit string`, read raw from `RENDER_GIT_COMMIT` through the existing `value` lookup. `api.Options.Commit string`. Task 8's poll matches `commit` against `github.sha`.

- [ ] **Step 1: Write the failing tests**

`config_test.go`: a fully valid environment plus `RENDER_GIT_COMMIT=abc` loads with `Commit == "abc"`. Unset loads with `Commit == ""`.

`server_test.go` `TestReadyz`: build the server with `New(Options{Readiness: readyOK(), PublicLimit: allowAll, Commit: sha})` where `sha` is 40 lowercase hex characters. The body is exactly `{"status":"ok","commit":"` + sha + `"}` + `\n`. For each of `""`, `"abc1234"`, `strings.ToUpper(sha)`, and `sha + "0"`, the body is exactly `{"status":"ok"}\n`. A not-ready check with a valid `Commit` keeps the existing `503` bodies, with no `commit`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/config/ ./internal/api/ -run 'TestLoad|TestReadyz' -v`
Expected: FAIL (`unknown field Commit`).

- [ ] **Step 3: Implement**

- `readyBody` gains `Commit string \`json:"commit,omitempty"\``.
- `readyHandler(ready Readiness, commit string)` sets `Commit` only on the `200` path.
- In `New`, pass `options.Commit` when it matches the package-level `regexp.MustCompile("^[0-9a-f]{40}$")`, otherwise `""`.
- In `main.go`, set `Commit: cfg.Commit`.
- In the `Dockerfile` final stage, after `FROM gcr.io/distroless/static-debian12`, add `ARG RENDER_GIT_COMMIT` and `ENV RENDER_GIT_COMMIT=$RENDER_GIT_COMMIT`.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/config/ ./internal/api/ -v && go build ./...`
Expected: PASS; the build succeeds.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config backend/internal/api/server.go backend/internal/api/server_test.go backend/cmd/api/main.go backend/Dockerfile
git commit -m "feat: report the build commit on readiness"
```

---

### Task 7: OpenAPI and the success-example rule

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`

**Interfaces:**
- Consumes: Task 3's lift shapes and Task 6's `commit` field.

- [ ] **Step 1: Write the failing contract tests**

Add `lift_commentary_restriction` and `commentary_restriction_lifted` to the `want` list.

Add `TestSuccessExamples`. It scans `OpenAPI` from the start to the line `components:`. It tracks the last `operationId:`. On each line that is exactly `        "200":` or `        "201":`, it collects the following lines while they are blank or indented more than 8 spaces. When that block contains `application/json` and contains neither `example:` nor `examples:`, it records `operationId status`. A block whose first non-blank line is a `$ref:` is skipped. The test fails and lists every recorded pair.

Add `TestReadyCommit`: the `getReady` operation block contains `commit:` and `^[0-9a-f]{40}$`.

- [ ] **Step 2: Run to verify failure**

Run: `go test . -run 'TestOpenAPIDocument|TestSuccessExamples|TestReadyCommit' -v`
Expected: FAIL, listing the 21 responses below.

- [ ] **Step 3: Write the contract**

- `getReady` `200`: add optional `commit` (`type: string`, `pattern: "^[0-9a-f]{40}$"`). Keep `required: [status]` and `additionalProperties: false`. Change the single `example` to named `examples`: `ok` (`status: ok`) and `release` (`status: ok`, `commit: 4e8b56a…`, a real 40-hex value).
- `applyReviewAction`: summary `Remove commentary, delete a review, or lift a commentary restriction`. The description adds that the lift sends only `action`, copies the restriction's source reason and note into the audit, leaves the Review unchanged, and is `200` with `action_id` null when the pair has no restriction. Add request example `lift: {action: lift_commentary_restriction}`. `ReviewModerationAction` becomes `required: [action]`. The `action` enum adds `lift_commentary_restriction`. The `reason` description says it is required for `remove_commentary` and `delete_review` and must be omitted for the lift. `ReviewAuditEntry.action` adds `commentary_restriction_lifted`. The `400` description adds `must be omitted`.
- Add a success `example` to each of these, consistent with its schema: `201` of `createVenueSubmission`, `reportReview`, `createCheckIn`; `200` of `getMyReview`, `putMyReview`, `listVenueCheckIns`, `listReviewReports`, `getModerationReview`, `dismissReviewReport`, `patchMe`, `listMyVenues`, `getMyVenue`, `correctMyVenue`, `listMyCheckIns`, `dismissVenueReport`, `getModerationVenue`, `correctVenueAsOperator`; both `200` and `201` of `applyReviewAction` and `applyVenueAction`. Use `examples` with `remove`, `delete`, and `lift` entries for `applyReviewAction` `201`. Reuse the ids and timestamps already in the document's other examples.

- [ ] **Step 4: Run to verify pass**

Run: `go test . -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go
git commit -m "docs: document the lift, the release commit, and every success example"
```

---

### Task 8: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`
- Modify: `infra/render.yaml`
- Modify: `infra/README.md`

**Interfaces:**
- Consumes: Task 6's `/readyz` `commit`.

- [ ] **Step 1: Write `release.yml`**

- `name: release`; `on: push: branches: [main]` with no path filter.
- `permissions: contents: read`.
- `concurrency: { group: release, cancel-in-progress: false }`.
- One job `release` on `ubuntu-latest` with `environment: production`.
- `env`: `SUPABASE_DB_URL: ${{ secrets.SUPABASE_DB_URL }}`, `RENDER_DEPLOY_HOOK_URL: ${{ secrets.RENDER_DEPLOY_HOOK_URL }}`, `API_BASE_URL: ${{ vars.API_BASE_URL }}`.

Steps, in order:
1. `actions/checkout@v4`; `actions/setup-go@v5` with `go-version: "1.24"`.
2. `go vet ./...` and `go test ./...` with `working-directory: backend`.
3. Check the inputs, without printing the URL or the hook:
   ```bash
   port=$(sed -nE 's#^postgres(ql)?://.*@[^:/@]+:([0-9]+)/.*#\2#p' <<<"$SUPABASE_DB_URL")
   [[ $port == 5432 ]] || { echo "SUPABASE_DB_URL must use port 5432"; exit 1; }
   [[ $API_BASE_URL =~ ^https://[^/]+$ ]] || { echo "API_BASE_URL must be an https origin with no path"; exit 1; }
   ```
4. `supabase/setup-cli@v3` with `version: 2.84.2`, then `supabase db push --db-url "$SUPABASE_DB_URL" --workdir infra --yes`.
5. Deploy:
   ```bash
   sep='?'; [[ $RENDER_DEPLOY_HOOK_URL == *\?* ]] && sep='&'
   code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${RENDER_DEPLOY_HOOK_URL}${sep}ref=${GITHUB_SHA}")
   [[ $code == 200 || $code == 202 ]] || { echo "deploy hook returned $code"; exit 1; }
   ```
6. Prove the release:
   ```bash
   deadline=$((SECONDS + 900))
   while (( SECONDS < deadline )); do
     body=$(curl -fsS --max-time 10 "$API_BASE_URL/readyz" || true)
     [[ $(jq -r '.status + " " + (.commit // "")' <<<"$body" 2>/dev/null) == "ok $GITHUB_SHA" ]] && exit 0
     sleep 15
   done
   echo "readyz never reported $GITHUB_SHA"; exit 1
   ```

- [ ] **Step 2: Turn off Render auto-deploy**

In `infra/render.yaml`, add `autoDeployTrigger: "off"` under the service. `healthCheckPath` stays `/healthz`.

- [ ] **Step 3: Document the operator steps**

Add a `Release` section to `infra/README.md`. A push to `main` runs `release.yml`, which tests, applies migrations, calls the deploy hook, and succeeds only when `/readyz` reports that commit. Then list the one-time steps for the later promotion: create the GitHub environment `production`, limited to `main` with no reviewer, holding the secrets `SUPABASE_DB_URL` (session pooler, port 5432) and `RENDER_DEPLOY_HOOK_URL`, plus the variable `API_BASE_URL`; copy the deploy hook from the Render service settings; sync the blueprint so `autoDeployTrigger` is off. Render's `/healthz` success is not the release proof.

- [ ] **Step 4: Verify**

Run: `ruby -ryaml -e 'YAML.load_file(".github/workflows/release.yml"); YAML.load_file("infra/render.yaml")'` from the repo root.
Expected: no output and exit code 0. The workflow is not run in this session.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/release.yml infra/render.yaml infra/README.md
git commit -m "ci: release by migrating, deploying through the hook, and proving readiness"
```

---

### Task 9: Handoff document and living docs

**Files:**
- Create: `docs/api-handoff.md`
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the routes, `operationId`s, and behavior from Tasks 1–8.

- [ ] **Step 1: Write `docs/api-handoff.md`**

Four parts, as in the spec's "Handoff document" section. Each step is one line: method, path, caller, success status, and `operationId`.
1. **Calling the API.** Bearer JWT; `Idempotency-Key` on `createVenueSubmission`, the three report routes, `createCheckIn`, and the three moderation action routes; the `{"error":{code,message,request_id}}` envelope; cursor pagination with `limit` 1–100, default 20, `meta.next_cursor`, and `400 cursor: invalid`; `403 forbidden` across the app and operator split.
2. **User sequences.** `patchMe` → `getMe`; `listGenres` / `listVenues` / `getVenue` / `listVenueReviews`; `createVenueSubmission` → `listMyVenues` / `getMyVenue` → `correctMyVenue`; `putMyReview` / `getMyReview` / `deleteMyReview`; `createProfileReport`, `reportVenue`, `reportReview`; `createCheckIn` → `listMyCheckIns` / `listVenueCheckIns` → `deleteMyCheckIn`.
3. **Moderation sequences.** Profile: `listProfileReports` → `getModerationProfile` → `applyProfileModeration` or `dismissProfileReport`. Venue: `listVenueReports` → `getModerationVenue` → `applyVenueAction` / `correctVenueAsOperator` or `dismissVenueReport`. Review: `listReviewReports` → `getModerationReview` → `applyReviewAction` (`remove_commentary`, `delete_review`) or `dismissReviewReport`; the User then sees `409 commentary_locked`; a later `applyReviewAction` with `{"action":"lift_commentary_restriction"}` on a current Review, with no reason. After the Review that began the restriction has ended, the lift waits until that User has a Review there again. After the lift, the Review stays rating-only and the removal stays visible until the User writes commentary.
4. **Account deletion.** The spec's text: callers, the 10-minute session, `DELETE MY ACCOUNT`, `204` only after Supabase confirms, `502 auth_provider_error` keeps local rows, then the removed list and the retained list, linking ADR 0003.

- [ ] **Step 2: Update the living docs**

`backend/AGENTS.md`: readiness `0012` in Runtime; the `internal/review` Layout line adds reports, moderation, the Commentary restriction, and its lift; `/readyz` adds `commit` when `RENDER_GIT_COMMIT` is a full SHA; a line under Shared paths says `.github/workflows/release.yml` is the only deployer and runs on push to `main`.

`docs/architecture/backend.md`: extend the Review paragraph at line 129 with the lift and the marker clear. Below the migration paragraph near line 241, add: a push to `main` runs `release.yml`, which tests, applies committed migrations, deploys through the Render hook, and succeeds only when `/readyz` returns the matching commit.

`DevSteps.md`:
- Line 510: the audit action list adds `commentary_restriction_lifted`.
- Lines 523–524: replace `Session 14 adds the Platform Moderator operation that lifts it before the admin handoff.` with: a Platform Moderator lifts it through `lift_commentary_restriction`, which appends `commentary_restriction_lifted` with the source reason and note and deletes the row; the User's next commentary save clears `commentary_removed_at`.
- Lines 717–719: replace the cascade sentence with the spec's removed and retained split.
- Leave the Session 14 status as `Not started`. Task 10 closes it.

- [ ] **Step 3: Run the full gate**

Run, from `backend/`: `gofmt -l . && go vet ./... && go test ./... && go build ./...`
Expected: `gofmt` prints nothing; vet, tests, and build pass.

- [ ] **Step 4: Commit**

```bash
git add docs/api-handoff.md backend/AGENTS.md docs/architecture/backend.md DevSteps.md
git commit -m "docs: hand off the user, moderation, and account deletion flows"
```

---

### Task 10: Operator handoff and closing Session 14

This task waits for the operator. The implementer prepares the commands and does not run the push.

- [ ] **Step 1: Operator applies `0012`**

The operator starts the Session 14 binary with `make run` from `backend/`, after stopping any older listener on port 8080, and confirms `curl -s localhost:8080/readyz` returns `{"status":"not_ready","reason":"migrations_behind"}`. Then, from the repo root: `supabase db push --workdir infra`. If the direct host is unreachable, use the Session 3 session-pooler fallback on port 5432. Do not use port 6543. The same process must then return `{"status":"ok"}`.

- [ ] **Step 2: Close the session in `DevSteps.md`**

Only after that reply: the status line becomes `**Status:** Sessions 1–14 are done.` The sequence table row becomes `| 14. API completion and admin handoff | Done |`. The Session 14 section becomes `**Status:** Done.`, plus one paragraph naming `lift_commentary_restriction`, migration `0012`, readiness `0012`, `docs/api-handoff.md`, and `.github/workflows/release.yml`, and stating that the first production run waits for the promotion of `dev` to `main`.

- [ ] **Step 3: Commit and open the pull request**

```bash
git add DevSteps.md
git commit -m "docs: close session 14"
git push
gh pr create --base dev --title "Session 14: API completion and admin handoff" --body-file docs/superpowers/specs/2026-10-24-session-14-api-handoff-design.md
```
