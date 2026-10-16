# Session 6 Profile Safety and Moderation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an app User report a Profile and let a Platform Moderator rename-require, suspend, or restore it through the Go API.

**Architecture:** `internal/profile` owns reports, moderation state, expiry, audit, quota, and their transactions. `internal/auth` answers whether the Supabase user has a `platform_operators` row. `internal/api` admits app routes and privileged routes and translates the results. Resource UUIDs stay in the API. Another person's auth User id stays off community responses; moderator responses include it.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, PostgreSQL/PostGIS, existing `internal/idempotency` and `internal/ratelimit`, OpenAPI 3.1, `httptest`, and `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-15-session-6-profile-safety-moderation-design.md`

## Global Constraints

- Continue on `feature/session-6-profile-safety`; merge into `dev` by pull request.
- Read `backend/AGENTS.md`, `docs/architecture/backend.md`, DevSteps Session 6, and the spec before editing.
- Keep `cmd/api` composition-only. Extend `internal/profile`, `internal/auth`, and `internal/api`. Add no moderation package and no new dependency.
- Do not edit migrations `0001`, `0002`, or `0003`. Add `infra/supabase/migrations/0004_profile_safety.sql`.
- One Supabase auth user is either an app User or a Platform Moderator. No route creates or deletes operator rows.
- App routes reject an operator with `403 forbidden`. Privileged routes reject an app User with `403 forbidden`.
- `GET /v1/me` returns the caller's own User id plus `moderation_reason`, `moderation_note`, and `suspended_until`.
- `POST /v1/profiles/{username}/reports` identifies the target by current Username and returns no User id. The report id is a resource UUID.
- Privileged routes identify Profiles and reports by UUID. Moderator Profile views include the User id.
- Report reasons are `abusive_name`, `hate_or_discrimination`, `impersonation`, `personal_information`, `spam`, and `other`. Report notes are required for `impersonation`, `personal_information`, and `other`. Action notes are required only for `other`.
- `profile_report` and `profile_moderation_action` require `Idempotency-Key`. Dismissal does not.
- A report takes one `report_quota_events` slot. The limit is 20 events per reporter in a rolling 24 hours.
- The API process never applies migrations. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Never log bearer tokens, database URLs, or full request bodies.
- Every code task follows red-green-refactor and ends in the named commit.

## Review Focus

1. **Stored action replay:** repeating a key whose `201` is already stored must return that same action id even though the Profile now matches the command; Task 7 pins this with `TestModerationReplayIgnoresCurrentMatch`.
2. **Expiry before patch:** a suspension whose `suspended_until` equals the request time is active before the patch is judged, and that normalization spends no write token; Task 3 pins this with `TestExpiredSuspensionNormalizesBeforePatch`.
3. **Quota after target deletion:** deleting the target removes the report and leaves the reporter's quota event, so the next report at 20 events returns `429 quota_exceeded`; Task 5 pins this with `TestQuotaSurvivesTargetDeletion`.
4. **Operator cannot become an app User:** an operator `PATCH /v1/me` returns `403 forbidden` and inserts no Profile; Task 4 pins this with `TestOperatorCannotCreateProfile`.
5. **Concurrent quota:** two reports racing for the last slot leave exactly 20 events; Task 5 pins this with `TestConcurrentReportQuota`.

---

## File Structure

New files:

```text
infra/supabase/migrations/0004_profile_safety.sql
backend/internal/profile/access.go       # PublicIdentity and CommunityAccess
backend/internal/profile/access_test.go
backend/internal/profile/report.go       # report command, quota, idempotency
backend/internal/profile/report_test.go
backend/internal/profile/moderate.go     # list, dismiss, and moderation actions
backend/internal/profile/moderate_test.go
backend/internal/api/moderation.go       # privileged HTTP adapters
backend/internal/api/moderation_test.go
backend/internal/database/safety_test.go
```

Focused modifications:

```text
backend/internal/profile/profile.go
backend/internal/profile/validation.go
backend/internal/profile/validation_test.go
backend/internal/profile/postgres.go
backend/internal/profile/postgres_test.go
backend/internal/auth/auth.go
backend/internal/auth/postgres_test.go
backend/internal/api/auth.go
backend/internal/api/auth_test.go
backend/internal/api/profile.go
backend/internal/api/profile_test.go
backend/internal/api/envelope.go
backend/internal/api/server.go
backend/internal/database/pool.go
backend/internal/database/postgres_test.go
backend/internal/database/access_test.go
backend/openapi.yaml
backend/contract_test.go
backend/AGENTS.md
DevSteps.md
```

`pgtest.MigratedURL` already applies every SQL file in order. No test-harness edit is required for `0004` to load.

### Task 1: Safety Migration and Readiness

**Files:**
- Create: `infra/supabase/migrations/0004_profile_safety.sql`
- Create: `backend/internal/database/safety_test.go`
- Modify: `backend/internal/database/pool.go`
- Modify: `backend/internal/database/postgres_test.go`
- Modify: `backend/internal/database/access_test.go`
- Test: `backend/internal/database/postgres_test.go`
- Test: `backend/internal/database/safety_test.go`
- Test: `backend/internal/database/access_test.go`

**Interfaces:**
- Consumes: migrations `0001` through `0003`, `database.Pool.Check`, `pgtest.MigratedURL`.
- Produces: tables `platform_operators`, `profile_reports`, `report_quota_events`, and `profile_moderation_actions`; Profile checks for reason, rename, suspension, and `other`; exclusion and append-only triggers; readiness version `0004`.

