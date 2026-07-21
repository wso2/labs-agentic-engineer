# Agent Grilling Support via Structured Question Cards — Design

**Date:** 2026-07-21
**Status:** Implemented — [#270](https://github.com/wso2/labs-agentic-engineer/issues/270), console ADR-0012 (this document records the design rationale; the code and ADR are the current truth)
**Scope:** Enable AEP's interactive spec agents to "grill" the user — a relentless,
one-question-at-a-time interview with recommended answers — rendered as native
Oxygen UI question cards in the console's agent chat.

## 1. Background: agent-driven UI landscape (research summary)

Four technologies were evaluated as of mid-2026:

| Technology | What it is | Verdict for AEP |
|---|---|---|
| **MCP Apps** (spec `2026-01-26`, Linux Foundation / Anthropic + OpenAI) | MCP extension: tools return HTML apps rendered in sandboxed iframes inside the host chat; postMessage JSON-RPC back-channel. Shipped in Claude, ChatGPT, VS Code Copilot. SDK: `@modelcontextprotocol/ext-apps`. | **Not for the in-console chat** — iframe UI is server-styled, the opposite of the Oxygen UI native-look requirement. **Future fit:** serve MCP Apps from `services/aep-mcp-server` so external hosts (Claude, ChatGPT) get rich AEP widgets (task status, validation dashboards). |
| **A2UI** (Google, v0.9.1, v1.0-rc) | Declarative JSON "blueprints"; the client renders them from its own trusted component catalog — no code execution, host-controlled styling. Transport-agnostic (A2A, AG-UI, SSE, MCP). Renderers: Lit, Angular, Flutter; **React renderer not shipped yet**. | Right philosophy (catalog + host styling = Oxygen native), wrong timing: no React renderer, spec still moving. **Revisit when v1.0 + React renderer land.** Interop exists with MCP (`application/a2ui+json`) if we later serve declarative UI outward. |
| **Thesys OpenUI** (openui.com) | Commercial generative-UI product with a proprietary compact wire format rendering to registered React components. | **Rejected:** vendor product, not a multi-party standard; its component model would fight the Oxygen UI mandate. (Note: unrelated namesakes exist — W3C open-ui.org, wandb/openui.) |
| **Purpose-built contract** (chosen) | A structured `ask_question` tool payload riding AEP's existing SSE `tool-call` frames, rendered by a hand-built Oxygen UI card. | **Chosen.** Smallest change, fully native, extensible; mirrors MCP's elicitation primitive conceptually. |

### Usefulness per SDLC phase (roadmap context)

Agent-driven UI seams in AEP, by devflow phase:

- **Spec/requirements** *(this design)* — structured clarifying questions / grilling interviews replace prose Q&A ping-pong. Highest value: every project passes through here, and ambiguity resolved early compounds downstream.
- **Design & plan gates** *(future)* — gate-approval cards in chat (summary + approve/reject/comment) wired to the Temporal devflow gates; same tool-call-as-UI pattern.
- **Dependency wiring** *(future)* — dynamic forms generated from `get_external_resource_schema` so users fill config instead of describing it.
- **Execution/validation** *(future)* — artifact preview cards embedding `packages/ui/*` views (wireframe, cell diagram, OpenAPI) inline in chat; validation-result dashboards. Outward-facing: MCP Apps on `aep-mcp-server` for external hosts.

## 2. Decision

Enable and upgrade the **existing dormant HITL path**. The codebase already contains:

- `ask_question` tool, implemented but unregistered
  (`services/agents/src/agents/main/tools/files.ts`, "Option B"), with a
  resolved-placeholder `execute` so transcripts never carry a dangling
  `tool_use`.
- The documented enable procedure: uncomment the registration in
  `buildFileTools` and add the paired `hasToolCall("ask_question")` stop
  condition at the main wiring call site (`run-turn.ts` doc comment).
- `awaiting-human` conversation status computed in
  `run-conversation-turn.ts` when a turn ends on that tool.
- The answer contract: the user's answer arrives as the next turn's plain
  user message.

This design upgrades the tool input from `{question}` to a structured payload,
enables the path, renders it natively, and makes the grilling skill its
flagship consumer. No new SSE event kinds, no BFF/OpenAPI changes, no
mid-turn blocking.

## 3. Architecture

### 3.1 Tool payload (`services/agents`)

Extend `askQuestionInputSchema`:

```ts
{
  question: string;                 // the clarifying question
  options: Array<{                  // 1–5 options
    label: string;                  // short display text
    description?: string;           // trade-off / implication
    recommended?: boolean;          // at most ONE option marked
  }>;
  multiSelect?: boolean;            // default false
}
```

A free-text "Other" affordance is always rendered client-side; no flag needed.
Zod refinements enforce option bounds and the single-`recommended` rule so a
malformed call fails validation and self-corrects via the existing
`tool-error` loop.

Registration: uncomment `[ASK_QUESTION]: askQuestionTool` in `buildFileTools`;
add `hasToolCall(ASK_QUESTION)` to the `stopWhen` array at the main wiring
call site. Keep the resolved-placeholder `execute`
(`{status: "awaiting_user_response", question}`).

### 3.2 Wire contract (`packages/agent-stream`)

Add `AskQuestionInput` to `src/contracts/sse-events.ts` as the wire source of
truth, with the same compile-time drift guard the file tools use against the
Zod schema in `tool.ts`. The payload rides the existing `tool-call` frame —
`AGENT_SSE_EVENT_TYPES` is untouched.

### 3.3 Console rendering (`apps/console`, agent-chat feature)

- **`QuestionCard.tsx`** (Oxygen UI): question text; radio list (or checkboxes
  when `multiSelect`) with the recommended option badged and pre-focused; an
  "Other…" free-text row; submit button. Read-only once answered or once any
  newer message exists. Visual language follows the existing tool cards in
  `AgentChatPanel`.
- **Fold mapper** (pure function, extending `toolGrouping.ts` or a sibling
  `questionFold.ts`): projects a `tool-call` frame with
  `toolName === "ask_question"` plus subsequent history into
  `{input, answered, chosen}`. Because state derives purely from the folded
  stream, answered/unanswered survives reloads and converges across tabs.
- **Answer transport:** submit posts the next turn with a deterministic
  plain-text serialization, e.g.
  `Answer to "<question>": <label>[, <label>…][ — <free text>]`.
  The user's choice renders as their chat bubble.
- **Composer in `awaiting-human`:** placeholder changes to
  "Answer the question above, or type a reply". Typing free text remains a
  first-class answer path — the card is a convenience, not a gate.

### 3.4 Grilling skill (agent skills library)

Add a `grilling` skill to the org skills library (the `_skills` snapshot
consumed via `loadSkill`), instructing the agent to:

- interview relentlessly until shared understanding, walking each branch of
  the design tree and resolving decision dependencies in order;
- ask via `ask_question` only, **one call per turn** (enforced structurally by
  the stop condition), with options and exactly one recommended answer;
- answer from the spec bundle itself when the question is answerable from
  files, instead of asking;
- not mutate files until the user confirms shared understanding.

The `ask_question` tool description points to the skill for interview
methodology.

## 4. Data flow (one grilling round)

1. User asks for grilling (or the agent hits ambiguity) → agent `loadSkill`s
   grilling → calls `ask_question` with the structured input.
2. Placeholder result resolves; `hasToolCall` stop condition ends the turn;
   manifest emits (empty for a chat-only turn); conversation status →
   `awaiting-human`.
3. Console folds the `tool-call` frame into a `QuestionCard`.
4. User clicks an option (or types free text) → console posts the next turn
   with the serialized answer.
5. Agent asks the next question, or proceeds (e.g. to file edits) once
   understanding is reached.

## 5. Error handling

- **Malformed tool input** (no options, >1 recommended): Zod fails →
  `tool-error` frame → model self-corrects, same loop as file-op errors. The
  renderer degrades to the generic tool card showing the question text if
  input doesn't parse client-side.
- **User ignores the card and types anything:** that message is the answer;
  the card flips read-only. The agent prompt says to interpret the next user
  message as the response.
- **Stale card (reload / second tab):** state derives from folded history;
  submission from a stale card is blocked by the newer-message check.
- **Severed stream mid-question:** no manifest → existing do-not-commit
  semantics; the persisted conversation still ends at the resolved question,
  so the card re-renders answerable on reload.
- **Runaway grilling:** `maxSteps` bounds a turn; the stop condition ends the
  turn at the first `ask_question` call, so one-question-per-turn is
  structural.

## 6. Testing

- **`services/agents`:** add the coverage the disabled path never had — a
  `run-conversation-turn` test scripting an `ask_question` call, asserting:
  turn ends after the call, transcript fully resolved (replay-safe), status
  `awaiting-human`, follow-up turn carries the answer as a user message.
  Schema unit tests for option bounds and single-recommended.
- **`packages/agent-stream`:** compile-time drift guard only.
- **`apps/console`:** component tests for `QuestionCard` states (single,
  multi, answered/read-only, malformed fallback); unit tests for the fold
  mapper; one `sse-cassette` recording of a grilling turn for playback
  integration.
- **Skill quality (follow-up, non-blocking):** an eval in the agents eval
  harness scoring one-question-per-turn with a recommended option.

## 7. Out of scope / future work

- Gate-approval cards, external-resource config forms, artifact preview
  cards — all reuse the tool-call-as-UI pattern established here.
- A2UI adoption review when v1.0 + a React renderer ship (migration path:
  swap the fold/renderer; the tool payload is already declarative).
- MCP Apps served from `services/aep-mcp-server` for rich AEP UI in external
  hosts.
