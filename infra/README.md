# Infra

- `supabase/migrations/`: SQL schema (PostGIS, venues, genres, RLS). From the repo root, `supabase db push --workdir infra` applies these files to the linked project and records their versions. The SQL editor does not write `supabase_migrations.schema_migrations`, so it does not make `/readyz` ready. `config.toml` is the CLI project file. Do not commit `.temp` or link credentials.
- `render.yaml`: Render blueprint for the Go API. In Render, set the blueprint path to `infra/render.yaml`.
