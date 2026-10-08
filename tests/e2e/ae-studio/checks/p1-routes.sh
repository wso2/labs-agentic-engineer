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

# P1 fresh-org and removed-route checks (1.4, 1.5). Read-only
# except for requests that change nothing: 1.4 is sent only while 1.3 saw an
# unconnected org (the call is refused with 409), and 1.5 probes routes that do
# not exist.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need TOK_USER_FILE 1.x "every route check needs the user token" || finish
need AEP 1.x "every route check needs the aep-api URL" || finish

state=""
if fresh_org_only 1.3; then
  studio=$(http_body GET "$AEP/api/v1/ae-studio" "$TOK_USER_FILE")
  state=$(printf '%s' "$studio" | jq -r '.state // "unreadable"' 2>/dev/null || true)
  expect_eq 1.3 absent "$state" "GET /api/v1/ae-studio state"
  expect_eq 1.3 none "$(printf '%s' "$studio" | jq -r 'if .urls then "set" else "none" end' 2>/dev/null || true)" "GET /api/v1/ae-studio urls"
fi

if fresh_org_only 1.4; then
  if [ "$state" = absent ]; then
    resp=$(http_body POST "$AEP/api/v1/projects" "$TOK_USER_FILE" -H 'Content-Type: application/json' \
      -d "{\"name\":\"${P:-e2e-check}\",\"prompt\":\"x\"}" -w '\n%{http_code}')
    expect_eq 1.4 409 "${resp##*$'\n'}" "POST /api/v1/projects status"
    expect_eq 1.4 github_not_connected "$(printf '%s' "${resp%$'\n'*}" | jq -r '.code // empty' 2>/dev/null || true)" "POST /api/v1/projects code"
  else
    skip 1.4 "the org is not unconnected ($state), the POST would create a project"
  fi
fi

# 1.5: every removed route answers its status with a valid user token. The
# rows mirror removedRoutes in services/aep-api/internal/edge/routes_test.go
# plus the older removals below it. Together they cover every path main's
# packages/contracts/api/v1/openapi.yaml had at f8fda3a4b that this branch's lacks.
p=${P:-x}
routes=(
  "GET /api/v1/projects/$p/files 404"
  "GET /api/v1/projects/$p/files/bundle 404"
  "GET /api/v1/projects/$p/files/specs/requirements.md 404"
  "POST /api/v1/projects/$p/files/apply 404"
  "GET /api/v1/collab/validate 404"
  "GET /api/v1/projects/$p/spec/collab-session 404"
  "GET /api/v1/projects/$p/activity 404"
  "GET /api/v1/projects/$p/activity/stream 404"
  "GET /api/v1/projects/$p/agents/conversations 404"
  "POST /api/v1/projects/$p/agents/x/messages 404"
  "GET /api/v1/projects/$p/turns/active 404"
  "GET /api/v1/projects/$p/turns/x 404"
  "GET /api/v1/projects/$p/turns/x/stream 404"
  "POST /api/v1/webhooks/github 404"
  "GET /api/v1/org/credentials/github/connect/callback 404"
  "POST /api/v1/config/git-provider/connect-sessions 404"
  "POST /api/v1/config/idp/client-secret 404"
  "GET /auth/external/jwks.json 404"
  "POST /internal/v1/mcp/playground-token 404"
  "POST /internal/v1/executions/x/credentials/refresh 404"
  "GET /internal/v1/validation/c/context 404"
  "POST /api/v1/rca-agent/reports 405"
)
for row in "${routes[@]}"; do
  read -r method path want <<<"$row"
  expect_eq 1.5 "$want" "$(http_code "$method" "$AEP$path" "$TOK_USER_FILE")" "$method $path"
done

# The dev group is gone: aep-api has one listener, so the port-forward reaches
# the same router and answers 404.
kubectl -n "$NS_AEP" port-forward svc/aep-api 19090:9090 >/dev/null 2>&1 &
BG_PID=$!
up=0
for _ in $(seq 1 20); do
  if curl -s -o /dev/null --max-time 2 http://127.0.0.1:19090/; then up=1; break; fi
  sleep 0.5
done
if [ "$up" = 1 ]; then
  expect_eq 1.5 404 "$(http_code POST http://127.0.0.1:19090/_dev/v1/secret-ref-resync "$TOK_USER_FILE")" "POST /_dev/v1/secret-ref-resync (port-forward)"
else
  fail 1.5 "port-forward to svc/aep-api:9090 did not come up"
fi

finish
