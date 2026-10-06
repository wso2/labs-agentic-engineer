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
| `internal/envidp/` | Installs the environment-tier Thunder + its binding record + the environment's API Platform gateway — see deployments/design/two-tier-thunder.md. Org/env come from oc.default_org_namespace/oc.pipeline_source_environment (cmd.ocOrgNamespace/ocPipelineSourceEnvironment); no runtime dependency on Agent Manager |

## Config

CLI has no local config file. After `aectl platform install` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. Sensitive values (Thunder admin secret) come from the ESO-synced
`aep-thunder-secrets` Secret. CLI flags and `AEP_*` env vars always override the ConfigMap.

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
