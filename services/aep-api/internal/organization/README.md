# organization — Organization Onboarding & Settings

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

Bring a tenant org onto the platform (JIT onboarding + the phantom-OU trust guard) and own every
per-org, org-keyed record that configures its integrations — GitHub credential, the AI agents card
(the model connection, coding runtime, Claude subscription), IDP publisher — all fronted by the
consolidated `/config` resource.

```mermaid
flowchart LR
  API(["/api/v1"]) --> SL
  CB(["/connect/callback"]) -.-> CORE
  S2S(["/internal/v1"]) -.-> CORE
  subgraph organization
    SL["slices — getconfig · patchconfig · testllm · connect/disconnect · rotate/discover idp · listorgs"]
    CORE["config orchestrator + credential / anthropic / model-connection / idp / org services"]
    SL --> CORE
    CORE --> DB[("organizations · org_credentials · org_model_connections · org_anthropic_credentials · org_agent_settings · organization_idp_profiles")]
  end
  CORE -->|AppInstallOps · IssueService| SC[[sourcecontrol]]
  CORE -->|CredentialStore · Resolver| SEC[[platform/secrets]]
  CORE -->|publisher app · OU| THUNDER(["Thunder"])
  CORE -->|model probe, netguard, no redirects| MODEL(["the org's model endpoint"])
```

## Slices
| Slice | Use-case | Entry |
|---|---|---|
| `getconfig` `patchconfig` | read / atomic multi-section write of the org config | `GET`+`PATCH .../config` |
| `testllm` | probe a model connection without saving it (rationed: 10 per org per minute) | `POST .../config/llm/test` |
| `connectgithub` `disconnectgithub` | start GitHub App connect / disconnect cascade | `POST .../config:connect-git-provider` etc. |
| `rotateidp` `discoveridp` | rotate the publisher client secret / OIDC discovery | `POST .../config:rotate-idp-secret` etc. |
| `listorgs` | enumerate orgs (tenant-gate carve-out — no org ctx) | `GET /organizations` |

*Flat in the domain root, outside the slices: the credential / anthropic / agent-settings /
model-connection / idp services, the model key rename watcher (`ModelKeyRename`), the raw
connect-callback controller, and the S2S credentials-refresh.*

