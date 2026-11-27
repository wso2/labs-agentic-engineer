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
#   bash deployments/scripts/setup-environment-aigateway.sh [org] [env]
#
# setup-agent-manager.sh calls this as its last step, so `make dev-env` gets it.
# Run it directly to add a gateway for a further environment.
#
# The AI gateway is the LLM proxy an AMP-governed agent's model traffic flows
# through. It is what makes a PII guardrail enforceable on a running agent
# without a redeploy, and it is per-ENVIRONMENT for the same reason Thunder and
# the API Platform gateway are: AMP issues an agent's model key per environment
# and filters its provider catalog by one.
#
# Without it, an ai-agent deployed here calls Anthropic directly: aep-api reads
# the binding off the Environment's annotations (step 6 below), and with no
# annotations there is no governed path to route onto.
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
#
# ── Why the API selector is set here ─────────────────────────────────────────
# The chart's `apiGateway.apiSelector` defaults to `scope: Cluster`: the gateway
# claims EVERY RestApi in the cluster. The gateway-operator does not refuse a
# RestApi two gateways match — it programs it onto one of them, silently, and
# after this install that one is the AI gateway. Every API deployed from then on
# 404s on the environment's API Platform gateway, which is where every URL the
# platform hands out, the webapp proxies and the validation probes point.
# Nothing fails at deploy time: the pod is Ready, the RestApi is Accepted, and
# the route is on the wrong gateway. The AI gateway's own routes come from the
# Agent Manager control plane, not from RestApi CRs, so it needs none of them;
# it gets the same label selector the API Platform gateway uses
# (`design/agent-manager-convergence.md`, the APIGateway row), on its own name,
# which no trait stamps today. A cluster that already ran with the default keeps
# the RestApis it adopted until they are re-created: delete them and the
# renderedrelease controller re-applies them onto the labelled gateway.

set -euo pipefail

ORG_NAME="${1:-${ORG_NS:-default}}"
ENV_NAME="${2:-${OC_ENV:-development}}"

CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-k3d-${CLUSTER_NAME}}"

# The same DNS suffix the cluster was built on — required here too, since this
# script registers a gateway address with amp-api and a wrong one is written
# into a record rather than rejected. setup-agent-manager.sh exports it when it
# calls this script; running it by hand means passing it.
: "${AE_DOMAIN:?set AE_DOMAIN — the suffix this cluster is published under (localhost for local k3d)}"
AMP_DOMAIN="amp.${AE_DOMAIN}"

# Same contract as AE_DOMAIN above: setup-agent-manager.sh exports WITH_TLS when
# it calls this script, and re-deriving here is what makes a by-hand run come
# out the same rather than silently defaulting to plain HTTP.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/tls-env.sh
. "${SCRIPT_DIR}/lib/tls-env.sh"

PUBLIC_THUNDER_URL="${PUBLIC_THUNDER_URL:-${SCHEME}://thunder.openchoreo.${AE_DOMAIN}:${CP_PORT}}"
THUNDER_NS="${THUNDER_NS:-thunder}"
THUNDER_RELEASE="${THUNDER_RELEASE:-thunder}"
AMP_NS="${AMP_NS:-wso2-amp}"

# The AI gateway chart moves on its own release line (0.14.x), independent of
# AMP_VERSION, so it gets its own pin rather than riding the Agent Manager one.
AI_GATEWAY_CHART_VERSION="${AI_GATEWAY_CHART_VERSION:-0.14.0-rc1}"

NS="${ORG_NAME}-${ENV_NAME}"
GW_NAME="ai-gateway-${ORG_NAME}-${ENV_NAME}"
RELEASE="$GW_NAME"
# The label the gateway-operator matches a RestApi to a gateway by; the
# api-configuration trait stamps it with the API Platform gateway's name.
RESTAPI_TARGET_LABEL="gateway.api-platform.wso2.com/restapi-target"
# The selector this gateway is installed with and checked against afterwards:
# a label match on its own name, which no trait stamps, so it selects nothing.
API_SELECTOR="{\"scope\":\"LabelSelector\",\"matchLabels\":{\"${RESTAPI_TARGET_LABEL}\":\"${GW_NAME}\"}}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-600s}"

# In-cluster for the runtime's own control-plane calls; public for ours, because
# this script runs on the host.
AMP_API_IN="http://amp-api.${AMP_NS}.svc.cluster.local:9000/api/v1"
AMP_API_OUT="${AMP_API_URL:-${SCHEME}://api.${AMP_DOMAIN}:${CP_PORT}/api/v1}"
# The Agent Manager console a browser opens, for the console's "Manage in Agent
# Manager" link on an agent's Deployments panel. Public, like AMP_API_OUT.
# Optional to aep-api: without it an agent just has no link.
AMP_CONSOLE_OUT="${AMP_CONSOLE_URL:-${SCHEME}://console.${AMP_DOMAIN}:${CP_PORT}}"
VHOST="${AI_GATEWAY_VHOST:-http://ai-gateway.${AMP_DOMAIN}:8084}"

