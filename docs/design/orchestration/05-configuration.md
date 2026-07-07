# Configuration — Local Dev vs. Production

> Principle: **the orchestrator + aep-api code is identical across environments.** Only *config*
> changes — where Temporal lives, what persists its history, and how the orchestrator reaches
> downstream services. Config is parsed in **one place per service**
> (`services/orchestrator/internal/config`, `aep-api`'s config module) and every variable is documented
> in `.env.example`.

---

## 1. What is the same everywhere
- The orchestrator binary (workflows + activities), the `aep-api` client logic.
- Signal/query names, workflow-ID format, and the **task-queue name** — all from
  `packages/contracts/orchestration` (so dev and prod can never disagree on the boundary).
- The dev-server and the in-cluster server speak the **same Temporal gRPC API** — switching is a
  host/port + persistence change, nothing else.

## 2. What differs (config only)

| Concern | Local dev | Production (in-cluster) |
|---|---|---|
| Temporal server | `temporalio/auto-setup` container (docker-compose) | Temporal **Helm** release in the `temporal` namespace |
| Temporal persistence | **SQLite** (ephemeral; wiped with the container) | **PostgreSQL** — separate `temporal` + `temporal_visibility` DBs |
| `TEMPORAL_HOSTPORT` | `temporal:7233` (compose) / `localhost:7233` | `temporal-frontend.temporal.svc.cluster.local:7233` |
| `TEMPORAL_NAMESPACE` | `default` | `aep` (registered at install) |
| `TEMPORAL_TASK_QUEUE` | `aep-orchestrator` | `aep-orchestrator` (**same**) |
| Web UI | `temporalio/ui` on `:8233` (open) | behind the gateway (`temporal.<domain>`), access-gated |
| Orchestrator worker | docker-compose service / `make dev` | OpenChoreo Component (k8s Deployment) with cluster creds |
| `aep-api` | docker-compose service | OpenChoreo Component |
| Downstream URLs (`database`, OC proxy) | compose hostnames / `localhost` | in-cluster service DNS |
| Secrets (Anthropic, GitHub, publisher) | `.env` file / dev root token | Secret Manager API / OpenBao + ServiceAccount |
| Per-org agent namespace + `ResourceQuota` | local k3d, small quota | cluster, real per-org quota |
| Org concurrency cap value | small default (e.g. 2) | per-org config (default + override) |
| Search Attributes (`org`) | registered on the dev namespace | registered at Helm install |

## 3. Environment variables (the contract)

Parsed once per service; all live in `.env.example`:

```
# Temporal connection (both orchestrator and aep-api)
TEMPORAL_HOSTPORT=localhost:7233          # dev: temporal:7233 · prod: temporal-frontend.temporal.svc:7233
TEMPORAL_NAMESPACE=default                # prod: aep
TEMPORAL_TASK_QUEUE=aep-orchestrator      # same everywhere (matches packages/contracts/orchestration)

# Orchestrator → downstream
DATABASE_SERVICE_BASE_URL=...             # generated database client target
PLATFORM_API_SERVICE_BASE_URL=...         # OpenChoreo / cluster-gateway-proxy (reuse existing convention)

# Per-org coding-agent cap (applied to wc-<org>-remote-worker namespace ResourceQuota)
ORG_AGENT_CONCURRENCY_DEFAULT=2           # dev small; prod default with per-org overrides
```

The `aep-api` config adds only its dial settings (same `TEMPORAL_*`); it runs no worker.

> Unlike the old `main` branch (which gracefully no-op'd when `TEMPORAL_HOSTPORT` was unset, because
> Temporal was an optional shadow), here Temporal is the **primary** orchestrator — both services
> **require** a reachable Temporal. There is no legacy fallback path.

## 4. Local dev setup

`deployments/docker-compose.yml` brings up the stack:

```yaml
services:
  temporal:                       # dev-server: server + default namespace, SQLite
    image: temporalio/auto-setup:<pinned>
    ports: ["7233:7233"]
    environment: [ "DB=sqlite" ]
  temporal-ui:
    image: temporalio/ui:<pinned>
    ports: ["8233:8233"]
    environment: [ "TEMPORAL_ADDRESS=temporal:7233", "TEMPORAL_UI_PORT=8233" ]
  orchestrator:
    build: ../services/orchestrator
    environment:
      - TEMPORAL_HOSTPORT=temporal:7233
      - TEMPORAL_NAMESPACE=default
      - TEMPORAL_TASK_QUEUE=aep-orchestrator
      - DATABASE_SERVICE_BASE_URL=http://database:8080
    depends_on: [temporal]
  # aep-api: same TEMPORAL_* env; depends_on temporal
```

Workflow: `make dev` (or `docker compose up`) → open the Web UI at **http://localhost:8233** → start a
ping workflow / run a cycle and watch it live. SQLite means history is **not** durable across a
`temporal` container wipe — fine for dev, and called out so nobody is surprised.

## 5. Production setup (in-cluster)

- **Temporal Helm release** into a dedicated `temporal` namespace, **Postgres-backed** (`temporal` +
  `temporal_visibility` DBs on the managed/production PostgreSQL). Mirrors how other control-plane
  components are Helm-installed.
- **Web UI** exposed behind the existing gateway (an `HTTPRoute` like the other consoles), access-gated
  — not a raw NodePort.
- **Orchestrator** deployed as an OpenChoreo Component; reaches Temporal at
  `temporal-frontend.temporal.svc.cluster.local:7233`. `aep-api` (also a Component) dials the same.
- **Config injection** via the Secret Manager API path (not the dev root token); ServiceAccount grants
  the orchestrator the cluster access its activities need (the same OC/proxy surface the old BFF had).
- **Namespace + ResourceQuota:** the dispatch activity's `EnsureNamespace(wc-<org>-remote-worker)` also
  ensures the per-org `ResourceQuota`/`LimitRange`; cap values come from per-org config.
- **Retention/archival** for Temporal event history configured at the namespace level.

> **Platform-touching.** Standing up Temporal in the cluster (Helm, gateway `HTTPRoute`, Postgres,
> secret injection, ResourceQuota provisioning) routes through `platform-design-expert` and the
> `docs/operations/cluster-health.md` pre-flight before implementation (plan phase **O5**).

## 6. Summary
Dev = one container (SQLite) + compose services. Prod = Helm Temporal (Postgres) + OpenChoreo
Components + gateway-exposed UI. **Same binaries, same task queue, same workflow code** — the only
delta is the `TEMPORAL_*` + downstream-URL + secret config and the persistence backend. That parity is
deliberate: a workflow that runs locally runs identically in the cluster.
