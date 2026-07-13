# Security policy

Please report vulnerabilities privately through GitHub Security Advisories for
this repository. Do not open a public issue containing credentials, exploits,
or private user data.

Only the latest tagged release receives security fixes. For an internet-facing
deployment, run ttl behind TLS, set `TTL_SECURE_COOKIES=1`, keep open signup
disabled, restrict `TTL_ALLOWED_ORIGINS`, use scoped expiring agent keys, and
back up the SQLite database and data directory together.

Reminder webhook signing secrets must be recoverable by the server and are
stored in the SQLite database. Protect backups and the data directory as
credentials. Outbound reminder webhooks reject private, loopback, link-local,
and unspecified targets by default; trusted internal deployments may opt in
with `TTL_ALLOW_PRIVATE_WEBHOOKS=true`.
