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

# Shared utilities — sourced by setup scripts.

# fetch_gh_raw <raw.githubusercontent.com URL> <dest-file>
# Downloads a raw GitHub file with bounded retries. Anonymous raw fetches are
# per-IP throttled (bursty 429s observed during repeated bring-ups); when
# LOCAL_DEV_ADMIN_GITHUB_PAT is available (env or deployments/.env) the fetch
# goes through the authenticated contents API instead — a far higher limit.
# Falls back to the plain unauthenticated raw URL when no PAT is configured.
fetch_gh_raw() {
    local url="$1" dest="$2"
    local pat="${LOCAL_DEV_ADMIN_GITHUB_PAT:-}"
    if [ -z "$pat" ]; then
        local envfile
        envfile="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.env"
        if [ -f "$envfile" ]; then
            pat=$(grep -E '^LOCAL_DEV_ADMIN_GITHUB_PAT=' "$envfile" | tail -1 | cut -d= -f2- | tr -d '"' | tr -d "'")
        fi
    fi
    # raw.githubusercontent.com/<owner>/<repo>/<ref>/<path> → API coordinates.
    local rest="${url#https://raw.githubusercontent.com/}"
    local owner="${rest%%/*}"; rest="${rest#*/}"
    local repo="${rest%%/*}"; rest="${rest#*/}"
    local ref="${rest%%/*}"; local path="${rest#*/}"
    local attempt
    for attempt in 1 2 3 4 5; do
        if [ -n "$pat" ]; then
            if curl -fsSL -H "Authorization: Bearer $pat" -H "Accept: application/vnd.github.raw+json" \
                "https://api.github.com/repos/$owner/$repo/contents/$path?ref=$ref" -o "$dest"; then
                return 0
            fi
        else
            if curl -fsSL "$url" -o "$dest"; then
                return 0
            fi
        fi
        echo "⚠️  fetch $url failed (attempt $attempt/5) — retrying in $((attempt * 15))s..."
        sleep $((attempt * 15))
    done
    echo "❌ Could not fetch $url after 5 attempts"
    return 1
}

is_port_in_use() {
    lsof -i :"$1" -sTCP:LISTEN &>/dev/null
}

