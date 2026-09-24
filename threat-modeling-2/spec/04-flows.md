# 04 Flows

Every network flow in the intended architecture, in WSO2 Cloud. The numbers match the pictures in [03-components.md](03-components.md) and [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md). Token details are in [07-identity-and-tokens.md](07-identity-and-tokens.md). Local differences are in [11-local-vs-cloud.md](11-local-vs-cloud.md).

There is no private HTTP path from the control plane into the org dataplane. Every hop between the planes goes through a public gateway and carries a token.

## Numbered flows

| # | From → to | Via | What | Token | Checked by, and how |
|---|---|---|---|---|---|
| 1 | Browser → `aep-api` | Public `aep-api` gateway | Console REST, turn start, design-turn SSE (`GET …/turns/{id}/stream`), Room token request | Platform IdP user JWT | Gateway `jwt-auth` (`iss=platform-idp`). `aep-api` authorizes user, org and Room. |
| 2 | `aep-api` → `ae-design-agent` | Org kgateway, TLS only | Design turns; the source of the SSE that `aep-api` copies to the browser. A Room-mode turn body also carries the agent Room token. | CP → DP service token, `aud` = org + `ae-design-agent` | `ae-design-agent`: signature against aep-api JWKS, `aud`, `exp`, token org = this pod's org. |
| 3 | `aep-api` / Temporal → `ae-studio-tools` | Org kgateway, TLS only | Git operations, GitHub REST, repo create | CP → DP service token, `aud` = org + `ae-studio-tools` | `ae-studio-tools`: same four checks. |
| 4 | Browser → `ae-collab` | Org kgateway, TLS only | Room WebSocket | Room token, `aud` = org + `ae-collab` + Room, fetched over flow 1 | `ae-collab`: signature against aep-api JWKS, `aud`, `exp`, token org = this pod's org. |
| 5 | GitHub → `ae-studio-tools` | Org kgateway, TLS only, webhook path | Webhook POST | None at the gateway | `ae-studio-tools`: `X-Hub-Signature-256` with the org HMAC. |
| 6 | `ae-studio-tools` → `aep-api` | Public `aep-api` gateway | Verified webhook: `X-GitHub-Delivery`, `X-GitHub-Event`, JSON body. No signature, no HMAC. | Publisher client token (`client_credentials` at the Platform IdP) | Gateway `jwt-auth`; `aep-api` checks `aud` prefix and `ouHandle`. `aep-api` redacts, dedups on the delivery id, stores, and dispatches to Temporal. `ae-studio-tools` returns GitHub's status from that result. |
| 7a | `ae-coding-tools` → `aep-api` | Public `aep-api` gateway | This run's platform calls | Publisher client token | Gateway `jwt-auth`; `aep-api` as for flow 6. `ae-coding-tools` refuses other platform calls. |
| 7b | `ae-coding-tools` → GitHub | Dataplane egress | Git and GitHub for this run's repository | gitpat | GitHub. `ae-coding-tools` refuses other repositories. |
| 8 | `ae-design-agent`, `ae-coding-agent` → Anthropic API | Dataplane egress, public 443 | Model calls | The Anthropic key each container mounts | Anthropic. |
| 9 | `ae-studio-tools` → GitHub | Dataplane egress | Clone, fetch, push, GitHub REST | gitpat | GitHub. |
| 10 | `aep-api` → SM API | Control plane | Write gitpat, org HMAC, Default key, Coding agent key | Not stated in this spec (open item) | SM API writes vault through the OpenChoreo Secret API. It returns keys and `secretReferenceName` only. |
| 11 | ESO → vault | ClusterSecretStore | Read secret values into the dataplane | Not stated in this spec (open item) | ExternalSecret refresh 15s → Kubernetes Secret → container env. |

Also on the control plane, not numbered: `aep-api` → Postgres (rows only), `aep-api` → OpenChoreo control plane (Ensure the Project, ProjectReleaseBinding and Resource; create the coding Job), `ae-studio-tools` and `ae-coding-tools` → Platform IdP (`client_credentials` for the publisher client token).

## What the browser calls

The browser talks to the dataplane **only** for the Room WebSocket.

| Call | Browser target | Token the browser sends |
|---|---|---|
| Turn start, design-turn SSE, other console REST, Room token request | `aep-api` (flow 1) | Platform IdP user JWT |
| Room WebSocket | Public URL of Resource `ae-studio`, container `ae-collab` (flow 4) | Room token |

The browser never calls `ae-design-agent` or `ae-studio-tools`. The Platform IdP user JWT never goes to the org gateway. Spec, build and conversation REST stay on `aep-api`.

## Inside the pods (not numbered)

All containers in a pod share one network, so any container can reach any `localhost` port. A call inside a pod carries no token **only** when a container that should not make it cannot reach the listener.

| Pod | From → to | Channel | What |
|---|---|---|---|
| `ae-studio` | `ae-collab` → `ae-studio-tools` | Unix socket in an emptyDir mounted only into these two containers. No token. | Files API: `files/bundle`, `files/apply`, seed, flush. `ae-studio-tools` commits and pushes. `ae-design-agent` cannot see the socket. |
| `ae-studio` | `ae-studio-tools` → `ae-design-agent` | Shared named emptyDir. No call. | Snapshots written by `ae-studio-tools`, read by `ae-design-agent`. |
| `ae-studio` | `ae-design-agent` → `ae-collab` | `localhost` Room WebSocket. Agent Room token from the turn body (flow 2). | Join this turn's Room. `ae-collab` checks the token as for flow 4: aep-api JWKS, `aud`, `exp`, token org = this pod's org. See [07-identity-and-tokens.md](07-identity-and-tokens.md). |
| coding Job | `ae-coding-agent` → `ae-coding-tools` | `127.0.0.1` listener, not an endpoint. No token. | Git, GitHub and platform actions for this run. `ae-coding-tools` never returns a secret value. The agent is the only other container, and the run's scope is fixed when the Job is created. |

Listeners for flows 2, 3, 4 and 5 are the only Resource endpoints. Both agent pods also deny ingress from other pods except through the org kgateway.
