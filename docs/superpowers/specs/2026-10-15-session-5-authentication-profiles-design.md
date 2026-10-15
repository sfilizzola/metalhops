# Session 5: Authentication and Profiles

**Date:** 2026-10-15
**Status:** Approved design, not yet implemented

## Goal

Add the first authenticated product slice to the MetalHops API. Session 5
verifies Supabase access tokens and live sessions, lets a User create and
maintain an active Profile, and lets that User permanently delete their
identity while retaining Venue Submissions without their Submitter.

The session adds:

- asymmetric JWT verification through Supabase JWKS;
- protected-route middleware and caller context;
- the Profile schema and `GET`, `PATCH`, and `DELETE /v1/me`;
- authenticated per-User write limiting;
- shared safe JSON request decoding;
- User deletion through the Supabase Admin API;
- bearer authentication and Profile operations in OpenAPI.

Session 6 remains responsible for Profile Reports, moderation actions,
Community Suspensions, rename requirements, public-identity redaction, and
operator authorization.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only.
- Do not edit migrations `0001` or `0002`.
- Product data remains available only through the Go API. Do not restore Data
  API grants or policies.
- Accept only asymmetric Supabase signing keys. A JWT signing secret is not
  required by the API.
- Add `golang-jwt/jwt/v5`, which DevSteps already approves.
- Add `golang.org/x/text/unicode/norm`, approved during this design, so
  display names can be normalized to Unicode NFC.
- Add no generic repository, service, common, or utility package.
- Do not implement Session 6 behavior early. Session 5 creates and updates
  active Profiles only.
- The API process never applies migrations.

## Approach

Use deep authentication and Profile modules:

```text
cmd/api
  -> config
  -> database
  -> auth
  -> profile
  -> ratelimit
  -> api

api
  -> narrow Authenticator and Profile capability interfaces

auth
  -> JWKS HTTP adapter
  -> private auth.sessions PostgreSQL query

profile
  -> private Profile PostgreSQL implementation
  -> Supabase Admin HTTP adapter
  -> per-User write limiter
```

`internal/auth` owns token verification, key refresh, live-session resolution,
and caller identity. `internal/profile` owns Profile rules, SQL, transaction
boundaries, and identity deletion. `internal/api` owns bearer extraction,
caller context, safe JSON decoding, route registration, and HTTP translation.
`internal/ratelimit` owns the in-memory per-User write buckets.

The interfaces consumed by `internal/api` are justified by two adapters: the
production capability and a focused HTTP-test fake. No interface wraps
Profile persistence, JWKS fetching, the Supabase Admin client, or the write
limiter merely to reproduce layers.

Rejected alternatives:

- **Put authentication and Profile orchestration in `internal/api`.** This
  couples transport, identity-provider behavior, and Profile policy.
- **Add handler, service, and repository packages.** PostgreSQL and Supabase
  each have one production adapter; extra layers would not hide meaningful
  variation.
- **Use OpenID discovery on every process start.** The Supabase project URL
  already determines the issuer and JWKS URL, so discovery adds another
  startup dependency without changing the trust decision.
- **Use a symmetric JWT secret.** Session 5 intentionally moves to key
  rotation through asymmetric JWKS.

No ADR is required because this design follows the existing architecture and
ADR 0002.

## Configuration

Remove the legacy `SUPABASE_JWT_SECRET` example and add these required
settings:

| Setting | Purpose |
| --- | --- |
| `SUPABASE_URL` | Base project URL used to derive Auth endpoints |
| `SUPABASE_JWT_AUDIENCE` | Exact accepted JWT audience |
| `SUPABASE_SERVICE_ROLE_KEY` | Server-only credential used for hard User deletion |

After removing a trailing slash from `SUPABASE_URL`, derive:

```text
issuer:   <SUPABASE_URL>/auth/v1
jwks:     <SUPABASE_URL>/auth/v1/.well-known/jwks.json
admin:    <SUPABASE_URL>/auth/v1/admin/users/{userID}
```

The URL must be absolute and use HTTPS, except that loopback HTTP is accepted
for local tests. Audience is non-empty. The service-role key is non-empty and
is never logged, included in an error, or copied into another configuration
value.

`backend/.env.example` documents local placeholders. `infra/render.yaml`
replaces the old JWT-secret entry with the three settings above using
`sync: false` for credentials and project-specific values. Existing settings
remain unchanged.

