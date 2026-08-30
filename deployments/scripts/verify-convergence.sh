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

# Asserts the invariants that make one cluster able to hold both platforms.
#
# These are the things that do NOT fail loudly on their own. A name collision
# between two cluster-scoped objects produces no error — the last apply wins and
# the other product's builds quietly change behaviour. A Thunder serving the
# right host while stamping the wrong `iss` produces a 401 with nothing in its
# own logs. This script exists to turn each of those into a visible assertion.
#
# Runs against whatever is installed: the Agent Manager checks are skipped, not
# failed, when the flag was off.
#
#   bash scripts/verify-convergence.sh

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

load_public_urls "$SCRIPT_DIR/../.env"

FAILURES=0
pass() { printf "   ✅ %s\n" "$1"; }
skip() { printf "   ⏭️  %s\n" "$1"; }
fail() { printf "   ❌ %s\n" "$1"; FAILURES=$((FAILURES + 1)); }
check() { [ "$2" = "$3" ] && pass "$1" || fail "$1 — expected '$3', got '$2'"; }

amp_installed() { kubectl get ns wso2-amp &>/dev/null; }

echo "=== Convergence invariants ==="

# ── 1. Exactly one platform IdP ─────────────────────────────────────────────
echo ""
echo "1️⃣  One platform IdP"
idp_count="$(helm list -A -o json --kube-context "$CLUSTER_CONTEXT" 2>/dev/null \
    | python3 -c "
import json,sys
rel = json.load(sys.stdin)
# Per-environment Thunders are a DIFFERENT tier and are expected to exist
# alongside the platform one — count only the platform release.
print(sum(1 for r in rel if r['name'] == '${THUNDER_RELEASE}'))")"
check "exactly one ${THUNDER_RELEASE} release" "$idp_count" "1"
if kubectl get ns thunder &>/dev/null; then
    fail "the pre-convergence 'thunder' namespace still exists — two IdPs would race the same hostname"
else
    pass "no leftover pre-convergence Thunder"
fi

# ── 2. The issuer Thunder stamps matches what OpenChoreo expects ────────────
echo ""
echo "2️⃣  Token issuer agrees with OpenChoreo's configured issuer"
tok="$(kubectl run "idp-probe-$RANDOM" -n "$THUNDER_NS" --rm -i --restart=Never \
    --image=curlimages/curl:8.11.1 --quiet --context "$CLUSTER_CONTEXT" -- \
    -s -X POST "${THUNDER_INTERNAL_URL}/oauth2/token" \
    -d "grant_type=client_credentials&client_id=aep-api-client&client_secret=aep-api-client-secret" \
    2>/dev/null | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null)"
if [ -z "$tok" ]; then
    fail "aep-api-client could not mint a token"
else
    claims="$(python3 -c "
import base64, json, sys
p = '$tok'.split('.')[1]; p += '=' * (-len(p) % 4)
c = json.loads(base64.urlsafe_b64decode(p))
print(c.get('iss',''), c.get('client_id',''), c.get('ouHandle',''))")"
    read -r iss cid ou <<<"$claims"
    check "iss" "$iss" "$PUBLIC_THUNDER_URL"
    # The whole reason the entitlement claim moved: ThunderID puts the subject
    # here, and OpenChoreo's bindings key on it.
    check "client_id claim present" "$cid" "aep-api-client"
    check "ouHandle claim present" "$ou" "default"
fi

# ── 3. Entitlement claims ───────────────────────────────────────────────────
echo ""
echo "3️⃣  Service-account bindings key on client_id"
stale="$(kubectl get clusterauthzrolebindings.openchoreo.dev --context "$CLUSTER_CONTEXT" \
    -o json 2>/dev/null | python3 -c "
import json,sys
bad = [b['metadata']['name'] for b in json.load(sys.stdin)['items']
       if b['spec']['entitlement']['claim'] == 'sub']
