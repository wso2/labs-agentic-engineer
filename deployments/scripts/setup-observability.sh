#!/bin/bash
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# scripts/setup-observability.sh — installs the OpenChoreo Observability
# Plane (minimum profile: Observer + cluster-agent + OpenSearch + Fluent Bit).
# Required for the in-UI Live Progress panel to stream coding-agent + build
# logs. Without this, the BFF's /progress/agent endpoint returns 503 and the
# UI falls back to "Live progress unavailable — falling back to status polling".
#
# Also wires the SRE (RCA) agent alert→RCA→AEP-handoff pipeline (see
# docs/developer-guide/sre-handoff-runbook.md): logs-adapter (alert-rule
# evaluation), observer auto-trigger config, and the RCA agent's AE_HANDOFF
# env so code-level RCA findings become GitHub issues + coding-agent runs.
#
# Idempotent: re-running is safe — helm install is gated by helm_install_if_not_exists,
# kubectl apply is server-side, and ExternalSecrets / CRs are upserts.
#
# setup.sh runs this stage unconditionally — Agent Manager's charts install
# against the plane — and then PARKS its heavy workloads (OpenSearch, Fluent
# Bit, the RCA agent, the adapters) at zero replicas, because this is the
# heaviest install and most local work never reads it. Live Progress's log
# archive and the alert→RCA handoff need it running:
# `bash scripts/park-observability.sh up`. start.sh recognises the parked state
# (see its stage 7c).
#
# Wiring summary:
#   - Helm: openchoreo-observability-plane @ v1.0.1-hotfix.1
#       Observer + cluster-agent + RCA. Creates HTTPRoute on its OWN
#       gateway (openchoreo-observability-plane/gateway-default, port 11080).
#   - Helm: observability-logs-opensearch @ v0.5.1
#       OpenSearch (storage) + Fluent Bit (DaemonSet, log shipper) +
#       logs-adapter (materialises ObservabilityAlertRules as OpenSearch
#       alerting monitors — 0.3.x had NO adapter, so log alert rules synced
#       but were never evaluated).
#   - ConfigMap patches (post-helm): observer-config auto-trigger keys
#       (LOGS_ADAPTER_ENABLED / RCA_SERVICE_URL / ALERT_SUPPRESSION_WINDOW)
#       and rca-agent-config AEP-handoff keys (AE_HANDOFF / AE_AUTO_DISPATCH /
#       AE_API_URL). Patched after helm so chart upgrades can't silently
#       drop them on re-runs.
#   - Cross-namespace HTTPRoute on the MAIN kgateway
#       (openchoreo-control-plane/gateway-default) for observer.openchoreo.localhost
#       so the BFF in docker-compose can reach the Observer via the same
#       k3d-openchoreo-serverlb:8080 it uses for everything else (the
#       obs-plane's own port-11080 gateway isn't exposed by k3d serverlb).
#   - ExternalSecret: opensearch-admin-credentials, observer-secret
#       Pull username/password/OAuth-client-secret from OpenBao
#       (seeded by single-cluster/values-openbao.yaml postStart hook).
#   - CR: ClusterObservabilityPlane/default registers this plane with the CP.
#   - Job: opensearch-bootstrap-templates — detection + self-heal ONLY. The
#       0.5.x chart's own setup hook owns the container-logs index template
#       (log: wildcard, pod_name + openchoreo_dev/* labels: keyword — the
#       exact mappings the Observer's queries and the logs-adapter's alert
#       monitors depend on). This job verifies the template and deletes any
#       index created under a wrong/older mapping (e.g. the Fluent Bit
#       first-write race) so it's recreated correctly. It must NOT put its
#       own template: a same-name template REPLACES the chart's, and a
#       log:text mapping silently breaks every log-based alert (wildcard
#       patterns then match analysed lowercase tokens — "ERROR" never matches).
#
# Knobs (env):
#   RCA_IMAGE_TAG   SRE-agent image tag to import/run
#                   (default: v1.0.1-hotfix.1-anthropic). This hotfix contains
#                   the Anthropic RCA fix and the OC SRE extension loader used by
#                   the AE remediation handoff.
#   AE_HANDOFF      enable the RCA→AEP coding-agent handoff (default: true)
#   AE_AUTO_DISPATCH auto-dispatch the coding agent after issue creation
#                   (default: true; false = issue-only, human dispatches)
#   AE_PUBLISH_REPORTS publish each completed RCA report to aep-api so it
#                   shows in the console Alerts bell/list (default: true;
#                   supported by the hotfix SRE image. Needs AEP_API_URL.
#   AEP_API_URL     aep-api REST base for report publishing
#                   (default: http://host.k3d.internal:9090). NOT AE_API_URL
#                   (that is the MCP server on :3401).
#   AEP_MCP_TOKEN   local bearer configured on aep-mcp-server as
#                   AEP_MCP_DEFAULT_BEARER (random per install, generated into
#                   deployments/.env by setup-aep.sh). The SRE extension does
#                   not send it as an HTTP header because the extension loader
#                   rejects credentials over plaintext local URLs.
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

# The observability plane tracks the OpenChoreo version (env.sh) — the two are
# released together and the observer speaks the control plane's API.
OBS_PLANE_VERSION="${OPENCHOREO_VERSION}"
# >= 0.5.1 REQUIRED for the logs-adapter (alert-rule evaluation engine).
# 0.3.x ships no adapter: ObservabilityAlertRules sync as Ready but are
# never evaluated — no alert ever fires, silently.
# Bumped 0.5.1 -> 0.5.3 (OBSERVABILITY_LOGS_VERSION in env.sh) for OC 1.2.0.
OBS_LOGS_VERSION="${OBSERVABILITY_LOGS_VERSION}"
# Adapter image override. Set to "" to run the chart's stock adapter image.
# See the adapter block in step 3 for why AEP carries a fork.
OBS_LOGS_ADAPTER_IMAGE_REPO="${OBS_LOGS_ADAPTER_IMAGE_REPO:-docker.io/tharindulak/observability-logs-opensearch-adapter}"
OBS_LOGS_ADAPTER_IMAGE_TAG="${OBS_LOGS_ADAPTER_IMAGE_TAG:-0.5.1-case-insensitive}"
NS="openchoreo-observability-plane"
# The SRE/RCA agent's Deployment, Service and container were all renamed
# ai-rca-agent -> sre-agent in observability-plane 1.2.0. Named once here so the
# next rename is a one-line change rather than a scavenger hunt through the
# eight places that address it.
RCA_DEPLOYMENT="sre-agent"