HTTP calls to JWKS and Supabase Admin use a shared long-lived client with an
explicit five-second timeout. The composition root passes modules only the
configuration they need.

## Authentication module

### Token verification

`internal/auth` accepts an encoded bearer token and returns:

```text
Caller
  UserID
  SessionID
  SessionCreatedAt
```

Verification checks, in order:

1. The token is structurally valid and has a non-empty `kid`.
2. `alg` is exactly `RS256` or `ES256`.
3. The key is present in the configured project's JWKS and matches the
   declared algorithm. ES256 keys must use P-256.
4. Signature, issuer, audience, expiration, and not-before are valid.
5. `sub` is a non-empty canonical UUID.
6. `session_id` is a non-empty canonical UUID.
7. `auth.sessions` contains that session ID for that User ID.

No clock-skew allowance is added beyond the JWT library's exact time checks.
Tests pass a controllable clock; production passes `time.Now`.

All client-controlled token failures return the same safe `401 unauthorized`
response. The response does not reveal whether the signature, claims, User,
or session failed. A missing or malformed bearer header has the same result.

Failure to query PostgreSQL is an internal `500`. Failure to fetch JWKS when a
refresh is required is `502 auth_provider_error`. A successful JWKS refresh
that still lacks the requested key is a client token failure and returns
`401`.

### JWKS cache and rotation

Fetch JWKS lazily on the first protected request. A successful document is
cached for ten minutes. At expiry, the next request refreshes it before
verification. An unknown `kid` forces one refresh even when the cache is
fresh.

Concurrent requests that need the same refresh share one in-flight fetch.
They do not each call Supabase. After a successful forced refresh, all waiters
use the new key set. Invalid JSON, duplicate key IDs, unsupported key types,
key/algorithm mismatches, and documents without usable keys fail the refresh.
A failed refresh is not cached and does not replace the last successful key
set. When refresh is required, verification fails closed rather than using an
expired cache.

The cache is private to `internal/auth`; no general cache interface or package
is added.

### Live sessions

Every protected request performs:

```sql
select created_at
from auth.sessions
where id = $1 and user_id = $2
```

The query returns one timestamp or no live session. It has the same explicit
query timeout pattern as other database work. The session timestamp is
included in `Caller` so Profile deletion can enforce recent authentication
without querying the same row twice.

The production migration does not create or alter Supabase's `auth.sessions`.
The PostGIS test harness adds the minimal `id`, `user_id`, and `created_at`
fixture needed by integration tests. The operator handoff verifies that the
configured production database User can read those columns.

## Protected HTTP composition

`internal/api` owns a narrow authenticator interface and protected middleware.
The middleware:

1. extracts exactly one `Authorization: Bearer <token>` credential;
2. calls the authenticator;
3. stores the resolved `Caller` in request context;
4. dispatches the protected handler.

Public operational and documentation routes never resolve identity. A missing
Profile does not block public discovery, and Session 5 adds no catalogue
route. Future public catalogue routes must not resolve caller identity. A
request with no token and a valid token see the same catalogue. That
catalogue contains Verified Venues and pending Venue Submissions whose
required fields are valid. A pending Venue is visibly not verified. Rejected
and Delisted Venues are excluded. A Review can target only a Verified Venue.
A Check-in can target a Verified Venue or a valid pending Venue. Session 5
protects only `/v1/me`. The middleware order is:

```text
access log
  -> request ID
  -> panic recovery
  -> public per-IP limit
  -> route mux
       -> protected authentication
       -> Profile handler
```

The existing public limiter therefore still consumes one token for protected
requests, including requests later rejected by authentication.

`api.New` moves to an options value rather than accumulating positional
arguments. The options contain documentation settings, readiness, public
middleware, authentication, and Profile route dependencies.

## Safe JSON decoding

Session 5 introduces the shared decoder required by all later JSON routes:

- require `Content-Type: application/json`; parameters such as `charset=utf-8`
  are accepted;
- limit the body to 64 KiB and return `413` when exceeded;
- reject bytes that are not valid UTF-8 before decoding;
- reject unknown object fields;
- require exactly one JSON object and reject trailing JSON values;
- preserve the distinction between omitted fields and explicit `null`;
- return field-specific `400 validation_error` responses where safe.

`PATCH /v1/me` and `DELETE /v1/me` both use this decoder. It remains in
`internal/api`; domain validation stays in `internal/profile`.

