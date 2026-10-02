# AGENTS.md — ae-studio-tools

Go module `github.com/wso2/aep/ae-studio-tools`: the tools container of the
org's AE Studio pod. In `go.work`, so the root `make build|test|lint|typecheck`
cover it.

## Commands

| Command | Does |
|---|---|
| `make build` | `go build ./...` |
| `make test` | `go test -short ./...` |
| `make gen-api` | regenerate `internal/gen` (and its `v1`, `filessock`, `aepapi` sub-packages) from the contracts under `packages/contracts` |
| `make gen-api-check` | codegen freshness gate (CI) |
| `make deadcode-check` | dead-code gate (CI); `make deadcode` reports without failing |

CI runs `gen-api-check` and `deadcode-check` through the root loop over every
Go module, and fails if either of this module's gates did not run. On a shell whose `GOROOT` does not match the
toolchain, run Go make targets with `env -u GOROOT`.

## Packages

| Package | Purpose |
|---|---|
| `cmd/ae-studio-tools` | wiring: secret-rev check, config, git engine (`repo.StaticToken(GITHUB_PAT)`, logs `repo.root {root_layout}` once), reaper (built before any listener, runs for the process lifetime), publisher token + aep-api client + project resolver, public + health listeners (ready once both are bound); SIGTERM/SIGINT drain the listeners, then stop the reaper |
| `internal/config` | `Load(getenv)` reads the pod env; `CheckSecretRev(getenv)` refuses to start when `AE_SECRET_REV` ≠ `AE_EXPECTED_SECRET_REV` |
| `internal/auth` | JWT verifier over the IdP JWKS; `UserGate` (`/v1`) and `M2MGate` (`/internal/v1`) |
| `internal/problem` | the `application/problem+json` error body (leaf, so gates and edge share it) |
| `internal/edge` | HTTP surfaces. `routes.go` is the public listener's mount table (gate before route matching in every group); `internal.go` the `/internal/v1` group (body cap → M2M gate → kin-openapi validator → generated server); `v1.go` the `/v1` group (user gate → validator → generated server, plus the read-file `{path...}` catch-all; a nested read-file path is validated as read-file with the whole remainder as `path`; a 400 is `path_invalid`; no write op, so a write is 404) and `v1_files.go` its Files ops with the error map (`path_invalid` 400, `path_not_found` / `ref_not_found` / `project_unknown` 404, `aep_api_unavailable` 503 + `Retry-After: 5` (none and a loud `aep_api.auth_rejected {op, project, cause}` when the pod's credentials were refused), `disk_full` 503, any other git failure `github_error` 502 with `files.git_failed {op, project, repo, class}`; resolver and git errors are logged by class only, never their text); `validator.go` the shared request validator (a route miss is 404); `webhook.go` is `POST /webhooks/github` (25 MiB cap → HMAC → forward; events `webhook.rejected`, `webhook.forward_failed`, `webhook.forwarded`, value-free); `accesslog.go` logs `internal.access {method, path, status, ms}`; a path ServeMux would redirect (a bare group root, dot segments, `//`) is 404 behind its group's gate instead; `NewHealth` serves `/healthz` and `/readyz` on the health port only |
| `internal/gen` | generated (do not edit): models, strict server and embedded spec for `/internal/v1`; `gen/v1` the same for `/v1`; `gen/filessock` the Files socket; `gen/aepapi` the aep-api client |
| `internal/files` | `Reader`: the read-only Files ops (`List`, `ReadAt`, `Bundle`) over the engine. Resolves the project through `projects.Resolver` on every call and addresses the clone as `{Org: AE_ORG_HANDLE, Project, RepoSlug: naming.SlugForURL(owner/repo)}`; owner/repo never come from the request. `paths.go` holds today's read rules, moved from aep-api: `specs/`, `tests/acceptance/report.json`, a one-segment `workload.yaml`, canonical and traversal-free; a ref is a hex object name. `List` and `Bundle` omit what the read rules refuse; the clone URL loses any userinfo; engine failures are `*RepoError{Repo: owner/repo}` |
| `internal/repo` | the git engine (moved from aep-api's gitfs): one bare clone per repo under `AE_STUDIO_DATA_DIR/repos/<org>/<project>/<slug>/`, plumbing reads and `Mutate` (push under `--force-with-lease`), askpass credential, per-repo flock; an ENOSPC on a read or a write is `DiskFullError` and queues the reaper's forced sweep; logs `repo.clone {repo, mode: "bare", ms}`. `naming` derives the slug and owner/repo from a GitHub URL; `repotest` is the file:// origin every engine, files and edge test uses |
| `internal/webhook` | `Valid`: constant-time `X-Hub-Signature-256` check against the current secret only; `Forwarder` hands a verified delivery to aep-api (`Unwired` answers `ErrUpstreamUnavailable` until phase 4) |
| `internal/github` | GitHub REST client over the gitpat; `Whoami` = `GET /user`, rate limits map to `ErrRateLimited` |
| `internal/platform` | AEP platform clients: `ClientCredentials` (client_credentials token, `client_secret_basic`, cached until 60 s before expiry, `Invalidate` on 401; a token-endpoint 400/401 is `ErrClientRejected`) and `NewAEPAPI` (generated aep-api client at `AEP_API_BASE_URL` + `/internal/v1`, bearer from the publisher token, one retry after a 401) |
| `internal/projects` | `Resolver`: project → GitHub repository through aep-api on every call, no cache; 404 → `ErrUnknown` (denial, `project_unknown`), anything else non-200 or unreachable → `ErrUnavailable` (`aep_api_unavailable`), also `ErrMisconfigured` when the pod's own credentials were refused (aep-api 401/403 after the retry, or `platform.ErrClientRejected` from the token endpoint); `projectstest.Fake` for callers' tests |
| `internal/repo/reaper` | studio-data lifecycle, one sweep every 5 min, plus a forced sweep (queued without blocking) on ENOSPC, which only fires when the node disk fills: the emptyDir `sizeLimit` is enforced by kubelet eviction, not ENOSPC. Passes: tmp and trash older than 1 h, git maintenance, then the one budget (block-usage `du` of the root; from 85 % purge trash, then evict mirrors least recently fetched (git dir / `FETCH_HEAD` mtime, not last read) down to 70 %, skipping any whose `repo.lock` is held). `UsagePct` = max(budget share incl. clones since the sweep, node `statfs` byte used%; inodes not watched) is published to the engine for `DiskFullError`. Logs `reaper.sweep {usedBytes, budgetBytes, pct, evicted}`. No orphan, recordings or leader pass |

`/v1` (`packages/contracts/api/ae-studio-tools/v1`) serves the read-only
Files ops: `GET /v1/projects/{p}/files?prefix=`, `…/files/{path...}?ref=`,
`…/files/bundle?prefix=&ref=`.

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
under an `edgetest/`, `projectstest/` or `repotest/` dir or a `_fortest.go` file (`scripts/deadcode.sh`).

## Image

`Dockerfile` context is this folder. The image is `alpine:3.21` with git and
tini (PID 1, forwards SIGTERM) and runs as UID 10001.
