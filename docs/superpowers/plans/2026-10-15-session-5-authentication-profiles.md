# Session 5 Authentication and Profiles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Supabase JWT/JWKS authentication and the complete active-Profile slice, including self-service User deletion and authenticated write limiting.

**Architecture:** `internal/auth` verifies asymmetric tokens and live Supabase sessions; `internal/profile` owns Profile rules, transactions, persistence, and the Supabase Admin deletion call. `internal/api` owns transport adaptation through narrow caller-owned interfaces, while `internal/ratelimit` owns a reservation-based per-User limiter.

**Tech Stack:** Go 1.24, `net/http`, `pgx/v5`, `golang-jwt/jwt/v5`, `golang.org/x/text/unicode/norm`, PostgreSQL/PostGIS, Supabase Auth, OpenAPI 3.1, `httptest`, and `testcontainers-go`.

**Spec:** `docs/superpowers/specs/2026-10-15-session-5-authentication-profiles-design.md`

## Global Constraints

- Start from `dev` on `feature/session-5-authentication-profiles`; merge into `dev` by pull request.
- Read `backend/AGENTS.md`, `docs/architecture/backend.md`, DevSteps Session 5, and the spec before editing.
- Keep `cmd/api` composition-only and keep business rules inside capability modules.
- Do not edit `0001_init.sql` or `0002_api_safety.sql`; add `0003_auth_profiles.sql`.
- Never restore Data API grants or add direct product-data policies.
- Accept JWT algorithms `RS256` and `ES256` only; ES256 uses P-256.
- Derive issuer and JWKS/Admin URLs from `SUPABASE_URL`; do not add OpenID discovery.
- Use a five-second shared HTTP-client timeout and a ten-minute successful JWKS cache.
- JSON bodies are at most 64 KiB, valid UTF-8, one object, and reject unknown fields.
- Session 5 creates and updates only active Profiles; Session 6 owns moderation behavior. `GET /v1/me` returns a stored non-active status and omits moderation feedback. An unchanged `PATCH` leaves that status in place.
- Session 5 does not add Venue, Review, Check-in, report, or public-catalogue routes. Those rules now live in DevSteps for later sessions.
- Add no generic repository, service, common, or utility package.
- Never log bearer tokens, service-role keys, database URLs, full request bodies, or provider response bodies.
- The API process never applies migrations.
- The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Every code task follows red-green-refactor and ends in the named focused commit.

## Review Focus

1. **JWT key confusion:** an RSA key labelled ES256, an EC key labelled RS256, `alg=none`, or HS256 must return the same `401 unauthorized` as any invalid token; Task 4 pins all four.
2. **Body boundary ambiguity:** exactly 65,536 bytes is accepted when otherwise valid, while 65,537 bytes returns `413` without parsing a prefix; Task 9 pins both boundaries.
3. **Concurrent no-row Profile creation:** two PATCH requests for the same new User must leave one Profile, return safe outcomes, and restore the failed request's write reservation; Task 8 pins the race.
4. **Deletion partial failure:** provider timeout or non-2xx must return `502`, retain local rows, restore the User token, and omit the provider body and key from errors/logs; Task 8 pins each condition.
5. **JWKS rotation under load:** concurrent requests for a new `kid` must cause one forced refresh and all waiters must observe the replacement set; Task 4 pins the fetch count.

---

## File Structure

New production files:

```text
backend/internal/auth/auth.go          # public Caller, verifier, live-session authentication
backend/internal/auth/jwks.go          # private JWKS parsing, caching, and coalesced refresh
backend/internal/profile/profile.go    # Profile values, commands, classified errors
backend/internal/profile/validation.go # Username/display-name normalization and validation
backend/internal/profile/postgres.go   # Profile reads, create/update transaction
backend/internal/profile/admin.go      # Supabase Admin hard-delete adapter
backend/internal/profile/delete.go     # recent-session and identity-deletion operation
backend/internal/api/auth.go           # bearer middleware and caller context
backend/internal/api/decode.go         # shared bounded strict JSON decoder
backend/internal/api/profile.go        # /v1/me HTTP adapters and error translation
infra/supabase/migrations/0003_auth_profiles.sql
```

New test files:

```text
backend/internal/auth/auth_test.go
backend/internal/auth/jwks_test.go
backend/internal/auth/postgres_test.go
backend/internal/profile/validation_test.go
backend/internal/profile/postgres_test.go
backend/internal/profile/admin_test.go
backend/internal/profile/delete_test.go
backend/internal/api/auth_test.go
backend/internal/api/decode_test.go
backend/internal/api/profile_test.go
```

Focused modifications:

```text
backend/internal/config/config.go
backend/internal/config/config_test.go
backend/internal/database/pool.go
backend/internal/database/postgres_test.go
backend/internal/database/access_test.go
backend/internal/pgtest/pgtest.go
backend/internal/ratelimit/limit.go
backend/internal/ratelimit/limit_test.go
backend/internal/api/server.go
backend/internal/api/server_test.go
backend/cmd/api/main.go
backend/openapi.yaml
backend/contract_test.go
backend/go.mod
backend/go.sum
backend/.env.example
infra/render.yaml
backend/AGENTS.md
docs/architecture/backend.md
DevSteps.md
```

