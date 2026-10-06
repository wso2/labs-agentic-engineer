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

# P10 disconnect (scenario 10.2). Read-only. Valid only after 10.1 disconnected
# GitHub: the run records DISCONNECTED=1 then (the Resource goes about 25 min
# after the disconnect locally, so run this after that wait).
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need DISCONNECTED 10.2 "GitHub was disconnected (10.1)" || finish
need P 10.2 "the project name" || finish
need GH_ORG 10.2 "the GitHub org" || finish
safe_ident 10.2 P "$P" || finish
safe_ident 10.2 GH_ORG "$GH_ORG" || finish

if hooks=$(gh api "repos/$GH_ORG/$P/hooks" 2>/dev/null); then
  expect_eq 10.2 0 "$(printf '%s' "$hooks" | jq 'length' 2>/dev/null || echo '?')" "hooks on $GH_ORG/$P"
else
  fail 10.2 "gh api repos/$GH_ORG/$P/hooks failed"
fi

if objs=$(kubectl get resource -n "$ORG" -o name 2>/dev/null); then
  expect_eq 10.2 0 "$(printf '%s\n' "$objs" | grep -c ae-studio || true)" "ae-studio Resource objects"
else
  fail 10.2 "kubectl get resource failed"
fi
if pods=$(kubectl get pods -A --no-headers 2>/dev/null); then
  expect_eq 10.2 0 "$(printf '%s\n' "$pods" | awk '$2 ~ /ae-studio/ { n++ } END { print n + 0 }')" "ae-studio pods"
else
  fail 10.2 "kubectl get pods failed"
fi

if refs=$(kubectl get secretreference -n "$ORG" -o name 2>/dev/null); then
  prefix=${SECRETREF_PREFIX:-$ORG}
  for s in github-pat github-webhook-secret; do
    expect_eq 10.2 0 "$(printf '%s\n' "$refs" | grep -c -E "/$prefix-$s-[0-9a-f]+\$" || true)" "SecretReferences $prefix-$s-<hex>"
  done
  for s in ae-publisher-client ae-studio-client; do
    expect_ge 10.2 1 "$(printf '%s\n' "$refs" | grep -c -E "/$prefix-$s-[0-9a-f]+\$" || true)" "SecretReferences $prefix-$s-<hex> kept"
  done
else
  fail 10.2 "kubectl get secretreference failed"
fi

where=""
[ -z "${OC_ORG_ID:-}" ] || where="where oc_org_id = '${OC_ORG_ID//\'/}'"
if rows=$(psql_q "select secret from org_secrets $where order by secret" 2>/dev/null); then
  for s in github-pat github-webhook-secret; do
    expect_eq 10.2 0 "$(printf '%s\n' "$rows" | grep -c -x -F "$s" || true)" "org_secrets rows $s"
  done
  for s in ae-publisher-client ae-studio-client; do
    expect_eq 10.2 1 "$(printf '%s\n' "$rows" | grep -c -x -F "$s" || true)" "org_secrets rows $s kept"
  done
else
  fail 10.2 "psql on postgres-0 failed"
fi

finish
