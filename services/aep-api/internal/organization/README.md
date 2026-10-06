# organization — Organization Onboarding & Settings

> **L2 · a domain.** Part of the [aep-api architecture](../../README.md).

Bring a tenant org onto the platform (JIT onboarding + the phantom-OU trust guard) and own every
per-org, org-keyed record that configures its integrations — GitHub credential, the AI agents card
(the model connection, coding runtime, Claude subscription), IDP publisher — all fronted by the
consolidated `/config` resource.

```mermaid
flowchart LR
  API(["/api/v1"]) --> SL
  S2S(["/internal/v1"]) -.-> CORE
  subgraph organization
    SL["slices — getconfig · patchconfig · testllm · disconnect · discover idp · listorgs"]
    CORE["config orchestrator + credential / anthropic / model-connection / idp / org services"]
    SL --> CORE
    CORE --> DB[("organizations · org_credentials · org_model_connections · org_anthropic_credentials · org_agent_settings · organization_idp_profiles")]
  end
  CORE -->|IdentityOps · IssueService| SC[[sourcecontrol]]
  CORE -->|org secrets: SecretReference + vault write| SM[[clients/secretmanagersvc]]
  CORE -->|publisher app · OU| THUNDER(["Thunder"])
  CORE -->|model probe, netguard, no redirects| MODEL(["the org's model endpoint"])
```

## Slices
| Slice | Use-case | Entry |
|---|---|---|
| `getconfig` `patchconfig` | read / atomic multi-section write of the org config | `GET`+`PATCH .../config` |
| `testllm` | probe a model connection without saving it (rationed: 10 per org per minute) | `POST .../config/llm/test` |
| `disconnectgithub` | disconnect cascade for the org's git provider | `POST .../config/git-provider/disconnect` |
| `discoveridp` | OIDC discovery for a BYO IDP | `GET .../config/idp/discovery` |
| `listorgs` | enumerate orgs (tenant-gate carve-out — no org ctx) | `GET /organizations` |
| `aestudio` | install and converge the org's AE Studio ([ADR-0040](../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)): its desired state, the Ensure over OpenChoreo, the status state machine; the tools pod's lookups behind `/internal/v1/ae-studio/` (`ProjectRepositories`: a project's repository; `SkillsRepositories`: the org's `_skills` repository, its library reconciled first) | `StudioConverger` · `AEStudioStatusReader` |

*Flat in the domain root, outside the slices: the credential / anthropic / agent-settings /
model-connection / idp services.*

