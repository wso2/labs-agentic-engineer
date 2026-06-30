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
handle).

---

## Secrets

### `SecretReference`
An OpenChoreo CR (`openchoreo.dev/v1alpha1`) authored in the **org NS** (CP)
that points at a KV path in the central secret backend. ESO materializes it
into a K8s `Secret` in the consuming-plane NS. Only the reference (KV path)
crosses plane boundaries; the value never does. See [[adr-tenant-secret-flow]].

### `GitSecret`
An OpenChoreo CR for build credentials, bound to `ClusterWorkflowPlane/default`
in AM's pattern. App-factory's path for GitHub PAT delivery to the build pod.

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
from agent-platform, which only runs SM API in cloud — see
[[adr-local-sm-api-stub]]):
- **Cloud:** `secret-manager-api.openchoreo.dp.${cloud_base_domain}` on
  `cloud-dp-oc-cp`.
- **Local:** a SM-API-compatible stub in the local docker-compose stack,
  backed by local OpenBao for KV storage and the local OC API for SR
  creation. The local stub preserves the WriteOnly + ManagesSecretReferences
  contract.

### `OpenBao`
HashiCorp Vault fork. Used as the **local** secret backend (ReadWrite) in
the lab dev stack, behind the same `secretmanagersvc` abstraction as SM API.
Phase 0 of the OC refactor ports the AM `openbao` provider.

### `effective-key`
The internal git-service HTTP endpoint that returns the org's Anthropic key
as plaintext JSON. Read by `agents-service` for interactive spec agents
(can't be replaced by ESO-mounted secrets while agents-service runs outside
OC). Stays in place for the **local read path** even after SM API is in use
because SM API is WriteOnly. See [[adr-effective-key-survives-sm-api]].

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
`CLUSTER_GATEWAY_PROXY_ALLOWED_CRS`. **Enforces no authentication today**
— middleware chain is logger-only; the `JWKS_URL` env var on the deployment
is dead config. Network-level isolation (in-cluster service URL +
HTTPRoute) is the actual protection.

The wso2cloud `ou` service is the existing caller — it dispatches per-org
bootstrap to DP **without any `Authorization` header**, only
`X-Correlation-ID` (`wso2cloud/backend/core/internal/ou/repository/cpapi.go`).
App-factory's BFF (also on `cloud-dp-oc-cp`) follows the same pattern to
dispatch coding-agent Jobs — see
[[adr-coding-agent-via-cluster-gateway-proxy]].

### `APP_FACTORY_BFF_TO_REMOTE_WORKER` — **legacy, unused**
Pre-provisioned Thunder OAuth2 M2M client (client_credentials grant) in
cloud's platform-idp, secret in Vault as
`app-factory-bff-to-remote-worker-client-secret`. **Provisioned for the
now-removed long-lived `remote-worker` service component**; not used by
the new Job-based dispatch (the proxy is un-authed; see
`cluster-gateway-proxy` term). Kept in the deployment configs as
historical bookkeeping; consider for cleanup later.

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
The short-lived bearer the BFF mints per coding-agent dispatch (`ASDLC_BEARER`
in the WorkflowRun param today). M1 plan replaces this with **AMP's eval-job
pattern**: per-org OAuth client-secret + per-run ExternalSecret +
`client_credentials` exchange at runner startup. See [[adr-runner-auth-amp-pattern]].

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

## Dependencies ("marketplace")

### `Marketplace`
**Working title** for app-factory's dependency-management subsystem:
identifying what each user component needs during the design phase, collecting
the config/secrets required to reach those needs, and wiring them through to
the coding agent and the deployed component. Not a browsable storefront —
there is no publish/consume surface.

### `Dependency`
A single, unified, kind-discriminated entry on a component's design
(frontmatter `dependencies`), **subsuming the legacy `dependsOn`
(same-project siblings) and `dependentApis` (external HTTP APIs) fields**.
Authored during the design phase. Invariant carried over from the
cross-component-wiring work: the dependency identifier must be a primitive
the LLM didn't invent.

