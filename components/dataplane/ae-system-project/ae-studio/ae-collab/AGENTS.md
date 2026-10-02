# AGENTS.md — components/dataplane/ae-system-project/ae-studio/ae-collab (`@aep/ae-collab`)

Yjs collaboration server for spec files —
[#86](https://github.com/wso2/labs-agentic-engineer/issues/86). Hocuspocus
(`Server` from `@hocuspocus/server`) hosting one room + one Y.Doc per project
(room `spec-<org>-<project>`, `Y.Map('files')` of file-path → `Y.Text`).

**Read #86 (body + design comments) before changing anything here** — the
truth model (doc live / repo durable), persistence tiers, and agent write
path are all decided there.

## Trust model

- **Pod mode** (`src/pod/`) verifies itself (07 §11). One Hocuspocus instance,
  two listeners; the listener a socket came in on picks the token kind
  (`pod/auth.ts`):
  - public `0.0.0.0:8081`, upgrades only on `/v1/rooms`: a Platform IdP user
    token of the pod's org (`userRule`). The participant is the token's user
    (`name`, else given + family name, else `sub`; `email`, else the noreply
    address). A `credit` connection parameter is ignored.
  - local `127.0.0.1:8091`, any path, no Origin check: only the pod's
    `ae-studio-<org>` client token (`orgRule`). The participant is the user
    the `credit` query parameter names (`{"name","email"}` JSON, name
    required); the in-pod agent runs the turn for them.
  Then the room: `spec-<orgHandle>-<project>` with the pod's own handle, and
  a project the Files socket's lookup knows (once per connection). The token
  is kept only as its `exp`: the connection is closed then (`pod/expiry.ts`,
  reason `token-expired`) unless the client pushed a fresher token
  (`provider.sendToken()`) that `onTokenSync` re-verified with the same
  check; a refused sync closes it at once (reason `permission-denied`). No
  token, claim or room name is logged: `room_*` lines name the listener and a
  fixed cause.
- **Legacy server** (deleted in Task 2.12) verifies nothing itself: room
  access is delegated whole to the BFF oracle (`validate-collab-access`), and
  seeding reads the spec bundle as the first joiner (their token).

## Modes

One per process, chosen by env in `src/modes.ts` (`selectModes`); boot fails
with none (`ae-collab: no config`) and a partial pod env fails naming every
missing key.

- **Pod mode** (`AE_ORG_ID` set, the AE Studio pod; `src/pod/`). Any
  legacy key below (`COLLAB_DEV`, `COLLAB_MOCK_BFF`, `AEP_API_BASE`) in a pod
  env fails the boot, naming the keys, so dev mode can never run there. The
  public port (`AE_LISTEN_PORT`, 8081) gates `/v1` HTTP (any casing) with
  `@aep/platform-idp-auth` (M2M → 401, another org → 403, both before route
  matching; no `/v1` operation yet, so an admitted request is a 404 problem).
  A room upgrade there must pass `originAllowed`: a present `Origin` must be
  listed in `AE_ALLOWED_ORIGINS` (403); an absent one is accepted while the
  phase-2 agents bridge exists (Task 3.22 makes it 403). Rooms seed from the
  Files socket's bundle (`AE_FILES_SOCKET`). The health port
  (`AE_HEALTH_PORT`, 9081, not routed) serves `/healthz` and `/readyz` (503
  until both room listeners are bound and while closing). SIGTERM/SIGINT
  close all three. The committer moves onto the Files socket in Task 2.12;
  until then a pod room does not commit.
- **Dev mode** (`COLLAB_DEV=1`, no BFF; `pnpm dev` sets it): the pod's
  listeners and Room with auth bypassed (every connection is the dev user,
  the project is the room name after `spec-`) and a fake Files socket
  holding `fixtures.ts`. Missing config never implies it.
- **Legacy server** (the chart Deployment `collab-server`; deleted in Task
  2.12), one of:
  - **Real BFF**: set `AEP_API_BASE`.
  - **Mock BFF** (`COLLAB_MOCK_BFF=1`): an embedded stand-in for the BFF
    (`mockbff.ts`) serves `validate-collab-access` + `get-project-spec` from
    the same fixtures, and the service runs its **real** auth and seed paths
    against it. A configured BFF outranks `COLLAB_DEV`.

Never enable dev mode or the mock BFF in a cluster.

## Room lifecycle

**A room exists only if it was seeded.** If the spec read fails — or the oracle
never resolved a project — the load is REFUSED rather than opening an empty
document ([#586](https://github.com/wso2/labs-agentic-engineer/issues/586)). An
unseeded room looks healthy and is not: its committer baseline is empty, so
every path writes with `baseSha: ""` (the Files API reads that as *must not
exist*) and every flush 409s for as long as the room lives — which is as long as
any client stays connected. Clients see an empty document with nothing to
distinguish it from an empty project, and an agent turn joins it, syncs, and is
told the project has no files.

Refusing costs nothing a retry does not recover: `onLoadDocument` runs per room
LOAD, so the room reloads and reseeds from git on the next attempt.

**Transient failures are tagged.** Hocuspocus runs the load hook inside the same
try/catch as authentication, so a refused room reaches the client as a
permission-denied frame — indistinguishable, by default, from a rejected bearer,
which clients are right to stop retrying. Anything that is not a verdict the
oracle actually made (5xx, a refused connection, a DNS failure mid-redeploy) is
therefore thrown with `reason: "upstream-unavailable"`, which Hocuspocus
forwards verbatim and the console reads to decide whether to retry or give up.
Keep that string in step with `useCollabSpec.ts` and `room-peer.ts`, which spell
it on their own side, as the stateless message types already are.

The split runs on STATUS, both for the oracle (`BffAccessDeniedError`) and for
the spec read (`BffReadError`) — which is why both carry one. A permanent answer
is a verdict and must NOT be tagged: a 404 for a project whose repo row is
missing would otherwise have every open tab reconnect forever against a room
that can never be seeded. The retryable 4xx (408, 425, 429) go the other way — a
BFF shedding load is the same outage wearing a different code.

A refused load clears the room's committer **baseline**, never the room state
itself. That state is shared by every connection that authenticated into the
room, and evicting it takes another tab's token with it — a room with no token
skips its flush, so that tab's edits would be discarded in silence at exactly
the moment several tabs are reconnecting together.

## Persistence + ops (shipped)

- **Committer**: quiet-period flush (`COLLAB_COMMIT_DEBOUNCE_MS`, default 60s)
  commits via the BFF `files/apply`; `COLLAB_COMMIT_MAX_DEBOUNCE_MS` caps
  continuous editing. Last-leave and shutdown also force a flush.
- **D6 token freshness**: clients push refreshed JWTs over the stateless
  channel; on apply 401/403 the server may pull once via `token-please`.
  Residual: a last-leave forced flush often has no client for pull — exposure
  stays the ≤60s debounce window.
- **Health**: `GET /healthz` → 200 `ok`. Helm: replicas **1**, probes on
  `/healthz`, 512Mi memory, `terminationGracePeriodSeconds: 30`, concurrent
  shutdown flush (pool 8).

## Env

| Var | Default | Meaning |
|---|---|---|
| `COLLAB_PORT` | `8091` | legacy server ws listen port |
| `AEP_API_BASE` | unset | BFF base incl. prefix, e.g. `http://localhost:9090/api/v1` |
| `COLLAB_DEV` | off | dev mode (pod listeners, auth bypassed, fake Files socket) when no BFF (real or mock) is set; never implied |
| `COLLAB_MOCK_BFF` | off | run the embedded mock BFF; overrides `AEP_API_BASE` |
| `COLLAB_MOCK_BFF_PORT` | `8092` | mock BFF listen port |
| `COLLAB_COMMIT_DEBOUNCE_MS` | `60000` | quiet period before a flush commits |
| `COLLAB_COMMIT_MAX_DEBOUNCE_MS` | `300000` | max wait during continuous editing |

Pod mode (all required once `AE_ORG_ID` is set, except the ports):
`AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`,
`AE_USER_AUDIENCES` (comma list), `AE_AGENT_CLIENT_ID`, `AE_ALLOWED_ORIGINS`
(comma list of bare origins), `AE_FILES_SOCKET`, `AE_LISTEN_PORT` (8081),
`AE_HEALTH_PORT` (9081); ports 1-65535. The local listener is fixed at
`127.0.0.1:8091`. Dev mode reads only the two ports and an optional
`AE_ALLOWED_ORIGINS`.

Commands: uniform verbs via the root `Makefile`; locally
`pnpm --filter @aep/ae-collab dev|test|lint|typecheck`.