- [ ] **Step 1: Write the failing readiness and constraint tests**

In `TestMigrationVersions`, after the `0003` insert, expect `(false, "migrations_behind")`. Then apply `0004_profile_safety.sql`, insert version `0004`, and expect `(true, "ok")`.

Add `backend/internal/database/safety_test.go`:

```go
func TestProfileSafetyConstraints(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.MigratedURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	user := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	other := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	if _, err := pool.Exec(ctx, `insert into auth.users (id) values ($1), ($2)`, user, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into profiles (id, username, display_name) values ($1, 'metal_fan', 'Metal Fan')`, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into platform_operators (user_id) values ($1)`, user); err == nil {
		t.Fatal("operator row overlapped a profile")
	}
	if _, err := pool.Exec(ctx, `insert into platform_operators (user_id) values ($1)`, other); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into profiles (id, username, display_name) values ($1, 'mod_fan', 'Mod')`, other); err == nil {
		t.Fatal("profile overlapped an operator")
	}
	if _, err := pool.Exec(ctx, `
		update profiles
		set status = 'rename_required', moderation_reason = 'abusive_name', suspended_until = now()
		where id = $1`, user); err == nil {
		t.Fatal("rename_required accepted an expiry")
	}
	if _, err := pool.Exec(ctx, `
		insert into profile_reports (reporter_user_id, target_user_id, reason)
		values ($1, $1, 'spam')`, user); err == nil {
		t.Fatal("self-report accepted")
	}
	var action string
	if err := pool.QueryRow(ctx, `
		insert into profile_moderation_actions (subject_user_id, action, actor_type, actor_user_id, reason)
		values ($1, 'rename_required', 'operator', $2, 'spam')
		returning action`, user, other).Scan(&action); err != nil || action != "rename_required" {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `delete from profile_moderation_actions`); err == nil {
		t.Fatal("audit delete succeeded")
	}
	if _, err := pool.Exec(ctx, `delete from auth.users where id = $1`, user); err != nil {
		t.Fatal(err)
	}
	var subject *string
	if err := pool.QueryRow(ctx, `select subject_user_id::text from profile_moderation_actions`).Scan(&subject); err != nil || subject != nil {
		t.Fatalf("audit subject %v %v", subject, err)
	}
}
```

Add these statements to the role loop in `TestDataAPIClosed`:

```go
`select * from platform_operators`,
`insert into platform_operators (user_id) values ('00000000-0000-4000-8000-000000000001')`,
`update platform_operators set created_at = now() where false`,
`delete from platform_operators where false`,
`select * from profile_reports`,
`insert into profile_reports (reporter_user_id, target_user_id, reason) values ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-8000-000000000002', 'spam')`,
`update profile_reports set status = 'pending' where false`,
`delete from profile_reports where false`,
`select * from report_quota_events`,
`insert into report_quota_events (user_id) values ('00000000-0000-4000-8000-000000000001')`,
`update report_quota_events set created_at = now() where false`,
`delete from report_quota_events where false`,
`select * from profile_moderation_actions`,
`insert into profile_moderation_actions (action, actor_type, reason) values ('suspended', 'system', 'spam')`,
`update profile_moderation_actions set reason = 'spam' where false`,
`delete from profile_moderation_actions where false`,
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/database -run 'TestMigrationVersions|TestProfileSafetyConstraints|TestDataAPIClosed' -count=1`

Expected: FAIL because `0004` is missing and readiness still accepts `0003`.

- [ ] **Step 3: Add the migration and raise readiness**

Set `requiredMigrationVersion` in `backend/internal/database/pool.go` to `"0004"`.

Create `infra/supabase/migrations/0004_profile_safety.sql`:

```sql
create table platform_operators (
  user_id uuid primary key references auth.users (id) on delete cascade,
  created_at timestamptz not null default now()
);

create table profile_reports (
  id uuid primary key default gen_random_uuid(),
  reporter_user_id uuid not null references auth.users (id) on delete cascade,
  target_user_id uuid not null references auth.users (id) on delete cascade,
  reason text not null,
  note text,
  status text not null default 'pending',
  created_at timestamptz not null default now(),
  resolved_at timestamptz,
  resolved_by_user_id uuid references auth.users (id) on delete set null,
  constraint profile_reports_reason_check check (
    reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other')
  ),
  constraint profile_reports_status_check check (
    status in ('pending', 'actioned', 'dismissed')
  ),
  constraint profile_reports_note_length check (
    note is null or char_length(note) between 1 and 1000
  ),
  constraint profile_reports_note_required check (
    reason not in ('impersonation', 'personal_information', 'other') or note is not null
  ),
  constraint profile_reports_distinct_users check (reporter_user_id <> target_user_id),
  constraint profile_reports_resolution_check check (
    (status = 'pending' and resolved_at is null and resolved_by_user_id is null)
    or (status in ('actioned', 'dismissed') and resolved_at is not null)
  )
);

create unique index profile_reports_pending_pair_key
  on profile_reports (reporter_user_id, target_user_id)
  where status = 'pending';

create index profile_reports_pending_list_key
  on profile_reports (created_at, id)
  where status = 'pending';

create table report_quota_events (
  id uuid primary key default gen_random_uuid(),
  user_id uuid not null references auth.users (id) on delete cascade,
  created_at timestamptz not null default now()
);