check_required_ports() {
    local ports=(
        "6550:Kubernetes API"
        "8080:Control Plane HTTP"
        "8443:Control Plane HTTPS"
        "19080:Data Plane HTTP"
        "19443:Data Plane HTTPS"
        "10081:Argo Workflows UI"
        "10082:Container Registry"
        "11080:Observability HTTP"
        "11085:OpenSearch HTTPS"
    )
    local blocked=()
    echo "🔍 Checking port availability..."
    for p in "${ports[@]}"; do
        local port="${p%%:*}" desc="${p#*:}"
        if is_port_in_use "$port"; then blocked+=("$port ($desc)"); fi
    done
    if [ ${#blocked[@]} -gt 0 ]; then
        echo "❌ Ports in use: ${blocked[*]}"
        echo "   Free them or stop the conflicting process."
        return 1
    fi
    echo "✅ All ports available"
}

# helm_release_deployed <release> <namespace> — succeeds ONLY when the release
# exists AND its status is `deployed`. A `failed` or `pending-*` release returns
# false, so a caller guarding `helm upgrade --install` on this re-drives the
# install instead of skipping it — the release then self-heals on the next run.
# A bare `helm status … &>/dev/null` existence check does NOT distinguish a
# failed release from a healthy one (the trap the workflow-plane install hit:
# a cert-manager webhook race left the release `failed`, and the existence check
# skipped the reinstall that would have recreated the certs).
helm_release_deployed() {
    # `rel_status`, not `status`: `status` is a read-only special var in zsh.
    local release="$1" ns="$2" rel_status
    rel_status="$(helm status "$release" -n "$ns" --kube-context "${CLUSTER_CONTEXT}" -o json 2>/dev/null \
        | grep -o '"status":"[a-z-]*"' | head -1)" || true
    [ "$rel_status" = '"status":"deployed"' ]
}

helm_install_if_not_exists() {
    local release="$1" ns="$2" chart="$3"; shift 3
    if helm status "$release" -n "$ns" --kube-context "${CLUSTER_CONTEXT}" &>/dev/null; then
        echo "⏭️  $release already installed, skipping"
        return 0
    fi
    echo "📦 Installing $release..."
    helm install "$release" "$chart" --namespace "$ns" --create-namespace --kube-context "${CLUSTER_CONTEXT}" "$@"
    echo "✅ $release installed"
}

refresh_kubeconfig() {
    echo "🔄 Refreshing kubeconfig..."
    k3d kubeconfig merge ${CLUSTER_NAME} --kubeconfig-merge-default --kubeconfig-switch-context
}

wait_for_cluster() {
    echo "⏳ Waiting for cluster..."
    for i in {1..30}; do
        if kubectl cluster-info --context ${CLUSTER_CONTEXT} --request-timeout=5s &>/dev/null; then
            echo "✅ Cluster ready"
            return 0
        fi
        echo "   Attempt $i/30..."
        sleep 2
    done
    return 1
}

ensure_cluster_accessible() {
    refresh_kubeconfig
    if kubectl cluster-info --context ${CLUSTER_CONTEXT} --request-timeout=10s &>/dev/null; then
        echo "✅ Cluster accessible"
        return 0
    fi
    echo "⚠️  Cluster not accessible. Restarting..."
    k3d cluster stop ${CLUSTER_NAME} 2>/dev/null || true
    k3d cluster start ${CLUSTER_NAME}
    refresh_kubeconfig
    wait_for_cluster
}

create_plane_cert_resources() {
    local ns="$1"
    kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f -
    kubectl wait -n openchoreo-control-plane --for=condition=Ready certificate/cluster-gateway-ca --timeout=120s
    local ca
    ca=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$ca" -n "$ns" --dry-run=client -o yaml | kubectl apply -f -
}

register_data_plane() {
    # obs_plane_name defaults to "default" for callers that don't pass one —
    # today there's only ever one ClusterObservabilityPlane (also named
    # "default", see setup-observability.sh), but this keeps that assumption
    # an explicit, overridable parameter rather than baked in, for future
    # multi-plane setups.
    local ca_cert="$1" plane_id="$2" secret_store="$3" obs_plane_name="${4:-default}"
    cat <<EOF | kubectl apply -f -
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterDataPlane
metadata:
  name: default
  namespace: default
spec:
  planeID: "$plane_id"
  secretStoreRef:
    name: "$secret_store"
  observabilityPlaneRef:
    kind: ClusterObservabilityPlane
    name: "$obs_plane_name"
  clusterAgent:
    clientCA:
      value: |
$(echo "$ca_cert" | sed 's/^/        /')
  gateway:
    ingress:
      external:
        name: gateway-default
        namespace: openchoreo-data-plane
        http:
          host: "openchoreoapis.localhost"
          listenerName: http
          port: 19080
        https:
          host: "openchoreoapis.localhost"
          listenerName: https
          port: 19443
EOF
    echo "✅ DataPlane registered"
}

register_workflow_plane() {
    local ca_cert="$1" plane_id="$2" secret_store="$3"
    cat <<EOF | kubectl apply -f -
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterWorkflowPlane
metadata:
  name: default
  namespace: default
spec:
  planeID: "$plane_id"
  secretStoreRef:
    name: "$secret_store"
  clusterAgent:
    clientCA:
      value: |
$(echo "$ca_cert" | sed 's/^/        /')
EOF
    echo "✅ WorkflowPlane registered"
}

# Ensure CoreDNS resolves host.k3d.internal to the docker bridge gateway.
# k3d exposes host.k3d.internal as a TLS SAN on the k3s server cert but
# does NOT inject a CoreDNS NodeHosts entry — pods can't resolve the name
# without help. We add the entry once at cluster creation (the bridge
# gateway IP is stable for the cluster's lifetime). Pairs with OC's
# coredns-custom.yaml rewrite rule (*.openchoreo.localhost → host.k3d.internal)
# so both the kgateway hostnames AND the bare host.k3d.internal name
# resolve correctly from inside pods.
ensure_host_k3d_internal_in_coredns() {
    local gw_ip
    gw_ip=$(docker network inspect "k3d-${CLUSTER_NAME}" \
        --format "{{(index .IPAM.Config 0).Gateway}}" 2>/dev/null)
    if [ -z "$gw_ip" ]; then
        echo "⚠️  Could not determine bridge gateway IP — skipping host.k3d.internal CoreDNS patch"
        return
    fi
    # We register a dedicated CoreDNS Server Block in `coredns-custom`
    # (imported via `import /etc/coredns/custom/*.server`, OUTSIDE the
    # main `.:53` block) rather than patching `coredns.NodeHosts`,
    # because k3s' Rancher addon controller periodically re-applies its
    # own coredns ConfigMap and wipes any custom NodeHosts entries — but
    # it does NOT touch coredns-custom. A separate server block (rather
    # than an `.override` fragment) is required because the main `.:53`
    # block already uses the `hosts` plugin once for NodeHosts; CoreDNS
    # forbids two `hosts` plugins in the same Server Block.
    local key="host-k3d-internal.server"
    local desired="host.k3d.internal:53 {
  hosts {
    ${gw_ip} host.k3d.internal
    fallthrough
  }
}
"
    local current
    current=$(kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" \
        -o jsonpath="{.data.${key}}" 2>/dev/null)
    if [ "$current" = "$desired" ]; then
        echo "✅ host.k3d.internal already in coredns-custom (${gw_ip})"
        return
    fi
    kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" -o json 2>/dev/null \
        | GW_IP="$gw_ip" KEY="$key" python3 -c "
import json, os, sys
cm = json.load(sys.stdin)
gw_ip = os.environ['GW_IP']
key = os.environ['KEY']
cm.setdefault('data', {})[key] = f'host.k3d.internal:53 {{\n  hosts {{\n    {gw_ip} host.k3d.internal\n    fallthrough\n  }}\n}}\n'
# Drop the broken .override entry we may have written in a prior attempt.
cm['data'].pop('host-k3d-internal.override', None)
cm['metadata'] = {'name': cm['metadata']['name'], 'namespace': cm['metadata']['namespace']}
json.dump(cm, sys.stdout)
" | kubectl apply --context "${CLUSTER_CONTEXT}" -f - >/dev/null
    kubectl rollout restart deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" >/dev/null
    kubectl rollout status deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" --timeout=60s >/dev/null
    echo "✅ host.k3d.internal added to coredns-custom (${gw_ip})"
}

# Make `*.openchoreo.localhost` AND `*.openchoreoapis.localhost` resolvable
# from inside pods. The OpenChoreo Helm chart ships an `openchoreo.override`
# that (a) only covers `*.openchoreo.localhost`, missing the public *runtime*
# hostnames the kgateway HTTPRoutes actually use (`*.openchoreoapis.localhost`),
# and (b) rewrites to `host.k3d.internal`, which can't be resolved from
# within the `.:53` server block (its hosts plugin lives in a SEPARATE
# `host.k3d.internal:53` server block). Result: any in-cluster pod calling
# its own platform's public gateway hostname fails with NXDOMAIN.
#
# We replace the chart's override with a rewrite that targets the kgateway
# Service FQDN directly — that name IS resolvable inside `.:53` via the
# `kubernetes` plugin, and Envoy's Host-header-based routing on the gateway
# preserves correct HTTPRoute selection because the client's Host header is
# untouched. Pairs with the schema/prompt-level "External dependent APIs"
# feature in the architect agent.
ensure_openchoreo_localhost_in_coredns() {
    local key="openchoreo.override"
    local desired='rewrite stop {
  name regex (.+\.)?(openchoreo|openchoreoapis)\.localhost gateway-default.openchoreo-data-plane.svc.cluster.local
  answer auto
}
'
    local current
    current=$(kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" \
        -o jsonpath="{.data.${key}}" 2>/dev/null)
    if [ "$current" = "$desired" ]; then
        echo "✅ openchoreo*.localhost rewrite already correct in coredns-custom"
        return
    fi
    kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" -o json 2>/dev/null \
        | KEY="$key" python3 -c "
import json, os, sys
cm = json.load(sys.stdin)
key = os.environ['KEY']
cm.setdefault('data', {})[key] = 'rewrite stop {\n  name regex (.+\\\\.)?(openchoreo|openchoreoapis)\\\\.localhost gateway-default.openchoreo-data-plane.svc.cluster.local\n  answer auto\n}\n'
cm['metadata'] = {'name': cm['metadata']['name'], 'namespace': cm['metadata']['namespace']}
json.dump(cm, sys.stdout)
" | kubectl apply --context "${CLUSTER_CONTEXT}" -f - >/dev/null
    kubectl rollout restart deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" >/dev/null
    kubectl rollout status deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" --timeout=60s >/dev/null
    echo "✅ openchoreo*.localhost rewrite installed in coredns-custom"
}

# Add the CoreDNS rewrites Agent Manager's hostnames need, alongside the
# openchoreo one above. Only called when ENABLE_AGENT_MANAGER=1.
#
# These go to host.k3d.internal rather than to an in-cluster Service, unlike the
# openchoreo rewrite. The difference matters: an in-cluster Service name would
# reach the gateway but lose the vhost the request has to match on, so agent and
# LLM-gateway calls would land on the wrong route. Hairpinning out to the k3d
# load balancer and back in preserves the Host header. This is the shape Agent
# Manager itself ships (deployments/k8s/coredns-amp-custom.yaml).
#
#   amp.localhost           console.amp.localhost / api.amp.localhost — and the
#                           gateway extension's bootstrap Job calls the latter
#                           from inside the cluster
#   agentmanager.localhost  the data-plane gateway's default agent host
#   am-gateway.localhost    the agent host advertised on Environment CRs
#   gateway.localhost       the AI-gateway / LLM-proxy vhost injected into agent
#                           pods
ensure_amp_localhost_in_coredns() {
    local changed=0 key host
    for key in amp agentmanager am-gateway gateway; do
        host="${key//-/\\-}"
        local cm_key="${key//-/}.override"
        local desired
        desired="rewrite stop {
  name regex (.+\\.)?${host}\\.localhost host.k3d.internal
  answer auto
}
"
        local current
        current="$(kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" \
            -o jsonpath="{.data.${cm_key}}" 2>/dev/null || true)"
        [ "$current" = "$desired" ] && continue
        kubectl get cm coredns-custom -n kube-system --context "${CLUSTER_CONTEXT}" -o json 2>/dev/null \
            | CM_KEY="$cm_key" REWRITE_HOST="$host" python3 -c "
import json, os, sys
cm = json.load(sys.stdin)
cm.setdefault('data', {})[os.environ['CM_KEY']] = (
    'rewrite stop {\n'
    '  name regex (.+\\\\.)?' + os.environ['REWRITE_HOST'] + '\\\\.localhost host.k3d.internal\n'
    '  answer auto\n'
    '}\n'
)
cm['metadata'] = {'name': cm['metadata']['name'], 'namespace': cm['metadata']['namespace']}
json.dump(cm, sys.stdout)
" | kubectl apply --context "${CLUSTER_CONTEXT}" -f - >/dev/null
        changed=1
    done
    if [ "$changed" = 1 ]; then
        kubectl rollout restart deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" >/dev/null
        kubectl rollout status deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" --timeout=60s >/dev/null
        echo "✅ *.amp / *.agentmanager / *.am-gateway / *.gateway .localhost rewrites installed"
    else
        echo "✅ Agent Manager DNS rewrites already correct in coredns-custom"
    fi
}

