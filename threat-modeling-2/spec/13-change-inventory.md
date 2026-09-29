# 13 Change inventory

Per component: what is added, changed and removed to reach the end state in this spec. Every row follows from a chapter of this spec. The implementation plan slices its work from this list. Items that wait on an open item say so.

## `aep-api`

| Change | What |
|---|---|
| Remove | Organization secret values in Postgres: the `org_secrets` rows for the gitpat (`github/pat`), the Default key (`anthropic/key`) and the Coding agent token (`anthropic/coding-key`), and the sealed `org_credentials.webhook_secrets`. What else stays in `org_credentials` waits on O-7. |
| Remove | The publisher client secret column in `organization_idp_profiles`. How it is created and rotated waits on O-6. |
| Remove | The best-effort vault copy after a Postgres write. The SM API write is the only write. |
| Remove | Every read of the gitpat after gitpat submit: `Token()` for gitfs, GitHub REST and GraphQL, MCP remote-git, and build secrets. |
| Remove | `credentials/refresh` returning a gitpat. |
| Remove | `EffectiveKey` decrypting the Default key, and the `X-Anthropic-Key` header on design turns. |
| Remove | Copying the user's Platform IdP JWT into a design turn for the Room join. |
| Remove | Minting the MCP token (`aud=aep-api-mcp`) and the `mcp: {url, token}` block in the design turn body. |
| Change | `/internal/v1/mcp` accepts only the publisher client token (flows 7a, 12). The branch that accepts the `aep-api`-minted MCP token is removed. The remote-git tools leave `aep-api`. |
| Remove | `collab/validate` and `spec/collab-session` as Room authorization. The pod decides ([07-identity-and-tokens.md](07-identity-and-tokens.md)). |
| Remove | The HS256 service token to the agents service. |
| Remove | Git work in `aep-api` (gitfs clone, fetch, commit, push) and the Files API used by collab. They move to `ae-studio-tools`. |
| Remove | Receiving gitpat webhooks on `aep-api` and checking their HMAC there, and the use of the single platform webhook secret for gitpat hooks. |
| Change | gitpat submit: check the gitpat in memory, write the gitpat and the org HMAC through the SM API, Ensure the runtime, wait for the public `ae-studio-tools` address, register the hook once, drop the gitpat ([05-lifecycle.md](05-lifecycle.md)). |
| Change | Key writes (Default key, Coding agent token) go only through the SM API. |
| Change | `StageBuildSecret` passes references only. |
| Change | Git operations, GitHub REST and repo create become CP → DP calls to `ae-studio-tools` (flow 3), with the AE-only M2M token. When `aep-api` forwards a user request to a git-only route, it forwards the user JWT. The Postgres row for a created repo stays on `aep-api`. |
| Change | Only server-started turns (for example "plan milestone M", "kick off project P", the marketplace chat) become CP → DP calls to `ae-design-agent` (flow 2), with the forwarded user JWT or the AE-only M2M token. The browser starts its own turns on `ae-design-agent` (flow 13). |
| Remove | The design-turn SSE copy on `aep-api`, the rehydrate proxy, the one-active-turn lock and the conversation thread and its rotation. They move into the pod. |
| Add | Ensure of Project `ae-system`, its `development` ProjectReleaseBinding and Resource `ae-studio`; upgrade of the same Resource on console load. |
| Remove | Minting the CP → DP service token and the Room token (RS256), the signing key and the `aep-api` JWKS. `aep-api` mints no token. |
| Add | A discovery endpoint for the console: the status (`Ready`) and public URLs of Resource `ae-studio`, read from its ResourceReleaseBinding (`status.outputs`, `Ready`). |
| Add | The AE-only control-plane client `APP_FACTORY_BFF_TO_AE_STUDIO` (working name): `client_credentials` at the Platform IdP, the token sent with `X-Impersonate-Org`, for calls with no user and for every `aep-api`-coupled call to `ae-studio-tools`. Its secret comes from WSO2 Cloud provisioning (O-15). |
| Add | Creating the per-org `ae-studio-<org>` client (a Thunder Agent entity in the org OU) with the Resource, and writing its secret through the SM API (flow 10) ([05-lifecycle.md](05-lifecycle.md)). |
| Add | A usage batch endpoint on the public gateway: accepts a batch of finished design-turn usage records from `ae-studio-tools` as the publisher client (flow 15), keys the ledger on the turn id, so a batch sent twice counts once. |
| Remove | The activity feed: the routes `/projects/{p}/activity` and `/activity/stream`, its writers, the table and its migration, `activityvocab`, and the design note `services/aep-api/design/agent-activity-feed.md`. The console has no reader of it. |
| Remove | The skills mirror with the gitpat. It moves to `ae-studio-tools` (flow 3). |
| Add | An endpoint that accepts verified webhook events from `ae-studio-tools` as the publisher client: look up the repository only in the token's org, redact, dedup on delivery id, store, dispatch to Temporal, return the status. |
| Change | The coding agent dispatch mounts the gitpat and the publisher client on `ae-coding-tools`, and only the org's Anthropic key or Coding agent token on `ae-coding-agent`. Where dependency secrets and test-user passwords go waits on O-11. |

