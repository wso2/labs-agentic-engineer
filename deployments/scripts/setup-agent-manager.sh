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

# Installs the WSO2 Agent Management Platform alongside AEP on the SAME cluster,
# from WSO2's published OCI charts. Opt-in: ENABLE_AGENT_MANAGER=1.
#
# Nothing here is built from a checkout. Agent Manager publishes its whole
# platform as charts and images, and its own quick-start installer is built on
# that premise — "only public helm charts are used". This script is the AEP-side
# equivalent of agent-manager's deployments/quick-start/install-helpers.sh,
# minus everything AEP's own setup already did (cluster, prerequisites, the
# OpenChoreo planes, the gateway operator, and the shared platform Thunder,
# which setup-thunder.sh installs unconditionally).
#
# The one thing it does NOT do is share a Thunder with the per-environment tier.
# Agent Manager provisions one Thunder per environment for AgentID / workload
# identity, straight from the upstream `thunderid` chart at its own version.
# That tier is untouched by convergence — only the platform tier is shared.
#
# Teardown: scripts/teardown-agent-manager.sh.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

AMP_NS="wso2-amp"
OBS_NS="openchoreo-observability-plane"
DP_NS="openchoreo-data-plane"
WP_NS="openchoreo-workflow-plane"

echo "============================================"
echo "  Agent Management Platform ${AMP_VERSION}"
echo "============================================"

kubectl cluster-info --context "$CLUSTER_CONTEXT" &>/dev/null || {
    echo "❌ Cluster '$CLUSTER_CONTEXT' not running. Run scripts/setup.sh first." >&2; exit 1
}
kubectl get ns "$OBS_NS" &>/dev/null || {
    echo "❌ The observability plane is required by Agent Manager's console and observer." >&2
    echo "   Re-run setup with ENABLE_OBSERVABILITY=1." >&2
    exit 1
}

load_public_urls "$SCRIPT_DIR/../.env"

# ── 1. DNS for Agent Manager's hostnames ────────────────────────────────────
# The charts keep their own *.amp.localhost defaults (only Thunder's hostname is
# overridden, to AEP's). macOS resolves any *.localhost to 127.0.0.1 host-side,
# but IN-CLUSTER callers need these too: the gateway extension's bootstrap Job
# calls api.amp.localhost, and agents resolve their own gateway vhost.
#
# Rewritten to host.k3d.internal rather than to a Service, so the request
# hairpins out to the k3d load balancer and back in through the gateway with
# its Host header intact — which is what the vhost matching needs. This is the
# shape agent-manager validated (deployments/k8s/coredns-amp-custom.yaml).
echo ""
echo "1️⃣  CoreDNS rewrites for *.amp.localhost and the agent gateway hosts"
ensure_amp_localhost_in_coredns

# ── 2. Observability modules Agent Manager needs ────────────────────────────
# AEP's own observability install brings the plane, OpenSearch, Fluent Bit and
# the logs adapter. Agent Manager additionally needs traces and metrics —
# without them its console renders empty trace and metric views.
echo ""
echo "2️⃣  Tracing + metrics observability modules"
helm upgrade --install observability-traces-opensearch \
    oci://ghcr.io/openchoreo/helm-charts/observability-tracing-opensearch \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --version "${OBSERVABILITY_TRACING_VERSION}" \
    --set openSearch.enabled=false \
    --set openSearchSetup.openSearchSecretName=opensearch-admin-credentials \
    --timeout 15m
echo "   ✅ tracing module"

# The Prometheus chart's own CRDs race its first reconcile on a cold cluster.
# One retry after waiting for establishment is the documented recovery.
install_metrics_module() {
    helm upgrade --install observability-metrics-prometheus \
        oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
        --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
        --version "${OBSERVABILITY_METRICS_VERSION}" \
        --set adapter.image.tag="" \
        --timeout 10m
}
if ! metrics_out="$(install_metrics_module 2>&1)"; then
    echo "$metrics_out"
    if echo "$metrics_out" | grep -q "ensure CRDs are installed first"; then
        echo "⚠️  Prometheus CRDs not established yet — waiting, then retrying once..."
        kubectl wait --for=condition=established --timeout=120s \
            crd/servicemonitors.monitoring.coreos.com \
            crd/podmonitors.monitoring.coreos.com \
            crd/prometheuses.monitoring.coreos.com \
            crd/alertmanagers.monitoring.coreos.com 2>/dev/null || true
        install_metrics_module
    else
        echo "❌ metrics module install failed" >&2
        exit 1
    fi
fi
echo "   ✅ metrics module"

