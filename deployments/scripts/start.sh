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

# Start all AEP services.
# Usage: cd deployments && bash scripts/start.sh
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
DEPLOY_DIR="$SCRIPT_DIR/.."

# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"
# shellcheck source=utils.sh
source "$SCRIPT_DIR/utils.sh"

echo "=== Starting AEP Platform ==="

# Pre-flight — mirrors agent-manager/deployments/scripts/setup-platform.sh.
# These tools are required for the docker compose build + run flow; failing
# fast here beats cryptic errors from `docker compose up` minutes later.
if ! docker info &>/dev/null; then
    echo "❌ Docker is not running. Start Docker / Colima first."
    exit 1
fi
if ! docker compose version &>/dev/null; then
    echo "❌ Docker Compose plugin not installed. Install via Docker Desktop or 'brew install docker-compose'."
    exit 1
fi
if ! docker buildx version &>/dev/null; then
    echo "❌ Docker Buildx plugin not installed. Install via Docker Desktop or 'brew install docker-buildx'."
    exit 1
fi
if [ ! -f "$DEPLOY_DIR/docker-compose.yml" ]; then
    echo "❌ docker-compose.yml missing at $DEPLOY_DIR/docker-compose.yml"
    exit 1
fi
echo "✅ Pre-flight ok (docker + compose + buildx + docker-compose.yml)"

# Load specific env keys needed before `docker compose up` runs envsubst.
# We extract keys individually rather than blanket-sourcing because .env
# values can contain spaces (e.g. VITE_THUNDER_SCOPES="openid profile email")
# which break `set -a; source`.
_load_env_key() {
    [ -n "${!1}" ] && return 0
    [ -f "$DEPLOY_DIR/.env" ] || return 0
    local val
    val=$(grep "^$1=" "$DEPLOY_DIR/.env" | head -1 | cut -d= -f2-)
    val="${val#\"}" ; val="${val%\"}"
    val="${val#\'}" ; val="${val%\'}"
    [ -n "$val" ] && export "$1=$val" || true
}
_load_env_key ANTHROPIC_API_KEY
_load_env_key LOG_LEVEL
_load_env_key PUBLIC_THUNDER_URL
_load_env_key PUBLIC_CONSOLE_URL

# 0. Refresh k3d node DNS (8.8.8.8 fallback for image pulls). k3d's
#    native host.k3d.internal mapping + OC's CoreDNS rewrite handle the
#    rest — see fix_node_dns comment in utils.sh.
#
#    Then assert IN-CLUSTER DNS works. These are two different resolvers:
#    rewriting the node's file does not reach CoreDNS, which snapshotted its
#    upstream when its pod was created. A Colima restart is precisely when
#    CoreDNS ends up pointing at an address that no longer answers, so this is
#    a pre-flight condition — a cluster whose pods can't resolve github.com
#    fails every coding-agent run at `git clone`.
echo ""
echo "🔧 Refreshing k3d node DNS..."
if kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null; then
    fix_node_dns
    ensure_cluster_dns_healthy || exit 1
else
    echo "⚠️  k3d cluster not accessible — skipping DNS refresh (run setup.sh if cluster is missing)"
fi

# 1. OpenBao port bridge — git-service (in docker-compose) needs to reach
#    the k3d-hosted OpenBao at host.docker.internal:8200. Fresh clusters
#    created from k3d-local-config.yaml have the mapping baked in (8200:30820
#    → loadbalancer → openbao-0); pre-existing clusters don't, so we fall
#    back to a kubectl port-forward.
echo ""
echo "🔐 Ensuring OpenBao reachable on :8200..."
if curl -s --max-time 1 http://localhost:8200/v1/sys/health > /dev/null 2>&1; then
    echo "   OpenBao already reachable on :8200"
elif kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null; then
    if pgrep -f "port-forward.*openbao.*8200" > /dev/null 2>&1; then
        echo "   OpenBao port-forward already running"
    else
        kubectl port-forward --context "${CLUSTER_CONTEXT}" -n openbao svc/openbao 8200:8200 \
            > /tmp/aep-openbao-portfwd.log 2>&1 &
        echo "   port-forward PID: $! (logs: /tmp/aep-openbao-portfwd.log)"
        for i in 1 2 3 4 5 6 7 8 9 10; do
            if curl -s --max-time 1 http://localhost:8200/v1/sys/health > /dev/null 2>&1; then
                echo "✅ OpenBao reachable on :8200 (via port-forward)"
                break
            fi
            sleep 1
        done
    fi
else
    echo "⚠️  k3d cluster not accessible — git-service will fail OpenBao readiness"