# Fix DNS on all k3d nodes. Keeps Docker's embedded DNS (127.0.0.11) as primary
# so that Docker-internal names (container names) still resolve, and adds
# 8.8.8.8 as a fallback for external image pulls.
fix_node_dns() {
    echo "🔧 Fixing k3d node DNS resolution..."
    local node
    for node in $(docker ps --filter "name=k3d-${CLUSTER_NAME}" --format '{{.Names}}'); do
        # Docker's embedded resolver (127.0.0.11) is preferred when it works:
        # it is what resolves other CONTAINER names on the k3d network. But it
        # does not always survive the node container being restarted — after a
        # `k3d cluster stop/start`, or a Colima resize, it answers CONNECTION
        # REFUSED. Listing a dead resolver first is not harmless: containerd's
        # image pulls intermittently fail with "lookup registry-1.docker.io:
        # Try again" rather than falling through cleanly, and the cluster then
        # sits in ImagePullBackOff looking like a registry problem.
        #
        # So probe it, and only put it first if it actually answers. The public
        # resolvers are the fallback either way; two of them, because a single
        # unreachable one strands the node with no external DNS at all.
        if docker exec "$node" sh -c \
            'nslookup -timeout=2 localhost 127.0.0.11 >/dev/null 2>&1' 2>/dev/null; then
            docker exec "$node" sh -c \
                'printf "nameserver 127.0.0.11\nnameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf' \
                2>/dev/null || true
        else
            echo "   ${node}: Docker's embedded resolver is not answering — using public resolvers only"
            docker exec "$node" sh -c \
                'printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf' \
                2>/dev/null || true
        fi
    done
    echo "✅ Node DNS configured"
}

