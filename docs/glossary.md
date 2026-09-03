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
OC CP API. Project, Component, Workload, and ReleaseBinding CRs live here on
cluster `cloud-dp-oc-cp`. Local OSS uses the OC org handle (`default`) as
this CP namespace. The `wc-…` string is *also* the vault path segment
(`OrgBaseNamespace`); that is not automatically the SecretReference CR
namespace — see SecretReference.

### `org-env NS` (DP) — `wc-<orgUUID8>-<orgHash8>-<env>`
The data-plane namespace minted **once per (org, env)** by wso2cloud's `ou`
service, iterating over every `Environment` CR present in the bootstrap CP
directory. Today the only env is `development`. Holds user-app runtime
workloads on cluster `cloud-dp-oc-dp`.

### `workflows NS` (WP) — `workflows-wc-<orgUUID8>-<orgHash8>`
The workflow-plane namespace on `cloud-dp-oc-ci`. **Hosts aep's
`dockerfile-builder` WorkflowRuns** (image builds — exactly WP's purpose).
Coding-agent work never runs here and no longer runs as a WorkflowRun at all: a
run cycle is an OpenChoreo Component, and its Job renders into the project's
release NS on the DP.

### `release NS` (DP) — `dp-<orgPrefix>-<projectPrefix>-<env>-<hash>`
The component-release sub-namespace minted by OC's `renderedrelease-controller`
per `ReleaseBinding`. Holds the rendered user app pods **and** the rendered
coding-agent cycle Jobs: a cycle's ephemeral `coding-agent` Component binds to
the project's `development` environment, so OC renders its `batch/v1 Job` and
the ExternalSecrets it needs here, beside the project's own workloads.
App-factory writes nothing into this namespace directly — every create and
delete goes through the OC API on the CP.

---

## Project model

### `OC Project`
A logical grouping inside the **org NS** (CP), identified by labels on
Component/Workload CRs. **Not a Kubernetes namespace.** Multiple OC Projects
share the same org NS and the same org-env NS — credential
isolation is per-org, not per-project.

### `aep Project` (Postgres)
The platform's own project entity (`ComponentTask.ProjectID`, etc.).
One-to-one with an OC Project; the link is the OC Project name (a project
handle).

---

## Secrets

### `SecretReference`
An OpenChoreo CR (`openchoreo.dev/v1alpha1`) that points at a KV path in the
central secret backend. It **must live in the same control-plane namespace
as the Workload/ReleaseBinding that `secretKeyRef`s it** — OpenChoreo
ReleaseBinding collect looks up the CR in `releaseBinding.Namespace`, not
in the vault path. On local OSS that CP namespace is the OC org handle
(`default`); on cloud it is the org NS. `OrgBaseNamespace` (`wc-…`) is
only the vault path segment (`user-app-secrets/<wc-…>/<name>`). Do not
author the CR into `wc-…` unless that is also the Workload's CP
namespace. ESO materializes the reference into a K8s `Secret` in the
consuming-plane NS. Only the reference (KV path) crosses plane boundaries;
the value never does.

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

AEP selects one secrets provider per process (no fallback chain):
- **Cloud / overlay:** SM-API HTTP client (`ManagesSecretReferences()=true`) —
  the server owns `SecretReference` CR creation.
- **Local / OSS:** in-process OpenBao-direct provider when `OPENBAO_ADDR` (and
  `OPENBAO_TOKEN`) are set. The provider writes KV only
  (`ManagesSecretReferences()=false`); the high-level client authors
  `SecretReference` CRs via OpenChoreo into the Workload control-plane
  namespace (not the vault `wc-…` segment). See
  `services/aep-api/design/composition-seam.md`.

### `OpenBao`
HashiCorp Vault fork. Local/OSS secret KV backend behind the
`secretsprovider` / `secretmanagersvc` abstraction (OpenBao-direct provider).
Cloud overlay may use a different backend via SM-API.

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
authors one: `dockerfile-builder` (image build). Coding-agent work is **not** a
ClusterWorkflow — it uses the per-org job ComponentType defined below.

### `coding-agent` ComponentType
The **namespaced, per-org** OC ComponentType (`workloadType: job`) the BFF seeds
into the org NS and lazily re-seeds at dispatch, so a pre-rollout org works on
first use. Every run cycle — `coding | conflict | fix | validation` — is one
ephemeral Component of this type in the **milestone's own project**, so OC
renders the cycle's `batch/v1 Job` into the project's release NS and materialises
its ExternalSecrets from the org's secret store (refs only; the BFF writes no
secret material). The type pins the cost envelope: `backoffLimit: 0`,
`activeDeadlineSeconds` (3h for a coding cycle, 2h for a validation cycle),
`ttlSecondsAfterFinished`, and schema-bounded CPU/memory requests and limits.
The type name is also the key wso2cloud's entitlement gate reads
(`job/coding-agent`, `coding-agent`): a create over the org's cap answers `402`,
which the platform reports as a **blocked** run, never a failed one. Not a
`ClusterComponentType`, and never seeded through wso2cloud's
org-default-resources bootstrap. Design:
`services/aep-api/internal/delivery/codingagent/design/oc-job-dispatch.md`.

