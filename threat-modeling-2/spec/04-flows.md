# 04 Flows

Every network flow in the intended architecture, in WSO2 Cloud. The numbers match the pictures in [03-components.md](03-components.md) and [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md). Token details are in [07-identity-and-tokens.md](07-identity-and-tokens.md). Local differences are in [11-local-vs-cloud.md](11-local-vs-cloud.md).

There is no private HTTP path from the control plane into the org dataplane. Every hop between the planes goes through a public gateway and carries a token. Every token is issued by the Platform IdP.

## Numbered flows

"The `ae-studio` check" below means: signature against the public Platform IdP JWKS, `iss=platform-idp`, `exp`, `aud` on the container's allow-list, then the org and role rule ([07-identity-and-tokens.md](07-identity-and-tokens.md)).

| # | From → to | Via | What | Token | Checked by, and how |
|---|---|---|---|---|---|
| 1 | Browser → `aep-api` | The console's own web server, then `aep-api` inside the control plane. A public gateway route for the console is planned. | Console REST for work that needs Postgres, OpenChoreo or Temporal: settings, builds, deploys, project create, usage. The `ae-studio` status and URL lookup on console load. | Platform IdP user JWT | `aep-api` itself: signature against the Platform IdP JWKS, `iss=platform-idp`, `aud`, `exp`. Then it authorizes user and org. The web server checks nothing. |
| 2 | `aep-api` → `ae-design-agent` | Org kgateway, TLS only | Server-started turns only: a high-level request such as "plan milestone M" or "kick off project P", and the marketplace chat. The pod runs the turn and its GitHub work. Temporal owns order, retries and timeouts. | The forwarded user JWT, or with no user the AE-only M2M (machine-to-machine) token + `X-Impersonate-Org` | `ae-design-agent`: the `ae-studio` check. For the M2M token: client id pinned, `X-Impersonate-Org` = this pod's org. |
| 3 | `aep-api` / Temporal → `ae-studio-tools` | Org kgateway, TLS only | Git operations, GitHub REST, repo create and the skills mirror, when an `aep-api` route or Temporal work needs them | The forwarded user JWT, or with no user the AE-only M2M token + `X-Impersonate-Org` | `ae-studio-tools`: as for flow 2. |
| 4 | Browser → `ae-collab` | Org kgateway, TLS only | Room WebSocket | Platform IdP user JWT, sent on connect, not as a cookie | `ae-collab`: the `ae-studio` check. |
| 5 | GitHub → `ae-studio-tools` | Org kgateway, TLS only, webhook path | Webhook POST | None at the gateway | `ae-studio-tools`: `X-Hub-Signature-256` with the org HMAC. |
| 6 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | Verified webhook: `X-GitHub-Delivery`, `X-GitHub-Event`, JSON body. No signature, no HMAC. | Publisher client token (`client_credentials` at the Platform IdP) | Gateway `jwt-auth`; `aep-api` checks `aud` prefix and takes the org from `ouHandle`. It looks up the event's repository only among that org's repositories. `aep-api` redacts, dedups on the delivery id, stores, and dispatches to Temporal. `ae-studio-tools` returns GitHub's status from that result. |
| 7a | `ae-coding-tools` → `aep-api` | Public `aep-api` gateway | This run's platform calls, including the platform MCP tool calls of `ae-coding-agent`. `ae-coding-tools` serves the two remote-git tools itself over flow 7b. | Publisher client token | Gateway `jwt-auth`; `aep-api` as for flow 6. `ae-coding-tools` refuses other platform calls. |
| 7b | `ae-coding-tools` → GitHub | Dataplane egress | Git and GitHub for this run's repository | gitpat | GitHub. `ae-coding-tools` refuses other repositories. |
| 8 | `ae-design-agent`, `ae-coding-agent` → Anthropic API | Dataplane egress, public 443 | Model calls | The Anthropic key each container mounts | Anthropic. |
| 9 | `ae-studio-tools` → GitHub | Dataplane egress | Clone, fetch, push, GitHub REST | gitpat | GitHub. |
| 10 | `aep-api` → SM API | Control plane | Write gitpat, org HMAC, Default key, Coding agent token, `ae-studio-<org>` client secret | The user's Platform IdP JWT, passed through (WSO2 Cloud). Writes with no user on the request: open (O-3). | SM API writes vault through the OpenChoreo Secret API. It returns keys and `secretReferenceName` only. |
| 11 | ESO → vault | ClusterSecretStore | Read secret values into the dataplane | Not stated in this spec (open item) | ExternalSecret refresh 15s → Kubernetes Secret → container env. |
| 12 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | Platform MCP tool calls for `ae-design-agent`, one tool call per request: the nine tools that run on `aep-api` (external resources, org endpoints, platform resource types, groups, and the OpenAPI fetch, validate and slice tools). `ae-studio-tools` serves the two remote-git tools itself over flow 9. | Publisher client token | Gateway `jwt-auth`; `/internal/v1/mcp` on `aep-api` checks `aud` prefix and takes the org from `ouHandle`. It accepts no other token. |
| 13 | Browser → `ae-design-agent` | Org kgateway, TLS only | Turn start and turn SSE (the stream of a running turn) | Platform IdP user JWT | `ae-design-agent`: the `ae-studio` check. It holds the one-active-turn lock, the conversation thread and the snapshots in the pod. |
| 14 | Browser → `ae-studio-tools` | Org kgateway, TLS only | Git-only REST: spec file reads, issue-backed task lists, repo reads | Platform IdP user JWT | `ae-studio-tools`: the `ae-studio` check. |
| 15 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | A batch of finished design-turn usage records, every few minutes and on pod shutdown | Publisher client token | Gateway `jwt-auth`; `aep-api` as for flow 6. The ledger keys on the turn id, so a batch sent twice counts once. |

