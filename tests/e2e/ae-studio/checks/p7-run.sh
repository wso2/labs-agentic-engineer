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

# P7 Build run (7.8, 7.12, 7.14, 7.16, 7.17, 7b). Read-only.
# The OC Component is named <project>-<jobRef> (scoped); run_cycles.job_ref is
# the unscoped ca-... name, so every lookup matches on JOB_REF as a substring.
# Checks that need the coding Component to still exist (7.8 live half, 7.16)
# SKIP once the settler has deleted it; 7b asserts the deletion.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need P 7.x "the project name" || finish
need CYCLE_ID 7.x "the coding cycle id" || finish
safe_ident 7.x P "$P" || finish
safe_ident 7.x CYCLE_ID "$CYCLE_ID" || finish
job=${JOB_REF:-}
[ -z "$job" ] || safe_ident 7.x JOB_REF "$job" || finish

# history_names JQ_EVENT_TYPE JQ_NAME_PATH: the names in the workflow history
# ($history, `temporal workflow show --output json`) of the events of one type,
# one per line. The default text output lists event types only, never the
# activity or signal names.
history_names() {
  printf '%s' "$history" | jq -r --arg t "$1" ".events[] | select(.eventType == \$t) | $2 // empty" 2>/dev/null || true
}

# 7.3: the DevRunWorkflow and its planning activity.
history=""
if need WORKFLOW_ID 7.3 "the Temporal workflow id"; then
  safe_ident 7.3 WORKFLOW_ID "$WORKFLOW_ID" || finish
  if temporal workflow describe --namespace default --workflow-id "$WORKFLOW_ID" >/dev/null 2>&1; then
    pass 7.3 "temporal workflow describe $WORKFLOW_ID answered"
  else
    fail 7.3 "temporal workflow describe $WORKFLOW_ID failed"
  fi
  if history=$(temporal workflow show --namespace default --workflow-id "$WORKFLOW_ID" --output json 2>/dev/null) && printf '%s' "$history" | jq -e '.events | length > 0' >/dev/null 2>&1; then
    expect_ge 7.3 1 "$(history_names EVENT_TYPE_ACTIVITY_TASK_SCHEDULED .activityTaskScheduledEventAttributes.activityType.name | grep -c -x -F PlanMilestone || true)" "PlanMilestone activities scheduled in the history"
  else
    history=""
    fail 7.3 "temporal workflow show $WORKFLOW_ID --output json failed or held no events"
  fi
fi

# The cycle row.
cycle=""
if cycle=$(psql_q "select coalesce(component_uid, '') || '|' || coalesce(merge_sha, '') || '|' || coalesce(pr_number::text, '') || '|' || (component_deleted_at is not null) || '|' || coalesce(job_ref, '') || '|' || coalesce(to_char(component_deleted_at at time zone 'UTC', 'YYYY-MM-DD\"T\"HH24:MI:SS\"Z\"'), '') from run_cycles where id::text = '$CYCLE_ID'" 2>/dev/null); then
  IFS='|' read -r component_uid merge_sha pr_number deleted row_job deleted_at <<<"$cycle"
  [ -n "$job" ] || job=$row_job
  expect_eq 7.8 1 "$(printf '%s\n' "$cycle" | n_lines)" "run_cycles rows for $CYCLE_ID"
else
  fail 7.8 "psql on postgres-0 failed (run_cycles)"
  component_uid="" merge_sha="" pr_number="" deleted="" row_job="" deleted_at=""
fi

# 7.8: the Component was dispatched with its uid recorded; while it exists its
# uid matches and its secrets are references.
if [ -n "$component_uid" ]; then
  pass 7.8 "run_cycles.component_uid is recorded"
else
  fail 7.8 "run_cycles.component_uid is empty"
fi
comp=""
comps_ok=0
if [ -n "$job" ] && comps=$(kubectl get component -n "$ORG" -o name 2>/dev/null); then
  comps_ok=1
  comp=$(printf '%s\n' "$comps" | grep -F -- "$job" | head -1 || true)
