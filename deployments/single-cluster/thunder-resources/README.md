# AEP's Thunder bootstrap resources

Declarative ThunderID resource documents for the OAuth clients AEP needs. They
are merged with Agent Manager's own bootstrap documents into one ConfigMap by
`scripts/setup-thunder.sh`, which then installs `wso2-amp-thunder-extension`
pointing `thunder.bootstrap.configMap` at the merged result.

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
adds the files in this directory, and applies the union. Agent Manager's half
therefore tracks its chart automatically — bumping `AMP_VERSION` needs no edit
here.

## File naming

Numeric prefixes order the import; ThunderID applies them in lexical order.
Agent Manager occupies 50-73, so AEP's files start at 80 to land after
everything its clients might depend on (the resource server, the roles, the
built-in Administrator role).

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
