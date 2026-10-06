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

# P8 secret rotation (scenario 8.3, 8.6 to 8.9). Read-only. The run records
# what the rotation replaces: OLD_PAT_REF, OLD_WEBHOOK_REF, POD_UID_BEFORE_ROTATION
# and REV_BEFORE_ROTATION (8.2). 8.3 reads the roll window, which lasts seconds:
# it runs only with P8_ROLL=1, started while the banner shows. 8.8 reads the
# newest ping delivery of the hook, which the driver triggers after the roll.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need P 8.x "the project name" || finish
safe_ident 8.x P "$P" || finish
prefix=${SECRETREF_PREFIX:-$ORG}

# 8.3: during the roll.
if [ "${P8_ROLL:-0}" = 1 ]; then
  if need AEP 8.3 "the aep-api URL" && need TOK_USER_FILE 8.3 "the user token"; then
    expect_eq 8.3 provisioning "$(http_body GET "$AEP/api/v1/ae-studio" "$TOK_USER_FILE" | jq -r '.state // empty' 2>/dev/null || true)" "ae-studio state"
    code=$(curl_auth "$TOK_USER_FILE" -D "$SCRATCH/h" -o "$SCRATCH/b" -w '%{http_code}' "$AEP/api/v1/projects/$P/tags")
    expect_eq 8.3 503 "$code" "git-backed read status"
    expect_eq 8.3 ae_studio_unavailable "$(jq -r '.code // empty' "$SCRATCH/b" 2>/dev/null || true)" "git-backed read code"
    expect_eq 8.3 5 "$(tr -d '\r' <"$SCRATCH/h" | awk -F': *' 'tolower($1) == "retry-after" { print $2 }')" "Retry-After"
    code=$(curl_auth "$TOK_USER_FILE" -o "$SCRATCH/b" -w '%{http_code}' "$AEP/api/v1/projects/$P/status")
    expect_eq 8.3 200 "$code" "project status"
    expect_eq 8.3 unavailable "$(jq -r '.spec.availability // empty' "$SCRATCH/b" 2>/dev/null || true)" "spec.availability"
  fi
else
  skip 8.3 "reads the roll window (set P8_ROLL=1 while the banner shows)"
fi

# 8.6: the references and the roll.
if need OLD_PAT_REF 8.6 "the github-pat reference name before the rotation"; then
  safe_ident 8.6 OLD_PAT_REF "$OLD_PAT_REF" || finish
  if ref=$(psql_q "select secret_ref_name from org_secrets where secret = 'github-pat'" 2>/dev/null); then
    if [[ $ref =~ ^$prefix-github-pat-[0-9a-f]+$ ]] && [ "$ref" != "$OLD_PAT_REF" ]; then
      pass 8.6 "org_secrets github-pat names a new reference"
    else
      fail 8.6 "org_secrets github-pat is $ref, old was $OLD_PAT_REF"
    fi
  else
    fail 8.6 "psql on postgres-0 failed"
  fi
  gone=$(kubectl get secretreference "$OLD_PAT_REF" -n "$ORG" 2>&1 || true)
  case $gone in *NotFound*) pass 8.6 "old SecretReference $OLD_PAT_REF is gone" ;; *) fail 8.6 "old SecretReference $OLD_PAT_REF still answers" ;; esac
fi
if need OLD_WEBHOOK_REF 8.6 "the webhook-secret reference name before the rotation"; then
  expect_eq 8.6 "$OLD_WEBHOOK_REF" "$(psql_q "select secret_ref_name from org_secrets where secret = 'github-webhook-secret'" 2>/dev/null || true)" "github-webhook-secret reference name"
fi
if resolve_studio; then
  if need POD_UID_BEFORE_ROTATION 8.6 "the pod uid before the rotation"; then
    sel=$(kubectl -n "$DPNS" get deploy "$DNAME" -o json 2>/dev/null | jq -r '.spec.selector.matchLabels | to_entries | map("\(.key)=\(.value)") | join(",")' || true)
    uid=$(kubectl -n "$DPNS" get pods -l "$sel" -o jsonpath='{.items[0].metadata.uid}' 2>/dev/null || true)
    if [ -n "$uid" ] && [ "$uid" != "$POD_UID_BEFORE_ROTATION" ]; then pass 8.6 "pod uid changed"; else fail 8.6 "pod uid is $uid, before the rotation $POD_UID_BEFORE_ROTATION"; fi
  fi
  if need REV_BEFORE_ROTATION 8.6 "AE_EXPECTED_SECRET_REV before the rotation"; then
    rev=$(kubectl -n "$DPNS" get deploy "$DNAME" -o jsonpath='{.spec.template.spec.containers[?(@.name=="ae-studio-tools")].env[?(@.name=="AE_EXPECTED_SECRET_REV")].value}' 2>/dev/null || true)
    if [ -n "$rev" ] && [ "$rev" != "$REV_BEFORE_ROTATION" ]; then pass 8.6 "AE_EXPECTED_SECRET_REV changed"; else fail 8.6 "AE_EXPECTED_SECRET_REV is unchanged or empty"; fi
  fi
  if secrets=$(kubectl -n "$DPNS" get secret -o name 2>/dev/null); then
    expect_eq 8.6 1 "$(printf '%s\n' "$secrets" | grep -c -E -- '-tools$' || true)" "Secrets named *-tools in $DPNS"
  else
    fail 8.6 "kubectl get secret in $DPNS failed"
  fi

  # 8.7: the first read after the roll cloned bare.
  if logs=$(tools_logs 2>/dev/null); then
    expect_ge 8.7 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"repo.clone"' '"mode":"bare"')" "repo.clone mode bare lines in the new pod"
  else
    fail 8.7 "could not read the ae-studio-tools log"
    logs=""
  fi

  # 8.8: the newest ping was delivered and forwarded by the new pod.
  if need HOOK_ID 8.8 "the hook id" && need GH_ORG 8.8 "the GitHub org" && safe_ident 8.8 GH_ORG "$GH_ORG"; then
    if pings=$(gh api "repos/$GH_ORG/$P/hooks/$HOOK_ID/deliveries" 2>/dev/null); then
      ping=$(printf '%s' "$pings" | jq -r '[.[] | select(.event == "ping")] | first | "\(.status_code) \(.guid)"' 2>/dev/null || true)
      expect_eq 8.8 200 "${ping%% *}" "newest ping delivery status"
      if safe_ident 8.8 ping-guid "${ping##* }"; then
        expect_ge 8.8 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"webhook.forwarded"' "\"delivery\":\"${ping##* }\"")" "webhook.forwarded lines for the newest ping"
      fi
    else
      fail 8.8 "could not list the hook's deliveries"
    fi
  fi
else
  fail 8.6 "expected exactly one ae-studio Deployment for org $ORG"
fi

# 8.9: no secret value anywhere in the server, the rotated token included.
if need_values 8.9; then
  read -r lines hits <<<"$(pg_dump_scan)"
  if [ "${lines:-0}" -gt 0 ]; then
    expect_eq 8.9 0 "${hits:-?}" "pg_dumpall lines holding a secret value (of $lines)"
  else
    fail 8.9 "pg_dumpall produced no output"
  fi
fi

finish
