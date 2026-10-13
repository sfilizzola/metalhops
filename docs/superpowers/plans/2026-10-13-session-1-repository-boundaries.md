# Session 1 Repository Boundaries Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Android, the Go API, the future admin web app, and shared infrastructure distinguishable with documentation only.

**Architecture:** `admin/AGENTS.md` is the only full copy of the admin boundary. `admin/README.md` is a shorter human page. The root agent guide and the README roadmap only name the scope and point at the right document.

**Tech Stack:** Markdown only. Checks use `cmp`, `grep`, `find`, and `git` from a POSIX shell. Do not run Go tests; this session changes no backend file.

**Spec:** `docs/superpowers/specs/2026-10-13-session-1-repository-boundaries-design.md`

## Global Constraints

- Add `admin/README.md` and `admin/AGENTS.md`. Do not choose or scaffold a web framework, and do not add application code.
- `admin/AGENTS.md` is the only full copy of the admin boundary. Product behavior stays in DevSteps.
- The root agent guide stays the map loaded every session. Current focus stays "Backend API."
- The human roadmap separates operator moderation during the API work from the later admin app.
- No `admin/CLAUDE.md` until admin work starts, matching the Android stub.
- Leave `docs/superpowers/specs/2026-10-13-agent-entry-points-design.md` unchanged.
- Do not create `docs/operations/moderation.md`.
- Do not edit the product-approach paragraph, the repository table, or earlier roadmap lines in `README.md`.
- Work on `feature/admin-repository-boundaries`, targeting `dev` by pull request.
- Commands in project files use plain tools (`git`, `grep`, `cmp`), never a personal CLI wrapper.

## Review Focus

- An agent asked to start the admin app scaffolds a web framework. Expected: `admin/AGENTS.md` forbids scaffolding and names no framework. Pinned in Task 1.
- An agent connects admin to Supabase product tables or the service-role key. Expected: both admin files keep those off this stack, and the root guide does not repeat that rule. Pinned in Tasks 1, 2, and 3.
- An agent creates `docs/operations/moderation.md` because the guide names that path. Expected: the file is absent, and `admin/AGENTS.md` says later API sessions add it. Pinned in Tasks 1 and 5.
- An agent treats admin as the active stack. Expected: current focus stays `Backend API.` and the admin row says `Not started`. Pinned in Task 3.
- A reader treats operator moderation and the admin app as one step before Android beta. Expected: the SQL-runbook line stays before Android beta, and the admin-app line sits after Android beta and before iOS. Pinned in Task 4.

## Setup

Local `dev` already contains the spec commit. Start the feature branch from that commit. Do not reset `dev`.

```bash
git checkout -b feature/admin-repository-boundaries
git add docs/superpowers/plans/2026-10-13-session-1-repository-boundaries.md
git commit -m "$(cat <<'EOF'
docs: plan session 1 repository boundaries

Give the documentation-only admin boundary a task list before any framework is chosen.

EOF
)"
```

Expected: the branch exists, the plan is committed, and `admin/` does not exist yet.

---

### Task 1: Admin agent guide

**Files:**
- Create: `admin/AGENTS.md`

**Interfaces:**
- Consumes: nothing.
- Produces: `admin/AGENTS.md`, the canonical boundary. Task 2 links to the filename `AGENTS.md` from inside `admin/`. Task 3 lists the path `admin/AGENTS.md`. Task 5 expects this file to be one of exactly two files in `admin/`.

- [ ] **Step 1: Confirm the guide is missing**

Run:

```bash
test -f admin/AGENTS.md && echo exists || echo missing
```

Expected: `missing`

- [ ] **Step 2: Create `admin/AGENTS.md`**

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

- [ ] **Step 3: Check the guide**

Run:

```bash
cmp - admin/AGENTS.md <<'EOF'
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
EOF
echo "cmp-exit:$?"
grep -c 'Do not choose or scaffold a web framework' admin/AGENTS.md
grep -c 'service-role' admin/AGENTS.md
grep -c 'Later API sessions add' admin/AGENTS.md
grep -E -i -c 'react|vue|svelte|htmx|next\.js' admin/AGENTS.md || true
```