fi
if [ -n "$comp" ]; then
  uid=$(kubectl get "$comp" -n "$ORG" -o jsonpath='{.metadata.uid}' 2>/dev/null || true)
  expect_eq 7.8 "$component_uid" "$uid" "Component uid vs run_cycles.component_uid"
  if spec=$(kubectl get component,workload -n "$ORG" -o json 2>/dev/null); then
    refs=$(printf '%s' "$spec" | jq -c --arg job "$job" '[.items[] | select(.metadata.name | contains($job))]' 2>/dev/null || true)
    expect_ge 7.8 1 "$(printf '%s' "$refs" | grep -c -E 'default-github-pat-[0-9a-f]+' || true)" "Job spec references default-github-pat-<hex>"
    expect_ge 7.8 1 "$(printf '%s' "$refs" | grep -c -E 'default-coding-agent-key-[0-9a-f]+' || true)" "Job spec references default-coding-agent-key-<hex>"
    if need_values 7.8; then
      read -r _ hits <<<"$(printf '%s\n' "$refs" | value_scan)"
      expect_eq 7.8 0 "$hits" "Job spec lines holding a secret value"
    fi
  else
    fail 7.8 "kubectl get component,workload failed"
  fi
elif [ "$deleted" = true ]; then
  skip 7.8 "the Component is already deleted (settled): the live half does not apply"
else
  fail 7.8 "no Component matching $job in $ORG, and component_deleted_at is not set"
fi

# 7.12: webhook round trip for the PR.
# shellcheck disable=SC2153 # PR_NUMBER is the run.env value; pr_number is the cycle row column
if need PR_NUMBER 7.12 "the PR number" && need GH_ORG 7.12 "the GitHub org" && safe_ident 7.12 PR_NUMBER "$PR_NUMBER" && safe_ident 7.12 GH_ORG "$GH_ORG"; then
  q="select d.delivery_id || '|' || d.action || '|' || d.attempts || '|' || (d.processed_at is not null) || '|' || (d.abandoned_at is null) from webhook_deliveries d join webhook_payloads p on p.delivery_id = d.delivery_id where d.event = 'pull_request' and p.payload->'pull_request'->>'number' = '$PR_NUMBER' and p.payload->'repository'->>'full_name' = '$GH_ORG/$P' order by d.received_at"
  rows=""
  for _ in $(seq 1 30); do
    rows=$(psql_q "$q" 2>/dev/null) || { rows=""; break; }
    if [ "$(printf '%s\n' "$rows" | n_lines)" -ge 2 ] && [ "$(printf '%s\n' "$rows" | grep -c -F '|false|' || true)" = 0 ]; then break; fi
    sleep 2
  done
  expect_ge 7.12 2 "$(printf '%s\n' "$rows" | n_lines)" "pull_request deliveries for PR $PR_NUMBER (opened, closed)"
  logs=""
  logs=$(tools_logs) || logs=""
  hook_deliveries=""
  if need HOOK_ID 7.12 "the hook id (delivery statuses)"; then
    hook_deliveries=$(gh api "repos/$GH_ORG/$P/hooks/$HOOK_ID/deliveries" --paginate 2>/dev/null) || hook_deliveries=""
  fi
  while IFS='|' read -r did action attempts processed abandoned_null; do
    [ -n "$did" ] || continue
    expect_eq 7.12 1 "$attempts" "$action delivery attempts"
    expect_eq 7.12 true "$processed" "$action delivery processed_at is set"
    expect_eq 7.12 true "$abandoned_null" "$action delivery abandoned_at is null"
    if [ -n "$hook_deliveries" ]; then
      expect_eq 7.12 200 "$(printf '%s' "$hook_deliveries" | jq -rs --arg g "$did" '[.[][] | select(.guid == $g)] | first | .status_code' 2>/dev/null || true)" "$action delivery status on the hook"
    fi
    if [ "$(printf '%s\n' "$logs" | log_hits '"msg":"webhook.forwarded"' "\"delivery\":\"$did\"")" -ge 1 ]; then
      pass 7.12 "$action delivery forwarded by the pod"
    else
      skip 7.12 "$action delivery has no webhook.forwarded line in the current pod log (rolled or rotated)"
    fi
  done <<<"$rows"
  if [ -n "$merge_sha" ]; then pass 7.12 "run_cycles.merge_sha is set"; else fail 7.12 "run_cycles.merge_sha is empty"; fi
  expect_eq 7.12 "$PR_NUMBER" "$pr_number" "run_cycles.pr_number"
  if [ -n "$history" ]; then
    expect_ge 7.12 1 "$(history_names EVENT_TYPE_WORKFLOW_EXECUTION_SIGNALED .workflowExecutionSignaledEventAttributes.signalName | grep -c -x -F run-pr-merged || true)" "run-pr-merged signals in the workflow history"
  else
    skip 7.12 "no workflow history read (WORKFLOW_ID unset or the read failed)"
  fi
fi

