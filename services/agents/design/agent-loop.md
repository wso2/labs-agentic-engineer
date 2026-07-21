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
| **`ask_question` Option B** (placeholder `execute()`; registered in the `files` set, structured options payload, `hasToolCall` stop — #270) | fully-resolved transcript (no `MissingToolResultsError`), uniform resume; the answer arrives as the next turn's user message |
| **Tool results carry no file content** (`OpOk` drops `newContent`) | echoing the file makes input scale file×edits (violates ADR-0001), and it is the only stale-able carrier |
| **Append-only divergence note** (FE `filesChangedExternally`; no `reconcile`) | rewriting history breaks the prompt-cache prefix |
| **SSE event types in `src/contracts/sse-events.ts`** | one shared definition for producer + playground, owned by the service; `OpResult` / tool-input types re-exported from the domain Zod schemas (no parallel copy) |