## Profile model

Migration `0003_auth_profiles.sql` creates:

```text
profiles
  id                   uuid primary key
  username             text not null
  display_name         text not null
  status               text not null default 'active'
  moderation_reason    text null
  moderation_note      text null
  suspended_until      timestamptz null
  created_at           timestamptz not null default now()
  updated_at           timestamptz not null default now()
```

`id` references `auth.users(id) on delete cascade`. A unique index on
`lower(username)` enforces case-insensitive identity. `status` is constrained to
`active`, `rename_required`, or `suspended`. Username checks require ASCII
matching `^[A-Za-z0-9_]{3,30}$` for its characters. The stored casing is
preserved, while uniqueness uses the case-folded value. Display name is limited to 80
characters. Moderation note is limited to 1,000 characters when present.

Session 5 always writes `status = 'active'` with all moderation fields null
on a real Profile update. An unchanged `PATCH` is a no-op: it leaves a stored
non-active status and its moderation fields as they are, does not change
`updated_at`, and does not spend a write token. `GET /v1/me`
returns the stored status, including a non-active status already in the row,
and omits moderation reason, note, and suspension expiry. Session 6 must replace that
write before it creates a `rename_required` or `suspended` Profile. The
migration includes the full Profile state columns selected during design,
but Session 6 owns moderation taxonomies, transitions, audit rows, expiry, and
state-specific constraints. Session 6 may strengthen those constraints in a
new migration; Session 5 does not guess them.

The migration also replaces the initial `venues.submitted_by` foreign key
with one referencing `auth.users(id) on delete set null`. This preserves Venue
Submissions when a User is deleted.

Enable RLS on `profiles`. Revoke all privileges on it from `PUBLIC`, `anon`,
`authenticated`, and `service_role`. Add no RLS policy: ADR 0002 requires
author-only reads to use the Go API, and a table without a Data API grant
needs no policy. The table uses no sequence.

Raise the binary's required migration version from `0002` to `0003`.

## Profile validation

### Username

Compare identity by ASCII case folding, but store and return the casing the
User submitted. The stored value must be 3–30 ASCII letters, digits, and
underscore. `MetalFan` and `metalfan` are the same Username. The owner may
correct `sfilizZola` to `sfilizzola`; that updates the same handle, changes
`updated_at`, spends one User write token, and does not return
`username_taken`. Submitting the exact stored casing is unchanged and spends
nothing. Another User cannot take any casing of it.
`metal_fan` and `metalfan` remain different Usernames.

Reject reserved platform identities with `400 username_reserved`. The base
reserved values are:

```text
admin
administrator
mod
moderator
metalhops
support
staff
official
```

Reservation comparison removes underscores and explores both interpretations
of `1` (`i` and `l`) while mapping `0→o`, `3→e`, `4→a`, `5→s`, and `7→t`.
Any resulting skeleton equal to a reserved value, or containing `metalhops`,
is reserved. This check runs after case folding and before persistence.
`ad_min` and `adm1n` are reserved. `adminfan` is available. `my_metalhops` is
reserved because its skeleton contains `metalhops`. Ordinary uniqueness still
treats an underscore as significant when the resulting handle is not protected.

### Display name

Validate that the input bytes were UTF-8 in the HTTP adapter. In the Profile
module, trim Unicode whitespace, normalize to NFC, and require 1–80 Unicode
characters. Reject null bytes, line feeds, and other non-whitespace control
characters. A display name is single-line. It is not checked for uniqueness,
reservation, or impersonation, so it may copy another Username or a protected
word.

The normalized display name is compared for no-op updates. Username no-op
comparison uses the exact stored casing, so a casing-only change is an update.
That correction stores `active`, clears moderation fields, changes
`updated_at`, and spends one write token. Omitted fields stay as stored. When the resulting Username and display name
match the stored values, the `PATCH` is a no-op even if the stored status is
not `active`. Both stored values are returned and later used by clients.

## Profile capability and transactions

`internal/profile` exposes domain operations for:

- getting the caller's Profile;
- creating or partially updating the caller's Profile;
- permanently deleting the caller's User identity.

Its private PostgreSQL implementation uses the existing pool directly. It
does not expose database records, transactions, or HTTP types.

### Read