# _cluster_dns_resolves runs a throwaway pod that resolves a public name through
# cluster DNS (10.43.0.10). Non-zero means resolution failed.
_cluster_dns_resolves() {
    local image="$1" host="$2"
    kubectl run "aep-dns-probe-${RANDOM}" \
        --context "${CLUSTER_CONTEXT}" --rm --attach --quiet --restart=Never \
        --image="$image" --command -- \
        sh -c "nslookup ${host} >/dev/null 2>&1" >/dev/null 2>&1
}

# Assert pods can resolve public names through CoreDNS, and repair CoreDNS when
# they can't.
#
# CoreDNS's `.:53` block forwards to `/etc/resolv.conf`, and the forward plugin
# reads that file ONCE at process start. The file is a snapshot of the node's
# resolver taken when the pod sandbox was created and is never refreshed. So any
# change to node DNS after CoreDNS started — fix_node_dns above, or Docker
# re-deriving a restarted node's resolv.conf after a Colima reboot — leaves
# CoreDNS forwarding to an address that may no longer answer. The symptom is
# nasty because it is silent and asymmetric: the node resolves fine, pods
# resolve nothing external, and a coding-agent run dies at `git clone` with
# "Could not resolve host: github.com".
#
# k3d-local-config.yaml pins pod DNS to a static resolv.conf so newly created
# clusters cannot hit this at all. This check covers clusters created before that
# pin plus any other drift: it probes real resolution and, if it fails, restarts
# CoreDNS so it re-reads the current resolver. Runs on every start.sh because a
# Colima restart is exactly when the race fires.
ensure_cluster_dns_healthy() {
    local probe_image="${AEP_DNS_PROBE_IMAGE:-busybox:1.36}"
    local probe_host="${AEP_DNS_PROBE_HOST:-github.com}"

    echo "🔧 Checking in-cluster DNS..."
    if _cluster_dns_resolves "$probe_image" "$probe_host"; then
        echo "✅ Cluster DNS resolves ${probe_host}"
        return 0
    fi

    echo "⚠️  Pods cannot resolve ${probe_host} — restarting CoreDNS so it re-reads the node resolver"
    kubectl rollout restart deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" >/dev/null 2>&1 || true
    kubectl rollout status deployment coredns -n kube-system --context "${CLUSTER_CONTEXT}" --timeout=90s >/dev/null 2>&1 || true

    if _cluster_dns_resolves "$probe_image" "$probe_host"; then
        echo "✅ Cluster DNS repaired (CoreDNS restarted)"
        return 0
    fi

    echo "❌ Pods still cannot resolve ${probe_host} after restarting CoreDNS."
    echo "   Every external name fails in-cluster: coding-agent git clone, image pulls by name."
    echo "   Inspect the two ends of the forward chain:"
    echo "     docker exec k3d-${CLUSTER_NAME}-server-0 cat /etc/resolv.conf"
    echo "     kubectl --context ${CLUSTER_CONTEXT} -n kube-system logs -l k8s-app=kube-dns --tail=20"
    return 1
}

