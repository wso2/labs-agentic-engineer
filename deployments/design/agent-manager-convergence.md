# Running Agent Manager on the AEP cluster

`bash scripts/setup.sh` brings up ONE k3d cluster running both AEP and the WSO2
Agent Management Platform, with Agent Manager reachable the way Thunder is — a
vhost on the OpenChoreo control-plane gateway at `:8080`.

Agent Manager is part of the base profile (it began as an opt-in flag and was
made the default on 2026-09-07). It adds roughly 22 pods; what keeps the profile
affordable is that the observability plane both products share is installed and
then **parked** at zero replicas by `scripts/park-observability.sh`, which is
also the switch that turns it on. `scripts/teardown-agent-manager.sh` removes
Agent Manager from a cluster that keeps running AEP.

Why there is one IdP rather than two, and what that cost: **ADR-0027**. Who owns
it, now that both products depend on it: **ADR-0028**. The second identity tier
— one Thunder and one API Platform gateway per environment, bound by a record —
is [`two-tier-thunder.md`](two-tier-thunder.md) and **ADR-0029**.

---

## The shape

```
                       k3d cluster "openchoreo"
  ┌──────────────────────────────────────────────────────────────┐
  │  OpenChoreo 1.2.0    CP · DP · WP  (+ observability plane)    │
  │                                                               │
  │  platform-idp/  ThunderID 1.0.0 ── the ONE platform IdP,      │
  │                 owned by neither product                      │
  │                 published at thunder.openchoreo.localhost      │
  │                 bootstrap = AM's bundle + AEP's,               │
  │                             singletons composed by the         │
  │                             installer                          │
  │                                                               │
  │  AEP                          │  Agent Manager (flag)         │
  │   aep-* OAuth clients          │   amp-* OAuth clients         │
  │   ClusterProjectType/default   │   ProjectType/default         │
  │   Environment/default ─── ONE environment, shared by both      │
  │   aep-* build templates        │   amp-* build workflows       │
  └──────────────────────────────────────────────────────────────┘
       per (org, env), platform-owned, for BOTH products:
               APIGateway api-platform-<org>-<env>
               + thunder-<org>-<env>, one Thunder per env,
                 trusting platform-idp
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
| The platform IdP, `platform-idp` (ThunderID via `wso2-amp-thunder-extension`) | A different IdP release means a different PVC and issuer. Flipping the flag would invalidate every login. Its name is deliberately neither product's — `scripts/env.sh` holds it, every address derives from there, and the chart it comes from does not leak into it (ADR-0028). |
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
| `Environment/default` and `DeploymentPipeline/default` (ns `default`) | creates both with kubectl (`setup-aep.sh`) | renders both from its platform-resources chart | ONE of each, shared: AEP creates them, Agent Manager's chart adopts them — see below. Both products deploy into the same environment, so there is one environment Thunder and one API Platform gateway between them. |
| `APIGateway` | was ONE cluster-wide `api-platform-default`, `scope: Cluster` | one per (org, env), `scope: LabelSelector` | Per (org, env) for both products. Cluster scope adopted every RestApi in the cluster, including the other product's. AEP's cluster-wide gateway is gone: `setup-environment-gateway.sh` installs `api-platform-<org>-<env>` from Agent Manager's gateway-extension chart, and the `api-configuration` trait stamps `restapi-target: api-platform-<org>-<env>` on every RestApi and points its Backend at that environment's runtime (`:22893`). The gateway then terminates auth against that environment's Thunder alone — a platform-IdP token 401s there. |
| `cors` server_config (allowed origins) | needs `http://localhost:8090` | declares only its own two origins | ONE document, composed by the installer from both declarations — see below. |
| `observability-logs-opensearch` | 0.5.1 | 0.5.3 | One release. AEP moved to 0.5.3; only AEP installs it. |
| API Platform gateway-operator | `api-platform-operator` 0.6.0 | `gateway-operator` 0.11.0 | One operator, at 0.11.0, keeping AEP's release name. |
| External Secrets | 2.0.1 | 1.3.2 | 2.0.1. Both use only `external-secrets.io/v1`. |

### The Environment and DeploymentPipeline hand-over

Worth its own note, because it took three failed attempts to get right and the
error message names a different culprit each time.

Neither product can rename its pipeline: the OpenChoreo API hardcodes
`DeploymentPipeline/default` when a client creates a project without naming
one, and both products create projects that way. And neither wants a second
environment: one environment means one environment Thunder and one API Platform
gateway for both. So there is one `Environment/default` and one
`DeploymentPipeline/default` with one promotion path, both created by
`setup-aep.sh` and both owned by Agent Manager's chart once it is installed —
the same hand-over, applied to two objects.

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
the hand-over, and what the chart renders by default — one environment and one
promotion path, both `default` — is what AEP wrote. The one thing the chart adds
is an explicit gateway ingress on the Environment, and its host is passed in as
the ClusterDataPlane's own (`openchoreoapis.localhost` locally) rather than the
chart's default `am-gateway.localhost`: component routes are built from that
host, and the Agent Manager one is NXDOMAIN from inside the cluster (its CoreDNS
rewrite targets `host.k3d.internal`), which is where the validation runner runs.
The binding annotations `setup-environment-thunder.sh` projected onto the
Environment are not in the chart's manifest and survive, because server-side
apply owns fields, not objects.