kubectl() { command kubectl --context "$CLUSTER_CONTEXT" "$@"; }
fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

echo ""
echo "🤖 AI gateway for ${ORG_NAME}/${ENV_NAME}"

# ThunderID's Service is not named after its release — the chart appends its own
# suffix — so it is found by label, the same way setup-agent-manager.sh does. A
# wrong address here is silent: the runtime's token mints would fail at runtime,
# long after this script reported success.
THUNDER_SVC="$(kubectl -n "$THUNDER_NS" get svc -l "app.kubernetes.io/instance=${THUNDER_RELEASE}" \
    -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
[ -n "$THUNDER_SVC" ] \
    || fail "No Service in namespace ${THUNDER_NS} carries app.kubernetes.io/instance=${THUNDER_RELEASE}." \
            "The gateway runtime mints its Agent Manager tokens through it."
THUNDER_INTERNAL_TOKEN_URL="http://${THUNDER_SVC}.${THUNDER_NS}.svc.cluster.local:8090/oauth2/token"

# wait_for_deployment_available — both of the Deployments below are created by
# the gateway operator AFTER this release installs, so they can still be absent
# when helm returns. Waiting for Available on an object that does not exist yet
# fails immediately; wait for it to be created first.
wait_for_deployment_available() {
    local ns="$1" name="$2" timeout="${3:-600s}"
    kubectl wait --for=create "deployment/${name}" -n "$ns" --timeout="$timeout" >/dev/null
    kubectl wait --for=condition=Available "deployment/${name}" -n "$ns" --timeout="$timeout" >/dev/null
}

# ── 1. A scoped token ────────────────────────────────────────────────────────
# Explicit scopes are mandatory: a client_credentials mint without them returns
# a token whose `aud` is the client id and which carries no scope claim at all —
# the same omission that makes the chart's own bootstrap Job unusable here.
SCOPES="amp:gateway:create amp:gateway:read amp:gateway:update amp:gateway:token-manage amp:environment:read amp:org:view"
TOKEN="$(curl -s --max-time 30 -X POST "${PUBLIC_THUNDER_URL}/oauth2/token" \
    -u "amp-api-client:amp-api-client-secret" \
    -d grant_type=client_credentials \
    --data-urlencode "scope=${SCOPES}" \
    --data-urlencode "resource=urn:wso2:amp" \
    | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null || true)"
[ -n "$TOKEN" ] \
    || fail "Could not mint an Agent Manager token from ${PUBLIC_THUNDER_URL}." \
            "amp-api-client is published by setup-env-for-aectl.sh's bootstrap; check it imported."

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
        print(e.get('id', '')); break" 2>/dev/null || true)"
    [ -n "$ENV_ID" ] && break
    [ "$attempt" -lt 3 ] && sleep 5
done
[ -n "$ENV_ID" ] \
    || fail "Environment '${ENV_NAME}' is not registered in Agent Manager." \
            "setup-agent-manager.sh registers it; this script runs after that step."
echo "   ✅ environment resolved (${ENV_ID})"

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
        print(g.get('uuid', '')); break" 2>/dev/null || true)"

if [ -n "$GW_ID" ]; then
    echo "   ⏭️  gateway already registered (${GW_ID})"
else
    # gatewayType "AI" is stored and returned as "EGRESS" — AMP's own mapping,
    # and what makes this gateway selectable when a provider is attached to it.
    GW_ID="$(curl -s --max-time 30 -X POST "${AMP_API_OUT}/orgs/${ORG_NAME}/gateways" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
        -d "{\"name\":\"${GW_NAME}\",\"displayName\":\"AI Gateway ${ORG_NAME}/${ENV_NAME}\",\"gatewayType\":\"AI\",\"vhost\":\"${VHOST}\",\"environmentIds\":[\"${ENV_ID}\"]}" \
        | python3 -c "import sys,json;print(json.load(sys.stdin).get('uuid',''))" 2>/dev/null || true)"
    [ -n "$GW_ID" ] || fail "Gateway registration failed for ${GW_NAME}."
    echo "   ✅ gateway registered (${GW_ID})"
fi

# ── 4. The runtime's registration token ──────────────────────────────────────
# Written where the APIGateway CR's spec.controlPlane.tokenSecretRef points. The
# Secret is left alone when it exists: rotating the token cuts off a controller
# that is already connected.
if kubectl get secret "${GW_NAME}-token" -n "$NS" &>/dev/null; then
    echo "   ⏭️  registration token Secret already present"
else
    TOKEN_FILE="$(mktemp)"
    trap 'rm -f "$TOKEN_FILE"' EXIT
    curl -s --max-time 30 -X POST "${AMP_API_OUT}/orgs/${ORG_NAME}/gateways/${GW_ID}/tokens" \
        -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '' \
        | python3 -c "import sys,json;sys.stdout.write(json.load(sys.stdin).get('token',''))" \
        > "$TOKEN_FILE" 2>/dev/null || true
    [ -s "$TOKEN_FILE" ] || fail "Could not generate a registration token for ${GW_NAME}."
    kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    kubectl create secret generic "${GW_NAME}-token" -n "$NS" \
        --from-file="token=${TOKEN_FILE}" --dry-run=client -o yaml \
        | kubectl apply -f - >/dev/null
    echo "   ✅ registration token stored in ${NS}/${GW_NAME}-token"
