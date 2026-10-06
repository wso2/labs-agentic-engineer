# ae-studio: the ResourceType and its pod

One org's AE Studio is the Resource `ae-studio` of the Project `ae-system`, in
the org's own namespace. `aep-api` installs the ResourceType per org and keeps
it current ([ADR-0040](../../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)).
The source is [`resourcetype.yaml`](../resourcetype.yaml); `aep-api` embeds a
byte copy (`services/aep-api/internal/organization/aestudio/resourcetype.yaml`,
copied by `go generate` and held equal by a drift test). The YAML is the authority for every exact value;
this note records the shape and the reasons.

Inputs come in two kinds, and the kind decides what a change costs:

- **`parameters`** change per release or org: images, `org`, `modelConnection`,
  `githubOwner`, `webhookRelayUrl`, the two containers' secret sets. A change
  cuts a new ResourceRelease, `aep-api` re-pins the binding, and the pod rolls.
- **`environmentConfigs`** are install facts: gateway host, scheme, listener,
  port suffix, `consoleOrigins`, IdP URLs and audiences, `aepApiBaseUrl`,
  `aeOnlyClientId`, `runtimeClassName`, `cilium`, `storage`, `pullSecret`,
  `extraEgress`, `webhookRelay.image`. A change is a binding PUT, no release.

## What it renders

Every name is built from `${metadata.name}`, the Resource's rendered name.

