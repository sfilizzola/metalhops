# User flows: sign up/sign in, and check in

Plain-language walkthrough of two things a person does with MetalHops: getting
an account, and checking in at a venue. This explains every term as it comes
up — it does not replace [`api-handoff.md`](../api-handoff.md) or
[`backend/openapi.yaml`](../../backend/openapi.yaml), which stay the source of
truth for exact fields and status codes.

## Flow 1: Sign up, sign in, and create a profile

### Step 1 — Sign up or sign in (Supabase, not our API)

MetalHops doesn't have its own "create account" or "login" endpoint. That job
belongs to **Supabase Auth**, a separate service our backend trusts. The app
talks to Supabase directly with an email/password (or Google/Apple), and
Supabase hands back an **access token** — a signed piece of text called a
**JWT** (JSON Web Token) that proves "this person is signed in."

- Sign up: the app sends an email + password to Supabase, which creates the
  account.
- Sign in: the app sends the same email + password, and Supabase returns a
  fresh JWT.

Our Go API never sees the password. It only ever sees the JWT.

### Step 2 — Call our API with the token

From here on, every request the app makes to our API includes that token in
a header, proving who is calling:

```
Authorization: Bearer <the JWT from Supabase>
```

Our API checks the token is genuine and still valid, then knows *which*
Supabase user is calling. But at this point, that person is only an
"app caller" — signed in, but with no MetalHops identity yet (no username).

### Step 3 — Create the profile

The app calls `GET /v1/me`. The first time, this returns
`404 profile_not_found` — totally normal, it just means "no profile saved
yet."

The app then calls `PATCH /v1/me` with a username and display name:

```json
{ "username": "metal_fan", "display_name": "Metal Fan" }
```

This first call *creates* the profile. Only now does this person become a
**User** — someone who can submit venues, write reviews, and check in.
Calling `PATCH /v1/me` again later just edits the name.

**Summary:** Supabase handles "who are you," our API handles "what's your
MetalHops identity." A JWT bridges the two.

---

## Flow 2: Check in at a venue

A check-in is just "I was here" — a timestamped record of a User visiting a
venue. No special preparation needed beyond already being a User (Step 3
above).

### Step 1 — Pick a venue

The app already has a venue id from browsing (`GET /v1/venues`).

### Step 2 — Build the request

The app sends:

```
POST /v1/venues/{venueID}/check-ins
Authorization: Bearer <the same JWT as always>
Idempotency-Key: <a key the client makes up, see below>
Content-Type: application/json

{}
```

An empty body `{}` means "I'm here right now." To record a visit from
earlier, the app can send a timestamp instead:

```json
{ "visited_at": "2026-10-26T17:30:00Z" }
```

That timestamp must be within the last 24 hours and no more than 5 minutes in
the future — this isn't a tool for backdating visits from last week.

### What's the `Idempotency-Key`, and why does the client make it up?

Networks are unreliable — a request can time out even though the server
actually received and processed it. If the app just retried blindly, the
user could end up with two check-ins from one tap.

To prevent that, **the client generates a random key (e.g. a UUID) before
sending the request, and attaches it as the `Idempotency-Key` header.** If
the request has to be retried — same key, same body — the server recognizes
"I already did this" and just resends the original result instead of
creating a second check-in. A new, separate check-in needs a new key.

This same pattern is used anywhere an accidental duplicate would matter:
submitting a venue, filing a report, and checking in.

### Step 3 — Success

A new check-in returns `201 Created`:

```json
{
  "data": {
    "id": "ffffffff-ffff-4fff-8fff-ffffffffffff",
    "venue_id": "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
    "visited_at": "2026-10-26T17:10:00Z",
    "recorded_at": "2026-10-26T17:10:00Z",
    "venue": {
      "name": "Iron Maiden Pub",
      "city": "Oslo",
      "country_code": "NO",
      "venue_type": "pub",
      "verification_status": "verified"
    }
  }
}
```

### The one-hour spacing rule

A User can't check in at the *same venue* twice within one hour — this stops
someone from spamming check-ins at the same spot. Trying to do so returns
`409 check_in_spacing`, naming the earlier check-in that's blocking it. If a
check-in was a mistake, deleting it (`DELETE /v1/me/check-ins/{checkInID}`)
immediately frees up that hour.

**Summary:** a check-in is one `POST` with a client-generated idempotency
key for safety, and an optional timestamp if it isn't "right now."
