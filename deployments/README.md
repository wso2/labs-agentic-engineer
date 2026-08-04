# AEP — v1 local setup (pure OpenChoreo)

A lighter alternative to `deployments-v2/` (which uses WSO2 Cloud's Flux/kustomize
layered model). v1 runs the same code, with OpenChoreo + Thunder + OpenBao + ESO
+ kgateway installed via direct `helm install`s (no Flux).

## Two local-dev options (both supported)

There are **two ways** to run the AEP services locally. Both share the same k3d
cluster + OpenChoreo install (`scripts/setup.sh`); they differ only in how the
long-lived AEP services (BFF, agents, console, collab, postgres, smee-client) run.

| | **Docker Compose** (default) | **Skaffold + k3d** |
|---|---|---|
| Where services run | host containers (`docker compose`) | in-cluster (Helm + Skaffold) |
| Entry point | `bash scripts/start.sh` | `make setup-local` → `make dev-cluster` |
| Inner loop | rebuild + `docker compose up` | Skaffold rebuilds + redeploys on file change |
| Console | http://localhost:8090 | http://console.openchoreo.localhost:8080 |

> Compose is the documented path (see the root README) — the Skaffold flow is
> here for in-cluster work. Pick one; don't run both against the same cluster at
> once.

Common to both: **coding-agent** runs as one-shot pods via the `aep-coding-agent`
ClusterWorkflow (`manifests/aep-coding-agent.yaml`); **builds** use the
`dockerfile-builder` ClusterWorkflow (`manifests/docker-build-workflow.yaml`),
whose `generate-workload-cr` step exchanges OAuth tokens at Thunder via the
`openchoreo-workload-publisher-client` bootstrapped during setup.

### Option A — Docker Compose (default)

```bash
# 1. One-shot bring-up — k3d cluster + prereqs + OpenChoreo + Thunder + AEP infra
bash scripts/setup.sh

# 2. Start the long-lived compose stack (stop: scripts/stop.sh)
bash scripts/start.sh
# → http://localhost:8090 (admin / admin)
```

No Anthropic key is needed to bring this up: the agents build their model per turn
from the calling org's connected credential (`X-Anthropic-Key`), and there is no
platform fallback. Set `ANTHROPIC_API_KEY` + `LOCAL_DEV_ADMIN_GITHUB_PAT` in
`.env` only to have `start.sh` pre-connect them via `scripts/seed-dev.sh` and skip
the Settings clickthrough. The observability plane's RCA agent is the one true
consumer of a platform-level key from `.env`.

### Option B — Skaffold + k3d (in-cluster)

```bash
# 1. One-shot bring-up (same as above)
bash scripts/setup.sh

# 2. Register secrets + Thunder clients + resource-type catalog (idempotent)
make setup-local

# 3. Inner dev loop — build images, load into k3d, deploy via Helm, watch
make dev-cluster
# Console: http://console.openchoreo.localhost:8080 · aep-api: http://localhost:9090
```

To trigger component builds on PR merge here, copy
`helm-charts/platform/values.local.dev.yaml.example` to `values.local.dev.yaml`
(git-ignored) and set a smee.io `webhook.deliveryURL` — see that file's comments.
The Compose flow needs no equivalent: setup provisions a channel into `.env` and
the stack runs the relay.

## Compose architecture (host-side compose ↔ in-cluster OC)

```
┌─────────────────────── docker compose ───────────────────────┐
│ console (nginx)  aep-api  agents                             │
│        :8090         :9090     :4000                          │
│                                                               │
│ postgres :5433  smee-client (relays smee.io → aep-api)      │
└───────────────────────────┬───────────────────────────────────┘
                            │  same docker network: k3d-openchoreo
                            ▼
┌──────────────────────── k3d cluster ──────────────────────────┐
│ OC Control / Data / Workflow planes                           │
│ Thunder IDP   OpenBao   ESO   kgateway                        │
│                                                               │
│ ClusterWorkflow: aep-coding-agent  ← BFF dispatches   │
│ ClusterWorkflow: dockerfile-builder        ← BFF dispatches   │
└───────────────────────────────────────────────────────────────┘
```

Key wiring:

- `git-service` uses **host KUBECONFIG** (seeded by `start.sh` from `k3d kubeconfig get … --internal`) to write per-WorkflowRun Secrets into `workflows-default`. Mirrors agent-manager's `KUBECONFIG=/app/.kube/config` env knob.
- The coding-agent pod reaches `git-service` and `aep-api` (running on the host) via `host.k3d.internal`, which we pin to the **docker bridge gateway** in CoreDNS NodeHosts. Pods → host.
- **Two separate resolvers.** `fix_node_dns` sets the *node's* `/etc/resolv.conf` (image pulls); `k3s-resolv.conf`, mounted via `files:` in `k3d-local-config.yaml` and passed as `--resolv-conf`, is what every `dnsPolicy: Default` pod gets — CoreDNS included. CoreDNS reads its upstream once at startup and never refreshes, so without the static pin a Colima restart can leave it forwarding to a dead address: the node resolves fine, pods resolve nothing external, and coding-agent runs die at `git clone` with `Could not resolve host: github.com`. `ensure_cluster_dns_healthy` (run by `setup-k3d.sh` and every `start.sh`) probes real resolution and restarts CoreDNS if it has gone stale.
- `OPENBAO_ADDR=host.docker.internal:8200` — OpenBao's `NodePort` 30820 is exposed on host port 8200 by `k3d-local-config.yaml`.
- Thunder OAuth apps (`aep-console-client`, `aep-api-client`, BFF→service triplets, **`openchoreo-workload-publisher-client`**) are bootstrapped via Thunder helm pre-install scripts (`single-cluster/values-thunder.yaml`), same pattern as agent-manager's `wso2-amp-thunder-extension`.

