# thunder-app-operator — final shape

Reconciles the `ThunderApplication` custom resource (`aep.wso2.com/v1alpha1`)
into a real OAuth (PKCE) application on the shared Platform IdP (Thunder).
Deployed cluster-wide, single replica, leader election off, watching all
namespaces — the same shape as the CNPG operator. It is the *only* component
in the platform that calls Thunder's admin REST API; the BFF never does.

The CR is rendered by the `thunder-app` `ClusterResourceType`
(`deployments/single-cluster/resource-types/thunder-app/resourcetype.yaml`),
one instance per project per `thunder-app` platform-resource dependency. See
ADR-0006 for why this exists
(`docs/decisions/ADR-0006-auth-as-platform-resource.md`). The chart that
installs this operator lives at
`deployments/single-cluster/resource-types/thunder-app/operator/helm/` (see
that chart's README for why it is an optional, PE-installed reference
implementation rather than part of the platform install).

## Reconcile loop

- **Create/update**: `EnsureApplication` (idempotent — safe to call every
  reconcile) creates or updates the Thunder OAuth app named
  `aep-<namespace>-<cr-name>`, then `ensureConfigMap` publishes the assigned
  `client_id` into a `<cr-name>-oauth` ConfigMap, owned (controller ref) by
  the CR so it's garbage-collected on delete. `status.ready` is set true only
  after both steps succeed — this is the field the ClusterResourceType's
  `readyWhen` CEL gate reads, so a consuming binding can't flip Ready before
  the client_id actually exists.
- **Delete**: an `aep.wso2.com/thunder-application` finalizer holds the CR
  until `DeleteApplication` deregisters it from Thunder; the finalizer is
  dropped only after that call succeeds, so the app is never orphaned on
  Thunder.
- **Failure**: a Thunder error is recorded on `status.message`
  (`status.ready=false`) and requeued after a fixed 30s backoff — no
  exponential backoff at this scale; Thunder outages are transient and the CR
  is cheap to re-reconcile.

## Spec fields

`displayName`, `scopes` (space-separated), `redirectUris` (comma-separated,
may be empty at creation). `redirectUris` is platform-managed: aep-api patches
it via the binding's `environmentConfigs` once the consuming SPA's public URL
resolves; the operator picks up the change on its next reconcile. Because
Thunder rejects an empty redirect URI at application-creation time, the
client (`internal/thunder/client.go`) substitutes a reserved, non-routable
placeholder (`https://pending.invalid/callback`) until a real one is patched
in.

There is deliberately no `instanceRef` (or equivalent) field — the operator
always targets the single Platform IdP. Bring-your-own Thunder instances are
out of scope; the CRD leaves room to add one additively later without a
breaking change.
