# Running Agent Manager on the AEP cluster

`ENABLE_AGENT_MANAGER=1 bash scripts/setup.sh` brings up ONE k3d cluster running
both AEP and the WSO2 Agent Management Platform, with Agent Manager reachable
the way Thunder is — a vhost on the OpenChoreo control-plane gateway at `:8080`.

Off by default. The flag adds roughly 22 pods and 4–5 GB of RAM, so the default
profile costs what it always did.

Why there is one IdP rather than two, and what that cost: **ADR-0024**.

---

## The shape

```
                       k3d cluster "openchoreo"
  ┌──────────────────────────────────────────────────────────────┐
  │  OpenChoreo 1.2.0    CP · DP · WP  (+ observability plane)    │
  │                                                               │
  │  amp-thunder/  ThunderID 1.0.0 ── the ONE platform IdP        │
  │                 published at thunder.openchoreo.localhost      │
  │                 bootstrap = Agent Manager's docs + AEP's       │
  │                                                               │
  │  AEP                          │  Agent Manager (flag)         │
  │   aep-* OAuth clients          │   amp-* OAuth clients         │
  │   ClusterProjectType/default   │   ProjectType/default         │
  │   Environment/development      │   Environment/default         │
  │   aep-* build templates        │   amp-* build workflows       │
  │   APIGateway api-platform-…    │   APIGateway per (org, env)   │
  │                                │   + one Thunder per env       │
  └──────────────────────────────────────────────────────────────┘
       shared: DeploymentPipeline/default, gateway-operator,
               External Secrets, OpenBao, kgateway, cert-manager
```

Everything Agent Manager needs comes from published OCI charts at
`AMP_VERSION` (`env.sh`). No sibling checkout, no local images, and AEP's
`docker-compose.yml` is untouched in both profiles.

## What is unconditional, and why

Several pins moved forward for AEP whether or not the flag is on, because
flagging them would mean maintaining two base configurations — a worse tax than
the RAM.

| Change | Why it cannot be a toggle |
|---|---|
| OpenChoreo → 1.2.0 | One cluster, one control plane. A version is not a toggle. Agent Manager's charts need `ProjectType`, which lands in 1.2.0, and cannot go backwards — so AEP moves forward. |
| ThunderID via `wso2-amp-thunder-extension` | A different IdP release means a different PVC and issuer. Flipping the flag would invalidate every login. |
| Entitlement claim → `client_id` | Follows the IdP move — it is a ThunderID behaviour change, not an OpenChoreo one. Two claim configs is exactly the dual-config trap. |
| gateway-operator → 0.11.0 / chart 1.2.2 | Upgrade in place is supported; **downgrade is not**. Flag-flipping would be one-way. |
| `ClusterProjectType/default` | AEP's own `CreateProject` needs it whether or not Agent Manager is installed. |
| `aep-`prefixed build templates | Harmless when Agent Manager is absent, required when present. |

### Two things the 1.2.0 charts started requiring

Neither is a collision — they are just gates the older charts did not have, and
both fail at `helm template` time with a clear message.

* **`Project.spec.type`.** Required on the CRD. The OpenChoreo *API* defaults it
  to `ClusterProjectType/default`, so AEP only has to make that object exist —
  which `setup-aep.sh` now does. A direct `kubectl apply` of a Project (the
  API Platform POC manifests) has to state it.

* **`observer.extraEnvs`.** The observability-plane chart now fails its own
  render if any of `controlPlaneApiUrl`, `observer.extraEnvs` or
  `rca.openchoreoApiUrl` still carries its placeholder `.invalid` domain — and
  the chart's own DEFAULT `extraEnvs` does. So the block must be supplied even
  where nothing about it is deployment-specific. It REPLACES rather than merges,
  so the chart's `AUTHZ_TIMEOUT` default has to be restated alongside it.

* **The SRE/RCA agent was renamed.** Its Deployment, Service and container all
  went `ai-rca-agent` → `sre-agent` in observability-plane 1.2.0. This one does
  NOT fail at render time — it fails eight steps later, at the first
  `kubectl ... deploy/ai-rca-agent`, with a bare `NotFound`.

And one that is a genuine behaviour change rather than a gate: creating a
Project no longer materializes its cell namespace. A `ProjectReleaseBinding`
pins a release to an environment and owns that namespace, and nothing creates
one for you. The failure is silent — the project reports `Created=True` and
`Ready=True`, then every component deploy fails with "namespace ... not found".
`aep-api` authors the binding per environment the project's pipeline promotes
through (`internal/clients/openchoreo/project_cell_client.go`).

## The collisions, and how each is resolved

