# ae-studio-tools: route groups and gates

One rule shapes the container: **one prefix per caller, one gate per
prefix.** A Unix socket counts as a prefix: it has one client container and
one gate, the mount. The trust model is
[ADR-0041](../../../../../../docs/decisions/ADR-0041-ae-studio-checks-platform-idp-tokens-itself.md);
the webhook route is
[ADR-0043](../../../../../../docs/decisions/ADR-0043-github-delivers-each-repositorys-webhooks-to-ae-studio.md).
The mount tables are `internal/edge/routes.go` (`Routes`,
`FilesSocketRoutes`, `MCPSocketRoutes`); the gates are `internal/auth/verify.go`.

## Gate table

Every refusal is `application/problem+json` with a stable `code`.

| Prefix | Caller | Check, in order | Refusals |
|---|---|---|---|
| `/v1/` (port 8082) | the browser (console) | Platform IdP user JWT: RS256, exact `iss` (`AE_IDP_ISSUER`), `exp`, `aud` in `AE_USER_AUDIENCES`, a `sub`, not a `client_credentials` token; then the org rule: `ouId` = `AE_ORG_ID` and `ouHandle` = `AE_ORG_HANDLE`. Then the contract validator. | 401 `unauthenticated` (with `WWW-Authenticate`), 403 `org_mismatch`, 503 `idp_unavailable` + `Retry-After: 5`, 400 `path_invalid` |
| `/internal/v1/` (port 8082) | `aep-api` | Body cap per operation (1 MiB, `create-commit` 16 MiB, `start-repo-turn` 4 MiB, `put-repo-references` 80 MiB), ahead of the gate. AE-only M2M: `client_credentials`, `aud` and `client_id` = `AE_M2M_CLIENT_ID`, no `ouId` claim; then `X-Impersonate-Org` = `AE_ORG_ID`. Then the validator, then the owner guard: a `/internal/v1/repos/{owner}/{repo}/…` owner must equal `AE_GITHUB_OWNER` (case-insensitive; unset refuses all). | 413 `payload_too_large`, 401 `unauthenticated`, 403 `org_mismatch`, 503 `idp_unavailable`, 400 `validation_failed`, 403 `owner_not_allowed` |
| `POST /webhooks/github` (port 8082) | GitHub, or the local `webhook-relay` | 8 deliveries in flight, body 25 MiB, body read within 10 s, then `X-Hub-Signature-256` against `GITHUB_WEBHOOK_SECRET` (current secret only). No token. | 503 `busy`, 413 `payload_too_large`, 408 `request_timeout`, 400 `body_unreadable`, 401 `signature_invalid` |
| Files socket `AE_FILES_SOCKET` | `ae-collab` | The mount. 40 s per request (inside `ae-collab`'s 45 s call deadline), 25 MiB body, then the validator. | 413 `payload_too_large`, 400 `path_invalid` |
| MCP socket `AE_MCP_SOCKET` | `ae-design-agent` | The mount. 20 s per request, 1 MiB body, the validator, then the tool allow-list (below). | 413 `payload_too_large`, 400 `invalid_request` |
| `/healthz`, `/readyz` (port 9082) | kubelet | none; the port is not in the Service and not routed | |
| anything else | | | 404 `not_found` |

Each gate runs before route matching inside its group, so an unauthenticated
caller sees 401 or 403 for an unknown path before it could learn the path is
404. A path `ServeMux` would redirect (a bare group root, dot segments, `//`)
is a 404 behind its group's gate. The Turn socket (`AE_TURN_SOCKET`) runs the
other way: `ae-design-agent` serves it and this container dials it.

A user token never clears `/internal/v1`: the M2M gate needs a
`client_credentials` token with no org claim. The AE-only token never clears
`/v1`: the user gate refuses every `client_credentials` token.

## How a request names its repository

- **`/internal/v1` names owner and repo** (`/internal/v1/repos/{owner}/{repo}/…`).
  `aep-api` holds the project's `git_repositories` row and sends what it
  records. The owner guard keeps every such op on the org's connected GitHub
  account. `POST /internal/v1/repos` (create) and `POST /internal/v1/trash`
  carry the owner in the body and apply the same check.
- **`/v1` and the sockets name a project.** The container resolves it on
  every request through `aep-api`
  (`GET /internal/v1/ae-studio/projects/{projectName}/repository`), with no
  cache. 404 there is `project_unknown`. The Files socket contract has no
  owner or repo field at all, so a request naming one is refused before any
  lookup. The turns op names its project in the body and its owner/repo must
  be that project's repository.
- **Why the browser and the sockets cannot name a repository:** the gitpat
  reaches every repository its GitHub user can reach, including non-AE
  repositories under the same owner, personal ones and other GitHub orgs. A
  caller-named repository on `/v1` or a socket would read from them, or push
  to them through a Room. An owner check cannot tell an AE repository from
  any other under the same owner; only `aep-api`'s `git_repositories` knows
  which repositories are this org's.
- **Cost accepted:** `/v1` reads, Room saves and turn starts fail while
  `aep-api` is down (503 `aep_api_unavailable` + `Retry-After: 5`). When the
  pod's own credentials are refused the answer is the same 503 without
  `Retry-After`, and the container logs `aep_api.auth_rejected {op, project,
  cause}`.

`/v1` is read-only: files list, read and bundle, under `specs/`,
`tests/acceptance/report.json` and a one-segment `workload.yaml`, at a hex
ref. A user's write goes to `aep-api`, which uses `/internal/v1`.

## Calls out to aep-api

The container holds two `aep-api` credentials; each opens one route group
(ADR-0041 decision 5).

| Credential | Env | Used for |
|---|---|---|
| the org's `ae-studio-<org>` client | `AE_STUDIO_CLIENT_ID`, `AE_STUDIO_CLIENT_SECRET` | `/internal/v1/ae-studio/*`: project and skills repository lookups, dependency completions, turn usage, the webhook forward |
| the org's publisher client | `AE_PUBLISHER_CLIENT_ID`, `AE_PUBLISHER_CLIENT_SECRET` | `/internal/v1/mcp` only |

The `ae-studio-<org>` token never leaves this container. The webhook forward
posts the verified body byte for byte, inside an 8 s budget with at most two
retries on a transport error or 5xx. GitHub then sees 200 when `aep-api`
answered 2xx or a 4xx other than 401, 403 and 429; else 503
`aep_api_unavailable`. A 401, 403 or 429 refuses the pod's credential or rate,
not the delivery, so it reads as unavailable.

## MCP tool allow-list

`AllowedTools` (`internal/mcp/tools.go`, pinned by `TestAllowedTools_Pinned`)
holds 11 names. `tools/list` answers exactly these, locally. `tools/call` of
any other name is JSON-RPC `-32602`; a method other than `initialize`,
`tools/list` and `tools/call` is `-32601`.

- **Served in the pod with the gitpat:** `get_remote_git_file_contents`,
  `search_remote_git_code`. The owner must equal `AE_GITHUB_OWNER`; `repo`
  must be a plain name; search scope qualifiers are refused.
- **Forwarded to `aep-api` `POST /internal/v1/mcp` with the publisher token**
  (name and arguments only): `list_external_resources`,
  `get_external_resource_schema`, `list_org_endpoints`,
  `list_org_component_endpoints`, `list_platform_resource_types`,
  `list_groups`, `validate_openapi_spec`, `fetch_openapi_spec`,
  `slice_openapi_spec`.

The nine forwarded descriptors match `aep-api`'s
`internal/dependencies/mcpdiscovery/mcp_tools.go`; the two remote-git ones match the
coding runner's `runners/remote-worker/src/lib/remote_git.ts`. A change to a
tool changes all three.

## Problem codes as aep-api reads them

`aep-api`'s client (`services/aep-api/internal/clients/aestudiotools/errors.go`)
maps each answer back to the error its callers branch on with `errors.Is`.

| Code | Status | `aep-api` error |
|---|---|---|
| `project_unknown` | 404 | `sourcecontrol.ErrRepoNotFound` |
| `ref_not_found`, `path_not_found` | 404 | `ErrRefNotFound`, `ErrPathNotFound` |
| `issue_not_found`, `milestone_not_found` | 404 | `ErrIssueNotFound`, `ErrMilestoneNotFound` |
| `tag_exists`, `not_fast_forward`, `repo_name_conflict` | 409 | `ErrTagAlreadyExists`, `ErrRefNotFastForward`, `ErrRepoNameConflict` |
| `conflict` (with `conflicts[]`) | 409 | `*sourcecontrol.CommitConflictError` |
| `turn_in_progress` (with `activeTurnId`) | 409 | `aestudiotools.ErrTurnInProgress` |
| `owner_not_allowed` | 403 | `ErrOwnerNotAllowed` (a verdict on the request, never a token fault) |
| `reference_rejected` | 400 | `ErrReferenceRejected` |
| `validation_failed` on `get-head` or `list-tree` | 400 | `ErrRefInvalid` |
| `github_rate_limited` (+ `Retry-After`) | 429 | `*sourcecontrol.RateLimitedError` |
| `github_error` (+ `githubStatus`) | 502 | `*sourcecontrol.HTTPStatusError` with GitHub's status |
| `disk_full`, `aep_api_unavailable`, any other 503, a gateway's 404/502/503/504 with no problem body | 503 and others | `ErrAEStudioUnavailable` |
| 401, or a 403 the gate wrote (`org_mismatch` or no body) | 401, 403 | one retry with a fresh token, then `ErrAEStudioMisconfigured` and `ae_studio.auth_failed {org, status}` |

Anything else is an `aestudiotools.StatusError`. The container retries no
GitHub call itself, except a commit's non-fast-forward loop: `aep-api` and
Temporal own retries.

## Log events

All JSON `slog` on stdout. No event carries a body, a token, a signature,
tool arguments or a usage record's values. Git's and GitHub's own error text
(it names the command and the clone URL) and the project resolver's error text
never reach a line: a failure is logged by class, `githubStatus` or `cause`.
A new event follows both rules and joins this table.

| Event | Fields | When |
|---|---|---|
| `repo.root` | `root_layout` | once at start, the studio-data root's layout |
| `internal.access` | `method`, `path`, `status`, `ms` | every `/internal/v1` request, after the answer (no headers, no query) |
| `webhook.forwarded` | `delivery`, `event`, `status` | `aep-api` took or refused the delivery for good; `status` is `aep-api`'s |
| `webhook.rejected` | `delivery`, `event`, `reason`, `status` (forward failures only) | `reason` is the refusal code (`busy`, `payload_too_large`, `request_timeout`, `body_unreadable`, `signature_invalid`) or `aep_api_unavailable`, whose `status` is `aep-api`'s last (0 when not reached) |
| `turns.start` | `kind`, `project`, `turnId` | the agent accepted the turn |
| `turns.result` | `kind`, `project`, `turnId`, `status` | the turn's stream ended |
| `turns.socket_failed` | the turn's fields, then `error` (unreachable) or `status` (any other answer) | the Turn socket failed the relay (503 `agent_unavailable`, 502 `agent_error`) |
| `mcp.tools_call` | `tool`, `repo` (remote-git only), `upstream` (`pod` or `aep-api`) | each allowed `tools/call` |
| `mcp.upstream_failed` | `method`, `error` | `aep-api` could not answer a forwarded call (502 `aep_api_unavailable`) |
| `repo.clone` | `repo`, `mode` (`bare`), `ms` | a cold clone finished |
| `files.git_failed` | `op`, `project`, `repo`, `class` | a Files op's git failure (502 `github_error`) |
| `files.disk_full` | `op`, `project` or `repo` | a Files op or a reference upload met a full disk (503 `disk_full`) |
| `repo.git_failed` | `op`, `repo`, `githubStatus` | a `/internal/v1` git op's failure (502 `github_error`) |
| `repo.disk_full` | `op`, `repo` | a `/internal/v1` git op met a full disk |
| `repo.commit_conflict` | `op`, `repo` | a commit's tree kept changing, nothing applied (409 `conflict`) |
| `repo.not_fast_forward` | `op`, `repo` | the branch moved during a commit (409 `not_fast_forward`) |
| `github.call_failed` | `op`, `repo`, `status`, `githubStatus` | a GitHub op was refused or failed |
| `github.repo_owner_mismatch` | `owner`, `githubOwner`, `repo` | create-repo answered a different owner than asked, so nothing is returned (403 `owner_not_allowed`) |
| `aep_api.unavailable` | `op`, `project` | the project resolver could not reach `aep-api` (503, `Retry-After`) |
| `aep_api.auth_rejected` | `op`, `project`, `cause` (`aep_api` or `token_endpoint`) | `aep-api` or the token endpoint refused the pod's own credentials; the operator's signal |
| `auth.idp_unavailable` | `gate` (`user` or `m2m`) | the IdP's key set could not be fetched |
| `usage.dropped` | `count`, `reason` (`outbox_full` or `rejected`), `status` (rejected only) | records left the outbox for good |
| `usage.send_failed` | `count`, `error` | a batch will be retried |
| `reaper.sweep` | `usedBytes`, `budgetBytes`, `pct`, `evicted` | each sweep of the studio-data root |
| `skills.mirror_conflict` | `repo`, `attempt` | the skills mirror commit lost a race and recomputes |
| `snapshot.idea_unreadable` | `project`, `step` (`read` or `parse`), `error` (read only) | the project descriptor's idea could not be read; the snapshot answers no idea |
| `references.overlay_failed` | `project`, `step`, `error` | a reference overlay step failed (best effort; the lookup still answers) |

`delivery` and `event` are sender-chosen headers logged before the signature
is checked, so both are cut to 64 runes.