# SRE-agent handoff knobs (see header). AE_API_URL is how the in-cluster RCA
# agent reaches the docker-compose-hosted aep-mcp-server on the host.
AE_HANDOFF="${AE_HANDOFF:-true}"
AE_AUTO_DISPATCH="${AE_AUTO_DISPATCH:-true}"
AE_API_URL="${AE_API_URL:-http://host.k3d.internal:3401}"
AEP_MCP_URL="${AEP_MCP_URL:-${AE_API_URL}/mcp}"
case "$AEP_MCP_URL" in
    http://*|https://*) ;;
    *) AEP_MCP_URL="http://${AEP_MCP_URL}" ;;
esac
# Report publishing: POST each completed RCA report to aep-api
# so it surfaces in the console Alerts bell/list. AEP_API_URL is aep-api's REST
# base — DISTINCT from AE_API_URL (the MCP server on :3401); reports go to the
# HTTP API on :9090.
AE_PUBLISH_REPORTS="${AE_PUBLISH_REPORTS:-true}"
AEP_API_URL="${AEP_API_URL:-http://host.k3d.internal:9090}"

echo "=== Installing OpenChoreo Observability Plane ==="

kubectl cluster-info --context $CLUSTER_CONTEXT &>/dev/null || {
    echo "❌ Cluster '$CLUSTER_CONTEXT' not running. Run: ./setup-k3d.sh && ./setup-prerequisites.sh && ./setup-openchoreo.sh"
    exit 1
}

# ── 1. Namespace + ExternalSecrets (pulled from OpenBao) ─────────────────
echo ""
echo "1️⃣  Namespace + ExternalSecrets"
kubectl --context "$CLUSTER_CONTEXT" create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
# The observability-plane chart mounts the cluster-gateway-ca ConfigMap into
# its cluster-agent pod but does not create it (same as the DP/WF charts).
# Without this the cluster-agent pod sits in ContainerCreating forever with
# `MountVolume.SetUp failed for volume "server-ca" : configmap "cluster-gateway-ca" not found`.
# Mirrors create_plane_cert_resources calls for openchoreo-{data,workflow}-plane
# in setup-openchoreo.sh.
create_plane_cert_resources "$NS"
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: opensearch-admin-credentials
  namespace: $NS
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: opensearch-admin-credentials
  data:
    - secretKey: username
      remoteRef: { key: opensearch-username, property: value }
    - secretKey: password
      remoteRef: { key: opensearch-password, property: value }
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: observer-secret
  namespace: $NS
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: observer-secret
  data:
    - secretKey: OPENSEARCH_USERNAME
      remoteRef: { key: opensearch-username, property: value }
    - secretKey: OPENSEARCH_PASSWORD
      remoteRef: { key: opensearch-password, property: value }
    - secretKey: UID_RESOLVER_OAUTH_CLIENT_SECRET
      remoteRef: { key: observer-oauth-client-secret, property: value }
EOF
echo "✅ ExternalSecrets applied"

# ── 1b. RCA (SRE) agent image + secret ───────────────────────────────────
# The RCA agent runs the published Anthropic hotfix image. It carries the OC SRE
# remediation extension loader; AE owns the extension content and mounts it in
# step 3d.
# The agent reads its LLM key + OAuth client secret from the rca-agent-secret
# Secret (envFrom). RCA_LLM_API_KEY comes from ANTHROPIC_API_KEY in deployments/.env;
# OAUTH_CLIENT_SECRET must equal the openchoreo-rca-agent client secret registered
# by the IdP bootstrap (single-cluster/thunder-resources/86-openchoreo-rca-agent.yaml).
echo ""
echo "1️⃣b RCA agent image + secret"
# Resolution order:
#   1. local image            ${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}
#   2. registry pull          ${RCA_IMAGE_PULL} (Docker Hub mirror)
#
# RCA_IMAGE_REPO is the fully qualified image name (default:
# tharindulak/sre-agent),
# not a short local alias — deliberately. An earlier version used a short repo
# name here and retagged the pulled image to it before `k3d image import`; the
# Deployment then referenced that short, unqualified name. That worked right
# after import, but k3d/containerd's image GC can evict it later — and because
# the reference had no registry/namespace, kubelet's re-pull attempt resolved
# to docker.io/library/<name> (Docker Hub's default namespace for official
# images) instead of our actual image, and failed outright
# (ImagePullBackOff: "pull access denied, repository does not exist"). Using
# the fully-qualified name everywhere means a cache-evicted image can always
# be re-pulled from the real registry — no more silent long-term fragility.
RCA_IMAGE_REPO="${RCA_IMAGE_REPO:-tharindulak/sre-agent}"
RCA_IMAGE_TAG="${RCA_IMAGE_TAG:-v1.0.1-hotfix.1-anthropic}"
RCA_IMAGE_PULL="${RCA_IMAGE_PULL:-tharindulak/sre-agent:v1.0.1-hotfix.1-anthropic}"
if ! docker image inspect "${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}" >/dev/null 2>&1; then
    echo "   ${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG} not built locally — trying registry ${RCA_IMAGE_PULL}..."
    if docker pull "$RCA_IMAGE_PULL" >/dev/null 2>&1; then
        docker tag "$RCA_IMAGE_PULL" "${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}"
        echo "✅ pulled ${RCA_IMAGE_PULL} → retagged as ${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}"
    fi
