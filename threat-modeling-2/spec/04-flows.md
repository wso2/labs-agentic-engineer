# 04 Flows

Every network flow in the intended architecture, in WSO2 Cloud. The numbers match the pictures in [03-components.md](03-components.md) and [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md). Token details are in [07-identity-and-tokens.md](07-identity-and-tokens.md). Local differences are in [11-local-vs-cloud.md](11-local-vs-cloud.md).

There is no private HTTP path from the control plane into the org dataplane. Every hop between the planes goes through a public gateway and carries a token. Every token is issued by the Platform IdP.

## Numbered flows

"The `ae-studio` check" below means: signature against the public Platform IdP JWKS, `iss=platform-idp`, `exp`, `aud` on the container's allow-list, then the org and role rule ([07-identity-and-tokens.md](07-identity-and-tokens.md)).

| # | From → to | Via | What | Token | Checked by, and how |
|---|---|---|---|---|---|
| 1 | Browser → `aep-api` | The console's own web server, then `aep-api` inside the control plane. A public gateway route for the console is planned. | Console REST for work that needs Postgres, OpenChoreo or Temporal: settings, builds, deploys, project create, usage. The `ae-studio` status and URL lookup on console load. | Platform IdP user JWT | `aep-api` itself: signature against the Platform IdP JWKS, `iss=platform-idp`, `aud`, `exp`. Then it authorizes user and org. The web server checks nothing. |
| 2 | `aep-api` / Temporal → `ae-studio-tools` `/internal/v1/*` | Org kgateway, TLS and CORS, no token check | Server-started turn: a high-level request such as "plan milestone M" or "kick off project P", and the marketplace chat. `ae-studio-tools` starts the turn on `ae-design-agent` over the in-pod turn socket and returns the turn result in the flow 2 response. Temporal owns order, retries and timeouts, and does the GitHub writes that follow from the result (for example issues from a plan) over flow 3. | The AE-only M2M (machine-to-machine) token + `X-Impersonate-Org`, even when a user started the work. The request names the user to credit. | `ae-studio-tools`: the `ae-studio` check, client id pinned, `X-Impersonate-Org` = this pod's org. It refuses a user JWT on `/internal/v1/*`. |
| 3 | `aep-api` / Temporal → `ae-studio-tools` | Org kgateway, TLS and CORS, no token check | Two route sets. `/internal/v1/*`: the low-level git and GitHub operations (commit and push, issues, comments, labels, milestones, pull requests and merge, repo create, the skills mirror, reads for Temporal), when an `aep-api` route or Temporal work needs them. `/v1/*`: the git-only reads of flow 14, when `aep-api` forwards a user request. | `/internal/v1/*`: the AE-only M2M token + `X-Impersonate-Org` only, even when a user started the work. `/v1/*`: the forwarded user JWT only. | `ae-studio-tools`: the `ae-studio` check. On `/internal/v1/*` as for flow 2; it refuses a user JWT there. On `/v1/*` it refuses the AE-only M2M token. |
| 4 | Browser → `ae-collab` | Org kgateway, TLS and CORS, no token check | Room WebSocket | Platform IdP user JWT, sent in the Hocuspocus auth message on connect, never in the URL or a cookie | `ae-collab`: the `ae-studio` check. |
| 5 | GitHub → `ae-studio-tools` | Org kgateway, TLS and CORS, no token check, webhook path | Webhook POST | None at the gateway | `ae-studio-tools`: `X-Hub-Signature-256` with the org HMAC. |
| 6 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | Verified webhook: `X-GitHub-Delivery`, `X-GitHub-Event`, JSON body. No signature, no HMAC. | Publisher client token (`client_credentials` at the Platform IdP) | Gateway `jwt-auth`; `aep-api` checks `aud` prefix and takes the org from `ouHandle`. It looks up the event's repository only among that org's repositories. `aep-api` redacts, dedups on the delivery id, stores, and dispatches to Temporal. `ae-studio-tools` returns GitHub's status from that result. |
| 7a | `ae-coding-tools` → `aep-api` | Public `aep-api` gateway | This run's platform calls, including the platform MCP tool calls of `ae-coding-agent`. `ae-coding-tools` serves the two remote-git tools itself over flow 7b. | Publisher client token | Gateway `jwt-auth`; `aep-api` as for flow 6. `ae-coding-tools` refuses other platform calls. |
| 7b | `ae-coding-tools` → GitHub | Dataplane egress | Git and GitHub for this run's repository | gitpat | GitHub. `ae-coding-tools` refuses other repositories. |
| 8 | `ae-design-agent`, `ae-coding-agent` → Anthropic API | Dataplane egress, public 443 | Model calls | The Anthropic key each container mounts | Anthropic. |
| 9 | `ae-studio-tools` → GitHub | Dataplane egress | Clone, fetch, push, GitHub REST | gitpat | GitHub. |
| 10 | `aep-api` → SM API | Control plane | Write gitpat, org HMAC, Default key, Coding agent token, `ae-studio-<org>` client secret | The user's Platform IdP JWT, passed through (WSO2 Cloud). Writes with no user on the request: open (O-3). | SM API writes vault through the OpenChoreo Secret API. It returns keys and `secretReferenceName` only. |
| 11 | ESO → vault | ClusterSecretStore | Read secret values into the dataplane | Not stated in this spec (open item) | ExternalSecret refresh 15s → Kubernetes Secret → container env. |
| 12 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | Platform MCP tool calls for `ae-design-agent`, one tool call per request: the nine tools that run on `aep-api` (external resources, org endpoints, platform resource types, groups, and the OpenAPI fetch, validate and slice tools). `ae-studio-tools` serves the two remote-git tools itself over flow 9. | Publisher client token | Gateway `jwt-auth`; `/internal/v1/mcp` on `aep-api` checks `aud` prefix and takes the org from `ouHandle`. It accepts no other token. |
| 13 | Browser → `ae-design-agent` | Org kgateway, TLS and CORS, no token check | Turn start and turn SSE (the stream of a running turn) | Platform IdP user JWT | `ae-design-agent`: the `ae-studio` check. This is the only token it accepts. It holds the one-active-turn lock, the conversation thread and the snapshots in the pod. |
| 14 | Browser → `ae-studio-tools` | Org kgateway, TLS and CORS, no token check | Git-only REST (`/v1/*`): spec file reads, issue-backed task lists, repo reads | Platform IdP user JWT | `ae-studio-tools`: the `ae-studio` check. `/v1/*` routes are the only `ae-studio-tools` routes that accept a user JWT. |
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
| Room WebSocket | Public URL of Resource `ae-studio`, container `ae-collab` (flow 4) | Platform IdP user JWT, in the Hocuspocus auth message on connect |
| Git-only REST: spec file reads, issue-backed task lists, repo reads | Public URL of Resource `ae-studio`, container `ae-studio-tools` (flow 14) | Platform IdP user JWT |

