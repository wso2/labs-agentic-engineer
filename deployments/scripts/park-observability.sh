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

# scripts/park-observability.sh — park (scale to zero) or restore the heavy
# workloads of the observability plane, without uninstalling anything.
#
# Usage: bash scripts/park-observability.sh [down|up|status]     (default: status)
#
# Why this exists. setup.sh installs the observability plane for every cluster,
# because Agent Manager's charts install against it (its tracing module's setup
# Job writes OpenSearch index templates, its console reads traces and metrics
# from it). Installed and RUNNING, that plane is the single largest consumer on
# a laptop cluster: OpenSearch, Prometheus, Alertmanager, the RCA agent, the
# shippers and the query adapters add up to roughly 2 GiB of requests and over
# 3 GiB of limits on an 8 GiB Colima VM. Most local work never reads a trace or
# a metric. So setup.sh installs everything and then parks the heavy half here
# as its last step; `up` brings it back when a log archive, a trace view or the
# alert→RCA pipeline is wanted. There is no setup flag for this on purpose:
# the plane is always installed, and this script is the one switch.
#
# What "parked" costs, stated plainly:
#   - The AEP console's log ARCHIVE for finished cycles (observer → OpenSearch)
#     reads as "logs unavailable". Live pod tails are unaffected: they go
#     through the OpenChoreo API, not this plane.
#   - Agent Manager's traces, metrics and logs views are empty.
#   - No alert is evaluated, so the alert → RCA → coding-agent handoff is off.
#   - Nothing is lost on `up`: OpenSearch keeps its PVC, and every chart, CR,
#     ConfigMap patch and HTTPRoute the setup scripts made stays in place.
#
# What stays running while parked: observer, controller-manager, the cluster
# agent, the plane's gateway and amp-observer. They are small, and they are
# what the two consoles actually call — with the stores parked they answer "no
# data" instead of refusing the connection.
#
# Mechanics:
#   - Deployments and StatefulSets: `kubectl scale --replicas=0`, with the
#     previous replica count remembered in an annotation so `up` restores what
#     was there rather than assuming 1.
#   - The Prometheus operator is parked FIRST on the way down and restored
#     FIRST on the way up: it owns the Prometheus/Alertmanager StatefulSets and
#     would otherwise scale them straight back to their CR's replica count.
#   - Fluent Bit is a DaemonSet, which has no replica count. It is parked with a
#     nodeSelector no node carries and restored by removing that selector.
#   - Absent objects are skipped and named, so the same script serves a cluster
#     where Agent Manager has been torn down (no metrics or tracing modules).
#
# Idempotent: `down` on a parked plane and `up` on a running one are no-ops.
# A later `helm upgrade` of one of these charts (a setup re-run) resets the
# replica counts to the chart's; setup.sh re-parks at its end for that reason.
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

NS="openchoreo-observability-plane"
ACTION="${1:-status}"
PARKED_ANNOTATION="aep.io/parked-replicas"
PARK_NODE_LABEL="aep.io/parked"

# The RCA agent's Deployment is named once, in env.sh (it was renamed
# ai-rca-agent -> sre-agent in observability-plane 1.2.0).
RCA_DEPLOYMENT="${RCA_DEPLOYMENT:-sre-agent}"

# Order is the restore order: the operator before the StatefulSets it owns.
PARK_DEPLOYMENTS=(
    prometheus-operator
    kube-state-metrics
    metrics-adapter-prometheus
    logs-adapter-opensearch
    tracing-adapter-opensearch
    opentelemetry-collector
    "$RCA_DEPLOYMENT"
)
PARK_STATEFULSETS=(
    opensearch-master
    prometheus-openchoreo-observability
    alertmanager-openchoreo-observability
)
PARK_DAEMONSETS=(
    fluent-bit
)

k() { kubectl --context "$CLUSTER_CONTEXT" -n "$NS" "$@"; }

exists() { k get "$1" "$2" &>/dev/null; }

park_scalable() {
    local kind="$1" name="$2"
    if ! exists "$kind" "$name"; then
        echo "   ⏭️  $kind/$name not installed"
        return
    fi
    local replicas
    replicas="$(k get "$kind" "$name" -o jsonpath='{.spec.replicas}')"
    if [ "${replicas:-0}" = "0" ]; then
        echo "   ✅ $kind/$name already at 0"
        return
    fi
    k annotate "$kind" "$name" "${PARKED_ANNOTATION}=${replicas}" --overwrite >/dev/null
    k scale "$kind" "$name" --replicas=0 >/dev/null
    echo "   ⏸️  $kind/$name ${replicas} → 0"
}

restore_scalable() {
    local kind="$1" name="$2"
    if ! exists "$kind" "$name"; then
        echo "   ⏭️  $kind/$name not installed"
        return
    fi
    local replicas remembered
    replicas="$(k get "$kind" "$name" -o jsonpath='{.spec.replicas}')"
    remembered="$(k get "$kind" "$name" -o jsonpath="{.metadata.annotations.aep\.io/parked-replicas}")"
    if [ "${replicas:-0}" != "0" ]; then
        echo "   ✅ $kind/$name already running (${replicas})"
        return
    fi
    k scale "$kind" "$name" --replicas="${remembered:-1}" >/dev/null
    k annotate "$kind" "$name" "${PARKED_ANNOTATION}-" >/dev/null 2>&1 || true
    echo "   ▶️  $kind/$name 0 → ${remembered:-1}"
}

