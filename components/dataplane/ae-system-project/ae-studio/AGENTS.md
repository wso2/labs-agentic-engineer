# AGENTS.md — ae-studio/

The org's AE Studio: one pod per organization with one container per folder.

| Folder | Package | Container |
|---|---|---|
| `ae-design-agent/` | `@aep/ae-design-agent` | design turns |
| `ae-collab/` | `@aep/ae-collab` | the Room (Hocuspocus) |
| `ae-studio-tools/` | Go module `github.com/wso2/aep/ae-studio-tools` | tools (GitHub, webhooks) |

A fourth container, `webhook-relay` (the public gosmee image), runs only when the Resource's `webhookRelayUrl` is set (a local
install with `ae_studio.webhook_relay.enabled`). Its channel and exposure:
[`design/README.md`](design/README.md#webhook-relay-local-only).

The containers run only in the per-org ae-studio pod (aep-api provisions it; `skaffold/ae-studio.yaml`
builds the three images, the relay's is public); the platform chart has no Deployment for any of them.

Design notes: [`design/README.md`](design/README.md) for the ResourceType (what it renders,
env, routes, secrets, converge, local vs Cloud); [`ae-studio-tools/design/`](ae-studio-tools/design/)
for that container's route-group gates and clone storage; [`ae-design-agent/design/turn-runtime.md`](ae-design-agent/design/turn-runtime.md)
for a turn's lifecycle (start, lock, usage record, shutdown); [`ae-collab/design/room.md`](ae-collab/design/room.md)
for who may join a Room and how it saves.
