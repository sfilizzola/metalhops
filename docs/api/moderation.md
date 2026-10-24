# Moderation API

For a **Platform Moderator**: a trusted MetalHops operator (a
`platform_operators` row), not a User and not a Profile. These routes decide
Venue eligibility, Profile restrictions, and final Review moderation. A User
calling any of these gets `403 forbidden`.

## Shared pattern

Every action route (`applyProfileModeration`, `applyVenueAction`,
`applyReviewAction`) follows the same shape, so read this once:

- **`201` with a real `action_id`** — something actually changed; one audit
  row was appended.
- **`200` with `action_id: null`** — the target was already in that state
  with the same reason/note; nothing was written, nothing is charged against
  rate limits for the write itself.
- All three require `Idempotency-Key`. A retry with the same key replays the
  stored response.
- Acting on a report (or correcting a Venue) resolves every *pending* report
  on that target in the same transaction. Dismissing one report resolves
  only that one report and changes nothing else.
- A `note` is required whenever the chosen `reason` is `other`; otherwise
  it's optional. This combination is the **Restriction explanation**, and
  the affected User sees it on their own Profile or Review — reporter notes
  stay private to moderators.

## Quick reference

| Method & path | Business purpose | `operationId` |
| --- | --- | --- |
| `GET /v1/moderation/profile-reports` | Queue of pending profile reports | `listProfileReports` |
| `GET /v1/moderation/profiles/{userID}` | Full moderation view of one profile | `getModerationProfile` |
| `POST /v1/moderation/profiles/{userID}/actions` | Rename-require, suspend, or restore a profile | `applyProfileModeration` |
| `PATCH /v1/moderation/profile-reports/{reportID}` | Dismiss a profile report | `dismissProfileReport` |
| `GET /v1/moderation/venue-reports` | Queue of pending venue reports | `listVenueReports` |
| `GET /v1/moderation/venues/{venueID}` | Full moderation view of one venue | `getModerationVenue` |
| `PUT /v1/moderation/venues/{venueID}` | Correct a venue's details as an operator | `correctVenueAsOperator` |
| `POST /v1/moderation/venues/{venueID}/actions` | Verify, reject, delist, or reopen a venue | `applyVenueAction` |
| `PATCH /v1/moderation/venue-reports/{reportID}` | Dismiss a venue report | `dismissVenueReport` |
| `GET /v1/moderation/review-reports` | Queue of pending review reports | `listReviewReports` |
| `GET /v1/moderation/reviews/{reviewID}` | Full moderation view of one review | `getModerationReview` |
| `POST /v1/moderation/reviews/{reviewID}/actions` | Remove commentary, delete a review, or lift a restriction | `applyReviewAction` |
| `PATCH /v1/moderation/review-reports/{reportID}` | Dismiss a review report | `dismissReviewReport` |

---

## Profile moderation

### `GET /v1/moderation/profile-reports` — Pending profile reports

Oldest first. Each item shows the reporter and the target's full Profile —
moderators always see the real stored Username and display name, never the
redacted "MetalHops User" a public surface would show.

### `GET /v1/moderation/profiles/{userID}` — One profile, in full

The complete moderation picture for one User: current status, moderation
history, and pending reports against them.

### `POST /v1/moderation/profiles/{userID}/actions` — Apply a profile decision

Three actions:

- **`rename_required`** — objects to the Username or display name, with a
  reason (`abusive_name`, `hate_or_discrimination`, `impersonation`,
  `personal_information`, `spam`, `other`). The User may change their name
  while it stands, but only a moderator can end it (withdraw it, or replace
  it with a suspension).
- **`suspended`** — a **Community Suspension**: blocks contribution
  (submitting, reviewing, checking in, renaming) while leaving reads
  untouched. Takes `suspended_until`; a moderator can clear it early.
- **`restored`** — clears whichever restriction is standing.

A rename requirement and a suspension can't both stand — placing one ends
the other, recorded as its own "ended" entry carrying the *original*
decision's Restriction explanation, not a new one.

### `PATCH /v1/moderation/profile-reports/{reportID}` — Dismiss a report

Body is exactly `{"status":"dismissed"}`. Resolves only this one report;
the Profile is untouched. Dismissing an already-resolved report is a
harmless no-op.