# 7.14: the usage ledger row of the cycle.
if row=$(psql_q "select l.phase || '|' || (l.input_tokens + l.output_tokens) || '|' || (l.cost_usd is not null and l.cost_usd = c.cost_usd) from agent_usage_ledger l join run_cycles c on c.id::text = l.source_id where l.source = 'run_cycle' and l.source_id = '$CYCLE_ID'" 2>/dev/null); then
  expect_eq 7.14 1 "$(printf '%s\n' "$row" | n_lines)" "ledger rows for the cycle"
  IFS='|' read -r phase tokens same <<<"$row"
  expect_eq 7.14 build "$phase" "ledger phase"
  expect_ge 7.14 1 "${tokens:-0}" "ledger tokens"
  expect_eq 7.14 true "$same" "ledger cost_usd equals the cycle's"
else
  fail 7.14 "psql on postgres-0 failed (ledger)"
fi

# 7.16: while the Component exists its binding is suspended.
if [ -n "$comp" ]; then
  if rb=$(kubectl get releasebinding -n "$ORG" -o json 2>/dev/null); then
    expect_eq 7.16 true "$(printf '%s' "$rb" | jq -r --arg job "$job" '[.items[] | select(.metadata.name | contains($job)) | .spec.componentTypeEnvironmentConfigs.suspend] | first // "none"' 2>/dev/null || true)" "binding componentTypeEnvironmentConfigs.suspend"
  else
    fail 7.16 "kubectl get releasebinding failed"
  fi
else
  skip 7.16 "no coding Component exists any more (settled)"
fi

# 7.17: no ownerless coding pod.
if pods=$(kubectl get pods -A -o json 2>/dev/null); then
  expect_eq 7.17 0 "$(printf '%s' "$pods" | jq --arg p "$P-" '[.items[] | select(.metadata.name | startswith($p)) | select((.metadata.ownerReferences // []) | length == 0)] | length' 2>/dev/null || echo '?')" "ownerless pods named $P-*"
else
  fail 7.17 "kubectl get pods failed"
  pods=""
fi

# 7b: after the settle the Component and its pod are gone, the run is kept and
# read by project scope.
if [ "$deleted" = true ] && [ -n "$job" ]; then
  if [ "$comps_ok" = 1 ]; then
    expect_eq 7b 0 "$(printf '%s\n' "$comps" | grep -c -F -- "$job" || true)" "Components matching $job"
  else
    fail 7b "kubectl get component failed"
  fi
  if [ -n "$pods" ]; then
    expect_eq 7b 0 "$(printf '%s' "$pods" | jq --arg j "$job" '[.items[] | select(.metadata.name | contains($j))] | length' 2>/dev/null || echo '?')" "pods matching $job"
  fi
  if need TAG 7b "the version tag (recording)" && need AEP 7b "the aep-api URL" && need TOK_USER_FILE 7b "the user token"; then
    runs=$(http_body GET "$AEP/api/v1/projects/$P/builds/$TAG/runs" "$TOK_USER_FILE")
    expect_eq 7b kept "$(printf '%s' "$runs" | jq -r --arg c "$CYCLE_ID" '[.runs[].cycles[] | select(.id == $c) | .recording] | first // "none"' 2>/dev/null || true)" "RunCycleView.recording"
  fi
  # observer.read is logged when a request reads the settled cycle's feed.
  # The log check reads the running container's log only: when every aep-api
  # container started after the settle, that read may predate the log, so
  # no line is a SKIP; a container that saw the whole post-settle window
  # and holds no line is a FAIL.
  if alogs=$(aep_logs 2>/dev/null); then
    reads=$(printf '%s\n' "$alogs" | log_hits '"msg":"observer.read"' '"scope":"project"' "\"componentUid\":\"$component_uid\"")
    if [ "$reads" -ge 1 ]; then
      pass 7b "observer.read lines (scope project) for the Component uid = $reads (>= 1)"
    else
      started=$(kubectl -n "$NS_AEP" get pods -l app=aep-api -o json 2>/dev/null |
        jq -r '[.items[].status.containerStatuses[]? | select(.name == "aep-api") | .state.running.startedAt // empty] | min // empty' 2>/dev/null || true)
      if [ -z "$started" ] || [ -z "$deleted_at" ]; then
        fail 7b "no observer.read line (scope project) for the Component uid, and the aep-api start or component_deleted_at could not be read"
      elif [[ "$started" > "$deleted_at" ]]; then
        skip 7b "no observer.read line: the aep-api container started $started, after the settle at $deleted_at (log lost)"
      else
        fail 7b "observer.read lines (scope project) for the Component uid: got 0, and the aep-api log covers the settle at $deleted_at"
      fi
    fi
  else
    fail 7b "could not read the aep-api log"
  fi
else
  skip 7b "the Component is not deleted yet (run_cycles.component_deleted_at unset)"
fi

finish
