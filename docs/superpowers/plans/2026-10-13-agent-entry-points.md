# Agent Entry Points Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shared Cursor and Claude Code instruction files so agents working from the monorepo root follow the right stack's conventions, starting with the Go backend.

**Architecture:** One canonical `AGENTS.md` per scope (root, `backend/`, `android/`). Cursor reads these natively. Claude Code reads a one-line `CLAUDE.md` beside the root and backend guides that imports the sibling `AGENTS.md`.

**Tech Stack:** Markdown only. Checks use `wc`, `grep`, and `git` from a POSIX shell.

**Spec:** `docs/superpowers/specs/2026-10-13-agent-entry-points-design.md`

## Global Constraints

- Everyone opens the repository root; files must work from there.
- Soft focus: work in the stack the task names; change shared files only when the task needs them; ask before editing another stack's code.
- Each fact lives in exactly one file. `CLAUDE.md` files contain exactly one line: `@AGENTS.md`.
- Project files must not depend on personal agent setup (no `rtk` or other personal CLI wrappers in commands).
- Root `AGENTS.md` stays under about 60 lines.
- Ask before adding any dependency to `backend/go.mod`; the Go standard library needs no approval.
- Product vision stays in `README.md`; agent files link to it rather than copying it.
- Do not create `.cursor/rules/`, `ios/`, an `infra/` guide, or `android/CLAUDE.md`.
- Do not change the Go version in `go.mod` or CI in this plan.

## Review Focus

- A Claude Code session started at the root must see the root guide: root `CLAUDE.md` is exactly `@AGENTS.md` (Task 1 checks the byte content).
- An agent asked about backend dependencies must answer "ask first", not pick a README library: `backend/AGENTS.md` names the rule and lists the README libraries only as candidates (Task 2 greps for both).
- An agent asked to touch `android/` must stop and confirm: root scope rule and `android/AGENTS.md` both say to ask (Tasks 1 and 3 grep for it).
- A contributor without `rtk` must be able to copy every command: no `rtk` string in any agent file (Task 4 greps for it).
- Migration edits must not rewrite history: `backend/AGENTS.md` forbids editing applied migrations and requires row-level security (Task 2 greps for both).

---

### Task 1: Root guide and Claude shim

**Files:**
- Create: `AGENTS.md`
- Create: `CLAUDE.md`

**Interfaces:**
- Consumes: nothing.
- Produces: root `AGENTS.md` that names `backend/AGENTS.md` and `android/AGENTS.md` as the stack guides (Tasks 2 and 3 create them).

- [ ] **Step 1: Run the check to confirm the files are missing**

Run:

```bash
test -f AGENTS.md && echo exists || echo missing; test -f CLAUDE.md && echo exists || echo missing
```

Expected: `missing` twice.

- [ ] **Step 2: Create `AGENTS.md`**

```markdown
# MetalHops agent guide

MetalHops is an Untappd-style app for finding rock and metal bars. Product
vision and roadmap live in [README.md](README.md); read it for context, but
the rules below are what to follow.

## Stacks

| Path | What | Status |
| --- | --- | --- |
| `backend/` | Go REST API (Render) | Active |
| `android/` | Kotlin, Jetpack Compose app | Not started |
| `infra/` | Supabase migrations, Render blueprint | Shared |
| `.github/workflows/` | CI | Shared |

## Current focus

Backend API.

## Scope

- The repository is always opened at the root, but most tasks touch one stack.
- Work in the stack the task names and follow that stack's guide.
- Change shared files (`infra/`, CI, docs) only when the task needs them.
- Ask before editing another stack's code.

## Before working in a stack

Read that folder's `AGENTS.md` first:

- `backend/AGENTS.md`
- `android/AGENTS.md`

## Repo-wide rules

- Branch from `dev` as `feature/<name>`; merge into `dev` by pull request.
  `main` is release-ready only.
- Never commit secrets. `.env` files stay local; `.env.example` is the
  template.
- Follow `.editorconfig`.
- Keep these agent files current: when a command, path, or rule changes,
  update the matching `AGENTS.md` in the same pull request.
```

- [ ] **Step 3: Create `CLAUDE.md`**

The file contains exactly one line:

```markdown
@AGENTS.md
```

- [ ] **Step 4: Run the checks**

Run:

```bash
wc -l < AGENTS.md
printf '@AGENTS.md\n' | cmp - CLAUDE.md && echo shim-ok
grep -c 'backend/AGENTS.md\|android/AGENTS.md' AGENTS.md
grep -q 'Ask before editing another stack' AGENTS.md && echo scope-ok
grep -q '^Backend API\.$' AGENTS.md && echo focus-ok
```

Expected: a line count under 60, then `shim-ok`, `2`, `scope-ok`, `focus-ok`.

- [ ] **Step 5: Commit**

```bash
git add AGENTS.md CLAUDE.md
git commit -m "docs: add root agent guide and Claude shim"
```

---

### Task 2: Backend guide and Claude shim

**Files:**
- Create: `backend/AGENTS.md`
- Create: `backend/CLAUDE.md`

**Interfaces:**
- Consumes: root `AGENTS.md` points here (Task 1).
- Produces: nothing other tasks rely on.

- [ ] **Step 1: Run the check to confirm the files are missing**

Run:

```bash
test -f backend/AGENTS.md && echo exists || echo missing; test -f backend/CLAUDE.md && echo exists || echo missing
```

