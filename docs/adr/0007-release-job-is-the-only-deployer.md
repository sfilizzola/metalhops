# The release job is the only deployer

A push to `main` deploys only through the release job. Render does not deploy
from that push. Auto-deploy stays off, and a dashboard deploy is not a
release path. The blueprint records auto-deploy off. The live service is
switched off when `dev` is promoted.

Letting Render deploy from the push was rejected. That build can start
before migrations are applied, and it never waits for the readiness proof.
A dashboard deploy was rejected for the same reason.
