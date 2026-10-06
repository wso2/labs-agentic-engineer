# Turn runtime

Every design-agent turn runs in this container, in memory, on behalf of the
org's AE Studio pod. Why the work lives in the org's pod is
[ADR-0040](../../../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md);
who may call it is
[ADR-0041](../../../../../../docs/decisions/ADR-0041-ae-studio-checks-platform-idp-tokens-itself.md).
This note is the turn's lifecycle: how it starts, who may watch it, what it
leaves behind and how it ends. The memory bound of every collection named
here is in [pod-memory-bounds.md](pod-memory-bounds.md).

| Piece | Code |
|---|---|
| Browser contract (`/v1`) | `packages/contracts/api/ae-design-agent/v1/openapi.yaml` |
| Turn socket contract | `packages/contracts/sockets/ae-studio/turn/openapi.yaml` |
| The one start path | `src/turns/start-turn.ts` (`TurnStarter`) |
| The lock, the stream end, the record | `src/turns/turn-desk.ts` (`TurnDesk`) |
| Replay | `src/turns/replay-buffer.ts` |
| Threads | `src/conversations/thread-book.ts`, `marketplace-book.ts` |
| Usage hand-off | `src/usage/outbox.ts` |
| Shutdown | `src/pod/shutdown.ts` |

## Two callers, one start path

- **The console** calls `/v1` with the user's Platform IdP token. A browser
  turn is `kind: browser`.
- **`ae-studio-tools`** calls the Turn socket (`AE_TURN_SOCKET`) for the
  turns `aep-api` asks for: the project kickoff (`kind: start` on the socket,
  `kickoff` in the record) and a Plan. The mount is the gate; no request
  carries a token.

Both go through `TurnStarter`. Before a turn is accepted it checks, in order:
the pod is not shutting down, the org has a model key, the instruction is
usable, the project resolves (the MCP socket's project lookup, which also
writes the snapshots), the snapshots and attachments can be read, no turn
runs on the scope, and the thread admits the send. A refused send takes no
lock.

The pod parses `/<skill>` flow commands (`src/turns/start-spec.ts`) into a
`TurnSpec`; callers send the instruction verbatim and never compose prompt
text ([ADR-0003](ADR-0003-turn-composition-lives-here.md)).

- **Kickoff** is `/start` on the project's current thread, in the Room,
  credited to the user `aep-api` names.
- **Plan** runs the task-plan toolset on a throwaway conversation (no Room, no
  thread), dropped when the turn ends.

## Start and watch are separate

`POST …/turns` answers `202 {turnId}` and the turn runs detached from the
request: a closed tab does not stop it. Watchers attach to its replay buffer.

| Op (`/v1`) | Answer |
|---|---|
| `GET /projects/{p}/conversations/current` | the project's current thread, created lazily |
| `POST /projects/{p}/conversations` | rotate: `201` with the new thread |
| `GET /projects/{p}/conversations/{c}/messages` | rehydrate; `[]` for the current id before its first turn |
| `POST /projects/{p}/conversations/{c}/turns` | `202 {turnId}`, or `409` (below) |
| `GET /projects/{p}/turns/active` | `200 TurnStatus` or `204` |
| `GET /projects/{p}/turns/{t}` | `TurnStatus`, `404` past retention |
| `GET /projects/{p}/turns/{t}/stream?from=N` | SSE replay from frame `N`, then the live tail |

- **The stream.** Each frame is `id: <index>` and `data: <part>`;
  `: keep-alive` every 15 s. `from` wins over a `Last-Event-ID` header. It
  ends with `turn-completed {}` or `turn-failed {reason, message?, code?,
  host?, resetAt?}`, then `[DONE]`. `reason` is `agent-error` (with `code`
  `provider_limit` or `output_truncated` when it can be named),
  `stream-died`, `shutdown` or `internal`. A running turn that overflowed its
  buffer answers `409 replay_truncated` until it ends.
