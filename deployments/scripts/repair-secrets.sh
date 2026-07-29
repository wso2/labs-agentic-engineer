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

# Trigger in-process OpenBao secret repair after a local cluster reseed.
#
# When the k3d cluster (or just the OpenBao volume) is torn down, the
# secret-ref metadata rows on org_credentials + org_anthropic_credentials still
# point at OpenBao paths that no longer exist. aep-api's
# POST /_dev/v1/sm-api-resync re-reads from the encrypted credential store and
# re-pushes through the in-process secrets provider — no plaintext crosses
# the HTTP boundary. This script is trigger-only.
#
# The repair endpoint is TestMode-gated on the BFF (off in production) and
# this script aborts unless kubectl is pointed at the local k3d cluster.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=env.sh
source "$SCRIPT_DIR/env.sh"

BFF_URL="${BFF_URL:-http://localhost:9090}"

# Local-only gate. CLUSTER_CONTEXT is exported by env.sh (k3d-openchoreo
# for the canonical local setup). Bail out loudly otherwise.
CURRENT_CTX="$(kubectl config current-context 2>/dev/null || true)"
if [ -z "$CURRENT_CTX" ] || [ "$CURRENT_CTX" != "$CLUSTER_CONTEXT" ]; then
    echo "⚠️  repair-secrets: current kubectl context ($CURRENT_CTX) != $CLUSTER_CONTEXT — refusing to run."
    echo "   This script must only run against the local k3d cluster."
    exit 1
fi

# Wait briefly for the BFF to come up after `docker compose up -d`. 30s
# total — enough for migrations + service init on a warm rebuild.
echo "🔍 Waiting for aep-api at $BFF_URL/healthz (up to 30s)..."
for i in $(seq 1 30); do
    if curl -sS --max-time 1 -o /dev/null -w '%{http_code}' "$BFF_URL/healthz" 2>/dev/null | grep -q '^200$'; then
        break
    fi
    if [ "$i" -eq 30 ]; then
        echo "❌ aep-api never reached /healthz — skipping secret resync."
        exit 0
    fi
    sleep 1
done

echo "🔐 Triggering in-process secret resync..."
RESP="$(curl -sS -X POST -w '\n%{http_code}' "$BFF_URL/_dev/v1/sm-api-resync" 2>&1 || true)"
HTTP_CODE="$(echo "$RESP" | tail -n1)"
BODY="$(echo "$RESP" | sed '$d')"

if [ "$HTTP_CODE" = "404" ]; then
    echo "ℹ️  /_dev/v1/sm-api-resync not mounted — needs TEST_MODE=true AND LOCAL_OPENBAO_REPAIR=true on aep-api. Skipping."
    exit 0
fi
if [ "$HTTP_CODE" != "200" ]; then
    echo "⚠️  resync endpoint returned HTTP $HTTP_CODE"
    echo "$BODY"
    exit 0
fi

# Status-only body (no secret material). Best-effort summary when jq is present.
if command -v jq >/dev/null 2>&1; then
    ORG_COUNT="$(echo "$BODY" | jq '.orgs | length')"
    WRITE_COUNT="$(echo "$BODY" | jq '[.orgs[].written] | add // 0')"
    echo "🔐 Resync complete: $WRITE_COUNT write(s) across $ORG_COUNT org(s)."
    ERRORS="$(echo "$BODY" | jq -r '.orgs[] | select(.anthropicError or .githubPatError) | "  - \(.ocOrgId): anthropic=\(.anthropicError // "-") github=\(.githubPatError // "-")"')"
    if [ -n "$ERRORS" ]; then
        echo "⚠️  resync errors:"
        echo "$ERRORS"
        exit 1
    fi
else
    echo "🔐 Resync triggered (install jq to see a summary)."
fi

# The workflow-plane registry push secret is platform-scoped (not per-org) and
# also lives only in OpenBao — an OpenBao restart wipes it. Empty-auth
# dockerconfigjson is correct for the local anonymous registry.
echo "🐳 Ensuring workflow-plane registry-push-secret..."
kubectl -n openbao exec openbao-0 -- sh -c \
    'VAULT_ADDR=http://127.0.0.1:8200 vault kv put -mount=secret registry-push-secret value="{\"auths\":{}}"' >/dev/null \
    && echo "  ✅ registry-push-secret" || echo "  ⚠️  could not seed registry-push-secret (openbao-0 unreachable?)"

exit 0
