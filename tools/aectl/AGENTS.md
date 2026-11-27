# AGENTS.md — tools/aectl

AEP Control Plane CLI. Single binary (`aectl`) that installs AEP, provisions OpenBao, and creates the OAuth clients of the platform IdP (on ThunderID, or in an Asgardeo organization).

## Commands

```bash
go build -o aectl .  # build CLI
```

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands for the CLI (`platform`, `config`, `secret`, `sre`, `uninstall`) |
| `internal/openbao/` | HTTP client for OpenBao API |
| `internal/thunder/` | Thunder admin client (OAuth app registration over HTTP), port-forward |
| `internal/asgardeo/` | Client of the Application Management API of an Asgardeo organization, for `idp.clients: asgardeo`: creates AE's applications, lists the wrong settings of an application that exists, checks a secret by a token request, regenerates a secret |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward) |
| `internal/config/` | Viper config defaults and init |
| `internal/idpclaims/` | Resolves the `idp.claims` block: a preset (data files in `presets/`), then the values that the caller lays over it. `platformIDPClaims` (`cmd/platform_idp.go`) supplies the config keys of the loaded config |
| `internal/oidc/` | Reads the platform IdP's endpoints from its OIDC discovery document (`Discover`). `Endpoints.OnBase` puts the same paths on another address: Thunder's in-cluster Service, or the HTTPS gateway through which the environment Thunders fetch its keys |
| `internal/envidp/` | Installs the environment-tier Thunder + its binding record + the environment's API Platform gateway — see deployments/design/two-tier-thunder.md. Org/env come from oc.default_org_namespace/oc.pipeline_source_environment (cmd.ocOrgNamespace/ocPipelineSourceEnvironment); no runtime dependency on Agent Manager |

## Config

CLI has no local config file. After `aectl platform install` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. The ThunderID admin secret is not in the ConfigMap: `aectl` reads it
from the ESO-synced `aep-thunder-admin-creds` Secret, key `client-secret`
(`LoadThunderSecretFromCluster`, `internal/config/config.go`). CLI flags and `AEP_*` env vars
always override the ConfigMap.
`platform update` and `sre install` refuse to run without a valid loaded config
(`requireAEStudioConfig`, `cmd/update.go`): both re-derive and re-apply the
`aeStudio.*` values from it (`sre install` flips `sreAgent.*` through an
in-process `platform update`).

`idp.clients` selects who creates the clients of the platform IdP: `thunder`
(the default), `imported` or `asgardeo`. A key in `keyRegistry`
(`internal/config/config.go`) can belong to values of `idp.clients`
(`clients`). The four `thunder.*` keys are required for `thunder` and are
refused for the other values (`exclusive`). `asgardeo.api_url` is required
for `asgardeo` and is refused for the other values. `idp.issuer` is required
for `imported` and `asgardeo`. There, `idp.issuer` and `asgardeo.api_url`
must be https URLs. For `thunder`, `idp.issuer` is optional: if it is not
set, the issuer is `thunder.public_url`. `idp.org_handle` is always
required. `administersThunder` (`cmd/platform_idp.go`) reports the value
`thunder`. `idpClients` gives the clients that `aectl` creates for the
value.

The `idp.claims` block gives the claim names of the platform IdP
(`internal/idpclaims`). Each value comes from two layers, in this order:
the preset (`idp.claims.preset`: `thunder`, `asgardeo` or `none`; `thunder`
if it is not set) and the `idp.claims.*` keys of the config
(`ResolveClaims`, `internal/config/config.go`). A claim value does not come
from the environment: each command that validates a config stops if an
`AEP_IDP_CLAIMS_*` variable is set (`ClaimsEnvRefusal`).
`platform install`, `platform update` and `platform config test` print each
value and its layer. With `none`, a required value that is not set stops the
command. `aectl` passes the values to the chart. Every reader of the IdP's
tokens reads by them.

## Thunder clients and seeds

- `aepThunderClients` (`cmd/thunder_cli.go`) is the canonical client list; each
  confidential client's secret is generated once, seeded in OpenBao under
  `aep/idp-clients/<vaultName>` (`generatedThunderClientNames`,
  `cmd/platform.go`) and delivered OpenBao → ESO → Secret → env.
  `ae-studio-internal-client` (vault name `ae-studio-internal`, env
  `AE_STUDIO_INTERNAL_CLIENT_SECRET`) carries no org claims
  (`noUserAttributes`), and is `optional` (its own Secret, `aep-ae-studio-internal-secrets`; a
  missing Secret skips the client with a warning).
  `aectl-openchoreo-client` (`aectlOpenChoreoClientID`, vault name
  `aectl-openchoreo`, env `AECTL_OPENCHOREO_CLIENT_SECRET`) is aectl's own
  client: aectl calls OpenChoreo's Secret API with it. Its secret is in its
  own Secret, `aep-aectl-client-secrets`, which no pod reads. The platform
  chart grants the ID in `aectl.openchoreoClient.clientId` only
  `secret:create` and `secret:update` (`templates/aectl/authz.yaml`). The two
  IDs must be the same (`TestAectlOpenChoreoClient_MatchesTheChartsBinding`).
