# MetalHops API reference

This is the human-readable map of the backend API: what each endpoint is
*for*, in MetalHops business terms, and who is allowed to call it. It sits
beside two other sources and does not repeat them:

- [`backend/openapi.yaml`](../../backend/openapi.yaml) is the exact contract
  (field names, types, status codes, examples). Browse it at `GET /docs`
  (Scalar) on a running instance, or read it raw at `GET /openapi.yaml`.
- [`CONTEXT.md`](../../CONTEXT.md) is the glossary. Capitalized terms below
  (User, Profile, Venue, Review, Check-in, Platform Moderator, …) mean exactly
  what it defines them to mean.
- [`api-handoff.md`](../api-handoff.md) is the step-by-step call sequence for
  building a client (admin app, mobile app). Read it once you know which
  group of endpoints you need; this reference is for looking up one endpoint
  at a time.
- [`user-flows.md`](user-flows.md) is a plain-language walkthrough of sign-up,
  sign-in, and a check-in. Start there if the route pages are too dense.

If this file and `openapi.yaml` ever disagree on a field, status code, or
error, `openapi.yaml` wins — open an issue, it means this doc is stale.

## Who calls the API

Every route belongs to exactly one of three groups, matched to one calling
identity:

| Group | Doc | Caller | Needs `Authorization`? |
| --- | --- | --- | --- |
| **Public** | [`public.md`](public.md) | Anyone — a website visitor, an app before sign-in, a search crawler | No |
| **App** | [`app.md`](app.md) | A signed-in **User** (a Supabase sign-in with a MetalHops **Profile**) | Yes |
| **Moderation** | [`moderation.md`](moderation.md) | A **Platform Moderator** — a trusted MetalHops operator, not a User | Yes |

A Platform Moderator calling an App route, or a User calling a Moderation
route, gets `403 forbidden`. The two identities never overlap.

## Conventions shared by every route

These rules apply across all three groups; each endpoint page only calls out
what's different.

- **Errors** are always `{"error":{"code":…,"message":…,"request_id":…}}`.
  Branch on `code`, never on `message`. A `400 validation_error` adds
  `fields`, naming the invalid input.
- **Auth**: a protected route reads `Authorization: Bearer <Supabase JWT>`
  from a live Supabase session. Missing or rejected is `401 unauthorized`.
- **Pagination**: list endpoints take `limit` (1–100, default 20) and return
  `meta.next_cursor`. Send that cursor back unchanged for the next page;
  `null` means the last page. A cursor from different filters is
  `400 validation_error`.
- **Idempotency**: routes that create something important
  (`createVenueSubmission`, `createProfileReport`, `reportVenue`,
  `reportReview`, `createCheckIn`, `applyProfileModeration`,
  `applyVenueAction`, `applyReviewAction`) require an `Idempotency-Key`
  header. Reusing a key with the same request replays the first response;
  reusing it with a different request is `409 idempotency_key_reused`.
- **Rate limits**: every route (except `/healthz` and `/readyz`) is limited
  per client IP. Writes by a signed-in caller also draw from a personal
  bucket. A refusal is `429 rate_limited` with `Retry-After`.
- **Request bodies** are JSON, at most 64 KiB, UTF-8, one object, and reject
  unknown fields.

## Finding an endpoint

Start from the business question you're answering:

- "How does someone sign up and check in?" → [`user-flows.md`](user-flows.md)
- "How do people find a bar?" → [`public.md`](public.md)
- "How does a User submit a venue, leave a review, or check in?" →
  [`app.md`](app.md)
- "How does a moderator handle a report or verify a venue?" →
  [`moderation.md`](moderation.md)

Each page opens with a quick-reference table (method, path, business purpose)
followed by one section per endpoint written in plain language, with a
pointer to the matching `operationId` in `openapi.yaml` for exact field
contracts.
