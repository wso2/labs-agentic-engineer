# AGENTS.md — ae-studio/

The org's AE Studio: one pod per organization with one container per folder.

| Folder | Package | Container |
|---|---|---|
| `ae-design-agent/` | `@aep/ae-design-agent` | design turns |
| `ae-collab/` | `@aep/ae-collab` | the Room (Hocuspocus) |
| `ae-studio-tools/` | Go module `github.com/wso2/aep/ae-studio-tools` | tools (GitHub, webhooks) |

All three run only in the per-org ae-studio pod (aep-api provisions it; images
built by `skaffold/ae-studio.yaml`); the platform chart has no Deployment for
any of them. `ae-studio-tools` has no chart Deployment.
