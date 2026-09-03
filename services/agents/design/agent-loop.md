# Agent loop

The `@aep/agents` main agent: an interactive, streaming spec editor. Tool-edit
rationale is [ADR-0001](./ADR-0001-anchored-file-edits.md); tool *semantics* live in
`src/agents/main/{bundle,tool}.ts` (the source of truth).

## Shape

A user sends a natural-language instruction; the agent **streams proposed changes**
token-by-token (markdown + YAML + OpenAPI). **The service writes no files** —
persisting an accepted doc is a separate commit service, so "which document is
current" is a caller concern and accept/edit/save happen out of band.

**One turn = one HTTP request.** `POST /conversations/:id/turns` runs one turn and
streams raw `StreamPart` frames until `[DONE]`, then the socket closes — no
long-lived connection, no mid-turn client→server channel. A follow-up re-enters as
the *next* turn, so resume is free and "awaiting-human" is just the gap between two
requests.

**One stream, symmetric consumers.** The Express route is the producer; the playground
(and a future browser client) are consumers: `toChange` projects each `tool-result`
into a reviewable change, and `applyToolCall` folds the streamed calls through the
canonical `FileBundle` ops to reconstruct file state — no second matcher.

## Locked decisions

| Decision | Why |
|---|---|
| **Server-side `execute()`** (no execute-less tools) | rich `OpResult` self-correction must stay inside one `agent.stream()` call |
| **`runTurn` writes nothing** (`ai`-only imports) | file-applying is a consumer concern; one stream shape, no second code path |
| **`ModelMessage[]` persisted verbatim** (not `UIMessage[]`) | wire is raw `StreamPart`, loop is `ModelMessage`-native → zero-conversion resume |
| **Whole-aggregate save, last-write-wins** | history is append-only, so the saved array only grows |
| **Caller-supplied id + lazy create** | the BFF owns its id namespace; resume is free |
| **Raw `StreamPart` on the wire** (no envelope) | FE+BE ship together; `tool-result` already carries everything `toChange` needs |
| **Full `files` snapshot every turn** | the service touches no repo/disk; the snapshot is the single source of file truth |
| **Human-between-turns** (`stopWhen` only, no approval pause) | restart-safe, persistence-aligned, no long-lived per-human promises |
| **`ask_question` / `ask_questions` Option B** (placeholder `execute()`, `hasToolCall` stop) | structured HITL on the `files` set (console ADR-0012 / #270): a fully-resolved transcript (no `MissingToolResultsError`), turn ends `awaiting-human`, the answer returns as the next turn's plain message |
| **Tool results carry no file content** (`OpOk` drops `newContent`) | echoing the file makes input scale file×edits (violates ADR-0001), and it is the only stale-able carrier |
| **Append-only divergence note** (FE `filesChangedExternally`; no `reconcile`) | rewriting history breaks the prompt-cache prefix |
| **Rolling prompt-cache breakpoint** (moved onto the newest message each step via `prepareStep`; the turn-prompt marker stays put) | a breakpoint fixed at the turn prompt freezes the cache boundary where the turn *started*, so every assistant/tool message the loop appends is re-prefilled uncached on every remaining step — waste quadratic in step count (measured: 750K uncached vs 445K cached input on one 20-step generation, one 70KB `loadSkill` result re-sent 19 times). The pinned marker is what the *next* turn reads from |
| **A flow's whole skill lineup is inlined up front** (`FLOW_SUPPORTING_SKILLS`, on the per-turn prompt) | the flow already names the guidance it walks, so the model's first act was a `loadSkill` batch: one model step, and ~70KB arriving as a tool *result* — landing after the turn prompt's cache marker, re-prefilled per step instead of read. Inlined, the same bytes ride inside the marked prompt: cached from step 1 and again next turn. Conditional members stay in (which components exist is decided *during* the turn, so there is nothing to condition on at compose time). Never the system prompt, whose prefix must stay byte-stable |
| **The INSTRUCTED skill is always inlined** (every non-chat instruction opens "Load the `<skill>` skill and follow it") | naming a skill and then waiting to be asked for it spends a whole model step on a body we already hold — measured at 3.8s on `/start`, 3.6s on a plan turn. Covers org-authored flows too, since resolution runs through the `SkillSource`, not this repo. Guidance a flow is CERTAIN to read therefore belongs in a skill rather than a `references/` file: references are not inlinable (ADR-0002) |
| **A file write settles at its own call** ([ADR-0004](./ADR-0004-a-write-settles-at-its-own-call.md)) | the SDK queues a step's tool calls and runs them all at `model-call-end`, so a batched design turn's first file had no verdict until the last file's body finished streaming — four completed documents shown as pending for minutes. A bundle op is a pure function of the bundle and the args, and the args close at `tool-input-end`, so it runs there and its `tool-result` rides its own `tool-call`; the ledger memoises per `toolCallId`, so the SDK's later `execute()` re-reads that verdict instead of re-applying the op |
| **SSE event types in `src/contracts/sse-events.ts`** | one shared definition for producer + playground, owned by the service; `OpResult` / tool-input types re-exported from the domain Zod schemas (no parallel copy) |
