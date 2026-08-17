# ADR-0001: Monorepo tooling and naming conventions

- **Status:** Accepted
- **Date:** 2026-06-15
- **Context:** the AEP rewrite (`aep-rewrite` branch) stands up a polyglot
  monorepo and needs pinned, uniform tooling so agents and humans share one set
  of commands and so type errors are the self-correction signal.

## Decision

| Concern | Decision |
|---|---|
| JS task graph | pnpm workspaces + **Turborepo** 2.x (caching, `--filter`, task deps) |
| Go graph | a single **`go.work`** spanning the workspace's Go modules (no `replace`). A standalone module with its own Dockerfile-only build (no shared code, e.g. `deployments/local-secret-manager-api`) deliberately stays out |
| Single entry point | a root **`Makefile`** fanning out to `turbo` (TS) and a `go` loop |
| Go version | `go 1.26` (workspace + all modules) |
| Node | 22 LTS (`engines.node >=22`) |
| pnpm | 10 (`packageManager: pnpm@10`) |
| npm scope | `@aep/*` (e.g. `@aep/contracts`, `@aep/ui-explorer`) |
| Go module prefix | `github.com/wso2/aep/<name>` (e.g. `.../aep-api`, `.../aectl`, `.../thunder-app-operator`) |
| Contracts | OpenAPI-first, REST only, for the platform's own service/BFF APIs; JSON Schema for internal events. `tools/aectl`'s CLI↔`aep-server` control-plane channel is the one exception — a gRPC service defined in `tools/aectl/proto/admin.proto` (see `tools/aectl/AGENTS.md`) |
| Go codegen | `oapi-codegen` `StrictServerInterface`, pinned via go.mod `tool` directive |
| TS codegen | `openapi-typescript` |
| Generated code | asymmetric by consumer. aep-api's contract codegen (`internal/gen/*_gen.go`, `internal/igen/*_gen.go`) is named with an underscore precisely so it misses the `*.gen.go` ignore pattern — it's **committed**, with `make gen-api-check` as a CI freshness gate diffing it against the contract. The OpenChoreo API client (`internal/clients/openchoreo/gen/*.gen.go`) does match that pattern but is explicitly un-ignored (`!services/aep-api/internal/clients/openchoreo/gen/*.gen.go`) and also committed, generated once from a pinned upstream spec. The console's generated types and route tree (`apps/console/src/generated/`) really are gitignored — no negation covers them |
| Lint | `eslint` (flat config) + `golangci-lint` v2 (goheader for license) |
| License | Apache-2.0 header via `addlicense`; enforced by `make license-check` |

## Uniform verbs

`build`, `dev`, `test`, `lint`, `typecheck`, `gen` — every package exposes them
(package.json scripts for TS, the Makefile loop for Go); the root `Makefile` is
the one entry point.

## Consequences

- A contract change forces consumers to regenerate and fails them if now wrong.
- Adding a Go module to the workspace = one `use` line in `go.work` (the
  Makefile's uniform verbs discover workspace members dynamically via
  `go list -m`); a module that opts out builds and is linted independently,
  outside those verbs. Adding a TS package = a workspace glob match.
- Generated code must be produced (`make gen`) before a fresh build; this is
  wired as a build-graph prestep, not a manual step.
