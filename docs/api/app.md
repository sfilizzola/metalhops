# App API

For a signed-in caller: someone who completed Supabase sign-in and sends
`Authorization: Bearer <JWT>`. Two sub-states matter:

- **app caller** — signed in, but has not finished creating a **Profile**
  yet (no Username and display name saved). Can read most things, cannot
  contribute.
- **User** — an app caller with a Profile. Can submit venues, review, check
  in, and report. A **rename requirement** or **Community Suspension** on
  that Profile blocks writes (`403 profile_rename_required` /
  `403 profile_suspended`) but never blocks reads.

A Platform Moderator calling any of these routes gets `403 forbidden` — see
[`moderation.md`](moderation.md) for their own identity and routes.

## Quick reference

| Method & path | Business purpose | `operationId` |
| --- | --- | --- |
| `GET /v1/me` | Read my own profile | `getMe` |
| `PATCH /v1/me` | Create or update my profile | `patchMe` |
| `DELETE /v1/me` | Permanently delete my account | `deleteMe` |
| `POST /v1/venues` | Submit a new venue | `createVenueSubmission` |
| `GET /v1/me/venues` | List venues I've submitted | `listMyVenues` |
| `GET /v1/me/venues/{venueID}` | Read one of my submissions | `getMyVenue` |
| `PUT /v1/me/venues/{venueID}` | Correct a pending/rejected submission | `correctMyVenue` |
| `GET /v1/venues/{venueID}/reviews/me` | Read my review of a venue | `getMyReview` |
| `PUT /v1/venues/{venueID}/reviews/me` | Write or replace my review | `putMyReview` |
| `DELETE /v1/venues/{venueID}/reviews/me` | Delete my review | `deleteMyReview` |
| `POST /v1/venues/{venueID}/check-ins` | Check in at a venue | `createCheckIn` |
| `GET /v1/venues/{venueID}/check-ins` | See a venue's recent check-ins | `listVenueCheckIns` |
| `GET /v1/me/check-ins` | List my check-in history | `listMyCheckIns` |
| `DELETE /v1/me/check-ins/{checkInID}` | Remove one of my check-ins | `deleteMyCheckIn` |
| `POST /v1/profiles/{username}/reports` | Report a profile | `createProfileReport` |
| `POST /v1/venues/{venueID}/reports` | Report a venue's listing info | `reportVenue` |
| `POST /v1/venues/{venueID}/reviews/{reviewID}/reports` | Report a review's commentary | `reportReview` |

---

## Profile

### `GET /v1/me` — Read my profile

Returns the caller's stored Username, display name, and moderation status.
`404 profile_not_found` means this sign-in hasn't created a Profile yet —
that's the normal first-run state, not an error to alarm the user over.

### `PATCH /v1/me` — Create or update my profile

The first successful call *creates* the Profile (this is how a User comes
into existence); later calls update it. Sending the same Username and
display name again is a no-op: `200` with `updated_at` unchanged.

**Key rules:**
- A Username must be unique; a collision is `409 username_taken`.
- A **Protected Username** (matches a reserved or MetalHops-branded name,
  ignoring underscores and lookalike digits) is `400 username_reserved`.
- While a Community Suspension stands, the Username and display name cannot
  change: `403 profile_suspended`.
- A display name has no uniqueness rule — it's just what's shown.

### `DELETE /v1/me` — Delete my account

**Business purpose:** permanent, irreversible account deletion. Body must be
exactly `{"confirmation":"DELETE MY ACCOUNT"}`.

**Key rules:**
- Any authenticated caller except a Platform Moderator can call it — even an
  app caller with no Profile yet, or a suspended User.
- Requires a session created within the last 10 minutes
  (`403 recent_authentication_required` otherwise) — re-authenticate and
  retry.
- No `Idempotency-Key`.
- Removed with the account: the Profile, Reviews, Check-ins, and the User's
  own filed reports (and reports against their Profile/Reviews, since the
  target cascades). A standing Commentary restriction ends.
