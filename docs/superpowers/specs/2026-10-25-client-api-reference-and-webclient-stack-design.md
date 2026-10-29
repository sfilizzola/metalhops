# Client API reference discoverability and the webclient stack

**Date:** 2026-10-25
**Status:** Partly implemented. `webclient/` now exists with a real guide and
README, and the framework was chosen: Vite, React, and TypeScript. The root
`AGENTS.md` and `README.md` rows are done. The `android/` and `admin/` API
reference pointer sections (scope 2) remain future work.

## Goal

Every agent that builds a client against the Go API — starting with the new
`webclient/` stack, and later `android/` and `admin/` — needs a reliable path
to the business-level API reference that already exists
(`docs/api/README.md`, `public.md`, `app.md`, `moderation.md`, and
`docs/api-handoff.md`), and needs to know, unambiguously, which routes its
stack is allowed to call. Today that reference exists but nothing in
`android/AGENTS.md` or `admin/AGENTS.md` points to it, and there is no
`webclient/` stack at all.

This is documentation and stub scaffolding only. It does not change
`backend/`, `docs/api/*.md`, or `docs/api-handoff.md` content, and it does
not choose a framework or write application code for any client stack.

## Background

- `docs/api/README.md` already splits every route into three caller groups
  (Public, App, Moderation) and states the cross-group rule: a Platform
  Moderator on an App route, or a User on a Moderation route, is
  `403 forbidden`.
- `docs/api-handoff.md` already gives step-by-step call sequences per
  feature (create a profile, submit a venue, review, check in, report,
  moderate, delete an account).
- `android/AGENTS.md` and `admin/AGENTS.md` are stubs today
  ("planned but not created yet" / "future stack... documentation only").
  Neither links to `docs/api/` yet.
- The product decision driving this change: a consumer-facing **web client**
  ships as the MVP, before Android and admin exist, and before iOS. It is a
  new, third client stack alongside `android/` and `admin/`.
- `DevSteps.md` is the closed backend API roadmap (Sessions 1–14, all
  "Done"). Its Session 14 success criteria says "The Android client starts
  only after the admin app exists." This document does not amend
  `DevSteps.md` — that file is a historical record of the completed backend
  roadmap, not the live product roadmap. The new client ordering
  (webclient → admin/android → iOS) lives in `README.md` instead, which is
  the forward-looking document.

## Scope

### 1. New `webclient/` stack (stub)

A documentation-only stack, following the exact pattern `android/` and
`admin/` already use: a short `README.md`, a fuller `AGENTS.md`, framework
undecided, "ask before scaffolding."

It is the consumer-facing MVP: discovery, Profile creation, Venue
Submissions, Reviews, Check-ins, and reports. It calls **Public and App**
routes only. It must never call a `/v1/moderation/...` route.

`webclient/README.md`:

```markdown
# MetalHops web client

Future consumer-facing web app: the MVP before Android and iOS exist. This
folder is documentation only until its own design cycle. The framework waits
for that cycle.

It discovers venues, lets a person sign up for a Profile, submit a venue,
write Reviews, and record Check-ins — the Public and App routes only. It
never calls a privileged `/v1/moderation/...` route; that is `admin/`'s job.

Agent rules for this folder: [AGENTS.md](AGENTS.md).
```

`webclient/AGENTS.md`:

```markdown
# Web client agent guide

The consumer-facing web app is a future stack: the MVP that ships before
Android and iOS. This folder is documentation only. Ask before scaffolding it
or choosing a framework.

It is the first client to call the product API. It calls Public and App
routes only — discovery, Profile, Venue Submissions, Reviews, Check-ins, and
reports. It never calls a `/v1/moderation/...` route; a User token on one of
those is `403 forbidden`, and that surface belongs to `admin/` instead.

## API reference

Before work starts, read:

- [`docs/api/README.md`](../docs/api/README.md) — business map of every
  route, grouped by caller (public, app, moderation)
- [`docs/api/public.md`](../docs/api/public.md) and
  [`docs/api/app.md`](../docs/api/app.md) — the two groups this stack calls
- [`docs/api-handoff.md`](../docs/api-handoff.md) — step-by-step call
  sequences per feature (create a profile, submit a venue, review, check in,
  report, delete an account)
- [`CONTEXT.md`](../CONTEXT.md) — glossary; capitalized terms mean exactly
  what it defines
- `backend/openapi.yaml` — the exact field contract, served at
  `/openapi.yaml` and browsable at `/docs`

When work starts, replace this stub with a real guide and add a `CLAUDE.md`
containing `@AGENTS.md`.
```

### 2. Pointers from the existing stubs

`android/AGENTS.md` gains an "API reference" section naming
`docs/api/public.md` and `app.md` (the two groups it calls) and stating it
never calls a moderation route.

`admin/AGENTS.md` gains the mirror: an "API reference" section naming
`docs/api/moderation.md` (the only group it calls) and stating it never
signs in as a Profile-bearing User.

Both additions are new sections; no existing wording in either file is
removed.

### 3. Repo-wide docs reflect the new stack

- Root `AGENTS.md`: add a `webclient/` row to the Stacks table
  (`Consumer-facing web app (framework TBD); MVP shipped before native
  apps` / Not started), placed after the `backend/` row.
- Root `README.md`:
  - **Repository Structure** table: add a `webclient/` row.
  - **Architecture** diagram: add a fourth box, `Web client (MVP, first)`,
    alongside Admin app / Android app / iOS app.
  - **Product Approach** numbered list: insert a new step for the web
    client MVP before the admin app step, renumbering the rest.
  - **Roadmap** checklist: add a line for the web client MVP before the
    admin/Android lines.

### Out of scope

- Choosing a framework for `webclient/`.
- Any change to `docs/api/*.md`, `docs/api-handoff.md`, `CONTEXT.md`,
  `backend/openapi.yaml`, or any backend code.
- Any change to `DevSteps.md`.
- Scaffolding `android/` or `admin/` beyond their existing stub state.

## Exit condition

- `webclient/README.md` and `webclient/AGENTS.md` exist and match the
  stub conventions of `android/` and `admin/`.
- `android/AGENTS.md` and `admin/AGENTS.md` each link to the `docs/api/`
  file(s) relevant to that stack and state the route group they must never
  call.
- Root `AGENTS.md` and root `README.md` list `webclient/` alongside
  `android/` and `admin/`, in an order consistent with "web client ships
  first."
- No change to `backend/`, `docs/api/*.md`, `docs/api-handoff.md`,
  `CONTEXT.md`, or `DevSteps.md`.
