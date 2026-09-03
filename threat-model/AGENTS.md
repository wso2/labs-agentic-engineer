# AGENTS.md — threat-model/

Google Docs import chapters for the Agentic Engineer threat model. Paste order is in `README.md`.

## Today vs intended

- **Intended** comes from the spec and researched production shape.
- **Today** comes from the live WSO2 Cloud **dev** clusters. Confirm with `kubectl` before writing a control as in place or missing.
- GitOps YAML, a ConfigMap value, and a research note are **authored config**. They are not cluster state until `kubectl` shows the object.

Kubeconfigs (local, never commit, never paste): `/Users/kajendran/Documents/wso2-kubeconfigs` (`cloud-cp.yaml`, `cloud-dp-oc-cp.yaml`, `cloud-dp-oc-dp.yaml`, `cloud-dp-oc-ci.yaml`). If kubectl hits a Cloudflare “use VPN” page, that is not “the cluster is empty.”

## Two isolation fences

Keep these separate. Do not put one in the other’s STRIDE table.

| Fence | What it splits | Where it lives |
|---|---|---|
| Login-token tenant gate | Agentic Engineer API / Postgres data by organization (`ouId` on the verified JWT) | I01 |
| OpenChoreo org namespace | Customer OpenChoreo Projects and related CRs | one `wc-*` namespace per organization on `cloud-dp-oc-cp` |

I01 is sign-in and the user API. Namespace placement of OpenChoreo Projects is not an I01 elevation-of-privilege row.

## Verified on Cloud dev (2026-09-03)

- AE API ConfigMap still sets `PLATFORM_API_NAMESPACE_OVERRIDE=admin=app-factory-user-projects,default=app-factory-user-projects`.
- Namespace `app-factory-user-projects` does **not** exist on `cloud-cp` or `cloud-dp-oc-cp`.
- OpenChoreo Projects labeled `cloud.wso2.com/product-name=app-factory` sit in the org’s `wc-*` namespace (per organization), not in a shared override namespace.
- A platform SecretReference still lists `anthropic-api-key`. That key is **not** in the API or agents-service pod env. Org design agents have no Cloud platform fallback.
- Agents-service is ClusterIP `:4000` only (no HTTPRoute). API reaches it in-cluster (`AGENTS_SVC_BASE_URL`, short service hostname, port 4000).
- Agents-service has no `ANTHROPIC_*` env. The API has `CREDENTIAL_ENCRYPTION_KEY`. Public API HTTPRoute has jwtAuth enabled; webhook jwtAuth is off.
- CiliumNetworkPolicies exist on AE pods; they have no egress FQDN/CIDR rules pinning Anthropic.

## Verified on Cloud dev (2026-09-03) — I05 coding-agent

- AE API deploy in `dp-wso2cloud-app-factory-development-*` is 1 replica Ready. ConfigMap has `AGENT_PLATFORM_URL` = public gateway host `https://development-wso2cloud.gateway.dev.cloud.wso2.com/app-factory-api-app-factory-api-endpoint` (same API HTTPRoute as I01). `AGENT_RUNNER_IMAGE` is `ghcr.io/wso2/aep/remote-worker:0.6.0-rc.16`.
- Public API ReleaseBinding `jwtAuth.enabled: true` (webhook jwtAuth off).
- On `cloud-dp-oc-cp`, `coding-agent` is a **namespaced** ComponentType (`workloadType: job`), not a ClusterComponentType. Seen in one org `wc-*` namespace (BFF-seeded). Template has `backoffLimit` max 0, `restartPolicy: Never`, no `privileged`, no pod `securityContext`.
- A retained coding-agent Component/Workload still lists secret env `ANTHROPIC_API_KEY`, `GITHUB_TOKEN`, `PUBLISHER_CLIENT_ID`, `PUBLISHER_CLIENT_SECRET` (`secretKeyRef`) and plain `AEP_PLATFORM_URL` / `AEP_MCP_URL` / `AEP_PROMPT`.
- On `cloud-dp-oc-dp`, the matching customer release namespace had no live Job or pod (TTL). An ExternalSecret for that cycle was still Ready and syncing those four secret **keys**. Do not dump secret values.
- Do not use `cloud-dp-oc-ci` for this chapter (builds are I07).

Longer kubectl checklist (gitignored learning notes): `learning/threat-modeling/threat-model-agentic-engineer/kubectl-verify.md`.
