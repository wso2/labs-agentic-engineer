# 12 Gaps and open items

Three kinds of entry:

- **Gaps**: the intended control is designed, but WSO2 Cloud does not have it in place yet. The Cloud threat model models the intended control and tags the gap.
- **Accepted risks**: known and accepted in this design.
- **Open items**: a decision this spec needs and does not make. None of them is decided here. Items marked **WSO2 Cloud ask** need a change by the WSO2 Cloud team.

## Gaps (control not yet in place)

| Gap | Today | Intended | Where it shows |
|---|---|---|---|
| **GAP-3** | No gVisor RuntimeClass. | gVisor on both agent pods. | TB-4, TB-6; [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md) |

GAP-1 is retired. Moving the webhook to `ae-studio-tools` is AE's own change, listed in [13-change-inventory.md](13-change-inventory.md), not a missing WSO2 Cloud control.

GAP-2 is retired. `ae-studio` checks Platform IdP tokens against the public Platform IdP JWKS, so no `aep-api`-minted token waits for a token exchange at Environment Thunder. GAP-3 keeps its number.

## Accepted risks

- **No prompt-injection filter** on either agent. A model container holds only an Anthropic key, and the tools containers act only within their scope.
- **Model containers hold an Anthropic key.** The agent needs it to run. An AI gateway that holds the key is out of scope.
- **Platform MCP calls from `ae-design-agent` are not bound to a user or a turn.** The MCP socket carries no token, so the agent can call a tool between turns, and `aep-api` sees the org's publisher client, not the user. The tools are read-only and scoped to this pod's org, which is less than the Default key the container already holds.
- **An `ae-studio-<org>` token opens any Room of its org while it lives.** `ae-collab` checks it for org only, and accepts it only on its `localhost` listener, so a leaked copy cannot be used from the internet. The token lives only in `ae-studio-tools` and `ae-design-agent` memory, and its lifetime is what the Platform IdP issues. `ae-design-agent` already holds the Default key of the same org.
- **Unsent usage records are lost if the pod dies.** `ae-studio-tools` sends design-turn usage every few minutes and on shutdown (flow 15). Records between the last batch and a crash are not counted.
- **Project `ae-system` in WSO2 Cloud** is created by `aep-api`, counts toward the org's `projects` quota, and is visible to the org. Accepted for now.

## Open items

