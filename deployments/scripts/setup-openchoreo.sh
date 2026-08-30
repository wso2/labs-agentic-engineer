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

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

echo "=== Installing OpenChoreo ==="

kubectl cluster-info --context $CLUSTER_CONTEXT &>/dev/null || {
    echo "❌ Cluster '$CLUSTER_CONTEXT' not running. Run: ./setup-k3d.sh && ./setup-prerequisites.sh"
    exit 1
}
kubectl config use-context $CLUSTER_CONTEXT

# Load PUBLIC_THUNDER_URL / PUBLIC_CONSOLE_URL so values-cp.yaml can be
# rendered with the user's chosen public URLs.
load_public_urls "$SCRIPT_DIR/../.env"
RENDERED_CP_VALUES="$(render_values_file "$SCRIPT_DIR/../single-cluster/values-cp.yaml")"
trap 'rm -f "$RENDERED_CP_VALUES"' EXIT

# ============================================================================
# Control Plane
# ============================================================================
echo "1️⃣  Control Plane"

# Create backstage-secrets before installing the control plane.
# The Backstage pod references this secret for its backend-secret, OAuth client-secret,
# and Jenkins API key. Without it, the pod stays in CreateContainerConfigError.
if ! kubectl get secret backstage-secrets -n openchoreo-control-plane &>/dev/null; then
    echo "🔑 Creating backstage-secrets..."
    kubectl create namespace openchoreo-control-plane --dry-run=client -o yaml | kubectl apply -f -
    kubectl create secret generic backstage-secrets \
        -n openchoreo-control-plane \
        --from-literal=backend-secret="$(head -c 32 /dev/urandom | base64)" \
        --from-literal=client-secret="backstage-portal-secret" \
        --from-literal=jenkins-api-key="placeholder-not-in-use"
fi

# A fresh control-plane install races the controller-manager's validating
# webhook: the OC chart applies webhook-gated CRs (ClusterAuthzRoleBinding)
# in the SAME helm pass that first creates the controller-manager pod, so on a
# cold image pull the webhook service has no endpoints yet and the install errors
# ("no endpoints available for service controller-manager-webhook-service").
# helm then leaves the release in `failed` — a bare existence check would wrongly
# report "already installed" and skip re-applying those CRs, leaving the cluster
# half-configured. So: treat ONLY a `deployed` release as installed, and retry
# the install once the webhook is ready (helm reconciles the partial release on
# the second pass — the documented recovery path).

# Bumping OPENCHOREO_VERSION on a cluster that already carries an older release
# needs the chart's CRDs re-applied by hand (helm upgrade skips crds/) — see
# sync_chart_crds in utils.sh. No-op on a first install, so it runs
# unconditionally rather than trying to detect the upgrade case.
sync_chart_crds openchoreo-control-plane "${OPENCHOREO_VERSION}"

cp_status="$(helm status openchoreo-control-plane -n openchoreo-control-plane \
    --kube-context ${CLUSTER_CONTEXT} -o json 2>/dev/null \
    | grep -o '"status":"[a-z-]*"' | head -1)" || true
if [ "$cp_status" = '"status":"deployed"' ]; then
    echo "⏭️  Already installed"
else
    echo "📦 Installing OpenChoreo Control Plane (may take up to 10 minutes)..."
    cp_attempt=0
    until helm upgrade --install openchoreo-control-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-control-plane \
        --version ${OPENCHOREO_VERSION} \
        --namespace openchoreo-control-plane --create-namespace \
        --values "$RENDERED_CP_VALUES"; do
        cp_attempt=$((cp_attempt + 1))
        if [ "$cp_attempt" -ge 3 ]; then
            echo "❌ Control Plane install failed after ${cp_attempt} attempts"
            exit 1
        fi
        echo "⚠️  Control-plane install attempt ${cp_attempt} hit the controller-manager"
        echo "    validating-webhook race; waiting for the webhook to become ready, then"
        echo "    retrying (helm reconciles the partially-applied release)..."
        kubectl wait -n openchoreo-control-plane --for=condition=available --timeout=300s \
            deployment/controller-manager || true
        # Block until the validating-webhook service actually has endpoints.
        for _ in $(seq 1 30); do
            if [ -n "$(kubectl get endpoints controller-manager-webhook-service \
                -n openchoreo-control-plane \
                -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)" ]; then
                break
            fi
            sleep 5
        done
    done
