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

# Regression probe for per-operation scope enforcement on a generated app.
#
# Sibling of verify-api-platform.sh, which asserts WHICH IDENTITY TIER a
# gateway trusts. This one asserts, one layer up, WHAT A GIVEN USER MAY DO —
# at the gateway, and again at the service with the gateway bypassed, because
# the two answer differently on purpose:
#
#   at the gateway   every failure is 401 with a byte-identical body and no
#                    WWW-Authenticate. Missing scope is indistinguishable from
#                    no token at all. The gateway cannot be made to say 403:
#                    `onFailureStatusCode` is rejected per-API and per-operation
#                    and is a cluster-wide ConfigMap setting. This is decision
#                    A1: the SPA absorbs it by gating before it calls and
#                    treating a 401-while-my-token-is-still-valid as Forbidden.
#
#   at the service   the middleware answers a true 403 +
#                    `WWW-Authenticate: Bearer error="insufficient_scope"`, and
#                    401 when `X-User-Id` is EMPTY (the gateway always sets a
#                    mapped header, to the empty string when the claim is
#                    absent — test for empty, never for missing).
#
# so a run that only ever calls the gateway cannot tell a working scope check
# from a service that ignores scopes entirely.
#
# ── Usage ──────────────────────────────────────────────────────────────────
#
#   bash deployments/scripts/verify-scopes.sh --creds <file> [options]
#
# Credentials are read from a file (or from the environment), NEVER from argv:
# a password on the command line is visible to every process on the host via
# `ps`, and lands in the shell history. Nothing in this script echoes one, and
# no token is ever printed.
#
# The same rule binds every HTTP call below, because a minted token and a
# client secret are credentials too: they reach curl through `--config -`,
# which is a PIPE from this shell to curl's stdin, so they appear in neither
# curl's argv nor any file on disk. `-H "Authorization: Bearer …"` and
# `-d "…client_secret=…"` would both have leaked through `ps`. The in-cell
# calls are the one place a header still rides argv, and deliberately so: they
# carry `X-User-Id`/`X-User-Scopes` and no token at all, which is the whole
# point of that layer — behind the gateway those headers ARE the identity.
#
# The credentials file is sourced as shell (`KEY=value` lines, `#` comments):
#
#   SPA_CLIENT_ID=expense-tracker-webapp
#   EMPLOYEE_USERNAME=test-employee
#   EMPLOYEE_PASSWORD=...
#   APPROVER_USERNAME=test-approver
#   APPROVER_PASSWORD=...
#   # optional, for the multi-group header-shape assertion
#   MULTIGROUP_USERNAME=...
#   MULTIGROUP_PASSWORD=...
#
# On the local plane those logins come from the `aep:gate/roles` issue's
# `<!-- aep:test-users -->` comment. Keep the file out of the repo (the
# deployments/.creds/ directory is git-ignored) and `chmod 600` it.
#
# ── How a user token is minted ─────────────────────────────────────────────
#
# There is NO password/ROPC grant on Thunder 1.0.0. A user token is minted
# headlessly in five calls through the flow API, producing a token identical
# to the browser's. Four traps, each encoded below:
#
#   1. `POST /flow/execute` without `flowId` → 401 FES-1017 with a misleading
#      "administrative flows require an authenticated caller" message. Nothing
#      is wrong with the caller; the request is simply not bound to an
#      authorization request.
#   2. Direct initiation (`applicationId` + `flowType`) → 400 FES-1010: a
#      `browser`-type app may only be driven through an authorize-initiated
#      flow. Step 1 must therefore be the real `/oauth2/authorize` redirect.
#   3. The action key is `action`, NOT `actionId`/`actionRef`. A wrong key
#      does not error — the server silently re-renders the same VIEW as a 200,
#      which reads exactly like a bad password.
#   4. `flowStatus: COMPLETE` does not carry the code. It carries an
#      `assertion` JWT, which must be handed to the separate
#      `POST /oauth2/auth/callback` {assertion, authId} to get the code. That
#      step is in no discovery document.
#
# `resource=<RS identifier>` rides ONLY `/authorize` — the code exchange sends
# none, and the `aud` survives because Thunder binds it into the refresh token.
#
# The system (T2) token is minted with verify-api-platform.sh's `mint_t2`
# shape, from the environment's own binding — never verify-convergence.sh's
# `mint_system_token`, which targets the PLATFORM tier (T1) and would be
# rejected here for the right reason at the wrong layer.
#
# ── What it asserts ────────────────────────────────────────────────────────
#
#   preflight  the component's RestApi is `Programmed=True` AND
#              `observedGeneration == metadata.generation`. An applied edit is
#              NOT live when `kubectl apply` returns; the previous generation
#              keeps serving (~90 s observed) and a matrix run in that window
#              reports a false pass.
#
#   gateway    200  read op, holder of the scope
#              401  no token
#              401  platform-IdP (T1) token          (wrong issuer/audience)
#              401  T2 system token                  (wrong audience)
#              401  read op, holder of a DIFFERENT scope — and the body is
#                   byte-identical to the no-token 401, which is decision A1
#                   stated as an assertion rather than a caveat
#              200  the public operation, no token
#              404  an undeclared path
#              200  the SIGNED-IN operation (`--signed-in-path`, default /me)
#                   with EITHER user's token, 401 without one. That class
#                   demands a valid token and NO scope, so a projection that
#                   quietly turned "signed-in" into "public" — or into "needs
#                   a scope" — passes every row above and fails here.
#              204  a CORS preflight: OPTIONS with `Origin` and
#                   `Access-Control-Request-Method`, no token. The browser
#                   sends it BEFORE the real call and never attaches the
#                   Authorization header, so a gateway that ran jwt-auth on
#                   OPTIONS would 401 the preflight and the generated SPA
#                   would never reach any of the rows above. Synthesised with
#                   curl — there is no browser in this run.
#
#   service    (kubectl exec from inside the cell, straight to
#              http://<component>.<namespace>:<port>, gateway bypassed)
#              401  no X-User-Id
#              403 + WWW-Authenticate: Bearer error="insufficient_scope"
#                   with X-User-Id but without the scope
#              200  with X-User-Id and the scope
#
#   ownership  the employee's collection read returns strictly fewer rows than
#              the approver's (`own` vs `all` on the same operation)
#
#   headers    with --multi-group-user, `x-user-groups` is a JSON array of the
#              user's groups (`["A","B"]`) — never comma-separated. Skipped,
#              not failed, when no such user is supplied or when the echo path
#              does not expose it.
#
# Exit code is non-zero if any assertion fails.

set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/env.sh"
# shellcheck source=/dev/null
source "$SCRIPT_DIR/utils.sh"

