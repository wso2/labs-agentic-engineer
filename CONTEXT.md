# Lab App Factory — Domain Glossary

> Living glossary for the lab-app-factory codebase. Captures shared language
> between the platform, OpenChoreo (OC), wso2cloud, and Agent Manager (AM)
> reference. Not a spec — terms only. Implementation decisions live in ADRs.

---

## Plane and namespace topology

### `org NS` (CP) — `wc-<orgUUID8>-<orgHash8>`
The OpenChoreo control-plane namespace minted **once per organization** when
the org subscribes to a product. Created by wso2cloud's `ou` service
(`backend/core/internal/ou/util/namespace.go::GenerateNamespaceName`) via the
OC CP API. All OC CRs the platform authors for an org (Project, Component,
Workload, ReleaseBinding, SecretReference) live here, on cluster
`cloud-dp-oc-cp`.

### `org-env NS` (DP) — `wc-<orgUUID8>-<orgHash8>-<env>`
The data-plane namespace minted **once per (org, env)** by wso2cloud's `ou`
service, iterating over every `Environment` CR present in the bootstrap CP
directory. Today the only env is `development`. Holds user-app runtime
workloads on cluster `cloud-dp-oc-dp`.

### `remote-worker NS` (DP, new) — `wc-<orgUUID8>-<orgHash8>-remote-worker`
The DP-cluster namespace **per organization** where app-factory's
**coding-agent** WorkflowRun pods run (LLM-driven source-editing work — a
different category of workload from user-component image builds, which stay
on WP). Lives on `cloud-dp-oc-dp` (the same cluster as user-app workloads),
not on the WP cluster. Parallel slot to `-development` but env-less by
intent: coding-agent is not bound to an app env. Provisioned by wso2cloud's
`ou` bootstrap (new YAML template alongside `03-development_environment.yaml`).
Holds the per-org Anthropic key + GitHub PAT materialized via ESO from
`SecretReference`s. See [[adr-coding-agent-off-workflow-plane]].

### `workflows NS` (WP) — `workflows-wc-<orgUUID8>-<orgHash8>`
The workflow-plane namespace on `cloud-dp-oc-ci`. **Continues to host
app-factory's `dockerfile-builder` WorkflowRuns** (image builds — exactly
WP's purpose). Coding-agent runs are migrating off this NS to the new
remote-worker NS on DP, but builds stay.

### `release NS` (DP) — `dp-<orgPrefix>-<projectPrefix>-<env>-<hash>`
The component-release sub-namespace minted by OC's `renderedrelease-controller`
per `ReleaseBinding`. Holds the rendered user app pods. App-factory does not
write here.

---

## Project model

### `OC Project`
A logical grouping inside the **org NS** (CP), identified by labels on
Component/Workload CRs. **Not a Kubernetes namespace.** Multiple OC Projects
share the same org NS and the same org-env / remote-worker NS — credential
isolation is per-org, not per-project.

### `app-factory Project` (Postgres)
The platform's own project entity (`ComponentTask.ProjectID`, etc.).
One-to-one with an OC Project; the link is the OC Project name (a project
handle). **`project_id` is a per-org slug — unique only within an org**, reused
across orgs. Every webhook PR/issue→task lookup must scope by `(org_id,
project_id)`; the globally-unique anchor is the `git_repositories` row.

### `ComponentType` vs `ClusterComponentType`
OC render templates for a component (e.g. `deployment/service`,
`deployment/web-application`). Same NAME, two kinds: the cluster-scoped
`ClusterComponentType` (OC built-in) has **no** `registry-pull-secret`, while
the per-org **namespaced** `ComponentType` (provisioned by platform-api
`ProvisionOrgUnit` in cloud) carries `imagePullSecrets` so workloads can pull
their per-org ECR image. App-factory references `kind: ComponentType`
unconditionally; local `setup-asdlc.sh` provisions the namespaced types in the
org ns to match. See [[adr-namespaced-componenttype-local-provisioning]].

---

## Secrets

### `SecretReference`
An OpenChoreo CR (`openchoreo.dev/v1alpha1`) authored in the **org NS** (CP)
that points at a KV path in the central secret backend. ESO materializes it
into a K8s `Secret` in the consuming-plane NS. Only the reference (KV path)
crosses plane boundaries; the value never does.

### `GitSecret`
An OpenChoreo CR for build credentials. The BFF lands the build GitHub-App
token via OC `CreateGitSecret` (OpenBao + a per-org `SecretReference` named
`app-factory-component-build-git-secret`); `dockerfile-builder` synthesises the
per-run `<runName>-git-secret` ExternalSecret. OC owns the cross-plane (CP→WP)
write — never a direct in-cluster Secret apply. The per-org SecretReference is
refreshed (delete+create) every build because the App token is short-lived.
See [[adr-build-git-secret-via-openchoreo]].

