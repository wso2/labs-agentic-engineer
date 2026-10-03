# SRE (RCA) agent on the aectl/Helm install path

Final-state notes for how the **stock** OpenChoreo SRE/RCA agent (no AE
patch, no forked image) is brought up on the Helm/`aectl` install path.

## What runs where

- **The SRE handoff** — served by aep-api itself, at
  `/internal/v1/sre-handoff/mcp` (two MCP tools: search and create issues),
  when `sreAgent.enabled`. See
  [`services/aep-api/design/sre-handoff.md`](../../../services/aep-api/design/sre-handoff.md).
- **Observability plane + SRE agent** — installed on demand by
  `aectl sre install` (the OpenChoreo `openchoreo-observability-plane` and
  `observability-logs-opensearch` charts, OC ≥ 1.2.5). Not part of
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

### Logs adapter

The one non-stock image. aep-api files a single "error → RCA" rule per
service component and relies on the logs adapter matching it regardless of
case. The stock `observability-logs-opensearch` adapter (0.5.x and 0.6.0)
compiles a rule's `query` into a case-sensitive `wildcard: *error*`, so it
misses lines that only say `ERROR` or `Error`, which covers most structured
loggers. `--adapter-image` (default
`tharindulak/observability-logs-opensearch-adapter:0.5.1-case-insensitive`)
sets the adapter on the logs release either way: on the one aectl installs,
and on an existing plane's release at that release's chart version, with its
values kept (`helmPinLogsAdapter`). On an existing plane aectl then
restarts the plane's `controller-manager`: it reconciles an alert rule only
when the rule's spec changes, so without a restart the rules synced before
the swap keep the monitors the stock adapter compiled. If an existing plane has no
`observability-logs-opensearch` release, aectl warns and changes nothing.
Drop the pin once an upstream adapter release matches case-insensitively.

### Why a v1.3.0 image on an OpenChoreo 1.2.5 plane

AE stays on OpenChoreo 1.2.5 because Agent Manager needs OpenChoreo 1.2: its
`ClusterTrait/horizontal-pod-autoscaler` clashes with OpenChoreo 1.3.0's.
The v1.3.0 agent is the first stock image with `remediation/` extensions,
which the AE handoff needs. It runs unchanged on the 1.2.5 plane:

- the `sre-agent` chart templates and `rca.*` values are the same in the
  1.2.5 and 1.3.0 observability-plane charts, so only the image is
  overridden;
- OpenChoreo 1.2.5's MCP server serves every tool the agent calls, with the
  same names, arguments and response shapes;
