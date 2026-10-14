# Session 3 Backend Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Go API start from validated configuration, serve operational routes through shared HTTP infrastructure, and report database readiness without applying migrations.

**Architecture:** `cmd/api` loads `internal/config`, opens an `internal/database` pool, and serves `internal/api`. The HTTP package owns envelopes, middleware, and `/healthz`, `/readyz`, `/openapi.yaml`, and `/docs`. It depends on a readiness function, not on `pgx`. The pool observes `supabase_migrations.schema_migrations` and does not apply SQL.

**Tech Stack:** Go 1.23, `net/http`, `log/slog`, `github.com/jackc/pgx/v5/pgxpool`, `github.com/testcontainers/testcontainers-go/modules/postgres`, image `postgis/postgis:16-3.5`.

**Spec:** `docs/superpowers/specs/2026-10-14-session-3-backend-foundation-design.md`

## Global Constraints

- `DATABASE_URL` is required and must parse. The process still listens when Postgres refuses connections.
- Session pooler settings: prepared statements stay at the `pgx` default, `MaxConns` is 5, `MinConns` is 0.
- Startup does not apply SQL. Required migration version is the string `0001`.
- The only new seam is `api.Readiness`. Production passes `api.ReadinessFunc(pool.Check)`. Tests pass another `ReadinessFunc`.
- `backend/contract.go` keeps embedding `backend/openapi.yaml`. `cmd/api` passes `contract.OpenAPI` into `internal/api`.
- Document `/readyz` only. `/openapi.yaml`, `/docs`, and the bearer scheme stay out of the contract.
- `PORT` empty means `8080`. Any other value must be an integer from 1 through 65535 with no surrounding whitespace. Rejected text is `PORT must be an integer from 1 through 65535`.
- Empty `DATABASE_URL` returns `DATABASE_URL is required`. A parse failure returns `DATABASE_URL must be a valid PostgreSQL URL` and does not wrap the driver error.
- `DOCS_UI_ENABLED`: empty or `true` enables `/docs`; `false` leaves it unregistered; any other value, including `TRUE`, `1`, and ` true`, returns `DOCS_UI_ENABLED must be true, false, or empty`.
- This session does not read `SUPABASE_JWT_SECRET`.
- Check reasons are `ok`, `database_unreachable`, `migrations_unavailable`, and `migrations_behind`.
- `/readyz` returns `200` `{"status":"ok"}` or `503` `{"status":"not_ready","reason":"<reason>"}`.
- Request IDs are 16 bytes from `crypto/rand`, hex-encoded, in `X-Request-ID`. Rand failure uses request ID `unavailable`.
- Panic response code is `internal_error` and message is `The server could not complete the request.` The stack stays out of the body.
- Server timeouts are 5s headers, 10s read, 10s write, 60s idle. Shutdown timeout is 15s. Ping and version query each use a 2s timeout.
- Do not add a migration file, a Go migrate command, rate limits, idempotency, JWT verification, or JSON body decoding.
- The API process does not apply SQL. Session 3 is incomplete until the operator runs the Task 8 commands and `GET /readyz` returns 200 against the hosted project. Do not run `supabase start`.
- Commit `infra/supabase/config.toml` and, when `supabase init` creates it, `infra/supabase/.gitignore`. Do not commit `.temp`, `.branches`, the database password, or linked-project credentials.
- Do not create Profile, Venue, Review, or Check-in packages.
- Do not change `infra/render.yaml` or mark Session 3 done in `DevSteps.md`. A `{"status":"ok"}` reply from `/readyz` does not change DevSteps. That status update waits for a later docs commit.
- Do not add a test under `cmd/api`.
- Work on `feature/backend-foundation`, targeting `dev` by pull request.
- Commands in project files use plain tools (`git`, `go`, `make`). Run `go` commands from `backend/`.

## Review decisions

- The API listens when Postgres is down and does not apply SQL. There is no `make migrate` and no `supabase start`.
- Session 3 is incomplete until the operator runs the Task 8 commands against the hosted project and replies with `{"status":"ok"}` from `GET /readyz`.
- The implementer may install the Supabase CLI and run `supabase init --workdir infra`. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
- Commit `infra/supabase/config.toml`. Commit `infra/supabase/.gitignore` when init creates it. Leave every other init file untracked, including `seed.sql`. Do not commit `.temp`, `.branches`, the database password, or linked-project credentials.
- Set `project_id` to `metalhops`. Leave ports and enabled services as init writes them.
- Leave `major_version` as init writes it. If `link` or `db push` reports a mismatch, stop and ask the operator for the hosted major version before changing that one field.
- The migration file stays `infra/supabase/migrations/0001_init.sql`. If `db push` rejects that name, stop and report the CLI error. Do not rename it in this session.
- `db push` first uses the CLI's linked direct connection. If that host is unreachable, rerun it with `--db-url` set to the session-pooler URL on port 5432, read with the same rule as `config.Load`: an environment value wins, including when empty; otherwise the last `DATABASE_URL` line in `backend/.env` is used after stripping one optional `export ` prefix, and the value is everything after the first `=`. Do not use port 6543. Do not print the URL. This fallback stays in Task 8 and is not copied into `infra/README.md`.
- The password is entered in the terminal prompt or read from the local environment or `.env`. It is not written into the repo or into chat.
- `api.Serve` owns the shutdown wait, the 15-second drain, and the pool close. `cmd/api` calls it and gains no new test.
- `DevSteps.md` is not edited. A `{"status":"ok"}` reply does not mark Session 3 done. That status update waits for a later docs commit.
- Commit `config.toml` and the README change before sending the operator commands.

## Review Focus

