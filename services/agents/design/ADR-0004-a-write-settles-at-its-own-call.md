# ADR-0004 — A file write settles at its own call, not at the step's tail

## Context

A design turn writes several files in ONE step: the model emits five parallel
`addFile`/`editFile` calls in a single assistant message ("the component
design.json files, openapi.yaml, wireframes.dsl, security.json and the
validation criteria — all independent, in one batch"). That batching is
deliberate: it is one model call instead of five.

The AI SDK does not execute a tool when its call arrives. It queues every call
of a step and runs them all at `model-call-end` — after the whole assistant
message has streamed (`ai@7`, `runToolsTransformation`). So the first
`tool-result` of a batched step lands only after the LAST call, and file 1's
verdict waits on file 5's body.

Consumers had already been designed around half of this. `tool-input-end` is the
per-call signal that one file's body is complete (for a file tool the arguments
ARE the body), so the console stops that card's spinner there. But the *verdict*
— did the bundle accept the write? — is a separate fact, and one that must never
be guessed: a write can be refused (ALREADY_EXISTS, NOT_UNIQUE, INVALID_YAML, a
component-design or wireframe gate), and painting a success tick on a refused
write is a lie the user acts on. So the card held a verdict-less pending ring
until the result came.

On a batched step that ring was the state of every finished file for the whole
batch. Four completed documents sat under a grey pending ring for minutes while
the fifth streamed, which reads as a stall — the exact opposite of what had
happened. The same delay held back the spec rail's per-file status and the error
text of a rejected write.

The verdict, though, never depended on anything the SDK was waiting for. A
bundle op is a pure function of the bundle and the call's arguments, and the
arguments are complete at `tool-input-end`.

## Decision

**A file write is applied and reported at its own `tool-input-end`, and its
`tool-result` rides that call's own `tool-call`.**

`services/agents/src/agents/main/tools/write-ledger.ts` owns this. The turn's
`WriteLedger` is created with the `files` tool set (`buildFileToolSet`), so the
tools and the ledger cannot be wired apart, and `tapWrites` projects it over the
turn's `onEvent` in `runConversationTurn`.

What keeps it honest:

- **One apply per `toolCallId`.** The ledger memoises the `OpResult`; the SDK's
  later `execute()` returns the SAME verdict rather than re-applying. A second
  `addFile` would answer ALREADY_EXISTS and the model would read a failure for a
  write that succeeded.
- **The tool's own `inputSchema` validates the streamed args.** A call the SDK
  would reject as invalid is never applied early, and its frames stay exactly as
  the SDK produced them.
- **Exactly one `tool-result` per call on the wire.** A call the ledger settled
  suppresses the SDK's duplicate. A call it did not settle — a severed stream, a
  turn with no tap, a `task-plan` turn — is forwarded untouched, which is what
  makes every other path byte-identical to before.
- **Order is unchanged.** The ops still apply in call order (a provider
  serialises the tool_use blocks of one message); only the wall-clock moment
  moves earlier.

The consumers keep their two-fact split: `tool-input-end` flips the card to
"body written", the verdict sets the tick. The gap between them is now one
frame instead of a whole batch, but it is still a gap, and a rejected write
still lands as an error rather than a tick.

## Consequences

- The console ticks each file as it lands, and reports a refused write mid-batch
  with its fix instruction, without any change to how it folds the stream.
- The doc writer (`StreamingDocWriter`) deliberately keeps observing the SDK's
  own frames rather than the re-projected wire: its optimistic preview is still
  finalized — or rolled back — by the authoritative `execute()`, so a severed
  turn's preview is dropped exactly as before.
- A turn that dies between an input-end and the SDK's execute leaves the bundle
  holding an op the transcript has no result for. Safe by construction: such a
  turn emits no manifest, and D14 refuses to commit a turn without one.
- The wire's frame ORDER is now this service's contract, not the SDK's. Both
  halves are pinned in `services/agents/test/frame-order.test.ts` — the raw SDK
  behaviour and what we emit over it — because the second only makes sense while
  the first is still true. If an SDK upgrade starts flushing results per call,
  the ledger's suppression is what keeps one result per call.