# PUBLIC_THUNDER_URL — the PLATFORM tier's public issuer, whose token must be
# rejected here. Read from deployments/.env like every other probe rather than
# assumed, because an installation may publish it on another host.
load_public_urls "$SCRIPT_DIR/../.env"

usage() {
    cat <<'USAGE'
Usage: verify-scopes.sh --creds <file> [options]

  --creds <file>          KEY=value file with SPA_CLIENT_ID and the test-user
                          logins. Never pass a password on the command line.
                          May be omitted if the same variables are exported.

  --org <name>            OpenChoreo org / namespace        (default: default)
  --env <name>            environment                       (default: default)
  --project <name>        project handle, for messages      (default: unset)
  --component <name>      component whose API is probed     (required)
  --endpoint <name>       workload endpoint name            (default: http)
  --port <n>              service port for the in-cell call (default: discovered)
  --resource <identifier> the project resource server the token is minted for
                          (required; rides /authorize as `resource=`)
  --redirect-uri <url>    the SPA's registered redirect URI
                          (default: http://localhost:5199/callback.html)
  --base-url <url>        override the discovered gateway URL for the API

  --read-path <path>      operation the employee MAY call   (default: /claims)
  --read-scope <handle>   the scope it requires             (default: claims:read)
  --other-path <path>     operation the employee may NOT call
                                                            (default: /reports/monthly)
  --public-path <path>    operation that needs no token     (default: /health)
  --missing-path <path>   path no operation declares        (default: /nope)
  --signed-in-path <path> operation that needs a token but no scope
                                                            (default: /me)
  --origin <url>          Origin the CORS preflight is sent with
                          (default: the origin of --redirect-uri)

  --exec-pod <name>       pod in the data-plane namespace to curl from. When
                          omitted the script looks for one that carries curl
                          and otherwise falls back to an ephemeral
                          `kubectl run --rm` pod running curlimages/curl,
                          which the node PULLS FROM DOCKER HUB — on an
                          air-gapped or rate-limited cluster pass --exec-pod
                          instead of waiting on an ImagePullBackOff.
  --multi-group-user <u>  username (password from MULTIGROUP_PASSWORD) whose
                          token carries two groups, for the header-shape check
  --groups-echo-path <p>  operation that echoes the caller's identity
                                                            (default: /me)
  -h, --help              this text
USAGE
}

ORG_NAME="${ORG_NAME:-default}"
ENV_NAME="${ENV_NAME:-default}"
PROJECT_NAME="${PROJECT_NAME:-}"
COMPONENT_NAME="${COMPONENT_NAME:-}"
ENDPOINT_NAME="${ENDPOINT_NAME:-http}"
SERVICE_PORT="${SERVICE_PORT:-}"
RESOURCE_ID="${RESOURCE_ID:-}"
REDIRECT_URI="${REDIRECT_URI:-http://localhost:5199/callback.html}"
BASE_URL="${BASE_URL:-}"
READ_PATH="/claims"
READ_SCOPE="claims:read"
OTHER_PATH="/reports/monthly"
PUBLIC_PATH="/health"
MISSING_PATH="/nope"
SIGNED_IN_PATH="/me"
SPA_ORIGIN=""
EXEC_POD=""
MULTIGROUP_USERNAME="${MULTIGROUP_USERNAME:-}"
GROUPS_ECHO_PATH="/me"
CREDS_FILE=""

while [ $# -gt 0 ]; do
    case "$1" in
        --creds) CREDS_FILE="$2"; shift 2 ;;
        --org) ORG_NAME="$2"; shift 2 ;;
        --env) ENV_NAME="$2"; shift 2 ;;
        --project) PROJECT_NAME="$2"; shift 2 ;;
        --component) COMPONENT_NAME="$2"; shift 2 ;;
        --endpoint) ENDPOINT_NAME="$2"; shift 2 ;;
        --port) SERVICE_PORT="$2"; shift 2 ;;
        --resource) RESOURCE_ID="$2"; shift 2 ;;
        --redirect-uri) REDIRECT_URI="$2"; shift 2 ;;
        --base-url) BASE_URL="$2"; shift 2 ;;
        --read-path) READ_PATH="$2"; shift 2 ;;
        --read-scope) READ_SCOPE="$2"; shift 2 ;;
        --other-path) OTHER_PATH="$2"; shift 2 ;;
        --public-path) PUBLIC_PATH="$2"; shift 2 ;;
        --missing-path) MISSING_PATH="$2"; shift 2 ;;
        --signed-in-path) SIGNED_IN_PATH="$2"; shift 2 ;;
        --origin) SPA_ORIGIN="$2"; shift 2 ;;
        --exec-pod) EXEC_POD="$2"; shift 2 ;;
        --multi-group-user) MULTIGROUP_USERNAME="$2"; shift 2 ;;
        --groups-echo-path) GROUPS_ECHO_PATH="$2"; shift 2 ;;
        -h|--help) usage; exit 0 ;;
        *) echo "unknown argument: $1" >&2; usage >&2; exit 2 ;;
    esac
done

# ── Credentials ────────────────────────────────────────────────────────────
# Sourced, not parsed, so the file can carry comments. `set -a` exports the
# assignments so the python minter can read them from the environment — argv
# is world-readable through `ps`, the environment of a child process is not.
if [ -n "$CREDS_FILE" ]; then
    if [ ! -f "$CREDS_FILE" ]; then
        echo "❌ credentials file not found: ${CREDS_FILE}" >&2
        exit 2
    fi
    perms="$(stat -f '%Lp' "$CREDS_FILE" 2>/dev/null || stat -c '%a' "$CREDS_FILE" 2>/dev/null)"
    case "$perms" in
        600|400) ;;
        *) echo "   ⚠️  ${CREDS_FILE} is mode ${perms}; chmod 600 it." >&2 ;;
    esac
    set -a
    # shellcheck source=/dev/null
    source "$CREDS_FILE"
    set +a
fi

missing=""
for v in COMPONENT_NAME RESOURCE_ID SPA_CLIENT_ID EMPLOYEE_USERNAME EMPLOYEE_PASSWORD APPROVER_USERNAME APPROVER_PASSWORD; do
    [ -n "${!v:-}" ] || missing="${missing} ${v}"
done
if [ -n "$missing" ]; then
    echo "❌ missing required value(s):${missing}" >&2
    echo "   --component and --resource are flags; the rest belong in --creds." >&2
    exit 2
fi