create index report_quota_events_user_created_key
  on report_quota_events (user_id, created_at);

create table profile_moderation_actions (
  id uuid primary key default gen_random_uuid(),
  subject_user_id uuid references auth.users (id) on delete set null,
  action text not null,
  actor_type text not null,
  actor_user_id uuid references auth.users (id) on delete set null,
  reason text,
  note text,
  suspended_until timestamptz,
  created_at timestamptz not null default now(),
  constraint profile_moderation_actions_action_check check (
    action in ('rename_required', 'suspended', 'restored')
  ),
  constraint profile_moderation_actions_actor_check check (
    (actor_type in ('operator', 'user') and actor_user_id is not null)
    or (actor_type = 'system' and actor_user_id is null)
  ),
  constraint profile_moderation_actions_reason_check check (
    (action = 'restored' and (reason is null or reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other')))
    or (action in ('rename_required', 'suspended') and reason in ('abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other'))
  ),
  constraint profile_moderation_actions_expiry_check check (
    action = 'suspended' or suspended_until is null
  ),
  constraint profile_moderation_actions_note_check check (
    (note is null or char_length(note) between 1 and 1000)
    and (reason is distinct from 'other' or note is not null)
  )
);

create function reject_overlapping_identity() returns trigger
language plpgsql as $$
begin
  if tg_table_name = 'platform_operators' then
    if exists (select 1 from profiles where id = new.user_id) then
      raise exception 'operator and profile overlap' using errcode = '23514';
    end if;
  elsif exists (select 1 from platform_operators where user_id = new.id) then
    raise exception 'operator and profile overlap' using errcode = '23514';
  end if;
  return new;
end $$;

create trigger platform_operators_no_profile
  before insert or update on platform_operators
  for each row execute function reject_overlapping_identity();

create trigger profiles_no_operator
  before insert or update on profiles
  for each row execute function reject_overlapping_identity();

create function reject_moderation_action_mutation() returns trigger
language plpgsql as $$
begin
  raise exception 'profile moderation actions are append-only' using errcode = '55000';
end $$;

create trigger profile_moderation_actions_append_only
  before update or delete on profile_moderation_actions
  for each row execute function reject_moderation_action_mutation();

alter table profiles add constraint profiles_moderation_reason_check check (
  moderation_reason is null or moderation_reason in (
    'abusive_name', 'hate_or_discrimination', 'impersonation', 'personal_information', 'spam', 'other'
  )
);
alter table profiles add constraint profiles_rename_required_check check (
  status <> 'rename_required' or (moderation_reason is not null and suspended_until is null)
);
alter table profiles add constraint profiles_suspended_check check (
  status <> 'suspended' or moderation_reason is not null
);
alter table profiles add constraint profiles_moderation_note_present_check check (
  moderation_note is null or char_length(moderation_note) >= 1
);
alter table profiles add constraint profiles_other_note_check check (
  moderation_reason is distinct from 'other' or moderation_note is not null
);

alter table platform_operators enable row level security;
alter table profile_reports enable row level security;
alter table report_quota_events enable row level security;
alter table profile_moderation_actions enable row level security;
revoke all on table platform_operators from public, anon, authenticated, service_role;
revoke all on table profile_reports from public, anon, authenticated, service_role;
revoke all on table report_quota_events from public, anon, authenticated, service_role;
revoke all on table profile_moderation_actions from public, anon, authenticated, service_role;
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/database -run 'TestMigrationVersions|TestProfileSafetyConstraints|TestDataAPIClosed' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add infra/supabase/migrations/0004_profile_safety.sql backend/internal/database
git commit -m "$(cat <<'EOF'
feat: add profile safety schema

Operators, reports, quota events, and audit rows need constraints before the API can enforce them.

EOF
)"
```

### Task 2: Identity and Contribution Rules

**Files:**
- Create: `backend/internal/profile/access.go`
- Create: `backend/internal/profile/access_test.go`
- Modify: `backend/internal/profile/validation.go`
- Modify: `backend/internal/profile/validation_test.go`
- Test: `backend/internal/profile/access_test.go`
- Test: `backend/internal/profile/validation_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 at runtime. Tests are pure.
- Produces:

```go
type Identity struct {
	Username    string
	DisplayName string
}

func PublicIdentity(status, username, displayName string) Identity

var ErrProfileRequired = errors.New("profile: profile required")
var ErrRenameRequired = errors.New("profile: rename required")

type SuspendedError struct{ Until *time.Time }

func (e *SuspendedError) Error() string

func CommunityAccess(found bool, status string, until *time.Time) error

func ValidReason(reason string) bool
func NoteRequired(reason string, report bool) bool
func NormalizeNote(value string) (string, error)
```

- [ ] **Step 1: Write the failing rule tests**

```go
func TestPublicIdentity(t *testing.T) {
	active := PublicIdentity("active", "MetalFan", "Metal Fan")
	if active.Username != "MetalFan" || active.DisplayName != "Metal Fan" {
		t.Fatalf("%+v", active)
	}
	for _, status := range []string{"rename_required", "suspended"} {
		got := PublicIdentity(status, "MetalFan", "Metal Fan")
		if got.Username != "" || got.DisplayName != "MetalHops User" {
			t.Fatalf("%s %+v", status, got)
		}
	}
}

func TestCommunityAccess(t *testing.T) {
	if !errors.Is(CommunityAccess(false, "", nil), ErrProfileRequired) {
		t.Fatal("missing profile")
	}
	if !errors.Is(CommunityAccess(true, "rename_required", nil), ErrRenameRequired) {
		t.Fatal("rename")
	}
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	err := CommunityAccess(true, "suspended", &until)
	var suspended *SuspendedError
	if !errors.As(err, &suspended) || suspended.Until == nil || !suspended.Until.Equal(until) {
		t.Fatal(err)
	}
	if err := CommunityAccess(true, "suspended", nil); !errors.As(err, &suspended) || suspended.Until != nil {
		t.Fatal(err)
	}
	if err := CommunityAccess(true, "active", nil); err != nil {
		t.Fatal(err)
	}
}

func TestModerationText(t *testing.T) {
	if !ValidReason("spam") || ValidReason("nope") {
		t.Fatal("reason")
	}
	if !NoteRequired("other", false) || NoteRequired("spam", false) {
		t.Fatal("action note")
	}
	if !NoteRequired("impersonation", true) || NoteRequired("spam", true) {
		t.Fatal("report note")
	}
	note, err := NormalizeNote("  café\nline  ")
	if err != nil || note != "café\nline" {
		t.Fatalf("%q %v", note, err)
	}
	if _, err := NormalizeNote("bad\x00"); err == nil {
		t.Fatal("control")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/profile -run 'TestPublicIdentity|TestCommunityAccess|TestModerationText' -count=1`

Expected: FAIL with undefined `PublicIdentity`.

- [ ] **Step 3: Implement the rules**

`access.go` returns `Identity{Username: "", DisplayName: "MetalHops User"}` for `rename_required` and `suspended`. Every other status returns the stored values. `CommunityAccess` returns `ErrProfileRequired` when `found` is false, `ErrRenameRequired` for `rename_required`, and `&SuspendedError{Until: until}` for `suspended`. Active returns nil.

`ValidReason` accepts only the six spec reasons. `NoteRequired` is true for `other`, and also for `impersonation` and `personal_information` when `report` is true. `NormalizeNote` rejects invalid UTF-8, null bytes, and control characters other than line feeds, trims, normalizes NFC, and requires 1–1,000 runes. Reuse `unicode/utf8`, `unicode`, `strings`, and `norm.NFC`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/profile -run 'TestPublicIdentity|TestCommunityAccess|TestModerationText' -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile/access.go backend/internal/profile/access_test.go backend/internal/profile/validation.go backend/internal/profile/validation_test.go
git commit -m "$(cat <<'EOF'
feat: define profile contribution and public identity

Later community writes need one decision for rename, suspension, and redaction.

EOF
)"
```

### Task 3: Owner Moderation State

**Files:**
- Modify: `backend/internal/profile/profile.go`
- Modify: `backend/internal/profile/postgres.go`
- Modify: `backend/internal/profile/postgres_test.go`
- Modify: `backend/internal/api/envelope.go`
- Modify: `backend/internal/api/profile.go`
- Modify: `backend/internal/api/profile_test.go`
- Test: `backend/internal/profile/postgres_test.go`
- Test: `backend/internal/api/profile_test.go`

**Interfaces:**
- Consumes: `CommunityAccess`, `SuspendedError`, `ErrRenameRequired`, migration `0004`.
- Produces: `Profile.ModerationReason *string`, `Profile.ModerationNote *string`, `Profile.SuspendedUntil *time.Time`. `Get`, `Patch`, and `Delete` normalize an expired suspension before their own decision. `writeProfileError` maps `*SuspendedError` to `403 profile_suspended` and includes `details.suspended_until` only when `Until != nil`.

- [ ] **Step 1: Write the failing owner-state tests**

Add `TestExpiredSuspensionNormalizesBeforePatch` to `postgres_test.go`. Insert a Profile with `status = 'suspended'`, reason `spam`, and `suspended_until` equal to the service clock. Give the limiter one observable reservation. Call `Patch` with a new display name. Assert the result is `active`, moderation fields are nil, the display name changed, one `profile_moderation_actions` row exists with `action = 'restored'`, `actor_type = 'system'`, `actor_user_id` null, and reason `spam`, and the limiter still has 59 tokens after the successful patch. The normalization itself must not be the spent token: a second call that only reads through `Get` on another expired row leaves the limiter at 60.

Add three more service tests:

- Indefinite suspension (`suspended_until` null) plus a changed display name returns `*SuspendedError` with `Until == nil`, leaves the row suspended, and spends no token.
- The same suspended row patched with its current Username and display name returns `200`-equivalent stored values and spends no token.
- `rename_required` with a casing-only Username change stores `active`, clears moderation fields, and appends `restored` with `actor_type = 'user'`, the caller id, and the previous reason and note.

Extend the HTTP profile test. The active `GET /v1/me` body includes `"moderation_reason":null,"moderation_note":null,"suspended_until":null` after `status`. A fake service returning `*profile.SuspendedError` with a UTC time yields `403`, code `profile_suspended`, and `"suspended_until":"2026-11-01T00:00:00Z"` inside `details`. A nil `Until` omits `details`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/profile ./internal/api -run 'TestExpiredSuspensionNormalizesBeforePatch|TestProfile' -count=1`

