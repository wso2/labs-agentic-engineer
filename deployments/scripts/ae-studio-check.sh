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
# One ok/FAIL line per check; exit 1 on any FAIL. A kubectl read that fails
# is a FAIL line too, never an abort without a summary. Never reads a Secret
# value: only key names, and AE_SECRET_REV compared in-shell and never
# printed; the relay channel URL is compared in-shell too, never
# printed. It writes nothing; the one POST is an unsigned GitHub ping, which
# the tools container refuses (401) before acting on it.

set -euo pipefail

ORG="${ORG:-default}"
CUR=$(kubectl config current-context 2>/dev/null || true)
kcp() { kubectl --context "${CP_CONTEXT:-$CUR}" "$@"; }
kdp() { kubectl --context "${DP_CONTEXT:-$CUR}" "$@"; }

FAILS=0
fail() {
  echo "FAIL $1"
  FAILS=$((FAILS + 1))
}
check() { # check <description> <command...>
  local desc=$1
  shift
  if "$@" >/dev/null 2>&1; then
    echo "ok   $desc"
  else
    fail "$desc"
  fi
}
# read_into <var> <description> <command...>: sets var to the command's
# stdout; when the command fails, sets it empty and records a FAIL line.
read_into() {
  local __var=$1 __desc=$2 __out
  shift 2
  if __out=$("$@" 2>/dev/null); then
    printf -v "$__var" '%s' "$__out"
  else
    printf -v "$__var" '%s' ""
    fail "read $__desc"
  fi
}
lines() { printf '%s' "$1" | grep -c . || true; }
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

if [ -z "${CP_CONTEXT:-$CUR}" ] || [ -z "${DP_CONTEXT:-$CUR}" ]; then
  fail "a kube context (no current context, and CP_CONTEXT/DP_CONTEXT unset)"
  finish
fi

# --- control plane ---------------------------------------------------------
read_into rt_hash "ResourceType ae-studio" kcp get resourcetype ae-studio -n "$ORG" \
  -o jsonpath='{.metadata.annotations.aep\.wso2\.com/ae-studio-template-hash}'
check "ResourceType ae-studio has its template-hash annotation" test -n "$rt_hash"

read_into latest "Resource ae-studio" kcp get resource ae-studio -n "$ORG" -o jsonpath='{.status.latestRelease.name}'
check "Resource ae-studio has a latestRelease" test -n "$latest"
# The org's relay channel: set = the pod runs a webhook-relay container.
read_into relay_url "Resource ae-studio webhookRelayUrl" kcp get resource ae-studio -n "$ORG" \
  -o jsonpath='{.spec.parameters.webhookRelayUrl}'

read_into rrbs "ResourceReleaseBindings in namespace '$ORG'" kcp get resourcereleasebinding -n "$ORG" \
  -l "openchoreo.dev/resource=ae-studio" -o name
nrrb=$(lines "$rrbs")
check "exactly one ae-studio ResourceReleaseBinding (found $nrrb)" test "$nrrb" = 1
if [ "$nrrb" != 1 ]; then
  echo "     want one ResourceReleaseBinding labelled openchoreo.dev/resource=ae-studio in namespace '$ORG' (context ${CP_CONTEXT:-$CUR}); none means the converge has not run for this org"
  finish
fi
rrb=$rrbs
read_into pin "binding $rrb pin" kcp get "$rrb" -n "$ORG" -o jsonpath='{.spec.resourceRelease}'
read_into ready "binding $rrb Ready" kcp get "$rrb" -n "$ORG" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}'
check "binding Ready" test "$ready" = True
check "binding pinned to latestRelease (${latest:-none})" test -n "$latest" -a "$pin" = "$latest"

# --- dataplane: Deployment by OC labels, names derived from it ------------
read_into deploys "ae-studio Deployments" kdp get deploy -A -l "openchoreo.dev/resource=ae-studio,openchoreo.dev/namespace=$ORG" \
  -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name}{"\n"}{end}'
