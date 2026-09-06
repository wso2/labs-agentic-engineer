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

# read_json <what> <kubectl args...> — a kubectl read whose FAILURE is a
# failure of the check that wanted it.
#
# Most checks below are shaped "compute the offenders, empty means clean". An
# unread list is empty in exactly the same way, so a `2>/dev/null` on the
# kubectl that feeds one turns an unreachable cluster, a missing CRD or a typo'd
# resource name into a green tick. Every such read goes through here instead:
# the document on success, one recorded failure and no output otherwise.
read_json() {
    local what="$1"; shift
    local out
    if ! out="$(kubectl --context "$CLUSTER_CONTEXT" "$@" -o json 2>&1)"; then
        fail "could not read ${what} — this check asserted nothing: $(printf '%s' "$out" | tr '\n' ' ' | cut -c1-160)"
        return 1
    fi
    printf '%s' "$out"
}

# assert_ran <n> <what> [skip] — guards a check whose assertions come from a
# loop. Zero iterations print nothing at all, which reads as a clean check
# rather than an absent one. Pass `skip` where an empty list is a legitimate
# state of the cluster (no app deployed yet) rather than a defect (no
# environment at all).
assert_ran() {
    [ "${1:-0}" -gt 0 ] && return 0
    if [ "${3:-}" = "skip" ]; then
        skip "no ${2} on this cluster — nothing to check"
    else
        fail "no ${2} to check — this check asserted nothing"
    fi
    return 1
}

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
# Retried: `kubectl run --rm -i` occasionally returns nothing at all on a busy
# cluster — the pod runs and its output is lost. Reporting that as "could not
# mint a token" would be a false negative on the single most load-bearing check
# in this script, so a silent empty result is retried rather than believed.
tok=""
for _ in 1 2 3; do
    tok="$(kubectl run "idp-probe-$RANDOM" -n "$THUNDER_NS" --rm -i --restart=Never \
        --image=curlimages/curl:8.11.1 --quiet --context "$CLUSTER_CONTEXT" -- \
        -s -X POST "${THUNDER_INTERNAL_URL}/oauth2/token" \
        -d "grant_type=client_credentials&client_id=aep-api-client&client_secret=aep-api-client-secret" \
        2>/dev/null | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null)"
    [ -n "$tok" ] && break
    sleep 3
done
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
if bindings_json="$(read_json "clusterauthzrolebindings" get clusterauthzrolebindings.openchoreo.dev)"; then
    stale="$(printf '%s' "$bindings_json" | python3 -c "
import json,sys
bad = [b['metadata']['name'] for b in json.load(sys.stdin)['items']
       if b['spec']['entitlement']['claim'] == 'sub']
print(','.join(bad))")"
    if [ -z "$stale" ]; then
        pass "no binding left on the 'sub' claim"
    else
        fail "still on 'sub': ${stale}"
    fi
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

# ── 5. Every environment has its own gateway, and every RestApi names one ───
echo ""
echo "5️⃣  Per-environment APIGateways"
# There is no cluster-wide gateway. A gateway terminates a managed API's
# authentication, so each environment gets its own — `api-platform-<org>-<env>`
# in namespace `<org>-<env>` — trusting only that environment's Thunder
# (scripts/setup-environment-gateway.sh). An environment without one has nothing
# to serve its components' APIs.
GATEWAYS="$(kubectl get apigateway -A --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name} {end}' 2>/dev/null)"
export GATEWAYS
programmed_gateway() {
    kubectl get apigateway "$2" -n "$1" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null
}
while read -r env_ns env_name; do
    [ -n "${env_name:-}" ] || continue
    gw_ns="${env_ns}-${env_name}"
    gw="api-platform-${env_ns}-${env_name}"
    case " $GATEWAYS " in
        *" ${gw_ns}/${gw} "*)
            check "${env_ns}/${env_name} → apigateway/${gw} Programmed" \
                "$(programmed_gateway "$gw_ns" "$gw")" "True" ;;
        *) fail "${env_ns}/${env_name} has no apigateway/${gw} in ${gw_ns}" ;;
    esac
