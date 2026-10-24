# An applied migration stays when the deploy fails

Once a release has applied its migrations, they stay applied. A failed deploy
hook or a failed readiness poll does not roll them back and does not call the
hook again. The process already running keeps serving. A later push to `main`
runs the release again: migrations already applied are not applied twice, and
that later commit is what gets deployed. A failed test or a failed migration
never calls the hook. The failed attempt does not retry the hook. Re-running
that job is a new run of the same commit: the migration is already applied,
and that run calls the hook. A new push is a different commit and waits its
turn. There is no manual dispatch.

Rolling the migration back was rejected. Applied migrations are forward-only,
and an automatic down step would be a second, unreviewed change to production.
Deploying before migrating was rejected too. The new process would start
against a schema it is not ready for.
