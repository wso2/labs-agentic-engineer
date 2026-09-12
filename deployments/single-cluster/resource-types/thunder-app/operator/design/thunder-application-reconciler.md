# thunder-app-operator — final shape

Reconciles the `ThunderApplication` custom resource (`aep.wso2.com/v1alpha1`)
into a real OAuth (PKCE) application on **the Thunder that serves the CR's
(organization, environment)** — the environment identity tier, one instance per
(org, env), not the cluster-wide platform IdP. Deployed cluster-wide, single
replica, leader election off, watching all namespaces — the same shape as the
CNPG operator. It is the *only* component in the platform that calls Thunder's
admin REST API for these applications; the BFF never does.

The CR is rendered by the `thunder-app` `ClusterResourceType`
(`deployments/single-cluster/resource-types/thunder-app/resourcetype.yaml`),
one instance per project per `thunder-app` platform-resource dependency. See
ADR-0006 for why this exists
(`docs/decisions/ADR-0006-auth-as-platform-resource.md`). The chart that
installs this operator lives at
`deployments/single-cluster/resource-types/thunder-app/operator/helm/` (see
that chart's README for why it is an optional, PE-installed reference
implementation rather than part of the platform install).

## Resolving the target

The operator holds no Thunder of its own — no address, no credential, no
default. Each pass resolves the instance for the CR from the **binding record**
that `deployments/scripts/setup-environment-thunder.sh <org> <env>` writes for
every environment that has a Thunder (`internal/controller/binding.go`):

1. The CR's `openchoreo.dev/namespace` (the organization) and
   `openchoreo.dev/environment` labels give the coordinates. OpenChoreo's
   renderedrelease-controller stamps both on every object it renders, so the
   ClusterResourceType template does not have to carry them.
2. Those coordinates select **one ConfigMap** cluster-wide, by the labels
   `aep.wso2.com/kind=thunder-binding`, `aep.wso2.com/org`, `aep.wso2.com/env`
   — never by name. It carries `issuer`, `adminURL`,
   `systemResourceIdentifier`, and the name/namespace of the Secret half.
3. That Secret (`client-id`, `client-secret`) is read from the operator's OWN
   namespace. The Secret informer and the SA's Role are namespace-scoped by
   design, which is exactly why the environment script MIRRORS the credential
   there. A binding that names any other namespace is refused with that reason,
   rather than reported as a Secret that does not exist.

Zero matches is not an error condition of the operator: the CR reports
`ready=false` with a message naming the labels it looked for and the script
that writes them, and requeues. Two matches is an ambiguity the operator
refuses to guess at.

### Client cache

One `thunder.AdminClient` per (org, environment), kept because it caches the
`system` access token it mints. Invalidation is **by value**: the resolved
target (address, resource indicator, credential — a comparable struct) is
compared with the cached one, so a rotated credential or a re-pointed
`adminURL` replaces the client on the next pass. The watches on binding
ConfigMaps and Secrets only make that pass happen promptly — they re-enqueue
every CR of the changed (org, environment); correctness does not depend on an
event arriving.

## Reconcile loop

- **Create/update**: `EnsureApplication` (idempotent — safe to call every
  reconcile) creates or updates the OAuth app named `aep-<namespace>-<cr-name>`
  on the resolved instance, then `ensureConfigMap` publishes the assigned
  `client_id` plus that instance's `issuer` and `jwks_url` into a
  `<cr-name>-oauth` ConfigMap, owned (controller ref) by the CR so it's
  garbage-collected on delete. The same three coordinates land on
  `status.clientId` / `status.issuer` / `status.jwksUrl`, which is where the
  ClusterResourceType's `issuer` and `jwks_url` outputs read them from — a
  consumer learns WHERE its app lives instead of assuming one IdP.
  `status.adminURL` records the instance the app was created on, for the delete
  path below. `status.ready` is set true only after both steps succeed. The
  app's browser origin is registered with the instance in the same pass — see
  *Browser origins* below.
