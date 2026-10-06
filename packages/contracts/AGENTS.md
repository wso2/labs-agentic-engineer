# AGENTS.md — packages/contracts (`@aep/contracts`)

## Regenerate

The OpenAPI contracts are hand-maintained (contract-first), never generated.
Edit deliberately: server code and client types are generated FROM them.
After an edit, regenerate in EVERY consumer below; CI's `gen-api-check` goes
red in any Go module you skipped, and a skipped `pnpm gen` leaves a stale
committed `src/generated/` file.

| Contract | Regenerate with |
|---|---|
| `api/v1/openapi.yaml` (aep-api's public API, OpenAPI 3.0.3) | `make gen-api` in `services/aep-api`; `pnpm gen` in `apps/console` and in `runners/remote-worker` |
| `api/internal/v1/openapi.yaml` (aep-api's internal API) | `make gen-api` in `services/aep-api` and in `ae-studio-tools` (its client of the ops it calls) |
| `api/ae-studio-tools/v1/openapi.yaml` (the pod's browser file reads) | `make gen-api` in `ae-studio-tools`; `pnpm gen` in `apps/console` |
| `api/ae-studio-tools/internal/v1/openapi.yaml` (the pod's S2S API) | `make gen-api` in `ae-studio-tools` and in `services/aep-api` (its client) |
| `api/ae-design-agent/v1/openapi.yaml` (the design agent's browser API: turns, conversations) | `pnpm gen` in `apps/console` and in `ae-design-agent` |
| `sockets/ae-studio/files/openapi.yaml` | `make gen-api` in `ae-studio-tools`; `pnpm gen` in `ae-collab` |
| `sockets/ae-studio/mcp/openapi.yaml` | `make gen-api` in `ae-studio-tools`; `pnpm gen` in `ae-design-agent` |
| `sockets/ae-studio/turn/openapi.yaml` (`golden/` holds the NDJSON streams both sides test against) | `make gen-api` in `ae-studio-tools`; `pnpm gen` in `ae-design-agent` |

`ae-studio-tools` is `components/dataplane/ae-system-project/ae-studio/ae-studio-tools`;
`ae-design-agent` and `ae-collab` are its siblings. A socket's mount is its
gate. `schemas/` holds the JSON Schemas the design and planning tools validate
with.

## Instructions

- The `ae-studio-tools` contracts' errors are RFC 9457 problems
  (`application/problem+json` `{type, title, status, detail?, code}`), not the
  aep-api `Error` shape.
- A few schemas are hand-written Go types (e.g. `platform/orgconfig.Config*`) and
  marked `x-go-type:` in the contract — don't duplicate them.
- Every error response points at the shared `Error` schema
  (`{code, message, details?[{field, message}]}`, always `application/json`);
  validation failures are `400`.
- The `path` parameter of `read-file` is a trailing wildcard (may contain
  slashes); the server registers the extra catch-all route for it.
- `commands/` holds the `/<command>` chat grammar and nothing else. **No prompt
  text lives in this package** — a command parses into FACTS (which token, which
  idea) that a caller puts on a `TurnSpec` (declared in `@aep/agent-stream`, `src/contracts/sse-events.ts`). The sentences those facts become —
  including which skill a token loads, and which branch of it — belong to
  `components/dataplane/ae-system-project/ae-studio/ae-design-agent/src/prompts/` (ADR-0003 in that service's `design/`).
