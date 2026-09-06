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

# scripts/seed-test-users.sh — local-dev convenience that creates a fixed set of
# test users with password `admin` on ONE ENVIRONMENT's identity provider.
#
#   cd deployments && bash scripts/seed-test-users.sh [<org> <env>]
#
# Defaults to `default default`, the environment AEP deploys and validates
# in. There is no cluster-wide option any more and no platform-IdP default: an
# identity provider belongs to an (org, environment) pair, and an account minted
# on one is rejected by every other, so seeding "the cluster's users" would name
# no sign-in anybody can reach. The platform IdP holds no test users at all —
# it is neutral infrastructure (ADR-0028), and a build's roles and test users are
# provisioned on the environment's own Thunder (ADR-0022).
#
# Idempotent: each user is skipped if a user with the same username already
# exists. Safe to re-run after every cluster refresh.
#
# ── How the environment's identity provider is found ────────────────────────
# Through kubectl, from the binding record setup-environment-thunder.sh wrote.
# That record has three projections; this script reads the two in the cluster
# because kubectl is the only tool a host running this already has — OpenBao's
# copy needs a port-forward or the NodePort mapping only some clusters have, and
# aep-api (which runs outside the cluster) is the consumer that reads it there.
#
#   ConfigMap  labelled aep.wso2.com/kind=thunder-binding + org + env
#              → issuer, systemResourceIdentifier, and where the Secret lives
#   Secret     named by that ConfigMap → client-id, client-secret
#
# The LABELS are the contract, not the names: the operator selects on them too.
#
# Knobs (env, all optional — every one defaults out of the binding record):
#   THUNDER_URL          the environment IdP's public issuer
#   SYSTEM_CLIENT_ID     defaults to the binding's client-id
#   SYSTEM_CLIENT_SECRET defaults to the binding's client-secret
#   THUNDER_SYSTEM_RESOURCE_IDENTIFIER
#                        the System resource server's identifier, sent as the
#                        `resource` indicator (default: the binding's)
#   TEST_USER_PASSWORD   defaults to admin
#
# `aep-system-client` is registered on every environment Thunder by
# setup-environment-thunder.sh and bound to the Thunder Administrator role — it
# can mint scope=system tokens that the /users admin endpoint accepts.
#
# The `resource` indicator is not optional. ThunderID resolves a requested scope
# against a resource server; without the indicator it uses the server-wide
# default, which on this cluster is Agent Manager's and does not define
# `system`. The scope is then dropped silently — the token endpoint still
# answers 200 — and every call below 403s. The mint step checks the scope claim
# so that failure is named here rather than four requests later.

set -u
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

ORG_NAME="${1:-${ORG_NAME:-default}}"
ENV_NAME="${2:-${ENV_NAME:-default}}"

echo "=== seed-test-users ==="
echo "  Environment: ${ORG_NAME}/${ENV_NAME}"

# ── 0. The binding record ────────────────────────────────────────────────────
# Resolved by LABEL through the readers in utils.sh, the same way the operator
# and the gateway script resolve it.
if [ -z "$(thunder_binding_configmap "$ORG_NAME" "$ENV_NAME")" ]; then
    echo "❌ ${ORG_NAME}/${ENV_NAME} has no Thunder binding."
    echo "   Run: bash scripts/setup-environment-thunder.sh ${ORG_NAME} ${ENV_NAME}"
    exit 1
fi
BINDING_ISSUER="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" issuer)"
BINDING_SYSTEM_RS="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" systemResourceIdentifier)"
BINDING_SECRET_NAME="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" secretName)"
BINDING_SECRET_NS="$(thunder_binding_value "$ORG_NAME" "$ENV_NAME" secretNamespace)"

if [ -z "$BINDING_ISSUER" ]; then
    echo "❌ the binding for ${ORG_NAME}/${ENV_NAME} records no issuer — re-run setup-environment-thunder.sh"
    exit 1
fi

# The credential half. Read from the Secret the ConfigMap names, so a rotated
# credential is picked up without touching this script.
read_secret_field() {
    kubectl get secret "$BINDING_SECRET_NAME" -n "$BINDING_SECRET_NS" --context "$CLUSTER_CONTEXT" \
        -o "jsonpath={.data.$1}" 2>/dev/null | base64 -d 2>/dev/null || true
}
BINDING_CLIENT_ID="$(read_secret_field client-id)"
BINDING_CLIENT_SECRET="$(read_secret_field client-secret)"

THUNDER_URL="${THUNDER_URL:-$BINDING_ISSUER}"
SYSTEM_CLIENT_ID="${SYSTEM_CLIENT_ID:-${BINDING_CLIENT_ID:-aep-system-client}}"
SYSTEM_CLIENT_SECRET="${SYSTEM_CLIENT_SECRET:-$BINDING_CLIENT_SECRET}"
DEFAULT_SYSTEM_RS="${BINDING_SYSTEM_RS:-${THUNDER_URL%/}/mcp}"
THUNDER_SYSTEM_RESOURCE_IDENTIFIER="${THUNDER_SYSTEM_RESOURCE_IDENTIFIER:-$DEFAULT_SYSTEM_RS}"
TEST_USER_PASSWORD="${TEST_USER_PASSWORD:-admin}"

