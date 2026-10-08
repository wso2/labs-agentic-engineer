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

# deployments/scripts/restore-host-k3d-internal.sh — give host.k3d.internal an
# address again, after a node restart has taken it away.
#
# Usage: bash deployments/scripts/restore-host-k3d-internal.sh [apply|check]
#        (default: apply; both are safe to repeat)
#
# ── Why this exists ─────────────────────────────────────────────────────────
#
# Every CoreDNS rewrite on this cluster answers with host.k3d.internal:
# OpenChoreo's own *.openchoreo.localhost key, the *.openchoreoapis.localhost
# key setup-env-for-aectl.sh adds beside it, and the *.amp / *.gateway /
# *.agentmanager / *.am-gateway keys setup-agent-manager.sh merges in. So does
# OpenChoreo's build chain, which addresses the local registry directly as
# host.k3d.internal:10082.
#
# k3d defines that name when it CREATES the cluster, in the `coredns`
# ConfigMap's NodeHosts key. NodeHosts is owned by k3s-supervisor, which
# regenerates it from node addresses every time the node container restarts —
# and regenerating it drops the line k3d added. `k3d cluster start` does not
# put it back; only creating a cluster does. A cluster that has been through a
# host reboot therefore keeps every rewrite and loses the one name they all
# point at.
#
# Nothing fails loudly when that happens. Pods get NXDOMAIN for hostnames that
# resolve perfectly from the host, and it surfaces several systems away: the
# OpenChoreo API cannot fetch Thunder's JWKS, so it rejects every token as
# INVALID_TOKEN, so callers see 401s with no mention of DNS. Builds fail on an
# unreachable registry. setup-environment-aigateway.sh reads an empty
# environment list out of a 500 and reports the environment as unregistered.
#
# This writes the name into `coredns-custom` instead, which no controller
# reconciles, so it survives the next restart as well.
#
# `template`, not `hosts`: the .:53 server block already loads
# `hosts /etc/coredns/NodeHosts`, and CoreDNS accepts one hosts instance per
# block.

set -euo pipefail

MODE="${1:-apply}"
case "$MODE" in
    apply|check) ;;
    *) echo "usage: $(basename "$0") [apply|check]" >&2; exit 2 ;;
esac

CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-k3d-${CLUSTER_NAME}}"
CM_KEY="hostk3dinternal.override"

kubectl() { command kubectl --context "$CLUSTER_CONTEXT" "$@"; }
fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

# The address is the cluster network's gateway — where the Docker host answers
# from inside the cluster, and where k3d publishes the load balancer's ports.
# Read off the network rather than assumed: the subnet is Docker's to choose,
# and a node's own address changes when its container is recreated.
HOST_GW="$(docker network inspect "k3d-${CLUSTER_NAME}" \
    --format '{{range .IPAM.Config}}{{.Gateway}}{{end}}' 2>/dev/null || true)"
[ -n "$HOST_GW" ] \
    || fail "No gateway on the k3d-${CLUSTER_NAME} docker network." \
            "Is the cluster up? \`k3d cluster list\`, and CLUSTER_NAME=${CLUSTER_NAME}."

DESIRED='template IN A host.k3d.internal {
  match "^host\.k3d\.internal\.$"
  answer "{{ .Name }} 60 IN A '"${HOST_GW}"'"
  fallthrough
}'

current="$(kubectl -n kube-system get cm coredns-custom \
    -o jsonpath="{.data.${CM_KEY//./\\.}}" 2>/dev/null || true)"

if [ "$MODE" = "check" ]; then
    echo "host.k3d.internal on ${CLUSTER_CONTEXT}"
    echo "   network gateway:  ${HOST_GW}"
    # NodeHosts is reported too: it is where k3d put the name and where k3s
    # takes it away, so its state says whether this cluster has restarted since
    # it was created.
    if kubectl -n kube-system get cm coredns -o jsonpath='{.data.NodeHosts}' 2>/dev/null \
        | grep -q 'host\.k3d\.internal'; then
        nodehosts_carries_name=1
        echo "   NodeHosts:        carries the name (k3d's own entry is intact)"
    else
        nodehosts_carries_name=0
        echo "   NodeHosts:        dropped (expected after a node restart)"
    fi
    if [ "$current" = "$DESIRED" ]; then
        echo "   coredns-custom:   ✅ ${CM_KEY} present, answers ${HOST_GW}"
        exit 0
    elif [ -n "$current" ]; then
        echo "   coredns-custom:   ⚠️  ${CM_KEY} present but stale (gateway moved?)"
        exit 1
    elif [ "$nodehosts_carries_name" = 1 ]; then
        # Resolving today, on k3d's own entry — so the rewrites work and nothing
        # is broken yet. Still reported as a finding: that entry goes at the
        # first node restart, and this is the window in which to pre-empt it.
        echo "   coredns-custom:   ❌ ${CM_KEY} absent — the name resolves from NodeHosts"
        echo "                        today, and goes with it at the next node restart"
        exit 1
    else
        echo "   coredns-custom:   ❌ ${CM_KEY} absent — rewrites resolve to nothing"
        exit 1
    fi
fi

if [ "$current" = "$DESIRED" ]; then
    echo "⏭️  host.k3d.internal already answers ${HOST_GW}"
    exit 0
fi

# Merge-patched, never applied: setup-agent-manager.sh and
# setup-env-for-aectl.sh own their own keys in this same ConfigMap, and an
# apply that declares only this one would prune theirs.
kubectl -n kube-system patch cm coredns-custom --type merge \
    -p "$(V="$DESIRED" K="$CM_KEY" python3 -c \
        'import json,os; print(json.dumps({"data": {os.environ["K"]: os.environ["V"]}}))')"
kubectl -n kube-system rollout restart deployment/coredns
kubectl -n kube-system rollout status deployment/coredns --timeout=120s
echo "✅ host.k3d.internal answers ${HOST_GW}"
echo ""
echo "   Verify from inside the cluster:"
echo "     kubectl -n kube-system run dnscheck --rm -i --restart=Never \\"
echo "       --image=busybox:1.36 --command -- nslookup thunder.openchoreo.${AE_DOMAIN:-localhost}."