Expected: FAIL because moderation fields and expiry behavior are absent.

- [ ] **Step 3: Implement normalization and the owner responses**

Add the three fields to `Profile` and to `profileColumns`:

```go
const profileColumns = `id::text, username, display_name, status, moderation_reason, moderation_note, suspended_until, created_at, updated_at`
```

Update `scanProfile` to scan them. Add `details map[string]any` with `json:"details,omitempty"` to `errorDetail`. Add:

```go
func WriteErrorDetails(w http.ResponseWriter, status int, code, message, requestID string, fields map[string]string, details map[string]any) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code: code, Message: message, RequestID: requestID, Fields: fields, Details: details,
	}})
}
```

Keep `WriteError` for callers that have no details.

Add `normalizeExpiry(ctx, tx, userID string) error` in `postgres.go`. It locks `profiles` for update. When status is `suspended` and `suspended_until` is non-null and `!until.After(s.clock())`, it inserts the system `restored` audit row, copying reason and note, then sets the Profile to `active` with all moderation fields null and a new `updated_at`. It spends no limiter token. `Get`, `Patch`, and `Delete` call it in a committed transaction before their existing work. `Get` then reads the committed row.

In `Patch`, after the unchanged-value return, when the stored status is `suspended`, return `&SuspendedError{Until: current.SuspendedUntil}` before reserving a token. The existing active update remains. When the stored status is `rename_required` and a value changes, use that same update and, in the same transaction, insert `restored` with actor type `user`, actor id the caller, and the reason and note from the locked row.

