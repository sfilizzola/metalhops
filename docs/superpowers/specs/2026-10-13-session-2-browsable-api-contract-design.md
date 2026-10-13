# Session 2: Browsable API contract

**Date:** 2026-10-13
**Status:** Approved design, not yet implemented

## Goal

Let a local browser open the implemented API contract and send `GET /healthz`
from Scalar. Session 2 of [DevSteps](../../../DevSteps.md) adds the OpenAPI
document, `GET /openapi.yaml`, and the Scalar `/docs` page.

## Constraints

- Document only routes the running API implements. This session documents
  `GET /healthz`.
- Leave `/openapi.yaml`, `/docs`, and the bearer security scheme out of the
  contract. Session 5 adds the scheme with the first protected route.
- Keep the routes in `cmd/api`. Session 3 introduces `internal/api` and
  `internal/config`.
- Add no Go dependency. Scalar stays a pinned CDN script.
- `DOCS_UI_ENABLED` defaults to enabled when unset. Render sets `false`.
- This spec is committed on `dev`. Implementation uses a `feature/*` branch
  when coding starts.

## Approach

Embed `backend/openapi.yaml` and serve it from the existing server.
`go:embed` can only include files beside or below the Go source, so
`backend/contract.go` (`package contract`) embeds the contract and `cmd/api`
imports `github.com/metalhops/metalhops/backend`. The Scalar page lives at
`backend/cmd/api/docs.html` and is embedded from `main`.

Rejected alternatives:

- **Add `internal/api` now and leave `/healthz` in `cmd/api`.** Routing would
  be split across two packages until Session 3.
- **Pull the Session 3 foundation forward.** Configuration and the HTTP module
  would land in the docs session.

## Behavior

`GET /healthz` stays `{"status":"ok"}`.

`GET /openapi.yaml` is always registered. It returns the embedded file with
`Content-Type: application/yaml`. The body is the file bytes, with no JSON
envelope.

`GET /docs` depends on `DOCS_UI_ENABLED`. Comparison is exact and
case-sensitive, with no trimming:

- Empty or `true` serves the page. `.env.example` sets `true`.
- `false` leaves the route unregistered, so the server returns `404`.
  `infra/render.yaml` sets `false`.
- Any other value, including `TRUE`, `1`, and ` true`, stops the process at
  startup with `DOCS_UI_ENABLED must be true, false, or empty`.

`main` parses the variable before it listens. The parser is:

```go
func docsUIEnabled(value string) (bool, error) {
    switch value {
    case "", "true":
        return true, nil
    case "false":
        return false, nil
    default:
        return false, fmt.Errorf("DOCS_UI_ENABLED must be true, false, or empty")
    }
}
```

```go
enabled, err := docsUIEnabled(os.Getenv("DOCS_UI_ENABLED"))
if err != nil {
    log.Fatal(err)
}
```

`newMux(docsEnabled bool)` registers `/healthz` and `/openapi.yaml` on every
call, and registers `/docs` only when `docsEnabled` is true.

If the CDN is unavailable, `/docs` still returns its HTML. `/healthz` and
`/openapi.yaml` keep working.

## Files

| File | Change |
| --- | --- |
| `backend/openapi.yaml` | New contract |
| `backend/contract.go` | Embeds the contract |
| `backend/cmd/api/docs.html` | Scalar page |
| `backend/cmd/api/main.go` | Env check and the two routes |
| `backend/cmd/api/main_test.go` | Handler and parser tests |
| `backend/.env.example` | `DOCS_UI_ENABLED=true` |
| `backend/AGENTS.md` | Documentation-routes section |
| `backend/README.md` | Point `make run` at `/docs` |
| `infra/render.yaml` | `DOCS_UI_ENABLED=false` |
| `docs/architecture/backend.md` | Transitional embed note |
| `DevSteps.md` | Move the bearer scheme to Session 5 |

No migration, `go.mod` change, Dockerfile change, or CI change. The contract
is inside the binary, so the distroless image needs no extra file copy.

### `backend/contract.go`

```go
package contract

import _ "embed"

// shortcut: module-root embed because go:embed cannot see a parent directory; move with the HTTP routes in Session 3.
//go:embed openapi.yaml
var OpenAPI []byte
```

### `backend/openapi.yaml`

```yaml
openapi: 3.1.0
info:
  title: MetalHops API
  version: 0.1.0
  description: Routes the running API implements.
servers:
  - url: http://localhost:8080
paths:
  /healthz:
    get:
      operationId: getHealth
      summary: Process health
      responses:
        "200":
          description: The process is running.
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                required: [status]
                properties:
                  status:
                    const: ok
              example:
                status: ok
```