### `WorkflowRun`
An OC CR that instantiates a `ClusterWorkflow`. The OC `workflowrun-controller`
projects it onto the appropriate plane via the `ClusterWorkflowPlane` mTLS
bridge.

### `ClusterWorkflowPlane`
The OC primitive that tells the `workflowrun-controller` **which cluster** to
project a WorkflowRun onto. Points at the CI cluster (`cloud-dp-oc-ci`), which is
where aep's image builds belong. Nothing about coding-agent dispatch depends on
it any more.

### `spec.resources` (on ClusterWorkflow)
The template block for per-run resources (`ExternalSecret` for credentials) that
OC projects alongside the workflow. Verified working on dev cloud for
agent-platform. **Not relevant for aep's coding-agent** — a run cycle is a job
Component, and its ExternalSecrets are rendered by the `coding-agent`
ComponentType from the org's secret store.

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

The wso2cloud `ou` service is its caller — it dispatches per-org bootstrap to DP
**without any `Authorization` header**, only `X-Correlation-ID`
(`wso2cloud/backend/core/internal/ou/repository/cpapi.go`). **App-factory is not
a caller.** Its BFF reaches no cluster directly: cycle Jobs are OC Components
created and deleted through the OC API, their status comes from the OC resource
tree, and their live logs come from the OC resource-logs endpoint. aep-api holds
no Kubernetes client and no `CLUSTER_GATEWAY_PROXY_URL`.

### `AEP_BFF_TO_REMOTE_WORKER` — **legacy, unused**
Pre-provisioned Thunder OAuth2 M2M client (client_credentials grant) in
cloud's platform-idp, secret in Vault as
`aep-bff-to-remote-worker-client-secret`. **Provisioned for the
now-removed long-lived `remote-worker` service component**; not used by the OC
job-Component dispatch that replaced it. A cycle pod authenticates to aep-api
with the org's publisher `client_credentials` token (MCP and internal
callbacks); app-factory calls no proxy. Kept in the deployment configs as
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

### `Publisher client`
The organization's Thunder confidential OAuth app (`aep-publisher-{org}`).
The coding-agent Job authenticates to aep-api as this client
(`client_credentials`) for platform callbacks and MCP — local and cloud.
Distinct from other M2M clients and from the design
agent's BFF MCP token.

