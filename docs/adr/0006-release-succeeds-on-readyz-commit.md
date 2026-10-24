# A release succeeds only when readiness returns the pushed commit

A release is successful only when the public readiness check returns 200
with status ok and the commit that was pushed. Render's health check is not
that proof. A ready response that omits the commit, or names a different
one, keeps the release waiting until it fails. Once that wait has ended in
failure, a later ready response does not make the release successful.

Trusting the health check was rejected. The process still running can be
healthy while the new one is not serving. Trusting any ready 200 was
rejected too. That response can be the previous release.