- **Delete**: an `aep.wso2.com/thunder-application` finalizer holds the CR until
  `DeleteApplication` deregisters it. Because an application can only be removed
  from the instance it was created on, and the credential for any instance lives
  only in that instance's binding Secret, the finalizer is released without a
  delete in exactly three cases, each logged at error level with the orphaned
  `client_id` and issuer: the (org, environment) has **no** binding ConfigMap at
  all (the environment's Thunder was removed together with its record, so there
  is nothing left to delete from and no credential that could ever reach it);
  the binding now names a **different** instance than `status.adminURL`; or the
  CR carries **no (org, environment) labels**, so nothing could be looked up in
  the first place. The last one is reported separately (`errNoCoordinates`,
  wrapped alongside `errNoBinding` so every other caller is unaffected): an
  unlabelled CR is not evidence that an environment lost its Thunder, and
  saying so would send the reader hunting a removed instance instead of the
  missing labels.

  A binding whose ConfigMap is present but whose Secret is missing is repairable —
  re-run the environment script — so that case keeps the finalizer and retries.
  Holding on regardless would wedge the CR and its namespace forever. Once the
  app is deregistered its browser origin is withdrawn, best-effort.
- **Failure**: an error is recorded on `status.message` (`status.ready=false`)
  and requeued after a fixed 30s backoff — no exponential backoff at this scale;
  Thunder outages are transient and the CR is cheap to re-reconcile. The status
  write is an `Update`, not a merge `Patch`: `ready` is a required property of
  the status schema, and a merge patch of a status that has never been written
  omits it, which the API server rejects.

## Browser origins (CORS)

The app an environment Thunder is provisioned for is a **browser** client of
that Thunder. Its first call is a cross-origin `fetch` of
`/.well-known/openid-configuration`, and the code exchange that follows is a
cross-origin `POST`. Without `Access-Control-Allow-Origin` on those responses
the browser discards them, so the SPA hangs on its loading screen while `curl`
against the identical URL returns a healthy 200. The only place the app's origin
appears anywhere in the platform is `spec.redirectUris` on this CR, so
registering it is the operator's job.

CORS on ThunderID is one server-wide setting with two layers, readable and
writable at `/server-config/cors`:

| layer | written by | holds |
|---|---|---|
| `readOnly` | bootstrap documents at install | the platform's own origins (T1: `thunder-resources/89-platform-cors-config.yaml`; T2: nothing) |
| `writable` | `PUT /server-config/cors` | **this operator's** projection of the CRs |
| `merged` | derived | the union, and what the server enforces |

That split is the safety argument for touching CORS at runtime: writing the
writable layer cannot clobber the platform-composed `cors` singleton, whose
redeclaration would replace the entire allow-list (the hazard
`deployments/scripts/verify-convergence.sh` check 13 exists to catch).

On every reconcile — create **and** delete — the operator lists every
ThunderApplication carrying the same `openchoreo.dev/namespace` +
`openchoreo.dev/environment` labels, reduces their redirect URIs to origins
(scheme + host + port; anything a browser could not send an `Origin` for is
dropped), and makes that set the writable layer. A projection rather than an
append is what makes deletes and drift converge with no bookkeeping of the
operator's own: a manual edit, a restored database or a re-imported bundle is
corrected on the next pass. The consequence is that the writable layer **belongs
to this operator** on every instance it manages; a publisher needing a permanent
origin declares it in a bootstrap document, which is where the platform's own
origins already live. An unchanged set is compared and skipped, so a steady-state
loop issues no writes.

A failure to register origins is deliberately **not** fatal to `status.ready`.
The OAuth app exists and every consumer of the CR's outputs can proceed; what is
missing is only the browser's permission to call the IdP. Failing the CR would
take a whole deployment down for a transient IdP hiccup, so the reason lands on
`status.message` and the CR requeues on the normal backoff. On the delete path
the withdrawal is best-effort for the same reason inverted: a stale entry in an
allow-list is not worth an undeletable namespace, so the finalizer is released
and the error is logged.

