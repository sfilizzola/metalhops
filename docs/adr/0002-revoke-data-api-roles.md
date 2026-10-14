# Revoke Data API roles on every product table

Supabase can grant `anon`, `authenticated`, and `service_role` on a new table
through default privileges owned by another role. A migration cannot reliably
clear those defaults. Every migration that creates a product table revokes
`PUBLIC`, `anon`, `authenticated`, and `service_role` on that table in the
same file, after creating it. A sequence created in that file gets the same
revoke. A table that uses `gen_random_uuid()` adds no sequence revoke.
Migration `0002` does this for the catalogue tables, `genres_id_seq`, and
`idempotency_records`, and it does not try to clear the defaults. Author-only
product reads go through the Go API. The Render `DATABASE_URL` user owns
these tables, and no second API role is granted. This supersedes the allowance
in ADR 0001 for authenticated author-only Data API reads.