`GET /v1/me` selects by caller User ID. A missing row is a classified
Profile-not-found result. The response returns the stored status. A non-active
row is not refused. Moderation reason, note, and suspension expiry are omitted.

### Create or update

`PATCH /v1/me` accepts:

```json
{
  "username": "metal_fan",
  "display_name": "Metal Fan"
}
```

Creating a User requires both non-null fields. A failed creation leaves no
Profile, so that sign-in is not yet a User. Updating an existing Profile requires
at least one non-null field and leaves omitted fields unchanged. Explicit
`null` is invalid. Normalize and validate the complete resulting Profile
before writing.

The Profile module owns one transaction:

1. lock the caller's existing Profile row when present;
2. derive and validate the complete normalized state;
3. return the current Profile immediately when the Username and display name
   are unchanged, including a stored non-active status;
4. reserve a per-User write token;
5. insert or update the Profile and `updated_at`;
6. commit the transaction;
7. commit the write-token reservation.

On validation, uniqueness, database, or commit failure, cancel the
reservation. A unique constraint race maps to `409 username_taken`. No-op
updates return `200` without changing `updated_at` or consuming a token.

The transaction belongs to `internal/profile`; neither the HTTP adapter nor
the composition root controls its order.

## Per-User write limit

Add a caller-keyed limiter to `internal/ratelimit` with:

- burst capacity 60;
- sustained refill of one token per second;
- the same three-minute idle threshold and 10,000-key bound as the public
  limiter;
- canonical User UUIDs as keys;
- deterministic clock tests;
- `Retry-After` rounded up to whole seconds and at least one.

The limiter returns a reservation that can be committed after a real state
change or canceled on failure. Cancellation restores the reserved capacity
without exceeding the bucket's maximum. Operations for one User are
serialized under the limiter's lock so cancellation and concurrent
reservations remain deterministic.

`GET` never reserves. Invalid, conflicting, no-op, and failed writes do not
consume a token. A successful Profile create/update and successful User
deletion consume one token. The bucket remains process-local because the MVP
runs one API instance.

A refusal returns `429 rate_limited` with `Retry-After`. It is separate from
the public per-IP bucket, which runs first.

## Self-service User deletion

`DELETE /v1/me` accepts exactly:

```json
{"confirmation":"DELETE MY ACCOUNT"}
```

Deletion remains available for an abandoned sign-in with no Profile. That
sign-in is not a User. The normal creation flow sends both required fields.
Session 6 must preserve deletion for moderated Profile states.

The Profile capability:

1. validates the exact confirmation;
2. requires the already resolved live session to have
   `SessionCreatedAt >= now - 10 minutes`;
3. reserves a per-User write token;
4. calls
   `DELETE <SUPABASE_URL>/auth/v1/admin/users/{userID}?should_soft_delete=false`;
5. sends the service-role key in both `apikey` and bearer authorization
   headers as required by Supabase;
6. returns success only for a confirmed 2xx provider response;
7. commits the token and returns `204`.

A valid but older session returns `403 recent_authentication_required`.
Provider timeout, transport failure, or non-2xx status returns
`502 auth_provider_error`, cancels the write-token reservation, and does not
claim success. Response bodies from Supabase are never relayed or logged.

The remote hard delete owns the database cascade; it cannot participate in a
local transaction. Migration `0003` ensures the resulting database behavior:

- Profile rows cascade delete;
- idempotency rows continue to cascade delete through migration `0002`;
- Venue Submissions remain and set `submitted_by` to null. No User can
  correct an unowned submission, and no later signup becomes its Submitter.

Later feature migrations implement the specified cascade or nulling behavior
for Reviews, Check-ins, Reports, restrictions, and moderation audit rows.

## HTTP contract

The OpenAPI document adds an HTTP bearer security scheme and applies it only
to protected operations.

### `GET /v1/me`

- `200` with `{"data": {"id", "username", "display_name", "status"}}`.
  `id` is the User id, the system's reference. Username is the public
  identifier people use. Moderation reason, note, and suspension expiry are
  omitted. Session 6 adds that feedback when rename and suspension exist.
  Every Profile created here has status `active`. A stored non-active status
  is returned when that row already exists.
- `401 unauthorized` for every missing or invalid authentication result.
- `404 profile_not_found` until the Profile is created. That sign-in is not
  yet a User.
- `429 rate_limited` with `Retry-After` when the public per-IP bucket refuses
  the request.