- `orgClients` (`cmd/thunder_cli.go`) adds the two clients of the org:
  `aep-publisher-<org>` and `ae-studio-<org>`. `aectl` registers them under
  the organization unit that `idp.org_handle` names (`OrgOUHandle`,
  `internal/thunder`). It does not create an organization unit. Their vault
  names are `org-publisher` and `org-studio`, and aep-api reads their secrets
  from the `aep-org-client-secrets` Secret. `aectl` does not register
  `aep-system-client` on the platform ThunderID, because aep-api administers
  no IdP.
- `aeStudioOverrides` (`cmd/platform.go`) derives the chart's `aeStudio.*`
  values for `platform install` and `platform update`. The public scheme,
  listener and port suffix come from `tls.enabled` alone (`http`/`http`/`:19080`
  or `https`/`https`/`:19443`); `consoleOrigins` is `[console public URL,
  http://localhost:8090]`.
- `discoverPlatformIDP` (`cmd/platform_idp.go`) reads the platform IdP's
  endpoints from `<issuer>/.well-known/openid-configuration`. The issuer is
  `idp.issuer`. If `idp.issuer` is not set, the issuer is
  `thunder.public_url` (`platformIssuerKey`).
  `platform install`, `platform update` and `sre install` each fetch the
  document once, from the operator's machine. `platform install` fetches it
  before it changes anything in the cluster. If the document cannot be
  fetched, or names another issuer, the command stops. No installer code
  builds the platform IdP's issuer, JWKS or token URL by appending a path.
  The Thunder admin client (`internal/thunder`) and the environment Thunders
  (`internal/envidp`) still name the paths of Thunder's own API.
  `platformIDPOverrides` writes the chart values: `idp.clients`, the six
  `idp.claims.*` values
  (the empty ones too, because the defaults of the chart are those of one
  IdP), `idp.jwksURL` and `idp.tokenURL`,
  `aeStudio.idp.{issuer,jwksUrl,tokenUrl}`, `idp.orgHandle` and
  `idp.orgNamespace`. When the IdP is a ThunderID that `aectl` administers
  (`idp.clients: thunder`), the JWKS and token URLs are the paths of the
  document on `thunder.url`, the address at which pods reach the IdP. If
  not, they are the URLs that the document gives.
  `platformIDPInstallArgs` writes `idp.issuer`.
  `envidp` builds the environment Thunders' trust of the platform IdP from
  the same endpoints on `https://<host>:8443`.
- `platform install` overwrites (plain PUT) only the secrets it is given this
  run: `aep/anthropic-api-key`, `aep/opensearch-username` and, where it
  administers Thunder, `aep/thunder-admin/client-{id,secret}`
  (`platformInputPaths`, `writePlatformSecrets`, `cmd/platform_secrets.go`).
  Every secret it generates — the `aep/idp-clients/<vaultName>` secrets,
  `aep/postgres-password`, `aep/opensearch-password` and
  `aep/webhook-relay-seed` (relay on or off) — is written create-only
  (`generatedPlatformSecrets`, `seedGeneratedSecrets`, `putCreateOnly`,
  `cmd/platform_secrets.go`) and by default never rewritten; that file's
  header comment explains why. The one exception is a client secret under
  `idp.clients: asgardeo`: with `--replace-client-secrets`, `aectl` writes a
  fresh one over its vault path (see "Asgardeo clients").
  `install --reuse-secrets` overwrites nothing beyond that same exception:
  it requires
  `requiredOpenBaoPaths` and tops up, create-only, missing Thunder client
  secrets and (relay on) the relay seed. `platform update` writes only
  `aep/webhook-relay-seed`, create-only and only with
  `ae_studio.webhook_relay.enabled` (or the legacy `webhook.local_smee.enabled`
  while the new key is unset) (`cmd/webhook_relay.go`). `aep/agents-jwt-secret`, `aep/task-signing-key`,
  `aep/webhook-secret` and `aep/openbao-token` are no longer seeded or required.

## Imported clients

With `idp.clients: imported`, the admin of the platform IdP creates the
clients, and `aectl` administers nothing on the IdP. An Asgardeo organization
is an example. What differs from the sections above:

