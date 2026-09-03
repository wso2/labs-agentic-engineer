# ADR-0025 — Fire-and-forget UI tool calls

**Status:** Accepted (2026-08-28) · **Issue:**
[#576](https://github.com/wso2/labs-agentic-engineer/issues/576) · **Amends:**
[ADR-0012](ADR-0012-agent-hitl-tool-call-question-cards.md)

## Context

ADR-0012 established tool-call-as-UI: agent-to-user interactive UI rides as
tool calls on the existing SSE stream, rendered as native cards. Every such
tool so far is HITL — the turn **ends at the call** (`hasToolCall` stop) and
the conversation waits on a human.

A plan declaration (#576) is the same shape of thing — a structured payload
the console renders — but the agent is not asking anything: it declares what
it is about to write and keeps working. Ending the turn there would defeat
the point.

## Decision

**The tool-call-as-UI convention carries two classes of tool, distinguished
by what the turn does next:**

1. **HITL** (`ask_question`, `ask_questions`) — the turn ends at the call;
   the conversation enters `awaiting-human`. As per ADR-0012.
2. **Fire-and-forget** (`declare_plan`) — the tool's `execute` resolves
   immediately with an acknowledgement placeholder, the call is **not** a
   stop condition, and the turn continues. The payload is UI state, not a
   request; nothing awaits an answer.

Everything else in ADR-0012 applies unchanged to both classes: the payload
rides the existing `tool-call` frame (no new SSE event kinds),
`@aep/agent-stream` owns the wire tool names and input types so producer and
renderer can't drift, rendering is reconstructed from replayed history on
rehydrate, and the tools are scoped to the files toolset.

A fire-and-forget call renders wherever its payload belongs (the plan renders
in the spec rail) plus as an ordinary activity step in the chat, like any
other tool call — it never renders as a card awaiting input.

## Delivery

The console half ships first, mock-verified, exactly as ADR-0012's did: the
wire contract in `@aep/agent-stream`, the fold, the rail rendering, and typed
MSW frames. **The producer does not exist yet** — no tool is registered in the
agents service, so the Zod schema and its `Equal<>` drift guard, and the
`design` skill's prompt change, arrive with the backend handshake. Until then
no real turn declares anything and the rail degrades to its pre-#576 behavior,
which is also the permanent path for any skill that declares nothing.

## Rejected

- **A new SSE event kind for ambient UI state** — ADR-0012 rejected new
  event kinds for questions; the same holds here. One transport, one
  ownership point for the wire types.
- **Making the plan an answer-less question card** — abusing the HITL stop
  condition would pause the turn and demand a click that means nothing.
