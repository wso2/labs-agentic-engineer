# The Room in the AE Studio pod

`ae-collab` hosts each project's Room: one Hocuspocus document per project
(room `spec-<orgHandle>-<project>`), shared live by the project's users and
the design agent, and saved to git by a committer. Why it runs in the org's
pod is
[ADR-0045](../../../../../../docs/decisions/ADR-0045-design-work-runs-in-the-organizations-ae-studio.md);
the token rules are
[ADR-0046](../../../../../../docs/decisions/ADR-0046-ae-studio-checks-platform-idp-tokens-itself.md).
This note records the shape of the Room and why. Commands, env and the
modes are in [`../AGENTS.md`](../AGENTS.md).

| Piece | Code |
|---|---|
| One Hocuspocus, no port of its own | `src/pod/room-server.ts` |
| The listeners | `src/pod/listeners.ts` |
| Who may join | `src/pod/auth.ts` |
| Token expiry | `src/pod/expiry.ts` |
| When a Room commits | `src/pod/commits.ts`, `src/committer.ts` |
| The Files socket client | `src/files-client.ts` |

## One Hocuspocus, two listeners

One Hocuspocus instance serves every Room. It has no server of its own: each
listener hands its WebSocket upgrades to `handleConnection` with the
listener's name in the context, so both listeners share every Room. The
client cannot set that name, so it is what decides who a connection is.

| Listener | Where | Caller | Admission | Participant |
|---|---|---|---|---|
| public | `0.0.0.0:8081` (`AE_LISTEN_PORT`), upgrades only on `/v1/rooms` | the browser | `Origin` in `AE_ALLOWED_ORIGINS`, then a Platform IdP user token of the pod's org (`userRule` from `@aep/platform-idp-auth`) | the token's user; a `credit` parameter is ignored |
| local | the Room socket (`AE_ROOM_SOCKET`), a Unix socket, mode 0660, on an emptyDir shared by `ae-collab` and `ae-design-agent` only | the design agent | the mount; no token is read | the user the `credit` parameter `{name, email}` names, for whom the agent runs the turn |

Then the room: its prefix must be `spec-<AE_ORG_HANDLE>-`, and its project
must pass the Files socket lookup once per connection.

- **Why the agent holds no token here.** The org's `ae-studio-<org>` client
  token stays inside `ae-studio-tools`. Socket access is the agent's identity,
  as on the pod's other sockets.
- **Why a refusal is tagged.** Hocuspocus runs the load hook inside the same
  try/catch as authentication, so a refused load reaches the client looking
  like a rejected token. An outage (Files socket 5xx, 408/425/429, an
  unreachable or stalled socket, IdP keys that cannot be fetched) is thrown
  with `reason: "upstream-unavailable"`, and the console and the agent retry
  it. A verdict (`permission-denied`, `project_unknown`) is not tagged, so a
  Room that can never load does not make every open tab reconnect forever.
  The console's `specRoom.ts` and the agent's `room-peer.ts` spell the
  same string on their side.

## Token expiry is enforced

A user connection lives only as long as its token. `ae-collab` keeps the
token's `exp` and closes the connection then (reason `token-expired`),
unless the console has pushed a fresher token through the provider's token
sync and `onTokenSync` re-verified it with the same check. A refused sync
closes the connection at once (`permission-denied`). An IdP whose keys
cannot be fetched keeps the old deadline. So a role change reaches an open
Room with the user's next token.

The agent's connection has no deadline, and a token synced on it is ignored.
Saves carry no user token at all, so the old token-please exchange is gone.

## The Files socket, one project per call

Seeding and committing go to `ae-studio-tools` over the pod-local Files
socket (`AE_FILES_SOCKET`; contract
`packages/contracts/sockets/ae-studio/files/openapi.yaml`). Every call names
a project, never a repository, and `ae-studio-tools` resolves the project's
repository through `aep-api` on every call, with no cache.

- **Why per call.** If `ae-collab` named owner and repo, a compromised
  public WebSocket server could push to any repository the org's gitpat
  reaches. Only `aep-api` knows which repositories are this org's.
- **What it costs.** One `aep-api` read per save, and at most one save a
  minute per Room under steady editing. A save while `aep-api` is down fails
  and is retried; a project deleted while its Room is open fails its next
  save instead of writing to the old repository.
- The commit author is the gitpat identity `ae-studio-tools` resolves; each
  Room participant rides as a `Co-authored-by` trailer.

## How a Room saves

The committer (`src/committer.ts`, hooks in `src/pod/commits.ts`) makes one
commit per flush through the Files socket's `apply`. It sends no token (the
socket is pod-local); `ae-studio-tools` sets the author and the message
carries a `Co-authored-by` trailer per Room participant.

- **Cadence.** A quiet period of 60 s commits and 300 s caps continuous
  editing. The last leave forces a flush, and a stateless `{type:"flush", id}`
  forces one and is acked `flushed` or `flush-error` (the console's
  flush-before-build). Interim flushes hold markdown with pending agent marks;
  forced ones commit it. The baseline is the seed as the doc serializes it, so
  an unedited file never flushes.
- **One flush per Room at a time.** The debounced store, `flush`, the last
  leave, a retry and shutdown queue on the Room (`RoomState.flushing`), so a
  later flush diffs against the baseline the earlier one left.
- **An outage keeps the doc.** `FilesUnavailableError` covers every outage the
  pod reports: 5xx including `disk_full` and `aep_api_unavailable`,
  408/425/429, `not_fast_forward`, an unreachable socket, and a request past
  its 45 s deadline (longer than the pod's own 40 s budget, so the pod answers
  first). The committer keeps the doc and its baseline as they were, so the
  next flush retries the same diff, and the Room hears `flush-error` ("AE
  Studio is restarting — your edits are kept and will save shortly."). A
  last-leave flush that fails this way keeps the Room loaded and retries every
  5 s, doubling to 60 s, while nobody is in it, until it lands (the Room then
  unloads), someone rejoins, the Room unloads, or shutdown flushes it itself.
- **A verdict is reported.** `FilesDeniedError` surfaces with its message,
  except a write-rule refusal of one path (the pod's `path_invalid`: outside
  `specs/`, over 5 MiB). That change is set aside (not resent until the file
  changes), the rest of the flush is saved, and every `flush-warnings`
  restates the path as unsaved while it stays so. A Room whose only unsaved
  changes are refused ones may unload; it logs how many
  (`room_unloaded_with_refused {count}`), never which.
- **Conflicts** (a stale `baseSha`) refetch the bundle; the doc wins over the
  paths the Room changed, with at most 2 retries. Every path saved over a
  commit made outside the Room is reported (not a blob this Room committed
  itself). A path the Room undid while the bundle was read is re-seeded. Files
  changed outside the Room and unedited in it are re-seeded into the doc; files
  git gained outside the Room are never deleted.
- **`flush-warnings`.** After every successful apply the Room hears the
  stateless `{type:"flush-warnings", warnings:[{path, message}]}`: the pod's
  warnings plus the saved-over paths. An empty list clears the console's
  alert.
- **Shutdown.** On SIGTERM both Room listeners stop accepting, the Room
  sockets end and every update they delivered is applied (an edit typed after
  the sockets close is not saved: the console discards its doc on teardown and
  builds a fresh one for the next Room). Every loaded Room is then
  force-flushed, 8 at a time, and Rooms whose edits landed unload. All of it
  runs inside one 8 s budget, within the 10 s `ae-studio-tools` keeps its
  sockets open. The health listener closes last, then the process exits.