Also not numbered:

- On the control plane: `aep-api` → Postgres (rows only); `aep-api` → OpenChoreo control plane (Ensure the Project, ProjectReleaseBinding and Resource; read the Resource's ResourceReleaseBinding for status and URLs; create the coding Job); `aep-api` → Platform IdP (`client_credentials` for the AE-only M2M token; create `aep-publisher-<org>` and `ae-studio-<org>`).
- From the dataplane, over egress: `ae-collab`, `ae-design-agent` and `ae-studio-tools` → Platform IdP public JWKS (`https://platform-idp-<env>.gateway.<base>/oauth2/jwks`, no authentication). `ae-studio-tools` and `ae-coding-tools` → Platform IdP (`client_credentials` for the publisher client token). `ae-studio-tools` also → Platform IdP (`client_credentials` for the `ae-studio-<org>` token).

## What the browser calls

| Call | Browser target | Token the browser sends |
|---|---|---|
| On load: the `ae-studio` status and URLs | Console web server, then `aep-api` (flow 1) | Platform IdP user JWT |
| Settings, builds, deploys, project create, usage | Console web server, then `aep-api` (flow 1) | Platform IdP user JWT |
| Turn start, turn SSE | Public URL of Resource `ae-studio`, container `ae-design-agent` (flow 13) | Platform IdP user JWT |
| Room WebSocket | Public URL of Resource `ae-studio`, container `ae-collab` (flow 4) | Platform IdP user JWT |
| Git-only REST: spec file reads, issue-backed task lists, repo reads | Public URL of Resource `ae-studio`, container `ae-studio-tools` (flow 14) | Platform IdP user JWT |

On load, `aep-api` reads the ResourceReleaseBinding of Resource `ae-studio`: the URLs from `status.outputs` (the ResourceType publishes them as outputs, because a Resource has no invoke URL) and readiness from the `Ready` condition. Until `ae-studio` is ready, the console shows a loader.

The calls to `ae-studio` are cross-origin. They send the user JWT as a bearer token, never a cookie. The Resource routes answer CORS preflight at the gateway, with no token ([03-components.md](03-components.md)).

Routes that also need Postgres, OpenChoreo or Temporal stay on `aep-api`. When they need git or GitHub, `aep-api` calls `ae-studio-tools` over flow 3.

## Inside the pods (not numbered)

All containers in a pod share one network, so any container can reach any `localhost` port. A call inside a pod carries no token **only** when a container that should not make it cannot reach the listener.

| Pod | From → to | Channel | What |
|---|---|---|---|
| `ae-studio` | `ae-collab` → `ae-studio-tools` | Unix socket in an emptyDir mounted only into these two containers. No token. | Files API: `files/bundle`, `files/apply`, seed, flush. `ae-studio-tools` commits and pushes. `ae-design-agent` cannot see the socket. |
| `ae-studio` | `ae-design-agent` → `ae-studio-tools` | Unix socket in a second emptyDir mounted only into these two containers. No token. | Platform MCP tools, fixed allow-list of eleven read-only tools. Two remote-git tools run on `ae-studio-tools` with the gitpat (flow 9); the other nine go to `aep-api` over flow 12. The same socket answers the Room-join token request (an `ae-studio-<org>` token) and takes finished-turn usage records for flow 15. `ae-collab` cannot see this socket. |
| `ae-studio` | `ae-studio-tools` → `ae-design-agent` | Shared named emptyDir. No call. | Snapshots written by `ae-studio-tools`, read by `ae-design-agent`. |
| `ae-studio` | `ae-design-agent` → `ae-collab` | `localhost` Room WebSocket. The `ae-studio-<org>` token from `ae-studio-tools`. | Join the turn's Room. `ae-collab` checks the token: Platform IdP JWKS, `iss`, `exp`, `aud` = this org's `ae-studio-<org>`, `ouId` and `ouHandle` = this pod's org. See [07-identity-and-tokens.md](07-identity-and-tokens.md). |
| coding Job | `ae-coding-agent` → `ae-coding-tools` | `127.0.0.1` listener, not an endpoint. No token. | Git, GitHub and platform actions for this run, and the platform MCP tools (remote-git served by `ae-coding-tools`, the rest over flow 7a). `ae-coding-tools` never returns a secret value. The agent is the only other container, and the run's scope is fixed when the Job is created. |

`ae-studio-tools` tells its callers apart by the socket a request arrives on: the Files API socket is `ae-collab`, the MCP socket is `ae-design-agent`. Each socket serves only its own methods. This holds only because the pod does not share a process namespace ([09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md)).

Listeners for flows 2, 3, 4, 5, 13 and 14 are the only Resource endpoints. Both agent pods also deny ingress from other pods except through the org kgateway.