- Kept after deletion: Venue Submissions (ownerless — `submitted_by`
  becomes null), Venue Reports filed by someone else, and every moderation
  decision as an anonymized **Restriction record** (the decision stays, the
  identity doesn't).

---

## Venue submissions

The self-submit model: any User can propose a new venue, and it's visible
(as "not yet verified") in the Public catalogue immediately — see
[`public.md`](public.md) for how the catalogue renders it, and
[`moderation.md`](moderation.md) for how it gets verified or rejected.

### `POST /v1/venues` — Submit a venue

**Key rules:**
- Requires an `Idempotency-Key`. A replay of the same key returns the
  original `201` body and uses no quota.
- Limit: 10 submissions per User in a rolling 24 hours
  (`429 quota_exceeded`). Corrections don't count against this.
- Duplicate detection: compares the **Normalized name** (case/accent-folded,
  punctuation stripped) within 100 metres of an existing pending or verified
  venue. A hit is `409 duplicate_venue`; the response includes the existing
  venue's id only when it's safe to disclose (verified, or the caller's own
  submission).
- Needs a Profile first — no Profile is `409 profile_required`.

### `GET /v1/me/venues` — List my submissions

Every status (pending, verified, rejected, delisted), newest-updated first.
Readable even while the caller must rename or is suspended.

### `GET /v1/me/venues/{venueID}` — Read one of my submissions

Same venue a Submitter sees for their own pending/rejected listing,
including rejection feedback not shown publicly.

### `PUT /v1/me/venues/{venueID}` — Correct my submission

Only the **Submitter** can correct it, and only while it's pending or
rejected. Correcting a rejected one — even with nothing actually changed —
sends it back to pending review. An unchanged body on an already-pending
submission is a no-op (`200`, `updated_at` untouched). Doesn't count toward
the 10/24h submission quota, and doesn't need an `Idempotency-Key`.

---

## Reviews

One Review per User per Venue: a required 1–5 rating plus optional written
commentary. Only Verified Venues can be reviewed.

### `GET /v1/venues/{venueID}/reviews/me` — Read my review

Shows the caller's own Username and display name even under a rename
requirement or suspension (unlike the public list).

### `PUT /v1/venues/{venueID}/reviews/me` — Write or replace my review

**Key rules:**
- The Venue must be Verified — a pending one is `409 venue_not_verified`.
- Replaces the whole Review (rating + commentary) in one call; there's no
  partial update.
- Saving the same rating and commentary again is a no-op: `200`,
  `updated_at` unchanged.
- If a Platform Moderator has placed a **Commentary restriction** on this
  User for this Venue, commentary is rejected with
  `409 commentary_locked` — the rating alone still saves. See
  [`moderation.md`](moderation.md#lift-a-commentary-restriction) for how a
  restriction is lifted.
- No `Idempotency-Key` needed — the request is naturally idempotent (it
  replaces, not appends).

### `DELETE /v1/venues/{venueID}/reviews/me` — Delete my review

Ends the Review. Reviewing the same Venue again later is a brand-new Review,
not a revival of the old one.

---

## Check-ins

A personal record of visiting a Venue — "I was here" — not proof of physical
presence.

### `POST /v1/venues/{venueID}/check-ins` — Check in

**Key rules:**
- Requires `Idempotency-Key`.
- Works on a pending *or* Verified Venue (unlike Reviews).
- `visited_at` defaults to now; it must be within the last 24 hours and no
  more than 5 minutes in the future.
- **Check-in spacing**: at least one hour must separate two of the same
  User's check-ins at the same Venue, even across the Venue being Delisted
  and later reopened. A tighter gap is refused. To "move" a mistaken
  check-in, delete it and record a new one.

### `GET /v1/venues/{venueID}/check-ins` — Venue's recent check-ins

The **Check-in feed**: the most recent 20 check-ins at this Venue, newest
first, by public identity. Requires a Profile (any status, including
suspended) — this is the one venue-scoped read that an app caller without a
Profile cannot do.

### `GET /v1/me/check-ins` — My check-in history

Every check-in the caller has ever recorded, newest first. Readable under a
rename requirement or suspension; only recording and deleting are blocked.

### `DELETE /v1/me/check-ins/{checkInID}` — Delete a check-in

Removes it, and frees up its hour of spacing immediately.

---

## Reports

Three report types, one shared **Report quota**: 20 reports of any kind per
User in a rolling 24 hours (`429 quota_exceeded` beyond that). Filing uses a
slot permanently — dismissal, resolution, or the target's account deletion
never returns it.

### `POST /v1/profiles/{username}/reports` — Report a profile

For abuse, impersonation, personal information, or spam on someone's public
identity. A User can't report their own Profile. A note is required for
`impersonation`, `personal_information`, and `other`.

### `POST /v1/venues/{venueID}/reports` — Report a venue's listing

For incorrect or outdated venue information — not a way to hide a venue a
User dislikes. The Submitter can't report their own venue. Only a venue
currently in the Public catalogue can be reported; anything else is
`404` (indistinguishable from "doesn't exist"). A note is required for
`incorrect_details` and `other`.

### `POST /v1/venues/{venueID}/reviews/{reviewID}/reports` — Report a review

For another User's written commentary on safety grounds. A rating-only
Review (no commentary, or commentary already removed) can't be reported —
there's nothing to assess.

All three share `Idempotency-Key`-required semantics and return `201` with
no reference back to the reporter's identity in the stored report.
