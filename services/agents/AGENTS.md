# AGENTS.md — services/agents (`@aep/agents`)

TS interactive spec agents (Vercel AI SDK). Seeded with ONE agent: the **main
file-mutation agent** (prompt-driven add/edit/remove over a spec bundle), exposed
as an **SSE turn stream** (one turn = one HTTP request). The runtime **writes no
files** — accept/edit/save is a separate concern.

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
chat merges `draftExternalResource` onto that files set for the synthetic
register project; spec and project turns keep the files set byte-identical. Callers do
not send a tool set — two ways to say what a turn is for is two ways to
disagree. Selection lives in `run-conversation-turn.ts` (the loop stays generic); the shared skill loaders
(`tools/skill-tools.ts`) attach to either set. `execute()` validates + accumulates
only — the service never touches GitHub; the BFF plan tap performs the issue writes
off the stream. The plan tool contract (inputs, results, error codes, the
`tasks/<n>.md` convention) and the published JSON Schemas live in `@aep/agent-stream`.

## Run

- `pnpm --filter @aep/agents dev` — SSE server, watch/reload. `start` — run once.
- Endpoints: `GET /healthz` (open) · `POST /conversations/:id/turns` (SSE) ·
  `GET /conversations/:id` — the last two behind the M2M gate.
- **No boot-time Anthropic key**: the model is built per turn from the
  `X-Anthropic-Key` header (missing → 400). `X-Org-Id` is LOAD-BEARING: the
  conversation's `org_` segment must equal it (403 otherwise — the §12 fence).
- **One turn shape**: `turn` (a `TurnSpec` — what the turn is FOR) + `workspace`
  (IDs + shas; files/skills read from `WORKSPACE_MOUNT_ROOT` snapshots via
  `snapshot-path.ts` + `load-workspace.ts`). Inline `files`/`skills`, a
  pre-composed `instruction`, or a caller-chosen `toolset`/`eagerSkills` in the
  body → 400.
  Every successful turn ends with a terminal `manifest` frame (D14:
  mutated-paths → sha256) before `[DONE]`; a failed/severed stream has none.
- **M2M gate is always on**: set `AGENT_JWT_JWKS_URL` (RS256) **or**
  `AGENT_JWT_SECRET` (HS256) — the server refuses to boot with neither. `aud`
  defaults to `agents-service` (`AGENT_JWT_AUDIENCE`); `AGENT_JWT_ISSUER` optional.
- **Store**: Postgres when a connection URL resolves — `DATABASE_URL` verbatim
  (local dev) or discrete `DB_HOST` / `DB_PORT` / `DB_USER` / `DB_PASSWORD` /
  `DB_NAME` (+ optional `DB_SSLMODE`) assembled like aep-api (platform
  release-binding); `DATABASE_URL` wins when both are set. Incomplete discrete
  fields → in-memory. Idempotent bootstrap + TTL sweep
  (`CONVERSATIONS_TTL_MS` / `CONVERSATIONS_SWEEP_MS`).
- Keep-alives every `AGENT_KEEPALIVE_MS` (default 15s) while a turn streams.
- Callers (e.g. the `@aep/playground` CLI) read `ANTHROPIC_API_KEY` themselves and
  send it (plus an HS256 M2M token) as headers — the service holds no key.
- **MCP dependency-discovery** (optional): the caller — not the service — pushes an
  `mcp: { url, token }` bundle on the turn (aep-api in production; the playground in
  local dev). Absent → no discovery tools (byte-identical to today); malformed → a
  clean pre-stream 400.
- **Turn journal** (optional, #463): the caller pushes
  `journal: { text, author?: { id, displayName } }` — the raw client-sent
  instruction + acting user, stored beside the transcript (never woven into
  the prompt). `GET /conversations/:id` serves a DISPLAY projection, not the
  raw transcript: user rows carry the journal text + author (a journal-less
  turn falls back to its raw stored message); assistant/tool rows pass through.
  The read is org-fenced like the turn POST (`X-Org-Id` must match the id's
  org segment). Absent → no entry; malformed → a clean pre-stream 400.

## Test

- `test` — unit tests (`test/**/*.test.ts`), no tokens. Tests and their shared
  fixtures live in `test/` (never in the shipped `src/` tree), mirroring
  `@aep/agent-stream` and `@aep/playground`. Fixtures/doubles are flat siblings:
  `test/seed-files.ts` (the spec-bundle fixture), `test/skill-source.ts` (the
  `SkillSource` double). Cross-package test-support that must be importable (the
  `mock-model`) stays in `src/shared/` and is published via `exports`.

The local-filesystem playground and the model-eval harness live in the root
`@aep/playground` package (they drive this service over HTTP like any caller);
this service ships only the runtime + its unit tests.

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