done <<EOF
$(kubectl get environment -A --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .items[*]}{.metadata.namespace} {.metadata.name}{"\n"}{end}' 2>/dev/null)
EOF
# The other half of the same contract: the api-configuration trait stamps
# `restapi-target` on every RestApi, and an APIGateway selects on exactly that
# label. A RestApi whose label names no existing gateway is served by nothing at
# all, which looks like a 404 on a perfectly healthy-looking CR.
if restapis_json="$(read_json "RestApis" get restapi -A)"; then
    orphans="$(printf '%s' "$restapis_json" | python3 -c "
import json, os, sys
known = {g.split('/')[-1] for g in os.environ.get('GATEWAYS', '').split()}
bad = []
for r in json.load(sys.stdin).get('items', []):
    md = r['metadata']
    where = md['namespace'] + '/' + md['name']
    target = md.get('labels', {}).get('gateway.api-platform.wso2.com/restapi-target')
    if not target:
        bad.append(where + ' (unlabelled)')
    elif target not in known:
        bad.append(where + ' -> ' + target)
print(', '.join(bad))")"
    if [ -z "$orphans" ]; then
        pass "every RestApi's restapi-target names an existing APIGateway"
    else
        fail "RestApis served by no gateway: ${orphans}"
    fi
fi

# ── 6. One DeploymentPipeline, one environment ──────────────────────────────
# Both products deploy into Environment/default, so the pipeline has exactly one
# promotion path whether or not Agent Manager is installed.
echo ""
echo "6️⃣  DeploymentPipeline/default"
envs="$(kubectl get deploymentpipeline default -n default --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .spec.promotionPaths[*]}{.sourceEnvironmentRef.name}{"\n"}{end}' 2>/dev/null | sort | tr '\n' ' ')"
check "promotes through the shared environment only" "$(echo $envs)" "default"

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

# ── 8. The shared IdP admits BOTH consoles' origins ─────────────────────────
#
# The consoles' very first call is a browser fetch of the OIDC discovery
# document. If Thunder answers it without an Access-Control-Allow-Origin the
# browser discards a perfectly good 200 and the console dies before it can even
# render a login form — while curl against the same URL looks completely
# healthy, which is what makes this worth asserting rather than eyeballing.
#
# Both origins are checked because the allow-list lives in a single `cors`
# server_config document that either product's bootstrap can redeclare; the
# regression to catch is one product's document silently replacing the other's
# origin. The unlisted origin is checked too, so a wildcard that "fixes" this by
# admitting everything fails here instead of passing.
echo ""
echo "8️⃣  CORS: the shared IdP admits both consoles"
cors_origin() {
    curl -s -D- -o /dev/null --max-time 10 \
        "${PUBLIC_THUNDER_URL}/.well-known/openid-configuration" \
        -H "Origin: $1" 2>/dev/null \
        | tr -d '\r' | awk 'tolower($1)=="access-control-allow-origin:"{print $2}'
}
cors_expect=("${PUBLIC_CONSOLE_URL}")
if amp_installed; then
    cors_expect+=("http://console.amp.localhost:8080")
else
    skip "Agent Manager not installed — its console origin is not asserted"
fi
for o in "${cors_expect[@]}"; do
    check "origin ${o} allowed" "$(cors_origin "$o")" "$o"
done
denied="$(cors_origin "http://not-allowed.invalid")"
if [ -z "$denied" ]; then
    pass "unlisted origin refused (allow-list, not wildcard)"
else
    fail "unlisted origin was allowed — got '${denied}'"
fi

