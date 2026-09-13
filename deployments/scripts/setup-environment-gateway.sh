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

# Usage
#
#   bash setup-environment-gateway.sh <org> <env>
#   ORG_NAME=<org> ENV_NAME=<env> bash setup-environment-gateway.sh
#
# Gives an environment its own API Platform gateway. One gateway per (org, env),
# never one per cluster: the gateway is where a managed API's authentication is
# terminated, and an environment's APIs must be terminated against THAT
# environment's identity tier — the environment Thunder (T2)
# setup-environment-thunder.sh provisions — and against nothing else.
#
#   RestApi label   gateway.api-platform.wso2.com/restapi-target
#                     = api-platform-<org>-<env>
#   APIGateway      api-platform-<org>-<env> in namespace <org>-<env>
#   runtime Service api-platform-<org>-<env>-gw-gateway-gateway-runtime
#                     .<org>-<env>  port 22893 (https 22894)
#   kgateway vhost  <env>-<org>.gateway.localhost, served on the shared
#                   data-plane Gateway `gateway-default` at :19080
#
# Every one of those five names is the CHART's derivation, not this script's
# invention — see the chart's _helpers.tpl (apiGatewayName, restApiTarget,
# gatewayHostname, runtimeUrl). Three other places in this repo must agree with
# them and are commented to say so:
#   * manifests/api-platform/api-configuration-trait.yaml (and the platform
#     chart's copy) — the RestApi label and the Backend host:port
#   * services/aep-api/internal/projects/gateway_address.go — APIGatewayHost
#   * verify-convergence.sh check 5 — every Environment has its gateway
#
# ── Where the identity half comes from ──────────────────────────────────────
#
# The gateway's `ThunderKeyManager` is read from the BINDING ConfigMap that
# setup-environment-thunder.sh writes (labels aep.wso2.com/kind=thunder-binding
# + /org + /env). Not from a naming derivation, and not from the platform IdP:
# the binding is the record of the instance that actually exists for this
# environment, whichever publisher created it. No binding ⇒ this script stops,
# because a gateway wired to a guessed issuer fails as a 401 on the
# environment's APIs with nothing pointing back here.
#
# Consequence, and the point of the second tier: a platform-IdP (T1) token is NOT
# valid at an environment's gateway, and neither is a sibling environment's T2
# token. Only the environment's own T2 is a keymanager here.
#
# ── Two modes ───────────────────────────────────────────────────────────────
#
#   CREATE  no deployed release for (org, env) → install the chart.
#   BIND    a deployed release exists (Agent Manager created it, or an earlier
#           run did) → no helm upgrade. Same publisher rule the T2 script
#           follows: never upgrade a release you did not create. The binding is
#           still checked against what the release is actually configured with,
#           and a mismatch is reported loudly rather than silently converged.
#
# Inputs (all optional):
#   AMP_API_URL        Agent Manager's API (default in env.sh).
#                      Whether it ANSWERS decides bootstrap.enabled — the chart's
#                      pre-install hook Job registers the gateway in Agent
#                      Manager, and there is nothing to register with on a
#                      cluster where Agent Manager was torn down.
#   WAIT_TIMEOUT       (default 300s)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

ORG_NAME="${1:-${ORG_NAME:-}}"
ENV_NAME="${2:-${ENV_NAME:-}}"
if [ -z "$ORG_NAME" ] || [ -z "$ENV_NAME" ]; then
    echo "usage: $(basename "$0") <org> <env>   (or ORG_NAME=… ENV_NAME=…)" >&2
    exit 2
fi

WAIT_TIMEOUT="${WAIT_TIMEOUT:-300s}"

validate_dns_label "$ORG_NAME" "$ENV_NAME" || exit 1

# The chart's own derivations, spelled here because this script waits on the
# objects by name. gateway.name / gateway.hostname are left unset in the values
# below so the chart derives exactly these.
NS="${ORG_NAME}-${ENV_NAME}"
RELEASE="api-platform-${ORG_NAME}-${ENV_NAME}"
GATEWAY_NAME="$RELEASE"
# env FIRST, org second. The chart derives its kgateway hostname as
# "<environment>-<orgName>.gateway.localhost" and the vhost is what gets
# REGISTERED in Agent Manager — which is write-once there, so a vhost that
# disagrees with the hostname the HTTPRoute actually serves cannot be corrected
# later, only warned about. An earlier version of the caller passed
# "<org>-<env>" and the two only coincide when org and env are the same word.
GATEWAY_HOSTNAME="${ENV_NAME}-${ORG_NAME}.gateway.localhost"
GATEWAY_VHOST="http://${GATEWAY_HOSTNAME}:19080"
RUNTIME_SVC="${GATEWAY_NAME}-gw-gateway-gateway-runtime"

