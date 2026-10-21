# Session 10 Venue Safety and Moderation API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let operators verify, reject, correct, delist, and reopen Venues; let a Submitter correct a rejected Venue back to pending; let Users report a catalogue Venue; and keep an append-only audit of every venue status change.

**Architecture:** `internal/venue` owns moderation, reports, and every write transaction, in new `moderation.go`, `moderate.go`, `report.go` files that mirror `internal/profile`. Each write opens one transaction, locks the Venue row, and (for `POST`s) uses `idempotency.Lookup` then `idempotency.Execute`, so a no-op neither charges the write limiter nor appends an audit row. `internal/profile` exports two small report-quota helpers so venue and profile reports share one combined limit of 20 per rolling 24 hours. `internal/api` adds a third interface, `VenueModeration`, with the PostgreSQL service and an HTTP-test fake as its two adapters. Migration `0008` adds the `delisted` status, rejection and delisting columns, `venue_reports`, and `venue_moderation_actions`.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL 16, PostGIS 3.5, OpenAPI 3.1, `httptest`, `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-20-session-10-venue-safety-moderation-design.md`

## Global Constraints

- Follow DevSteps and `docs/architecture/backend.md`. Keep `cmd/api` as composition only.
- Do not edit migrations `0001` through `0007`. Add `infra/supabase/migrations/0008_venue_safety.sql`.
- Add no Go dependency. No new package. No operator-provisioning route.
- The API process never applies migrations. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Public catalogue contracts from Sessions 7 and 8 are unchanged. `PublicVenue.verification_status` stays `pending` or `verified`.
- Rejection reasons: `duplicate`, `not_eligible`, `inaccurate_details`, `insufficient_information`, `cannot_verify`, `other`. Delisting reasons: `permanently_closed`, `duplicate`, `no_longer_eligible`, `other`. Venue-report reasons: `permanently_closed`, `moved`, `duplicate`, `incorrect_details`, `no_longer_eligible`, `other`.
- Notes are 1 to 1,000 characters. `other` requires a note for rejection, delisting, and reports. `incorrect_details` also requires a note for reports.
- Combined report quota: 20 moderation Reports per User per rolling 24 hours, shared with profile reports through `report_quota_events`.
- Idempotency operations: `venue_report` (key is the reporter) and `venue_moderation_action` (key is the operator). `POST /v1/venues/{venueID}/reports` and `POST /v1/moderation/venues/{venueID}/actions` require `Idempotency-Key`. `PUT` and `PATCH` routes do not.
- Every new table enables RLS and revokes `public`, `anon`, `authenticated`, `service_role` in the migration that creates it (ADR 0002).
- Operator recognition uses the existing `requireOperator`. App routes use `requireApp`.
- Reads and each transaction use a two-second timeout.
- Work on `feature/session-10-venue-safety` (already created). Merge into `dev` by pull request.
- Commands run from `backend/` with plain `go` and `git`. The spec commit `c1343e5` is already on the branch.

## Review Focus

1. **Correcting a rejected Venue with unchanged content:** the submitter-correction no-op shortcut (`sameSubmission`) must apply only when the Venue is already `pending`. A rejected Venue resubmitted with identical fields must still become `pending` and lose its feedback. Task 4 `TestCorrectRejectedWithSameContent` pins it.
2. **Replay and no-op of a moderation action:** replaying the same key must return the stored response and add no second audit row; the same action on a Venue already in the target state must return `200` with `action_id: null`, add no audit row, and spend no write token; a reused key with a different body must be `idempotency_key_reused`. Task 5 pins all three.
3. **Deleted Submitter or reporter:** `submitted_by` is nullable and profiles can be missing, so moderator reads must left-join and return `null` profiles instead of failing. Task 7 `TestListReportsDeletedSubmitter` pins it.
4. **Report visibility and order of refusals:** a rejected, delisted, genre-less, or unknown Venue is `404 venue_not_found` even for its own Submitter; a self-report on a catalogue Venue is `400 self_report`. Task 7 pins the order (catalogue check first, then self-report) and that a report filed after a delist sees the new status because the Venue row is locked `for share`.
5. **Existing `rejected` rows and seeds:** migration `0008` backfills a reason for any `rejected` row so the new check can be added, and several existing tests insert `rejected` Venues directly. Task 1 updates those seeds, otherwise the whole suite fails on the new constraint.
6. **Known gap, not tested:** `reopen` does not re-run the strong-duplicate check. The operator decides. Documented in the spec and not changed here.

---

## File Structure

New files:

```text
infra/supabase/migrations/0008_venue_safety.sql
backend/internal/database/venue_safety_test.go   # constraint, trigger, and RLS tests for 0008
backend/internal/venue/moderation.go             # types, errors, reason sets, normalization, profile scanning helpers
backend/internal/venue/moderation_test.go        # unit tests for normalization
backend/internal/venue/moderate.go               # ModerateVenue, CorrectVenue, GetModerationVenue, audit and report resolution helpers
backend/internal/venue/moderate_test.go          # PostGIS tests for the operator paths
backend/internal/venue/report.go                 # ReportVenue, ListVenueReports, DismissVenueReport, report cursor
backend/internal/venue/report_test.go            # PostGIS tests for reports
backend/internal/api/venue_moderation.go         # VenueModeration interface, handlers, error mapping
backend/internal/api/venue_moderation_test.go    # HTTP tests with a fake adapter
```

Focused modifications:

```text
backend/internal/database/pool.go                # requiredMigrationVersion 0008
backend/internal/database/postgres_test.go       # migration history test
backend/internal/database/access_test.go         # new tables closed to Data API roles
backend/internal/profile/report.go               # export CheckReportQuota and RecordReportEvent
backend/internal/venue/submission.go             # rejection fields on Submission and submissionBody
backend/internal/venue/own.go                    # rejection columns in submissionColumns and scanSubmission
backend/internal/venue/submit.go                 # Correct accepts rejected; shared applyDraft
backend/internal/venue/postgres_test.go          # insertVenue seeds a rejection reason
backend/internal/venue/submit_test.go            # seedVenue seeds a rejection reason
backend/internal/venue/index_test.go             # query-plan test for report indexes
backend/internal/api/submission.go               # rejection fields in the view; message change
backend/internal/api/server.go                   # Options.VenueModeration and six routes
backend/cmd/api/main.go                          # wire VenueModeration
backend/openapi.yaml, backend/contract_test.go   # contract
backend/AGENTS.md, DevSteps.md                   # readiness, routes, status
```

---

### Task 1: Migration 0008, readiness, and fixture repair

**Files:**
- Create: `infra/supabase/migrations/0008_venue_safety.sql`
- Create: `backend/internal/database/venue_safety_test.go`
- Modify: `backend/internal/database/pool.go:14`, `backend/internal/database/postgres_test.go:117-122`, `backend/internal/database/access_test.go`
- Modify: `backend/internal/venue/postgres_test.go:213-224` (`insertVenue`), `backend/internal/venue/submit_test.go:66-86` (`seedVenue`)

**Interfaces:**
- Produces: tables `venue_reports`, `venue_moderation_actions`; columns `venues.rejection_reason`, `rejection_note`, `delisting_reason`, `delisting_note`; constraint names `venue_reports_pending_pair_key`, indexes `venue_reports_pending_list_key`, `venue_reports_pending_venue_key`. Later tasks rely on these exact names.

- [ ] **Step 1: Write the migration**

Create `infra/supabase/migrations/0008_venue_safety.sql`:

```sql
-- Venue safety: delisting, rejection feedback, Venue Reports, and the moderation audit log.
alter table venues drop constraint venues_verification_status_check;
alter table venues add constraint venues_verification_status_check
  check (verification_status in ('pending', 'verified', 'rejected', 'delisted'));

alter table venues
  add column rejection_reason text,
  add column rejection_note text,
  add column delisting_reason text,
  add column delisting_note text;

-- Rows rejected before feedback existed need a reason for the state check below.
update venues
set rejection_reason = 'other', rejection_note = 'Rejected before rejection feedback existed.'
where verification_status = 'rejected';

alter table venues
  add constraint venues_rejection_reason_check check (
    rejection_reason is null
    or rejection_reason in ('duplicate', 'not_eligible', 'inaccurate_details', 'insufficient_information', 'cannot_verify', 'other')
  ),
  add constraint venues_delisting_reason_check check (
    delisting_reason is null
    or delisting_reason in ('permanently_closed', 'duplicate', 'no_longer_eligible', 'other')
  ),
  add constraint venues_rejection_state_check check (
    (verification_status = 'rejected') = (rejection_reason is not null)
    and (rejection_note is null or rejection_reason is not null)
  ),
  add constraint venues_delisting_state_check check (
    (verification_status = 'delisted') = (delisting_reason is not null)
    and (delisting_note is null or delisting_reason is not null)
  ),
  add constraint venues_rejection_note_check check (
    rejection_note is null or char_length(rejection_note) between 1 and 1000
  ),
  add constraint venues_delisting_note_check check (
    delisting_note is null or char_length(delisting_note) between 1 and 1000
  ),
  add constraint venues_rejection_other_note_check check (
    rejection_reason is distinct from 'other' or rejection_note is not null
  ),
  add constraint venues_delisting_other_note_check check (
    delisting_reason is distinct from 'other' or delisting_note is not null
  );

create table venue_reports (
  id uuid primary key default gen_random_uuid(),
  reporter_user_id uuid not null references auth.users (id) on delete cascade,
  venue_id uuid not null references venues (id) on delete cascade,
  reason text not null,
  note text,
  status text not null default 'pending',
  created_at timestamptz not null default now(),
  resolved_at timestamptz,
  resolved_by_user_id uuid references auth.users (id) on delete set null,
  constraint venue_reports_reason_check check (
    reason in ('permanently_closed', 'moved', 'duplicate', 'incorrect_details', 'no_longer_eligible', 'other')
  ),
  constraint venue_reports_status_check check (status in ('pending', 'actioned', 'dismissed')),
  constraint venue_reports_note_length check (note is null or char_length(note) between 1 and 1000),
  constraint venue_reports_note_required check (
    reason not in ('incorrect_details', 'other') or note is not null
  ),
  constraint venue_reports_resolution_check check (
    (status = 'pending' and resolved_at is null and resolved_by_user_id is null)
    or (status in ('actioned', 'dismissed') and resolved_at is not null)
  )
);

create unique index venue_reports_pending_pair_key
  on venue_reports (reporter_user_id, venue_id)
  where status = 'pending';

create index venue_reports_pending_list_key
  on venue_reports (created_at, id)
  where status = 'pending';

create index venue_reports_pending_venue_key
  on venue_reports (venue_id, created_at, id)
  where status = 'pending';

create table venue_moderation_actions (
  id uuid primary key default gen_random_uuid(),
  venue_id uuid not null references venues (id),
  action text not null,
  prior_status text not null,
  new_status text not null,
  rejection_reason text,
  rejection_note text,
  delisting_reason text,
  delisting_note text,
  actor_user_id uuid references auth.users (id) on delete set null,
  created_at timestamptz not null default now(),
  constraint venue_moderation_actions_transition_check check (
    (action = 'verify' and prior_status = 'pending' and new_status = 'verified')
    or (action = 'reject' and prior_status = 'pending' and new_status = 'rejected')
    or (action = 'delist' and prior_status = 'verified' and new_status = 'delisted')
    or (action = 'reopen' and prior_status = 'delisted' and new_status = 'verified')
    or (action = 'correct' and prior_status = new_status and new_status in ('pending', 'verified'))
  ),
  constraint venue_moderation_actions_reason_check check (
    ((rejection_reason is not null) = (action = 'reject'))
    and ((delisting_reason is not null) = (action = 'delist'))
    and (rejection_reason is null or rejection_reason in ('duplicate', 'not_eligible', 'inaccurate_details', 'insufficient_information', 'cannot_verify', 'other'))
    and (delisting_reason is null or delisting_reason in ('permanently_closed', 'duplicate', 'no_longer_eligible', 'other'))
  ),
  constraint venue_moderation_actions_note_check check (
    (rejection_note is null or (rejection_reason is not null and char_length(rejection_note) between 1 and 1000))
    and (delisting_note is null or (delisting_reason is not null and char_length(delisting_note) between 1 and 1000))
    and (rejection_reason is distinct from 'other' or rejection_note is not null)
    and (delisting_reason is distinct from 'other' or delisting_note is not null)
  )
);

create index venue_moderation_actions_venue_key
  on venue_moderation_actions (venue_id, created_at, id);

create function reject_venue_moderation_action_mutation() returns trigger
language plpgsql as $$
begin
  -- ON DELETE SET NULL updates this row and fires the BEFORE UPDATE trigger.
  -- Nulling actor_user_id is the only change allowed.
  if tg_op = 'UPDATE'
    and new.id is not distinct from old.id
    and new.venue_id is not distinct from old.venue_id
    and new.action is not distinct from old.action
    and new.prior_status is not distinct from old.prior_status
    and new.new_status is not distinct from old.new_status
    and new.rejection_reason is not distinct from old.rejection_reason
    and new.rejection_note is not distinct from old.rejection_note
    and new.delisting_reason is not distinct from old.delisting_reason
    and new.delisting_note is not distinct from old.delisting_note
    and new.created_at is not distinct from old.created_at
    and (new.actor_user_id is not distinct from old.actor_user_id or new.actor_user_id is null)
  then
    return new;
  end if;
  raise exception 'venue moderation actions are append-only' using errcode = '55000';
end $$;

create trigger venue_moderation_actions_append_only
  before update or delete on venue_moderation_actions
  for each row execute function reject_venue_moderation_action_mutation();

create function reject_venue_moderation_action_without_actor() returns trigger
language plpgsql as $$
begin
  if new.actor_user_id is null then
    raise exception 'venue moderation actions require an actor id' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger venue_moderation_actions_actor_required
  before insert on venue_moderation_actions
  for each row execute function reject_venue_moderation_action_without_actor();

alter table venue_reports enable row level security;
alter table venue_moderation_actions enable row level security;
revoke all on table venue_reports from public, anon, authenticated, service_role;
revoke all on table venue_moderation_actions from public, anon, authenticated, service_role;
revoke execute on function reject_venue_moderation_action_mutation() from public, anon, authenticated, service_role;
revoke execute on function reject_venue_moderation_action_without_actor() from public, anon, authenticated, service_role;
```

- [ ] **Step 2: Write the failing persistence test**

Create `backend/internal/database/venue_safety_test.go`:

```go
package database

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metalhops/metalhops/backend/internal/pgtest"
)

func vsCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestVenueSafetyConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	operator := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	venue := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, user, operator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, venue_type)
		values ($1, 'Bar', ST_MakePoint(10, 60)::geography, '1 Street', 'Oslo', 'NO', 'bar')`, venue); err != nil {
		t.Fatal(err)
	}

	// Venue state checks.
	for _, c := range []struct{ name, sql string }{
		{"rejected without reason", `update venues set verification_status = 'rejected' where id = $1`},
		{"reason on a pending venue", `update venues set rejection_reason = 'duplicate' where id = $1`},
		{"rejection other without note", `update venues set verification_status = 'rejected', rejection_reason = 'other' where id = $1`},
		{"unknown rejection reason", `update venues set verification_status = 'rejected', rejection_reason = 'bad' where id = $1`},
		{"delisted without reason", `update venues set verification_status = 'delisted' where id = $1`},
		{"delisting other without note", `update venues set verification_status = 'delisted', delisting_reason = 'other' where id = $1`},
		{"empty note", `update venues set verification_status = 'delisted', delisting_reason = 'duplicate', delisting_note = '' where id = $1`},
	} {
		if _, err := pool.Exec(ctx, c.sql, venue); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'rejected', rejection_reason = 'duplicate' where id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'pending', rejection_reason = null, rejection_note = null where id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update venues set verification_status = 'delisted', delisting_reason = 'permanently_closed' where id = $1`, venue); err != nil {
		t.Fatal(err)
	}

	// Venue reports.
	for _, c := range []struct{ name, sql string }{
		{"incorrect_details without note", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'incorrect_details')`},
		{"other without note", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'other')`},
		{"unknown reason", `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'spam')`},
	} {
		if _, err := pool.Exec(ctx, c.sql, user, venue); vsCode(err) != "23514" {
			t.Fatalf("%s: %v", c.name, err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, user, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'duplicate')`, user, venue); vsCode(err) != "23505" {
		t.Fatalf("second pending report: %v", err)
	}
	if _, err := pool.Exec(ctx, `update venue_reports set status = 'dismissed' where venue_id = $1`, venue); vsCode(err) != "23514" {
		t.Fatalf("resolved without time: %v", err)
	}
	if _, err := pool.Exec(ctx, `update venue_reports set status = 'dismissed', resolved_at = now() where venue_id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'duplicate')`, user, venue); err != nil {
		t.Fatalf("a resolved report blocked a new one: %v", err)
	}

	// Audit log.
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason)
		values ($1, 'delist', 'verified', 'delisted', 'duplicate')`, venue); vsCode(err) != "23514" {
		t.Fatalf("audit without actor: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
		values ($1, 'delist', 'verified', 'delisted', $2)`, venue, operator); vsCode(err) != "23514" {
		t.Fatalf("delist without reason: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id)
		values ($1, 'verify', 'rejected', 'verified', $2)`, venue, operator); vsCode(err) != "23514" {
		t.Fatalf("verify from rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venue_moderation_actions (venue_id, action, prior_status, new_status, delisting_reason, actor_user_id)
		values ($1, 'delist', 'verified', 'delisted', 'duplicate', $2)`, venue, operator); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update venue_moderation_actions set delisting_reason = 'other' where venue_id = $1`, venue); vsCode(err) != "55000" {
		t.Fatalf("audit update: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from venue_moderation_actions where venue_id = $1`, venue); vsCode(err) != "55000" {
		t.Fatalf("audit delete: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, operator); err != nil {
		t.Fatalf("deleting the operator: %v", err)
	}
	var actor *string
	if err := pool.QueryRow(ctx, `select actor_user_id::text from venue_moderation_actions where venue_id = $1`, venue).Scan(&actor); err != nil || actor != nil {
		t.Fatalf("actor after delete %v %v", actor, err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/database -run TestVenueSafetyConstraints -v`
Expected: FAIL (before Step 1's migration exists it fails on missing columns; if you wrote the migration first, move on to Step 4 and use this run as the pass check).

- [ ] **Step 4: Update readiness and the migration-history test**

In `backend/internal/database/pool.go` change `requiredMigrationVersion` to `"0008"`.

In `backend/internal/database/postgres_test.go`, replace the `0007` block (lines 117-122) with:

```go
	execSQL(t, pool, readMigration(t, "0007_venue_submissions.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0007')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0007 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, readMigration(t, "0008_venue_safety.sql"))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0008')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0008 ready %v reason %s", ready, reason)
	}
```

- [ ] **Step 5: Close the new tables to the Data API roles**

In `backend/internal/database/access_test.go`, append to the `statements` list, next to the `profile_reports` entries:

```go
		`select * from venue_reports`,
		`insert into venue_reports (reporter_user_id, venue_id, reason) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'moved')`,
		`update venue_reports set status = 'pending' where false`,
		`delete from venue_reports where false`,
		`select * from venue_moderation_actions`,
		`insert into venue_moderation_actions (venue_id, action, prior_status, new_status, actor_user_id) values ('00000000-0000-4000-8000-000000000002', 'verify', 'pending', 'verified', '00000000-0000-4000-8000-000000000001')`,
		`update venue_moderation_actions set action = 'verify' where false`,
		`delete from venue_moderation_actions where false`,
```

- [ ] **Step 6: Repair fixtures that insert `rejected` Venues**

In `backend/internal/venue/submit_test.go`, inside `seedVenue`, replace the insert so a rejected seed carries a reason. Change the statement to:

```go
	var rejectionReason, rejectionNote *string
	if s.status == "rejected" {
		rejectionReason, rejectionNote = ptr("other"), ptr("seed")
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, location, address, city, country_code, venue_type,
			verification_status, submitted_by, created_at, updated_at, rejection_reason, rejection_note
		) values (
			$1, $2, ST_SetSRID(ST_MakePoint($3::float8, $4::float8), 4326)::geography,
			'1 Metal Street', 'Oslo', 'NO', 'pub', $5, $6::uuid, $7, $8, $9, $10
		)`, s.id, s.name, s.lon, s.lat, s.status, s.submitter, s.created, s.updated, rejectionReason, rejectionNote); err != nil {
		t.Fatal(err)
	}
```

In `backend/internal/venue/postgres_test.go`, replace the `insertVenue` statement with:

```go
	var rejectionReason, rejectionNote *string
	if status == "rejected" {
		rejectionReason, rejectionNote = ptr("other"), ptr("seed")
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (
			id, name, description, location, address, city, country_code, venue_type,
			verification_status, rejection_reason, rejection_note
		) values (
			$1, $2, 'A metal bar', ST_MakePoint(10, 60)::geography,
			'1 Metal Street', 'Oslo', 'DE', 'bar', $3, $4, $5
		)`, id, name, status, rejectionReason, rejectionNote); err != nil {
		t.Fatal(err)
	}
```

- [ ] **Step 7: Run the whole suite**

Run: `go test ./...`
Expected: PASS. If a test elsewhere inserts a `rejected` row without a reason, apply the same two-column fix there.

- [ ] **Step 8: Commit**

```bash
git add ../infra/supabase/migrations/0008_venue_safety.sql internal/database internal/venue/postgres_test.go internal/venue/submit_test.go
git commit -m "feat: add venue safety schema

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 2: Share the report quota

**Files:**
- Modify: `backend/internal/profile/report.go:108-170`
- Test: `backend/internal/profile/report_test.go` (existing tests must stay green; add one)

**Interfaces:**
- Produces, in package `profile`:
  - `func CheckReportQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error` takes the advisory lock `hashtextextended('reports' || chr(31) || userID, 0)`, counts `report_quota_events` in the last 24 hours, and returns `*QuotaError` at 20 or more.
  - `func RecordReportEvent(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error` inserts one `report_quota_events` row.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/profile/report_test.go` (use the file's existing `testPool(t)` and `insertProfile` helpers; read their signatures first with `grep -n "func testPool\|func insertProfile" internal/profile/*_test.go`):

```go
func TestReportQuotaHelpers(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1)`, user); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i := 0; i < 20; i++ {
		if err := CheckReportQuota(ctx, tx, user, now); err != nil {
			t.Fatalf("report %d: %v", i, err)
		}
		if err := RecordReportEvent(ctx, tx, user, now.Add(-time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var quota *QuotaError
	if err := CheckReportQuota(ctx, tx, user, now); !errors.As(err, &quota) || quota.RetryAfter < 1 {
		t.Fatalf("expected quota error, got %v", err)
	}
	if err := CheckReportQuota(ctx, tx, user, now.Add(25*time.Hour)); err != nil {
		t.Fatalf("old events still counted: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/profile -run TestReportQuotaHelpers -v`
Expected: FAIL, `undefined: CheckReportQuota`.

- [ ] **Step 3: Extract the helpers**

In `backend/internal/profile/report.go`, add:

```go
const reportQuota = 20

// CheckReportQuota is shared by every moderation Report: profile, venue, and (later) review.
func CheckReportQuota(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('reports' || chr(31) || $1, 0))`, userID); err != nil {
		return fmt.Errorf("profile: lock: %w", err)
	}
	var count int
	var oldest *time.Time
	if err := tx.QueryRow(ctx, `
		select count(*), min(created_at)
		from report_quota_events
		where user_id = $1::uuid and created_at > $2`, userID, now.Add(-24*time.Hour)).Scan(&count, &oldest); err != nil {
		return fmt.Errorf("profile: quota: %w", err)
	}
	if count < reportQuota {
		return nil
	}
	retryAfter := 1
	if oldest != nil {
		retryAfter = max(1, int(math.Ceil(oldest.Add(24*time.Hour).Sub(now).Seconds())))
	}
	return &QuotaError{RetryAfter: retryAfter}
}

func RecordReportEvent(ctx context.Context, tx pgx.Tx, userID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `insert into report_quota_events (user_id, created_at) values ($1::uuid, $2)`, userID, now); err != nil {
		return fmt.Errorf("profile: report: %w", err)
	}
	return nil
}
```

Then in `Report`, replace the inline lock, count, and quota block (from the `pg_advisory_xact_lock` call through the `if count >= 20 {...}` block) with:

```go
		if err := CheckReportQuota(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
```

and replace the inline `insert into report_quota_events` with:

```go
		if err := RecordReportEvent(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
```

- [ ] **Step 4: Run the profile tests**

Run: `go test ./internal/profile -v`
Expected: PASS, including the existing report quota tests.

- [ ] **Step 5: Commit**

```bash
git add internal/profile/report.go internal/profile/report_test.go
git commit -m "refactor: share the report quota across capabilities

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 3: Moderation types and normalization

**Files:**
- Create: `backend/internal/venue/moderation.go`, `backend/internal/venue/moderation_test.go`

**Interfaces:**
- Consumes: `ValidationError`, `CanonicalVenueID`, `Submission` (Task 4 adds its rejection fields; this task does not need them), `profile.Profile`, `profile.NormalizeNote`.
- Produces (all in package `venue`):

```go
type ModerationCommand struct {
	ActorID, VenueID, Action string
	Reason                   string
	ReasonSet                bool
	Note                     *string
	NoteSet                  bool
	Key                      string
}
type ModerationResult struct {
	Created  bool
	ActionID *string
	Venue    Moderated
}
type Moderated struct {
	Submission
	DelistingReason, DelistingNote *string
}
type PendingReport struct {
	ID        string
	Reason    string
	Note      *string
	CreatedAt time.Time
	Reporter  *profile.Profile
}
type ModerationVenue struct {
	Moderated
	Submitter *profile.Profile
	Reports   []PendingReport
}
type OperatorCorrectCommand struct {
	ActorID, VenueID string
	Fields           RawSubmission
}
type ReportCommand struct {
	ReporterID, VenueID, Reason string
	Note                        *string
	Key                         string
}
type VenueReport struct {
	ID, VenueID, Reason string
	Note                *string
	Status              string
	CreatedAt           time.Time
}
type VenueSummary struct{ ID, Name, City, CountryCode, VerificationStatus string }
type ReportItem struct {
	ID        string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
	Reporter  *profile.Profile
	Venue     VenueSummary
	Submitter *profile.Profile
}
type ReportPage struct {
	Items      []ReportItem
	NextCursor *string
}
type InvalidTransitionError struct{ Action, Status string }
var ErrSelfReport, ErrReportPending, ErrReportNotFound, ErrReportResolved error
func normalizeModeration(ModerationCommand) (moderationState, error)
func normalizeVenueReport(reason string, note *string) (string, *string, error)
```

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/venue/moderation_test.go`:

```go
package venue

import (
	"errors"
	"strings"
	"testing"
)

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("expected ValidationError, got %v", err)
	}
	return v.Fields
}

func TestNormalizeModeration(t *testing.T) {
	id := "00000000-0000-4000-8000-000000000001"
	long := strings.Repeat("x", 1001)
	cases := []struct {
		name   string
		cmd    ModerationCommand
		field  string
		action string
		to     string
	}{
		{"verify", ModerationCommand{VenueID: id, Action: "verify"}, "", "verify", "verified"},
		{"reopen", ModerationCommand{VenueID: id, Action: "reopen"}, "", "reopen", "verified"},
		{"reject", ModerationCommand{VenueID: id, Action: "reject", Reason: "duplicate", ReasonSet: true}, "", "reject", "rejected"},
		{"delist", ModerationCommand{VenueID: id, Action: "delist", Reason: "permanently_closed", ReasonSet: true}, "", "delist", "delisted"},
		{"unknown action", ModerationCommand{VenueID: id, Action: "ban"}, "action", "", ""},
		{"bad id", ModerationCommand{VenueID: "nope", Action: "verify"}, "venue_id", "", ""},
		{"verify with reason", ModerationCommand{VenueID: id, Action: "verify", Reason: "duplicate", ReasonSet: true}, "reason", "", ""},
		{"reopen with note", ModerationCommand{VenueID: id, Action: "reopen", Note: ptr("x"), NoteSet: true}, "note", "", ""},
		{"reject without reason", ModerationCommand{VenueID: id, Action: "reject"}, "reason", "", ""},
		{"reject with delisting reason", ModerationCommand{VenueID: id, Action: "reject", Reason: "moved", ReasonSet: true}, "reason", "", ""},
		{"delist with rejection reason", ModerationCommand{VenueID: id, Action: "delist", Reason: "cannot_verify", ReasonSet: true}, "reason", "", ""},
		{"reject other needs note", ModerationCommand{VenueID: id, Action: "reject", Reason: "other", ReasonSet: true}, "note", "", ""},
		{"note too long", ModerationCommand{VenueID: id, Action: "delist", Reason: "duplicate", ReasonSet: true, Note: &long, NoteSet: true}, "note", "", ""},
	}
	for _, c := range cases {
		state, err := normalizeModeration(c.cmd)
		if c.field != "" {
			if _, ok := fieldsOf(t, err)[c.field]; !ok {
				t.Fatalf("%s: want field %s, got %v", c.name, c.field, err)
			}
			continue
		}
		if err != nil || state.action != c.action || state.to != c.to {
			t.Fatalf("%s: %+v %v", c.name, state, err)
		}
	}
	state, err := normalizeModeration(ModerationCommand{VenueID: id, Action: "reject", Reason: "other", ReasonSet: true, Note: ptr("  Not a venue  "), NoteSet: true})
	if err != nil || state.note == nil || *state.note != "Not a venue" {
		t.Fatalf("note trimming %+v %v", state, err)
	}
}

func TestNormalizeVenueReport(t *testing.T) {
	for _, c := range []struct {
		reason string
		note   *string
		field  string
	}{
		{"moved", nil, ""},
		{"duplicate", ptr("same bar"), ""},
		{"incorrect_details", nil, "note"},
		{"other", nil, "note"},
		{"spam", nil, "reason"},
		{"", nil, "reason"},
	} {
		_, _, err := normalizeVenueReport(c.reason, c.note)
		if c.field == "" && err != nil {
			t.Fatalf("%s: %v", c.reason, err)
		}
		if c.field != "" {
			if _, ok := fieldsOf(t, err)[c.field]; !ok {
				t.Fatalf("%s: want field %s, got %v", c.reason, c.field, err)
			}
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue -run 'TestNormalize' -v`
Expected: FAIL, undefined `normalizeModeration`.

- [ ] **Step 3: Write `moderation.go`**

```go
package venue

import (
	"errors"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

type ModerationCommand struct {
	ActorID   string
	VenueID   string
	Action    string
	Reason    string
	ReasonSet bool
	Note      *string
	NoteSet   bool
	Key       string
}

type Moderated struct {
	Submission
	DelistingReason *string
	DelistingNote   *string
}

type ModerationResult struct {
	Created  bool
	ActionID *string
	Venue    Moderated
}

type PendingReport struct {
	ID        string
	Reason    string
	Note      *string
	CreatedAt time.Time
	Reporter  *profile.Profile
}

type ModerationVenue struct {
	Moderated
	Submitter *profile.Profile
	Reports   []PendingReport
}

type OperatorCorrectCommand struct {
	ActorID string
	VenueID string
	Fields  RawSubmission
}

type ReportCommand struct {
	ReporterID string
	VenueID    string
	Reason     string
	Note       *string
	Key        string
}

type VenueReport struct {
	ID        string
	VenueID   string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
}

type VenueSummary struct {
	ID                 string
	Name               string
	City               string
	CountryCode        string
	VerificationStatus string
}

type ReportItem struct {
	ID        string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
	Reporter  *profile.Profile
	Venue     VenueSummary
	Submitter *profile.Profile
}

type ReportPage struct {
	Items      []ReportItem
	NextCursor *string
}

type InvalidTransitionError struct{ Action, Status string }

func (e *InvalidTransitionError) Error() string { return "venue: invalid transition" }

var (
	ErrSelfReport     = errors.New("venue: self report")
	ErrReportPending  = errors.New("venue: report pending")
	ErrReportNotFound = errors.New("venue: report not found")
	ErrReportResolved = errors.New("venue: report resolved")
)

var rejectionReasons = set("duplicate", "not_eligible", "inaccurate_details", "insufficient_information", "cannot_verify", "other")
var delistingReasons = set("permanently_closed", "duplicate", "no_longer_eligible", "other")
var reportReasons = set("permanently_closed", "moved", "duplicate", "incorrect_details", "no_longer_eligible", "other")

func set(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, v := range values {
		out[v] = struct{}{}
	}
	return out
}

var transitions = map[string]struct{ from, to string }{
	"verify": {"pending", "verified"},
	"reject": {"pending", "rejected"},
	"delist": {"verified", "delisted"},
	"reopen": {"delisted", "verified"},
}

type moderationState struct {
	action, from, to string
	reason, note     *string
}

func normalizeModeration(cmd ModerationCommand) (moderationState, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return moderationState{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	t, ok := transitions[cmd.Action]
	if !ok {
		return moderationState{}, &ValidationError{Fields: map[string]string{"action": "must be verify, reject, delist, or reopen"}}
	}
	state := moderationState{action: cmd.Action, from: t.from, to: t.to}
	switch cmd.Action {
	case "verify", "reopen":
		fields := map[string]string{}
		if cmd.ReasonSet {
			fields["reason"] = "must be omitted"
		}
		if cmd.NoteSet {
			fields["note"] = "must be omitted"
		}
		if len(fields) > 0 {
			return moderationState{}, &ValidationError{Fields: fields}
		}
	case "reject":
		reason, note, err := reasoned(cmd.Reason, cmd.ReasonSet, cmd.Note, rejectionReasons)
		if err != nil {
			return moderationState{}, err
		}
		state.reason, state.note = reason, note
	case "delist":
		reason, note, err := reasoned(cmd.Reason, cmd.ReasonSet, cmd.Note, delistingReasons)
		if err != nil {
			return moderationState{}, err
		}
		state.reason, state.note = reason, note
	}
	return state, nil
}

func reasoned(reason string, set bool, rawNote *string, allowed map[string]struct{}) (*string, *string, error) {
	if !set {
		return nil, nil, &ValidationError{Fields: map[string]string{"reason": "required"}}
	}
	if _, ok := allowed[reason]; !ok {
		return nil, nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	var note *string
	if rawNote != nil {
		normalized, err := normalizeNote(*rawNote)
		if err != nil {
			return nil, nil, err
		}
		note = &normalized
	}
	if reason == "other" && note == nil {
		return nil, nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return &reason, note, nil
}

func normalizeVenueReport(reason string, rawNote *string) (string, *string, error) {
	if _, ok := reportReasons[reason]; !ok {
		return "", nil, &ValidationError{Fields: map[string]string{"reason": "must be a known reason"}}
	}
	var note *string
	if rawNote != nil {
		normalized, err := normalizeNote(*rawNote)
		if err != nil {
			return "", nil, err
		}
		note = &normalized
	}
	if (reason == "incorrect_details" || reason == "other") && note == nil {
		return "", nil, &ValidationError{Fields: map[string]string{"note": "required"}}
	}
	return reason, note, nil
}

// normalizeNote reuses the Profile note rules and reports them as a venue ValidationError.
func normalizeNote(value string) (string, error) {
	note, err := profile.NormalizeNote(value)
	var invalid *profile.ValidationError
	if errors.As(err, &invalid) {
		return "", &ValidationError{Fields: invalid.Fields}
	}
	return note, err
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/venue -run 'TestNormalize' -v`
Expected: PASS. Then `go vet ./internal/venue` (a rename of the local `set` parameter in `reasoned` may be needed because it shadows the package function `set`; if vet or the compiler complains, rename the parameter to `provided`).

- [ ] **Step 5: Commit**

```bash
git add internal/venue/moderation.go internal/venue/moderation_test.go
git commit -m "feat: define venue moderation values

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Rejection feedback and Submitter correction of a rejected Venue

**Files:**
- Modify: `backend/internal/venue/submission.go` (`Submission`, `submissionBody`, `bodyOf`, `submission()`)
- Modify: `backend/internal/venue/own.go` (`submissionColumns`, `scanSubmission`)
- Modify: `backend/internal/venue/submit.go` (`Correct`)
- Modify: `backend/internal/api/submission.go` (`submissionView`, `submissionJSON`, `writeSubmissionError`), `backend/internal/api/submission_test.go:179`
- Test: `backend/internal/venue/submit_test.go`, `backend/internal/venue/own_test.go`

**Interfaces:**
- Consumes: `seedVenue` with `status: "rejected"` (Task 1 gives it reason `other`, note `seed`).
- Produces: `Submission.RejectionReason *string`, `Submission.RejectionNote *string`; `submissionView` gains `rejection_reason` and `rejection_note`; the submitter `Correct` accepts `pending` and `rejected`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/venue/submit_test.go`:

```go
func TestCorrectRejectedVenue(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(40), name: "Old Name", status: "rejected", submitter: ptr(userA), lat: 30, lon: 10})
	punk := genreID(t, h.pool, "Punk")

	got, err := h.service.Correct(context.Background(), CorrectCommand{UserID: userA, VenueID: vid(40), Fields: rawVenue("New Name", 30, 10, punk)})
	if err != nil {
		t.Fatal(err)
	}
	if got.VerificationStatus != "pending" || got.RejectionReason != nil || got.RejectionNote != nil || got.Name != "New Name" {
		t.Fatalf("corrected %+v", got)
	}
	var reason *string
	if err := h.pool.QueryRow(context.Background(), `select rejection_reason from venues where id = $1`, vid(40)).Scan(&reason); err != nil || reason != nil {
		t.Fatalf("feedback not cleared %v %v", reason, err)
	}
	if _, err := h.service.Correct(context.Background(), CorrectCommand{UserID: userA, VenueID: vid(40), Fields: rawVenue("New Name", 30, 10, punk)}); err != nil {
		t.Fatalf("pending recorrection: %v", err)
	}
}

func TestCorrectRejectedWithSameContent(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(41), name: "Same", status: "rejected", submitter: ptr(userA), lat: 31, lon: 10})
	punk := genreID(t, h.pool, "Punk")
	got, err := h.service.Correct(context.Background(), CorrectCommand{UserID: userA, VenueID: vid(41), Fields: rawVenue("Same", 31, 10, punk)})
	if err != nil || got.VerificationStatus != "pending" || got.RejectionReason != nil {
		t.Fatalf("unchanged rejected correction stayed rejected: %+v %v", got, err)
	}
}

func TestCorrectRefusesVerifiedAndDelisted(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(42), name: "Live", status: "verified", submitter: ptr(userA), lat: 32, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status, delisting_reason, submitted_by)
		values ($1, 'Gone', ST_MakePoint(10, 33)::geography, '1 Street', 'Oslo', 'NO', 'pub', 'delisted', 'duplicate', $2)`, vid(43), userA); err != nil {
		t.Fatal(err)
	}
	punk := genreID(t, h.pool, "Punk")
	for _, id := range []string{vid(42), vid(43)} {
		_, err := h.service.Correct(context.Background(), CorrectCommand{UserID: userA, VenueID: id, Fields: rawVenue("X", 1, 1, punk)})
		var notPending *NotPendingError
		if !errors.As(err, &notPending) {
			t.Fatalf("%s: %v", id, err)
		}
	}
}
```

Then find the existing test that expects a rejected correction to fail (`grep -n "rejected" internal/venue/submit_test.go`, around lines 289 and 450). Change its expectation for `rejected` from `NotPendingError` to success, keeping `verified` as an error.

Append to `backend/internal/venue/own_test.go`:

```go
func TestOwnSubmissionCarriesRejectionFeedback(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(44), name: "Nope", status: "rejected", submitter: ptr(userA)})
	got, err := h.service.GetOwn(context.Background(), userA, vid(44))
	if err != nil || got.RejectionReason == nil || *got.RejectionReason != "other" || got.RejectionNote == nil || *got.RejectionNote != "seed" {
		t.Fatalf("%+v %v", got, err)
	}
}
```

In `backend/internal/api/submission_test.go` line 179, change the table row to expect that a `rejected` status is no longer special. Replace:

```go
		{&venue.NotPendingError{Status: "rejected"}, 409, "venue_not_pending", "Only a pending venue can be corrected.", ""},
```

with:

```go
		{&venue.NotPendingError{Status: "delisted"}, 409, "venue_not_pending", "This venue can no longer be corrected.", ""},
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue ./internal/api -run 'TestCorrect|TestOwnSubmission|TestSubmissionRoutes|TestSubmissionErrors' -v` (adjust the last name to the test that contains the table at line 179: `grep -n "func Test" internal/api/submission_test.go`).
Expected: FAIL (no `RejectionReason` field).

- [ ] **Step 3: Add the fields**

`backend/internal/venue/submission.go`: add to `Submission` after `VerificationStatus`:

```go
	RejectionReason    *string
	RejectionNote      *string
```

Add to `submissionBody` after `VerificationStatus`:

```go
	RejectionReason    *string     `json:"rejection_reason"`
	RejectionNote      *string     `json:"rejection_note"`
```

In `bodyOf`, set `RejectionReason: s.RejectionReason, RejectionNote: s.RejectionNote`; in `(b submissionBody) submission()`, copy them back.

`backend/internal/venue/own.go`: extend `submissionColumns` to end `... v.verification_status, v.rejection_reason, v.rejection_note,` before `genresJSON`, and `scanSubmission`'s `Scan` list to `&got.VerificationStatus, &got.RejectionReason, &got.RejectionNote, &genres, ...`. Check other users of `submissionColumns` with `grep -n "submissionColumns" internal/venue/*.go` and keep their column order consistent.

- [ ] **Step 4: Change `Correct`**

In `backend/internal/venue/submit.go`, change the status check and the no-op and update:

```go
	if status != "pending" && status != "rejected" {
		return Submission{}, &NotPendingError{Status: status}
	}
```

```go
	if status == "pending" && sameSubmission(current, draft) {
```

Replace the update statement with:

```go
	if _, err := tx.Exec(txCtx, `
		update venues set
			name = $2, description = $3,
			location = ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
			address = $6, city = $7, country_code = $8, venue_type = $9,
			verification_status = 'pending', rejection_reason = null, rejection_note = null
		where id = $1::uuid`,
```

(arguments unchanged).

- [ ] **Step 5: API view and message**

`backend/internal/api/submission.go`: add to `submissionView` after `VerificationStatus`:

```go
	RejectionReason    *string     `json:"rejection_reason"`
	RejectionNote      *string     `json:"rejection_note"`
```

set both in `submissionJSON`, and in `writeSubmissionError` replace the `notPending` case with:

```go
	case errors.As(err, &notPending):
		WriteError(w, http.StatusConflict, "venue_not_pending", "This venue can no longer be corrected.", id, nil)
```

- [ ] **Step 6: Run the suites**

Run: `go test ./internal/venue ./internal/api -v 2>&1 | tail -30`
Expected: PASS. Fix any exact-JSON assertion in existing API tests by adding the two null fields.

- [ ] **Step 7: Commit**

```bash
git add internal/venue internal/api
git commit -m "feat: let a Submitter correct a rejected venue

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Operator moderation actions

**Files:**
- Create: `backend/internal/venue/moderate.go`, `backend/internal/venue/moderate_test.go`

**Interfaces:**
- Consumes: `normalizeModeration`, `moderationState`, `Moderated`, `ModerationResult`, `InvalidTransitionError` (Task 3); `Submission` with rejection fields (Task 4); `submissionColumns`, `scanSubmission`, `queryRower` (`own.go`); `idempotency.Lookup/Execute`; `s.writes.Reserve`.
- Produces:

```go
func (s *Service) ModerateVenue(ctx context.Context, cmd ModerationCommand) (ModerationResult, error)
func moderated(ctx context.Context, q queryRower, venueID string) (Moderated, error)   // ErrNotFound if absent
func insertAudit(ctx context.Context, tx pgx.Tx, a auditRow) (string, error)
func resolveReports(ctx context.Context, tx pgx.Tx, venueID, actorID string, now time.Time) error
type auditRow struct {
	venueID, action, prior, next, actorID string
	rejectionReason, rejectionNote, delistingReason, delistingNote *string
	at time.Time
}
```

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/venue/moderate_test.go`:

```go
package venue

import (
	"context"
	"errors"
	"testing"
)

const operatorID = "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"

func insertOperator(t *testing.T, h *harness) {
	t.Helper()
	insertUser(t, h.pool, operatorID, "", nil)
	if _, err := h.pool.Exec(context.Background(), `insert into platform_operators (user_id) values ($1)`, operatorID); err != nil {
		t.Fatal(err)
	}
}

func act(h *harness, venueID, action, key string, reason string, note *string) (ModerationResult, error) {
	return h.service.ModerateVenue(context.Background(), ModerationCommand{
		ActorID: operatorID, VenueID: venueID, Action: action, Key: key,
		Reason: reason, ReasonSet: reason != "", Note: note, NoteSet: note != nil,
	})
}

func venueStatus(t *testing.T, h *harness, id string) (status string, rejection, delisting *string) {
	t.Helper()
	if err := h.pool.QueryRow(context.Background(),
		`select verification_status, rejection_reason, delisting_reason from venues where id = $1`, id).Scan(&status, &rejection, &delisting); err != nil {
		t.Fatal(err)
	}
	return
}

func TestModerateLifecycle(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(50), name: "Life", status: "pending", submitter: ptr(userA), lat: 40, lon: 10})

	res, err := act(h, vid(50), "verify", "verify-key-1", "", nil)
	if err != nil || !res.Created || res.ActionID == nil || res.Venue.VerificationStatus != "verified" {
		t.Fatalf("verify %+v %v", res, err)
	}
	res, err = act(h, vid(50), "delist", "delist-key-1", "permanently_closed", nil)
	if err != nil || res.Venue.VerificationStatus != "delisted" || res.Venue.DelistingReason == nil || *res.Venue.DelistingReason != "permanently_closed" {
		t.Fatalf("delist %+v %v", res, err)
	}
	res, err = act(h, vid(50), "reopen", "reopen-key-1", "", nil)
	if err != nil || res.Venue.VerificationStatus != "verified" || res.Venue.DelistingReason != nil {
		t.Fatalf("reopen %+v %v", res, err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1 and actor_user_id = $2`, vid(50), operatorID); n != 3 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestModerateRejectAndTransitions(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	seedVenue(t, h.pool, seed{id: vid(51), name: "Reject Me", status: "pending", lat: 41, lon: 10})
	res, err := act(h, vid(51), "reject", "reject-key-1", "other", ptr("Not a bar"))
	if err != nil || res.Venue.VerificationStatus != "rejected" || res.Venue.RejectionNote == nil || *res.Venue.RejectionNote != "Not a bar" {
		t.Fatalf("reject %+v %v", res, err)
	}
	var bad *InvalidTransitionError
	for _, c := range []struct{ action, reason string }{{"verify", ""}, {"delist", "duplicate"}, {"reopen", ""}} {
		if _, err := act(h, vid(51), c.action, "bad-"+c.action+"-1", c.reason, nil); !errors.As(err, &bad) || bad.Status != "rejected" {
			t.Fatalf("%s from rejected: %v", c.action, err)
		}
	}
	// A different reason on an already rejected venue is a conflict, not a silent change.
	if _, err := act(h, vid(51), "reject", "reject-key-2", "duplicate", nil); !errors.As(err, &bad) {
		t.Fatalf("re-reject with another reason: %v", err)
	}
	if _, err := act(h, vid(52), "verify", "missing-key-1", "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing venue: %v", err)
	}
}

func TestModerateReplayNoopAndReuse(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	seedVenue(t, h.pool, seed{id: vid(53), name: "Replay", status: "pending", lat: 42, lon: 10})

	first, err := act(h, vid(53), "verify", "replay-key-1", "", nil)
	if err != nil || !first.Created {
		t.Fatal(first, err)
	}
	if _, err := h.pool.Exec(context.Background(), `update venues set name = 'Renamed' where id = $1`, vid(53)); err != nil {
		t.Fatal(err)
	}
	replay, err := act(h, vid(53), "verify", "replay-key-1", "", nil)
	if err != nil || !replay.Created || *replay.ActionID != *first.ActionID || replay.Venue.Name != "Replay" {
		t.Fatalf("replay should return the stored response: %+v %v", replay, err)
	}
	if _, err := act(h, vid(53), "delist", "replay-key-1", "duplicate", nil); !errors.Is(err, idempotency.ErrReused) {
		t.Fatalf("reused key: %v", err)
	}

	before := tokensLeft(h.limiter, operatorID)
	noop, err := act(h, vid(53), "verify", "noop-key-001", "", nil)
	if err != nil || noop.Created || noop.ActionID != nil {
		t.Fatalf("no-op %+v %v", noop, err)
	}
	if after := tokensLeft(h.limiter, operatorID); after != before {
		t.Fatalf("no-op spent a write token: %d to %d", before, after)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1`, vid(53)); n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func TestModerateResolvesPendingReports(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(54), name: "Reported", status: "verified", lat: 43, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, userA, vid(54)); err != nil {
		t.Fatal(err)
	}
	if _, err := act(h, vid(54), "delist", "report-key-01", "permanently_closed", nil); err != nil {
		t.Fatal(err)
	}
	var status string
	var by *string
	if err := h.pool.QueryRow(context.Background(), `select status, resolved_by_user_id::text from venue_reports where venue_id = $1`, vid(54)).Scan(&status, &by); err != nil || status != "actioned" || by == nil || *by != operatorID {
		t.Fatalf("report %s %v %v", status, by, err)
	}
}
```

Add `"github.com/metalhops/metalhops/backend/internal/idempotency"` to the imports of this file.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue -run TestModerate -v`
Expected: FAIL, `h.service.ModerateVenue undefined`.

- [ ] **Step 3: Write `moderate.go` (part 1: helpers and `ModerateVenue`)**

```go
package venue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

type moderatedBody struct {
	submissionBody
	DelistingReason *string `json:"delisting_reason"`
	DelistingNote   *string `json:"delisting_note"`
}

type storedModeration struct {
	ActionID string        `json:"action_id"`
	Venue    moderatedBody `json:"venue"`
}

type moderationCommandJSON struct {
	VenueID string  `json:"venue_id"`
	Action  string  `json:"action"`
	Reason  *string `json:"reason"`
	Note    *string `json:"note"`
}

type auditRow struct {
	venueID, action, prior, next, actorID                          string
	rejectionReason, rejectionNote, delistingReason, delistingNote *string
	at                                                             time.Time
}

func moderated(ctx context.Context, q queryRower, venueID string) (Moderated, error) {
	sub, err := scanSubmission(q.QueryRow(ctx, `select `+submissionColumns+` from venues v where v.id = $1::uuid`, venueID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Moderated{}, ErrNotFound
	}
	if err != nil {
		return Moderated{}, err
	}
	out := Moderated{Submission: sub}
	if err := q.QueryRow(ctx, `select delisting_reason, delisting_note from venues where id = $1::uuid`, venueID).
		Scan(&out.DelistingReason, &out.DelistingNote); err != nil {
		return Moderated{}, fmt.Errorf("venue: delisting: %w", err)
	}
	return out, nil
}

func moderatedToBody(m Moderated) moderatedBody {
	return moderatedBody{submissionBody: bodyOf(m.Submission), DelistingReason: m.DelistingReason, DelistingNote: m.DelistingNote}
}

func (b moderatedBody) moderated() Moderated {
	return Moderated{Submission: b.submission(), DelistingReason: b.DelistingReason, DelistingNote: b.DelistingNote}
}

func insertAudit(ctx context.Context, tx pgx.Tx, a auditRow) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		insert into venue_moderation_actions (
			venue_id, action, prior_status, new_status,
			rejection_reason, rejection_note, delisting_reason, delisting_note,
			actor_user_id, created_at
		) values ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9::uuid, $10)
		returning id::text`,
		a.venueID, a.action, a.prior, a.next,
		a.rejectionReason, a.rejectionNote, a.delistingReason, a.delistingNote,
		a.actorID, a.at).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("venue: audit: %w", err)
	}
	return id, nil
}

func resolveReports(ctx context.Context, tx pgx.Tx, venueID, actorID string, now time.Time) error {
	if _, err := tx.Exec(ctx, `
		update venue_reports
		set status = 'actioned', resolved_at = $3, resolved_by_user_id = $2::uuid
		where venue_id = $1::uuid and status = 'pending'`, venueID, actorID, now); err != nil {
		return fmt.Errorf("venue: reports: %w", err)
	}
	return nil
}

func textEqual(got, want *string) bool {
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

func (s *Service) ModerateVenue(ctx context.Context, cmd ModerationCommand) (ModerationResult, error) {
	state, err := normalizeModeration(cmd)
	if err != nil {
		return ModerationResult{}, err
	}
	if s.writes == nil {
		return ModerationResult{}, errWritesUnavailable
	}
	now := s.clock()
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ModerationResult{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	var status string
	var rejReason, rejNote, delReason, delNote *string
	err = tx.QueryRow(txCtx, `
		select verification_status, rejection_reason, rejection_note, delisting_reason, delisting_note
		from venues where id = $1::uuid for update`, cmd.VenueID).Scan(&status, &rejReason, &rejNote, &delReason, &delNote)
	if errors.Is(err, pgx.ErrNoRows) {
		return ModerationResult{}, ErrNotFound
	}
	if err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	command, err := json.Marshal(moderationCommandJSON{VenueID: cmd.VenueID, Action: cmd.Action, Reason: state.reason, Note: state.note})
	if err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	req := idempotency.Request{UserID: cmd.ActorID, Operation: "venue_moderation_action", Key: cmd.Key, Command: command}
	stored, found, err := idempotency.Lookup(txCtx, tx, now, req)
	if err != nil {
		return ModerationResult{}, err
	}
	if found {
		result, err := moderationFromStored(stored)
		if err != nil {
			return ModerationResult{}, err
		}
		if err := tx.Commit(txCtx); err != nil {
			return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
		}
		return result, nil
	}
	if status == state.to {
		curReason, curNote := rejReason, rejNote
		if state.to == "delisted" {
			curReason, curNote = delReason, delNote
		}
		if state.to == "verified" {
			curReason, curNote = nil, nil
		}
		if !textEqual(curReason, state.reason) || !textEqual(curNote, state.note) {
			return ModerationResult{}, &InvalidTransitionError{Action: cmd.Action, Status: status}
		}
		current, err := moderated(txCtx, tx, cmd.VenueID)
		if err != nil {
			return ModerationResult{}, err
		}
		if err := tx.Commit(txCtx); err != nil {
			return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
		}
		return ModerationResult{Created: false, Venue: current}, nil
	}
	if status != state.from {
		return ModerationResult{}, &InvalidTransitionError{Action: cmd.Action, Status: status}
	}

	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	executed, err := idempotency.Execute(txCtx, tx, now, req, func(ctx context.Context) (idempotency.Result, error) {
		next, retryAfter, ok := s.writes.Reserve(cmd.ActorID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var newRejReason, newRejNote, newDelReason, newDelNote *string
		switch state.to {
		case "rejected":
			newRejReason, newRejNote = state.reason, state.note
		case "delisted":
			newDelReason, newDelNote = state.reason, state.note
		}
		if _, err := tx.Exec(ctx, `
			update venues set verification_status = $2,
				rejection_reason = $3, rejection_note = $4, delisting_reason = $5, delisting_note = $6
			where id = $1::uuid`, cmd.VenueID, state.to, newRejReason, newRejNote, newDelReason, newDelNote); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: moderate: %w", err)
		}
		actionID, err := insertAudit(ctx, tx, auditRow{
			venueID: cmd.VenueID, action: cmd.Action, prior: status, next: state.to, actorID: cmd.ActorID,
			rejectionReason: newRejReason, rejectionNote: newRejNote, delistingReason: newDelReason, delistingNote: newDelNote, at: now,
		})
		if err != nil {
			return idempotency.Result{}, err
		}
		if err := resolveReports(ctx, tx, cmd.VenueID, cmd.ActorID, now); err != nil {
			return idempotency.Result{}, err
		}
		view, err := moderated(ctx, tx, cmd.VenueID)
		if err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(storedModeration{ActionID: actionID, Venue: moderatedToBody(view)})
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: moderate: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return ModerationResult{}, err
	}
	result, err := moderationFromStored(executed)
	if err != nil {
		return ModerationResult{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return ModerationResult{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return result, nil
}

func moderationFromStored(r idempotency.Result) (ModerationResult, error) {
	var body storedModeration
	if err := json.Unmarshal(r.Body, &body); err != nil {
		return ModerationResult{}, fmt.Errorf("venue: moderate: %w", err)
	}
	id := body.ActionID
	return ModerationResult{Created: true, ActionID: &id, Venue: body.Venue.moderated()}, nil
}
```

Note: `scanSubmission` converts `CreatedAt`/`UpdatedAt` to UTC, so stored and live views agree. `moderated` runs two queries on the same `tx`; that is intentional (see Task 6 for the shared reads).

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/venue -run TestModerate -v`
Expected: PASS. If `countRows` or `tokensLeft` complain about arguments, read their signatures in `submit_test.go:118-146`.

- [ ] **Step 5: Commit**

```bash
git add internal/venue/moderate.go internal/venue/moderate_test.go
git commit -m "feat: moderate venues with an append-only audit

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 6: Operator correction and the moderation detail read

**Files:**
- Modify: `backend/internal/venue/submit.go` (extract `applyDraft`), `backend/internal/venue/moderate.go`
- Modify: `backend/internal/venue/moderation.go` (profile scanning helpers)
- Test: `backend/internal/venue/moderate_test.go`

**Interfaces:**
- Consumes: Task 5's `moderated`, `insertAudit`, `resolveReports`, `auditRow`; `checkGenres`, `checkDuplicate`, `replaceGenres`, `sameSubmission`.
- Produces:

```go
func (s *Service) CorrectVenue(ctx context.Context, cmd OperatorCorrectCommand) (Moderated, error)
func (s *Service) GetModerationVenue(ctx context.Context, venueID string) (ModerationVenue, error)
func applyDraft(ctx context.Context, tx pgx.Tx, venueID string, d Draft) error   // update row + genres
func profileColumns(alias string) string                                       // nine columns, aliased
type nullableProfile struct{ ... }; func (n *nullableProfile) dest() []any; func (n nullableProfile) profile() *profile.Profile
```

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/venue/moderate_test.go`:

```go
func TestOperatorCorrect(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(60), name: "Typo Bar", status: "verified", submitter: ptr(userA), lat: 50, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(61), name: "Rejected", status: "rejected", lat: 51, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'incorrect_details' ) `, userB, vid(60)); err == nil {
		t.Fatal("note should be required")
	}
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason, note) values ($1, $2, 'incorrect_details', 'typo')`, userB, vid(60)); err != nil {
		t.Fatal(err)
	}
	punk := genreID(t, h.pool, "Punk")
	correct := func(id string, raw RawSubmission) (Moderated, error) {
		return h.service.CorrectVenue(context.Background(), OperatorCorrectCommand{ActorID: operatorID, VenueID: id, Fields: raw})
	}

	got, err := correct(vid(60), rawVenue("Type Bar", 50, 10, punk))
	if err != nil || got.Name != "Type Bar" || got.VerificationStatus != "verified" {
		t.Fatalf("correct %+v %v", got, err)
	}
	var prior, next string
	if err := h.pool.QueryRow(context.Background(), `select prior_status, new_status from venue_moderation_actions where venue_id = $1 and action = 'correct'`, vid(60)).Scan(&prior, &next); err != nil || prior != "verified" || next != "verified" {
		t.Fatalf("audit %s %s %v", prior, next, err)
	}
	var reportStatus string
	if err := h.pool.QueryRow(context.Background(), `select status from venue_reports where venue_id = $1`, vid(60)).Scan(&reportStatus); err != nil || reportStatus != "actioned" {
		t.Fatalf("report %s %v", reportStatus, err)
	}
	if _, err := correct(vid(60), rawVenue("Type Bar", 50, 10, punk)); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, h, `select count(*) from venue_moderation_actions where venue_id = $1 and action = 'correct'`, vid(60)); n != 1 {
		t.Fatalf("unchanged correction wrote audit: %d", n)
	}
	var bad *InvalidTransitionError
	if _, err := correct(vid(61), rawVenue("Rejected 2", 51, 10, punk)); !errors.As(err, &bad) || bad.Status != "rejected" {
		t.Fatalf("rejected correction: %v", err)
	}
	if _, err := correct(vid(99), rawVenue("None", 1, 1, punk)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	seedVenue(t, h.pool, seed{id: vid(62), name: "Type Bar", status: "verified", lat: 52, lon: 10})
	var dup *DuplicateError
	if _, err := correct(vid(62), rawVenue("Type Bar", 50, 10, punk)); !errors.As(err, &dup) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestGetModerationVenue(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(70), name: "Detail", status: "verified", submitter: ptr(userA), lat: 60, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `insert into venue_reports (reporter_user_id, venue_id, reason) values ($1, $2, 'moved')`, userB, vid(70)); err != nil {
		t.Fatal(err)
	}
	got, err := h.service.GetModerationVenue(context.Background(), vid(70))
	if err != nil || got.Submitter == nil || got.Submitter.ID != userA || len(got.Reports) != 1 || got.Reports[0].Reporter == nil || got.Reports[0].Reporter.ID != userB {
		t.Fatalf("%+v %v", got, err)
	}
	seedVenue(t, h.pool, seed{id: vid(71), name: "Orphan", status: "pending", lat: 61, lon: 10})
	orphan, err := h.service.GetModerationVenue(context.Background(), vid(71))
	if err != nil || orphan.Submitter != nil || len(orphan.Reports) != 0 {
		t.Fatalf("%+v %v", orphan, err)
	}
	if _, err := h.service.GetModerationVenue(context.Background(), vid(72)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := h.service.GetModerationVenue(context.Background(), "nope"); err == nil {
		t.Fatal("bad id accepted")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue -run 'TestOperatorCorrect|TestGetModerationVenue' -v`
Expected: FAIL, `CorrectVenue undefined`.

- [ ] **Step 3: Extract `applyDraft` from `Correct`**

In `submit.go`, add:

```go
func applyDraft(ctx context.Context, tx pgx.Tx, venueID string, d Draft) error {
	if _, err := tx.Exec(ctx, `
		update venues set
			name = $2, description = $3,
			location = ST_SetSRID(ST_MakePoint($4::float8, $5::float8), 4326)::geography,
			address = $6, city = $7, country_code = $8, venue_type = $9
		where id = $1::uuid`,
		venueID, d.Name, d.Description, d.Longitude, d.Latitude,
		d.Address, d.City, d.CountryCode, d.VenueType); err != nil {
		return fmt.Errorf("venue: correct: %w", err)
	}
	return replaceGenres(ctx, tx, venueID, d.GenreIDs)
}
```

In the submitter `Correct`, replace the update statement and `replaceGenres` call with:

```go
	if _, err := tx.Exec(txCtx, `
		update venues set verification_status = 'pending', rejection_reason = null, rejection_note = null
		where id = $1::uuid`, cmd.VenueID); err != nil {
		return Submission{}, fmt.Errorf("venue: correct: %w", err)
	}
	if err := applyDraft(txCtx, tx, cmd.VenueID, draft); err != nil {
		return Submission{}, err
	}
```

Run `go test ./internal/venue -run 'Correct' -v` and confirm it still passes before continuing.

- [ ] **Step 4: Add profile scanning helpers**

Append to `moderation.go` (add imports `strings` is not needed; add `fmt`):

```go
func profileColumns(alias string) string {
	return fmt.Sprintf(`%[1]s.id::text, %[1]s.username, %[1]s.display_name, %[1]s.status,
		%[1]s.moderation_reason, %[1]s.moderation_note, %[1]s.suspended_until, %[1]s.created_at, %[1]s.updated_at`, alias)
}

// nullableProfile scans a LEFT JOINed profile; every column is null when the User has no Profile.
type nullableProfile struct {
	id, username, displayName, status, reason, note *string
	until, createdAt, updatedAt                     *time.Time
}

func (n *nullableProfile) dest() []any {
	return []any{&n.id, &n.username, &n.displayName, &n.status, &n.reason, &n.note, &n.until, &n.createdAt, &n.updatedAt}
}

func (n nullableProfile) profile() *profile.Profile {
	if n.id == nil {
		return nil
	}
	p := profile.Profile{
		ID: *n.id, Username: *n.username, DisplayName: *n.displayName, Status: *n.status,
		ModerationReason: n.reason, ModerationNote: n.note, SuspendedUntil: n.until,
		CreatedAt: n.createdAt.UTC(), UpdatedAt: n.updatedAt.UTC(),
	}
	if p.SuspendedUntil != nil {
		until := p.SuspendedUntil.UTC()
		p.SuspendedUntil = &until
	}
	return &p
}
```

- [ ] **Step 5: Write `CorrectVenue` and `GetModerationVenue`**

Append to `moderate.go`:

```go
func (s *Service) CorrectVenue(ctx context.Context, cmd OperatorCorrectCommand) (Moderated, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return Moderated{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	draft, err := ParseSubmission(cmd.Fields)
	if err != nil {
		return Moderated{}, err
	}
	if s.writes == nil {
		return Moderated{}, errWritesUnavailable
	}
	now := s.clock()
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return Moderated{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var status string
	err = tx.QueryRow(txCtx, `select verification_status from venues where id = $1::uuid for update`, cmd.VenueID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Moderated{}, ErrNotFound
	}
	if err != nil {
		return Moderated{}, fmt.Errorf("venue: correct: %w", err)
	}
	if status != "pending" && status != "verified" {
		return Moderated{}, &InvalidTransitionError{Action: "correct", Status: status}
	}
	if err := checkGenres(txCtx, tx, draft.GenreIDs); err != nil {
		return Moderated{}, err
	}
	current, err := moderated(txCtx, tx, cmd.VenueID)
	if err != nil {
		return Moderated{}, err
	}
	if sameSubmission(current.Submission, draft) {
		if err := tx.Commit(txCtx); err != nil {
			return Moderated{}, fmt.Errorf("venue: commit: %w", err)
		}
		return current, nil
	}
	// The operator's own id is not a Submitter, so only Verified matches are disclosed to them.
	if err := checkDuplicate(txCtx, tx, cmd.ActorID, &cmd.VenueID, draft); err != nil {
		return Moderated{}, err
	}
	reservation, retryAfter, ok := s.writes.Reserve(cmd.ActorID)
	if !ok {
		return Moderated{}, &RateLimitError{RetryAfter: retryAfter}
	}
	committed := false
	defer func() {
		if !committed {
			reservation.Cancel()
		}
	}()
	if err := applyDraft(txCtx, tx, cmd.VenueID, draft); err != nil {
		return Moderated{}, err
	}
	if _, err := insertAudit(txCtx, tx, auditRow{
		venueID: cmd.VenueID, action: "correct", prior: status, next: status, actorID: cmd.ActorID, at: now,
	}); err != nil {
		return Moderated{}, err
	}
	if err := resolveReports(txCtx, tx, cmd.VenueID, cmd.ActorID, now); err != nil {
		return Moderated{}, err
	}
	got, err := moderated(txCtx, tx, cmd.VenueID)
	if err != nil {
		return Moderated{}, err
	}
	if err := tx.Commit(txCtx); err != nil {
		return Moderated{}, fmt.Errorf("venue: commit: %w", err)
	}
	reservation.Commit()
	committed = true
	return got, nil
}

// shortcut: the detail lists at most 100 pending reports, oldest first; add a cursor if a Venue ever collects more.
const detailReportLimit = 100

func (s *Service) GetModerationVenue(ctx context.Context, venueID string) (ModerationVenue, error) {
	if !CanonicalVenueID(venueID) {
		return ModerationVenue{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	base, err := moderated(queryCtx, s.pool, venueID)
	if err != nil {
		return ModerationVenue{}, err
	}
	out := ModerationVenue{Moderated: base, Reports: []PendingReport{}}
	var submitter nullableProfile
	if err := s.pool.QueryRow(queryCtx, `
		select `+profileColumns("sp")+`
		from venues v left join profiles sp on sp.id = v.submitted_by
		where v.id = $1::uuid`, venueID).Scan(submitter.dest()...); err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: submitter: %w", err)
	}
	out.Submitter = submitter.profile()
	rows, err := s.pool.Query(queryCtx, `
		select r.id::text, r.reason, r.note, r.created_at, `+profileColumns("rp")+`
		from venue_reports r left join profiles rp on rp.id = r.reporter_user_id
		where r.venue_id = $1::uuid and r.status = 'pending'
		order by r.created_at, r.id
		limit $2`, venueID, detailReportLimit)
	if err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item PendingReport
		var reporter nullableProfile
		if err := rows.Scan(append([]any{&item.ID, &item.Reason, &item.Note, &item.CreatedAt}, reporter.dest()...)...); err != nil {
			return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
		}
		item.CreatedAt = item.CreatedAt.UTC()
		item.Reporter = reporter.profile()
		out.Reports = append(out.Reports, item)
	}
	if err := rows.Err(); err != nil {
		return ModerationVenue{}, fmt.Errorf("venue: reports: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 6: Run the venue suite**

Run: `go test ./internal/venue -v 2>&1 | tail -30`
Expected: PASS. Note the duplicate test relies on `checkDuplicate` ignoring the excluded self id (`v.id is distinct from $5`); vid(62) is verified, 50 degrees north, 10 degrees east is about 222 km from latitude 52, so move vid(62) to `lat: 50.0003, lon: 10` if the duplicate case does not trip (100 m is about 0.0009 degrees).

- [ ] **Step 7: Commit**

```bash
git add internal/venue
git commit -m "feat: correct venues as an operator and read moderation detail

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Venue reports

**Files:**
- Create: `backend/internal/venue/report.go`, `backend/internal/venue/report_test.go`
- Modify: `backend/internal/venue/index_test.go`

**Interfaces:**
- Consumes: `normalizeVenueReport`, `ReportCommand`, `VenueReport`, `ReportItem`, `ReportPage`, error vars (Task 3); `profileColumns`, `nullableProfile` (Task 6); `profile.CheckReportQuota`, `profile.RecordReportEvent`, `profile.RequireContributor`, `(*profile.Service).NormalizeExpiry`.
- Produces:

```go
func (s *Service) ReportVenue(ctx context.Context, cmd ReportCommand) (VenueReport, error)
func (s *Service) ListVenueReports(ctx context.Context, limit int, cursor string) (ReportPage, error)
func (s *Service) DismissVenueReport(ctx context.Context, actorID, reportID string) (ReportItem, error)
const reportListSQL, venueReportsSQL  // exported to the plan test
```

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/venue/report_test.go`:

```go
package venue

import (
	"context"
	"errors"
	"testing"

	"github.com/metalhops/metalhops/backend/internal/profile"
)

func report(h *harness, user, venueID, key, reason string, note *string) (VenueReport, error) {
	return h.service.ReportVenue(context.Background(), ReportCommand{ReporterID: user, VenueID: venueID, Reason: reason, Note: note, Key: key})
}

func TestReportVenue(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(80), name: "Open", status: "verified", submitter: ptr(userA), lat: 70, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(81), name: "Pending OK", status: "pending", lat: 71, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(82), name: "Rejected", status: "rejected", submitter: ptr(userB), lat: 72, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status, delisting_reason)
		values ($1, 'Delisted', ST_MakePoint(10, 73)::geography, '1 Street', 'Oslo', 'NO', 'pub', 'delisted', 'duplicate')`, vid(83)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `
		insert into venues (id, name, location, address, city, country_code, venue_type, verification_status)
		values ($1, 'No genre', ST_MakePoint(10, 74)::geography, '1 Street', 'Oslo', 'NO', 'pub', 'pending')`, vid(84)); err != nil {
		t.Fatal(err)
	}

	got, err := report(h, userB, vid(80), "report-key-01", "moved", nil)
	if err != nil || got.Status != "pending" || got.VenueID != vid(80) {
		t.Fatalf("report %+v %v", got, err)
	}
	if _, err := report(h, userB, vid(81), "report-key-02", "other", ptr("closed down")); err != nil {
		t.Fatalf("pending venue: %v", err)
	}
	// Repeating the same key replays; a second pending report with a new key conflicts.
	if replay, err := report(h, userB, vid(80), "report-key-01", "moved", nil); err != nil || replay.ID != got.ID {
		t.Fatalf("replay %+v %v", replay, err)
	}
	if _, err := report(h, userB, vid(80), "report-key-03", "duplicate", nil); !errors.Is(err, ErrReportPending) {
		t.Fatalf("second report: %v", err)
	}
	if _, err := report(h, userA, vid(80), "report-key-04", "moved", nil); !errors.Is(err, ErrSelfReport) {
		t.Fatalf("self report: %v", err)
	}
	for _, id := range []string{vid(82), vid(83), vid(84), vid(85)} {
		if _, err := report(h, userA, id, "hidden-"+id[len(id)-2:]+"-key", "moved", nil); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	// Own rejected Venue: the catalogue check comes first.
	if _, err := report(h, userB, vid(82), "hidden-own-key1", "moved", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("own rejected: %v", err)
	}
	if _, err := report(h, userB, vid(81), "report-key-05", "incorrect_details", nil); err == nil {
		t.Fatal("missing note accepted")
	}
	if _, err := report(h, userB, "nope", "report-key-06", "moved", nil); err == nil {
		t.Fatal("bad venue id accepted")
	}
}

func TestReportQuotaIsShared(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(86), name: "Quota", status: "verified", submitter: ptr(userA), lat: 75, lon: 10})
	if _, err := h.pool.Exec(context.Background(), `
		insert into report_quota_events (user_id, created_at) select $1, $2 from generate_series(1, 20)`, userB, submitNow.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	var quota *profile.QuotaError
	if _, err := report(h, userB, vid(86), "quota-key-001", "moved", nil); !errors.As(err, &quota) {
		t.Fatalf("want quota error, got %v", err)
	}
}

func TestListAndDismissReports(t *testing.T) {
	h := newHarness(t)
	insertOperator(t, h)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(90), name: "Listed", status: "verified", submitter: ptr(userA), lat: 76, lon: 10})
	seedVenue(t, h.pool, seed{id: vid(91), name: "Orphan Venue", status: "verified", lat: 77, lon: 10})
	for _, id := range []string{vid(90), vid(91)} {
		if _, err := report(h, userB, id, "list-key-"+id[len(id)-2:]+"-x", "moved", nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := h.service.ListVenueReports(context.Background(), 1, "")
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("page 1 %+v %v", page, err)
	}
	if page.Items[0].Venue.ID != vid(90) || page.Items[0].Reporter == nil || page.Items[0].Submitter == nil || page.Items[0].Submitter.ID != userA {
		t.Fatalf("item %+v", page.Items[0])
	}
	second, err := h.service.ListVenueReports(context.Background(), 1, *page.NextCursor)
	if err != nil || len(second.Items) != 1 || second.NextCursor != nil || second.Items[0].Submitter != nil {
		t.Fatalf("orphan submitter %+v %v", second, err)
	}
	if _, err := h.service.ListVenueReports(context.Background(), 0, ""); err == nil {
		t.Fatal("limit 0 accepted")
	}
	if _, err := h.service.ListVenueReports(context.Background(), 5, "garbage"); err == nil {
		t.Fatal("bad cursor accepted")
	}

	item, err := h.service.DismissVenueReport(context.Background(), operatorID, page.Items[0].ID)
	if err != nil || item.Status != "dismissed" {
		t.Fatalf("dismiss %+v %v", item, err)
	}
	if again, err := h.service.DismissVenueReport(context.Background(), operatorID, page.Items[0].ID); err != nil || again.Status != "dismissed" {
		t.Fatalf("dismiss twice %+v %v", again, err)
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, "nope"); err == nil {
		t.Fatal("bad id accepted")
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, vid(1)); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := act(h, vid(91), "delist", "list-delist-1", "duplicate", nil); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := h.pool.QueryRow(context.Background(), `select id::text from venue_reports where venue_id = $1`, vid(91)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.DismissVenueReport(context.Background(), operatorID, id); !errors.Is(err, ErrReportResolved) {
		t.Fatalf("dismiss actioned: %v", err)
	}
}

func TestListReportsDeletedSubmitter(t *testing.T) {
	h := newHarness(t)
	insertUser(t, h.pool, userA, "active", nil)
	insertUser(t, h.pool, userB, "active", nil)
	seedVenue(t, h.pool, seed{id: vid(92), name: "Left", status: "verified", submitter: ptr(userA), lat: 78, lon: 10})
	if _, err := report(h, userB, vid(92), "deleted-key-01", "moved", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(context.Background(), `delete from auth.users where id = $1`, userA); err != nil {
		t.Fatal(err)
	}
	page, err := h.service.ListVenueReports(context.Background(), 5, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Submitter != nil || page.Items[0].Venue.Name != "Left" {
		t.Fatalf("%+v %v", page, err)
	}
}
```

Add `"time"` to the imports. Append to `backend/internal/venue/index_test.go`:

```go
func TestVenueReportIndexes(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	if _, err := pool.Exec(ctx, `insert into auth.users (id) select gen_random_uuid() from generate_series(1, 500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		insert into venues (id, name, location, address, city, country_code, verification_status)
		select gen_random_uuid(), 'Venue ' || i, ST_SetSRID(ST_MakePoint((i % 360) - 179.5, ((i / 360) % 170) - 85), 4326)::geography,
		       i || ' Main Street', 'City', 'DE', 'verified'
		from generate_series(1, 1000) as i`); err != nil {
		t.Fatal(err)
	}
	// Pair (i % 500, (i / 5) % 1000) is unique for i in 0..4999, so the pending-pair index never collides.
	if _, err := pool.Exec(ctx, `
		with users as (select id, row_number() over (order by id) - 1 as n from auth.users),
		     venues_n as (select id, row_number() over (order by id) - 1 as n from venues)
		insert into venue_reports (reporter_user_id, venue_id, reason, created_at)
		select users.id, venues_n.id, 'moved', now() - (i || ' minutes')::interval
		from generate_series(0, 4999) as i
		join users on users.n = i % 500
		join venues_n on venues_n.n = (i / 5) % 1000`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "vacuum analyze venue_reports"); err != nil {
		t.Fatal(err)
	}
	var venueID string
	if err := pool.QueryRow(ctx, `select venue_id::text from venue_reports limit 1`).Scan(&venueID); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, sql, index string
		args             []any
	}{
		{"pending list", reportListSQL, "venue_reports_pending_list_key", []any{nil, nil, 21}},
		{"venue reports", venueReportsSQL, "venue_reports_pending_venue_key", []any{venueID, 100}},
	} {
		rows, err := pool.Query(ctx, "explain "+c.sql, c.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(line + "\n")
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.String(), c.index) {
			t.Fatalf("%s plan does not use %s:\n%s", c.name, c.index, plan.String())
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/venue -run 'TestReport|TestListAndDismiss|TestListReports|TestVenueReportIndexes' -v`
Expected: FAIL, `ReportVenue undefined`.

- [ ] **Step 3: Write `report.go`**

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/metalhops/metalhops/backend/internal/idempotency"
	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/ratelimit"
)

const reportCursorDiscriminator = "venue-report-v1"

const reportSelect = `
	select r.id::text, r.reason, r.note, r.status, r.created_at,
	       v.id::text, v.name, v.city, v.country_code, v.verification_status,
	       ` + "%s" + `
	from venue_reports r
	join venues v on v.id = r.venue_id
	left join profiles rp on rp.id = r.reporter_user_id
	left join profiles sp on sp.id = v.submitted_by`

var reportColumns = fmt.Sprintf(reportSelect, profileColumns("rp")+", "+profileColumns("sp"))

const reportListSQL = `
	select r.id::text, r.reason, r.note, r.status, r.created_at
	from venue_reports r
	where r.status = 'pending'
	  and ($1::timestamptz is null or (r.created_at, r.id) > ($1::timestamptz, $2::uuid))
	order by r.created_at, r.id
	limit $3`

const venueReportsSQL = `
	select r.id::text, r.reason, r.note, r.created_at
	from venue_reports r
	where r.venue_id = $1::uuid and r.status = 'pending'
	order by r.created_at, r.id
	limit $2`

type venueReportCommand struct {
	VenueID string  `json:"venue_id"`
	Reason  string  `json:"reason"`
	Note    *string `json:"note"`
}

type venueReportBody struct {
	ID        string    `json:"id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) ReportVenue(ctx context.Context, cmd ReportCommand) (VenueReport, error) {
	if !CanonicalVenueID(cmd.VenueID) {
		return VenueReport{}, &ValidationError{Fields: map[string]string{"venue_id": "must be a canonical uuid"}}
	}
	reason, note, err := normalizeVenueReport(cmd.Reason, cmd.Note)
	if err != nil {
		return VenueReport{}, err
	}
	if s.profiles == nil || s.writes == nil {
		return VenueReport{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.profiles.NormalizeExpiry(txCtx, cmd.ReporterID); err != nil {
		return VenueReport{}, err
	}
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if err := profile.RequireContributor(txCtx, tx, cmd.ReporterID); err != nil {
		return VenueReport{}, err
	}
	// for share: a concurrent moderation action (for update) either finishes first, so this
	// report sees the new status, or waits for this report and then resolves it.
	var submitter *string
	err = tx.QueryRow(txCtx, `
		select v.submitted_by::text from venues v
		where v.id = $1::uuid
		  and v.verification_status in ('pending', 'verified')
		  and exists (select 1 from venue_genres g where g.venue_id = v.id)
		for share of v`, cmd.VenueID).Scan(&submitter)
	if errors.Is(err, pgx.ErrNoRows) {
		return VenueReport{}, ErrNotFound
	}
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
	}
	if submitter != nil && *submitter == cmd.ReporterID {
		return VenueReport{}, ErrSelfReport
	}
	command, err := json.Marshal(venueReportCommand{VenueID: cmd.VenueID, Reason: reason, Note: note})
	if err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
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
		UserID: cmd.ReporterID, Operation: "venue_report", Key: cmd.Key, Command: command,
	}, func(ctx context.Context) (idempotency.Result, error) {
		if err := profile.CheckReportQuota(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		var pending bool
		if err := tx.QueryRow(ctx, `
			select exists (
				select 1 from venue_reports
				where reporter_user_id = $1::uuid and venue_id = $2::uuid and status = 'pending'
			)`, cmd.ReporterID, cmd.VenueID).Scan(&pending); err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		if pending {
			return idempotency.Result{}, ErrReportPending
		}
		next, retryAfter, ok := s.writes.Reserve(cmd.ReporterID)
		if !ok {
			return idempotency.Result{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		var stored venueReportBody
		err := tx.QueryRow(ctx, `
			insert into venue_reports (reporter_user_id, venue_id, reason, note, created_at)
			values ($1::uuid, $2::uuid, $3, $4, $5)
			returning id::text, venue_id::text, reason, note, status, created_at`,
			cmd.ReporterID, cmd.VenueID, reason, note, now).Scan(
			&stored.ID, &stored.VenueID, &stored.Reason, &stored.Note, &stored.Status, &stored.CreatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "venue_reports_pending_pair_key" {
				return idempotency.Result{}, ErrReportPending
			}
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		stored.CreatedAt = stored.CreatedAt.UTC()
		if err := profile.RecordReportEvent(ctx, tx, cmd.ReporterID, now); err != nil {
			return idempotency.Result{}, err
		}
		body, err := json.Marshal(stored)
		if err != nil {
			return idempotency.Result{}, fmt.Errorf("venue: report: %w", err)
		}
		return idempotency.Result{Status: 201, Body: body}, nil
	})
	if err != nil {
		return VenueReport{}, err
	}
	var stored venueReportBody
	if err := json.Unmarshal(result.Body, &stored); err != nil {
		return VenueReport{}, fmt.Errorf("venue: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return VenueReport{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return VenueReport{ID: stored.ID, VenueID: stored.VenueID, Reason: stored.Reason, Note: stored.Note, Status: stored.Status, CreatedAt: stored.CreatedAt}, nil
}

func scanReportItem(row pgx.Row) (ReportItem, error) {
	var item ReportItem
	var reporter, submitter nullableProfile
	dest := []any{
		&item.ID, &item.Reason, &item.Note, &item.Status, &item.CreatedAt,
		&item.Venue.ID, &item.Venue.Name, &item.Venue.City, &item.Venue.CountryCode, &item.Venue.VerificationStatus,
	}
	dest = append(dest, reporter.dest()...)
	dest = append(dest, submitter.dest()...)
	if err := row.Scan(dest...); err != nil {
		return ReportItem{}, err
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.Reporter = reporter.profile()
	item.Submitter = submitter.profile()
	return item, nil
}

func (s *Service) ListVenueReports(ctx context.Context, limit int, cursor string) (ReportPage, error) {
	if err := ValidateLimit(limit); err != nil {
		return ReportPage{}, err
	}
	var cursorAt *time.Time
	var cursorID *string
	if cursor != "" {
		at, id, err := decodeReportCursor(cursor)
		if err != nil {
			return ReportPage{}, err
		}
		cursorAt, cursorID = &at, &id
	}
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := s.pool.Query(queryCtx, reportColumns+`
		where r.status = 'pending'
		  and ($1::timestamptz is null or (r.created_at, r.id) > ($1::timestamptz, $2::uuid))
		order by r.created_at, r.id
		limit $3`, cursorAt, cursorID, limit+1)
	if err != nil {
		return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
	}
	defer rows.Close()
	items := make([]ReportItem, 0)
	for rows.Next() {
		item, err := scanReportItem(rows)
		if err != nil {
			return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ReportPage{}, fmt.Errorf("venue: reports: %w", err)
	}
	var next *string
	if len(items) > limit {
		items = items[:limit]
		encoded := encodeReportCursor(items[limit-1].CreatedAt, items[limit-1].ID)
		next = &encoded
	}
	return ReportPage{Items: items, NextCursor: next}, nil
}

func (s *Service) DismissVenueReport(ctx context.Context, actorID, reportID string) (ReportItem, error) {
	if !CanonicalVenueID(reportID) {
		return ReportItem{}, &ValidationError{Fields: map[string]string{"report_id": "must be a canonical uuid"}}
	}
	if s.writes == nil {
		return ReportItem{}, errWritesUnavailable
	}
	txCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(txCtx)
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var status string
	err = tx.QueryRow(txCtx, `select status from venue_reports where id = $1::uuid for update`, reportID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ReportItem{}, ErrReportNotFound
	}
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: report: %w", err)
	}
	if status == "actioned" {
		return ReportItem{}, ErrReportResolved
	}
	var reservation *ratelimit.Reservation
	committed := false
	defer func() {
		if reservation != nil && !committed {
			reservation.Cancel()
		}
	}()
	if status == "pending" {
		next, retryAfter, ok := s.writes.Reserve(actorID)
		if !ok {
			return ReportItem{}, &RateLimitError{RetryAfter: retryAfter}
		}
		reservation = next
		if _, err := tx.Exec(txCtx, `
			update venue_reports set status = 'dismissed', resolved_at = $2, resolved_by_user_id = $3::uuid
			where id = $1::uuid and status = 'pending'`, reportID, s.clock(), actorID); err != nil {
			return ReportItem{}, fmt.Errorf("venue: report: %w", err)
		}
	}
	item, err := scanReportItem(tx.QueryRow(txCtx, reportColumns+` where r.id = $1::uuid`, reportID))
	if err != nil {
		return ReportItem{}, fmt.Errorf("venue: report: %w", err)
	}
	if err := tx.Commit(txCtx); err != nil {
		return ReportItem{}, fmt.Errorf("venue: commit: %w", err)
	}
	if reservation != nil {
		reservation.Commit()
		committed = true
	}
	return item, nil
}

func encodeReportCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(reportCursorDiscriminator + "|" + createdAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeReportCursor(cursor string) (time.Time, string, error) {
	invalid := &ValidationError{Fields: map[string]string{"cursor": "invalid"}}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", invalid
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != reportCursorDiscriminator || !CanonicalVenueID(parts[2]) {
		return time.Time{}, "", invalid
	}
	at, err := time.Parse(time.RFC3339Nano, parts[1])
	if err != nil {
		return time.Time{}, "", invalid
	}
	return at.UTC(), parts[2], nil
}
```

Two cleanups the compiler will force. First, `reportSelect` and `reportColumns` as written build the select with a format placeholder; simplify to a plain string by writing `const reportBase = \`select ... from ... left join profiles sp on sp.id = v.submitted_by\`` with a literal column list and `var reportColumns = ...` composed with `profileColumns("rp")` and `profileColumns("sp")` through ordinary string concatenation. Second, `reportListSQL` selects only five columns because it exists so `TestVenueReportIndexes` can explain the index-backed core of the list. Keep the real list query (`reportColumns + where ...`) and make `reportListSQL` the same `where`/`order by`/`limit` over `venue_reports r` alone, which is what the pending-list index serves. If the explain on the joined query also uses the index, point the test at the joined query instead and delete `reportListSQL`.

`DismissVenueReport` validates `reportID` with `CanonicalVenueID`, which is only a canonical-UUID regex. It is reused on purpose; do not add a second copy.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/venue -run 'TestReport|TestListAndDismiss|TestListReports|TestVenueReportIndexes' -v`
Expected: PASS. Then `go test ./internal/venue ./internal/profile`.

- [ ] **Step 5: Commit**

```bash
git add internal/venue
git commit -m "feat: report venues and review reports as an operator

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 8: HTTP routes

**Files:**
- Create: `backend/internal/api/venue_moderation.go`, `backend/internal/api/venue_moderation_test.go`
- Modify: `backend/internal/api/server.go` (`Options`, routes), `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: every service method from Tasks 5 to 7; `requireApp`, `requireOperator`, `DecodeJSON`, `writeDecodeError`, `optionalString`, `dismissRequest` (`moderation.go`), `submissionRequest.raw()`, `submissionView`, `moderationProfile`, `parseVenueLimit`, `rejectedQuery`, `writeSubmissionError`.
- Produces:

```go
type VenueModeration interface {
	ReportVenue(context.Context, venue.ReportCommand) (venue.VenueReport, error)
	ListVenueReports(context.Context, int, string) (venue.ReportPage, error)
	GetModerationVenue(context.Context, string) (venue.ModerationVenue, error)
	ModerateVenue(context.Context, venue.ModerationCommand) (venue.ModerationResult, error)
	CorrectVenue(context.Context, venue.OperatorCorrectCommand) (venue.Moderated, error)
	DismissVenueReport(context.Context, string, string) (venue.ReportItem, error)
}
// Options.VenueModeration VenueModeration
```

Routes: `POST /v1/venues/{venueID}/reports` (app), `GET /v1/moderation/venue-reports`, `GET /v1/moderation/venues/{venueID}`, `POST /v1/moderation/venues/{venueID}/actions`, `PUT /v1/moderation/venues/{venueID}`, `PATCH /v1/moderation/venue-reports/{reportID}` (operator).

- [ ] **Step 1: Write the failing HTTP tests**

Create `backend/internal/api/venue_moderation_test.go`:

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

type fakeVenueModeration struct {
	report    venue.VenueReport
	page      venue.ReportPage
	detail    venue.ModerationVenue
	result    venue.ModerationResult
	moderated venue.Moderated
	item      venue.ReportItem
	err       error
	calls     int

	reportCmd    venue.ReportCommand
	moderateCmd  venue.ModerationCommand
	correctCmd   venue.OperatorCorrectCommand
	limit        int
	cursor       string
	detailID     string
	dismissActor string
	dismissID    string
}

func (f *fakeVenueModeration) ReportVenue(_ context.Context, c venue.ReportCommand) (venue.VenueReport, error) {
	f.calls++
	f.reportCmd = c
	return f.report, f.err
}
func (f *fakeVenueModeration) ListVenueReports(_ context.Context, limit int, cursor string) (venue.ReportPage, error) {
	f.calls++
	f.limit, f.cursor = limit, cursor
	return f.page, f.err
}
func (f *fakeVenueModeration) GetModerationVenue(_ context.Context, id string) (venue.ModerationVenue, error) {
	f.calls++
	f.detailID = id
	return f.detail, f.err
}
func (f *fakeVenueModeration) ModerateVenue(_ context.Context, c venue.ModerationCommand) (venue.ModerationResult, error) {
	f.calls++
	f.moderateCmd = c
	return f.result, f.err
}
func (f *fakeVenueModeration) CorrectVenue(_ context.Context, c venue.OperatorCorrectCommand) (venue.Moderated, error) {
	f.calls++
	f.correctCmd = c
	return f.moderated, f.err
}
func (f *fakeVenueModeration) DismissVenueReport(_ context.Context, actor, id string) (venue.ReportItem, error) {
	f.calls++
	f.dismissActor, f.dismissID = actor, id
	return f.item, f.err
}

const (
	venueUUID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	reportUUID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

func moderationVenueHandler(authenticator *fakeAuth, fake *fakeVenueModeration) http.Handler {
	return New(Options{
		Readiness: readyOK(), PublicLimit: allowAll, Auth: authenticator, VenueModeration: fake,
		Logger: slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
	})
}

func TestVenueReportRoute(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	fake := &fakeVenueModeration{report: venue.VenueReport{ID: reportUUID, VenueID: venueUUID, Reason: "moved", Status: "pending", CreatedAt: at}}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}}
	handler := moderationVenueHandler(authenticator, fake)
	path := "/v1/venues/" + venueUUID + "/reports"
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "report-key-1"}

	rec := requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed)
	if rec.Code != http.StatusCreated || fake.reportCmd.Key != "report-key-1" || fake.reportCmd.VenueID != venueUUID || fake.reportCmd.Reason != "moved" {
		t.Fatalf("create %d %s %+v", rec.Code, rec.Body.String(), fake.reportCmd)
	}
	for _, want := range []string{`"venue_id":"` + venueUUID + `"`, `"status":"pending"`, `"created_at":"2026-10-20T12:00:00Z"`} {
		if !bytes.Contains(rec.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, rec.Body.String())
		}
	}
	before := fake.calls
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), map[string]string{"Content-Type": "application/json"}), http.StatusBadRequest, "validation_error")
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`not json`), keyed), http.StatusBadRequest, "validation_error")
	if fake.calls != before {
		t.Fatal("invalid requests reached the service")
	}
	authenticator.operator = true
	assertCode(t, requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed), http.StatusForbidden, "forbidden")
	authenticator.operator = false

	for _, c := range []struct {
		err    error
		status int
		code   string
	}{
		{venue.ErrNotFound, 404, "venue_not_found"},
		{venue.ErrSelfReport, 400, "self_report"},
		{venue.ErrReportPending, 409, "venue_already_reported"},
		{&profile.QuotaError{RetryAfter: 30}, 429, "quota_exceeded"},
		{&venue.RateLimitError{RetryAfter: 3}, 429, "rate_limited"},
		{&venue.ValidationError{Fields: map[string]string{"note": "required"}}, 400, "validation_error"},
		{idempotency.ErrReused, 409, "idempotency_key_reused"},
		{profile.ErrProfileRequired, 409, "profile_required"},
		{errors.New("boom"), 500, "internal_error"},
	} {
		fake.err = c.err
		rec := requestWith(handler, http.MethodPost, path, "Bearer t", []byte(`{"reason":"moved"}`), keyed)
		assertCode(t, rec, c.status, c.code)
		if c.status == 429 && rec.Header().Get("Retry-After") == "" {
			t.Fatalf("%s: no Retry-After", c.code)
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
			t.Fatal("leaked internal error")
		}
	}
}

func TestVenueModerationReads(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reporter := &profile.Profile{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", Username: "rep", DisplayName: "Rep", Status: "active", CreatedAt: at, UpdatedAt: at}
	cursor := "next"
	fake := &fakeVenueModeration{
		page: venue.ReportPage{Items: []venue.ReportItem{{
			ID: reportUUID, Reason: "moved", Status: "pending", CreatedAt: at, Reporter: reporter,
			Venue: venue.VenueSummary{ID: venueUUID, Name: "Iron", City: "Oslo", CountryCode: "NO", VerificationStatus: "verified"},
		}}, NextCursor: &cursor},
		detail: venue.ModerationVenue{
			Moderated: venue.Moderated{Submission: sampleSubmission()},
			Reports:   []venue.PendingReport{{ID: reportUUID, Reason: "moved", CreatedAt: at, Reporter: reporter}},
		},
	}
	authenticator := &fakeAuth{caller: auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}}
	handler := moderationVenueHandler(authenticator, fake)
	paths := []string{"/v1/moderation/venue-reports", "/v1/moderation/venues/" + venueUUID}

	for _, p := range paths {
		rec := authed(handler, http.MethodGet, p, "", nil)
		assertCode(t, rec, http.StatusForbidden, "forbidden")
	}
	if fake.calls != 0 {
		t.Fatal("app user reached operator reads")
	}
	authenticator.operator = true

	list := authed(handler, http.MethodGet, paths[0], "", nil)
	if list.Code != http.StatusOK || fake.limit != 20 || fake.cursor != "" {
		t.Fatalf("list %d limit %d %s", list.Code, fake.limit, list.Body.String())
	}
	for _, want := range []string{`"next_cursor":"next"`, `"reporter":{`, `"submitter":null`, `"venue":{"id":"` + venueUUID + `"`} {
		if !bytes.Contains(list.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, list.Body.String())
		}
	}
	authed(handler, http.MethodGet, paths[0]+"?limit=5&cursor=abc", "", nil)
	if fake.limit != 5 || fake.cursor != "abc" {
		t.Fatalf("params %d %q", fake.limit, fake.cursor)
	}
	assertCode(t, authed(handler, http.MethodGet, paths[0]+"?limit=x", "", nil), http.StatusBadRequest, "validation_error")

	detail := authed(handler, http.MethodGet, paths[1], "", nil)
	if detail.Code != http.StatusOK || fake.detailID != venueUUID {
		t.Fatalf("detail %d %s", detail.Code, detail.Body.String())
	}
	for _, want := range []string{`"delisting_reason":null`, `"rejection_reason":null`, `"submitter":null`, `"reports":[{`} {
		if !bytes.Contains(detail.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, detail.Body.String())
		}
	}
	fake.err = venue.ErrNotFound
	assertCode(t, authed(handler, http.MethodGet, paths[1], "", nil), http.StatusNotFound, "venue_not_found")
}

func TestVenueModerationWrites(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	actionID := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	moderated := venue.Moderated{Submission: sampleSubmission()}
	moderated.VerificationStatus = "delisted"
	reason := "permanently_closed"
	moderated.DelistingReason = &reason
	fake := &fakeVenueModeration{
		result:    venue.ModerationResult{Created: true, ActionID: &actionID, Venue: moderated},
		moderated: moderated,
		item:      venue.ReportItem{ID: reportUUID, Reason: "moved", Status: "dismissed", CreatedAt: at, Venue: venue.VenueSummary{ID: venueUUID}},
	}
	caller := auth.Caller{UserID: "dddddddd-dddd-4ddd-8ddd-dddddddddddd"}
	authenticator := &fakeAuth{caller: caller}
	handler := moderationVenueHandler(authenticator, fake)
	actionPath := "/v1/moderation/venues/" + venueUUID + "/actions"
	putPath := "/v1/moderation/venues/" + venueUUID
	patchPath := "/v1/moderation/venue-reports/" + reportUUID
	keyed := map[string]string{"Content-Type": "application/json", "Idempotency-Key": "action-key-1"}
	plain := map[string]string{"Content-Type": "application/json"}

	for _, c := range []struct {
		method, path, body string
	}{
		{http.MethodPost, actionPath, `{"action":"verify"}`},
		{http.MethodPut, putPath, submissionBodyJSON},
		{http.MethodPatch, patchPath, `{"status":"dismissed"}`},
	} {
		assertCode(t, requestWith(handler, c.method, c.path, "Bearer t", []byte(c.body), keyed), http.StatusForbidden, "forbidden")
	}
	if fake.calls != 0 {
		t.Fatal("app user reached operator writes")
	}
	authenticator.operator = true

	created := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"delist","reason":"permanently_closed","note":"gone"}`), keyed)
	if created.Code != http.StatusCreated || fake.moderateCmd.ActorID != caller.UserID || fake.moderateCmd.VenueID != venueUUID ||
		fake.moderateCmd.Action != "delist" || !fake.moderateCmd.ReasonSet || fake.moderateCmd.Reason != "permanently_closed" ||
		fake.moderateCmd.Note == nil || *fake.moderateCmd.Note != "gone" || fake.moderateCmd.Key != "action-key-1" {
		t.Fatalf("action %d %s %+v", created.Code, created.Body.String(), fake.moderateCmd)
	}
	for _, want := range []string{`"action_id":"` + actionID + `"`, `"delisting_reason":"permanently_closed"`, `"verification_status":"delisted"`} {
		if !bytes.Contains(created.Body.Bytes(), []byte(want)) {
			t.Fatalf("missing %s in %s", want, created.Body.String())
		}
	}
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"verify"}`), plain), http.StatusBadRequest, "validation_error")
	fake.result.Created, fake.result.ActionID = false, nil
	noop := requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"delist","reason":"permanently_closed"}`), keyed)
	if noop.Code != http.StatusOK || !bytes.Contains(noop.Body.Bytes(), []byte(`"action_id":null`)) {
		t.Fatalf("no-op %d %s", noop.Code, noop.Body.String())
	}
	fake.err = &venue.InvalidTransitionError{Action: "verify", Status: "rejected"}
	assertCode(t, requestWith(handler, http.MethodPost, actionPath, "Bearer t", []byte(`{"action":"verify"}`), keyed), http.StatusConflict, "invalid_venue_transition")
	fake.err = nil

	put := requestWith(handler, http.MethodPut, putPath, "Bearer t", []byte(submissionBodyJSON), plain)
	if put.Code != http.StatusOK || fake.correctCmd.VenueID != venueUUID || fake.correctCmd.ActorID != caller.UserID || fake.correctCmd.Fields.Name == nil {
		t.Fatalf("put %d %s %+v", put.Code, put.Body.String(), fake.correctCmd)
	}

	patch := requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain)
	if patch.Code != http.StatusOK || fake.dismissID != reportUUID || fake.dismissActor != caller.UserID {
		t.Fatalf("patch %d %s", patch.Code, patch.Body.String())
	}
	assertCode(t, requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"actioned"}`), plain), http.StatusBadRequest, "validation_error")
	for _, c := range []struct {
		err          error
		status       int
		code         string
	}{
		{venue.ErrReportNotFound, 404, "venue_report_not_found"},
		{venue.ErrReportResolved, 409, "report_already_resolved"},
	} {
		fake.err = c.err
		assertCode(t, requestWith(handler, http.MethodPatch, patchPath, "Bearer t", []byte(`{"status":"dismissed"}`), plain), c.status, c.code)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/api -run 'TestVenueReportRoute|TestVenueModeration' -v`
Expected: FAIL, `unknown field VenueModeration`.

- [ ] **Step 3: Write `venue_moderation.go`**

```go
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/metalhops/metalhops/backend/internal/profile"
	"github.com/metalhops/metalhops/backend/internal/venue"
)

type VenueModeration interface {
	ReportVenue(context.Context, venue.ReportCommand) (venue.VenueReport, error)
	ListVenueReports(context.Context, int, string) (venue.ReportPage, error)
	GetModerationVenue(context.Context, string) (venue.ModerationVenue, error)
	ModerateVenue(context.Context, venue.ModerationCommand) (venue.ModerationResult, error)
	CorrectVenue(context.Context, venue.OperatorCorrectCommand) (venue.Moderated, error)
	DismissVenueReport(context.Context, string, string) (venue.ReportItem, error)
}

type venueReportRequest struct {
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

type venueReportView struct {
	ID        string    `json:"id"`
	VenueID   string    `json:"venue_id"`
	Reason    string    `json:"reason"`
	Note      *string   `json:"note"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type venueSummaryView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	City               string `json:"city"`
	CountryCode        string `json:"country_code"`
	VerificationStatus string `json:"verification_status"`
}

type venueReportItemView struct {
	ID        string           `json:"id"`
	Reason    string           `json:"reason"`
	Note      *string          `json:"note"`
	Status    string           `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	Reporter  *profile.Profile `json:"reporter"`
	Venue     venueSummaryView `json:"venue"`
	Submitter *profile.Profile `json:"submitter"`
}

type pendingReportView struct {
	ID        string           `json:"id"`
	Reason    string           `json:"reason"`
	Note      *string          `json:"note"`
	CreatedAt time.Time        `json:"created_at"`
	Reporter  *profile.Profile `json:"reporter"`
}

type moderatedView struct {
	submissionView
	DelistingReason *string `json:"delisting_reason"`
	DelistingNote   *string `json:"delisting_note"`
}

type moderationVenueView struct {
	moderatedView
	Submitter *profile.Profile    `json:"submitter"`
	Reports   []pendingReportView `json:"reports"`
}

type venueActionView struct {
	ActionID *string       `json:"action_id"`
	Venue    moderatedView `json:"venue"`
}

type venueActionRequest struct {
	Action optionalString `json:"action"`
	Reason optionalString `json:"reason"`
	Note   optionalString `json:"note"`
}

func profilePtr(p *profile.Profile) *profile.Profile {
	if p == nil {
		return nil
	}
	got := moderationProfile(*p)
	return &got
}

func moderatedJSON(m venue.Moderated) moderatedView {
	return moderatedView{submissionView: submissionJSON(m.Submission), DelistingReason: m.DelistingReason, DelistingNote: m.DelistingNote}
}

func reportItemJSON(item venue.ReportItem) venueReportItemView {
	return venueReportItemView{
		ID: item.ID, Reason: item.Reason, Note: item.Note, Status: item.Status, CreatedAt: item.CreatedAt.UTC(),
		Reporter: profilePtr(item.Reporter), Submitter: profilePtr(item.Submitter),
		Venue: venueSummaryView{
			ID: item.Venue.ID, Name: item.Venue.Name, City: item.Venue.City,
			CountryCode: item.Venue.CountryCode, VerificationStatus: item.Venue.VerificationStatus,
		},
	}
}

func missingKey(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Idempotency-Key") != "" {
		return false
	}
	WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
		"idempotency_key": "required",
	})
	return true
}

func reportVenue(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req venueReportRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := venue.ReportCommand{ReporterID: caller.UserID, VenueID: r.PathValue("venueID"), Key: r.Header.Get("Idempotency-Key")}
	if req.Reason.Set && !req.Reason.Null {
		cmd.Reason = req.Reason.Value
	}
	if req.Note.Set && !req.Note.Null {
		note := req.Note.Value
		cmd.Note = &note
	}
	got, err := moderation.ReportVenue(r.Context(), cmd)
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusCreated, venueReportView{
		ID: got.ID, VenueID: got.VenueID, Reason: got.Reason, Note: got.Note, Status: got.Status, CreatedAt: got.CreatedAt.UTC(),
	})
}

func listVenueReports(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	limit, err := parseReportLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeVenueModerationError(logger, w, r, &venue.ValidationError{Fields: map[string]string{"limit": "must be from 1 to 100"}})
		return
	}
	page, err := moderation.ListVenueReports(r.Context(), limit, r.URL.Query().Get("cursor"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	items := make([]venueReportItemView, 0, len(page.Items))
	for _, item := range page.Items {
		items = append(items, reportItemJSON(item))
	}
	WriteList(w, http.StatusOK, items, page.NextCursor)
}

func getModerationVenue(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	got, err := moderation.GetModerationVenue(r.Context(), r.PathValue("venueID"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	reports := make([]pendingReportView, 0, len(got.Reports))
	for _, item := range got.Reports {
		reports = append(reports, pendingReportView{
			ID: item.ID, Reason: item.Reason, Note: item.Note, CreatedAt: item.CreatedAt.UTC(), Reporter: profilePtr(item.Reporter),
		})
	}
	WriteData(w, http.StatusOK, moderationVenueView{moderatedView: moderatedJSON(got.Moderated), Submitter: profilePtr(got.Submitter), Reports: reports})
}

func applyVenueAction(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req venueActionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if missingKey(w, r) {
		return
	}
	cmd := venue.ModerationCommand{
		ActorID: caller.UserID, VenueID: r.PathValue("venueID"), Action: req.Action.Value, Key: r.Header.Get("Idempotency-Key"),
	}
	if req.Reason.Set {
		cmd.ReasonSet = true
		if !req.Reason.Null {
			cmd.Reason = req.Reason.Value
		}
	}
	if req.Note.Set {
		cmd.NoteSet = true
		if !req.Note.Null {
			note := req.Note.Value
			cmd.Note = &note
		}
	}
	result, err := moderation.ModerateVenue(r.Context(), cmd)
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	WriteData(w, status, venueActionView{ActionID: result.ActionID, Venue: moderatedJSON(result.Venue)})
}

func correctVenueAsOperator(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req submissionRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	got, err := moderation.CorrectVenue(r.Context(), venue.OperatorCorrectCommand{
		ActorID: caller.UserID, VenueID: r.PathValue("venueID"), Fields: req.raw(),
	})
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, moderatedJSON(got))
}

func dismissVenueReport(w http.ResponseWriter, r *http.Request, moderation VenueModeration, logger *slog.Logger) {
	caller, ok := CallerFrom(r.Context())
	if !ok {
		writeVenueModerationError(logger, w, r, errors.New("missing caller"))
		return
	}
	var req dismissRequest
	if err := DecodeJSON(w, r, &req); err != nil {
		writeDecodeError(w, r, err)
		return
	}
	if !req.Status.Set || req.Status.Null || req.Status.Value != "dismissed" {
		WriteError(w, http.StatusBadRequest, "validation_error", "The request is invalid.", requestID(r.Context()), map[string]string{
			"status": "must be dismissed",
		})
		return
	}
	item, err := moderation.DismissVenueReport(r.Context(), caller.UserID, r.PathValue("reportID"))
	if err != nil {
		writeVenueModerationError(logger, w, r, err)
		return
	}
	WriteData(w, http.StatusOK, reportItemJSON(item))
}

func writeVenueModerationError(logger *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	id := requestID(r.Context())
	var transition *venue.InvalidTransitionError
	var quota *profile.QuotaError
	switch {
	case errors.Is(err, venue.ErrSelfReport):
		WriteError(w, http.StatusBadRequest, "self_report", "You cannot report your own venue.", id, nil)
	case errors.Is(err, venue.ErrReportPending):
		WriteError(w, http.StatusConflict, "venue_already_reported", "You already reported this venue.", id, nil)
	case errors.Is(err, venue.ErrReportNotFound):
		WriteError(w, http.StatusNotFound, "venue_report_not_found", "Report not found.", id, nil)
	case errors.Is(err, venue.ErrReportResolved):
		WriteError(w, http.StatusConflict, "report_already_resolved", "That report is already resolved.", id, nil)
	case errors.As(err, &transition):
		WriteError(w, http.StatusConflict, "invalid_venue_transition", "This action is not available for a venue with that status.", id, nil)
	case errors.As(err, &quota):
		w.Header().Set("Retry-After", strconv.Itoa(quota.RetryAfter))
		WriteError(w, http.StatusTooManyRequests, "quota_exceeded", "The report limit has been reached.", id, nil)
	default:
		writeSubmissionError(logger, w, r, err)
	}
}
```

- [ ] **Step 4: Register the routes**

In `backend/internal/api/server.go` add `VenueModeration VenueModeration` to `Options`, and before `return wrap(...)`:

```go
	mux.Handle("POST /v1/venues/{venueID}/reports", requireApp(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reportVenue(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/venue-reports", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		listVenueReports(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("GET /v1/moderation/venues/{venueID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		getModerationVenue(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("POST /v1/moderation/venues/{venueID}/actions", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		applyVenueAction(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("PUT /v1/moderation/venues/{venueID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correctVenueAsOperator(w, r, options.VenueModeration, options.Logger)
	})))
	mux.Handle("PATCH /v1/moderation/venue-reports/{reportID}", requireOperator(options.Auth, options.Logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dismissVenueReport(w, r, options.VenueModeration, options.Logger)
	})))
```

In `backend/cmd/api/main.go` add `VenueModeration: venues,` after `Submissions: venues,`.

- [ ] **Step 5: Run the API suite and the whole backend**

Run: `go test ./internal/api -v 2>&1 | tail -30` then `go build ./... && go vet ./...`
Expected: PASS. `*venue.Service` must satisfy `VenueModeration`; `go build ./cmd/api` proves it. `parseReportLimit` returns a `*profile.ValidationError`, which `listVenueReports` replaces with a venue one so the mapping stays in one place.

- [ ] **Step 6: Commit**

```bash
git add internal/api cmd/api
git commit -m "feat: route venue reports and moderation

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

### Task 9: Contract, guides, and final verification

**Files:**
- Modify: `backend/openapi.yaml`, `backend/contract_test.go`, `backend/AGENTS.md`, `DevSteps.md`

**Interfaces:**
- Consumes: the six routes and views from Task 8, `Submission` rejection fields from Task 4.
- Produces: the documented contract that `contract_test.go` checks.

- [ ] **Step 1: Extend the contract test (failing)**

In `backend/contract_test.go`, add to the `want` list:

```go
		"/v1/venues/{venueID}/reports",
		"/v1/moderation/venue-reports",
		"/v1/moderation/venue-reports/{reportID}",
		"/v1/moderation/venues/{venueID}",
		"/v1/moderation/venues/{venueID}/actions",
		"venue_report",
		"venue_moderation_action",
		"venue_already_reported",
		"venue_report_not_found",
		"invalid_venue_transition",
		"rejection_reason",
		"delisting_reason",
		"delisted",
		"The report limit has been reached.",
```

and add the new operation ids to the bearer-security loop: `"reportVenue", "listVenueReports", "getModerationVenue", "applyVenueAction", "correctVenueAsOperator", "dismissVenueReport"`.

Run: `go test . -run TestOpenAPIDocument -v`. Expected: FAIL, missing `"/v1/venues/{venueID}/reports"`.

- [ ] **Step 2: Document the routes**

In `backend/openapi.yaml`, copy the structure of `/v1/profiles/{username}/reports` (line ~774), `/v1/moderation/profile-reports` (~942), `/v1/moderation/profile-reports/{reportID}` (~1032), `/v1/moderation/profiles/{userID}` (~1149), and `/v1/moderation/profiles/{userID}/actions` (~1225), and add these paths after `/v1/moderation/profiles/{userID}/actions`. Reuse the existing `components/responses` (`Unauthorized`, `ContributionForbidden`, `RateLimited`, `Internal`, `BodyTooLarge`, `UnsupportedMedia`, `AuthProvider`), the operator `403` inline response used by the profile moderation paths, and the UUID `pattern` used by `venueID` at `/v1/me/venues/{venueID}`.

| Path | Method | `operationId` | Success | Errors to document |
| --- | --- | --- | --- | --- |
| `/v1/venues/{venueID}/reports` | `post` | `reportVenue` | `201 VenueReportResponse` | `400` (`validation_error`, `self_report`), `401`, `403` (contribution), `404 venue_not_found`, `409` (`venue_already_reported`, `profile_required`, `idempotency_key_reused`), `413`, `415`, `429` (`quota_exceeded` message `The report limit has been reached.`, `rate_limited`), `500` |
| `/v1/moderation/venue-reports` | `get` | `listVenueReports` | `200 VenueReportItemListResponse` | `400`, `401`, `403` operator, `429`, `500` |
| `/v1/moderation/venue-reports/{reportID}` | `patch` | `dismissVenueReport` | `200 VenueReportItemResponse` | `400`, `401`, `403`, `404 venue_report_not_found`, `409 report_already_resolved`, `413`, `415`, `429`, `500` |
| `/v1/moderation/venues/{venueID}` | `get` | `getModerationVenue` | `200 ModerationVenueResponse` | `400`, `401`, `403`, `404 venue_not_found`, `429`, `500` |
| `/v1/moderation/venues/{venueID}` | `put` | `correctVenueAsOperator` | `200 ModeratedVenueResponse` | `400`, `401`, `403`, `404`, `409` (`invalid_venue_transition`, `duplicate_venue`), `413`, `415`, `429`, `500` |
| `/v1/moderation/venues/{venueID}/actions` | `post` | `applyVenueAction` | `201` or `200 VenueActionResponse` | `400`, `401`, `403`, `404`, `409` (`invalid_venue_transition`, `idempotency_key_reused`), `413`, `415`, `429`, `500` |

Every operation has `security: - bearerAuth: []`. `reportVenue` and `applyVenueAction` carry the `Idempotency-Key` header parameter (8 to 128 printable ASCII characters; operations `venue_report` and `venue_moderation_action`). Say in the `applyVenueAction` description: an action on a venue already in the target state returns `200` with `action_id: null`, writes nothing, and spends no write token; a change returns `201`, appends one audit row, and marks the venue's pending reports `actioned`. Allowed transitions: `verify` from `pending`, `reject` from `pending`, `delist` from `verified`, `reopen` from `delisted`. Say in `correctVenueAsOperator`: applies to `pending` or `verified` venues, never changes status, appends an audit row with equal prior and new status, and marks pending reports `actioned`.

Add schemas under `components/schemas` (all `additionalProperties: false`):

```yaml
    VenueReportRequest:
      type: object
      additionalProperties: false
      required: [reason]
      properties:
        reason:
          type: string
          enum: [permanently_closed, moved, duplicate, incorrect_details, no_longer_eligible, other]
        note:
          type: [string, "null"]
          minLength: 1
          maxLength: 1000
          description: Required when the reason is incorrect_details or other.
    VenueReport:
      type: object
      additionalProperties: false
      required: [id, venue_id, reason, note, status, created_at]
      properties:
        id: { type: string, format: uuid }
        venue_id: { type: string, format: uuid }
        reason: { type: string }
        note: { type: [string, "null"] }
        status: { type: string, enum: [pending] }
        created_at: { type: string, format: date-time }
    VenueReportResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data: { $ref: "#/components/schemas/VenueReport" }
    VenueSummary:
      type: object
      additionalProperties: false
      required: [id, name, city, country_code, verification_status]
      properties:
        id: { type: string, format: uuid }
        name: { type: string }
        city: { type: string }
        country_code: { type: string, pattern: "^[A-Z]{2}$" }
        verification_status: { type: string, enum: [pending, verified, rejected, delisted] }
    VenueReportItem:
      type: object
      additionalProperties: false
      required: [id, reason, note, status, created_at, reporter, venue, submitter]
      properties:
        id: { type: string, format: uuid }
        reason: { type: string }
        note: { type: [string, "null"] }
        status: { type: string, enum: [pending, actioned, dismissed] }
        created_at: { type: string, format: date-time }
        reporter:
          oneOf: [{ $ref: "#/components/schemas/ModerationProfile" }, { type: "null" }]
        venue: { $ref: "#/components/schemas/VenueSummary" }
        submitter:
          oneOf: [{ $ref: "#/components/schemas/ModerationProfile" }, { type: "null" }]
    VenueReportItemResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data: { $ref: "#/components/schemas/VenueReportItem" }
    VenueReportItemListResponse:
      type: object
      additionalProperties: false
      required: [data, meta]
      properties:
        data:
          type: array
          items: { $ref: "#/components/schemas/VenueReportItem" }
        meta:
          type: object
          additionalProperties: false
          required: [next_cursor]
          properties:
            next_cursor: { type: [string, "null"] }
    ModeratedVenue:
      allOf:
        - $ref: "#/components/schemas/Submission"
        - type: object
          required: [delisting_reason, delisting_note]
          properties:
            delisting_reason: { type: [string, "null"], enum: [permanently_closed, duplicate, no_longer_eligible, other, null] }
            delisting_note: { type: [string, "null"] }
    ModeratedVenueResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data: { $ref: "#/components/schemas/ModeratedVenue" }
    VenueActionRequest:
      type: object
      additionalProperties: false
      required: [action]
      properties:
        action: { type: string, enum: [verify, reject, delist, reopen] }
        reason:
          type: string
          description: Required for reject (a rejection reason) and delist (a delisting reason). Omit for verify and reopen.
        note:
          type: [string, "null"]
          minLength: 1
          maxLength: 1000
          description: Required when the reason is other.
    VenueActionResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data:
          type: object
          additionalProperties: false
          required: [action_id, venue]
          properties:
            action_id: { type: [string, "null"], format: uuid }
            venue: { $ref: "#/components/schemas/ModeratedVenue" }
    ModerationVenueResponse:
      type: object
      additionalProperties: false
      required: [data]
      properties:
        data:
          allOf:
            - $ref: "#/components/schemas/ModeratedVenue"
            - type: object
              required: [submitter, reports]
              properties:
                submitter:
                  oneOf: [{ $ref: "#/components/schemas/ModerationProfile" }, { type: "null" }]
                reports:
                  type: array
                  description: Pending reports, oldest first, at most 100.
                  items:
                    type: object
                    additionalProperties: false
                    required: [id, reason, note, created_at, reporter]
                    properties:
                      id: { type: string, format: uuid }
                      reason: { type: string }
                      note: { type: [string, "null"] }
                      created_at: { type: string, format: date-time }
                      reporter:
                        oneOf: [{ $ref: "#/components/schemas/ModerationProfile" }, { type: "null" }]
```

Before pasting, run `grep -n "ModerationProfile\|ModerationReportList" backend/openapi.yaml` and use the actual name of the full-Profile schema that `ModerationReportList` already references; rename `ModerationProfile` above to match. Because `ModeratedVenue` uses `allOf` with a `Submission` that is `additionalProperties: false`, replace that wrapper by listing `delisting_reason` and `delisting_note` directly in a standalone `ModeratedVenue` object that copies the `Submission` properties, if the validator rejects the `allOf`; do the same for the detail schema.

In the existing `Submission` schema (line ~1521) make three edits: add `rejection_reason` and `rejection_note` (both `type: [string, "null"]`) to `properties` and to `required`; change the `verification_status` enum to `[pending, verified, rejected, delisted]`; and in `correctMyVenue` replace the two `409` examples so `not_pending` is the only `venue_not_pending` example (delete the `rejected` example, and change the response description to "the Venue is verified or delisted").

- [ ] **Step 3: Run the contract test**

Run: `go test . -v`
Expected: PASS.

- [ ] **Step 4: Update the guides**

`backend/AGENTS.md`: change readiness `0007` to `0008` (line 42); change the `internal/venue` line to "Genre list, public catalogue, discovery filters, sort modes, filter-bound cursors, Venue submissions, Venue reports, and operator moderation (verify, reject, correct, delist, reopen, and the append-only audit)"; in the auth-model route list, add `POST /v1/venues/{venueID}/reports` to the app routes and the five `/v1/moderation/venue...` routes to the operator routes (read the list with `grep -n "moderation/profile" backend/AGENTS.md` and follow its format).

`DevSteps.md`: line 6 becomes `**Status:** Sessions 1–10 are done. Session 11 is next. Sessions 12–14 are not started.`; in the sequence table change the last row to `| 10. Venue safety and moderation API | Done |` and `| 11–14 | Not started |`; in the Session 10 section change `**Status:** Not started.` to `**Status:** Done.` and append one paragraph naming the six routes, `operator recognition by platform_operators through requireOperator`, migration `0008`, the combined report quota, and that the Review Report effect of delisting moves to Session 12.

- [ ] **Step 5: Final verification**

Run from `backend/`:

```bash
gofmt -l .
go vet ./...
go test ./...
```

Expected: `gofmt -l .` prints nothing, vet is clean, all tests pass (Docker must be running; otherwise the `pgtest` tests skip locally, so confirm with `CI=1 go test ./...` if they report SKIP).

- [ ] **Step 6: Commit**

```bash
git add openapi.yaml contract_test.go AGENTS.md ../DevSteps.md
git commit -m "docs: publish the venue safety contract

Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage.**
- Migration `0008`, status, fields, reports, audit, RLS, readiness: Task 1.
- Shared report quota (20, combined): Task 2, used in Task 7.
- Reasons, notes, transitions, normalization: Task 3.
- Rejection feedback and rejected correction: Task 4.
- `verify`, `reject`, `delist`, `reopen`, audit, report resolution, idempotency, no-ops: Task 5.
- Operator correction (with audit and report resolution) and moderation detail with full Profiles: Task 6.
- Report creation, list, dismiss, catalogue-visibility rule, self-report, `404` for hidden Venues: Task 7.
- Six routes, `403` rule, error mapping: Task 8.
- OpenAPI, AGENTS.md, DevSteps: Task 9.
- Out of scope per spec: Review Reports, check-in summaries, provisioning, merging. Not planned.

**Two points to confirm with the reviewer before executing:**
- Operator correction and a no-op `PUT` (unchanged fields) write nothing; only a real change appends the audit row and resolves reports. The spec says "appends an audit row" for a correction and is silent on the no-op case.
- Operator `PUT` marks pending reports `actioned`, following DevSteps ("correct ... and atomically resolves each handled report") even though the spec lists the resolution only under moderation actions.

**Type consistency.** Service methods (`ReportVenue`, `ListVenueReports`, `GetModerationVenue`, `ModerateVenue`, `CorrectVenue`, `DismissVenueReport`) match the `VenueModeration` interface exactly. `Moderated` embeds `Submission`; `ModerationVenue` embeds `Moderated`. `moderationState` fields (`action`, `from`, `to`, `reason`, `note`) are the ones read in Task 5.

**Known weak spots.**
- Task 7's `reportColumns`/`reportSelect` snippet is written with a format placeholder and the text tells the implementer to simplify it. Treat that as an instruction to write plain concatenation, not as a placeholder to leave.
- The OpenAPI schema block assumes a full-Profile schema name that Step 2 tells the implementer to look up.
- `TestVenueReportIndexes` depends on the planner choosing the partial indexes; if the joined list query does not use `venue_reports_pending_list_key`, Task 7 says to switch the test to the joined query.