Map `*SuspendedError` in `writeProfileError`. Include `details` only when `Until != nil`, formatted with `time.RFC3339`. Copy the three new fields in `view`. Update the existing exact JSON assertion.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/profile ./internal/api -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile backend/internal/api/envelope.go backend/internal/api/profile.go backend/internal/api/profile_test.go
git commit -m "$(cat <<'EOF'
feat: enforce rename and suspension on the caller profile

The owner needs moderation feedback, and an expired suspension has to clear before the next profile write.

EOF
)"
```

### Task 4: Operator Gate

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/auth/postgres_test.go`
- Modify: `backend/internal/api/auth.go`
- Modify: `backend/internal/api/auth_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/internal/api/profile_test.go`
- Test: `backend/internal/auth/postgres_test.go`
- Test: `backend/internal/api/auth_test.go`
- Test: `backend/internal/api/profile_test.go`

**Interfaces:**
- Consumes: `platform_operators` from Task 1 and the existing `Authenticator.Authenticate`.
- Produces:

```go
func (v *Verifier) IsOperator(ctx context.Context, userID string) (bool, error)

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Caller, error)
	IsOperator(context.Context, string) (bool, error)
}
```

`requireApp` wraps `/v1/me`. An operator receives `403 forbidden` before `Profiles` is called. `fakeAuth.IsOperator` defaults to false so existing app-user tests stay valid.

- [ ] **Step 1: Write the failing operator tests**

Add a postgres test that inserts one `platform_operators` row and asserts `IsOperator` is true for that id and false for another existing auth user.

Add `TestOperatorCannotCreateProfile` in `api/profile_test.go`. The fake authenticator returns `IsOperator == true`. `PATCH /v1/me` with both required fields returns `403` and code `forbidden`. The fake Profiles `Patch` records no call.

Add the same assertion for `GET /v1/me` and `DELETE /v1/me`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/auth ./internal/api -run 'TestOperator|TestAuthMiddleware' -count=1`

Expected: FAIL because `IsOperator` is not on the authenticator.

- [ ] **Step 3: Implement the lookup and app-route gate**

```go
func (v *Verifier) IsOperator(ctx context.Context, userID string) (bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var operator bool
	err := v.pool.QueryRow(queryCtx, `
		select exists(select 1 from platform_operators where user_id = $1::uuid)`, userID).Scan(&operator)
	if err != nil {
		return false, fmt.Errorf("auth: operator lookup: %w", err)
	}
	return operator, nil
}
```

Extend `Authenticator` and `fakeAuth`. `requireApp` calls `requireAuth`, then `IsOperator`. A true result writes `403 forbidden` with message `This action is unavailable for this account.` A lookup error logs the wrapped error and returns `500 internal_error`. Change `server.go` so all three `/v1/me` registrations use `requireApp`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/auth ./internal/api -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/auth backend/internal/api
git commit -m "$(cat <<'EOF'
feat: reject operator calls on app profile routes

A moderator login has to stay separate from profile creation.

EOF
)"
```

### Task 5: Profile Reports and Quota

**Files:**
- Create: `backend/internal/profile/report.go`
- Create: `backend/internal/profile/report_test.go`
- Modify: `backend/internal/api/profile.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/internal/api/profile_test.go`
- Test: `backend/internal/profile/report_test.go`
- Test: `backend/internal/api/profile_test.go`

**Interfaces:**
- Consumes: `CommunityAccess`, `ValidReason`, `NoteRequired`, `NormalizeNote`, `idempotency.Execute`, `ErrProfileRequired`, `ErrRenameRequired`, `*SuspendedError`, `*RateLimitError`.
- Produces:

```go
type ReportCommand struct {
	ReporterID string
	Username   string
	Reason     string
	Note       *string
	Key        string
}

type Report struct {
	ID        string
	Username  string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
}

type QuotaError struct{ RetryAfter int }

func (s *Service) Report(ctx context.Context, cmd ReportCommand) (Report, error)

var ErrSelfReport = errors.New("profile: self report")
var ErrReportPending = errors.New("profile: report pending")
```

