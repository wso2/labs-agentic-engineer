# @aep/agent-stream

The single **client-side consumption surface** for the spec agent's turn stream.
The agents service produces the stream; the console, the evals, and the
playground fold it — all through **one** definition here, so fold semantics can't
diverge between implementations. The package has **zero server-side dependencies**
(no Express, no AI SDK), so it is safe to bundle into the browser.

## What's here

| Export | Purpose |
|---|---|
| `StreamPart`, `SSE_DONE`, `AGENT_SSE_EVENT_TYPES` | the raw wire frame + terminal sentinel |
| `TurnRequest`, `WorkspaceRef`, `Toolset`, `*Input`, `OpResult`, `Change` | the turn-request body (workspace ref + `toolset`) + SSE payload shapes |
| `FileBundle`, `applyToolCall`, `toChange`, `isFileMutationTool` | the fold: reconstruct file state from the streamed tool-calls |
| `checkComponentDesign`, `componentDesignSchema`, `COMPONENT_DESIGN_JSON_RE` | the component `design.json` write-gate (travels with `FileBundle`) |
| `PLAN_TASK`/`UPDATE_TASK`, `PlanTaskInput`/`UpdateTaskInput`, `*Result`, `TaskContextFile` | the plan-turn tool contract (tasks-github-native §9.3) |
| `planTaskInputSchema`, `updateTaskInputSchema` | the plan-tool Zod inputs (the agents service tool `inputSchema`s + the JSON-schema source) |
| `parseKnownComponents`, `parseTaskContextFile` | the read-only plan context convention (`specs/design/components/…` + `tasks/<n>.md`) |
| `componentDesignJsonSchema()`, `planTaskJsonSchema()`, `updateTaskJsonSchema()` | the schemas as JSON Schema, for the Go BFF |
| `streamTurn(baseUrl, id, body, { headers })` | the reference SSE reader (transport-only; caller supplies auth + key headers) |

## Write gates

Every write through `FileBundle` is gated by artifact kind, and a rejected write
leaves the bundle **byte-for-byte unchanged** — the tool result carries a
self-correctable error instead, so the model fixes it in the same turn:

| Path | Gate | Code |
|---|---|---|
| `*.yaml` / frontmatter | parse-only YAML reparse | `INVALID_YAML` |
| `components/*/design.json` | `checkComponentDesign` (JSON + schema + `name` = directory) | `INVALID_JSON`, `SCHEMA_VIOLATION` |
| `wireframes.dsl` | flow-dialect syntax (invalid lines would be silently dropped) | `INVALID_DSL` |
| `components/*/openapi.yaml` | OpenAPI 3.x, has paths, has operations | `INVALID_OPENAPI` |
| `components/*/design.json` | every dependency names a node `design.cell` declares | `UNKNOWN_DEPENDENCY` |
| `domain-model.md` | exactly one mermaid `erDiagram`, in the prescribed subset | `INVALID_DIAGRAM` |
| `flows/*.md` | exactly one mermaid `sequenceDiagram`; every participant is a node `design.cell` declares or an actor the PRD names | `INVALID_DIAGRAM`, `UNKNOWN_PARTICIPANT` |

The OpenAPI gate deliberately matches the coverage of the platform's
`validate_openapi_spec` MCP tool — which is itself purely structural — so
validating a spec no longer costs a round trip. That tool takes the document as a
string, so an agent asking about a file it had just written had to retype the
whole thing as tool input (measured: 4.1k output tokens and 28.9s for a 13KB
spec). A `dependencies/<name>.openapi.yaml` is exempt: those are third-party
documents recorded as-is.

## Published JSON Schema

The Zod schemas are published as JSON Schema so the Go BFF validates against the
same definitions the agents service uses — one schema, not two hand-kept copies.
The checked-in artifacts live under `packages/contracts/schemas/`:
`component-design.schema.json` (design.json save-gate), `plan-task.schema.json`
and `update-task.schema.json` (the plan tool inputs the BFF plan tap vendors).

Regenerate them after changing a schema:

```
pnpm --filter @aep/agent-stream gen
```

`turbo build` runs `gen` first, and `test/json-schema.test.ts` fails if any
checked-in artifact drifts — so they can't go stale silently. The one contextual
rule the design.json gate adds (`name` must equal the component directory) is not
expressible in a standalone JSON Schema; both sides apply it separately.