## Ports
| Port | Dir | Peer · contract |
|---|---|---|
| `IdentityOps` · `IssueService` | needs | `sourcecontrol` — the validator's PAT probe (the gitpat's GitHub user, read through the org's pod), disconnect issue cascade |
| `OwnerLookup` | offers | `sourcecontrol` — `CredentialService.GitHubOwner`, the connected login new repositories are created under |
| `thundersvc` · `secretmanagersvc` | needs | publisher-app CRUD + OU check · secret-ref mirror |
| `OrganizationService` · `CredentialService` · `AnthropicCredentialService` · `IDPService` | offers | `delivery` (coding identity/publisher) · `sourcecontrol` (credential resolution) |
| `ModelConnectionService` — `ConnectionReader` · `CodingCredentialResolver` | offers | the app root (the governor's and AE Studio's keyless `Connection`) · `projects` (ai-agent model access: `KeyPathRef`) · `delivery` (the coding credential and the connection's model; the evaluation key's reference) |
| `RateCard` | needs | `platform/modelcost` (the boot-time `Stamper`) — whether `(host, model)` is priced, for `llm.priced` |
| `AgentSettingsService` | offers | `delivery` (the run's runtime) |
| `StudioConverger` · `AEStudioStatusReader` (+ `AEStudioStatus`) | declared here, implemented by `aestudio` | the gitpat submit and a key or connection save trigger a converge; `GET /ae-studio` reads the state |
| `aestudio.OC` | needs | OpenChoreo — Project, PRB, ResourceType, Resource, RRB, SecretReference reads; one client set: status reads and the converge alike as aep-api's own identity where the install impersonates orgs (the `/api/v1` org-membership gate is the only check on the caller) |

## Owns
- `organizations` (+ `thunder_org_uuid`, `llm_disconnected_at`), `org_credentials`,
  `org_model_connections` (one row per org, absent = no connection: format, base URL, host, model, auth
  scheme, probed limits and image input; no character of the key — the key lives only in the vault,
  as the org secret `default-key`, and `llm` reads as configured only while that reference row exists),
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
  - A stored connection is usable by construction: a save that changes it is refused unless its
    probe with the request's key passes (a model-only edit is saved unprobed); there is no status
    and nothing revalidates.
  - `agents` is never null on the wire: the platform default runtime until someone chooses,
    `updatedBy` telling the two apart. `null` on the PATCH resets (row and token deleted).
  - A runtime is never substituted. Only a runtime the installation can run is selectable
    (`agents.availableRuntimes`, `agents_runtime_unavailable`); `llmFormats[].runtimes` says which
    runtimes here run each format.
  - `llm_disconnected_at` is the only trace of a disconnected connection; projected as
    `llmDisconnectedAt` while `llm` is null, cleared by the next connection save.
  - A key lives only in the vault. A save writes each key it carries from the request, under the
    card's lock (`AgentsCardRepository.Lock`, held on its own connection from the first read through
    the copies): a connection key as a new `default-key` reference, a subscription token as a new
    `coding-agent-key` one (`OrgSecretWriter.Write`, lock order card → default-key →
    coding-agent-key), and the rows commit as one transaction inside the last write. A failed vault
    write saves nothing, logs `orgsecret.write_failed {org, secret, reason}` (value-free) and answers
    `502 secret_store_write_failed` on the key's section; a failed transaction undoes the new
    references. No key is stored in or read
    from Postgres, and no triplet is stamped on the rows. A save that writes a key on an installation
    with no secret store is refused (`503 secrets_delivery_unavailable`).
  - A connection edit (format or base URL) needs the key in the same save (`llm_key_required`):
    Agent Manager's provider is rewritten whole. A model-only edit needs none and is saved unprobed.
    Test connection always needs the key in its body.
  - After the commit, still under the card's lock: the key's path consumers (`ModelKeyConsumers`: the
    org's `ai-agent-model-access` SecretReference behind direct ai-agent components, implemented in
    `projects`) move onto the new `default-key` reference (while that fails the previous reference
    is kept), a deleted credential's row and reference are removed (best-effort), Agent Manager's
    provider gets the request's key, and only then are the replaced references retired.
  - A save that changes what the AE Studio pod reads triggers its converge after the commit: a
    written Default key, a disconnect (the pod is re-pinned without the key), or a change to the
    connection's non-secret fields (`AE_MODEL_CONNECTION`). A subscription token never rolls the
    pod: the next coding Job reads its row.
  - Exactly one credential reaches a coding run. `ResolveCodingCredential(ctx, org, runtime)` is
    the single statement of which: the subscription only on `claude-code`, else the connection key,
    failing closed on an unusable subscription. It answers with a kind and the connection, never a
    variable name; dispatch (`codingagent/model_env.go`) maps that to the runner's env.
  - Generated agents run on the connection, on every format (`modelconn.CapabilitiesOf` says
    `GeneratedAgents` for all). The Agent Manager provider's copy follows it (`syncModelProvider`):
    republished after commit, with the request's key, on every save that carries a key (a failed
    push answers `502 agent_manager_not_updated`, the key staying saved), and cleared once on a
    disconnect (best-effort).
- **The model connection is read only through `ModelConnectionService`** (`model_connection_service.go`):
  a `modelconn.Connection` (format, base URL, host, model, auth scheme, limits, image input) alone
  (`Connection`), beside its key's reference (`KeyRef`, `KeyPathRef`) or as the coding credential
  (`ResolveCodingCredential`), from `org_model_connections` and the reference rows. Nothing reads the
  key.
- **Consumers mount the reference an org secret's row records, not its triplet** (R7,
  `RecordedOrgSecretRef`): `KeyRef` and `ResolveCodingCredential` take the `default-key` /
  `coding-agent-key` name, coding dispatch the `github-pat` (key `token`) and `ae-publisher-client`
  names, a component build (`StageBuildSecret`) the `github-pat` name as `repository.secretRef`
  (the checkout reads key `password`; no value passes through aep-api, no row is
  `ErrOrgDisconnected`), so a rotation never hands out a deleted reference. No consumer has a
  triplet fallback: an org with no row has no reference, and coding dispatch refuses. A mount needs
  only the name and the key (C10), so `KeyRef` carries no vault path. The ai-agent model access,
  which points its own SecretReference at the key's vault path, reads `KeyPathRef` instead: the
  `default-key` row's name with the vault path that SecretReference's `spec.data` reads (names and
  paths, never a value), failing closed when either is missing. The model keys have no triplet
  fallback: an org with no `default-key` / `coding-agent-key` row (saved before the rows existed)
  resolves no key until it saves it again.
