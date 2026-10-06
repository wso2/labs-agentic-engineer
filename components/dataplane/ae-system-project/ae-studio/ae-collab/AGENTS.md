# AGENTS.md — components/dataplane/ae-system-project/ae-studio/ae-collab (`@aep/ae-collab`)

Yjs collaboration server for spec files, the `ae-collab` container of the
org's AE Studio pod. One `Hocuspocus` instance (`@hocuspocus/server`) hosts one
room + one Y.Doc per project (room `spec-<org>-<project>`, `Y.Map('files')` of
file-path → `Y.Text`); the doc is live, git is durable.

## Who may join

Two listeners share every room; the listener a socket came in on decides who
it is (`src/pod/auth.ts`). The public one admits a Platform IdP user token of
the pod's org, the Room socket (`AE_ROOM_SOCKET`) admits the in-pod design
agent by mount. Token expiry, `onTokenSync` and the participant rules are in
[`design/room.md`](design/room.md); read it before touching `src/pod/auth.ts`
or `src/pod/expiry.ts`. Logs carry no token, claim or room name: `room_*`
lines name the listener and a fixed cause. A token sync for another user of
the org is accepted and logged (`room_token_subject_changed`). Frames are capped at 32 MiB on
both listeners (1009).

## Modes

One per process, chosen by env in `src/modes.ts` (`selectModes`); boot fails
with none (`ae-collab: no config`) and a partial pod env fails naming every
missing key.

- **Pod mode** (`AE_ORG_ID` set, the AE Studio pod; `src/pod/`). A pod env
  carrying `COLLAB_DEV`, or a key of the removed legacy server
  (`COLLAB_MOCK_BFF`, `AEP_API_BASE`), fails the boot, naming the keys, so
  dev mode can never run there. The
  public port (`AE_LISTEN_PORT`, 8081) gates `/v1` HTTP (any casing) with
  `@aep/platform-idp-auth` (M2M → 401, another org → 403, IdP keys
  unreachable → 503 `idp_unavailable`, all before route matching; the group has no operation, so an admitted request is a 404 problem).
  A room upgrade there must pass `originAllowed`: the `Origin` must be
  present and listed in `AE_ALLOWED_ORIGINS` (403 otherwise); the in-pod
  agent joins on the Room socket, which has no Origin check. Rooms seed from and
  commit through the Files socket (`AE_FILES_SOCKET`). The health port
  (`AE_HEALTH_PORT`, 9081, not routed) serves `/healthz` and `/readyz` (503
  until both room listeners are bound and while closing). SIGTERM/SIGINT run
  the close path (see Persistence).
- **Dev mode** (`COLLAB_DEV=1` and no `AE_ORG_ID`; `pnpm dev` sets it): the
  pod's listeners and Room with auth bypassed (every connection is the dev
  user, the project is the room name after `spec-`) and a fake Files socket
  holding `fixtures.ts`; flushes commit into that fake. Missing config never
  implies it.

Never enable dev mode in a cluster.

## Room lifecycle

**A room exists only if it was seeded.** If the spec read fails — or the
project lookup does not know the project — the load is REFUSED rather than opening an empty
document (an empty doc would look healthy). An
unseeded room looks healthy and is not: its committer baseline is empty, so
every path writes with `baseSha: ""` (the Files socket reads that as *must not
exist*) and every flush 409s for as long as the room lives — which is as long as
any client stays connected. Clients see an empty document with nothing to
distinguish it from an empty project, and an agent turn joins it, syncs, and is
told the project has no files.

Refusing costs nothing a retry does not recover: `onLoadDocument` runs per room
LOAD, so the room reloads and reseeds from git on the next attempt.

**Transient failures are tagged** `reason: "upstream-unavailable"`; a verdict
(any other 4xx, e.g. `project_unknown`) is not (why:
[`design/room.md`](design/room.md)). Keep that string in step with
`useCollabSpec.ts` and `room-peer.ts`, which spell it on their own side, as
the stateless message types already are.

A refused load drops the room state (baseline and participants) and destroys
the document: no other connection holds a room whose load failed.

## Persistence + ops

How a Room saves (committer cadence, failure classes, conflicts,
`flush-warnings`, shutdown budget) is one note:
[`design/room.md`](design/room.md#how-a-room-saves). Code: `committer.ts`,
`pod/commits.ts`. Keep these in step with it:

- The stateless messages `flush` / `flushed` / `flush-error` /
  `flush-warnings` and the reason `upstream-unavailable` are spelled on the
  console side too (`useCollabSpec.ts`); the agent's `room-peer.ts` spells
  `upstream-unavailable`.
- Shutdown's 8 s budget ends inside ae-studio-tools' 10 s Files socket drain.
- `/healthz` and `/readyz` are on the health port.

## Env

Pod mode (all required once `AE_ORG_ID` is set, except the ports):
`AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`,
`AE_USER_AUDIENCES` (comma list), `AE_ALLOWED_ORIGINS` (comma list of bare
origins), `AE_FILES_SOCKET`, `AE_ROOM_SOCKET`, `AE_LISTEN_PORT` (8081),
`AE_HEALTH_PORT` (9081); ports 1-65535. Dev mode reads `COLLAB_DEV`, the two
ports, an optional `AE_ALLOWED_ORIGINS` and an optional `AE_ROOM_SOCKET`
(default `<tmpdir>/ae-collab-room.sock`).

Commands: uniform verbs via the root `Makefile`; locally
`pnpm --filter @aep/ae-collab dev|test|lint|typecheck`. `pnpm --filter
@aep/ae-collab gen` regenerates `src/generated/files-socket.d.ts` from the
Files socket contract (`packages/contracts/sockets/ae-studio/files/openapi.yaml`).
