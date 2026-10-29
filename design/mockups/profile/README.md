# Profile mockups (design exploration)

Static HTML drafts for the Profile user page and edit screen. Open `index.html` by double-click: it shows each page at 375 and 1280 px with a dark/light toggle. **Sample data, not for production.** The chosen variant is implemented later in React in `webclient/`.

Files: `page-a-stacked.html`, `page-b-stage.html`, `page-c-split.html` (user page), `edit-1-sectioned.html`, `edit-2-two-column.html` (edit), `mockup.css` (shared layout and shell, tokens only), `mockup.js` (theme toggle, mockup only), `screenshots/`. Pages link `../../system/tokens.css` and `components.css`; a `?theme=light` query param sets the theme.

## Variants

| Page | Variant |
| --- | --- |
| A | Stacked hero: centred avatar, name, bio, Edit button, then stat tiles, Coming soon, details, account. Closest to today. |
| B | Stage card: wide raised card, avatar breaking its top edge, display-size name, counters as a strip docked at the card bottom, details in a two-column grid. |
| C | Split: at 960 px and up a sticky identity rail (avatar, name, From, Edit) beside stats, Coming soon, details (bio, gender) and account; stacked on mobile. |
| 1 | Edit, single column with section headings and hairlines; Save/Cancel in a sticky bottom bar on mobile (page padding equals the bar), inline on desktop. |
| 2 | Edit, each section a raised card; two columns from 960 px (photo + identity left, about + location right), actions bottom right; stacked on mobile. |

## Position answers

| | Narrative role | Viewer distance | Visual temperature | Capacity |
| --- | --- | --- | --- | --- |
| A | "This is me": summary, then facts | Arm's length (phone), desk (desktop) | Warm, quiet, one amber action | One column absorbs growth |
| B | A stage: person presented, numbers as set list | Name and avatar from across a desk | Warmest, most theatrical | Strip can dock more counters; grid grows by row |
| C | Identity beside activity | Desk; rail stays in view | Cool, organised, dashboard-like | Right column grows freely; rail fixed |
| 1 | A task: change, save, leave | Arm's length, one thumb path | Neutral, calm, no cards | Sections stack without limit |
| 2 | A workspace: who you are / the details | Desk; both columns visible | Structured, cards like the user page | Right column takes longer content |

## Tokens used

Colour: `--color-surface`, `-surface-raised`, `-surface-overlay`, `-surface-field`, `-on-surface`, `-on-surface-muted`, `-outline`, `-outline-variant`, `-primary`, `-primary-container`, `-on-primary-container`. Type: `--type-display/title/heading/body/label/caption-*`, `--font-body`, `--font-display` (Grenze Gotisch only for the name, page title and wordmark). Also `--space-*`, `--radius-*`, `--border-*`, `--size-*`, `--elevation-1`. Components reused from `components.css`: `.btn`, `.field`, `.avatar--photo`, `.tag`, `.identity`, `.icon`, `.t-*`. Derived (documented here): `--avatar-xl` (2 x `--space-16`), `--avatar-lg`, the shell (copied from `webclient/src/shell/shell.css`), select/textarea styles (from `webclient/src/ui/ui.css`), stat tile, Coming soon group, stat strip, sticky action bar. Icons: Lucide 0.469.0, inline.
