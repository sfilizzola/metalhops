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

`cmd/api` does not import `internal/idempotency`. Later creation routes call
`Execute` inside a transaction they begin and finish. Profile reports, Venue
submissions, Reviews, and Check-ins are those callers. There is no Go
interface around `Execute`. PostgreSQL is the only adapter that can prove the
lock and the primary key.

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
`authenticated` as `NOLOGIN NOSUPERUSER` roles without `BYPASSRLS`, creates
schema `auth`, table `auth.users (id uuid primary key)`, and a stable SQL
function `auth.uid()` that returns `null::uuid`, then applies every `*.sql`
file in `infra/supabase/migrations` in lexical order. Both functions skip with a
message that starts `Docker is unavailable:` when Docker fails and `CI` is
empty. A Docker failure with `CI` set fails the test. `cmd/api` does not
import this package.

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
`PUBLIC`, `anon`, and `authenticated`.

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
no policies and without `FORCE ROW LEVEL SECURITY`. The same three roles lose
all privileges on this table. Idempotency rows are not an author-only Data
API read.

The server connection remains the table owner, so the Go API can read and
write while those roles cannot. Author-only read policies wait for the
sessions that add the private rows they protect.

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

The request hash is the lowercase hex SHA-256 of `Command`.

Inside the open transaction, `Execute` deletes at most 100 rows whose
`expires_at` is less than or equal to `now`, ordered by `expires_at`
ascending, using `FOR UPDATE SKIP LOCKED`. It then takes
`pg_advisory_xact_lock(hashtextextended($1, 0))` where `$1` is the user ID,
a NUL byte, the operation, a NUL byte, and the key.

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

`Execute` does not commit or roll back. A caller rollback removes the inserted
row with the rest of the transaction, and the same key can call the callback
again.

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
Cloudflare lists published on 14 October 2026 plus `10.0.0.0/8`:

```text
173.245.48.0/20, 103.21.244.0/22, 103.22.200.0/22, 103.31.4.0/22, 141.101.64.0/18, 108.162.192.0/18, 190.93.240.0/20, 188.114.96.0/20, 197.234.240.0/22, 198.41.128.0/17, 162.158.0.0/15, 104.16.0.0/13, 104.24.0.0/14, 172.64.0.0/13, 131.0.72.0/22, 2400:cb00::/32, 2606:4700::/32, 2803:f800::/32, 2405:b500::/32, 2405:8100::/32, 2a06:98c0::/29, 2c0f:f248::/32, 10.0.0.0/8
```

`172.16.0.0/12` and `192.168.0.0/16` are not in that list. `backend/.env.example`
sets `TRUSTED_PROXIES=` and comments that an empty value uses the TCP peer.

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
newline. The per-user write limit stays in Session 5.

## Tests

`internal/database` readiness uses `pgtest.EmptyURL`. It creates
`supabase_migrations.schema_migrations` and the `auth` fixture, including
both `anon` and `authenticated`, itself. An empty database is
`migrations_unavailable`. Recorded `0000` and recorded `0001` are
`migrations_behind`. Applying `0001_init.sql` and `0002_api_safety.sql`, then
recording `0002`, is ready. The shortcut comment that keeps the harness inside
the database tests is removed.

`internal/database/access_test.go` uses `MigratedURL`. The test grants `anon`
and `authenticated` `USAGE` on schema `public`, then `SET ROLE` to each role.
`SELECT`, `INSERT`, `UPDATE`, and `DELETE` on `genres`, `venues`,
`venue_genres`, and `idempotency_records` fail with SQLSTATE `42501`.
`SELECT nextval('genres_id_seq')` fails with `42501`. The owner connection
inserts one genre, one `auth.users` row, one venue, and one idempotency row.

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
`GET /readyz` are unlimited while every other route allows 120 requests per
minute per client IP.

`docs/architecture/backend.md` adds the two production packages to the module
map. `internal/pgtest` stays out of that map because production code does not
import it. The transitional bullet about `0001_init.sql` permitting direct
catalogue reads and Venue submissions is removed. The guide states that
migration `0002_api_safety.sql` closed those policies, and that creation
routes call `idempotency.Execute` inside their own transaction.

DevSteps stays "Session 4 is next" until the implementation meets the exit
condition. The hosted database stays at `0001` until an operator applies
`0002`. This session does not run `supabase db push`. After the constant
changes, hosted `/readyz` returns `migrations_behind` until that push records
`0002`.

## Out of scope

- JWT verification, profiles, and the authenticated per-user write limit.
- `/v1` routes and parsing `Idempotency-Key`.
- OpenAPI path additions, including a `429` response on `/healthz` or
  `/readyz`.
- Author-only Data API read policies.
- Daily submission and report quotas.
- Editing `0001_init.sql`.

## Exit condition

Direct Data API reads and writes of the catalogue fail for `anon` and
`authenticated`. `Execute` replays a matching retry, rejects a conflicting
retry, honors expiry, serializes concurrent callers, and disappears with a
rolled-back transaction. Every route except `GET /healthz` and `GET /readyz`
is limited to 120 requests per minute per resolved client IP.
