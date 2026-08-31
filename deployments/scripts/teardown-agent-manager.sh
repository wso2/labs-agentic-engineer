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

# Removes Agent Manager from a cluster AEP keeps running — what makes
# ENABLE_AGENT_MANAGER genuinely reversible rather than a one-way door.
#
# ── What this deliberately does NOT remove ──────────────────────────────────
#
#   * The platform Thunder (amp-thunder). It is AEP's IdP too — it is installed
#     unconditionally for exactly this reason, so that flipping the flag never
#     invalidates a login.
#   * OpenChoreo, the gateway operator, External Secrets. Shared base.
#   * The observability plane and the logs module. AEP uses them under
#     ENABLE_OBSERVABILITY.
#   * DeploymentPipeline/default. Removing it would break AEP's project
#     creation, which relies on OpenChoreo defaulting to that exact name.
#     `helm uninstall` of the platform-resources release deletes it, so it is
#     re-applied afterwards with AEP's own promotion path.
#
# It DOES remove the tracing and metrics modules, which only Agent Manager
# needs, and every per-environment Thunder — those belong to the environments
# Agent Manager created and have no meaning without it.

set -uo pipefail   # deliberately NOT -e: teardown is best-effort per resource
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"

echo "============================================"
echo "  Removing the Agent Management Platform"
echo "============================================"

uninstall() {
    local release="$1" ns="$2"
    if helm status "$release" -n "$ns" --kube-context "$CLUSTER_CONTEXT" &>/dev/null; then
        echo "🗑️  ${release} (${ns})"
        helm uninstall "$release" -n "$ns" --kube-context "$CLUSTER_CONTEXT" --wait --timeout 5m >/dev/null 2>&1 \
            || echo "   ⚠️  uninstall reported an error — continuing"
    fi
}

# Namespaces this script must NEVER remove, and never uninstall a release from
# by name-pattern alone. Every one belongs to AEP or to the shared base.
#
# This list is load-bearing. Agent Manager's per-environment gateway releases
# are named `api-platform-<org>-<env>` — but the API Platform OPERATOR also
# creates a child release for AEP's OWN gateway, named `api-platform-default-gw`,
# in openchoreo-data-plane. A name pattern alone matches both, and an earlier
# version of this script uninstalled AEP's gateway and then deleted the entire
# openchoreo-data-plane namespace with it: no cluster-agent, no data plane, and
# every AEP deploy failing with "no agents found for plane dataplane/default".
PROTECTED_NAMESPACES="
default kube-system kube-public kube-node-lease
openchoreo-control-plane openchoreo-data-plane openchoreo-workflow-plane
openchoreo-observability-plane
cert-manager external-secrets openbao cnpg-system temporal
thunder-app-operator-system ${THUNDER_NS}
"

is_protected() {
    local candidate="$1" ns
    for ns in $PROTECTED_NAMESPACES; do
        [ "$candidate" = "$ns" ] && return 0
    done
    return 1
}

echo ""
echo "1️⃣  Per-environment gateways and Thunders"
# Both are named per (org, environment); discover rather than assume `default`,
# so an installation that added environments is fully cleaned up. Anything
# living in a protected namespace is skipped — see PROTECTED_NAMESPACES.
for rel in $(helm list -A -o json --kube-context "$CLUSTER_CONTEXT" 2>/dev/null \
        | python3 -c "
import json,sys
for r in json.load(sys.stdin):
    n = r['name']
    if n.startswith('api-platform-') or n.startswith('amp-thunder-'):
        print(f\"{n}:{r['namespace']}\")
" 2>/dev/null); do
    name="${rel%%:*}"; ns="${rel#*:}"
    [ "$name" = "${THUNDER_RELEASE}" ] && continue   # never the shared platform IdP
    if is_protected "$ns"; then
        echo "   skipping ${name} — lives in ${ns}, which AEP owns"
        continue
    fi
    uninstall "$name" "$ns"
    kubectl delete ns "$ns" --context "$CLUSTER_CONTEXT" --ignore-not-found --wait=false >/dev/null 2>&1
done

echo ""
echo "2️⃣  Agent Manager charts"
uninstall amp-evaluation-extension openchoreo-workflow-plane
uninstall amp-observability-traces openchoreo-observability-plane
uninstall amp wso2-amp
uninstall agent-sandbox openchoreo-data-plane
uninstall amp-platform-resources default

echo ""
echo "3️⃣  Observability modules only Agent Manager needed"
uninstall observability-metrics-prometheus openchoreo-observability-plane
uninstall observability-traces-opensearch openchoreo-observability-plane

echo ""
echo "4️⃣  Restoring AEP's DeploymentPipeline"
# The platform-resources uninstall took it with it — see the header. AEP's own
# promotion path only, now that Agent Manager's environment is gone.
kubectl apply --context "$CLUSTER_CONTEXT" -f - <<'OCEOF' >/dev/null
apiVersion: openchoreo.dev/v1alpha1
kind: DeploymentPipeline
metadata:
  name: default
  namespace: default
spec:
  promotionPaths:
    - sourceEnvironmentRef:
        name: development
      targetEnvironmentRefs: []
OCEOF
echo "   ✅ DeploymentPipeline/default restored (development only)"

echo ""
echo "5️⃣  Namespaces"
# Only Agent Manager's own. The shared ones are in PROTECTED_NAMESPACES.
kubectl delete ns wso2-amp --context "$CLUSTER_CONTEXT" --ignore-not-found --wait=false >/dev/null 2>&1
kubectl delete ns agent-sandbox-system --context "$CLUSTER_CONTEXT" --ignore-not-found --wait=false >/dev/null 2>&1

echo ""
echo "✅ Agent Manager removed. AEP is untouched — the platform IdP, OpenChoreo,"
echo "   the gateway operator and the observability plane all stay."
echo "   Re-install with: ENABLE_AGENT_MANAGER=1 bash scripts/setup-agent-manager.sh"
