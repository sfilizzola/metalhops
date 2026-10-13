# Backend (Go API) agent guide

REST API for MetalHops, hosted on Render and backed by Supabase (Postgres +
PostGIS). Run commands from `backend/`.

## Commands

    cp .env.example .env   # first time only
    make run               # http://localhost:8080/healthz
    make test              # go test ./...
    make vet               # go vet ./...
    make build             # bin/api

## Layout

- `cmd/api/`: composition root only (configuration, adapters, and server
  lifecycle).
- `internal/`: all domain code.
- `openapi.yaml`: contract for implemented HTTP routes.

Before adding backend packages or seams, read
[`docs/architecture/backend.md`](../docs/architecture/backend.md). Capability
modules are the primary shape. Handler, service, and repository roles do not
require separate packages, and a Go interface needs real variation rather than
layering ceremony.

## Planning a session

Every backend session plan follows the
[architecture guide](../docs/architecture/backend.md) and
[DevSteps](../DevSteps.md). The guide sets the module shape, and DevSteps sets
the scope. Each plan must state:

- which capability module owns each new behavior, and where its transactions
  are owned;
- any new seam or Go interface, plus the second adapter or real variation
  that justifies it (otherwise leave it out);
- the test surface: capability, HTTP, and persistence integration tests;
- the migrations, `openapi.yaml`, and `AGENTS.md` updates the session needs.

If a plan has to break a rule in the guide, update the guide in the same pull
request, or record an ADR under `docs/adr/` when the reason is load-bearing.
Never drift from it silently.

## Definition of done

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass; CI runs the same checks.
- Every new endpoint ships with a test.
- The change passes the review checks in the architecture guide.

## Dependencies

- Ask before adding any dependency to `go.mod`. The Go standard library needs
  no approval.
- Libraries suggested in the root README are candidates to propose, not
  defaults: `chi`, `pgx` + `sqlc`, `goose`, OpenAPI tooling,
  `testcontainers-go`.

## Shared paths backend work may change

- `infra/supabase/migrations/`: add a new numbered file (`0002_<name>.sql`,
  and so on). Never edit a migration that is already on `dev` or `main`; add
  a new one instead. Every new table enables row-level security and defines
  its policies in the same migration.
- `infra/render.yaml`: Render blueprint for this service.
- `.github/workflows/backend.yml`: backend CI.

## Auth model

- The apps sign in with Supabase Auth and send the JWT to this API, which
  verifies it.
- The Supabase service-role key exists only in server environment variables,
  never in the repo or the apps.
