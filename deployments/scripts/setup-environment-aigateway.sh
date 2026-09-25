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

# Provision one environment's AI gateway: the Agent Manager gateway record, the
# runtime that serves it, and the binding annotations aep-api resolves it by.
#
# Usage: cd deployments && bash scripts/setup-environment-aigateway.sh <org> <env>
#
# The AI gateway is the LLM proxy an AMP-governed agent's model traffic flows
# through. It is what makes a PII guardrail enforceable on a running agent
# without a redeploy, and it is per-ENVIRONMENT for the same reason Thunder and
# the API Platform gateway are: AMP issues an agent's model key per environment
# and filters its provider catalog by one.
#
# ── Why the chart's own bootstrap Job is disabled ────────────────────────────
# wso2-amp-ai-gateway-extension ships a post-install Job that registers the
# gateway itself, and it cannot work against this platform IdP. The Job mints
# its token with `grant_type=client_credentials` and NO `scope` parameter; this
# IdP answers that with a scope-less token, and amp-api then refuses its first
# call with 403 "insufficient permissions". amp-api-client is correctly assigned
# amp-role-admin — the role is not the problem, the request is. So this script
# performs the registration with a properly scoped token and installs the chart
# with bootstrap.enabled=false. The same compensation setup-agent-manager.sh
# already applies to Agent Manager's other charts.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"
# shellcheck source=utils.sh
source "$SCRIPT_DIR/utils.sh"

# PUBLIC_THUNDER_URL lives in .env, not env.sh — same call every sibling script
# makes before minting anything.
load_public_urls "$SCRIPT_DIR/../.env"

ORG_NAME="${1:-default}"
ENV_NAME="${2:-default}"
NS="${ORG_NAME}-${ENV_NAME}"
GW_NAME="ai-gateway-${ORG_NAME}-${ENV_NAME}"
RELEASE="$GW_NAME"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-600s}"

# In-cluster for the runtime's own control-plane calls; public for ours, because
# this script runs on the host.
AMP_API_IN="http://amp-api.wso2-amp.svc.cluster.local:9000/api/v1"
AMP_API_OUT="http://api.amp.localhost:8080/api/v1"
VHOST="${AI_GATEWAY_VHOST:-http://ai-gateway.amp.localhost:8084}"

# Explicit scopes are mandatory: a client_credentials mint without them returns
# a token whose `aud` is the client id and which carries no scope claim at all.
SCOPES="amp:gateway:create amp:gateway:read amp:gateway:update amp:gateway:token-manage amp:environment:read amp:org:view"

echo "=== AI gateway for ${ORG_NAME}/${ENV_NAME} ==="

# ── 1. A scoped token ────────────────────────────────────────────────────────
TOKEN="$(curl -s --max-time 30 -X POST "${PUBLIC_THUNDER_URL}/oauth2/token" \
    -u "amp-api-client:amp-api-client-secret" \
    -d grant_type=client_credentials \
    --data-urlencode "scope=${SCOPES}" \
    --data-urlencode "resource=urn:wso2:amp" \
    | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null)"
if [ -z "$TOKEN" ]; then
    echo "❌ could not mint an Agent Manager token from ${PUBLIC_THUNDER_URL}"
    exit 1
fi

# ── 2. The environment's id, by name ─────────────────────────────────────────
# Retried: amp-api answers slowly on a loaded machine, and a single short curl
# turns that into "environment not registered" — a message that sends the
# operator to re-run an install that was never the problem.
ENV_ID=""
for attempt in 1 2 3; do
    ENV_ID="$(curl -s --max-time 30 -H "Authorization: Bearer $TOKEN" \
    "${AMP_API_OUT}/orgs/${ORG_NAME}/environments" \
    | python3 -c "
import sys, json
try:
    envs = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for e in envs if isinstance(envs, list) else envs.get('environments', []):
    if e.get('name') == '${ENV_NAME}':
        print(e.get('id', '')); break" 2>/dev/null)"
    [ -n "$ENV_ID" ] && break
    [ "$attempt" -lt 3 ] && sleep 5
done
if [ -z "$ENV_ID" ]; then
    echo "❌ environment '${ENV_NAME}' is not registered in Agent Manager"
    echo "   Run scripts/setup-agent-manager.sh first."
    exit 1
fi
echo "✅ environment '${ENV_NAME}' resolved (${ENV_ID})"

# ── 3. The gateway record ────────────────────────────────────────────────────
# Idempotent by name: re-running binds to what is there rather than creating a
# second gateway the providers would have to choose between.
GW_ID="$(curl -s --max-time 30 -H "Authorization: Bearer $TOKEN" \
    "${AMP_API_OUT}/orgs/${ORG_NAME}/gateways?limit=100" \
    | python3 -c "
import sys, json
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for g in d.get('gateways', []):
    if g.get('name') == '${GW_NAME}':
        print(g.get('uuid', '')); break" 2>/dev/null)"

if [ -n "$GW_ID" ]; then
    echo "⏭️  gateway '${GW_NAME}' already registered (${GW_ID})"
else
    # gatewayType "AI" is stored and returned as "EGRESS" — AMP's own mapping,
    # and what makes this gateway selectable when a provider is attached to it.
    GW_ID="$(curl -s --max-time 30 -X POST "${AMP_API_OUT}/orgs/${ORG_NAME}/gateways" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        -d "{\"name\":\"${GW_NAME}\",\"displayName\":\"AI Gateway ${ORG_NAME}/${ENV_NAME}\",\"gatewayType\":\"AI\",\"vhost\":\"${VHOST}\",\"environmentIds\":[\"${ENV_ID}\"]}" \
        | python3 -c "import sys,json;print(json.load(sys.stdin).get('uuid',''))" 2>/dev/null)"
    if [ -z "$GW_ID" ]; then
        echo "❌ gateway registration failed"
        exit 1
    fi
    echo "✅ gateway registered (${GW_ID})"
