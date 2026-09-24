# 07 Identity and tokens

Every token in the intended architecture: who issues it, who it is for, how long it lives, and who checks it. Flow numbers refer to [04-flows.md](04-flows.md).

## Issuers

| Issuer | What it is |
|---|---|
| **Platform IdP** | The shared Thunder issuer (`iss=platform-idp`). Issues user JWTs and publisher client tokens. |
| **`aep-api`** | Mints short RS256 JWTs and publishes its JWKS. Same family as today's MCP tokens. |
| **Environment Thunder** | The per-(org, environment) Thunder. In WSO2 Cloud it runs on cloud-cp (Resource `tid`), not next to Resource `ae-studio`. It is the **intended** issuer for exchanged tokens. It is not a "dataplane IdP". |

## Tokens now

| Token | Issuer | Subject / org | `aud` | TTL | Carried on | Checked by |
|---|---|---|---|---|---|---|
| User JWT | Platform IdP | the user | unchanged | unchanged | flow 1 | Public `aep-api` gateway `jwt-auth`; `aep-api` authorizes user and org. |
| CP → DP service token | `aep-api` (RS256) | org in claims (`ocOrgId`) | org + `ae-studio-tools` (flow 3); org + `ae-design-agent` (flow 2) | 5 minutes | flows 2, 3 | The receiving container (below). |
| Room token | `aep-api` (RS256) | org in claims | org + `ae-collab` + Room | 5 minutes | flow 4 | `ae-collab` (below). |
| Agent Room token | `aep-api` (RS256) | `sub` = the user who started the turn, `act` = `ae-design-agent`, org in claims | org + `ae-collab` + Room | until the turn deadline (at most 30 minutes) | inside the flow-2 turn body, then `ae-design-agent` → `ae-collab` on `localhost` | `ae-collab` (below). |
| Publisher client token | Platform IdP (`client_credentials`) | the org's publisher client `aep-publisher-<org>` | prefix checked by `aep-api` | what the Platform IdP issues today | flows 6, 7a, 12 | Public `aep-api` gateway `jwt-auth`; `aep-api` checks `aud` prefix and `ouHandle`. |

No token is stored in Postgres. The minted tokens are made per call and live only in memory.

### CP → DP service token

`aep-api` mints one per call. The Temporal worker runs inside `aep-api`, so it uses the same token. The receiving container (`ae-studio-tools` or `ae-design-agent`) checks:

1. the signature, against the aep-api JWKS;
2. `aud` is this org and this container;
3. `exp`;
4. the org in the claims equals **this** pod's org.

The container does not trust the gateway for identity. The org kgateway is TLS only. There is no API key on this hop.

### Room token

The browser asks `aep-api` for a Room token over flow 1. `aep-api` authorizes this user, this org and this Room, then mints the token. It is not a second service identity and not the CP → DP service token: its `aud` is `ae-collab` + Room, never `ae-studio-tools`.

`ae-collab` checks the signature against the aep-api JWKS, `aud`, `exp`, and that the claim org equals this pod's org. It does not call `collab/validate` for authorization: `aep-api` already bound the Room when it minted the token.

### Publisher client

`ae-studio-tools` and `ae-coding-tools` mount the publisher client (`client_id`, `client_secret`) through ESO. They get a token with `client_credentials` at the Platform IdP, then call the public `aep-api` gateway. The publisher client is used **only** from the dataplane to the control plane. It is never mounted on a model container, and there is no second publisher app. It also carries the platform MCP tool calls of both agents: flow 12 for `ae-design-agent`, flow 7a for `ae-coding-agent`.

### How `ae-design-agent` joins a Room

For a Room-mode turn, `aep-api` authorizes this user, this org and this Room, as for the browser. It then mints an **agent Room token** and puts it in the turn body on flow 2. This replaces today's copy of the user's JWT.

- `aud`: this org + `ae-collab` + this Room. Same as the browser's Room token.
- `sub`: the user who started the turn. `act`: `ae-design-agent`.
- `exp`: the turn deadline, at most 30 minutes. There is no refresh. The token stays valid for a reconnect during the turn.

`ae-collab` checks it exactly as the browser's Room token: the signature against the aep-api JWKS, `aud`, `exp`, and that the claim org equals this pod's org. It has no extra rules for an agent peer. Commits credit the user in `sub`, as today. Agent edits stay held for review.

The token lives only in `ae-design-agent` memory. If it leaks, it opens this one Room until the turn ends. A token is needed even on `localhost`: `ae-collab` serves every Room of the org, and only `aep-api` knows which Room this turn may join.

### How `ae-design-agent` calls platform MCP tools

The design agent uses the same pattern as the coding agent: the model container asks its tools container, and the tools container calls `aep-api` as the publisher client. `ae-design-agent` holds no token for `aep-api`.

- **Channel.** `ae-design-agent` calls `ae-studio-tools` on a Unix socket in its own emptyDir, mounted only into those two containers. No token. It is not the Files API socket: `ae-collab` cannot see the MCP socket, and `ae-design-agent` cannot see the Files API socket.
- **What `ae-studio-tools` serves.** One MCP server with a fixed allow-list of eleven read-only tools. Any other JSON-RPC method or tool name is refused.
  - `get_remote_git_file_contents` and `search_remote_git_code` run on `ae-studio-tools` with the gitpat (flow 9). The repo owner must be the org's GitHub owner.
  - The other nine go to `aep-api` `/internal/v1/mcp` over flow 12, one tool call per request, with the publisher client token.