### Task 1: Profile Migration, Auth Fixture, and Readiness

**Files:**
- Create: `infra/supabase/migrations/0003_auth_profiles.sql`
- Modify: `backend/internal/pgtest/pgtest.go`
- Modify: `backend/internal/database/pool.go`
- Modify: `backend/internal/database/postgres_test.go`
- Modify: `backend/internal/database/access_test.go`
- Test: `backend/internal/database/postgres_test.go`
- Test: `backend/internal/database/access_test.go`

**Interfaces:**
- Consumes: migrations `0001` and `0002`, `pgtest.MigratedURL`.
- Produces: `profiles`, nullable retained `venues.submitted_by`, minimal `auth.sessions`, and readiness requirement `0003`.

- [ ] **Step 1: Write the failing migration/readiness tests**

Extend `TestMigrationVersions` to apply and record `0003_auth_profiles.sql`; assert `0002` remains `migrations_behind` and `0003` returns `(true, "ok")`. Extend `TestDataAPIClosed` with:

```go
`select * from profiles`,
`insert into profiles (id, username, display_name) values (
  '00000000-0000-4000-8000-000000000001', 'blocked', 'Blocked'
)`,
`update profiles set display_name = 'Blocked' where false`,
`delete from profiles where false`,
```

Add an owner-path assertion that deleting an `auth.users` row deletes its
Profile and idempotency row but retains its Venue with `submitted_by is null`.

- [ ] **Step 2: Run the tests to verify they fail**

Run:

```bash
cd backend
go test ./internal/database -run 'TestMigrationVersions|TestDataAPIClosed' -count=1
```

Expected: FAIL because migration `0003_auth_profiles.sql` and the required
`profiles` behavior do not exist.

- [ ] **Step 3: Extend the test Auth fixture**

In `pgtest.MigratedURL`, add Supabase's minimal session shape before applying
migrations:

```sql
create table if not exists auth.sessions (
  id         uuid primary key,
  user_id    uuid not null references auth.users (id) on delete cascade,
  created_at timestamptz not null
);
create index if not exists auth_sessions_user_id_idx on auth.sessions (user_id);
```

Add the same fixture to `TestMigrationVersions`.

- [ ] **Step 4: Add migration `0003_auth_profiles.sql`**

Use explicit constraint names so later migrations can strengthen them:

```sql
create table profiles (
  id uuid primary key references auth.users (id) on delete cascade,
  username text not null,
  display_name text not null,
  status text not null default 'active',
  moderation_reason text,
  moderation_note text,
  suspended_until timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint profiles_username_format check (
    username ~ '^[A-Za-z0-9_]{3,30}$'
  ),
  constraint profiles_display_name_length check (
    char_length(display_name) between 1 and 80
  ),
  constraint profiles_status_check check (
    status in ('active', 'rename_required', 'suspended')
  ),
  constraint profiles_moderation_note_length check (
    moderation_note is null or char_length(moderation_note) <= 1000
  ),
  constraint profiles_active_fields_check check (
    status <> 'active'
    or (
      moderation_reason is null
      and moderation_note is null
      and suspended_until is null
    )
  )
);

create unique index profiles_username_identity_key on profiles (lower(username));

alter table profiles enable row level security;
revoke all on table profiles from public, anon, authenticated, service_role;

alter table venues drop constraint venues_submitted_by_fkey;
alter table venues
  add constraint venues_submitted_by_fkey
  foreign key (submitted_by) references auth.users (id) on delete set null;
```

Do not add a Profile policy or sequence.

- [ ] **Step 5: Raise readiness and pass focused tests**

Change `requiredMigrationVersion` to `"0003"`, then run:

```bash
cd backend
go test ./internal/database ./internal/pgtest -count=1
```

Expected: PASS, or explicit `Docker is unavailable:` skips outside CI.

- [ ] **Step 6: Commit**

```bash
cd ..
git add infra/supabase/migrations/0003_auth_profiles.sql \
  backend/internal/pgtest/pgtest.go \
  backend/internal/database/pool.go \
  backend/internal/database/postgres_test.go \
  backend/internal/database/access_test.go
git commit -m "feat: add profile schema"
```

### Task 2: Supabase Configuration and Dependencies

**Files:**
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/config/config_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

**Interfaces:**
- Consumes: environment lookup and `.env` precedence.
- Produces: `Config.SupabaseURL`, `Config.SupabaseJWTAudience`, and `Config.SupabaseServiceRoleKey`.

- [ ] **Step 1: Write failing configuration tests**

Make the valid fixture include:

```go
"SUPABASE_URL":              "https://project.supabase.co/",
"SUPABASE_JWT_AUDIENCE":     "authenticated",
"SUPABASE_SERVICE_ROLE_KEY": "service-secret",
```

Assert the stored URL is `https://project.supabase.co`, and add table cases
for missing values, relative URLs, credentials in URLs, non-loopback HTTP, and
accepted `http://127.0.0.1:54321`.
Update every existing valid-load helper and `.env` fixture with these three
required settings so proxy, docs, port, and precedence tests continue to test
their intended behavior.