fi

# 1b. Temporal reachability check (informational). aep-api reaches the
#     in-cluster Temporal frontend directly on the shared k3d docker network
#     (k3d-openchoreo-server-0:31733 NodePort — no host bridge needed), and the
#     Web UI is published on host :8233 by setup-temporal.sh via the k3d
#     loadbalancer. This is just a heads-up when Temporal isn't installed.
echo ""
echo "⏱️  Checking Temporal (devflow workflow engine)..."
if kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null \
        && kubectl --context "${CLUSTER_CONTEXT}" -n temporal get svc temporal-frontend &>/dev/null; then
    echo "   Temporal installed — aep-api reaches it at k3d-${CLUSTER_NAME}-server-0:31733; Web UI: http://localhost:8233"
else
    echo "   Temporal not installed — /devflows will answer 503 (run scripts/setup-temporal.sh to enable workflows)"
fi

# 2. GitHub App private key placeholder — docker-compose mounts the file
#    into git-service. If the operator hasn't dropped a real key, we touch
#    a placeholder so the volume mount succeeds; the seed skips silently
#    and App-mode connect surfaces the error at the connect endpoint.
if [ ! -f "$DEPLOY_DIR/github-app-private-key.pem" ]; then
    echo ""
    echo "🔑 Creating placeholder for GitHub App private key..."
    echo "   Drop a real key at $DEPLOY_DIR/github-app-private-key.pem to enable App-mode connect."
    touch "$DEPLOY_DIR/github-app-private-key.pem"
fi

# 3. Seed an internal kubeconfig for git-service. The host kubeconfig points
#    at https://127.0.0.1:6550 which is unreachable from inside a container.
#    The k3d server container is on the `k3d-openchoreo` docker network as
#    `k3d-openchoreo-server-0`, so rewrite the server URL accordingly.
#    git-service mounts this file at /app/.kube/config (read-only). This
#    mirrors agent-manager's docker-compose pattern (KUBECONFIG env var).
echo ""
echo "🔑 Seeding internal kubeconfig for git-service..."
INTERNAL_KUBE_DIR="$DEPLOY_DIR/.kube"
INTERNAL_KUBECONFIG="$INTERNAL_KUBE_DIR/config"
mkdir -p "$INTERNAL_KUBE_DIR"
if kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null; then
    # Use k3d's "internal" kubeconfig which sets the server to the in-network
    # hostname. If unavailable, fall back to a manual rewrite of the host
    # kubeconfig.
    if k3d kubeconfig get "${CLUSTER_NAME}" --internal > "$INTERNAL_KUBECONFIG" 2>/dev/null; then
        echo "✅ Wrote internal kubeconfig (k3d --internal) → $INTERNAL_KUBECONFIG"
    else
        kubectl config view --raw --minify --context "${CLUSTER_CONTEXT}" > "$INTERNAL_KUBECONFIG"
        # k3d server's in-network port is always 6443 (not the host-mapped 6550)
        # and the container hostname follows the k3d-<cluster>-server-0 pattern.
        sed -i.bak -E "s|server: https?://[^[:space:]]+|server: https://k3d-${CLUSTER_NAME}-server-0:6443|" "$INTERNAL_KUBECONFIG"
        rm -f "$INTERNAL_KUBECONFIG.bak"
        echo "✅ Wrote rewritten kubeconfig → $INTERNAL_KUBECONFIG"
    fi
    # 644 so non-root compose containers (appuser uid 1000) can read the bind mount.
    chmod 644 "$INTERNAL_KUBECONFIG"
else
    echo "⚠️  Cluster unreachable — leaving existing $INTERNAL_KUBECONFIG (may be stale)"
    [ -f "$INTERNAL_KUBECONFIG" ] || touch "$INTERNAL_KUBECONFIG"
fi

# BFF Task JWT signing key — bind-mounted into aep-api as
#    /app/keys/task-signing.pem (docker-compose volume). The BFF reads
#    the PEM from BFF_TASK_SIGNING_KEY_PATH; mounting beats env-passing
#    a multi-line value through compose's `${VAR}` substitution.
TASK_KEY_PATH="$DEPLOY_DIR/keys/task-signing.pem"
if [ ! -f "$TASK_KEY_PATH" ]; then
    echo "⚠️  BFF Task JWT signing key missing at $TASK_KEY_PATH — coding-agent dispatch will fail."
    echo "   Run setup-aep.sh to generate it."
else
    chmod 644 "$TASK_KEY_PATH"
fi

