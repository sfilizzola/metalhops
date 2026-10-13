# Session 2 Browsable API Contract Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve the implemented OpenAPI contract and a local Scalar page for `GET /healthz`.

**Architecture:** `backend/openapi.yaml` is the contract. `backend/contract.go` embeds it because `go:embed` cannot see a parent directory, and `cmd/api` serves that embed at `GET /openapi.yaml`. `GET /docs` serves a static HTML page embedded beside `main` when `DOCS_UI_ENABLED` allows it.

**Tech Stack:** Go standard library (`net/http`, `go:embed`, `httptest`). OpenAPI 3.1. Scalar `@scalar/api-reference@1.74.0` from jsDelivr. No new Go dependency.

**Spec:** `docs/superpowers/specs/2026-10-13-session-2-browsable-api-contract-design.md`

## Global Constraints

- Document only routes the running API implements. This session documents `GET /healthz`.
- Leave `/openapi.yaml`, `/docs`, and the bearer security scheme out of the contract. Session 5 adds the scheme with the first protected route.
- Keep the routes in `cmd/api`. Session 3 introduces `internal/api` and `internal/config`.
- Add no Go dependency. Scalar stays a pinned CDN script.
- `DOCS_UI_ENABLED` defaults to enabled when unset. Render sets `false`.
- Comparison of `DOCS_UI_ENABLED` is exact and case-sensitive, with no trimming. Empty or `true` enables the page. `false` leaves `/docs` unregistered. Any other value stops the process with `DOCS_UI_ENABLED must be true, false, or empty`.
- `GET /openapi.yaml` returns `Content-Type: application/yaml` and the embedded file bytes.
- `GET /docs`, when enabled, returns `Content-Type: text/html; charset=utf-8` and `Content-Security-Policy: default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`.
- The script URL is `https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js` and the integrity value is `sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6`.
- The module path `github.com/metalhops/metalhops/backend` declares `package contract`. Call `contract.OpenAPI`, not `backend.OpenAPI`.
- Do not edit `go.mod`, `go.sum`, the Dockerfile, or CI.
- Do not create `internal/api` or `internal/config`.
- Work on `feature/browsable-api-contract`, targeting `dev` by pull request.
- Commands in project files use plain tools (`git`, `go`, `make`), never a personal CLI wrapper.
- Run Go commands from `backend/`.

## Review Focus

- `DOCS_UI_ENABLED=false` still serves `/docs`. Expected: `404`, while `/openapi.yaml` stays `200`. Pinned in Task 4.
- `TRUE`, `1`, or ` true` enables the page. Expected: `docsUIEnabled` returns `DOCS_UI_ENABLED must be true, false, or empty`, and `main` exits before listen. Pinned in Tasks 3 and 4.
- The contract advertises `/v1`, a bearer scheme, or the `{"data":` envelope. Expected: the served YAML contains none of those, and its bytes equal `contract.OpenAPI`. Pinned in Task 2.
- The docs page loads a Scalar proxy or example host, or omits `crossorigin`, so the browser blocks the script or sends requests elsewhere. Expected: `data-url="/openapi.yaml"`, `crossorigin="anonymous"`, and neither `proxy` nor `registry.scalar.com`. Pinned in Task 4.
- The integrity hash does not match the pinned CDN file, so the browser blocks Scalar. Expected: `/docs` loads the contract and `GET /healthz` returns `{"status":"ok"}`. Pinned in Task 6. Do not change the version or hash in this session.

## File map

- `backend/openapi.yaml` — OpenAPI 3.1 document for `GET /healthz` only.
- `backend/contract.go` — embeds `openapi.yaml` as `OpenAPI`.
- `backend/contract_test.go` — pins the document text.
- `backend/cmd/api/docs.html` — Scalar page.
- `backend/cmd/api/main.go` — env parser, `/openapi.yaml`, and `/docs`.
- `backend/cmd/api/main_test.go` — HTTP and parser tests.
- `backend/.env.example`, `infra/render.yaml`, `backend/AGENTS.md`, `backend/README.md`, `docs/architecture/backend.md`, `DevSteps.md` — the switch and the Session 5 handoff.

