# AGENTS.md — components/dataplane/ae-system-project/ae-studio/ae-collab (`@aep/ae-collab`)

Yjs collaboration server for spec files —
[#86](https://github.com/wso2/labs-agentic-engineer/issues/86). One
`Hocuspocus` instance (`@hocuspocus/server`) hosting one room + one Y.Doc per project
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
  check; a refused sync closes it at once (reason `permission-denied`). An
  IdP whose keys cannot be fetched is never a verdict: a join gets
  `upstream-unavailable`, a sync keeps the old deadline. A sync for another
  user of the org is accepted and logged (`room_token_subject_changed`). A
  refused room load drops the room state, participants included. Frames are
  capped at 32 MiB on both listeners (1009). No token, claim or room name is
  logged: `room_*` lines name the listener and a fixed cause.

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
  unreachable → 503 `idp_unavailable`, all before route matching; no `/v1` operation yet, so an admitted request is a 404 problem).
  A room upgrade there must pass `originAllowed`: a present `Origin` must be
  listed in `AE_ALLOWED_ORIGINS` (403); an absent one is accepted while the
  phase-2 agents bridge exists (Task 3.22 makes it 403). Rooms seed from and
  commit through the Files socket (`AE_FILES_SOCKET`). The health port
  (`AE_HEALTH_PORT`, 9081, not routed) serves `/healthz` and `/readyz` (503
  until both room listeners are bound and while closing). SIGTERM/SIGINT run
  the close path (see Persistence).
- **Dev mode** (`COLLAB_DEV=1` and no `AE_ORG_ID`; `pnpm dev` sets it): the
  pod's listeners and Room with auth bypassed (every connection is the dev
  user, the project is the room name after `spec-`) and a fake Files socket
  holding `fixtures.ts`; flushes commit into that fake. Missing config never
  implies it.

Never enable dev mode in a cluster. The chart's `collab-server` Deployment
(removed in Task 2.16) runs the legacy env and no longer boots.

## Room lifecycle

**A room exists only if it was seeded.** If the spec read fails — or the
project lookup does not know the project — the load is REFUSED rather than opening an empty
document ([#586](https://github.com/wso2/labs-agentic-engineer/issues/586)). An
unseeded room looks healthy and is not: its committer baseline is empty, so
every path writes with `baseSha: ""` (the Files socket reads that as *must not
exist*) and every flush 409s for as long as the room lives — which is as long as
any client stays connected. Clients see an empty document with nothing to
distinguish it from an empty project, and an agent turn joins it, syncs, and is
told the project has no files.

Refusing costs nothing a retry does not recover: `onLoadDocument` runs per room
LOAD, so the room reloads and reseeds from git on the next attempt.

**Transient failures are tagged.** Hocuspocus runs the load hook inside the same
try/catch as authentication, so a refused room reaches the client as a
permission-denied frame — indistinguishable, by default, from a rejected bearer,
which clients are right to stop retrying. Anything that is not a verdict (a
Files socket 5xx, 408/425/429, an unreachable or stalled socket) is therefore
thrown with `reason: "upstream-unavailable"`, which Hocuspocus forwards
verbatim and the console reads to decide whether to retry or give up. Keep that
string in step with `useCollabSpec.ts` and `room-peer.ts`, which spell it on
their own side, as the stateless message types already are. A verdict (any
other 4xx, e.g. `project_unknown`) is NOT tagged: a project that can never be
seeded must not have every open tab reconnect forever.

A refused load drops the room state (baseline and participants) and destroys
the document: no other connection holds a room whose load failed.

## Persistence + ops

- **Committer** (`committer.ts`, hooks in `pod/commits.ts`): one commit per
  flush through the Files socket's `apply`, no token (the socket is
  pod-local; ae-studio-tools sets the author, the message carries a
  `Co-authored-by` trailer per room participant). A quiet period of 60 s
  commits, 300 s caps continuous editing, the last leave forces a flush, and
  a stateless `{type:"flush", id}` forces one and is acked `flushed` or
  `flush-error` (the console's flush-before-build). Interim flushes hold
  markdown with pending agent marks; forced ones commit it. The baseline is
  the seed as the doc serializes it, so an unedited file never flushes.
- **Failure classes**: an outage (`FilesUnavailableError`: 5xx incl.
  `disk_full` and `aep_api_unavailable`, 408/425/429, `not_fast_forward`, an
  unreachable socket, a request past its 45 s deadline, which is longer
  than the pod's own 40 s per-request budget so the pod answers first) keeps the doc and the
  baseline as they were, so the next flush retries; the room hears
  `flush-error` "AE Studio is restarting — your edits are kept and will save
  shortly." A last-leave flush that fails with anything but a verdict keeps
  the room loaded (no unload) and retries it every 5 s doubling to 60 s while
  nobody is in it, until it lands (then the room unloads), a rejoin or an
  unload, or shutdown, which flushes it itself. A verdict
  (`FilesDeniedError`) is reported with its message, except a write-rule
  refusal of one path (the pod names it on its `path_invalid`: outside
  `specs/`, over 5 MiB): that change is set aside (not resent until the file
  changes), the rest of the flush is saved, and every `flush-warnings` restates
  the path as unsaved while it stays so. A room whose only unsaved changes are
  refused ones may unload.
- **One flush per room at a time**: the debounced store, `flush`, the last
  leave, a retry and shutdown queue on the room (`RoomState.flushing`), so a
  later one diffs against the baseline the earlier one left.
- **Conflicts** (a stale `baseSha`): refetch the bundle, then doc wins over
  the paths the room changed, at most 2 retries, and every path saved over a
  commit made outside the room is reported (not a blob this room committed
  itself). A path the room undid while the bundle was read is re-seeded. Files changed outside the room and
  unedited in it are re-seeded into the doc; files git gained outside the room
  are never deleted.
- **`flush-warnings`**: after every successful apply the room hears
  `{type:"flush-warnings", warnings:[{path, message}]}` (the pod's warnings plus
  the saved-over paths); an empty list clears the console's Alert.
- **Shutdown** (SIGTERM, 07 §10), inside one 8 s budget that ends inside
  ae-studio-tools' 10 s Files socket drain window: both room listeners stop
  accepting, the room sockets end and every update they delivered is applied
  (so no edit reaches a room after its flush read it; an edit typed after
  the room's sockets close is not saved: the console discards its doc on
  teardown and builds a fresh one for the next room), every loaded room
  is force-flushed (8 at a time), and rooms whose edits landed unload (the
  last-leave unloads the closed sockets started are awaited, not repeated).
  Then the health listener closes, and only then does the process exit.
- **Health**: `/healthz` and `/readyz` on the health port.

## Env

Pod mode (all required once `AE_ORG_ID` is set, except the ports):
`AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`,
`AE_USER_AUDIENCES` (comma list), `AE_AGENT_CLIENT_ID`, `AE_ALLOWED_ORIGINS`
(comma list of bare origins), `AE_FILES_SOCKET`, `AE_LISTEN_PORT` (8081),
`AE_HEALTH_PORT` (9081); ports 1-65535. The local listener is fixed at
`127.0.0.1:8091`. Dev mode reads `COLLAB_DEV`, the two ports and an optional
`AE_ALLOWED_ORIGINS`.

Commands: uniform verbs via the root `Makefile`; locally
`pnpm --filter @aep/ae-collab dev|test|lint|typecheck`.
