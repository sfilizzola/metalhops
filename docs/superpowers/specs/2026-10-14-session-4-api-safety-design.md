# Session 4: API safety and data boundary

**Date:** 2026-10-14
**Status:** Approved design, not yet implemented

## Goal

Make the Go API the only public way to read or write product data, make
creation retries safe for later routes, and apply a baseline per-IP abuse
limit before those routes exist. Session 4 of [DevSteps](../../../DevSteps.md)
adds migration `0002`, the idempotency helper, and the public rate limiter.

## Constraints

- Leave `infra/supabase/migrations/0001_init.sql` unchanged.
- Add no `/v1` route, no `Idempotency-Key` parsing, and no OpenAPI path.
- `GET /healthz` and `GET /readyz` stay in the contract and gain no `429`
  response. They are exempt from the limiter.
- Add `golang.org/x/time/rate`. DevSteps already approves it.
- This spec is committed on `dev`. Implementation uses a `feature/*` branch
  when coding starts.

## Approach

Add `internal/idempotency`, `internal/ratelimit`, and the shared PostGIS
harness `internal/pgtest`. `cmd/api` composes the limiter and passes its
middleware into `internal/api`. The idempotency package has no production
caller in this session.

Rejected alternatives:

- **Fold the table into `internal/database` and the limiter into
  `internal/api`.** The database package would own a product table, and the
  next limiter would grow inside the HTTP package.
- **An idempotency interface with an in-memory fake.** The fake cannot
  represent the advisory lock, the 100-row cleanup, or the primary key.
- **A temporary HTTP route that exercises retries.** The helper's tests are
  the caller until a real creation route exists.

## Packages

```text
cmd/api
  -> internal/config
  -> internal/database
  -> internal/ratelimit
  -> internal/api
```

`cmd/api` does not import `internal/idempotency`. Later authenticated `POST`
routes that create submissions, Reports, Check-ins, or moderation/audit
effects call `Execute` inside a transaction they begin and finish. Reads,
state-setting `PUT` and `PATCH` routes, and `DELETE` routes do not require
`Idempotency-Key`, because repeating them does not create another resource or
audit effect. There is no Go interface around `Execute`. PostgreSQL is the
only adapter that can prove the lock and the primary key.

An operation is one idempotent `POST` action, not one database outcome.
`Execute` checks only that its name matches `^[a-z0-9_]{1,64}$`. Each `POST`
route that requires a key uses one stable operation. A corrected Check-in uses
a new key under the same Check-in operation. Session 4 assigns no operation
names and adds no product route.

`internal/ratelimit` owns the token buckets, the client-IP walk, and the
middleware function. `internal/api` does not import it. `cmd/api` passes
`ratelimit.Middleware(limiter)` into `api.New`, and `api` runs that function
after it has set `X-Request-ID` and inside panic recovery:

```text
access log -> request ID -> panic recovery -> limiter -> mux
```

The clock is a `func() time.Time`. Production passes `time.Now`. Tests pass a
clock they control. The access log gains no fields.

`internal/pgtest` starts `postgis/postgis:16-3.5`. `EmptyURL` returns a
database with no application objects. `MigratedURL` creates `anon` and
`authenticated` as `NOLOGIN NOSUPERUSER` roles without `BYPASSRLS`, and
`service_role` as `NOLOGIN NOSUPERUSER BYPASSRLS`. It grants those three roles
`USAGE` on schema `public` and default `ALL` on future tables and sequences in
that schema, so a missing `REVOKE` still leaves `service_role` able to read.
It creates schema `auth`, table `auth.users (id uuid primary key)`, and a
stable SQL function `auth.uid()` that returns `null::uuid`, then applies every
`*.sql` file in `infra/supabase/migrations` in lexical order. Both functions
skip with a message that starts `Docker is unavailable:` when Docker fails and
`CI` is empty. A Docker failure with `CI` set fails the test. `cmd/api` does
not import this package.

`requiredMigrationVersion` in `internal/database` becomes `"0002"`.

## Data boundary

Add `infra/supabase/migrations/0002_api_safety.sql`.

Drop these policies by name, without `IF EXISTS`:

- `genres are public` on `genres`
- `verified venues are public` on `venues`
- `users submit pending venues` on `venues`
- `venue genres follow venue visibility` on `venue_genres`

