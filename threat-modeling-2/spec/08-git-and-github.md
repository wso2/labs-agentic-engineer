# 08 Git and GitHub

After gitpat submit, the control plane can never read the gitpat. Every operation that uses it runs in the org dataplane, on a container that runs no model. GitHub is reached with the gitpat only; a GitHub App may come later.

## Where each operation runs

| Operation | Where | Reached by |
|---|---|---|
| clone, fetch, commit, push (spec files, Files API apply) | `ae-studio-tools` | flow 3 from `aep-api` / Temporal; `localhost` from `ae-collab` |
| GitHub REST: issues, pull requests, milestones, merge | `ae-studio-tools` | flow 3 |
| MCP remote-git | `ae-studio-tools` | flow 3 |
| repo create | GitHub call on `ae-studio-tools`; the Postgres row on `aep-api` | flow 3 |
| webhook **register** | `aep-api`, during gitpat submit, with the in-memory gitpat. The hook URL is the public address of `ae-studio-tools`. | [05-lifecycle.md](05-lifecycle.md) |
| webhook **verify** | `ae-studio-tools`, with the org HMAC from vault | flow 5 |
| gitpat check at submit | `aep-api`, with the gitpat from the request body, in memory | flow 1 |
| coding run: git and GitHub for this run's repository | `ae-coding-tools` | `localhost` from `ae-coding-agent`; flow 7b to GitHub |

`aep-api` may use the gitpat only from the gitpat submit request body. After that it never reads it again. Build clone secrets are passed as **references** only.

## Webhook path

The webhook path is flows 5 and 6 in the picture in [03-components.md](03-components.md).

### Where GitHub posts (flow 5)

`ae-studio-tools` listens on a webhook path of its public address. The gateway checks no JWT and no API key; it only ends TLS. `ae-studio-tools` checks `X-Hub-Signature-256` with the org HMAC.

This route is different from the Room WebSocket and from the control-plane service route (`aud` = `ae-studio-tools`). The same receiver serves both installs:

| Install | What GitHub calls |
|---|---|
| WSO2 Cloud | Public org kgateway, TLS only, the webhook path on `ae-studio-tools`. |
| Local | A smee.io channel. The in-cluster smee client forwards to the same path. smee is not part of the Cloud threat model. |

### What crosses back (flow 6)

After the HMAC check, `ae-studio-tools` calls `aep-api` as the publisher client and sends:

- `X-GitHub-Delivery`
- `X-GitHub-Event`
- the JSON body

It never sends the signature or the HMAC secret.

`aep-api` redacts the body as it does today, dedups on the delivery id, stores it, and dispatches to Temporal in the same process. `ae-studio-tools` returns GitHub's status from that result, so a failure is still retried by GitHub.

### What gitpat submit registers

gitpat submit waits until `ae-studio-tools` can accept the POST, then registers that address **once**. If the wait ends with no address, no hook is registered. There is no second URL, and the control-plane webhook URL is not a stand-in. The full procedure is in [05-lifecycle.md](05-lifecycle.md).

Today's hook still ends on the control-plane webhook RestApi. That is GAP-1 ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)).

## Not chosen, and why

- **Git writes stay on `aep-api`.** Needs a control-plane read of the gitpat, a GitHub App, or a Secret API read of the value.
- **HMAC checked on `aep-api`, by a platform HMAC or a Secret API read.** Either one secret for all orgs, or a control-plane read path to a value.
- **Local ingress as the only front door.** A local cluster is often not reachable from GitHub; smee keeps one receiver for both installs.
- **GitHub keeps posting to the control-plane RestApi, which forwards the raw POST for verify.** Keeps the webhook on the control plane.
- **Register a placeholder URL before `ae-studio-tools` is ready.** `aep-api` cannot patch the hook later, because it has no gitpat.
- **Send a delivery-id pointer or a field projection instead of the body.** `aep-api` needs the body to store and dispatch the event.
- **Per-org API Platform gateway in front of the webhook.** App Factory orgs do not get one, and it is not the later path.
