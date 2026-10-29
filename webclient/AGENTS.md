# Web client agent guide

Consumer-facing web app: Vite, React, and TypeScript. It is the MVP that ships
before Android and iOS. Run commands from `webclient/`.

## Commands

    cp .env.example .env.local   # first time only, then fill the VITE_* values
    npm ci                       # install from the lockfile
    npm run dev                  # http://localhost:5173 (strict port)
    npm test                     # vitest run
    npm run typecheck            # tsc --noEmit
    npm run build                # typecheck, then vite build

Node 22.12 or newer. Setup, Supabase settings, and Vercel notes are in
[README.md](README.md).

## Layout

- `src/api.ts`: pure `fetch` client for the Go API. It takes a `getToken`
  function and holds no session state.
- `src/supabase.ts`: the single Supabase client. Create no other.
- `src/App.tsx`: sign-in and sign-up screen, session state, and Profile
  creation.

## Rules

- Call Public and App routes only. Never call a `/v1/moderation/...` route;
  a User token there is `403 forbidden`, and that surface belongs to `admin/`.
- Send the access token only to `VITE_API_BASE_URL`.
- No secrets. `VITE_*` values are public in the bundle: use the Supabase
  publishable key, never a service-role or secret key.
- Sign-in and sign-up errors stay generic so they do not reveal whether an
  email is registered.
- Ask before adding a dependency.
- Update this file and [README.md](README.md) in the same pull request when a
  command, path, or rule changes.

## API reference

Before work starts, read:

- [`docs/api/README.md`](../docs/api/README.md): business map of every route,
  grouped by caller (public, app, moderation)
- [`docs/api/public.md`](../docs/api/public.md) and
  [`docs/api/app.md`](../docs/api/app.md): the two groups this stack calls
- [`docs/api-handoff.md`](../docs/api-handoff.md): step-by-step call
  sequences per feature
- [`CONTEXT.md`](../CONTEXT.md): glossary; capitalized terms mean exactly
  what it defines
- [`backend/openapi.yaml`](../backend/openapi.yaml): the exact field
  contract, served at `/openapi.yaml` and browsable at `/docs`

## Definition of done

`npm run typecheck`, `npm test`, and `npm run build` all pass.