---

## Venue moderation

### `GET /v1/moderation/venue-reports` — Pending venue reports

Oldest first, each with the reporter, a venue summary, and the Submitter —
either can be null if that User has since been deleted.

### `GET /v1/moderation/venues/{venueID}` — One venue, in full

Works for a venue in *any* status (pending, verified, rejected, delisted):
its submitted details, the Submitter's full profile, pending reports, and
moderation history (newest 20 entries) — everything a moderator needs to
decide, from one consistent snapshot.

### `PUT /v1/moderation/venues/{venueID}` — Correct a venue's details

Operator-side equivalent of a Submitter's correction: fixes the venue's
fields and Genres without changing its verification status. Appends an
audit entry only when something actually changed, and resolves that venue's
pending reports in the same step. A pending Venue Submission has no
separate correction queue — this route and `getModerationVenue` are how a
moderator reviews it before deciding.

### `POST /v1/moderation/venues/{venueID}/actions` — Decide a venue's eligibility

Four actions, each valid from a specific starting status:

| Action | From | To | Reason required? |
| --- | --- | --- | --- |
| `verify` | pending | verified | no |
| `reject` | pending | rejected | yes — `duplicate`, `not_eligible`, `inaccurate_details`, `insufficient_information`, `cannot_verify`, `other` |
| `delist` | verified | delisted | yes — `permanently_closed`, `duplicate`, `no_longer_eligible`, `other` |
| `reopen` | delisted | verified | no |

**Key rules:**
- `verify` and `reopen` fail with `409 venue_incomplete` if the venue has no
  Genre assigned yet.
- **Reopening** restores the *same* Venue, including its past Check-ins and
  Reviews — it is not a new listing. (Note: `reopen` does not re-run
  duplicate detection against venues submitted while this one was delisted —
  a known gap.)
- Rejecting an already-rejected venue, or delisting an already-delisted one,
  with a *different* reason or note still writes a new audit entry (the
  **Rejection Feedback** changed, even though the status didn't).
- Every state-changing action resolves that venue's pending Venue Reports.

### `PATCH /v1/moderation/venue-reports/{reportID}` — Dismiss a report

Same pattern as profile reports: exact body, resolves only this report.

---

## Review moderation

### `GET /v1/moderation/review-reports` — Pending review reports

Oldest first. A rating-only Review never appears here — there's no
commentary to assess.

### `GET /v1/moderation/reviews/{reviewID}` — One review, in full

The review itself, its pending reports, moderation history (newest 20), and
any standing **Commentary restriction** on that author for that venue. Works
even if the venue is now pending, rejected, or delisted.

### `POST /v1/moderation/reviews/{reviewID}/actions` — Decide on a review

Three actions:

- **`remove_commentary`** — hides the written text; the rating stays public
  and the removal stays visibly marked. Starts a **Commentary restriction**
  on that User for that Venue. Needs a `reason` (same safety-reason enum as
  reports) and an optional/required `note`.
- **`delete_review`** — ends the Review entirely (rating included). Also
  starts a Commentary restriction. Same reason rules as above.
- **`lift_commentary_restriction`** — see below. Takes *only* `{"action": …}`
  — a `reason` or `note` here is `400 validation_error`.

Acting resolves every pending Review Report on that review.

#### Lift a commentary restriction

The restriction isn't tied to the review that triggered it forever — it
follows the *User + Venue pair*. To lift it, the moderator calls this action
against **a Review that User currently has for that Venue** (which may be a
newer, rating-only Review written after the original one was deleted).

- The lifted restriction's record carries the **Restriction explanation**
  (reason + note) of the decision that *started* it — the moderator supplies
  nothing new.
- If no restriction is standing for that User/Venue pair, the lift is `200`
  with `action_id: null` and writes nothing — safe to retry.
- After lifting, the Review stays rating-only and the "commentary removed"
  marker stays visible until the User writes commentary again. Nothing is
  un-deleted.
- If the review that started the restriction has since ended and the User
  hasn't reviewed that venue again, there's no review to lift through yet —
  the lift waits for their next Review.

### `PATCH /v1/moderation/review-reports/{reportID}` — Dismiss a report

Same pattern as the other two report types.
