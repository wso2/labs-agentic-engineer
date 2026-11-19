# AGENTS.md — tools/aectl

AEP Control Plane CLI. Single binary (`aectl`) that installs AEP, provisions OpenBao, and manages Thunder OAuth clients.

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
(the default) or `imported`. A key in `keyRegistry`
(`internal/config/config.go`) can belong to one value of `idp.clients`
(`clients`). The four `thunder.*` keys are required for `thunder` and are
refused for `imported` (`exclusive`). `idp.issuer` is required for
`imported`, where it must be an https URL. For `thunder` it is optional: if
it is not set, the issuer is `thunder.public_url`. `idp.org_handle` is always
required.

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
- `orgThunderClients` (`cmd/thunder_cli.go`) adds the two clients of the org:
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
  `idp.orgNamespace`. When `aectl` registers the clients, the JWKS and token
  URLs are the paths of the document on `thunder.url`, the address at which
  pods reach the IdP. If not, they are the URLs that the document gives.
  `platformIDPInstallArgs` writes `idp.issuer`.
  `envidp` builds the environment Thunders' trust of the platform IdP from
  the same endpoints on `https://<host>:8443`.
- `platform install` overwrites (plain PUT) every OpenBao seed it owns: the
  `aep/idp-clients/<vaultName>` secrets,
  `aep/anthropic-api-key`, `aep/postgres-password`, `aep/webhook-relay-seed`
  (relay on or off), `aep/opensearch-{username,password}` and
  `aep/thunder-admin/client-{id,secret}` (`cmd/platform.go`, the `secrets`
  list). `install --reuse-secrets` overwrites nothing: it requires
  `requiredOpenBaoPaths` and tops up, create-only, missing Thunder client
  secrets and (relay on) the relay seed. `platform update` writes only
  `aep/webhook-relay-seed`, create-only and only with
  `ae_studio.webhook_relay.enabled` (or the legacy `webhook.local_smee.enabled`
  while the new key is unset) (`cmd/webhook_relay.go`). `aep/agents-jwt-secret`, `aep/task-signing-key`,
  `aep/webhook-secret` and `aep/openbao-token` are no longer seeded or required.

## Imported clients

With `idp.clients: imported`, the admin of the platform IdP creates the
clients, and `aectl` administers nothing on the IdP. An Asgardeo organization
is an example. `registersClients` (`cmd/platform_idp.go`) reads the setting.
What differs from the sections above:

- **Secrets.** `aectl` generates no client secret and asks for no admin
  secret. The admin of the IdP creates the clients (for Asgardeo:
  `deployments/scripts/ae-asgardeo-apps.sh`) and imports four secrets under
  `aep/idp-clients/`: `aep-api-client`, `ae-studio-internal`,
  `org-publisher` and `org-studio` (`importedClientNames`,
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
