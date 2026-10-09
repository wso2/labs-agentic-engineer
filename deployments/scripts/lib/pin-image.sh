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

# pin_node_image: shared by build-runner.sh (sourced) and `make dev-images`
# (run as `bash pin-image.sh <repo:tag>...`, which exits 1 when an image is
# missing from a node and 0 otherwise, an unpinned-but-present image only warns).
# CLUSTER_NAME defaults to openchoreo.
CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"

# pin_node_image <repo:tag> labels an imported local-only tag
# io.cri-containerd.pinned=pinned on every server/agent node, so kubelet's image
# GC never evicts it (it has no registry to be pulled back from). An import
# replaces the containerd record, so this runs after every import.
#   0 — on every node and pinned; 1 — on every node, not pinned everywhere;
#   2 — missing from a node: `k3d image import` can flake and still exit 0.
pin_node_image() {
    local image="$1" nodes eligible=0 found=0 pinned=0 node ref
    nodes="$(k3d node list --no-headers 2>/dev/null \
        | awk -v c="$CLUSTER_NAME" '$3 == c && ($2 == "server" || $2 == "agent") { print $1 }')"
    for node in $nodes; do
        eligible=$((eligible + 1))
        # A local tag lands as docker.io/library/<repo>:<tag>; match the whole ref.
        ref="$(docker exec "$node" ctr -n k8s.io images ls -q 2>/dev/null \
            | grep -m1 -Fx -e "$image" -e "docker.io/library/$image" || true)"
        [ -n "$ref" ] || continue
        found=$((found + 1))
        docker exec "$node" ctr -n k8s.io images label \
            "$ref" io.cri-containerd.pinned=pinned >/dev/null 2>&1 && pinned=$((pinned + 1))
    done
    if [ "$eligible" -eq 0 ]; then
        echo "⚠️  no server/agent node found for cluster '$CLUSTER_NAME' — cannot pin $image"
        return 2
    fi
    if [ "$found" -lt "$eligible" ]; then
        echo "⚠️  $image is missing from $((eligible - found))/$eligible node(s) — the import did not land"
        return 2
    fi
    if [ "$pinned" -lt "$found" ]; then
        echo "⚠️  could not pin $image on $((found - pinned))/$found node(s) — kubelet image GC may evict this local-only tag"
        return 1
    fi
    echo "📌 pinned $image against kubelet image GC ($pinned/$eligible node(s))"
    return 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
    rc=0
    for image in "$@"; do
        pin_node_image "$image" || { [ $? -eq 2 ] && rc=1; }
    done
    exit "$rc"
fi