Expected: `missing` twice.

- [ ] **Step 2: Create `backend/AGENTS.md`**

```markdown
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

- `cmd/api/`: entry point only (server setup and routing).
- `internal/`: all domain code.

## Definition of done

- `gofmt -l .` prints nothing.
- `go vet ./...` and `go test ./...` pass; CI runs the same checks.
- Every new endpoint ships with a test.

## Dependencies

- Ask before adding any dependency to `go.mod`. The Go standard library needs
  no approval.
- Libraries suggested in the root README are candidates to propose, not
  defaults: `chi`, `pgx` + `sqlc`, `goose`, OpenAPI tooling,
  `testcontainers-go`.

## Shared paths backend work may change

- `infra/supabase/migrations/`: add a new numbered file (`0002_<name>.sql`,
  and so on). Never edit a migration that has been applied. Every new table
  enables row-level security and defines its policies in the same migration.
- `infra/render.yaml`: Render blueprint for this service.
- `.github/workflows/backend.yml`: backend CI.

## Auth model

- The apps sign in with Supabase Auth and send the JWT to this API, which
  verifies it.
- The Supabase service-role key exists only in server environment variables,
  never in the repo or the apps.
```

- [ ] **Step 3: Create `backend/CLAUDE.md`**

The file contains exactly one line:

```markdown
@AGENTS.md
```

- [ ] **Step 4: Run the checks**

Run:

```bash
printf '@AGENTS.md\n' | cmp - backend/CLAUDE.md && echo shim-ok
grep -q 'Ask before adding any dependency' backend/AGENTS.md && echo deps-ok
grep -q 'candidates to propose, not' backend/AGENTS.md && echo candidates-ok
grep -q 'Never edit a migration that has been applied' backend/AGENTS.md && echo migrations-ok
grep -q 'row-level security' backend/AGENTS.md && echo rls-ok
(cd backend && make -n run test vet build >/dev/null) && echo make-targets-ok
```

Expected: `shim-ok`, `deps-ok`, `candidates-ok`, `migrations-ok`, `rls-ok`, `make-targets-ok`.

- [ ] **Step 5: Commit**

```bash
git add backend/AGENTS.md backend/CLAUDE.md
git commit -m "docs: add backend agent guide and Claude shim"
```

---

### Task 3: Android stub

**Files:**
- Create: `android/AGENTS.md`

**Interfaces:**
- Consumes: root `AGENTS.md` points here (Task 1).
- Produces: nothing.

- [ ] **Step 1: Run the check to confirm the file is missing**

Run:

```bash
test -f android/AGENTS.md && echo exists || echo missing
```

Expected: `missing`.

- [ ] **Step 2: Create `android/AGENTS.md`**

```markdown
# Android agent guide

The Android app (Kotlin, Jetpack Compose) is planned but not created yet.
Ask before scaffolding it or adding files here. When work starts, replace this
stub with a real guide and add a `CLAUDE.md` containing `@AGENTS.md`.
```

- [ ] **Step 3: Run the checks**

Run:

```bash
grep -q 'Ask before scaffolding' android/AGENTS.md && echo ask-ok
test ! -e android/CLAUDE.md && echo no-shim-ok
```

Expected: `ask-ok`, `no-shim-ok`.

- [ ] **Step 4: Commit**

```bash
git add android/AGENTS.md
git commit -m "docs: add Android agent guide stub"
```

---

### Task 4: Repository-wide checks and tool verification

**Files:**
- Modify: none, unless verification fails (see Step 4).

**Interfaces:**
- Consumes: all files from Tasks 1–3.
- Produces: verification results reported to the user.

- [ ] **Step 1: Run the repository-wide checks**

Run:

```bash
! grep -rn 'rtk' AGENTS.md CLAUDE.md backend/AGENTS.md backend/CLAUDE.md android/AGENTS.md && echo no-personal-tools-ok
test ! -e .cursor/rules && test ! -e ios && test ! -e infra/AGENTS.md && echo out-of-scope-ok
git check-ignore AGENTS.md CLAUDE.md backend/AGENTS.md backend/CLAUDE.md android/AGENTS.md || echo not-ignored-ok
git status --short
```

Expected: `no-personal-tools-ok`, `out-of-scope-ok`, `not-ignored-ok`, and an empty `git status`.

- [ ] **Step 2: Verify Claude Code (manual, user runs this)**

Open Claude Code at the repository root and ask:
"How do I run the backend tests, and can I add `chi`?"

Expected: it answers `make test` (from `backend/`) and says adding `chi` needs approval first.

- [ ] **Step 3: Verify Cursor (manual, user runs this)**

Open the repository root in Cursor, open `backend/cmd/api/main.go`, and ask the same question in a new chat.

Expected: the same two answers. Then ask either tool: "Add a `MainActivity.kt` in `android/`." Expected: it stops and asks before creating anything.

- [ ] **Step 4: Fallback only if Cursor fails Step 3**

Create `.cursor/rules/backend.mdc`:

```markdown
---
description: Backend (Go API) conventions
globs: backend/**
alwaysApply: false
---

Follow `backend/AGENTS.md` for all work under `backend/`.
```

Repeat Step 3, then commit:

```bash
git add .cursor/rules/backend.mdc
git commit -m "docs: add Cursor rule pointing to backend guide"
```