# ── 3. Hand DeploymentPipeline/default over to Helm ─────────────────────────
# Both products want a DeploymentPipeline named `default` in namespace
# `default`, and neither can rename its own: the OpenChoreo API hardcodes
# `DeploymentPipeline/default` when a client creates a project without naming
# one, and both AEP and Agent Manager create projects that way.
#
# So there is ONE object, carrying the union of both promotion paths — AEP's
# `development` and Agent Manager's `default`. Agent Manager's chart owns it,
# because a chart cannot adopt an object it did not create; stamping Helm's
# ownership metadata onto AEP's copy first is the documented way to hand it
# over. The union is passed as values so what Helm renders is what is already
# there.
echo ""
echo "3️⃣  Handing DeploymentPipeline/default to Helm (union of both promotion paths)"
if kubectl get deploymentpipeline default -n default &>/dev/null; then
    kubectl annotate deploymentpipeline default -n default --overwrite \
        meta.helm.sh/release-name=amp-platform-resources \
        meta.helm.sh/release-namespace=default >/dev/null
    kubectl label deploymentpipeline default -n default --overwrite \
        app.kubernetes.io/managed-by=Helm >/dev/null
    echo "   ✅ ownership stamped for adoption"
fi

# ── 4. Agent Manager's charts ───────────────────────────────────────────────
# AEP used to ship its build templates under the same three cluster-scoped names
# Agent Manager uses — `checkout-source`, `containerfile-build`, `publish-image`
# — as divergent forks of the same upstream. Nothing errors when both are
# present: the last apply wins and the other product's builds quietly change
# behaviour. AEP's are now `aep-`prefixed, but a cluster that predates that
# rename still carries the old kubectl-applied objects, and Helm refuses to
# create an object it does not own. Remove them, and ONLY when they are not
# already Helm-managed.
echo ""
echo "4️⃣  Clearing pre-rename build templates, if any"
for tpl in checkout-source containerfile-build publish-image; do
    owner="$(kubectl get clusterworkflowtemplate "$tpl" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null || true)"
    if kubectl get clusterworkflowtemplate "$tpl" --context "$CLUSTER_CONTEXT" &>/dev/null \
       && [ "$owner" != "Helm" ]; then
        kubectl delete clusterworkflowtemplate "$tpl" --context "$CLUSTER_CONTEXT" >/dev/null
        echo "   removed stale ${tpl} (AEP's copy is now aep-${tpl})"
    fi
done

echo ""
echo "5️⃣  Platform resources extension"
helm upgrade --install amp-platform-resources \
    "${AMP_REGISTRY}/wso2-amp-platform-resources-extension" \
    --version "${AMP_VERSION}" \
    --namespace default --kube-context "$CLUSTER_CONTEXT" \
    --set "deploymentPipeline.promotionOrder[0].sourceEnvironmentRef.name=development" \
    --set-json "deploymentPipeline.promotionOrder[0].targetEnvironmentRefs=[]" \
    --set "deploymentPipeline.promotionOrder[1].sourceEnvironmentRef.name=default" \
    --set-json "deploymentPipeline.promotionOrder[1].targetEnvironmentRefs=[]" \
    --timeout 10m
echo "   ✅ ProjectType, Environment, ComponentTypes, amp-* workflows, traits"

echo ""
echo "6️⃣  Agent sandbox module"
# Agents run as sandboxed pods rendered from the SandboxTemplate /
# SandboxWarmPool CRDs this module provides. Versioned independently of
# AMP_VERSION — it is an OpenChoreo community module.
helm upgrade --install agent-sandbox \
    oci://ghcr.io/openchoreo/helm-charts/agent-sandbox \
    --version "${AGENT_SANDBOX_MODULE_VERSION}" \
    --namespace "$DP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --wait \
    --set namespace=openchoreo-control-plane \
    --set dataPlaneNamespace="$DP_NS" \
    --set dataPlaneServiceAccount=cluster-agent-dataplane \
    --set upstream.version="${AGENT_SANDBOX_UPSTREAM_VERSION}" \
    --timeout 10m
kubectl wait -n agent-sandbox-system --context "$CLUSTER_CONTEXT" \
    --for=condition=available --timeout=180s deployment/agent-sandbox-controller
echo "   ✅ sandbox controller ready"