Expected: no `cmp` diff, then `cmp-exit:0`, `1`, `1`, `1`, and `0`.

- [ ] **Step 4: Commit**

```bash
git add admin/AGENTS.md
git commit -m "$(cat <<'EOF'
docs: add the admin agent boundary

Keep agents from scaffolding a framework or using Supabase for product data.

EOF
)"
```

---

### Task 2: Admin human page

**Files:**
- Create: `admin/README.md`

**Interfaces:**
- Consumes: `admin/AGENTS.md` from Task 1. The page links to `AGENTS.md` in the same folder.
- Produces: `admin/README.md`. Task 5 expects this file to be the other of the two files in `admin/`.

- [ ] **Step 1: Confirm the page is missing**

Run:

```bash
test -f admin/README.md && echo exists || echo missing
```

Expected: `missing`

- [ ] **Step 2: Create `admin/README.md`**

```markdown
# MetalHops admin

Future moderation web app. This folder is documentation only. The framework,
screens, and moderation API contract wait for a separate design cycle.

The app will call privileged endpoints on the Go API. Supabase product tables
and the service-role key stay off this stack. Until the app exists, operators
use the SQL runbook described in [DevSteps](../DevSteps.md).

Agent rules for this folder: [AGENTS.md](AGENTS.md).
```

- [ ] **Step 3: Check the page is the shorter copy**

Run:

```bash
cmp - admin/README.md <<'EOF'
# MetalHops admin

Future moderation web app. This folder is documentation only. The framework,
screens, and moderation API contract wait for a separate design cycle.

The app will call privileged endpoints on the Go API. Supabase product tables
and the service-role key stay off this stack. Until the app exists, operators
use the SQL runbook described in [DevSteps](../DevSteps.md).

Agent rules for this folder: [AGENTS.md](AGENTS.md).
EOF
echo "cmp-exit:$?"
grep -c 'service-role' admin/README.md
grep -c '\[AGENTS.md\](AGENTS.md)' admin/README.md
grep -c 'DevSteps' admin/README.md
grep -c 'Ask before adding files' admin/README.md || true
grep -c 'docs/operations/moderation.md' admin/README.md || true
grep -c 'CLAUDE.md' admin/README.md || true
```

Expected: no `cmp` diff, then `cmp-exit:0`, `1`, `1`, `1`, `0`, `0`, and `0`.

- [ ] **Step 4: Commit**

```bash
git add admin/README.md
git commit -m "$(cat <<'EOF'
docs: add the admin folder introduction

Give human readers the boundary without copying the agent rules.

EOF
)"
```

---

### Task 3: Root agent map

**Files:**
- Modify: `AGENTS.md` (stacks table and "before working" list)

**Interfaces:**
- Consumes: the path `admin/AGENTS.md` produced by Task 1.
- Produces: a root stacks table in the order `backend/`, `android/`, `admin/`, `infra/`, `.github/workflows/`, and a "before working" list that includes `admin/AGENTS.md`.

- [ ] **Step 1: Confirm the root map does not name admin yet**

Run:

```bash
grep -c 'admin/' AGENTS.md || true
grep -c '^Backend API\.$' AGENTS.md
```

Expected: `0` and `1`.

- [ ] **Step 2: Update the stacks table and the before-working list**

Replace the stacks table with:

```markdown
| Path | What | Status |
| --- | --- | --- |
| `backend/` | Go REST API (Render) | Active |
| `android/` | Kotlin, Jetpack Compose app | Not started |
| `admin/` | Moderation web app; documentation only until its own design cycle | Not started |
| `infra/` | Supabase migrations, Render blueprint | Shared |
| `.github/workflows/` | CI | Shared |
```

Replace the before-working list with:

```markdown
- `backend/AGENTS.md`
- `android/AGENTS.md`
- `admin/AGENTS.md`
```

Leave the current-focus line as `Backend API.`

- [ ] **Step 3: Check the map**

Run:

```bash
awk '
  /^\| `backend\/`/ { backend = NR }
  /^\| `android\/`/ { android = NR }
  /^\| `admin\/`/ { admin = NR }
  /^\| `infra\/`/ { infra = NR }
  /^\| `\.github\/workflows\/`/ { ci = NR }
  END {
    if (backend && android && admin && infra && ci && backend < android && android < admin && admin < infra && infra < ci)
      print "order-ok"
    else
      print "order-bad", backend, android, admin, infra, ci
  }
' AGENTS.md
grep -c 'Moderation web app; documentation only until its own design cycle' AGENTS.md
grep -c '^- `admin/AGENTS.md`$' AGENTS.md
grep -c '^Backend API\.$' AGENTS.md
grep -c 'service-role' AGENTS.md || true
test "$(wc -l < AGENTS.md)" -lt 60 && echo lines-ok
```

Expected: `order-ok`, `1`, `1`, `1`, `0`, `lines-ok`.

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md
git commit -m "$(cat <<'EOF'
docs: name admin on the root agent map

Let every session tell the future moderation app from the active API.

EOF
)"
```

---

### Task 4: README roadmap

**Files:**
- Modify: `README.md` (roadmap checklist only)

**Interfaces:**
- Consumes: nothing from the admin files. The new lines point at `DevSteps.md`.
- Produces: the roadmap tail in the spec. Task 5 does not edit this file.

- [ ] **Step 1: Confirm the combined moderation line is still present**

Run:

```bash
grep -c 'Verification and moderation flow' README.md
grep -c 'Operator moderation through the SQL runbook' README.md || true
grep -c 'Admin web app, in its own design cycle after the API' README.md || true
```

Expected: `1`, `0`, and `0`.

- [ ] **Step 2: Replace the roadmap tail**

Replace the checklist from the Android MVP line through the iOS line with:

```markdown
- [ ] Android MVP: map, venue pages, submissions, ratings
- [ ] Operator moderation through the SQL runbook, as part of the API roadmap ([DevSteps](DevSteps.md))
- [ ] Android beta (Google Play internal/closed testing)
- [ ] Admin web app, in its own design cycle after the API
- [ ] iOS app (Swift / SwiftUI)
```

Do not edit the product-approach paragraph, the repository table, or any earlier checklist line.

- [ ] **Step 3: Check the roadmap**

Run:

```bash
awk '
  /Operator moderation through the SQL runbook, as part of the API roadmap/ { runbook = NR }
  /Android beta \(Google Play internal\/closed testing\)/ { beta = NR }
  /Admin web app, in its own design cycle after the API/ { admin = NR }
  /iOS app \(Swift \/ SwiftUI\)/ { ios = NR }
  END {
    if (runbook && beta && admin && ios && runbook < beta && beta < admin && admin < ios)
      print "order-ok"
    else
      print "order-bad", runbook, beta, admin, ios
  }
' README.md
grep -c 'Verification and moderation flow' README.md || true
grep -F -c 'monorepo: `backend/` (Go), `android/`, later `ios/`' README.md
grep -F -c 'build moderation as a separate basic frontend after the' README.md
grep -F -c '| `admin/` | Moderation web app (future) |' README.md
```

Expected: `order-ok`, `0`, `1`, `1`, and `1`.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "$(cat <<'EOF'
docs: separate operator moderation from the admin app

The roadmap treated the SQL runbook and the later web app as one step.

EOF
)"
```

---

### Task 5: Session boundary

**Files:**
- Test: read-only checks. This task creates and modifies nothing.

**Interfaces:**
- Consumes: `admin/AGENTS.md`, `admin/README.md`, the root map, and the roadmap from Tasks 1–4.
- Produces: nothing.

- [ ] **Step 1: Confirm the session added only the two admin documents**

Run:

```bash
find admin -type f | sort
test ! -e admin/CLAUDE.md && echo no-claude-shim
test ! -e docs/operations/moderation.md && echo no-runbook
git diff --exit-code origin/dev -- \
  DevSteps.md \
  backend \
  docs/architecture \
  docs/adr \
  docs/superpowers/specs/2026-10-13-agent-entry-points-design.md \
  && echo untouched-ok
```

Expected:

```text
admin/AGENTS.md
admin/README.md
no-claude-shim
no-runbook
untouched-ok
```

- [ ] **Step 2: Do not commit**

This task changes no files. Do not create an empty commit.
