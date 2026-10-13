# 🤘 MetalHops

**Find the best metal and rock bars around the world — where the music is loud, the beer is cold, and the food is good.**

MetalHops is an Untappd-style app for the rock and metal scene. Instead of checking in beers, you discover, rate, and rank the *places*: bars, pubs, and venues that actually play the music you love.

> **Status:** Idea / planning phase. This README describes the vision and initial scope; everything here is open for discussion.

---

## Why MetalHops?

Finding a good rock or metal bar in a new city is mostly luck. Maps apps tell you a place is "a bar" with 4.3 stars, but not whether they play death metal or Top 40. MetalHops fills that gap with community-driven, music-first reviews.

The idea came from a trip to Indústria/Düsseldorf for a System of a Down show, where a great rock bar (complete with darts) made it clear how much better trips are when you know where to go.

## Core Concept

- **Community-powered database** of rock and metal bars, pubs, and venues worldwide.
- **Self-submit model:** users can add new places, and bar owners can submit and claim their own listings.
- **Verification layer:** submissions are reviewed or verified before being fully trusted on the map.
- **Rankings and ratings** for each place, by city, region, and genre.

## Planned Features

### Discovery
- Map and list view of nearby and worldwide venues
- Search and filters by genre, vibe, beer selection, food, and distance

### Venue Profiles
- Music category: what actually plays (classic hard rock, death metal, folk metal, punk, etc.)
- Atmosphere: dedicated bar vs. club/nightlife vibe
- Extras: darts, pool tables, live music, outdoor seating
- Drinks and food: beer selection (e.g. "cold beer"), kitchen quality
- Photos and community reviews

### Community
- Check-ins and ratings, in the spirit of Untappd
- Leaderboards and rankings by city, country, and genre
- Verified-venue badge for owner-submitted and confirmed listings
- Moderation and reporting tools

### Stretch Ideas
- Augmented reality (AR) view for nearby venues, if it proves worthwhile
- Badges and achievements for exploring new places
- Event listings for gigs and themed nights

## Product Approach

1. **Supabase as the foundation:** managed Postgres + PostGIS, auth, and storage at $0 to start.
2. **Go API on Render:** custom business logic (verification, moderation, rankings) lives in a Go service, so Android and iOS share the same rules.
3. **Native Android (Kotlin) MVP:** GPS and maps are core features, so we build natively from day one.
4. **Validate:** grow the venue database and gather feedback from the scene.
5. **Native iOS (Swift) later:** reuse the same API once Android proves the concept.
6. **Optional web:** a small landing page and moderation/admin panel, only if needed.

The project is deliberately scoped to need **less constant dedication**, making it a good fit for a small team contributing part-time.

## Architecture

```
┌──────────────┐   ┌──────────────┐
│ Android app  │   │   iOS app    │   (later)
│ Kotlin       │   │ Swift        │
└──────┬───────┘   └──────┬───────┘
       └────────┬─────────┘
                ▼
     Go REST API (Render)
                ▼
  Supabase: PostgreSQL + PostGIS
        Auth · Storage
```

The apps authenticate with Supabase Auth and send the resulting JWT to the Go API, which verifies it. Simple reads can go straight to Supabase (protected by row-level security); anything with business rules goes through the Go API.

## Technical Direction

We lean on existing open-source tools and study the *concepts* behind Untappd (check-ins, ratings, feeds) and geolocation apps (map discovery, nearby search).

### Backend API: Go

**Decision: Go**, hosted on [Render](https://render.com/), with Supabase as the data layer.

- Compiles to a single static binary and a tiny Docker image, so it's easy to ship.
- Testing is built in (`go test`), with fast builds and CI.
- Small language and enforced formatting (`gofmt`) keep the codebase clean.
- Handles high concurrency well if the app grows.

Suggested libraries and tooling:

| Area | Choice |
| --- | --- |
| Router | Standard library `net/http` (Go 1.22+) or `chi` |
| Database access | `pgx` + `sqlc` (type-safe queries) |
| Migrations | `goose` or Supabase migrations |
| API contract | OpenAPI, with generated Kotlin and Swift clients |
| Auth | Verify Supabase JWTs in middleware |
| Integration tests | `testcontainers-go` against real Postgres/PostGIS |

### Database and Auth: Supabase

- Managed PostgreSQL with PostGIS for "bars near me" and "within X km" queries.
- Supabase Auth for email and social login (Google/Apple).
- Supabase Storage for venue photos.
- Row-level security (RLS) policies on every table, tested explicitly.

Free-tier caveats to watch (verify on Supabase's pricing page):

- Database size is capped at around 500 MB.
- Projects pause after about a week of inactivity, which matters during beta.
- Use the connection pooler address from the Go service.

### Hosting: Render

- Deploy the Go API as a Docker web service, straight from GitHub.
- The free tier sleeps after about 15 minutes of inactivity, so the first request is slow. Fine for early development; plan to move to a paid always-on instance before a public beta.
- Choose a Render region close to the Supabase region to keep query latency low.
- Keep secrets (database URL, Supabase service-role key) in Render environment variables, never in the repo or the app.
- Fallback: because the API is a stateless container, it can also run on a home server behind a Cloudflare Tunnel, or on Google Cloud Run, with only configuration changes.

### Mobile

| Platform | Stack |
| --- | --- |
| Android (first) | Kotlin, Jetpack Compose, Retrofit/Ktor client, Hilt, Room |
| iOS (later) | Swift, SwiftUI, URLSession/async-await |
| Maps and GPS | Google Maps SDK or MapLibre (open source); Fused Location Provider on Android, Core Location on iOS |

### Supporting Services

- **Venue data seeding:** OpenStreetMap / Overpass API to bootstrap the database
- **CI/CD:** GitHub Actions (test, lint, build; Render auto-deploys from `main`)
- **Backups:** scheduled `pg_dump` to storage we control, since free-tier backups are limited

## Roadmap

- [ ] Business case and scope discussion
- [ ] Detailed planning and feature prioritization
- [ ] Data model: venues, reviews, genres, users
- [ ] Supabase project: schema, PostGIS, auth, RLS policies
- [ ] Repo setup, tooling, and CI (monorepo: `backend/` (Go), `android/`, later `ios/`)
- [ ] Go API on Render: JWT verification, venues, nearby search, reviews
- [ ] Seed venue data from OpenStreetMap
- [ ] Android MVP: map, venue pages, submissions, ratings
- [ ] Verification and moderation flow
- [ ] Android beta (Google Play internal/closed testing)
- [ ] iOS app (Swift / SwiftUI)

## Repository Structure

| Folder | Purpose |
|---|---|
| `android/` | Android app (Kotlin) |
| `ios/` | iOS app (future) |
| `backend/` | Go API |
| `docs/` | Product, architecture and API docs |
| `design/` | Wireframes and assets |
| `infra/` | Supabase migrations and Render config |
| `scripts/` | Dev and CI helper scripts |

## Branching

- `main`: stable, release-ready
- `dev`: integration branch; feature branches (`feature/*`) merge here via PR

## Contributing

This is a collaborative project between friends, with Claude as a coding partner. Once the repository is set up, work happens on `feature/*` branches and merges into `dev` through pull requests.

## License

To be decided.

---

*Keep it loud. 🤘🍺*
