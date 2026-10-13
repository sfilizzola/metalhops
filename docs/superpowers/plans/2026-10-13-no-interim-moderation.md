# No Interim Moderation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove every temporary moderation alternative from the living roadmap and make the admin app the next product after the API.

**Architecture:** `DevSteps.md` stays the API roadmap and ends ready for the admin app. `README.md` carries the same order. The admin guides and ADR 0001 stop describing a SQL bridge. No routes or application code are added.

**Tech Stack:** Markdown documentation.

**Spec:** `docs/superpowers/specs/2026-10-13-no-interim-moderation-design.md`

## Global Constraints

- Documentation only. Do not add application code, a migration, an OpenAPI route, or a web framework.
- Do not choose moderation URL paths or request bodies. Sessions 6, 10, and 12 expose the operator actions DevSteps already specifies. The implementing session names the routes.
- Do not add an operator-provisioning model. DevSteps already records an operator User on moderation audit rows and returns `403` when an authenticated caller cannot perform an operation. Each moderation session must name how that existing rule recognizes an operator before adding the endpoint.
- The admin app keeps its own design cycle. `admin/AGENTS.md` remains the only full copy of the admin boundary.
- Current focus stays "Backend API."
- Leave completed specs and plans under `docs/superpowers/` as historical records, including the session 1 boundary spec.
- Edited DevSteps text uses "temporary" only in "Moderation has no temporary path." and does not use "runbook".
- The owner-claims sentence must not contain the substring `temporary`. Rephrase "temporarily hide" to "hide Review commentary until a Platform Moderator reviews it".

## Review Focus

- A reader treats "Android can…" in Sessions 7–13 as permission to build Android during the API roadmap. Expected: those lines describe the API capability; the goal, Session 14, the success criteria, and the README keep Android after the admin app.
- A reader follows the session 1 spec's old roadmap order. Expected: that file stays historical, and the living README and DevSteps carry the new order.
- A grep for `temporary` matches "temporarily" in the owner-claims sentence. Expected: that sentence no longer contains the substring.
- A session plan invents moderation URLs from this change. Expected: DevSteps names actions and says the session plan names the routes.
- An operator-provisioning model gets added. Expected: DevSteps says this roadmap does not add one, and each endpoint session names the existing `403` rule.

---

### Task 1: DevSteps

**Files:**
- Modify: `DevSteps.md`
- Test: `rg` over `DevSteps.md`

**Interfaces:**
- Consumes: the approved spec's DevSteps section.
- Produces: an admin-ready API roadmap with privileged moderation actions and no runbook.

- [ ] **Step 1: Confirm the bridge is still present**

Run: `rg -n 'runbook|temporary' DevSteps.md`

Expected: matches in the deferred list, moderation paragraph, venue and review procedures, Sessions 6, 10, and 12, and the integration list.

- [ ] **Step 2: Replace the goal, boundary, procedures, sessions, and success criteria**

Use the replacement text in the spec section of this plan titled "DevSteps replacement".

- [ ] **Step 3: Check DevSteps**

Run: `rg -n 'runbook|temporary' DevSteps.md`

Expected: one match, the success-criteria sentence "Moderation has no temporary path."

- [ ] **Step 4: Commit**

```bash
git add DevSteps.md
git commit -m "docs: make DevSteps hand off to the admin app"
```

### Task 2: README

**Files:**
- Modify: `README.md`
- Test: `rg` over `README.md`

**Interfaces:**
- Consumes: the DevSteps handoff from Task 1.
- Produces: the public sequence API, admin app, then Android.

- [ ] **Step 1: Confirm the old order**

Run: `rg -n 'SQL runbook|Android MVP|after the API' README.md`

Expected: the roadmap still lists the SQL runbook and Android before the admin app.

- [ ] **Step 2: Replace the product approach, diagram, and roadmap tail**

Use the replacement text in "README replacement". Leave the Mobile table's "Android (first)" cell unchanged.

- [ ] **Step 3: Check the README**

Run: `rg -n 'runbook|temporary' README.md`