park_daemonset() {
    local name="$1"
    if ! exists daemonset "$name"; then
        echo "   ⏭️  daemonset/$name not installed"
        return
    fi
    local current
    current="$(k get daemonset "$name" -o jsonpath="{.spec.template.spec.nodeSelector.aep\.io/parked}")"
    if [ "$current" = "true" ]; then
        echo "   ✅ daemonset/$name already parked"
        return
    fi
    k patch daemonset "$name" --type=merge \
        -p "{\"spec\":{\"template\":{\"spec\":{\"nodeSelector\":{\"${PARK_NODE_LABEL}\":\"true\"}}}}}" >/dev/null
    echo "   ⏸️  daemonset/$name → no node matches"
}

restore_daemonset() {
    local name="$1"
    if ! exists daemonset "$name"; then
        echo "   ⏭️  daemonset/$name not installed"
        return
    fi
    local current
    current="$(k get daemonset "$name" -o jsonpath="{.spec.template.spec.nodeSelector.aep\.io/parked}")"
    if [ "$current" != "true" ]; then
        echo "   ✅ daemonset/$name already running"
        return
    fi
    # JSON-pointer escaping: "/" inside the key becomes "~1".
    k patch daemonset "$name" --type=json \
        -p '[{"op":"remove","path":"/spec/template/spec/nodeSelector/aep.io~1parked"}]' >/dev/null
    echo "   ▶️  daemonset/$name → every node"
}

status() {
    echo "Observability plane workloads in $NS (parked = spec.replicas 0 / no matching node):"
    printf '   %-12s %-40s %-9s %-6s %s\n' KIND NAME REPLICAS READY STATE
    local kind name replicas ready state
    for name in "${PARK_DEPLOYMENTS[@]}"; do
        exists deployment "$name" || continue
        replicas="$(k get deployment "$name" -o jsonpath='{.spec.replicas}')"
        ready="$(k get deployment "$name" -o jsonpath='{.status.readyReplicas}')"
        [ "${replicas:-0}" = "0" ] && state=parked || state=running
        printf '   %-12s %-40s %-9s %-6s %s\n' deployment "$name" "${replicas:-0}" "${ready:-0}" "$state"
    done
    for name in "${PARK_STATEFULSETS[@]}"; do
        exists statefulset "$name" || continue
        replicas="$(k get statefulset "$name" -o jsonpath='{.spec.replicas}')"
        ready="$(k get statefulset "$name" -o jsonpath='{.status.readyReplicas}')"
        [ "${replicas:-0}" = "0" ] && state=parked || state=running
        printf '   %-12s %-40s %-9s %-6s %s\n' statefulset "$name" "${replicas:-0}" "${ready:-0}" "$state"
    done
    for name in "${PARK_DAEMONSETS[@]}"; do
        exists daemonset "$name" || continue
        ready="$(k get daemonset "$name" -o jsonpath='{.status.numberReady}')"
        if [ "$(k get daemonset "$name" -o jsonpath="{.spec.template.spec.nodeSelector.aep\.io/parked}")" = "true" ]; then
            state=parked
        else
            state=running
        fi
        printf '   %-12s %-40s %-9s %-6s %s\n' daemonset "$name" - "${ready:-0}" "$state"
    done
}

kubectl cluster-info --context "$CLUSTER_CONTEXT" --request-timeout=5s &>/dev/null || {
    echo "❌ Cluster '$CLUSTER_CONTEXT' is not reachable." >&2
    exit 1
}
if ! kubectl --context "$CLUSTER_CONTEXT" get namespace "$NS" &>/dev/null; then
    echo "ℹ️  Namespace $NS does not exist — the observability plane is not installed; nothing to $ACTION."
    exit 0
fi

case "$ACTION" in
    down)
        echo "⏸️  Parking the heavy observability workloads in $NS"
        for name in "${PARK_DEPLOYMENTS[@]}"; do park_scalable deployment "$name"; done
        for name in "${PARK_STATEFULSETS[@]}"; do park_scalable statefulset "$name"; done
        for name in "${PARK_DAEMONSETS[@]}"; do park_daemonset "$name"; done
        echo "✅ Parked. Restore with: bash scripts/park-observability.sh up"
        ;;
    up)
        echo "▶️  Restoring the observability workloads in $NS"
        for name in "${PARK_DEPLOYMENTS[@]}"; do restore_scalable deployment "$name"; done
        for name in "${PARK_STATEFULSETS[@]}"; do restore_scalable statefulset "$name"; done
        for name in "${PARK_DAEMONSETS[@]}"; do restore_daemonset "$name"; done
        echo "⏳ OpenSearch takes a few minutes to become ready; the adapters recover on their own once it does."
        ;;
    status)
        status
        ;;
    *)
        echo "Usage: $0 [down|up|status]" >&2
        exit 2
        ;;
esac
