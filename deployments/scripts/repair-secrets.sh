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

# Repair per-org secrets in OpenBao after a local cluster reseed.
#
# When the k3d cluster (or just the OpenBao volume) is torn down, the
# SM-API metadata rows on org_credentials + org_anthropic_credentials still
# point at OpenBao paths that no longer exist. Every subsequent
# coding-agent dispatch then produces ExternalSecrets that ESO can't sync;
# the runner pod hangs in CreateContainerConfigError and the agent never
# starts.
#
# Flow:
#   1. POST asdlc-api /api/v1/_test/sm-api-resync — the BFF returns the
#      cred-store plaintext + the OpenBao path the dispatcher will read.
#   2. For each write, `vault kv put -mount=secret <kvPath> <property>=<value>`
#      via `kubectl exec openbao-0`.
#
# The repair endpoint is TestMode-gated on the BFF (off in production) and
# this script aborts unless kubectl is pointed at the local k3d cluster —
# two layers of "you're really local" before plaintext crosses the
# localhost boundary. Idempotent: rows without an SM-API triplet are
# skipped, missing cred-store entries are skipped, and `vault kv put`
# overwrites cleanly when the path already has a value.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"

BFF_URL="${BFF_URL:-http://localhost:9090}"
OPENBAO_NS="openbao"
OPENBAO_POD="openbao-0"

# Local-only gate. CLUSTER_CONTEXT is exported by env.sh (k3d-openchoreo
# for the canonical local setup). Bail out loudly otherwise.
CURRENT_CTX="$(kubectl config current-context 2>/dev/null || true)"
if [ -z "$CURRENT_CTX" ] || [ "$CURRENT_CTX" != "$CLUSTER_CONTEXT" ]; then
    echo "⚠️  repair-secrets: current kubectl context ($CURRENT_CTX) != $CLUSTER_CONTEXT — refusing to run."
    echo "   This script writes plaintext secrets to OpenBao; it must only run locally."
    exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
    echo "ℹ️  repair-secrets: jq not installed — skipping (install with 'brew install jq' to enable)."
    exit 0
fi

# Wait briefly for the BFF to come up after `docker compose up -d`. 30s
# total — enough for migrations + service init on a warm rebuild.
echo "🔍 Waiting for asdlc-api at $BFF_URL/health (up to 30s)..."
for i in $(seq 1 30); do
    if curl -sS --max-time 1 -o /dev/null -w '%{http_code}' "$BFF_URL/health" 2>/dev/null | grep -q '^200$'; then
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "❌ asdlc-api never reached /health — skipping secret resync."
        exit 0
    fi
    sleep 1
done

echo "🔐 Fetching reseed bundles from asdlc-api..."
RESP="$(curl -sS -X POST -w '\n%{http_code}' "$BFF_URL/api/v1/_test/sm-api-resync" 2>&1 || true)"
HTTP_CODE="$(echo "$RESP" | tail -n1)"
BODY="$(echo "$RESP" | sed '$d')"

if [ "$HTTP_CODE" = "404" ]; then
    echo "ℹ️  /_test/sm-api-resync not mounted — needs TEST_MODE=true AND LOCAL_OPENBAO_REPAIR=true on asdlc-api. Skipping."
    exit 0
fi
if [ "$HTTP_CODE" != "200" ]; then
    echo "⚠️  resync endpoint returned HTTP $HTTP_CODE"
    echo "$BODY"
    exit 0
fi

ORG_COUNT="$(echo "$BODY" | jq '.orgs | length')"
WRITE_COUNT="$(echo "$BODY" | jq '[.orgs[].writes[]] | length')"
if [ "$WRITE_COUNT" = "0" ]; then
    echo "ℹ️  no orgs with populated SM-API triplets — nothing to reseed."
    # Surface any per-org errors even when nothing was returned.
    ERRORS="$(echo "$BODY" | jq -r '.orgs[] | select(.anthropicError or .githubPatError) | "  - \(.ocOrgId): anthropic=\(.anthropicError // "-") github=\(.githubPatError // "-")"')"
    [ -n "$ERRORS" ] && { echo "⚠️  errors:"; echo "$ERRORS"; }
    exit 0
fi

# Dev-mode OpenBao (chart default in our local setup) uses a fixed root
# token. Discover the configured value from the pod env so the script
# keeps working if it ever rotates.
OPENBAO_ROOT_TOKEN="$(kubectl exec -n "$OPENBAO_NS" "$OPENBAO_POD" -- sh -c 'printenv VAULT_DEV_ROOT_TOKEN_ID || printenv BAO_DEV_ROOT_TOKEN_ID' 2>/dev/null | tr -d '[:space:]')"
if [ -z "$OPENBAO_ROOT_TOKEN" ]; then
    echo "⚠️  $OPENBAO_NS/$OPENBAO_POD has no dev-root-token env — cluster isn't in dev mode; cannot reseed automatically."
    exit 0
fi

WROTE=0
FAILED=0
while IFS= read -r WRITE; do
    KV_PATH="$(echo "$WRITE" | jq -r '.kvPath')"
    PROPERTY="$(echo "$WRITE" | jq -r '.property')"
    VALUE="$(echo "$WRITE" | jq -r '.value')"
    if [ -z "$KV_PATH" ] || [ -z "$PROPERTY" ] || [ -z "$VALUE" ]; then
        continue
    fi
    # `vault kv put` is idempotent (overwrite). Plaintext + root token are
    # both passed as positional args to the exec sh -c (no argv on the host
    # side: kubectl exec runs the shell inside the openbao container).
    if kubectl exec -n "$OPENBAO_NS" "$OPENBAO_POD" -- sh -c \
        'export VAULT_ADDR=http://127.0.0.1:8200 \
         && export VAULT_TOKEN="$1" \
         && vault kv put -mount=secret '"$KV_PATH"' '"$PROPERTY"'="$2" >/dev/null' \
        _ "$OPENBAO_ROOT_TOKEN" "$VALUE" 2>/dev/null; then
        WROTE=$((WROTE + 1))
        echo "  ✅ $KV_PATH ($PROPERTY)"
    else
        FAILED=$((FAILED + 1))
        echo "  ❌ $KV_PATH ($PROPERTY) — vault kv put failed"
    fi
done < <(echo "$BODY" | jq -c '.orgs[].writes[]')

echo "🔐 Reseed complete: $WROTE write(s) across $ORG_COUNT org(s)$( [ "$FAILED" -gt 0 ] && echo "; $FAILED failed" )."

# Surface per-org errors from the BFF (cred-store misses, etc.).
ERRORS="$(echo "$BODY" | jq -r '.orgs[] | select(.anthropicError or .githubPatError) | "  - \(.ocOrgId): anthropic=\(.anthropicError // "-") github=\(.githubPatError // "-")"')"
[ -n "$ERRORS" ] && { echo "⚠️  bundle errors:"; echo "$ERRORS"; }
