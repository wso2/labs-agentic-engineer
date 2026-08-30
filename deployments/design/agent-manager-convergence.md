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

## The scripts

| Script | Runs when | Does |
|---|---|---|
| `setup-thunder.sh` | always | Merges Agent Manager's bootstrap documents with `single-cluster/thunder-resources/`, installs the one platform IdP on AEP's hostname. |
| `setup-agent-manager.sh` | flag on | CoreDNS rewrites, tracing + metrics modules, pipeline hand-over, then the platform-resources / sandbox / agent-manager / observability / evaluation charts. |
| `setup-agent-manager-env.sh` | flag on | The `default` environment's own Thunder and its API Platform gateway. Split out because both drive Agent Manager's admin API over its public URL and fail for reasons unrelated to the chart installs. |
| `teardown-agent-manager.sh` | manual | Makes the flag genuinely reversible. Leaves the platform IdP, OpenChoreo, the gateway operator and the observability plane alone, and restores AEP's own `DeploymentPipeline/default`. |

## Resource cost

**Profile A — flag off.** Unchanged from before convergence. The unconditional
base changes add no pods: ThunderID replaces Thunder one-for-one, OpenChoreo's
plane pod count is the same, and the gateway operator is the same single
deployment. An 8 GB Colima keeps working.

Measured on this base, 33 pods, observability off and the compose stack down:
**3.7 GB** resident inside the VM.

**Profile B — flag on.** Roughly +22 pods: OpenSearch, Prometheus and its
operator, `amp-api`/`amp-console`/PostgreSQL, the per-environment gateway
runtime, the observability plane, the tracing adapter and OTel collector, one
environment Thunder, Fluent Bit, `amp-observer` and the sandbox controller.
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
