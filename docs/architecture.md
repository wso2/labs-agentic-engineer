# AEP — Architecture

> **Note:** this describes the *target* architecture. Components land as they are
> built.

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
| `packages/` | shared libraries (`contracts`, `core`, `ui`, `clients`, `agent`) | no |
| `deployments/` | canonical local setup (k3d + docker-compose); resource types that ship a reference operator keep it under `resource-types/<type>/operator/` (e.g. `thunder-app-operator`) | n/a (operator subdirs: yes, in-cluster) |

## Data & contract ownership

- Each service **owns** the OpenAPI it produces, stored under
  `packages/contracts/<service>/openapi.yaml`.
- Internal events are JSON Schema under `packages/contracts/events/`.
- Generated clients/servers are produced into gitignored `generated/` / `*.gen.go`
  as a build/dev prestep — never hand-edited.

## Codegen pipeline

```
openapi/*.yaml ──> openapi-typescript ──> TS client (generated/)
              └──> oapi-codegen ────────> Go StrictServerInterface (*.gen.go)
```

The build graph (`turbo` + `go.work`) wires every consumer's `build`/`typecheck`
behind `gen`, and CI runs `gen` + `git diff --exit-code` to catch staleness. See
`docs/decisions/ADR-0001-tooling-and-naming.md`.

## Service map (target — populated during migration)

- `aep-api` — Go BFF + GitHub webhook receiver (git ops folded in).
- `database` — Go data service.
- `agents` — TS interactive spec agents (Vercel AI SDK).
- `collab` — TS Yjs collaboration server.
- `coding-agent` (runner) — TS Claude Agent SDK one-shot pod.
- `console` (app) — React frontend.