# The preflight's Origin is the SPA's, and the SPA's origin is the origin of
# the redirect URI it is registered with — the one address in this run that is
# already known to be a browser origin. --origin overrides it for a SPA served
# from somewhere else.
if [ -z "$SPA_ORIGIN" ]; then
    SPA_ORIGIN="$(printf '%s' "$REDIRECT_URI" | sed -E 's|^([a-zA-Z][a-zA-Z0-9+.-]*://[^/]+).*$|\1|')"
fi

OPERATOR_NS="thunder-app-operator-system"
GATEWAY_NS="${ORG_NAME}-${ENV_NAME}"
GATEWAY_NAME="api-platform-${ORG_NAME}-${ENV_NAME}"
T1_CLIENT_ID="${POC_CLIENT_ID:-aep-api-client}"
T1_CLIENT_SECRET="${POC_CLIENT_SECRET:-aep-api-client-secret}"
T1_ISSUER="${THUNDER_PUBLIC:-${PUBLIC_THUNDER_URL:-http://thunder.openchoreo.localhost:8080}}"
KC=(kubectl --context "$CLUSTER_CONTEXT")

# ── Cleanup ────────────────────────────────────────────────────────────────
# `kubectl run --rm` deletes its pod only when the run finishes; a Ctrl-C, a
# CI timeout or a failed assertion that exits early leaves it Running, and the
# next run then collides on the name. Each ephemeral pod is therefore named
# verify-scopes-<pid>-<n> — unique per call AND per run, so two runs never
# fight — and this trap sweeps that pid's pods whatever ends the script.
# The counter lives in a file because `incell` is always called inside `$( )`,
# and a subshell cannot hand a variable back to its parent.
EPHEMERAL_SEQ_FILE=""
next_pod_name() {
    local n
    n=$(( $(cat "$EPHEMERAL_SEQ_FILE" 2>/dev/null || echo 0) + 1 ))
    printf '%s' "$n" > "$EPHEMERAL_SEQ_FILE"
    printf 'verify-scopes-%s-%s' "$$" "$n"
}
cleanup() {
    [ -n "${CLEANUP_DONE:-}" ] && return 0
    CLEANUP_DONE=1
    # Best effort throughout: a cleanup that fails must not turn a passing run
    # into a failing one, so nothing here touches EXIT_CODE and every command
    # swallows its status.
    if [ -n "${DP_NS:-}" ] && [ -n "$EPHEMERAL_SEQ_FILE" ] && [ -s "$EPHEMERAL_SEQ_FILE" ]; then
        local leaked
        leaked="$("${KC[@]}" get pods -n "$DP_NS" -o name 2>/dev/null \
            | sed -n "s|^pod/\(verify-scopes-$$-[0-9][0-9]*\)$|\1|p")"
        if [ -n "$leaked" ]; then
            # shellcheck disable=SC2086
            "${KC[@]}" delete pod -n "$DP_NS" --ignore-not-found --wait=false $leaked >/dev/null 2>&1
        fi
    fi
    [ -n "$EPHEMERAL_SEQ_FILE" ] && rm -f "$EPHEMERAL_SEQ_FILE" 2>/dev/null
    return 0
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
EPHEMERAL_SEQ_FILE="$(mktemp "${TMPDIR:-/tmp}/verify-scopes.$$.seq.XXXXXX")"

EXIT_CODE=0
RESULTS=()
record() { # <PASS|FAIL|SKIP> <layer> <label> <detail>
    RESULTS+=("$1|$2|$3|$4")
    [ "$1" = "FAIL" ] && EXIT_CODE=1
    return 0
}
assert_eq() { # <layer> <label> <expected> <actual>
    if [ "$3" = "$4" ]; then
        record PASS "$1" "$2" "$3"
        printf '   ✅ %-44s %s\n' "$2" "$4"
    else
        record FAIL "$1" "$2" "expected $3, got $4"
        printf '   ❌ %-44s expected %s, got %s\n' "$2" "$3" "$4"
    fi
}

# ── Handing curl a secret without putting it in argv ───────────────────────
# Every call that carries a bearer token or a client secret pipes a curl
# config into `curl --config -`. curl's own argv then holds only the URL and
# the shape flags; the credential travels down a pipe and is never written
# anywhere. The escaper is what makes the quoted config form exact: a `"` or a
# `\` inside a secret would otherwise end the value early or eat a character.
cfg_escape() { sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'; }
bearer_cfg() { # <token> -> a curl config on stdout
    printf 'header = "Authorization: Bearer %s"\n' "$(printf '%s' "$1" | cfg_escape)"
}
form_cfg() { # <urlencoded body> -> a curl config that POSTs it
    printf 'header = "Content-Type: application/x-www-form-urlencoded"\ndata = "%s"\n' \
        "$(printf '%s' "$1" | cfg_escape)"
}

echo "=== Scopes: ${ORG_NAME}/${ENV_NAME} ${PROJECT_NAME:+${PROJECT_NAME}/}${COMPONENT_NAME} ==="

# ───────────────────────────────────────────────────────────────────────────
# 1. Addresses
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "1️⃣  Resolving the deployed API"

RB_NAME="${COMPONENT_NAME}-${ENV_NAME}"
DP_HOST="$("${KC[@]}" get releasebinding "$RB_NAME" -n "$ORG_NAME" \
    -o jsonpath='{.status.endpoints[0].serviceURL.host}' 2>/dev/null)"
if [ -z "$DP_HOST" ]; then
    echo "❌ no ReleaseBinding ${RB_NAME} in namespace ${ORG_NAME} with an endpoint." >&2
    echo "   Is the component deployed to ${ENV_NAME}?" >&2
    exit 1
fi
DP_NS="$(echo "$DP_HOST" | sed -E 's|^[^.]+\.([^.]+)\..*|\1|')"
if [ -z "$SERVICE_PORT" ]; then
    SERVICE_PORT="$("${KC[@]}" get releasebinding "$RB_NAME" -n "$ORG_NAME" \
        -o jsonpath='{.status.endpoints[0].serviceURL.port}' 2>/dev/null)"
fi
if [ -z "$BASE_URL" ]; then
    # Discovered, never constructed: a trait-rendered RestApi is served on a
    # different vhost (…openchoreoapis.localhost) from a hand-authored one in
    # the environment namespace, on port 19080 rather than the control plane's
    # 8080, under a per-project path prefix. Every one of those is a fact about
    # this cluster, not about the API.
    BASE_URL="$("${KC[@]}" get releasebinding "$RB_NAME" -n "$ORG_NAME" \
        -o jsonpath='{.status.endpoints[0].externalURLs.http.scheme}://{.status.endpoints[0].externalURLs.http.host}:{.status.endpoints[0].externalURLs.http.port}{.status.endpoints[0].externalURLs.http.path}' 2>/dev/null)"
fi
BASE_URL="${BASE_URL%/}"
SERVICE_URL="http://${COMPONENT_NAME}.${DP_NS}:${SERVICE_PORT}"
echo "   data-plane namespace: ${DP_NS}"
echo "   gateway base URL:     ${BASE_URL}"
echo "   in-cell service URL:  ${SERVICE_URL}"
if [ -z "$BASE_URL" ] || [ -z "$SERVICE_PORT" ]; then
    echo "❌ could not resolve the API's address from the ReleaseBinding status." >&2
    exit 1
fi

# ───────────────────────────────────────────────────────────────────────────
# 2. Preflight: the deployed generation is the one being served
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "2️⃣  RestApi is live (Programmed + observedGeneration)"

RESTAPI_NAME="$("${KC[@]}" get restapi -n "$DP_NS" \
    -o jsonpath="{range .items[*]}{.metadata.name}{'\n'}{end}" 2>/dev/null \
    | grep -E "^${COMPONENT_NAME}(-${ENDPOINT_NAME})?$" | head -1)"
if [ -z "$RESTAPI_NAME" ]; then
    RESTAPI_NAME="$("${KC[@]}" get restapi -n "$DP_NS" \
        -o jsonpath="{range .items[*]}{.metadata.name}{'\n'}{end}" 2>/dev/null \
        | grep "^${COMPONENT_NAME}" | head -1)"
fi
if [ -z "$RESTAPI_NAME" ]; then
    echo "❌ no RestApi for ${COMPONENT_NAME} in ${DP_NS}." >&2
    exit 1
fi

# A rendering error is invisible from the wire: a RestApi that fails the
# gateway controller's validation serves NOTHING — every path 404s, including
# operations that carry no policy — and that is byte-identical to "the API was
# never deployed". Poll both signals, then give the runtime a moment: the
# controller reports Programmed when the push lands, and the policy engine
# loads it a beat later.
# `observedGeneration` is on the CONDITION, not on `status`. A RestApi on this
# gateway has no top-level `status.observedGeneration` at all — reading that
# path yields the empty string forever, so a preflight written against it never
# passes (or, worse, is written as "empty is fine" and never checks anything).
wait_programmed() {
    local i prog gen obs
    for i in $(seq 1 60); do
        prog="$("${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" \
            -o jsonpath='{.status.conditions[?(@.type=="Programmed")].status}' 2>/dev/null)"
        gen="$("${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" -o jsonpath='{.metadata.generation}' 2>/dev/null)"
        obs="$("${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" \
            -o jsonpath='{.status.conditions[?(@.type=="Programmed")].observedGeneration}' 2>/dev/null)"
        [ -n "$obs" ] || obs="$("${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" \
            -o jsonpath='{.status.observedGeneration}' 2>/dev/null)"
        if [ "$prog" = "True" ] && [ -n "$gen" ] && [ "$gen" = "$obs" ]; then
            printf '   ✅ %-44s generation %s\n' "${RESTAPI_NAME} Programmed" "$gen"
            record PASS preflight "RestApi Programmed, observedGeneration==generation" "gen ${gen}"
            sleep 3
            return 0
        fi
        sleep 2
    done
    printf '   ❌ %-44s Programmed=%s generation=%s observed=%s\n' "$RESTAPI_NAME" "$prog" "$gen" "$obs"
    "${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" \
        -o jsonpath='{.status.conditions[?(@.type=="Programmed")].message}{"\n"}' >&2
    record FAIL preflight "RestApi Programmed, observedGeneration==generation" "Programmed=${prog} gen=${gen} observed=${obs}"
    return 1
}
wait_programmed || { echo "   the matrix below would test the PREVIOUS generation — stopping." >&2; exit 1; }

# ───────────────────────────────────────────────────────────────────────────
# 3. Tokens
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "3️⃣  Minting tokens (nothing below is printed)"

# mint_t2 — the environment's OWN system token, from the credential its
# thunder-binding record carries. verify-api-platform.sh's shape, verbatim.
mint_t2() {
    local org="$1" env="$2" secret cid cs rs issuer
    secret="thunder-binding-${org}-${env}"
    issuer="$("${KC[@]}" get configmap -A \
        -l "aep.wso2.com/kind=thunder-binding,aep.wso2.com/org=${org},aep.wso2.com/env=${env}" \
        -o jsonpath='{.items[0].data.issuer}' 2>/dev/null)"
    [ -n "$issuer" ] || return 1
    cid="$("${KC[@]}" get secret "$secret" -n "$OPERATOR_NS" -o jsonpath='{.data.client-id}' 2>/dev/null | base64 -d)"
    cs="$("${KC[@]}" get secret "$secret" -n "$OPERATOR_NS" -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d)"
    rs="$("${KC[@]}" get secret "$secret" -n "$OPERATOR_NS" -o jsonpath='{.data.system-resource-identifier}' 2>/dev/null | base64 -d)"
    [ -n "$cid" ] && [ -n "$cs" ] || return 1
    form_cfg "grant_type=client_credentials&client_id=${cid}&client_secret=${cs}&scope=system&resource=${rs}" \
        | curl -sS --config - "${issuer}/oauth2/token" \
        | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))'
}

T2_ISSUER="$("${KC[@]}" get configmap -A \
    -l "aep.wso2.com/kind=thunder-binding,aep.wso2.com/org=${ORG_NAME},aep.wso2.com/env=${ENV_NAME}" \
    -o jsonpath='{.items[0].data.issuer}' 2>/dev/null)"
if [ -z "$T2_ISSUER" ]; then
    echo "❌ no thunder-binding for ${ORG_NAME}/${ENV_NAME}; run setup-environment-thunder.sh." >&2
    exit 1
fi
echo "   issuer:   ${T2_ISSUER}"
echo "   resource: ${RESOURCE_ID}"

SYSTEM_TOKEN="$(mint_t2 "$ORG_NAME" "$ENV_NAME")"
[ -n "$SYSTEM_TOKEN" ] && echo "   ✅ T2 system token" || { echo "❌ could not mint the T2 system token." >&2; exit 1; }

T1_TOKEN="$(form_cfg "grant_type=client_credentials&client_id=${T1_CLIENT_ID}&client_secret=${T1_CLIENT_SECRET}" \
    | curl -sS --config - "${T1_ISSUER}/oauth2/token" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin).get("access_token",""))')"
[ -n "$T1_TOKEN" ] && echo "   ✅ platform IdP (T1) token — must be REJECTED below" \
    || { echo "❌ could not mint a platform-IdP token from ${T1_ISSUER}." >&2; exit 1; }

# WHAT TO ASK FOR. Thunder does NOT hand a token every scope the caller's roles
# grant — it returns the INTERSECTION of what was asked for with what they hold.
# Measured on the same account in the same minute:
#
#   scope=openid profile email group ou     -> "openid profile email group ou"
#   scope=… plus the project's catalog      -> "… claims:read claims:submit"
#
# So a run that asks only for the OIDC five gets a token carrying no permission
# at all, every 200 row below 401s, and the table accuses the gateway of a fault
# that is entirely in the probe.
#
# Ask for the project's WHOLE CATALOG, read from the directory that issues the
# token — NOT from the RestApi's operation table. The two are different sets and
# the difference is exactly what the ownership row below tests: a widening
# handle such as `claims:read-all` guards no operation (it changes which ROWS
# `claims:read` returns), so it appears nowhere in the gateway's table, and a
# token minted from that table leaves the approver seeing only their own claims
# and the row failing 0-vs-0 for a reason that is not the service's. The
# catalog is also what the platform writes onto the SPA's client allowlist, so
# this asks for exactly what the generated app asks for.
#
# Thunder then narrows per user, which is the per-user difference every
# assertion below is built on; a handle the caller does not hold is simply
# absent from the token rather than an error.
catalog_scopes() {
    AEP_T2="$T2_ISSUER" AEP_RS="$RESOURCE_ID" AEP_TOKEN="$SYSTEM_TOKEN" python3 - <<'PY'
import json, os, sys, urllib.error, urllib.request

T2 = os.environ["AEP_T2"].rstrip("/")
RS_IDENTIFIER = os.environ["AEP_RS"]
TOKEN = os.environ["AEP_TOKEN"]


# `limit` is capped at 100 on Thunder 1.0.0: 101 and above answer 400 RES-1011
# "The limit parameter must be a positive integer", which reads as a bad
# request rather than an out-of-range one and would send a reader hunting the
# value's TYPE. A project catalog is far smaller, so one page is the whole set.
def get(path):
    req = urllib.request.Request(T2 + path, headers={"Authorization": "Bearer " + TOKEN})
    try:
        with urllib.request.urlopen(req) as r:
            return json.loads(r.read() or b"{}")
    except (urllib.error.HTTPError, urllib.error.URLError, ValueError):
        return {}


def rows(body, *keys):
    for k in keys:
        if isinstance(body.get(k), list):
            return body[k]
    return []


server = None
for s in rows(get("/resource-servers?limit=100"), "resourceServers", "data"):
    if s.get("identifier") == RS_IDENTIFIER:
        server = s
        break
if not server:
    sys.exit(0)   # empty ask; the caller falls back
handles, seen = [], set()
for resource in rows(get("/resource-servers/%s/resources?limit=100" % server["id"]),
                     "resources", "data"):
    actions = get("/resource-servers/%s/resources/%s/actions?limit=100"
                  % (server["id"], resource["id"]))
    for action in rows(actions, "actions", "data"):
        handle = action.get("permission") or "%s:%s" % (resource.get("handle", ""),
                                                        action.get("handle", ""))
        if handle and handle not in seen:
            seen.add(handle)
            handles.append(handle)
print(" ".join(handles))
PY
}

# Fallback: every handle the RENDERED operation table can demand. Narrower than
# the catalog (it carries no widening handle), but it needs no directory read,
# so a run whose system token cannot see the directory still asserts the gateway
# rows instead of stopping. The banner says which source was used, because the
# ownership row's verdict depends on it.
restapi_scopes() {
    "${KC[@]}" get restapi "$RESTAPI_NAME" -n "$DP_NS" -o json 2>/dev/null | python3 -c '
import json, sys
try:
    cr = json.load(sys.stdin)
except Exception:
    sys.exit(0)
seen, out = set(), []
for op in cr.get("spec", {}).get("operations", []) or []:
    for policy in op.get("policies", []) or []:
        scopes = (policy.get("params") or {}).get("scopes") or {}
        for handle in (scopes.get("anyOf") or []) + (scopes.get("allOf") or []):
            if handle not in seen:
                seen.add(handle)
                out.append(handle)
print(" ".join(out))'
}

API_SCOPES="$(catalog_scopes)"
SCOPE_SOURCE="the project resource server's catalog"
if [ -z "$API_SCOPES" ]; then
    API_SCOPES="$(restapi_scopes)"
    SCOPE_SOURCE="the RestApi's operation table (directory unreadable)"
fi
REQUESTED_SCOPES="openid profile email group ou${API_SCOPES:+ ${API_SCOPES}}"
echo "   requesting:  ${REQUESTED_SCOPES}"
echo "   (from ${SCOPE_SOURCE})"

# The five-call flow login. Credentials arrive through the environment
# (AEP_LOGIN_USERNAME / AEP_LOGIN_PASSWORD), never through argv.
mint_user_token() { # <username-var> <password-var> -> prints the access token
    AEP_LOGIN_USERNAME="${!1}" AEP_LOGIN_PASSWORD="${!2}" \
    AEP_IDP="$T2_ISSUER" AEP_CLIENT_ID="$SPA_CLIENT_ID" \
    AEP_REDIRECT_URI="$REDIRECT_URI" AEP_RESOURCE="$RESOURCE_ID" \
    AEP_SCOPE="$REQUESTED_SCOPES" \
    python3 - <<'PY'
import base64, hashlib, http.cookiejar, json, os, sys, urllib.error, urllib.parse, urllib.request

IDP      = os.environ["AEP_IDP"].rstrip("/")
CLIENT   = os.environ["AEP_CLIENT_ID"]
REDIRECT = os.environ["AEP_REDIRECT_URI"]
RESOURCE = os.environ["AEP_RESOURCE"]
USER     = os.environ["AEP_LOGIN_USERNAME"]
PASSWORD = os.environ["AEP_LOGIN_PASSWORD"]
# The caller supplies this (AEP_SCOPE, built from the RestApi above). Thunder
# returns the INTERSECTION of what is asked for with what the user's roles
# grant, so a request naming only the OIDC five comes back carrying only those
# five — no permission at all, and every protected operation 401s for a reason
# that has nothing to do with the gateway. The default below is the OIDC five
# alone because that is the only set true of every deployment; a caller that
# wants to assert anything about permissions must name them.
SCOPE    = os.environ.get("AEP_SCOPE", "openid profile email group ou")

jar = http.cookiejar.CookieJar()
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **kw):
        return None
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar), NoRedirect)

def call(url, data=None, headers=None, method=None):
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        r = opener.open(req)
        return r.status, dict(r.headers), r.read()
    except urllib.error.HTTPError as e:
        return e.code, dict(e.headers), e.read()

def post_json(url, obj):
    s, _, b = call(url, json.dumps(obj).encode(), {"content-type": "application/json"}, "POST")
    try:
        return s, json.loads(b) if b else {}
    except ValueError:
        return s, {"raw": b[:300].decode("utf-8", "replace")}

def die(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)

verifier  = base64.urlsafe_b64encode(os.urandom(32)).rstrip(b"=").decode()
challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()

# 1. /authorize. `resource` rides HERE and only here — the code exchange sends
#    none, and the audience survives because Thunder binds it into the refresh
#    token. Trap 2: a `browser` app cannot be initiated directly with
#    applicationId+flowType (400 FES-1010), so the flow must start from this
#    redirect, which is what hands us flowId (authId) and executionId.
q = urllib.parse.urlencode({
    "response_type": "code", "client_id": CLIENT, "redirect_uri": REDIRECT,
    "scope": SCOPE, "state": "verify-scopes",
    "code_challenge": challenge, "code_challenge_method": "S256",
    "resource": RESOURCE,
})
status, headers, body = call(f"{IDP}/oauth2/authorize?{q}")
location = headers.get("location") or headers.get("Location")
if status != 302 or not location:
    die(f"authorize: expected a 302 to the sign-in gate, got {status} {body[:300]!r}")
params = urllib.parse.parse_qs(urllib.parse.urlparse(location).query)
if "authId" not in params or "executionId" not in params:
    die(f"authorize: redirect carries no authId/executionId: {location}")
flow_id, execution_id = params["authId"][0], params["executionId"][0]

# 2. Render the view and collect the challenge token. Trap 1: `flowId` is
#    mandatory — without it this is 401 FES-1017 "administrative flows require
#    an authenticated caller", which says nothing about the real problem.
status, view = post_json(f"{IDP}/flow/execute", {"flowId": flow_id, "executionId": execution_id})
if status != 200:
    die(f"flow/execute (view): {status} {json.dumps(view)[:300]}")
challenge_token = view.get("challengeToken")
actions = [a.get("ref") for a in view.get("data", {}).get("actions", [])]
action = actions[0] if actions else "action_001"

# 3. Submit the credentials. Trap 3: the key is `action`. With `actionId` or
#    `actionRef` the server re-renders the SAME view as a 200/INCOMPLETE
#    instead of erroring, which reads exactly like a rejected password.
status, done = post_json(f"{IDP}/flow/execute", {
    "flowId": flow_id, "executionId": execution_id, "action": action,
    "challengeToken": challenge_token,
    "inputs": {"username": USER, "password": PASSWORD},
})
if done.get("flowStatus") != "COMPLETE":
    die(f"login for {USER} did not COMPLETE ({status}, flowStatus="
        f"{done.get('flowStatus')}). A re-rendered VIEW here means either bad "
        f"credentials or a wrong action key.")

# 4. Trap 4: COMPLETE carries an `assertion`, not the code. This endpoint is in
#    no discovery document; it is what the sign-in SPA calls.
status, cb = post_json(f"{IDP}/oauth2/auth/callback",
                       {"assertion": done["assertion"], "authId": flow_id})
if status != 200 or "redirect_uri" not in cb:
    die(f"oauth2/auth/callback: {status} {json.dumps(cb)[:300]}")
code = urllib.parse.parse_qs(urllib.parse.urlparse(cb["redirect_uri"]).query).get("code", [None])[0]
if not code:
    die(f"no authorization code in {cb['redirect_uri']}")

# 5. Code exchange. No `resource` here, by design.
data = urllib.parse.urlencode({
    "grant_type": "authorization_code", "redirect_uri": REDIRECT,
    "code": code, "code_verifier": verifier, "client_id": CLIENT,
}).encode()
status, _, body = call(f"{IDP}/oauth2/token", data,
                       {"content-type": "application/x-www-form-urlencoded"}, "POST")
if status != 200:
    die(f"token: {status} {body[:300]!r}")
token = json.loads(body).get("access_token", "")
if not token:
    die("token response carried no access_token")
print(token)
PY
}

EMPLOYEE_TOKEN="$(mint_user_token EMPLOYEE_USERNAME EMPLOYEE_PASSWORD)"
[ -n "$EMPLOYEE_TOKEN" ] && echo "   ✅ ${EMPLOYEE_USERNAME} token" || { echo "❌ could not mint ${EMPLOYEE_USERNAME}'s token." >&2; exit 1; }
APPROVER_TOKEN="$(mint_user_token APPROVER_USERNAME APPROVER_PASSWORD)"
[ -n "$APPROVER_TOKEN" ] && echo "   ✅ ${APPROVER_USERNAME} token" || { echo "❌ could not mint ${APPROVER_USERNAME}'s token." >&2; exit 1; }

MULTIGROUP_TOKEN=""
if [ -n "$MULTIGROUP_USERNAME" ] && [ -n "${MULTIGROUP_PASSWORD:-}" ]; then
    MULTIGROUP_TOKEN="$(mint_user_token MULTIGROUP_USERNAME MULTIGROUP_PASSWORD)"
    [ -n "$MULTIGROUP_TOKEN" ] && echo "   ✅ ${MULTIGROUP_USERNAME} token (two groups)"
fi

# The employee's own scope set, for the service-layer cases. Read off the
# token rather than assumed: `scope` is one space-separated string and it
# INCLUDES the five OIDC scopes, so "holds a scope" is never "is authorized".
EMPLOYEE_SCOPES="$(printf '%s' "$EMPLOYEE_TOKEN" | python3 -c '
import base64, json, sys
p = sys.stdin.read().split(".")[1]
print(json.loads(base64.urlsafe_b64decode(p + "=" * (-len(p) % 4))).get("scope", ""))')"
EMPLOYEE_SUB="$(printf '%s' "$EMPLOYEE_TOKEN" | python3 -c '
import base64, json, sys
p = sys.stdin.read().split(".")[1]
print(json.loads(base64.urlsafe_b64decode(p + "=" * (-len(p) % 4))).get("sub", ""))')"
case " ${EMPLOYEE_SCOPES} " in
    *" ${READ_SCOPE} "*) ;;
    *) echo "   ⚠️  ${EMPLOYEE_USERNAME} does not hold ${READ_SCOPE}; the 200 row will fail for a directory reason, not a gateway one." >&2 ;;