# ── 9. The `system` scope survives the token endpoint ───────────────────────
#
# ThunderID resolves a requested scope against a resource server. With a
# server-wide default resource server set — Agent Manager's, on this cluster —
# a client_credentials request that does not name the System resource server as
# its `resource` gets a 200 and a token with NO scope claim, and every admin
# call then 403s with nothing in either response saying why. aep-api, the
# thunder-app operator and seed-test-users.sh all send the indicator; this
# mints exactly as they do and asserts both that the scope survived and that the
# directory answers. The unguarded mint is printed, not asserted: it documents
# the trap this check exists for, and its result is allowed to change when the
# platform's default resource server does.
echo ""
echo "9️⃣  System scope survives the token endpoint"
system_rs="${THUNDER_SYSTEM_RESOURCE_IDENTIFIER:-${PUBLIC_THUNDER_URL}/mcp}"
system_secret="${THUNDER_SYSTEM_CLIENT_SECRET:-aep-system-client-secret}"
mint_system_token() { # $1: resource indicator, or "" to omit it
    local args=(-d grant_type=client_credentials -d client_id=aep-system-client
        --data-urlencode "client_secret=${system_secret}" -d scope=system)
    [ -n "$1" ] && args+=(--data-urlencode "resource=$1")
    curl -s --max-time 10 -X POST "${PUBLIC_THUNDER_URL}/oauth2/token" "${args[@]}" 2>/dev/null \
        | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null
}
token_scope() { # prints the scope claim, space-joined; empty when absent
    python3 -c "
import base64, json, sys
p = sys.argv[1].split('.')[1]; p += '=' * (-len(p) % 4)
s = json.loads(base64.urlsafe_b64decode(p)).get('scope', '')
print(' '.join(s) if isinstance(s, list) else s)" "$1" 2>/dev/null
}
stok="$(mint_system_token "$system_rs")"
if [ -z "$stok" ]; then
    fail "aep-system-client could not mint a token (THUNDER_SYSTEM_CLIENT_SECRET set?)"
else
    check "scope claim with resource=${system_rs}" "$(token_scope "$stok")" "system"
    users_code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 \
        -H "Authorization: Bearer ${stok}" "${PUBLIC_THUNDER_URL}/users" 2>/dev/null)"
    check "GET /users with that token" "$users_code" "200"
fi
bare="$(mint_system_token "")"
[ -n "$bare" ] && printf "   ·  without the indicator the scope claim is '%s'\n" "$(token_scope "$bare")"

# ── 10. Every ThunderApplication lives on its environment's Thunder ─────────
#
# The thunder-app operator has no single target: it resolves one per CR from the
# (org, environment) binding record. Two things can silently undo that — an
# operator that falls back to the platform IdP, and a binding that drifts from
# what the CR was actually registered against — and neither shows up as an error
# anywhere. So: nothing the operator names may exist on T1, and every CR's
# recorded issuer must be its own environment's.
echo ""
echo "🔟  ThunderApplications live on their environment's Thunder, not on the platform IdP"
if [ -z "$stok" ]; then
    fail "cannot list platform-IdP applications (check 9 minted no token)"
else
    # The operator registers "aep-<cr-namespace>-<cr-name>" (thunderAppName in
    # internal/controller/thunderapplication_controller.go) and OpenChoreo renders
    # every CR into a dp-* namespace, so anything matching aep-dp-* on T1 is a
    # leftover of the single-target operator — including from CRs already deleted.
    strays="$(curl -s --max-time 10 -H "Authorization: Bearer ${stok}" \
        "${PUBLIC_THUNDER_URL}/applications?limit=200" 2>/dev/null \
        | python3 -c "
import sys, json
apps = json.load(sys.stdin).get('applications', [])
print(' '.join(sorted(a.get('clientId','') for a in apps if (a.get('clientId') or '').startswith('aep-dp-'))))" 2>/dev/null)"
    check "aep-dp-* applications on the platform IdP" "${strays:-<none>}" "<none>"
fi

# Per CR: the issuer it was registered against must be the one its (org, env)
# binding names. A CR with no binding is a failure in itself — the operator
# cannot have registered it anywhere.
checked=0
while IFS='|' read -r cr org env got want; do
    [ -n "$cr" ] || continue
    checked=$((checked + 1))
    check "${cr} issuer (org=${org} env=${env})" "${got:-<unset>}" "${want:-<no binding>}"
done < <(
    apps_json="$(kubectl get thunderapplications -A -o json --context "$CLUSTER_CONTEXT" 2>/dev/null)"
    binds_json="$(kubectl get configmaps -A -l aep.wso2.com/kind=thunder-binding -o json --context "$CLUSTER_CONTEXT" 2>/dev/null)"
    APPS="$apps_json" BINDS="$binds_json" python3 -c "
import json, os
apps = json.loads(os.environ['APPS'] or '{}').get('items', [])
binds = {}
for cm in json.loads(os.environ['BINDS'] or '{}').get('items', []):
    lb = cm['metadata'].get('labels', {})
    binds[(lb.get('aep.wso2.com/org'), lb.get('aep.wso2.com/env'))] = cm.get('data', {}).get('issuer', '')
for a in apps:
    m, lb = a['metadata'], a['metadata'].get('labels', {})
    org, env = lb.get('openchoreo.dev/namespace'), lb.get('openchoreo.dev/environment')
    print('|'.join([m['namespace'] + '/' + m['name'], org or '', env or '',
                    a.get('status', {}).get('issuer', ''), binds.get((org, env), '')]))"
)
assert_ran "$checked" "ThunderApplications" skip

