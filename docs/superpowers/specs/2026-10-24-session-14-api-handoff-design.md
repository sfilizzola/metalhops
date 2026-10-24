# Session 14: API completion and admin handoff

**Date:** 2026-10-24
**Status:** Approved in brainstorming; pending spec review

## Goal

Finish the API roadmap so the admin project can be designed against the
documented user and moderation routes. This session adds the Platform
Moderator operation that lifts a Commentary restriction, the release job that
applies migrations before it deploys, and the handoff document. It does not
start the admin app, generate a client, or promote `dev` to `main`.

The session adds:

- migration `0012`, which allows the audit action
  `commentary_restriction_lifted`;
- `lift_commentary_restriction` on the existing Review moderation route;
- the commentary save that clears `commentary_removed_at` after that lift;
- `.github/workflows/release.yml`, which is the only deployer;
- `docs/api-handoff.md`.

**Exit condition:** formatting, vet, unit tests, integration tests, migration
tests, and build pass; OpenAPI describes the runnable user and moderation
routes, including the lift; the release workflow is on `dev` and reviewed.
`DevSteps.md` marks Session 14 done only after the operator applies `0012`
and a local `/readyz` from the Session 14 process returns 200. The first
production run waits until `dev` is promoted to `main`.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Work on `feature/session-14-api-handoff`. Merge into `dev` by pull request.
- Do not edit migrations `0001` through `0011`. Add no Go dependency.
- `internal/review` owns the lift and the save that follows it. The
  transaction stays in that module. There is no new route, package, or Go
  interface. Profile and Venue stay as they are.
- An operator is still a `platform_operators` row, checked by
  `requireOperator`.
- The API process does not apply migrations.
- `healthCheckPath` stays `/healthz`. Render auto-deploy is turned off.
- The implementer does not run `supabase login`, `supabase link`,
  `supabase db push`, or `git push` of `main`. The implementer does not
  create the GitHub environment or the Render deploy hook.
- Account-deletion behavior stays as the foreign keys and ADR 0003 already
  implement it. This session corrects the `DevSteps.md` wording to that
  behavior.
- Left for later: the admin app, generated clients, operator provisioning,
  Venue merging, hidden commentary, report history, and beta certification.

## Commentary restriction lift

`POST /v1/moderation/reviews/{reviewID}/actions` remains behind
`requireOperator`. It still requires `Idempotency-Key`. The operation name
stays `review_moderation_action`. A missing or invalid token is `401
unauthorized`. A non-operator is `403 forbidden`.

The lift body is:

```json
{"action":"lift_commentary_restriction"}
```

`reason` and `note` are omitted, JSON null, or an empty reason with a null
note. A non-empty `reason`, or a `note` that is present and not JSON null, is
`400 validation_error` with that field set to `must be omitted`. Whitespace
is a present value. The action allow-list becomes `remove_commentary`,
`delete_review`, or `lift_commentary_restriction`. Any other action is `400`
with `action: must be remove_commentary, delete_review, or lift_commentary_restriction`.
`remove_commentary` and `delete_review` keep their current reason and note
rules. A missing idempotency key stays `idempotency_key: required`. Unknown
JSON fields stay rejected. The body limit stays 64 KiB.

Validation runs before the transaction. A validation failure spends no write
token and stores nothing. The idempotency command for an accepted lift is the
Review id, the action, an empty reason, and a null note.

The transaction then looks up the idempotency record. A stored `201` returns
that body and spends no write token, including after a later delete of the
Review. The stored body still omits commentary.

When the lookup misses, the transaction locks the Review `for update`. A
missing Review is `404 review_not_found` (`Review not found.`). Venue status
is not consulted. A pending, verified, rejected, or Delisted Venue can be
lifted. It then locks the User–Venue restriction row `for update` and looks
up the idempotency record again, so a concurrent same-key request that waited
on the Review lock replays the stored `201` instead of taking the no-op.

When that second lookup misses and the pair has no restriction row, the
result is `200`, `action_id` null, and the loaded operator Review, including
its commentary. There is no audit row and no write token. The result is not
stored, so the same key no-ops again. A concurrent lift waits on the Review
lock, then takes this path.

When the restriction row is present, `idempotency.Execute` runs in the same
transaction. The callback copies `reason` and `note` from the restriction's
source action, inserts one `review_moderation_actions` row, then deletes that
one restriction row. The audit row is:

- `review_id`, `reviewer_user_id`, and `venue_id` from the locked Review;
- `action` `commentary_restriction_lifted`;
- `reason` and `note` copied from the source action;
- `operator_user_id` the caller;
- `created_at` the request clock.

The Review row is unchanged, including `commentary_removed_at`, `updated_at`,
the rating, the commentary, and pending reports. Success is `201` with
`action_id`, `review_id`, and the operator Review document. The stored
idempotency body omits commentary, as the other actions do. The write spends
one User write token, committed only after the transaction commits. An empty
bucket is `429 rate_limited` with `Retry-After`, and the transaction rolls
back, so the restriction and the audit stay as they were. A reused key with a
different normalized command stays `409 idempotency_key_reused`.