## What was removed from the previous v1

- `collab-server` — collaborative editing is deferred.
- Long-lived `remote-worker` container — coding agent is now a one-shot pod via `ClusterWorkflow: aep-coding-agent`.

## Files

| Path | Purpose |
|---|---|
| `scripts/setup.sh` | One-shot chain: k3d → prereqs → OpenChoreo → AEP infra |
| `scripts/setup-k3d.sh` | k3d cluster + CoreDNS |
| `scripts/setup-prerequisites.sh` | cert-manager + ESO + kgateway + OpenBao |
| `scripts/setup-openchoreo.sh` | Control Plane + Data Plane + Workflow Plane + Thunder |
| `scripts/setup-aep.sh` | ClusterWorkflows + ClusterComponentTypes + Environment + AuthzRoleBindings + `.env` |
| `scripts/setup-local.sh` | **(Skaffold)** K8s Secrets + Thunder clients + resource-type catalog + thunder-app operator (`make setup-local`) |
| `../skaffold.yaml` | **(Skaffold)** in-cluster build/deploy for `make dev-cluster` |
| `helm-charts/platform/values.local.dev.yaml.example` | **(Skaffold)** per-developer override template (webhook/smee, etc.) |
| `scripts/start.sh` | **(Compose, legacy)** Refresh DNS, seed kubeconfig, `docker compose up` |
| `scripts/stop.sh` | **(Compose, legacy)** `docker compose down` (cluster stays) |
| `docker-compose.yml` | **(Compose, legacy)** long-lived host services |
| `manifests/docker-build-workflow.yaml` | `dockerfile-builder` ClusterWorkflow (Argo CWTs) |
| `manifests/aep-coding-agent.yaml` | Coding-agent one-shot pod template (mirrors v2 exactly) |
| `single-cluster/values-thunder.yaml` | Thunder helm values + bootstrap scripts (users, OAuth apps) |
| `single-cluster/values-cp.yaml` | OC Control Plane helm values |
| `single-cluster/values-dp.yaml` | OC Data Plane helm values |

## Credentials

The Thunder default admin (`admin` / `admin`) is in the **Administrators** group. `setup-aep.sh` binds that group to the OC `admin` ClusterAuthzRole.

For GitHub repo provisioning, connect a PAT (or GitHub App) at **Settings → GitHub Integration**.
For AI generation, connect an Anthropic key at **Settings → Anthropic Integration** — per-org, with no platform fallback.

## POC: API Platform + Thunder JWT (`poc-api-platform` branch)

Branch-scoped experiment to prove the WSO2 API Platform gateway + the
`api-configuration` ClusterTrait + Thunder JWT validation work on this
`deployments/` setup. Findings live in `POC-API-PLATFORM.md`.

What it adds:

- `setup-prerequisites.sh` — step 6 installs the AP gateway-operator (`gateway-operator` v0.4.0, runtime image v0.9.0) into `openchoreo-data-plane`, applies `manifests/api-platform/{gateway-config.yaml,rbac.yaml,api-gateway.yaml}`.
- `setup-aep.sh` — adds `api-configuration` to the `service` ClusterComponentType's `allowedTraits` and installs the ClusterTrait CR from `manifests/api-platform/api-configuration-trait.yaml`.
- `manifests/poc-api-platform/` — two hello-world Components (`poc-public`, `poc-protected`) using `mendhak/http-https-echo:35`. Both have the trait attached; only the protected one's ReleaseBinding sets `jwtAuth.enabled: true`.
- `scripts/setup-thunder-client.sh` — bootstraps the `poc-api-platform-client` confidential OAuth client via `kubectl exec` into the Thunder pod (idempotent).
- `scripts/verify-api-platform.sh` — applies the manifests, mints a token, runs the 4-cell truth table.

Run the POC:

```bash
# After setup.sh has finished — the AP install is already part of setup-prerequisites.sh
bash scripts/verify-api-platform.sh
```

Expected output (truth table):

```
✅ protected + valid token                expected 200, got 200
✅ protected + no token                   expected 401, got 401
✅ public + valid token                   expected 200, got 200
✅ public + no token                      expected 200, got 200
```

When something fails, `POC-API-PLATFORM.md` is the running log of every
gotcha — that document is the actual deliverable of this branch.

## Tear down

```bash
# Skaffold: Ctrl-C the `make dev-cluster` watch (it cleans up its deploys)
bash scripts/stop.sh                # Compose: stops compose; cluster stays
k3d cluster delete openchoreo       # destroy cluster (loses all OC state)
```
