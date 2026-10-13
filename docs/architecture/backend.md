# Backend architecture

This guide defines how the MetalHops Go backend is shaped as it grows. Read it
with [DevSteps](../../DevSteps.md), which owns product scope and implementation
order, and [ADR 0001](../adr/0001-product-data-through-go-api.md), which owns
the product-data decision.

The goal is not to prevent every future refactor. Requirements will change.
The goal is to keep change local by building deep modules around MetalHops
capabilities instead of repeating ceremonial layers.

## Architectural priorities

Use these priorities in order when design goals conflict:

1. Preserve the domain language in [CONTEXT.md](../../CONTEXT.md).
2. Keep business invariants and their transactions local to one module.
3. Keep each interface smaller than the implementation behind it.
4. Make production behavior testable through the same interface callers use.
5. Prefer the standard library and existing dependencies.
6. Add a seam only when behavior actually varies.

Clean code is a result of these constraints, not a package count. Small
functions and files are useful only when they improve locality or make an
interface easier to understand.

## System shape

```text
Android and future clients
          |
          | HTTPS + JSON + Supabase bearer token
          v
  HTTP module and adapters
          |
          v
  capability modules
  Profile | Venue | Review | Check-in
          |
          v
  PostgreSQL/PostGIS implementation
```

Supabase Auth is the identity provider. The Go HTTP interface is the only
public product-data interface. The server-owned PostgreSQL connection may
bypass RLS, so capability modules enforce visibility and authorship. RLS
remains a separately tested defense.

## Module map

The expected top-level shape is:

```text
backend/
├── cmd/api/                 # composition root only
├── internal/
│   ├── api/                 # HTTP server, routing, middleware, envelopes
│   ├── auth/                # caller resolution and session verification
│   ├── config/              # environment parsing and validation
│   ├── database/            # pool lifecycle and database readiness
│   ├── profile/             # Profile capability
│   ├── venue/               # Genre, discovery, and Venue capability
│   ├── review/              # Review and rating capability
│   └── checkin/             # Check-in capability
└── openapi.yaml             # implemented HTTP interface
```

This tree names ownership, not mandatory internal layers. A capability may use
several files while remaining one Go package and one module. Do not create a
`handler`, `service`, or `repository` package merely to reproduce this shape
inside every capability.

### `cmd/api`

The composition root:

- loads and validates configuration;
- opens long-lived adapters;
- composes capability modules and the HTTP module;
- starts the server and coordinates graceful shutdown.

It contains no route behavior, domain rule, SQL, response shape, or environment
default. Its tests should be limited to composition that cannot be exercised
through another module.

### Infrastructure modules

`internal/api` owns behavior shared by the whole HTTP interface: routing,
request IDs, panic recovery, structured logging, body limits, common response
envelopes, and operational routes.

`internal/config` converts environment strings into one validated configuration
value. Invalid required configuration fails at startup. Callers do not read
environment variables directly.

`internal/database` owns pool creation, timeouts, readiness, migration-version
checks, and shutdown. It does not own Venue, Profile, Review, or Check-in SQL.

`internal/auth` owns Supabase token verification, JWKS refresh, live-session
resolution, and caller identity. Authorization rules such as Venue authorship
remain in the capability that owns the rule.

### Capability modules

Capability modules are the primary architecture. Each module owns the complete
implementation of its domain behavior:

- domain values and validation;
- commands and results used by callers;
- visibility and authorship rules;
- transaction boundaries;
- SQL and record mapping;
- domain error classification;
- HTTP adaptation for its routes, unless keeping adapters in `internal/api`
  gives a smaller interface.

For example, the Venue module owns submission validation, Genre assignment,
duplicate detection, the advisory lock, the transaction, and safe duplicate
details. Splitting those rules among public packages would reduce locality.

File names may describe implementation roles:

```text
internal/venue/
├── venue.go                 # domain values and public module interface
├── submission.go            # submission behavior
├── discovery.go             # catalogue behavior
├── postgres.go              # SQL implementation
├── http.go                  # HTTP adapter
└── *_test.go
```

Files are not modules. Split a Go package only when the new package presents a
smaller interface and passes the deletion test.

## Dependency rules

Allowed dependencies point toward capability ownership:

```text
cmd/api
  -> api, auth, config, database, capability modules

api
  -> capability interfaces needed for routing

capability module
  -> standard library, pgx types where useful, shared value modules

capability module
  -X-> another capability's implementation
  -X-> cmd/api
  -X-> HTTP request or response types in domain behavior
```

Cross-capability behavior is coordinated by a module that owns the operation.
Do not create a generic `common`, `utils`, or `helpers` package. Code becomes
shared only after at least two real callers need the same behavior and can use
a smaller interface than duplicating it.

Import cycles indicate misplaced ownership. Resolve the ownership question;
do not hide the cycle behind an unowned interface package.

## Seams and adapters

An interface includes types, invariants, errors, ordering, configuration, and
performance expectations—not only a Go `interface` declaration.

Follow these rules:

1. Define a seam where behavior must vary without editing the caller.
2. Put a Go interface with the caller that consumes it.
3. Include only operations that caller needs.
4. Return domain values, not database rows or HTTP values.
5. Remove a seam whose only adapter is a pass-through.

One adapter is a hypothetical seam. Two adapters make it real. Valid pairs
include PostgreSQL and a focused in-memory adapter when both exercise the same
meaningful contract. A mock generated only to satisfy a preselected layer does
not justify a seam.

Apply the deletion test during review: if deleting a module makes complexity
disappear, delete it. If the complexity would spread into several callers, the
module is producing depth.

