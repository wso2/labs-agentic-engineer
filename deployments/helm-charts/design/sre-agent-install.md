# SRE (RCA) agent on the aectl/Helm install path

Final-state notes for how the **stock** OpenChoreo SRE/RCA agent (no AE
patch, no forked image) is brought up on the Helm/`aectl` install path.

## What runs where

- **`aep-mcp-server`** — deployed by the platform chart
  (`templates/aep-mcp-server/`), always on. Stateless Streamable-HTTP MCP
  server on port 3400 wrapping aep-api's issue-create + coding-agent dispatch
  endpoints. It forwards the caller's `Authorization` header to `aep-api`
  unchanged and holds no credential of its own.
- **Observability plane + SRE agent** — installed on demand by
  `aectl sre install` (the OpenChoreo `openchoreo-observability-plane` and
  `observability-logs-opensearch` charts, OC ≥ 1.3.0). Not part of
  `aectl init`/`aectl platform install`.

## Image

The stock, unmodified upstream image, pinned by digest so both architectures
resolve the same build (`tools/aectl/cmd/sre.go`, `--rca-image-repo` /
`--rca-image-tag`):

```
ghcr.io/openchoreo/sre-agent:v1.3.0@sha256:25e5e6c423d049460f4a60d95497747c2a99b02713faf8523a38ef8e6a85c599
```

No AE-forked image, no `RCA_LLM_API_KEY_FILE` credential file: the agent
takes its model, key and MCP token as plain environment variables, sourced
from a Secret (below).

## `aectl sre install --org <org>`

Requires OC ≥ 1.3.0 (`minOCVersion`, skippable with
`--skip-oc-version-check`) and pinning the platform chart
(`--platform-chart` or `--platform-version` — required, so this command can
never silently upgrade the platform release to whatever is latest on GHCR).
Detects the observability plane (looks for the `sre-agent` Deployment, or
`ai-rca-agent` on older chart versions, in the obs namespace); warns if
absent, then installs/upgrades (idempotent). It then:

1. Creates the obs-namespace `SecretStore` (OpenBao) + `ExternalSecret`s:
   `rca-agent-secret`, `opensearch-admin-credentials`, `observer-secret`.
   All sourced from `secret/data/aep/*` — **no plaintext secret is handled
   by the command**. Every `ExternalSecret` aectl renders carries a
   per-run `force-sync` annotation, so a re-run's apply always changes the
   spec and ESO syncs immediately rather than waiting out its
   `refreshInterval`.
2. Creates the **AE-owned Secret** `sre-agent-aep` in the obs namespace,
   create-only (never overwrites an existing one — a re-run must not
   clobber what aep-api's reconciler already pushed), with four empty
   placeholder keys: `RCA_LLM_API_KEY`, `RCA_MODEL_NAME`, `RCA_LLM_BASE_URL`,
   `AEP_MCP_TOKEN`. Applies the namespaced `Role`
   `aep-api-sre-push` + `RoleBinding` that let aep-api's `aep-api`
   ServiceAccount get/update/patch that one Secret, get/patch the discovered
   Deployment and its `/scale` subresource, and list pods.
3. `helm upgrade --install` of the two OpenChoreo charts, with the agent's
   `rca.extraEnvs` carrying `EXTENSIONS_DIR`, `AEP_MCP_URL`,
   `SSL_CERT_FILE=/opt/aep/ca/ca-bundle.crt`, and the four
   `secretKeyRef`s onto `sre-agent-aep` — so the model, key and MCP token
   arrive at the agent only through that one Secret, which aep-api's
   `internal/sreagent.Reconciler` keeps converged with the org's resolved
   [SRE model connection](../../../services/aep-api/design/sre-model-connection.md)
   (60-second tick, plus an immediate kick on every relevant Console save).
   The reconciler scales the Deployment to 0 while unconfigured and back to
   1 once a connection resolves.
4. **A Helm post-renderer** (`aectl sre post-render`, a hidden subcommand;
   wired as a Helm-4 plugin when `helm version` is ≥ 4, else the classic
   `--post-renderer <exe> --post-renderer-args ...` form) surgically edits
   the rendered `sre-agent` Deployment's pod spec — touching only
   `volumes`, `initContainers` and the agent container's `volumeMounts`, so
   every chart-authored field the transform doesn't know about passes
   through unchanged:
   - mounts ConfigMap `sre-agent-extensions` (optional — Helm renders the
     agent before this ConfigMap exists on a fresh install) at
     `/opt/aep/sre-agent-extensions`, populating only `remediation/`
     (`mcp.json`, `CONTEXT.md`, the coding-agent-handoff `SKILL.md`).
   - adds an **initContainer**, same image as the agent, that builds
     `/opt/aep/ca/ca-bundle.crt` by concatenating the image's own
     `/etc/ssl/certs/ca-certificates.crt` with the `cluster-gateway-ca`
     ConfigMap's `ca.crt` (required — every agent TLS call reads
     `SSL_CERT_FILE` from this path) into an `emptyDir`, mounted read-only
     on the agent. aectl cannot read the image's system bundle to bake a
     static copy — a copy would also go stale on cluster-CA rotation — so
     the bundle is built fresh in-pod on every start.
5. Post-Helm ConfigMap wiring (`observer-config`, `rca-agent-config`) +
   a rollout restart so the agent picks up refreshed remediation assets.
6. Authz grants (`aep-observer-reader`, `rca-agent-dispatch`), the
   cross-namespace `observer-mainkgw` HTTPRoute, and the
   `ClusterObservabilityPlane` CR.