# ── 11. Every environment's binding record is complete and usable ──────────
#
# The binding is what makes the second tier addressable: the operator finds its
# target through it, aep-api finds its credential through it, and
# setup-environment-gateway.sh reads the issuer it trusts out of it. It lives in
# four places at once — a ConfigMap, a Secret mirrored into the operator's
# namespace, an OpenBao document and a set of Environment annotations — and
# nothing in the cluster reconciles them against each other. A projection that
# drifts fails at the consumer that reads it, one 401 at a time, so all four are
# compared here against the ConfigMap and the credential is actually exercised.
echo ""
echo "1️⃣1️⃣  Every environment has a working Thunder binding"
env_annotation() { # <org> <env> <suffix>
    kubectl get environment "$2" -n "$1" --context "$CLUSTER_CONTEXT" \
        -o "jsonpath={.metadata.annotations.aep\\.wso2\\.com/thunder-$3}" 2>/dev/null
}
# The T2 credential is minted exactly as aep-api and the operator mint it —
# client_secret_post, scope=system, and the environment's OWN resource
# indicator, which is `<its issuer>/mcp` and not the platform IdP's.
mint_env_token() { # <issuer> <client id> <secret> <resource>
    curl -s --max-time 15 -X POST "${1}/oauth2/token" \
        -d grant_type=client_credentials \
        --data-urlencode "client_id=$2" \
        --data-urlencode "client_secret=$3" \
        -d scope=system \
        --data-urlencode "resource=$4" 2>/dev/null \
        | python3 -c "import sys,json;print(json.load(sys.stdin).get('access_token',''))" 2>/dev/null
}
checked=0
while read -r env_ns env_name; do
    [ -n "${env_name:-}" ] || continue
    checked=$((checked + 1))
    where="${env_ns}/${env_name}"
    b="$(thunder_binding_configmaps "$env_ns" "$env_name")"
    set -- $b
    if [ "$#" -eq 0 ]; then
        fail "${where} has no thunder-binding ConfigMap (scripts/setup-environment-thunder.sh ${env_ns} ${env_name})"
        continue
    elif [ "$#" -gt 1 ]; then
        fail "${where} has ${#} thunder-binding ConfigMaps: ${b}— the operator refuses an ambiguous binding"
        continue
    fi
    b_ns="${1%%/*}"; b_name="${1##*/}"
    pass "${where} → configmap/${b_name} in ${b_ns}"

    # Safe to read by (org, env) now that the ambiguity check above has passed:
    # there is exactly one record, so the reader in utils.sh resolves the same
    # ConfigMap this loop just named.
    binding_data() { thunder_binding_value "$env_ns" "$env_name" "$1"; }
    b_release="$(binding_data release)"
    b_issuer="$(binding_data issuer)"
    b_admin="$(binding_data adminURL)"
    b_rs="$(binding_data systemResourceIdentifier)"
    b_path="$(binding_data secretPath)"
    b_sec_name="$(binding_data secretName)"
    b_sec_ns="$(binding_data secretNamespace)"

    # The release the binding names must be the one actually installed there.
    # A binding pointing at a release that no longer exists reads perfectly and
    # resolves to an issuer nothing answers on.
    if helm_release_deployed "$b_release" "$b_ns"; then
        pass "${where} → helm release ${b_release} deployed in ${b_ns}"
    else
        fail "${where} binding names release ${b_release} in ${b_ns}, which is not deployed"
    fi

    # aep-api sees only the OpenChoreo API, so the non-secret half is projected
    # onto the Environment. It is a copy, and a copy can go stale.
    check "${where} annotation thunder-issuer" "$(env_annotation "$env_ns" "$env_name" issuer)" "$b_issuer"
    check "${where} annotation thunder-admin-url" "$(env_annotation "$env_ns" "$env_name" admin-url)" "$b_admin"
    check "${where} annotation thunder-system-resource-identifier" \
        "$(env_annotation "$env_ns" "$env_name" system-resource-identifier)" "$b_rs"
    check "${where} annotation thunder-secret-path" "$(env_annotation "$env_ns" "$env_name" secret-path)" "$b_path"
    check "${where} annotation thunder-binding" "$(env_annotation "$env_ns" "$env_name" binding)" "$b_name"

    # The third projection. aep-api runs outside the cluster and reads the
    # credential from here and nowhere else, so an absent or stale document is
    # an environment aep-api cannot provision roles or test users in.
    # Stdin closed: this loop is fed by a heredoc, and `bao` attaches stdin (it
    # has to, for the writes elsewhere) — without the redirect it swallows the
    # rest of the heredoc and the loop checks the first environment and stops.
    bao_doc="$(bao kv get -mount="${b_path%%/*}" -format=json "${b_path#*/}" 2>/dev/null </dev/null \
        | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)['data']['data']