print(','.join(bad))")"
if [ -z "$stale" ]; then
    pass "no binding left on the 'sub' claim"
else
    fail "still on 'sub': ${stale}"
fi

# ── 4. No cluster-scoped build-template collisions ──────────────────────────
echo ""
echo "4️⃣  Build templates do not collide"
for tpl in aep-checkout-source aep-containerfile-build aep-publish-image aep-generate-workload; do
    kubectl get clusterworkflowtemplate "$tpl" --context "$CLUSTER_CONTEXT" &>/dev/null \
        && pass "$tpl" || fail "$tpl missing"
done
if amp_installed; then
    for tpl in checkout-source containerfile-build publish-image; do
        owner="$(kubectl get clusterworkflowtemplate "$tpl" --context "$CLUSTER_CONTEXT" \
            -o jsonpath='{.metadata.labels.app\.kubernetes\.io/managed-by}' 2>/dev/null)"
        check "$tpl is Agent Manager's" "$owner" "Helm"
    done
else
    skip "Agent Manager's templates (not installed)"
fi

# ── 5. AEP's gateway selects only AEP's RestApis ────────────────────────────
echo ""
echo "5️⃣  APIGateway scoping"
scope="$(kubectl get apigateway api-platform-default -n openchoreo-data-plane \
    --context "$CLUSTER_CONTEXT" -o jsonpath='{.spec.apiSelector.scope}' 2>/dev/null)"
check "AEP's gateway is label-scoped" "$scope" "LabelSelector"
# A trait that stopped stamping the label would leave every RestApi orphaned —
# served by no gateway at all, which looks like a 404 on a healthy-looking CR.
unlabelled="$(kubectl get restapi -A --context "$CLUSTER_CONTEXT" -o json 2>/dev/null | python3 -c "
import json,sys
items = json.load(sys.stdin).get('items', [])
bad = [f\"{r['metadata']['namespace']}/{r['metadata']['name']}\" for r in items
       if not r['metadata'].get('labels', {}).get('gateway.api-platform.wso2.com/restapi-target')]
print(','.join(bad))" 2>/dev/null)"
if [ -z "$unlabelled" ]; then
    pass "every RestApi names a gateway"
else
    fail "RestApis served by no gateway: ${unlabelled}"
fi

# ── 6. One DeploymentPipeline, covering both environments ───────────────────
echo ""
echo "6️⃣  DeploymentPipeline/default"
envs="$(kubectl get deploymentpipeline default -n default --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .spec.promotionPaths[*]}{.sourceEnvironmentRef.name}{"\n"}{end}' 2>/dev/null | sort | tr '\n' ' ')"
if amp_installed; then
    check "promotes through both environments" "$(echo $envs)" "default development"
else
    check "promotes through AEP's environment" "$(echo $envs)" "development"
fi

# ── 7. Agent Manager, when installed ────────────────────────────────────────
echo ""
echo "7️⃣  Agent Manager"
if ! amp_installed; then
    skip "not installed (ENABLE_AGENT_MANAGER=0)"
else
    for d in amp-api amp-console; do
        ready="$(kubectl get deployment "$d" -n wso2-amp --context "$CLUSTER_CONTEXT" \
            -o jsonpath='{.status.conditions[?(@.type=="Available")].status}' 2>/dev/null)"
        check "$d available" "$ready" "True"
    done
    for url in "http://console.amp.localhost:8080" "http://api.amp.localhost:8080"; do
        code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$url" 2>/dev/null)"
        # Any answer that is not a connection failure proves the vhost routes;
        # the app's own status code is its business.
        if [ "$code" != "000" ]; then pass "$url routes (HTTP ${code})"; else fail "$url unreachable"; fi
    done
fi

echo ""
if [ "$FAILURES" -eq 0 ]; then
    echo "✅ All convergence invariants hold."
else
    echo "❌ ${FAILURES} invariant(s) violated."
fi
exit "$FAILURES"
