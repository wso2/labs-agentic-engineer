# AGENTS.md — ae-studio/

The org's AE Studio: one pod per organization with one container per folder.

| Folder | Package | Container |
|---|---|---|
| `ae-design-agent/` | `@aep/ae-design-agent` | design turns |
| `ae-collab/` | `@aep/ae-collab` | the Room (Hocuspocus) |

Both still deploy as the control-plane Deployments `aep-agents` and
`collab-server` from `deployments/helm-charts/platform`; the folder says where
they are going, the chart says where they run today.
