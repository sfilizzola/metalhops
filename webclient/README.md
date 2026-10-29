# MetalHops web client

Consumer-facing web app (Vite, React, TypeScript): the MVP that ships before
the Android and iOS apps. It calls the Go API's Public and App routes only. It
never calls a `/v1/moderation/...` route; that is `admin/`'s job.

Agent rules for this folder: [AGENTS.md](AGENTS.md).

## What it does

- Sign up, sign in, and sign out with Supabase Auth (email and password).
- Signed-in app shell: a sidebar on desktop, a top bar and drawer on narrow
  screens, with five sections (see [Routes](#routes)).
- Loads the caller's Profile with `GET /v1/me`, creates one with `PATCH /v1/me`
  when none exists, and edits it.
- Home, Discover, Check-ins, and My venues show an under-construction page.
  They show no data yet.

## Routes

| Path | Section | Access | State |
| --- | --- | --- | --- |
| `/login` | Sign in | Public | Functional |
| `/signup` | Sign up | Public | Functional |
| `/` | Home | App | Under construction |
| `/discover` | Discover | App | Under construction |
| `/check-ins` | Check-ins | App | Under construction |
| `/my-venues` | My venues | App | Under construction |
| `/profile` | Profile | App | Functional |

Signed-in users on `/login` or `/signup` go to `/`. Signed-out users on an App
route go to `/login`. Any other path redirects to `/`.

The sidebar and drawer read one list, `src/shell/nav.ts`:

| Label | Path | Icon (Lucide) |
| --- | --- | --- |
| Home | `/` | `House` |
| Discover | `/discover` | `Compass` |
| Check-ins | `/check-ins` | `MapPinCheck` |
| My venues | `/my-venues` | `Store` |
| Profile | `/profile` | `UserRound` |

The active link has `aria-current="page"`. The identity block at the bottom of
the sidebar links to `/profile`.

## Profile page

| Field | Editable | Notes |
| --- | --- | --- |
| Username | Yes | 3-30 letters, digits, or underscores |
| Display name | Yes | 1-80 characters |
| Status | No | Shown as a badge: Active, Rename required, or Suspended |
| Email | No | From the sign-in account |
| Moderation reason, note, suspended until | No | Shown only when status is not active |

- With no Profile yet (`404 profile_not_found`), the page shows the same form
  in create mode.
- While status is `suspended`, the API rejects changes to either field, so the
  inputs are disabled and an explanation is shown.
- Save stays disabled until a field changes. Cancel restores the stored values.
- A `409` marks Username as taken; a `400` marks the field the API rejects.
- The page loads the caller's own `/v1/me`. It cannot address another user's
  Profile.

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
| `VITE_API_BASE_URL` | `http://localhost:8080` locally; an `https://` URL for builds |

Use the publishable key only. Never put the service-role or secret key here.
Every `VITE_*` value is public in the built bundle.

`VITE_API_BASE_URL` is required for builds. Only `npm run dev` falls back to
`http://localhost:8080` when it is empty; a built app has no default.

If either Supabase variable is missing, or `VITE_API_BASE_URL` is empty (or,
outside `npm run dev`, not `https://`), the app shows "Missing configuration"
instead of the sign-in page and sends no request.

## Commands

Run from `webclient/`:

    npm run dev         # http://localhost:5173 (strict port)
    npm test            # vitest run
    npm run typecheck   # tsc --noEmit
    npm run build       # typecheck, then vite build into dist/
    npm run preview     # serve the built bundle
    npm run sync:design # copy design/system/ CSS into src/design/

## Design system

The look comes from the Backline design system in `design/system/` at the
repository root. The web client does not read that folder at runtime. It uses
a copy in `src/design/` (`tokens.css` and `components.css`), which the
`sync:design` script writes.

- After any change under `design/system/`, run `npm run sync:design` and commit
  the updated `src/design/` files.
- Never edit `src/design/` by hand. Its header says it is generated.
- `src/design-sync.test.ts` fails when the copy differs from `design/system/`,
  so `npm test` catches a missed sync.
- Fonts: Grenze Gotisch for display and titles, IBM Plex Sans elsewhere
  (`src/index.css`).
- Icons: Lucide only (`lucide-react`).

### CI path filter

`.github/workflows/webclient.yml` runs typecheck, tests, and build when a change
touches `webclient/**` or `design/system/**`, on pushes to `main` and `dev` and
on pull requests. A design change therefore always runs the sync test.

## Derived components

The design system has classes for tokens, buttons, fields, alerts, identity,
list states, skeletons, and icons. It has no spec for the app shell, the profile
summary, or multi-line and select fields. These
components are derived from those tokens and are not part of the design
system:

| Component | File | What it is |
| --- | --- | --- |
| `AppShell` | `src/shell/AppShell.tsx` | Page layout: sidebar, top bar and drawer, `<main>` |
| `Sidebar` | `src/shell/Sidebar.tsx` | Wordmark, nav list, identity block, Log out |
| `MobileNav` | `src/shell/MobileNav.tsx` | Top bar with menu button and the drawer (`role="dialog"`) |
| `NavItem` | `src/shell/NavItem.tsx` | One nav link: icon and label |
| `UnderConstruction` | `src/pages/UnderConstruction.tsx` | Placeholder page built on `.list-state` |
| Profile summary card | `src/profile/ProfilePage.tsx`, `profile.css` | Avatar, name, bio, From and gender on `surface-raised` with `elevation-1`, like `.venue-card` |
| Profile stat tiles | `src/profile/ProfilePage.tsx`, `profile.css` | Check-in and Venue counters in a 2-column row; Following, Followers and Crews in one dashed "Coming soon" group of 3 columns, muted text at full contrast |
| `AvatarUpload` | `src/profile/AvatarUpload.tsx` | Add, change and remove photo using `Button`, `Alert` and the Supabase `avatars` bucket |
| `Field` textarea and select | `src/ui/Field.tsx`, `src/ui/ui.css` | `as="textarea"` and `as="select"` variants with the same label, help and error pattern; the select hides the native arrow (`appearance: none`) and shows a Lucide `ChevronDown`; the design system has no spec for either |

Why they are derived: the design system gives no layout, navigation, or
placeholder pattern. Each one uses existing tokens, so a change to the design
system still flows through.

### Breakpoint: 960px

The sidebar shows at 960px and wider. Below that, the sidebar is hidden and the
top bar with the drawer takes its place. The value comes from the showcase's
content breakpoint, and it is set once, in the `@media` block at the end of
`src/shell/shell.css`.

The drawer closes on Escape, scrim click, navigation, and route change. Focus
moves into the drawer on open, stays inside it, and returns to the menu button
on close.

jsdom does not evaluate media queries, so no test checks the 960px switch or
horizontal scroll. Check both in a browser at 360px and at desktop width.

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

- Vercel project settings: Root Directory `webclient` (the app cannot build
  from the repository root), Framework Preset Vite, Build Command
  `npm run build`, Output Directory `dist`.
- `webclient/vercel.json` rewrites every path to `/index.html`. Without it,
  a page load or refresh on `/profile` returns 404, because routes are handled
  in the browser by React Router. Do not remove it.
- `webclient/vercel.json` also sets security headers on every path:
  `X-Content-Type-Options: nosniff`, `Referrer-Policy:
  strict-origin-when-cross-origin`, `X-Frame-Options: DENY`, and
  `Content-Security-Policy: frame-ancestors 'none'`. A full CSP
  (`default-src`, `connect-src`, ...) is a follow-up: it needs the real
  Supabase and API origins, which are not known until deployment.
- Set the three `VITE_*` variables in the Vercel dashboard for each
  environment. Redeploy after any change; Vite inlines them at build time.
- Add the production origin (exact scheme and host, no path) to the API's
  `ALLOWED_ORIGINS`, and the Vercel URL to the Supabase Site URL and redirect
  list.
- Preview deployment URLs are random, so the exact-match `ALLOWED_ORIGINS`
  list cannot allow them. Never use a wildcard.

The production API is `https://metalhops-qua7.onrender.com`; its docs are at
`/docs`.

## What is not done

- Home, Discover, Check-ins, and My venues are placeholders. Venue data,
  reviews, ratings, and check-ins are not built.
- Venue submission is not built.
- No deployment: no Vercel project and no production Supabase URL settings.
- The wordmark is the design system's placeholder.
- The 360px and desktop layouts have not been checked in a browser.
- No automated test covers the 960px switch.