# 6. Sync public URLs (.env → cluster). Touches Thunder ConfigMap, OIDC
#    issuer, HTTPRoute, redirect_uris. Idempotent — fast no-op if nothing
#    changed.
echo ""
if kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null; then
    load_public_urls "$DEPLOY_DIR/.env"
    apply_public_urls_to_cluster
else
    echo "⚠️  k3d cluster not accessible — skipping public-URL sync"
fi

# 6c. Generate the OpenChoreo API client (internal/clients/openchoreo/gen/*.gen.go)
#     if it's missing. Gitignored (source of truth is the pinned OC spec, see
#     the Makefile) — a fresh clone has no gen/ output, and aep-api's
#     Dockerfile does a plain `go build` with no codegen step of its own, so
#     the very first `docker compose up --build` would otherwise fail with
#     "no required module provides package .../gen". The Makefile target hits
#     the network (fetches the pinned spec), so skip it once already generated
#     rather than re-fetching on every start.
OC_GEN_DIR="$ROOT_DIR/services/aep-api/internal/clients/openchoreo/gen"
if [ ! -f "$OC_GEN_DIR/types.gen.go" ] || [ ! -f "$OC_GEN_DIR/client.gen.go" ]; then
    echo ""
    echo "🔧 Generating OpenChoreo API client (first run only)..."
    make -C "$ROOT_DIR/services/aep-api" gen-oc-client
fi

# 7. Bring up the compose stack. Coding-agent runners are OpenChoreo Job
#    Components created by aep-api (AGENT_RUNNER_IMAGE); no long-lived
#    remote-worker service and no ClusterWorkflow for coding.
cd "$DEPLOY_DIR"
echo ""
echo "🐳 Starting Docker services..."
# `.env` is the single source of truth for the smee webhook channel. Docker
# Compose lets an EXPORTED shell var override the `.env` file, so an inherited
# GITHUB_WEBHOOK_PROXY_URL (e.g. left over from a prior setup run) would bake a
# stale channel into the smee-client and silently drop every GitHub webhook —
# GitHub delivers to the .env channel, but the client listens on the exported
# one. Unset it here so the smee-client always subscribes to the .env channel.
unset GITHUB_WEBHOOK_PROXY_URL
docker compose up --build -d
echo "✅ Docker services started"

# 7b. Verify the aep-mcp-server (the OpenChoreo SRE agent's door into AEP —
#     issue creation + coding-agent dispatch during the RCA handoff). Compose
#     starts it; this just fails loudly instead of leaving the handoff to 502
#     at RCA time. See docs/developer-guide/sre-handoff-runbook.md.
echo ""
echo "🤝 Checking aep-mcp-server (SRE-agent handoff endpoint)..."
MCP_OK=""
for i in 1 2 3 4 5 6 7 8 9 10; do
    if curl -s --max-time 2 http://localhost:3401/healthz 2>/dev/null | grep -q '"ok"'; then
        MCP_OK=yes; break
    fi
    sleep 2
done
if [ -n "$MCP_OK" ]; then
    echo "✅ aep-mcp-server healthy on :3401"
else
    echo "⚠️  aep-mcp-server not responding on :3401 — the RCA→coding-agent handoff"
    echo "    will fail its ae_* tool calls. Check: docker logs aep-mcp-server"
fi

