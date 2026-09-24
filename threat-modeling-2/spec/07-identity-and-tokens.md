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
| Publisher client token | Platform IdP (`client_credentials`) | the org's publisher client `aep-publisher-<org>` | prefix checked by `aep-api` | what the Platform IdP issues today | flows 6, 7a | Public `aep-api` gateway `jwt-auth`; `aep-api` checks `aud` prefix and `ouHandle`. |

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

`ae-studio-tools` and `ae-coding-tools` mount the publisher client (`client_id`, `client_secret`) through ESO. They get a token with `client_credentials` at the Platform IdP, then call the public `aep-api` gateway. The publisher client is used **only** from the dataplane to the control plane. It is never mounted on a model container, and there is no second publisher app.

### How `ae-design-agent` joins a Room

**Not yet decided.** No token is chosen for `ae-design-agent` → `ae-collab` on `localhost`. The user's JWT is no longer copied into the turn, so today's way does not carry over. See [12-gaps-and-open-items.md](12-gaps-and-open-items.md).

## What never happens

- The user's Platform IdP JWT never goes to the org gateway or to a dataplane container.
- The publisher client is never in the browser and never on a model container.
- A Room token is never accepted by `ae-studio-tools`.

## Intended when WSO2 Cloud supports token exchange (GAP-2)

ThunderID can do RFC 8693 token exchange. WSO2 Cloud does not enable that grant today. When it does:

| Token | Intended issuer | Intended shape | Checked by |
|---|---|---|---|
| CP → DP service token | Environment Thunder | `aep-api` exchanges a Platform IdP JWT at Environment Thunder `/oauth2/token` and sends the new token. No custom API on `ae-studio`. | `ae-studio-tools` / `ae-design-agent`, against the Environment Thunder JWKS. |
| Room token | Environment Thunder | `aep-api` still authorizes the Room first, then exchanges the **user's** Platform IdP JWT. Subject the user, org this org, `aud` this org's `ae-collab` + this Room, TTL a few minutes. The exchange client is a platform client with a platform secret; the org comes from the user's token. | `ae-collab`, against the Environment Thunder JWKS, plus `aud`, `exp`, org. If Thunder will not put the Room into `aud`, `ae-collab` keeps one `collab/validate` call. |

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
