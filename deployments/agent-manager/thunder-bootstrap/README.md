# The shared ThunderID bootstrap documents

AEP and Agent Manager share one identity provider (ADR-0027), and both need
OAuth clients, roles and groups inside it. This directory holds **Agent
Manager's half**, frozen, plus the documents that settle what the two products
declare in common.

`setup-env-for-aectl.sh` copies every `*.yaml` here into the bootstrap folder
alongside AEP's own documents (its step 3d), **before** ThunderID is installed.
36 files, 43 documents, one install.

## Why they are published before the install, not after

ThunderID reads its bootstrap folder exactly once, from the chart's
`pre-install` setup Job. The running server mounts no bootstrap volume at all,
so a document added later is never read and restarting the pod re-imports
nothing. Whoever installs second cannot add anything; both halves have to be
present before the IdP exists.

## Why they are frozen rather than rendered

Agent Manager's documents come from its own chart, and rendering them at install
time would make AEP's installer depend on an Agent Manager chart even on a
cluster that never runs Agent Manager.

The cost is that they go stale when Agent Manager releases. Re-derive on a
version bump — `thunder.enabled=false` makes the chart a publisher into an IdP
it does not install:

```bash
helm template amp-thunder oci://ghcr.io/wso2/wso2-amp-thunder-extension \
  --version <AMP_VERSION> -f ../thunder-extension-values.yaml
```

Frozen from **wso2-amp-thunder-extension 1.0.0-rc2**. Re-derive, re-check the
table below, and re-run the duplicate scan before trusting a newer version.

## What the two products declare in common

ThunderID creates applications **by name** and aborts the entire import on the
first duplicate — one unresolved pair does not degrade the install, it stops it,
and no other document lands. Sharing an `id` is fine (that upserts); sharing
only a `name` is fatal.

Every conflict is resolved here, in the frozen set:

| Conflict | Resolution |
|---|---|
| application `Workload Publisher` | keep AEP's `87-workload-publisher-app.yaml`; drop `54-workload-publisher-app.yaml` |
| application `OpenChoreo Observer Resource Reader` | keep AEP's `88-observer-app.yaml`; drop `55-observer-resource-reader-app.yaml` |
| role `Administrator` | `99-composed-administrator-role.yaml` carries **both** assignments; drop `68-amp-role-system-client-thunder-admin.yaml` and AEP's `87-ae-install-client-admin-role.yaml` |
| resource_server `System` | `99-composed-system-resource-server.yaml`; drop `70-fix-thunder-system-rs-identifier.yaml` and AEP's `71-fix-system-resource-server-identifier.yaml` |
| server_config `cors` | `99-composed-cors.yaml` (union of both origin lists); drop `71-amp-cors-config.yaml` and AEP's `95-cors.yaml` |
| server_config `csp` | `99-composed-csp.yaml`; drop `73-amp-csp-config.yaml` |
| `69-amp-default-resource-server-config.yaml` | **kept** — AEP publishes no competing document, and both products send an explicit RFC 8707 resource indicator rather than relying on the server-wide default |
| everything else Agent Manager ships | kept as-is — users, groups, roles and applications are additive, not singletons |

The two dropped applications are the same OAuth client as AEP's: identical
`clientId`, `clientSecret`, `grantTypes`, `ouId` and `name`. Agent Manager's
copies differ only by carrying its full `amp:*` scope list, which its chart
stamps onto six applications from one shared `ampScopes` value — boilerplate
rather than a per-client grant. AEP's versions are also the ones `aectl`'s
`EnsureApplication` maintains by `clientId`, so keeping them avoids two
mechanisms disagreeing about the same client.

The `99-` prefix on the composed documents is deliberate: they are imported
last, so a renumber on either side cannot take a singleton back.

## Checking a change

After editing anything here, confirm no two documents share a `resource_type` +
`name` or `resource_type` + `id`. A duplicate does not surface as a bad value —
it surfaces as an IdP with none of Agent Manager's configuration and an
`APP-1020` buried in a Job that has already deleted itself.

## When this directory should go away

Each product should assert its own identity configuration through ThunderID's
admin API, the way `aectl` already registers AEP's OAuth clients
(`tools/aectl/internal/thunder`, `EnsureApplication`). The API creates by
`resource_type`'s endpoint with the document as the payload, returns a `400` for
one conflicting object instead of aborting everything, and needs no reinstall.
This directory is the interim: one list, one install, no API calls — until
Agent Manager exposes a registration path of its own.
