# 08 Git and GitHub

After gitpat submit, the control plane can never read the gitpat. Every operation that uses it runs in the org dataplane, on a container that runs no model. GitHub is reached with the gitpat only; a GitHub App may come later.

## Where each operation runs

| Operation | Where | Reached by |
|---|---|---|
| clone, fetch, commit, push (spec files, Files API apply) | `ae-studio-tools` | flow 3 from `aep-api` / Temporal on `/internal/v1/*`, with the AE-only M2M token; Unix socket from `ae-collab` |
| GitHub REST: issues, comments, labels, milestones, pull requests, merge | `ae-studio-tools` | flow 3 on `/internal/v1/*`, with the AE-only M2M token. This includes the writes Temporal does after a server-started turn (for example issues from a plan). |
| git-only reads for the browser: spec file reads, issue-backed task lists, repo reads | `ae-studio-tools` | `/v1/*`: flow 14 from the browser, and flow 3 when `aep-api` forwards a user request. Both carry the user JWT. |
| MCP remote-git (file contents, code search) | `ae-studio-tools` for `ae-design-agent`; `ae-coding-tools` for a coding run | MCP Unix socket from `ae-design-agent`, then flow 9; `127.0.0.1` from `ae-coding-agent`, then flow 7b |
| repo create | GitHub call on `ae-studio-tools`; the Postgres row on `aep-api` | flow 3 on `/internal/v1/*`, AE-only M2M token only |
| skills mirror: copy the org's skills into a project repo, reading the org-skills repo | `ae-studio-tools` (`aep-api` has no gitpat) | flow 3 on `/internal/v1/*`, AE-only M2M token only |
| webhook **register** | `aep-api`, during gitpat submit, with the in-memory gitpat. The hook URL is the public address of `ae-studio-tools`. | [05-lifecycle.md](05-lifecycle.md) |
| webhook **verify** | `ae-studio-tools`, with the org HMAC from vault | flow 5 |
| gitpat check at submit | `aep-api`, with the gitpat from the request body, in memory | flow 1 |
| coding run: git and GitHub for this run's repository | `ae-coding-tools` | `127.0.0.1` from `ae-coding-agent`; flow 7b to GitHub |

`aep-api` may use the gitpat only from the gitpat submit request body. After that it never reads it again. Build clone secrets are passed as **references** only.

## Routes on `ae-studio-tools`

`ae-studio-tools` offers low-level operations, split by prefix. Each prefix has its own middleware.

| Prefix | Accepts | Carries |
|---|---|---|
| `/v1/*` | The user JWT only. | Git-only reads: spec files, the repo tree, issue lists and reads. The browser calls them (flow 14), or `aep-api` passing on a user request (flow 3). |
| `/internal/v1/*` | The AE-only M2M token only, with the client id pinned and `X-Impersonate-Org` = this pod's org. It refuses a user JWT. It has its own OpenAPI group. | The low-level git and GitHub operations: commit and push, issues, comments, labels, milestones, pull requests and merge, repo create, the skills mirror, reads for Temporal (flow 3). The server-started turn (flow 2). |

The delivery engine (Temporal and webhook handling) stays on `aep-api`. It decides what to write and calls `/internal/v1/*` for each step. Users never call a write directly.

A leaked AE-only M2M token can do almost any GitHub operation in any org's repositories while it lives, by changing `X-Impersonate-Org`. Only `aep-api` holds it. Org-bound machine tokens narrow it to one org ([07-identity-and-tokens.md](07-identity-and-tokens.md#planned)).

## Webhook path

The webhook path is flows 5 and 6 in the picture in [03-components.md](03-components.md).

### Where GitHub posts (flow 5)

`ae-studio-tools` listens on a webhook path of its public address. The gateway checks no JWT and no API key; it only ends TLS. `ae-studio-tools` checks `X-Hub-Signature-256` with the org HMAC.

This route is different from the Room WebSocket and from the routes that check a Platform IdP token (flows 2, 3 and 14). The webhook route takes no token. The same receiver serves both installs:

| Install | What GitHub calls |
|---|---|
| WSO2 Cloud | Public org kgateway (TLS and CORS, no token check), the webhook path on `ae-studio-tools`. |
| Local | A smee.io channel. The in-cluster smee client forwards to the same path. smee is not part of the Cloud threat model. |

### What crosses back (flow 6)

After the HMAC check, `ae-studio-tools` calls `aep-api` as the publisher client and sends:

- `X-GitHub-Delivery`
- `X-GitHub-Event`
- the JSON body

It never sends the signature or the HMAC secret.

`aep-api` takes the org from the publisher client token (`ouHandle`) and looks up the event's repository only among that org's repositories. It does not store or dispatch an event for a repository the org does not have. `aep-api` redacts the body as it does today, dedups on the delivery id, stores it, and dispatches to Temporal in the same process. `ae-studio-tools` returns GitHub's status from that result, so a failure is still retried by GitHub.

### What gitpat submit registers

gitpat submit waits until `ae-studio-tools` can accept the POST, then registers that address **once**. If the wait ends with no address, no hook is registered. There is no second URL, and the control-plane webhook URL is not a stand-in. The full procedure is in [05-lifecycle.md](05-lifecycle.md).

The control-plane webhook RestApi is removed once the org kgateway route is live. It is an AE change, listed in [13-change-inventory.md](13-change-inventory.md).

## Not chosen, and why

- **A browser call to repo create or the skills mirror on `ae-studio-tools`.** The browser could skip `aep-api`. Those routes accept only the AE-only M2M token ([07-identity-and-tokens.md](07-identity-and-tokens.md)).
- **A few high-level commands on `/internal/v1/*` instead of low-level operations.** A leaked M2M token could do less, but more GitHub logic moves from `aep-api` into `ae-studio-tools`.
- **Temporal activities in the dataplane.** Needs Temporal reachable from the dataplane, and Temporal has no TLS or auth there.
- **Git writes stay on `aep-api`.** Needs a control-plane read of the gitpat, a GitHub App, or a Secret API read of the value.
- **HMAC checked on `aep-api`, by a platform HMAC or a Secret API read.** Either one secret for all orgs, or a control-plane read path to a value.
- **Local ingress as the only front door.** A local cluster is often not reachable from GitHub; smee keeps one receiver for both installs.
- **GitHub keeps posting to the control-plane RestApi, which forwards the raw POST for verify.** Keeps the webhook on the control plane.
- **Register a placeholder URL before `ae-studio-tools` is ready.** `aep-api` cannot patch the hook later, because it has no gitpat.
- **Send a delivery-id pointer or a field projection instead of the body.** `aep-api` needs the body to store and dispatch the event.
- **Per-org API Platform gateway in front of the webhook.** App Factory orgs do not get one, and it is not the later path.
