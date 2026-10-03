# AGENTS.md — components/dataplane/ae-system-project/ae-studio/ae-design-agent (`@aep/ae-design-agent`)

TS interactive spec agents (Vercel AI SDK), the design agent of an org's AE
Studio pod. Seeded with ONE agent: the **main file-mutation agent**
(prompt-driven add/edit/remove over a spec bundle), served on the pod's `/v1`
edge: a turn starts detached and watchers attach to its SSE stream. The
runtime **writes no files** — a project turn's edits go to the Room, whose
committer saves them.

## Design

The client-side consumption surface — wire types (SSE events, `OpResult`,
`*Input`, `Change`, `TurnRequest`), the `FileBundle` fold (`applyToolCall`,
`toChange`) with its per-artifact write gates (`design.json` schema,
`wireframes.dsl` syntax, `openapi.yaml` structure — so validating a spec costs no
round trip), the SSE reader
(`streamTurn`), and the published JSON Schema — lives in the workspace package
**`@aep/agent-stream`** (moved there so the console/playground fold one
definition). This service imports it; `tool.ts`'s Zod schemas are drift-guarded
against the wire `*Input` types there. See `design/`
(`ADR-0001-anchored-file-edits.md`, `ADR-0002-skills-progressive-disclosure.md`,
`agent-loop.md`).

**Prompt wording lives HERE** (`src/prompts/`, ADR-0003): callers state facts on
a `TurnSpec` and this service composes the instruction. Nothing outside this
service holds prompt text — see `src/prompts/README.md`.

**Skills** are guidance (not code): the service shows a name+description **catalog**
at the end of the system prompt, and the agent pulls a body on demand via the
**`loadSkill`** tool — both built over the `SkillSource` seam
(`src/agents/main/skill-source.ts`). One supply: skills load lazily from the
turn's `_skills` snapshot on the mount (`src/conversation/load-workspace.ts`);
they never travel in the turn payload. No skills → no catalog, behaves as today.
See ADR-0002 and `services/aep-api/design/shared-workspace-volume.md`.

**Audience** (ADR-0013) splits that catalog. A skill's `metadata.aep.audience`
lists the agents its guidance is written for — `design` or `coding` — and this
service is always the **design** side (`SERVICE_AUDIENCE`; the coding agent runs
in the remote-worker runner and never calls here), so nothing is passed per
request. Coding-agent rows are still **listed**: the design agent has to name a
skill to pin it onto a component's `design.json`, which is how that guidance
reaches the build — so the catalog groups them into a pin-only block, and
`load()` returns `{ refused: true }` rather than a body. `loadSkill` reports
those separately from unknown names (`refused` vs `missing`), because a refusal
indistinguishable from "no such skill" invites the agent to skip pinning. An
absent audience means every audience, so unmarked and org-authored skills are
unaffected — and a library with nothing pin-only renders the catalog
byte-identically, preserving the cached instruction prefix.

**Tool sets** (derived from `TurnSpec.kind`, tasks-github-native §9.3): the turn
selects which domain tools the generic loop registers. `files` (default, and identical to
an absent value) is the file-mutation set (`src/agents/main/tools/files.ts`) over a
`FileBundle` — the generation flows. `task-plan`
(`tools/task-plan.ts`) registers `planTask`/`updateTask` over a per-turn `TaskPlan`
accumulator (`task-plan-accumulator.ts`) and NO file tools; `files` then carries
READ-ONLY context (the spec/design bundle + one `tasks/<issueNumber>.md` rendering
per existing open Task) and nothing mutates it. `kind: "plan"` selects `task-plan`; every other kind selects `files`. Register
chat merges `draftExternalResource` onto that files set on the marketplace
route; spec and project turns keep the files set byte-identical. Callers do
not send a tool set — two ways to say what a turn is for is two ways to
disagree. Selection lives in `run-conversation-turn.ts` (the loop stays generic); the shared skill loaders
(`tools/skill-tools.ts`) attach to either set. `execute()` validates + accumulates
only — the service never touches GitHub; aep-api's plan tap performs the issue
writes off the stream. The plan tool contract (inputs, results, error codes, the
`tasks/<n>.md` convention) and the published JSON Schemas live in `@aep/agent-stream`.