A lift through a later rating-only Review uses that Review's id on the new
audit row. The source action may belong to an earlier deleted Review. The
copied reason is already one of the seven allowed reasons, so the existing
reason and note checks accept the insert.

## Save after a lift

`PUT /v1/venues/{venueID}/reviews/me` keeps its venue-then-contributor order
and still takes no idempotency key. The restriction check is unchanged: a
non-null commentary while the restriction row exists is `409
commentary_locked`.

When the normalized commentary is non-null and the stored
`commentary_removed_at` is non-null, the same `update` that writes the rating
and commentary sets `commentary_removed_at` to null and `updated_at` to the
save clock. `created_at` stays. The removal audit and the lift audit stay.
The check `commentary_removed_at is null or commentary is null` requires the
timestamp clear to happen in that same statement.

A rating-only save leaves `commentary_removed_at` set. An unchanged rating
with null commentary stays the current no-op and leaves the marker. Actioned
reports stay actioned. A new Review insert after a lift does not set the
timestamp.

## Migration `0012`

`infra/supabase/migrations/0012_commentary_restriction_lift.sql` replaces
`review_moderation_actions_action_check`. The new check allows
`commentary_removed`, `review_deleted`, and `commentary_restriction_lifted`.
It drops and re-adds that constraint. It adds no table, column, index,
policy, or revoke. `requiredMigrationVersion` becomes `0012`.

A persistence test inserts `commentary_restriction_lifted` with a copied
reason and rejects any other new action value. Readiness treats a database
whose highest version is `0011` as `migrations_behind` and `0012` as ready.
The existing harness applies every migration from an empty database, so
`0012` joins that run. The current Data API revocation test remains the RLS
proof.

## Release job

`.github/workflows/release.yml` runs on every push to `main`, with no path
filter. `permissions` is `contents: read`. The concurrency group is
`release` and `cancel-in-progress` is false, so a later push waits.

The job uses the GitHub environment `production`. That environment allows
`main` only and has no required reviewer. It holds three values, named in
`infra/README.md` and absent from the repo:

- `SUPABASE_DB_URL`, the session-pooler URL on port 5432;
- `RENDER_DEPLOY_HOOK_URL`;
- `API_BASE_URL`, the public `https` origin with no path and no trailing
  slash.

The existing `.github/workflows/backend.yml` stays as it is, including its
push to `main`. This job also runs `go vet ./...` and `go test ./...` from
`backend/` before it touches the database. A failing test stops the job
before `db push`.

The job parses `SUPABASE_DB_URL` and fails with `SUPABASE_DB_URL must use
port 5432` when the port is anything else, including 6543. It does not print
the URL or the hook. `API_BASE_URL` must start with `https://` and have no
path. Any other value fails the job before the hook is called.

The migration step installs the CLI with `supabase/setup-cli@v3` and
`version: 2.84.2`. It runs:

```text
supabase db push --db-url "$SUPABASE_DB_URL" --workdir infra --yes
```

A failed push stops the job. The deploy hook is not called.

The deploy step POSTs the hook with the query parameter `ref` set to
`github.sha`. A hook URL that already has a query string keeps those
parameters. A `200` or `202` continues. Any other status fails the job.

Render's health check stays `/healthz`. The job does not read `/healthz`.

The Dockerfile's final stage declares `ARG RENDER_GIT_COMMIT` and sets
`ENV RENDER_GIT_COMMIT=$RENDER_GIT_COMMIT`, so the image keeps the commit
Render passes at build. When that value matches `^[0-9a-f]{40}$`, a ready
response is `{"status":"ok","commit":"<sha>"}\n`. When it is unset or any
other string, the body stays `{"status":"ok"}\n`. A `503` body never includes
`commit`.

The job polls `$API_BASE_URL/readyz` every 15 seconds for 15 minutes,
measured from the hook response. Connection errors, `503` responses, and a
`200` whose `commit` is absent or different keep polling. Success is HTTP
`200` whose JSON `status` is `ok` and whose `commit` equals `github.sha`.
A timeout fails the job.

`infra/render.yaml` sets `autoDeployTrigger: off`.

`infra/README.md` tells the operator, for the later promotion, to create the
`production` environment with those three values, create the service deploy
hook, and sync the blueprint so auto-deploy is off. This session does not do
those steps and does not push `main`.

## Operator handoff for `0012`

Before `DevSteps.md` marks Session 14 done, the operator starts the Session
14 binary and confirms local `GET /readyz` returns `migrations_behind`, then
runs `supabase db push --workdir infra`. That same process must then return
200. A 200 from a binary whose required version is older than `0012` does
not finish the gate. The direct-host and session-pooler fallback from the
Session 3 handoff still applies to this manual push. The release job is not
that push.

## Handoff document

`docs/api-handoff.md` is the sequence guide for the admin design cycle and
for a later Google Play disclosure. OpenAPI remains the field contract. Each
step names the method, path, caller, and success status, and points at the
OpenAPI `operationId` for fields. A body appears only where one value decides
the branch, such as `DELETE MY ACCOUNT` or `lift_commentary_restriction`.

The document has four parts.

