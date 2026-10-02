# AGENTS.md — ae-studio/

The org's AE Studio: one pod per organization with one container per folder.

| Folder | Package | Container |
|---|---|---|
| `ae-design-agent/` | `@aep/ae-design-agent` | design turns |
| `ae-collab/` | `@aep/ae-collab` | the Room (Hocuspocus) |
| `ae-studio-tools/` | Go module `github.com/wso2/aep/ae-studio-tools` | tools (GitHub, webhooks) |

`ae-design-agent` still deploys as the control-plane Deployment `aep-agents`
from `deployments/helm-charts/platform`; the folder says where it is going, the
chart says where it runs today. `ae-collab` runs only in the pod: the chart's
`collab-server` Deployment still renders but no longer boots (its legacy env
is gone; Task 2.16 removes it). `ae-studio-tools` has no chart Deployment.
