# AEP — Architecture

> Repo-wide map. Each service's README is the source of truth for its own
> current architecture — start with [`aep-api`](../services/aep-api/README.md).

## Overview

AEP is a spec-driven, AI-enhanced SDLC platform built on OpenChoreo. It is a
polyglot (Go + TypeScript) monorepo organized around **shared contracts**: every
REST boundary is described by an OpenAPI document owned by its producing service,
and every consumer uses generated types — so an incompatible change is a
compile error, not a runtime surprise.

## Buckets

| Bucket | Contents | Deploys? |
|---|---|---|
| `apps/` | React webapps (Vite + Oxygen UI) | yes |
| `services/` | long-lived deployables (Go + TS) | yes |
| `runners/` | one-shot / job images | as jobs |
| `packages/` | shared libraries: `contracts`, `clients`, `ui`, `agent-stream`, `collab-doc`, `design-projection`, `excalidraw-dsl`, `progress-view`, `sse-cassette` | no |
| `skills/` | the authored skill library, seeded and reconciled into each org's own repo | no — delivered as content |
| `playground/` | local harness that runs the real agents against a plain directory (no cluster, no GitHub, no database) | no |
| `evals/` | on-demand evaluation suites for the platform's agents (`spec-agents`: per-section + chained evals over the real agents service; see its README) | no — never in CI |
| `deployments/` | canonical local setup (k3d + OpenChoreo; a legacy docker-compose path); resource types that ship a reference operator keep it under `resource-types/<type>/operator/` (e.g. `thunder-app-operator`) | n/a (operator subdirs: yes, in-cluster) |

## Data & contract ownership

- Contracts are hand-maintained and live in `packages/contracts`, one document per
  audience rather than one per service:
  - `api/v1/openapi.yaml` — the public BFF contract the console is generated from.
  - `api/internal/v1/openapi.yaml` — the service-to-service surface.
  - `workflows/v1/openapi.yaml` — the workflow/runner surface.
- Artifacts the agents produce are JSON Schema under `packages/contracts/schemas/`
  (`component-design`, `plan-task`, `update-task`), consumed by `@aep/agent-stream`
  and the design views.
- Generated clients/servers are never hand-edited. Whether they are committed
  differs by consumer: aep-api's contract codegen (`internal/gen/`, `internal/igen/`)
  and the OpenChoreo client are **committed**, with `make gen-api-check` as the CI
  freshness gate; the console's `apps/console/src/generated/` is gitignored and
  regenerated as a build prestep.

## Codegen pipeline

```
packages/contracts/**/openapi.yaml ──> openapi-typescript ──> TS client (generated/)
                                   └──> oapi-codegen ───────> Go StrictServerInterface (*.gen.go)
```

The build graph (`turbo` + `go.work`) wires every consumer's `build`/`typecheck`
behind `gen`, and CI runs `gen` + `git diff --exit-code` to catch staleness. See
`docs/decisions/ADR-0001-tooling-and-naming.md`.

## Service map

- [`aep-api`](../services/aep-api/README.md) — Go BFF + GitHub webhook receiver
  (git ops folded in); domain-oriented modules + vertical slices.
- `agents` — TS interactive spec agents (Vercel AI SDK).
- `collab` — TS Yjs collaboration server.
- `aep-mcp-server` — MCP surface for the SRE/RCA handoff.
- `remote-worker` (runner) — TS Claude Agent SDK one-shot pod; one image serves
  both task kinds.
- `console` (app) — React frontend.

## How a version gets built

A spec version is cut as a `v<N>` tag and executed as **one supervised run over
one GitHub milestone**: the planner mints prose issues into it, one coding agent
works the whole milestone per cycle, its pull request auto-merges, the merge
fans out to a build per changed component, and the run settles when the working
set is empty and validation has a verdict. The decision and its costs are
[ADR-0011](decisions/ADR-0011-milestone-is-the-unit-of-execution.md); the
mechanism is
[`internal/delivery/README.md`](../services/aep-api/internal/delivery/README.md).
