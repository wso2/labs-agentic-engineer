# aep-api: the git boundary

`aep-api` holds no git state and no GitHub credential. Every git read, commit,
tag and GitHub call goes to the org's AE Studio pod, which holds the org's
gitpat and its clones
([ADR-0040](../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)).
This note is `aep-api`'s side of that hop: the credential, the ports, what
each failure means to each caller. The pod's side (its gates, its problem
codes, its clones) is in
[`ae-studio-tools/design/`](../../../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md).

| Piece | Code |
|---|---|
| The ports | `internal/sourcecontrol/git.go`, `ports.go` |
| The sentinels and `IsPermanent` | `internal/sourcecontrol/errors.go` |
| The adapter | `internal/clients/aestudiotools` (`client.go` is the package doc) |
| The in-memory pod for tests | `internal/clients/aestudiotools/aestudiotest` (`Fake`) |
| Sentinel → HTTP | `internal/edge/ae_studio_errors.go` (`classifyAEStudio`) |
| Sentinel → Temporal | `internal/delivery/run/errors.go` (`sourceControlErr`, `planErr`) |

## One credential: the AE-only client on `/internal/v1`

`aep-api` calls only the pod's `/internal/v1`, for every caller: user
requests, Temporal activities, webhook handlers, sweeps and the credential
validator. It presents the AE-only client's token (`client_credentials` of
`AE_STUDIO_INTERNAL_CLIENT_ID` / `AE_STUDIO_INTERNAL_CLIENT_SECRET` at
`AE_STUDIO_IDP_TOKEN_URL`) and `X-Impersonate-Org` = the pod's OU id. The
token is cached and refreshed once when the pod refuses it.

`aep-api` never forwards a user's JWT to the pod. The user check stays in
`aep-api`'s tenant gate; `aep-api` then acts as itself.

- The pod's `/v1` names projects, so a forwarded read would make the pod call
  back into `aep-api` for the repository: a control plane → dataplane →
  control plane loop.
- `/v1`'s path rules do not fit `aep-api`'s reads (the descriptor, the skills
  repository, the docs repository).
- Temporal, webhook and sweep callers have no user JWT.
- It would add no security: a compromised `aep-api` holds the AE-only
  credential anyway.

## The ports: shaped like the wire

`sourcecontrol.Git` (`Head`, `List`, `ReadFile`, `ReadBundle`, `ListTags`,
`Tag`, `Commit`) and the GitHub sub-ports (`RepoAdmin`, `IssueOps`,
`WebhookOps`, `TrashOps`, `SkillsMirrorOps`, `ReferencesOps`, `IdentityOps`)
mirror the pod's ops one to one, every call addressed by a `RepoRef` built
from the project's `git_repositories` row. There is no transaction over HTTP:
a writer commits with a `baseSha` per path and, on `ErrCommitConflict`,
re-reads and commits again (`CommitRetrying`, 3 attempts). `aep-api`'s own
writes (the descriptor, design commits, the skills library, the docs
repository) are complete files committed raw; the pod's apply core runs only
for agent and Room edits. Two adapters implement the ports:
`aestudiotools.Adapter` and `aestudiotest.Fake`.

- **Per-org endpoint.** The adapter resolves an org's tools URL and OU id
  from its AE Studio status and keeps them 30 s. A transport error, a 403 on
  the token or a gateway with no pod behind it drops the entry.
- **One read cache.** A read at a 40-hex commit sha never changes, so it is
  kept in one byte-bounded LRU (64 MiB per replica), keyed by org, repository,
  sha, op and arguments. A branch tip or a tag always makes the hop, and its
  answer is stored only under the sha it returned.
- **`sourcecontrol.Local()`** asks the pod to answer from its clone without a
  fetch. The status poll reads `head` and `tags` this way, then trees and
  files at those shas from the cache.
- **No retries in the adapter** except the one token refresh. Temporal, the
  sweeps and the callers own retries.

## What a failure means, per caller

Three sentinels say the pod cannot answer for the org's repositories. Every
caller branches on them with `errors.Is`.