## Run

- `pnpm --filter @aep/ae-design-agent dev` (watch) / `start` run the pod
  (`src/main.ts`). It needs the pod env; without `AE_ORG_ID` it refuses to
  boot, and a partial pod env fails naming every missing key
  (`src/pod/config.ts`). A local run goes through `@aep/playground`, which
  drives the same `/v1` edge in process with its dev `authenticate` adapter.
- **Listeners** (`src/pod/listeners.ts`): the public port (`AE_LISTEN_PORT`,
  8080) serves `/v1` (`src/edge/`) behind `authenticate`; the pod's adapter
  (`edge/authenticate.ts`, `@aep/platform-idp-auth`) admits a Platform IdP
  user token of `AE_IDP_ISSUER` with an `AE_USER_AUDIENCES` aud and
  `ouId`/`ouHandle` equal to `AE_ORG_ID`/`AE_ORG_HANDLE`. M2M → 401, another
  org → 403, IdP keys unreachable → 503 `idp_unavailable` (`Retry-After: 5`),
  all before route matching. The health port (`AE_HEALTH_PORT`, 9080, not
  routed) serves `/healthz` and `/readyz`. Start refuses when
  `AE_SECRET_REV` ≠ `AE_EXPECTED_SECRET_REV`. `close()` ends open
  connections (attached streams) and always closes health.
- **`/v1`** (07 §1, `packages/contracts/api/ae-design-agent/v1/openapi.yaml`):
  `edge/project-routes.ts` (current conversation, rotate, messages, turn
  start, active turn, turn status, stream) and `edge/marketplace-routes.ts`
  (the same without a project; a conversation belongs to its creator's
  `sub`, anyone else gets 404). A turn start answers `202 {turnId}` and runs
  detached; watchers attach to `GET …/turns/{t}/stream?from=N` (SSE
  `id: <index>` + `data: <part>`, `: keep-alive` every
  `AGENT_KEEPALIVE_MS`, end `turn-completed` / `turn-failed {reason, …}`
  then `[DONE]`; `Last-Event-ID` resumes at the next frame, `from` wins).
  Refusals: `409 {code: turn_in_progress, activeTurnId}` (also on a rotate
  while a turn runs), `409 {code: conversation_rotated}`, problem
  `no_default_key` (409), `project_unknown` (404), `shutting_down` (503,
  once `TurnStarter.refuse()` ran), `tools_unavailable` (503, the tools
  socket failed), `invalid_turn` / `attachment_rejected` (400),
  `payload_too_large` (413). Bodies are JSON or multipart
  (`edge/turn-input.ts`: ≤ 10 files, 5 MiB each, 15 MiB total, counted while
  the body streams; instruction ≤ 64 KiB).
- **The start path** (`turns/start-turn.ts`, `TurnStarter`, shared with the
  Turn socket): shutdown → key → instruction → `tools.lookup(project)`
  (writes the snapshots) → `turnSpecFor` → snapshot reads and document
  fitting → no running turn on the scope → `ThreadBook.admit` →
  `TurnDesk.start`. The run joins the Room for a project turn when a
  `room` adapter is wired (Task 3.13 supplies it), loads the MCP tools and
  web search by the gates of `turns/turn-spec.ts`, and calls
  `runConversationTurn` with the desk's signal. A failure the agent can name
  ends `agent-error` with its code (`provider_limit`, `output_truncated`).
  Credit is the verified user: `sub` as the author id, the name by
  `displayIdentity`'s rule.
