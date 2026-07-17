# AGENTS.md — services/aepctl

AEP Control Plane CLI and management server. Two binaries in one Go module:

| Binary | Entry point | Role |
|--------|------------|------|
| `aep` | `main.go` → `cmd/` | CLI tool — installs AEP, provisions OpenBao, manages Thunder OAuth clients |
| `aep-server` | `cmd/aep-server/` | gRPC server — runs in-cluster, handles provisioning RPCs from the CLI |

## Commands

```bash
go build -o aep .                        # build CLI
go build -o aep-server ./cmd/aep-server  # build server
```

## Key packages

| Package | Purpose |
|---------|---------|
| `cmd/` | Cobra commands for the CLI (init, sre, uninstall) |
| `cmd/aep-server/` | gRPC server binary (Init, ThunderSetup, OpenbaoUnseal handlers) |
| `internal/adminpb/` | Generated gRPC stubs from `proto/admin.proto` |
| `internal/openbao/` | HTTP client for OpenBao API |
| `internal/thunder/` | Thunder OAuth client registration (Job + CORS patch) |
| `internal/bootstrap/` | Crypto helpers (RSA key gen, password gen) |
| `internal/kubernetes/` | k8s client helpers (Job runner, port-forward) |
| `internal/config/` | Viper config defaults and init |

## Proto

`proto/admin.proto` defines the `AEPAdmin` gRPC service. To regenerate stubs after editing the proto:

```bash
buf generate   # or: protoc with protoc-gen-go + protoc-gen-go-grpc
```

## Config

CLI has no local config file. After `aep init` runs, it writes non-sensitive config to the
`aep-cli-config` ConfigMap in `wso2-aep`. All subsequent commands read from it automatically
via `PersistentPreRunE`. Sensitive values (Thunder admin secret) come from the ESO-synced
`aep-thunder-secrets` Secret. CLI flags and `AEP_*` env vars always override the ConfigMap.

Server reads env vars injected by the bootstrap Helm chart.