**Calling the API.** Bearer JWT, `Idempotency-Key` on the writes that require
one, the error envelope, and cursor pagination: `limit` 1–100 with default
20, opaque `next_cursor`, and `400` for a bad cursor. An app User on a
moderation route, or an operator on an app route, receives `403 forbidden`.

**User sequences.** Create a Profile, discover Venues, submit and correct a
Venue, save and delete a Review, file a Profile, Venue, or Review report,
and record, list, and delete a Check-in.

**Moderation sequences.** Profile, Venue, and Review queues: list, read, act,
and dismiss. The Review sequence includes `remove_commentary`, the lock the
User sees, `lift_commentary_restriction`, and the later commentary save that
clears `commentary_removed_at` while both audit rows stay.

**Account deletion.** `DELETE /v1/me` is available to every authenticated
caller except an operator, including a sign-in with no Profile and a
suspended User. It needs a live session created within the previous 10
minutes and the confirmation `DELETE MY ACCOUNT`. It takes no
`Idempotency-Key`. `204` is returned only after Supabase deletes the auth
user. A provider failure is `502 auth_provider_error`, and the local rows
stay.

Removed by that auth-user delete: the Profile; Reviews; Check-ins;
idempotency rows; commentary-restriction rows; Profile reports, because both
the reporter and the target cascade; Review reports, because the reporter
cascades and the author's Review cascades; Venue reports whose reporter is
the deleted user.

Retained: Venue submissions, with `submitted_by` set null; Venue reports
filed by someone else; append-only Profile, Venue, and Review moderation
audit, with deleted user ids set null. That audit is the Profile restriction
history in [ADR 0003](../../adr/0003-keep-restriction-records-after-user-deletion.md).
A commentary-restriction row is not that record, and it cascades with the
user.

`DevSteps.md` replaces the sentence that says deletion cascades restrictions
as a single group with this split. The endpoint behavior does not change.

## OpenAPI

`getReady` adds optional `commit`, a 40-character lowercase hex string, to
the `200` schema. `status` stays the only required property.
`additionalProperties` stays false. The existing example without `commit`
stays, and a named example shows `commit`. The `503` schema is unchanged.

`applyReviewAction` documents `lift_commentary_restriction` and a request
example with only `action`. The `201` and `200` responses stay the current
action envelope.

A contract test fails when any `200` or `201` response with an
`application/json` body has neither an `example` nor an `examples` entry.
`204` responses have no example. The responses that need a success example
today are `listMyVenues` and `listMyCheckIns`. The test names any other
response that fails the same rule, and the session adds that example.

## Testing

`internal/review` covers a real lift, the copied reason and note, the
unchanged Review row, the `200` no-op and a repeat of that key, replay of the
stored `201`, a reused key, a present reason or note, a missing Review, a
lift through a later rating-only Review, a Delisted Venue, pending reports
left unchanged, and one write token only on the `201`. A commentary save
after the lift clears `commentary_removed_at` and leaves both audit rows. A
rating-only save leaves the marker.

One Postgres test walks a verified Venue through a commentary Review,
`remove_commentary`, the lift, and the commentary save that clears the
marker. The removal audit and the lift audit both remain, and the restriction
row is gone.

`internal/api` covers the new action on the existing route, `403` for a
non-operator, and the `201`, `200`, `400`, and `404` mapping.

`internal/api` covers `/readyz`: a 40-hex `RENDER_GIT_COMMIT` adds `commit`,
and an unset value leaves `{"status":"ok"}\n`. A `503` body is unchanged.

Pagination and the existing error envelopes stay on their current tests.
This session adds no new list.

The release workflow is reviewed on the pull request. This session does not
run it.

## Docs that land with the code

- `backend/openapi.yaml`, as above.
- `docs/api-handoff.md`, as above.
- `backend/AGENTS.md` raises readiness to `0012`, names the lift on the
  existing Review action route, and names `release.yml` as the only deployer.
- `docs/architecture/backend.md` states that the Review module owns the lift
  and the marker clear, and that a push to `main` applies migrations, deploys
  through the Render hook, and succeeds only when `/readyz` returns the
  matching commit.
- `infra/README.md` names the three `production` values, `autoDeployTrigger:
  off`, and the operator steps for the later promotion.
- `DevSteps.md` records the account-deletion split. It marks Session 14 done
  only after the operator handoff above: the status line becomes Sessions
  1–14 done, and the Session 14 section names the lift, migration `0012`,
  readiness `0012`, `docs/api-handoff.md`, and `release.yml`.

## Out of scope

- Running the release job, pushing `main`, creating the GitHub environment,
  creating the deploy hook, or syncing the Render blueprint.
- A Render API key, a manual workflow dispatch, and a required reviewer on
  `production`.
- A new moderation route, a pair route addressed by User and Venue, and a
  lift reason supplied by the operator.
- Changing what `DELETE /v1/me` deletes or retains.
- New pagination behavior, a report-history endpoint, hidden commentary,
  operator provisioning, Venue merging, generated clients, and the admin app.
- Certifying a beta release. Render's `/healthz` success is not the release
  proof.
