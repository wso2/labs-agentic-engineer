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

# P0 baseline (scenario 0.1, 0.5). Read-only.
# 0.5 asserts a fresh org; FRESH_ORG=0 skips it on a cluster that has one.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

# 0.1: the logs half of the observability plane is up.
if pods=$(kubectl get pods -A --no-headers 2>/dev/null); then
  read -r total bad <<<"$(printf '%s\n' "$pods" | awk '/fluent-bit|logs-adapter/ { t++; if ($4 != "Running") b++ } END { print t + 0, b + 0 }')"
  expect_ge 0.1 1 "$total" "Fluent Bit and logs adapter pods"
  expect_eq 0.1 0 "$bad" "those pods not Running"
else
  fail 0.1 "kubectl get pods failed"
fi

if fresh_org_only 0.5; then
  if objs=$(kubectl get resourcetype,resource -A -o name 2>/dev/null); then
    expect_eq 0.5 0 "$(printf '%s\n' "$objs" | grep -c ae-studio || true)" "ae-studio ResourceType/Resource objects"
  else
    fail 0.5 "kubectl get resourcetype,resource failed"
  fi
  if rows=$(psql_q "select count(*) from org_secrets" 2>/dev/null); then
    expect_eq 0.5 0 "$rows" "org_secrets rows"
  else
    fail 0.5 "psql on postgres-0 failed"
  fi
fi

finish
