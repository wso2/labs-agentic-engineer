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

## Agents and the loop

Each agent is a `ToolLoopAgent` built in its own module by a factory, following the AI
SDK's building-agents guidance: `createMainAgent` (`src/agents/main/agent.ts`, the spec
editor), `createIssuesAgent` (`src/agents/issues/agent.ts`) and `createIssueAgent`
(`src/agents/issue/agent.ts`). A factory takes what the agent IS (its deps: tools,
instructions, or for a view's agent what it builds them from) and `AgentRunSettings` (`src/agents/run-settings.ts`), what `runTurn` decided for
this turn: the model built from the org's connection, the step cap, the output ceiling,
retries, provider options, `instructionsWrap` (the system prompt, with the cache
breakpoint when caching is on) and `prepareStep` (the rolling breakpoint). Every agent
is assembled by one helper, `buildToolLoopAgent`, which stops at the step cap, on an
accepted question call (`questionStop`) or on an accepted hand-off call (`handOffStop`),
and leaves an absent option off entirely.

`runTurn` takes `agentFor(run)` rather than tools and instructions: it pushes the user
message, which is what fixes the turn prompt's index for the rolling breakpoint, then
calls `agentFor` once and streams the agent it returns. `runConversationTurn` picks the
factory: the Issues view's turns get `createIssuesAgent`, which merges the turn's MCP
tools under its own and gates `create_issue`; an issue's thread gets `createIssueAgent`,
which keeps that issue's MCP tools and gates every write; every other turn gets `createMainAgent`
over the tool set it assembles (files or task-plan, then MCP, register draft and web
search).

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
| **The model is built per turn from the org's connection** (`X-Model-Key` + turn body `connection` and `model`; else Anthropic's own API on `AGENT_MODEL`) | the organization has one connection (format, URL, key, model) for every agent ([root ADR-0038](../../../docs/decisions/ADR-0038-an-organization-has-one-model-connection.md)), and a change applies from its next turn: aep-api resolves it on each turn, with its capabilities computed once there (`modelconn.CapabilitiesOf`), and the route builds the model from it (`buildModel(conn)` → `createModel`, one branch per format). The service checks a model id's shape only; whether the host serves it is the host's answer. `AGENT_MODEL` is only the default for a caller that names none (the playground, local dev) |
| **OpenCode Go requests carry the conversation ID** (`x-opencode-session`) | Go uses a stable session header for routing and prompt caching. The provider seam adds it and an AEP user agent only for the OpenAI-compatible `https://opencode.ai/zen/go/v1` endpoint; other connections keep their existing headers. |
| **Per-call options follow the format** (`modelProviderOptions(conn)`, `maxOutputTokensFor`, `modelCacheBreakpoint`) | Anthropic format: `effort`, except on Haiku 4.5 / Sonnet 4.5, which answer it with a 400 (a quirk of those ids, so the deny-list applies on this format only). OpenAI-compatible: the same level as `reasoning_effort`. The per-step output cap is `AGENT_MAX_OUTPUT_TOKENS` lowered to the connection's `outputLimit`; cache markers ride only a connection with `capabilities.promptCache` |
| **Capabilities decide, never the provider string** (`web_search` by `capabilities.webSearch`, attachments by `nativePdf` / `imageInput`) | an Anthropic-format model on another host reports the same SDK provider and cannot run Anthropic's server tool. `anthropic-server-tool` registers it; `ollama-api` registers a platform-executed `web_search` over `@aep/web-search` on the connection's own host and key; `none` registers nothing. A PDF off Anthropic's own API becomes a `text/plain` part under the same file name; an image is sent unless `imageInput` is `no`. A part the model cannot read (a scanned PDF, an image on a text-only model) is refused with a 400 naming it when the user attached it to this message (`fitAttachments`), and left out and named in the prompt when it is a reference document (`fitReferences`, before the shared attachment budget is applied), which is re-read every turn and would otherwise fail every turn of a project whose org moved to such a model |
| **History is cleaned only for turns another connection wrote** (`historyFor(messages, journal, {fingerprint, imageInput})`, fingerprint `format@host` on the journal entry) | reasoning and provider-executed tool calls replay only to the host that wrote them (an OpenAI-compatible host 400s on a server `web_search` with no tool message). The model is not in the fingerprint: Anthropic's API accepts one Claude model's signed thinking replayed to another (checked `claude-haiku-4-5` ↔ `claude-sonnet-5`, both ways), so a model change on the same host keeps the history and its cache. The one model-dependent part is an image: on a connection whose `imageInput` is `no`, every stored image, from any turn, becomes a short text naming the file, since a model without vision 400s the whole request over one. Turns from the current connection replay byte for byte, so the cache holds; the filter is deterministic, so the cleaned prefix caches again from the second turn after a switch. A copy is never the append target: the turn's tail is carried onto the stored transcript |
| **Failures the service can name end on one coded `error` frame** (`provider_limit`, `output_truncated`) | a 429 past a 5-minute `retry-after`, or a 5-minute streak of 429s, stops the turn at once instead of sleeping on a spent plan (short waits retry, `maxRetries` 6, and each one streams a `provider-wait` status frame naming the host, which the next frame supersedes); every 429 writes one scrubbed `model_provider_429` log line. aep-api stores the code on the failed turn (reason `agent-error`, `TurnStatus.code`). A step cut off by the output limit inside a file write fails the turn naming the file, after the transcript is saved and before any manifest, so the fold never commits a draft missing it |
| **`ask_question` / `ask_questions` Option B** (placeholder `execute()`, stop on an *accepted* question call) | structured HITL on the `files` set (console ADR-0012 / #270): a fully-resolved transcript (no `MissingToolResultsError`), turn ends `awaiting-human` only when the placeholder resolved; a call the schema rejects does not stop the turn — the model reads the error and retries — and the answer returns as the next turn's plain message |
| **Tool results carry no file content** (`OpOk` drops `newContent`) | echoing the file makes input scale file×edits (violates ADR-0001), and it is the only stale-able carrier |
| **Append-only divergence note** (FE `filesChangedExternally`; no `reconcile`) | rewriting history breaks the prompt-cache prefix |
| **Rolling prompt-cache breakpoint** (moved onto the newest message each step via `prepareStep`; the turn-prompt marker stays put) | a breakpoint fixed at the turn prompt freezes the cache boundary where the turn *started*, so every assistant/tool message the loop appends is re-prefilled uncached on every remaining step — waste quadratic in step count (measured: 750K uncached vs 445K cached input on one 20-step generation, one 70KB `loadSkill` result re-sent 19 times). The pinned marker is what the *next* turn reads from |
| **A flow's whole skill lineup is inlined up front** (`FLOW_SUPPORTING_SKILLS`, on the per-turn prompt) | the flow already names the guidance it walks, so the model's first act was a `loadSkill` batch: one model step, and ~70KB arriving as a tool *result* — landing after the turn prompt's cache marker, re-prefilled per step instead of read. Inlined, the same bytes ride inside the marked prompt: cached from step 1 and again next turn. Conditional members stay in (which components exist is decided *during* the turn, so there is nothing to condition on at compose time). Never the system prompt, whose prefix must stay byte-stable |
| **The INSTRUCTED skill is always inlined** (every non-chat instruction opens "Load the `<skill>` skill and follow it") | naming a skill and then waiting to be asked for it spends a whole model step on a body we already hold — measured at 3.8s on `/start`, 3.6s on a plan turn. Covers org-authored flows too, since resolution runs through the `SkillSource`, not this repo. Guidance a flow is CERTAIN to read therefore belongs in a skill rather than a `references/` file: references are not inlinable (ADR-0002) |
| **A file write settles at its own call** ([ADR-0004](./ADR-0004-a-write-settles-at-its-own-call.md)) | the SDK queues a step's tool calls and runs them all at `model-call-end`, so a batched design turn's first file had no verdict until the last file's body finished streaming — four completed documents shown as pending for minutes. A bundle op is a pure function of the bundle and the args, and the args close at `tool-input-end`, so it runs there and its `tool-result` rides its own `tool-call`; the ledger memoises per `toolCallId`, so the SDK's later `execute()` re-reads that verdict instead of re-applying the op |
| **SSE event types in `src/contracts/sse-events.ts`** | one shared definition for producer + playground, owned by the service; `OpResult` / tool-input types re-exported from the domain Zod schemas (no parallel copy) |

## Views

A **view** is a main-panel view of the console that owns an agent of its own. The turn
body's optional `view` (`VIEWS` in `@aep/agent-stream`; absent means the spec agent)
selects that view's agent in place of the spec editor:
`toolsetFor(turn, view)` returns `issues` for `view: "issues"`, `issue` for
`view: "issue"`, and otherwise derives `files`/`task-plan` from the turn. `runConversationTurn` runs an `issues` turn on
`createIssuesAgent` (`src/agents/issues/agent.ts`: the report classifier, the question
tools, the turn's MCP tools under them, Issues-agent instructions, no spec bundle), and
it stops on an accepted question call like every agent, so a "File this issue?" card
ends the turn awaiting the user. Adding a view is one entry in `VIEWS`, one tool set,
one `src/agents/<view>/` with its `agent.ts` factory and one entry in `VIEW_AGENTS`
(`src/agents/views.ts`); a view the main agent hands reports to also has one entry each
in `HAND_OFF_TOOLS` and `HAND_OFF_DESCRIPTIONS` (`src/agents/main/tools/hand-off.ts`).
Only the Issues view receives a hand-off and only its chat leaves the main chat a branch
note (`BRANCH_NOTE_VIEWS`); a view's agent works on no spec, so its prompt is the user's
message alone, with no divergence note and no branch note.

An **issue's thread** (`view: "issue"`) is the chat on one filed issue's card. The turn
also carries `issueNumber` (a positive integer: required for that view, a 400 on any
other), which `createIssueAgent` names in its prompt. Its tools are the question tools and,
from the turn's MCP block, the issue's own tools by contract name (`get_issue`,
`list_components`, `comment_issue`, `edit_issue`, `close_issue`, `reopen_issue`,
`hand_to_coding_agent`); anything else the server lists is left out. The MCP token names
the issue, so the tools act on it alone. Its turns carry no `outcome`.

The Issues agent files on one explicit answer. The prompt has it call a single
`ask_question` with the exact question `FILE_QUESTION` and the exact options `FILE_IT`
(flagged `recommended: true`) and "Change it". `gateCreateIssue` (`filing-gate.ts`) wraps
the MCP tool named `create_issue`: unless the turn's instruction is the single-answer
serialization of `FILE_IT` to `FILE_QUESTION` (a batched answer never qualifies), the tool
refuses with an error that tells the model what to ask. Once confirmed, the tool executes
at most once in the turn; any later call is refused, so injected text in an earlier tool
result, or a retry after a timeout that did file, cannot file twice, and a failed first
attempt is reported to the user rather than retried silently.

Every write an issue's agent makes waits the same way, generalised
(`src/agents/confirmation.ts`: `answeredWith`, `refusing`, `onceAsShown`, which the filing
gate uses too). `CONFIRMATIONS` (`issue/confirm-gate.ts`) gives each write tool its question
and option: "Post this comment?" → "Post it", "Apply this edit?" → "Apply it", "Close this
issue?" → "Close it", "Reopen this issue?" → "Reopen it", "Hand this to the coding agent?"
→ "Hand it over", each with "Not now". `gateWrites` lets the one tool the turn's single
answer confirms run once and refuses every other write with an error naming its question;
`get_issue` and `list_components` are never gated. The prompt reads the issue first,
drafts each change, asks its question, and before a hand-off asks which component (the
options from `list_components`); a "Deploy a version first" answer is relayed as is.

A gate binds the tool AND its arguments to the card the user answered, not only which
write runs. The agent puts the exact change in the confirm option's `description`, in one
canonical rendering per tool (`describeChange` beside `CONFIRMATIONS`: the comment; "Title:
…" and/or "Body:\n…"; the reason; "Component: …"; nothing for a reopen; `describeFiling`
for `create_issue`: title, kind and body). An argument the rendering does not name is shown
too, so nothing reaches the tool unshown. In the answer turn `runConversationTurn` hands the
agent the last accepted `ask_question` input from the history the service stored before
the turn (`lastAskedQuestion`), never the instruction or this turn's model output. The
confirmed tool runs only when that card asked its own question and its confirm option's
description equals the call's rendering (CRLF→LF, trimmed); otherwise the call is refused
("Not done: this is not the change the user confirmed…"), which uses up nothing, as the
confirmed change is still the only one that can run. No card in the history (an answer
typed by hand) refuses too. A note on the answer does not change what was confirmed: the
prompts have the agent ask again with the revised text.

The report classifier (`classify_report`, Jev) never blocks a turn. Below 0.8 confidence,
or for a `question`, it asks the agent to clarify; a missing key, a non-2xx response, a
malformed body, a network error or the 5 s timeout all yield kind `unknown` with
`needsClarification`, and the agent asks the user which kind the report is.

## Prototype write gate

A turn's file tools are built with `gates.prototypeRender` (`buildFileToolSet`),
the render check in `src/prototype/render-check.ts`: `@wso2/prototype-kit/check`'s
`checkPrototypeFiles` on the Oxygen theme's `check-runtime.js`, resolved once at
import with the kit's `resolveTheme`. A write that leaves a whole prototype pair
(`prototype.tsx`, or `prototype.json` beside an existing source) goes through
`agent-stream`'s `writeWithRenderCheck`, which draws it in an isolated Node child
(permission model, 15 s limit) asynchronously: the event loop serves other
conversations meanwhile. The write ledger queues the turn's later writes behind
a pending verdict, and `tapWrites` holds later frames, so call order and wire
order are unchanged; the turn drains the tap before its manifest. The image therefore builds the kit and the theme (`dist` runtimes) and
needs Node 22. The static stages and the `INVALID_PROTOTYPE` code with its
`findings` are in `@aep/agent-stream` (its README, Write gates).