### `GITHUB_REPO_VISIBILITY`
Visibility of repos the BFF creates for new projects. **Cloud default: `public`**;
local keeps `private`. Cloud is public because the build git secret can't be
delivered there yet — wso2cloud platform-api doesn't route `/gitsecrets`
(wso2-enterprise/wso2cloud#319), so `dockerfile-builder` clones unauthenticated,
which only works for public repos. **Private-repo cloud builds are blocked on
#319.** Not a builder limitation — local k3d builds private repos fine once the
secret is delivered. See [[adr-build-git-secret-via-openchoreo]].

### `SM API` — Secret Manager API
The platform secret backend service. **WriteOnly** — `GetSecretWithValue`
returns `ErrNotSupported`. Authorizes via inbound user JWT. Implements
`ManagesSecretReferences()=true` — the server itself owns
`SecretReference` CR creation, so the calling BFF must not author SRs in
addition.

- **Server source:** `wso2cloud/backend/secret-manager-api/` (full Go
  service with its own Dockerfile, `cmd/secret-manager-api/main.go`).
- **Client library:** `agent-platform/agent-manager-service/secrets/`
  (the Go HTTP client that the BFF calls).

App-factory runs SM API in **both** local and cloud (deliberate divergence
from agent-platform, which only runs SM API in cloud):
- **Cloud:** `secret-manager-api.openchoreo.dp.${cloud_base_domain}` on
  `cloud-dp-oc-cp` — the real service.
- **Local:** an **in-repo Go stub** at `deployments/local-secret-manager-api/`
  (NOT the cloud binary — that needs a private `wso2cloud` checkout). Reproduces
  the `POST/GET/DELETE /secrets` contract: writes local OpenBao KV-v2 and
  creates the `SecretReference` CR via the local OC API; preserves WriteOnly +
  ManagesSecretReferences. The BFF uses the SM-API provider in both envs (never
  an openbao provider). See [[adr-local-sm-api-in-repo-stub]].

### `AGENT_CLUSTER_SECRET_STORE` / `secretstore-read`
The ESO `ClusterSecretStore` the per-run coding-agent ExternalSecrets read
from, selected at deploy via `AGENT_CLUSTER_SECRET_STORE` (default `default`
locally; `secretstore-read` in cloud). Per-org coding-agent secrets live under
`user-app-secrets/<org>/cred-*`; only `secretstore-read` (AppRole
`approle-creds-read-permission`) grants that path. `application-secrets-read`
only grants `cloud-dp-secrets/data/application` → 403 + worker stuck in
`CreateContainerConfigError`.

### `repair-secrets` (local only)
`deployments/scripts/repair-secrets.sh`, run by `start.sh`: after a k3d/OpenBao
teardown the BFF's SM-API metadata still points at vault paths that no longer
exist, so dispatch produces ExternalSecrets ESO can't sync. The script reseeds
OpenBao from the BFF cred-store (TestMode-gated endpoint → `vault kv put`),
gated on the `k3d-openchoreo` context. Shell→vault, not SM-API, because SM-API
has no no-user (service-identity) write path.

### `OpenBao`
HashiCorp Vault fork. Used as the **local** secret backend (ReadWrite) in
the lab dev stack, behind the same `secretmanagersvc` abstraction as SM API.
Phase 0 of the OC refactor ports the AM `openbao` provider.

### `effective-key`
The internal git-service HTTP endpoint that returns the org's Anthropic key
as plaintext JSON. Read by `agents-service` for interactive spec agents
(can't be replaced by ESO-mounted secrets while agents-service runs outside
OC). Stays in place for the **local read path** even after SM API is in use
because SM API is WriteOnly.

---

## Workflow dispatch

### `ClusterWorkflow`
An OC cluster-scoped CR that defines a reusable workflow template. App-factory
authors two: `app-factory-coding-agent` (one-shot remote-worker pod per task)
and `dockerfile-builder` (image build).

### `WorkflowRun`
An OC CR that instantiates a `ClusterWorkflow`. The OC `workflowrun-controller`
projects it onto the appropriate plane via the `ClusterWorkflowPlane` mTLS
bridge.

### `ClusterWorkflowPlane`
The OC primitive that tells the `workflowrun-controller` **which cluster** to
project a WorkflowRun onto. Today points at the CI cluster
(`cloud-dp-oc-ci`); needs to be reconfigured / a new instance authored to
project app-factory's runs onto the DP cluster's remote-worker NS.

### `spec.resources` (on ClusterWorkflow)
The template block for per-run resources (`ExternalSecret` for credentials)
that OC projects alongside the workflow. Verified working on dev cloud for
agent-platform. **No longer relevant for app-factory's coding-agent** —
coding-agent moves off OC WorkflowRun entirely (see
[[adr-coding-agent-via-cluster-gateway-proxy]]); the BFF authors the
ExternalSecret directly via cluster-gateway-proxy.

### `cluster-gateway-proxy`
A wso2cloud-team-owned HTTP→DP-cluster reverse proxy
(`wso2cloud/backend/cluster-gateway-proxy/`, deployed in
`openchoreo-control-plane` on `cloud-dp-oc-cp`, also exposed externally
at `cluster-gateway-proxy.openchoreo.dp.${cloud_base_domain}`). Forwards
namespace-scoped K8s API calls to a DP cluster via OC's cluster-gateway +
cluster-agent WebSocket tunnel. Allowlist-gated by
`CLUSTER_GATEWAY_PROXY_ALLOWED_CRS`. **The cloud proxy validates platform-idp
JWTs** (`JWKS_URL` is live) — so the BFF attaches its platform-idp M2M token
(the `APP_FACTORY_BFF_TO_PLATFORM_API` provider) as `Authorization: Bearer` on
every call, including `StreamPodLog`. The **local** proxy stub is unauthenticated
(nil auth provider → no header).

App-factory's BFF (on `cloud-dp-oc-cp`) dispatches coding-agent Jobs through
this proxy — the same call pattern the wso2cloud `ou` service uses for per-org
DP bootstrap, but authenticated. See
[[adr-coding-agent-via-cluster-gateway-proxy]].

### `APP_FACTORY_BFF_TO_REMOTE_WORKER` — **legacy, unused**
Pre-provisioned Thunder OAuth2 M2M client (client_credentials grant) in
cloud's platform-idp, secret in Vault as
`app-factory-bff-to-remote-worker-client-secret`. **Provisioned for the
now-removed long-lived `remote-worker` service component**; not used by
the new Job-based dispatch (which authenticates to the proxy with the
`APP_FACTORY_BFF_TO_PLATFORM_API` M2M token, not this one). Kept in the
deployment configs as historical bookkeeping; consider for cleanup later.

---

## Identity and tokens

### `Thunder`
WSO2's IDP (`platform-idp` on cloud). Issues OIDC tokens for users and
client-credentials tokens for service-to-service. The lab stack runs a local
Thunder instance via `deployments/single-cluster/values-thunder.yaml`.

### `M2M client secret`
A `client_credentials` OAuth client provisioned in Thunder for service-to-
service auth (e.g. `APP_FACTORY_BFF_TO_PLATFORM_API`,
`app-factory-bff-to-remote-worker-client-secret`). Stored as a SecretReference
sourced from Vault on cloud; a literal env var locally.

### `Task JWT`
A short-lived RS256 bearer the BFF mints per coding-agent dispatch
(`ASDLC_BEARER`). **Local-only fallback now** — the cloud gateway's `jwtAuth`
rejects it (it isn't a platform-idp token).

### `publisher client-credentials` (runner → BFF auth)
The cloud runner→BFF auth path (implemented, replacing the Task-JWT plan). The
BFF provisions a per-org **publisher** Thunder OAuth app **at dispatch** for
*every* component (decoupled from API security), mirrors its cc secret to
SM-API, and emits a per-run ExternalSecret materialising
`PUBLISHER_CLIENT_ID/SECRET`; the runner mints a `client_credentials` token at
startup. The app **must be registered under the org's own OU** — a cc token's
`ouHandle` follows the app's OU, and the BFF verifier requires
`ouHandle == orgHandle`. Cross-org defense: same check.

### `service identity` / `X-Impersonate-Org`
How the BFF authenticates OC calls. **User-initiated** calls forward the inbound
user JWT (platform-api routes/bills by its `ouId`). **Async/service** calls
(webhooks, watchers, dispatch, build) carry the BFF M2M token and set
`X-Impersonate-Org` to the target org — resolved from the URL `namespaces/{ns}`
segment, keyed by org **handle**, preferring the Thunder `ouId`. A distinct ctx
marker (`WithServiceIdentity`) signals service vs user; an M2M token must never
sit in the user-token ctx key, and a resolver miss **aborts** the call (never
silently mis-routes/mis-bills to the Admin OU). See
[[adr-dual-mode-oc-auth-impersonation]].

---

## Source repositories (reference layout, all under `wso2/software-factory/`)

- `lab-app-factory/` — this repo. Platform code.
- `agent-manager/` — OSS open-core AM (the reference "right way"). Source of
  the `secretmanagersvc` interfaces + the `openbao` provider to port.
- `agent-platform/` — Enterprise AM superset deployed on WSO2 Cloud. Source
  of the `secret-manager-api` provider (private overlay artifact).
- `wso2cloud/` — wso2cloud platform code. `backend/core/internal/ou/` is the
  org-unit provisioner; its `util.GenerateNamespaceName` is the canonical
  source of the `wc-<orgUUID8>-<orgHash8>` shape.
- `wso2cloud-deployement-main/` — GitOps repo for cloud deployments. Holds
  app-factory's release-bindings, ClusterWorkflow CRs, Vault SecretReference
  definitions.
- `openchoreo/` — OC source. Authoritative for what
  `ClusterWorkflow`/`WorkflowRun`/`SecretReference`/`GitSecret` actually mean.