- `500 internal_error` when live-session storage is unavailable.
- `502 auth_provider_error` when a required JWKS refresh is unavailable.

### `PATCH /v1/me`

- `200` with the stored Profile when the Username and display name are
  unchanged, including a stored non-active status. A real update returns the
  normalized active Profile and clears moderation fields. A casing-only
  Username correction is that kind of update. A new Username
  replaces the previous one immediately and releases it, with no alias or
  history.
- `400 validation_error` for malformed fields.
- `400 username_reserved` for a protected Username.
- `401 unauthorized`.
- `409 username_taken`.
- `413` for a body above 64 KiB.
- `415` for unsupported content type.
- `429 rate_limited` with `Retry-After`.
- `500 internal_error`.
- `502 auth_provider_error` when a required JWKS refresh is unavailable.

The route does not require `Idempotency-Key`; repeating the normalized state
cannot create a second Profile.

### `DELETE /v1/me`

- `204` after Supabase confirms hard deletion.
- `400 validation_error` for bad confirmation.
- `401 unauthorized`.
- `403 recent_authentication_required`.
- `413` for a body above 64 KiB.
- `415` for unsupported content type.
- `429 rate_limited` with `Retry-After`.
- `500 internal_error`.
- `502 auth_provider_error`.

The route does not require `Idempotency-Key`.

Every error uses the shared envelope and request ID. No token, key, request
body, upstream body, SQL detail, or stack detail is returned.

## Error and failure behavior

| Failure | Result |
| --- | --- |
| Missing, malformed, expired, or untrusted token | `401 unauthorized` |
| JWT valid but live session absent | `401 unauthorized` |
| Required JWKS refresh unavailable | `502 auth_provider_error` |
| Session query unavailable | `500 internal_error` |
| Profile absent on `GET` | `404 profile_not_found` |
| Username already stored by another User | `409 username_taken` |
| Authenticated session older than ten minutes on delete | `403 recent_authentication_required` |
| User write bucket empty | `429 rate_limited` with `Retry-After` |
| Supabase Admin deletion fails | `502 auth_provider_error` |
| Profile transaction fails | Roll back and cancel the write reservation |

Logs retain wrapped internal causes with the request ID, but redaction tests
prove that bearer tokens, service-role keys, database URLs, and provider
response bodies are absent.

## Test strategy

### Authentication tests

A local `httptest` JWKS server and generated RSA and P-256 keys cover:

- valid RS256 and ES256 tokens;
- malformed tokens and signatures;
- rejected symmetric or unlisted algorithms;
- wrong issuer or audience;
- expired and not-yet-valid tokens;
- malformed, missing, or non-canonical `sub` and `session_id`;
- missing, revoked, and wrong-User live sessions;
- initial JWKS fetch, ten-minute expiry, unknown-key refresh, and rotation;
- one upstream fetch for concurrent refresh waiters;
- malformed or unsupported JWKS documents;
- safe behavior when JWKS is unavailable.

No test contacts Supabase.

### Profile capability tests

Exercise the capability rather than private helpers:

- create, read, partial update, and unchanged update;
- username casing, case-insensitive ownership, owner casing correction,
  shape, reservations, and uniqueness race;
- display-name trimming, NFC normalization, UTF-8 and control rejection;
- create requiring both fields and update requiring at least one;
- timestamp behavior;
- transaction rollback and write-reservation cancellation;
- token consumption only for successful state changes;
- recent-session deletion with and without a Profile;
- fake Supabase Admin success, timeout, and non-2xx response;
- secret and provider-body redaction.

### HTTP tests

Use focused auth and Profile fakes to cover:

- protected and public route composition;
- bearer parsing and caller context;
- every documented status, envelope, and header;
- JSON media type, 64 KiB boundary, invalid UTF-8, unknown fields, explicit
  nulls, and trailing values;
- no `Idempotency-Key` requirement;
- OpenAPI security only on protected routes.

### Persistence integration tests

Extend the PostGIS harness and integration suite to cover:

- all migrations from an empty database;
- readiness behind at `0002` and ready at `0003`;
- Profile constraints and unique Username races;
- Profile reads and writes through the owner connection;
- failed reads and writes for `PUBLIC`, `anon`, `authenticated`, and
  `service_role`;
- live-session lookup by both session and User ID;
- User deletion cascading Profile and idempotency rows;
- User deletion retaining a Venue Submission with null `submitted_by`.