fi
RCA_IMAGE="${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}"
# k3d image import is known to flake transiently ("short read ... unexpected
# EOF" while ingesting a layer blob — truncated docker→node tar stream), and
# some k3d versions exit 0 anyway. So: import, VERIFY the image is really in
# the node's containerd, retry once, and if it still isn't there fall back to
# registry-direct (helm values point at $RCA_IMAGE_PULL and the node pulls
# from Docker Hub itself — possible since the image is published multi-arch).
_rca_image_in_node() {
    # imported local images land as docker.io/library/<repo>:<tag> — substring
    # match on repo:tag covers both that and registry-form names
    docker exec "k3d-${CLUSTER_NAME}-server-0" \
        ctr -n k8s.io images ls -q 2>/dev/null | grep -q "${RCA_IMAGE_REPO}:${RCA_IMAGE_TAG}"
}
if docker image inspect "$RCA_IMAGE" >/dev/null 2>&1; then
    IMPORTED=""
    for attempt in 1 2; do
        k3d image import "$RCA_IMAGE" -c "$CLUSTER_NAME" || true
        if _rca_image_in_node; then IMPORTED=yes; break; fi
        echo "⚠️  import attempt ${attempt} did not land in the node (transient k3d flake) — retrying..."
    done
    if [ -n "$IMPORTED" ]; then
        # The patched tag is built locally and exists in no registry, so an image-GC
        # eviction would be unrecoverable — pin it (see pin_node_image in utils.sh).
        # It also re-verifies on EVERY server/agent node, where _rca_image_in_node
        # above only checks server-0: exit 2 means the import landed on some nodes
        # but not all, so fall through to the registry-direct path rather than
        # deploying a pod that cannot start wherever the image is missing.
        PIN_RC=0
        pin_node_image "$RCA_IMAGE" || PIN_RC=$?
        if [ "$PIN_RC" = "2" ]; then
            IMPORTED=""
        else
            echo "✅ imported $RCA_IMAGE into k3d-$CLUSTER_NAME (verified in node containerd)"
        fi
    fi
    if [ -z "$IMPORTED" ]; then
        echo "⚠️  k3d import did not land on every node — switching to registry-direct:"
        echo "    the cluster will pull ${RCA_IMAGE_PULL} from Docker Hub instead."
        RCA_IMAGE_REPO="${RCA_IMAGE_PULL%%:*}"
        RCA_IMAGE_TAG="${RCA_IMAGE_PULL##*:}"
    fi
else
    echo "⚠️  $RCA_IMAGE not found locally and registry pull failed — build it"
    echo "    (docker build -t $RCA_IMAGE <openchoreo>/agents/sre-agent) or the RCA pod"
    echo "    will stay ImagePullBackOff. Continuing; other obs-plane components are unaffected."
fi
ANTHROPIC_API_KEY="$(grep -E '^ANTHROPIC_API_KEY=' "$SCRIPT_DIR/../.env" 2>/dev/null | head -1 | cut -d= -f2-)"
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "ℹ️  ANTHROPIC_API_KEY not set in deployments/.env — static RCA fallback is empty."
    echo "   This is OK when the org key is configured in the AE Console; the required"
    echo "   rca-agent-anthropic-secret projection below is the runtime source."
fi
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" create secret generic rca-agent-secret \
    --from-literal=RCA_LLM_API_KEY="$ANTHROPIC_API_KEY" \
    --from-literal=OAUTH_CLIENT_SECRET="openchoreo-rca-agent-secret" \
    --dry-run=client -o yaml | kubectl --context "$CLUSTER_CONTEXT" apply -f - >/dev/null
echo "✅ rca-agent-secret applied"

# ── 2. Observability plane chart (Observer + cluster-agent + RCA) ────────
echo ""
echo "2️⃣  openchoreo-observability-plane chart (v${OBS_PLANE_VERSION})"
cat > /tmp/obs-plane-values.yaml <<EOF
observer:
  openSearchSecretName: opensearch-admin-credentials
  secretName: observer-secret
  http:
    hostnames:
      - observer.openchoreo.localhost
  # The obs-plane runs co-located with the control-plane here, so talk
  # to it via in-cluster DNS instead of the chart default
  # (api.openchoreo.localhost, which only resolves on the host).
  controlPlaneApiUrl: "http://openchoreo-api.openchoreo-control-plane.svc.cluster.local:8080"
  # REQUIRED from chart 1.2.0. The chart now fails its own render if any of
  # controlPlaneApiUrl / observer.extraEnvs / rca.openchoreoApiUrl still carries
  # its placeholder ".invalid" domain, and its DEFAULT extraEnvs does:
  # OBSERVER_BASE_URL=http://observer.openchoreo.invalid:11080. So the block has
  # to be supplied here even though only one of its two entries is really ours.
  #
  # extraEnvs REPLACES the chart's list rather than merging, so AUTHZ_TIMEOUT is
  # restated at the chart's own default — dropping it would silently shorten the
  # authorization timeout.
  #
  # Port 8080, not the chart's 11080: step 4 below disables the bundled
  # port-11080 Gateway (k3d's serverlb does not publish it) and routes the
  # observer through the main kgateway on :8080 by Host header instead.
  extraEnvs:
    - name: OBSERVER_BASE_URL
      value: "http://observer.openchoreo.localhost:8080"
    - name: AUTHZ_TIMEOUT
      value: "30s"
security:
  enabled: true
  oidc:
    # In-cluster addresses of the platform IdP, from env.sh (this heredoc expands).
    jwksUrl: "${THUNDER_INTERNAL_JWKS_URL}"
    tokenUrl: "${THUNDER_INTERNAL_TOKEN_URL}"
    authServerBaseUrl: "http://thunder.openchoreo.localhost:8080"