- **The publisher client secret lives only in vault; builds and deploys only read** ([ADR-0042](../../../../docs/decisions/ADR-0042-an-org-secrets-value-lives-only-in-vault.md)).
  The gitpat submit's `EnsureClient(publisher)` is the one writer of the publisher app and its
  `ae-publisher-client` reference; the profile keeps `publisher_client_id` and
  `publisher_thunder_app_id`, never the secret or a reference to it. `POST /build` runs
  `RequirePublisherForBuild`: a read of the `ae-publisher-client` row (no Thunder call, no heal); no
  row is `delivery.ErrPublisherCredentialsMissing`, answered `409 publisher_credentials_missing`
  "Reconnect GitHub to set up this organization's build credentials". The deploy reads only the
  profile's issuer (`projects.OrgIDPProfiles`). There is no user rotation: a lost reference is healed
  by the next gitpat submit. Coding dispatch mounts the reference the row names, and refuses
  without one.
- **A BYO IdP needs an issuer.** `SetProfile` (and `PATCH /config` before it writes any section)
  refuses an `asgardeo` or `custom` profile with a blank issuer: 400 `validation_failed` at
  `body.idp`. The deploy pins protected APIs to that issuer, and with none they would trust every
  keymanager on the cluster. `platform` takes the cluster's issuer, so it needs none.
- **Thunder org apps are read by their stored entity id.** Thunder has no lookup by clientId, so the
  profile keeps `publisher_thunder_app_id` (and `studio_thunder_app_id` for `ae-studio-<org>`); every
  ensure and delete passes it to `thundersvc`, which falls back to one full list scan only on a
  miss. A revoke or IDP-kind switch clears it with `publisher_client_id`, and removes the
  `ae-publisher-client` reference once the Thunder app is deleted, so the build gate stops passing on
  a deleted app's credentials.
- **The org secrets are written as a new reference per write** (`OrgSecretWriter`): new
  reference → row (compare-and-swap) → repoint → delete the previous one by its stored name, under
  a per-(org, secret) advisory lock taken after any caller lock and never inside a repoint. The
  GitHub PAT (`github-pat`, keys `token` + `password`) and the two org clients go through it; no
  triplet or secret column is stamped.
  - Retire timing (ruled in phase 1): the previous reference is retired right after the row and
    stamp commit, not after the AE Studio pod has moved off it. The pod's converge is only
    triggered, so its ExternalSecrets (`es-tools`, `es-agent`) still name the retired vault path
    until that converge repoints them. They set `deletionPolicy: Retain` explicitly
    (`resourcetype.yaml`, pinned by `TestTemplate_Invariants`), so the pod keeps its last synced
    Secret, the previous values, until the converge rolls it onto the new reference. Retiring only
    after a converge confirms the new rev is the later fix, owned by the phase whose pod first
    uses these secrets.
- **A gitpat submit is: validate → Connect → `github-pat` reference → `github-webhook-secret` (made
  once, under the lock) → `EnsureClient` publisher, then studio → converge trigger** (`gitpat_submit.go`).
  Connect writes no reference itself, and the setup runs after the patch's other sections (an idp
  kind switch's publisher revoke cannot undo it). Every vault path derives from the request's `ouId`
  (the Secret Manager API derives namespaces from the JWT); a client's Thunder OU is the org row's
  `thunder_org_uuid`, and `EnsureClient` refuses when they differ. A failed step fails `gitProvider`
  with code `ae_studio_setup_incomplete` (502, or 409 for a concurrent write, a foreign-OU client or a
  missing or disagreeing OU): the connection is saved, and saving the token again retries every failure
  but the OU ones, whose message names the operator action. OUs compare as UUIDs, whatever their case. With no converger or no secrets
  delivery the submit succeeds and logs `ae_studio_not_configured`. Nothing waits for the pod.