# Configure k3s containerd to use the workflow-plane registry via ClusterIP.
# Kubelet can't resolve Kubernetes service DNS, so we mirror the service name
# to its ClusterIP. Requires k3s restart to take effect.
configure_registry_mirror() {
    echo "🔧 Configuring k3s registry mirror for workflow-plane registry..."
    local registry_ip
    registry_ip=$(kubectl get svc registry -n openchoreo-workflow-plane --context "${CLUSTER_CONTEXT}" -o jsonpath='{.spec.clusterIP}' 2>/dev/null)
    if [ -z "$registry_ip" ]; then
        echo "⚠️  Workflow-plane registry not found — skipping"
        return 1
    fi

    for node in $(docker ps --filter "name=k3d-${CLUSTER_NAME}" --format '{{.Names}}'); do
        docker exec "$node" sh -c "
mkdir -p /etc/rancher/k3s
cat > /etc/rancher/k3s/registries.yaml <<EOF
mirrors:
  \"registry.openchoreo-workflow-plane.svc.cluster.local:10082\":
    endpoint:
      - \"http://${registry_ip}:10082\"
  # dockerfile-builder's publish-image step tags images with
  # host.k3d.internal:10082/<image>:<tag> (the registry is exposed on the
  # host's port 10082 via k3d-local-config.yaml). Kubelet inside the
  # cluster cannot reach that host:port — but the actual registry pod
  # listens on the cluster-IP below. Mirror the host.k3d.internal:10082
  # name to the registry service IP so kubelet can pull without leaving
  # the cluster network.
  \"host.k3d.internal:10082\":
    endpoint:
      - \"http://${registry_ip}:10082\"
EOF
" 2>/dev/null || true
    done
    echo "✅ Registry mirror configured (${registry_ip}:10082)"

    # k3s must be restarted to pick up registries.yaml changes.
    # We restart k3s by sending SIGHUP to PID 1 in each node, then
    # re-apply DNS fixes that get reset during restart.
    echo "🔄 Restarting k3s to apply registry configuration..."
    for node in $(docker ps --filter "name=k3d-${CLUSTER_NAME}" --format '{{.Names}}'); do
        docker exec "$node" sh -c "kill -HUP 1" 2>/dev/null || true
    done
    sleep 15
    wait_for_cluster || { echo "❌ Cluster failed to restart"; return 1; }

    # DNS fixes are reset after k3s restart — re-apply them
    fix_node_dns
}