RLS stays enabled on those tables. The migration does not enable `FORCE ROW
LEVEL SECURITY` and does not add a policy. It revokes all privileges on
`genres`, `venues`, `venue_genres`, and sequence `genres_id_seq` from
`PUBLIC`, `anon`, `authenticated`, and `service_role`. It does not try to
clear Supabase default privileges owned by another role. Every later migration
that creates a product table revokes `PUBLIC`, `anon`, `authenticated`, and
`service_role` on that table, after creating it, in the same file. A sequence
created in that file gets the same revoke after it is created. A table that
uses `gen_random_uuid()` adds no sequence revoke.

`idempotency_records`:

| Column | Rule |
| --- | --- |
| `user_id` | `uuid not null references auth.users (id) on delete cascade` |
| `operation` | `text not null`, check `^[a-z0-9_]{1,64}$` |
| `key` | `text not null`, length 8–128, every byte in `0x20`–`0x7E` |
| `request_hash` | `text not null`, check `^[0-9a-f]{64}$` |
| `response_status` | `smallint not null`, check 200–299 |
| `response_body` | `jsonb not null` |
| `created_at` | `timestamptz not null default now()` |
| `expires_at` | `timestamptz not null` |

The primary key is `(user_id, operation, key)`. Index
`idempotency_records_expires_at_idx` is on `expires_at`. RLS is enabled, with
no policies and without `FORCE ROW LEVEL SECURITY`. `PUBLIC`, `anon`,
`authenticated`, and `service_role` lose all privileges on this table.
Idempotency rows are not an author-only Data API read.

The server connection remains the table owner, so the Go API can read and
write while those roles cannot. The Render `DATABASE_URL` user is that owner.
Session 4 does not grant a second API role. `authenticator` and
`service_role` are not that user.

## Idempotency helper

```go
type Request struct {
    UserID    string
    Operation string
    Key       string
    Command   []byte
}

type Result struct {
    Status int
    Body   []byte
}

func Execute(ctx context.Context, tx pgx.Tx, now time.Time, req Request, fn func(context.Context) (Result, error)) (Result, error)
```

`UserID` is a lowercase canonical UUID (`8-4-4-4-12` hex). `ErrInvalid` and
`ErrReused` are sentinel errors. Callers use `errors.Is`.

`Execute` returns `ErrInvalid`, before using the transaction, when the user
ID, operation, or key fails its column check, when `Command` is empty, or
when `Command` is longer than 65536 bytes. A call with a bad key on an
already rolled-back transaction still returns `ErrInvalid`.

The request hash is the lowercase hex SHA-256 of `Command`. `Execute` does
not parse or normalize those bytes. The write route that calls `Execute`
passes the normalized validated command. Session 4 defines no normalization,
because it adds no such route.

Inside the open transaction, `Execute` deletes at most 100 expired rows,
including other Users' rows, whose `expires_at` is less than or equal to
`now`, ordered by `expires_at` ascending, using `FOR UPDATE SKIP LOCKED`. It
then takes `pg_advisory_xact_lock(hashtextextended($1, 0))` where `$1` is the
user ID, an ASCII Unit Separator (`0x1F`), the operation, another Unit
Separator, and the key. PostgreSQL `text` rejects a NUL byte. User IDs,
operation names, and keys cannot contain `0x1F`. Those deletions
stay in the caller's transaction. A rollback puts the rows back. Session 4
adds no separate cleanup transaction.

Under that lock, a row with `expires_at` greater than `now` and the same hash
is a replay: the callback is skipped, and `Execute` returns the stored status
and the stored JSON bytes. The same live key with a different hash returns
`ErrReused` and skips the callback. A missing row, or a row with `expires_at`
less than or equal to `now`, means the callback runs. That expired row is
deleted under the lock before the callback, including when the 100-row
cleanup did not delete it.

A row is live while `expires_at` is greater than the supplied `now`. The
insert sets `expires_at` to `now` plus 24 hours. A call at the original `now`
plus 24 hours minus one nanosecond replays. A call at the original `now` plus
24 hours runs the callback.

The callback runs only in the proceed case. `Execute` records its result only
when the callback returns a nil error and a status from 200 through 299 whose
body is valid JSON (`json.Valid`). The insert uses that body. `Execute`
returns the `response_body` bytes PostgreSQL stored, so the first result and
a later replay are the same bytes. A callback error is returned as-is and
leaves no row. A status outside 200–299 with a nil error is returned as-is
and leaves no row. Invalid JSON on a 2xx result returns `ErrInvalid` and
leaves no row.