fi
echo "⏳ Waiting for Control Plane (core components)..."
kubectl wait -n openchoreo-control-plane --for=condition=available --timeout=300s \
    deployment/controller-manager \
    deployment/openchoreo-api \
    deployment/cluster-gateway \
    deployment/gateway-default
echo "✅ Control Plane ready"
echo ""

# ============================================================================
# Data Plane
# ============================================================================
echo "2️⃣  Data Plane"
# Only a `deployed` release counts as installed — a `failed`/`pending` release
# falls through to re-drive `helm upgrade --install` (self-healing).
if helm_release_deployed openchoreo-data-plane openchoreo-data-plane; then
    echo "⏭️  Already installed"
else
    echo "📦 Installing OpenChoreo Data Plane..."
    sync_chart_crds openchoreo-data-plane "${OPENCHOREO_VERSION}"
    create_plane_cert_resources openchoreo-data-plane
    helm upgrade --install openchoreo-data-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-data-plane \
        --version ${OPENCHOREO_VERSION} \
        --namespace openchoreo-data-plane --create-namespace \
        --values "${SCRIPT_DIR}/../single-cluster/values-dp.yaml"
fi
echo "⏳ Waiting for Data Plane..."
kubectl wait -n openchoreo-data-plane --for=condition=available --timeout=300s deployment --all
echo "✅ Data Plane ready"

# Register Data Plane
if ! kubectl get clusterdataplane default -n default &>/dev/null; then
    echo "🔗 Registering Data Plane..."
    local_ca=$(kubectl get secret cluster-agent-tls -n openchoreo-data-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    register_data_plane "$local_ca" "default" "default"
fi
echo ""

# ============================================================================
# Platform IdP (ThunderID) + the entitlement claim that follows it
# ============================================================================
echo "3️⃣  Platform IdP"
bash "$SCRIPT_DIR/setup-thunder.sh"

# ThunderID 1.0.0 puts a client_credentials token's subject in the `client_id`
# claim, not the `sub` that OpenChoreo's service-account entitlement mapping
# defaults to. Every service account 403s until this is switched, so it moves
# in the SAME step as the IdP itself — a cluster with the new Thunder and the
# old claim is not a state anyone should be able to reach.
#
# Server-side apply under Helm's own field manager, not `kubectl patch`: patch
# claims the field under a `kubectl-patch` manager, and the NEXT
# `helm upgrade --install` of the control plane then fails outright with
# "conflict with kubectl-patch".
echo "🔧 Switching the service-account entitlement claim to client_id..."
if kubectl get configmap openchoreo-api-config -n openchoreo-control-plane &>/dev/null; then
    patched_api_config="$(kubectl get configmap openchoreo-api-config \
        -n openchoreo-control-plane -o yaml \
        | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g")"
    if ! echo "$patched_api_config" | grep -q "claim: client_id"; then
        echo "❌ openchoreo-api-config carries no entitlement claim to switch" >&2
        exit 1
    fi
    echo "$patched_api_config" | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null
    kubectl rollout restart deployment/openchoreo-api -n openchoreo-control-plane
    kubectl rollout status deployment/openchoreo-api -n openchoreo-control-plane --timeout=120s
    echo "   ✓ openchoreo-api-config"
else
    echo "❌ openchoreo-api-config not found — the control plane is not installed" >&2
    exit 1
fi