- **A gitpat disconnect takes the org's AE Studio down before the credential goes** (`OrgDisconnectService`,
  [ADR-0040](../../../../docs/decisions/ADR-0040-design-work-runs-in-the-organizations-ae-studio.md)): the repo hooks are unregistered through the pod while it still holds the
  gitpat (best effort, `WebhookService.UnregisterOrg`); the Resource `ae-studio` is deleted
  (`aestudio.Service.Remove`, which holds the org's converges and waits out a running one until the
  cascade ends; OpenChoreo's finalizer takes its binding, release, pod, clones and reference documents
  with it; the Project and ResourceType stay); the `github-pat` and `github-webhook-secret` rows and
  references are removed (`SecretRefWriter.RemoveGitHubSecrets`); the hook ids are forgotten
  (`ForgetOrg`); then Phase D. Any of the last four failing stops the cascade with the credential
  active, and a retry repeats it (every step treats "already gone" as done). The converge gate needs
  both the gitpat row and an ACTIVE credential, and the sweep's hook repair needs the active credential,
  so nothing brings the pod or the hooks back for a disconnected org; a reconnect writes both secrets
  anew (the webhook secret as on a first submit) and converges.
- **AE Studio converges on drift, single-flight per org** (`aestudio`). The Ensure
  is Project `ae-system` → PRB in the write target (`WriteTargets.Resolve(org, "ae-system")`) →
  ResourceType `ae-studio` (PUT in place, annotated `aep.wso2.com/ae-studio-template-hash`, never
  deleted) → Resource `ae-studio` → wait for its release → RRB `ae-studio-<env>` pinned to it. Each
  step reads first and writes only what differs. Drift compares the live objects projected onto the
  keys aep-api writes, so OpenChoreo's defaults, labels and status are never drift. A Trigger during a
  converge makes it run once more. The converge keeps the request's values but not its cancellation.
  Status and the converge read and write as aep-api's own identity: where the install has an M2M
  identity and impersonates orgs, their clients never pass the caller's JWT through
  (`app.aeStudioOCConfig`). It takes no lock and
  ensures no client: org secrets are read by reference (`spec.data` copied, never recomputed), and a
  container's `rev` hashes the reference names it reads, so a save rolls the pod and a no-op save
  does not. The agent's key entry exists only while the `default-key` row does. A failed converge
  answers `failed` for 30 s unless the desired state changes (one that failed before it had a desired
  state matches any). A missing ProjectReleaseBinding is drift. A binding with no drift that is not
  Ready answers `provisioning` until it is stuck, then `failed` (logged value-free as
  `ae_studio.status_failed` with an OC reason code): a terminal Ready reason (`RenderingFailed`,
  `InvalidReleaseConfiguration` or `ReleaseOwnershipConflict`: a release OC cannot render or own) once a minute has passed since this replica's
  last successful converge, or not Ready for longer than `notReadyBound` (10 min, above the pod's
  200 s startup budget; this is how a stuck pod, CrashLoopBackOff, ImagePullBackOff or unschedulable, reaches `failed`, as OC reports no distinct reason for it) counted from the later of that converge and the Ready condition's last
  transition. Nothing is converged for it; a save that changes the desired state starts the clock
  again.
- **`EnsureClient` keeps Thunder and the vault agreeing** (`client_ensure.go`): a created app is
  stored with the secret Thunder returns once; a found app with no reference row is healed with a new
  secret written to the vault before Thunder's `PUT` (inside the repoint, so a failed `PUT` rolls the
  reference back); a found app with its row is left alone. An `ae-studio-<org>` app under another OU
  fails the ensure and is never touched. The whole ensure (Thunder ensure, row check, write, `PUT`)
  holds the client secret's lock (`OrgSecretWriter.WithLock`). Thunder calls and profile updates take
  no advisory lock, so the lock order holds.
- **`OrgCatalogVaultKey` reconstructs a Registered External's org-catalog vault path from the
  request JWT `ouId`** — a read, not a second write. Used after aep-api restart when the
  process-local value plane is empty (ADR-0021). A missing `ouId` cannot invent a path.
- Org config wire types (`ConfigProjection`/`ConfigPatch`/`*Projection`) are hand-written pure DTOs in
  `models/` (codegen can't express them) — referenced directly, **not** a wire/domain split.
- The `ListOrganizations` op is the one tenant-gate carve-out (it carries no org context). Platform-wide
  rules (tenant gate, secrets fence) → [../../README.md](../../README.md).