echo "============================================"
echo "  Environment gateway — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"

# ── The binding ─────────────────────────────────────────────────────────────
# By LABEL, never by name: the binding's name is an implementation detail of the
# script that writes it, and its namespace is the T2's, which is derived by
# Agent Manager's naming library and can be truncated for long (org, env) pairs.
echo ""
echo "🔎 Resolving the environment Thunder binding"
binding="$(thunder_binding_configmap "$ORG_NAME" "$ENV_NAME")"
if [ -z "$binding" ]; then
    echo "❌ No thunder-binding ConfigMap for ${ORG_NAME}/${ENV_NAME}." >&2
    echo "   The gateway's ThunderKeyManager is read from it — run setup-environment-thunder.sh first:" >&2
    echo "     bash ${SCRIPT_DIR}/setup-environment-thunder.sh ${ORG_NAME} ${ENV_NAME}" >&2
    exit 1
fi
BINDING_NS="${binding%%/*}"
BINDING_NAME="${binding##*/}"

T2_ISSUER="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" issuer)"
T2_ADMIN_URL="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" adminURL)"
if [ -z "$T2_ISSUER" ] || [ -z "$T2_ADMIN_URL" ]; then
    echo "❌ Binding ${BINDING_NS}/${BINDING_NAME} has no issuer/adminURL — not a binding this script understands." >&2
    exit 1
fi
# The JWKS endpoint is derived from the admin URL rather than carried as its own
# key: adminURL is the instance's in-cluster base URL, and ThunderID serves
# /oauth2/jwks off it. The gateway runtime fetches this from inside the cluster,
# so it is the in-cluster address that belongs here, never the public issuer.
T2_JWKS_URL="${T2_ADMIN_URL%/}/oauth2/jwks"
echo "   binding:  ${BINDING_NS}/${BINDING_NAME}"
echo "   issuer:   ${T2_ISSUER}"
echo "   jwks:     ${T2_JWKS_URL}"

# ── Is there an Agent Manager to register with? ─────────────────────────────
if amp_api_present; then
    BOOTSTRAP=true
    echo "   Agent Manager: answering at ${AMP_API_URL} — the gateway will register itself"
else
    BOOTSTRAP=false
    echo "   Agent Manager: not answering at ${AMP_API_URL} — installing without registration"
fi

# ── Namespace ───────────────────────────────────────────────────────────────
# Whether the namespace already existed is decided BEFORE it is applied: it is
# the ownership marker remove-environment-thunder.sh reads to decide whether it
# may delete this namespace, and after the apply the answer is unknowable.
NS_CREATED=false
kubectl get namespace "$NS" --context "$CLUSTER_CONTEXT" >/dev/null 2>&1 || NS_CREATED=true
kubectl create namespace "$NS" --context "$CLUSTER_CONTEXT" --dry-run=client -o yaml \
    | kubectl apply --context "$CLUSTER_CONTEXT" -f - >/dev/null
# Sandboxed agents may egress only to namespaces carrying this label, so it has
# to be on the namespace before the gateway runtime starts.
kubectl label namespace "$NS" --context "$CLUSTER_CONTEXT" \
    "amp.wso2.com/api-platform-gateway=true" --overwrite >/dev/null
if [ "$NS_CREATED" = true ]; then
    kubectl label namespace "$NS" --context "$CLUSTER_CONTEXT" \
        "aep.wso2.com/namespace-created-by=setup-environment-gateway.sh" --overwrite >/dev/null
fi

# ── The gateway's at-rest encryption key ────────────────────────────────────
# gateway-controller 1.2.x mounts an AES-256 key from a Secret in its OWN
# namespace, so every per-environment gateway needs its own copy. The NAME is
# set once on the gateway OPERATOR (manifests/api-platform/operator-values.yaml)
# and applied by it to every gateway it deploys — see env.sh
# GATEWAY_ENCRYPTION_SECRET_NAME for why that name is AEP's.
#
# Generated once and left alone on re-runs: rotating it makes every already
# encrypted gateway secret undecryptable.
if kubectl get secret "$GATEWAY_ENCRYPTION_SECRET_NAME" -n "$NS" --context "$CLUSTER_CONTEXT" &>/dev/null; then
    echo "   ✅ gateway encryption key already present (preserved)"
