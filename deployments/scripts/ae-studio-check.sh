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

# Read-only check of the live ae-studio Resource and its pod. Usage:
#   make ae-studio-check ORG=<org> [CP_CONTEXT=<ctx>] [DP_CONTEXT=<ctx>]
# CP_CONTEXT holds the ResourceType, Resource and ResourceReleaseBinding;
# DP_CONTEXT holds the pod and answers the public hosts. Both default to the
# current kube context (local k3d is one cluster). OpenChoreo renders the
# dataplane objects as r-<resource>-<env>-<hash> in a dp-* namespace, so the
# Deployment is found by its OC labels and every other name is derived from it.
# One ok/FAIL line per check; exit 1 on any FAIL. Never reads a Secret value:
# only key names, and AE_SECRET_REV compared in-shell and never printed.

set -euo pipefail

ORG="${ORG:-default}"
CUR=$(kubectl config current-context)
kcp() { kubectl --context "${CP_CONTEXT:-$CUR}" "$@"; }
kdp() { kubectl --context "${DP_CONTEXT:-$CUR}" "$@"; }

FAILS=0
check() { # check <description> <command...>
  local desc=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "ok   $desc"
  else
    echo "FAIL $desc"
    FAILS=$((FAILS + 1))
  fi
}
finish() {
  if [ "$FAILS" -gt 0 ]; then
    echo "ae-studio-check: $FAILS check(s) failed"
    exit 1
  fi
  echo "ae-studio-check: all checks passed"
  exit 0
}
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

# --- control plane ---------------------------------------------------------
rt_hash=$(kcp get resourcetype ae-studio -n "$ORG" \
  -o jsonpath='{.metadata.annotations.aep\.wso2\.com/ae-studio-template-hash}' 2>/dev/null || true)
check "ResourceType ae-studio has its template-hash annotation" test -n "$rt_hash"

latest=$(kcp get resource ae-studio -n "$ORG" -o jsonpath='{.status.latestRelease.name}' 2>/dev/null || true)
check "Resource ae-studio has a latestRelease" test -n "$latest"

rrb=$(kcp get resourcereleasebinding -n "$ORG" -o name 2>/dev/null | grep '/ae-studio-' | head -1 || true)
check "ResourceReleaseBinding ae-studio-* exists" test -n "$rrb"
if [ -z "$rrb" ]; then
  echo "     no ae-studio ResourceReleaseBinding in namespace '$ORG' (context ${CP_CONTEXT:-$CUR}): converge has not run for this org"
  finish
fi
pin=$(kcp get "$rrb" -n "$ORG" -o jsonpath='{.spec.resourceRelease}')
ready=$(kcp get "$rrb" -n "$ORG" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}')
check "binding Ready" test "$ready" = True
check "binding pinned to latestRelease (${latest:-none})" test -n "$latest" -a "$pin" = "$latest"

# --- dataplane: Deployment by OC labels, names derived from it ------------
deploys=$(kdp get deploy -A -l "openchoreo.dev/resource=ae-studio,openchoreo.dev/namespace=$ORG" \
  -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}' 2>/dev/null || true)
ndeploy=$(printf '%s' "$deploys" | grep -c . || true)
check "exactly one ae-studio Deployment in the cell namespace (found $ndeploy)" test "$ndeploy" = 1
if [ "$ndeploy" = 1 ]; then
  dpns=${deploys%%/*}
  dname=${deploys##*/}
  want=${AE_STUDIO_EXPECT_CONTAINERS:-3}
  sel=$(kdp get deploy "$dname" -n "$dpns" \
    -o go-template='{{range $k, $v := .spec.selector.matchLabels}}{{$k}}={{$v}},{{end}}')
  sel=${sel%,}
  pod_ready=$(kdp get pods -n "$dpns" -l "$sel" \
    -o jsonpath='{range .items[0].status.containerStatuses[*]}{.ready}{"\n"}{end}' 2>/dev/null | grep -c true || true)
  check "pod containers ready $pod_ready/$want" test "$pod_ready" = "$want"

  # Secret derived from the tools container's envFrom; key names and the
  # revision match only, never a value.
  secret=$(kdp get deploy "$dname" -n "$dpns" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].envFrom[0].secretRef.name}')
  expect_rev=$(kdp get deploy "$dname" -n "$dpns" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].env[?(@.name=="AE_EXPECTED_SECRET_REV")].value}')
  check "tools container references a Secret (derived name)" test -n "$secret"
  keys=$(kdp get secret "$secret" -n "$dpns" -o go-template='{{range $k, $v := .data}}{{$k}}{{"\n"}}{{end}}' 2>/dev/null || true)
  for k in GITHUB_PAT GITHUB_WEBHOOK_SECRET AE_PUBLISHER_CLIENT_ID AE_PUBLISHER_CLIENT_SECRET \
    AE_STUDIO_CLIENT_ID AE_STUDIO_CLIENT_SECRET AE_SECRET_REV; do
    check "Secret has key $k" grep -qx "$k" <<<"$keys"
  done
  live_rev=$(kdp get secret "$secret" -n "$dpns" -o jsonpath='{.data.AE_SECRET_REV}' 2>/dev/null | base64 -d 2>/dev/null || true)
  check "Secret AE_SECRET_REV matches the container's AE_EXPECTED_SECRET_REV" \
    test -n "$expect_rev" -a "$live_rev" = "$expect_rev"
else
  echo "     expected one Deployment labelled openchoreo.dev/resource=ae-studio,openchoreo.dev/namespace=$ORG (context ${DP_CONTEXT:-$CUR})"
fi

# --- public hosts ----------------------------------------------------------
out() { kcp get "$rrb" -n "$ORG" -o jsonpath="{.status.outputs[?(@.name==\"$1\")].value}"; }
design=$(out designUrl)
tools=$(out toolsUrl)
collab=$(out collabUrl | sed 's#^ws#http#')
check "binding outputs designUrl/toolsUrl/collabUrl present" test -n "$design" -a -n "$tools" -a -n "$collab"
if [ -n "$design" ] && [ -n "$tools" ] && [ -n "$collab" ]; then
  for u in "$design/v1/projects/x/turns/active" "$tools/v1/projects/x/files" "$collab/v1/rooms"; do
    got=$(curl -s --max-time 10 -o "$tmp" -w '%{http_code} %{content_type}' "$u" || true)
    check "unauthenticated GET $u is 401 problem+json (got ${got:-none})" \
      test "${got%%;*}" = "401 application/problem+json"
  done
  code=$(curl -s --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'X-GitHub-Event: ping' \
    --data '{}' "$tools/webhooks/github" || true)
  check "unsigned POST /webhooks/github is 401 (got ${code:-none})" test "$code" = 401
fi

finish