rca:
  # SRE / RCA agent. Uses a locally-built image that carries the Anthropic
  # structured-output fix (ToolStrategy instead of ProviderStrategy — the stock
  # ghcr.io/openchoreo/sre-agent image rejects Anthropic with many tools:
  # "grammar too large") and, with the 'handoff' tag, the AEP coding-agent
  # handoff stage. Built + imported in step 1b above. If you switch to an
  # OpenAI model without the handoff, the stock image works and you can drop
  # the image override.
  enabled: true
  image:
    repository: ${RCA_IMAGE_REPO}
    tag: ${RCA_IMAGE_TAG}
    pullPolicy: IfNotPresent          # locally-imported image, not a registry pull
  llm:
    modelName: anthropic:claude-sonnet-4-6
  secretName: rca-agent-secret        # created in step 1b (RCA_LLM_API_KEY + OAUTH_CLIENT_SECRET)
  oauth:
    clientId: openchoreo-rca-agent    # registered by thunder-resources/86-openchoreo-rca-agent.yaml
  openchoreoApiUrl: "http://openchoreo-api.openchoreo-control-plane.svc.cluster.local:8080"
  # Stock limit is cpu:250m — too low for trace-heavy analyses. The agent can
  # trip its liveness probe (exit 137) mid-run, which orphans the report in
  # "pending". Bump CPU/mem so analyses complete.
  resources:
    requests:
      cpu: 250m
      memory: 1Gi
    limits:
      cpu: "1"
      memory: 2Gi
  http:
    hostnames:
      - rca-agent.openchoreo.localhost
# Disable the chart's standalone Gateway on :11080. k3d-openchoreo-serverlb
# doesn't expose 11080, and step 4 below adds a cross-NS HTTPRoute on the
# main kgateway (:8080) which is what the BFF reaches via
# Host: observer.openchoreo.localhost. The bundled Gateway is dead weight.
gateway:
  enabled: false
EOF
# Use `upgrade --install` (not the shared helm_install_if_not_exists helper)
# so re-runs pick up value changes from /tmp/obs-plane-values.yaml. The
# helper skips already-installed releases, which would silently bypass any
# future tuning here.
#
# --force-conflicts: this Helm (v4+) defaults --server-side to "auto", which
# uses SSA once a release's prior revision did. Step 3b below kubectl-patches
# observer-config/rca-agent-config/the RCA agent deployment AFTER every
# helm run (deliberately — see that step's comment), which stamps those
# fields with fieldManager "kubectl-patch". Without --force-conflicts, the
# NEXT re-run of this same `helm upgrade` fails outright ("Apply failed with
# 1 conflict: conflict with \"kubectl-patch\"") because SSA sees a foreign
# owner on a field the chart also sets. Safe to force here: step 3b
# unconditionally re-asserts the authoritative values right after this
# command anyway, so which side wins THIS apply doesn't matter.
helm upgrade --install observability-plane \
    "oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane" \
    --namespace "$NS" --create-namespace --kube-context "${CLUSTER_CONTEXT}" \
    --version "$OBS_PLANE_VERSION" \
    --values /tmp/obs-plane-values.yaml \
    --force-conflicts \
    --timeout 10m
echo "⏳ Waiting for Observer + controller-manager..."
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" wait --for=condition=Available deployment/observer --timeout=300s
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" wait --for=condition=Available deployment/controller-manager --timeout=300s
echo "✅ observability-plane ready"

# ── 3. Logs + OpenSearch + Fluent Bit chart ──────────────────────────────
echo ""
echo "3️⃣  observability-logs-opensearch chart (v${OBS_LOGS_VERSION})"
# Adapter image: AEP runs a patched build of the upstream 0.5.1 adapter whose
# alert-rule log matching is case-insensitive. The stock adapter compiles a
# rule into a case-SENSITIVE wildcard, so a rule watching "ERROR" silently
# stops firing the moment a code change rewords the log line to "error" — which
# is exactly what happened when a coding-agent PR detached an alert from the
# failure it was watching.
#
# Still not fixed upstream as of community-modules 0.5.4: BuildAlertQuery in
# internal/opensearch/queries.go emits a `wildcard` on `log` with no
# `case_insensitive: true`, unlike the log-LEVEL filter a few lines above it
# which does set the flag. So the fork stays.
#
# The fork is built from 0.5.1 while the CHART is now 0.5.3. That combination is
# verified: the 0.5.1 adapter reads 0.5.3's configuration correctly (OpenSearch
# address, index prefixes, observer URL) and reaches Ready. If a future chart
# does break it, clear OBS_LOGS_ADAPTER_IMAGE_REPO to fall back to the stock
# image and accept case-sensitive alert matching.
#
# Expect it to CrashLoopBackOff for the first minute or two of a fresh install:
# it refuses to start without OpenSearch, and OpenSearch is a large image and a
# slow boot. It recovers on its own once the StatefulSet is Ready.
if [ -n "$OBS_LOGS_ADAPTER_IMAGE_REPO" ]; then
    OBS_ADAPTER_IMAGE_BLOCK="  image:
    repository: ${OBS_LOGS_ADAPTER_IMAGE_REPO}
    tag: ${OBS_LOGS_ADAPTER_IMAGE_TAG}"
else
    OBS_ADAPTER_IMAGE_BLOCK="  # stock chart adapter image (case-SENSITIVE alert matching)"
fi
cat > /tmp/obs-logs-values.yaml <<EOF
openSearchSetup:
  openSearchSecretName: opensearch-admin-credentials
# Local-dev sizing — default heap is -Xmx512M, which resolves to ~980 MiB
# resident with JVM overhead. 256M heap is enough for one developer's log
# volume and brings resident usage to ~500-600 MiB. Subchart key is
# openSearch (camelCase) — confirmed via
# 'helm get values observability-logs-opensearch --all'.
# (Backticks avoided here — this heredoc is unquoted to allow ${VAR}
# substitution elsewhere, so backticks would trigger command substitution.)
openSearch:
  opensearchJavaOpts: "-Xmx256M -Xms256M"
  resources:
    requests:
      cpu: 200m
      memory: 512Mi
    limits:
      memory: 768Mi
# Enable Fluent Bit immediately so log collection is active from first install
# (avoids a second helm-upgrade pass).
fluent-bit:
  enabled: true