fi

# ── 4. The runtime's registration token ──────────────────────────────────────
# Written where the APIGateway CR's spec.controlPlane.tokenSecretRef points. The
# Secret is left alone when it exists: rotating the token cuts off a controller
# that is already connected.
if kubectl get secret "${GW_NAME}-token" -n "$NS" --context "$CLUSTER_CONTEXT" &>/dev/null; then
    echo "⏭️  registration token Secret already present"
else
    TOKEN_FILE="$(mktemp)"
    trap 'rm -f "$TOKEN_FILE"' EXIT
    curl -s --max-time 30 -X POST "${AMP_API_OUT}/orgs/${ORG_NAME}/gateways/${GW_ID}/tokens" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '' \
        | python3 -c "import sys,json;sys.stdout.write(json.load(sys.stdin).get('token',''))" \
        > "$TOKEN_FILE" 2>/dev/null || true
    if [ ! -s "$TOKEN_FILE" ]; then
        echo "❌ could not generate a registration token for ${GW_NAME}"
        exit 1
    fi
    kubectl create namespace "$NS" --context "$CLUSTER_CONTEXT" &>/dev/null || true
    kubectl create secret generic "${GW_NAME}-token" -n "$NS" --context "$CLUSTER_CONTEXT" \
        --from-file="token=${TOKEN_FILE}" --dry-run=client -o yaml \
        | kubectl apply --context "$CLUSTER_CONTEXT" -f - >/dev/null
    echo "✅ registration token stored in ${NS}/${GW_NAME}-token"
fi

# ── 5. The runtime ───────────────────────────────────────────────────────────
# THE NAMESPACE IS NOT THE CHART'S DEFAULT. It renders into
# openchoreo-data-plane, where the gateway controller then never starts:
#
#   MountVolume.SetUp failed for volume "encryption-keys":
#   secret "api-platform-controller-aesgcm-key" not found
#
# That Secret is per-namespace, written into <org>-<env> by
# setup-environment-gateway.sh, and one gateway-operator means one key name for
# every gateway it deploys. The per-environment namespace is also where this
# belongs on its own merits, beside that environment's API Platform gateway.
echo ""
echo "📦 Installing ${RELEASE} in ${NS}"
helm upgrade --install "$RELEASE" \
    "oci://ghcr.io/wso2/wso2-amp-ai-gateway-extension" \
    --version "${AI_GATEWAY_CHART_VERSION}" \
    --namespace "$NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "agentManager.apiUrl=${AMP_API_IN}" \
    --set "agentManager.orgName=${ORG_NAME}" \
    --set "agentManager.idp.tokenUrl=${THUNDER_INTERNAL_TOKEN_URL}" \
    --set "gateway.name=${GW_NAME}" \
    --set "gateway.environment=${ENV_NAME}" \
    --set "gateway.vhost=${VHOST}" \
    --set "apiGateway.namespace=${NS}" \
    --set "bootstrap.enabled=false" \
    --timeout 15m

echo ""
echo "⏳ Waiting for the AI gateway to serve..."
# Both Deployments are created by the gateway operator AFTER this release is
# installed, so they can still be absent when helm returns — wait for them to
# exist before waiting for them to be Available.
wait_for_deployment_available "$NS" "${RELEASE}-gw-controller" "$WAIT_TIMEOUT"
wait_for_deployment_available "$NS" "${RELEASE}-gw-gateway-runtime" "$WAIT_TIMEOUT"

# ── 6. The binding record ────────────────────────────────────────────────────
# aep-api runs OUTSIDE the cluster, so the Environment's annotations are the one
# projection of this record it can read — the same reason
# setup-environment-thunder.sh writes the Thunder binding there.
# The endpoint has a PUBLIC form and an IN-CLUSTER one, for the same reason the
# Thunder binding carries an issuer and an admin URL: an agent's pod resolves the
# Service, and nothing outside the cluster does. The vhost is published on no host
# port, so the Service address is the one an agent can actually reach — and the
# gateway's router matches on "*", so it serves whichever Host arrives.
INTERNAL_ENDPOINT="http://${RELEASE}-gw-gateway-runtime.${NS}.svc.cluster.local:8084"

kubectl annotate environment "$ENV_NAME" -n "$ORG_NAME" --context "$CLUSTER_CONTEXT" --overwrite \
    "aep.wso2.com/aigateway-endpoint=${VHOST}" \
    "aep.wso2.com/aigateway-internal-endpoint=${INTERNAL_ENDPOINT}" \
    "aep.wso2.com/aigateway-admin-url=${AMP_API_OUT}" \
    "aep.wso2.com/aigateway-gateway=${GW_ID}" \
    "aep.wso2.com/aigateway-secret-path=secret/aep/amp/${ORG_NAME}" \
    "aep.wso2.com/aigateway-binding=${GW_NAME}" >/dev/null

echo ""
echo "✅ AI gateway ready for ${ORG_NAME}/${ENV_NAME}"
echo "   gateway id: ${GW_ID}"
echo "   vhost:      ${VHOST}"
echo "   in-cluster: ${INTERNAL_ENDPOINT}"