## Setup

Local `dev` already contains the spec commit. Start the feature branch from the current `dev` tip. Do not reset `dev`.

```bash
git checkout -b feature/browsable-api-contract
git add docs/superpowers/plans/2026-10-13-session-2-browsable-api-contract.md
git commit -m "$(cat <<'EOF'
docs: plan session 2 browsable API contract

Give the OpenAPI page a task list before the routes are added.

EOF
)"
```

Expected: the branch exists, the plan is committed, and `backend/openapi.yaml` does not exist yet.

---

### Task 1: OpenAPI document

**Files:**
- Create: `backend/openapi.yaml`
- Create: `backend/contract.go`
- Test: `backend/contract_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `package contract` and `var OpenAPI []byte`. Tasks 2 and 4 import `github.com/metalhops/metalhops/backend` and read `contract.OpenAPI`.

- [ ] **Step 1: Write the failing test**

Create `backend/contract_test.go`:

```go
package contract

import (
	"bytes"
	"testing"
)

func TestOpenAPIDocument(t *testing.T) {
	for _, want := range []string{"openapi: 3.1.0", "/healthz", "http://localhost:8080"} {
		if !bytes.Contains(OpenAPI, []byte(want)) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"/v1", "bearer"} {
		if bytes.Contains(OpenAPI, []byte(banned)) {
			t.Fatalf("contract contains %q", banned)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `backend/`:

```bash
go test . -run TestOpenAPIDocument -count=1
```

Expected: FAIL with `undefined: OpenAPI`.

- [ ] **Step 3: Write the minimal implementation**

Create `backend/openapi.yaml` with a trailing newline:

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

Create `backend/contract.go`:

```go
package contract

import _ "embed"

// shortcut: module-root embed because go:embed cannot see a parent directory; move with the HTTP routes in Session 3.
//go:embed openapi.yaml
var OpenAPI []byte
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `backend/`:

```bash
go test . -run TestOpenAPIDocument -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/openapi.yaml backend/contract.go backend/contract_test.go
git commit -m "$(cat <<'EOF'
feat: add the implemented OpenAPI contract

Record only GET /healthz so the published document matches the running API.

EOF
)"
```

---

### Task 2: Serve the contract

**Files:**
- Modify: `backend/cmd/api/main.go`
- Test: `backend/cmd/api/main_test.go`

**Interfaces:**
- Consumes: `contract.OpenAPI []byte` from Task 1.
- Produces: `func newMux() *http.ServeMux` still takes no arguments, and `GET /openapi.yaml` is always registered. Task 4 replaces this signature with `func newMux(docsEnabled bool) *http.ServeMux`.

- [ ] **Step 1: Write the failing test**

Add this test to `backend/cmd/api/main_test.go`. Leave `TestHealthz` calling `newMux()`.

```go
func TestOpenAPI(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("content type %q", got)
	}
	body := rec.Body.String()
	if body != string(contract.OpenAPI) {
		t.Fatalf("body does not match the embedded contract: %q", body)
	}
	for _, want := range []string{"openapi: 3.1.0", "/healthz", "http://localhost:8080"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"/v1", "bearer", `{"data":`} {
		if strings.Contains(body, banned) {
			t.Fatalf("contract contains %q", banned)
		}
	}
}
```

Add these imports:

```go
import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metalhops/metalhops/backend"
)
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `backend/`:

```bash
go test ./cmd/api -run TestOpenAPI -count=1
```

Expected: FAIL with `expected status 200, got 404`.

- [ ] **Step 3: Write the minimal implementation**