Expected errors:

```text
SUPABASE_URL is required
SUPABASE_URL must be an absolute HTTPS URL or a loopback HTTP URL
SUPABASE_JWT_AUDIENCE is required
SUPABASE_SERVICE_ROLE_KEY is required
```

Assert none of the supplied secret values appears in any error.

- [ ] **Step 2: Run the configuration tests to verify failure**

```bash
cd backend
go test ./internal/config -run 'TestLoadEnvironment|TestLoadRejectsEnvironment' -count=1
```

Expected: FAIL because the Supabase fields are absent.

- [ ] **Step 3: Implement parsing**

Add the fields to `Config`, require all three values, strip only trailing `/`
from the URL, parse with `net/url`, reject query/fragment/userinfo, and accept
HTTP only for `localhost`, `127.0.0.0/8`, or `::1`.

```go
type Config struct {
	Port                   string
	DatabaseURL            string
	DocsUIEnabled          bool
	TrustedProxies         []net.IPNet
	SupabaseURL            string
	SupabaseJWTAudience    string
	SupabaseServiceRoleKey string
}
```

- [ ] **Step 4: Add direct dependencies**

```bash
cd backend
go get github.com/golang-jwt/jwt/v5@v5.3.0
go get golang.org/x/text@v0.29.0
go mod tidy
```

Expected: both packages appear as direct requirements and the module stays on
Go 1.24.

- [ ] **Step 5: Run tests and commit**

```bash
cd backend
go test ./internal/config -count=1
git add internal/config/config.go internal/config/config_test.go go.mod go.sum
git commit -m "feat: configure Supabase authentication"
```

### Task 3: Reservation-Based Per-User Write Limiter

**Files:**
- Modify: `backend/internal/ratelimit/limit.go`
- Modify: `backend/internal/ratelimit/limit_test.go`

**Interfaces:**
- Consumes: canonical User UUID and deterministic `func() time.Time`.
- Produces:

```go
type UserLimiter struct
type Reservation struct
func NewUserLimiter(clock func() time.Time) *UserLimiter
func (l *UserLimiter) Reserve(userID string) (*Reservation, int, bool)
func (r *Reservation) Commit()
func (r *Reservation) Cancel()
```

- [ ] **Step 1: Write failing bucket tests**

Cover 60 immediate reservations, refusal of the 61st, one token after one
second, User isolation, commit, cancel restoring capacity, cancel being
idempotent, three-minute idle eviction, 10,000 fresh keys refusing a new User
with `Retry-After: 60`, and concurrent reserve/cancel without exceeding 60.

- [ ] **Step 2: Run the tests to verify failure**

```bash
cd backend
go test ./internal/ratelimit -run User -count=1
```

Expected: FAIL because `UserLimiter` is undefined.

- [ ] **Step 3: Implement the limiter**

Use a mutex-protected custom token bucket, not HTTP middleware:

```go
type userBucket struct {
	tokens   float64
	refilled time.Time
	lastUsed time.Time
}

type UserLimiter struct {
	mu    sync.Mutex
	clock func() time.Time
	keys  map[string]*userBucket
}

type Reservation struct {
	limiter *UserLimiter
	bucket  *userBucket
	once    sync.Once
}
```

Refill at one token per second, cap at 60, debit on successful reserve, and
restore one token on `Cancel`. `Commit` consumes the reservation permanently.
Both terminal methods use `sync.Once`. Keep all non-idle keys when enforcing
the 10,000-key bound; eviction runs only before inserting a new key.

Use these calculations under the mutex:

```go
elapsed := now.Sub(bucket.refilled).Seconds()
bucket.tokens = math.Min(60, bucket.tokens+elapsed)
bucket.refilled = now
retryAfter := max(1, int(math.Ceil(1-bucket.tokens)))
```

- [ ] **Step 4: Run package tests and commit**

```bash
cd backend
go test ./internal/ratelimit -count=1
git add internal/ratelimit/limit.go internal/ratelimit/limit_test.go
git commit -m "feat: add per-user write limiter"
```

### Task 4: JWKS Cache and Cryptographic Token Verification

**Files:**
- Create: `backend/internal/auth/auth.go`
- Create: `backend/internal/auth/jwks.go`
- Create: `backend/internal/auth/auth_test.go`
- Create: `backend/internal/auth/jwks_test.go`

**Interfaces:**
- Consumes: project URL, exact audience, `*http.Client`, clock.
- Produces:

```go
var ErrUnauthorized = errors.New("auth: unauthorized")
var ErrProvider = errors.New("auth: provider unavailable")

type Caller struct {
	UserID           string
	SessionID        string
	SessionCreatedAt time.Time
}

func New(
	projectURL string,
	audience string,
	pool *pgxpool.Pool,
	client *http.Client,
	clock func() time.Time,
) *Verifier
```

`Authenticate` is completed in Task 5; this task pins private token identity
verification through package tests.

- [ ] **Step 1: Write generated-key and fake-JWKS tests**

Use `rsa.GenerateKey`, `ecdsa.GenerateKey(elliptic.P256())`, and an
`httptest.Server`. Test RS256/ES256 success, expiry, nbf, issuer, audience,
missing/non-canonical UUID claims, and all Review Focus key-confusion cases.
Use fixed claims:

```go
sub := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
sessionID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
issuer := server.URL + "/auth/v1"
audience := "authenticated"
```

- [ ] **Step 2: Write failing JWKS lifecycle tests**

Count server calls and assert:

- first verification fetches once;
- a known key remains cached before ten minutes;
- expiry refreshes once;
- a new `kid` forces one refresh;
- 20 concurrent requests for the new `kid` produce one refresh;
- failed or malformed refresh does not replace the last successful set;
- an expired cache plus failed refresh returns `ErrProvider`;
- successful refresh without the key returns `ErrUnauthorized`.

- [ ] **Step 3: Run tests to verify failure**

```bash
cd backend
go test ./internal/auth -run 'JWT|JWKS' -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 4: Implement strict JWK parsing**

Parse only:

```text
kty=RSA, alg=RS256, non-empty kid, base64url n/e
kty=EC,  alg=ES256, crv=P-256, non-empty kid, base64url x/y
```

Reject duplicate `kid`, unsupported keys, invalid points, empty usable sets,
and key/algorithm mismatch. Keep parsing and cache state private.

- [ ] **Step 5: Implement cache refresh coalescing**

Use one mutex and condition variable:

```go
type keyCache struct {
	mu         sync.Mutex
	cond       *sync.Cond
	refreshing bool
	keys       map[string]key
	expiresAt  time.Time
}
```

Set `expiresAt = now.Add(10 * time.Minute)` only after a successful parse.
Waiters re-check the cache after the active refresh finishes. Never hold the
cache mutex while making the HTTP request.

- [ ] **Step 6: Implement claim verification**

Use `jwt.ParseWithClaims` with:

```go
jwt.WithValidMethods([]string{"RS256", "ES256"}),
jwt.WithIssuer(projectURL+"/auth/v1"),
jwt.WithAudience(audience),
jwt.WithExpirationRequired(),
jwt.WithTimeFunc(clock),
```

Use a private claims type embedding `jwt.RegisteredClaims` plus
`SessionID string`. Validate lowercase canonical UUID strings with a private
regular expression. Map every parse, signature, key, algorithm, and claim
failure to `ErrUnauthorized`; preserve `ErrProvider`.

- [ ] **Step 7: Pass tests and commit**

```bash
cd backend
go test ./internal/auth -run 'JWT|JWKS' -count=1
git add internal/auth/auth.go internal/auth/jwks.go \
  internal/auth/auth_test.go internal/auth/jwks_test.go
git commit -m "feat: verify Supabase JWTs"
```

### Task 5: Live Supabase Session Authentication

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Create: `backend/internal/auth/postgres_test.go`

**Interfaces:**
- Consumes: verified token identity and `auth.sessions`.
- Produces:

```go
func (v *Verifier) Authenticate(ctx context.Context, encoded string) (Caller, error)
```

- [ ] **Step 1: Write failing integration tests**

Using `pgtest.MigratedURL`, insert one `auth.users` row and matching
`auth.sessions` row. Assert `Authenticate` returns all three Caller fields.
Then assert `ErrUnauthorized` for a missing session and a session belonging to
another User. Drop or rename `auth.sessions` inside an isolated test and
assert the returned error is neither `ErrUnauthorized` nor `ErrProvider`.

- [ ] **Step 2: Run tests to verify failure**

```bash
cd backend
go test ./internal/auth -run Session -count=1
```

Expected: FAIL because `Authenticate` does not query the live session.

- [ ] **Step 3: Implement live-session lookup**

After cryptographic verification, run with a two-second child context:

```sql
select created_at
from auth.sessions
where id = $1 and user_id = $2
```

Map `pgx.ErrNoRows` to `ErrUnauthorized`. Wrap other database errors without
including the token. Return a `Caller` only after the query succeeds.

- [ ] **Step 4: Pass the full auth package and commit**

```bash
cd backend
go test ./internal/auth -count=1
git add internal/auth/auth.go internal/auth/postgres_test.go
git commit -m "feat: require live Supabase sessions"
```

### Task 6: Profile Values and Validation

**Files:**
- Create: `backend/internal/profile/profile.go`
- Create: `backend/internal/profile/validation.go`
- Create: `backend/internal/profile/validation_test.go`

**Interfaces:**
- Consumes: raw Profile commands.
- Produces:

```go
type Profile struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Patch struct {
	Username    *string
	DisplayName *string
}

type DeleteCommand struct {
	UserID           string
	SessionCreatedAt time.Time
	Confirmation     string
}

var ErrNotFound = errors.New("profile: not found")
var ErrUsernameTaken = errors.New("profile: username taken")
var ErrUsernameReserved = errors.New("profile: username reserved")
var ErrRecentAuthenticationRequired = errors.New("profile: recent authentication required")
var ErrAuthProvider = errors.New("profile: auth provider")

type ValidationError struct {
	Fields map[string]string
}

type RateLimitError struct {
	RetryAfter int
}