esac

# ───────────────────────────────────────────────────────────────────────────
# 4. The gateway matrix
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "4️⃣  At the gateway (${BASE_URL})"

status_of() { # <path> [token]
    if [ -n "${2:-}" ]; then
        bearer_cfg "$2" | curl -sS --config - -o /dev/null -w '%{http_code}' "${BASE_URL}${1}"
    else
        curl -sS -o /dev/null -w '%{http_code}' "${BASE_URL}${1}"
    fi
}
body_of() { # <path> [token]
    if [ -n "${2:-}" ]; then
        bearer_cfg "$2" | curl -sS --config - "${BASE_URL}${1}"
    else
        curl -sS "${BASE_URL}${1}"
    fi
}
# The CORS preflight a browser sends before the real call: OPTIONS, an Origin,
# the method it intends to use — and deliberately NO token, because the
# browser has not been asked for credentials yet and will not attach any.
preflight_status() { # <path>
    curl -sS -o /dev/null -w '%{http_code}' -X OPTIONS \
        -H "Origin: ${SPA_ORIGIN}" \
        -H "Access-Control-Request-Method: GET" \
        -H "Access-Control-Request-Headers: authorization" \
        "${BASE_URL}${1}"
}

assert_eq gateway "200 ${READ_PATH} with ${READ_SCOPE}"      200 "$(status_of "$READ_PATH" "$EMPLOYEE_TOKEN")"
assert_eq gateway "401 ${READ_PATH} no token"                401 "$(status_of "$READ_PATH")"
assert_eq gateway "401 ${READ_PATH} platform-IdP (T1) token" 401 "$(status_of "$READ_PATH" "$T1_TOKEN")"
assert_eq gateway "401 ${READ_PATH} T2 system token (aud)"   401 "$(status_of "$READ_PATH" "$SYSTEM_TOKEN")"
assert_eq gateway "401 ${OTHER_PATH} scope not held"         401 "$(status_of "$OTHER_PATH" "$EMPLOYEE_TOKEN")"
assert_eq gateway "200 ${PUBLIC_PATH} no token"              200 "$(status_of "$PUBLIC_PATH")"
assert_eq gateway "404 ${MISSING_PATH} undeclared"           404 "$(status_of "$MISSING_PATH")"