| Sentinel | When | User request | Temporal | Sweeps |
|---|---|---|---|---|
| `ErrAEStudioAbsent` | no AE Studio for the org: GitHub is not connected | 409 `github_not_connected` | permanent: non-retryable | skip the org |
| `ErrAEStudioUnavailable` | provisioning, failed, unreachable, out of disk, its IdP down, or its call to `aep-api` failed | 503 `ae_studio_unavailable`, `Retry-After: 5` | retried under the activity's policy, which waits out a roll | skip the org this pass |
| `ErrAEStudioMisconfigured` | the pod refused `aep-api`'s AE-only token after one refresh, or `aep-api` has no AE-only credentials | 503 `ae_studio_misconfigured`, no `Retry-After` | permanent: an operator fixes it, no retry does | skip the row |

`ErrAEStudioMisconfigured` logs one of:

| Event | Fields | When |
|---|---|---|
| `ae_studio.auth_failed` | `org`, `status` | the pod refused a freshly fetched token (401, or a 403 its auth layer wrote) |
| `ae_studio.misconfigured` | `org`, `reason` | `client_credentials_missing` (logged once per process) or `token_refused` (the IdP refused the client) |

Two more answers reach users through the same map:

| Error | User request | Temporal |
|---|---|---|
| `ErrOwnerNotAllowed`: the repository is not under the org's connected GitHub account | 409 `owner_not_allowed` | permanent |
| `*RateLimitedError`: GitHub's rate limit, relayed by the pod | 429 `github_rate_limited`, `Retry-After` as GitHub said | retried |

`classifyAEStudio` is the one sentinel → HTTP map. It runs in the shared
error writer, so `/api/v1` and the internal route groups answer the same,
unless a slice already gave the request a 4xx of its own. Which errors are
permanent is `IsPermanent`'s list; `sourceControlErr` and `planErr` make
those non-retryable in Temporal. How each pod problem code maps back to a
sentinel is the pod's table: [Problem codes as aep-api reads
them](../../../components/dataplane/ae-system-project/ae-studio/ae-studio-tools/design/route-groups.md#problem-codes-as-aep-api-reads-them).

## Callers

| Caller | Pod ops | On absent or unavailable |
|---|---|---|
| Design and spec reads (`spec`) | `ReadBundle`, `ReadFile`, `List` at the head or a sha | user 409/503; Temporal retries or fails permanent |
| Status poll (`projects`) | `Head` and `ListTags` with `Local()`, then reads at those shas | degrades: 200, the spec facts read `unavailable` with the reason, build and deploy intact (`specUnavailableReason`) |
| Project create (`projects`) | `RequireReady`, then `CreateOrgRepo`, `RegisterWebhook` | `RequireReady` runs before the OpenChoreo project exists, so a refused create leaves nothing half-made; a failed repo create stops the create and compensates |
| Project delete (`sourcecontrol`) | `DeleteWebhook`, `TrashRepo` | best-effort |
| Version tags (`spec`) | `Tag`; `ErrTagAlreadyExists` recomputes the next tag | user 503 |
| Skills library, org resource docs | reads and `Commit` on the `_skills` and docs repositories | user 503 |
| Skills mirror (`spec`) | `MirrorSkills` | warn and continue |
| Reference upload (`spec/files`) | `PutReferences`, streamed | user 503 (`disk_full`), 400 `reference_rejected` |
| Kickoff (`spec`) and Plan (`delivery/task`) | `StartTurn` | `ErrTurnInProgress` and unavailable retry in Temporal; the kickoff never fails the create |
| Run supervisor and validation (`delivery`) | issues, milestones, pull requests, reads at a sha | `sourceControlErr`: unavailable retries, absent fails permanent |
| Eventcore handlers and sweeps (`delivery/eventcore`) | issues, milestone counts, pull requests, design reads, hook repair | a handler 5xx is replayed, then swept; the hook repair skips an org whose pod is absent or unavailable this pass |
| SRE issue writes (`/internal/v1/sre/…`) | issues | 503 or 409 to the SRE caller, no fallback |
| Credential validator (`organization`) | `GitHubIdentity` | GitHub's 401/403/404 on the gitpat is unauthorized; anything else, the pod's state included, skips the tick |