- **TurnDesk** (`turns/turn-desk.ts`): the only turn lock (one running turn
  per project, per marketplace conversation), the replay buffer, the 30-min
  cap, retention, and each finished turn's record. `finishedTurnSink` pushes
  the record to the usage outbox and the closing context size to the
  ThreadBook (auto-rotation past 80 % of the connection's window).
- **Model**: built per turn from the org's connection, read once at boot
  (`shared/connection-env.ts`: `AE_MODEL_CONNECTION` + `ANTHROPIC_API_KEY`;
  either missing → turns answer `no_default_key`; malformed → boot fails,
  value-free). `createModel` has one branch per format (`@ai-sdk/anthropic`,
  `@ai-sdk/openai-compatible`); reasoning effort (`AGENT_REASONING_EFFORT`)
  rides as `effort` on the Anthropic format (not to Haiku 4.5 / Sonnet 4.5)
  and as `reasoning_effort` on OpenAI-compatible. What the connection
  supports (`web_search` strategy, native PDFs, images, cache markers) is its
  `capabilities`, never the provider string. A 429 past a 5-minute
  `retry-after` ends the turn with a `provider_limit` frame and every 429
  logs one `model_provider_429` line (`shared/provider-limit.ts`). Every
  provider request goes out through `shared/guarded-fetch.ts`: the host
  resolves once, any non-public answer is refused, the socket dials the
  checked address, and a redirect is refused rather than followed.
- **Snapshots**: `shared/snapshot-path.ts` resolves
  `<AE_SNAPSHOTS_DIR>/projects/<project>/<headSha>` and
  `<AE_SNAPSHOTS_DIR>/skills/<skillsSha>` from the lookup's shas (project a
  DNS label, sha full hex, dir stat-checked); a marketplace turn reads only
  the skills snapshot. `turns/start-spec.ts` (`turnSpecFor`, port of
  aep-api's `start_command.go`) classifies the raw instruction: `/<token>
  [text]` is a flow, `/start` takes the idea typed inline, else the lookup's
  `idea`; `/start` and flow turns list the lookup's references.
- **Conversations**: `conversations/thread-book.ts` (one current thread per
  project, rotation, auto-rotation) and `conversations/marketplace-book.ts`
  (per-user marketplace conversations), messages behind the
  `ConversationStore` port (in memory; a restart starts every thread fresh;
  the playground has a file adapter). The messages read is a DISPLAY
  projection: user rows carry the journal text + author; each journal entry
  records the connection that wrote the turn, and
  `conversation/history-for.ts` drops what another connection cannot replay.
- **Tools socket** (`src/tools-socket/`): the one port to ae-studio-tools
  over the Unix socket in `AE_MCP_SOCKET`: `mcpFetch` (the MCP client's
  transport), `roomToken`, `postUsage`, `lookup` (404 `project_unknown` →
  `null`) and `skills`. `client.ts` is the undici socket adapter, `fake.ts`
  the in-process one for tests. `src/usage/outbox.ts` holds finished-turn
  records (cap 200, oldest dropped, retry every 2 s, in order);
  `drain(timeoutMs)` flushes it at shutdown (Task 3.13).
- The wire has no `manifest` frame: a turn's usage is the run's result and
  rides its usage record (07 §7).

## Test

- `test` — unit tests (`test/**/*.test.ts`), no tokens. Tests and their shared
  fixtures live in `test/` (never in the shipped `src/` tree), mirroring
  `@aep/agent-stream` and `@aep/playground`. Fixtures/doubles are flat siblings:
  `test/seed-files.ts` (the spec-bundle fixture), `test/skill-source.ts` (the
  `SkillSource` double). Provider cassettes (`test/fixtures/provider-cassettes/`,
  one directory per turn, one `@aep/sse-cassette` file per model call) replay a
  turn per format through `createModel`; `test/provider-cassette.ts` replays and
  records them. Cross-package test-support that must be importable (the
  `mock-model`) stays in `src/shared/` and is published via `exports`.

The local-filesystem playground and the model-eval harness live in the root
`@aep/playground` package (they drive the `/v1` edge in process);
this service ships only the runtime + its unit tests. `test/helpers/edge.ts`
starts the real pod listeners with a local IdP and the fake tools socket.

## Conventions

- Agent + SDK wiring (the `ToolLoopAgent` loop, tools, prompt, server) lives
  here; the client-safe fold + wire contracts live in `@aep/agent-stream`.
- Latest Claude models by default (see the `claude-api` skill for model ids).
- One agent per `src/agents/<name>/`; the loop (`run-turn.ts`) is shared.
- `src/` writes no files **on the turn path**; its only filesystem READS are the
  §12 snapshot dirs (`load-workspace.ts`, paths derived solely by
  `snapshot-path.ts`). The one write is DevTools retention
  (`shared/devtools-retention.ts`), which prunes the debug capture once at boot
  before the server listens — never while a turn runs, and never a spec file.
