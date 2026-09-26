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
| Add | `aep-api` mints the agent Room token for each Room-mode turn and puts it in the turn body ([07-identity-and-tokens.md](07-identity-and-tokens.md)). |
| Remove | `collab/validate` as Room authorization. |
| Remove | The HS256 service token to the agents service. |
| Remove | Git work in `aep-api` (gitfs clone, fetch, commit, push) and the Files API used by collab. They move to `ae-studio-tools`. |
| Remove | Receiving gitpat webhooks on `aep-api` and checking their HMAC there, and the use of the single platform webhook secret for gitpat hooks. |
| Change | gitpat submit: check the gitpat in memory, write the gitpat and the org HMAC through the SM API, Ensure the runtime, wait for the public `ae-studio-tools` address, register the hook once, drop the gitpat ([05-lifecycle.md](05-lifecycle.md)). |
| Change | Key writes (Default key, Coding agent token) go only through the SM API. |
| Change | `StageBuildSecret` passes references only. |
| Change | Git operations, GitHub REST and repo create become CP → DP calls to `ae-studio-tools` (flow 3). The Postgres row for a created repo stays on `aep-api`. |
| Change | Design turns and the SSE source become CP → DP calls to `ae-design-agent` (flow 2). `aep-api` still serves design-turn SSE to the browser. |
| Add | Ensure of Project `ae-system`, its `development` ProjectReleaseBinding and Resource `ae-studio`; upgrade of the same Resource on console load. |
| Add | Minting of the CP → DP service token (RS256, 5 minutes, `aud` org + container) and the Room token (RS256, 5 minutes, `aud` org + `ae-collab` + Room), signed with the key behind its JWKS. |
| Add | An endpoint for the Room token. The console names the project; `aep-api` authorizes user and org, looks up the project in that org, and mints the token for its Room. It does not parse a Room id. |
| Remove | The skills mirror with the gitpat. It moves to `ae-studio-tools` (flow 3). |
| Add | An endpoint that accepts verified webhook events from `ae-studio-tools` as the publisher client: look up the repository only in the token's org, redact, dedup on delivery id, store, dispatch to Temporal, return the status. |
| Change | The coding agent dispatch mounts the gitpat and the publisher client on `ae-coding-tools`, and only the org's Anthropic key or Coding agent token on `ae-coding-agent`. Where dependency secrets and test-user passwords go waits on O-11. |

## OpenChoreo objects and templates

| Change | What |
|---|---|
| Add | ResourceType `ae-studio`: one pod, three containers, named emptyDirs (one only for the Files API socket, one only for the MCP socket), `shareProcessNamespace: false`, pod controls, egress and the ingress rule from [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md), gVisor when the RuntimeClass exists. |
| Add | Routes on the org kgateway for `ae-design-agent` (flow 2), `ae-studio-tools` service (flow 3) and webhook (flow 5), and `ae-collab` Room WebSocket (flow 4). TLS only. |
| Change | The `coding-agent` ComponentType: rename container `main` to `ae-coding-agent`; add container `ae-coding-tools`; apply the same pod controls and egress. |

## New images and containers

| Image = container | What it is |
|---|---|
| `ae-design-agent` | Today's agents service, run in the dataplane. Mounts the Default key. Reads snapshots from the emptyDir. Calls platform MCP tools on the MCP socket of `ae-studio-tools`. No URL-fetch tool. |
| `ae-collab` | Today's collab server, run in the dataplane. Checks the Room token. Talks the Files API to `ae-studio-tools` over a Unix socket. Accepts the agent Room token from `ae-design-agent`. |
| `ae-studio-tools` | New. Git, GitHub REST, the MCP server for `ae-design-agent` on its own socket (allow-list of eleven tools: remote-git with the gitpat, the rest to `aep-api` over flow 12), the Files API (`specs/` only, 5 MiB a file), the skills mirror, the webhook receiver with HMAC check, the CP → DP token check, the publisher client call to `aep-api`, snapshots to the emptyDir. |
| `ae-coding-agent` | Today's coding agent container `main`, renamed. Holds only the org's AI keys (the Coding agent token or the Default key). |
| `ae-coding-tools` | New. Git and GitHub for this run's repository, platform calls for this run, as the publisher client. Serves the platform MCP tools to `ae-coding-agent`: remote-git with the gitpat, the rest over flow 7a. |

## Console

| Change | What |
|---|---|
| Change | The Room WebSocket goes to the public address of `ae-collab` with a Room token from `aep-api`. Local installs may keep the same-origin `/collab` path. |
| Remove | Sending the user's JWT to collab. |

## Deployment (control plane)

| Change | What |
|---|---|
| Remove | The agents service and collab Deployments from the control plane. |
| Remove | The shared `/workspaces` volume between `aep-api` and the agents service. |
| Remove | The control-plane GitHub webhook RestApi, once the org kgateway route is live. |
| Remove | The agents HS256 service-token secret. |
