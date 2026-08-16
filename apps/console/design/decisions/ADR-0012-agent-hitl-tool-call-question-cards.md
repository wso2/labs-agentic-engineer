# ADR-0012 — Agent HITL via tool-call question cards

**Status:** Accepted (2026-07-23) · **Issue:**
[#270](https://github.com/wso2/labs-agentic-engineer/issues/270) · **Backend
handshake:** [#271](https://github.com/wso2/labs-agentic-engineer/issues/271)

## Context

Spec agents need to ask users structured questions — grilling interviews,
clarifications — with options and a recommended answer, rendered natively in
the console's agent activity stream. External agent-driven-UI standards were
evaluated: MCP Apps (sandboxed iframes, server-styled), A2UI (declarative
catalog rendering, no React renderer yet, pre-1.0), Thesys OpenUI (commercial,
proprietary format). None fit the Oxygen-native, ships-today requirement.

## Decision

**Agent-to-user interactive UI is carried as tool calls on the existing SSE
stream and rendered as native Oxygen UI cards.** Two tools cover the shapes a
question can take, one convention:

1. The agent expresses the interaction as a **tool call** — `ask_question`
   (one question) or `ask_questions` (a batch answered as a form). The Zod
   input schema is the structural contract: options, at most one
   `recommended`, unique labels, `multiSelect`.
2. The turn **ends at the call** (`hasToolCall` stop condition, files toolset
   only); the tool's `execute` resolves a placeholder so transcripts replay
   cleanly; the conversation enters `awaiting-human`.
3. The payload rides the **existing `tool-call` frame** — no new SSE event
   kinds; `@aep/agent-stream` owns the wire tool names, input types, and the
   answer serializers (`buildAnswerInstruction` / `buildAnswersInstruction`),
   so producer (agents) and renderer (console) can't drift.
4. The console **folds** the stream into a uniform `question` message carrying
   a list of questions (single = length 1); a single `QuestionCard` renders
   one question compactly or several as a form. Answered-ness derives from the
   log (`answerableQuestionIds`) — reload / multi-tab safe — and cards are
   reconstructed from history on rehydrate.
5. The user's response returns as the **next turn's plain-text instruction**
   (`Answer to "<q>": …` for one, an `Answers:` bullet list for a batch) —
   never a new API channel. Free text in the composer is an equally valid
   answer path; a card answer is recorded only after the turn actually starts,
   so a failed send never strands an answer behind a read-only card.
6. Tools of this kind are **prompt-restrained** ("only when you cannot proceed
   safely"); skills (e.g. grilling) loosen that for interview modes. HITL is
   scoped to the **files toolset**, so an MCP-discovered tool of the same name
   can never stall a headless task-plan turn.

## Rejected

- **MCP Apps for in-console chat** — iframe UI is server-styled; violates the
  Oxygen-native mandate. (Still the candidate for serving AEP UI to *external*
  MCP hosts via `aep-mcp-server`.)
- **A2UI adoption now** — right philosophy, but no React renderer and a
  pre-1.0 spec. Revisit at v1.0 + React renderer; this convention's declarative
  tool payloads keep that migration a renderer swap.
- **Thesys OpenUI** — commercial vendor format; conflicts with the Oxygen
  component mandate.
- **A structured answer field on `TurnRequest`** — contract churn across
  BFF/callers for no gain over readable plain text.

## Delivery

Front-end (#270): the wire contract additions in `@aep/agent-stream`, the
`withGrillingInterview` opt-in in `@aep/contracts/prompts` (since retired —
`/start` carries the interview flow and the server owns expansion), and the
console rendering — mock-verified. The agents-service tool registration + stop
condition, and the platform grilling skill, land via the backend handshake
(#271); until then real-mode cards don't render (the agent never calls the
tools) and the FE is mock-complete by design.

## Amendment — grilling sessions (2026-08-16, #486)

Multi-round grilling sessions ([#477](https://github.com/wso2/labs-agentic-engineer/issues/477))
run as a sequence of ordinary question forms, so points 1–5 hold unchanged.
The only contract growth is an optional `session` input on `ask_questions`: a
short `title` plus the full area checklist (`{ name, state: done | now |
todo }`), restated every round with moved states and stable names.

- **Session chrome is decoration, never a gate.** The console parses `session`
  separately from the questions (`parseSessionInfo`), so a malformed checklist
  costs the header and never the form. One-form interviews omit the field and
  render exactly as before.
- **`session` precedes `questions` in the schema** so the streaming extractor
  (`extractStreamingSession`) paints the session's scope before the first
  question object closes.
- **The finish valve replaces the blanket skip text.** "Finish — use
  recommendations" serializes through `buildFinishInstruction`: answers
  already given ride as decisions quoted per question, unanswered questions
  are listed for recommended answers tagged `*assumed*`. That split is the
  decision-to-question link the closing summary's "N asked · M assumed"
  derives from — still plain text on the next turn (point 5), still no new
  channel, and a partially answered round loses nothing.
- **Park/resume needs no new machinery.** A round is the shared room entry
  (#430 D5), so navigating away parks it and returning re-renders the same
  entry with co-edited answers intact; a re-mirror computed before the session
  parsed never strips the checklist.
- **The summary card is a prose convention, not a frame.** A closing message
  leading with the `Session summary` marker renders as a bordered card;
  without the marker it is ordinary narration.
