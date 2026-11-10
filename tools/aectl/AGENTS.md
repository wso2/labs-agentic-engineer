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
| `internal/oidc/` | Reads the platform IdP's endpoints from its OIDC discovery document (`Discover`). `Endpoints.OnBase` puts the same paths on another address: Thunder's in-cluster Service, or the HTTPS gateway through which the environment Thunders fetch its keys |
| `internal/envidp/` | Installs the environment-tier Thunder + its binding record + the environment's API Platform gateway — see deployments/design/two-tier-thunder.md. Org/env come from oc.default_org_namespace/oc.pipeline_source_environment (cmd.ocOrgNamespace/ocPipelineSourceEnvironment); no runtime dependency on Agent Manager |

## Config

CLI has no local config file. After `aectl platform install` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. Sensitive values (Thunder admin secret) come from the ESO-synced
`aep-thunder-secrets` Secret. CLI flags and `AEP_*` env vars always override the ConfigMap.
`platform update` and `sre install` refuse to run without a valid loaded config
(`requireAEStudioConfig`, `cmd/update.go`): both re-derive and re-apply the
`aeStudio.*` values from it (`sre install` flips `sreAgent.*` through an
in-process `platform update`).

`idp.kind` selects the platform IdP: `thunder` (the default) or `asgardeo`.
A key in `keyRegistry` (`internal/config/config.go`) can belong to one kind
(`idpKind`). The four `thunder.*` keys are required for `thunder` and are not
read for `asgardeo`. `idp.issuer` and `idp.org_handle` are required for
`asgardeo` and are refused for `thunder` (`exclusive`).

## Thunder clients and seeds

- `aepThunderClients` (`cmd/thunder_cli.go`) is the canonical client list; each
  confidential client's secret is generated once, seeded in OpenBao under
  `aep/thunder-clients/<vaultName>` (`generatedThunderClientNames`,
  `cmd/platform.go`) and delivered OpenBao → ESO → Secret → env.
  `ae-studio-internal-client` (vault name `ae-studio-internal`, env
  `AE_STUDIO_INTERNAL_CLIENT_SECRET`) carries no org claims
  (`noUserAttributes`), and is `optional` (its own Secret, `aep-ae-studio-internal-secrets`; a
  missing Secret skips the client with a warning).
- `aeStudioOverrides` (`cmd/platform.go`) derives the chart's `aeStudio.*`
  values for `platform install` and `platform update`. The public scheme,
  listener and port suffix come from `tls.enabled` alone (`http`/`http`/`:19080`
  or `https`/`https`/`:19443`); `consoleOrigins` is `[console public URL,
  http://localhost:8090]`.
- `discoverPlatformIDP` (`cmd/platform_idp.go`) reads the platform IdP's
  endpoints from `<issuer>/.well-known/openid-configuration`. The issuer is
  `thunder.public_url`, or `idp.issuer` for `asgardeo`.
  `platform install`, `platform update` and `sre install` each fetch the
  document once, from the operator's machine. `platform install` fetches it
  before it changes anything in the cluster. If the document cannot be
  fetched, or names another issuer, the command stops. No installer code
  builds the platform IdP's issuer, JWKS or token URL by appending a path.
  The Thunder admin client (`internal/thunder`) and the environment Thunders
  (`internal/envidp`) still name the paths of Thunder's own API.
  `platformIDPOverrides` writes the chart values: `idp.kind` (the optional
  config key `idp.kind`; `thunder` when unset), `thunder.jwksURL` and
  `thunder.tokenURL` (the document's paths on `thunder.url`, the address at
  which pods reach the IdP), and `aeStudio.idp.{issuer,jwksUrl,tokenUrl}`.
  `envidp` builds the environment Thunders' trust of the platform IdP from
  the same endpoints on `https://<host>:8443`.
- `platform install` overwrites (plain PUT) every OpenBao seed it owns: the
  `aep/thunder-clients/<vaultName>` secrets, `aep/thunder-clients/system-client-id`,
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

## Asgardeo as the platform IdP

With `idp.kind: asgardeo` the platform IdP is an Asgardeo organization, and
`aectl` administers nothing on it. `onAsgardeo` (`cmd/platform_idp.go`)
reads the kind from config. What differs from the sections above:

- **Secrets.** `aectl` generates no client secret and asks for no admin
  secret. The organization's admin creates the clients
  (`deployments/scripts/ae-asgardeo-apps.sh`) and imports four secrets under
  `aep/thunder-clients/`: `aep-api-client`, `ae-studio-internal`,
  `org-publisher` and `org-studio` (`importedClientNames`,
  `cmd/platform_secrets.go`). `requireImportedClientSecrets` restores a
  secret that OpenBao lost from the cluster Secret that holds it. Then it
  stops, with the import command, for each secret that is still missing.
  `platform install` calls it before it writes its own inputs.
  `install --reuse-secrets` and `platform sync-clients` call it too.
  `platformInputPaths`, `requiredOpenBaoPaths` and `generatedClientSecrets`
  leave out the Thunder admin credentials, `system-client-id` and the
  Thunder client secrets.
- **Helm values.** `platformIDPOverrides` adds `idp.orgHandle` (from
  `idp.org_handle`) and `idp.orgNamespace` (from `oc.default_org_namespace`).
  The JWKS and token URLs are the public ones. `platformIDPInstallArgs` sets
  no `thunder.adminURL`. `aeStudioExtraEgress` has no IdP rule, because the
  pod's NetworkPolicy already allows public HTTPS.
- **No registration.** `platform install` and `platform sync-clients` do not
  call `doThunderSetup`.
- **Environment Thunders.** `envIDPPlatformTrust` gives `envidp` no platform
  IdP to trust. Only Agent Manager uses that trust, and Agent Manager
  installs on a Thunder platform only.
- **`sre install`** refuses (`sreIDPSupported`, `cmd/sre.go`). It registers
  OpenChoreo's observer and RCA clients on the platform IdP.