On load, `aep-api` reads the ResourceReleaseBinding of Resource `ae-studio`: the URLs from `status.outputs` (the ResourceType publishes them as outputs, because a Resource has no invoke URL) and readiness from the `Ready` condition. Until `ae-studio` is ready, the console shows a loader.

The calls to `ae-studio` are cross-origin. The REST calls send the user JWT as a bearer header. The console reads the turn SSE with a fetch-based SSE client that sends the same bearer header, never with the native `EventSource`, which would need the token in the URL. The Room WebSocket sends it in the Hocuspocus auth message. No call puts it in a URL or a cookie. The Resource routes answer CORS preflight at the gateway, with no token ([03-components.md](03-components.md)).

Routes that also need Postgres, OpenChoreo or Temporal stay on `aep-api`, and so does the delivery engine (Temporal and webhook handling). When they need git or GitHub, `aep-api` calls `ae-studio-tools` over flow 3: `/v1/*` with the forwarded user JWT for git-only reads, `/internal/v1/*` with the AE-only M2M token for everything else. Users never call a git or GitHub write directly.

## Inside the pods (not numbered)

All containers in a pod share one network, so any container can reach any `localhost` port. A call inside a pod carries no token **only** when a container that should not make it cannot reach the listener.

| Pod | From → to | Channel | What |
|---|---|---|---|
| `ae-studio` | `ae-collab` → `ae-studio-tools` | Unix socket in an emptyDir mounted only into these two containers. No token. | Files API: `files/bundle`, `files/apply`, seed, flush, and the project-known lookup for a Room join. `ae-studio-tools` commits and pushes. `ae-design-agent` cannot see the socket. |
| `ae-studio` | `ae-design-agent` → `ae-studio-tools` | Unix socket in a second emptyDir mounted only into these two containers. No token. | Platform MCP tools, fixed allow-list of eleven read-only tools. Two remote-git tools run on `ae-studio-tools` with the gitpat (flow 9); the other nine go to `aep-api` over flow 12. The same socket carries the Room-join token request (an `ae-studio-<org>` token), the hand-off of finished-turn usage records for flow 15, and the project-known lookup. `ae-collab` cannot see this socket. |
| `ae-studio` | `ae-studio-tools` → `ae-design-agent` | The turn socket, a Unix socket served by `ae-design-agent` in the same emptyDir as the MCP socket, mounted only into these two containers. No token. | Start a server-started turn for flow 2 and get its result back. `ae-collab` cannot see this socket. The model has no tool that reaches it. |
| `ae-studio` | `ae-studio-tools` → `ae-design-agent` | Shared named emptyDir. No call. | Snapshots written by `ae-studio-tools`, read by `ae-design-agent`. |
| `ae-studio` | `ae-design-agent` → `ae-collab` | The `localhost` Room listener of `ae-collab`, not an endpoint. The `ae-studio-<org>` token from `ae-studio-tools`, in the Hocuspocus auth message on connect. | Join the turn's Room. `ae-collab` checks the token: Platform IdP JWKS, `iss`, `exp`, `aud` = this org's `ae-studio-<org>`, `ouId` and `ouHandle` = this pod's org. See [07-identity-and-tokens.md](07-identity-and-tokens.md). |
| coding Job | `ae-coding-agent` → `ae-coding-tools` | `127.0.0.1` listener, not an endpoint. No token. | Git, GitHub and platform actions for this run, and the platform MCP tools (remote-git served by `ae-coding-tools`, the rest over flow 7a). `ae-coding-tools` never returns a secret value. The agent is the only other container, and the run's scope is fixed when the Job is created. |

`ae-studio-tools` tells its callers apart by the socket a request arrives on: the Files API socket is `ae-collab`, the MCP socket is `ae-design-agent`. Each socket serves only its own methods. `ae-design-agent` serves the turn socket, and only `ae-studio-tools` can reach it. This holds only because the pod does not share a process namespace ([09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md)).

Listeners for flows 2, 3, 4, 5, 13 and 14 are the only Resource endpoints: `ae-design-agent` serves flow 13 only, `ae-collab` flow 4, `ae-studio-tools` flows 2, 3, 5 and 14. Both agent pods also deny ingress from other pods except through the org kgateway.
