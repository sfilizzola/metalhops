# Session 4 — what this implements

Session 4 makes MetalHops safer before product APIs exist.

Today, Supabase’s Data API can still reach catalogue tables (`genres`,
`venues`, `venue_genres`) through roles like `anon` and `authenticated`.
This session closes that path with migration `0002`. Those roles lose table
and sequence privileges. Product data is meant to go through the Go API only.
The database owner (the Render / pooler `DATABASE_URL` user) still can read
and write; the public Data API roles cannot.

The same migration adds an `idempotency_records` table. The Go package
`internal/idempotency` provides `Execute`: a helper that later authenticated
`POST` routes will call inside a transaction. If a client retries the same
key with the same command, they get the stored success response. If they
reuse the key with a different command, they get a conflict. Expired rows
are cleaned in small batches. Concurrent retries for the same key are
serialized with an advisory lock. Session 4 does **not** add HTTP routes or
`Idempotency-Key` parsing yet — only the helper and storage.

Public traffic gets a simple per-IP rate limit: burst of 120 requests, then
about 2 per second. Health (`/healthz`) and readiness (`/readyz`) stay
unlimited so probes keep working. Client IP comes from the TCP peer, or from
`X-Forwarded-For` when the peer is in `TRUSTED_PROXIES` (empty locally;
pinned Cloudflare ranges plus `10.0.0.0/8` on Render).

Readiness also moves forward: the API expects migration `0002` recorded
before `/readyz` returns OK. Applying `0002` on the hosted project is an
operator step (`supabase db push`); the implementer does not push it.

**Out of scope here:** JWT auth, `/v1` product routes, OpenAPI changes for
idempotency headers, and the later per-user write rate limit.