This is the near-term answer. The longer-term one is to stop the browser talking
to the IdP at all: terminate OIDC at the gateway the app already sits behind, so
the app's origin never makes a cross-origin call. See
https://github.com/wso2/labs-agentic-engineer/issues/724 — when that lands, this
projection can be retired.

## Spec fields

`displayName`, `scopes` (space-separated), `validityPeriod` (seconds),
`redirectUris` (comma-separated, may be empty at creation). `redirectUris` is
platform-managed: aep-api patches it via the binding's `environmentConfigs`
once the consuming SPA's public URL resolves; the operator picks up the change
on its next reconcile. Because Thunder rejects an empty redirect URI at
application-creation time, the client (`internal/thunder/client.go`)
substitutes a reserved, non-routable placeholder
(`https://pending.invalid/callback`) until a real one is patched in.

`scopes` is written through to the registered application's
`inboundAuthConfig[oauth2].config.scopes`. The CR carries it space-joined
(that is the ClusterResourceType parameter's shape) and ThunderID's contract
types the field as a JSON array, so the reconciler splits it.

**The allowlist is truthfulness, not enforcement.** Measured on ThunderID
1.0.0: the field is stored and read back faithfully and has no effect on the
authorization-code flow — narrowing it does not narrow the issued token, and an
unknown or ungranted handle is dropped silently rather than refused with
`invalid_scope`. The only thing that narrows an end-user token is group → role
→ permissions, intersected with the resource server the request's `resource`
indicator names. The write exists so the registered application describes its
client honestly (and keeps working if ThunderID ever starts enforcing it); it
is not a second control point and must not be described as one. The one place
the field is load-bearing today is `system` on an m2m client, which is granted
through this list rather than through a role.

A CR that names no scopes is silent, not a statement that the client may
request nothing: the stored allowlist is left alone rather than narrowed.

`validityPeriod` is the ACCESS token lifetime. Unset leaves the operator's 24h
default — which is what every project wants, and which aep-api therefore never
overrides. It exists so a fixture app can be patched down to a few minutes and
exercise the silent renew without waiting out a day; the ID token keeps the
long default either way, because shortening the SPA's session is not the point.

Whatever is written is read back: `verifyWritten` GETs the application once
after every create and update and diffs the STORED object — never the body that
was sent. That is the only defence available on this API, which answers 200 to a
payload it only partly recognises and keeps the rest to itself. The scope diff
is a subset check rather than an equality one, because ThunderID normalises on
write (`scopeClaims.email` sent as `["email","email_verified"]` comes back as
`["email"]`); a read-back carrying more than was sent is the server having an
opinion, while a scope going missing is a silent misregistration.

There is deliberately no `instanceRef` (or equivalent) field. The target is not
a property of the application — it is a property of the (org, environment) the
application is rendered into, and the environment's binding record is the one
place that says which instance that is. Bring-your-own Thunder is out of scope;
the CRD leaves room to add such a field additively later.

## Nudging the RenderedRelease

The owning ResourceReleaseBinding resolves its `issuer` and `jwks_url` outputs
from `applied.app.status.*`, and OpenChoreo evaluates that CEL against the
**status snapshot** its RenderedRelease controller records in
`RenderedRelease.status.resources[]` when it applies the manifests — not against
the live CR. That controller does not watch what it applied; it re-observes
only on its own stable requeue (about six minutes on OpenChoreo 1.2). The apply
and the operator's status write race, so the snapshot is normally taken before
`status.issuer` exists and the binding reports `OutputResolutionFailed` until
the requeue (measured live: Ready flipped exactly six minutes after the CR was
ready).

So when the operator sets status it patches an annotation
(`aep.wso2.com/thunder-ready-nudge`, valued with the client_id so repeat passes
are no-ops) on the **RenderedRelease** that applied the app, found by the
`openchoreo.dev/rendered-release-name` / `-namespace` labels
renderedrelease-controller stamps on every object it applies. That reconcile
re-reads the CR and records its status; the binding, which Owns the
RenderedRelease, re-reconciles off that status update and resolves its outputs.

Nudging the binding itself — the first version of this — cannot work: it only
re-evaluates the same stale snapshot.