else
    key_tmp="$(mktemp)"
    openssl rand 32 > "$key_tmp"
    kubectl create secret generic "$GATEWAY_ENCRYPTION_SECRET_NAME" -n "$NS" --context "$CLUSTER_CONTEXT" \
        "--from-file=${GATEWAY_ENCRYPTION_SECRET_KEY}=${key_tmp}" >/dev/null
    rm -f "$key_tmp"   # never leave the plaintext key on disk
    echo "   ✅ gateway encryption key created"
fi

# ── The control-plane registration token ────────────────────────────────────
# The chart's APIGateway CR always names a token Secret, and the gateway
# controller reads it as a NON-optional env var (APIP_GW_CONTROLLER_CONTROLPLANE_TOKEN)
# — a missing Secret leaves the pod in CreateContainerConfigError forever, with
# the APIGateway stuck Programmed=False.
#
# With Agent Manager present the chart's bootstrap Job creates that Secret when
# it registers. Without one there is nothing to register with and no token to
# hold, so the Secret is created empty: the controller starts, finds no control
# plane, and serves the RestApis the gateway-operator pushes into it locally —
# which is the whole of what AEP needs from it. Never overwritten, so a later
# run with Agent Manager present does not clobber a real token.
TOKEN_SECRET="${RELEASE}-token"
if [ "$BOOTSTRAP" = false ] \
   && ! kubectl get secret "$TOKEN_SECRET" -n "$NS" --context "$CLUSTER_CONTEXT" &>/dev/null; then
    kubectl create secret generic "$TOKEN_SECRET" -n "$NS" --context "$CLUSTER_CONTEXT" \
        --from-literal=token="" >/dev/null
    echo "   ✅ empty control-plane token Secret created (no Agent Manager to register with)"
fi

# ── Values ──────────────────────────────────────────────────────────────────
# A values FILE, not a wall of --set: Helm silently ignores an unknown --set
# path, so a typo in one of these keys would install a gateway that looks
# correct and trusts the wrong issuer.
#
# keymanagers[0] (agent-manager-service) is re-asserted alongside [1] because
# this file REPLACES the chart's default list rather than merging into it.
# Every field of [1] is spelled out because the chart's static default names
# Agent Manager's own Thunder release and issuer, neither of which exists here.
#
# bootstrap.identityProviders is deliberately NOT the same list: it is what
# Agent Manager exposes in its Security UI, so it carries only the environment's
# own IdP and never the internal agent-manager-service keymanager.
VALUES_FILE="$(mktemp)"
trap 'rm -f "$VALUES_FILE"' EXIT
{
    cat <<YAML
agentManager:
  orgName: ${ORG_NAME}
  idp:
    tokenUrl: ${THUNDER_INTERNAL_TOKEN_URL}
gateway:
  environment: ${ENV_NAME}
  vhost: ${GATEWAY_VHOST}
apiGateway:
  namespace: ${NS}
  config:
    policyConfigurations:
      jwtauth_v1:
        keymanagers:
          - name: agent-manager-service
            issuer: agent-manager-service
            jwks:
              remote:
                uri: http://amp-api.wso2-amp.svc.cluster.local:9000/auth/external/jwks.json
                skipTlsVerify: true
          - name: ThunderKeyManager
            issuer: ${T2_ISSUER}
            jwks:
              remote:
                uri: ${T2_JWKS_URL}
                skipTlsVerify: true
bootstrap:
  enabled: ${BOOTSTRAP}
YAML
    if [ "$BOOTSTRAP" = true ]; then
        cat <<YAML
  identityProviders:
    - name: ThunderKeyManager
      issuer: ${T2_ISSUER}
      jwksUri: ${T2_JWKS_URL}
      skipTlsVerify: true
YAML
    fi
} > "$VALUES_FILE"

# ── CREATE or BIND ──────────────────────────────────────────────────────────
# helm_release_deployed, not a bare `helm status`: a release left `failed` by an
# earlier run must be re-driven, not bound to.
if helm_release_deployed "$RELEASE" "$NS"; then
    echo ""
    echo "ℹ️  Release ${RELEASE} is already deployed — binding to it. No helm upgrade."
    echo "   (The gateway may have been created by Agent Manager's environment step; a"
    echo "    publisher never upgrades a release it did not create.)"
    live_values="$(helm get values "$RELEASE" -n "$NS" --kube-context "$CLUSTER_CONTEXT" -o json)"
    live_issuer="$(printf '%s' "$live_values" | python3 -c 'import json,sys
