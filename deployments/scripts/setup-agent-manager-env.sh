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

# Second half of the Agent Manager install: the `default` environment's own
# Thunder, and the API Platform gateway in front of it.
#
# Split from setup-agent-manager.sh because both steps talk to amp-api over its
# PUBLIC url and drive Agent Manager's own admin API — they need the platform to
# be not just installed but serving, and they fail in ways that have nothing to
# do with the chart installs before them. Keeping them separate means a failure
# here does not leave the core install looking broken.
#
# ── The second Thunder tier ─────────────────────────────────────────────────
#
# This is NOT the shared platform IdP. Agent Manager runs one Thunder per
# environment, for AgentID / workload identity, and pulls the UPSTREAM
# `thunderid` chart directly at its own pinned version — deliberately decoupled
# from AMP_VERSION and from whatever the platform tier happens to run. The
# API Platform gateway consumes it as a ThunderKeyManager to validate JWTs on
# agent endpoints.
#
# Convergence does not touch this tier. Only the platform tier is shared,
# because OpenChoreo's control plane has exactly one OIDC issuer.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

load_public_urls "$SCRIPT_DIR/../.env"

ENV_NAME="${ENV_NAME:-default}"
ORG_NAME="${ORG_NAME:-default}"
AMP_API_URL="${AMP_API_URL:-http://api.amp.localhost:8080/api/v1}"
IDP_TOKEN_URL="${IDP_TOKEN_URL:-${PUBLIC_THUNDER_URL}/oauth2/token}"
GATEWAY_NS="${ORG_NAME}-${ENV_NAME}"
GATEWAY_RELEASE="api-platform-${ORG_NAME}-${ENV_NAME}"
GATEWAY_VHOST="http://${ORG_NAME}-${ENV_NAME}.gateway.localhost:19080"
AM_REF="amp/v${AMP_VERSION}"
AM_SCRIPT_BASE="https://raw.githubusercontent.com/wso2/agent-manager/${AM_REF}/deployments/scripts"

echo "============================================"
echo "  Agent Manager — '${ENV_NAME}' environment"
echo "============================================"

# amp-api has to be answering on its public URL, not merely Ready: the scripts
# below register resources through it.
echo ""
echo "⏳ Waiting for amp-api on ${AMP_API_URL}..."
for _ in $(seq 1 60); do
    if curl -fsS -o /dev/null -w '' "${AMP_API_URL%/api/v1}/health" 2>/dev/null \
       || curl -fsS -o /dev/null "${AMP_API_URL}" 2>/dev/null; then
        break
    fi
    sleep 5
done
echo "✅ amp-api reachable"

# ── 1. The environment's own Thunder ────────────────────────────────────────
# Fetched from Agent Manager's release rather than vendored: the script and its
# two helper libraries are versioned together with the charts, and a stale copy
# here would drift from the chart it provisions against. CHART_VERSION is left
# unset on purpose so the script pins its own validated ThunderID release —
# AMP_VERSION has no bearing on which ThunderID an env-Thunder runs.
echo ""
echo "1️⃣  Provisioning the environment's Thunder"
ENV_THUNDER_SCRIPT="$(mktemp)"
trap 'rm -f "$ENV_THUNDER_SCRIPT"' EXIT
fetch_gh_raw "${AM_SCRIPT_BASE}/add-environment-thunder.sh" "$ENV_THUNDER_SCRIPT"

ENV_NAME="$ENV_NAME" \
DISPLAY_NAME="${DISPLAY_NAME:-Default}" \
ORG_NAME="$ORG_NAME" \
THUNDER_HANDLE="${THUNDER_HANDLE:-${ENV_NAME}-idp}" \
AMP_API_URL="$AMP_API_URL" \
IDP_TOKEN_URL="$IDP_TOKEN_URL" \
SCRIPT_BASE_URL="$AM_SCRIPT_BASE" \
    bash "$ENV_THUNDER_SCRIPT"
echo "✅ environment Thunder provisioned"

# ── 2. The API Platform gateway for that environment ────────────────────────
echo ""
echo "2️⃣  API Platform gateway"

# Sandboxed agents may egress only to namespaces carrying this label, so it has
# to be on the namespace before the gateway runtime starts.
kubectl create namespace "$GATEWAY_NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl label namespace "$GATEWAY_NS" "amp.wso2.com/api-platform-gateway=true" --overwrite >/dev/null