- A `.env` value that contains another `=` is cut at the second one, so a pooler URL or password is saved wrong. Expected: the value is everything after the first `=`, quotes included. Pinned in Task 2.
- `Open` dials during construction and returns an error when the host refuses connections, so `/healthz` never starts. Expected: `Open` returns a pool and `MaxConns` is 5. Pinned in Task 4.
- Version `0002` is treated as older than `0001`. Expected: `Check` returns `true, "ok"`. Pinned in Task 4.
- `crypto/rand` failure skips the access log or writes a stack into the body. Expected: `500` envelope with `request_id` `unavailable`, the access log records status 500, and the body has no `goroutine`. Pinned in Task 5.
- A shutdown deadline returns without closing the pool, so a Supabase session stays checked out. Expected: `closePool` runs and the deadline error is returned. Pinned in Task 6.

## File map

- `backend/go.mod`, `backend/go.sum` — Go 1.23, `pgx/v5`, `testcontainers-go`.
- `backend/internal/config/config.go` — `Load`.
- `backend/internal/config/config_test.go` — environment and `.env` tests.
- `backend/internal/database/pool.go` — `Open`, `Check`, `Close`, `Redact`.
- `backend/internal/database/pool_test.go` — refused connections and redaction.
- `backend/internal/database/postgres_test.go` — PostGIS migration-version cases.
- `backend/internal/api/envelope.go` — JSON writers.
- `backend/internal/api/envelope_test.go` — envelope shapes.
- `backend/internal/api/middleware.go` — access log, request ID, panic recovery.
- `backend/internal/api/server.go` — routes and `New`.
- `backend/internal/api/server_test.go` — route, middleware, and shutdown tests.
- `backend/internal/api/docs.html` — Scalar page. Task 5 copies it. Task 6 deletes `backend/cmd/api/docs.html`.
- `backend/cmd/api/main.go` — composition only.
- `backend/openapi.yaml` — add `/readyz`.
- `backend/contract.go` — keep the embed; update the comment.
- `backend/.env.example`, `backend/AGENTS.md`, `docs/architecture/backend.md`, `infra/README.md` — guide updates.
- `infra/supabase/config.toml` — Supabase CLI project file. Task 8.

## Setup

Local `dev` contains the spec commit. Start the feature branch from the current `dev` tip. Do not reset `dev`.

```bash
git checkout -b feature/backend-foundation
git add docs/superpowers/plans/2026-10-14-session-3-backend-foundation.md
git commit -m "$(cat <<'EOF'
docs: plan session 3 backend foundation

Give the foundation work a task list before the packages are added.

EOF
)"
```

Expected: the branch exists and `backend/internal/config` does not exist yet.

---

### Task 1: Configuration from the environment

**Files:**
- Modify: `backend/go.mod`
- Create: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config{Port string, DatabaseURL string, DocsUIEnabled bool}` and `func Load() (Config, error)`. `Load` calls unexported `load(lookup func(string) (string, bool), dir string)`. Later tasks call `Load` only.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/config_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestLoadEnvironment(t *testing.T) {
	dir := t.TempDir()
	lookup := map[string]string{
		"DATABASE_URL": "postgres://user:s3cret@127.0.0.1:5432/metalhops",
	}
	getenv := func(key string) (string, bool) {
		value, ok := lookup[key]
		return value, ok
	}

	cfg, err := load(getenv, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" || !cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
	if cfg.DatabaseURL != lookup["DATABASE_URL"] {
		t.Fatalf("url %q", cfg.DatabaseURL)
	}

	lookup["PORT"] = "9090"
	lookup["DOCS_UI_ENABLED"] = "false"
	lookup["SUPABASE_JWT_SECRET"] = "legacy-secret"
	cfg, err = load(getenv, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
}

func TestLoadRejectsEnvironment(t *testing.T) {
	dir := t.TempDir()
	validURL := "postgres://user:s3cret@127.0.0.1:5432/metalhops"
	cases := []struct {
		name    string
		env     map[string]string
		errText string
	}{
		{name: "missing url", env: map[string]string{}, errText: "DATABASE_URL is required"},
		{name: "bad url", env: map[string]string{"DATABASE_URL": "not-a-url-s3cret"}, errText: "DATABASE_URL must be a valid PostgreSQL URL"},
		{name: "port zero", env: map[string]string{"DATABASE_URL": validURL, "PORT": "0"}, errText: "PORT must be an integer from 1 through 65535"},
		{name: "port high", env: map[string]string{"DATABASE_URL": validURL, "PORT": "65536"}, errText: "PORT must be an integer from 1 through 65535"},
		{name: "port padded", env: map[string]string{"DATABASE_URL": validURL, "PORT": " 8080"}, errText: "PORT must be an integer from 1 through 65535"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(func(key string) (string, bool) {
				value, ok := tc.env[key]
				return value, ok
			}, dir)
			if err == nil || err.Error() != tc.errText {
				t.Fatalf("err %v", err)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Fatalf("error contains secret: %s", err.Error())
			}
		})
	}

	for _, value := range []string{"TRUE", "1", " true"} {
		_, err := load(func(key string) (string, bool) {
			if key == "DATABASE_URL" {
				return "postgres://user:s3cret@127.0.0.1:5432/metalhops", true
			}
			if key == "DOCS_UI_ENABLED" {
				return value, true
			}
			return "", false
		}, dir)
		if err == nil || err.Error() != "DOCS_UI_ENABLED must be true, false, or empty" {
			t.Fatalf("%q err %v", value, err)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

From `backend/`:

```bash
go test ./internal/config/ -count=1
```

Expected: FAIL because package `config` does not exist.

- [ ] **Step 3: Set Go 1.23 and implement Load**

In `backend/go.mod`, change `go 1.22` to `go 1.23`. From `backend/`:

```bash
go get github.com/jackc/pgx/v5
```

Create `backend/internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Port          string
	DatabaseURL   string
	DocsUIEnabled bool
}

func Load() (Config, error) {
	return load(os.LookupEnv, ".")
}

