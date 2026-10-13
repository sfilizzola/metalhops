# Session 1: Repository boundaries

**Date:** 2026-10-13
**Status:** Approved design, not yet implemented

## Goal

Make Android, the Go API, the future admin web app, and shared infrastructure
distinguishable before any admin implementation exists. Session 1 of
[DevSteps](../../../DevSteps.md) is documentation only.

## Constraints

- Add `admin/README.md` and `admin/AGENTS.md`. Do not choose or scaffold a web
  framework, and do not add application code.
- `admin/AGENTS.md` is the only full copy of the admin boundary. Product
  behavior stays in DevSteps.
- The root agent guide stays the map loaded every session. Current focus stays
  "Backend API."
- The human roadmap separates operator moderation during the API work from the
  later admin app.
- No `admin/CLAUDE.md` until admin work starts, matching the Android stub.
- Leave the 2026-10-13 agent-entry-points spec as a historical record.

## Approach

One canonical boundary in `admin/AGENTS.md`. The root map and the README only
name the scope and point at that guide. `admin/README.md` states the boundary
in the human text below. It is a shorter page, and the agent guide remains the
full copy.

Rejected alternatives:

- **Load the boundary every session.** Putting the admin rules in the root
  `AGENTS.md` repeats a stack-specific fact in the file every task loads, and
  spends the root guide's line budget while the focus is still the backend.
- **Mirror the boundary everywhere now.** Copying it into both admin files,
  adding `admin/CLAUDE.md`, and updating the agent-entry-points spec means
  every later edit has to touch each copy.

## Files

### Root `AGENTS.md`

Insert this row after `android/`:

| Path | What | Status |
| --- | --- | --- |
| `admin/` | Moderation web app; documentation only until its own design cycle | Not started |

Add `admin/AGENTS.md` to the "before working" list. Leave the current-focus
line as "Backend API."

The resulting stacks table order is `backend/`, `android/`, `admin/`,
`infra/`, `.github/workflows/`.

### `README.md`

Leave the product-approach paragraph and the repository table as they are.
They already name `admin/`.

In the roadmap checklist, replace "Verification and moderation flow" with the
SQL-runbook line in that same position. Place the admin-app line after Android
beta and before iOS. The tail of the checklist becomes:

```markdown
- [ ] Android MVP: map, venue pages, submissions, ratings
- [ ] Operator moderation through the SQL runbook, as part of the API roadmap ([DevSteps](DevSteps.md))
- [ ] Android beta (Google Play internal/closed testing)
- [ ] Admin web app, in its own design cycle after the API
- [ ] iOS app (Swift / SwiftUI)
```

Earlier checklist lines stay unchanged, including the monorepo setup line.

### `admin/AGENTS.md`

```markdown
# Admin agent guide

The moderation web app is a future stack. This folder is documentation only.
Ask before adding files here. Do not choose or scaffold a web framework.

The future app calls privileged endpoints on the Go API. Supabase product
tables and the service-role key stay off this stack. There is no separate
admin backend.

Operators moderate through the SQL runbook until that app exists. DevSteps
places the runbook at `docs/operations/moderation.md`. Later API sessions add
that file.

Profile, Venue, Review, and report behavior lives in
[DevSteps](../DevSteps.md). Link to it from here.

When admin work starts, replace this stub with a real guide and add a
`CLAUDE.md` containing `@AGENTS.md`.
```

### `admin/README.md`

```markdown
# MetalHops admin

Future moderation web app. This folder is documentation only. The framework,
screens, and moderation API contract wait for a separate design cycle.

The app will call privileged endpoints on the Go API. Supabase product tables
and the service-role key stay off this stack. Until the app exists, operators
use the SQL runbook described in [DevSteps](../DevSteps.md).

Agent rules for this folder: [AGENTS.md](AGENTS.md).
```

## Out of scope

- Framework choice, package files, screens, and a moderation HTTP contract.
- `admin/CLAUDE.md`.
- `docs/operations/moderation.md`. Later API sessions add that runbook.
- Edits to `docs/superpowers/specs/2026-10-13-agent-entry-points-design.md`.
- Backend code, migrations, OpenAPI, and the backend architecture guide.

## Exit condition

A reader can tell the scopes apart from the docs alone:

- Backend is the active Go API.
- Android is a future app and is not started.
- Admin is a future moderation web app, documentation only, with the boundary
  in `admin/AGENTS.md`.
- Infra and CI stay shared.

Confirm by reading the four files:

- The root stacks table and "before working" list include `admin/`.
- Current focus is still "Backend API."
- `admin/AGENTS.md` is the only full copy of the boundary and does not choose
  or scaffold a framework.
- `admin/README.md` matches the human page in this spec and links to the guide
  and DevSteps.
- The README roadmap has the SQL-runbook line before Android beta, and the
  admin-app line after Android beta and before iOS.
- The session adds no `admin/CLAUDE.md`, application code, framework files, or
  `docs/operations/moderation.md`.

Work happens on `feature/admin-repository-boundaries` and targets `dev` by
pull request. The Go checks in DevSteps (`gofmt`, `go vet`, `go test`,
OpenAPI, migrations) are not acceptance criteria for this session, because no
backend file changes.
