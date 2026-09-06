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
#   * The platform IdP (release/namespace THUNDER_RELEASE / THUNDER_NS in
#     env.sh, `platform-idp` by default). It is neutral infrastructure both
#     products log in through, installed unconditionally for exactly this
#     reason, so that flipping the flag never invalidates a login.
#   * OpenChoreo, the gateway operator, External Secrets. Shared base.
#   * The observability plane and the logs module. AEP uses them under
#     ENABLE_OBSERVABILITY.
#   * Environment/default and DeploymentPipeline/default. The environment is
#     the one AEP deploys into, and the pipeline is what OpenChoreo defaults a
#     new project to. Both are Helm-owned by the platform-resources release once
#     Agent Manager is installed (setup-agent-manager.sh step 3), so
#     `helm uninstall` deletes them; both are re-applied afterwards exactly as
#     setup-aep.sh creates them.
#
#   * Per-environment API Platform gateways (`api-platform-<org>-<env>` in
#     namespace `<org>-<env>`) and the environment Thunders they authenticate
#     against. Both tiers became PLATFORM infrastructure with the two-tier
#     design: AEP's own environments get them from setup-aep.sh whether or not
#     Agent Manager is installed, every AEP component's managed API is served by
#     one, and `api-configuration` points every RestApi at the one for its
#     environment. Removing them here would leave every AEP deploy with a
#     RestApi no gateway serves — a 404 on a healthy-looking CR — and no
#     identity tier to validate its tokens against.
#
# It DOES remove the tracing and metrics modules, which only Agent Manager needs.

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
# This list is load-bearing, and it now carries every `<org>-<env>` namespace as
# well. Those hold the per-environment API Platform gateway and are the
# platform's, not Agent Manager's: AEP installs one per environment of its own
# (setup-aep.sh → setup-environment-gateway.sh), and the API Platform OPERATOR
# creates a child release beside each one named `api-platform-<org>-<env>-gw`.
# A name pattern alone matches all of them, and an earlier version of this
# script uninstalled the gateway it matched and then deleted its namespace: no
# cluster-agent, no data plane, and every AEP deploy failing with "no agents
# found for plane dataplane/default".
#
# The last two lines are DISCOVERED, not listed, because an installation that
# added environments has more of them than this file could name:
#
#   * `<org>-<env>` for every OpenChoreo Environment — that environment's
#     API Platform gateway namespace.
#   * every namespace holding a thunder-binding ConfigMap — an environment
#     Thunder AEP has bound to and reads its credential from. An environment
#     Thunder with no binding was created for an environment AEP does not have,
#     which makes it Agent Manager's alone and fair game below.
PROTECTED_NAMESPACES="
default kube-system kube-public kube-node-lease
openchoreo-control-plane openchoreo-data-plane openchoreo-workflow-plane
openchoreo-observability-plane
cert-manager external-secrets openbao cnpg-system temporal
thunder-app-operator-system ${THUNDER_NS}
$(kubectl get environment -A --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}-{.metadata.name} {end}' 2>/dev/null)
$(kubectl get configmap -A --context "$CLUSTER_CONTEXT" -l aep.wso2.com/kind=thunder-binding \
    -o jsonpath='{range .items[*]}{.metadata.namespace} {end}' 2>/dev/null)
"

is_protected() {
    local candidate="$1" ns
    for ns in $PROTECTED_NAMESPACES; do
        [ "$candidate" = "$ns" ] && return 0
    done
    return 1
}

echo ""
echo "1️⃣  Per-environment gateways and Thunders Agent Manager alone created"
# Both are named per (org, environment); discover rather than assume `default`,
# so an installation that added environments is fully cleaned up. Anything
# living in a protected namespace is skipped — see PROTECTED_NAMESPACES, which
# now covers every environment AEP has, in both tiers. What is left here is an
# environment Agent Manager created for itself, which has no meaning without it.
#
# The selection is deliberately narrow, for the reason PROTECTED_NAMESPACES
# exists. Gateways are `api-platform-<org>-<env>`. Environment Thunders are
# `thunder-<org>-<env>` (`amp-thunder-<org>-<env>` before Agent Manager moved
# the prefix — both are matched so an older install is cleaned up too), AND are
# releases of the upstream `thunderid` chart: the platform IdP and the
# thunder-app operator share the `thunder-` prefix and come from other charts,
# so the chart name is what tells an environment Thunder apart, not the prefix.
for rel in $(helm list -A -o json --kube-context "$CLUSTER_CONTEXT" 2>/dev/null \
        | python3 -c "
import json,sys
for r in json.load(sys.stdin):
    n, chart = r['name'], r.get('chart', '')
    is_env_gateway = n.startswith('api-platform-')
    is_env_thunder = ((n.startswith('thunder-') or n.startswith('amp-thunder-'))
                      and chart.startswith('thunderid-'))
    if is_env_gateway or is_env_thunder:
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
echo "4️⃣  Restoring AEP's Environment and DeploymentPipeline"
# The platform-resources uninstall took both with it — see the header. The
# Environment comes back as setup-aep.sh writes it (without the gateway ingress
# Agent Manager's chart persisted on it) and the pipeline with its one promotion
# path. The environment's Thunder, gateway and binding record were never
# touched: their namespaces are in PROTECTED_NAMESPACES, discovered from this
# same Environment before anything was uninstalled — but the record's
# projection onto the Environment CR (its aep.wso2.com/thunder-* annotations)
# went with the object, so setup-environment-thunder.sh re-projects it.
kubectl apply --context "$CLUSTER_CONTEXT" -f - <<'OCEOF' >/dev/null
apiVersion: openchoreo.dev/v1alpha1
kind: Environment
metadata:
  name: default
  namespace: default
spec:
  dataPlaneRef:
    kind: ClusterDataPlane
    name: default
---
apiVersion: openchoreo.dev/v1alpha1
kind: DeploymentPipeline
metadata:
  name: default
  namespace: default
spec:
  promotionPaths:
    - sourceEnvironmentRef:
        name: default
      targetEnvironmentRefs: []
OCEOF
echo "   ✅ Environment/default and DeploymentPipeline/default restored"
bash "$SCRIPT_DIR/setup-environment-thunder.sh" default default >/dev/null \
    && echo "   ✅ binding record re-projected onto Environment/default" \
    || echo "   ⚠️  setup-environment-thunder.sh default default failed — re-run it to re-annotate the Environment"

echo ""
echo "5️⃣  Namespaces"
# Only Agent Manager's own. The shared ones are in PROTECTED_NAMESPACES.
kubectl delete ns wso2-amp --context "$CLUSTER_CONTEXT" --ignore-not-found --wait=false >/dev/null 2>&1
kubectl delete ns agent-sandbox-system --context "$CLUSTER_CONTEXT" --ignore-not-found --wait=false >/dev/null 2>&1

echo ""
echo "✅ Agent Manager removed. AEP is untouched — the platform IdP, OpenChoreo,"
echo "   the gateway operator and the observability plane all stay."
echo "   Re-install with: ENABLE_AGENT_MANAGER=1 bash scripts/setup-agent-manager.sh"