# ----------------------------------------------------------------------------
# Locally-imported images
# ----------------------------------------------------------------------------
# An image that was IMPORTED into the node rather than pulled has no registry
# behind it, so a kubelet eviction is unrecoverable: the next pod that needs it
# sits in ImagePullBackOff (or ErrImageNeverPull under `pullPolicy: Never`) until
# someone re-imports by hand. kubelet's imageGCManager collects unused images once
# the node's image filesystem crosses its high threshold — 85%, freeing down to
# 80% — least recently used first (it sorts byLastUsedAndDetected, NOT by size),
# and keeps going until it has freed enough bytes. An idle local-only tag is
# therefore taken early, and a large one frees a big chunk of the target in one
# go. The thresholds are kubelet defaults; nothing in
# deployments/k3d-local-config.yaml currently sets them (it could, by the same
# --kubelet-arg mechanism it already uses for eviction-hard).
#
# Labelling the image `io.cri-containerd.pinned=pinned` is containerd's own way of
# saying an image has no upstream to recover from: CRI reports it as
# `pinned: true` and the kubelet skips it when collecting. Verify with
#   docker exec k3d-<cluster>-server-0 crictl inspecti <repo>:<tag> | grep pinned
#
# An import REPLACES the containerd record, so this belongs in the import path —
# a one-off `ctr label` does not survive the next import.
#
# The contract is ALL-OR-NOTHING across the cluster's server/agent nodes, because a
# pod is scheduled to one of them and a partial result is a bug on whichever node
# was missed. Anything less than complete is reported as a failure, never as a
# qualified success.
#
# Usage: pin_node_image <repo:tag>
#   0 — every eligible node has the image and it is pinned there
#   1 — every node has it, but the label could not be applied somewhere; the image
#       is usable, it is just still collectible
#   2 — at least one eligible node is missing the image (or there are no eligible
#       nodes), i.e. the import did not land. `k3d image import` is known to flake
#       and still exit 0 — see the RCA import in setup-observability.sh — so
#       callers should treat this as an import failure, not a pinning failure.
pin_node_image() {
    local image="$1" nodes eligible=0 found=0 pinned=0 node ref
    # Every server/agent node: `k3d image import` loads into all of them, so
    # pinning only the first would leave the eviction reproducible on whichever
    # node the pod happens to land on. The loadbalancer node runs no containerd.
    nodes="$(k3d node list --no-headers 2>/dev/null \
        | awk -v c="$CLUSTER_NAME" '$3 == c && ($2 == "server" || $2 == "agent") { print $1 }')"
    for node in $nodes; do
        eligible=$((eligible + 1))
        # A local tag lands as docker.io/library/<repo>:<tag>; a registry-qualified
        # name keeps its host. Match the WHOLE ref against both forms — a substring
        # match would happily select <repo>:<tag>-debug and pin the wrong record.
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


# ----------------------------------------------------------------------------
# Public URL handling
# ----------------------------------------------------------------------------
# .env carries two canonical fields:
#   PUBLIC_THUNDER_URL   — public URL the browser uses to reach Thunder
#   PUBLIC_CONSOLE_URL   — public URL the browser uses to reach the console
# Everything that needs these values (Helm values, ConfigMaps, redirect URIs,
# OIDC issuer) derives from them — edit .env, re-run start.sh, done.

# Load PUBLIC_THUNDER_URL / PUBLIC_CONSOLE_URL from the project .env into the
# current shell, then derive PUBLIC_THUNDER_HOST / PORT / SCHEME from the URL.
# Exits non-zero if .env is missing or doesn't define both URLs.
load_public_urls() {
    local env_file="${1:-${SCRIPT_DIR:-.}/../.env}"
    PUBLIC_THUNDER_URL=""
    PUBLIC_CONSOLE_URL=""
    if [ -f "$env_file" ]; then
        # grep exits 1 when no match — tolerate with || true so callers using
        # set -o pipefail don't abort before the defaults below apply.
        PUBLIC_THUNDER_URL="$(grep -E '^PUBLIC_THUNDER_URL=' "$env_file" 2>/dev/null | head -1 | cut -d= -f2- || true)"
        PUBLIC_CONSOLE_URL="$(grep -E '^PUBLIC_CONSOLE_URL=' "$env_file" 2>/dev/null | head -1 | cut -d= -f2- || true)"
    fi
    # First-install fallback: .env doesn't exist yet, so use local defaults.
    : "${PUBLIC_THUNDER_URL:=http://thunder.openchoreo.localhost:8080}"
    : "${PUBLIC_CONSOLE_URL:=http://localhost:8090}"
    # Strip trailing slash for consistency
    PUBLIC_THUNDER_URL="${PUBLIC_THUNDER_URL%/}"
    PUBLIC_CONSOLE_URL="${PUBLIC_CONSOLE_URL%/}"

    # Derive scheme / host / port
    if [[ "$PUBLIC_THUNDER_URL" == https://* ]]; then
        PUBLIC_THUNDER_SCHEME="https"
        local default_port=443
    else
        PUBLIC_THUNDER_SCHEME="http"
        local default_port=80
    fi
    local hostport="${PUBLIC_THUNDER_URL#*://}"
    hostport="${hostport%%/*}"
    if [[ "$hostport" == *:* ]]; then
        PUBLIC_THUNDER_HOST="${hostport%:*}"
        PUBLIC_THUNDER_PORT="${hostport##*:}"
    else
        PUBLIC_THUNDER_HOST="$hostport"
        PUBLIC_THUNDER_PORT="$default_port"
    fi
    export PUBLIC_THUNDER_URL PUBLIC_CONSOLE_URL \
           PUBLIC_THUNDER_HOST PUBLIC_THUNDER_PORT PUBLIC_THUNDER_SCHEME
}

# Render a Helm values file with `${PUBLIC_*}` placeholders into a temp file
# (post-processing dedupes any duplicate hostnames the substitution produced —
# in local mode PUBLIC_THUNDER_HOST equals thunder.openchoreo.localhost).
# Echoes the rendered file path on stdout.
render_values_file() {
    local src="$1"
    local rendered
    rendered="$(mktemp -t "aep-values.XXXXXX.yaml")"
    # Only expand the public URL placeholders — bootstrap scripts contain
    # bash variables like ${SCRIPT_DIR} that must NOT be touched.
    envsubst '${PUBLIC_THUNDER_URL} ${PUBLIC_THUNDER_HOST} ${PUBLIC_THUNDER_PORT} ${PUBLIC_THUNDER_SCHEME} ${PUBLIC_CONSOLE_URL}' < "$src" > "$rendered"
    # Dedupe consecutive identical YAML list items (handles HTTPRoute hostnames)
    python3 - "$rendered" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1])
out, prev = [], None
for line in p.read_text().splitlines():
    stripped = line.strip()
    if stripped.startswith("- ") and stripped == prev:
        continue
    out.append(line)
    prev = stripped if stripped.startswith("- ") else None
p.write_text("\n".join(out) + "\n")
PY
    echo "$rendered"
}

# Patch the running cluster to match the current PUBLIC_* env vars.
# Idempotent: skips work when the live state already matches.
#
# This used to be several hundred lines of surgery on Thunder 0.34 — rewriting
# its ConfigMap key by key, patching its HTTPRoute hostnames, and reaching into
# the running pod to UPDATE redirect_uris in SQLite. None of that survives the
# move to ThunderID, whose config, routing and OAuth clients all come from Helm
# values and the declarative bootstrap. Re-running setup-thunder.sh with the new
# PUBLIC_* values applies every one of them through the supported path, so the
# Thunder half of this function is now that one call.
apply_public_urls_to_cluster() {
    if ! kubectl get ns "${THUNDER_NS}" >/dev/null 2>&1; then
        echo "⚠️  ${THUNDER_NS} namespace not found — skipping public-URL sync"
        return 0
    fi

    echo "🔄 Syncing public URLs to cluster…"
    echo "   thunder: ${PUBLIC_THUNDER_URL}"
    echo "   console: ${PUBLIC_CONSOLE_URL}"

    # Compare against the address Thunder is actually serving before paying for
    # a helm upgrade plus a bootstrap re-import.
    local current_public_url
    current_public_url="$(kubectl -n "${THUNDER_NS}" get cm "${THUNDER_RELEASE}-config-map" \
        -o jsonpath='{.data.deployment\.yaml}' 2>/dev/null \
        | sed -nE 's/^[[:space:]]*public_url:[[:space:]]*"?([^" ]+)"?.*/\1/p' | head -1)"

    if [ "$current_public_url" != "$PUBLIC_THUNDER_URL" ]; then
        echo "   public URL changed (${current_public_url:-unset} → ${PUBLIC_THUNDER_URL}) — reconverging the IdP"
        # THUNDER_FORCE_UPGRADE re-drives an already-deployed release. This is
        # the one caller that means "the values changed, converge it" rather
        # than "install it if absent".
        THUNDER_FORCE_UPGRADE=1 bash "${SCRIPT_DIR}/setup-thunder.sh"
    else
        echo "   ✓ platform IdP already serving ${PUBLIC_THUNDER_URL}"
    fi

    # OpenChoreo API: only the OIDC issuer changes. Patch its ConfigMap directly.
    if kubectl get cm openchoreo-api-config -n openchoreo-control-plane >/dev/null 2>&1; then
        local current_issuer
        current_issuer="$(kubectl -n openchoreo-control-plane get cm openchoreo-api-config \
            -o jsonpath='{.data.config\.yaml}' \
            | sed -nE 's/^[[:space:]]*issuer:[[:space:]]*"([^"]+)".*/\1/p' | head -1)"
        if [ "$current_issuer" != "$PUBLIC_THUNDER_URL" ]; then
            local cm_yaml
            cm_yaml="$(mktemp)"
            kubectl -n openchoreo-control-plane get cm openchoreo-api-config -o yaml > "$cm_yaml"
            python3 - "$cm_yaml" "$PUBLIC_THUNDER_URL" <<'PYEOF'
import sys, re, pathlib
path, issuer = sys.argv[1:]
p = pathlib.Path(path)
p.write_text(re.sub(r'(issuer:\s*)"[^"]*"', rf'\g<1>"{issuer}"', p.read_text(), count=1))
PYEOF
            kubectl replace -f "$cm_yaml" >/dev/null
            kubectl -n openchoreo-control-plane rollout restart deploy/openchoreo-api >/dev/null
            rm -f "$cm_yaml"
            echo "   ✓ openchoreo-api OIDC issuer updated"
        fi
    fi

    echo "✅ Public URLs synced"
}

generate_machine_ids() {
    local cluster_name="$1"
    echo "🆔 Generating machine IDs..."
    local nodes
    nodes=$(k3d node list -o json | grep -o '"name"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/"name"[[:space:]]*:[[:space:]]*"//;s/"$//' | grep "^k3d-${cluster_name}-")
    for node in $nodes; do
        docker exec "$node" sh -c "cat /proc/sys/kernel/random/uuid | tr -d '-' > /etc/machine-id" 2>/dev/null || true
    done
    echo "✅ Machine IDs generated"
}

# ── Chart CRD synchronisation ────────────────────────────────────────────────
# `helm upgrade` NEVER touches a chart's crds/ directory — it applies them only
# on the very first install. So bumping a chart's version on a cluster that
# already has the old release leaves the OLD CRDs in place, and every new kind
# or field the new version introduces is silently unavailable. That is not a
# theoretical concern here: the OpenChoreo 1.1.1 -> 1.2.0 bump adds the
# ProjectType / ClusterProjectType CRDs that Project.spec.type now requires, so
# without this an upgraded cluster fails every project creation while a fresh
# one works.
#
# Applying the new CRDs is safe: CRDs are additive and keep serving the
# existing stored versions alongside the new ones. --force-conflicts is needed
# because the first install stamped them with Helm's field manager.
#
# Usage: sync_chart_crds <chart-name> <version> [oci-registry]
sync_chart_crds() {
    local chart="$1"
    local version="$2"
    local registry="${3:-oci://ghcr.io/openchoreo/helm-charts}"
    local tmp rc=0

    tmp="$(mktemp -d)" || {
        echo "❌ Failed to create temp dir for ${chart} CRD sync" >&2
        return 1
    }
    if helm pull "${registry}/${chart}" --version "$version" \
        --untar --untardir "$tmp" >/dev/null 2>&1; then
        if compgen -G "$tmp/${chart}/crds/*.yaml" >/dev/null; then
            echo "   Syncing ${chart} CRDs to ${version} (helm upgrade does not update crds/)..."
            kubectl --context "${CLUSTER_CONTEXT}" apply --server-side --force-conflicts \
                -f "$tmp/${chart}/crds/" >/dev/null || rc=1
        fi
    else
        echo "❌ Failed to pull ${chart} ${version} for CRD sync" >&2
        rc=1
    fi
    rm -rf "$tmp"
    return "$rc"
}
