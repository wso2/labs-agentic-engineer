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

# P3 AE Studio pod (3.4, 3.5). Read-only: `make ae-studio-check`
# is itself read-only (its one POST is an unsigned ping the pod refuses).
# 3.4 compares against POD_UID_BEFORE_KEY, which phase 3 of the run records.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

root=$(cd "$(dirname "$0")/../../../.." && pwd)

# 3.3
if out=$(make -C "$root" ae-studio-check ORG="$ORG" 2>&1); then
  pass 3.3 "make ae-studio-check ORG=$ORG: $(printf '%s\n' "$out" | tail -1)"
else
  fail 3.3 "make ae-studio-check ORG=$ORG failed: $(printf '%s\n' "$out" | grep -E '^FAIL' | head -5 | paste -sd';' -)"
fi

if ! resolve_studio; then
  fail 3.4 "expected exactly one ae-studio Deployment for org $ORG"
  finish
fi

dep=$SCRATCH/deploy.json
if ! kubectl -n "$DPNS" get deploy "$DNAME" -o json >"$dep" 2>/dev/null; then
  fail 3.4 "kubectl get deploy $DPNS/$DNAME failed"
  finish
fi
sel=$(jq -r '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")' "$dep")

# 3.4: the key save rolled the pod; the design agent expects a secret revision;
# one Secret per container, no leftovers.
if need POD_UID_BEFORE_KEY 3.4 "the pod uid recorded before the key save"; then
  uid=$(kubectl -n "$DPNS" get pods -l "$sel" -o jsonpath='{.items[0].metadata.uid}' 2>/dev/null || true)
  if [ -n "$uid" ] && [ "$uid" != "$POD_UID_BEFORE_KEY" ]; then
    pass 3.4 "pod uid changed since the key save"
  else
    fail 3.4 "pod uid is $uid, recorded before the key save: $POD_UID_BEFORE_KEY"
  fi
fi
rev=$(jq -r '[.spec.template.spec.containers[] | select(.name == "ae-design-agent") | .env[]? | select(.name == "AE_EXPECTED_SECRET_REV") | .value] | first // ""' "$dep")
if [ -n "$rev" ]; then pass 3.4 "ae-design-agent AE_EXPECTED_SECRET_REV is set"; else fail 3.4 "ae-design-agent AE_EXPECTED_SECRET_REV is empty"; fi
if secrets=$(kubectl -n "$DPNS" get secret -o name 2>/dev/null); then
  expect_eq 3.4 1 "$(printf '%s\n' "$secrets" | grep -c -E -- '-tools$' || true)" "Secrets named *-tools in $DPNS"
  expect_eq 3.4 1 "$(printf '%s\n' "$secrets" | grep -c -E -- '-agent$' || true)" "Secrets named *-agent in $DPNS"
  expect_eq 3.4 0 "$(printf '%s\n' "$secrets" | grep -c -E -- '-(tools|agent)-[0-9a-f]{6,}$' || true)" "revision-suffixed Secret leftovers in $DPNS"
else
  fail 3.4 "kubectl get secret in $DPNS failed"
fi

# 3.5: pod shape.
expect_eq 3.5 Recreate "$(jq -r '.spec.strategy.type' "$dep")" "Deployment strategy"
expect_eq 3.5 3Gi "$(jq -r '[.spec.template.spec.volumes[] | select(.name == "studio-data") | .emptyDir.sizeLimit] | first // "none"' "$dep")" "studio-data sizeLimit"
expect_eq 3.5 1 "$(jq '[.spec.template.spec.containers[] | select(.name == "ae-design-agent") | .volumeMounts[] | select(.subPath == "snapshots" and .readOnly == true)] | length' "$dep")" "ae-design-agent snapshots mounts (subPath snapshots, readOnly)"
expect_eq 3.5 3 "$(jq '[.spec.template.spec.volumes[] | select(.name | endswith("-sock")) | select(.emptyDir.medium == "Memory")] | length' "$dep")" "socket emptyDirs on Memory"
expect_eq 3.5 false "$(jq -r '.spec.template.spec.automountServiceAccountToken' "$dep")" "automountServiceAccountToken"
expect_ge 3.5 30 "$(jq -r '.spec.template.spec.terminationGracePeriodSeconds // 0' "$dep")" "terminationGracePeriodSeconds"
expect_eq 3.5 absent "$(jq -r '.spec.template.spec.runtimeClassName // "absent"' "$dep")" "runtimeClassName"

finish
