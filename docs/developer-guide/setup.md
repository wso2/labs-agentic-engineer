# Developer guide — setup & dev flow

## Prerequisites

- Go 1.26 (auto-downloaded by the toolchain if your local `go` is older;
  `GOTOOLCHAIN=auto`).
- Node 22 LTS + pnpm 10 (`corepack enable` to get the pinned pnpm).
- `make tools` to install pinned Go tools (golangci-lint).

## First run

```bash
make install     # pnpm install + go work sync
make gen         # regenerate contracts (TS clients + Go server interfaces)
make build       # build everything
```

## Uniform verbs

All driven from the root `Makefile` (the single entry point):

| Verb | What it does |
|---|---|
| `make gen` | `openapi-typescript` (TS) + `go generate` (Go) codegen |
| `make build` | turbo build (TS) + `go build` (go.work) — runs `gen` first |
| `make dev` | start dev servers |
| `make test` | turbo test + `go test` |
| `make lint` | eslint + golangci-lint |
| `make typecheck` | `tsc` + `go vet` |
| `make license-check` | fail if any source lacks the Apache header |

## Adding a package

- **TypeScript:** create `packages/<name>` (or `apps/`, `services/`, `runners/`)
  with a `package.json` exposing the uniform scripts. A workspace glob picks it up
  — no tooling edits.
- **Go:** create the module and add one `use` line to `go.work`. The Makefile
  discovers it dynamically.

## Local stack

One step: `make dev-env`. It runs `deployments/scripts/setup-env-for-aectl.sh`
(k3d cluster, OpenChoreo, ThunderID, OpenBao), builds this checkout's service
images, and installs the platform onto them with `aectl`. Everything runs
in-cluster — there is no host-container path any more. `make dev-update`
rebuilds and rolls just the images whose sources changed.

After a host restart the cluster comes back by itself, but `host.k3d.internal`
does not, and in-cluster hostname resolution fails in ways that surface as
unrelated 401s. Diagnose and repair with
`deployments/scripts/restore-host-k3d-internal.sh check|apply`; the
`deployments/` README's "After a host restart" section explains why, and covers
the OpenBao secrets that are also lost on restart.