# The bindings shipped by the control-plane chart are keyed on `sub` for the
# same reason and need the same move. AEP's own bindings are authored directly
# with `client_id` (setup-aep.sh), so this loop is a no-op for them.
for binding in $(kubectl get clusterauthzrolebindings.openchoreo.dev \
    -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    claim="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" \
        -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    [ "$claim" = "sub" ] || continue
    # 2>/dev/null: the FIRST server-side apply on an object Helm created with
    # client-side apply performs a field-manager migration, and the redundant
    # re-apply that follows it races the controller's own write. kubectl reports
    # that as a non-fatal warning and the migration itself still lands — but the
    # warning is indistinguishable from a real failure, so the claim is verified
    # below rather than trusted.
    kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o yaml \
        | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g" \
        | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null 2>&1 || true
    now="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" \
        -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    if [ "$now" != "client_id" ]; then
        echo "❌ ClusterAuthzRoleBinding ${binding} is still on claim '${now}'." >&2
        echo "   Every service account bound through it would 403 against ThunderID." >&2
        exit 1
    fi
    echo "   ✓ ${binding}"
done
echo "✅ Entitlement claim is client_id"

# The control-plane chart uses security.oidc.tokenUrl for BOTH the OpenChoreo
# API metadata (browser-facing) and Backstage's token exchange (server-side).
# The browser URL is not resolvable from inside the cluster, so Backstage gets
# the in-cluster service address instead.
echo "🔧 Patching Backstage token URL for in-cluster resolution..."
kubectl set env deployment/backstage \
    -n openchoreo-control-plane --context "${CLUSTER_CONTEXT}" \
    OPENCHOREO_AUTH_TOKEN_URL="${THUNDER_INTERNAL_URL}/oauth2/token"
kubectl rollout status deployment/backstage -n openchoreo-control-plane \
    --context "${CLUSTER_CONTEXT}" --timeout=120s
echo "✅ Backstage token URL patched"
echo ""

# ============================================================================
# Workflow Plane (optional, for builds)
# ============================================================================
echo "4️⃣  Workflow Plane"
# Only a `deployed` release counts as installed. This is the guard that caused
# the reported setup failure: a cert-manager webhook TLS-handshake timeout left
# this release `failed` (its cluster-agent-tls cert never created, so the
# cluster-agent pod hung in ContainerCreating and the wait below timed out), and
# the old existence check skipped the reinstall that would have recreated it.
if helm_release_deployed openchoreo-workflow-plane openchoreo-workflow-plane; then
    echo "⏭️  Already installed"
else
    echo "📦 Installing Workflow Plane..."
    sync_chart_crds openchoreo-workflow-plane "${OPENCHOREO_VERSION}"
    create_plane_cert_resources openchoreo-workflow-plane

    # Pre-fetched via fetch_gh_raw (PAT-aware, retried) — a direct helm
    # --values URL read hits the anonymous raw.githubusercontent.com throttle
    # with no retry (a 429 here aborted a bring-up once).
    REGISTRY_VALUES_FILE="$(mktemp)"
    fetch_gh_raw "https://raw.githubusercontent.com/openchoreo/openchoreo/v${OPENCHOREO_VERSION}/install/k3d/single-cluster/values-registry.yaml" "$REGISTRY_VALUES_FILE"
    helm upgrade --install registry docker-registry \
        --repo https://twuni.github.io/docker-registry.helm \
        --namespace openchoreo-workflow-plane --create-namespace \
        --values "$REGISTRY_VALUES_FILE"
    rm -f "$REGISTRY_VALUES_FILE"

    helm upgrade --install openchoreo-workflow-plane \
        oci://ghcr.io/openchoreo/helm-charts/openchoreo-workflow-plane \
        --version ${OPENCHOREO_VERSION} \
        --namespace openchoreo-workflow-plane --create-namespace
fi
echo "⏳ Waiting for Workflow Plane..."
kubectl wait -n openchoreo-workflow-plane --for=condition=available --timeout=300s deployment --all
echo "✅ Workflow Plane ready"

if ! kubectl get clusterworkflowplane default -n default &>/dev/null; then
    echo "🔗 Registering Workflow Plane..."
    wp_ca=$(kubectl get secret cluster-agent-tls -n openchoreo-workflow-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    register_workflow_plane "$wp_ca" "default" "default"
fi
echo ""

echo "✅ OpenChoreo installation complete!"
echo ""
echo "📊 Pod Status:"
for ns in openchoreo-control-plane openchoreo-data-plane openchoreo-workflow-plane "${THUNDER_NS}"; do
    echo "--- $ns ---"
    kubectl get pods -n $ns --no-headers 2>/dev/null || echo "  (no pods)"
    echo ""
done