HTTP maps those to `400 self_report` and `409 profile_already_reported`. `ErrProfileRequired` maps to `409 profile_required`. The route is `POST /v1/profiles/{username}/reports` behind `requireApp`.

- [ ] **Step 1: Write the failing report tests**

`TestQuotaSurvivesTargetDeletion` creates reporter and target Profiles, inserts 19 quota events newer than 24 hours, creates one report, deletes the target auth user, and asserts the report count is 0 while the reporter still has 20 events. The next `Report` call returns `*QuotaError` with `RetryAfter >= 1` and does not insert event 21.

`TestConcurrentReportQuota` starts at 19 events and runs two different target reports together. One succeeds, one returns `*QuotaError`, and the event count is 20.

Also assert:

- `MetalFan` resolves to the stored Username `metalfan` in the response, and the response struct has no target User id.
- The same key and same target replays one row and one event.
- The same key with a different reason returns `idempotency.ErrReused`.
- A self-report returns `ErrSelfReport` and creates no event.
- A second pending pair returns `ErrReportPending`.
- A resolved pair can be reported again.
- `rename_required` returns `ErrRenameRequired`. An unexpired suspension returns `*SuspendedError`. An expired suspension normalizes and then allows the report.
- A missing reporter returns `ErrProfileRequired`.

The HTTP test posts `{"reason":"spam"}` with `Idempotency-Key: report-key` to `/v1/profiles/metalfan/reports` and expects `201` JSON `{"data":{"id":"...","username":"metalfan","reason":"spam","note":null,"status":"pending","created_at":"..."}}`. Assert the body does not contain the target User id. Missing key, operator caller, and `ErrReportPending` cover `400 validation_error`, `403 forbidden`, and `409 profile_already_reported`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/profile ./internal/api -run 'TestQuota|TestConcurrentReportQuota|TestReport' -count=1`

Expected: FAIL with undefined `Service.Report`.

- [ ] **Step 3: Implement report creation**

`Report` calls `normalizeExpiry` and commits it before the report transaction. The report transaction locks the caller Profile and uses `CommunityAccess`. Validation failures, a missing target, a self-report, and a pending duplicate return before `Reserve`. Inside the idempotency callback, reserve the write token before the inserts, cancel it if the callback returns an error, and commit it only after the transaction commits. A replay does not enter the callback and spends no User write token. A target Username change made after the report leaves that pending report open; assert that in the service test.

The normalized command is JSON from:

```go
type reportCommand struct {
	TargetUserID string  `json:"target_user_id"`
	Reason       string  `json:"reason"`
	Note         *string `json:"note"`
}
```

Resolve the target with `select id::text, username, status from profiles where lower(username) = lower($1) for update`. No row is `ErrNotFound`. The caller id is `ErrSelfReport`. Validate reason and note with Task 2. Inside `idempotency.Execute` with operation `profile_report`:

```go
_, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('reports' || chr(31) || $1, 0))`, reporterID)
```

Count events where `created_at > now-24h`. At 20 or more, return `*QuotaError` whose `RetryAfter` is `ceil((oldest+24h)-now)` and at least 1. A pending pair returns `ErrReportPending`. Otherwise insert the report and one quota event in that callback. `Execute` stores the `201` body. A replay returns that body and does not insert again.

Register the route on `requireApp`. Decode with `DecodeJSON`. Map `idempotency.ErrInvalid` and a missing header to `400 validation_error`. Map `idempotency.ErrReused` to `409 idempotency_key_reused`. Map `*QuotaError` to `429 quota_exceeded` with `Retry-After` set from `RetryAfter`, using `WriteError`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/profile ./internal/api -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile/report.go backend/internal/profile/report_test.go backend/internal/api/profile.go backend/internal/api/profile_test.go backend/internal/api/server.go
git commit -m "$(cat <<'EOF'
feat: accept profile reports by username

App users can file one pending report without receiving another person's user id.

EOF
)"
```

### Task 6: Moderator Reads

**Files:**
- Create: `backend/internal/profile/moderate.go`
- Create: `backend/internal/profile/moderate_test.go`
- Create: `backend/internal/api/moderation.go`
- Create: `backend/internal/api/moderation_test.go`
- Modify: `backend/internal/api/server.go`
- Test: `backend/internal/profile/moderate_test.go`
- Test: `backend/internal/api/moderation_test.go`

**Interfaces:**
- Consumes: `Profile` from Task 3 and `requireAuth` from Task 4.
- Produces:

```go
type ReportItem struct {
	ID        string
	Reason    string
	Note      *string
	Status    string
	CreatedAt time.Time
	Reporter  Profile
	Target    Profile
}

type ReportPage struct {
	Items      []ReportItem
	NextCursor *string
}

func (s *Service) ListReports(ctx context.Context, limit int, cursor string) (ReportPage, error)
func (s *Service) GetModerationProfile(ctx context.Context, userID string) (Profile, error)
```

`requireOperator` returns `403 forbidden` when `IsOperator` is false. Routes are `GET /v1/moderation/profile-reports` and `GET /v1/moderation/profiles/{userID}`.

- [ ] **Step 1: Write the failing read tests**

Insert three pending reports and one dismissed report. `ListReports(ctx, 2, "")` returns the two oldest pending reports, each with both full Profiles, and a non-nil cursor. The next call returns the third and a nil cursor. A malformed cursor returns `*ValidationError` with field `cursor`. A canonical unknown User id returns `ErrNotFound`. `abc` returns `*ValidationError` with field `user_id`.

The HTTP test checks an app User gets `403 forbidden`, an operator gets the list envelope `{"data":[...],"meta":{"next_cursor":...}}`, and each embedded Profile contains `id`, `username`, `display_name`, `status`, `moderation_reason`, `moderation_note`, `suspended_until`, `created_at`, and `updated_at`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/profile ./internal/api -run 'TestListReports|TestModerationReads' -count=1`

