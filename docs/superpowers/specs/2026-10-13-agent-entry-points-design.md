# Agent entry points (Cursor and Claude Code)

**Date:** 2026-10-13
**Status:** Approved design, not yet implemented

## Goal

Give Cursor and Claude Code the same project instructions, so that an agent
working in this monorepo knows the stacks, stays focused on the stack a task
names, and follows that stack's conventions. The initial focus is the Go
backend API.

## Constraints

- Everyone opens the **repository root**, never a stack folder on its own.
- Most tasks touch a single stack. Agents use a **soft focus**: they start in
  the stack the task names and follow its conventions. They may change shared
  files (infra, CI, docs) when the task requires it. They must not edit
  another stack's code without asking.
- Instructions must not drift between tools. Each fact lives in one file.
- Project files must not depend on any contributor's personal agent setup
  (for example, a personal `~/CLAUDE.md` or CLI wrappers).

## Approach

One canonical `AGENTS.md` per scope. Claude Code gets a `CLAUDE.md` shim
beside each one that only imports it.

Rejected alternatives:

- **Full copies per tool** (complete `CLAUDE.md` and a complete Cursor rule):
  every change has to be made twice, and the copies drift.
- **Human docs as the source** (`docs/` holds the guide, agent files only
  point to it): agents often stop at the pointer and miss the stack rules.

## File layout

```text
metalhops/
├── AGENTS.md            # canonical root map, loaded every session
├── CLAUDE.md            # contains only: @AGENTS.md
├── backend/
│   ├── AGENTS.md        # canonical backend guide
│   └── CLAUDE.md        # contains only: @AGENTS.md
└── android/
    └── AGENTS.md        # stub: not started, ask before scaffolding
```

How each tool discovers the files when the root is open:

- **Cursor** reads the root `AGENTS.md` and nested `AGENTS.md` files for the
  folders being worked in.
- **Claude Code** reads the root `CLAUDE.md` and loads `backend/CLAUDE.md`
  when it works on backend files. The `@AGENTS.md` import pulls in the
  canonical text.

Out of scope for now:

- `.cursor/rules/`: add `.cursor/rules/backend.mdc` (pointing to
  `backend/AGENTS.md`) only if verification shows Cursor missing the nested
  guide.
- An `ios/` guide: the folder holds only a placeholder until iOS work starts.
- An `infra/` guide: the backend guide covers which `infra/` paths backend
  work may change.
- `android/CLAUDE.md`: added when Android work starts.

## File contents

### Root `AGENTS.md`

Target: under about 60 lines, because both tools load it every session.

1. **What this is:** one sentence about MetalHops and a link to `README.md`
   for the product vision (not copied).
2. **Stacks:** `backend/` (Go API, active), `android/` (Kotlin, not started),
   `infra/` (Supabase migrations, Render blueprint), `.github/workflows/` (CI).
3. **Current focus:** "Backend API." This single line is updated when
   priorities change.
4. **Scope rule:** work in the stack the task names. Change shared files only
   when the task needs them. Ask before editing another stack's code.
5. **Before working in a stack:** read that folder's `AGENTS.md`.
6. **Repo-wide rules:** `feature/*` branches merge into `dev` by pull request;
   never commit secrets (`.env` stays local, `.env.example` is the template);
   follow `.editorconfig`; write commands with plain tools, never personal
   CLI wrappers or aliases.

### `backend/AGENTS.md`

1. **Commands:** `cp .env.example .env`, then `make run` (serves
   `http://localhost:8080/healthz`), `make test`, `make vet`, `make build`.
2. **Layout:** `cmd/api` is the entry point only; domain code goes under
   `internal/`.
3. **Definition of done:** `gofmt` is clean, and `go vet ./...` and
   `go test ./...` pass, matching CI. Every new endpoint ships with a test.
4. **Dependencies:** ask before adding any dependency to `go.mod`. The Go
   standard library needs no approval. Candidate libraries from `README.md`
   (`chi`, `pgx` + `sqlc`, `goose`, OpenAPI tooling, `testcontainers-go`) are
   options to propose, not defaults.
5. **Shared paths backend work may change:**
   - `infra/supabase/migrations/`: add a new numbered file; never edit a
     migration that is already on `dev` or `main`. Every new table enables row-level
     security and defines its policies in the same migration.
   - `infra/render.yaml`
   - `.github/workflows/backend.yml`
6. **Auth model:** apps send a Supabase JWT; the Go API verifies it. The
   Supabase service-role key exists only in server environment variables,
   never in the repo or the apps.

### `android/AGENTS.md`

A stub of a few lines: the Android app is planned (Kotlin, Jetpack Compose)
but not created. Ask before scaffolding it.

### `CLAUDE.md` shims

Root and `backend/` each contain exactly one line: `@AGENTS.md`.

## Maintenance

Where a new fact goes:

- Applies to the whole repo (branching, secrets): root `AGENTS.md`.
- Applies to one stack (commands, layout, dependency rules): that stack's
  `AGENTS.md`.
- Applies to only one tool: avoid. If unavoidable, put it in
  `.cursor/rules/` or the `CLAUDE.md` shim with a note explaining why.
- Product vision stays in `README.md`; agent files link to it.

When things change:

- Priorities shift: edit the root "Current focus" line.
- Android (or iOS) work starts: replace the stub with a real guide and add the
  `CLAUDE.md` shim.
- A command or path changes: update the stack guide in the same pull request.

## Verification

Run once after the files land, with the repository root open:

1. In Claude Code, ask: "How do I run the backend tests, and can I add
   `chi`?" Expected: `make test`, and that adding `chi` needs approval first.
2. In Cursor, with a backend file open, ask the same question. Expected: the
   same answers.
3. In either tool, ask for a change in `android/`. Expected: the agent stops
   and confirms, because Android is out of scope and not started.

If Cursor fails step 2, add `.cursor/rules/backend.mdc` pointing to
`backend/AGENTS.md` and repeat.

## Known issue (separate change)

`backend/go.mod` declares Go 1.22 while CI uses Go 1.23. Align them in a
separate change; the backend guide should state the single agreed version once
that is done.
