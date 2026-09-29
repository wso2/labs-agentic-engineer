# Platform chart — authorization objects

The templates are in `platform/templates/authz/`.

Every authorization object the platform installs into OpenChoreo, in one place.

OpenChoreo authorizes API calls against its own `AuthzRole` /
`AuthzRoleBinding` CRs — it does **not** consult Thunder role assignments.
Thunder decides who holds an AE permission; these decide what OpenChoreo lets
the platform do while acting on that person's behalf. Both halves have to
exist or a caller who is correctly entitled in Thunder still meets a 403 from
OpenChoreo.

| File | Scope | What it authorizes |
|---|---|---|
| `aep-api-client.yaml` | cluster | aep-api's own service identity, for the calls it makes as itself (coding-agent dispatch pre-flight, webhooks) |
| `ae-roles.yaml` | the org's namespace | the `ae-admin` / `ae-developer` roles a signed-in person's calls run under |

## Why the roles are installed rather than provisioned at runtime

They used to be created by `GET /authz/ensure`, which the console called as the
first step of onboarding. That made an authorization boundary depend on
somebody having signed in and reached a wizard — the org was unusable until
they did, the failure surfaced as an opaque 403 several systems away, and the
onboarding gate needed a permission-free carve-out of its own to run at all.

Installing them removes the ordering problem entirely: the roles exist before
anyone signs in, because they arrive with the platform.

## Keeping `spec.actions` honest

`ae-roles.yaml` spells out the OpenChoreo actions each role holds. Those are
derived in Go — `internal/authz`'s `OcActionCatalog` maps each `ae:*`
permission to the OC actions it implies, and a role's action list is the union
over the permissions it holds (`aeperms`).

YAML cannot import that, so the two could drift, and the failure would be
silent: a missing action is a 403 from OpenChoreo on an operation the AE gate
already allowed, which reads as a platform bug rather than a stale list.
`TestChartAuthzRolesMatchCatalog` (`internal/authz`) parses that directory and
fails when they disagree. Edit the Go catalog, run that test, and it tells you
exactly what to write here.