`Execute` does not commit or roll back. The caller commits only when
`Execute` returns a nil error and a 2xx result. A callback error, a non-2xx
result, `ErrInvalid`, and `ErrReused` roll the transaction back. A replay
returns a nil error and the stored 2xx result, so the caller may commit that
transaction; the replay itself changed nothing. A caller rollback removes the
inserted row with the rest of the transaction, and the same key can call the
callback again. Each later write route tests a callback that writes and then
returns an error: neither the domain write nor an idempotency row remains.

## Public rate limit

`ratelimit.New(clock func() time.Time, proxies []net.IPNet) *Limiter` builds
one in-memory map guarded by a mutex. Each new key gets
`rate.NewLimiter(2, 120)`: the bucket starts full, holds 120 tokens, and
refills at 2 tokens per second. The 121st immediate request is refused. One
token is available 500ms later.

A known key updates its last-use time and consumes a token. A bucket whose
last use is 3 minutes or more before the clock time is idle. Idle eviction
runs only when a new key would be inserted. The map holds at most 10,000
keys. A new address evicts idle keys first. When the map is still full, that
new address is refused and every active bucket stays. Three minutes is longer
than the 60 seconds a bucket needs to refill, so eviction does not replace a
depleted bucket with a full one.

Token consumption uses `ReserveN(now, 1)`. A delay of zero keeps the
reservation. A positive delay cancels the reservation at `now`. `Retry-After`
is that delay rounded up to whole seconds, and at least 1. A refusal because
the map is full uses `Retry-After: 60`.

`Config.TrustedProxies` is `[]net.IPNet`. Unset or empty `TRUSTED_PROXIES`
parses as no proxies. Any other value is a comma-separated list of CIDRs,
with space trimmed around the whole value and around each entry. An empty
entry or a value `net.ParseCIDR` rejects stops startup with exactly
`TRUSTED_PROXIES must be a comma-separated list of CIDRs`. Duplicate CIDRs
are accepted.

The limit key of a parsed address is `ip.String()` after `To4` when the
address is IPv4 or IPv4-mapped. `[::ffff:203.0.113.9]:1234` and
`203.0.113.9:1234` share a bucket. The peer comes from `net.SplitHostPort`
and `net.ParseIP`. When that peer is outside `TRUSTED_PROXIES`,
`X-Forwarded-For` is ignored. When the peer is inside the list, the
middleware splits that header on commas, trims each entry, parses it with
`net.ParseIP`, and walks from the right. It skips unparseable entries and
addresses that are inside the list. The first remaining address is the
client. A trusted peer whose header yields no client address is limited as
that canonical peer IP. A `RemoteAddr` that does not yield an IP is limited
under the raw `RemoteAddr` string, and its forwarded header is ignored.

One pinned walk: peer `10.21.157.68:443` and
`X-Forwarded-For: 203.0.113.5, 198.51.100.10, 104.16.0.1` resolves to
`198.51.100.10`, because `10.21.157.68` is inside `10.0.0.0/8` and
`104.16.0.1` is inside `104.16.0.0/13`. The leftmost `203.0.113.5` is not
the client. Peer `203.0.113.8:1234` with `X-Forwarded-For: 198.51.100.10`
resolves to `203.0.113.8`.

`render.yaml` sets `TRUSTED_PROXIES` to this single quoted value, the
Cloudflare lists published on 14 October 2026 plus `10.0.0.0/8`.
`healthCheckPath` stays `/healthz`. Session 4 does not point it at
`/readyz`.

```text
173.245.48.0/20, 103.21.244.0/22, 103.22.200.0/22, 103.31.4.0/22, 141.101.64.0/18, 108.162.192.0/18, 190.93.240.0/20, 188.114.96.0/20, 197.234.240.0/22, 198.41.128.0/17, 162.158.0.0/15, 104.16.0.0/13, 104.24.0.0/14, 172.64.0.0/13, 131.0.72.0/22, 2400:cb00::/32, 2606:4700::/32, 2803:f800::/32, 2405:b500::/32, 2405:8100::/32, 2a06:98c0::/29, 2c0f:f248::/32, 10.0.0.0/8
```

`172.16.0.0/12` and `192.168.0.0/16` are not in that list. The process does
not fetch Cloudflare ranges at startup; the list changes only when this
pinned value changes. A caller that can open a TCP connection from
`10.0.0.0/8` can choose its rate-limit identity through `X-Forwarded-For`.
`backend/.env.example` sets `TRUSTED_PROXIES=` and comments that an empty
value uses the TCP peer.

