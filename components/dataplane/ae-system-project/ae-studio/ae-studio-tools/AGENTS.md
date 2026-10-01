# AGENTS.md — ae-studio-tools

Go module `github.com/wso2/aep/ae-studio-tools`: the tools container of the
org's AE Studio pod. In `go.work`, so the root `make build|test|lint|typecheck`
cover it.

## Commands

| Command | Does |
|---|---|
| `make build` | `go build ./...` |
| `make test` | `go test -short ./...` |
| `make deadcode-check` | dead-code gate (CI); `make deadcode` reports without failing |

CI runs `deadcode-check` (and `gen-api-check` once the module has one) through
the root loop over every Go module. On a shell whose `GOROOT` does not match the
toolchain, run Go make targets with `env -u GOROOT`.

## Packages

| Package | Purpose |
|---|---|
| `cmd/ae-studio-tools` | wiring: secret-rev check, config, health listener, shutdown |
| `internal/config` | `Load(getenv)` reads the pod env; `CheckSecretRev(getenv)` refuses to start when `AE_SECRET_REV` ≠ `AE_EXPECTED_SECRET_REV` |
| `internal/edge` | HTTP surfaces; `NewHealth` serves `/healthz` and `/readyz` on the health port |

## Env

Required: `AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`,
`AE_USER_AUDIENCES` (comma list), `AE_M2M_CLIENT_ID`, `GITHUB_PAT`,
`GITHUB_WEBHOOK_SECRET`. Optional: `AE_LISTEN_PORT` (default `8082`),
`AE_HEALTH_PORT` (default `9082`; not in the Service, not routed). Every
missing or invalid key is named in one error; values are never logged.

## Dead code

Tests do not count as consumers. Keep an unwired function only with a
`//deadcode:keep <reason>` line in its doc comment; test-support packages go
under an `edgetest/` dir or a `_fortest.go` file (`scripts/deadcode.sh`).

## Image

`Dockerfile` context is this folder. The image is distroless and runs as
UID 10001.