v = json.load(sys.stdin) or {}
for km in v.get("apiGateway", {}).get("config", {}).get("policyConfigurations", {}).get("jwtauth_v1", {}).get("keymanagers", []):
    if km.get("name") == "ThunderKeyManager":
        print(km.get("issuer", ""))
        break')"
    live_vhost="$(printf '%s' "$live_values" | python3 -c 'import json,sys
print((json.load(sys.stdin) or {}).get("gateway", {}).get("vhost", ""))')"
    if [ "$live_issuer" = "$T2_ISSUER" ]; then
        echo "   ✅ ThunderKeyManager issuer matches the binding (${T2_ISSUER})"
    else
        echo "   ⚠️  ThunderKeyManager issuer is '${live_issuer:-<unset>}', the binding says '${T2_ISSUER}'."
        echo "      Tokens from this environment's Thunder will be rejected at this gateway."
        echo "      Re-create the release deliberately, or fix the binding — this script will not upgrade it."
    fi
    if [ "$live_vhost" != "$GATEWAY_VHOST" ]; then
        echo "   ⚠️  Registered vhost is '${live_vhost:-<unset>}', this environment serves on '${GATEWAY_VHOST}'."
        echo "      The vhost is write-once in Agent Manager, so an upgrade would only log a drift"
        echo "      warning there. Re-create the release to correct it."
    fi
    # A release that carries no owner label was installed before the label
    # existed, or by Agent Manager. Recording that is what stops
    # remove-environment-thunder.sh uninstalling it later.
    if [ -z "$(kubectl get namespace "$NS" --context "$CLUSTER_CONTEXT" \
        -o 'jsonpath={.metadata.labels.aep\.wso2\.com/release-created-by}' 2>/dev/null)" ]; then
        kubectl label namespace "$NS" --context "$CLUSTER_CONTEXT" \
            "aep.wso2.com/release-created-by=external" >/dev/null
    fi
else
    echo ""
    echo "📦 Installing ${RELEASE} in ${NS}"
    helm upgrade --install "$RELEASE" \
        "${AMP_REGISTRY}/wso2-amp-api-platform-gateway-extension" \
        --version "${AMP_VERSION}" \
        --namespace "$NS" --create-namespace --kube-context "$CLUSTER_CONTEXT" \
        --values "$VALUES_FILE" \
        --timeout 20m
    kubectl label namespace "$NS" --context "$CLUSTER_CONTEXT" \
        "aep.wso2.com/release-created-by=setup-environment-gateway.sh" --overwrite >/dev/null
    if [ "$BOOTSTRAP" = true ]; then
        echo "⏳ Waiting for the gateway bootstrap Job..."
        kubectl wait --for=condition=complete "job/${RELEASE}-bootstrap" \
            -n "$NS" --context "$CLUSTER_CONTEXT" --timeout="$WAIT_TIMEOUT"
    fi
fi

# ── The gateway is serving ──────────────────────────────────────────────────
# Asserted in BOTH modes: binding to a release someone else installed is only
# safe if it is actually up. Registration completing is not the same as the
# runtime serving traffic, which is why both conditions are waited on.
echo ""
echo "⏳ Waiting for the gateway to be Programmed..."
kubectl wait --for=condition=Programmed "apigateway/${GATEWAY_NAME}" \
    -n "$NS" --context "$CLUSTER_CONTEXT" --timeout="$WAIT_TIMEOUT"
kubectl wait --for=condition=Available "deployment/${RUNTIME_SVC}" \
    -n "$NS" --context "$CLUSTER_CONTEXT" --timeout="$WAIT_TIMEOUT"
# The chart's own RestApi, waited on because it is the cheapest proof that this
# gateway actually ADOPTS a RestApi labelled for it — the same mechanism every
# component's managed API depends on, and the one that silently serves nothing
# when the label and the selector disagree.
kubectl wait --for=condition=Programmed "restapi/${RELEASE}-otel-restapi" \
    -n "$NS" --context "$CLUSTER_CONTEXT" --timeout="$WAIT_TIMEOUT"

echo ""
echo "============================================"
echo "  ✅ Environment gateway ready — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"
echo "  APIGateway:     ${GATEWAY_NAME} (namespace ${NS})"
echo "  RestApi label:  gateway.api-platform.wso2.com/restapi-target=${GATEWAY_NAME}"
echo "  Runtime:        ${RUNTIME_SVC}.${NS}:22893"
echo "  Public vhost:   ${GATEWAY_VHOST}"
echo "  ThunderKeyManager issuer: ${T2_ISSUER}"
echo "  Registered in Agent Manager: ${BOOTSTRAP}"
