# SRE (RCA) agent on the aectl/Helm install path

Final-state notes for how the OpenChoreo SRE/RCA agent is brought up on the
Helm/`aectl` install, at parity with the local docker-compose path
(`deployments/scripts/setup-observability.sh`).

## What runs where

- **`aep-mcp-server`** — deployed by the platform chart
  (`templates/aep-mcp-server/`), always on. Stateless Streamable-HTTP MCP server
  on port 3400 wrapping aep-api's issue-create + coding-agent dispatch endpoints.
  The RCA agent calls it during the handoff (`AE_API_URL`). It forwards the
  caller's bearer to `aep-api` (`AEP_API_BASE_URL=http://aep-api:9090`), which
  enforces org-scoped auth.
- **Observability plane + RCA agent** — installed on demand by
  `aectl sre install` (the OpenChoreo `openchoreo-observability-plane` and
  `observability-logs-opensearch` charts). Not part of `aectl init`.

## `aectl sre install`

Detects the observability plane (looks for the `observer` Deployment in the obs
namespace); warns if absent, then installs/upgrades (idempotent). It then:

1. Creates the obs-namespace `SecretStore` (OpenBao) + `ExternalSecret`s:
   `rca-agent-secret`, `opensearch-admin-credentials`, `observer-secret`. All
   sourced from `secret/data/aep/*` — **no plaintext secret is handled by the
   command**.
2. `helm upgrade --install` of the two OpenChoreo charts (RCA `enabled`, image,
   model, OIDC → in-cluster Thunder, chart gateway disabled).
3. Post-helm ConfigMap wiring (`observer-config`, `rca-agent-config`) + rollout
   restarts, using in-cluster Service DNS.
4. Authz grants (`aep-observer-reader`, `rca-agent-dispatch`), the cross-namespace
   `observer-mainkgw` HTTPRoute, and the `ClusterObservabilityPlane` CR.
5. The OpenSearch index-template detect/self-heal Job.

### Prerequisite

`aectl init` must run first. It registers the `openchoreo-rca-agent` Thunder
confidential client and seeds OpenBao — including the OpenSearch admin
credentials (`aep/opensearch-username` / `-password`), which must be written
while the OpenBao root token is still held (init revokes it at the end).

### Secret model (why it works with no OpenBao role change)

The platform `SecretStore` sets no `serviceAccountRef`, so ESO authenticates to
OpenBao as its controller SA (`external-secrets/external-secrets`) — the single
SA bound to the `eso-reader` role. That SA serves ExternalSecrets in *any*
namespace, so the obs-namespace `SecretStore` reads `secret/data/aep/*` directly.
The `aep-secret-reader` policy already covers the new `aep/opensearch-*` paths.

## In-cluster vs docker-compose

| | docker-compose (setup.sh) | aectl/Helm |
|---|---|---|
| `AE_API_URL` | `http://host.k3d.internal:3401` | `http://aep-mcp-server.<ns>.svc.cluster.local:3400` |
| `AEP_API_URL` | `http://host.k3d.internal:9090` | `http://aep-api.<ns>.svc.cluster.local:9090` |
| RCA/observer/opensearch secrets | `.env` + OpenBao seed | OpenBao → ESO in the obs namespace |

## Local k3d prerequisite: `/etc/machine-id`

The upstream Fluent Bit chart mounts the node's `/etc/machine-id` (hostPath, type
File). k3d node images ship **without** it, so on k3d the `fluent-bit` DaemonSet is
stuck in `Init` with `hostPath type check failed: /etc/machine-id is not a file`,
and no logs reach OpenSearch (so log-based alerts never fire). This is a
**k3d-only** quirk — real/systemd nodes always have `/etc/machine-id`, so
production is unaffected, and we do not override the chart for it.

On k3d, create it once per cluster (survives stop/start; re-run after a
delete/recreate):

```bash
for n in $(k3d node list -o json | jq -r '.[].name' | grep server); do
  docker exec "$n" sh -c 'test -e /etc/machine-id || cat /proc/sys/kernel/random/uuid | tr -d "-" > /etc/machine-id'
done
# then let the DaemonSet retry:
kubectl -n openchoreo-observability-plane rollout restart daemonset -l app.kubernetes.io/name=fluent-bit
```

## Security / production follow-ups

This is parity with a dev-oriented setup, not a hardened production config.
Tracked follow-ups:

- **Auto-dispatch is ON by default** (`--ae-auto-dispatch=false` for issue-only).
  A fired alert can drive automated code changes; the RCA agent feeds pod logs to
  an LLM (prompt-injection surface).
- **Non-WSO2 images**: `tharindulak/openchoreo-sre-agent` and the case-insensitive
  logs-adapter. Mirror to WSO2/GHCR and pin by digest for prod.
- **No NetworkPolicies** on the chart (platform-wide gap); `aep-mcp-server:3400` is
  guarded only by aep-api JWT validation.
- **OpenSearch** is dev-sized (256M heap, no HA); no global LLM cost cap.
- Per-org Anthropic key rotation (aep-api ExternalSecret push) is not yet wired;
  the static org key from OpenBao is used.
