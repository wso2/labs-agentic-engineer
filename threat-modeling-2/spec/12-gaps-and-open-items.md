# 12 Gaps and open items

Three kinds of entry:

- **Gaps**: the intended control is designed, but WSO2 Cloud does not have it in place yet. The Cloud threat model models the intended control and tags the gap.
- **Accepted risks**: known and accepted in this design.
- **Open items**: a decision this spec needs and does not make. None of them is decided here.

## Gaps (control not yet in place)

| Gap | Today | Intended | Where it shows |
|---|---|---|---|
| **GAP-1** | The GitHub webhook still ends on the control-plane webhook RestApi (CORS only, no `jwt-auth`). | GitHub → org kgateway → `ae-studio-tools` webhook path (flow 5). | TB-2; [08-git-and-github.md](08-git-and-github.md) |
| **GAP-2** | The CP → DP service token (flows 2, 3), the Room token (flow 4) and the agent Room token are RS256 tokens minted by `aep-api`. WSO2 Cloud has no token-exchange grant. | RFC 8693 token exchange at Environment Thunder. Intended Room token: issuer Environment Thunder, subject the user, `aud` this org's `ae-collab` + Room; `ae-collab` checks the Environment Thunder JWKS. | TB-2, TB-3; [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| **GAP-3** | No gVisor RuntimeClass. | gVisor on both agent pods. | TB-4, TB-6; [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md) |

## Accepted risks

- **No prompt-injection filter** on either agent. A model container holds only an Anthropic key, and the tools containers act only within their scope.
- **Model containers hold an Anthropic key.** The agent needs it to run. An AI gateway that holds the key is out of scope.
- **Project `ae-system` in WSO2 Cloud** is created by `aep-api`, counts toward the org's `projects` quota, and is visible to the org. Accepted for now.

## Open items

| # | Open item | Why it matters | Where it is marked |
|---|---|---|---|
| O-2 | **How `ae-design-agent` calls the platform's MCP endpoint from the dataplane.** Today it sends an `aep-api`-minted MCP token (`aud=aep-api-mcp`) to `/internal/v1/mcp`. The public `aep-api` gateway expects `iss=platform-idp`, and the publisher client may not be mounted on a model container. | The design agent needs platform tools during a turn. No flow covers this call yet. | [04-flows.md](04-flows.md) (no flow number) |
| O-3 | **Authentication of flow 10** (`aep-api` → SM API) **and flow 11** (ESO → vault). | Both cross TB-9. | [04-flows.md](04-flows.md), [10-cloud-trust-boundaries.md](10-cloud-trust-boundaries.md) |
| O-4 | **How a changed secret reaches a running container.** ESO refreshes the Kubernetes Secret every 15 seconds, but a container reads it as an environment variable. | A key or gitpat change may not take effect in `ae-studio` until its pod restarts. | [05-lifecycle.md](05-lifecycle.md) |
| O-5 | **Order of the first Ensure and the Default key.** gitpat submit creates Resource `ae-studio`, which references the Default key. The org may not have set a Default key yet. | The pod may not start without a referenced secret. | [05-lifecycle.md](05-lifecycle.md) |
| O-6 | **How the publisher client secret is created and rotated** once Postgres holds no secret values. Today `aep-api` keeps it in a Postgres column as well as a SecretReference. | The control plane must not read it back. | [13-change-inventory.md](13-change-inventory.md) |
| O-7 | **What stays in `org_credentials`** after the secret values leave: the GitHub identity and the secret-reference names. | Only the values are required to leave. | [13-change-inventory.md](13-change-inventory.md) |
| O-8 | **Cloud Project name** once the WSO2 Cloud orchestrator can create Resource `ae-studio`. | May move off `ae-system` and off the org's quota. | [11-local-vs-cloud.md](11-local-vs-cloud.md) |
| O-9 | **Switch to the Environment Thunder exchange** for the CP → DP service token and the Room token, once WSO2 Cloud enables the grant. Needs a new decision to switch verifiers and `aud`. | Closes GAP-2. | [07-identity-and-tokens.md](07-identity-and-tokens.md) |
| O-10 | **How dataplane containers fetch and refresh the aep-api JWKS.** `ae-studio-tools`, `ae-design-agent` and `ae-collab` check `aep-api`-minted tokens against its JWKS. The only public door into `aep-api` is the gateway with `jwt-auth` (`iss=platform-idp`). The route, its authentication and key rotation are not stated. | A DP → CP call that crosses TB-3 and has no flow number. | [04-flows.md](04-flows.md), [07-identity-and-tokens.md](07-identity-and-tokens.md) |

O-1 (how `ae-design-agent` joins a Room) is decided: the agent Room token in [07-identity-and-tokens.md](07-identity-and-tokens.md), with the in-pod channel rules in [09-sandboxing-and-guardrails.md](09-sandboxing-and-guardrails.md). The other numbers are kept.

A later wish to merge `ae-collab` and `ae-studio-tools` into one container also needs a new decision. It changes the secret split, the Room token `aud`, and TB-4 and TB-5, and it puts the public Room WebSocket on the container that holds the gitpat.