# The signed-in class: a token is demanded, a scope is not. It is the one class
# the rows above cannot see — a projection that rendered it public would still
# pass every 200, and one that attached a scope to it would still pass every
# 401 — so it is pinned from both sides, with EITHER user's token and with none.
assert_eq gateway "200 ${SIGNED_IN_PATH} employee (signed-in)"  200 "$(status_of "$SIGNED_IN_PATH" "$EMPLOYEE_TOKEN")"
assert_eq gateway "200 ${SIGNED_IN_PATH} approver (signed-in)"  200 "$(status_of "$SIGNED_IN_PATH" "$APPROVER_TOKEN")"
assert_eq gateway "401 ${SIGNED_IN_PATH} no token"              401 "$(status_of "$SIGNED_IN_PATH")"

# Synthesised preflight. 204 and not 200: the gateway answers the preflight
# itself rather than routing it. A 401 here means jwt-auth is running on
# OPTIONS, which no browser can satisfy and which therefore breaks the SPA
# before any of the assertions above are ever reached.
assert_eq gateway "204 OPTIONS preflight from ${SPA_ORIGIN}"    204 "$(preflight_status "$READ_PATH")"

# Decision A1, as an assertion rather than a caveat: a caller cannot tell
# "your scope is missing" from "you sent no token" — same status, same bytes,
# no WWW-Authenticate on either. The generated SPA is built on this being
# true, so it is checked rather than remembered.
NO_TOKEN_BODY="$(body_of "$READ_PATH")"
NO_SCOPE_BODY="$(body_of "$OTHER_PATH" "$EMPLOYEE_TOKEN")"
if [ "$NO_TOKEN_BODY" = "$NO_SCOPE_BODY" ]; then
    printf '   ✅ %-44s %s\n' "A1: missing scope == no token (body)" "$(printf '%s' "$NO_TOKEN_BODY" | head -c 60)"
    record PASS gateway "A1: missing-scope body is byte-identical to no-token" "identical"