except Exception:
    sys.exit(0)
print(d.get('issuer', '') + ' ' + ('secret' if d.get('clientSecret') else 'no-secret'))" 2>/dev/null)"
    check "${where} OpenBao ${b_path}" "${bao_doc:-<missing>}" "${b_issuer} secret"

    # And the credential itself, exercised the way every consumer does.
    client_id="$(kubectl get secret "$b_sec_name" -n "$b_sec_ns" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.data.client-id}' 2>/dev/null | base64 -d 2>/dev/null)"
    client_secret="$(kubectl get secret "$b_sec_name" -n "$b_sec_ns" --context "$CLUSTER_CONTEXT" \
        -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d 2>/dev/null)"
    if [ -z "$client_secret" ]; then
        fail "${where} binding names secret/${b_sec_name} in ${b_sec_ns}, which has no client-secret"
    else
        # The PUBLIC issuer, not adminURL: this script runs on the host, where
        # the in-cluster Service name does not resolve. The two are the same
        # instance — that is what the binding asserts.
        etok="$(mint_env_token "$b_issuer" "$client_id" "$client_secret" "$b_rs")"
        if [ -z "$etok" ]; then
            fail "${where} ${client_id} could not mint on ${b_issuer}"
        else
            check "${where} scope claim with resource=${b_rs}" "$(token_scope "$etok")" "system"
            check "${where} GET ${b_issuer}/users" \
                "$(curl -s -o /dev/null -w '%{http_code}' --max-time 15 \
                    -H "Authorization: Bearer ${etok}" "${b_issuer}/users" 2>/dev/null)" "200"
        fi
    fi
done <<EOF
$(kubectl get environment -A --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .items[*]}{.metadata.namespace} {.metadata.name}{"\n"}{end}' 2>/dev/null)
EOF
assert_ran "$checked" "Environments"

# ── 12. Each gateway trusts exactly its own environment's Thunder ──────────
#
# The gateway's ThunderKeyManager is installed from the binding, and a Helm
# release is never upgraded in place by these scripts — so a gateway that
# outlives a re-provisioned Thunder keeps trusting the old issuer. The failure
# is a 401 on every managed API in that environment, with a gateway reporting
# Programmed and a Thunder reporting healthy. The rendered config is the truth
# here, not the values file: it is what the controller actually loaded.
echo ""
echo "1️⃣2️⃣  Every gateway's ThunderKeyManager is its own environment's Thunder"
checked=0
while read -r env_ns env_name; do
    [ -n "${env_name:-}" ] || continue
    checked=$((checked + 1))
    where="${env_ns}/${env_name}"
    gw_ns="${env_ns}-${env_name}"
    b="$(thunder_binding_configmap "$env_ns" "$env_name")"
    if [ -z "$b" ]; then
        skip "${where} — no binding to compare the gateway against (check 11)"
        continue
    fi
    want_issuer="$(thunder_binding_value "$env_ns" "$env_name" issuer)"
    got_issuer="$(kubectl get configmap "api-platform-${env_ns}-${env_name}-config" -n "$gw_ns" \
        --context "$CLUSTER_CONTEXT" -o jsonpath='{.data.values\.yaml}' 2>/dev/null | python3 -c "
import sys, yaml
try:
    cfg = (yaml.safe_load(sys.stdin) or {}).get('gateway', {}).get('config', {}) or {}
except Exception:
    print(''); raise SystemExit
