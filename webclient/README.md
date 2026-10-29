# MetalHops web client

Consumer-facing web app (Vite, React, TypeScript): the MVP that ships before
the Android and iOS apps. It calls the Go API's Public and App routes only. It
never calls a `/v1/moderation/...` route; that is `admin/`'s job.

Agent rules for this folder: [AGENTS.md](AGENTS.md).

## What it does

- Sign up, sign in, and sign out with Supabase Auth (email and password).
- Calls `GET /v1/me` with the session token.
- Creates a minimal Profile with `PATCH /v1/me`: Username of 3-30 letters,
  digits, or underscores, and a display name of 1-80 characters.

## Prerequisites

- Node 22.12 or newer, and npm.
- A Supabase project (see [Supabase settings](#supabase-settings)).
- The Go API running locally. Render's deployed API does not accept browser
  origins until its `ALLOWED_ORIGINS` is set, so run it locally for now:

      cd backend && make run   # http://localhost:8080

  `backend/.env` must contain `ALLOWED_ORIGINS=http://localhost:5173`.

## First-time setup

    cd webclient
    cp .env.example .env.local
    npm ci

Fill `.env.local`:

| Variable | Value |
| --- | --- |
| `VITE_SUPABASE_URL` | Supabase project URL |
| `VITE_SUPABASE_PUBLISHABLE_KEY` | Supabase publishable key |
| `VITE_API_BASE_URL` | `http://localhost:8080` locally |

Use the publishable key only. Never put the service-role or secret key here.
Every `VITE_*` value is public in the built bundle.

## Commands

Run from `webclient/`:

    npm run dev         # http://localhost:5173 (strict port)
    npm test            # vitest run
    npm run typecheck   # tsc --noEmit
    npm run build       # typecheck, then vite build into dist/
    npm run preview     # serve the built bundle

## Supabase settings

The app depends on these dashboard settings:

- **Authentication > Providers:** Email is enabled.
- **Email confirmation:** if enabled, sign-up shows "Check your email to
  confirm your account" and the person signs in after confirming. If
  disabled, sign-up signs them in directly.
- **Authentication > URL Configuration:** Site URL `http://localhost:5173`;
  redirect URL allow-list `http://localhost:5173/**`.
- The hosted default mailer is rate-limited. Expect confirmation emails to
  throttle during testing.
- **Asymmetric JWT signing keys.** The Go API rejects HS256 tokens. Check
  that this returns a non-empty `keys` array:

      curl <SUPABASE_URL>/auth/v1/.well-known/jwks.json

## Prepared for Vercel (not deployed yet)

Nothing is deployed. When publishing:

- Vercel project settings: Root Directory `webclient`, Framework Preset
  Vite, Build Command `npm run build`, Output Directory `dist`.
- Set the three `VITE_*` variables in the Vercel dashboard for each
  environment. Redeploy after any change; Vite inlines them at build time.
- Add the production origin (exact scheme and host, no path) to the API's
  `ALLOWED_ORIGINS`, and the Vercel URL to the Supabase Site URL and redirect
  list.
- Preview deployment URLs are random, so the exact-match `ALLOWED_ORIGINS`
  list cannot allow them. Never use a wildcard.
- No `vercel.json` until the app has client-side routes.

The production API is `https://metalhops-qua7.onrender.com`; its docs are at
`/docs`.
