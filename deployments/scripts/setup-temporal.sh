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

# scripts/setup-temporal.sh — installs the Temporal workflow engine that
# drives the AEP devflow workflows (services/aep-api/internal/feature/devflow).
# aep-api runs the Temporal WORKER in-process; this deploys the SERVER + Web UI
# into the k3d cluster, reached from docker-compose via the start.sh port
# bridge (host.docker.internal:7233).
#
# Labs profile: a SINGLE-POD Temporal dev server (`temporal server start-dev`,
# from the temporalio/admin-tools image) persisting to a SQLite file on a PVC.
# There is NO Cassandra and NO schema job — one Deployment + one Service + one
# PVC, which is fast and reliable on a resource-tight k3d. The dev server
# auto-creates the `default` namespace and serves the Web UI on :8233.
#
# The PVC is what makes this usable rather than merely fast. start-dev's DEFAULT
# store is in-memory, and a restart then erases every workflow — which is not a
# tolerable local-demo tradeoff, because the loss is silent and unrecoverable:
# `milestone_runs` in Postgres still reads `state=running`, no workflow exists to
# drive it, nothing reconciles the two, and the console shows a run "preparing
# the coding agent" forever. Observed live: the k3d node container restarted, and
# every in-flight run across every project was orphaned with no terminal reason
# and no way back except clearing rows by hand.
#
# Scope of the durability: the SQLite file survives pod restarts, node-container
# restarts and `stop.sh`/`start.sh`. It does NOT survive `k3d cluster delete`
# (teardown.sh), which removes the node's local-path storage with the cluster.
# Still not a production topology — a demo-scale durable-execution backend.
#
# We deliberately do NOT use the temporalio/temporal Helm chart: it bundles only
# a multi-node Cassandra + separate schema jobs, which are heavy and flaky on a
# local k3d already running OpenChoreo + observability.
#
# Idempotent: kubectl apply is server-side; re-running re-applies the manifest.

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

# Pin the admin-tools image (ships the `temporal` CLI + dev server) so a
# version bump is deliberate.
TEMPORAL_IMAGE="temporalio/admin-tools:1.29.7-tctl-1.18.4-cli-1.7.2"
NS="temporal"

echo "============================================"
echo "  AEP — Temporal workflow engine (dev server)"
echo "============================================"

kubectl cluster-info --context "$CLUSTER_CONTEXT" &>/dev/null || {
    echo "❌ Cluster '$CLUSTER_CONTEXT' not running. Run ./setup.sh first."
    exit 1
}

# If a previous run installed the (retired) Helm chart, remove it so its
# crash-looping Cassandra pods + PVCs don't linger next to the dev server.
if helm status temporal -n "$NS" --kube-context "$CLUSTER_CONTEXT" &>/dev/null; then
    echo "🧹 Removing the retired Temporal Helm release (moving to the dev server)..."
    helm uninstall temporal -n "$NS" --kube-context "$CLUSTER_CONTEXT" >/dev/null 2>&1 || true
    kubectl --context "$CLUSTER_CONTEXT" -n "$NS" delete pvc --all >/dev/null 2>&1 || true
fi

echo "📦 Applying the Temporal dev-server Deployment + Service..."
kubectl --context "$CLUSTER_CONTEXT" apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: ${NS}
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: temporal-state
  namespace: ${NS}
  labels: { app: temporal-frontend }
spec:
  # k3d's default StorageClass (local-path) binds on first use, so no
  # storageClassName is pinned here.
  accessModes: [ReadWriteOnce]
  resources:
    requests: { storage: 2Gi }
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: temporal-frontend
  namespace: ${NS}
  labels: { app: temporal-frontend }
spec:
  replicas: 1
  # Recreate, not RollingUpdate: the PVC is ReadWriteOnce, so a rolling update
  # deadlocks — the new pod cannot mount the volume the old pod still holds.
  strategy:
    type: Recreate
  selector:
    matchLabels: { app: temporal-frontend }
  template:
    metadata:
      labels: { app: temporal-frontend }
    spec:
      # The image runs as uid/gid 1000 (temporal). fsGroup makes the
      # freshly-provisioned volume group-writable, without which start-dev
      # cannot create its SQLite file and the pod crash-loops.
      securityContext:
        fsGroup: 1000
      volumes:
        - name: state
          persistentVolumeClaim: { claimName: temporal-state }
      containers:
        - name: temporal
          image: ${TEMPORAL_IMAGE}
          # start-dev: frontend on :7233, Web UI on :8233, 'default' namespace
          # auto-registered. --ip 0.0.0.0 so both are reachable in-cluster.
          command: ["temporal"]
          args:
            - server
            - start-dev
            - --ip
            - 0.0.0.0
            - --namespace
            - default
            # Persist to the PVC. Without this, start-dev keeps state in memory
            # and every restart silently orphans all in-flight runs.
            - --db-filename
            - /data/temporal.db
            - --log-level
            - warn
          volumeMounts:
            - { name: state, mountPath: /data }
          ports:
            - { name: grpc, containerPort: 7233 }
            - { name: ui, containerPort: 8233 }
          readinessProbe:
            tcpSocket: { port: 7233 }
            initialDelaySeconds: 5
            periodSeconds: 5
          resources:
            requests: { cpu: 100m, memory: 256Mi }
            limits: { cpu: "1", memory: 1Gi }
---
apiVersion: v1
kind: Service
metadata:
  name: temporal-frontend
  namespace: ${NS}
  labels: { app: temporal-frontend }
spec:
  # NodePort with FIXED ports so aep-api (in docker-compose, on the shared
  # k3d-openchoreo network) can reach the frontend at
  # k3d-openchoreo-server-0:31733 — a docker-DNS-resolvable target, unlike a
  # host.docker.internal bridge (see docker-compose.yml TEMPORAL_HOSTPORT).
  type: NodePort
  selector: { app: temporal-frontend }
  ports:
    - { name: grpc, port: 7233, targetPort: 7233, nodePort: 31733 }
    - { name: ui, port: 8233, targetPort: 8233, nodePort: 31823 }
EOF

echo "⏳ Waiting for the Temporal dev server to become ready..."
kubectl --context "$CLUSTER_CONTEXT" -n "$NS" rollout status deployment/temporal-frontend --timeout=180s

# Expose the Web UI on the host via the k3d loadbalancer (host :8233 → nodePort
# 31823), so the browser can open http://localhost:8233. Idempotent: skip if the
# serverlb already publishes 8233. (aep-api does NOT use this — it reaches the
# frontend by the docker-DNS NodePort above; this is browser-only.)
if ! docker ps --filter "name=k3d-${CLUSTER_NAME}-serverlb" --format '{{.Ports}}' 2>/dev/null | grep -q "8233->"; then
    echo "🌐 Publishing the Temporal Web UI on host :8233 via the k3d loadbalancer..."
    k3d cluster edit "${CLUSTER_NAME}" --port-add "8233:31823@loadbalancer" >/dev/null 2>&1 \
        && echo "   host :8233 → Temporal Web UI" \
        || echo "   ⚠️  could not add the LB port (open the UI with: kubectl -n ${NS} port-forward svc/temporal-frontend 8233:8233)"
fi

echo ""
echo "✅ Temporal dev server installed."
echo "   Frontend (gRPC): k3d-${CLUSTER_NAME}-server-0:31733 (aep-api reaches this on the k3d docker network)"
echo "   Web UI:          http://localhost:8233 (via the k3d loadbalancer)"
echo "   Namespace:       'default' (auto-registered by start-dev)"
echo ""