## OpenChoreo objects and templates

| Change | What |
|---|---|
| Add | ResourceType `ae-studio`: one pod, three containers, named emptyDirs (one only for the Files API socket, one only for the MCP socket), `shareProcessNamespace: false`, pod controls, egress and the ingress rule from [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md), gVisor when the RuntimeClass exists. |
| Add | Routes on the org kgateway for `ae-design-agent` (flows 2 and 13), `ae-studio-tools` (flows 3 and 14, and the webhook, flow 5) and `ae-collab` Room WebSocket (flow 4). TLS only, no identity check. |
| Add | A Gateway API `CORS` filter on each browser-facing route (exact console origin, `Authorization` and `Content-Type`, no credentials), and raised request and stream-idle timeouts on the turn SSE and Room WebSocket routes. |
| Add | ResourceType outputs for the public URLs of the routes, so `aep-api` can read them from the ResourceReleaseBinding. |
| Add | Pod configuration for the token checks: the Platform IdP `iss` and JWKS URL, the pod's fixed org (`ouId`, `ouHandle`), the `aud` allow-lists, and the pinned AE-only client id. |
| Change | The `coding-agent` ComponentType: rename container `main` to `ae-coding-agent`; add container `ae-coding-tools`; apply the same pod controls and egress. |

## New images and containers

| Image = container | What it is |
|---|---|
| `ae-design-agent` | Today's agents service, run in the dataplane. Mounts the Default key. JWT middleware (Platform IdP token check, TypeScript). Turn orchestration in the pod: turn start and SSE for the browser (flow 13), server-started turns (flow 2), the one-active-turn lock, the conversation thread and its rotation, usage records buffered for `ae-studio-tools`. Reads snapshots from the emptyDir. Calls platform MCP tools, asks for the Room-join token and for the project-known lookup, on the MCP socket of `ae-studio-tools`. No URL-fetch tool. |
| `ae-collab` | Today's collab server, run in the dataplane. JWT middleware (TypeScript): checks the user JWT or the `ae-studio-<org>` token in the Hocuspocus auth message on connect. Talks the Files API to `ae-studio-tools` over a Unix socket, including the project-known lookup. |
| `ae-studio-tools` | New. Git, GitHub REST, the MCP server for `ae-design-agent` on its own socket (allow-list of eleven tools: remote-git with the gitpat, the rest to `aep-api` over flow 12), the Files API (`specs/` only, 5 MiB a file) and the project-known lookup, the skills mirror, the webhook receiver with HMAC check, git-only REST for the browser (flow 14), JWT middleware (Platform IdP token check, Go) that splits routes by token kind, the Room-join token (`ae-studio-<org>`) served on the MCP socket, the usage batch sender (flow 15), the publisher client call to `aep-api`, snapshots to the emptyDir. |
| `ae-coding-agent` | Today's coding agent container `main`, renamed. Holds only the org's AI keys (the Coding agent token or the Default key). |
| `ae-coding-tools` | New. Git and GitHub for this run's repository, platform calls for this run, as the publisher client. Serves the platform MCP tools to `ae-coding-agent`: remote-git with the gitpat, the rest over flow 7a. |

## Console

| Change | What |
|---|---|
| Add | On load, ask `aep-api` for the `ae-studio` status and URLs. Show a loader until `Ready`. |
| Change | Call `ae-design-agent` (turn start and turn SSE, flow 13), `ae-collab` (Room WebSocket, flow 4) and `ae-studio-tools` (git-only REST, flow 14) directly, with the user JWT as a bearer header. |
| Change | The Room WebSocket sends the user JWT in the Hocuspocus auth message on connect, never in the URL or a cookie. |
| Remove | The Room token request to `aep-api` (`spec/collab-session`) and the nginx `/collab` route. |
| Remove | The design-turn stream read from `aep-api`, and the activity feed calls. |

## Deployment (control plane)

| Change | What |
|---|---|
| Remove | The agents service and collab Deployments from the control plane. |
| Remove | The shared `/workspaces` volume between `aep-api` and the agents service. |
| Remove | The control-plane GitHub webhook RestApi, once the org kgateway route is live. |
| Remove | The agents HS256 service-token secret. |
| Remove | The nginx `/collab` route in the console web server. |
| Add | The AE-only client id and secret delivered to `aep-api` in the WSO2 Cloud deployment (O-15), and the console origin for the CORS filter. |
