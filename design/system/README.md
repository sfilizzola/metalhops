# MetalHops design system · Backline

Foundation tokens and core components for the web client, Android (Compose)
and iOS (SwiftUI). Open `index.html` by double-click to see every token and
component in dark and light mode.

## Direction

**Backline**, after Territory Studio's on-screen interfaces for film: a stage
monitor under tungsten light. It is dark-first and also has a light mode.

- Oxblood black surfaces and one tungsten amber for primary actions,
  selections and ratings. A green pairs with the Verified Venue badge and the
  Check-in done state.
- Blackletter (Grenze Gotisch) for display and title text only: Venue
  names, page and section titles. Body text, labels and numbers always use
  IBM Plex Sans.
- Texture is a faint grain and glow on the dark page background only, never
  behind text.
- No skulls, flames or pentagrams. Icons come from Lucide only.

The two directions not chosen are kept for reference in `design/directions/`.

## Files

| File | What |
| --- | --- |
| `tokens.json` | Source of truth. Platform-neutral tokens and Compose role hints, plus the colour pairs that must pass contrast. |
| `tokens.css` | CSS custom properties for both modes. Generated, do not edit. |
| `tokens.js` | The tokens plus measured contrast ratios, for the showcase. Generated, do not edit. |
| `components.css` | Reference CSS for every component. Needs `tokens.css`. |
| `icons.js` | Inline Lucide SVG sprite for the showcase. Generated, do not edit. |
| `index.html`, `showcase.js` | The showcase page. |
| `build-tokens.mjs` | Builds `tokens.css` and `tokens.js`, and fails if a colour pair misses WCAG AA or a colour falls outside sRGB. |
| `build-icons.mjs` | Downloads the listed Lucide icons and builds `icons.js`. |

```sh
node design/system/build-tokens.mjs   # after editing tokens.json
node design/system/build-icons.mjs    # after adding an icon name (needs network)
```

Both scripts need only Node 18 or newer; there are no dependencies.

## Fonts

| Role | Font | Licence | Source |
| --- | --- | --- | --- |
| Display (display, title) | Grenze Gotisch, weight 700 | SIL Open Font License 1.1 | [Google Fonts](https://fonts.google.com/specimen/Grenze+Gotisch) |
| Body (heading, body, label, caption, all numbers) | IBM Plex Sans, weights 400 / 500 / 600 | SIL Open Font License 1.1 | [Google Fonts](https://fonts.google.com/specimen/IBM+Plex+Sans) |

The OFL allows bundling the fonts in Android and iOS apps. The showcase loads
them from Google Fonts. Without a network connection it falls back to Georgia
and the system sans-serif, and the layout still holds.

IBM Plex Sans renders numbers with tabular figures
(`font-variant-numeric: tabular-nums` on web; `fontFeatureSettings = "tnum"`
in Compose; `.monospacedDigit()` in SwiftUI).

## Icons

[Lucide](https://lucide.dev), version 0.469.0, ISC licence. Icons sit on a
24 px grid with a 2 px stroke and round caps, and are used at 16, 20 and
24 px. They are used as-is, never redrawn. Android can use the Lucide SVGs
as vector drawables; iOS can add them to the asset catalogue as template
images.

## Token naming

Every CSS custom property is `--<group>-<name>`:

| Group | Examples | Notes |
| --- | --- | --- |
| `color` | `--color-surface`, `--color-on-primary`, `--color-success-container` | `on-X` is the text and icon colour for use on `X`. Names follow Material 3 roles; `compose` in `tokens.json` gives the exact role, and roles marked `(custom)` need an extension colour scheme in Compose. |
| `type` | `--type-body-size`, `--type-title-line-height`, `--type-label-tracking` | Steps: display, title, heading, body, label, caption. Each has `-font`, `-size`, `-line-height`, `-weight` and `-tracking`. |
| `space` | `--space-1` (4) to `--space-16` (64) | Multiples of 4: the number is the step, not the pixel value. |
| `radius` | `--radius-sm`, `--radius-md`, `--radius-lg`, `--radius-full` | `full` is used for pills and avatars. |
| `border` | `--border-hairline` (1), `--border-strong` (2), `--border-focus` (3) | |
| `size` | `--size-tap-min` (48), `--size-icon-md`, `--size-avatar-full` | Tap targets are never below 48 px. |
| `elevation` | `--elevation-1`, `--elevation-2` | Pair each level with its surface: level 1 with `surface-raised`, level 2 with `surface-overlay`. On dark, depth comes from the lighter surface plus a hairline ring; the shadow only reinforces it. |
| `duration`, `easing` | `--duration-fast` (120 ms), `--easing-standard` | Durations drop to 0 when the user asks for reduced motion. |

Modes: `:root` and `[data-theme="dark"]` hold dark values, and
`[data-theme="light"]` holds light values. Lengths in `tokens.json` are
unitless: px on web, dp in Compose, pt in SwiftUI. The CSS converts type
sizes to rem so browser zoom works.

## Rules the components rely on

- Every text and icon pair listed in `tokens.json` → `contrast` meets WCAG
  AA in both modes: 4.5:1 for text, 3:1 for UI parts. The measured ratios are
  in the showcase. Add a pair there whenever you combine two colours in a
  new way.
- Hover and pressed states mix the fill toward `on-surface`, which only
  ever raises contrast with the text on top.
- Selection never relies on colour alone: selected filter chips add a check
  mark, and the rating input writes the value out as text.
- The Verified Venue badge means a moderator accepted the listing for the
  public catalogue. It uses a plain check mark, never a shield, crown or seal,
  and never the words "official", "owner" or "claimed".
- Sign-in errors are generic ("Email or password is incorrect") and never
  reveal whether an email is registered.
- The MetalHops wordmark in the showcase is a placeholder set in Grenze
  Gotisch. The real logo is a separate task.