Both products are OpenChoreo platforms, so they reach for the same names.

| Object | AEP | Agent Manager | Resolution |
|---|---|---|---|
| `ClusterWorkflowTemplate` `checkout-source`, `containerfile-build`, `publish-image` | own fork of each | own fork of each | AEP renamed to `aep-*`. **The dangerous one**: nothing errors, the last apply wins, and the other product's builds silently change behaviour. `setup-agent-manager.sh` also deletes pre-rename copies left by an older cluster. |
| `DeploymentPipeline/default` (ns `default`) | promotes from `development` | promotes from `default` | ONE object carrying both paths — see below. |
| `APIGateway` API selection | was `scope: Cluster` | `scope: LabelSelector` | AEP moved to `LabelSelector` and its `api-configuration` trait now stamps the matching label on every RestApi. Cluster scope would have adopted Agent Manager's RestApis too, and both gateways would serve the same APIs. |
| `cors` server_config (allowed origins) | needs `http://localhost:8090` | declares only its own two origins | ONE document carrying both — see below. |
| `observability-logs-opensearch` | 0.5.1 | 0.5.3 | One release. AEP moved to 0.5.3; only AEP installs it. |
| API Platform gateway-operator | `api-platform-operator` 0.6.0 | `gateway-operator` 0.11.0 | One operator, at 0.11.0, keeping AEP's release name. |
| External Secrets | 2.0.1 | 1.3.2 | 2.0.1. Both use only `external-secrets.io/v1`. |

### The DeploymentPipeline hand-over

Worth its own note, because it took three failed attempts to get right and the
error message names a different culprit each time.

Neither product can rename its pipeline: the OpenChoreo API hardcodes
`DeploymentPipeline/default` when a client creates a project without naming
one, and both products create projects that way. So there is one object,
carrying the union of both promotion paths, and Agent Manager's chart owns it.

Handing it over takes three things, and the first two alone are not enough:

1. **Helm ownership metadata** — the `meta.helm.sh/release-*` annotations and
   the `managed-by: Helm` label. Without these the install fails outright with
   "invalid ownership metadata".
2. **Dropping `kubectl.kubernetes.io/last-applied-configuration`.** AEP creates
   the object with a CLIENT-side `kubectl apply`; Helm's server-side apply
   migrates that annotation into a `kubectl-client-side-apply` field manager
   that owns `.spec.promotionPaths`, and the install dies on the conflict.
3. **`--force-conflicts` on the install.** This is the one that actually
   matters. Removing the annotation does not remove the field manager it
   created — and resetting `metadata.managedFields` to `[{}]`, Kubernetes'
   documented escape hatch, only renames the owner to `before-first-apply` and
   the conflict returns under a new name.

Forcing is not a workaround here: taking that field over is the entire point of
the hand-over, and the union passed as chart values is exactly what should end
up there.

Verified as safe to coexist: `Environment` (`development` vs `default`),
ComponentTypes (`service`/`web-application` vs `agent-api`/`external-agent-api`),
ClusterWorkflows (`dockerfile-builder` vs `amp-*`), ClusterTraits, AEP's
`postgres-cnpg` and `thunder-app` ClusterResourceTypes, and
`ClusterProjectType/default` vs the namespaced `ProjectType/default` — different
kinds, different objects.

### The CORS allow-list is a shared document, not a chart value

Agent Manager's bootstrap declares a `cors` server_config listing its own two
browser origins. On a converged cluster that is not wrong, only incomplete: it
predates AEP sharing the IdP. AEP redeclares the same document as
`89-aep-cors-config.yaml`, carrying the union. Server-config documents apply in
filename order and a redeclaration updates rather than duplicates, so 89 lands
after Agent Manager's 71.

Two things about this are worth writing down, because both fail silently.

**It cannot be a Helm value.** ThunderID 1.0.0's static `deployment.yaml` has no
CORS section at all, so `--set thunder.configuration.cors.allowedOrigins[...]`
writes a key nothing reads and Helm reports success. The cluster then looks
correctly configured and still refuses every cross-origin call. The bootstrap
document is the only thing that sets this.

**A re-import alone does not apply it.** Thunder reads server_config into its
runtime configuration once, at startup. Re-importing updates the database and
changes nothing that is serving traffic, so a corrected `cors` or
`defaultResourceServer` imports "successfully" while the running Thunder keeps
the old value. `reimport_bootstrap()` restarts Thunder after a successful
import for exactly this reason.