# gateway-controller 1.2.x mounts an AES-256 at-rest encryption key from a
# Secret in its OWN namespace — AEP's key lives in openchoreo-data-plane and is
# not visible here. Generated once and left alone on re-runs: rotating it drops
# all encrypted gateway state.
if ! kubectl get secret "${GATEWAY_ENCRYPTION_SECRET_NAME}" -n "$GATEWAY_NS" &>/dev/null; then
    key_tmp="$(mktemp)"
    openssl rand 32 > "$key_tmp"
    kubectl create secret generic "${GATEWAY_ENCRYPTION_SECRET_NAME}" -n "$GATEWAY_NS" \
        "--from-file=${GATEWAY_ENCRYPTION_SECRET_KEY}=${key_tmp}" >/dev/null
    rm -f "$key_tmp"   # never leave the plaintext key on disk
    echo "   ✅ gateway encryption key created"
else
    echo "   ✅ gateway encryption key already present (preserved)"
fi

# Wire the gateway's ThunderKeyManager to the environment's own Thunder.
# keymanagers[0] is re-asserted alongside [1] because this install passes no
# values file, and --set on [1] alone would drop [0].
#
# The release-name / issuer derivation is FETCHED from Agent Manager's own
# thunder-naming.sh rather than reimplemented here. That file is explicit that
# it is the single source of truth for the bash side of this algorithm (53-char
# cap, truncate-to-46 plus a sha256-6 suffix) and that no copy should exist
# elsewhere — a second implementation that drifted would silently point the
# gateway at a Thunder that is not there.
THUNDER_NAMING_LIB="$(mktemp)"
trap 'rm -f "$ENV_THUNDER_SCRIPT" "$THUNDER_NAMING_LIB"' EXIT
fetch_gh_raw "${AM_SCRIPT_BASE}/thunder-naming.sh" "$THUNDER_NAMING_LIB"
# shellcheck source=/dev/null
source "$THUNDER_NAMING_LIB"
ENV_THUNDER_RELEASE="$(thunder_release_name "$ORG_NAME" "$ENV_NAME")"
thunder_args=()
if helm status "$ENV_THUNDER_RELEASE" --namespace "$ENV_THUNDER_RELEASE" &>/dev/null; then
    ENV_THUNDER_JWKS="http://${ENV_THUNDER_RELEASE}-service.${ENV_THUNDER_RELEASE}.svc.cluster.local:8090/oauth2/jwks"
    ENV_THUNDER_ISSUER="$(thunder_issuer "${THUNDER_HANDLE:-${ENV_NAME}-idp}")"
    thunder_args=(
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[0].name=agent-manager-service"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[0].issuer=agent-manager-service"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[0].jwks.remote.uri=http://amp-api.wso2-amp.svc.cluster.local:9000/auth/external/jwks.json"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[0].jwks.remote.skipTlsVerify=true"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[1].name=ThunderKeyManager"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[1].issuer=${ENV_THUNDER_ISSUER}"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[1].jwks.remote.uri=${ENV_THUNDER_JWKS}"
        --set "apiGateway.config.policyConfigurations.jwtauth_v1.keymanagers[1].jwks.remote.skipTlsVerify=true"
        --set "bootstrap.identityProviders[0].name=ThunderKeyManager"
        --set "bootstrap.identityProviders[0].issuer=${ENV_THUNDER_ISSUER}"
        --set "bootstrap.identityProviders[0].jwksUri=${ENV_THUNDER_JWKS}"
        --set "bootstrap.identityProviders[0].skipTlsVerify=true"
    )
else
    echo "   ⚠️  no env-Thunder release found — the gateway keeps its default ThunderKeyManager"
fi

helm upgrade --install "$GATEWAY_RELEASE" \
    "${AMP_REGISTRY}/wso2-amp-api-platform-gateway-extension" \
    --version "${AMP_VERSION}" \
    --namespace "$GATEWAY_NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
    --set "apiGateway.namespace=${GATEWAY_NS}" \
    --set "agentManager.orgName=${ORG_NAME}" \
    --set "gateway.environment=${ENV_NAME}" \
    --set "gateway.vhost=${GATEWAY_VHOST}" \
    "${thunder_args[@]}" \
    --timeout 20m

echo "⏳ Waiting for the gateway bootstrap Job..."
kubectl wait --for=condition=complete "job/${GATEWAY_RELEASE}-bootstrap" \
    -n "$GATEWAY_NS" --timeout=300s

# Registration completing is not the same as the runtime serving traffic.
echo "⏳ Waiting for the gateway to be Programmed..."
kubectl wait --for=condition=Programmed "apigateway/${GATEWAY_RELEASE}" -n "$GATEWAY_NS" --timeout=300s
kubectl wait --for=condition=Available \
    "deployment/${GATEWAY_RELEASE}-gw-gateway-gateway-runtime" -n "$GATEWAY_NS" --timeout=300s
kubectl wait --for=condition=Programmed "restapi/${GATEWAY_RELEASE}-otel-restapi" \
    -n "$GATEWAY_NS" --timeout=300s

echo ""
echo "✅ '${ENV_NAME}' environment ready — Thunder + API Platform gateway at ${GATEWAY_VHOST}"
