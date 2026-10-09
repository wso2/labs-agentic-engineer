# AGENTS.md — ae-studio-tools

Go module `github.com/wso2/aep/ae-studio-tools`: the tools container of the
org's AE Studio pod. In `go.work`, so the root `make build|test|lint|typecheck`
cover it.

## Commands

| Command | Does |
|---|---|
| `make build` | `go build ./...` |
| `make test` | `go test -short ./...` |
| `make gen-api` | regenerate `internal/gen` (and its `v1`, `filessock`, `mcpsock`, `turnsock`, `aepapi` sub-packages) from the contracts under `packages/contracts` |
| `make gen-api-check` | codegen freshness gate (CI) |
| `make deadcode-check` | dead-code gate (CI); `make deadcode` reports without failing |

CI runs `gen-api-check` and `deadcode-check` through the root loop over every
Go module, and fails if either of this module's gates did not run. On a shell whose `GOROOT` does not match the
toolchain, run Go make targets with `env -u GOROOT`.

## Where things live

Route groups, gates, the MCP allow-list and problem codes:
[`design/route-groups.md`](design/route-groups.md) (start at its
[gate table](design/route-groups.md#gate-table)). Clone volume, budget and the
reaper: [`design/clone-storage.md`](design/clone-storage.md).

| Package | Holds |
|---|---|
| `cmd/ae-studio-tools` | wiring and shutdown order |
| `internal/config` | `Load(getenv)` reads the pod env; `CheckSecretRev` refuses to start when `AE_SECRET_REV` is not `AE_EXPECTED_SECRET_REV` |
| `internal/auth` | JWT verifier over the IdP JWKS; `UserGate` (`/v1`), `M2MGate` (`/internal/v1`) |
| `internal/problem` | the `application/problem+json` body (a leaf, shared by gates and edge) |
| `internal/edge` | the mount tables (`routes.go`), the `/internal/v1` group (`internal*.go`), the owner guard, the Files and MCP socket mounts |
| `internal/gen` | generated, never edited; one sub-package per contract |
| `internal/mcp` | the MCP socket's JSON-RPC server and `AllowedTools` |
| `internal/files` | read-only Files ops, `Snapshot`, and `Applier` (the Files socket's apply) |
| `internal/repo` | the git engine (bare mirrors, `Mutate`, `Commit`, tags), its `/internal/v1/repos/{owner}/{repo}` handler, and `reaper/` |
| `internal/turns` | `Relay` of aep-api's turns to the Turn socket; `turnstest` is a fake Turn socket replaying the golden streams |
| `internal/usage` | the turn-usage outbox `Sender` |
| `internal/webhook` | HMAC check and the `Forwarder` to aep-api |
| `internal/github` | the pod's one GitHub client and its `/internal/v1` ops |
| `internal/skills` | org skill library catalog and the project skill mirror |
| `internal/projects` | `Resolver`: project to repository through aep-api |
| `internal/platform` | client-credentials tokens and the generated aep-api client |
| `internal/designspec`, `internal/securityspec`, `internal/jsonschema` | spec validators vendored from aep-api |

## Rules

- **Every `/internal/v1/repos/{owner}/{repo}/…` op** answers 403
  `owner_not_allowed` unless the owner equals `AE_GITHUB_OWNER`
  (case-insensitively); unset refuses all. `owner`/`repo` never come from a
  Files-socket or MCP request: `projects.Resolver` resolves the project
  through aep-api on every call, with no cache.
- **Keep in step.** The MCP tool descriptors in three places (aep-api
  `mcpdiscovery/mcp_tools.go`, `internal/mcp`, the runner's
  `runners/remote-worker/src/lib/remote_git.ts`); the `internal/skills`
  catalog parse and aep-api's `spec/repo_store.go`, `skill_service.go`,
  `skill_manifest.go`; the vendored schemas in `designspec` and
  `securityspec` and their `*_vendor_test.go` drift tests; the turn relay's
  35 min budget (`internal/turns/relay.go`), which sits above the design
  agent's 30 min turn cap and equals the gateway route's allowance.
- **Log events are a contract** the on-call reads. The
  [log events tables](design/route-groups.md#log-events) have one row per
  structured `<area>.<event>` event in the code (level, fields, when), a list
  of the startup and shutdown messages, and the rules on what a line may carry
  (rows with raw error text are marked). A new or changed `<area>.<event>` goes
  in a table in the same commit.
- **Shutdown** keeps both sockets accepting for one 10 s window (ae-collab's
  8 s flush budget and the design agent's 8 s outbox drain fit inside it),
  then shuts down the listeners (15 s for requests in flight) beside the
  usage outbox's final flush (3 s), then stops the reaper, inside the pod's
  30 s grace. `TestShutdown_*` hold this order.

## Env

`internal/config` is the list: every missing or invalid key is named in one
error and values are never logged. The ResourceType's env and Secret keys are
in [`../design/README.md`](../design/README.md). Two behaviours to know:
`AE_GITHUB_OWNER` may be empty (it refuses every remote-git call), and
`AE_WEBHOOK_URL` is the org's relay channel when the Resource's
`webhookRelayUrl` is set, else the ResourceType's `webhookUrl` output.
Optional: `AE_LISTEN_PORT` (8082), `AE_HEALTH_PORT` (9082, not routed).

## Dead code

Tests do not count as consumers. Keep an unwired function only with a
`//deadcode:keep <reason>` line in its doc comment; test-support packages go
under an `edgetest/`, `projectstest/`, `repotest/` or `turnstest/` dir or a `_fortest.go` file (`scripts/deadcode.sh`).

## Image

`Dockerfile` context is this folder. The image is `alpine:3.21` with git and
tini (PID 1, forwards SIGTERM) and runs as UID 10001.
