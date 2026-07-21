# ADR-0012 — Agent HITL via tool-call question cards

**Status:** Accepted (2026-07-21) · **Issue:**
[#270](https://github.com/wso2/labs-agentic-engineer/issues/270) · **Spec:**
`docs/superpowers/specs/2026-07-21-agent-grilling-question-cards-design.md`

## Context

Spec agents need to ask users structured questions (grilling interviews,
clarifications) with options and a recommended answer, rendered natively in
the console chat. External agent-driven-UI standards were evaluated: MCP
Apps (sandboxed iframes, server-styled), A2UI (declarative catalog
rendering, no React renderer yet, pre-1.0), Thesys OpenUI (commercial,
proprietary format).

## Decision

**Agent-to-user interactive UI is carried as tool calls on the existing SSE
stream and rendered as native Oxygen UI cards.** The convention any future
interactive surface must follow:

1. The agent expresses the interaction as a **tool call** (`ask_question`
   first; approval cards, config forms later) whose Zod input schema is the
   structural contract — options, at most one `recommended`, `multiSelect`.
2. The turn **ends at the call** (`hasToolCall` stop condition); the tool's
   `execute` resolves a placeholder so transcripts replay cleanly; the
   conversation enters `awaiting-human`.
3. The payload rides the **existing `tool-call` frame** — no new SSE event
   kinds; `@aep/agent-stream` holds the wire type with a drift guard.
4. The console **folds** the stream into card state (answered/unanswered
   derives from history — reload/multi-tab safe) and renders the card only
   on the complete `tool-call` frame.
5. The user's response returns as the **next turn's plain-text
   instruction** (`Answer to "<question>": <label>[, <label>] — <note>`) —
   never a new API channel. Free text in the composer is always an equally
   valid answer path.
6. Tools of this kind are **always registered, prompt-restrained** ("only
   when you cannot proceed safely"); skills (e.g. grilling) loosen the
   restraint for interview modes. No per-turn HITL flags.

## Rejected

- **MCP Apps for in-console chat** — iframe UI is server-styled; violates
  the Oxygen-native mandate. (Remains the candidate for serving AEP UI to
  *external* MCP hosts via `aep-mcp-server`.)
- **A2UI adoption now** — right philosophy (trusted-catalog declarative
  rendering), but no React renderer and a pre-1.0 spec. Revisit at
  v1.0 + React renderer; this convention's declarative tool payloads keep
  that migration a renderer swap.
- **Thesys OpenUI** — commercial vendor format; conflicts with the Oxygen
  component mandate.
- **Structured answer field on `TurnRequest`** — contract churn across
  BFF/callers for no v1 gain over readable plain text.
