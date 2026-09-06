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
#
# The chart caps the Prometheus operator at 40m CPU, which is sized for a
# cluster running one platform. Here it runs two, and the operator sits pinned
# at its ceiling: its /healthz then cannot answer inside the 1s probe timeout,
# so the kubelet SIGTERMs it and it restarts forever (exit 0, "Completed" — the
# giveaway that this is a probe kill and not a crash). The metrics adapter dies
# with it, because no operator means no Prometheus StatefulSet to connect to.
# Memory is untouched; measured use is 16Mi of the 60Mi it is already given.
install_metrics_module() {
    helm upgrade --install observability-metrics-prometheus \
        oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
        --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
        --version "${OBSERVABILITY_METRICS_VERSION}" \
        --set adapter.image.tag="" \
        --set kube-prometheus-stack.prometheusOperator.resources.requests.cpu=50m \
        --set kube-prometheus-stack.prometheusOperator.resources.limits.cpu=300m \
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

# ── 3. Hand Environment/default and DeploymentPipeline/default over to Helm ──
# Both products want the same two objects in namespace `default`, and neither
# can rename its own. The OpenChoreo API hardcodes `DeploymentPipeline/default`
# when a client creates a project without naming one, and both AEP and Agent
# Manager create projects that way. `Environment/default` is the ONE environment
# both products deploy into: AEP so that one environment Thunder and one API
# Platform gateway serve both, Agent Manager because its chart names it so.
#
# setup-aep.sh creates both with kubectl before this script runs. Agent
# Manager's platform-resources chart owns them from here on, because a chart
# cannot adopt an object it did not create; stamping Helm's ownership metadata
# onto AEP's copies first is the documented way to hand them over. The chart's
# defaults render what AEP wrote — one promotion path from `default` — plus the
# gateway ingress Agent Manager persists on its Environment, so the hand-over
# changes the owner, not the environment. The annotations
# setup-environment-thunder.sh projected onto the Environment are not in the
# chart's manifest and survive the apply: server-side apply owns fields, not
# objects.
echo ""
echo "3️⃣  Handing Environment/default and DeploymentPipeline/default to Helm"
for kind in environment deploymentpipeline; do
    kubectl get "$kind" default -n default &>/dev/null || continue
    kubectl annotate "$kind" default -n default --overwrite \
        meta.helm.sh/release-name=amp-platform-resources \
        meta.helm.sh/release-namespace=default >/dev/null
    kubectl label "$kind" default -n default --overwrite \
        app.kubernetes.io/managed-by=Helm >/dev/null
    # Ownership metadata alone is not enough. setup-aep.sh creates these objects
    # with a CLIENT-side `kubectl apply`, which leaves a
    # last-applied-configuration annotation; Helm's server-side apply migrates
    # that into a "kubectl-client-side-apply" field manager owning the spec, and
    # the install then dies with
    #
    #   conflict with "kubectl-client-side-apply": .spec.promotionPaths
    #
    # Removing the annotation alone does NOT fix it: once that field manager
    # exists in metadata.managedFields it outlives the annotation it came from.
    # `managedFields: [{}]` is Kubernetes' documented reset for exactly this —
    # it drops every recorded owner so the next apply establishes ownership
    # cleanly. The object's spec is untouched; only the bookkeeping is cleared.
    kubectl annotate "$kind" default -n default \
        kubectl.kubernetes.io/last-applied-configuration- >/dev/null 2>&1 || true
    kubectl patch "$kind" default -n default --type=merge \
        -p '{"metadata":{"managedFields":[{}]}}' >/dev/null 2>&1 || true
    echo "   ✅ ${kind}/default ownership stamped for adoption"
done

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
# --force-conflicts: this release is DELIBERATELY taking over
# Environment/default and DeploymentPipeline/default from whoever wrote them
# last (see step 3). Helm's server-side apply refuses that by default, and the
# owner it names shifts depending on how the object got there —
# "kubectl-client-side-apply" for one AEP created, "before-first-apply" for one
# whose field management was reset. Forcing is the point of the hand-over, not
# a workaround for a surprise: the chart's defaults — one environment, one
# promotion path, both named `default` — are exactly what should end up there.
#
# --reset-values: an upgrade that is given no values re-applies the LAST
# release's values (Helm's documented behaviour, v3 and v4 alike), so a cluster
# whose earlier install passed a promotion order would keep it forever. The
# chart's defaults are the contract here; nothing is meant to be carried over.
#
# The one value passed: the ingress host the chart persists on Environment/default.
# Its default is Agent Manager's own `am-gateway.localhost`, and every component
# route in the shared environment — AEP's included — is then built as
# `<...>.am-gateway.localhost`. Inside the cluster that name is NXDOMAIN (Agent
# Manager's CoreDNS rewrite targets host.k3d.internal, which the `.:53` server
# cannot follow — see ensure_openchoreo_localhost_in_coredns), so a runner
# validating a deployed app cannot reach it. Mirroring the ClusterDataPlane's
# host keeps the environment on the platform's data-plane hostname, the one
# AEP's CoreDNS rewrite already resolves to the gateway Service, and the one the
# environment used before the chart owned it.
DP_INGRESS_HOST="$(kubectl get clusterdataplane default --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{.spec.gateway.ingress.external.http.host}' 2>/dev/null || true)"
DP_INGRESS_HOST="${DP_INGRESS_HOST:-openchoreoapis.localhost}"
helm upgrade --install amp-platform-resources \
    "${AMP_REGISTRY}/wso2-amp-platform-resources-extension" \
    --version "${AMP_VERSION}" \
    --namespace default --kube-context "$CLUSTER_CONTEXT" \
    --force-conflicts --reset-values \
    --set-string "environment.gateway.http.host=${DP_INGRESS_HOST}" \
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
# Their in-cluster siblings move too. The chart defaults every in-cluster
# address to its OWN Thunder release (amp-thunder-extension-service.amp-thunder),
# but the platform IdP is neutral infrastructure with its own name (env.sh
# THUNDER_RELEASE / THUNDER_NS), so each of them is set from THUNDER_INTERNAL_URL
# — a chart left on its default here dials a Service that does not exist:
#
#   keyManager.jwksUrl     where amp-api fetches the IdP's signing keys
#   oidc.tokenUrl          where amp-api mints its own client-credentials tokens
#   thunder.resolveToHost  the host:port amp-api actually dials for admin calls
#                          (baseURL's host is still sent as the Host header)
#
# Helm accepts an unknown --set path without complaint, so every one of these
# paths was verified by rendering the chart and grepping for the old address
# (none left) and the new one. Repeat that check when bumping AMP_VERSION.
#
# thunderHostBaseDomain is deliberately LEFT at amp.localhost: it builds the
# per-environment Thunder hostnames, and that tier is not shared.
# AMP_API_IMAGE_TAG (and optionally AMP_API_IMAGE_REPOSITORY), when set, run a
# locally built amp-api instead of the release image — for exercising an
# unreleased Agent Manager change against this cluster. The image has to be in
# the k3d node already (`k3d image import <ref> -c ${CLUSTER_NAME}`); the chart
# pulls IfNotPresent, so a tag that exists nowhere else would otherwise hang in
# ImagePullBackOff. The migration Job runs the same binary, so it gets the same
# image.
amp_image_args=()
if [ -n "${AMP_API_IMAGE_TAG:-}" ]; then
    echo "   ℹ️  AMP_API_IMAGE_TAG set — amp-api runs ${AMP_API_IMAGE_REPOSITORY:-ghcr.io/wso2/amp-api}:${AMP_API_IMAGE_TAG}"
    amp_image_args=(
        --set "agentManagerService.image.tag=${AMP_API_IMAGE_TAG}"
        --set "dbMigration.image.tag=${AMP_API_IMAGE_TAG}"
    )
    if [ -n "${AMP_API_IMAGE_REPOSITORY:-}" ]; then
        amp_image_args+=(
            --set "agentManagerService.image.repository=${AMP_API_IMAGE_REPOSITORY}"
            --set "dbMigration.image.repository=${AMP_API_IMAGE_REPOSITORY}"
        )
    fi
fi
helm upgrade --install amp "${AMP_REGISTRY}/wso2-agent-manager" \
    --version "${AMP_VERSION}" \
    --namespace "$AMP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    ${amp_image_args[@]+"${amp_image_args[@]}"} \
    --set console.config.instrumentationUrl="http://default-default.gateway.localhost:19080/otel" \
    --set agentManagerService.config.amObserverPublicURL="http://traces.amp.localhost:11080" \
    --set "agentManagerService.config.keyManager.issuer=${PUBLIC_THUNDER_URL}" \
    --set "agentManagerService.config.thunder.baseURL=${PUBLIC_THUNDER_URL}" \
    --set "console.config.auth.baseUrl=${PUBLIC_THUNDER_URL}" \
    --set "agentManagerService.config.keyManager.jwksUrl=${THUNDER_INTERNAL_JWKS_URL}" \
    --set "agentManagerService.config.oidc.tokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --set "agentManagerService.config.thunder.resolveToHost=${THUNDER_SVC_HOST}:8090" \
    --timeout 30m
echo "⏳ Waiting for Agent Manager..."
kubectl rollout status statefulset/amp-postgresql -n "$AMP_NS" --context "$CLUSTER_CONTEXT" --timeout=600s
kubectl wait -n "$AMP_NS" --context "$CLUSTER_CONTEXT" \
    --for=condition=available --timeout=600s deployment/amp-api deployment/amp-console
echo "   ✅ amp-api + amp-console ready"

# THUNDER_RELEASE_PREFIX, when set, is the leading segment of every environment
# Thunder's release name (thunder-naming.sh; the platform's target is `thunder`,
# see design/two-tier-thunder.md). amp-api derives the same names to find those
# instances, so it has to be told the same prefix — IDP_RELEASE_PREFIX, read by
# the agent-manager build that carries the configurable prefix. The published
# ${AMP_VERSION} chart schema predates that value, so it cannot go through
# --set; it is set on the Deployment after the install. A release image that
# does not read the variable ignores it, and its instance lookups then fail
# against renamed releases — which is the upstream ask this knob stands in for.
if [ -n "${THUNDER_RELEASE_PREFIX:-}" ]; then
    echo "   ℹ️  THUNDER_RELEASE_PREFIX=${THUNDER_RELEASE_PREFIX} — telling amp-api (IDP_RELEASE_PREFIX)"
    kubectl set env deployment/amp-api -n "$AMP_NS" --context "$CLUSTER_CONTEXT" \
        "IDP_RELEASE_PREFIX=${THUNDER_RELEASE_PREFIX}" >/dev/null
    kubectl rollout status deployment/amp-api -n "$AMP_NS" --context "$CLUSTER_CONTEXT" --timeout=300s
fi

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
# Ports and TLS are Agent Manager's own values for this gateway
# (single-cluster/values-op.yaml there): 11080/11085 are what k3d publishes for
# it, and TLS is off, matching every other gateway in this local setup.
#
# `enabled=true` alone is not enough. The chart defaults tls.enabled to true
# with an EMPTY certificateRefs, and enabling the gateway without disabling TLS
# renders an HTTPS listener whose certificateRefs serialises as null — which the
# Gateway API CRD then rejects outright:
#   spec.listeners[1].tls.certificateRefs in body must be of type array: "null"
helm upgrade observability-plane \
    oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane \
    --namespace "$OBS_NS" --kube-context "$CLUSTER_CONTEXT" \
    --version "${OPENCHOREO_VERSION}" \
    --reuse-values \
    --set gateway.enabled=true \
    --set gateway.httpPort=11080 \
    --set gateway.httpsPort=11085 \
    --set gateway.tls.enabled=false \
    --force-conflicts --timeout 10m

# auth.issuer defaults to the chart's own thunder.amp.localhost. It is what
# amp-observer validates `iss` against, so it has to name the IdP this
# deployment actually publishes — same move as the three values on the
# agent-manager chart above. Its two in-cluster addresses (where the observer
# mints its own token, and where it fetches the signing keys) default to the
# chart's own Thunder release and move to the platform IdP for the same reason
# as the agent-manager chart's (step 7).
helm upgrade --install amp-observability-traces \
    "${AMP_REGISTRY}/wso2-amp-observability-extension" \
    --version "${AMP_VERSION}" \
    --namespace "$OBS_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "amObserver.auth.issuer=${PUBLIC_THUNDER_URL}" \
    --set "amObserver.auth.jwksUrl=${THUNDER_INTERNAL_JWKS_URL}" \
    --set "amObserver.observer.idpTokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
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
# The eval job mints its publisher token at the platform IdP, and the same
# NetworkPolicy that scopes its egress allows the IdP by NAMESPACE. Both
# default to the chart's own Thunder release; both follow env.sh here. Missing
# the namespace one is the silent failure: the token request is simply denied
# egress and the job times out with nothing naming the policy.
helm upgrade --install amp-evaluation-extension \
    "${AMP_REGISTRY}/wso2-amp-evaluation-extension" \
    --version "${AMP_VERSION}" \
    --namespace "$WP_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    ${eval_args[@]+"${eval_args[@]}"} \
    --set "ampEvaluation.publisher.idpTokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --set "networkPolicy.evaluationJob.idp.namespace=${THUNDER_NS}" \
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
