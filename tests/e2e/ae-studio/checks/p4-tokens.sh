#!/usr/bin/env bash
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

# P4 token negatives (scenario 4.2, 4.4 to 4.11). Read-only requests. 4.3 (the
# Room WebSocket) stays a manual agent-browser step.
# Tokens: U user (TOK_USER_FILE), W other-org user (TOK_W_FILE), M the AE-only
# M2M client (TOK_M_FILE), B the publisher client (TOK_B_FILE).
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need DESIGN 4.x "every check needs the design agent URL" || finish
need TOOLS 4.x "every check needs the tools URL" || finish
need AEP 4.x "every check needs the aep-api URL" || finish
p=${P:-x}

# want ID WANT LABEL METHOD URL TOKEN_FILE [curl args...]
want() {
  local id=$1 w=$2 label=$3 method=$4 url=$5 tok=$6
  shift 6
  expect_eq "$id" "$w" "$(http_code "$method" "$url" "$tok" "$@")" "$label"
}

if need TOK_W_FILE 4.2 "the other-org user token"; then
  want 4.2 403 "W on design turns/active" GET "$DESIGN/v1/projects/$p/turns/active" "$TOK_W_FILE"
  want 4.2 403 "W on tools files" GET "$TOOLS/v1/projects/$p/files?prefix=specs/" "$TOK_W_FILE"
  want 4.4 404 "W on aep-api project read" GET "$AEP/api/v1/projects/$p" "$TOK_W_FILE"
fi

for t in M B; do
  f=TOK_${t}_FILE
  if need "$f" "4.5-4.6" "the $t token"; then
    want 4.5 401 "$t on design /v1" GET "$DESIGN/v1/projects/$p/turns/active" "${!f}"
    want 4.5 401 "$t on tools /v1" GET "$TOOLS/v1/projects/$p/files?prefix=specs/" "${!f}"
    want 4.6 401 "$t on aep-api /api/v1" GET "$AEP/api/v1/projects" "${!f}"
  fi
done

if need TOK_USER_FILE 4.7 "the user token"; then
  want 4.7 401 "U on tools /internal/v1/github/identity" GET "$TOOLS/internal/v1/github/identity" "$TOK_USER_FILE"
  want 4.7 401 "U on aep-api /internal/v1 repository" GET "$AEP/internal/v1/ae-studio/projects/$p/repository" "$TOK_USER_FILE"
  want 4.11 404 "U on design /internal/v1/x" GET "$DESIGN/internal/v1/x" "$TOK_USER_FILE"
  want 4.11 404 "U on tools /admin" GET "$TOOLS/admin" "$TOK_USER_FILE"
fi

# 4.8: the impersonation header must be the pod's org (its AE_ORG_ID, the org's
# OU id), not the handle.
if need TOK_M_FILE 4.8 "the M token"; then
  org_id=${ORG_OU_ID:-}
  if [ -z "$org_id" ] && resolve_studio; then
    org_id=$(kubectl -n "$DPNS" get deploy "$DNAME" -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].env[?(@.name=="AE_ORG_ID")].value}' 2>/dev/null || true)
  fi
  url="$TOOLS/internal/v1/github/identity"
  want 4.8 403 "M, no X-Impersonate-Org" GET "$url" "$TOK_M_FILE"
  want 4.8 403 "M, X-Impersonate-Org of another org" GET "$url" "$TOK_M_FILE" -H 'X-Impersonate-Org: 00000000-0000-0000-0000-000000000000'
  if [ -n "$org_id" ]; then
    resp=$(http_body GET "$url" "$TOK_M_FILE" -H "X-Impersonate-Org: $org_id" -w '\n%{http_code}')
    expect_eq 4.8 200 "${resp##*$'\n'}" "M, X-Impersonate-Org of the pod's org"
    expect_eq 4.8 true "$(printf '%s' "${resp%$'\n'*}" | jq -r 'has("login") and has("id")' 2>/dev/null || true)" "identity body has login and id"
  else
    skip 4.8 "the pod's org id (AE_ORG_ID or ORG_OU_ID) could not be read"
  fi
  want 4.9 401 "M on aep-api /internal/v1 repository" GET "$AEP/internal/v1/ae-studio/projects/$p/repository" "$TOK_M_FILE"
fi

# 4.10: CORS preflight from the console origin and from a foreign one.
if need CONSOLE 4.10 "the console origin"; then
  pre=$DESIGN/v1/projects/$p/turns/active
  hdrs=$(http_headers OPTIONS "$pre" - -H "Origin: $CONSOLE" -H 'Access-Control-Request-Method: GET' -H 'Access-Control-Request-Headers: authorization')
  expect_eq 4.10 "$CONSOLE" "$(printf '%s\n' "$hdrs" | awk -F': ' '$1 == "access-control-allow-origin" { print $2 }')" "allow-origin for the console origin"
  expect_eq 4.10 1 "$(printf '%s\n' "$hdrs" | awk -F': ' '$1 == "access-control-allow-headers" && tolower($2) ~ /authorization/ { n++ } END { print n + 0 }')" "allow-headers names authorization"
  hdrs=$(http_headers OPTIONS "$pre" - -H "Origin: ${OTHER_ORIGIN:-https://evil.example}" -H 'Access-Control-Request-Method: GET' -H 'Access-Control-Request-Headers: authorization')
  expect_eq 4.10 0 "$(printf '%s\n' "$hdrs" | awk -F': ' '$1 == "access-control-allow-origin" { n++ } END { print n + 0 }')" "allow-origin for a foreign origin"
fi

finish
