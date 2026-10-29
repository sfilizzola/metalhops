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
    npm run sync:design          # copy design/system/ CSS into src/design/

Node 22.12 or newer. Setup, Supabase settings, and Vercel notes are in
[README.md](README.md).

## Layout

- `src/main.tsx`: mounts the app in `BrowserRouter`, imports the global CSS.
- `src/App.tsx`: route table. `/login` and `/signup` are Public; `/`,
  `/discover`, `/check-ins`, `/my-venues`, and `/profile` are App routes behind
  `RequireAuth`. Any other path redirects to `/`.
- `src/api.ts`: pure `fetch` client for the Go API. It takes a `getToken`
  function and holds no session state.
- `src/supabase.ts`: the single Supabase client. Create no other.
- `src/auth/`: `AuthProvider` and `useSession`, `AuthPage` (sign-in and
  sign-up), and `guards.tsx` (`RequireAuth`, `RedirectIfAuthed`).
- `src/shell/`: `AppShell`, `Sidebar`, `MobileNav` (top bar and drawer),
  `NavItem`, `shell.css`. `nav.ts` is the single list of sections.
- `src/pages/`: `Home`, `Discover`, `CheckIns`, `MyVenues`, and
  `UnderConstruction`, the placeholder for sections not built yet.
- `src/profile/`: `MeProvider` (loads `GET /v1/me` once and exposes `useMe()`),
  `ProfilePage`, `ProfileForm`, `initials.ts` (avatar initials, shared with
  the sidebar), `profile.css`.
- `src/ui/`: shared primitives: `Button`, `Field`, `Alert`, `Spinner` (with
  `Skeleton`), `Icon`.
- `src/design/`: generated copy of `design/system/tokens.css` and
  `components.css`. Header says generated.
- `scripts/sync-design.mjs`: writes `src/design/` from `design/system/`.

## Rules

- Call Public and App routes only. Never call a `/v1/moderation/...` route;
  a User token there is `403 forbidden`, and that surface belongs to `admin/`.
- Send the access token only to `VITE_API_BASE_URL`.
- No secrets. `VITE_*` values are public in the bundle: use the Supabase
  publishable key, never a service-role or secret key.
- Sign-in and sign-up errors stay generic so they do not reveal whether an
  email is registered.
- Never edit `src/design/` by hand. After any change under `design/system/`,
  run `npm run sync:design` and commit the copy.
- Use design tokens (`var(--color-*)`, `var(--space-*)`, `var(--type-*)`) and
  the design system classes. No literal colours, font families, or other
  one-off values in component CSS.
- Icons come from Lucide (`lucide-react`) only, at the design system's sizes.
- Components the design system does not specify (sidebar, top bar, drawer,
  nav item, under-construction state) are derived. Keep them documented as
  derived in [README.md](README.md#derived-components).
- Only Profile is functional. Other sections use `UnderConstruction` and must
  not show fake or sample data.
- The navigation list lives in `src/shell/nav.ts`. Add or rename a section
  there, not in the sidebar or drawer.
- The sidebar and drawer switch at 960px (`src/shell/shell.css`). Change that
  value only with the derived-components note in README.md.
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