- **Org.** Fixed by the publisher client. `aep-api` takes it from `ouHandle`, never from the agent's input.
- **Check.** Gateway `jwt-auth` (`iss=platform-idp`), then `aep-api` checks the `aud` prefix and `ouHandle`, as for flows 6 and 7a.

The coding Job does the same in `ae-coding-tools`: remote-git with its gitpat (flow 7b), the other tools over flow 7a.

Calls are not bound to a user or a turn. `ae-design-agent` can call a tool between turns. The tools are read-only and scoped to this pod's org, which is less than the Default key the container already holds. This is an accepted risk ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)).

## What never happens

- The user's Platform IdP JWT never goes to the org gateway or to a dataplane container.
- The publisher client is never in the browser and never on a model container.
- A Room token is never accepted by `ae-studio-tools`.
- `ae-design-agent` never reaches the Files API of `ae-studio-tools`.
- `aep-api` mints no MCP token. `/internal/v1/mcp` accepts only the publisher client token.
- `ae-design-agent` never holds a token for `aep-api`.

## Intended when WSO2 Cloud supports token exchange (GAP-2)

ThunderID can do RFC 8693 token exchange. WSO2 Cloud does not enable that grant today. When it does:

| Token | Intended issuer | Intended shape | Checked by |
|---|---|---|---|
| CP → DP service token | Environment Thunder | `aep-api` exchanges a Platform IdP JWT at Environment Thunder `/oauth2/token` and sends the new token. No custom API on `ae-studio`. | `ae-studio-tools` / `ae-design-agent`, against the Environment Thunder JWKS. |
| Room token | Environment Thunder | `aep-api` still authorizes the Room first, then exchanges the **user's** Platform IdP JWT. Subject the user, org this org, `aud` this org's `ae-collab` + this Room, TTL a few minutes. The exchange client is a platform client with a platform secret; the org comes from the user's token. The agent Room token follows the same switch. | `ae-collab`, against the Environment Thunder JWKS, plus `aud`, `exp`, org. If Thunder will not put the Room into `aud`, `ae-collab` keeps one `collab/validate` call. |

The console flow stays "ask `aep-api`, then open the Room WebSocket". The publisher client stays the DP → CP identity. Until the exchange exists, the `aep-api`-minted tokens are the control in place, and the threat model tags the gap.

## Not chosen, and why

- **A Platform IdP machine token for CP → DP.** One shared client gives a token that is not per-org. One client per org needs an org secret the control plane can read, which the write-only SM API forbids. And user JWTs share `iss=platform-idp`, so a loose `aud` check would turn a stolen user JWT into a service token.
- **The publisher client for CP → DP.** Mixes directions and still needs a control-plane-readable org secret.
- **A custom exchange API on `ae-studio`.** Adds a public surface. Token exchange is a call to Environment Thunder; `ae-studio` is not needed for it.
- **Org dataplane API keys as the hop's identity.** An API key does not bind the org and the caller.
- **The user's Platform IdP JWT presented to `ae-collab`.** Puts a token that works on every user API into the dataplane.
- **`aep-api` proxies the Room WebSocket.** Yjs across two public gateways.
- **The browser opens design-turn SSE on the dataplane.** Keeps a second browser-facing surface; SSE stays on `aep-api`.
- **Environment Thunder only, with no minted stand-in.** WSO2 Cloud has no exchange grant today.
- **The minted Room token as the permanent design.** It is the stand-in until the exchange exists.
- **No token for the agent's Room join, because it is on `localhost`.** Any container in the pod reaches any `localhost` port, and `ae-collab` serves every Room of the org. The agent could join any Room.
- **The CP → DP service token for the agent's Room join.** Its `aud` is `ae-design-agent`. `ae-collab` would accept a token meant for another container.
- **A secret shared inside the pod.** A new secret on the model container, with no Room or user binding.
- **A 5-minute agent Room token.** Hocuspocus checks the token only on connect, so a reconnect after 5 minutes fails the Room.
- **A 5-minute agent Room token that `aep-api` refreshes over flow 2.** Seamless, but adds a refresh route and a timer per turn. The Default key on the same container already outlasts and outreaches a turn-long Room token.
- **An agent-only `sub`.** Loses today's credit of the user in commits.
- **An `aep-api`-minted MCP token sent by `ae-design-agent` to a public `aep-api` route with gateway `jwt-auth` off.** Reuses today's code, but adds a second public `aep-api` route where only the app checks the token, and breaks the rule that dataplane → control plane calls use the publisher client.
- **`aep-api` runs the MCP tools and returns results over flow 2.** No dataplane → control plane call, but it needs an MCP bridge over SSE, a result route and shared turn state in both services.
- **`localhost` TCP for the MCP channel.** `ae-collab`, which serves the public Room WebSocket, could call the tools.
- **A per-turn MCP token on the in-pod channel.** Binds calls to a turn, but adds a third minted token for read-only tools that give less than the Default key already does.
- **One socket for the Files API and MCP.** All three containers would mount it, and `ae-design-agent` could call `files/apply`.
- **`aep-api` forwards the remote-git tools to `ae-studio-tools` over flow 3.** An extra hop; `ae-studio-tools` already holds the gitpat and serves the agent directly.
