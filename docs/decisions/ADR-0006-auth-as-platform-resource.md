# ADR-0006 — End-user auth is an explicit `thunder-app` platform resource, provisioned by an in-repo operator

Generated apps needed end-user sign-in, and the source design carried this
implicitly: a `callerIdentity` field on a component silently declared "an
end-user reaches this API," and the BFF provisioned the backing Thunder OAuth
application itself — an imperative REST call sitting inside request handling,
with admin Thunder credentials reachable from the application plane. Nothing
forced the architect (or a human editor) to make the sign-in requirement
visible as a dependency the user reviews and approves, and nothing else in the
platform provisions infrastructure this way: every other piece of
per-project infrastructure (starting with `postgres-cnpg`) is a
`platform-resource` dependency rendered to an OpenChoreo CR and reconciled by
a cluster-wide operator, never called directly from the BFF.

## Decision

End-user auth is now an explicit dependency, not an implicit flag. A `service`
or `web-app` component that needs it declares
`{kind: platform-resource, resourceType: thunder-app}` — the architect
proposes this when the spec implies sign-in; the user approves and
provisions it in the console drawer exactly like a database. The SPA and the
service whose API it protects declare the **same dependency name**, so both
sides resolve to the one OAuth application.

The `thunder-app` `ClusterResourceType`
(`deployments/single-cluster/resource-types/thunder-app/resourcetype.yaml`)
renders a namespaced `ThunderApplication` CR (`aep.wso2.com/v1alpha1`). A new
in-repo operator, `deployments/single-cluster/resource-types/thunder-app/operator/`
(Helm chart at
`deployments/single-cluster/resource-types/thunder-app/operator/helm/`), deployed cluster-wide — the same
shape as the CNPG operator — reconciles it: it creates a public PKCE OAuth
application against the shared Platform IdP's admin REST API, publishes the
assigned `client_id` to a `<cr-name>-oauth` ConfigMap and to
`status.clientId` (the authoritative source of the `client_id` binding
output), and sets `status.ready` once registration succeeds. The ClusterResourceType's
`readyWhen` CEL gate reads that status field directly (`getUnknownResourceHealth`
reports `Healthy` unconditionally for a foreign CRD, so without this explicit
gate the binding would flip ready before the client_id exists). On delete, a
finalizer on the CR deregisters the application from Thunder before the
object is removed. The BFF makes **no Thunder API calls** for user
applications any more — the same type-agnostic resource-provisioning pipeline
that already handles `postgres-cnpg` handles `thunder-app`, unmodified.

`exposesAPI.auth: end-user-required` is derived by the platform, not authored:
at design save (before the tag-cut), any `service` component that declares a
dependency on a resource type carrying the `aep.wso2.com/role: end-user-auth`
marker — `thunder-app` is the first and, at the time of writing, only such
type — gets this value stamped automatically
(`services/aep-api/internal/feature/design/derive_auth.go`; see ADR-0007 for
why this keys on the marker rather than the `thunder-app` name itself). An
explicit `exposesAPI.auth: service-required` on such a component is a
self-contradiction — the dependency says the API sits behind end-user
sign-in, the flag says it never does — and is rejected as a validation error
rather than silently overridden or silently allowed.

SPAs receive OAuth coordinates via `window._env_`, sourced generically from the
dependency binding's outputs (see ADR-0007 for the generic `<DEP>_<OUTPUT>`
key contract that replaced the original fixed `THUNDER_*` names). Redirect
URIs are platform-managed, not authored: aep-api patches the binding's
`environmentConfigs.redirectUris` with the SPA's `/callback` URL once its
public URL resolves, declaratively — the operator picks up the change on its
next reconcile, the same cascade-on-deploy path other resource outputs use.
The SPA itself computes `redirect_uri = window.location.origin + '/callback'`
in the browser; the platform's role is limited to getting that origin
registered on the binding (ADR-0007's consumer-URL-patch marker).

Token verification for backends is unchanged by this decision: the API
gateway validates JWTs against the Platform IdP's JWKS and injects
`X-User-Id`; services never verify tokens themselves.

## Alternatives considered

- **Imperative provisioning from the BFF, via the documented
  `ResourceProvisioner` seam.** Rejected — the whole point of this change was
  to get *out* of the business of the BFF calling Thunder directly; the user
  explicitly wanted pure OC-CR provisioning, no API calls from the BFF.
- **Crossplane `provider-http`.** Rejected — a heavy runtime dependency to
  make one REST call, and a weak domain model (an HTTP request/response pair
  is not a meaningful representation of "an OAuth application exists").
- **A Kubernetes `Job` in the resource template.** Rejected — a Job has no
  delete-time lifecycle to hook a deregistration call into, and it would
  require admin Thunder credentials to be reachable from data-plane
  namespaces, widening the credential blast radius the operator pattern
  otherwise avoids.
- **A dedicated Thunder instance per project.** Rejected on multiple counts:
  no operator exists to run one, its bootstrap isn't CR-expressible, a
  per-project issuer breaks the single shared keymanager-gateway trust chain
  the gateway relies on, readiness would be far slower, and a fresh instance
  starts with an empty user store — pointless for an IdP whose entire value
  is a shared user base.
- **Keeping the implicit `callerIdentity` field.** Rejected — the user
  required an explicit opt-in: an app must *explicitly say* it needs
  authentication, not have it inferred silently from spec text.

Bring-your-own Thunder instances remain **out of scope**. The CRD
deliberately leaves room for a future `instanceRef` field, but nothing wires
it up today — the operator only ever targets the one Platform IdP.

## Consequences

- **+** Exact structural symmetry with `postgres-cnpg`: same cluster-wide
  reconciler shape, same per-project `Resource`/`ResourceReleaseBinding`
  wiring, same type-agnostic BFF pipeline. Adding `thunder-app` required zero
  changes to the resource-provisioning pipeline itself.
- **+** The BFF no longer holds or uses Thunder admin credentials; the
  operator is the only component that talks to Thunder's admin API.
- **+** A single shared Platform IdP trust chain is preserved — the gateway's
  JWKS-based verification story is unaffected by this change.
- **−** Because Thunder rejects an empty redirect URI at OAuth-application
  creation, a reserved placeholder (`https://pending.invalid/callback`) is
  registered on the wire until the SPA's real public URL resolves and aep-api
  patches it in. This is a deliberate, temporary, non-routable value — never
  reachable, never used to complete a real sign-in.
- **−** Any design.json still carrying `callerIdentity` is a hard parse
  failure, not a silent migration: the codec's `DisallowUnknownFields`
  decoding rejects the key outright (`json: unknown field "callerIdentity"`).
  Such files must be hand-edited to drop the key before they can be read
  again — consistent with the codec's existing strict-unknown-key
  philosophy, but a breaking change for any design authored before this ADR.