## Ports
| Port | Dir | Peer · contract |
|---|---|---|
| `AppInstallOps` · `IssueService` | needs | `sourcecontrol` — App/PAT probes, disconnect issue cascade |
| `CredentialStore` · `Resolver` · `AppTokenMinter` | needs | `platform/secrets` — sealed git-token / model-key / subscription store, credential resolution |
| `thundersvc` · `secretmanagersvc` | needs | publisher-app CRUD + OU check · secret-ref mirror |
| `OrganizationService` · `CredentialService` · `AnthropicCredentialService` · `IDPService` | offers | `delivery` (coding identity/publisher) · `sourcecontrol` (credential resolution) · the edge (dev secret-ref resync) |
| `ModelConnectionService` — `ConnectionReader` · `CodingCredentialResolver` | offers | the app root (the spec agents' and task planning's connection + key per turn; Agent Manager's provider key) · `projects` (ai-agent model access) · `delivery` (the coding credential and the connection's model; the evaluation key) |
| `RateCard` | needs | `platform/modelcost` (the boot-time `Stamper`) — whether `(host, model)` is priced, for `llm.priced` |
| `AgentSettingsService` | offers | `delivery` (the run's runtime) |
| `CredentialsRefreshService` | offers | the S2S runner-refresh op (edge projects it onto `igen.RefreshResponse`) |

## Owns
- `organizations` (+ `thunder_org_uuid`, `llm_disconnected_at`), `org_credentials`,
  `org_model_connections` (one row per org, absent = no connection: format, base URL, host, model, auth
  scheme, probed limits and image input, key preview; the key's bytes in `org_secrets` `model/key`,
  mirrored to SM-API under the entity `model-connection`. `ModelKeyRename` moves a key found under the
  Anthropic-era names (`anthropic/key`, entity `anthropic`) onto these: `migrate/phase20_model_key_rename`
  copies a connected org's bytes at boot, the watcher uploads the new mirror and switches the row under
  the card's lock, and retires the old copies on a periodic pass (never at boot) once the org has no
  open cycle),
  `org_anthropic_credentials` (the optional `coding` Claude subscription only — CHECK
  `org_anthropic_credentials_subscription_only`), `org_agent_settings` (the runtime; one row per org,
  absent = the platform default), `ai_agent_model_endpoints` (the endpoint the Agent Manager govern stage
  last stored beside each governed ai-agent's key, per environment; non-secret, read by
  `delivery/agentgovernance` to tell a moved base path), `organization_idp_profiles` + `idp_audit_events` — gorm + entities in
  this domain (`entity_*.go` over `repository_*.go`), single write-authority.

## Invariants — don't break
- **The phantom-OU trust guard** (`ouIsTrustworthy`): reject a JWT `ouId` ONLY when a wired validator
  positively reports it does not exist; empty id / no validator / transient error all fail-open. A phantom
  OU poisons `wc-` namespace derivation + the publisher OU binding. Both write paths are guarded.
- **This domain is FAIL-LOUD**, not nil-tolerant: a nil collaborator panics, unlike sourcecontrol's
  503 — the edge assigns it directly, no `OrEmpty`.
- The `/config` PATCH is an **atomic multi-section** apply; sections are three-state `patch.Field`.
- **The AI agents card** (`llm` + `agents`, [ADR-0038](../../../../docs/decisions/ADR-0038-an-organization-has-one-model-connection.md),
  [ADR-0036](../../../../docs/decisions/ADR-0036-the-coding-credential-is-a-subscription.md)):
  - One save is ONE transaction under the card's per-org advisory locks, `org_anthropic:<org>` then
    `org_model:<org>` (the first is the previous release's name, so replicas of both serialize),
    covering the connection row, the subscription row, `org_agent_settings` and the `org_secrets`
    bytes (`repository_agents_card.go`). A failure anywhere writes nothing.
    `AgentSettingsService` is the only writer of these rows.
  - `llm` is patched field by field (`model_connection_rule.go`): first connect needs `kind` and
    `apiKey` (the URL and model default from `modelconn.Formats`, so `{kind: anthropic, apiKey}`
    connects); a host or port change needs a new key (`https://x` and `https://x:443` are one origin); a format change on the same host keeps it; keys under 12
    characters are refused; an https URL with no userinfo, query or fragment, a path-less Anthropic URL
    gaining `/v1`. The `sk-ant-` shape and subscription-token refusal apply only on `api.anthropic.com`.
  - One rule, judged on the state the patch leaves (`judgeCard`), before the probe and again in the
    transaction: a format needs a runtime this installation runs; Claude Code needs the Anthropic
    format; the subscription needs `claude-code` and `claudeSubscription` (Anthropic's own API), and a
    save that leaves either deletes it; `llm: null` deletes the connection, its bytes and the token.
  - The probe (`model_probe.go`, one prober per format) runs before the transaction whenever the key,
    URL, format or model changes, through `netguard` with NO redirects (a redirect is
    `llm_unreachable`; Go forwards `x-api-key` across one). It lists models, and proves the key with
    one `max_tokens=1` request when the listing is missing or public (Ollama's is). 401 on x-api-key
    earns one Bearer retry; a rate-limit-shaped 429 proves the key (warning `provider_limit`), a bare
    429 is `llm_unexpected_status`; 5xx is `llm_upstream_error` (502). On `ollama.com` it reads
    `/api/show` for the context window and vision. An unlisted model is a warning, not a refusal. The
    apply refuses (409) a connection that changed since it was probed.
  - A stored connection is usable by construction: there is no status and nothing revalidates.
  - `agents` is never null on the wire: the platform default runtime until someone chooses,
    `updatedBy` telling the two apart. `null` on the PATCH resets (row and token deleted).
  - A runtime is never substituted. Only a runtime the installation can run is selectable
    (`agents.availableRuntimes`, `agents_runtime_unavailable`); `llmFormats[].runtimes` says which
    runtimes here run each format.
  - `llm_disconnected_at` is the only trace of a disconnected connection; projected as
    `llmDisconnectedAt` while `llm` is null, cleared by the next connection save.
  - The copies outside Postgres (the SM-API mirrors, the Agent Manager provider) follow the commit,
    best-effort, in a second transaction under the same locks, made from the rows as they stand, so
    two saves' copies land in save order and a stored key never sits beside another host's row. A
    save that writes a key clears the row's `secret_ref_*` triplet (a save that keeps the key keeps
    it), so a failed mirror fails dispatch closed instead of mounting the previous key; a deleted
    credential's copy is deleted unless a credential saved since owns its path, and an orphaned copy
    is accepted.
  - Exactly one credential reaches a coding run. `ResolveCodingCredential(ctx, org, runtime)` is
    the single statement of which: the subscription only on `claude-code`, else the connection key,
    failing closed on an unusable subscription. It answers with a kind and the connection, never a
    variable name; dispatch (`codingagent/model_env.go`) maps that to the runner's env.
  - Generated agents run on the connection, on every format (`modelconn.CapabilitiesOf` says
    `GeneratedAgents` for all). The Agent Manager provider's copy follows it (`syncModelProvider`):
    republished after commit on a save that changes the key, URL, format or auth scheme, and
    cleared once on a disconnect.
- **The model connection is read only through `ModelConnectionService`** (`model_connection_service.go`):
  a `modelconn.Connection` (format, base URL, host, model, auth scheme, limits, image input) beside the
  key's bytes (`Effective`), its vault reference (`KeyRef`) or the coding credential
  (`ResolveCodingCredential`), all from `org_model_connections`. No consumer outside this domain reads
  the rows for a key.
- **Publisher SecretReference for coding Jobs is fail-closed on `POST /build`.**
  `ProvisionPublisherForBuild` (actor `build-provision`) ensures the Thunder publisher app and stamps
  `secret_ref_name` while the console JWT is on ctx. A missing or disabled `SecretRefWriter` returns
  an error (Build 503) and does not touch Thunder. `EnsureOrgPublisher` on the deployment path still
  swallows SM-API errors. Coding dispatch reads `secret_ref_name` only.
- **`OrgCatalogVaultKey` reconstructs a Registered External's org-catalog vault path from the
  request JWT `ouId`** — a read, not a second write. Used after aep-api restart when the
  process-local value plane is empty (ADR-0021). A missing `ouId` cannot invent a path.
- Org config wire types (`ConfigProjection`/`ConfigPatch`/`*Projection`) are hand-written pure DTOs in
  `models/` (codegen can't express them) — referenced directly, **not** a wire/domain split.
- The `ListOrganizations` op is the one tenant-gate carve-out (it carries no org context). Platform-wide
  rules (tenant gate, secrets fence) → [../../README.md](../../README.md).