func (e *ValidationError) Error() string { return "profile: validation failed" }
func (e *RateLimitError) Error() string  { return "profile: rate limited" }
```

- [ ] **Step 1: Write failing Username tests**

Table-test preserved casing, case-insensitive ownership, an owner correction
from `sfilizZola` to `sfilizzola`, 3/30 boundaries, invalid punctuation and
non-ASCII, all reserved bases, underscore removal, digit lookalikes, both
`1→i` and `1→l`, skeletons containing `metalhops`, available `adminfan`, and
reserved `my_metalhops`. Assert malformed input
returns a `ValidationError` and reserved input returns `ErrUsernameReserved`.

- [ ] **Step 2: Write failing display-name tests**

Cover Unicode whitespace trimming, decomposed-to-NFC conversion, 1/80
character boundaries, invalid UTF-8 strings, null byte, LF, control
characters, whitespace-only input, and preservation of internal spaces.

- [ ] **Step 3: Run tests to verify failure**

```bash
cd backend
go test ./internal/profile -run Validation -count=1
```

Expected: FAIL because the package does not exist.

- [ ] **Step 4: Implement normalization**

Use `utf8.ValidString`, `strings.TrimSpace`, `norm.NFC.String`,
`utf8.RuneCountInString`, and `unicode.IsControl`. Username reservation must
compare against the small reserved set without generating all `2^n`
expansions: carry the possible reserved-prefix states while scanning each rune
and branch only for digit `1`.

The normalization entry points stay private and return normalized values:

```go
func normalizeUsername(value string) (string, error)
func normalizeDisplayName(value string) (string, error)
func reservedUsername(value string) bool
```

- [ ] **Step 5: Pass tests and commit**

```bash
cd backend
go test ./internal/profile -run Validation -count=1
git add internal/profile/profile.go internal/profile/validation.go \
  internal/profile/validation_test.go
git commit -m "feat: validate profiles"
```

### Task 7: Supabase Admin Hard-Delete Adapter

**Files:**
- Create: `backend/internal/profile/admin.go`
- Create: `backend/internal/profile/admin_test.go`

**Interfaces:**
- Consumes: validated project URL, service-role credential, shared HTTP client.
- Produces:

```go
type AdminClient struct
func NewAdminClient(projectURL, serviceRoleKey string, client *http.Client) *AdminClient
func (c *AdminClient) DeleteUser(ctx context.Context, userID string) error
```

- [ ] **Step 1: Write failing HTTP-adapter tests**

An `httptest.Server` must assert:

```text
DELETE /auth/v1/admin/users/{userID}?should_soft_delete=false
apikey: <service-role-key>
Authorization: Bearer <service-role-key>
```

Cover 200/204 success, timeout, non-2xx with a distinctive secret body, and a
server URL with a path prefix. Assert returned errors contain neither the key
nor provider body.

- [ ] **Step 2: Run tests to verify failure**

```bash
cd backend
go test ./internal/profile -run Admin -count=1
```

Expected: FAIL because `AdminClient` is undefined.

- [ ] **Step 3: Implement the Admin adapter**

Build the URL from the validated project URL and `url.PathEscape(userID)`.
Send no request body. Accept any 2xx status and close a bounded response body
without returning its contents. Return one opaque wrapped error for transport
or non-2xx failure.

```go
req, err := http.NewRequestWithContext(
	ctx,
	http.MethodDelete,
	c.baseURL+"/auth/v1/admin/users/"+url.PathEscape(userID)+"?should_soft_delete=false",
	nil,
)
req.Header.Set("apikey", c.serviceRoleKey)
req.Header.Set("Authorization", "Bearer "+c.serviceRoleKey)
```

- [ ] **Step 4: Pass tests and commit**

```bash
cd backend
go test ./internal/profile -run Admin -count=1
git add internal/profile/admin.go internal/profile/admin_test.go
git commit -m "feat: add Supabase Admin client"
```

### Task 8: Profile Persistence and User Deletion

**Files:**
- Create: `backend/internal/profile/postgres.go`
- Create: `backend/internal/profile/postgres_test.go`
- Create: `backend/internal/profile/delete.go`
- Create: `backend/internal/profile/delete_test.go`
- Modify: `backend/internal/profile/profile.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `*ratelimit.UserLimiter`, `*AdminClient`, clock,
  `Patch`, and `DeleteCommand`.
- Produces:

```go
type Service struct
func New(
	pool *pgxpool.Pool,
	writes *ratelimit.UserLimiter,
	admin *AdminClient,
	clock func() time.Time,
) *Service
func (s *Service) Get(ctx context.Context, userID string) (Profile, error)
func (s *Service) Patch(ctx context.Context, userID string, patch Patch) (Profile, error)
func (s *Service) Delete(ctx context.Context, cmd DeleteCommand) error
```

- [ ] **Step 1: Write failing create/update integration tests**

Cover absent `Get`, create requiring both fields, read, partial updates,
explicit unchanged values, an unchanged `PATCH` that leaves a stored
non-active status and its moderation fields in place, a casing-only Username
correction that stores `active`, clears moderation fields, changes
`updated_at`, and spends one write token, username collision, invalid values
before SQL, 61 state changes at one instant, and token restoration after a
constraint/database failure.

