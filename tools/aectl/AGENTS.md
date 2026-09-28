# AGENTS.md — tools/aectl

AEP Control Plane CLI. Single binary (`aectl`) that installs AEP, provisions OpenBao, and manages Thunder OAuth clients.

## Commands

```bash
go build -o aectl .  # build CLI
```

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands for the CLI (init, sre, uninstall) |
| `internal/openbao/` | HTTP client for OpenBao API |
| `internal/thunder/` | Thunder admin client (OAuth app registration over HTTP), port-forward |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward) |
| `internal/config/` | Viper config defaults and init |
| `internal/envidp/` | Installs the environment (T2) Thunder + its binding record + the environment's API Platform gateway — see deployments/design/two-tier-thunder.md. Org/env come from oc.default_org_namespace/oc.pipeline_source_environment (cmd.ocOrgNamespace/ocPipelineSourceEnvironment); no runtime dependency on Agent Manager |

## Config

CLI has no local config file. After `aectl init` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. Sensitive values (Thunder admin secret) come from the ESO-synced
`aep-thunder-secrets` Secret. CLI flags and `AEP_*` env vars always override the ConfigMap.