7. The OpenSearch index-template detect/self-heal Job.

### Install-time SRE model seed (opt-in)

Three flags, orthogonal to everything above: `--llm-api-key-file` (path; the
key is read from the file, trimmed, and never taken as a flag value or
logged), `--llm-model` (e.g. `gpt-5.4`), and `--llm-base-url` (default
`https://api.openai.com/v1`). `--llm-api-key-file` and `--llm-model` must be
given together — one without the other is an error; neither is unchanged
behaviour (no seed, no `sreAgent.seed.secretName`).

When given, aectl create-or-updates a Secret `sre-model-seed` in the **AE
namespace** (`apiKey`/`model`/`baseURL` keys), ahead of the internal `aectl
platform update` that flips `sreAgent.*` on the platform release, and adds
`--set sreAgent.seed.secretName=sre-model-seed` to that update — so the
Secret always exists by the time aep-api could read it. A run without the
flags leaves `sreAgent.seed.secretName` alone: it never clears an
already-seeded org's value. See
[`sre-model-connection.md`](../../../services/aep-api/design/sre-model-connection.md#install-time-seed)
for what aep-api does with the seed.

### Prerequisite

`aectl init` must run first. It registers the `openchoreo-rca-agent` Thunder
confidential client and seeds OpenBao — including the OpenSearch admin
credentials (`aep/opensearch-username` / `-password`), which must be written
while the OpenBao root token is still held (init revokes it at the end).

### Secret model (why it works with no OpenBao role change)

The platform `SecretStore` sets no `serviceAccountRef`, so ESO authenticates
to OpenBao as its controller SA (`external-secrets/external-secrets`) — the
single SA bound to the `eso-reader` role. That SA serves ExternalSecrets in
*any* namespace, so the obs-namespace `SecretStore` reads `secret/data/aep/*`
directly. The `aep-secret-reader` policy already covers the `aep/opensearch-*`
paths. The AE-owned `sre-agent-aep` Secret is separate from all of this: it
holds no OpenBao-sourced material at create time (aectl creates it empty),
and its four keys are populated only by aep-api's reconciler, over the
Kubernetes API, using the namespaced Role above — never through ESO.

## Reaching aep-mcp-server over https

The agent's `AEP_MCP_URL` (from `rca.extraEnvs`) is the platform
control-plane gateway's https route:
`https://<sreAgent.mcpHostname>[:port]/mcp` (default
`aep-mcp.openchoreo.localhost:8443` in dev). An `HTTPRoute` (`sectionName
https`) plus a `ReferenceGrant` publish `aep-mcp-server` on that listener; a
`NetworkPolicy` admits only pods matching the gateway's own label
(`gateway.networking.k8s.io/gateway-name: gateway-default`) in
`openchoreo-control-plane` — the agent never reaches the Service directly.
`Authorization: Bearer ${AEP_MCP_TOKEN}` (the org-bound token aep-api mints
and pushes) travels in `remediation/mcp.json`'s `headers`; the extension
loader refuses to send headers to a plaintext URL, which is why this route
must be https. See
[`sre-handoff-security.md`](../../../docs/developer-guide/sre-handoff-security.md).

The dev control-plane gateway (`deployments/scripts/setup-env-for-aectl.sh`)
enables an `https` listener (`gateway.tls.enabled=true`, hostname pattern
`*.openchoreo.localhost`, port 8443) with a certificate issued by the
chart's own `cluster-gateway-selfsigned-issuer` (CA-backed off
`cluster-gateway-ca`, reused rather than standing up a second CA).

## In-cluster addressing

| | value |
|---|---|
| `AEP_MCP_URL` | `https://<sreAgent.mcpHostname>[:port]/mcp` (gateway route) |
| `SSL_CERT_FILE` | `/opt/aep/ca/ca-bundle.crt` (built in-pod, see above) |
| RCA/observer/opensearch secrets | OpenBao → ESO in the obs namespace |
| SRE agent's model/key/token | AE-owned Secret `sre-agent-aep`, pushed by aep-api |

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

- **Auto-dispatch is not aectl's to gate any more.** The stock agent's
  remediation handoff always calls `ae_search_related_issues` +
  `ae_create_issue`; AE owns whether that issue is adopted for automated
  code changes (see the handoff runbook's Issue outcomes).
- **NetworkPolicies are partial** (platform-wide gap). The only one is
  `templates/aep-mcp-server/networkpolicy.yaml`, rendered with
  `sreAgent.enabled`: it admits only `gateway-default`'s proxy pods. With
  the SRE agent off, `aep-mcp-server:3400` is guarded only by aep-api JWT
  validation.
- **OpenSearch** is dev-sized (256M heap, no HA); no global LLM cost cap.
  One alert measured about 290k tokens on `gpt-5.4` in the proof.
- **The sqlite report store is a single point of failure.** See
  [`sre-model-connection.md`](../../../services/aep-api/design/sre-model-connection.md)
  — the Deployment's rollout strategy is `Recreate` (a `ReadWriteOnce` PVC
  backs it), so any push that changes the agent's Secret, including a bad
  one, tears the running agent down before the replacement starts.
- Per-org SRE model connection rotation is AE-owned; the agent consumes
  only the four env values aep-api pushes into `sre-agent-aep`.
- `aectl sre uninstall` deletes the whole observability namespace even
  when `aectl` only adopted an existing plane (`sre_uninstall.go`) — out
  of scope here, flagged for a follow-up.