- **`TurnStatus`** carries `kind` (`browser | kickoff | plan`) and `flow`
  (the `/skill` token, `""` for a plain chat turn). The console reads agent
  activity from `turns/active` here, never from `aep-api`.
- **Retention.** A buffer stays attachable 120 s after the turn ends; a
  `TurnStatus` is kept for the last 20 finished turns of its scope or one
  hour, whichever is shorter; then `404`. A turn is capped at 30 minutes.

## One lock per project

`TurnDesk` is the only turn lock: one running turn per scope. A project is
one scope for browser, kickoff and Plan turns alike, so a Plan blocks chat on
that project and a chat blocks planning. A plan read against a design that is
changing underneath it would plan the wrong Tasks.

- A busy scope answers `409 turn_in_progress` with `activeTurnId`: in the
  `TurnConflict` body on `/v1`, and on the Turn socket, where `aep-api` reads
  it as `ErrTurnInProgress` and Temporal retries.
- A send to a thread that is no longer current answers `409
  conversation_rotated`. The thread also rotates itself before a send once
  its context passes 80 % of the connection's declared window.
- **Reattach, never restart.** A server-started turn carries the caller's
  `turnId`. An id the desk still knows (running, or finished and retained)
  reattaches and starts nothing. `aep-api` derives the kickoff's id as a
  uuidv5 of `org/project` (`services/aep-api/internal/spec/kickoff.go`), so a
  retried kickoff reattaches; once the pod has forgotten it, `aep-api`'s
  finished-turn ledger check refuses a second kickoff.
- The lock lives and dies with the process. `ae-studio`'s `Recreate` rollout
  keeps one pod per org, so there is never a second desk to disagree with.

## The usage record

Every finished turn, Plan turns included, hands its whole record to the
usage outbox once: `turnId`, `project` (absent on a marketplace turn),
`conversationId`, `kind`, `flow`, `status`, `reason`, `code`, `baseRef`,
`skillsRef`, `startedAt`, `finishedAt`, `author`, `model`, `modelHost`, the
four token counts and `contextTokens`. The schema is `TurnRecord` in
`packages/contracts/sockets/ae-studio/mcp/openapi.yaml`.

- The outbox posts each record to `ae-studio-tools` (`POST /turn-usage` on
  the MCP socket), in order, retrying every 2 s while the socket is down. It
  holds at most 200; past that the oldest is dropped, and a record the socket
  refuses for good is dropped too. Both drops log `usage.dropped`.
- `ae-studio-tools` coalesces records for 5 s into one `record-turn-usage`
  call to `aep-api`, so a record lands within seconds. That matters because
  `aep-api` reads `agent_turns` for the status poll, the build gate's design
  baseline and the kickoff check. `agent_turns` is the finished-turn ledger:
  idempotent on the turn id, priced at ingest.
- "Running" never leaves the pod. `aep-api` holds only finished turns.
- A record still queued when the pod dies is lost. The window is seconds.

## Shutdown

Kubernetes signals the pod's three containers at once, and the pod's
`terminationGracePeriodSeconds` is 30. On SIGTERM this container:

1. Refuses new turns: `/v1` and the Turn socket answer `503 shutting_down`.
2. Ends every running turn at once: browsers read `turn-failed {reason:
   shutdown}`, the Turn socket `result {status: failed, code: shutdown}`, and
   Temporal retries the same `turnId` on the new pod. This returns within 2 s
   even when a run ignores its abort.
3. Drains the usage outbox. Steps 2 and 3 end at most 8 s after SIGTERM,
   inside the 10 s that `ae-studio-tools` keeps its MCP socket open after its
   own SIGTERM.
4. Closes the listeners.

## Marketplace conversations

Marketplace chat has its own routes under `/v1/marketplace/…`, with the same
shapes and no project.

- A conversation belongs to the user who created it: every call must carry
  the same JWT `sub`, and to anyone else the conversation does not exist.
- The lock is per conversation, so two users can register at once.
- There is no Room and no project lookup. The turn reads only the org skills
  snapshot, which the MCP socket's `GET /skills` writes.
- Starting over is a new conversation, not a rotation.
