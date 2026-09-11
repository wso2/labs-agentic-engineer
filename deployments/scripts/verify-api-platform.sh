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

# End-to-end verification of a managed API on its environment's own gateway.
#
#   bash scripts/verify-api-platform.sh [<org> <env>]     (default: default default)
#
# Deploys two hello-world components (manifests/poc-api-platform) through the
# `api-configuration` trait and asserts WHICH identities the gateway in front of
# them accepts. That question is the whole point of a per-environment gateway:
#
#   public    + no token              200   no policy, no identity needed
#   public    + this env's T2 token   200   a token never makes an open API closed
#   protected + no token              401
#   protected + this env's T2 token   200   the environment's OWN identity tier
#   protected + the platform IdP (T1) 401   the platform login is not an API identity
#   protected + a SIBLING env's T2    401   environments do not share an identity
#
# The last two are the invariant. A gateway terminates authentication, so it
# must do so against exactly one environment's identity tier: the `api-platform-
# <org>-<env>` gateway's only Thunder keymanager is the T2 recorded in that
# environment's binding (scripts/setup-environment-gateway.sh). Both 401s are
# the difference between an environment and a cluster-wide free-for-all, and
# neither fails loudly on its own — a gateway that accepted a T1 token would
# look perfectly healthy.
#
# Header is `Authorization: Bearer`, which is the gateway runtime's default and
# what the trait's `jwt-auth v1` policy therefore reads. (Agent Manager's own
# RestApis override it per-API to `x-amp-api-key`; AEP's do not.)
#
# The token is minted from the aep-system-client credential the binding record
# carries — the mirrored Secret in thunder-app-operator-system, one of the three
# copies setup-environment-thunder.sh writes (the others are the T2's own
# namespace and OpenBao `secret/aep/thunder/<org>/<env>`, for aep-api, which
# runs outside the cluster).
#
# Findings from the original POC run are in deployments/POC-API-PLATFORM.md;
# the two that still shape this script are that reconciled resources land in a
# per-environment `dp-*` namespace discovered from ReleaseBinding status, and
# that the router 404s on a context path with no trailing slash.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

load_public_urls "$SCRIPT_DIR/../.env"

ORG_NAME="${1:-${ORG_NAME:-default}}"
ENV_NAME="${2:-${ENV_NAME:-default}}"
POC_MANIFESTS="${SCRIPT_DIR}/../manifests/poc-api-platform"
OPERATOR_NS="thunder-app-operator-system"

# The platform IdP client every AEP service already has
# (single-cluster/thunder-resources/80-aep-api-client.yaml). Its token must be
# REJECTED here — it is minted at the platform tier, which this gateway does not
# trust.
T1_CLIENT_ID="${POC_CLIENT_ID:-aep-api-client}"
T1_CLIENT_SECRET="${POC_CLIENT_SECRET:-aep-api-client-secret}"
T1_ISSUER="${THUNDER_PUBLIC:-${PUBLIC_THUNDER_URL:-http://thunder.openchoreo.localhost:8080}}"

GATEWAY_NS="${ORG_NAME}-${ENV_NAME}"
GATEWAY_NAME="api-platform-${ORG_NAME}-${ENV_NAME}"
GATEWAY_HOST="${GATEWAY_NAME}-gw-gateway-gateway-runtime.${GATEWAY_NS}:22893"

EXIT_CODE=0
fail() { printf "   ❌ %s\n" "$1"; EXIT_CODE=1; }

echo "=== API Platform: ${ORG_NAME}/${ENV_NAME} on its own gateway ==="
echo "   APIGateway:   ${GATEWAY_NAME} (namespace ${GATEWAY_NS})"
echo "   Gateway host: ${GATEWAY_HOST}"

# ───────────────────────────────────────────────────────────────────────────
# 1. The environment's identity tier, from its binding
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "1️⃣  Resolving identity tiers"