Docker-unavailable behavior remains unchanged: skip locally with an explicit
message and fail in CI.

### Completion checks

From `backend/`:

```text
gofmt -l .
go vet ./...
go test ./...
```

Formatting must print nothing, and vet and tests must pass. The OpenAPI
contract test must prove that the three routes and bearer scheme are present.

## Files and documentation

Expected production additions:

```text
backend/internal/auth/
backend/internal/profile/
infra/supabase/migrations/0003_auth_profiles.sql
```

Expected updates include:

- `backend/cmd/api/main.go`;
- `backend/internal/api`;
- `backend/internal/config`;
- `backend/internal/database`;
- `backend/internal/pgtest`;
- `backend/internal/ratelimit`;
- `backend/openapi.yaml`;
- `backend/go.mod` and `backend/go.sum`;
- `backend/.env.example`;
- `infra/render.yaml`;
- `backend/AGENTS.md`.

Update `backend/AGENTS.md` with the new packages, required migration `0003`,
authentication configuration, protected-route behavior, and per-User write
limit. Update the architecture guide only if implementation must depart from
this approved shape. Add an ADR only if that departure is load-bearing.

## Operator handoff and completion gate

Migration `0003` must be recorded on the hosted Supabase project before
Session 5 is complete. The API process does not apply it, and the implementer
does not run `supabase login`, `supabase link`, or `supabase db push`.

The operator first ensures no older process owns port 8080, supplies the
Session 5 environment, and starts the Session 5 binary:

```bash
cd backend
make run
```

In another terminal, before pushing the migration:

```bash
curl -sS http://localhost:8080/readyz
```

The expected response is:

```json
{"status":"not_ready","reason":"migrations_behind"}
```

From the repository root, use the already linked Supabase project:

```bash
supabase db push --workdir infra
```

If the project is not linked, stop. Do not guess a project reference or put a
database password in chat. If the normal push cannot use the direct
connection, use the existing session-pooler URL on port 5432, never the
transaction pooler on port 6543:

```bash
supabase db push --workdir infra --db-url "$DATABASE_URL"
```

Verify the migration and the live-session read using the same configured
database User:

```bash
psql "$DATABASE_URL" -X -v ON_ERROR_STOP=1 \
  -c "select version from supabase_migrations.schema_migrations order by version desc limit 1;" \
  -c "select id, user_id, created_at from auth.sessions limit 0;"
```

The highest version must be `0003`, and the second query must succeed. Do not
print `DATABASE_URL`.

Finally, query the same Session 5 process:

```bash
curl -sS http://localhost:8080/readyz
```

The expected response is:

```json
{"status":"ok"}
```

If the first readiness response was already `200`, confirm that the highest
recorded version is `0003` and that the responding process was started from
Session 5 after the old listener was cleared. Migration history identifies
the schema, not the binary.

Once `supabase_migrations.schema_migrations` records `0003`,
`0003_auth_profiles.sql` is immutable. A normal failed push that records no
version does not freeze it. If `0003` objects exist without the version row,
stop and report that state; do not edit the migration to conceal it. A later
correction uses a new migration.

The operator replies with the readiness result and confirms version `0003`
and the `auth.sessions` read. Only then may DevSteps mark Session 5 done.

## Out of scope

- Profile Reports and moderation audit rows.
- Rename-required and suspended behavior.
- Operator recognition or privileged moderation routes.
- Public Profile attribution and redaction.
- Venue, Review, Report, Check-in, and public-catalogue routes. The catalogue
  and report rules decided during this design stay in DevSteps for later
  sessions.
- Idempotency-key parsing or new idempotent `POST` operations.
- Daily contribution quotas.
- A second API database role or direct author Data API access.
- OpenID discovery.
- Migration application at process startup.
- The release job, Render deployment, or beta certification.

## Exit condition

Valid Supabase identities with live sessions can get, create, and update an
active Profile. Invalid identities fail consistently. A recently
authenticated User can hard-delete their identity; Profile and idempotency
rows disappear while Venue Submissions remain without a Submitter. Protected
writes use the per-User rate limit, and OpenAPI describes the implemented
bearer-protected contract.

Formatting, vet, unit tests, integration tests, migration tests, and contract
tests pass. The hosted project records migration `0003`, the configured
database User can read the required `auth.sessions` columns, and the Session 5
process returns ready.