For Review Focus item 3, synchronize two goroutines creating the same User
Profile. Assert one row exists, neither response leaks SQL, and a following
sequence can still consume all capacity not committed by the losing request.

- [ ] **Step 2: Write failing deletion-flow tests**

Using the migrated database, create a User, live session, Profile,
idempotency row, and Venue Submission. Make the fake Admin handler delete the
`auth.users` row on success. Assert:

- exact confirmation and a session exactly ten minutes old succeed;
- a session ten minutes plus one nanosecond old returns
  `ErrRecentAuthenticationRequired`;
- deletion works without a Profile;
- success removes Profile/idempotency and nulls Venue `submitted_by`;
- provider failure retains every local row and restores limiter capacity;
- the 61st successful mutation is rate limited with the expected retry.

- [ ] **Step 3: Run tests to verify failure**

```bash
cd backend
go test ./internal/profile -run 'Get|Patch|Concurrent|Delete' -count=1
```

Expected: FAIL because `Service`, persistence, and deletion are undefined.

- [ ] **Step 4: Implement reads and row mapping**

Use one private projection and mapper:

```sql
select id, username, display_name, status, created_at, updated_at
from profiles
where id = $1
```

Map no row to `ErrNotFound`. Keep `pgx.Row` and database records private.

- [ ] **Step 5: Implement the owned create/update transaction**

Begin a transaction with an explicit timeout. Take a transaction-scoped
advisory lock for `"profile\x1f"+userID` before selecting, so concurrent
first-time PATCH requests for the same User cannot both observe no row. Select
the current row `for update`; validate the complete normalized state; return
an unchanged row before reserving. Reserve one User token, then insert or
update with the provided clock value. Commit the database transaction before
committing the reservation. Defer cancellation unless both commits succeed.

```sql
select pg_advisory_xact_lock(
  hashtextextended('profile' || chr(31) || $1, 0)
)
```

Map PostgreSQL unique violation on `profiles_username_identity_key` to
`ErrUsernameTaken`; wrap every other database error.

```go
reservation, retryAfter, ok := s.writes.Reserve(userID)
if !ok {
	return Profile{}, &RateLimitError{RetryAfter: retryAfter}
}
committed := false
defer func() {
	if !committed {
		reservation.Cancel()
	}
}()
```

- [ ] **Step 6: Implement deletion orchestration**

Validate confirmation, compare `SessionCreatedAt` with
`clock().Add(-10*time.Minute)`, reserve a User token, call the concrete Admin
client, cancel on failure, and commit the reservation only on success. Map
all Admin failures to `ErrAuthProvider`.

```go
if cmd.Confirmation != "DELETE MY ACCOUNT" {
	return &ValidationError{Fields: map[string]string{
		"confirmation": "must equal DELETE MY ACCOUNT",
	}}
}
if cmd.SessionCreatedAt.Before(s.clock().Add(-10 * time.Minute)) {
	return ErrRecentAuthenticationRequired
}
```

- [ ] **Step 7: Pass package tests and commit**

```bash
cd backend
go test ./internal/profile -count=1
git add internal/profile/profile.go internal/profile/postgres.go \
  internal/profile/postgres_test.go internal/profile/delete.go \
  internal/profile/delete_test.go
git commit -m "feat: persist profiles and delete users"
```

### Task 9: Protected Middleware, Safe JSON, and Profile HTTP Routes

**Files:**
- Create: `backend/internal/api/auth.go`
- Create: `backend/internal/api/auth_test.go`
- Create: `backend/internal/api/decode.go`
- Create: `backend/internal/api/decode_test.go`
- Create: `backend/internal/api/profile.go`
- Create: `backend/internal/api/profile_test.go`
- Modify: `backend/internal/api/server.go`
- Modify: `backend/internal/api/server_test.go`

**Interfaces:**
- Consumes:

```go
type Authenticator interface {
	Authenticate(context.Context, string) (auth.Caller, error)
}

type Profiles interface {
	Get(context.Context, string) (profile.Profile, error)
	Patch(context.Context, string, profile.Patch) (profile.Profile, error)
	Delete(context.Context, profile.DeleteCommand) error
}
```

- Produces:

```go
type Options struct {
	DocsEnabled bool
	OpenAPI     []byte
	Readiness   Readiness
	PublicLimit func(http.Handler) http.Handler
	Auth        Authenticator
	Profiles    Profiles
	Logger      *slog.Logger
}

func New(options Options) http.Handler
```

- [ ] **Step 1: Write failing strict-decoder tests**

Test accepted `application/json` and `application/json; charset=utf-8`;
rejected missing/text media types; unknown fields; explicit null visibility;
invalid UTF-8; empty body; arrays; trailing objects; exactly 65,536 bytes;
and 65,537 bytes returning a typed too-large result before partial decode.

- [ ] **Step 2: Write failing authentication middleware tests**

Use a focused fake authenticator. Cover no header, wrong scheme, empty token,
multiple Authorization headers, authenticator unauthorized/provider/internal
errors, Caller context, and no authenticator calls for operational or docs
routes.

- [ ] **Step 3: Write failing Profile route tests**

Use focused fakes to pin every status in the spec, request/response field
names, `Retry-After`, exact deletion confirmation, no Idempotency-Key
requirement, normalized data envelopes, and request IDs. Assert public IP
middleware runs before authentication.

