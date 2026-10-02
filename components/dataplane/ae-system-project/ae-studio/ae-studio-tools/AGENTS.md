# AGENTS.md — ae-studio-tools

Go module `github.com/wso2/aep/ae-studio-tools`: the tools container of the
org's AE Studio pod. In `go.work`, so the root `make build|test|lint|typecheck`
cover it.

## Commands

| Command | Does |
|---|---|
| `make build` | `go build ./...` |
| `make test` | `go test -short ./...` |
| `make gen-api` | regenerate `internal/gen` from `packages/contracts/api/ae-studio-tools/internal/v1/openapi.yaml` |
| `make gen-api-check` | codegen freshness gate (CI) |
| `make deadcode-check` | dead-code gate (CI); `make deadcode` reports without failing |

CI runs `gen-api-check` and `deadcode-check` through the root loop over every
Go module, and fails if either of this module's gates did not run. On a shell whose `GOROOT` does not match the
toolchain, run Go make targets with `env -u GOROOT`.

## Packages

| Package | Purpose |
|---|---|
| `cmd/ae-studio-tools` | wiring: secret-rev check, config, public + health listeners (ready once both are bound), shutdown |
| `internal/config` | `Load(getenv)` reads the pod env; `CheckSecretRev(getenv)` refuses to start when `AE_SECRET_REV` ≠ `AE_EXPECTED_SECRET_REV` |
| `internal/auth` | JWT verifier over the IdP JWKS; `UserGate` (`/v1`) and `M2MGate` (`/internal/v1`) |
| `internal/problem` | the `application/problem+json` error body (leaf, so gates and edge share it) |
| `internal/edge` | HTTP surfaces. `routes.go` is the public listener's mount table (gate before route matching in every group); `internal.go` the `/internal/v1` group (body cap → M2M gate → kin-openapi validator → generated server); `webhook.go` is `POST /webhooks/github` (25 MiB cap → HMAC → forward; events `webhook.rejected`, `webhook.forward_failed`, `webhook.forwarded`, value-free); `accesslog.go` logs `internal.access {method, path, status, ms}`; a path ServeMux would redirect (a bare group root, dot segments, `//`) is 404 behind its group's gate instead; `NewHealth` serves `/healthz` and `/readyz` on the health port only |
| `internal/gen` | generated: models, strict server and embedded spec for `/internal/v1` (do not edit) |
| `internal/webhook` | `Valid`: constant-time `X-Hub-Signature-256` check against the current secret only; `Forwarder` hands a verified delivery to aep-api (`Unwired` answers `ErrUpstreamUnavailable` until phase 4) |
| `internal/github` | GitHub REST client over the gitpat; `Whoami` = `GET /user`, rate limits map to `ErrRateLimited` |
| `internal/platform` | AEP platform clients: `ClientCredentials` (client_credentials token, `client_secret_basic`, cached until 60 s before expiry, `Invalidate` on 401; a token-endpoint 400/401 is `ErrClientRejected`) and `NewAEPAPI` (generated aep-api client at `AEP_API_BASE_URL` + `/internal/v1`, bearer from the publisher token, one retry after a 401) |
| `internal/projects` | `Resolver`: project → GitHub repository through aep-api on every call, no cache; 404 → `ErrUnknown` (denial, `project_unknown`), anything else non-200 or unreachable → `ErrUnavailable` (`aep_api_unavailable`), also `ErrMisconfigured` when the pod's own credentials were refused (aep-api 401/403 after the retry, or `platform.ErrClientRejected` from the token endpoint); `projectstest.Fake` for callers' tests |
| `internal/repo/reaper` | studio-data lifecycle, one sweep every 5 min (and on ENOSPC, queued without blocking): tmp and trash older than 1 h, git maintenance, then the one budget (`du` of the root; from 85 % purge trash, then evict mirrors LRU down to 70 %, skipping any whose `repo.lock` is held). `UsagePct` = max(budget share incl. clones since the sweep, node `statfs` used%) is published to the engine for `DiskFullError`. Logs `reaper.sweep {usedBytes, budgetBytes, pct, evicted}`. No orphan, recordings or leader pass |

The `/v1` contract (`packages/contracts/api/ae-studio-tools/v1`) has no
operations yet, so nothing is generated from it; `/v1` answers 404 behind the
user gate.

## Env

Required: `AE_ORG_ID`, `AE_ORG_HANDLE`, `AE_IDP_ISSUER`, `AE_IDP_JWKS_URL`,
`AE_USER_AUDIENCES` (comma list), `AE_M2M_CLIENT_ID`, `GITHUB_PAT`,
`GITHUB_WEBHOOK_SECRET`, `AE_IDP_TOKEN_URL` and `AEP_API_BASE_URL` (absolute
http(s)), `AE_PUBLISHER_CLIENT_ID`, `AE_PUBLISHER_CLIENT_SECRET`,
`AE_STUDIO_DATA_DIR` (absolute path, the studio-data root),
`AE_STORAGE_BUDGET_BYTES` (int64 > 0, the reaper's budget). Optional: `AE_LISTEN_PORT` (default `8082`),
`AE_HEALTH_PORT` (default `9082`; not in the Service, not routed). Every
missing or invalid key is named in one error; values are never logged.

## Dead code

Tests do not count as consumers. Keep an unwired function only with a
`//deadcode:keep <reason>` line in its doc comment; test-support packages go
under an `edgetest/` or `projectstest/` dir or a `_fortest.go` file (`scripts/deadcode.sh`).

## Image

`Dockerfile` context is this folder. The image is distroless and runs as
UID 10001.
