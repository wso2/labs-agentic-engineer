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

# Turn the sign-in deploy-wait ON for the local compose plane.
#
# aep-api runs as a docker-compose CONTAINER locally, not as a pod, so it has no
# in-cluster apiserver address, ServiceAccount token or CA. cfg.KubeAPI.BaseURL
# is therefore empty, the ThunderApplication CR reader is never wired
# (app.go: "ThunderApplication CR reader disabled"), and a web application's
# deploy verdict never waits for Thunder to carry its sign-in callback. That is
# fine for everyday local work and wrong for testing the wait itself.
#
# This mints a READ-ONLY ServiceAccount in the k3d cluster (thunderapplications:
# get/list/watch and nothing else), writes the cluster CA next to the other local
# keys, and prints the three values to paste into deployments/.env. It changes
# nothing about the cluster's own workloads.
#
# Undo: unset the three KUBE_API_* keys in .env and recreate aep-api. The
# ServiceAccount is harmless to leave behind; `--revoke` deletes it.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENTS_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
KEYS_DIR="${DEPLOYMENTS_DIR}/keys"
CA_PATH="${KEYS_DIR}/k3d-ca.crt"

SA_NAME="aep-thunder-reader"
SA_NAMESPACE="default"
SECRET_NAME="${SA_NAME}-token"
# The apiserver as the aep-api CONTAINER reaches it: both are on the
# k3d-openchoreo docker network, and the serving cert carries
# k3d-openchoreo-serverlb as a SAN.
KUBE_API_URL="https://k3d-openchoreo-serverlb:6443"

if [[ "${1:-}" == "--revoke" ]]; then
  kubectl delete clusterrolebinding "${SA_NAME}" --ignore-not-found
  kubectl delete clusterrole "${SA_NAME}" --ignore-not-found
  kubectl -n "${SA_NAMESPACE}" delete secret "${SECRET_NAME}" --ignore-not-found
  kubectl -n "${SA_NAMESPACE}" delete serviceaccount "${SA_NAME}" --ignore-not-found
  rm -f "${CA_PATH}"
  echo "Revoked. Remove the three KUBE_API_* keys from deployments/.env and recreate aep-api."
  exit 0
fi

if ! kubectl cluster-info >/dev/null 2>&1; then
  echo "ERROR: no reachable kube context. Bring the local cluster up first." >&2
  exit 1
fi

echo "==> Creating the read-only ThunderApplication reader in ${SA_NAMESPACE}"
kubectl apply -f - <<YAML >/dev/null
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ${SA_NAME}
  namespace: ${SA_NAMESPACE}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ${SA_NAME}
rules:
  - apiGroups: ["aep.wso2.com"]
    resources: ["thunderapplications"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ${SA_NAME}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ${SA_NAME}
subjects:
  - kind: ServiceAccount
    name: ${SA_NAME}
    namespace: ${SA_NAMESPACE}
---
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${SA_NAMESPACE}
  annotations:
    kubernetes.io/service-account.name: ${SA_NAME}
type: kubernetes.io/service-account-token
YAML

# The controller fills token/ca.crt asynchronously; a fresh Secret is empty for
# a moment and an empty token reads as a silent 401 later.
echo "==> Waiting for the token controller to populate the Secret"
TOKEN=""
for _ in $(seq 1 30); do
  TOKEN="$(kubectl -n "${SA_NAMESPACE}" get secret "${SECRET_NAME}" -o jsonpath='{.data.token}' 2>/dev/null | base64 -d 2>/dev/null || true)"
  [[ -n "${TOKEN}" ]] && break
  sleep 1
done
if [[ -z "${TOKEN}" ]]; then
  echo "ERROR: the ServiceAccount token was never populated." >&2
  exit 1
fi

mkdir -p "${KEYS_DIR}"
kubectl -n "${SA_NAMESPACE}" get secret "${SECRET_NAME}" -o jsonpath='{.data.ca\.crt}' | base64 -d > "${CA_PATH}"
if [[ ! -s "${CA_PATH}" ]]; then
  echo "ERROR: the cluster CA came back empty (${CA_PATH})." >&2
  exit 1
fi

# Prove the credential before handing it over: a token that cannot LIST is worth
# finding out about here, not as a deploy that silently never waits.
echo "==> Verifying the credential can LIST thunderapplications"
STATUS="$(docker run --rm --network k3d-openchoreo \
  -v "${CA_PATH}:/ca.crt:ro" curlimages/curl:latest \
  -sS --cacert /ca.crt -H "Authorization: Bearer ${TOKEN}" \
  -o /dev/null -w '%{http_code}' --max-time 15 \
  "${KUBE_API_URL}/apis/aep.wso2.com/v1alpha1/thunderapplications" 2>/dev/null || true)"
if [[ "${STATUS}" != "200" ]]; then
  echo "ERROR: LIST returned HTTP ${STATUS:-<none>} (want 200)." >&2
  exit 1
fi
echo "    LIST ok (HTTP 200)"

cat <<EOF

Add these to deployments/.env, then recreate the BFF:

KUBE_API_BASE_URL=${KUBE_API_URL}
KUBE_API_BEARER=${TOKEN}
KUBE_API_CA_FILE=/app/keys/k3d-ca.crt

    docker compose up -d --no-deps aep-api

Confirm it took — this line must NOT appear in the logs:
    "ThunderApplication CR reader disabled"
EOF