Expected: FAIL with undefined `ListReports`.

- [ ] **Step 3: Implement the reads**

Encode a cursor as `base64.RawURLEncoding` of `created_at.UTC().Format(time.RFC3339Nano) + "|" + id`. Decode rejects any other shape. Default limit 20 and maximum 100 match DevSteps; a limit below 1 or above 100 is `validation_error` field `limit`.

```sql
select r.id::text, r.reason, r.note, r.status, r.created_at,
       reporter.id::text, reporter.username, reporter.display_name, reporter.status,
       reporter.moderation_reason, reporter.moderation_note, reporter.suspended_until,
       reporter.created_at, reporter.updated_at,
       target.id::text, target.username, target.display_name, target.status,
       target.moderation_reason, target.moderation_note, target.suspended_until,
       target.created_at, target.updated_at
from profile_reports r
join profiles reporter on reporter.id = r.reporter_user_id
join profiles target on target.id = r.target_user_id
where r.status = 'pending'
  and ($1::timestamptz is null or (r.created_at, r.id) > ($1::timestamptz, $2::uuid))
order by r.created_at, r.id
limit $3
```

Request `limit+1` rows. Return at most `limit`, and set the cursor from the last returned row when an extra row exists. `GetModerationProfile` reads one Profile by canonical UUID. `requireOperator` is the inverse of `requireApp`: a false lookup writes `403 forbidden` with message `This action requires a moderator account.` Register both GET routes behind it. Reads do not call the write limiter.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/profile ./internal/api -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile/moderate.go backend/internal/profile/moderate_test.go backend/internal/api/moderation.go backend/internal/api/moderation_test.go backend/internal/api/server.go
git commit -m "$(cat <<'EOF'
feat: list pending profile reports for moderators

The admin app needs the current profile of both people on each pending report.

EOF
)"
```

### Task 7: Moderator Actions and Dismissal

**Files:**
- Modify: `backend/internal/profile/moderate.go`
- Modify: `backend/internal/profile/moderate_test.go`
- Modify: `backend/internal/api/moderation.go`
- Modify: `backend/internal/api/moderation_test.go`
- Test: `backend/internal/profile/moderate_test.go`
- Test: `backend/internal/api/moderation_test.go`

**Interfaces:**
- Consumes: `ListReports`, `GetModerationProfile`, `ValidReason`, `NoteRequired`, `NormalizeNote`, `idempotency.Execute`, `*RateLimitError`.
- Produces:

```go
type ModerationCommand struct {
	ActorID        string
	TargetID       string
	Action         string
	Reason         string
	ReasonSet      bool
	Note           *string
	NoteSet        bool
	SuspendedUntil *time.Time
	UntilSet       bool
	Key            string
}

type ModerationResult struct {
	Created  bool
	ActionID *string
	Profile  Profile
}

func (s *Service) Moderate(ctx context.Context, cmd ModerationCommand) (ModerationResult, error)
func (s *Service) DismissReport(ctx context.Context, actorID, reportID string) (ReportItem, error)

var ErrReportNotFound = errors.New("profile: report not found")
var ErrReportResolved = errors.New("profile: report resolved")
```

`Created == false` is HTTP `200` with `"action_id":null`. `Created == true` is HTTP `201`. Dismissal is `PATCH /v1/moderation/profile-reports/{reportID}` behind `requireOperator`.

- [ ] **Step 1: Write the failing action tests**

`TestModerationReplayIgnoresCurrentMatch` applies `suspended` with a key, records the `201` action id, and repeats the same key after the row already has that state. The second result is `Created == true` with the same action id. Assert one audit row and one idempotency row.

Then cover:

- The same suspended command with a new key, while the row already matches, returns `Created == false`, writes no audit row, and leaves no idempotency row. A later SQL change back to `active` lets that same key create the `201`.
- `rename_required` and `suspended` can replace each other. `suspended_until` in the past is `*ValidationError`. Omitted expiry stays null.
- `restored` with any reason, note, or expiry is `*ValidationError`. A real restore copies the cleared reason and note into the audit row and sets actor type `operator`.
- The action marks two pending reports `actioned`, including one inserted after the test has read the list and before `Moderate` runs.
- The same key with a different reason returns `idempotency.ErrReused`.
- Dismissing a pending report returns status `dismissed`, sets `resolved_by_user_id`, spends one token, and leaves the Profile unchanged. Repeating it returns the same item and spends no second token. Dismissing an actioned report returns `ErrReportResolved`. An unknown canonical id returns `ErrReportNotFound`. `abc` returns `*ValidationError` field `report_id`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/profile -run 'TestModeration|TestDismiss' -count=1`

Expected: FAIL with undefined `Service.Moderate`.

- [ ] **Step 3: Implement actions and dismissal**

Validate the command before touching idempotency. `rename_required` requires a reason, forbids a non-null expiry, and allows a null or omitted expiry. `suspended` requires a reason and rejects an expiry that is not after `s.clock()`. `restored` rejects `ReasonSet`, `NoteSet`, and `UntilSet`. `other` requires a normalized note.

