# API handoff

This is the sequence guide for the admin design cycle and for a later Google
Play disclosure. [`backend/openapi.yaml`](../backend/openapi.yaml) is the
field contract. Each step names the method, path, caller, success status, and
the `operationId` to read for fields. A rule appears here only when it
changes the sequence. [CONTEXT.md](../CONTEXT.md) keeps the language.

## Calling the API

Callers:

- **anyone**: no `Authorization` header is read. Only the per-IP limit
  applies.
- **app caller**: a Supabase sign-in that is not a Platform Moderator. It may
  not have a Profile yet.
- **User**: an app caller with a Profile. A write needs an active Profile.
  While a rename requirement or a Community Suspension stands, a write is
  `403 profile_rename_required` or `403 profile_suspended`, and reads still
  work.
- **Platform Moderator**: a caller with a `platform_operators` row.

Every protected route takes `Authorization: Bearer <Supabase JWT>` for a live
session. A missing or rejected token is `401 unauthorized`. A Platform
Moderator on an app route, or an app caller on a moderation route, is
`403 forbidden`.

`Idempotency-Key` is required on `createVenueSubmission`,
`createProfileReport`, `reportVenue`, `reportReview`, `createCheckIn`,
`applyProfileModeration`, `applyVenueAction`, and `applyReviewAction`. Use a
new key for each new intent. The same key with the same command replays the
stored response. The same key with a different command is
`409 idempotency_key_reused`. Every other route takes no key.

Every error body is `{"error":{"code":…,"message":…,"request_id":…}}`.
`fields` names invalid inputs on a `400 validation_error`. Branch on `code`,
never on `message`.

A paginated list takes `limit` from 1 to 100, default 20, and returns
`meta.next_cursor`. Send that cursor back unchanged for the next page; `null`
means the last page. A cursor from other filters or another route is
`400 validation_error` with `cursor: invalid`. The paginated lists are
`listVenues`, `listVenueReviews`, `listMyVenues`, `listMyCheckIns`,
`listProfileReports`, `listVenueReports`, and `listReviewReports`.

## User sequences

Create a Profile:

1. `GET /v1/me` · app caller · `200` · `getMe`. `404 profile_not_found`
   means this sign-in has no Profile yet.
2. `PATCH /v1/me` · app caller · `200` · `patchMe`. The first call creates the
   Profile; later calls update it.
3. `GET /v1/me` · User · `200` · `getMe`.

Discover Venues:

1. `GET /v1/genres` · anyone · `200` · `listGenres`
2. `GET /v1/venues` · anyone · `200` · `listVenues`
3. `GET /v1/venues/{venueID}` · anyone · `200` · `getVenue`
4. `GET /v1/venues/{venueID}/reviews` · anyone · `200` · `listVenueReviews`

Submit and correct a Venue:

1. `POST /v1/venues` · User · `201` · `createVenueSubmission`. The Venue is
   pending and already in the Public catalogue.
2. `GET /v1/me/venues` · app caller · `200` · `listMyVenues`
3. `GET /v1/me/venues/{venueID}` · User · `200` · `getMyVenue`
4. `PUT /v1/me/venues/{venueID}` · User · `200` · `correctMyVenue`. Only a
   pending or rejected Venue can be corrected. Correcting a rejected Venue
   returns it to pending.

Save and delete a Review:

1. `PUT /v1/venues/{venueID}/reviews/me` · User · `201` new, `200` replaced ·
   `putMyReview`. The Venue must be verified; a pending Venue is
   `409 venue_not_verified`.
2. `GET /v1/venues/{venueID}/reviews/me` · User · `200` · `getMyReview`
3. `DELETE /v1/venues/{venueID}/reviews/me` · User · `204` · `deleteMyReview`.
   Saving again later is a new Review.

File a report:

- `POST /v1/profiles/{username}/reports` · User · `201` · `createProfileReport`
- `POST /v1/venues/{venueID}/reports` · User · `201` · `reportVenue`
- `POST /v1/venues/{venueID}/reviews/{reviewID}/reports` · User · `201` ·
  `reportReview`

All three share the Report quota of 20 in a rolling 24 hours.

Record, list, and delete a Check-in:

1. `POST /v1/venues/{venueID}/check-ins` · User · `201` · `createCheckIn`
2. `GET /v1/me/check-ins` · app caller · `200` · `listMyCheckIns`
3. `GET /v1/venues/{venueID}/check-ins` · User · `200` · `listVenueCheckIns`
4. `DELETE /v1/me/check-ins/{checkInID}` · User · `204` · `deleteMyCheckIn`

Correcting a Check-in is a delete, then a new `createCheckIn` with a new key.

## Moderation sequences

Every step here is a Platform Moderator call. An action that changes state is
`201` with an `action_id`. An action that changes nothing is `200` with
`action_id` null. A dismissal resolves only that one report.