if [ -z "$SYSTEM_CLIENT_SECRET" ]; then
    echo "❌ no admin credential for ${ORG_NAME}/${ENV_NAME}"
    echo "   Expected secret/${BINDING_SECRET_NAME} in ${BINDING_SECRET_NS}."
    echo "   Run: bash scripts/setup-environment-thunder.sh ${ORG_NAME} ${ENV_NAME}"
    exit 1
fi

echo "  Issuer:      $THUNDER_URL"
echo "  Binding:     secret/${BINDING_SECRET_NAME} in ${BINDING_SECRET_NS} (client=${SYSTEM_CLIENT_ID})"

# username|email|given_name
USERS=(
    "mark|mark@testorg.com|Mark"
    "john|john@testorg.com|John"
    "chris|chris@testorg.com|Chris"
    "emily|emily@testorg.com|Emily"
)

# 1. Mint system token, naming the System resource server
TOKEN_RESP=$(curl -sS -X POST "${THUNDER_URL%/}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=${SYSTEM_CLIENT_ID}" \
    --data-urlencode "client_secret=${SYSTEM_CLIENT_SECRET}" \
    -d "scope=system" \
    --data-urlencode "resource=${THUNDER_SYSTEM_RESOURCE_IDENTIFIER}" 2>/dev/null || true)
TOKEN=$(printf '%s' "$TOKEN_RESP" \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
    echo "❌ Thunder did not return an access_token."
    echo "   Response (first 200 chars):"
    printf '   %s\n' "$(printf '%s' "$TOKEN_RESP" | head -c 200)"
    exit 1
fi

# The token endpoint does not report a dropped scope; the token does.
TOKEN_SCOPE=$(printf '%s' "$TOKEN" | python3 -c '
import base64, json, sys
parts = sys.stdin.read().strip().split(".")
if len(parts) != 3:
    sys.exit(0)
payload = parts[1] + "=" * (-len(parts[1]) % 4)
scope = json.loads(base64.urlsafe_b64decode(payload)).get("scope", "")
print(" ".join(scope) if isinstance(scope, list) else scope)' 2>/dev/null || true)
case " ${TOKEN_SCOPE} " in
    *" system "*)
        echo "✅ Token minted (client=${SYSTEM_CLIENT_ID}, scope=system)"
        ;;
    *)
        echo "❌ Thunder issued a token WITHOUT the system scope (scope='${TOKEN_SCOPE}')."
        echo "   The scope was resolved against a resource server that does not define it"
        echo "   and dropped. THUNDER_SYSTEM_RESOURCE_IDENTIFIER='${THUNDER_SYSTEM_RESOURCE_IDENTIFIER}'"
        echo "   must be the identifier of this Thunder's System resource server"
        echo "   (conventionally <public issuer>/mcp)."
        exit 1
        ;;
esac

# 2. Fetch default OU
OU_RESP=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    "${THUNDER_URL%/}/organization-units/tree/default" 2>/dev/null || true)
OU_ID=$(printf '%s' "$OU_RESP" \
    | grep -o '"handle":"default"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"handle":"default"' \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$OU_ID" ]; then
    echo "❌ Could not resolve default OU ID."
    echo "   Response (first 200 chars):"
    printf '   %s\n' "$(printf '%s' "$OU_RESP" | head -c 200)"
    exit 1
fi
echo "✅ Default OU: $OU_ID"

# 3. List existing usernames once for idempotency
EXISTING=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    "${THUNDER_URL%/}/users" 2>/dev/null \
    | grep -o '"username":"[^"]*"' | cut -d'"' -f4)

echo ""
FAILED=0
for row in "${USERS[@]}"; do
    IFS='|' read -r username email given <<<"$row"

    if printf '%s\n' "$EXISTING" | grep -qx "$username"; then
        echo "⏭️  $username — already exists, skipping"
        continue
    fi

    # Only attributes the seeded Person schema declares: an unknown one
    # (`email_verified`, say) fails the whole create with USR-1019.
    payload=$(printf '{"type":"Person","ouId":"%s","attributes":{"username":"%s","password":"%s","email":"%s","given_name":"%s","family_name":"User"}}' \
        "$OU_ID" "$username" "$TEST_USER_PASSWORD" "$email" "$given")

    resp=$(curl -sS -w '\n%{http_code}' -X POST \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "${THUNDER_URL%/}/users" 2>/dev/null || echo $'\n000')
    code=$(printf '%s' "$resp" | tail -1)
    body=$(printf '%s' "$resp" | sed '$d')

    case "$code" in
        201|200)
            id=$(printf '%s' "$body" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
            echo "✅ $username — created (id=$id)"
            ;;
        409)
            echo "⏭️  $username — already exists (409)"
            ;;
        *)
            echo "❌ $username — failed (HTTP $code)"
            printf '   %s\n' "$(printf '%s' "$body" | head -c 200)"
            FAILED=$((FAILED + 1))
            ;;
    esac
done

echo ""
if [ "$FAILED" -eq 0 ]; then
    echo "=== seed-test-users complete — these logins are valid on ${THUNDER_URL} and NOWHERE else ==="
else
    echo "=== seed-test-users: ${FAILED} user(s) failed ==="
fi
exit "$FAILED"