### `internal dependency`
A dependency that runs within wso2cloud: a sibling component in the same
project (`component`), a service in another project of the same org
(`org-service`), or (future) a [[#platform-resource]] the platform provisions
on demand. The platform resolves URLs/credentials itself; the user is never
asked for them.

### `platform-resource`
An internal dependency that wso2cloud **provisions on demand** — future, not
yet available in wso2cloud, so its shape is not pinned down here. One
dependency kind sub-typed by `resourceType` (e.g. `database`, `message-queue`,
`cache`, `identity-provider`, …); the provisionable set is a **platform
catalog** so new types are added without changing the taxonomy. Scope (per
project vs. per organization) is undecided. Provisioning maps onto OpenChoreo's
`ResourceType` system.

### `external dependency`
A dependency that does not run within wso2cloud — a SaaS (Salesforce, GitHub),
a public/corporate API, or user-managed infrastructure (a MySQL, a queue).
**One generic kind** (`kind: external`); the old `external-api` / `saas` /
`external-resource` split is retired. The platform asks the user for the
config/secret **values**; how to use it (which SDK, which auth, where the spec
lives) is carried in the dependency's free-form `description`, not a kind enum.
A library with no external service behind it is not a dependency.

### `connection`
A registered [[#external-dependency]] the org reuses. Two layers: a **registry**
(app-factory-owned, **org-level**) holding the definition — `name`,
`description`, the config **key schema**, and "used by" bindings, read by the
architect so other projects reuse the shape; and **values + wiring** (OpenChoreo,
**per project** — see [[#connection-resourcetype]]) created when the user fills
values via a config-collection task. "Reuse" = reuse of the *schema*, not the
*credentials* (values are per-project; #3245 Resources can't be shared
cross-project). `component` / `org-service` need no connection — the platform
resolves them per consumer.

### `connection form`
The shape of an external connection = its **`config` key schema** (which keys,
which `secret`). It determines the [[#connection-resourcetype]]'s wiring
(ConfigMap + ESO ExternalSecret + outputs); `authType` is **not** part of it —
the auth *mechanism* is the agent's code, carried in `description`. A schema
change for a connection = a new ResourceType (suffix), never an edit, so RTs are
effectively immutable.

### `connection ResourceType`
The OpenChoreo `ResourceType` (namespaced, per-org) backing an external
connection's values + wiring, per the **Resource model**
([openchoreo#3245](https://github.com/openchoreo/openchoreo/discussions/3245),
shipped v1.1). Get-or-created **per connection** (named after it; wiring from its
[[#connection-form]]) in the org NS; a `Resource` is cut per project, a
`ResourceReleaseBinding` per env (BFF-authored + pinned —
the Resource path has no `AutoDeploy`). Secrets land via a store-backed ESO
`ExternalSecret` the RT emits into the workload's release NS (value written
through SM-API); the workload consumes outputs via
`Workload.spec.dependencies.resources[].ref` + `envBindings`, gated by
`ResourceDependenciesReady`. Living in the shared org NS, it is visible to both
app-factory and Devant ([[#oc-project]]s are a single shared entity per org).
Supersedes the earlier BFF-env / ConfigurationGroup (#3595) approaches.

### `credential class`
Whether an external connection's credential may be exposed to the browser:
**publishable** (a client-side key, fine in a SPA's `window._env_`) or
**secret** (server-side only — a backend `service`, never a SPA). The primary
signal is the per-key `secret: true|false`; `credential class` is the SPA
browser-exposure advisory layered on top.

### `image-based component`
A project component whose **origin** is a prebuilt container image (e.g.
Keycloak), not agent-written source. Resolved (image + version) and configured
via the same resolution + config-collection flow as a [[#connection]], deployed
as an ordinary OC Component; other components reach it via the `component`
dependency + OC Connections. **Not** a dependency kind, and **not** a
[[#platform-resource]] identity-provider (which the *platform* provisions and
shares) — an image component is a project running *its own* image.

### `component config`
Runtime config **variables a user supplies** on a component (source or image) —
plain settings and/or secrets that are *not* an external service. Collected via a
config-collection task + the per-env value form, injected into the component's
own ReleaseBinding (plain as env literals; secrets via the **component-native**
path — the ComponentType template emits an ESO `ExternalSecret` + `secretKeyRef`).
The Resource model is reserved for external [[#connection]]s; component config
never needs a Resource.

### `dependency task`
A typed task on the project board representing the work needed to satisfy a
dependency: **config-collection** (done by the user — completes when values
are saved in the console) or **resource-provisioning** (future — done by the
platform; completes when the OC `Resource` is ready and its outputs resolve).
Part of one task graph with component-implementation tasks; any task type can
gate any other. **Only task types that produce repo artifacts get GitHub
issues** (component implementation today); config-collection tasks are
platform-only — nothing dependency-related is written to (currently public)
project repos.

### `resolution` / `scoped resolution chat`
How a dependency the (single-turn) architect couldn't resolve alone gets
settled on the Architecture page. The dependency shows as a **tile**
(`ambiguous` — has candidates; `unresolved` — needs input). The target UI is a
**scoped resolution chat** — a per-dependency side chat (own conversational
agent + tools) that presents candidates as **chips** and talks through
discovery; an interim inline-card form does the same before that ships.
Resolving **pins** the choice; an **Apply / regenerate** action folds all pins
into one architect re-run. A design with `ambiguous`/`unresolved` dependencies
cannot be saved.

---


### `ClusterResourceType`
The **cluster-scoped, platform-engineer-authored** OpenChoreo provisioning
template (`openchoreo.dev/v1alpha1`, `kind: ClusterResourceType`) a
[[#platform-resource]] references. app-factory **discovers** the installed set
read-only (`ListClusterResourceTypes`) and **never authors** them — the cluster
operator installs them (wso2cloud in cloud; the local `deployments/` scripts
stand in for the platform-engineer locally, e.g. the `postgres-cnpg` sample
backed by the CloudNativePG operator). Distinct from a
[[#connection-resourcetype]], which is **namespaced, per-org, and
app-factory-authored** for an external connection's wiring. The dependency's
`resourceType` open-string IS the discovered `ClusterResourceType.metadata.name`
— no abstract→concrete indirection. See
[[adr-platform-resource-via-direct-oc-resource-model]].

### `ResourceProvisioner`
The BFF seam (P5) that fills a [[#platform-resource]]'s outputs.
**`OCNativeProvisioner`** (built) authors a `Resource` + per-env
`ResourceReleaseBinding` against a discovered [[#clusterresourcetype]] and lets
the OC controller + the cluster's real provisioner fill the binding outputs
**asynchronously** (a readiness watcher observes the native `Ready` condition —
a real DB takes minutes). **`PlatformProvisioner`** is a **reserved** second
implementation for `wso2-enterprise/wso2cloud#638` ("Platform Services
Provisioning") — designed-for, not built. Consumption
(`workload.spec.dependencies.resources[]`) is identical regardless of which
implementation provisioned. The P1-era "platform-resource = future" note above
is realized by P5 **locally**; it remains future in wso2cloud (#638). See
[[adr-platform-resource-via-direct-oc-resource-model]].
