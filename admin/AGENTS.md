# Admin agent guide

The moderation web app is a future stack. This folder is documentation only.
Ask before adding files here. Do not choose or scaffold a web framework.

The future app calls privileged endpoints on the Go API. Supabase product
tables and the service-role key stay off this stack. There is no separate
admin backend.

The Go API provides those privileged moderation endpoints before this stack
starts. There is no operator SQL runbook, and `docs/operations/moderation.md`
is not part of the plan.

Profile, Venue, Review, and report behavior lives in
[DevSteps](../DevSteps.md). Link to it from here.

When admin work starts, replace this stub with a real guide and add a
`CLAUDE.md` containing `@AGENTS.md`.
