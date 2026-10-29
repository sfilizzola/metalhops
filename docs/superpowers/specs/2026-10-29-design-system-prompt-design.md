# Design system base: huashu-design prompt

## Goal

Give the `huashu-design` skill one self-contained prompt that produces the
base of the MetalHops design system: foundation tokens and core components on
a single HTML showcase page.

## Decisions

| Topic | Decision |
| --- | --- |
| Scope | Foundations plus core components on one showcase page; no screen mockups |
| Direction | Three contrasting directions as demos; the user picks one before system work |
| Tone | Unmistakably metal but approachable; a usable consumer app, not band merch |
| Modes | Dark-first with a light mode; semantic token names that map to Compose and SwiftUI |
| Logo | Placeholder wordmark in the display font, marked temporary |
| Output | `design/system/`; no change to `webclient/` |
| Prompt style | Product context written into the prompt, not pointers to repo docs |

The prompt carries the context itself because `CONTEXT.md` and the API docs
are dominated by moderation and backend rules that a design agent does not
need.

## Out of scope

- Real logo and brand mark.
- Screen mockups and flows.
- Wiring tokens into `webclient/`; a later task, which must ask before adding
  a dependency.
- Admin app styling.

## The prompt

Copy everything inside the fence below into a session with the
`huashu-design` skill attached, run from the repository root.

```text
Create the base of the MetalHops design system: foundation tokens plus core
components, shown on one HTML showcase page.

## Product

MetalHops is an Untappd-style app for finding rock and metal bars. Instead of
checking in beers, people discover, rate, and rank the places: bars, pubs, and
clubs where rock or metal is a regular part of the night, not a one-off event.
A maps app says a place is "a bar, 4.3 stars"; MetalHops says whether it plays
death metal or Top 40, and whether the beer is cold and there is a dartboard.

- Users add places as Venue Submissions. A Platform Moderator reviews each
  one; accepted ones become Verified Venues in the public catalogue.
- Users write one Review per Verified Venue: a required whole-number rating
  from 1 to 5 (5 is best) and optional commentary.
- Users record Check-ins at a Venue they are visiting. A Venue page shows its
  20 most recent Check-ins.
- A Venue has a Venue type (bar, pub, or club), one or more Genres (classic
  hard rock, death metal, black metal, folk metal, doom, thrash, punk,
  stoner, and so on), and extras (darts, pool, live music, outdoor seating,
  kitchen).
- Each User has a Profile: a Username (3-30 letters, digits, or underscores,
  shown as @username) and a display name (1-80 characters).

Platforms: a web app first (Vite, React, TypeScript), then native Android
(Jetpack Compose), then iOS (SwiftUI). This system is the shared source for
all three.

## Where it is used

On a phone, in a dim and loud bar, often one-handed, sometimes after a few
beers; also on a laptop while planning a trip to a new city. So: high
contrast, generous tap targets (at least 44 px), text that reads at arm's
length, and state that is obvious at a glance.

## Tone

Every direction must feel unmistakably rock and metal, and still be a
consumer app people use comfortably every week. It must feel at home in a
dark bar, not look like a band merch page. Directions differ in execution,
not in attitude.

- No blackletter or distressed type for body text, labels, or numbers.
  A characterful display face for headings is welcome.
- Texture and grit only where it never hurts legibility or contrast.
- No skulls, flames, or pentagram clichés as decoration.

## Assets

There are no brand assets: no logo, palette, fonts, or guidelines.
`design/assets/` and `design/wireframes/` are empty, and the web client is a
bare system-font stylesheet. Do not search for an existing MetalHops brand.
Use the design direction advisor flow.

For the logo, set "MetalHops" in the chosen display font as a placeholder
wordmark and label it "placeholder wordmark" in the showcase. The real logo
is a later task.

## Process

1. Propose three contrasting directions from three different design schools,
   each tied to a named designer or studio, with the reasoning for MetalHops.
2. Build one small demo per direction, using real MetalHops content: a venue
   card for a plausible bar (for example a Düsseldorf rock pub with darts and
   cold beer), genre tags, a 1-5 rating, and the Verified Venue badge. Show
   dark mode in each demo.
3. Stop and wait for me to pick one or mix them. Do not build the system
   before I choose.
4. Build the full showcase in the chosen direction, show it to me partway
   through, and verify it in a browser before delivering.

## Foundations

- Color: dark-first, with a light mode. Semantic names that map to Compose
  Material roles and SwiftUI, such as `surface`, `surface-raised`,
  `on-surface`, `on-surface-muted`, `primary`, `on-primary`, `accent`,
  `outline`, `success`, `warning`, `danger`, `on-danger`. Define colors in
  `oklch()`. Every text and icon pair meets WCAG AA (4.5:1 for text, 3:1 for
  large text and UI parts); list the measured ratios in the showcase.
- Type: one display face and one body face, both free to use on web and in
  apps (Google Fonts or similar licence). A type scale with named steps
  (display, title, heading, body, label, caption) and line heights. Numbers
  for ratings and counts must be tabular.
- Spacing scale, radius scale, elevation (how depth reads on a dark
  surface), border widths, and motion (durations and easing), each with
  named tokens.
- Iconography: name one open-source icon set to use. Do not draw icons.

## Components

Show every component in dark and light mode, with all its states (default,
hover, focus, pressed, disabled, and error where relevant):

- Buttons: primary, secondary, ghost, and destructive.
- Text input with label, help text, and error. Error text is generic, for
  example "Email or password is incorrect"; never reveal whether an email is
  registered.
- Venue card: name, Venue type, city and country, Genres, average rating,
  Review count, distance, and the Verified Venue badge. Include a photo
  placeholder slot and a no-photo variant.
- Rating: a read-only display of an average (for example 4.3 from 128
  Reviews) and an input that picks a whole number from 1 to 5.
- Genre tag: single tag, a wrapping group, and selected and unselected
  states for use as a filter.
- Verified Venue badge. It means a moderator accepted the listing for the
  public catalogue. It must not suggest that the owner submitted, claimed, or
  endorses the Venue, so avoid wording or icons that read as "official" or
  "owner".
- Check-in button, with a done state.
- Identity: avatar with display name and @username, in compact and full
  sizes.
- Review item: rating, commentary, author identity, date, plus a variant
  whose commentary was removed by a moderator: the rating stays and a quiet
  note says the commentary was removed.
- Empty, loading, and error states for a list of Venues.

## Deliverables

Write everything to `design/system/`:

- `index.html`: the showcase, self-contained, opening by double-click, with a
  dark and light toggle.
- `tokens.css`: CSS custom properties for both modes.
- `tokens.json`: the same tokens in a platform-neutral form for later
  Compose and SwiftUI mapping.
- `README.md`: the chosen direction, the fonts and icon set with licences,
  and how the tokens are named.

Do not change anything under `webclient/`.

## Guardrails

- No purple gradients, emoji icons, invented stats, or rounded cards with a
  coloured left border.
- Use honest image placeholders (a labelled block), not hand-drawn SVG
  imagery.
- Use the product terms above exactly: Venue, Verified Venue, Genre, Review,
  Check-in, Username, display name.
```

## Acceptance

- The prompt runs without needing any other repository file.
- The run stops after the three direction demos and waits for a pick.
- The final output lands only in `design/system/` and contains the four
  deliverables.