- the one gap is closed by aectl ([`rca-agent` role](#rca-agent-role)).

## `aectl sre install`

Requires OC ≥ 1.2.5, with no upper bound (`minOCVersion`, skippable with
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
2. Writes the **agent's Secret** `sre-agent-aep` in the obs namespace:
   `RCA_LLM_API_KEY`, `RCA_MODEL_NAME` (`openai:<model>`) and
   `RCA_LLM_BASE_URL` from `--llm-api-key-file`/`--llm-model`/`--llm-base-url`
   (probed first, see [SRE model](#sre-model)), and `AEP_MCP_TOKEN`, the
   handoff key. The key is generated once (32 random bytes) and reused on a
   re-run unless `--rotate-handoff-token`; aectl writes it into aep-api's
   Secret `sre-handoff` in the AE namespace first.
3. `helm upgrade --install` of the two OpenChoreo charts, with the agent's
   `rca.extraEnvs` carrying `EXTENSIONS_DIR`, `AEP_MCP_URL`,
   `SSL_CERT_FILE=/opt/aep/ca/ca-bundle.crt`, and the four
   `secretKeyRef`s onto `sre-agent-aep`, so the model, key and handoff key
   arrive at the agent only through that one Secret. Without a model the
   agent is held at 0 replicas; with one, at 1, restarted whenever the
   Secret changed.
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
5. The platform release's `sreAgent.*` (`enabled`, `tokenSecret`,
   `tokenHash`, `mcpHostname`), through an in-process `aectl platform
   update`, and removal of what earlier versions installed for aep-api to
   push the model (the `aep-api-sre-push` Role and RoleBinding, the
   `sre-model-seed` Secret).
6. Post-Helm ConfigMap wiring (`observer-config`, `rca-agent-config`) +
   a rollout restart so the agent picks up refreshed remediation assets.
7. Authz grants (`aep-observer-reader`, `rca-agent-dispatch`), the two
   [`rca-agent` role](#rca-agent-role) actions where missing, the
   cross-namespace `observer-mainkgw` HTTPRoute, and the
   `ClusterObservabilityPlane` CR.
8. The OpenSearch index-template detect/self-heal Job.

### SRE model

Three flags: `--llm-api-key-file` (path; the key is read from the file,
trimmed, and never taken as a flag value or logged), `--llm-model` (e.g.
`gpt-5.4`), and `--llm-base-url` (default `https://api.openai.com/v1`, https
only). The first two go together; one without the other is an error.

When given, aectl calls `<base URL>/models` with the key, without following
redirects, and stops before writing anything if the provider rejects it or
cannot be reached. A run without them keeps the model `sre-agent-aep` already
holds; a first install without them leaves the agent at 0 replicas.

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
paths. The `sre-agent-aep` and `sre-handoff` Secrets are separate from all
of this: aectl writes them over the Kubernetes API from its own flags and the
key it generates, never through ESO.

## Reaching aep-api's handoff over https

The agent's `AEP_MCP_URL` (from `rca.extraEnvs`) is the platform
control-plane gateway's https route:
`https://<sreAgent.mcpHostname>[:port]/internal/v1/sre-handoff/mcp` (default
host `aep-mcp.openchoreo.localhost:8443` in dev). The platform chart's
`aep-api-sre-handoff` `HTTPRoute` (`sectionName https`) matches exactly that
path and sends it to aep-api's Service. `Authorization: Bearer
${AEP_MCP_TOKEN}` travels in `remediation/mcp.json`'s `headers`; the
extension loader refuses to send headers to a plaintext URL, which is why
this route must be https. See
[`sre-handoff-security.md`](../../../docs/developer-guide/sre-handoff-security.md).

The dev control-plane gateway (`deployments/scripts/setup-env-for-aectl.sh`)
enables an `https` listener (`gateway.tls.enabled=true`, hostname pattern
`*.openchoreo.localhost`, port 8443) with a certificate issued by the
chart's own `cluster-gateway-selfsigned-issuer` (CA-backed off
`cluster-gateway-ca`, reused rather than standing up a second CA).

## In-cluster addressing

| | value |
|---|---|
| `AEP_MCP_URL` | `https://<sreAgent.mcpHostname>[:port]/internal/v1/sre-handoff/mcp` (gateway route) |
| `SSL_CERT_FILE` | `/opt/aep/ca/ca-bundle.crt` (built in-pod, see above) |
| RCA/observer/opensearch secrets | OpenBao → ESO in the obs namespace |
| SRE agent's model/key/handoff key | `sre-agent-aep`, written by aectl |

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

## `rca-agent` role

The v1.3.0 agent's `get_resource`, `list_resource_release_bindings` and
`get_resource_release_binding` tools are filtered out of its catalog unless
the control plane's `rca-agent` ClusterAuthzRole holds `resource:view` and
`resourcereleasebinding:view`. OpenChoreo 1.3.0's chart grants both; 1.2.5's
does not. The role is a list item in the control-plane chart's
`bootstrap.roles` values, which Helm cannot merge into, so `aectl sre
install` adds whichever are missing with a JSON patch
(`tools/aectl/cmd/sre_role.go`). A role that already has them is left as it
is.

If aectl cannot patch the role (no RBAC for `clusterauthzroles`, or no
`rca-agent` role), it warns with the `kubectl patch` to run by hand, and the
install continues: the agent still completes RCA and remediation, without
visibility into Resources and ResourceReleaseBindings. `aectl sre uninstall`
leaves the role alone, since aectl cannot tell the actions it added from the
ones the chart ships.

## Moving to OpenChoreo 1.3.0 later

Once Agent Manager supports OpenChoreo 1.3.0:

- install the matching 1.3.0 observability-plane chart
  (`--obs-plane-version`). Keep the digest-pinned `--rca-image-tag`: the
  chart's own default is the bare `v1.3.0` tag (its `appVersion`), which
  isn't pinned to one build;
- the `rca-agent` step then finds nothing to add, since 1.3.0's chart grants
  both actions;
- revisit `minOCVersion` and the dev-env pins (`OC_BRANCH`, `OC_VERSION` in
  `deployments/scripts/setup-env-for-aectl.sh`).

## Security / production follow-ups

This is parity with a dev-oriented setup, not a hardened production config.
Tracked follow-ups:

- **Auto-dispatch is not aectl's to gate any more.** The stock agent's
  remediation handoff always calls `ae_search_related_issues` +
  `ae_create_issue`; AE owns whether that issue is adopted for automated
  code changes (see the handoff runbook's Issue outcomes).
- **No NetworkPolicies** on the chart (platform-wide gap). Inside the
  cluster, aep-api's handoff mount is guarded only by the handoff key.
- **The logs adapter image is personal** (`tharindulak/...`, see
  [Logs adapter](#logs-adapter)). Mirror it to WSO2/GHCR and pin by digest
  for production, until the case-insensitive match ships upstream
  (`openchoreo/community-modules`).
- **OpenSearch** is dev-sized (256M heap, no HA); no global LLM cost cap.
  One alert measured about 290k tokens on `gpt-5.4` in the proof.
- **The sqlite report store is a single point of failure.** The
  Deployment's rollout strategy is `Recreate` (a `ReadWriteOnce` PVC backs
  it), so every restart that picks up a changed Secret tears the running
  agent down before the replacement starts.
- **The org is the agent's claim, checked against the observer.** One agent
  serves every org on its plane; each handoff call names its org and aep-api
  verifies it against the observer's recent alerts (see
  [`sre-handoff.md`](../../../services/aep-api/design/sre-handoff.md)). A
  WSO2 Cloud `wc-…` namespace does not map to an org handle, so the handoff
  fails closed there.
- `aectl sre uninstall` deletes the whole observability namespace even
  when `aectl` only adopted an existing plane (`sre_uninstall.go`) — out
  of scope here, flagged for a follow-up.
