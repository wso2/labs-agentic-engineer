# AGENTS.md — components/

This tree mirrors where each deployable runs, not the language it is written in.
A new deployable goes in the folder of the place it is deployed to.

| Folder | Runs in | Holds |
|---|---|---|
| `controlplane/` | the AE control plane (namespace `wso2-aep`, chart `deployments/helm-charts/platform`) | nothing yet; `aep-api`, the console and tryit still live under `services/` and `apps/` |
| `dataplane/user-project/` | a user's Project in the org's dataplane | nothing yet; the coding agent still lives under `runners/` |
| `dataplane/ae-system-project/` | AE's own per-org Project `ae-system` in the org's dataplane | `ae-studio/` |

Every level keeps an `AGENTS.md` and a `CLAUDE.md` containing `@AGENTS.md`.