Lock the target Profile. Build the normalized command:

```go
type actionCommand struct {
	UserID         string     `json:"user_id"`
	Action         string     `json:"action"`
	Reason         *string    `json:"reason"`
	Note           *string    `json:"note"`
	SuspendedUntil *time.Time `json:"suspended_until"`
}
```

Look in `idempotency_records` for the actor, operation `profile_moderation_action`, and key. An unexpired matching hash returns the stored `201` and sets `Created` true. A different hash returns `idempotency.ErrReused`. When no live record exists, compare the locked Profile with the requested result. An equal status, reason, note, and expiry returns `Created` false without calling `Execute`. Otherwise call `Execute`; its callback updates the Profile, inserts the audit row, and runs:

```sql
update profile_reports
set status = 'actioned', resolved_at = $3, resolved_by_user_id = $2::uuid
where target_user_id = $1::uuid and status = 'pending'
```

Reserve the write token only in that callback. The restoration audit copies the locked reason and note. Restrictive audit rows store the submitted reason, note, and expiry.

`DismissReport` locks the report row. Pending becomes `dismissed` with the actor and `s.clock()`, then spends one token. `dismissed` returns the current item without a token. `actioned` returns `ErrReportResolved`. Join both Profiles for the response.

HTTP decodes the action body with `DecodeJSON`. A present `reason`, `note`, or `suspended_until` sets the matching `Set` flag. `null` for `suspended_until` sets `UntilSet` and a nil pointer. Map `Created` to `200` or `201`. Dismissal accepts only `{"status":"dismissed"}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/profile ./internal/api -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/profile/moderate.go backend/internal/profile/moderate_test.go backend/internal/api/moderation.go backend/internal/api/moderation_test.go
git commit -m "$(cat <<'EOF'
feat: apply profile moderation actions

Moderators can rename-require, suspend, restore, and dismiss through idempotent API commands.

EOF
)"
```

### Task 8: Contract and Session Closeout

**Files:**
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`
- Modify: `backend/AGENTS.md`
- Modify: `DevSteps.md`
- Test: `backend/contract_test.go`

**Interfaces:**
- Consumes: every route and error code from Tasks 3–7.
- Produces: an OpenAPI document that matches those routes, readiness text `0004`, and DevSteps Session 6 marked done with the operator rule and route names.

- [ ] **Step 1: Write the failing contract test**

Add these strings to `TestOpenAPIDocument`:

```go
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
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test . -run TestOpenAPIDocument -count=1`

Expected: FAIL with the first missing path.

- [ ] **Step 3: Update the contract and docs**

Add the three moderation fields to `Profile`, required, nullable. Add `ModeratorProfile` with those fields plus required `created_at` and `updated_at`. Document:

- `POST /v1/profiles/{username}/reports` with `Idempotency-Key`, `201`, and the report body without a User id.
- `GET /v1/moderation/profile-reports` with `limit`, `cursor`, and the report item containing two `ModeratorProfile` values.
- `GET /v1/moderation/profiles/{userID}`.
- `POST /v1/moderation/profiles/{userID}/actions` with the three actions and `200`/`201` result `{action_id, profile}`.
- `PATCH /v1/moderation/profile-reports/{reportID}` with `{"status":"dismissed"}`.

Every new route uses `bearerAuth`. Include the error codes from the spec table. `operationId` values are `createProfileReport`, `listProfileReports`, `getModerationProfile`, `applyProfileModeration`, and `dismissProfileReport`.

In `backend/AGENTS.md`, say `GET /readyz` requires migration `0004` or newer, and describe `internal/profile` as Profile, report, and moderation behavior. `internal/auth` also answers the operator lookup.

Replace the Session 6 body in `DevSteps.md` with:

```markdown
### Session 6: Profile safety and moderation API

**Status:** Done.

Add Profile Reports, rename-required and suspended states, public identity
redaction, moderation audit, expiry behavior, and privileged moderation routes.
A Platform Moderator is a Supabase auth user with a `platform_operators` row
and no Profile. App routes return `403 forbidden` to that caller. Privileged
routes return `403 forbidden` to an authenticated caller without that row.
No route provisions operators.

`POST /v1/profiles/{username}/reports` files a report and requires
`Idempotency-Key`. `GET /v1/moderation/profile-reports` lists pending reports.
`GET /v1/moderation/profiles/{userID}` reads one Profile.
`POST /v1/moderation/profiles/{userID}/actions` applies `rename_required`,
`suspended`, or `restored` and requires `Idempotency-Key`.
`PATCH /v1/moderation/profile-reports/{reportID}` dismisses one report and
does not require `Idempotency-Key`.

**Exit condition:** Users can report unsafe Profiles, operators can require a
rename or suspend community writes through those endpoints, and moderated
identities are not exposed publicly.
```

Change the DevSteps header status to: `Sessions 1–6 are done. Session 7 is next. Sessions 8–14 are not started.`

- [ ] **Step 4: Run the full backend checks**

Run: `cd backend && gofmt -l . && go vet ./... && go test ./...`

Expected: `gofmt` prints nothing. `go vet` and `go test` pass.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract_test.go backend/AGENTS.md DevSteps.md
git commit -m "$(cat <<'EOF'
docs: publish the profile moderation contract

Session 6 is usable only when the OpenAPI document and roadmap name the operator rule and routes.

EOF
)"
```