Verified as safe to coexist:
ComponentTypes (`service`/`web-application` vs `agent-api`/`external-agent-api`),
ClusterWorkflows (`dockerfile-builder` vs `amp-*`), ClusterTraits, AEP's
`postgres-cnpg` and `thunder-app` ClusterResourceTypes, and
`ClusterProjectType/default` vs the namespaced `ProjectType/default` — different
kinds, different objects.

### The CORS allow-list is composed by the installer, not owned by a product

ThunderID has exactly ONE `cors` server_config, and a redeclaration replaces it
— so whichever bootstrap document imports last owns the whole allow-list.
Neither product owns the union, and neither should have to maintain the other's
origins.

So each declares only its own — Agent Manager in its chart's
`71-amp-cors-config.yaml`, AEP in `89-platform-cors-config.yaml` — and
`setup-thunder.sh` unions them at install time into the document that imports
last, which is why AEP's keeps a prefix sorting after Agent Manager's. The
composition is a superset by construction: it can add an origin, never drop one.
The other two ThunderID singletons are composed the same way but with a
different rule: `90-platform-default-resource-server.yaml` and
`91-platform-csp.yaml` take Agent Manager's value unchanged, because there is
nothing per-product to union there — what the composition buys is that no
publisher's file is the last word. All three files are named `platform-`, not
`aep-`, for that reason.

Three things about this are worth writing down, because all three fail silently.

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

**A hand-maintained union rots.** The first version of this was one product's
document carrying both products' origins. Nothing enforced it: Agent Manager
adding an origin to its chart needed a matching edit in AEP's repo, and skipping
that edit produced the symptom below on the *other* product's console. The
composition removes the cross-repo edit entirely.

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
| `setup-thunder.sh` | always | Merges both products' bootstrap bundles, composes the singletons, installs the one platform IdP (`platform-idp`) on AEP's hostname, and makes that hostname reach the IdP's HTTPS gateway from inside the cluster. |
| `setup-agent-manager.sh` | flag on | CoreDNS rewrites, tracing + metrics modules, pipeline hand-over, then the platform-resources / sandbox / agent-manager / observability / evaluation charts — each with every Thunder address overridden off the chart default onto `env.sh`'s. |
| `setup-agent-manager-env.sh` | flag on | The `default` environment: Agent Manager's own Thunder provisioning step, then the two shared steps below for the binding record and the gateway. Split out because it drives Agent Manager's admin API over its public URL and fails for reasons unrelated to the chart installs. |
| `setup-environment-thunder.sh` | always, per env | `<org> <env>` — that environment's own Thunder (`thunder-<org>-<env>`, trusting the platform IdP as an issuer) and the binding record naming it. Binds instead of re-provisioning when Agent Manager already created the pair. |
| `setup-environment-gateway.sh` | always, per env | `<org> <env>` — that environment's API Platform gateway, `api-platform-<org>-<env>` in namespace `<org>-<env>`, with the binding above as its only Thunder keymanager and vhost `<env>-<org>.gateway.localhost:19080`. Binds without upgrading when the release already exists. |
| `remove-environment-thunder.sh` | manual, per env | `<org> <env>` — withdraws that environment's gateway and Thunder and every projection of its binding record, in Agent Manager's own order. Uninstalls a release only when the namespace's `aep.wso2.com/release-created-by` label says these scripts installed it. |
| `teardown-agent-manager.sh` | manual | Makes the flag genuinely reversible. Leaves the platform IdP, OpenChoreo, the gateway operator and the observability plane alone, and restores AEP's own `DeploymentPipeline/default`. **Read its `PROTECTED_NAMESPACES` before changing how it selects releases** — see below. |

### Why the teardown has a protected-namespace list

Per-environment gateway releases are named `api-platform-<org>-<env>`, and they
are the PLATFORM's — AEP installs one for every environment of its own, whether
or not Agent Manager is present. Beside each one the API Platform **operator**
creates a child Helm release named `api-platform-<org>-<env>-gw`.

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

That protected list is no longer a fixed list ALONE. Its core is still the
enumerated set of namespaces AEP owns outright, and the per-environment tiers
are appended to it at run time: both are platform infrastructure now, so the
namespaces holding them are DISCOVERED — one
`<org>-<env>` for every OpenChoreo `Environment` (its gateway), plus every
namespace holding a `thunder-binding` ConfigMap (an environment Thunder AEP has
bound to). What is left over is an environment Agent Manager created for
itself, which has no meaning without it.

The environment Thunders have the same problem from the other direction. Their
releases are `thunder-<org>-<env>` — a prefix the platform IdP and AEP's
`thunder-app` operator also start with, and deleting either would be worse than
deleting a gateway. So the prefix alone does not select: the release must also
be a `thunderid-*` chart, which is the thing that actually distinguishes the
tier. The legacy `amp-thunder-<org>-<env>` prefix is still matched, so a cluster
built before Agent Manager moved the prefix still cleans up.

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