# mint_t2 <org> <env> — a client_credentials token from that environment's own
# Thunder, using the aep-system-client credential its binding record carries.
# Prints the token, or nothing when the environment has no binding.
mint_t2() {
    local org="$1" env="$2" secret cid cs rs issuer
    # A second statement: bash expands a whole `local` line before assigning any
    # of it, so ${org} is still unset on the line that declares it.
    secret="thunder-binding-${org}-${env}"
    issuer="$(kubectl get configmap -A --context "$CLUSTER_CONTEXT" \
        -l "aep.wso2.com/kind=thunder-binding,aep.wso2.com/org=${org},aep.wso2.com/env=${env}" \
        -o jsonpath='{.items[0].data.issuer}' 2>/dev/null)"
    [ -n "$issuer" ] || return 1
    cid="$(kubectl get secret "$secret" -n "$OPERATOR_NS" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.data.client-id}' 2>/dev/null | base64 -d)"
    cs="$(kubectl get secret "$secret" -n "$OPERATOR_NS" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d)"
    rs="$(kubectl get secret "$secret" -n "$OPERATOR_NS" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.data.system-resource-identifier}' 2>/dev/null | base64 -d)"
    [ -n "$cid" ] && [ -n "$cs" ] || return 1
    # client_secret_post with the resource indicator, exactly as aep-api and the
    # operator mint. Without `resource` the token comes back scope-less — the
    # trap P1 documents — which does not matter to the gateway but would make a
    # failure here read as the wrong problem.
    curl -sS -X POST "${issuer}/oauth2/token" \
        -H 'Content-Type: application/x-www-form-urlencoded' \
        -d "grant_type=client_credentials&client_id=${cid}&client_secret=${cs}&scope=system&resource=${rs}" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))'
}

OWN_T2="$(mint_t2 "$ORG_NAME" "$ENV_NAME")"
if [ -z "$OWN_T2" ]; then
    echo "❌ No usable thunder-binding for ${ORG_NAME}/${ENV_NAME}." >&2
    echo "   Run: bash ${SCRIPT_DIR}/setup-environment-thunder.sh ${ORG_NAME} ${ENV_NAME}" >&2
    exit 1
fi
echo "   ✅ ${ENV_NAME}'s own T2 token minted"

T1_TOKEN="$(curl -sS -X POST "${T1_ISSUER}/oauth2/token" \
    -H 'Content-Type: application/x-www-form-urlencoded' \
    -d "grant_type=client_credentials&client_id=${T1_CLIENT_ID}&client_secret=${T1_CLIENT_SECRET}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))')"
if [ -z "$T1_TOKEN" ]; then
    echo "❌ Could not mint a platform-IdP token from ${T1_ISSUER} as ${T1_CLIENT_ID}." >&2
    exit 1
fi
echo "   ✅ platform IdP (T1) token minted — must be REJECTED below"

# A sibling environment of the same org, whichever one exists. Optional: a
# single-environment installation has no sibling to be rejected.
SIBLING_ENV="$(kubectl get configmap -A --context "$CLUSTER_CONTEXT" \
    -l "aep.wso2.com/kind=thunder-binding,aep.wso2.com/org=${ORG_NAME}" \
    -o jsonpath='{range .items[*]}{.metadata.labels.aep\.wso2\.com/env}{"\n"}{end}' 2>/dev/null \
    | grep -v "^${ENV_NAME}$" | head -1)"
SIBLING_T2=""
if [ -n "$SIBLING_ENV" ]; then
    SIBLING_T2="$(mint_t2 "$ORG_NAME" "$SIBLING_ENV")"
fi
if [ -n "$SIBLING_T2" ]; then
    echo "   ✅ sibling environment '${SIBLING_ENV}' T2 token minted — must be REJECTED below"
else
    echo "   ⏭️  no sibling environment with a binding — the cross-environment cell is skipped"
fi