Profile:

1. `GET /v1/moderation/profile-reports` · Platform Moderator · `200` ·
   `listProfileReports`
2. `GET /v1/moderation/profiles/{userID}` · Platform Moderator · `200` ·
   `getModerationProfile`
3. Act: `POST /v1/moderation/profiles/{userID}/actions` · Platform Moderator ·
   `201` or `200` · `applyProfileModeration`
4. Or dismiss: `PATCH /v1/moderation/profile-reports/{reportID}` · Platform
   Moderator · `200` · `dismissProfileReport`

Venue:

1. `GET /v1/moderation/venue-reports` · Platform Moderator · `200` ·
   `listVenueReports`
2. `GET /v1/moderation/venues/{venueID}` · Platform Moderator · `200` ·
   `getModerationVenue`
3. Act: `POST /v1/moderation/venues/{venueID}/actions` · Platform Moderator ·
   `201` or `200` · `applyVenueAction`
4. Or correct: `PUT /v1/moderation/venues/{venueID}` · Platform Moderator ·
   `200` · `correctVenueAsOperator`
5. Or dismiss: `PATCH /v1/moderation/venue-reports/{reportID}` · Platform
   Moderator · `200` · `dismissVenueReport`

A pending Venue Submission has no queue of its own. It appears in
`listVenues` as not verified; `getModerationVenue` reads it, and
`applyVenueAction` verifies or rejects it.

Review:

1. `GET /v1/moderation/review-reports` · Platform Moderator · `200` ·
   `listReviewReports`
2. `GET /v1/moderation/reviews/{reviewID}` · Platform Moderator · `200` ·
   `getModerationReview`. It shows the standing Commentary restriction, if
   any.
3. Act: `POST /v1/moderation/reviews/{reviewID}/actions` · Platform
   Moderator · `201` or `200` · `applyReviewAction` with `remove_commentary`
   or `delete_review`
4. Or dismiss: `PATCH /v1/moderation/review-reports/{reportID}` · Platform
   Moderator · `200` · `dismissReviewReport`
5. Either action begins a Commentary restriction for that User and Venue.
   The User can still save a rating, but commentary on `putMyReview` is
   `409 commentary_locked`, with the Restriction explanation.
6. Lift: `POST /v1/moderation/reviews/{reviewID}/actions` · Platform
   Moderator · `201` · `applyReviewAction` with
   `{"action":"lift_commentary_restriction"}`

The lift is an action on a Review the User currently has for that Venue, on a
verified or Delisted Venue. It carries no reason of its own; a `reason` or
`note` is `400 validation_error` with `must be omitted`. Its Restriction
record carries the Restriction explanation of the decision that began the
restriction ([ADR 0005](adr/0005-ending-restriction-carries-its-explanation.md)).
When no Commentary restriction stands, the lift is `200` with `action_id`
null and leaves no record.

After the Review that began the restriction has ended, that `reviewID` is
`404 review_not_found`. The lift waits until that User has a Review there
again, and goes through that Review. No moderation route yet finds that newer
Review. While the restriction stands it is rating-only, and a rating-only Review
cannot be reported, so it never reaches `listReviewReports`. `listVenueReviews`
is a public list, not a lookup: it is empty unless the Venue is verified, and
it does not name an author under a rename requirement or a Community
Suspension. The admin design must not assume that lookup exists.

After the lift, the Review stays rating-only and the removal stays visible
until the User writes commentary. The User's next `putMyReview` with
commentary saves it and clears the removal. A rating-only save leaves the
removal visible.

## Account deletion

`DELETE /v1/me` · app caller · `204` · `deleteMe`, with the body
`{"confirmation":"DELETE MY ACCOUNT"}`.

- Every authenticated caller except a Platform Moderator may call it,
  including a sign-in with no Profile and a suspended User.
- It needs a live session created within the previous 10 minutes. An older
  session is `403 recent_authentication_required`; sign in again, then retry.
- It takes no `Idempotency-Key`.
- `204` is returned only after Supabase deletes the auth user. A provider
  failure is `502 auth_provider_error`, and every local row stays.

Removed with the auth user:

- the Profile;
- Reviews;
- Check-ins;
- idempotency rows;
- Commentary restriction rows;
- Profile Reports, because both the reporter and the target cascade;
- Review Reports, because the reporter cascades and the author's Review
  cascades;
- Venue Reports whose reporter is the deleted User.

Retained:

- Venue Submissions, with `submitted_by` set null;
- Venue Reports filed by someone else;
- the Profile and Review moderation audits, with deleted User ids set null.
  These are the Restriction records that
  [ADR 0003](adr/0003-keep-restriction-records-after-user-deletion.md) keeps.
- the Venue moderation audit, as that Venue's eligibility history. It is not
  a Restriction record.

A Commentary restriction row is not a Restriction record. A standing
Commentary restriction ends with the User.