| # | Open item | Why it matters | Where it is marked |
|---|---|---|---|
| O-3 | **Authentication of flow 11** (ESO → vault), **and of flow 10 writes with no user on the request** (for example the publisher client ensure at deploy). A user-started flow 10 write carries the user's Platform IdP JWT on WSO2 Cloud. | Both cross TB-9. | [04-flows.md](04-flows.md), [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md) |
| O-4 | **How a changed secret reaches a running container.** ESO refreshes the Kubernetes Secret every 15 seconds, but a container reads it as an environment variable. | A key or gitpat change may not take effect in `ae-studio` until its pod restarts. | [05-lifecycle.md](05-lifecycle.md) |
| O-5 | **Order of the first Ensure and the Default key.** gitpat submit creates Resource `ae-studio`, which references the Default key. The org may not have set a Default key yet. | The pod may not start without a referenced secret. | [05-lifecycle.md](05-lifecycle.md) |
| O-6 | **How the publisher client secret is created and rotated** once Postgres holds no secret values. Today `aep-api` keeps it in a Postgres column as well as a SecretReference. | The control plane must not read it back. | [13-change-inventory.md](13-change-inventory.md) |
| O-7 | **What stays in `org_credentials`** after the secret values leave: the GitHub identity and the secret-reference names. | Only the values are required to leave. | [13-change-inventory.md](13-change-inventory.md) |
| O-8 | **Cloud Project name** once the WSO2 Cloud orchestrator can create Resource `ae-studio`. | May move off `ae-system` and off the org's quota. | [11-local-vs-cloud.md](11-local-vs-cloud.md) |
| O-11 | **Where dependency secrets and test-user passwords live during a coding run.** Intended: dependency secrets (for example the app's database password) stay out of the `ae-coding-agent` container and are held by `ae-coding-tools`, like the gitpat. Test-user passwords are kept in vault through the SM API and are not posted in GitHub issue comments; `ae-coding-tools` mounts them for a validation run, and the `ae-coding-agent` container never holds them. Once the passwords are in the write-only store, the API cannot read them back, so how a Developer sees a test-user password is not yet decided. This replaces the accepted risk in ADR-0022 of passwords in issue comments and needs a new ADR. | Model containers must mount only the org's Anthropic keys. A password in an issue comment is readable by anyone who can read the repository. | [03-components.md](03-components.md), [06-secrets.md](06-secrets.md) |
| O-12 | **WSO2 Cloud ask: an AE-only audience.** An AE resource server on the Platform IdP (RFC 8707 resource indicators), so user tokens meant for `ae-studio` carry an AE-only `aud`. The `ae:*` roles come with H-7, not with this ask. | `ae-studio` accepts `aud=APP_FACTORY_CONSOLE`: a user JWT leaked from the dataplane works on every console API, and at platform-api, for up to 1 hour. An AE-only `aud` alone does not stop the platform-api replay, because platform-api checks neither `aud` nor `iss` (O-16). | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-13 | **WSO2 Cloud ask: org-bound machine tokens** (thunderid#4037) for `aep-api` → `ae-studio`. | The AE-only M2M token carries no org; the org is the `X-Impersonate-Org` header. A leaked token reaches other orgs' `ae-studio-tools` `/internal/v1/*` by changing the header: almost any GitHub operation in any org's repositories. Only `aep-api` holds it. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-14 | **WSO2 Cloud ask: the Thunder Agent entity for `ae-studio-<org>`.** Whether the Platform IdP has the Agent entity type AE needs, and whether `aep-api` can give the Agent entity a fixed client id (the `aud` that `ae-collab` checks). If not, `ae-studio-<org>` is an application in the org OU with an `ae-` name prefix. | `ae-collab` pins the `aud` of the Room-join token. An OU other than the org OU breaks the `ouId`/`ouHandle` org rule. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-15 | **WSO2 Cloud ask: provisioning `APP_FACTORY_BFF_TO_AE_STUDIO`** (working name): the Platform IdP application, its secret in the SRE vault, and its delivery to `aep-api` in the WSO2 Cloud deployment. The client must stay out of platform-api's impersonation policy. | `aep-api` cannot call `ae-studio-tools` `/internal/v1/*` until the client exists: no server-started turn (Temporal, kickoff, marketplace chat) and no git or GitHub write, even with a user on the request. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-16 | **WSO2 Cloud ask: platform-api checks `aud`.** platform-api checks the signature, `alg` and `exp` of a JWT, but neither `aud` nor `iss`. It should refuse a token whose `aud` is not meant for it. | A user JWT leaked from the dataplane works at platform-api as the user, whatever its `aud`. O-12 alone does not close this. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |

O-9 is closed. There is no Environment Thunder exchange to switch to, because `ae-studio` checks Platform IdP tokens.

O-10 is closed. The dataplane containers fetch the public Platform IdP JWKS over egress ([04-flows.md](04-flows.md)), and there is no aep-api JWKS.

The conversation thread and its rotation live in `ae-design-agent`, in the pod. This spec does not name how the 7-day conversation delete runs there.

O-1 (how `ae-design-agent` joins a Room) is decided: it joins with an `ae-studio-<org>` token that `ae-studio-tools` gets from the Platform IdP and hands over on the MCP socket ([07-identity-and-tokens.md](07-identity-and-tokens.md)), with the in-pod channel rules in [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md).

O-2 (how `ae-design-agent` calls the platform MCP endpoint) is decided after the lock: `ae-design-agent` calls `ae-studio-tools` on its own Unix socket, `ae-studio-tools` serves the remote-git tools with the gitpat and passes the other tools to `aep-api` as the publisher client (flow 12). See [07-identity-and-tokens.md](07-identity-and-tokens.md) and [04-flows.md](04-flows.md). The other numbers are kept.

A later wish to merge `ae-collab` and `ae-studio-tools` into one container also needs a new decision. It changes the secret split, and TB-4 and TB-5, and it puts the public Room WebSocket on the container that holds the gitpat.