Expected: no matches. The roadmap order is Go API, admin app, OpenStreetMap, Android client, Android beta, iOS.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: put the admin app before any client"
```

### Task 3: Admin guides and ADR 0001

**Files:**
- Modify: `admin/AGENTS.md`
- Modify: `admin/README.md`
- Modify: `docs/adr/0001-product-data-through-go-api.md`
- Test: `rg` over those three files

**Interfaces:**
- Consumes: the "no SQL bridge" rule from Task 1.
- Produces: admin guidance and an ADR that name only privileged API workflows.

- [ ] **Step 1: Confirm the bridge is still named**

Run: `rg -n 'runbook|temporary' admin/AGENTS.md admin/README.md docs/adr/0001-product-data-through-go-api.md`

Expected: each file still describes the SQL bridge.

- [ ] **Step 2: Replace the bridge sentences**

Use "Admin and ADR replacement". Keep the framework unselected and the service-role key off the admin stack.

- [ ] **Step 3: Check the three files**

Run: `rg -n 'runbook|docs/operations/moderation.md|temporary' admin/AGENTS.md admin/README.md docs/adr/0001-product-data-through-go-api.md`

Expected: admin files say there is no operator SQL runbook and that `docs/operations/moderation.md` is not part of the plan. The ADR has no match.

- [ ] **Step 4: Commit**

```bash
git add admin/AGENTS.md admin/README.md docs/adr/0001-product-data-through-go-api.md
git commit -m "docs: retire the operator SQL bridge"
```

### Task 4: Spec status and living-document check

**Files:**
- Modify: `docs/superpowers/specs/2026-10-13-no-interim-moderation-design.md`
- Test: the spec's verification search

**Interfaces:**
- Consumes: Tasks 1–3.
- Produces: a spec marked implemented and a passing living-document search.

- [ ] **Step 1: Mark the spec implemented**

Change `**Status:** Approved design, not yet implemented` to `**Status:** Implemented`.

- [ ] **Step 2: Run the spec's search**

Search only `DevSteps.md`, `README.md`, `admin/AGENTS.md`, `admin/README.md`, and `docs/adr/0001-product-data-through-go-api.md` for `runbook`, `docs/operations/moderation.md`, and `temporary`.

Expected: DevSteps contains `temporary` only in "Moderation has no temporary path." Admin files contain `runbook` and `docs/operations/moderation.md` only in absence statements. The ADR and README contain none of the three.

- [ ] **Step 3: Confirm scope**

Expected: `git diff` does not modify `docs/superpowers/plans/2026-10-13-session-1-repository-boundaries.md`, `docs/superpowers/specs/2026-10-13-session-1-repository-boundaries-design.md`, `AGENTS.md`, `backend/AGENTS.md`, `docs/architecture/backend.md`, `CONTEXT.md`, or application code. `AGENTS.md` still says current focus is `Backend API.`

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-10-13-no-interim-moderation-design.md
git commit -m "docs: mark the interim-moderation spec implemented"
```

## DevSteps replacement

Goal:

```text
Deliver the complete Go API, including the user-facing contract and the
permanent privileged moderation endpoints, through a sequence of small,
independently reviewable sessions. Each session must leave the API
working, tested, and documented so a new session can continue without relying
on unstated context.

This roadmap ends as an admin-ready API. The admin app is the next project,
and the Android client starts only after that app exists. Certifying a
Google Play internal, closed, or public beta is a later cross-stack milestone.
```

Add this included bullet after authenticated Review reporting:

```text
- Privileged endpoints for Profile, Venue, and Review moderation decisions.
```

Owner-claims bullet:

```text
- Owner claims and owner-managed listings. In the future, a Verified Venue
  Owner may hide Review commentary until a Platform Moderator reviews it,
  but cannot hide or change its rating or make a final moderation decision.
```

Deferred moderation bullet:

```text
- The moderation UI, which belongs to the admin app.
```

Moderation paragraph:

```text
Moderation is a separate basic web application under `admin/`. This roadmap
does not build that app or choose its framework. The app calls privileged
endpoints on this Go API, not direct Supabase access or a separate backend.
Operator actions are those privileged endpoints. A caller who is not an
operator receives the existing `403`. Each session that adds one of these
endpoints names how that rule recognizes an operator, and this roadmap does
not add an operator-provisioning model.
```

Venue lifecycle sentence:

```text
discovery. Moderation transitions are performed through privileged moderation
endpoints.
```

Venue operator paragraph:

```text
Trusted operators perform the `pending` to `verified` or `rejected` transition
through a privileged moderation endpoint. The endpoint uses a transaction,
enforces the same rejection invariants, appends the moderation action, and
requires the operator and one submission to be identified explicitly. It also
lists pending Venue Reports, supports correcting a Verified Venue or moving it
to or from `delisted`, and atomically resolves each handled report. A
moderation action marks all pending Reports for that Venue as `actioned`;
reports without an action are dismissed individually.
```

Profile sentence:

```text
A privileged moderation endpoint lists pending Profile Reports and resolves
them atomically with any moderation action. Report submission does not alter
the target Profile, and the MVP exposes no report-history endpoint.
```

Review paragraph:

```text
Privileged moderation endpoints support two atomic actions: clear and lock
commentary while preserving the rating, or record the action and delete the
entire Review. Both actions require a reason and operator User ID and create
the User–Venue commentary restriction. The endpoints also list pending Review
Reports and atomically mark each handled report as `actioned` or `dismissed`.
A moderation action marks all pending Reports for that Review as `actioned`;
reports without an action are dismissed individually.
```

Integration bullet ending: `through the privileged moderation API.`

Session 6 title: `Profile safety and moderation API`

```text
Add Profile Reports, rename-required and suspended states, public identity
redaction, moderation audit, expiry behavior, and privileged endpoints that
list and resolve Profile Reports and apply rename-required and suspension
actions. The session plan names the routes and how the existing `403` rule
recognizes an operator. Do not add an operator-provisioning model.
```

Exit: `operators can require a rename or suspend community writes through those endpoints, and moderated identities are not exposed publicly.`

Session 10 title: `Venue safety and moderation API`

```text
Add rejection feedback, correction of rejected submissions, Venue Reports,
the Delisted Venue lifecycle, append-only moderation audit, and privileged
endpoints that verify, reject, correct, or delist Venues and resolve Venue
Reports. The session plan names the routes and how the existing `403` rule
recognizes an operator. Do not add an operator-provisioning model.
```

Exit: `operators can verify, reject, correct, or delist Venues through those endpoints; Users can report Verified Venue problems; catalogue history is retained.`

Session 12 title: `Review safety and moderation API`

```text
Add Review Reports, commentary restrictions, the append-only moderation audit
schema, and privileged endpoints for listing and resolving Review Reports,
clearing and locking commentary, or deleting the Review. The session plan
names the routes and how the existing `403` rule recognizes an operator. Do
not add an operator-provisioning model.
```

Exit: `Users can report unsafe commentary, and trusted operators can resolve reports through those endpoints without retaining removed text.`

Session 14 title: `API completion and admin handoff`

```text
Run cross-feature integration scenarios, verify migration-from-empty and RLS,
close OpenAPI example gaps, confirm error and pagination edge cases, validate
the migration-before-deploy release workflow and Render configuration,
document the user-facing and moderation request flows, and document the
account-deletion flow needed for later Google Play disclosure and support
pages.
Do not generate or commit client code, and do not start the admin app.
```

Exit: `the OpenAPI contract describes the runnable user and moderation routes, and deployment configuration is validated without certifying a beta release.`

Success criteria:

```text
After Session 14, the admin project can be designed and built against the
documented API, including moderation. The Android client starts only after
the admin app exists. Each roadmap session is independently understandable,
testable, and reviewable. Moderation has no temporary path.
```

## README replacement

Product steps 2–6:

```text
2. **Go API on Render:** custom business logic (verification, moderation, and rankings) lives in a Go service, so the admin app, Android, and iOS share one contract. Moderation rules are permanent privileged endpoints.
3. **Admin web app:** build it in its own design cycle, calling the privileged Go API endpoints.
4. **Native Android client:** build it after the admin app exists. GPS and maps are core features, so Android is native.
5. **Validate:** grow the venue database and gather feedback from the scene.
6. **Native iOS (Swift) later:** reuse the same API once Android proves the concept.
```

Diagram: the diagram in the spec. The paragraph under it names the admin app, Android, and iOS as the clients that authenticate with Supabase and send the JWT to the Go API.

Roadmap tail:

```text
- [ ] Go API on Render, including privileged moderation endpoints ([DevSteps](DevSteps.md))
- [ ] Admin web app, in its own design cycle
- [ ] Seed venue data from OpenStreetMap
- [ ] Android client: map, venue pages, submissions, ratings
- [ ] Android beta (Google Play internal/closed testing)
- [ ] iOS app (Swift / SwiftUI)
```

## Admin and ADR replacement

`admin/AGENTS.md` replaces the runbook paragraph with:

```text
The Go API provides those privileged moderation endpoints before this stack
starts. There is no operator SQL runbook, and `docs/operations/moderation.md`
is not part of the plan.
```

`admin/README.md` replaces the runbook sentence with:

```text
The Go API provides those endpoints before this stack starts. There is no
operator SQL runbook, and `docs/operations/moderation.md` is not part of the
plan.
```

ADR 0001 final sentence:

```text
Moderation is performed only through privileged Go API workflows, and there is
no operator SQL bridge.
```