else
    printf '   ❌ %-44s bodies differ\n' "A1: missing scope == no token (body)"
    record FAIL gateway "A1: missing-scope body is byte-identical to no-token" "bodies differ"
fi
WWW_AUTH="$(bearer_cfg "$EMPLOYEE_TOKEN" \
    | curl -sS --config - -D - -o /dev/null "${BASE_URL}${OTHER_PATH}" \
    | tr -d '\r' | grep -ci '^www-authenticate:' || true)"
assert_eq gateway "no WWW-Authenticate on the 401" 0 "$WWW_AUTH"

# ───────────────────────────────────────────────────────────────────────────
# 5. The service, with the gateway bypassed
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "5️⃣  At the service, in-cell (${SERVICE_URL})"

# A pod inside the cell that can make an HTTP call. Preference order: one the
# caller named, then any running pod in the namespace that already has curl (no
# cluster change at all), then an ephemeral pod that deletes itself. The probe
# asks for curl and nothing else: every request below is built out of curl's
# flags (-w '%{http_code}', -D -), so a pod that carries only wget would be
# selected and then fail on the first call.
EXEC_MODE=""
if [ -n "$EXEC_POD" ]; then
    EXEC_MODE="exec"
else
    for p in $("${KC[@]}" get pods -n "$DP_NS" --field-selector=status.phase=Running \
            -o jsonpath='{range .items[*]}{.metadata.name}{"\n"}{end}' 2>/dev/null); do
        if "${KC[@]}" exec -n "$DP_NS" "$p" -- sh -c 'command -v curl' >/dev/null 2>&1; then
            EXEC_POD="$p"; EXEC_MODE="exec"; break
        fi
    done
    [ -n "$EXEC_POD" ] || EXEC_MODE="run"
