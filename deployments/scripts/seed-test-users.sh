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

# scripts/seed-test-users.sh — local-dev convenience that creates a fixed
# set of test users in Thunder's `default` OU with password `admin`.
#
# Idempotent: each user is skipped if a user with the same username already
# exists. Safe to re-run after every cluster refresh.
#
#   cd deployments && bash scripts/seed-test-users.sh
#
# Knobs (env, with defaults):
#   THUNDER_URL          defaults to http://thunder.openchoreo.localhost:8080
#   SYSTEM_CLIENT_ID     defaults to aep-system-client
#   SYSTEM_CLIENT_SECRET defaults to aep-system-client-secret
#   TEST_USER_PASSWORD   defaults to admin
#
# The `aep-system-client` is registered by the Thunder bootstrap
# (see deployments/single-cluster/values-thunder.yaml) and bound to the
# Thunder Administrator role by 60-aep-system-role.sh — it can mint
# scope=system tokens that the /users admin endpoint accepts.

set -u

THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SYSTEM_CLIENT_ID="${SYSTEM_CLIENT_ID:-aep-system-client}"
SYSTEM_CLIENT_SECRET="${SYSTEM_CLIENT_SECRET:-aep-system-client-secret}"
TEST_USER_PASSWORD="${TEST_USER_PASSWORD:-admin}"

# username|email|given_name
USERS=(
    "mark|mark@testorg.com|Mark"
    "john|john@testorg.com|John"
    "chris|chris@testorg.com|Chris"
    "emily|emily@testorg.com|Emily"
)

echo "=== seed-test-users ==="
echo "  Thunder: $THUNDER_URL"

# 1. Mint system token
TOKEN_RESP=$(curl -sS -X POST "${THUNDER_URL%/}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=${SYSTEM_CLIENT_ID}" \
    -d "client_secret=${SYSTEM_CLIENT_SECRET}" \
    -d "scope=system" 2>/dev/null || true)
TOKEN=$(printf '%s' "$TOKEN_RESP" \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
    echo "❌ Thunder did not return an access_token."
    echo "   Response (first 200 chars):"
    printf '   %s\n' "$(printf '%s' "$TOKEN_RESP" | head -c 200)"
    exit 1
fi
echo "✅ Token minted (client=${SYSTEM_CLIENT_ID})"

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
for row in "${USERS[@]}"; do
    IFS='|' read -r username email given <<<"$row"

    if printf '%s\n' "$EXISTING" | grep -qx "$username"; then
        echo "⏭️  $username — already exists, skipping"
        continue
    fi

    payload=$(printf '{"type":"Person","ouId":"%s","attributes":{"username":"%s","password":"%s","sub":"%s","email":"%s","email_verified":true,"given_name":"%s","family_name":"User"}}' \
        "$OU_ID" "$username" "$TEST_USER_PASSWORD" "$username" "$email" "$given")

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
            ;;
    esac
done

# 4. Ensure every test user is in the Administrators group. OpenChoreo's
# authz grants console access via the token's `groups` claim
# (administrators-group-binding in wso2-ae-oc-extensions maps the group to
# the admin ClusterAuthzRole); a user with no group has NO OC entitlement,
# so every OC-backed project read 403s and the project view never renders
# (#123). jq is required here, like patch-thunder-new-console.sh.
echo ""
GROUP_ID=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    "${THUNDER_URL%/}/groups" 2>/dev/null \
    | jq -r '.groups[] | select(.name == "Administrators") | .id' | head -1)
if [ -z "$GROUP_ID" ]; then
    echo "❌ Administrators group not found in Thunder — OC access will 403 (#123)."
    exit 1
fi

MEMBER_IDS=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    "${THUNDER_URL%/}/groups/${GROUP_ID}/members" 2>/dev/null \
    | jq -r '.members[].id')
ALL_USERS=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
    "${THUNDER_URL%/}/users" 2>/dev/null)

for row in "${USERS[@]}"; do
    IFS='|' read -r username _ _ <<<"$row"
    user_id=$(printf '%s' "$ALL_USERS" \
        | jq -r --arg u "$username" '.users[] | select(.attributes.username == $u) | .id' | head -1)
    if [ -z "$user_id" ]; then
        echo "❌ $username — no user id found; cannot add to Administrators"
        continue
    fi
    if printf '%s\n' "$MEMBER_IDS" | grep -qx "$user_id"; then
        echo "⏭️  $username — already in Administrators"
        continue
    fi
    code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{\"members\":[{\"type\":\"user\",\"id\":\"${user_id}\"}]}" \
        "${THUNDER_URL%/}/groups/${GROUP_ID}/members/add" 2>/dev/null || echo 000)
    if [ "$code" = "200" ]; then
        echo "✅ $username → Administrators group"
    else
        echo "❌ $username — members/add failed (HTTP $code)"
    fi
done

echo ""
echo "=== seed-test-users complete ==="
