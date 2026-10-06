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
  unreachable → 503 `idp_unavailable`, all before route matching; no `/v1` operation yet, so an admitted request is a 404 problem).
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
  refused ones may unload; it logs how many it unloaded with
  (`room_unloaded_with_refused {count}`), never which.
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
- **Shutdown** (SIGTERM), inside one 8 s budget that ends inside
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
`AE_USER_AUDIENCES` (comma list), `AE_ALLOWED_ORIGINS` (comma list of bare
origins), `AE_FILES_SOCKET`, `AE_ROOM_SOCKET`, `AE_LISTEN_PORT` (8081),
`AE_HEALTH_PORT` (9081); ports 1-65535. Dev mode reads `COLLAB_DEV`, the two
ports, an optional `AE_ALLOWED_ORIGINS` and an optional `AE_ROOM_SOCKET`
(default `<tmpdir>/ae-collab-room.sock`).

Commands: uniform verbs via the root `Makefile`; locally
`pnpm --filter @aep/ae-collab dev|test|lint|typecheck`.