fi

incell() { # <args…> -> the curl output; prints the status when -w is used
    if [ "$EXEC_MODE" = "exec" ]; then
        "${KC[@]}" exec -n "$DP_NS" "$EXEC_POD" -- curl "$@" 2>/dev/null
    else
        "${KC[@]}" run "$(next_pod_name)" -n "$DP_NS" --rm -i --restart=Never --quiet \
            --image=curlimages/curl:8.10.1 --command -- curl "$@" 2>/dev/null
    fi
}
if [ "$EXEC_MODE" = "run" ]; then
    echo "   (no pod in ${DP_NS} carries curl — using an ephemeral curlimages/curl:8.10.1"
    echo "    pod, which the node pulls from Docker Hub: the first call blocks on that pull,"
    echo "    and on an air-gapped or rate-limited cluster it never returns. Pass --exec-pod"
    echo "    a pod that already has curl to skip it.)"
else
    echo "   calling from pod ${EXEC_POD}"
fi

# The middleware's contract, and the reason the service enforces scopes at all:
# behind the gateway these headers are authoritative, in front of it they are
# caller-controlled. `X-User-Id` EMPTY (not missing) is the 401 — the gateway
# always sets a mapped header, to "" when the claim is absent.
SVC_NO_ID="$(incell -sS -o /dev/null -w '%{http_code}' "${SERVICE_URL}${READ_PATH}")"
assert_eq service "401 no X-User-Id" 401 "$SVC_NO_ID"

