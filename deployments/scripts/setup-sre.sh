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

# Wires the OpenChoreo SRE agent's alert → RCA → AE issue handoff onto a
# cluster built by setup-env-for-aectl.sh and provisioned by
# `aectl platform install`. make dev-env runs it unless WITH_SRE=0 or
# WITH_OBSERVABILITY=0.
#
#   bash deployments/scripts/setup-sre.sh
#
# The in-cluster pieces are all aectl's and the platform chart's; this script
# only runs them in the order the handoff needs. See
# docs/developer-guide/sre-handoff-runbook.md.
#
#   1. the shared handoff bearer at aep/aep-mcp-token, generated once. Re-runs
#      keep it: aep-api and aep-mcp-server read it from one Secret at pod start,
#      so rotating it here would leave running pods on the old value.
#   2. `aectl platform update --set sreHandoff.enabled=true`, which wires that
#      bearer into both services and locks aep-mcp-server's ingress to the SRE
#      agent's pods (docs/developer-guide/sre-handoff-security.md).
#   3. the observability-alert-rule ClusterTrait the chart's ComponentTypes
#      allow but `aectl platform install` does not apply (deployments/README.md).
#   4. `aectl sre install`, which enables the SRE agent on the observability
#      plane setup-env-for-aectl.sh installed (at that plane's chart version,
#      with the SRE image override), mounts the AE extension, and points the
#      agent at the org's model connection key as saved in the AE Console
#      (an Anthropic key: the SRE image calls Anthropic).
#
# The SRE agent has no key of its own: until the org's model connection is saved
# in the Console, its pod waits for it. Save the key, then re-run this script
# (only step 4 then changes anything).
#
# Every step is idempotent.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-k3d-${CLUSTER_NAME}}"
AEP_NS="${AEP_NS:-wso2-aep}"
OBS_NS="${OBS_NS:-openchoreo-observability-plane}"
AECTL="${AECTL:-$REPO_ROOT/tools/aectl/aectl-skaffold}"
PLATFORM_CHART="${PLATFORM_CHART:-$REPO_ROOT/deployments/helm-charts/platform}"
HANDOFF_SECRET="aep-sre-handoff-secrets"
ALERT_RULE_TRAIT="$REPO_ROOT/deployments/manifests/api-platform/observability-alert-rule-trait.yaml"

kubectl() { command kubectl --context "$CLUSTER_CONTEXT" "$@"; }
fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

echo "============================================"
echo "  SRE agent → AE handoff"
echo "============================================"

# ── Preflight ───────────────────────────────────────────────────────────────
# aectl talks to the current kubeconfig context, not CLUSTER_CONTEXT, so the
# two must agree or the script's checks and aectl's writes hit different
# clusters.
[ "$(command kubectl config current-context 2>/dev/null)" = "$CLUSTER_CONTEXT" ] \
    || fail "current kubectl context is not $CLUSTER_CONTEXT" \
            "kubectl config use-context $CLUSTER_CONTEXT"
[ -x "$AECTL" ] || fail "$AECTL not found" "make dev-env builds it (cd tools/aectl && go build -o aectl-skaffold .)"
kubectl get deployment aep-api -n "$AEP_NS" &>/dev/null \
    || fail "aep-api is not installed in $AEP_NS" "run aectl platform install first (make dev-env)"
kubectl get deployment observer -n "$OBS_NS" &>/dev/null \
    || fail "no observability plane in $OBS_NS" "setup-env-for-aectl.sh installs it unless WITH_OBSERVABILITY=0"
command -v openssl &>/dev/null || fail "openssl is required to generate the handoff bearer"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
chmod 700 "$TMP_DIR"

# import_secret <path> <value>: `aectl platform secret import` reads the value
# from a file so it never appears in the process list.
import_secret() {
    local file="$TMP_DIR/value"
    (umask 077 && printf '%s' "$2" > "$file")
    "$AECTL" platform secret import --path "$1" --value-file "$file"
    rm -f "$file"
}

# ── 1. Shared handoff bearer ───────────────────────────────────────────────
if kubectl get secret "$HANDOFF_SECRET" -n "$AEP_NS" &>/dev/null; then
    echo "🔐 Keeping the existing handoff bearer (aep/aep-mcp-token)"
else
    echo "🔐 Generating the handoff bearer at aep/aep-mcp-token"
    import_secret aep/aep-mcp-token "$(openssl rand -hex 32)"
fi

# ── 2. Enable the chart's sreHandoff block ─────────────────────────────────
echo "⚙️  Enabling sreHandoff on the platform release"
"$AECTL" platform update --namespace "$AEP_NS" --platform-chart "$PLATFORM_CHART" \
    --set sreHandoff.enabled=true

echo "⏳ Waiting for $HANDOFF_SECRET to sync"
for _ in $(seq 1 60); do
    kubectl get secret "$HANDOFF_SECRET" -n "$AEP_NS" &>/dev/null && break
    sleep 2
done
kubectl get secret "$HANDOFF_SECRET" -n "$AEP_NS" &>/dev/null \
    || fail "$HANDOFF_SECRET did not sync" "kubectl -n $AEP_NS describe externalsecret $HANDOFF_SECRET"
kubectl rollout status deployment/aep-api -n "$AEP_NS" --timeout=300s
kubectl rollout status deployment/aep-mcp-server -n "$AEP_NS" --timeout=300s

# ── 3. Alert-rule trait ────────────────────────────────────────────────────
echo "📐 Applying the observability-alert-rule ClusterTrait"
kubectl apply -f "$ALERT_RULE_TRAIT"

# ── 4. SRE agent on the observability plane ─────────────────────────────────
echo "🤖 Running aectl sre install"
"$AECTL" sre install --namespace "$AEP_NS" --obs-namespace "$OBS_NS" --assets-root "$REPO_ROOT"

echo ""
echo "✅ SRE handoff wired. Runbook: docs/developer-guide/sre-handoff-runbook.md"
