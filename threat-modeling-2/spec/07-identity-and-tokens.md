# 07 Identity and tokens

Every token in the intended architecture: who issues it, who it is for, how long it lives, and who checks it. Flow numbers refer to [04-flows.md](04-flows.md). Local differences are in [11-local-vs-cloud.md](11-local-vs-cloud.md).

## Issuers

| Issuer | What it is |
|---|---|
| **Platform IdP** | The shared Thunder issuer (`iss=platform-idp`, a bare string, not a URL). It issues every token in this chapter. Its public JWKS (the public keys receivers use to check its tokens) is `https://platform-idp-<env>.gateway.<base>/oauth2/jwks`. It needs no authentication. |

This is the only issuer. `aep-api` mints no token and publishes no JWKS. Environment Thunder (the per-organization, per-environment Thunder) is not used by AE.

## Tokens

| Token | Client | Subject / org | `aud` | TTL | Carried on | Checked by |
|---|---|---|---|---|---|---|
| User JWT | The console client `APP_FACTORY_CONSOLE` | `sub` = the user; org in `ouId` (org UUID) and `ouHandle` | `APP_FACTORY_CONSOLE` | What the Platform IdP issues (1 hour on WSO2 Cloud) | flows 1, 4, 13, 14 from the browser; flow 2, and flow 3 for the git-only routes, when `aep-api` forwards a user request | `aep-api` on flow 1. The receiving `ae-studio` container on flows 2, 3, 4, 13, 14. |
| AE-only control-plane M2M (machine-to-machine) | `APP_FACTORY_BFF_TO_AE_STUDIO` (working name). A new client in the admin OU (organization unit), `client_credentials` grant. | `sub` = the client. No org claim: the org is in the `X-Impersonate-Org` header. | Its own client id | What the Platform IdP issues | flow 2 when no user is on the request; flow 3 for every `aep-api`-coupled operation | `ae-design-agent` (flow 2), `ae-studio-tools` (flow 3). |
| `ae-studio-<org>` | A per-org Thunder **Agent entity** in the org OU, `client_credentials` grant, OU claims on | `sub` = the Agent entity; org in `ouId` and `ouHandle` | Its own client id | What the Platform IdP issues | `ae-design-agent` → `ae-collab` on `localhost` (Room join) | `ae-collab` only. |
| Publisher client token | The org's publisher client `aep-publisher-<org>`, `client_credentials` grant | `sub` = the client; org in `ouHandle` | `aep-publisher-<org>` | What the Platform IdP issues | flows 6, 7a, 12, 15 (dataplane → control plane only) | Public `aep-api` gateway `jwt-auth`; `aep-api` checks the `aud` prefix and `ouHandle`. |

No token is stored in Postgres, vault, a ConfigMap or a file. Every container keeps the tokens it gets in memory only.

## What each `ae-studio` container checks

The org kgateway (the public gateway of the org dataplane) checks no identity. Each of `ae-collab`, `ae-design-agent` and `ae-studio-tools` checks every request itself, with no call out except the JWKS fetch:

1. The signature, against the public Platform IdP JWKS. The algorithm must be one the JWKS publishes (an RSA key and an EC P-256 key). The JWKS sends no cache header, so each container caches it with a TTL and fetches it again when a token names an unknown `kid` (key id).
2. `iss` is exactly `platform-idp`.
3. `exp`.
4. `aud` is on the container's allow-list, and it says which token kind this is (table below). A token that matches no kind is refused.
5. The org rule for that kind, then the role rule for a user ([below](#the-org-and-role-rule)).

| Container | Accepts | Refuses |
|---|---|---|
| `ae-design-agent` | User JWT (flows 13, 2). AE-only M2M (flow 2). | `ae-studio-<org>`, publisher client, any other `aud`. |
| `ae-studio-tools` | User JWT on the git-only routes only (spec file reads, issue-backed task lists, repo reads): flow 14, and flow 3 when `aep-api` forwards a user request to that same set. AE-only M2M on the `aep-api`-coupled routes only (flow 3): repo create, the skills mirror, and the other git and GitHub work that `aep-api` or Temporal drives. | A user JWT on an `aep-api`-coupled route. `ae-studio-<org>`, publisher client, any other `aud`. The webhook path (flow 5) takes no token; it checks the org HMAC ([08-git-and-github.md](08-git-and-github.md)). |
| `ae-collab` | User JWT (flow 4). `ae-studio-<org>` of this pod's org (the agent's Room join). Both arrive in the Hocuspocus auth message on connect, never in the URL or a cookie. | AE-only M2M, publisher client, any other `aud`. |

Each container has its own small check: TypeScript in the two Node containers (`ae-design-agent`, `ae-collab`) and Go in `ae-studio-tools`. There is no shared checker and no forwarding of checked claims between containers.

## The org and role rule

Every decision is made in the pod. No container calls `aep-api` for a join or a turn.

1. **Token org = pod org.** The pod's org (its `ouId` and `ouHandle`) is fixed when the Resource is rendered.
   - User JWT and `ae-studio-<org>`: `ouId` **and** `ouHandle` must both equal the pod's org.
   - AE-only M2M: `X-Impersonate-Org` must equal the pod's org UUID, the client id must be the pinned `APP_FACTORY_BFF_TO_AE_STUDIO`, and the grant must be `client_credentials`. The pod compares the header to its own fixed value. It does no lookup.
2. **Role (user JWT only).** The role claim in the token: `ae:design` to edit and run turns, `ae:design-view` to watch. Until WSO2 Cloud issues the `ae:*` roles, the rule is membership of the org (step 1). A role change takes effect with the user's next token.
3. **Project.** The project must be one of this org's repositories that `ae-studio-tools` knows. `ae-collab` asks `ae-studio-tools` on the Files API socket. `ae-design-agent` asks on the MCP socket. Neither takes a project or Room from a string it parses.

The `ae-studio-<org>` token and the AE-only M2M token are checked for org only, with no role step. `aep-api` has already authorized the user, or is running its own background work, before it sends the AE-only M2M token.

## AE-only control-plane client

`aep-api` uses `APP_FACTORY_BFF_TO_AE_STUDIO` (working name) for two kinds of call:

- calls to `ae-studio` with no user on the request: Temporal activities, project kickoff, the marketplace chat;
- every `aep-api`-coupled operation on `ae-studio-tools` (flow 3), such as repo create and the skills mirror, even when a user started it.

It gets the token with `client_credentials` at the Platform IdP and sends it with `X-Impersonate-Org` set to the target org.

- The client is **not** in platform-api's impersonation policy. A copy taken from a dataplane pod is refused at platform-api.
- Its secret stays in `aep-api`. It never reaches a dataplane pod.
- For a user's turn (flow 2) or a user's request to the git-only routes (flow 3), `aep-api` forwards the user JWT instead.
- WSO2 Cloud provisions the client and its secret (O-15).

A leaked AE-only M2M token reaches every org's `ae-studio` while it lives, because the org is only a header. Org-bound machine tokens are planned ([below](#planned)).

`APP_FACTORY_BFF_TO_PLATFORM_API` stays `aep-api`'s client for platform-api only. It is never sent to the dataplane, and `ae-studio` never accepts it.

## Publisher client

`ae-studio-tools` and `ae-coding-tools` mount the publisher client (`client_id`, `client_secret`) through ESO (External Secrets Operator). They get a token with `client_credentials` at the Platform IdP, then call the public `aep-api` gateway. The publisher client is used **only** from the dataplane to the control plane. It is never mounted on a model container, and there is no second publisher app. It carries:

- flow 6: verified webhook events from `ae-studio-tools`;
- flow 7a: the coding run's platform calls, including the platform MCP tool calls of `ae-coding-agent`;
- flow 12: the platform MCP tool calls of `ae-design-agent`;
- flow 15: design-turn usage records. `ae-design-agent` buffers the record of each finished turn and hands it to `ae-studio-tools` on the MCP socket. `ae-studio-tools` sends a batch to `aep-api` every few minutes and when the pod shuts down. `aep-api` keys the ledger on the turn id, so a batch sent twice counts once. Records not yet sent are lost if the pod dies (accepted risk, [12-gaps-and-open-items.md](12-gaps-and-open-items.md)).

No `ae-studio` container accepts the publisher client token.

## How `ae-design-agent` joins a Room

`ae-design-agent` joins a Room on `ae-collab` with an `ae-studio-<org>` token.

- **The identity.** `aep-api` creates `ae-studio-<org>` at runtime, as it creates `aep-publisher-<org>`. It is a Thunder Agent entity in the org OU, so its token carries `ouId` and `ouHandle`, and Agent entities are listed apart from applications. If WSO2 Cloud cannot use Agent entities, it is an application in the org OU with an `ae-` name prefix (O-14). `aep-api` writes its secret through the SM API (flow 10). ESO mounts it **only** on `ae-studio-tools`.
- **Getting the token.**
  - When `ae-design-agent` joins a Room, it asks `ae-studio-tools` on the MCP socket.
  - `ae-studio-tools` gets an `ae-studio-<org>` token with `client_credentials` at the Platform IdP and returns it. It returns a token only for a Room join.
  - `ae-design-agent` keeps the token in memory for that connection. A reconnect asks again.
  - The model container never holds the client secret.
- **Sending it.** `ae-design-agent` sends the token in the Hocuspocus auth message on connect to `localhost`, as the browser does. Never in the URL.
- **The check.** `ae-collab` checks the signature, `iss`, `exp`, `aud` = the `ae-studio-<org>` client of this pod's org, and `ouId` and `ouHandle` = this pod's org. It checks org only. It has no other rule for an agent peer.
- **Scope.** The token opens any Room of the org while it lives. This is an accepted risk ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)).
- **Credit.** Commits credit the user named in the turn: from the user JWT on flow 13, or from `aep-api` on flow 2.
- **A separate identity.** `ae-studio-<org>` is not the publisher client. A coding run holds the publisher client, so it cannot join a Room.