SVC_NO_SCOPE="$(incell -sS -o /dev/null -w '%{http_code}' \
    -H "X-User-Id: ${EMPLOYEE_SUB}" -H "X-User-Scopes: openid profile email group ou" \
    "${SERVICE_URL}${READ_PATH}")"
assert_eq service "403 identified but scope not held" 403 "$SVC_NO_SCOPE"

SVC_CHALLENGE="$(incell -sS -D - -o /dev/null \
    -H "X-User-Id: ${EMPLOYEE_SUB}" -H "X-User-Scopes: openid profile email group ou" \
    "${SERVICE_URL}${READ_PATH}" | tr -d '\r' | grep -i '^www-authenticate:' || true)"
case "$SVC_CHALLENGE" in
    *'error="insufficient_scope"'*)
        printf '   ✅ %-44s %s\n' "WWW-Authenticate on the 403" "$SVC_CHALLENGE"
        record PASS service 'WWW-Authenticate: Bearer error="insufficient_scope"' "present" ;;
    *)
        printf '   ❌ %-44s got: %s\n' "WWW-Authenticate on the 403" "${SVC_CHALLENGE:-<none>}"
        record FAIL service 'WWW-Authenticate: Bearer error="insufficient_scope"' "${SVC_CHALLENGE:-absent}" ;;
esac

SVC_OK="$(incell -sS -o /dev/null -w '%{http_code}' \
    -H "X-User-Id: ${EMPLOYEE_SUB}" -H "X-User-Scopes: ${EMPLOYEE_SCOPES}" \
    "${SERVICE_URL}${READ_PATH}")"
assert_eq service "200 identified and holding ${READ_SCOPE}" 200 "$SVC_OK"

# ───────────────────────────────────────────────────────────────────────────
# 6. Ownership — the scope says WHICH OPERATION, the rows say WHOSE DATA
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "6️⃣  Ownership filtering on ${READ_PATH}"

count_rows() { # reads a JSON body on stdin
    python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print(-1); raise SystemExit
if isinstance(d, list):
    print(len(d))
elif isinstance(d, dict):
    for k in ("items", "data", "results", "claims"):
        if isinstance(d.get(k), list):
            print(len(d[k])); break
    else:
        print(-1)
else:
    print(-1)'
}
EMP_ROWS="$(body_of "$READ_PATH" "$EMPLOYEE_TOKEN" | count_rows)"
APP_ROWS="$(body_of "$READ_PATH" "$APPROVER_TOKEN" | count_rows)"
if [ "$EMP_ROWS" -lt 0 ] || [ "$APP_ROWS" -lt 0 ]; then
    printf '   ⏭️  %-44s could not count rows in the response\n' "ownership"
    record SKIP ownership "employee sees fewer rows than the approver" "unparseable body"
elif [ "$EMP_ROWS" -lt "$APP_ROWS" ]; then
    printf '   ✅ %-44s employee %s < approver %s\n' "ownership narrows the rows" "$EMP_ROWS" "$APP_ROWS"
    record PASS ownership "employee sees fewer rows than the approver" "${EMP_ROWS} < ${APP_ROWS}"
else
    printf '   ❌ %-44s employee %s, approver %s\n' "ownership narrows the rows" "$EMP_ROWS" "$APP_ROWS"
    record FAIL ownership "employee sees fewer rows than the approver" "${EMP_ROWS} vs ${APP_ROWS}"
fi

# ───────────────────────────────────────────────────────────────────────────
# 7. x-user-groups is JSON — never observed with two groups before
# ───────────────────────────────────────────────────────────────────────────
echo ""
echo "7️⃣  Header shapes"

if [ -z "$MULTIGROUP_TOKEN" ]; then
    printf '   ⏭️  %-44s no --multi-group-user supplied\n' "x-user-groups is a JSON array"
    record SKIP headers "x-user-groups JSON array for a two-group user" "no two-group user"
else
    GROUPS_BODY="$(body_of "$GROUPS_ECHO_PATH" "$MULTIGROUP_TOKEN")"
    # One group renders as ["Finance"]; two must render as ["A","B"] — a JSON
    # array, brackets and quotes included, NOT a comma-separated string. The
    # one-group shape is measured; this is the first two-group observation.
    SHAPE="$(printf '%s' "$GROUPS_BODY" | python3 -c '
import json, re, sys
raw = sys.stdin.read()
m = re.search(r"\[\s*\"[^\"]+\"\s*(,\s*\"[^\"]+\"\s*)+\]", raw)
if m:
    try:
        print("json-array", len(json.loads(m.group(0))))
    except Exception:
        print("unparseable", 0)
else:
    print("absent", 0)')"
    case "$SHAPE" in
        "json-array "[2-9]*)
            printf '   ✅ %-44s %s\n' "x-user-groups is a JSON array" "$SHAPE"
            record PASS headers "x-user-groups JSON array for a two-group user" "$SHAPE" ;;
        *)
            printf '   ⏭️  %-44s %s at %s — does the echo path expose groups?\n' \
                "x-user-groups is a JSON array" "$SHAPE" "$GROUPS_ECHO_PATH"
            record SKIP headers "x-user-groups JSON array for a two-group user" "$SHAPE" ;;
    esac
fi

# ───────────────────────────────────────────────────────────────────────────
# 8. Table
# ───────────────────────────────────────────────────────────────────────────
echo ""
printf '%-6s %-10s %-52s %s\n' "RESULT" "LAYER" "ASSERTION" "DETAIL"
printf '%-6s %-10s %-52s %s\n' "------" "----------" "----------------------------------------------------" "------"
for row in "${RESULTS[@]}"; do
    IFS='|' read -r r l a d <<< "$row"
    printf '%-6s %-10s %-52s %s\n' "$r" "$l" "$a" "$d"
done

echo ""
if [ "$EXIT_CODE" = 0 ]; then
    echo "✅ every assertion passed for ${COMPONENT_NAME} on ${GATEWAY_NAME}."
else
    echo "❌ one or more assertions failed. Inspect:"
    echo "   kubectl get restapi ${RESTAPI_NAME} -n ${DP_NS} -o yaml"
    echo "   kubectl logs -n ${GATEWAY_NS} -l app.kubernetes.io/component=gateway-runtime --tail=200"
    echo "   kubectl logs -n ${DP_NS} -l component=${COMPONENT_NAME} --tail=200"
fi
exit "$EXIT_CODE"