## Transactions and persistence

The capability that owns an invariant owns its transaction. It may call
private persistence helpers, but transaction ordering is not exposed to an
HTTP adapter or the composition root.

Required patterns:

- accept `context.Context` for blocking work;
- use explicit query and transaction timeouts;
- keep SQL with the capability whose language it uses;
- map database records before they cross the module interface;
- translate expected constraint failures into domain errors;
- return unexpected database errors without exposing details to clients;
- use PostgreSQL and PostGIS for durable concurrency and geographic rules;
- never apply migrations during process startup.

Add a repository-style seam only when a capability has two meaningful
persistence adapters or a caller needs a stable persistence interface.
Otherwise, direct private `pgx` implementation keeps the interface smaller.

Migrations are forward-only and immutable after reaching `dev` or `main`.
Every new table enables RLS and defines its policies in the same migration.
Expand-and-contract releases handle incompatible schema changes.

## HTTP adaptation

HTTP adapters parse and validate transport syntax, call one capability
operation, and translate its result. They do not decide domain policy.

The shared HTTP module owns:

- JSON and content-type rules;
- request size limits;
- request IDs and safe error envelopes;
- authentication middleware composition;
- pagination syntax shared by endpoints;
- liveness, readiness, and documentation routes.

Capability modules own field semantics, visibility, conflicts, and
authorization. OpenAPI describes only implemented routes. A route change is
complete only when implementation, tests, and `openapi.yaml` agree.

## Errors

Errors cross seams by meaning, not by storage or framework detail.

- Capability modules classify expected outcomes such as validation failure,
  duplicate Venue, missing Profile, forbidden contribution, or Check-in
  cooldown.
- HTTP adapters map those outcomes to the status and error codes specified in
  DevSteps.
- Infrastructure failures retain their cause for logs and become a safe
  internal error at the HTTP interface.
- Private resource lookups preserve the specified `404` behavior and never
  reveal existence through different errors.

Do not compare error-message strings. Do not return SQL, token, stack, or
provider details to clients.

## Configuration and process lifecycle

Go 1.23 is the backend toolchain version for source, CI, and container builds.

Configuration is parsed once. Each setting has one owner, type, default, and
validation rule. Secrets are never logged. Feature modules receive only the
configuration they need rather than the entire process environment.

The process starts in this order:

1. parse and validate configuration;
2. construct logging and clocks;
3. open the database pool and long-lived external adapters;
4. compose capability modules;
5. compose the HTTP module;
6. start serving.

Shutdown reverses ownership: stop accepting requests, drain active work within
the configured timeout, and close adapters. Liveness does not depend on the
database; readiness does.

The current `SUPABASE_JWT_SECRET` setting is transitional scaffolding. The
authentication session replaces it with issuer, audience, project URL, and
JWKS configuration before protected routes are implemented.

## Testing strategy

The interface is the test surface.

### Capability tests

Exercise domain behavior through the capability interface. Cover invariants,
visibility, authorship, transaction rollback, expected errors, and clock
edges. Prefer table-driven tests when cases share the same behavior.

Use a controllable clock where time changes results. Do not add sleeps.

### HTTP tests

Use `httptest` to verify transport behavior: routing, headers, decoding,
envelopes, authentication composition, and status mapping. A focused fake
capability adapter is valid when it proves the HTTP seam; it must not duplicate
domain behavior.

### Persistence integration tests

Use one test harness with the pinned PostGIS image. The harness creates the
minimal Supabase `auth` schema, applies every migration from an empty database,
and gives each test isolated state.

Test SQL where its semantics matter: PostGIS distances, stable pagination,
constraints, RLS, advisory locks, transaction rollback, and concurrent writes.
Do not mock SQL for these behaviors.

Local integration tests may skip with an explicit Docker-unavailable reason.
CI treats them as blocking.

### Architecture checks in review

For every new or changed module, reviewers ask:

- Which MetalHops capability owns this behavior?
- Is the interface smaller than the implementation?
- Where is the transaction owned?
- Does a seam have two meaningful adapters?
- What happens under the deletion test?
- Can the invariant be tested through the caller's interface?
- Did a database or HTTP type leak across the seam?
- Are OpenAPI, migrations, and agent guidance updated where required?

## Worked feature slice: Venue submission

The HTTP adapter decodes the authenticated request and idempotency key. The
Venue module validates the command, checks Profile contribution eligibility,
acquires the duplicate-detection lock, checks the durable quota, inserts the
pending Venue and Genre links, and records idempotency in one transaction. It
returns a Venue result or a classified domain error. The HTTP adapter maps that
result to the documented envelope.

This is one deep capability module even if the implementation spans several
files. Do not expose validation, duplicate detection, quota checking, and
record insertion as an ordered script that callers must assemble.

## Transitional state

The repository intentionally begins smaller than this target:

- `cmd/api` still owns `/healthz`; the backend-foundation session moves HTTP
  behavior behind the HTTP module.
- `go.mod` may lag the Go 1.23 toolchain until foundation work updates module
  metadata and dependencies together.
- migration `0001_init.sql` still permits direct catalogue reads and Venue
  submissions; the API-safety migration must close those policies before
  product clients are built.
- legacy JWT-secret configuration remains until the authentication session
  introduces asymmetric verification.
- `backend/contract.go` embeds `openapi.yaml` because `go:embed` cannot
  reference a parent directory. The backend-foundation session moves that
  embed with the HTTP routes.

These are named migration steps, not patterns to copy into new code.
