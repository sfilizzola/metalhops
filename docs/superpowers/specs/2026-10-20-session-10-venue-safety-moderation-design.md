# Session 10: Venue safety and moderation API

**Date:** 2026-10-20
**Status:** Pending review of this spec

## Goal

Operators can verify, reject, correct, or delist Venues. A Submitter can
correct a rejected Venue back to pending. Users can report a Venue in the
public catalogue. Catalogue history is retained and every status change is
audited.

The session adds:

- migration `0008`: the `delisted` status, rejection and delisting fields,
  `venue_reports`, and `venue_moderation_actions`;
- six routes (below);
- a correction path for rejected Venues in `PUT /v1/me/venues/{venueID}`,
  replacing the Session 9 refusal.

## Constraints

- Follow [DevSteps](../../../DevSteps.md) and the
  [backend architecture guide](../../architecture/backend.md).
- Keep `cmd/api` as composition only. All behavior goes in `internal/venue`
  and `internal/api`. No new package.
- Do not edit migrations `0001` through `0007`. Add no Go dependency.
- No operator-provisioning model. Operators are recognized by the existing
  `platform_operators` row through `requireOperator`.
- The venue public catalogue contracts from Sessions 7 and 8 are unchanged.
  Rejected and Delisted Venues are already hidden because the catalogue
  selects only `pending` and `verified`.
- Review Reports do not exist yet. The DevSteps clause that delisting actions
  pending Review Reports moves to Session 12.

## Data (migration `0008`)

- `venues.verification_status` check gains `delisted`.
- New nullable columns `rejection_reason`, `rejection_note`,
  `delisting_reason`, `delisting_note`. Checks: rejection fields are non-null
  only when `rejected`, delisting fields only when `delisted`; reasons use the
  DevSteps value sets; `other` requires a note; notes are at most 1,000
  characters.
- `venue_reports`: reporter (`on delete cascade`), venue, reason, optional
  note, status (`pending`, `actioned`, `dismissed`), creation time, resolution
  metadata. Partial unique index on (reporter, venue) where pending; partial
  index on (created_at, id) where pending for the operator list.
  `incorrect_details` and `other` require a note.
- `venue_moderation_actions`: append-only. Venue, prior and new status,
  rejection and delisting reason and note, operator user id (`on delete set
  null`, ADR 0003), timestamp. An operator correction records equal prior and
  new status and no reason. It never copies the Venue.
- Report quota reuses `report_quota_events` and its combined limit of 20 per
  rolling 24 hours (DevSteps). No new quota table.
- Every new table enables RLS and revokes `public`, `anon`, `authenticated`,
  `service_role` (ADR 0002).
- `requiredMigrationVersion` in `internal/database/pool.go` becomes `0008`;
  `TestMigrationVersions` is updated.

## Approach

`internal/venue` owns moderation, reports, and both write transactions, in
`moderate.go` and `report.go`, mirroring `internal/profile`. A new
`VenueModeration` interface in `internal/api` has two adapters: the PostgreSQL
service and an HTTP-test fake.

### Routes

| Route | Caller | Notes |
| --- | --- | --- |
| `POST /v1/venues/{venueID}/reports` | User | `Idempotency-Key` |
| `GET /v1/moderation/venue-reports` | Operator | pending, cursor-paged |
| `GET /v1/moderation/venues/{venueID}` | Operator | full Profiles of Submitter and reporters |
| `POST /v1/moderation/venues/{venueID}/actions` | Operator | `verify`, `reject`, `delist`, `reopen`; `Idempotency-Key` |
| `PUT /v1/moderation/venues/{venueID}` | Operator | correction; no key |
| `PATCH /v1/moderation/venue-reports/{reportID}` | Operator | dismiss; no key |

### Transitions

`pending` to `verified` or `rejected`; `verified` to `delisted`; `delisted`
back to `verified` (`reopen`). A request for the current state is a no-op: no
audit row, no rate-limit charge. Any other transition is `409`.

`Moderate` follows `profile.Moderate`: lock the Venue row, `idempotency.Lookup`,
no-op check, then `idempotency.Execute` containing the write reservation, the
status update, the audit insert, and marking every pending report for that Venue
`actioned`. `PATCH` dismisses one pending report, is idempotent on an already
dismissed report, and returns `409` for an actioned one.

### Submitter correction

`Correct` accepts `pending` and `rejected`. A rejected Venue returns to
`pending`, clears rejection feedback, re-runs the duplicate check excluding
itself, and reappears in the catalogue as not verified. A verified or delisted
Venue stays `409 venue_not_pending` with `This venue can no longer be
corrected.`

### Operator correction

`PUT /v1/moderation/venues/{venueID}` takes the Submitter-correction fields,
applies to `pending` or `verified`, never changes status, and appends an audit
row.

### Reports

Allowed for a Venue in the public catalogue (verified, or pending with a
genre). A self-report is `400`. A rejected, delisted, hidden, or unknown Venue
is `404 venue_not_found`. A second pending report is `409
venue_already_reported`. Quota exhaustion is `429` with `Retry-After`. Reports
never change Venue visibility.

## Documentation

`backend/openapi.yaml` documents the routes, schemas, and error codes, and
`contract_test.go` checks it. `backend/AGENTS.md` raises readiness to `0008`
and lists the routes. `DevSteps.md` marks Session 10 done and moves the header
to Sessions 1–10 done, Session 11 next.

## Testing

- Capability (`internal/venue`): every transition and refused transition,
  idempotent replay, no-ops, report resolution, rejected correction with
  duplicate re-check, report quota and self-report.
- HTTP (`internal/api`): each route and error mapping, using the fake.
- Persistence (`internal/database`): constraints, the append-only trigger, RLS
  closed. `venue/index_test.go` gains query-plan checks for the pending-report
  list.
- Catalogue: rejected and delisted Venues stay hidden.

## Assumptions to confirm

- A hidden-by-status Venue returns `404 venue_not_found` to a reporter. DevSteps
  says only that such Venues "cannot be reported".
- Operator correction audits with equal prior and new status and no reason.

## Out of scope

- Review Reports and the Review-related effects of delisting (Session 12).
- Check-in summaries for rejected and delisted Venues (Session 13).
- Operator provisioning, Venue merging, and the admin app.