Replace `backend/cmd/api/main.go` with this file. `newMux` still takes no arguments.

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/metalhops/metalhops/backend"
)

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /openapi.yaml", openAPIHandler)
	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(contract.OpenAPI)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("MetalHops API listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `backend/`:

```bash
go test ./cmd/api -count=1
```

Expected: PASS for `TestHealthz` and `TestOpenAPI`.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api/main.go backend/cmd/api/main_test.go
git commit -m "$(cat <<'EOF'
feat: serve the OpenAPI contract

Expose the embedded document at GET /openapi.yaml in every environment.

EOF
)"
```

---

### Task 3: Docs UI setting

**Files:**
- Modify: `backend/cmd/api/main.go`
- Test: `backend/cmd/api/main_test.go`

**Interfaces:**
- Consumes: nothing from the mux.
- Produces: `func docsUIEnabled(value string) (bool, error)`. Task 4 calls it with `os.Getenv("DOCS_UI_ENABLED")` and passes the bool to `newMux`.

- [ ] **Step 1: Write the failing test**

Add this test to `backend/cmd/api/main_test.go`:

```go
func TestDocsUIEnabled(t *testing.T) {
	ok, err := docsUIEnabled("")
	if err != nil || !ok {
		t.Fatalf("empty: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("true")
	if err != nil || !ok {
		t.Fatalf("true: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("false")
	if err != nil || ok {
		t.Fatalf("false: enabled=%v err=%v", ok, err)
	}
	for _, value := range []string{"TRUE", "1", " true"} {
		enabled, gotErr := docsUIEnabled(value)
		if gotErr == nil || enabled {
			t.Fatalf("%q: enabled=%v err=%v", value, enabled, gotErr)
		}
		if gotErr.Error() != "DOCS_UI_ENABLED must be true, false, or empty" {
			t.Fatalf("%q error %q", value, gotErr.Error())
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `backend/`:

```bash
go test ./cmd/api -run TestDocsUIEnabled -count=1
```

Expected: FAIL with `undefined: docsUIEnabled`.

- [ ] **Step 3: Write the minimal implementation**

Add this function to `backend/cmd/api/main.go` and add `"fmt"` to the imports. Do not call it from `main` yet.

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

- [ ] **Step 4: Run the test to verify it passes**

Run from `backend/`:

```bash
go test ./cmd/api -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api/main.go backend/cmd/api/main_test.go
git commit -m "$(cat <<'EOF'
feat: parse the docs UI switch

Accept only empty, true, and false so a typo cannot turn the page on.

EOF
)"
```

---

### Task 4: Scalar page

**Files:**
- Create: `backend/cmd/api/docs.html`
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/cmd/api/main_test.go`

**Interfaces:**
- Consumes: `docsUIEnabled(value string) (bool, error)` from Task 3 and `contract.OpenAPI` from Task 1.
- Produces: `func newMux(docsEnabled bool) *http.ServeMux`. This replaces the no-argument `newMux` from Task 2. `main` passes `os.Getenv("DOCS_UI_ENABLED")` through `docsUIEnabled` and exits on error before listen.

- [ ] **Step 1: Write the failing test**

Replace `backend/cmd/api/main_test.go` with:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metalhops/metalhops/backend"
)

func TestHealthz(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	newMux(false).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestOpenAPI(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)

	newMux(false).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/yaml" {
		t.Fatalf("content type %q", got)
	}
	body := rec.Body.String()
	if body != string(contract.OpenAPI) {
		t.Fatalf("body does not match the embedded contract: %q", body)
	}
	for _, want := range []string{"openapi: 3.1.0", "/healthz", "http://localhost:8080"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"/v1", "bearer", `{"data":`} {
		if strings.Contains(body, banned) {
			t.Fatalf("contract contains %q", banned)
		}
	}
}

func TestDocsEnabled(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)

	newMux(true).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("content type %q", got)
	}
	const csp = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if got := rec.Header().Get("Content-Security-Policy"); got != csp {
		t.Fatalf("csp %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js",
		"sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6",
		`data-url="/openapi.yaml"`,
		`crossorigin="anonymous"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"proxy", "registry.scalar.com"} {
		if strings.Contains(body, banned) {
			t.Fatalf("docs page contains %q", banned)
		}
	}
}

func TestDocsDisabled(t *testing.T) {
	mux := newMux(false)

	docs := httptest.NewRecorder()
	mux.ServeHTTP(docs, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if docs.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", docs.Code)
	}

	spec := httptest.NewRecorder()
	mux.ServeHTTP(spec, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if spec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", spec.Code)
	}
}

func TestDocsUIEnabled(t *testing.T) {
	ok, err := docsUIEnabled("")
	if err != nil || !ok {
		t.Fatalf("empty: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("true")
	if err != nil || !ok {
		t.Fatalf("true: enabled=%v err=%v", ok, err)
	}
	ok, err = docsUIEnabled("false")
	if err != nil || ok {
		t.Fatalf("false: enabled=%v err=%v", ok, err)
	}
	for _, value := range []string{"TRUE", "1", " true"} {
		enabled, gotErr := docsUIEnabled(value)
		if gotErr == nil || enabled {
			t.Fatalf("%q: enabled=%v err=%v", value, enabled, gotErr)
		}
		if gotErr.Error() != "DOCS_UI_ENABLED must be true, false, or empty" {
			t.Fatalf("%q error %q", value, gotErr.Error())
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run from `backend/`:

```bash
go test ./cmd/api -run TestDocsEnabled -count=1
```

Expected: FAIL to compile with `too many arguments in call to newMux`.

- [ ] **Step 3: Write the minimal implementation**

Create `backend/cmd/api/docs.html` with 4-space indentation and a trailing newline:

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

Replace `backend/cmd/api/main.go` with:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/metalhops/metalhops/backend"
)

//go:embed docs.html
var docsHTML []byte

const docsCSP = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

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

func newMux(docsEnabled bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /openapi.yaml", openAPIHandler)
	if docsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	return mux
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	_, _ = w.Write(contract.OpenAPI)
}

func docsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", docsCSP)
	_, _ = w.Write(docsHTML)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	docsEnabled, err := docsUIEnabled(os.Getenv("DOCS_UI_ENABLED"))
	if err != nil {
		log.Fatal(err)
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           newMux(docsEnabled),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("MetalHops API listening on :%s", port)
	log.Fatal(srv.ListenAndServe())
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run from `backend/`:

```bash
gofmt -w cmd/api/main.go cmd/api/main_test.go
go test ./cmd/api -count=1
```

Expected: PASS, including `TestDocsEnabled` and `TestDocsDisabled`.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api/docs.html backend/cmd/api/main.go backend/cmd/api/main_test.go
git commit -m "$(cat <<'EOF'
feat: serve the Scalar docs page

Load the pinned contract browser only when DOCS_UI_ENABLED allows it.

EOF
)"
```

---

### Task 5: Document the switch

**Files:**
- Modify: `backend/.env.example`
- Modify: `infra/render.yaml`
- Modify: `backend/AGENTS.md`
- Modify: `backend/README.md`
- Modify: `docs/architecture/backend.md`
- Modify: `DevSteps.md`

**Interfaces:**
- Consumes: the routes and env values from Tasks 3 and 4.
- Produces: the operator-facing switch. No Go signature changes.

- [ ] **Step 1: Confirm the switch is undocumented**

Run from the repository root:

```bash
grep -n DOCS_UI_ENABLED backend/.env.example infra/render.yaml backend/AGENTS.md || true
grep -n "localhost:8080/docs" backend/README.md || true
grep -n "declares no bearer security scheme" DevSteps.md || true
```

Expected: no matches. `grep` exits non-zero; `|| true` keeps the step going.

- [ ] **Step 2: Update the env files and guides**

Append to `backend/.env.example`:

```text
# Scalar UI at /docs. Unset or true enables it. Render sets false.
DOCS_UI_ENABLED=true
```

Add this entry after the `SUPABASE_JWT_SECRET` entry in `infra/render.yaml`:

```yaml
      - key: DOCS_UI_ENABLED
        value: "false"
```

The `envVars` list becomes:

```yaml
    envVars:
      - key: DATABASE_URL
        sync: false
      - key: SUPABASE_JWT_SECRET
        sync: false
      - key: DOCS_UI_ENABLED
        value: "false"
```

Insert this section in `backend/AGENTS.md` after the Layout section and before `## Planning a session`:

```markdown
## Documentation routes

`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

Both routes live in `cmd/api` until Session 3 moves HTTP behavior. The
contract embed is `backend/contract.go`.
```

In `backend/README.md`, replace the `make run` comment so the line is:

```text
make run     # http://localhost:8080/docs
```

Add this bullet to the Transitional state list in `docs/architecture/backend.md`:

```markdown
- `backend/contract.go` embeds `openapi.yaml` because `go:embed` cannot
  reference a parent directory. The backend-foundation session moves that
  embed with the HTTP routes.
```

In `DevSteps.md`, replace this paragraph:

```markdown
The OpenAPI security scheme accepts Supabase bearer tokens. Developers can
authorize once in the page and exercise protected endpoints. The contract
contains realistic request and response examples, but no real credentials or
production hostnames.
```

with:

```markdown
The contract documents only implemented routes. Session 2 has no protected
routes, so it declares no bearer security scheme. The authentication session
adds that scheme when the first protected route exists. Examples contain no
real credentials or production hostnames.
```

In the Session 5 paragraph, replace:

```markdown
implements the agreed contribution retention for User deletion.
```

with:

```markdown
implements the agreed contribution retention for User deletion. The OpenAPI
document gains the bearer security scheme on those protected routes.
```

- [ ] **Step 3: Confirm the text landed**

Run from the repository root:

```bash
grep -n 'DOCS_UI_ENABLED=true' backend/.env.example
grep -n 'value: "false"' infra/render.yaml
grep -n '## Documentation routes' backend/AGENTS.md
grep -n 'http://localhost:8080/docs' backend/README.md
grep -n 'backend/contract.go' docs/architecture/backend.md
grep -n 'declares no bearer security scheme' DevSteps.md
grep -n 'bearer security scheme on those protected routes' DevSteps.md
```

Expected: one matching line from each command.

- [ ] **Step 4: Commit**

```bash
git add backend/.env.example infra/render.yaml backend/AGENTS.md backend/README.md docs/architecture/backend.md DevSteps.md
git commit -m "$(cat <<'EOF'
docs: record the local Scalar page switch

Keep Render from serving /docs and point the bearer scheme at Session 5.

EOF
)"
```

---

### Task 6: Verify the running page

**Files:**
- Modify: none, unless `gofmt` rewrites a Go file from an earlier task.

**Interfaces:**
- Consumes: the server from Task 4 and the env files from Task 5.
- Produces: a checked local exit condition. No new API.

- [ ] **Step 1: Run the backend checks**

Run from `backend/`:

```bash
gofmt -l .
go vet ./...
go test ./...
```

Expected: `gofmt -l .` prints nothing. Vet and test exit 0.

If `gofmt -l .` prints files, run `gofmt -w` on those files, repeat the three commands, and commit:

```bash
git add backend
git commit -m "$(cat <<'EOF'
style: format the docs contract server

EOF
)"
```

If `gofmt` prints nothing, do not create a commit.

- [ ] **Step 2: Confirm the dependency and image files stayed untouched**

Run from the repository root:

```bash
git diff dev -- backend/go.mod backend/go.sum backend/Dockerfile .github/workflows/backend.yml
```

Expected: no diff.

- [ ] **Step 3: Open the page in a browser**

From `backend/`, with `DOCS_UI_ENABLED` unset:

```bash
make run
```

Open `http://localhost:8080/docs`. Scalar shows `GET /healthz` and no authorize control. Send that request. Expected: `{"status":"ok"}`.

If the browser reports an integrity failure or a content-security-policy refusal, stop. Do not change the Scalar version or the hash in this session.

Stop the process. Start it again with the page disabled:

```bash
DOCS_UI_ENABLED=false make run
```

In another shell:

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/docs
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/openapi.yaml
curl -s http://localhost:8080/healthz
```

Expected: `404`, then `200`, then `{"status":"ok"}`.

Start it once more with an invalid value:

```bash
DOCS_UI_ENABLED=TRUE make run
```

Expected: the process exits before listening and prints `DOCS_UI_ENABLED must be true, false, or empty`.

- [ ] **Step 4: Stop the server**

Stop the running `make run` process. Leave the feature branch unmerged.
