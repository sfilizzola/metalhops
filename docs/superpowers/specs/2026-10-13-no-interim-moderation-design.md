# No interim moderation bridge

**Date:** 2026-10-13
**Status:** Approved design, not yet implemented

## Goal

Make the living roadmap say that the Go API includes permanent privileged
moderation endpoints, the admin app is the next product, and the Android
client starts only after that app exists. Remove every temporary moderation
alternative from the API roadmap.

## Constraints

- Documentation only. Do not add application code, a migration, an OpenAPI
  route, or a web framework.
- Do not choose moderation URL paths or request bodies. Sessions 6, 10, and 12
  expose the operator actions DevSteps already specifies. The implementing
  session names the routes.
- Do not add an operator-provisioning model. DevSteps already records an
  operator User on moderation audit rows and returns `403` when an
  authenticated caller cannot perform an operation. Each moderation session
  must name how that existing rule recognizes an operator before adding the
  endpoint.
- The admin app keeps its own design cycle. `admin/AGENTS.md` remains the only
  full copy of the admin boundary.
- Current focus stays "Backend API."
- Leave completed specs and plans under `docs/superpowers/` as historical
  records, including the session 1 boundary spec whose roadmap order this
  decision supersedes.

## Approach

Correct the current roadmap documents so they agree. `DevSteps.md` remains the
API roadmap and ends ready for the admin app. `README.md` carries the same
order for people. The two admin guides and ADR 0001 stop describing a SQL
bridge.

Rejected alternatives:

- **A one-line policy.** Adding "no temporary alternatives" while leaving the
  runbook sessions and Android-before-admin order in place lets a later
  session follow the contradictory instructions.
- **Plan the admin app inside DevSteps.** That chooses admin scope before its
  design cycle.

## Delivery sequence

1. Finish the Go API, including permanent privileged moderation endpoints.
2. Build the admin app against those endpoints.
3. Build the Android client only after the admin app exists.

No public or beta client ships before the admin app. The API roadmap creates
no operator SQL runbook, no temporary moderation endpoint, and no
`docs/operations/moderation.md`.

## DevSteps

Replace both goal statements that make Android the consumer of this roadmap.
The roadmap delivers the complete Go API, including the user-facing contract
and the permanent moderation endpoints, through the existing small sessions.
It ends as an admin-ready API. The admin app is the next project, and the
Android client starts only after that app exists. Google Play certification
stays a later cross-stack milestone.

In the deferred list, remove moderation decision endpoints from the deferred
work. The moderation UI stays deferred with the admin app. Venue, Review, and
Profile report intake stay in this roadmap, and their decisions become
privileged endpoints.

Replace the moderation-boundary paragraph. State that `admin/` is a separate
web application, this roadmap does not build it or choose its framework, and
the app calls privileged endpoints on this Go API. Delete the SQL-runbook
bridge and the promise to add `docs/operations/moderation.md`.

In the Profile, Venue, and Review behavior sections, keep the current
outcomes, invariants, and audit records. Replace each runbook procedure with
the same procedure on a privileged endpoint:

- Profile reports can be listed and resolved, and an operator can require a
  rename or suspend community writes.
- Venue submissions can be verified, rejected, or corrected, and a Verified
  Venue can move to and from `delisted`. Report resolution stays in the same
  transaction as the moderation action.
- Review reports can be listed and resolved. The operator can clear and lock
  commentary or delete the Review.

A non-operator receives the existing `403`.

Rename the three bridge sessions and Session 14:

- Session 6, "Profile safety and moderation API," adds the profile endpoints
  above instead of the temporary operator SQL runbook.
- Session 10, "Venue safety and moderation API," adds the venue endpoints
  above. Delete "Do not add a moderation decision endpoint."
- Session 12, "Review safety and moderation API," adds the review endpoints
  above instead of the operator SQL runbook.
- Session 14, "API completion and admin handoff," keeps the cross-feature,
  migration, OpenAPI, and deployment checks. It documents the user-facing and
  moderation flows, and the account-deletion flow needed for later Google Play
  disclosure. It generates no client code and does not start the admin app.
  Its exit condition is an OpenAPI contract covering the runnable user and
  moderation routes.

Change the Review-report integration coverage from resolution through the
operator runbook to resolution through the privileged moderation API.

Replace the success criteria. After Session 14, the admin project can be
designed and built against the documented API. The Android client starts only
after the admin app exists. Moderation has no temporary path. The edited
DevSteps text uses "temporary" only in that absence statement and does not use
"runbook" at all.

## Living documents

### `README.md`

Keep the Supabase foundation as step 1. Replace the remaining
product-approach steps with:

2. Go API on Render. Custom business logic, including verification,
   moderation, and rankings, lives in the Go service, so the admin app,
   Android, and iOS use one contract. The moderation rules are permanent
   privileged endpoints.
3. Admin web app, in its own design cycle, calling the privileged Go API
   endpoints.
4. Native Android client after the admin app exists. GPS and maps are core
   features, so Android is native.
5. Validate by growing the venue database and gathering feedback.
6. Native iOS later, reusing the same API once Android proves the concept.

Replace the architecture diagram with:

```text
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│  Admin app   │   │ Android app  │   │   iOS app    │
│              │   │ (after admin)│   │   (later)    │
└──────┬───────┘   └──────┬───────┘   └──────┬───────┘
       └──────────────────┼──────────────────┘
                          ▼
               Go REST API (Render)
                          ▼
        Supabase: PostgreSQL + PostGIS
              Auth · Storage
```

Update the paragraph under the diagram so the admin app is one of the clients
that authenticate with Supabase and send the JWT to the Go API.

From the Go API checklist item onward, the roadmap becomes:

- Go API on Render, including privileged moderation endpoints, linking to
  DevSteps.
- Admin web app, in its own design cycle.
- Seed venue data from OpenStreetMap.
- Android client: map, venue pages, submissions, and ratings.
- Android beta through Google Play internal or closed testing.
- iOS app.

The OpenStreetMap item stays a separate post-API catalogue import. It moves
below the admin item so the admin app is the next product step after the API.
The Mobile table keeps "Android (first)": that cell orders Android before iOS,
while the product approach and roadmap order the admin app before either
mobile client.

### Admin guides

In `admin/AGENTS.md` and `admin/README.md`, delete the instruction to moderate
through the SQL runbook. State that the Go API provides the privileged
moderation endpoints before this stack starts, and that
`docs/operations/moderation.md` is not part of the plan. Keep the framework
unselected, Supabase product tables and the service-role key off this stack,
and the behavior link to DevSteps.

### ADR 0001

Keep the decision that product data goes through the Go API. Replace the final
sentence with: moderation is performed only through privileged Go API
workflows, and there is no operator SQL bridge.

## Unchanged

- Completed files under `docs/superpowers/`.
- `AGENTS.md`, `backend/AGENTS.md`, `docs/architecture/backend.md`, and
  `CONTEXT.md`.
- The repository's current focus, "Backend API."

## Verification

After the edits, search only `DevSteps.md`, `README.md`, `admin/AGENTS.md`,
`admin/README.md`, and `docs/adr/0001-product-data-through-go-api.md` for
`runbook`, `docs/operations/moderation.md`, and `temporary`. The only allowed
hits are statements that those alternatives are absent. Confirm the README
order is Go API, admin app, then Android. Confirm `git diff` does not touch
`docs/superpowers/plans/`,
`docs/superpowers/specs/2026-10-13-session-1-repository-boundaries-design.md`,
or application code.

Work happens on `feature/no-interim-moderation` and targets `dev` by pull
request.