- **Secrets.** `aectl` generates no client secret and asks for no admin
  secret. The admin of the IdP creates the clients (for Asgardeo:
  `deployments/scripts/ae-asgardeo-apps.sh`) and imports five secrets under
  `aep/idp-clients/`: `aep-api-client`, `ae-studio-internal`,
  `aectl-openchoreo`, `org-publisher` and `org-studio` (`importedClientNames`,
  `cmd/platform_secrets.go`). `requireImportedClientSecrets` restores a
  secret that OpenBao lost from the cluster Secret that holds it. Then it
  stops, with the import command, for each secret that is still missing.
  `platform install` calls it before it writes its own inputs.
  `install --reuse-secrets` and `platform sync-clients` call it too.
  `platformInputPaths`, `requiredOpenBaoPaths` and `generatedClientSecrets`
  leave out the Thunder admin credentials and the Thunder client secrets.
- **Helm values.** The JWKS and token URLs are the public ones.
  `aeStudioExtraEgress` has no IdP rule, because the pod's NetworkPolicy
  already allows public HTTPS.
- **No registration.** `platform install` and `platform sync-clients` do not
  call `doThunderSetup`.
- **Environment Thunders.** `envIDPPlatformTrust` gives `envidp` no platform
  IdP to trust. Only Agent Manager uses that trust, and Agent Manager needs
  clients that `aectl` registers.
- **`sre install`** refuses (`sreIDPSupported`, `cmd/sre.go`). It uses the
  observer and RCA clients of OpenChoreo, which `platform install` registers
  only with `idp.clients: thunder`.

## Asgardeo clients

With `idp.clients: asgardeo`, `aectl` creates AE's clients in an Asgardeo
organization (`internal/asgardeo`, `cmd/asgardeo_cli.go`). The Helm values,
the environment Thunders and `sre install` are as in "Imported clients".
What differs:

- **Clients.** `asgardeoClients` gives six: four of `aepThunderClients`
  (`asgardeoPlatformClientIDs`) and the two of `orgClients`. `aectl` does
  not create the clients of OpenChoreo.
- **Admin credential.** `platform install` and `platform sync-clients` read
  it from the files of `--idp-admin-client-id-file` and
  `--idp-admin-client-secret-file` (`readIDPAdminCredential`). It is not a
  config key and is not stored. `preflightAsgardeo` gets a management token
  and looks up each client before the command changes the cluster. The
  command uses that token to its end.
  `refuseIDPAdminFlags` refuses the flags for another value of
  `idp.clients`.
- **Secrets.** `generatedClientSecrets` gives the five secrets of
  `importedClientNames` a generator (`generatedAsgardeoClientSecrets`).
  Nothing is imported.
- **Setup.** `doAsgardeoSetup` runs where `doThunderSetup` runs.
  `ensureAsgardeoClients` creates a client that the organization does not
  have, with its secret from the cluster Secret. It does not change a client
  that exists: a wrong setting stops the command.
- **A secret that does not match.** Asgardeo does not return a secret and
  does not accept one for an application that exists. `SecretWorks` asks the
  organization for a token with the secret of the vault. If the token is
  refused, the command stops. With `--replace-client-secrets`,
  `RegenerateSecret` gets a new secret from Asgardeo, and `aectl` writes it
  over the vault path. This is the only write of `aectl` over a client
  secret that the vault holds, and `install --reuse-secrets` does it too
  with the flag.
- **Pods that hold an old secret.** A pod takes a secret only when it
  starts. The pod template of each Deployment that reads a client Secret
  holds a checksum of the Secret (`clientSecretChecksumAnnotation`).
  `reconcileClientSecretReaders` runs at the end of each run. After a
  replace it syncs the cluster Secrets. Then `restartStaleSecretReaders`
  writes the checksum to each Deployment whose checksum is not the checksum
  of the Secret, in one patch for all its stale Secrets. The changed pod
  template restarts the Deployment. A Deployment that has no checksum is
  restarted once. Thus a run that stopped after the write to the vault is
  completed by the next run. A replaced secret whose Secret did not sync is
  an error. `waitForRollouts` then waits until each reader has rolled out,
  in every run, and a rollout that does not finish is an error.
  `platform sync-clients` waits for `syncClientsRolloutTimeout`, and
  `platform install` for `platformPodsReadyTimeout`.
- **GitHub reconnect.** A changed secret of one of the org's clients needs
  a GitHub reconnect, because only aep-api writes the secret that the org
  stores. `orgClientSecretWarning` says so. `reconcileClientSecretReaders`
  prints it before a step that can fail, in the run that replaced the
  secret, and in a later run that finds the Secret of the org's clients
  changed after the pods of a reader started.