`info.version` stays `0.1.0`. This session defines no version-bump rule.

### `backend/cmd/api/docs.html`

`.editorconfig` gives HTML a 4-space indent. The page is:

```html
<!doctype html>
<html>
    <head>
        <meta charset="utf-8" />
        <title>MetalHops API</title>
        <meta name="viewport" content="width=device-width, initial-scale=1" />
    </head>
    <body>
        <script
            id="api-reference"
            type="module"
            data-url="/openapi.yaml"
            src="https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js"
            integrity="sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6"
            crossorigin="anonymous"></script>
    </body>
</html>
```

The hash is the SHA-384 of that exact 995-byte `@scalar/api-reference@1.74.0`
entry file. The entry hardcodes `1.74.0` and imports the matching bundle from
jsDelivr. The integrity attribute covers the entry file. The page sets no
proxy URL, so Scalar sends requests to `http://localhost:8080`.

`GET /docs` sets `Content-Type: text/html; charset=utf-8` and this policy:

```text
default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'
```

`style-src` allows inline styles because Scalar 1.74.0 writes inline `style`
attributes. Scripts load from jsDelivr, fonts load from `fonts.scalar.com`,
and API calls stay on the page origin.

### Configuration snippets

Add this line to `backend/.env.example`:

```text
# Scalar UI at /docs. Unset or true enables it. Render sets false.
DOCS_UI_ENABLED=true
```

Add this entry under `envVars` in `infra/render.yaml`:

```yaml
- key: DOCS_UI_ENABLED
  value: "false"
```

### Guide updates

Add this section to `backend/AGENTS.md` after the Layout section and before Planning a session:

```markdown
## Documentation routes

`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

Both routes live in `cmd/api` until Session 3 moves HTTP behavior. The
contract embed is `backend/contract.go`.
```

In `backend/README.md`, change the `make run` comment to
`http://localhost:8080/docs`.

In `docs/architecture/backend.md`, add this bullet to Transitional state:

```markdown
- `backend/contract.go` embeds `openapi.yaml` because `go:embed` cannot
  reference a parent directory. The backend-foundation session moves that
  embed with the HTTP routes.
```

In `DevSteps.md`, replace the bearer-scheme paragraph under Browser API
navigation with:

```markdown
The contract documents only implemented routes. Session 2 has no protected
routes, so it declares no bearer security scheme. The authentication session
adds that scheme when the first protected route exists. Examples contain no
real credentials or production hostnames.
```

In the Session 5 paragraph, add this sentence after the User-deletion
sentence:

```markdown
The OpenAPI document gains the bearer security scheme on those protected
routes.
```

## Tests

`cmd/api/main_test.go` covers:

- `GET /healthz` through `newMux(false)` still returns `{"status":"ok"}`.
- `GET /openapi.yaml` returns `200`, `Content-Type: application/yaml`, and the
  `contract.OpenAPI` bytes. The body contains `openapi: 3.1.0`, `/healthz`,
  and `http://localhost:8080`. It contains neither `/v1` nor `bearer`.
- With `newMux(true)`, `GET /docs` returns `200`,
  `Content-Type: text/html; charset=utf-8`, the CSP header above, the pinned
  script URL, the integrity hash, and `data-url="/openapi.yaml"`.
- With `newMux(false)`, `GET /docs` returns `404`. The test asserts the
  status only. `GET /openapi.yaml` still returns `200`.
- `docsUIEnabled` accepts empty and `true`, accepts `false`, and rejects
  `TRUE`, `1`, and ` true` with the startup error text.

From `backend/`:

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass.

Confirm in a browser: run the API, open `/docs`, and send `GET /healthz` from
Scalar. The response is `{"status":"ok"}`. Restart with `DOCS_UI_ENABLED=false`
and confirm `/docs` is `404` while `/openapi.yaml` is still served.

## Out of scope

- `internal/api`, `internal/config`, and moving `/healthz`.
- The Go 1.23 `go.mod` bump.
- A bearer security scheme, product routes, and protected-route examples.
- A self-hosted Scalar bundle.
- Database, migration, and integration tests.

## Exit condition

A local browser can open `/docs`, read the `GET /healthz` contract, and
receive `{"status":"ok"}` from the running service. With
`DOCS_UI_ENABLED=false`, `/docs` returns `404` and `/openapi.yaml` still
returns the contract.
