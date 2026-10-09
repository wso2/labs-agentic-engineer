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

# P5 hook and kickoff (scenario 5.2 to 5.5, 5.8). Read-only (gh GETs, SQL
# selects, log reads). The log checks need values the run records at the
# checkpoint: PING_DELIVERY_ID (5.4) and KICKOFF_TURN_ID (5.5); a pod that
# rolled since has lost those lines, so without the ids they SKIP.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need GH_ORG 5.x "the GitHub org" || finish
need P 5.x "the project name" || finish
safe_ident 5.x P "$P" || finish
safe_ident 5.x GH_ORG "$GH_ORG" || finish

# 5.2. The hook's config.url is a capability (locally a smee channel anyone
# can read): the hooks are read and the URL compared with xtrace off, and only
# the count, the id and "matches" leave that block.
xtrace_off
hooks_ok=0 hook_count="" hook_id="" hook_url_state=unset
if hooks=$(gh api "repos/$GH_ORG/$P/hooks" 2>/dev/null); then
  hooks_ok=1
  hook_count=$(printf '%s' "$hooks" | jq 'length' 2>/dev/null || true)
  hook_id=$(printf '%s' "$hooks" | jq -r '.[0].id // empty' 2>/dev/null || true)
  if [ -n "${HOOK_URL:-}" ]; then
    github_hook_url=$(printf '%s' "$hooks" | jq -r '.[0].config.url // empty' 2>/dev/null || true)
    if [ -z "$github_hook_url" ]; then
      hook_url_state="is empty on GitHub"
    elif [ "$github_hook_url" = "$HOOK_URL" ]; then
      hook_url_state=matches
    else
      hook_url_state="does not match HOOK_URL"
    fi
    github_hook_url=""
  fi
fi
hooks=""
xtrace_on_again
if [ "$hooks_ok" = 1 ]; then
  expect_eq 5.2 1 "$hook_count" "hooks on $GH_ORG/$P"
  case $hook_url_state in
    unset) skip 5.2 "the hook URL (HOOK_URL is not set)" ;;
    matches) pass 5.2 "hook URL matches HOOK_URL" ;;
    *) fail 5.2 "hook URL $hook_url_state" ;;
  esac
else
  fail 5.2 "gh api repos/$GH_ORG/$P/hooks failed"
fi
expect_eq 5.2 main "$(gh api "repos/$GH_ORG/$P" --jq .default_branch 2>/dev/null || true)" "default branch"
expect_eq 5.2 specs/.agentic-engineer.toml "$(gh api "repos/$GH_ORG/$P/contents/specs/.agentic-engineer.toml" --jq .path 2>/dev/null || true)" "descriptor path"
hook_id=${HOOK_ID:-$hook_id}

# 5.3
if repo_row=$(psql_q "select repo_url || '|' || coalesce(webhook_id::text, '') from git_repositories where project_id = '$P'" 2>/dev/null); then
  expect_eq 5.3 1 "$(printf '%s\n' "$repo_row" | n_lines)" "git_repositories rows for $P"
  expect_eq 5.3 "${hook_id:-?}" "${repo_row##*|}" "git_repositories.webhook_id"
else
  fail 5.3 "psql on postgres-0 failed"
fi

# 5.4: GitHub's ping on hook create (the oldest ping delivery of the hook).
if [ -n "$hook_id" ] && pings=$(gh api "repos/$GH_ORG/$P/hooks/$hook_id/deliveries" --paginate 2>/dev/null); then
  ping=$(printf '%s' "$pings" | jq -rs '[.[][] | select(.event == "ping")] | last | "\(.status_code) \(.guid)"' 2>/dev/null || true)
  expect_eq 5.4 200 "${ping%% *}" "ping delivery status"
else
  fail 5.4 "could not list the hook's deliveries"
  ping=""
fi
if need PING_DELIVERY_ID 5.4 "the ping's delivery id (log and row checks)"; then
  safe_ident 5.4 PING_DELIVERY_ID "$PING_DELIVERY_ID" || finish
  if logs=$(tools_logs 2>/dev/null); then
    expect_ge 5.4 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"webhook.forwarded"' "\"delivery\":\"$PING_DELIVERY_ID\"")" "webhook.forwarded lines for the ping"
  else
    fail 5.4 "could not read the ae-studio-tools log"
  fi
  if n=$(psql_q "select count(*) from webhook_deliveries where delivery_id = '$PING_DELIVERY_ID'" 2>/dev/null); then
    expect_eq 5.4 1 "$n" "webhook_deliveries rows for the ping"
  else
    fail 5.4 "psql on postgres-0 failed"
  fi
fi

# 5.5: the kickoff reached the pod (wire kind "start").
if need KICKOFF_TURN_ID 5.5 "the kickoff turn id"; then
  safe_ident 5.5 KICKOFF_TURN_ID "$KICKOFF_TURN_ID" || finish
  if logs=$(tools_logs 2>/dev/null); then
    expect_ge 5.5 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"turns.start"' '"kind":"start"' "\"project\":\"$P\"" "\"turnId\":\"$KICKOFF_TURN_ID\"")" "turns.start lines (kind start)"
    expect_ge 5.5 1 "$(printf '%s\n' "$logs" | log_hits '"msg":"internal.access"' '"method":"POST"' "\"path\":\"/internal/v1/repos/$GH_ORG/$P/turns\"")" "internal.access lines for the turns route"
  else
    fail 5.5 "could not read the ae-studio-tools log"
  fi
fi

# 5.8: the ledger row of the kickoff turn (ledger kind kickoff). The row is
# written once and never updated, so updated_at is when aep-api stored it;
# created_at is the turn's START (the ledger orders by it), not the write.
# The pod coalesces finished turns for about 5 s before it sends them.
if row=$(psql_q "select kind || '|' || status || '|' || (input_tokens + output_tokens) || '|' || (cost_usd is not null) || '|' || coalesce(author_id, '') || '|' || coalesce(round(extract(epoch from (updated_at - finished_at)))::text, '') from agent_turns where project_id = '$P' and kind = 'kickoff'" 2>/dev/null); then
  expect_eq 5.8 1 "$(printf '%s\n' "$row" | n_lines)" "agent_turns kickoff rows for $P"
  IFS='|' read -r _ status tokens costed author lag <<<"$row"
  expect_eq 5.8 completed "$status" "kickoff status"
  expect_ge 5.8 1 "$tokens" "kickoff tokens"
  expect_eq 5.8 true "$costed" "kickoff cost_usd is set"
  if [ -n "${USER_SUB:-}" ]; then expect_eq 5.8 "$USER_SUB" "$author" "kickoff author_id"; else expect_ge 5.8 1 "${#author}" "kickoff author_id length"; fi
  if [ -n "$lag" ] && [ "$lag" -le 10 ] && [ "$lag" -ge -1 ]; then pass 5.8 "row written ${lag}s after finished_at"; else fail 5.8 "row written ${lag:-?}s after finished_at, want within 10 s"; fi
else
  fail 5.8 "psql on postgres-0 failed"
fi

finish
