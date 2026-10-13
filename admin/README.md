# MetalHops admin

Future moderation web app. This folder is documentation only. The framework,
screens, and moderation API contract wait for a separate design cycle.

The app will call privileged endpoints on the Go API. Supabase product tables
and the service-role key stay off this stack. Until the app exists, operators
use the SQL runbook described in [DevSteps](../DevSteps.md).

Agent rules for this folder: [AGENTS.md](AGENTS.md).