The symptom either one produces is badly misleading. A console's first call is
a browser `fetch` of `/.well-known/openid-configuration`; with no
`Access-Control-Allow-Origin` on the reply the browser discards a healthy 200
and the console renders "Sign-in failed / Failed to fetch" **before it can show
a login form** — while `curl` against the identical URL returns 200 with a
valid discovery document. The request is fine; only the browser's cross-origin
check fails. Invariant 8 in `verify-convergence.sh` asserts both consoles'
origins are admitted and an unlisted one is not, so a wildcard cannot pass it.

## The scripts

| Script | Runs when | Does |
|---|---|---|
| `setup-thunder.sh` | always | Merges Agent Manager's bootstrap documents with `single-cluster/thunder-resources/`, installs the one platform IdP on AEP's hostname. |
| `setup-agent-manager.sh` | flag on | CoreDNS rewrites, tracing + metrics modules, pipeline hand-over, then the platform-resources / sandbox / agent-manager / observability / evaluation charts. |
| `setup-agent-manager-env.sh` | flag on | The `default` environment's own Thunder and its API Platform gateway. Split out because both drive Agent Manager's admin API over its public URL and fail for reasons unrelated to the chart installs. |
| `teardown-agent-manager.sh` | manual | Makes the flag genuinely reversible. Leaves the platform IdP, OpenChoreo, the gateway operator and the observability plane alone, and restores AEP's own `DeploymentPipeline/default`. **Read its `PROTECTED_NAMESPACES` before changing how it selects releases** — see below. |

### Why the teardown has a protected-namespace list

Agent Manager's per-environment gateway releases are named
`api-platform-<org>-<env>`. So is something else: the API Platform **operator**
creates a child Helm release for AEP's OWN gateway, named
`api-platform-default-gw`, in `openchoreo-data-plane`.

Selecting releases by that name prefix matches both. The first version of the
teardown did exactly that, and then deleted each matched release's namespace —
so tearing down Agent Manager uninstalled AEP's gateway and destroyed
`openchoreo-data-plane` with it. The cluster afterwards looks fine: every
remaining pod is Running, no error is printed, and the only symptom is that
every AEP deploy fails with

```
no agents found for plane dataplane/default
```

because the data-plane cluster-agent no longer exists.

Recovering is not just a re-run, either: AEP's `APIGateway` CR carries a
finalizer that only the gateway-operator clears, and the operator was in the
namespace being deleted. The namespace hangs in `Terminating` indefinitely —

```
NamespaceFinalizersRemaining: gateway.api-platform.wso2.com/apigateway-finalizer
```

— and every reinstall fails with "unable to create new content in namespace
... because it is being terminated". The finalizer has to be cleared by hand
before `setup-prerequisites.sh` and `setup-openchoreo.sh` can rebuild it.

The teardown now refuses to touch a release in any namespace AEP or the shared
base owns, and never deletes one. Anything that selects resources by name
pattern in a cluster two products share needs the same treatment.

### Two things a data-plane reinstall silently takes with it

Rebuilding `openchoreo-data-plane` is not a clean re-run, because two pieces of
state live in that namespace while the things that depend on them live
elsewhere. Both were found by the POC truth table failing after the recovery
above, and both look like something other than what they are.

**The cluster-agent's CA.** `ClusterDataPlane/default` lives in `default` but
pins a CA copied out of a Secret in `openchoreo-data-plane`. Reinstall the data
plane and cert-manager mints a fresh CA while the CR keeps the old one, so the
agent presents a certificate the gateway will not verify. Every deploy then
fails with `no agents found for plane dataplane/default` — which reads as a
missing agent, though the agent is `Running`; it is looping on `websocket: bad
handshake` while the gateway logs `certificate not valid for any CR`. It never
self-heals, because `setup-openchoreo.sh` used to register a plane only when
the CR did not already exist. It now re-registers on every run — `kubectl
apply`, so a no-op when the CA has not moved — and the same guard covers the
workflow plane. To confirm this specific failure, compare fingerprints:

```bash
kubectl get clusterdataplane default -n default \
  -o jsonpath='{.spec.clusterAgent.clientCA.value}' | openssl x509 -noout -fingerprint -sha256
kubectl get secret cluster-agent-tls -n openchoreo-data-plane \
  -o jsonpath='{.data.ca\.crt}' | base64 -d | openssl x509 -noout -fingerprint -sha256
```

**The gateway's AES key.** The API Platform controller encrypts its persisted
API state with a key held in that same namespace. Delete the namespace and the
key is regenerated, so every API registered under the old one becomes
undecryptable and vanishes from the gateway's runtime config. The `RestApi` CRs
still report `Accepted=True, Programmed=True` — nothing told them otherwise —
while the gateway answers `direct_response` 404 for a route it clearly matched.
Restarting the controller does not help; the state is gone, not stale. The
APIs have to be redeployed, which for the POC means deleting and re-applying
its two ReleaseBindings.

