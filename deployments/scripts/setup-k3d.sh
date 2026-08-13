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

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

echo "=== Setting up k3d Cluster for OpenChoreo ==="

# Detect Colima runtime — k3d's DNS fix replaces Docker's embedded DNS (127.0.0.11)
# with the gateway IP, which causes DNS timeouts in Colima due to firewall/network
# isolation. Setting K3D_FIX_DNS=0 preserves Docker's built-in DNS.
# See https://github.com/k3d-io/k3d/issues/1449
is_colima=false
if docker info --format '{{.Name}}' 2>/dev/null | grep -qi "colima"; then
    is_colima=true
fi

if k3d cluster list 2>/dev/null | grep -q "${CLUSTER_NAME}"; then
    echo "✅ k3d cluster '${CLUSTER_NAME}' already exists"
    ensure_cluster_accessible
    kubectl cluster-info --context ${CLUSTER_CONTEXT}
else
    check_required_ports || exit 1
    mkdir -p /tmp/k3d-shared

    K3D_CONFIG="${SCRIPT_DIR}/../k3d-local-config.yaml"
    if [ ! -f "$K3D_CONFIG" ]; then
        echo "❌ k3d config not found at $K3D_CONFIG"
        echo "   Restore it with: git checkout HEAD -- deployments/k3d-local-config.yaml"
        exit 1
    fi

    # k3d resolves a `files:` source against the CONFIG FILE's directory, and it
    # joins unconditionally — an absolute source becomes /tmp/Users/... and the
    # create dies with "could resolve source file path". The dev overlay below
    # also needs a writable copy of the config. So stage the config and every
    # file it references together in one directory and keep the source RELATIVE.
    # We run the create from that directory too, so the path resolves whether
    # k3d joins against the config dir or the process CWD.
    RESOLV_SRC="$(cd "${SCRIPT_DIR}/.." && pwd)/k3s-resolv.conf"
    if [ ! -f "$RESOLV_SRC" ]; then
        echo "❌ pod resolver file not found at $RESOLV_SRC"
        echo "   Restore it with: git checkout HEAD -- deployments/k3s-resolv.conf"
        exit 1
    fi
    K3D_STAGE_DIR="/tmp/aep-k3d-config"
    rm -rf "$K3D_STAGE_DIR"
    mkdir -p "$K3D_STAGE_DIR"
    cp "$RESOLV_SRC" "$K3D_STAGE_DIR/k3s-resolv.conf"
    cp "$K3D_CONFIG" "$K3D_STAGE_DIR/k3d-local-config.yaml"
    K3D_CONFIG="$K3D_STAGE_DIR/k3d-local-config.yaml"

    # Optional: bind-mount the repo-root skills/ library into the k3d server
    # node for ad-hoc debugging. Coding-agent Jobs use the baked image skills
    # (AGENT_RUNNER_IMAGE); this mount is not required for dispatch.
    if [ "${AEP_PROD_RUNNER:-0}" = "1" ]; then
        echo "🏷  AEP_PROD_RUNNER=1 — skipping host skills bind-mount"
    else
        # Check existence BEFORE cd — under `set -e` a failed cd inside the
        # command substitution would abort the script with a cryptic error
        # before the friendly check below could run.
        if [ ! -d "${SCRIPT_DIR}/../../skills" ]; then
            echo "❌ skills library not found at ${SCRIPT_DIR}/../../skills"
            echo "   Set AEP_PROD_RUNNER=1 to skip the optional host bind-mount."
            exit 1
        fi
        SKILLS_HOST_PATH="$(cd "${SCRIPT_DIR}/../../skills" && pwd)"
        # Append in place — the staged config must stay beside the files it
        # references, so this must not copy itself elsewhere.
        cat >> "$K3D_CONFIG" <<EOF
volumes:
  - volume: ${SKILLS_HOST_PATH}:/aep-dev/skills
    nodeFilters:
      - server:*
EOF
        echo "🧪 dev skills overlay — k3d node will bind-mount ${SKILLS_HOST_PATH} → /aep-dev/skills"
    fi

    # Run from the stage dir so a CWD-relative `files:` source resolves too.
    if [ "$is_colima" = true ]; then
        echo "🚀 Creating k3d cluster (Colima detected — K3D_FIX_DNS=0)..."
        ( cd "$K3D_STAGE_DIR" && K3D_FIX_DNS=0 k3d cluster create --config "$K3D_CONFIG" )
    else
        echo "🚀 Creating k3d cluster..."
        ( cd "$K3D_STAGE_DIR" && k3d cluster create --config "$K3D_CONFIG" )
    fi

    echo "✅ k3d cluster created!"
    refresh_kubeconfig
    wait_for_cluster || { echo "❌ Cluster failed to start"; exit 1; }
fi

echo "🔧 Applying CoreDNS custom configuration..."
# Pre-fetched via fetch_gh_raw (PAT-aware, retried) — a direct kubectl -f URL
# read hits the anonymous raw.githubusercontent.com throttle with no retry.
COREDNS_CUSTOM_FILE="$(mktemp)"
fetch_gh_raw "https://raw.githubusercontent.com/openchoreo/openchoreo/v${OPENCHOREO_VERSION}/install/k3d/common/coredns-custom.yaml" "$COREDNS_CUSTOM_FILE"
kubectl apply --context "${CLUSTER_CONTEXT}" -f "$COREDNS_CUSTOM_FILE"
rm -f "$COREDNS_CUSTOM_FILE"
echo "✅ CoreDNS configured"

# Fix node-level DNS (8.8.8.8 fallback for external image pulls).
fix_node_dns

# Ensure pods can resolve host.k3d.internal (k3d only sets it as a TLS SAN;
# the CoreDNS NodeHosts entry is on us). Paired with OC's coredns-custom.yaml
# rewrite for *.openchoreo.localhost above.
ensure_host_k3d_internal_in_coredns

# Repair OC's `openchoreo.override` so pods can also reach `*.openchoreo.localhost`
# and `*.openchoreoapis.localhost` — the chart-shipped rewrite only handles
# the first and targets a name the `.:53` plugin chain can't resolve.
ensure_openchoreo_localhost_in_coredns

# Prove pods can actually resolve public names before handing the cluster over.
# fix_node_dns above rewrites the NODE resolver, which CoreDNS does not re-read;
# this is what catches a CoreDNS left pointing at a stale upstream.
ensure_cluster_dns_healthy

generate_machine_ids "$CLUSTER_NAME"
echo ""
echo "✅ k3d cluster ready!"
