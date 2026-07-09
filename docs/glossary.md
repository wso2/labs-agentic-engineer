# Glossary — domain terms

# Lab AEP — Domain Glossary

> Living glossary for the lab-aep codebase. Captures shared language
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
The DP-cluster namespace **per organization** where aep's
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
aep's `dockerfile-builder` WorkflowRuns** (image builds — exactly
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

### `aep Project` (Postgres)
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
authors two: `aep-coding-agent` (one-shot remote-worker pod per task)
and `dockerfile-builder` (image build).

### `WorkflowRun`
An OC CR that instantiates a `ClusterWorkflow`. The OC `workflowrun-controller`
projects it onto the appropriate plane via the `ClusterWorkflowPlane` mTLS
bridge.

### `ClusterWorkflowPlane`
The OC primitive that tells the `workflowrun-controller` **which cluster** to
project a WorkflowRun onto. Today points at the CI cluster
(`cloud-dp-oc-ci`); needs to be reconfigured / a new instance authored to
project aep's runs onto the DP cluster's remote-worker NS.

### `spec.resources` (on ClusterWorkflow)
The template block for per-run resources (`ExternalSecret` for credentials)
that OC projects alongside the workflow. Verified working on dev cloud for
agent-platform. **No longer relevant for aep's coding-agent** —
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

### `AEP_BFF_TO_REMOTE_WORKER` — **legacy, unused**
Pre-provisioned Thunder OAuth2 M2M client (client_credentials grant) in
cloud's platform-idp, secret in Vault as
`aep-bff-to-remote-worker-client-secret`. **Provisioned for the
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

### Platform IdP
The single shared Thunder instance backing every generated app's end-user
sign-in, as opposed to a dedicated instance per project. One issuer, one JWKS,
one keymanager-gateway trust chain — the API gateway validates every JWT
against this one issuer's JWKS and injects `X-User-Id`; services never verify
tokens themselves. A future bring-your-own-instance reference is deliberately
out of scope; the `thunder-app` `ClusterResourceType`/CRD leave room for one
(e.g. an `instanceRef`) without a breaking change.

### Thunder application
A `platform-resource` dependency (`resourceType: thunder-app`) representing a
per-project OAuth (PKCE) client registered on the Platform IdP. Declared under
the *same* dependency name by both the SPA performing sign-in and the service
whose API it protects. Provisioned like any other platform resource — via a
`ThunderApplication` CR reconciled by the in-repo `thunder-app-operator`
against Thunder's admin REST API — never by an application-plane component
calling Thunder directly.

### `callerIdentity` — retired
The implicit per-component field this dependency replaces. Design agents no
longer emit it, and the design.json codec's `DisallowUnknownFields` decoding
rejects any design still carrying the key rather than silently dropping or
migrating it — such files must be hand-edited before they parse again.

### `M2M client secret`
A `client_credentials` OAuth client provisioned in Thunder for service-to-
service auth (e.g. `AEP_BFF_TO_PLATFORM_API`,
`aep-bff-to-remote-worker-client-secret`). Stored as a SecretReference
sourced from Vault on cloud; a literal env var locally.

### `Task JWT`
The short-lived bearer the BFF mints per coding-agent dispatch (`AEP_BEARER`
in the WorkflowRun param today). M1 plan replaces this with **AMP's eval-job
pattern**: per-org OAuth client-secret + per-run ExternalSecret +
`client_credentials` exchange at runner startup. See [[adr-runner-auth-amp-pattern]].

---

## Source repositories (reference layout, all under `wso2/software-factory/`)

- `lab-aep/` — this repo. Platform code.
- `agent-manager/` — OSS open-core AM (the reference "right way"). Source of
  the `secretmanagersvc` interfaces + the `openbao` provider to port.
- `agent-platform/` — Enterprise AM superset deployed on WSO2 Cloud. Source
  of the `secret-manager-api` provider (private overlay artifact).
- `wso2cloud/` — wso2cloud platform code. `backend/core/internal/ou/` is the
  org-unit provisioner; its `util.GenerateNamespaceName` is the canonical
  source of the `wc-<orgUUID8>-<orgHash8>` shape.
- `wso2cloud-deployement-main/` — GitOps repo for cloud deployments. Holds
  aep's release-bindings, ClusterWorkflow CRs, Vault SecretReference
  definitions.
- `openchoreo/` — OC source. Authoritative for what
  `ClusterWorkflow`/`WorkflowRun`/`SecretReference`/`GitSecret` actually mean.

---

## Dependency management

### Dependency kinds
`component` (sibling in the same project), `org-service` (another project's
org-published component, addressed by project-prefixed catalog name),
`external` (third-party service consumed via configured values), and
`platform-resource` (platform-provisioned infrastructure from a typed catalog,
e.g. `postgres-cnpg`, `thunder-app`). Authored in
`specs/design/components/<name>/design.json`
under `dependencies[]`. The word **connection** is banned for these concepts —
in OC it means a consumed endpoint (WorkloadConnection), the opposite side.

### Resource-type marker
A PE-authored label or annotation (the `aep.wso2.com/` prefix) on a
`ClusterResourceType`, telling aep-api which generic consumption behavior a
`platform-resource` type needs — `role: end-user-auth` (stamp
`exposesAPI.auth` on dependents), `consumer-url-env-config` /
`consumer-url-path` (patch the consuming web-app's callback URL into an
env-config key), `skill` (auto-attach a named skill to the design). aep-api
keys behavior ONLY on markers, never on a `resourceType` name — adding a new
type, including a new auth flavor, is a cluster install plus a skill, never
an app-factory code change. See [[adr-metadata-driven-resource-consumption]].
_Avoid_: reserved name, well-known type name (no `resourceType` value carries
platform-level meaning; see ADR-0007's rejected alternatives).

### External resource registry
Org-level definitions (name + config-key schema) reusable across projects; the
design agent discovers them via MCP (`list_external_resources`) and reuses the
exact registered name. Values are per-project, per-environment; secret values
live in OpenBao via SM-API (`extres-<name>-<env>` entities) and reach pods
through ResourceReleaseBinding → ExternalSecret → env.

### Gates and typed tasks
`config-collection` and `resource-provisioning` SYSTEM rows gate component
tasks (three `depends_on_*` JSONB columns). Completion is contract-event
driven; a deploy cascades dispatch of held siblings. Recovery for a stuck
provision is drawer re-provision (SYSTEM tasks are not retryable via the
coding-agent path).

### Proceed-gate
`design/save` refuses (409) while any dependency is unresolved, naming the
component, dependency, and reason.