`api.New` gains the middleware argument
`limit func(http.Handler) http.Handler`. Production passes
`ratelimit.Middleware(limiter)`. A test that is not checking the limit passes
a function that returns its handler unchanged.

The middleware allows `GET /healthz` and `GET /readyz` through without a
token. Every other method and path, including `POST /healthz`,
`/openapi.yaml`, `/docs`, and unknown paths, consumes one. A refusal sets
`Retry-After`, keeps the existing `X-Request-ID`, and writes the error
envelope with status `429`, code `rate_limited`, message `Too many requests.`,
and no `fields` member. The body uses `json.Encoder`, including its trailing
newline. A request that the public limiter allows still consumes one token when
the handler later replays a stored response. A refusal does not call
`Execute` and does not change a stored idempotency row. The per-User write
limit stays in Session 5: burst capacity 60 and a sustained rate of 1 write
per second. A replay does not consume a User write token. Session 4 does not
implement that limit.

## Tests

`internal/database` readiness uses `pgtest.EmptyURL`. It creates
`supabase_migrations.schema_migrations` and the `auth` fixture, including
`anon`, `authenticated`, and `service_role`, itself. An empty database is
`migrations_unavailable`. Recorded `0000` and recorded `0001` are
`migrations_behind`. Applying `0001_init.sql` and `0002_api_safety.sql`, then
recording `0002`, is ready. The shortcut comment that keeps the harness inside
the database tests is removed.

`internal/database/access_test.go` uses `MigratedURL`. The test grants `anon`,
`authenticated`, and `service_role` `USAGE` on schema `public`, then `SET ROLE`
to each role. `SELECT`, `INSERT`, `UPDATE`, and `DELETE` on `genres`,
`venues`, `venue_genres`, and `idempotency_records` fail with SQLSTATE
`42501`. `SELECT nextval('genres_id_seq')` fails with `42501`. The owner
connection inserts one genre, one `auth.users` row, one venue, and one
idempotency row.

`internal/idempotency` uses `MigratedURL` and a fixed `now`:

- The same key and command run the callback once. The second call returns the
  same status and the same body bytes. `request_hash` equals the lowercase
  hex SHA-256 of the command.
- The same live key with different command bytes returns `ErrReused` and does
  not run the callback.
- A callback status of `409` with a nil error leaves no row, and the next call
  runs the callback.
- A callback error leaves no row, and the next call runs the callback.
- A 2xx body that fails `json.Valid` returns `ErrInvalid` and leaves no row.
- The callback inserts into a probe table created outside the transaction and
  returns `201`. The caller rolls back. The next call runs the callback
  again. After that second call commits, the probe table contains one row.
- A call at `now` plus 24 hours minus one nanosecond replays. A call at `now`
  plus 24 hours runs the callback.
- One hundred and one unrelated rows expire two hours before `now`. The row
  for the key under test expires one minute before `now`. After `Execute`,
  one unrelated expired row remains, and the key has one live row.
- Two connections call `Execute` with the same key and command. The first
  callback waits until the second call has entered `Execute`, then the first
  transaction commits. The callback runs once, both results are equal, and
  the test fails if that takes longer than 5 seconds.
- A bad key, operation, user ID, empty command, or command of 65537 bytes
  returns `ErrInvalid` on an already rolled-back transaction. A command of
  65536 bytes with an otherwise valid request is recorded.
- Deleting the `auth.users` row deletes that user's idempotency rows.

`internal/config` covers an empty value, a valid comma-separated CIDR list,
duplicate CIDRs, and a value that returns the startup error above.
`internal/ratelimit` covers the bucket, the 500ms refill, separate addresses,
the two pinned peer and header examples, a trusted peer with an empty header
sharing that peer's canonical bucket, IPv4-mapped IPv6 sharing the IPv4
bucket, idle eviction after 3 minutes when the map is full, and a full map of
fresh keys refusing a new address with `Retry-After: 60` while an existing
address still uses its own bucket.

Existing `internal/api` server tests pass an identity middleware. One server
test uses the real limiter, an empty proxy list, and a readiness fake that
reports not ready. From one `RemoteAddr`, 120 `GET /openapi.yaml` requests
succeed, the 121st is `429` with `rate_limited`, `Too many requests.`, the
request ID, `Retry-After`, and the encoder's trailing newline. A following
`GET /healthz` is `200`, `GET /readyz` is `503` with the existing not-ready
body, and `POST /healthz` is `429`.