Agent edits are shown in the Room for review. They are not held back: a save commits what the Room holds, and Build uses it.

A token is needed even on `localhost`: any container in the pod reaches any `localhost` port, and `ae-collab` serves every Room of the org.

## How `ae-design-agent` calls platform MCP tools

The design agent uses the same pattern as the coding agent: the model container asks its tools container, and the tools container calls `aep-api` as the publisher client. `ae-design-agent` holds no token for `aep-api`.

- **Channel.** `ae-design-agent` calls `ae-studio-tools` on a Unix socket in its own emptyDir (a pod-local scratch volume), mounted only into those two containers. No token. It is not the Files API socket: `ae-collab` cannot see the MCP socket, and `ae-design-agent` cannot see the Files API socket.
- **What `ae-studio-tools` serves.** The socket carries four things, and refuses any other JSON-RPC method or tool name:
  - one MCP server with a fixed allow-list of eleven read-only tools (below);
  - the Room-join token request;
  - the hand-off of finished-turn usage records;
  - the project-known lookup (is this project one of the org's repositories).
  - `get_remote_git_file_contents` and `search_remote_git_code` run on `ae-studio-tools` with the gitpat (flow 9). The repo owner must be the org's GitHub owner.
  - The other nine go to `aep-api` `/internal/v1/mcp` over flow 12, one tool call per request, with the publisher client token.
- **Org.** Fixed by the publisher client. `aep-api` takes it from `ouHandle`, never from the agent's input.
- **Check.** Gateway `jwt-auth` (`iss=platform-idp`), then `aep-api` checks the `aud` prefix and `ouHandle`, as for flows 6 and 7a.

The coding Job does the same in `ae-coding-tools`: remote-git with its gitpat (flow 7b), the other tools over flow 7a.

Calls are not bound to a user or a turn. `ae-design-agent` can call a tool between turns. The tools are read-only and scoped to this pod's org, which is less than the Default key the container already holds. This is an accepted risk ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)).

## What never happens

- A client secret (publisher client, `ae-studio-<org>`) is never on a model container and never in the browser.
- `APP_FACTORY_BFF_TO_PLATFORM_API` is never sent to the dataplane and never accepted by `ae-studio`.
- The `APP_FACTORY_BFF_TO_AE_STUDIO` secret never leaves `aep-api`.
- `aep-publisher-<org>` is never accepted by `ae-collab`, or by any other `ae-studio` container.
- The `ae-studio-<org>` token is never accepted by `ae-design-agent`, `ae-studio-tools` or `aep-api`.
- No `ae-studio` container calls `aep-api` to authorize a Room join or a turn.
- `ae-studio-tools` never accepts a user JWT on an `aep-api`-coupled route.
- No token is sent in a URL or a cookie.
- `ae-design-agent` never reaches the Files API of `ae-studio-tools`.
- `aep-api` mints no token. `/internal/v1/mcp` accepts only the publisher client token.
- `ae-design-agent` never holds a token for `aep-api`.

## Planned

Two Platform IdP features close the two gaps this design accepts. Each is a WSO2 Cloud ask ([12-gaps-and-open-items.md](12-gaps-and-open-items.md)).

| Planned | What it changes | Until then |
|---|---|---|
| **AE-only audience** (O-12) | An AE resource server on the Platform IdP (RFC 8707 resource indicators). The console asks for it, so user tokens meant for `ae-studio` carry an AE-only `aud` and the `ae:*` roles. `ae-studio` then drops `APP_FACTORY_CONSOLE` from its allow-list. | `ae-studio` accepts `aud=APP_FACTORY_CONSOLE`. A user JWT leaked from the dataplane works on every console API for up to 1 hour. |
| **Org-bound machine tokens** (O-13, thunderid#4037) | A machine token for `aep-api` → `ae-studio` that carries its org. `ae-studio` checks the org from the token, not from `X-Impersonate-Org`. | A leaked AE-only M2M token reaches other orgs' `ae-studio` by changing the header. |

## Not chosen, and why

- **Tokens minted by `aep-api` (RS256, its own JWKS) for the control plane → dataplane hop, the Room and the agent's Room join.** A second issuer in the dataplane. `ae-studio` would depend on `aep-api`'s signing key and its rotation. Platform IdP tokens are checked against a public JWKS.
- **Environment Thunder token exchange (RFC 8693).** WSO2 Cloud enables the grant for no client. An exchanged token has no refresh token and needs a live user token, so it cannot carry long Temporal work.
- **One checker in `ae-studio-tools` that forwards to the other containers.** Puts the public Room WebSocket on the container that holds the gitpat, and adds a hop that trusts forwarded claims.
- **Reusing `APP_FACTORY_BFF_TO_PLATFORM_API` at `ae-studio`.** A compromised pod could replay it to platform-api as any org.
- **A per-org client for `aep-api` → `ae-studio`.** The control plane cannot read a per-org secret from the write-only SM API.
- **The publisher client for the control plane → dataplane hop.** Mixes directions and still needs a control-plane-readable org secret.
- **Reusing `aep-publisher-<org>` for the Room join.** Coding runs hold it, so they could join Rooms.
- **The shared M2M token in the turn body.** An all-orgs token in a model container.
- **The `ae-studio-<org>` secret on `ae-design-agent`.** A client secret on a model container. `ae-studio-tools` holds it and serves only tokens.
- **A child OU or a shared OU for AE's apps.** The token then carries that OU's `ouId` and `ouHandle`, not the org's, which breaks the org rule. Thunder's application list ignores OU anyway, so it groups nothing.
- **Waiting for an AE audience before the dataplane checks user JWTs.** The console audience with the org rule works now. The AE-only audience is planned.
- **Asking `aep-api` per Room join or turn (`collab/validate`).** A control-plane call on every join. The token and the pod's own repository list answer it.
- **One user-JWT surface for all `ae-studio-tools` routes.** A browser could call repo create or the skills mirror directly and skip `aep-api`.
- **Org dataplane API keys as the hop's identity.** An API key does not bind the org and the caller.
- **`aep-api` proxies the Room WebSocket.** Yjs across two public gateways.
- **No token for the agent's Room join, because it is on `localhost`.** Any container in the pod reaches any `localhost` port, and `ae-collab` serves every Room of the org.
- **A secret shared inside the pod.** A new secret on the model container, with no org binding from the Platform IdP.
- **An `aep-api`-minted MCP token sent by `ae-design-agent` to a public `aep-api` route with gateway `jwt-auth` off.** Adds a second public `aep-api` route where only the app checks the token, and breaks the rule that dataplane → control plane calls use the publisher client.
- **`aep-api` runs the MCP tools and returns results over flow 2.** No dataplane → control plane call, but it needs an MCP bridge over SSE, a result route and shared turn state in both services.
- **`localhost` TCP for the MCP channel.** `ae-collab`, which serves the public Room WebSocket, could call the tools.
- **A per-turn token on the in-pod MCP channel.** Binds calls to a turn, but adds a token for read-only tools that give less than the Default key already does.
- **One socket for the Files API and MCP.** All three containers would mount it, and `ae-design-agent` could call `files/apply`.
- **`aep-api` forwards the remote-git tools to `ae-studio-tools` over flow 3.** An extra hop; `ae-studio-tools` already holds the gitpat and serves the agent directly.