fi

# ── 5. The runtime ───────────────────────────────────────────────────────────
# THE NAMESPACE IS NOT THE CHART'S DEFAULT. It renders into
# openchoreo-data-plane, where the gateway controller then never starts:
#
#   MountVolume.SetUp failed for volume "encryption-keys":
#   secret "api-platform-controller-aesgcm-key" not found
#
# That Secret is per-namespace, written into <org>-<env> by the environment's
# API Platform gateway install, and one gateway-operator means one key name for
# every gateway it deploys. The per-environment namespace is also where this
# belongs on its own merits, beside that environment's API Platform gateway.
echo "   ⏳ installing ${RELEASE} in ${NS}..."
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
    --set-json "apiGateway.apiSelector=${API_SELECTOR}" \
    --set "bootstrap.enabled=false" \
    --timeout 15m >/dev/null

wait_for_deployment_available "$NS" "${RELEASE}-gw-controller" "$WAIT_TIMEOUT"
wait_for_deployment_available "$NS" "${RELEASE}-gw-gateway-runtime" "$WAIT_TIMEOUT"
echo "   ✅ gateway runtime serving"

# The selector the operator holds, read back rather than trusted and compared
# whole: a chart version that renames `apiGateway.apiSelector` would take the
# override above as an unused value and keep its Cluster default, and one that
# adds entries of its own would narrow what was asked for; nothing would say so
# until the next deploy 404s. `aectl platform status` reports the same under
# Gateways for a cluster that drifted after install.
INSTALLED_SELECTOR="$(kubectl -n "$NS" get "apigateways.gateway.api-platform.wso2.com/${GW_NAME}" \
    -o jsonpath='{.spec.apiSelector}')" \
    || fail "Could not read APIGateway ${NS}/${GW_NAME} back after the install."
python3 -c 'import json, sys; sys.exit(0 if json.loads(sys.argv[1] or "{}") == json.loads(sys.argv[2]) else 1)' \
    "$INSTALLED_SELECTOR" "$API_SELECTOR" \
    || fail "APIGateway ${NS}/${GW_NAME} selects ${INSTALLED_SELECTOR:-nothing}, not ${API_SELECTOR}." \
            "With scope Cluster it claims every RestApi in the cluster — see 'Why the API selector is set here' above. The chart's apiGateway.apiSelector value may have moved in ${AI_GATEWAY_CHART_VERSION}."
echo "   ✅ API selector pinned to ${GW_NAME}"

# ── 6. The binding record ────────────────────────────────────────────────────
# aep-api runs OUTSIDE the cluster, so the Environment's annotations are the one
# projection of this record it can read — the same reason this environment's
# Thunder binding is written there.
#
# The endpoint has a PUBLIC form and an IN-CLUSTER one, for the same reason the
# Thunder binding carries an issuer and an admin URL: an agent's pod resolves the
# Service, and nothing outside the cluster does. The vhost is published on no host
# port, so the Service address is the one an agent can actually reach — and the
# gateway's router matches on "*", so it serves whichever Host arrives.
INTERNAL_ENDPOINT="http://${RELEASE}-gw-gateway-runtime.${NS}.svc.cluster.local:8084"

kubectl annotate environment "$ENV_NAME" -n "$ORG_NAME" --overwrite \
    "aep.wso2.com/aigateway-endpoint=${VHOST}" \
    "aep.wso2.com/aigateway-internal-endpoint=${INTERNAL_ENDPOINT}" \
    "aep.wso2.com/aigateway-admin-url=${AMP_API_OUT}" \
    "aep.wso2.com/aigateway-gateway=${GW_ID}" \
    "aep.wso2.com/aigateway-binding=${GW_NAME}" \
    "aep.wso2.com/amp-console-url=${AMP_CONSOLE_OUT}" >/dev/null

# The annotations ARE the binding, and aep-api reads them rather than being told:
# a missing one is not a degraded gateway, it is an agent that keeps calling
# Anthropic directly with nothing reporting why. Read back what landed.
for key in aigateway-endpoint aigateway-internal-endpoint aigateway-admin-url \
           aigateway-gateway aigateway-binding; do
    value="$(kubectl get environment "$ENV_NAME" -n "$ORG_NAME" \
        -o "jsonpath={.metadata.annotations.aep\\.wso2\\.com/${key}}" 2>/dev/null || true)"
    [ -n "$value" ] \
        || fail "Environment/${ENV_NAME} is missing the aep.wso2.com/${key} annotation." \
                "aep-api resolves the AI gateway from these; an agent would keep calling its model directly."
done

echo "   ✅ AI gateway bound to Environment/${ENV_NAME}"
echo "      gateway id: ${GW_ID}"
echo "      in-cluster: ${INTERNAL_ENDPOINT}"