func load(lookup func(string) (string, bool), dir string) (Config, error) {
	fileEnv, err := readEnvFile(dir)
	if err != nil {
		return Config{}, err
	}
	value := func(key string) string {
		if envValue, ok := lookup(key); ok {
			return envValue
		}
		return fileEnv[key]
	}

	port, err := parsePort(value("PORT"))
	if err != nil {
		return Config{}, err
	}
	databaseURL, err := parseDatabaseURL(value("DATABASE_URL"))
	if err != nil {
		return Config{}, err
	}
	docs, err := parseDocs(value("DOCS_UI_ENABLED"))
	if err != nil {
		return Config{}, err
	}
	return Config{Port: port, DatabaseURL: databaseURL, DocsUIEnabled: docs}, nil
}

func parsePort(value string) (string, error) {
	if value == "" {
		return "8080", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("PORT must be an integer from 1 through 65535")
	}
	return strconv.Itoa(n), nil
}

func parseDatabaseURL(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("DATABASE_URL is required")
	}
	if _, err := pgxpool.ParseConfig(value); err != nil {
		return "", fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL")
	}
	return value, nil
}

func parseDocs(value string) (bool, error) {
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

Add this stub at the bottom of the same file so Task 1 compiles. Task 2 replaces it. `dir` is referenced so the parameter is not unused:

```go
func readEnvFile(dir string) (map[string]string, error) {
	if dir == "" {
		return nil, fmt.Errorf("read .env")
	}
	return map[string]string{}, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

From `backend/`:

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/config
git commit -m "$(cat <<'EOF'
feat: validate API configuration

EOF
)"
```

---

### Task 2: Fill missing settings from .env

**Files:**
- Modify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Consumes: `load(lookup func(string) (string, bool), dir string) (Config, error)` from Task 1.
- Produces: the same `Load`. A missing `./.env` is ignored. File values fill keys the lookup reports as unset. Lookup values win.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/config/config_test.go`:

```go
func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	body := "  # comment\n" +
		"export PORT=9090\n" +
		"DATABASE_URL=postgres://user:p=a=ss@127.0.0.1:5432/metalhops\n" +
		"DOCS_UI_ENABLED=false\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := load(func(string) (string, bool) { return "", false }, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" || cfg.DocsUIEnabled {
		t.Fatalf("port %q docs %v", cfg.Port, cfg.DocsUIEnabled)
	}
	if cfg.DatabaseURL != "postgres://user:p=a=ss@127.0.0.1:5432/metalhops" {
		t.Fatalf("url %q", cfg.DatabaseURL)
	}

	cfg, err = load(func(key string) (string, bool) {
		if key == "PORT" {
			return "8081", true
		}
		return "", false
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8081" {
		t.Fatalf("env port lost: %q", cfg.Port)
	}

	quoted := t.TempDir()
	if err := os.WriteFile(filepath.Join(quoted, ".env"), []byte("PORT=\"8080\"\nDATABASE_URL=postgres://user:s3cret@127.0.0.1:5432/metalhops\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, quoted)
	if err == nil || err.Error() != "PORT must be an integer from 1 through 65535" {
		t.Fatalf("quoted port err %v", err)
	}

	missing := t.TempDir()
	_, err = load(func(key string) (string, bool) {
		if key == "DATABASE_URL" {
			return "postgres://user:s3cret@127.0.0.1:5432/metalhops", true
		}
		return "", false
	}, missing)
	if err != nil {
		t.Fatal(err)
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, ".env"), []byte("# c\n\nNOTAKEY\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, broken)
	if err == nil || err.Error() != ".env line 3 is malformed" || strings.Contains(err.Error(), "NOTAKEY") {
		t.Fatalf("malformed err %v", err)
	}

	emptyKey := t.TempDir()
	if err := os.WriteFile(filepath.Join(emptyKey, ".env"), []byte("=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, emptyKey)
	if err == nil || err.Error() != ".env line 1 is malformed" || strings.Contains(err.Error(), "value") {
		t.Fatalf("empty key err %v", err)
	}

	unreadable := t.TempDir()
	if err := os.Mkdir(filepath.Join(unreadable, ".env"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = load(func(string) (string, bool) { return "", false }, unreadable)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(unreadable, ".env")) || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("unreadable err %v", err)
	}
}
```

Add imports `os` and `path/filepath`. `strings` is already imported.

- [ ] **Step 2: Run the test to verify it fails**

From `backend/`:

```bash
go test ./internal/config/ -count=1 -run TestLoadEnvFile
```

Expected: FAIL because `readEnvFile` ignores the file.

- [ ] **Step 3: Parse .env**

Replace `readEnvFile` in `backend/internal/config/config.go` and add the imports `bufio`, `path/filepath`, and `strings`:

```go
func readEnvFile(dir string) (map[string]string, error) {
	path := filepath.Join(dir, ".env")
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read %s: %v", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		key, value, skip, err := parseEnvLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf(".env line %d is malformed", lineNo)
		}
		if skip {
			continue
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %v", path, err)
	}
	return values, nil
}

func parseEnvLine(line string) (key, value string, skip bool, err error) {
	if strings.TrimSpace(line) == "" {
		return "", "", true, nil
	}
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "#") {
		return "", "", true, nil
	}
	trimmed = strings.TrimPrefix(trimmed, "export ")
	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false, fmt.Errorf("malformed")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("malformed")
	}
	return key, value, false, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

From `backend/`:

```bash
gofmt -w internal/config/config.go internal/config/config_test.go
go test ./internal/config/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config
git commit -m "$(cat <<'EOF'
feat: load missing settings from a local env file

EOF
)"
```

---

### Task 3: JSON envelope helpers

**Files:**
- Create: `backend/internal/api/envelope.go`
- Test: `backend/internal/api/envelope_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `func WriteData(w http.ResponseWriter, status int, value any)`
  - `func WriteList(w http.ResponseWriter, status int, items any, nextCursor *string)`
  - `func WriteError(w http.ResponseWriter, status int, code, message, requestID string, fields map[string]string)`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/api/envelope_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteData(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteData(rec, http.StatusCreated, map[string]string{"username": "lars"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
	if strings.TrimSpace(rec.Body.String()) != `{"data":{"username":"lars"}}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestWriteListNullCursor(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteList(rec, http.StatusOK, []any{}, nil)
	if strings.TrimSpace(rec.Body.String()) != `{"data":[],"meta":{"next_cursor":null}}` {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestWriteErrorOmitsEmptyFields(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "validation_error", "The request is invalid.", "abc", nil)
	if strings.Contains(rec.Body.String(), "fields") {
		t.Fatalf("body %s", rec.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "validation_error" || got.Error.RequestID != "abc" {
		t.Fatalf("decoded %#v", got.Error)
	}
}

func TestWriteErrorIncludesFields(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, "validation_error", "The request is invalid.", "abc", map[string]string{
		"rating": "must be between 1 and 5",
	})
	var got struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Fields["rating"] != "must be between 1 and 5" {
		t.Fatalf("fields %#v", got.Error.Fields)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

From `backend/`:

```bash
go test ./internal/api/ -count=1 -run 'TestWrite'
```

Expected: FAIL because `WriteData` is undefined.

- [ ] **Step 3: Implement the writers**

Create `backend/internal/api/envelope.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

type dataBody struct {
	Data any `json:"data"`
}

type listBody struct {
	Data any  `json:"data"`
	Meta meta `json:"meta"`
}

type meta struct {
	NextCursor *string `json:"next_cursor"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"request_id"`
	Fields    map[string]string `json:"fields,omitempty"`
}

func WriteData(w http.ResponseWriter, status int, value any) {
	writeJSON(w, status, dataBody{Data: value})
}

func WriteList(w http.ResponseWriter, status int, items any, nextCursor *string) {
	writeJSON(w, status, listBody{Data: items, Meta: meta{NextCursor: nextCursor}})
}

func WriteError(w http.ResponseWriter, status int, code, message, requestID string, fields map[string]string) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code: code, Message: message, RequestID: requestID, Fields: fields,
	}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
```

- [ ] **Step 4: Run the test to verify it passes**

From `backend/`:

```bash
gofmt -w internal/api/envelope.go internal/api/envelope_test.go
go test ./internal/api/ -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api
git commit -m "$(cat <<'EOF'
feat: add shared JSON response envelopes

EOF
)"
```

---

### Task 4: Database pool and readiness check

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Create: `backend/internal/database/pool.go`
- Test: `backend/internal/database/pool_test.go`
- Test: `backend/internal/database/postgres_test.go`

**Interfaces:**
- Consumes: a PostgreSQL URL string. No HTTP types.
- Produces:
  - `func Open(ctx context.Context, databaseURL string) (*Pool, error)`
  - `func (p *Pool) Close()`
  - `func (p *Pool) Check(ctx context.Context) (bool, string)`
  - `func Redact(databaseURL, text string) string`
  - `const requiredMigrationVersion = "0001"`

- [ ] **Step 1: Write the failing pool test**

Create `backend/internal/database/pool_test.go`:

```go
package database

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestOpenDoesNotDialAndCheckReportsUnreachable(t *testing.T) {
	ctx := context.Background()
	const url = "postgres://metalhops:s3cret@127.0.0.1:1/metalhops"
	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if pool.pool.Stat().MaxConns() != 5 || pool.pool.Stat().TotalConns() != 0 {
		t.Fatalf("max %d total %d", pool.pool.Stat().MaxConns(), pool.pool.Stat().TotalConns())
	}

	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	ready, reason := pool.Check(ctx)
	if ready || reason != "database_unreachable" {
		t.Fatalf("ready %v reason %s", ready, reason)
	}
	if strings.Contains(buf.String(), "s3cret") || strings.Contains(buf.String(), "postgres://") {
		t.Fatalf("log contains url: %s", buf.String())
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	ready, reason = pool.Check(canceled)
	if ready || reason != "database_unreachable" {
		t.Fatalf("canceled ready %v reason %s", ready, reason)
	}
}

func TestRedactRemovesURL(t *testing.T) {
	got := Redact("postgres://user:s3cret@localhost/db", "failed postgres://user:s3cret@localhost/db")
	if got != "failed DATABASE_URL" || strings.Contains(got, "s3cret") {
		t.Fatalf("redacted %q", got)
	}
}
```

- [ ] **Step 2: Run the pool test to verify it fails**

From `backend/`:

```bash
go test ./internal/database/ -count=1 -run 'TestOpen|TestRedact'
```

Expected: FAIL because package `database` does not exist.

- [ ] **Step 3: Implement Open, Check, and Redact**

Create `backend/internal/database/pool.go`:

```go
package database

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const requiredMigrationVersion = "0001"

type Pool struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 5
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &Pool{pool: pool}, nil
}

func (p *Pool) Close() {
	p.pool.Close()
}

func (p *Pool) Check(ctx context.Context) (bool, string) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := p.pool.Ping(pingCtx); err != nil {
		return fail("database_unreachable", err)
	}

	queryCtx, cancelQuery := context.WithTimeout(ctx, 2*time.Second)
	defer cancelQuery()
	var version string
	err := p.pool.QueryRow(queryCtx, `
		select version
		from supabase_migrations.schema_migrations
		order by version desc
		limit 1`).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail("migrations_behind", nil)
	}
	if err != nil {
		return fail("migrations_unavailable", err)
	}
	if version < requiredMigrationVersion {
		return fail("migrations_behind", nil)
	}
	return true, "ok"
}

func fail(reason string, err error) (bool, string) {
	if err != nil {
		slog.Error("database check", "reason", reason, "err", err)
	} else {
		slog.Error("database check", "reason", reason)
	}
	return false, reason
}

func Redact(databaseURL, text string) string {
	if databaseURL == "" {
		return text
	}
	return strings.ReplaceAll(text, databaseURL, "DATABASE_URL")
}
```

- [ ] **Step 4: Run the pool test to verify it passes**

From `backend/`:

```bash
gofmt -w internal/database/pool.go internal/database/pool_test.go
go test ./internal/database/ -count=1 -run 'TestOpen|TestRedact'
```

Expected: PASS. The unreachable ping returns within about 2 seconds.

- [ ] **Step 5: Write the failing PostGIS test**

From `backend/`:

```bash
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
```

Create `backend/internal/database/postgres_test.go`:

```go
package database

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// shortcut: harness stays in database tests until a second package needs it.

func TestMigrationVersions(t *testing.T) {
	url := startPostGIS(t)
	ctx := context.Background()

	pool, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	ready, reason := pool.Check(ctx)
	if ready || reason != "migrations_unavailable" {
		t.Fatalf("empty ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		create schema if not exists supabase_migrations;
		create table supabase_migrations.schema_migrations (
			version text primary key
		)`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("empty history ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0000')`)
	ready, reason = pool.Check(ctx)
	if ready || reason != "migrations_behind" {
		t.Fatalf("0000 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `
		create schema if not exists auth;
		create table auth.users (
			id uuid primary key
		)`)
	execSQL(t, pool, readMigration(t))
	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0001')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0001 ready %v reason %s", ready, reason)
	}

	execSQL(t, pool, `insert into supabase_migrations.schema_migrations (version) values ('0002')`)
	ready, reason = pool.Check(ctx)
	if !ready || reason != "ok" {
		t.Fatalf("0002 ready %v reason %s", ready, reason)
	}
}

func startPostGIS(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	container, err := postgres.Run(ctx, "postgis/postgis:16-3.5",
		postgres.WithDatabase("metalhops"),
		postgres.WithUsername("metalhops"),
		postgres.WithPassword("metalhops"),
	)
	if err != nil {
		if os.Getenv("CI") == "" && (strings.Contains(err.Error(), "docker") || strings.Contains(err.Error(), "Docker")) {
			t.Skip("Docker is unavailable: " + err.Error())
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func execSQL(t *testing.T, pool *Pool, sql string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	results := conn.Conn().PgConn().Exec(ctx, sql)
	if err := results.Close(); err != nil {
		t.Fatal(err)
	}
}

func readMigration(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "infra", "supabase", "migrations", "0001_init.sql")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
```

- [ ] **Step 6: Run the PostGIS test**

From `backend/`:

```bash
gofmt -w internal/database/*.go
go test ./internal/database/ -count=1
```

Expected: PASS. Without Docker, `TestMigrationVersions` skips with a message that starts `Docker is unavailable:`. In CI, a Docker error fails the test.

- [ ] **Step 7: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/database
git commit -m "$(cat <<'EOF'
feat: report database readiness from migration history

EOF
)"
```

---

### Task 5: Operational HTTP server

**Files:**
- Create: `backend/internal/api/middleware.go`
- Create: `backend/internal/api/server.go`
- Create: `backend/internal/api/docs.html` by copying `backend/cmd/api/docs.html`
- Test: `backend/internal/api/server_test.go`
- Modify: `backend/openapi.yaml`
- Modify: `backend/contract.go`
- Modify: `backend/contract_test.go`

**Interfaces:**
- Consumes: `WriteError` from Task 3. `contract.OpenAPI` from `github.com/metalhops/metalhops/backend`.
- Produces:
  - `type Readiness interface { Check(context.Context) (ready bool, reason string) }`
  - `type ReadinessFunc func(context.Context) (bool, string)` with method `Check`
  - `func New(docsEnabled bool, openAPI []byte, ready Readiness) http.Handler`
  - `func Serve(ctx context.Context, srv *http.Server, timeout time.Duration, closePool func()) error` is Task 6. Do not add it here.

`cmd/api` still serves the old mux after this task. Task 6 switches the process.

- [ ] **Step 1: Write the failing server test**

Copy the page. Leave `backend/cmd/api/docs.html` in place so `cmd/api` still builds until Task 6 deletes it.

```bash
mkdir -p backend/internal/api
cp backend/cmd/api/docs.html backend/internal/api/docs.html
```

Create `backend/internal/api/server_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/metalhops/metalhops/backend"
)

func TestOperationalRoutes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := newServer(true, contract.OpenAPI, ReadinessFunc(func(context.Context) (bool, string) {
		return true, "ok"
	}), logger)

	health := get(handler, "/healthz?token=secret")
	if health.Code != http.StatusOK || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health %d %s", health.Code, health.Body.String())
	}
	assertRequestID(t, health)

	spec := get(handler, "/openapi.yaml")
	if spec.Code != http.StatusOK || spec.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("spec %d %q", spec.Code, spec.Header().Get("Content-Type"))
	}
	if spec.Body.String() != string(contract.OpenAPI) {
		t.Fatal("spec bytes differ")
	}
	for _, banned := range []string{"/v1", "bearer", `{"data":`} {
		if strings.Contains(spec.Body.String(), banned) {
			t.Fatalf("contract contains %q", banned)
		}
	}

	docs := get(handler, "/docs")
	if docs.Code != http.StatusOK || docs.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("docs %d %q", docs.Code, docs.Header().Get("Content-Type"))
	}
	const csp = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"
	if docs.Header().Get("Content-Security-Policy") != csp {
		t.Fatalf("csp %q", docs.Header().Get("Content-Security-Policy"))
	}
	for _, want := range []string{
		"https://cdn.jsdelivr.net/npm/@scalar/api-reference@1.74.0/esm.js",
		"sha384-fQ9g3BzMsV0YRs+bFQN9Vwro31LlcOIAb3WgtmfR5wFk1gfp1A9vjMaWtrr0i8H6",
		`data-url="/openapi.yaml"`,
		`crossorigin="anonymous"`,
	} {
		if !strings.Contains(docs.Body.String(), want) {
			t.Fatalf("missing %q", want)
		}
	}
	for _, banned := range []string{"proxy", "registry.scalar.com"} {
		if strings.Contains(docs.Body.String(), banned) {
			t.Fatalf("docs page contains %q", banned)
		}
	}

	disabled := newServer(false, contract.OpenAPI, readyOK(), logger)
	missing := get(disabled, "/docs")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("disabled docs %d", missing.Code)
	}

	logText := buf.String()
	if !strings.Contains(logText, `"path":"/healthz"`) || strings.Contains(logText, "token=secret") || strings.Contains(logText, "postgres://") {
		t.Fatalf("access log %s", logText)
	}
	if !strings.Contains(logText, `"method":"GET"`) || !strings.Contains(logText, `"status":200`) || !strings.Contains(logText, `"duration_ms"`) || !strings.Contains(logText, `"request_id"`) {
		t.Fatalf("access log fields %s", logText)
	}
}

func TestReadyz(t *testing.T) {
	ok := get(newServer(false, contract.OpenAPI, readyOK(), slog.Default()), "/readyz")
	if ok.Code != http.StatusOK || ok.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("ready %d %s", ok.Code, ok.Body.String())
	}
	for _, reason := range []string{"database_unreachable", "migrations_unavailable", "migrations_behind"} {
		handler := newServer(false, contract.OpenAPI, ReadinessFunc(func(context.Context) (bool, string) {
			return false, reason
		}), slog.Default())
		rec := get(handler, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status %d", reason, rec.Code)
		}
		if rec.Body.String() != `{"status":"not_ready","reason":"`+reason+"\"}\n" {
			t.Fatalf("%s body %s", reason, rec.Body.String())
		}
	}
}

func TestPanicEnvelope(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	handler := wrap(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := get(handler, "/boom")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "goroutine") || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("body leaked panic: %s", rec.Body.String())
	}
	var got struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Error.Code != "internal_error" || got.Error.Message != "The server could not complete the request." {
		t.Fatalf("error %#v", got.Error)
	}
	if rec.Header().Get("X-Request-ID") != got.Error.RequestID {
		t.Fatalf("header %q body %q", rec.Header().Get("X-Request-ID"), got.Error.RequestID)
	}
	if !strings.Contains(buf.String(), `"status":500`) || !strings.Contains(buf.String(), got.Error.RequestID) {
		t.Fatalf("panic log %s", buf.String())
	}
}

func TestRandomFailure(t *testing.T) {
	previous := readRandom
	readRandom = func([]byte) (int, error) { return 0, context.Canceled }
	t.Cleanup(func() { readRandom = previous })

	var buf bytes.Buffer
	handler := newServer(false, nil, readyOK(), slog.New(slog.NewJSONHandler(&buf, nil)))
	rec := get(handler, "/healthz")
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "goroutine") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"unavailable"`) {
		t.Fatalf("body %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), `"status":500`) {
		t.Fatalf("log %s", buf.String())
	}
}

func get(handler http.Handler, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func readyOK() Readiness {
	return ReadinessFunc(func(context.Context) (bool, string) { return true, "ok" })
}

func assertRequestID(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatalf("request id %q", id)
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("request id %q", id)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

From `backend/`:

```bash
go test ./internal/api/ -count=1 -run 'TestOperational|TestReadyz|TestPanic|TestRandom'
```

Expected: FAIL because `newServer` is undefined. `go test ./cmd/api` still passes because the original page is still embedded there.

- [ ] **Step 3: Implement middleware and routes**

Create `backend/internal/api/middleware.go`:

```go
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

type contextKey struct{}

var readRandom = rand.Read

func wrap(logger *slog.Logger, next http.Handler) http.Handler {
	return accessLog(logger, requestIDs(recoverPanic(logger, next)))
}

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", sw.Header().Get("X-Request-ID"),
		)
	})
}

