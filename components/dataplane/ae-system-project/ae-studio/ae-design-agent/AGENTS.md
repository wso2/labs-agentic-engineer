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
(`startAndStreamTurn`), and the published JSON Schema — lives in the workspace package
**`@aep/agent-stream`** (moved there so the console/playground fold one
definition). This service imports it; the Zod schemas in `src/agents/main/tools/` are drift-guarded
against the wire `*Input` types there. Design notes in `design/`:
[`agent-loop.md`](design/agent-loop.md) (the loop and its locked decisions),
[`turn-runtime.md`](design/turn-runtime.md) (a turn's lifecycle),
[`pod-memory-bounds.md`](design/pod-memory-bounds.md),
[`narration-policy.md`](design/narration-policy.md), and the ADRs.

**Prompt wording lives HERE** (`src/prompts/`, [ADR-0003](design/ADR-0003-turn-composition-lives-here.md)): callers state facts on
a `TurnSpec` and this service composes the instruction. Nothing outside this
service holds prompt text — see `src/prompts/README.md`.

**Skills** are guidance (not code): the service shows a name+description **catalog**
at the end of the system prompt, and the agent pulls a body on demand via the
**`loadSkill`** tool — both built over the `SkillSource` seam
(`src/agents/main/skill-source.ts`). One supply: skills load lazily from the
turn's `_skills` snapshot on the mount (`src/conversation/load-workspace.ts`);
they never travel in the turn payload. No skills, no catalog.
See [ADR-0002](design/ADR-0002-skills-progressive-disclosure.md) and [`ae-studio-tools/design/clone-storage.md`](../ae-studio-tools/design/clone-storage.md).

**Audience** ([ADR-0014](../../../../../docs/decisions/ADR-0014-skill-audience-is-metadata-visible-not-loadable.md)): a skill's `metadata.aep.audience` names the agents its
guidance is for. This service is always the **design** side
(`SERVICE_AUDIENCE`), so nothing is passed per request. Coding-audience skills
are listed in a pin-only block (the agent pins them onto a component's
`design.json`) and `load()` returns `{ refused: true }` rather than a body;
`loadSkill` reports `refused` apart from `missing`, because a refusal that
reads as "no such skill" invites the agent to skip pinning. A library with
nothing pin-only renders the catalog byte-identically, keeping the cached
instruction prefix.

**Tool sets** come from `TurnSpec.kind`, selected in `run-conversation-turn.ts`
(the loop stays generic); callers never send a tool set. `files` (the default)
is the file-mutation set (`src/agents/main/tools/files.ts`) over a
`FileBundle`. `kind: "plan"` selects `task-plan` (`tools/task-plan.ts`:
`planTask`/`updateTask` over a per-turn `TaskPlan` accumulator, no file tools;
`files` is read-only context). Register chat merges `draftExternalResource` onto
the files set on the marketplace route. `execute()` validates and accumulates
only: aep-api's plan tap performs the issue writes off the stream. The plan
tool contract and its JSON Schemas live in `@aep/agent-stream`; the planner
side is in [`design/task-planner-contract-parity.md`](design/task-planner-contract-parity.md).

## Run

- `pnpm --filter @aep/ae-design-agent dev` (watch) / `start` run the pod
  (`src/main.ts`). It needs the pod env; without `AE_ORG_ID` it refuses to
  boot, and a partial pod env fails naming every missing key
  (`src/pod/config.ts`). A local run goes through `@aep/playground`, which
  drives the same `/v1` edge in process with its dev `authenticate` adapter.
- **Listeners** (`src/pod/listeners.ts`): the public port (`AE_LISTEN_PORT`,
  8080) serves `/v1` (`src/edge/`) behind the `authenticate` gate
  (`edge/authenticate.ts`, `@aep/platform-idp-auth`): a Platform IdP user token
  of `AE_IDP_ISSUER` with an `AE_USER_AUDIENCES` audience and `ouId`/`ouHandle`
  equal to `AE_ORG_ID`/`AE_ORG_HANDLE`. M2M → 401, another org → 403, IdP keys
  unreachable → 503 `idp_unavailable` (`Retry-After: 5`), all before route
  matching. The Turn socket (`AE_TURN_SOCKET`, mode 0660; a stale socket file
  is replaced, any other file refuses the start) serves `edge/turn-socket.ts`
  for ae-studio-tools, whose mount is the gate. The health port
  (`AE_HEALTH_PORT`, 9080, not routed) serves `/healthz` and `/readyz`. Start
  refuses when `AE_SECRET_REV` ≠ `AE_EXPECTED_SECRET_REV`.
- **Turns** (start path, lock, replay, usage hand-off, shutdown, the `/v1` and
  Turn socket shapes): [`design/turn-runtime.md`](design/turn-runtime.md).
  Contracts: `packages/contracts/api/ae-design-agent/v1/openapi.yaml`,
  `packages/contracts/sockets/ae-studio/turn/` (its golden streams are the
  line format), `edge/turn-input.ts` for the body limits.
- **Room join** (`collab/local-room.ts`, `collab/room-peer.ts`): the
  agent dials ae-collab's Room socket (`AE_ROOM_SOCKET`, as
  `ws+unix:<path>:/`) for room `spec-<AE_ORG_HANDLE>-<project>`, sends no
  token, and names the user it works for in the `credit` parameter
  `{name, email}` (who may join: [`ae-collab/design/room.md`](../ae-collab/design/room.md)).
  Every connection has a fresh Y.Doc: a dropped connection is replaced, never
  resumed with its kept doc (a kept doc doubles a re-seeded Room), and the
  peer's writes are applied again where the new doc differs; writes still
  pending when the turn ends are lost. Leaving clears the agent's presence at
  once.
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
  the skills snapshot. `turns/start-spec.ts` (`turnSpecFor`) classifies the raw instruction: `/<token>
  [text]` is a flow, `/start` takes the idea typed inline, else the lookup's
  `idea`; `/start` and flow turns list the lookup's references.
- **Conversations**: `conversations/thread-book.ts` (one current thread per
  project) and `conversations/marketplace-book.ts` (per-user marketplace
  conversations); every in-process bound (rotation, eviction, caps) is in
  [`design/pod-memory-bounds.md`](design/pod-memory-bounds.md). Messages sit
  behind the `ConversationStore` port (in memory; a restart starts every thread
  fresh; the playground has a file adapter). The messages read is a DISPLAY
  projection: user rows carry the journal text + author; each journal entry
  records the connection that wrote the turn, and
  `conversation/history-for.ts` drops what another connection cannot replay.
- **Tools socket** (`src/tools-socket/`): the one port to ae-studio-tools
  over the Unix socket in `AE_MCP_SOCKET`: `mcpFetch` (the MCP client's
  transport), `postUsage`, `lookup` (404 `project_unknown` →
  `null`) and `skills`. `client.ts` is the undici socket adapter, `fake.ts`
  the in-process one for tests. `src/usage/outbox.ts` holds finished-turn
  records (cap 200, oldest dropped, retry every 2 s, in order);
  `drain(timeoutMs)` flushes it at shutdown (`pod/shutdown.ts`).

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
  snapshot dirs (`load-workspace.ts`, paths derived solely by
  `snapshot-path.ts`). The one write is DevTools retention
  (`shared/devtools-retention.ts`), which prunes the debug capture once at boot
  before the server listens — never while a turn runs, and never a spec file.