For all three protected methods, pin `401`, authentication-storage `500`,
JWKS `502`, and public-limit `429`. Keep capability-specific Profile and
deletion statuses separate.

- [ ] **Step 4: Run tests to verify failure**

```bash
cd backend
go test ./internal/api -run 'Decode|Auth|Profile' -count=1
```

Expected: FAIL because the new adapters do not exist.

- [ ] **Step 5: Implement the strict decoder**

Read through `http.MaxBytesReader` with a 65,536-byte limit, detect
`*http.MaxBytesError`, validate raw bytes with `utf8.Valid`, then decode with:

```go
decoder := json.NewDecoder(bytes.NewReader(body))
decoder.DisallowUnknownFields()
```

Decode once into the destination and once into an empty sentinel to require
`io.EOF`. Return classified transport errors; do not return decoder strings
directly to clients.

- [ ] **Step 6: Implement authentication and Caller context**

Require exactly one Authorization header and parse one `Bearer` credential.
Use a private context key and accessor. Translate:

```text
auth.ErrUnauthorized -> 401 unauthorized
auth.ErrProvider     -> 502 auth_provider_error
other                -> 500 internal_error
```

- [ ] **Step 7: Implement Profile handlers and mappings**

Use a transport-only optional string because `*string` cannot distinguish an
omitted property from explicit JSON null:

```go
type optionalString struct {
	Set   bool
	Null  bool
	Value string
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(data, []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(data, &o.Value)
}
```

Reject a set-and-null field, then construct `profile.Patch` from set values.
Map `profile.Profile` into a transport response containing only `id`,
`username`, `display_name`, and `status`; do not expose timestamps in Session
5. Map classified capability errors exactly:

```text
ValidationError                    -> 400 validation_error
ErrUsernameReserved                -> 400 username_reserved
ErrNotFound                        -> 404 profile_not_found
ErrUsernameTaken                   -> 409 username_taken
ErrRecentAuthenticationRequired    -> 403 recent_authentication_required
RateLimitError                     -> 429 rate_limited + Retry-After
ErrAuthProvider                    -> 502 auth_provider_error
everything else                    -> 500 internal_error
```

Log wrapped provider, database, and unexpected errors once with the request
ID. Log no request body, token, key, URL credential, SQL, or provider body.

- [ ] **Step 8: Convert server construction to Options**

Register public routes directly and wrap only `/v1/me` with authentication.
Keep middleware order:

```text
access log -> request ID -> panic recovery -> public limit -> mux
```

Update existing tests to construct `Options` and remove the old contract bans
on `/v1` and bearer.

- [ ] **Step 9: Pass API tests and commit**

```bash
cd backend
go test ./internal/api -count=1
git add internal/api/auth.go internal/api/auth_test.go \
  internal/api/decode.go internal/api/decode_test.go \
  internal/api/profile.go internal/api/profile_test.go \
  internal/api/server.go internal/api/server_test.go
git commit -m "feat: expose authenticated profile routes"
```

### Task 10: Composition, Environment, and OpenAPI Contract

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/.env.example`
- Modify: `infra/render.yaml`
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract_test.go`

**Interfaces:**
- Consumes: all Session 5 production modules.
- Produces: runnable configured server and published bearer-protected contract.

- [ ] **Step 1: Write failing contract assertions**

Require:

```go
for _, want := range []string{
	"/v1/me",
	"type: http",
	"scheme: bearer",
	"username_reserved",
	"recent_authentication_required",
	"auth_provider_error",
} {
	if !bytes.Contains(OpenAPI, []byte(want)) {
		t.Fatalf("missing %q", want)
	}
}
```

Also assert `/healthz` and `/readyz` do not carry a security requirement.

- [ ] **Step 2: Run contract/build checks to verify failure**

```bash
cd backend
go test . -run TestOpenAPIDocument -count=1
go test ./cmd/api -count=1
```

Expected: contract test FAIL; composition either fails after Task 9's options
change or still lacks Session 5 modules.

- [ ] **Step 3: Compose the production modules**

In `main`, construct:

```go
httpClient := &http.Client{Timeout: 5 * time.Second}
userLimiter := ratelimit.NewUserLimiter(time.Now)
authenticator := auth.New(
	cfg.SupabaseURL,
	cfg.SupabaseJWTAudience,
	pool.Raw(),
	httpClient,
	time.Now,
)
admin := profile.NewAdminClient(
	cfg.SupabaseURL,
	cfg.SupabaseServiceRoleKey,
	httpClient,
)
profiles := profile.New(pool.Raw(), userLimiter, admin, time.Now)
```

If `database.Pool` does not expose its underlying `*pgxpool.Pool`, add a
narrow `Raw() *pgxpool.Pool` accessor; do not move product SQL into
`internal/database`.

Pass these values through `api.Options`. Keep current server timeouts and
shutdown order.

- [ ] **Step 4: Update environment templates**

Replace `SUPABASE_JWT_SECRET` with:

```text
SUPABASE_URL=
SUPABASE_JWT_AUDIENCE=authenticated
SUPABASE_SERVICE_ROLE_KEY=
```