### `Task JWT`
Retired as the coding-agent Job's callback credential (that is the
publisher client). The BFF still mints short-lived RS256 identity JWTs
(`IssueServiceToken` / `IssueMCPToken`) for design-agent MCP and outbound
S2S; they carry org in `ocOrgId` and do not use the cycle id as subject.
Verifiers fetch the BFF's public key from `/auth/external/jwks.json`.
Distinct from Thunder user/M2M tokens and from the retired
`AEP_BFF_TO_REMOTE_WORKER` client.

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
an app-factory code change. See [ADR-0007](decisions/ADR-0007-metadata-driven-resource-consumption.md).
_Avoid_: reserved name, well-known type name (no `resourceType` value carries
platform-level meaning; see ADR-0007's rejected alternatives).

### External resource registry
The org-namespaced OpenChoreo `ResourceType` (ADR-0009). Domain terms live in
[`CONTEXT.md`](../CONTEXT.md): **Registered External resource**, **Project
External resource**, **consumption instructions**.

A Registered External's values are org-held (org-catalog vault path); a Project
External's values are per-project. Secret bytes never leave SM-API/OpenBao.
After aep-api restart the ResourceType still marks Registered (ADR-0021). The
design agent discovers rows via MCP `list_external_resources`.

### Unset
A declared external dependency config key authored on its binding with an empty
value. Nothing stands in for the value, so do not call it a placeholder. For a
secret key, the corresponding empty value is the binding's `secretStorePath`.

### Configured
Every config key the design currently declares for an external dependency has a
non-empty value. This is distinct from OpenChoreo binding **Ready**, which remains
true while values are unset; use `configured` for the AEP value state.

### Proceed-gate
`design/save` refuses (409) while any dependency is unresolved, naming the
component, dependency, and reason.

### Deploy gate
The milestone run's refusal to promote until every external dependency is
**configured** and every platform resource is provisioned (ADR-0023). It is not
a build gate: Build never blocks on a value, the coding agent runs with its env
vars defined and empty, and values are collected on the builds page meanwhile.
The two blockers are kept apart because they are different facts — a resource
still provisioning is the platform working, so the stage polls; an unconfigured
dependency is a person who has not acted, so the run parks in `waiting` with
reason `external-values` and names what it is waiting on. A Registered External
is outside the gate: its values are org-held and no project surface could clear
it.

---

## Milestone execution

> The decision and its costs:
> [ADR-0011](decisions/ADR-0011-milestone-is-the-unit-of-execution.md). The
> mechanism: `services/aep-api/internal/delivery/README.md`.

### Milestone
The GitHub milestone titled after a `v<N>` spec tag. It **is** the version:
the delivery increment and the version's ledger both. Its **number** is the
platform key everywhere — titles are renamable, and GitHub's title filters are
case-insensitive while its create-uniqueness is not, so a `?tag=` query
resolves number-through-run-rows and never matches a title.

### Milestone run
One supervised pass over one milestone — the platform's single dispatch door.
Kind is `dev` (delivers a version), `task` (a defect inside a delivered version)
or `validation` (re-judges a shipped one) — every platform predicate is written on
it, and only `dev` takes the one-build-per-project mutex. Origin (`spec-build`,
`incident-adoption`, `revalidate`) records where the run came from. State is
`planning | waiting | running | succeeded | failed | cancelled | blocked`.
`planning` is the fill window — the row is admitted (arming the mutex) before its
milestone is written, so it names work the platform is doing; `waiting` is the
unbounded wait, where something outside the platform is needed; `blocked` is
terminal and is not a failure — the org has no agent concurrency slot left.
A `waiting` run may carry a **waiting reason**: empty for the ordinary
between-cycles park, `external-values` for the [deploy gate](#deploy-gate), which
also names the dependencies it is blocked on so the console can link to them.
A milestone sees **sequential** runs across its life, so the workflow id is reused.
A run dispatches its cycles **one at a time**, so an org's concurrent-agent
entitlement counts in-flight milestone runs rather than tasks.

### Cycle
One dispatch within a run: `coding | conflict | fix | validation`. The cycle
record is where branch, PR number and merge SHA live, all **learned from
webhooks** — the agent derives its own branch identity, so the platform is
never told at dispatch. The run's loop POSITION is read from its latest cycle;
it is never stored as a phase enum, because fix and conflict cycles re-enter
earlier phases.

A cycle is also the **unit the coding agent runs as**: each one dispatches
exactly one ephemeral `coding-agent` job Component into the milestone's project,
never reused across cycles. While its pod lives, progress is the pod's own log
read through the OC API; once the pod is gone, the cycle's log is an observer
query, which is answerable only while the Component is retained. A finished
cycle's Component is **retained** and later **pruned oldest-first** past the
retention cap; a **cancelled** cycle's Component is deleted at once, because that
is what stops the pod and frees the org's entitlement slot — and with it the
cycle's agent log (cancelled runs keep no progress history). A cycle whose
Component has been pruned reports its log as unavailable — the platform keeps no
second copy.

The console calls one of these a **build session** — the same object, under a
name that reads as a unit of work rather than as loop machinery. The rename is
copy only: the model, the wire, the routes and the budgets all stay `cycle`
(`RunCycle`, `cycleCeiling`, `cycle-ceiling`, `/cycles/{cycleId}/builds`). The
bare word *session* never means this — that belongs to the spec-collaboration
Room.

A cycle also records what the merge policy decided about its pull request:
`resolves` (the matched agent-work issues, i.e. what the merge closes — the only
durable answer to "what did this cycle work", since the boundary read the
supervisor dispatches on returns counts), and `mergeVerdict` +`mergeReason` when
something decided against merging (`declined` by the policy, `refused` by the
host).

### Arming label
`aep`, and it says one thing: something may work this issue. It carries no
meaning about WHAT the work is — that is the kind — and it is also the
GitHub-side **adoption** trigger, so a human stamping it hands an issue to the
agent. Platform-written labels come back as webhook echoes and are dropped by
sender, which is what keeps arming a human act.

### Kind
Exactly one per issue, and the axis every routing predicate tests **positively**:
`development` (planned work from the spec), `bug` (a defect, from anywhere),
`conflict` (a pull request that will not merge), `validation` (judge the deployed
system), `provision` (a dispatch gate). A `bug` also carries a **source** —
`src/user`, `src/incident`, `src/validation`, `src/build`, `src/deploy`, absence
reading as `src/user` — which says who found it.

### Working set
Open, armed issues in the milestone whose kind the loop works: `development`,
`bug` or `conflict` for a build run; `bug` or `conflict` alone for a bug-fix run,
which works the deployed version and must never pick up the work of the version
being built — a build that gave up leaves its plan open, and only another build
may continue it. A **validation** run has none at all. A run settles when its own
working set is empty; the verdict is a separate run's answer about the version.

### Halted issue
Work a FAILED run could not finish, stamped `aep:halted` with a comment naming
the terminal reason. The reconcile sweep skips it, which is what makes a budget
mean something: open work on a milestone with no live run is otherwise
indistinguishable from work nobody started, so the run that gave up would be
replaced within a tick by one with fresh budgets. Cleared by a rebuild, or by a
person removing the label.

### Acceptance oracle
`specs/validation/validation-criteria.json` in its JUDGING role — the source of
truth a validation run grades the deployed system against. *Oracle* names what
the document DOES, not what it is: the console calls the document itself the
**Validation criteria** (`apps/console/design/lexicon.md` holds that mapping),
and one row inside it is an **acceptance criterion**, which is what its
`AC-001-a`-style id says. Different axes, so both words are correct and neither
is a leftover. Authored from the requirement prose alone by the
`validation-criteria` skill, deliberately blind to the design and the code it
will grade. A version with no acceptance oracle has nothing to validate: no
validation task is filed, nothing will ever judge it, and the verdict settles
`skipped` rather than staying silent.

### Green ending
The only thing that CLOSES a milestone: zero open working-set issues **and** a
terminal verdict on the version's newest validation run. A **build** settling at
deployed-green therefore leaves the milestone OPEN — the version is deployed and
unjudged, and the validation task it just filed is what judges it; the exception is
a version with no acceptance oracle, where no task is filed, nothing is coming, and
the milestone closes. A **validation** run settling succeeded closes it. A **bug-fix**
run never does, and no FAILED run of any kind does, because the way forward from a
failure is more work in the same version. Milestone state is display only —
nothing branches on it — except through one agent-side read: the validation agent
finds its work with `gh issue list --milestone`, which resolves by title and sees
only OPEN milestones, so a milestone closed at the hand-off hides the very task it
was closed over.

### Cancelled issue
Work a CANCELLED run had in flight, closed with a comment and stamped
`aep:cancelled`. Closing it is what makes the cancel stick: the reconcile sweep
starts a run over a milestone's open WORK when no run is live on it, so an issue
left open would have the run restarted within a tick. What a cancel reaches is per
run species — a **build**'s takes everything the increment was carrying, its
dispatch gates included, and closes the milestone with them, because the increment
is abandoned; a **bug-fix** run's takes only its bugs and conflicts and leaves the
version standing. Two populations survive even a build's cancel: the version's
**validation task**, because cancel reverts nothing and that task is a handle on
software still deployed, and the **ledger**, which the platform never touches at
all. Only issues that were OPEN at cancel time are marked,
which is what the marker is for: a build of an UNCHANGED spec reopens exactly them
and clears the label, while work a cycle genuinely finished stays closed. Nothing
is reverted — merged commits stay on `main` and promoted components keep serving.
Compare a **halted issue**, which stays open because that run may be retried.

### Dispatch gate
A `provision` issue. Never agent work — a **dispatch hold**: while one is open
the run dispatches nothing, and a hand-filed one mid-run is a deliberate human
brake. Gates are minted and resolved by `dependencies/provisioning`, and carry no
arming label, which is both why nothing works them and why they are counted on
their own rather than subtracted from the work waiting behind them.

### Ledger issue
An issue in a milestone that is **not armed**. Part of the version's record;
never worked, never stalling settle, and never written to — a cancel does not
close it and the sweep does not start a run for it, because it is nobody's work
until a human arms it. It may still be classified — a red-main
incident is filed as a `bug` so a human can see what it is — because
classification is not permission. Adding `aep` **adopts** it into the next cycle.

### Terminal reason
Why a non-succeeded run stopped. Each value names exactly ONE failure class —
`redispatch-budget`, `build-retrigger-budget`, `fix-chain-budget`,
`conflict-budget`, `no-progress`, `cycle-ceiling`, `validation-failed` — so a
reason is an explanation rather than a label. A run that settles for anything
outside this list is a bug in the loop, not a new state.

### Supersede
What the next build does to the previous version. A **plan** is replaced by a
plan: `v<N>`'s open `development` and `provision` issues are closed with a
`Superseded by v<N+1>` comment, and so is a `conflict`, which names a branch of
the version being superseded. A **defect** is not superseded by anything: open
`bug` issues are MOVED into `v<N+1>`'s milestone, because they are still broken
and the new version is what will ship the fix — "bug" read the way the working
sets read it, so an ARMED issue carrying no kind (the human hand-over) moves too. Then the old milestone is closed
and `v<N+1>` is planned fresh from the new spec. Moving is not arming — an
unadopted bug arrives still ledger-only. It is also half of what keeps the
reconcile sweep sound: a superseded milestone holds nothing workable, because its
plan is closed and its bugs have left.

## aep-api platform concepts

### Tenant gate
The deny-by-default middleware in aep-api's `edge` that binds every request to an
org taken ONLY from a verified JWT claim — never a path, query, or body — so one
org can never address another's data. Domains read the bound org from context; a
request carrying no verified org is refused. See `services/aep-api/README.md`
(Platform invariants).

### Phantom-OU (trust guard)
A JWT can name an organizational unit (`ouId`) that does not exist. aep-api rejects
an `ouId` ONLY when a wired validator positively reports it absent; an empty id, no
validator, or a transient lookup error all fail OPEN. A phantom OU would otherwise
poison `wc-` namespace derivation and the publisher OU binding.

### Committed-truth
aep-api's rule that a spec (requirements + design) is authoritative only once it is
committed to git `main`. An agent turn's output is hash-parity checked by the fold
(`platform/agentfold`) before commit; a mismatch rejects the turn and leaves `main`
untouched. The git commit — not any draft buffer — is the source of truth.

## Skills

### Skill audience
`metadata.aep.audience` on a `SKILL.md` — a list over `design` and `coding`,
naming which agent the guidance is written for. **Absent means both**, so
narrowing is opt-in and an unmarked or org-authored skill is never hidden by
omission. The design agent's catalog still *lists* a coding-audience skill (it
has to name one in order to pin it) but `load()` refuses to serve the body.
Audience never crosses a service boundary: the agents service is always the
design side, the runner always the coding side. ADR-0014.

### Skill availability (enabled / disabled)
Whether an org serves a skill at all, stored as `disabled` on the org's
`skills-manifest.json` entry rather than in frontmatter — frontmatter is part of
the content hash, so writing availability there would make a disabled skill read
as a divergence from the platform. Stored as the negative so the Go zero value
means *enabled*. A disabled skill is still reconciled and still surfaces platform
updates: disabled means "do not serve", not "do not track". ADR-0015.

### Pinned skill (`skillsPinned`)
Skills a component's `design.json` names as needed for its build, written by the
design agent. A pin does two things: it forces the skill into the project-repo
mirror even if audience or availability would withhold it, and it puts that
skill's body into the coding agent's context at startup. Named "pinned" rather
than "applied" (past-tense provenance) or "needed" (the list is deliberately not
exhaustive).

### Skill mirror
The `.claude/skills/` directory the BFF writes into a project repo, holding the
skills that build's agent may use: `(audience ∋ coding AND enabled) OR pinned`.
Written at project creation, pre-tag and dispatch, diff-first, and best-effort —
it can never fail a creation, publish or dispatch. The mirror *is* the filtered
set, so the runner applies no policy of its own.

### Skill allowlist
The SDK's `skills:` array. A skill the session discovered but which is absent
from this array is rejected outright when invoked, so the runner lists the whole
mirror, not just the pins. Membership grants a name and a description in the
model's catalog — **not** the body, which arrives on invocation. This is why a
pin additionally appends its body to the system prompt.
