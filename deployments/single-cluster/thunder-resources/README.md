# AEP's bootstrap bundle for the platform IdP

Declarative ThunderID resource documents for the OAuth clients and roles AEP
needs. `scripts/setup-thunder.sh` merges them with Agent Manager's own bundle
into one ConfigMap and installs the platform IdP pointing
`thunder.bootstrap.configMap` at the merged result.

The IdP itself is neither product's — it is `platform-idp`, shared, named for
what it is. Each product is a **publisher** into it, owning a bundle of
documents it alone edits. This directory is AEP's bundle. See
`docs/decisions/ADR-0028-the-platform-idp-is-neutral-infrastructure.md`.

## The publisher contract

Stated in full in `deployments/design/two-tier-thunder.md`; it applies at both
identity tiers and this directory is one bundle under it. Locally it comes to
three things: Agent Manager occupies prefixes 50-73 so AEP's files start at 80
(a collision between the two bundles is a hard error in `setup-thunder.sh`, not
a last-write-wins); no document here redeclares anything Agent Manager declares,
because a redeclaration replaces it silently; and any client here that asks for
the `system` scope must send the System resource server as its OAuth `resource`
indicator, or the scope is dropped and every admin call afterwards 403s.

**Three files here are not AEP documents.** `89-platform-cors-config.yaml`,
`90-platform-default-resource-server.yaml` and `91-platform-csp.yaml` are the
platform's composed `server_config` singletons: `setup-thunder.sh` writes their
values at install time, and their numbers must keep sorting after Agent
Manager's or the composition is the thing that gets overwritten. Each file's
header says what it composes; `verify-convergence.sh` check 13 asserts it.

## Why a merge, and not a second bootstrap source

`wso2-amp-thunder-extension` pins `thunder.bootstrap.configMap.name` to its own
chart-owned ConfigMap, and ThunderID's `setup-job.yaml` **fails the render** if
both `bootstrap.scripts` and `bootstrap.configMap` are set. So AEP cannot bolt
its documents on as a second source — there is exactly one bootstrap channel and
it is already taken.

`declarativeResources` is a separate channel, but it is not equivalent: it makes
the mounted files the *store* for a resource type, which would take ownership of
`application` away from the setup job that creates Agent Manager's clients.

Merging is the honest option. `setup-thunder.sh` runs `helm template` against
the pinned `AMP_VERSION`, lifts Agent Manager's rendered documents out of it,
adds the files in this directory, composes the singletons, rewrites the one
value Agent Manager's chart does not template (its System resource server's
identifier, which is derived from a public URL), and applies the union. Agent
Manager's half therefore tracks its chart automatically — bumping `AMP_VERSION`
needs no edit here.

## Format

Each file is one document with a `resource_type` header. The shapes mirror
Agent Manager's own documents — see `wso2-amp-thunder-extension`'s
`templates/amp-thunder-bootstrap.yaml` for the reference set.

`ouId: "01900000-0000-7000-8000-000000000001"` is ThunderID's built-in `default`
organization unit, fixed on every install. Applications must use `ouId` and not
`ouHandle`: the importer resolves the handle for roles, groups and users, but
NOT for applications, and an application bootstrapped with `ouHandle` ends up
with no OU at all — its client_credentials tokens then carry no `ouId`/`ouHandle`
claim.

`attributes: ["ouId", "ouHandle"]` under `token.accessToken.clientConfig` is
separately required: ThunderID only embeds those as token claims when the client
opts in. Without it a correct `ouId` above still yields tokens that OpenChoreo
rejects.

`${PUBLIC_CONSOLE_URL}`, `${PUBLIC_THUNDER_URL}` and `${PUBLIC_THUNDER_HOST}`
are substituted by `setup-thunder.sh` before the merge, so a document may list
both a fixed `http://localhost:...` form and the placeholder — the installer
de-duplicates them when they collapse onto each other, which is the common case.