In Render, make URL and service-role key `sync: false`; set audience to
`authenticated`. Leave `/healthz`, docs, and trusted proxies unchanged.

- [ ] **Step 5: Expand OpenAPI 3.1**

Add `components.securitySchemes.bearerAuth`, Profile and error schemas, the
three operations, exact request fields, response statuses, examples without
real credentials, and operation-level:

```yaml
security:
  - bearerAuth: []
```

Do not add top-level security, so operational/documentation routes remain
public. Document `429`, `500`, and authentication-provider `502` on every
protected operation; `DELETE` uses the same `502` schema for Admin deletion
failure.

- [ ] **Step 6: Pass build and contract tests**

```bash
cd backend
go test . ./cmd/api ./internal/config ./internal/api -count=1
go build ./cmd/api
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cmd/api/main.go internal/database/pool.go \
  .env.example ../infra/render.yaml openapi.yaml contract_test.go
git commit -m "feat: compose profile authentication"
```

### Task 11: Guidance and Full Local Verification

**Files:**
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`

**Interfaces:**
- Consumes: completed runnable Session 5 feature.
- Produces: contributor guidance and a locally verified branch.

- [ ] **Step 1: Update backend guidance**

Document:

- `internal/auth` and `internal/profile`;
- required migration `0003`;
- `SUPABASE_URL`, `SUPABASE_JWT_AUDIENCE`, and the server-only service-role
  key;
- every protected request's live `auth.sessions` lookup;
- burst 60 / one-per-second User write limit and reservation behavior;
- `/v1/me` routes and User-deletion retention;
- safe JSON rules.

In the architecture guide, replace the transitional legacy-JWT statement with
the implemented asymmetric JWKS design. Do not add an ADR.

- [ ] **Step 2: Format and inspect**

```bash
cd backend
gofmt -w cmd internal
test -z "$(gofmt -l .)"
```

Expected: the final command prints nothing and exits zero.

- [ ] **Step 3: Run all backend gates**

```bash
cd backend
go vet ./...
go test ./...
go build ./cmd/api
```

Expected: PASS. Integration tests may skip only with the explicit local
Docker-unavailable message; CI must run them.

- [ ] **Step 4: Review scope and secrets**

```bash
git diff --check
git status --short
git diff --stat dev...HEAD
git diff dev...HEAD -- . ':!backend/go.sum'
```

Confirm only Session 5 files changed; `.env`, binaries, tokens, keys, project
URLs, and database credentials are absent.

- [ ] **Step 5: Commit guidance**

```bash
git add AGENTS.md ../docs/architecture/backend.md
git commit -m "docs: explain profile authentication"
```

### Task 12: Operator Migration Gate and Session Completion

**Files:**
- Modify after operator proof: `DevSteps.md`

**Interfaces:**
- Consumes: hosted project link, Session 5 environment, committed migration `0003`.
- Produces: hosted migration proof, live-session permission proof, ready Session 5 process, and roadmap status.

- [ ] **Step 1: Hand the operator the exact startup commands**

The operator clears any old port-8080 listener, supplies the Session 5
environment, and runs:

```bash
cd backend
make run
```

The implementer does not run login, link, migration push, or Render deploy.

- [ ] **Step 2: Require pre-push readiness proof**

In a second terminal:

```bash
curl -sS http://localhost:8080/readyz
```

Expected:

```json
{"status":"not_ready","reason":"migrations_behind"}
```

- [ ] **Step 3: Prompt the operator to apply migration `0003`**

From the repository root:

```bash
supabase db push --workdir infra
```

If the linked direct connection fails and the existing session-pooler URL is
available, use port 5432:

```bash
supabase db push --workdir infra --db-url "$DATABASE_URL"
```

Never use the transaction pooler on port 6543. If the project is not linked,
stop and ask the operator to restore the existing link; do not guess a project
reference.

- [ ] **Step 4: Require schema and Auth permission proof**

Using the same configured database User:

```bash
psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 \
  -c "select version from supabase_migrations.schema_migrations order by version desc limit 1;" \
  -c "select id, user_id, created_at from auth.sessions limit 0;"
```

Expected: highest version `0003`; the zero-row session projection succeeds.
The operator must not print or paste `DATABASE_URL`.

- [ ] **Step 5: Require post-push readiness proof**

Against the same running Session 5 process:

```bash
curl -sS http://localhost:8080/readyz
```

Expected:

```json
{"status":"ok"}
```

If the first check was already ready, require both version `0003` and proof
that this listener was started from Session 5 after the old process stopped.
If objects exist without a `0003` history row, stop; add a later corrective
migration instead of editing recorded history.

- [ ] **Step 6: Mark Session 5 done only after proof**

Update the DevSteps header to Sessions 1–5 done and Session 6 next, set Session
5 and the implementation-sequence table to `Done`, and leave Sessions 6–14
unchanged.

- [ ] **Step 7: Commit the completion record**

```bash
git add DevSteps.md
git commit -m "docs: mark session 5 done"
```

- [ ] **Step 8: Final branch verification**

```bash
cd backend
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
git status --short
```

Expected: all checks pass and the worktree is clean.