From `backend/`:

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass.

## Documentation

`backend/AGENTS.md` lists `internal/idempotency`, `internal/ratelimit`, and
`internal/pgtest`. It states that readiness requires migration `0002`, that
empty `TRUSTED_PROXIES` uses the TCP peer, and that `GET /healthz` and
`GET /readyz` are unlimited while every other route uses a token bucket with
burst capacity 120 and a sustained rate of 2 requests per second per client
IP.

`docs/architecture/backend.md` adds the two production packages to the module
map. `internal/pgtest` stays out of that map because production code does not
import it. The transitional bullet about `0001_init.sql` permitting direct
catalogue reads and Venue submissions is removed. The guide states that
migration `0002_api_safety.sql` closed those policies, and that authenticated
`POST` routes which require an `Idempotency-Key` call `idempotency.Execute`
inside their own transaction.

DevSteps stays "Session 4 is next" until the operator handoff below records
`0002` and `GET /readyz` returns 200. The API process does not apply SQL.

## Out of scope

- JWT verification, profiles, and the authenticated per-User write limit
  (burst capacity 60, sustained rate 1 write per second).
- `/v1` routes and parsing `Idempotency-Key`.
- OpenAPI path additions, including a `429` response on `/healthz` or
  `/readyz`.
- Author-only Data API read policies. ADR 0002 keeps those reads on the Go API.
- Daily submission and report quotas. A row takes a slot when it is created.
  Later rejection, correction, verification, delisting, or report resolution
  does not return that slot or take another. A later `429 quota_exceeded`
  sets `Retry-After` to the whole seconds until the oldest counted row in
  that rolling 24-hour window falls outside it, and at least 1.
- Editing `0001_init.sql`.
- The release job. Session 14 adds it. A release is successful only after the
  committed migrations are applied and `/readyz` returns 200.

## Operator handoff

The API process does not apply SQL. Session 4 is incomplete until migration
`0002` is recorded on the hosted Supabase project and a local `GET /readyz`
returns 200. Before the push, the operator starts the Session 4 binary and
confirms `/readyz` returns `migrations_behind`. After the push, that same
process must return 200. If the first response is already 200, check the
highest version in `supabase_migrations.schema_migrations`. `0001` means the
Session 3 binary is still running: stop it and start the Session 4 binary.
`0002` finishes the gate only when that 200 came from the process started
from this session after the old listener on port 8080 was stopped. The
history row does not identify the binary: once `0002` is recorded, the
Session 3 binary also returns 200. Any other 200 does not finish the session. The
project is already linked from Session 3. Once
`supabase_migrations.schema_migrations` contains `0002`,
`0002_api_safety.sql` is immutable even if the branch is not merged. A failed
push does not freeze the file. A normal failure leaves neither the `0002`
objects nor the version row, so the file may be corrected and pushed again.
If those objects exist without the version row, stop and report that state.
Do not edit the file to paper over it. A later correction is a new migration.
The implementation
plan lists the operator's `supabase db push --workdir infra` command, the
session-pooler fallback on port 5432, and the version check. The implementer
does not run `supabase login`, `supabase link`, or `supabase db push`. Do not
run `supabase db reset --linked` or `supabase start`. Do not apply `0002` in
the SQL editor. The transaction pooler on port 6543 is not used. `DATABASE_URL`
stays the session-pooler URL, and that user owns the tables. This handoff
does not deploy Render. The release job still applies `0002` before it
deploys the binary that requires `0002`. `healthCheckPath` stays
`/healthz`. Session 4 does not add the release job. When Session 14 adds
it, a release is successful only after the committed migrations are applied
and `/readyz` returns 200. Render's `/healthz` success is not that proof.

## Exit condition

Direct Data API reads and writes of the catalogue fail for `anon`,
`authenticated`, and `service_role`. `Execute` replays a matching retry,
rejects a conflicting retry, honors expiry, serializes concurrent callers,
and disappears with a rolled-back transaction. Every route except `GET /healthz` and `GET /readyz`
uses a token bucket with burst capacity 120 and a sustained rate of 2
requests per second per resolved client IP. After the operator handoff, the
hosted project records migration `0002`. The Session 4 process returns
`migrations_behind` before the push and 200 after it, unless the history
already contains `0002` and the first 200 came from the process started
after the old listener on port 8080 was stopped.
