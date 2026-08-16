# AGENTS.md — tools/aepctl

AEP Control Plane CLI. Single binary (`aep`) that installs AEP, provisions OpenBao, and manages Thunder OAuth clients.

## Commands

```bash
go build -o aep .  # build CLI
```

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands for the CLI (init, sre, uninstall) |
| `internal/openbao/` | HTTP client for OpenBao API |
| `internal/thunder/` | Thunder OAuth client registration (Job + CORS patch) |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward) |
| `internal/config/` | Viper config defaults and init |

## Config

CLI has no local config file. After `aep init` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. Sensitive values (Thunder admin secret) come from the ESO-synced
`aep-thunder-secrets` Secret. CLI flags and `AEP_*` env vars always override the ConfigMap.
