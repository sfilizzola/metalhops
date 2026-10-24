# Infra

- `supabase/migrations/`: SQL schema (PostGIS, venues, genres, RLS). From the repo root, `supabase db push --workdir infra` applies these files to the linked project and records their versions. The SQL editor does not write `supabase_migrations.schema_migrations`, so it does not make `/readyz` ready. `config.toml` is the CLI project file. Do not commit `.temp` or link credentials.
- `render.yaml`: Render blueprint for the Go API. In Render, set the blueprint path to `infra/render.yaml`.

## Release

A push to `main` runs `.github/workflows/release.yml`, the only deployer. It
tests the backend, applies migrations with `supabase db push`, calls the
Render deploy hook, and succeeds only when `/readyz` reports the pushed
commit.

One-time setup for the later promotion:

- Create the GitHub environment `production`, limited to branch `main`, with
  no required reviewer.
- Add the environment secrets `SUPABASE_DB_URL` (the session-pooler URL, port
  5432) and `RENDER_DEPLOY_HOOK_URL` (copied from the Render service's
  Settings page), and the environment variable `API_BASE_URL` (the public
  `https` origin, no path, no trailing slash).
- Sync the blueprint so the service's `autoDeployTrigger` is off.

Render's own `/healthz` success is not the release proof; only the
workflow's `/readyz` poll is.