func requestIDs(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := newRequestID(readRandom)
		if err != nil {
			w.Header().Set("X-Request-ID", "unavailable")
			WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", "unavailable", nil)
			return
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func newRequestID(read func([]byte) (int, error)) (string, error) {
	buf := make([]byte, 16)
	if _, err := read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func recoverPanic(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			id, _ := r.Context().Value(contextKey{}).(string)
			logger.Error("panic", "request_id", id, "err", rec, "stack", string(debug.Stack()))
			WriteError(w, http.StatusInternalServerError, "internal_error", "The server could not complete the request.", id, nil)
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}
```

Create `backend/internal/api/server.go`:

```go
package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"net/http"
)

//go:embed docs.html
var docsHTML []byte

const docsCSP = "default-src 'none'; script-src https://cdn.jsdelivr.net; style-src 'unsafe-inline'; font-src https://fonts.scalar.com; img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

type Readiness interface {
	Check(ctx context.Context) (ready bool, reason string)
}

type ReadinessFunc func(context.Context) (bool, string)

func (f ReadinessFunc) Check(ctx context.Context) (bool, string) {
	return f(ctx)
}

func New(docsEnabled bool, openAPI []byte, ready Readiness) http.Handler {
	return newServer(docsEnabled, openAPI, ready, slog.Default())
}

func newServer(docsEnabled bool, openAPI []byte, ready Readiness, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readyHandler(ready))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPI)
	})
	if docsEnabled {
		mux.HandleFunc("GET /docs", docsHandler)
	}
	return wrap(logger, mux)
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type readyBody struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