echo ""
echo "7️⃣  Agent Manager (amp-api, amp-console, PostgreSQL)"
# The chart runs its own DB-migration and JWT-key-generation Jobs, so there is
# no equivalent of `make dev-migrate` / `make gen-keys` here.
#
# Three of the chart's values name the platform IdP by its PUBLIC url, and all
# three default to the chart's own thunder.amp.localhost. This deployment
# publishes that same Thunder on AEP's hostname instead, so all three move:
#
#   keyManager.issuer    the `iss` amp-api validates incoming tokens against —
#                        must equal what Thunder actually stamps
#   thunder.baseURL      admin-API requests ask for a token scoped to Thunder's
#                        System resource server, whose identifier is derived
#                        from this; Thunder only recognises the public URL
#   console.auth.baseUrl where the browser is sent to log in
#
# Their in-cluster siblings (keyManager.jwksUrl, thunder.resolveToHost) already
# point at amp-thunder-extension-service and need no change — the namespace and
# service name are unchanged, only the public hostname moved.
#
# thunderHostBaseDomain is deliberately LEFT at amp.localhost: it builds the
# per-environment Thunder hostnames, and that tier is not shared.
helm upgrade --install amp "${AMP_REGISTRY}/wso2-agent-manager" \
    --version "${AMP_VERSION}" \
    --namespace "$AMP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set console.config.instrumentationUrl="http://default-default.gateway.localhost:19080/otel" \
    --set agentManagerService.config.amObserverPublicURL="http://traces.amp.localhost:11080" \
    --set "agentManagerService.config.keyManager.issuer=${PUBLIC_THUNDER_URL}" \
    --set "agentManagerService.config.thunder.baseURL=${PUBLIC_THUNDER_URL}" \
    --set "console.config.auth.baseUrl=${PUBLIC_THUNDER_URL}" \
    --timeout 30m
echo "⏳ Waiting for Agent Manager..."
kubectl rollout status statefulset/amp-postgresql -n "$AMP_NS" --context "$CLUSTER_CONTEXT" --timeout=600s
kubectl wait -n "$AMP_NS" --context "$CLUSTER_CONTEXT" \
    --for=condition=available --timeout=600s deployment/amp-api deployment/amp-console
echo "   ✅ amp-api + amp-console ready"

echo ""
echo "8️⃣  Observability extension (amp-observer)"
# amp-observer's HTTPRoute names `gateway-default` with NO namespace, so it
# attaches to the Gateway in its own namespace — the observability plane's
# bundled one, on the port k3d publishes as 11080. AEP disables that Gateway
# (setup-observability.sh: "dead weight") because AEP reaches the observer
# through the main kgateway on :8080 by Host header instead. With Agent Manager
# installed it is no longer dead weight, so turn it back on.
#
# --reuse-values so this does not silently reset every value
# setup-observability.sh set.
echo "   re-enabling the observability plane's own gateway (amp-observer rides it)"
helm upgrade observability-plane \
    oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane \
    --namespace "$OBS_NS" --kube-context "$CLUSTER_CONTEXT" \
    --version "${OPENCHOREO_VERSION}" \
    --reuse-values --set gateway.enabled=true \
    --force-conflicts --timeout 10m

# auth.issuer defaults to the chart's own thunder.amp.localhost. It is what
# amp-observer validates `iss` against, so it has to name the IdP this
# deployment actually publishes — same move as the three values on the
# agent-manager chart above.
helm upgrade --install amp-observability-traces \
    "${AMP_REGISTRY}/wso2-amp-observability-extension" \
    --version "${AMP_VERSION}" \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "amObserver.auth.issuer=${PUBLIC_THUNDER_URL}" \
    --timeout 15m
if kubectl get deployment amp-observer -n "$OBS_NS" &>/dev/null; then
    kubectl wait -n "$OBS_NS" --context "$CLUSTER_CONTEXT" \
        --for=condition=available --timeout=300s deployment/amp-observer
fi
echo "   ✅ amp-observer"

echo ""
echo "9️⃣  Evaluation extension"
# The chart's NetworkPolicy targets workflows-<env>, which OpenChoreo only
# creates once a workflow has actually run. Pre-create it so install order does
# not depend on that.
kubectl create namespace workflows-default --dry-run=client -o yaml \
    | kubectl apply -f - >/dev/null
# The eval pod runs untrusted evaluator code. Scope its API-server egress to the
# k3d node network instead of taking the chart's RFC1918 default, which also
# spans the pod and service CIDRs.
eval_args=()
node_cidr="$(docker network inspect "k3d-${CLUSTER_NAME}" \
    --format '{{ (index .IPAM.Config 0).Subnet }}' 2>/dev/null || true)"
[ -n "$node_cidr" ] && eval_args=(--set "networkPolicy.evaluationJob.apiServer.cidrs[0]=${node_cidr}")
helm upgrade --install amp-evaluation-extension \
    "${AMP_REGISTRY}/wso2-amp-evaluation-extension" \
    --version "${AMP_VERSION}" \
    --namespace "$WP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    ${eval_args[@]+"${eval_args[@]}"} \
    --timeout 10m
echo "   ✅ evaluation extension"

echo ""
echo "============================================"
echo "  ✅ Agent Manager installed"
echo "============================================"
echo ""
echo "  Console: http://console.amp.localhost:8080"
echo "  API:     http://api.amp.localhost:8080"
echo "  Login:   admin / admin (the same platform IdP as the AEP console)"
echo ""
echo "  The default environment's own Thunder and its API Platform gateway are"
echo "  provisioned separately — they need amp-api reachable on its public URL:"
echo "      bash scripts/setup-agent-manager-env.sh"
echo ""
