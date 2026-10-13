# MetalHops API (Go)

REST API for MetalHops. Hosted on Render, backed by Supabase (Postgres + PostGIS).

```
cp .env.example .env
make run     # http://localhost:8080/docs
make test
```

Layout: `cmd/api` is the entrypoint; add domain code under `internal/`.
