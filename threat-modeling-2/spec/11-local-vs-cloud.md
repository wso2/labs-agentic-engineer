# 11 Local vs WSO2 Cloud

There is one architecture. A local install runs the same components, the same containers, the same secret split and **the same token checks everywhere**. Only the rows below differ. A self-hosted OpenChoreo install follows the local column.

| Thing | WSO2 Cloud | Local |
|---|---|---|
| Secret write | SM API writes vault and creates the SecretReference. | `aep-api` writes OpenBao directly and creates the SecretReference itself. |
| SecretReference namespace | The org control-plane namespace (`wc-…`). | `default`. |
| Dataplane ingress | Public org kgateway, TLS only. | ClusterIP, or a local kgateway. Console nginx may keep the same-origin `/collab` path for the Room WebSocket. |
| GitHub webhook in | GitHub → org kgateway → `ae-studio-tools` webhook path. | smee.io → in-cluster smee client → the same `ae-studio-tools` path. |
| DP → CP | Public `aep-api` gateway. | ClusterIP, same publisher client token. |
| Project and environment | `ae-system` / `development`. Counts toward the org's `projects` quota and is visible (accepted for now). | `ae-system` / `development`. |

## smee

smee is only how a local cluster becomes reachable from GitHub. It is not a second receiver: `ae-studio-tools` checks the same HMAC on the same path in both installs. During gitpat submit, the hook is registered only after the smee client forwards to a ready `ae-studio-tools`. The smee channel string may exist earlier.

smee is a third-party public relay and is not used in production. The WSO2 Cloud threat model does not include it.

## Project name later

When the WSO2 Cloud orchestrator can create the AE dataplane Resource, the Cloud Project name may change. Local can stay `ae-system`. This spec keeps `ae-system` in both until then.