ndeploy=$(lines "$deploys")
check "exactly one ae-studio Deployment in the cell namespace (found $ndeploy)" test "$ndeploy" = 1
if [ "$ndeploy" = 1 ]; then
  dpns=${deploys%%/*}
  dname=${deploys##*/}
  if [ -n "$relay_url" ]; then want_default=4; else want_default=3; fi
  want=${AE_STUDIO_EXPECT_CONTAINERS:-$want_default}
  read_into sel "Deployment $dpns/$dname selector" kdp get deploy "$dname" -n "$dpns" \
    -o go-template='{{range $k, $v := .spec.selector.matchLabels}}{{$k}}={{$v}},{{end}}'
  sel=${sel%,}
  pods=""
  if [ -n "$sel" ]; then
    # One line per pod: its name, then each container's ready flag.
    read_into pods "pods of $dpns/$dname" kdp get pods -n "$dpns" -l "$sel" \
      -o jsonpath='{range .items[*]}{.metadata.name}{range .status.containerStatuses[*]}{" "}{.ready}{end}{"\n"}{end}'
  fi
  npods=$(lines "$pods")
  check "Deployment has pods (found $npods)" test "$npods" -gt 0
  while read -r pod flags; do
    [ -n "$pod" ] || continue
    # shellcheck disable=SC2086 # split the flags one per line
    nready=$(printf '%s\n' $flags | grep -cx true || true)
    check "pod $pod containers ready $nready/$want" test "$nready" = "$want"
  done <<<"$pods"

  # Secret derived from the tools container's envFrom; key names and the
  # revision match only, never a value.
  read_into secret "tools container's Secret name" kdp get deploy "$dname" -n "$dpns" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].envFrom[0].secretRef.name}'
  read_into expect_rev "tools container's AE_EXPECTED_SECRET_REV" kdp get deploy "$dname" -n "$dpns" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].env[?(@.name=="AE_EXPECTED_SECRET_REV")].value}'
  check "tools container references a Secret (derived name)" test -n "$secret"
  if [ -n "$secret" ]; then
    read_into keys "Secret $dpns/$secret key names" kdp get secret "$secret" -n "$dpns" \
      -o go-template='{{range $k, $v := .data}}{{$k}}{{"\n"}}{{end}}'
    for k in GITHUB_PAT GITHUB_WEBHOOK_SECRET AE_STUDIO_CLIENT_ID AE_STUDIO_CLIENT_SECRET AE_SECRET_REV; do
      check "Secret has key $k" grep -qx "$k" <<<"$keys"
    done
    # The pod holds only its own aep-api client: the org's publisher client
    # stays with the coding Jobs (ADR-0046 decision 5).
    pub_keys=$(grep -c '^AE_PUBLISHER_' <<<"$keys" || true)
    check "Secret has no AE_PUBLISHER_* key" test "$pub_keys" = 0
    read_into rev_b64 "Secret $dpns/$secret AE_SECRET_REV" kdp get secret "$secret" -n "$dpns" -o jsonpath='{.data.AE_SECRET_REV}'
    live_rev=$(printf '%s' "$rev_b64" | base64 -d 2>/dev/null || true)
    check "Secret AE_SECRET_REV matches the container's AE_EXPECTED_SECRET_REV" \
      test -n "$expect_rev" -a "$live_rev" = "$expect_rev"
  fi

  # Webhook relay: present exactly when the Resource names a channel;
  # then the tools container's hooks point at it and gosmee has subscribed.
  read_into cnames "Deployment $dpns/$dname container names" kdp get deploy "$dname" -n "$dpns" \
    -o jsonpath='{range .spec.template.spec.containers[*]}{.name}{"\n"}{end}'
  has_relay=$(grep -cx webhook-relay <<<"$cnames" || true)
  if [ -n "$relay_url" ]; then
    check "webhook-relay container present (webhookRelayUrl is set)" test "$has_relay" = 1
    read_into hook_url "tools container's AE_WEBHOOK_URL" kdp get deploy "$dname" -n "$dpns" \
      -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].env[?(@.name=="AE_WEBHOOK_URL")].value}'
    check "tools container's AE_WEBHOOK_URL is the relay channel" test "$hook_url" = "$relay_url"
    read_into relay_logs "webhook-relay logs" kdp logs "deploy/$dname" -n "$dpns" -c webhook-relay
    check "webhook-relay subscribed and forwards to 127.0.0.1:8082/webhooks/github" \
      grep -q "Forwarding .*127\.0\.0\.1:8082/webhooks/github" <<<"$relay_logs"
  else
    check "no webhook-relay container (webhookRelayUrl is empty)" test "$has_relay" = 0
  fi
else
  echo "     expected one Deployment labelled openchoreo.dev/resource=ae-studio,openchoreo.dev/namespace=$ORG (context ${DP_CONTEXT:-$CUR})"
fi

# --- public hosts ----------------------------------------------------------
read_into design "binding output designUrl" kcp get "$rrb" -n "$ORG" -o jsonpath='{.status.outputs[?(@.name=="designUrl")].value}'
read_into tools "binding output toolsUrl" kcp get "$rrb" -n "$ORG" -o jsonpath='{.status.outputs[?(@.name=="toolsUrl")].value}'
read_into collab "binding output collabUrl" kcp get "$rrb" -n "$ORG" -o jsonpath='{.status.outputs[?(@.name=="collabUrl")].value}'
collab=${collab/#ws/http}
check "binding outputs designUrl/toolsUrl/collabUrl present" test -n "$design" -a -n "$tools" -a -n "$collab"
if [ -n "$design" ] && [ -n "$tools" ] && [ -n "$collab" ]; then
  for u in "$design/v1/projects/x/turns/active" "$tools/v1/projects/x/files" "$collab/v1/rooms"; do
    got=$(curl -s --max-time 10 -o "$tmp" -w '%{http_code} %{content_type}' "$u" || true)
    check "unauthenticated GET $u is 401 problem+json (got ${got:-none})" \
      test "${got%%;*}" = "401 application/problem+json"
  done
  # Unsigned, so the webhook handler refuses it before acting: benign.
  code=$(curl -s --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'X-GitHub-Event: ping' \
    --data '{}' "$tools/webhooks/github" || true)
  check "unsigned POST /webhooks/github is 401 (got ${code:-none})" test "$code" = 401
fi

finish