| id | Object | Name | Rendered when |
|---|---|---|---|
| `es-tools` | ExternalSecret → Secret | `${metadata.name}-tools` | always |
| `es-agent` | ExternalSecret → Secret | `${metadata.name}-agent` | the agent has a secret (the org's default key) |
| `es-pull` | ExternalSecret → `kubernetes.io/dockerconfigjson` Secret | `${metadata.name}-pull` | `pullSecret.remoteKey` is set |
| `deployment` | Deployment, 1 replica, `Recreate` | `${metadata.name}` | always; ready when `readyReplicas` and `updatedReplicas` are both 1 |
| `service` | Service: 8080 design, 8081 collab, 8082 tools | `${metadata.name}` | always |
| `route-design` | HTTPRoute | `${metadata.name}-design` | always |
| `route-collab` | HTTPRoute | `${metadata.name}-collab` | always |
| `route-tools` | HTTPRoute (`/v1`, `/internal/v1`, `/webhooks/github`) | `${metadata.name}-tools` | always |
| `route-tools-turns` | HTTPRoute (the turns op) | `${metadata.name}-tools-turns` | always |
| `streams-idle` | kgateway TrafficPolicy | `${metadata.name}-streams` | always |
| `netpol` | NetworkPolicy | `${metadata.name}` | always |
| `cnp-apiserver` | CiliumNetworkPolicy | `${metadata.name}-deny-apiserver` | `cilium` is true |

Outputs: `designUrl`, `collabUrl` (`ws`/`wss`), `toolsUrl`, `webhookUrl`.
They read only `metadata.*` and `environmentConfigs.*`, so they resolve on the
first reconcile. No ConfigMap and no ServiceAccount are rendered.

## Containers, sockets, probes

Pod: non-root UID/GID/fsGroup 10001, seccomp `RuntimeDefault`,
`automountServiceAccountToken: false`, `enableServiceLinks: false`,
`shareProcessNamespace: false`, `terminationGracePeriodSeconds: 30` (each
container drains inside it). Every container: read-only root, `drop: [ALL]`,
no privilege escalation. Every app container also gets its own `/tmp`
emptyDir and `HOME=/tmp`; `webhook-relay` gets neither.

| Container | Port / health port | Mounts | Startup probe budget |
|---|---|---|---|
| `ae-design-agent` | 8080 / 9080 | `mcp-sock`, `room-sock`, `studio-data` at `/snapshots` (`subPath: snapshots`, read-only) | 40 × 5 s = 200 s |
| `ae-collab` | 8081 / 9081 | `files-sock`, `room-sock` | 40 × 5 s = 200 s |
| `ae-studio-tools` | 8082 / 9082 | `files-sock`, `mcp-sock`, `studio-data` at `/studio-data` (read-write) | 30 × 2 s = 60 s |
| `webhook-relay` (optional) | none | none | none |

The two TypeScript containers start as `node --import tsx src/main.ts`, so
node is PID 1 and gets SIGTERM; tsx compiles at start, which is why their
startup budget is 200 s. `ae-studio-tools` runs under tini.

Health is on a separate port per container, absent from the Service, so no
route can reach it. `startupProbe` and `readinessProbe` both GET `/readyz`,
which turns 200 once the container's listeners and the sockets it serves are
bound, and 503 again when it starts draining.

**Sockets.** One `emptyDir{medium: Memory, sizeLimit: 1Mi}` per pair of
containers that talk, mounted into that pair only. The mount is the gate:
any process that can reach a socket is trusted by it (socket files are
`0660`).

| Socket | Dir (volume) | Served by | Dialed by |
|---|---|---|---|
| `/run/ae/files/files.sock` | `files-sock` | `ae-studio-tools` | `ae-collab` |
| `/run/ae/mcp/mcp.sock` | `mcp-sock` | `ae-studio-tools` | `ae-design-agent` |
| `/run/ae/mcp/turn.sock` | `mcp-sock` | `ae-design-agent` | `ae-studio-tools` |
| `/run/ae/room/room.sock` | `room-sock` | `ae-collab` | `ae-design-agent` |

**`studio-data`** is one disk emptyDir, `sizeLimit` from `storage.sizeLimit`
(3Gi), with an `ephemeral-storage` request from `storage.ephemeralRequest`
(1Gi) on `ae-studio-tools`. The agent sees only `snapshots/`, read-only, never
the mirrors or the reference store. Budget and eviction:
[`ae-studio-tools/design/clone-storage.md`](../ae-studio-tools/design/clone-storage.md).

## Env per container

| Container | Plain env | Secret env (from its ExternalSecret) |
|---|---|---|
| `ae-design-agent` | `AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`, `AE_USER_AUDIENCES`, `AE_MCP_SOCKET`, `AE_TURN_SOCKET`, `AE_ROOM_SOCKET`, `AE_SNAPSHOTS_DIR`, `AE_MODEL_CONNECTION`, `AE_EXPECTED_SECRET_REV` (`""` without a key), `AE_LISTEN_PORT`, `AE_HEALTH_PORT` | `ANTHROPIC_API_KEY`, `AE_SECRET_REV` |
| `ae-collab` | `AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`, `AE_USER_AUDIENCES`, `AE_ALLOWED_ORIGINS`, `AE_FILES_SOCKET`, `AE_ROOM_SOCKET`, `AE_LISTEN_PORT`, `AE_HEALTH_PORT` | none |
| `ae-studio-tools` | `AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`, `AE_IDP_TOKEN_URL`, `AE_USER_AUDIENCES`, `AE_M2M_CLIENT_ID`, `AEP_API_BASE_URL`, `AE_FILES_SOCKET`, `AE_MCP_SOCKET`, `AE_TURN_SOCKET`, `AE_STUDIO_DATA_DIR`, `AE_STORAGE_BUDGET_BYTES`, `AE_EXPECTED_SECRET_REV`, `AE_LISTEN_PORT`, `AE_HEALTH_PORT`, `AE_GITHUB_OWNER`, `AE_WEBHOOK_URL` | `GITHUB_PAT`, `GITHUB_WEBHOOK_SECRET`, `AE_STUDIO_CLIENT_ID`, `AE_STUDIO_CLIENT_SECRET`, `AE_SECRET_REV` |

`AE_ORG_ID` is the org's Platform IdP OU id; `AE_ORG_HANDLE` the org name.
`AE_USER_AUDIENCES` and `AE_ALLOWED_ORIGINS` are comma lists. `ae-collab`
checks `Origin` itself, because a WebSocket upgrade carries no CORS.
`AE_WEBHOOK_URL` is the relay channel when `webhookRelayUrl` is set, else the
`webhookUrl` output. The secret env names and the org secrets behind them are
listed once, in `aep-api`'s `secretEnvs` (`organization/aestudio/desired.go`).

## Routes, CORS, timeouts

Hosts are `oc_dns_label(metadata.resourceNamespace, "<container>")` +
`.` + `gatewayHost`: one DNS label per container, never built from a
parameter. Each route attaches to the external gateway on the
`listenerName` listener. There are no path rewrites, and only the listed
paths are routed.

| Route | Match | CORS | Request timeout | Stream idle |
|---|---|---|---|---|
| design | `PathPrefix /v1` | each of `consoleOrigins`; `GET POST DELETE OPTIONS`; `Authorization Content-Type`; no credentials | `0s` | 300 s |
| collab | `PathPrefix /v1/rooms` | none (`ae-collab` checks `Origin`) | `0s` | 300 s |
| tools `/v1` | `PathPrefix /v1` | each of `consoleOrigins`; `GET OPTIONS` | 60 s | |
| tools `/internal/v1` | `PathPrefix /internal/v1` | none | 120 s | |
| tools webhook | `POST`, `Exact /webhooks/github` | none | 10 s (GitHub's delivery window) | |
| tools-turns | `POST`, `^/internal/v1/repos/[^/]+/[^/]+/turns$` | none | 35 min | 300 s |

The turns op has its own route because its NDJSON stream outlives the
`/internal/v1` timeout: the agent caps a turn at 30 minutes. Who may call each
path: [`ae-studio-tools/design/route-groups.md`](../ae-studio-tools/design/route-groups.md).

## Network policy

- **Ingress:** only pods labelled `openchoreo.dev/system-component: gateway`
  (any namespace), on 8080 to 8082.
- **Egress:** kube-dns on 53, and `0.0.0.0/0` on TCP 80 and 443 except the
  private, link-local, CGNAT and loopback ranges; plus `extraEgress`, raw
  rules appended as given. The policy is pod-wide: every container gets the
  same egress.
- **`extraEgress` locally** admits the Thunder namespace on the IdP port and
  the `aep-api` pods on 9090, because both are in-cluster there. An install
  whose IdP and `aep-api` are public sets it to `[]`.
- **`cilium: true`** adds a CiliumNetworkPolicy that denies egress to
  `kube-apiserver`, `host` and `remote-node`, which a NetworkPolicy cannot
  express.

## Secret names and revision

The Secret names are fixed (`-tools`, `-agent`). OpenChoreo finds stale
objects by their resource id, not by name, so a name that carried a revision
would leave every earlier ExternalSecret and Secret behind, each still
holding an old token.

A changed secret still rolls the pod through a revision:

1. `aep-api` computes each container's `rev` from the set of org-secret
   reference names it reads (sha-256, first 16 hex). A save makes a new
   reference, so it changes `rev`; a no-op save does not.
2. ESO stamps `AE_SECRET_REV` into the Secret (`mergePolicy: Merge`); the pod
   spec carries `AE_EXPECTED_SECRET_REV`. A new `rev` changes the pod
   template, so the pod rolls.
3. A container exits at start while the two differ, so it never runs on a
   stale Secret; kubelet restarts it until ESO has synced (refresh 15 s).

The ExternalSecrets use `deletionPolicy: Retain`: a rotation retires the old
reference before the converge repoints them, and the last synced Secret keeps
the pod on its previous values until the roll. Dropping all of the agent's
secrets drops `es-agent`, whose id is then gone, so OpenChoreo prunes it.

## Converge

`aep-api` (`organization/aestudio`) installs, upgrades and removes the Resource
per org, both installs.

- **Steps**, each read-first, writing only what differs, logged as
  `ae_studio.converge {org, step, ms}` or `ae_studio.converge_failed {org,
  step, error}`: `desired` (compute the parameters and environment configs) →
  `project` (Project `ae-system` on `ProjectType/default`) → `write-target`
  (the pipeline's root environment, ADR-0039) → `project-binding` (creates the
  org's dataplane namespace) → `resourcetype` (PUT in place when the
  `aep.wso2.com/ae-studio-template-hash` annotation differs; never deleted) →
  `resource` (apply when parameters differ, wait up to 2 min for the new
  release) → `binding` (pin the release with the environment configs).
- **Single-flight per org.** One converge runs per org at a time, detached
  from the request, under a 3 min budget. A trigger during one makes it run
  once more after, so a save made mid-converge still lands.
- **Triggers:** `GET /api/v1/ae-studio` on drift (template hash, parameters,
  environment configs, or the pin against the latest release); a GitHub token
  submit; an agent-settings save that changes what the pod reads. There is no startup sweep across orgs, so a
  new release reaches an org the first time someone opens its console.
- **States** answered by `GET /api/v1/ae-studio`: `absent` (no active GitHub
  connection), `provisioning` (a converge runs or the binding is not Ready
  yet), `ready` (binding Ready with URLs), `failed`. `failed` is a converge
  that failed in the last 30 s on the same desired state, a terminal Ready
  reason (`RenderingFailed`, `InvalidReleaseConfiguration`,
  `ReleaseOwnershipConflict`) after a 1 min settle, or a binding not Ready for
  10 min (above the 200 s startup budget, equal to the Deployment's progress
  deadline).
- **Remove:** a GitHub disconnect removes AE Studio (`Service.Remove`). It
  holds the org so no converge starts, waits out a running one, and deletes the
  Resource; OpenChoreo deletes its bindings and the pod with it. The Project
  `ae-system` and the ResourceType stay, and a reconnect converges a new
  Resource.
- A first install on Cloud takes about 15 minutes, most of it OpenChoreo's
  first converge (ADR-0040).

## Local vs Cloud

Every environment config comes from one `aep-api` env (`AEStudioConfig`,
`services/aep-api/internal/config`), set by the platform chart's `aeStudio.*`
values. Locally aectl derives those values; Cloud sets the same env names in
its own deployment.

| Environment config | `aep-api` env | Local value (source) |
|---|---|---|
| `gatewayHost` | `AE_STUDIO_GATEWAY_HOST` | aectl `gateway.hostname` |
| `publicScheme`, `listenerName`, `publicPortSuffix` | `AE_STUDIO_PUBLIC_SCHEME`, `AE_STUDIO_LISTENER_NAME`, `AE_STUDIO_PUBLIC_PORT_SUFFIX` | from aectl `tls.enabled` alone: `http`/`http`/`:19080`, or `https`/`https`/`:19443` |
| `consoleOrigins` | `AE_STUDIO_CONSOLE_ORIGINS` | the console's public URL and `http://localhost:8090` (Vite `pnpm dev`) |
| `idp.issuer` | `AE_STUDIO_IDP_ISSUER` | aectl `thunder.public_url` |
| `idp.jwksUrl`, `idp.tokenUrl` | `AE_STUDIO_IDP_JWKS_URL`, `AE_STUDIO_IDP_TOKEN_URL` | aectl `thunder.url` + `/oauth2/jwks`, `/oauth2/token` (in-cluster: the issuer's host is loopback inside a pod) |
| `idp.userAudiences` | `AE_STUDIO_IDP_USER_AUDIENCES` | the console's client id (chart default) |
| `aepApiBaseUrl` | `AE_STUDIO_AEP_API_BASE_URL` | the chart: `aep-api`'s in-cluster Service URL |
| `aeOnlyClientId` | `AE_STUDIO_INTERNAL_CLIENT_ID` | `ae-studio-internal-client` |
| `runtimeClassName`, `cilium` | `AE_STUDIO_RUNTIME_CLASS_NAME`, `AE_STUDIO_CILIUM` | `""`, `false` |
| `storage.*` | `AE_STUDIO_STORAGE_SIZE_LIMIT`, `AE_STUDIO_STORAGE_EPHEMERAL_REQUEST`, `AE_STUDIO_STORAGE_BUDGET_BYTES` | `3Gi`, `1Gi`, `2147483648` |
| `pullSecret.*` | `AE_STUDIO_PULL_SECRET_KEY`, `AE_STUDIO_PULL_SECRET_PROPERTY` | unset (ghcr images are public) |
| `extraEgress` | `AE_STUDIO_EXTRA_EGRESS` (JSON) | Thunder and `aep-api` (above) |
| `webhookRelay.image` | `AE_STUDIO_WEBHOOK_RELAY_IMAGE` | the chart's pinned gosmee |

Images are parameters, from `AE_STUDIO_IMAGE_DESIGN_AGENT`,
`AE_STUDIO_IMAGE_COLLAB` and `AE_STUDIO_IMAGE_STUDIO_TOOLS`; an empty one
fails the converge loudly. Locally each build gets a unique tag, because an
unchanged tag leaves the parameters unchanged and the pod never rolls.

The ExternalSecrets read the DataPlane's `secretStore` (OpenBao locally).
Cloud's platform API forces every write into the org's namespace, which is
why the ResourceType is per org.

## Webhook relay (local only)

A laptop install has no public ingress, so each org's repository hooks
deliver to a smee.io channel instead:

- **Channel:** `https://smee.io/` + base64url(HMAC-SHA256(seed, org name))
  cut to 22 characters, derived on every converge and never stored. The seed
  is `aep/webhook-relay-seed`, read as `AE_STUDIO_WEBHOOK_RELAY_SEED`; unset
  means no relay. aectl's `ae_studio.webhook_relay.enabled` turns it on.
- **Container:** when `webhookRelayUrl` is set, a fourth container
  `webhook-relay` (gosmee, pinned by digest in the chart's
  `aeStudio.webhookRelay.image`) runs `client <channel>
  http://127.0.0.1:8082/webhooks/github`. It lives inside the pod because the
  NetworkPolicy admits only gateway pods; it mounts nothing, exposes no port,
  reads no Secret. `ae-studio-tools` checks the HMAC exactly as for a direct
  delivery.
- **Exposure:** the channel URL is a read capability: whoever holds it reads
  that org's deliveries. A forged delivery still fails the HMAC. smee.io
  buffers nothing, so a delivery during a roll is lost, and `aep-api`'s
  sweeps recover what it described. smee.io is a third-party relay, so
  production installs leave the seed unset.

## gVisor switch

The pod runs on the default runtime. `AE_STUDIO_RUNTIME_CLASS_NAME` becomes
the pod's `runtimeClassName` and is omitted when empty; it is empty on both
installs. A wrong value takes the only pod down, so `aep-api` sets it only
from install config. The pod already carries the
`dev.gvisor.spec.mount.<volume>.share: pod` annotations for its three socket
dirs. What turning it on needs:
[ADR-0040](../../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)
decision 7.