# logs-adapter (0.5.x): the alert-rule evaluation engine. The observer forwards
# ObservabilityAlertRule CRUD here; the adapter materialises each rule as an
# OpenSearch alerting monitor and webhooks fired alerts back to the observer.
# adapter.enabled defaults true in >=0.5.1; it only needs the credentials ref.
adapter:
  openSearchSecretName: opensearch-admin-credentials
${OBS_ADAPTER_IMAGE_BLOCK}
EOF
helm upgrade --install observability-logs-opensearch \
    "oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch" \
    --namespace "$NS" --create-namespace --kube-context "${CLUSTER_CONTEXT}" \
    --version "$OBS_LOGS_VERSION" \
    --values /tmp/obs-logs-values.yaml \
    --timeout 15m
echo "⏳ Waiting for OpenSearch StatefulSet (large image — first install ~5-10 min)..."
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status statefulset/opensearch-master --timeout=900s
echo "⏳ Waiting for logs-adapter..."
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status deploy/logs-adapter-opensearch --timeout=300s
echo "✅ logs-opensearch ready (incl. logs-adapter)"

# ── 3b. Alert→RCA auto-trigger + AEP handoff wiring ──────────────────────
# Post-helm ConfigMap patches + restarts. Patched here (not via chart values)
# because the chart doesn't expose all these keys and observer.extraEnvs
# REPLACES chart defaults — a patch after every helm run is deterministic.
#
#   observer-config:
#     LOGS_ADAPTER_ENABLED     without it, log alert rules are never evaluated
#     RCA_SERVICE_URL          where the observer POSTs /analyze on alert fire
#     ALERT_SUPPRESSION_WINDOW per-rule+component de-dup. UNSET ⇒ NO de-dup:
#                              concurrent RCA runs race the handoff's
#                              search-then-create dedup ⇒ duplicate GitHub
#                              issues + duplicate coding-agent dispatches.
#   rca-agent-config:
#     AE_HANDOFF               enables the RCA→AEP handoff stage (issue+dispatch)
#     AE_AUTO_DISPATCH         false ⇒ issue-only; a human dispatches from AEP
#     AE_API_URL               aep-mcp-server base URL (host.k3d.internal:3401)
#     AE_PUBLISH_REPORTS       publish RCA reports to aep-api (console Alerts)
#     AEP_API_URL              aep-api REST base (host.k3d.internal:9090)
#     AEP_MCP_URL              full MCP URL used by remediation/mcp.json.
#                              Auth is supplied by aep-mcp-server's local
#                              AEP_MCP_DEFAULT_BEARER fallback, not this
#                              plaintext client config.
echo ""
echo "3️⃣b Alert→RCA auto-trigger + AEP handoff wiring"
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" patch cm observer-config --type=merge -p \
    '{"data":{"LOGS_ADAPTER_ENABLED":"true","RCA_SERVICE_URL":"http://'"${RCA_DEPLOYMENT}"':8080","ALERT_SUPPRESSION_WINDOW":"1h"}}'
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout restart deploy/observer
if [ "$AE_HANDOFF" = "true" ]; then
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" patch cm rca-agent-config --type=merge -p \
        "{\"data\":{\"AE_HANDOFF\":\"true\",\"AE_AUTO_DISPATCH\":\"${AE_AUTO_DISPATCH}\",\"AE_API_URL\":\"${AE_API_URL}\",\"AEP_MCP_URL\":\"${AEP_MCP_URL}\",\"AE_PUBLISH_REPORTS\":\"${AE_PUBLISH_REPORTS}\",\"AEP_API_URL\":\"${AEP_API_URL}\"}}"
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout restart deploy/${RCA_DEPLOYMENT}
    echo "   AE handoff: enabled (auto-dispatch=${AE_AUTO_DISPATCH}, mcp=${AEP_MCP_URL})"
    echo "   Report publishing: ${AE_PUBLISH_REPORTS} (aep-api=${AEP_API_URL})"
else
    echo "   AE handoff: disabled (AE_HANDOFF=false)"
fi
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status deploy/observer --timeout=300s

# ── 3c. observer-auth-config: service-account claim → client_id ───────────
# The observer resolves service accounts through its OWN copy of the
# entitlement mapping, separate from openchoreo-api-config, and it also ships
# keyed on `sub`. ThunderID puts a client_credentials subject in `client_id`
# (see setup-openchoreo.sh), so without this every build-log and observability
# query from a service account gets a 403.
#
# The configmap is populated ASYNCHRONOUSLY — it can exist before its
# service-account claim is written. Patching too early would see no `sub` and
# silently do nothing, so wait for the claim to surface first (either `sub` to
# patch, or `client_id` if a prior run already did it).
echo ""
echo "3️⃣c observer-auth-config service-account claim"
_obs_claim=""
for _ in $(seq 1 30); do
    _obs_claim="$(kubectl --context "$CLUSTER_CONTEXT" -n "$NS" get configmap observer-auth-config -o yaml 2>/dev/null \
        | grep -oE "claim:[[:space:]]*['\"]?(sub|client_id)['\"]?" | head -1)"
    [ -n "$_obs_claim" ] && break
    sleep 4
done
if [ -z "$_obs_claim" ]; then
    # Distinguish a genuinely-absent configmap from one that is present but
    # whose claim never surfaced, so the outcome is not misleading.
    if kubectl --context "$CLUSTER_CONTEXT" -n "$NS" get configmap observer-auth-config &>/dev/null; then
        echo "❌ observer-auth-config is present but its service-account claim never surfaced" >&2
        exit 1
    fi
    echo "   ⚠️  observer-auth-config not found — skipping"
elif echo "$_obs_claim" | grep -q client_id; then
    echo "   ✓ already client_id"
else
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" get configmap observer-auth-config -o yaml \
        | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g" \
        | kubectl --context "$CLUSTER_CONTEXT" apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout restart deployment/observer >/dev/null
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status deployment/observer --timeout=120s >/dev/null
    echo "   ✓ patched to client_id"
fi
if [ "$AE_HANDOFF" = "true" ]; then
    # With AE_HANDOFF=true the agent's boot-time MCP test is FATAL: it must
    # reach aep-mcp-server (docker-compose, started later by start.sh). Only
    # wait for readiness if that server is already up (i.e. setup is being
    # re-run on a live stack); on a fresh setup the crash-loop is expected
    # and start.sh auto-recovers the agent once compose is up.
    if curl -s --max-time 2 http://localhost:3401/healthz 2>/dev/null | grep -q '"ok"'; then
        kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status deploy/${RCA_DEPLOYMENT} --timeout=300s || \
            echo "⚠️  ${RCA_DEPLOYMENT} not ready — check the RCA image import in step 1b"
    else
        echo "ℹ️  aep-mcp-server not running yet — ${RCA_DEPLOYMENT} will crash-loop until"
        echo "    'bash scripts/start.sh' brings the compose stack up (start.sh then"
        echo "    auto-restarts the agent). This is expected on a fresh setup."
    fi
fi
echo "✅ auto-trigger + handoff wiring applied"

# ── 3c. Dynamic Anthropic key — reuse the org's key as set via the AE console ──
# AnthropicCredentialService.Connect() (aep-api) stores the console-connected
# key in Postgres (org_secrets, AES-256-GCM) and best-effort mirrors it into
# OpenBao via the SM-API stub. For local Docker Compose + k3d development,
# scripts/repair-secrets.sh / scripts/reconcile-sre-anthropic-externalsecret.sh
# point ExternalSecrets at the AE-stored org key so ESO materializes it into
# this namespace as rca-agent-anthropic-secret.
#
# THIS script ensures the STRUCTURAL piece exists: the volume + mount + env var
# wiring below. The secret is NOT optional when AE_HANDOFF is on. A Ready SRE
# pod without an AE-managed org key is worse than a blocked pod: the first alert
# reaches RCA and fails at analysis time. The reconcile below makes the key the
# user saved through the AE Console (or local seed-dev's call into the same
# config API) available as rca-agent-anthropic-secret before the final readiness
# step. With AE_HANDOFF off the volume stays optional, so a missing secret
# cannot keep the SRE pod from starting.
if [ "$AE_HANDOFF" = "true" ]; then
    ANTHROPIC_SECRET_OPTIONAL=false
else
    ANTHROPIC_SECRET_OPTIONAL=true
fi
echo ""
echo "3️⃣c Dynamic Anthropic key (volume wiring; the ExternalSecret is owned by the RCA agent's own manifest)"
# Patched onto the Deployment (not chart values) for the same "survives a
# helm re-run" reason as step 3b's ConfigMap patches. A podSpec change here
# triggers K8s's normal rolling update on its own — no explicit restart needed.
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" patch deployment ${RCA_DEPLOYMENT} --type=strategic -p '
spec:
  template:
    spec:
      volumes:
        - name: anthropic-key
          secret:
            secretName: rca-agent-anthropic-secret
            optional: '"${ANTHROPIC_SECRET_OPTIONAL}"'
            defaultMode: 0400
      containers:
        - name: '"${RCA_DEPLOYMENT}"'
          volumeMounts:
            - name: anthropic-key
              mountPath: /etc/rca-agent/anthropic
              readOnly: true
          env:
            - name: RCA_LLM_API_KEY_FILE
              value: /etc/rca-agent/anthropic/RCA_LLM_API_KEY
'
echo "✅ ${RCA_DEPLOYMENT} volume/env wired for the dynamic Anthropic key"
echo "   Local start/repair projects AE's org Anthropic key into rca-agent-anthropic-secret."
echo "   Secret volume optional=${ANTHROPIC_SECRET_OPTIONAL} (required only when AE_HANDOFF=true)."
if [ -f "$SCRIPT_DIR/reconcile-sre-anthropic-externalsecret.sh" ]; then
    echo "🤖 Reconciling SRE Anthropic ExternalSecret if local AE metadata is available..."
    bash "$SCRIPT_DIR/reconcile-sre-anthropic-externalsecret.sh" || \
        echo "   ⚠️  could not reconcile SRE Anthropic ExternalSecret now; start.sh will retry."
fi

# ── 3d. AEP-owned SRE remediation extension — deploy-time mount ───────────
# The OC SRE agent loads remediation extensions from EXTENSIONS_DIR. AE owns
# this extension because it defines the AE MCP endpoint and the exact
# coding-agent-handoff skill contract. The deployment-owned context/mcp files
# live in deployments/sre-agent-extensions/remediation; the skill is mounted
# from services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md so the MCP
# server and agent instructions stay in sync.
if [ "$AE_HANDOFF" = "true" ]; then
    echo ""
    echo "3️⃣d SRE remediation extension — ConfigMap + mount"
    SRE_EXTENSION_DIR="$SCRIPT_DIR/../sre-agent-extensions/remediation"
    HANDOFF_CONTEXT="$SRE_EXTENSION_DIR/CONTEXT.md"
    HANDOFF_MCP_JSON="$SRE_EXTENSION_DIR/mcp.json"
    HANDOFF_SKILL="$SCRIPT_DIR/../../services/aep-mcp-server/skills/coding-agent-handoff/SKILL.md"
    if [ ! -f "$HANDOFF_CONTEXT" ] || [ ! -f "$HANDOFF_MCP_JSON" ] || [ ! -f "$HANDOFF_SKILL" ]; then
        echo "⚠️  SRE remediation extension files are incomplete — skipping mount."
        echo "    Expected $HANDOFF_CONTEXT, $HANDOFF_MCP_JSON, and $HANDOFF_SKILL"
        echo "    AE_HANDOFF is on, so ${RCA_DEPLOYMENT} will not dispatch AE issues until fixed."
    else
        # Render deterministically (create --dry-run) then apply, so re-runs are
        # idempotent and the ConfigMap can be diffed. ConfigMap keys are flat;
        # the Deployment volume items below map them back to remediation paths.
        #
        # The SRE extension loader validates the MCP URL before env expansion in
        # the remediation path, so render the concrete URL into mcp.json here.
        _rendered_mcp_json="$(mktemp)"
        awk -v url="$AEP_MCP_URL" '{ gsub(/\$\{AEP_MCP_URL\}/, url); print }' "$HANDOFF_MCP_JSON" > "$_rendered_mcp_json"
        kubectl --context "$CLUSTER_CONTEXT" -n "$NS" create configmap sre-agent-extensions \
            --from-file=CONTEXT.md="$HANDOFF_CONTEXT" \
            --from-file=mcp.json="$_rendered_mcp_json" \
            --from-file=SKILL.md="$HANDOFF_SKILL" \
            --dry-run=client -o yaml | kubectl --context "$CLUSTER_CONTEXT" apply -f - >/dev/null
        rm -f "$_rendered_mcp_json"
        echo "✅ sre-agent-extensions ConfigMap applied"

        # Patch the Deployment: mount the extension at
        # /etc/openchoreo/sre-agent/remediation/{CONTEXT.md,mcp.json,skills/...}.
        # A podSpec change here triggers a rolling update on its own.
        kubectl --context "$CLUSTER_CONTEXT" -n "$NS" patch deployment ${RCA_DEPLOYMENT} --type=strategic -p '
spec:
  template:
    spec:
      volumes:
        - name: sre-agent-extensions
          configMap:
            name: sre-agent-extensions
            items:
              - key: CONTEXT.md
                path: remediation/CONTEXT.md
              - key: mcp.json
                path: remediation/mcp.json
              - key: SKILL.md
                path: remediation/skills/coding-agent-handoff/SKILL.md
      containers:
        - name: '"${RCA_DEPLOYMENT}"'
          volumeMounts:
            - name: sre-agent-extensions
              mountPath: /etc/openchoreo/sre-agent
              readOnly: true
          env:
            - name: EXTENSIONS_DIR
              value: /etc/openchoreo/sre-agent
'
        echo "✅ ${RCA_DEPLOYMENT} volume/env wired for the remediation extension (EXTENSIONS_DIR=/etc/openchoreo/sre-agent)"
        echo "   Edit deployments/sre-agent-extensions or services/aep-mcp-server/skills/coding-agent-handoff, then re-run this script."
    fi
fi

# ── 4. Cross-namespace HTTPRoute on the MAIN kgateway ────────────────────
# The chart's own HTTPRoute attaches to a separate Gateway on port 11080.
# k3d's serverlb only exposes the main kgateway on port 8080. Add a second
# HTTPRoute targeting the main kgateway so docker-compose-hosted BFF can
# reach the Observer via http://k3d-openchoreo-serverlb:8080 + Host header.
echo ""
echo "4️⃣  Cross-namespace HTTPRoute on main kgateway"
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<EOF
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: observer-mainkgw
  namespace: $NS
spec:
  parentRefs:
    - name: gateway-default
      namespace: openchoreo-control-plane
      sectionName: http
  hostnames:
    - observer.openchoreo.localhost
  rules:
    - matches:
        - path: { type: PathPrefix, value: / }
      backendRefs:
        - name: observer
          port: 8080
      timeouts:
        request: "0s"
        backendRequest: "0s"
EOF
echo "✅ HTTPRoute observer-mainkgw applied"

# ── 4b. AEP authz role + binding for Observer ───────────────────
# The OC control-plane chart ships a `observer-resource-reader` role
# bound to `openchoreo-observer-resource-reader-client` (the Observer's
# UID-resolver subject), but that role only has component/project/
# namespace/environment :view — NOT logs:view or workflowrun:view, both
# of which the Observer requires for /api/v1/logs/query. Without these
# the Observer returns 403 "no matching policies found" even though
# JWT auth succeeds. Mirrors v2 wso2cloud-deployment/.../init/layer-2/controlplane.yaml.
echo ""
echo "4b. AEP ClusterAuthzRole + binding for Observer"
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<'EOF'
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRole
metadata:
  name: aep-observer-reader
spec:
  actions:
    - "logs:view"
    - "workflowrun:view"
    - "component:view"
    - "project:view"
    - "namespace:view"
    - "environment:view"
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: aep-observer-reader-binding
spec:
  effect: allow
  entitlement:
    # client_id, not sub: ThunderID puts a client_credentials token's subject in
    # the client_id claim. Same move as every other service-account binding
    # (setup-openchoreo.sh, setup-aep.sh) — this one lives here because the
    # observer role is only meaningful once the observability plane exists.
    claim: client_id
    value: openchoreo-observer-resource-reader-client
  roleMappings:
    - roleRef:
        kind: ClusterAuthzRole
        name: aep-observer-reader
EOF
echo "✅ AEP observer-reader role + binding applied"

# ── 5. ClusterObservabilityPlane CR (registers plane with the CP) ────────
echo ""
echo "5️⃣  ClusterObservabilityPlane CR"
# The cluster-agent needs the CA of the local obs-plane to verify the
# Observer's TLS cert. The chart creates a cluster-agent TLS secret (cluster-agent-tls) with the CA, but does not expose it as a value. Grab it from the secret and inject it into the CR.
# Checked in two steps (not a single piped command) because `set -e` alone
# doesn't catch a failing left side of a pipe — a missing secret or empty
# ca.crt would otherwise silently apply a ClusterObservabilityPlane with a
# blank clientCA, breaking cluster-agent's TLS verification with no error.
local_obs_ca_b64=$(kubectl --context "$CLUSTER_CONTEXT" get secret cluster-agent-tls -n "$NS" -o jsonpath='{.data.ca\.crt}')
if [ -z "$local_obs_ca_b64" ]; then
  echo "❌ cluster-agent-tls secret has no ca.crt data (or doesn't exist) in namespace $NS" >&2
  exit 1
fi
local_obs_ca=$(printf '%s' "$local_obs_ca_b64" | base64 -d)
if [ -z "$local_obs_ca" ]; then
  echo "❌ failed to base64-decode ca.crt from the cluster-agent-tls secret" >&2
  exit 1
fi
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterObservabilityPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(printf '%s' "$local_obs_ca" | sed 's/^/        /')
  observerURL: http://observer.openchoreo.localhost:11080
  # Lets the portal fetch RCA reports from the SRE agent (rca.enabled above).
  rcaAgentURL: http://rca-agent.openchoreo.localhost:11080
EOF
echo "✅ ClusterObservabilityPlane registered"

# ── 6. OpenSearch index-template bootstrap Job ───────────────────────────
# Fixes the upstream chart's race where Fluent Bit's first stdout-write can
# land before the container-logs index template applies, leaving
# kubernetes.pod_name as `text` instead of `keyword` — Observer's wildcard
# query then matches zero docs. Self-healing: applies the priority-500
# composable template and deletes any indices with the wrong mapping.
echo ""
echo "6️⃣  OpenSearch index-template bootstrap Job"
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" delete job opensearch-bootstrap-templates --ignore-not-found >/dev/null
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<'EOF'
apiVersion: batch/v1
kind: Job
metadata:
  name: opensearch-bootstrap-templates
  namespace: openchoreo-observability-plane
spec:
  backoffLimit: 5
  ttlSecondsAfterFinished: 600
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: bootstrap
          image: curlimages/curl:8.10.1
          env:
            - { name: OS_HOST, value: opensearch }
            - { name: OS_PORT, value: "9200" }
            - name: OS_USER
              valueFrom: { secretKeyRef: { name: opensearch-admin-credentials, key: username } }
            - name: OS_PASS
              valueFrom: { secretKeyRef: { name: opensearch-admin-credentials, key: password } }
          command: ["/bin/sh", "-c"]
          args:
            - |
              set -eu
              OS="https://${OS_HOST}:${OS_PORT}"
              CURL="curl -sk -u ${OS_USER}:${OS_PASS} -H Content-Type:application/json"
              echo "Waiting for OpenSearch ready..."
              for i in $(seq 1 60); do
                if $CURL "${OS}/_cluster/health?wait_for_status=yellow&timeout=5s" >/dev/null 2>&1; then break; fi
                sleep 5
              done
              # Do NOT PUT a template here. The 0.5.x module chart's own setup job
              # (post-install/upgrade hook) applies the authoritative container-logs
              # template: log as `wildcard` (the alert monitors' *phrase* queries
              # need it — on `text`, wildcard patterns match analysed lowercase
              # tokens and "ERROR" silently matches nothing), pod_name/labels as
              # keyword (the monitors' UID `term` filters need keyword). A custom
              # same-name template REPLACES the chart's and broke both — this job
              # is now detection + self-heal only.
              echo "Verifying chart template is in place (log must be wildcard-typed)..."
              tpl_log=$($CURL "${OS}/_index_template/container-logs" 2>/dev/null \
                | grep -o '"log":{"type":"[a-z_]*"}' | head -1 | cut -d'"' -f6)
              if [ "$tpl_log" != "wildcard" ]; then
                echo "WARNING: container-logs template maps log as '${tpl_log:-absent}' (expected 'wildcard')."
                echo "         The module chart's opensearch-setup-logs hook job should own this template."
              fi
              echo "Scanning indices for wrong mappings (pod_name/labels != keyword, log != wildcard)..."
              for idx in $($CURL "${OS}/_cat/indices/container-logs-*?h=index" 2>/dev/null); do
                t=$($CURL "${OS}/${idx}/_mapping/field/kubernetes.pod_name" 2>/dev/null \
                  | grep -o '"type":"[a-z]*"' | head -1 | cut -d'"' -f4)
                lt=$($CURL "${OS}/${idx}/_mapping/field/kubernetes.labels.openchoreo_dev%2Fcomponent-uid" 2>/dev/null \
                  | grep -o '"type":"[a-z]*"' | head -1 | cut -d'"' -f4)
                lg=$($CURL "${OS}/${idx}/_mapping/field/log" 2>/dev/null \
                  | grep -o '"type":"[a-z_]*"' | head -1 | cut -d'"' -f4)
                if [ "$t" = "text" ] || [ "$lt" = "text" ] || { [ -n "$lg" ] && [ "$lg" != "wildcard" ]; }; then
                  echo "  - ${idx}: pod_name='$t' component-uid='$lt' log='$lg', recreating"
                  $CURL -X DELETE "${OS}/${idx}" >/dev/null
                else echo "  - ${idx}: pod_name='$t' component-uid='${lt:-unset}' log='${lg:-unset}', ok"; fi
              done
              echo "Bootstrap complete."
EOF
echo "⏳ Waiting for bootstrap Job to finish..."
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" wait --for=condition=complete job/opensearch-bootstrap-templates --timeout=300s
echo "✅ OpenSearch index-template bootstrap complete"

echo ""
echo "7️⃣  Ensure observability workloads are running"
# setup-observability.sh is the repair/reconcile entry point for the
# alert→RCA→coding-agent pipeline. If a prior full setup or a manual
# park-observability.sh down left OpenSearch, Fluent Bit, the logs adapter, or
# the SRE agent parked at zero replicas, a direct re-run of this script must
# bring them back; otherwise the install reports success while no alert can be
# evaluated and no RCA can run. The full setup script may still choose to park
# them after all setup steps finish, but this script's own postcondition is an
# active observability/SRE pipeline.
bash "$SCRIPT_DIR/park-observability.sh" up

echo ""
echo "✅ Observability Plane installation complete!"
echo ""
echo "   Alert→RCA→coding-agent handoff is wired (AE_HANDOFF=${AE_HANDOFF})."
echo "   One step can't be automated: create an ObservabilityAlertRule per"
echo "   component you want auto-RCA on (needs the component's UID + name"
echo "   labels, incident.enabled + triggerAiRca: true)."
echo "   Guide: docs/developer-guide/sre-handoff-runbook.md"