## Resource cost

**Profile A — flag off.** Unchanged from before convergence. The unconditional
base changes add no pods: ThunderID replaces Thunder one-for-one, OpenChoreo's
plane pod count is the same, and the gateway operator is the same single
deployment. An 8 GB Colima keeps working.

Measured on this base, 33 pods, observability off and the compose stack down:
**3.7 GB** resident inside the VM.

**Profile B — flag on.** Measured, not estimated: **64 pods** and **7.3 GB**
resident in the VM with everything up — both platforms, the observability plane
with OpenSearch and Prometheus, the per-environment Thunder and gateway, and
AEP's compose stack. That is +31 pods and +3.6 GB over Profile A, comfortably
inside a 12 GB VM (the estimate in the plan was 4.3–5.2 GB added, so this came
in slightly under).

Budget **12 GB of VM memory and +15–25 GB of disk**:

```bash
colima stop
colima start --cpu 8 --memory 12 --disk 120   # disk can only grow
k3d cluster stop openchoreo && k3d cluster start openchoreo
```

**The third line is not optional.** A Colima restart re-creates the Docker
network, and the k3s node comes back holding its OLD node IP. It then
crash-loops on

```
Failed to start networking: unable to initialize network policy controller:
error getting node subnet: failed to find interface with specified node ip
```

which presents as an empty cluster: `kubectl get pods -A` returns nothing at
all, intermittently, because the API server is restarting under you. Stopping
and starting the cluster through k3d re-wires the node.

That restart then costs you the node's `/etc/resolv.conf` override, which k3d
resets to Docker's default resolver. Every pod that has to pull an image goes
`ImagePullBackOff` — the cluster looks alive and pulls nothing. Worse, Docker's
embedded resolver (127.0.0.11) does not reliably survive the node container
restarting: it answers CONNECTION REFUSED, and listing a dead resolver first
makes containerd fail pulls with "lookup registry-1.docker.io: Try again"
rather than falling through cleanly. `fix_node_dns` in `utils.sh` now probes it
and only puts it first when it actually answers:

```bash
cd deployments/scripts && source env.sh && source utils.sh && fix_node_dns
```

`ensure_cluster_dns_healthy` in `start.sh` covers neither of these — it repairs
CoreDNS's upstream, which is a third and later failure.

Worth knowing when checking recovery: "zero pods not-Ready" is also true of a
cluster with zero pods. Assert a plausible pod COUNT as well, or a mid-restart
cluster reads as healthy.

### The Prometheus operator's CPU limit is sized for one platform

The metrics module caps `prometheus-operator` at **40m CPU** with a **1-second**
liveness-probe timeout. That holds on a cluster running one platform. Running
two, the operator sits pinned at its ceiling (measured: 41m against the 40m
limit), `/healthz` cannot answer inside the second, and the kubelet SIGTERMs it
— forever. `setup-agent-manager.sh` raises the limit to 300m; it then settles at
12m and stops restarting. Memory is untouched, since measured use is 16Mi of the
60Mi already granted.

Two details make this hard to read from the symptoms. The operator's container
exits **0 / "Completed"**, because SIGTERM is a graceful shutdown — the pod
CrashLoopBackOffs without anything having crashed, and its logs end in orderly
shutdown messages rather than an error. And `metrics-adapter-prometheus`
crash-loops alongside it with `connection refused`, which reads as a networking
fault but is only a consequence: no operator means no Prometheus StatefulSet to
connect to. Fix the CPU limit and the adapter recovers on its own.

If 12 GB is too tight, in order of least damage: drop the metrics and tracing
modules (costs Agent Manager its metric and trace views, leaves its console,
deploys and logs working); run AEP's optional compose services only when needed;
cap OpenSearch's JVM heap explicitly rather than letting it size itself.

Agent Manager's docs warn that the in-cluster image store passes 13 GB once
agents are built, and that a small disk triggers `DiskPressure` evictions that
take cluster DNS down mid-build.

## Not covered

Product integration. This is local infrastructure convergence only — the two
platforms share a cluster, an OpenChoreo and an IdP, and nothing more. Worth
recording that the shallowest useful version of "author in AEP, manage in Agent
Manager" needs almost none of it: AEP already produces a source repo with a
Dockerfile and an OpenAPI contract, which is what Agent Manager's
Platform-Hosted agent flow consumes. The deeper version — one OpenChoreo
`Component` visible to both control planes — has unsettled ownership questions
and should be designed separately.
