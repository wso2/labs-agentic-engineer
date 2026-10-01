# AGENTS.md — packages/contracts (`@aep/contracts`)

## Instructions

- The OpenAPI contract is hand-maintained (contract-first), never generated.
  Edit deliberately — server code and console types are generated FROM it.
- `api/v1/openapi.yaml` is the single contract document (OpenAPI 3.0.3). After
  any edit, run `make gen-api` in `services/aep-api` (CI's `gen-api-check`
  enforces it).
- `api/ae-studio-tools/{v1,internal/v1}/openapi.yaml` are the AE Studio
  tools container's contracts. Their errors are RFC 9457 problems
  (`application/problem+json` `{type, title, status, detail?, code}`), not the
  aep-api `Error` shape. After an edit to the internal one, run `make gen-api`
  in `components/dataplane/ae-system-project/ae-studio/ae-studio-tools`.
- A few schemas are hand-written Go types (e.g. `platform/orgconfig.Config*`) and
  marked `x-go-type:` in the contract — don't duplicate them.
- Every error response points at the shared `Error` schema
  (`{code, message, details?[{field, message}]}`, always `application/json`);
  validation failures are `400`.
- The `path` parameter of `read-file` is a trailing wildcard (may contain
  slashes); the server registers the extra catch-all route for it.
- `commands/` holds the `/<command>` chat grammar and nothing else. **No prompt
  text lives in this package** — a command parses into FACTS (which token, which
  idea) that a caller puts on a `TurnSpec`. The sentences those facts become —
  including which skill a token loads, and which branch of it — belong to
  `components/dataplane/ae-system-project/ae-studio/ae-design-agent/src/prompts/` (see that service's ADR-0003). A `strings.json`
  → Go/TS codegen pipeline used to live here; it is gone, and so is this
  package's `gen` script.
