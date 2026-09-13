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

Common to both: **coding-agent** runs as an ephemeral OpenChoreo Job Component
in the project's dataplane (image from `AGENT_RUNNER_IMAGE`); **builds** use the
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

## What setup installs, and the one switch

`scripts/setup.sh` has one profile: AEP, the platform IdP, the OpenChoreo
observability plane, and Agent Manager (~22 pods) on the same cluster. There
are no enable flags. The only environment knob is `PREBUILD_RUNNER=0`, which
builds the runner image serially inside `setup-aep.sh` instead of in the
background.

What keeps that profile affordable on an 8 GB VM: the last step of setup
**parks** the observability plane's heavy workloads at zero replicas —
OpenSearch, Prometheus, Alertmanager, the RCA agent, Fluent Bit, the
OpenTelemetry collector and the three query adapters, about 2 GB of requests.
They stay installed; nothing is uninstalled. The switch is a script, usable on
a live cluster at any time:

```bash
bash scripts/park-observability.sh status   # what is parked
bash scripts/park-observability.sh up       # traces, metrics, log archive, alert→RCA on
bash scripts/park-observability.sh down     # back to idle
```

While parked, live coding-agent output in the console is unaffected (it is read
through the OpenChoreo API), but the archive of finished cycles reads "logs
unavailable", Agent Manager's trace/metric/log views are empty, and no alert is
evaluated. Observer, the plane's controller and gateway, and amp-observer stay
up so both consoles get "no data" rather than a refused connection. A setup
re-run resets the charts' replica counts and parks again at its end.
`teardown-agent-manager.sh` removes Agent Manager and leaves the park state as
it finds it.

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
│ Coding agent: OC Job Component (AGENT_RUNNER_IMAGE) ← BFF  │
│ ClusterWorkflow: dockerfile-builder        ← BFF dispatches   │
└───────────────────────────────────────────────────────────────┘
```

Key wiring:

- `git-service` uses **host KUBECONFIG** (seeded by `start.sh` from `k3d kubeconfig get … --internal`) to write per-WorkflowRun Secrets into `workflows-default`. Mirrors agent-manager's `KUBECONFIG=/app/.kube/config` env knob.
- The coding-agent pod reaches `git-service` and `aep-api` (running on the host) via `host.k3d.internal`, which we pin to the **docker bridge gateway** in CoreDNS NodeHosts. Pods → host.
- **Two separate resolvers.** `fix_node_dns` sets the *node's* `/etc/resolv.conf` (image pulls); `k3s-resolv.conf`, mounted via `files:` in `k3d-local-config.yaml` and passed as `--resolv-conf`, is what every `dnsPolicy: Default` pod gets — CoreDNS included. CoreDNS reads its upstream once at startup and never refreshes, so without the static pin a Colima restart can leave it forwarding to a dead address: the node resolves fine, pods resolve nothing external, and coding-agent runs die at `git clone` with `Could not resolve host: github.com`. `ensure_cluster_dns_healthy` (run by `setup-k3d.sh` and every `start.sh`) probes real resolution and restarts CoreDNS if it has gone stale.
- `OPENBAO_ADDR=host.docker.internal:8200` — OpenBao's `NodePort` 30820 is exposed on host port 8200 by `k3d-local-config.yaml`.
- The platform IdP is **ThunderID 1.0.0**, one instance per cluster, shared by AEP and Agent Manager because OpenChoreo's control plane has only one `security.oidc` issuer. It is owned by neither product and named for what it is: release and namespace **`platform-idp`** (`scripts/setup-thunder.sh`; the chart it comes from is still Agent Manager's `wso2-amp-thunder-extension`, which is an implementation detail). `scripts/env.sh` is the **single source of truth for its name** — `THUNDER_NS` / `THUNDER_RELEASE`, and the `THUNDER_INTERNAL_*` addresses every script and values file derives from. Agent Manager's own per-environment Thunders are a separate tier (`thunder-<org>-<env>`), never this one. See ADR-0028.
- **Each product publishes a bootstrap bundle; the platform composes the singletons.** AEP's OAuth apps (`aep-console-client`, `aep-api-client`, `aep-system-client`, BFF→service clients, **`openchoreo-workload-publisher-client`**) are declared in `single-cluster/thunder-resources/` and merged with Agent Manager's own set at install time. Documents ThunderID keeps exactly one of — `cors`, `defaultResourceServer`, `csp` — are composed by the installer rather than owned by a publisher: `cors` is the union of both products' origins, the other two adopt Agent Manager's value, and in all three cases the document that imports last is the platform's. See `design/two-tier-thunder.md` for the publisher contract, and that directory's README for why a merge rather than a second bootstrap source.
- **The IdP's public hostname reaches its own HTTPS gateway from inside the cluster.** `ensure_platform_idp_in_coredns` (`utils.sh`) adds a `rewrite stop` under the `0-platform-idp.override` key — sorted first, because CoreDNS imports fragments in name order and the rule is first-match. Without it `thunder.openchoreo.localhost` resolves in-cluster to the *data*-plane gateway, which serves nothing for it on 8443, and an environment Thunder's trusted-issuer JWKS fetch (HTTPS-only, by ThunderID's rules) times out.
- **Every `scope=system` mint names the System resource server** (`resource=<public Thunder URL>/mcp`): aep-api, the thunder-app operator, `seed-test-users.sh` and `verify-convergence.sh` (check 9) all send it. ThunderID resolves a requested scope against a resource server, and the merged bootstrap sets the server-wide default to Agent Manager's, which does not define `system` — a mint without the indicator gets a 200 and a token with no scope, and every admin call then 403s with nothing in either response saying why.

- **Roles and test users live on the ENVIRONMENT's Thunder, never on the platform IdP.** A build provisions the roles and test users `specs/design/security.json` declares onto the T2 of the environment that version is validated in (`default`), resolved per `(org, env)` from the binding on the OpenChoreo Environment plus the admin credential in OpenBao. `aep-api` reaches it at whichever of the binding's two addresses its own location can resolve — `THUNDER_ENV_ADMIN_ROUTE` defaults to the PUBLIC issuer outside the cluster and to the in-cluster Service inside a pod, because neither resolves from where the other is right. In docker compose that means the public issuer, so the compose service carries an `extra_hosts` line per environment (`<env>-idp.amp.localhost:host-gateway`). `scripts/seed-test-users.sh [<org> <env>]` (default `default default`) seeds the four fixed local logins (`mark`/`john`/`chris`/`emily`, password `admin`) on that same T2, finding it through the binding ConfigMap and Secret by label — it has no platform-IdP mode, because a login minted there is valid nowhere a build deploys. See ADR-0022's 2026-09-05 amendment.

- **An environment's identity and its gateway have one lifecycle.** `setup-environment-thunder.sh` then `setup-environment-gateway.sh` create the pair; `scripts/remove-environment-thunder.sh <org> <env>` withdraws it, in Agent Manager's own order — gateway, then the Thunder release, its HTTPRoute and namespace, then the binding's other projections, the `Environment` annotations and the registrations in `amp-api`. It uninstalls a release, or deletes a namespace, only when `aep.wso2.com/release-created-by` / `namespace-created-by` on the namespace say these scripts installed it; anything else is left running with only AEP's own artefacts withdrawn (its client and role deregistered from the instance over its admin API, its Secrets and ConfigMaps, the OpenBao entry). The OpenChoreo `Environment` itself is deliberately kept — delete it separately. `scripts/verify-convergence.sh` checks 11 and 12 assert the record is complete, mint with it, and compare each gateway's rendered keymanager issuer against it. The whole design is `design/two-tier-thunder.md`; the decision is ADR-0029.

- **One API Platform gateway per environment, not per cluster.** `api-platform-<org>-<env>` in namespace `<org>-<env>`, installed by `scripts/setup-environment-gateway.sh` — from `setup-aep.sh` for AEP's own environments and from `setup-agent-manager-env.sh` for Agent Manager's, one script either way, binding without upgrading when the release is already there. A gateway is where a managed API's authentication is terminated, so it must terminate against exactly one identity tier: its only Thunder keymanager is the T2 named in that environment's binding record (`scripts/setup-environment-thunder.sh`). A platform-IdP token and a sibling environment's token are both 401 there, which `scripts/verify-api-platform.sh` asserts. The runtime is reached in-cluster at `api-platform-<org>-<env>-gw-gateway-gateway-runtime.<org>-<env>:22893` and publicly on the kgateway vhost `<env>-<org>.gateway.localhost:19080` (env first — the chart's derivation, and write-once in Agent Manager once registered). The `api-configuration` trait stamps `restapi-target: api-platform-<org>-<env>` on every RestApi and points its Backend at that runtime; `aep-api` derives the same host (`projects.APIGatewayHost`). All four must move together.

## What was removed from the previous v1

- `collab-server` — collaborative editing is deferred.
- Long-lived `remote-worker` container — coding agent is now an ephemeral
  OpenChoreo Job Component (`AGENT_RUNNER_IMAGE`), not a ClusterWorkflow.

## Files

| Path | Purpose |
|---|---|
| `scripts/setup.sh` | One-shot chain: k3d → prereqs → OpenChoreo → AEP infra |
| `scripts/setup-k3d.sh` | k3d cluster + CoreDNS |
| `scripts/setup-prerequisites.sh` | cert-manager + ESO + kgateway + OpenBao |
| `scripts/setup-openchoreo.sh` | Control Plane + Data Plane + Workflow Plane, then the platform IdP + the `client_id` entitlement claim |
| `scripts/setup-thunder.sh` | The one platform IdP, `platform-idp` (ThunderID via `wso2-amp-thunder-extension`): both products' bootstrap bundles merged, the CORS singleton composed, and the in-cluster CoreDNS rewrite to its HTTPS gateway |
| `scripts/setup-aep.sh` | Build ClusterWorkflow + ComponentTypes + Environment + AuthzRoleBindings + `.env` + runner image |
| `scripts/setup-environment-thunder.sh` | `<org> <env>` — that environment's own Thunder (T2) and the binding record AEP reaches it through; binds instead of re-provisioning when one already exists |
| `scripts/setup-environment-gateway.sh` | `<org> <env>` — that environment's own API Platform gateway, its ThunderKeyManager read from the binding above; binds without upgrading when the release is already there |
| `scripts/remove-environment-thunder.sh` | `<org> <env> [--yes]` — the reverse of the two above: that environment's gateway, its Thunder and every projection of its binding record. Only uninstalls a release this repo's scripts recorded installing |
| `scripts/seed-test-users.sh` | `[<org> <env>]` (default `default default`) — the four fixed local test logins on THAT environment's Thunder, resolved from its binding record |
| `scripts/setup-local.sh` | **(Skaffold)** K8s Secrets + Thunder clients + resource-type catalog + thunder-app operator (`make setup-local`) |
| `../skaffold.yaml` | **(Skaffold)** in-cluster build/deploy for `make dev-cluster` |
| `helm-charts/platform/values.local.dev.yaml.example` | **(Skaffold)** per-developer override template (webhook/smee, etc.) |
| `scripts/start.sh` | **(Compose, legacy)** Refresh DNS, seed kubeconfig, `docker compose up` |
| `scripts/stop.sh` | **(Compose, legacy)** `docker compose down` (cluster stays) |
| `docker-compose.yml` | **(Compose, legacy)** long-lived host services |
| `manifests/docker-build-workflow.yaml` | `dockerfile-builder` ClusterWorkflow (Argo CWTs) |
| `single-cluster/thunder-resources/` | AEP's bootstrap bundle for the platform IdP (OAuth apps, role assignment, its half of the composed CORS singleton) |
| `single-cluster/thunder-env-resources/` | AEP's bootstrap bundle for an ENVIRONMENT Thunder — its system client and the `aep-`prefixed role granting it `system` |
| `design/two-tier-thunder.md` | The two identity tiers, the publisher contract, the binding record and the lifecycle |
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

- `setup-prerequisites.sh` — step 6 installs the AP gateway-**operator** into `openchoreo-data-plane` (versions in `scripts/env.sh`) and applies `manifests/api-platform/rbac.yaml`. It creates no gateway: those are per-environment (see above).
- `setup-aep.sh` — adds `api-configuration` to the `service` ClusterComponentType's `allowedTraits` and installs the ClusterTrait CR from `manifests/api-platform/api-configuration-trait.yaml`.
- `manifests/poc-api-platform/` — two hello-world Components (`poc-public`, `poc-protected`) using `mendhak/http-https-echo:35`. Both have the trait attached; only the protected one's ReleaseBinding sets `jwtAuth.enabled: true`.
- No dedicated POC client: the accepted token is minted from `aep-system-client` on the environment's Thunder (credential from the binding record), the rejected platform one from the already-bootstrapped `aep-api-client`.
- `scripts/verify-api-platform.sh` — applies the manifests and runs the truth table against the environment's own gateway with three identities: that environment's T2 (accepted), the platform IdP (rejected), and a sibling environment's T2 (rejected).

Run the POC:

```bash
# After setup.sh has finished — the AP install is already part of setup-prerequisites.sh
bash scripts/verify-api-platform.sh
```

Expected output (truth table):

```
✅ public + no token                      expected 200, got 200
✅ public + own T2 token                  expected 200, got 200
✅ protected + no token                   expected 401, got 401
✅ protected + own T2 token               expected 200, got 200
✅ protected + platform (T1)              expected 401, got 401
✅ protected + 'default' T2 token         expected 401, got 401
```

When something fails, `POC-API-PLATFORM.md` is the running log of every
gotcha — that document is the actual deliverable of this branch.

## Tear down

```bash
# Skaffold: Ctrl-C the `make dev-cluster` watch (it cleans up its deploys)
bash scripts/stop.sh                # Compose: stops compose; cluster stays
k3d cluster delete openchoreo       # destroy cluster (loses all OC state)
```