# The chart writes the rendered config in snake_case; the values file that
# produced it spells the same key in camelCase.
pc = cfg.get('policy_configurations') or cfg.get('policyConfigurations') or {}
for km in (pc.get('jwtauth_v1') or {}).get('keymanagers', []) or []:
    if km.get('name') == 'ThunderKeyManager':
        print(km.get('issuer', ''))
        break
else:
    print('')" 2>/dev/null)"
    check "${where} gateway ThunderKeyManager issuer" "${got_issuer:-<none>}" "$want_issuer"
done <<EOF
$(kubectl get environment -A --context "$CLUSTER_CONTEXT" \
    -o jsonpath='{range .items[*]}{.metadata.namespace} {.metadata.name}{"\n"}{end}' 2>/dev/null)
EOF
assert_ran "$checked" "Environments"

# ── 13. The IdP's server-wide singletons are platform-composed ─────────────
#
# ThunderID keeps exactly ONE server_config per name and a redeclaration
# replaces it, so for `cors`, `defaultResourceServer` and `csp` the document
# that imports LAST owns the whole value. Nothing enforces that at import time:
# a publisher that renumbers a file, or adds a second document of its own,
# silently takes the singleton over — and the symptom is a console that cannot
# fetch its discovery document (cors), a token minted with no scope
# (defaultResourceServer) or a sign-in page whose assets the browser blocks
# (csp). None of them names the bundle that caused it.
#
# So the merged bootstrap ConfigMap is asserted directly: for every singleton
# present, exactly one document is AEP's platform-composed one — matched against
# the files in single-cluster/thunder-resources/, not a name pattern — it sorts
# last, and it carries a composed value rather than the placeholder its file
# holds in Git. Check 8 asserts the effect of this on the running IdP; this
# asserts the bundle that produced it.
echo ""
echo "1️⃣3️⃣  The IdP's server_config singletons are platform-composed"
BOOTSTRAP_CM="aep-thunder-bootstrap"
cm_json="$(kubectl get configmap "$BOOTSTRAP_CM" -n "$THUNDER_NS" --context "$CLUSTER_CONTEXT" \
    -o json 2>/dev/null)"
if [ -z "$cm_json" ]; then
    fail "configmap/${BOOTSTRAP_CM} not found in ${THUNDER_NS} (scripts/setup-thunder.sh)"
else
    # One verdict per line, computed first and asserted after: a reader that
    # died would otherwise feed the loop nothing and this check would pass by
    # printing nothing at all.
    singleton_report="$(CM="$cm_json" AEP_DIR="${SCRIPT_DIR}/../single-cluster/thunder-resources" python3 -c "
import json, os, pathlib, yaml

data = json.loads(os.environ['CM'])['data']
aep_files = {p.name for p in pathlib.Path(os.environ['AEP_DIR']).glob('*.yaml')}

docs = {}
for name, body in data.items():
    try:
        doc = yaml.safe_load(body)
    except yaml.YAMLError:   # a multi-document file is never a server_config
        continue
    if isinstance(doc, dict) and doc.get('resource_type') == 'server_config':
        docs.setdefault(doc.get('name'), {})[name] = doc

