# ADR-0021 — The fold applies accepted writes only

**Status:** Accepted · **Refines:** the D14 fold-parity contract (`services/aep-api/internal/platform/agentfold`)

## Context

An agent turn streams file mutations. The agents service applies each one to
its in-memory `FileBundle`, behind a ladder of write gates, and the verdict —
accepted or rejected — goes back to the model as the tool result. aep-api does
not trust that bundle: it replays the same stream through its own fold and,
at the end, checks that the two agree byte for byte (the manifest gate). A
divergence rejects the turn.

The fold reproduced parity by porting every gate to Go. That stopped being
true without anyone deciding it: the `openapi.yaml` and `security.json` gates
exist only on the TS side. When such a gate rejects a write and the model
retries, the fold — having no gate — applies the rejected write first, then
refuses the retry as `ALREADY_EXISTS`, and the manifest check fails a healthy
turn. #687 adds a third TS-only gate (the design diagrams), and its parser is
deliberately not one to port twice.

## Decision

The fold applies a mutation only once the stream's own verdict for it says
the TS bundle accepted it. The write ledger already emits that verdict — a
`tool-result` carrying the `OpResult` — right behind every mutation
`tool-call`; the fold holds the call until the verdict arrives, applies it on
`ok: true`, and drops it otherwise. A verdict that is absent or malformed
drops the write too: the manifest gate then fails the turn loudly rather than
the fold guessing.

Gates live where the tool calls are, in `@aep/agent-stream`, and nowhere else
is required. The Go ports that already exist (YAML, `design.json`,
`wireframes.dsl`) stand as defence in depth; no new port is owed.

## Consequences

- Parity is by construction, for every gate present and future: a rejected
  write can never put the fold ahead of the bundle.
- "Independent replay" narrows to *independent replay of accepted ops*. The
  manifest hash still guards every applied byte; what the fold gives up is
  re-judging a write the bundle already refused — a judgment the model never
  acted on.
- A turn whose stream dies between a call and its verdict loses that write in
  the fold and fails the manifest gate — the safe failure the contract
  already prefers.
- Test fakes of the agents stream must emit the verdict frame with each call,
  as the real stream does.