# 7c. Verify the cluster half of the handoff — the ai-rca-agent deployment.
#     Best-effort: a rebuilt cluster loses the locally-imported RCA image and
#     the pod sits in ImagePullBackOff; re-running setup-observability.sh
#     fixes it (it re-imports, auto-pulling tharindulak/openchoreo-sre-agent
#     :handoff from Docker Hub if no local build exists).
if kubectl cluster-info --context "${CLUSTER_CONTEXT}" --request-timeout=5s &>/dev/null; then
    RCA_NS="openchoreo-observability-plane"
    if kubectl --context "${CLUSTER_CONTEXT}" -n "$RCA_NS" get deploy ai-rca-agent &>/dev/null; then
        RCA_READY=$(kubectl --context "${CLUSTER_CONTEXT}" -n "$RCA_NS" get deploy ai-rca-agent \
            -o jsonpath='{.status.readyReplicas}' 2>/dev/null)
        RCA_HANDOFF=$(kubectl --context "${CLUSTER_CONTEXT}" -n "$RCA_NS" get cm rca-agent-config \
            -o jsonpath='{.data.AE_HANDOFF}' 2>/dev/null)
        if [ "${RCA_READY:-0}" -ge 1 ]; then
            echo "✅ ai-rca-agent ready (AE_HANDOFF=${RCA_HANDOFF:-unset})"
        elif [ -n "$MCP_OK" ] && [ "$RCA_HANDOFF" = "true" ]; then
            # Expected after a fresh setup.sh: with AE_HANDOFF=true the agent's
            # boot-time MCP test is FATAL, and aep-mcp-server wasn't running
            # until a moment ago — the pod is in CrashLoopBackOff whose next
            # retry may be minutes away. Bounce it now that the MCP is up.
            echo "   ai-rca-agent not ready but aep-mcp-server just came up —"
            echo "   restarting it to break the crash-loop backoff..."
            kubectl --context "${CLUSTER_CONTEXT}" -n "$RCA_NS" rollout restart deploy/ai-rca-agent >/dev/null
            if kubectl --context "${CLUSTER_CONTEXT}" -n "$RCA_NS" rollout status deploy/ai-rca-agent --timeout=180s >/dev/null 2>&1; then
                echo "✅ ai-rca-agent ready (AE_HANDOFF=${RCA_HANDOFF})"
            else
                echo "⚠️  ai-rca-agent still not ready — check:"
                echo "    kubectl logs -n $RCA_NS deploy/ai-rca-agent"
            fi
        else
            echo "⚠️  ai-rca-agent not ready — alert→RCA (and the handoff) won't run."
            echo "    Likely a lost image import after a cluster rebuild. Fix:"
            echo "    bash scripts/setup-observability.sh"
        fi
    else
        echo "ℹ️  ai-rca-agent not installed — run scripts/setup-observability.sh to"
        echo "    enable the alert→RCA→coding-agent pipeline."
    fi
fi

# 8. Repair per-org secrets in OpenBao. When the local cluster (or just the
#    OpenBao volume) has been torn down since the last credential connect,
#    the SM-API metadata rows still point at OpenBao paths that no longer
#    exist. Without this stage every coding-agent dispatch hangs in
#    CreateContainerConfigError. Best-effort: failure here doesn't fail
#    start.sh. Safe in remote envs because repair-secrets.sh refuses to
#    run unless the kubectl context matches the local k3d cluster.
echo ""
if [ -x "$SCRIPT_DIR/repair-secrets.sh" ]; then
    bash "$SCRIPT_DIR/repair-secrets.sh" || \
        echo "⚠️  repair-secrets did not complete cleanly — see output above."
fi

# 9. Local-dev seed. Opt-in: runs only when the operator has set
#    LOCAL_DEV_ADMIN_GITHUB_PAT and/or ANTHROPIC_API_KEY in .env (both
#    are preserved across setup-aep.sh re-runs). Connects the default
#    org's credentials exactly as a user would via Settings — idempotent
#    on re-runs, and best-effort: failure here doesn't fail start.sh.
#    SKIP_DEV_SEED=1 disables the auto-run (e.g. to exercise the manual
#    Settings clickthrough, or to run scripts/seed-dev.sh yourself later).
echo ""
if [ "${SKIP_DEV_SEED:-0}" = "1" ]; then
    echo "⏭️  SKIP_DEV_SEED=1 — skipping dev seed (run scripts/seed-dev.sh manually when needed)"
elif grep -qE '^(LOCAL_DEV_ADMIN_GITHUB_PAT|ANTHROPIC_API_KEY)=.+' "$DEPLOY_DIR/.env" 2>/dev/null; then
    bash "$SCRIPT_DIR/seed-dev.sh" || \
        echo "⚠️  seed-dev did not complete cleanly — see output above."
else
    echo "⏭️  no LOCAL_DEV_ADMIN_GITHUB_PAT / ANTHROPIC_API_KEY in .env — skipping dev seed (scripts/seed-dev.sh)"
fi

echo ""
echo "============================================"
echo "  ✅ All services running!"
echo "============================================"
echo ""
echo "  Console:          http://localhost:8090"
echo "  API:              http://localhost:9090"
echo "  Agents:           http://localhost:4000"
echo "  SRE-handoff MCP:  http://localhost:3401 (alert → AI-RCA → issue → coding agent;"
echo "                    see docs/developer-guide/sre-handoff-runbook.md)"
echo "  Temporal Web UI:  http://localhost:8233   (devflow workflow dashboard)"
echo ""
echo "  Coding-agent:     OpenChoreo Job Component in the project dataplane"
echo "                    (image from AGENT_RUNNER_IMAGE / aep-runner:dev)."
echo ""
echo "  Login: admin / admin (default Thunder admin, in the 'Administrators' group)"
echo ""
echo "  To stop: cd deployments && bash scripts/stop.sh"