for singleton in ('cors', 'defaultResourceServer', 'csp'):
    found = docs.get(singleton, {})
    if not found:
        # csp is Agent Manager's to declare; the others are not optional.
        verdict = 'skip' if singleton == 'csp' else 'fail'
        print(f\"{verdict}|no \`{singleton}\` server_config document in the bundle\")
        continue
    ours = sorted(f for f in found if f in aep_files)
    if len(ours) != 1:
        print(f\"fail|\`{singleton}\`: {len(ours)} platform-composed document(s) \"
              f\"{ours or ''} among {sorted(found)} — expected exactly one\")
        continue
    composed = ours[0]
    last = max(found)
    if composed != last:
        print(f\"fail|\`{singleton}\`: {composed} is overridden by {last}, which imports after it\")
        continue
    if found[composed].get('value') in (None, {}, []):
        print(f\"fail|\`{singleton}\`: {composed} carries no composed value — \"
              f\"the placeholder was applied unmerged\")
        continue
    # Non-empty is not evidence of composition for \`cors\`: unlike the other
    # two, its file ships a NON-EMPTY list in Git, so an allow-list that never
    # saw a publisher's document looks exactly as healthy as a composed one.
    # The union is asserted directly — every publisher origin present in the
    # SAME bundle must survive into the composed document — and the envsubst
    # placeholders must be gone, since an unexpanded \${PUBLIC_CONSOLE_URL}
    # is an origin no browser will ever send.
    if singleton == 'cors':
        def origins(doc):
            return set((doc.get('value') or {}).get('allowedOrigins') or [])
        merged = origins(found[composed])
        unexpanded = sorted(o for o in merged if '\${' in str(o))
        if unexpanded:
            print(f\"fail|\`cors\`: {composed} carries unexpanded placeholder(s) {unexpanded}\")
            continue
        dropped = sorted(
            o for f, doc in found.items() if f != composed for o in origins(doc)
            if o not in merged and '\${' not in str(o))
        if dropped:
            print(f\"fail|\`cors\`: {composed} dropped publisher origin(s) {dropped} — \"
                  f\"the composition replaced the allow-list instead of unioning it\")
            continue
    others = [f for f in sorted(found) if f != composed]
    print(f\"pass|\`{singleton}\` composed into {composed}, last of {len(found)} \"
          f\"document(s) (publishers': {', '.join(others) or 'none'})\")
")"
    if [ -z "$singleton_report" ]; then
        fail "could not read the singletons out of configmap/${BOOTSTRAP_CM}"
    fi
    while IFS='|' read -r verdict detail; do
        [ -n "$verdict" ] || continue
        case "$verdict" in
            pass) pass "$detail" ;;
            skip) skip "$detail" ;;
            *)    fail "$detail" ;;
        esac
    done <<EOF
$singleton_report
EOF
fi

# ── 14. Every deployed app's origin is admitted by its environment IdP ──────
#
# A deployed SPA is a browser client of its ENVIRONMENT's Thunder (T2): it
# fetches the discovery document and exchanges its code with XHR. Its origin is
# born at deploy time and named nowhere except the ThunderApplication CR that
# carries its redirect URIs, so the thunder-app operator projects those origins
# into the instance's writable `cors` layer on every reconcile.
#
# The failure this catches is silent and total: with no
# Access-Control-Allow-Origin the browser discards a healthy 200 and the app
# hangs on its loading screen, while curl against the same URL looks perfect.
# Asserted from the outside — send the Origin, look for the echo — so it holds
# whatever wrote the allow-list.
echo ""
echo "1️⃣4️⃣  Deployed apps' origins are admitted by their environment IdP"
app_origins="$(kubectl get thunderapplications -A --context "$CLUSTER_CONTEXT" -o json 2>/dev/null | python3 -c "
import json, sys
from urllib.parse import urlsplit

try:
    items = json.load(sys.stdin).get('items', [])
except ValueError:
    sys.exit(0)

seen = set()
for app in items:
    if app.get('metadata', {}).get('deletionTimestamp'):
        continue
    issuer = (app.get('status') or {}).get('issuer') or ''
    if not issuer:
        continue   # not registered yet — check 1 covers a Thunder that is missing
    for uri in (app.get('spec', {}).get('redirectUris') or '').split(','):
        parts = urlsplit(uri.strip())
        if parts.scheme not in ('http', 'https') or not parts.netloc:
            continue
        origin = f'{parts.scheme}://{parts.netloc}'
        key = (issuer, origin)
        if key in seen:
            continue
        seen.add(key)
        print(f'{issuer} {origin}')
" 2>/dev/null)"
if [ -z "$app_origins" ]; then
    skip "no deployed app declares a browser redirect URI yet"
else
    while read -r issuer origin; do
        [ -n "$issuer" ] || continue
        got="$(curl -s -D- -o /dev/null --max-time 10 \
            "${issuer}/.well-known/openid-configuration" -H "Origin: ${origin}" 2>/dev/null \
            | tr -d '\r' | awk 'tolower($1)=="access-control-allow-origin:"{print $2}')"
        check "${origin} admitted by ${issuer}" "${got:-<none>}" "$origin"
    done <<EOF
$app_origins
EOF
fi

echo ""
if [ "$FAILURES" -eq 0 ]; then
    echo "✅ All convergence invariants hold."
else
    echo "❌ ${FAILURES} invariant(s) violated."
fi
exit "$FAILURES"