func readyHandler(ready Readiness) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, reason := ready.Check(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if ok {
			_ = json.NewEncoder(w).Encode(readyBody{Status: "ok"})
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(readyBody{Status: "not_ready", Reason: reason})
	}
}

func docsHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", docsCSP)
	_, _ = w.Write(docsHTML)
}
```

In `backend/openapi.yaml`, add this path after `/healthz`:

```yaml
  /readyz:
    get:
      operationId: getReady
      summary: Dependency readiness
      responses:
        "200":
          description: Required dependencies are ready.
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
        "503":
          description: A required dependency is not ready.
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                required: [status, reason]
                properties:
                  status:
                    const: not_ready
                  reason:
                    enum:
                      - database_unreachable
                      - migrations_unavailable
                      - migrations_behind
              example:
                status: not_ready
                reason: migrations_unavailable
```

In `backend/contract_test.go`, add `"/readyz"` to the wanted strings. Keep the bans on `/v1` and `bearer`.

Replace the comment in `backend/contract.go` with:

```go
// shortcut: module-root embed because go:embed cannot see a parent directory.
```

- [ ] **Step 4: Run the API tests**

From `backend/`:

```bash
gofmt -w internal/api/*.go contract.go
go test ./... -count=1
```

Expected: PASS, including `cmd/api`, because that package still embeds its own copy of `docs.html`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api backend/openapi.yaml backend/contract.go backend/contract_test.go
git commit -m "$(cat <<'EOF'
feat: serve operational routes from the HTTP package

EOF
)"
```

---

### Task 6: Process composition and shutdown

**Files:**
- Modify: `backend/cmd/api/main.go`
- Delete: `backend/cmd/api/main_test.go`
- Delete: `backend/cmd/api/docs.html`
- Create: `backend/internal/api/serve.go`
- Test: `backend/internal/api/serve_test.go`

**Interfaces:**
- Consumes: `config.Load`, `database.Open`, `database.Redact`, `(*database.Pool).Check`, `(*database.Pool).Close`, `api.New`, `api.ReadinessFunc`, `contract.OpenAPI`.
- Produces: `func Serve(ctx context.Context, srv *http.Server, timeout time.Duration, closePool func()) error`.

- [ ] **Step 1: Write the failing shutdown test**

Create `backend/internal/api/serve_test.go`:

```go
package api

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeClosesPoolWhenListenFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	closed := false
	srv := &http.Server{Addr: ln.Addr().String(), Handler: http.NewServeMux()}
	err = Serve(context.Background(), srv, time.Second, func() { closed = true })
	if err == nil || !closed {
		t.Fatalf("err %v closed %v", err, closed)
	}
}

func TestServeShutdownTimeoutClosesPool(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(started)
			<-release
		}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	closed := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, srv, 50*time.Millisecond, func() { close(closed) })
	}()

	deadline := time.Now().Add(2 * time.Second)
	var conn net.Conn
	for {
		conn, err = net.Dial("tcp", addr)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	select {
	case <-closed:
	default:
		t.Fatal("pool was not closed")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
	close(release)
}
```

- [ ] **Step 2: Run the test to verify it fails**

From `backend/`:

```bash
go test ./internal/api/ -count=1 -run TestServe
```

Expected: FAIL because `Serve` is undefined.

- [ ] **Step 3: Implement Serve and replace main**

Create `backend/internal/api/serve.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func Serve(ctx context.Context, srv *http.Server, timeout time.Duration, closePool func()) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		closePool()
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	closePool()
	return err
}
```

Replace `backend/cmd/api/main.go` with:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/metalhops/metalhops/backend"
	"github.com/metalhops/metalhops/backend/internal/api"
	"github.com/metalhops/metalhops/backend/internal/config"
	"github.com/metalhops/metalhops/backend/internal/database"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration", "err", err)
		os.Exit(1)
	}

	pool, err := database.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("database pool", "err", database.Redact(cfg.DatabaseURL, err.Error()))
		os.Exit(1)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.New(cfg.DocsUIEnabled, contract.OpenAPI, api.ReadinessFunc(pool.Check)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := api.Serve(ctx, srv, 15*time.Second, pool.Close); err != nil {
		slog.Error("server", "err", database.Redact(cfg.DatabaseURL, err.Error()))
		os.Exit(1)
	}
}
```

Delete `backend/cmd/api/main_test.go` and `backend/cmd/api/docs.html`.

- [ ] **Step 4: Run the full backend tests**

From `backend/`:

```bash
gofmt -w cmd/api/main.go internal/api/serve.go internal/api/serve_test.go
go test ./... -count=1
```

Expected: PASS. `TestMigrationVersions` skips locally when Docker is unavailable and passes when Docker can pull `postgis/postgis:16-3.5`.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api backend/internal/api/serve.go backend/internal/api/serve_test.go
git commit -m "$(cat <<'EOF'
feat: compose the API process and drain it on shutdown

EOF
)"
```

---

### Task 7: Guide updates

**Files:**
- Modify: `backend/.env.example`
- Modify: `backend/AGENTS.md`
- Modify: `docs/architecture/backend.md`

**Interfaces:**
- Consumes: the running behavior from Task 6.
- Produces: no Go symbols.

- [ ] **Step 1: Update the example environment**

Replace `backend/.env.example` with:

```text
PORT=8080
# Supabase session pooler URL (port 5432 on the pooler host).
DATABASE_URL=
# Unused until the authentication session. Session 3 does not read this.
SUPABASE_JWT_SECRET=
# Scalar UI at /docs. Unset or true enables it. Render sets false.
DOCS_UI_ENABLED=true
```

- [ ] **Step 2: Replace the backend runtime section**

In `backend/AGENTS.md`, replace the `## Documentation routes` section with:

```markdown
## Runtime

Go 1.23. `make run` from `backend/` reads `.env` in the current directory
when that file exists. Variables already in the environment win.

`GET /healthz` reports process health and does not check the database.
`GET /readyz` returns 200 when the database ping succeeds and migration
`0001` or newer is recorded. Otherwise it returns 503.
`GET /openapi.yaml` serves the embedded `openapi.yaml` in every environment.
`GET /docs` serves the Scalar page when `DOCS_UI_ENABLED` is unset or `true`.
The exact value `false` leaves `/docs` unregistered. Any other value stops
the process at startup. Local `.env.example` sets `true`. Render sets `false`.

HTTP behavior lives in `internal/api`. The contract embed remains
`backend/contract.go`. `docs.html` lives beside the HTTP package.
```

- [ ] **Step 3: Update the architecture guide**

In `docs/architecture/backend.md`, delete these two transitional bullets:

- `` `cmd/api` still owns `/healthz`; the backend-foundation session moves HTTP behavior behind the HTTP module. ``
- `` `go.mod` may lag the Go 1.23 toolchain until foundation work updates module metadata and dependencies together. ``

Delete the transitional bullet that says the backend-foundation session moves the `contract.go` embed. Under the module map, after the tree and before "This tree names ownership", add:

```markdown
`openapi.yaml` stays at `backend/openapi.yaml` and is embedded by
`backend/contract.go`, because `go:embed` cannot reference a parent
directory. The composition root passes those bytes into `internal/api`.
`docs.html` is embedded from `internal/api`.
```

Leave the migration `0001_init.sql` bullet and the legacy JWT-secret bullet in Transitional state. Do not edit `DevSteps.md` or `infra/render.yaml`.

- [ ] **Step 4: Verify the suite**

From `backend/`:

```bash
gofmt -l .
go vet ./...
go test ./... -count=1
```

Expected: `gofmt -l .` prints nothing. `go vet` and `go test` pass.

- [ ] **Step 5: Commit**

```bash
git add backend/.env.example backend/AGENTS.md docs/architecture/backend.md
git commit -m "$(cat <<'EOF'
docs: describe the backend foundation runtime

EOF
)"
```

### Task 8: Hosted migration handoff

**Files:**
- Create: `infra/supabase/config.toml`
- Create: `infra/supabase/.gitignore` when `supabase init` creates one
- Modify: `infra/README.md`

**Interfaces:**
- Consumes: the running API from Task 6 and migration `infra/supabase/migrations/0001_init.sql`.
- Produces: no Go symbols. The operator runs the commands below. The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.

- [ ] **Step 1: Initialize the CLI project**

From the repo root, install the CLI if `supabase --version` fails:

```bash
brew install supabase/tap/supabase
```

Then:

```bash
supabase init --workdir infra
```

`infra/supabase/migrations/0001_init.sql` must remain the only migration file. Do not rename it. Commit `infra/supabase/config.toml`. Commit `infra/supabase/.gitignore` when init creates it. Leave every other file init creates untracked, including `seed.sql`. Do not commit `.temp`, `.branches`, or any file that contains a database password or access token. Do not run `supabase start`. Leave `major_version` as `supabase init` writes it. If `link` or `db push` reports a Postgres major-version mismatch, stop and ask the operator for the hosted major version before changing that one field. Set `project_id` to `metalhops` when init writes any other value. Do not change ports or enabled services.

- [ ] **Step 2: Prepare the operator commands**

Do not send these commands until Step 4's commit exists. The pooler fallback below stays in this plan. Do not copy it into `infra/README.md`.

```bash
supabase login
supabase link --workdir infra --project-ref YOUR_PROJECT_REF
supabase db push --workdir infra
```

`supabase link` prompts for the database password in the terminal. That password stays out of the repo and out of chat. `db push` uses the CLI's linked direct connection. The API's `DATABASE_URL` stays the session-pooler URL, port 5432 on the pooler host. Do not run `supabase db reset --linked`.

If `db push` cannot reach the direct host, run this from the repo root instead. When `DATABASE_URL` is already set in the environment, that value wins, including when it is empty. Otherwise the command reads `backend/.env` with the API's line rule: skip blanks and comments, strip one optional `export ` prefix, and keep everything after the first `=` on the last `DATABASE_URL` line. Quotes stay. The port must be 5432. Do not use port 6543. Do not print the URL.

```bash
if [ -n "${DATABASE_URL+x}" ]; then
  url=$DATABASE_URL
else
  url=$(awk '
    {
      line = $0
      sub(/^[ \t]+/, "", line)
      if (line ~ /^#/ || line == "") next
      sub(/^export /, "", line)
      n = index(line, "=")
      if (n == 0) next
      key = substr(line, 1, n - 1)
      gsub(/^[ \t]+|[ \t]+$/, "", key)
      if (key == "DATABASE_URL") value = substr(line, n + 1)
    }
    END { print value }
  ' backend/.env)
fi
supabase db push --workdir infra --db-url "$url"
```

From `backend/`, with `DATABASE_URL` set to that pooler URL:

```bash
make run
```

From another terminal:

```bash
curl -sS http://localhost:8080/readyz
```

Expected body: `{"status":"ok"}`.

If the body is `{"status":"not_ready","reason":"migrations_unavailable"}`, the history row is missing. In the Supabase SQL editor, run:

```sql
select version from supabase_migrations.schema_migrations;
```

The expected version is `0001`. If `db push` rejects `0001_init.sql`, stop and report the CLI error. Do not rename the migration in this session.

- [ ] **Step 3: Record the commands**

Replace the migrations bullet in `infra/README.md` with:

```markdown
- `supabase/migrations/`: SQL schema (PostGIS, venues, genres, RLS). From the repo root, `supabase db push --workdir infra` applies these files to the linked project and records their versions. The SQL editor does not write `supabase_migrations.schema_migrations`, so it does not make `/readyz` ready. `config.toml` is the CLI project file. Do not commit `.temp` or link credentials.
```

- [ ] **Step 4: Commit the CLI project file**

```bash
git add infra/supabase/config.toml infra/README.md
git add infra/supabase/.gitignore
git commit -m "$(cat <<'EOF'
chore: add the Supabase CLI project file

EOF
)"
```

If `.gitignore` was not created, omit that `git add`. After the commit, send the Step 2 commands and wait. The session handoff is complete when the operator replies with `{"status":"ok"}`. That reply is not a commit, and it does not change `DevSteps.md`.

## Plan self-check

- Configuration, `.env`, envelopes, pool, migration check, HTTP routes, middleware, shutdown, guide updates, and the hosted-project handoff each have a task.
- `Readiness.Check`, `New`, `Open`, `Pool.Check`, `Load`, `Serve`, and `Redact` keep the same signatures after the task that introduces them.
- No task adds SQL execution on startup, a new migration, JWT parsing, rate limits, or a `cmd/api` test.
- The implementer does not run `supabase login`, `supabase link`, or `supabase db push`.