# ───────────────────────────────────────────────────────────────────────────
# 2. Deploy the two components
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "2️⃣  Applying POC manifests..."
# Server-side apply: the ProjectReleaseBinding controller creates the two
# ReleaseBindings itself, and a client-side apply racing that loses the
# traitEnvironmentConfigs (the jwtAuth switch) with an AlreadyExists on create.
for f in "${POC_MANIFESTS}"/*.yaml; do
    kubectl --context "${CLUSTER_CONTEXT}" apply --server-side --force-conflicts -f "$f" >/dev/null || exit 1
done
echo "   ✅ applied"

echo ""
echo "3️⃣  Waiting for ReleaseBindings to reach Ready..."
wait_rb_ready() {
    local rb="$1" cond
    for _ in $(seq 1 120); do
        cond=$(kubectl --context "${CLUSTER_CONTEXT}" get releasebinding "$rb" -n default \
            -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)
        if [ "$cond" = "True" ]; then
            echo "   ✅ ReleaseBinding $rb Ready"
            return 0
        fi
        sleep 1
    done
    echo "   ❌ ReleaseBinding $rb did not become Ready" >&2
    kubectl --context "${CLUSTER_CONTEXT}" describe releasebinding "$rb" -n default >&2
    return 1
}
wait_rb_ready "poc-public-${ENV_NAME}" || exit 1
wait_rb_ready "poc-protected-${ENV_NAME}" || exit 1

# Reconciled resources land in a per-environment data-plane namespace, not the
# Component's. Pattern: poc-public.dp-<hash>.svc.cluster.local → dp-<hash>.
DP_HOST=$(kubectl --context "${CLUSTER_CONTEXT}" get releasebinding "poc-public-${ENV_NAME}" -n default \
    -o jsonpath='{.status.endpoints[0].serviceURL.host}')
DP_NS=$(echo "$DP_HOST" | sed -E 's|^[^.]+\.([^.]+)\..*|\1|')
external_url() {
    kubectl --context "${CLUSTER_CONTEXT}" get releasebinding "$1" -n default \
        -o jsonpath='{.status.endpoints[0].externalURLs.http.scheme}://{.status.endpoints[0].externalURLs.http.host}:{.status.endpoints[0].externalURLs.http.port}{.status.endpoints[0].externalURLs.http.path}'
}
# The AP router 404s on the bare context path; the trailing slash is required.
PUBLIC_URL="$(external_url "poc-public-${ENV_NAME}")"; PUBLIC_URL="${PUBLIC_URL%/}/"
PROTECTED_URL="$(external_url "poc-protected-${ENV_NAME}")"; PROTECTED_URL="${PROTECTED_URL%/}/"
echo "   data-plane namespace: ${DP_NS}"
echo "   public URL:           ${PUBLIC_URL}"
echo "   protected URL:        ${PROTECTED_URL}"

# Each RestApi must name THIS environment's gateway. A RestApi labelled for a
# gateway that does not exist is served by nothing, which reads as a 404 from a
# perfectly healthy-looking CR.
echo ""
echo "   Trait-produced resources in ${DP_NS}:"
kubectl --context "${CLUSTER_CONTEXT}" get restapi -n "${DP_NS}" \
    -o custom-columns='NAME:.metadata.name,TARGET:.metadata.labels.gateway\.api-platform\.wso2\.com/restapi-target' \
    --no-headers 2>/dev/null | sed 's/^/     /'
for target in $(kubectl --context "${CLUSTER_CONTEXT}" get restapi -n "${DP_NS}" \
        -o jsonpath='{range .items[*]}{.metadata.labels.gateway\.api-platform\.wso2\.com/restapi-target}{"\n"}{end}' 2>/dev/null); do
    [ "$target" = "$GATEWAY_NAME" ] || fail "RestApi targets '${target}', expected '${GATEWAY_NAME}'"
done

# A Ready ReleaseBinding means the RestApi CR EXISTS, not that the gateway serves
# it: the gateway-controller still has to push the API and its jwt-auth policy to
# the runtime, and until the policy engine has loaded that config a protected
# call returns 500 instead of 401/200. The CR's Programmed condition is the
# controller saying the push landed; the router itself is then asked once more,
# because the engine loads the pushed policy a moment after the push. Asserting
# before either step is a race that a first run on a fresh cluster loses.
wait_restapis_programmed() {
    local i
    for i in $(seq 1 60); do
        local unprogrammed
        unprogrammed=$(kubectl --context "${CLUSTER_CONTEXT}" get restapi -n "${DP_NS}" \
            -o jsonpath='{range .items[*]}{.metadata.name}={.status.conditions[?(@.type=="Programmed")].status}{"\n"}{end}' 2>/dev/null \
            | grep -v '=True$' || true)
        if [ -z "$unprogrammed" ]; then
            echo "   ✅ every RestApi in ${DP_NS} is Programmed"
            return 0
        fi
        sleep 2
    done
    echo "   ❌ RestApis not Programmed after 120s: $(echo "$unprogrammed" | tr '\n' ' ')" >&2
    kubectl --context "${CLUSTER_CONTEXT}" describe restapi -n "${DP_NS}" >&2
    return 1
}
wait_router_serving() { # $1: a URL the router should answer with a non-5xx status
    local i code
    for i in $(seq 1 30); do
        code=$(curl -sS -o /dev/null -w '%{http_code}' "$1" 2>/dev/null || echo 000)
        case "$code" in 5??|000) sleep 2 ;; *) return 0 ;; esac
    done
    echo "   ❌ router still answers ${code} on ${1} after 60s" >&2
    return 1
}
wait_restapis_programmed || exit 1
wait_router_serving "$PROTECTED_URL" || exit 1

# ───────────────────────────────────────────────────────────────────────────
# 4. Truth table
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "4️⃣  Truth table (header: Authorization: Bearer):"

curl_status() {
    local url="$1" token="$2"
    if [ -n "$token" ]; then
        curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${token}" "$url"
    else
        curl -sS -o /dev/null -w '%{http_code}' "$url"
    fi
}
assert_eq() {
    local label="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        printf "   ✅ %-38s expected %s, got %s\n" "$label" "$expected" "$actual"
    else
        printf "   ❌ %-38s expected %s, got %s\n" "$label" "$expected" "$actual"
        EXIT_CODE=1
    fi
}

assert_eq "public + no token"          200 "$(curl_status "$PUBLIC_URL"    "")"
assert_eq "public + own T2 token"      200 "$(curl_status "$PUBLIC_URL"    "$OWN_T2")"
assert_eq "protected + no token"       401 "$(curl_status "$PROTECTED_URL" "")"
assert_eq "protected + own T2 token"   200 "$(curl_status "$PROTECTED_URL" "$OWN_T2")"
assert_eq "protected + platform (T1)"  401 "$(curl_status "$PROTECTED_URL" "$T1_TOKEN")"
if [ -n "$SIBLING_T2" ]; then
    assert_eq "protected + '${SIBLING_ENV}' T2 token" 401 "$(curl_status "$PROTECTED_URL" "$SIBLING_T2")"
fi

echo ""
if [ "$EXIT_CODE" = 0 ]; then
    echo "✅ Every cell passed on ${GATEWAY_NAME}."
else
    echo "❌ One or more cells failed. Inspect:"
    echo "   kubectl get restapi,backend,httproute -n ${DP_NS}"
    echo "   kubectl get apigateway ${GATEWAY_NAME} -n ${GATEWAY_NS} -o yaml"
    echo "   kubectl logs -n ${GATEWAY_NS} -l app.kubernetes.io/component=gateway-runtime --tail=200"
    echo "   kubectl logs -n ${GATEWAY_NS} -l app.kubernetes.io/component=controller --tail=200"
fi
exit "$EXIT_CODE"
