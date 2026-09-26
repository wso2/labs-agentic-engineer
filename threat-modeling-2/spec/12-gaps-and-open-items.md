# 12 Gaps and open items

Three kinds of entry:

- **Gaps**: the intended control is designed, but WSO2 Cloud does not have it in place yet. The Cloud threat model models the intended control and tags the gap.
- **Accepted risks**: known and accepted in this design.
- **Open items**: a decision this spec needs and does not make. None of them is decided here.

## Gaps (control not yet in place)

| Gap | Today | Intended | Where it shows |
|---|---|---|---|
| **GAP-2** | The CP → DP service token (flows 2, 3), the Room token (flow 4) and the agent Room token are RS256 tokens minted by `aep-api`. WSO2 Cloud has no token-exchange grant. | RFC 8693 token exchange at Environment Thunder. Intended Room token: issuer Environment Thunder, subject the user, `aud` this org's `ae-collab` + Room; `ae-collab` checks the Environment Thunder JWKS. | TB-2, TB-3; [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| **GAP-3** | No gVisor RuntimeClass. | gVisor on both agent pods. | TB-4, TB-6; [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md) |

GAP-1 is retired. Moving the webhook to `ae-studio-tools` is AE's own change, listed in [13-change-inventory.md](13-change-inventory.md), not a missing WSO2 Cloud control. GAP-2 and GAP-3 keep their numbers.

## Accepted risks

- **No prompt-injection filter** on either agent. A model container holds only an Anthropic key, and the tools containers act only within their scope.
- **Model containers hold an Anthropic key.** The agent needs it to run. An AI gateway that holds the key is out of scope.
- **Platform MCP calls from `ae-design-agent` are not bound to a user or a turn.** The MCP socket carries no token, so the agent can call a tool between turns, and `aep-api` sees the org's publisher client, not the user. The tools are read-only and scoped to this pod's org, which is less than the Default key the container already holds.
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
| O-9 | **Switch to the Environment Thunder exchange** for the CP → DP service token and the Room token, once WSO2 Cloud enables the grant. Needs a new decision to switch verifiers and `aud`. | Closes GAP-2. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-10 | **How dataplane containers fetch and refresh the aep-api JWKS.** `ae-studio-tools`, `ae-design-agent` and `ae-collab` check `aep-api`-minted tokens against its JWKS. The public doors into `aep-api` are the console web server (user JWT) and the gateway with `jwt-auth` (`iss=platform-idp`). The route, its authentication and key rotation are not stated. | A DP → CP call that crosses TB-3 and has no flow number. | [04-flows.md](04-flows.md), [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-11 | **Where dependency secrets and test-user passwords live during a coding run.** Intended: dependency secrets (for example the app's database password) stay out of the `ae-coding-agent` container and are held by `ae-coding-tools`, like the gitpat. Test-user passwords are kept in vault through the SM API, not posted in GitHub issue comments; the validation agent may receive a project's test-user password to sign in to the app (project-scoped test logins). This replaces the accepted risk in ADR-0022 of passwords in issue comments and needs a new ADR. | Model containers must mount only the org's Anthropic keys. A password in an issue comment is readable by anyone who can read the repository. | [03-components.md](03-components.md), [06-secrets.md](06-secrets.md) |

This spec does not name who runs the 7-day conversation delete. Today the agents service runs it with its own Postgres connection; in this spec that service becomes `ae-design-agent` and only `aep-api` reaches Postgres.

O-1 (how `ae-design-agent` joins a Room) is decided: the agent Room token in [07-identity-and-tokens.md](07-identity-and-tokens.md), with the in-pod channel rules in [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md).

O-2 (how `ae-design-agent` calls the platform MCP endpoint) is decided after the lock: `ae-design-agent` calls `ae-studio-tools` on its own Unix socket, `ae-studio-tools` serves the remote-git tools with the gitpat and passes the other tools to `aep-api` as the publisher client (flow 12). See [07-identity-and-tokens.md](07-identity-and-tokens.md) and [04-flows.md](04-flows.md). The other numbers are kept.

A later wish to merge `ae-collab` and `ae-studio-tools` into one container also needs a new decision. It changes the secret split, the Room token `aud`, and TB-4 and TB-5, and it puts the public Room WebSocket on the container that holds the gitpat.
