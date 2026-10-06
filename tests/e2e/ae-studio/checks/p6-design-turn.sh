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

# P6 design turn (scenario 6.2, 6.3, 6.8 to 6.10). Reads the watcher's file
# $RUN_DIR/sse-$T.log (each line "<epoch seconds> <SSE line>"), the pod logs,
# GitHub and the ledger. 6.3 is the one check that sends a request that could
# start a turn, so it runs only with P6_LIVE=1 and only while the turn T is
# still the project's active turn.
# shellcheck source=tests/e2e/ae-studio/checks/lib.sh
. "$(dirname "$0")/lib.sh"

need T 6.x "the design turn id" || finish
need P 6.x "the project name" || finish
safe_ident 6.x T "$T" || finish
safe_ident 6.x P "$P" || finish
sse=${SSE_LOG:-$RUN_DIR/sse-$T.log}

# 6.2: one long response, quiet gaps bounded, the closing frames.
completed_at=""
if [ -s "$sse" ]; then
  read -r elapsed gap keepalives <<<"$(tr -d '\r' <"$sse" | awk '
    NF { t = $1; if (!f) f = t; if (l && t - l > g) g = t - l; l = t; if ($2 == ":" && $3 == "keep-alive") k++ }
    END { print l - f, g + 0, k + 0 }')"
  expect_ge 6.2 120 "$elapsed" "seconds from the first to the last stream line"
  if [ "$gap" -le 20 ]; then pass 6.2 "longest gap between lines = ${gap}s (<= 20), $keepalives keep-alive lines"; else fail 6.2 "longest gap between lines = ${gap}s, want <= 20"; fi
  last2=$(tr -d '\r' <"$sse" | awk '$2 == "data:"' | tail -2)
  first_frame=$(printf '%s\n' "$last2" | sed -n 1p)
  second_frame=$(printf '%s\n' "$last2" | sed -n 2p)
  case $first_frame in *turn-completed*) pass 6.2 "second to last frame is turn-completed" ;; *) fail 6.2 "second to last frame is not turn-completed" ;; esac
  case $second_frame in *'[DONE]') pass 6.2 "last frame is [DONE]" ;; *) fail 6.2 "last frame is not [DONE]" ;; esac
  completed_at=${first_frame%% *}
else
  fail 6.2 "$sse is missing or empty"
fi

# 6.3: a second turn while T runs is refused with 409 turn_in_progress.
if [ "${P6_LIVE:-0}" = 1 ] && need DESIGN 6.3 "the design agent URL" && need TOK_USER_FILE 6.3 "the user token"; then
  status=$(http_body GET "$DESIGN/v1/projects/$P/turns/active" "$TOK_USER_FILE")
  active=$(printf '%s' "$status" | jq -r '.turnId // empty' 2>/dev/null || true)
  conv=$(printf '%s' "$status" | jq -r '.conversationId // empty' 2>/dev/null || true)
  if [ "$active" = "$T" ] && safe_ident 6.3 conversationId "$conv"; then
    resp=$(http_body POST "$DESIGN/v1/projects/$P/conversations/$conv/turns" "$TOK_USER_FILE" \
      -H 'Content-Type: application/json' -d '{"instruction":"concurrency probe"}' -w '\n%{http_code}')
    body=${resp%$'\n'*}
    expect_eq 6.3 409 "${resp##*$'\n'}" "second turn status"
    expect_eq 6.3 turn_in_progress "$(printf '%s' "$body" | jq -r '.code // empty' 2>/dev/null || true)" "second turn code"
    expect_eq 6.3 "$T" "$(printf '%s' "$body" | jq -r '.activeTurnId // empty' 2>/dev/null || true)" "second turn activeTurnId"
  else
    skip 6.3 "turn $T is not the project's active turn"
  fi
elif [ "${P6_LIVE:-0}" != 1 ]; then
  skip 6.3 "needs the turn still running (set P6_LIVE=1 while it does)"
fi

# 6.8: remote-git ran in the pod, never through aep-api. Pod calls count only
# inside the turn: from T_START to the turn-completed frame (open-ended when
# the stream log is missing). The log also holds the kickoff and later turns.
if need GH_ORG 6.8 "the GitHub org" && safe_ident 6.8 GH_ORG "$GH_ORG"; then
  if need T_START 6.8 "the turn start time (YYYY-MM-DDTHH:MM:SSZ)"; then
    turn_end=""
    if ! [[ "$T_START" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
      fail 6.8 "T_START is not YYYY-MM-DDTHH:MM:SSZ"
    elif [ -n "$completed_at" ] && ! turn_end=$(epoch_to_utc "$completed_at"); then
      fail 6.8 "the turn-completed frame time is not an epoch second"
    elif ! logs=$(tools_logs 2>/dev/null); then
      fail 6.8 "could not read the ae-studio-tools log"
    else
      pod=0
      for tool in get_remote_git_file_contents search_remote_git_code; do
        n=$(printf '%s\n' "$logs" | log_hits_between "$T_START" "$turn_end" '"msg":"mcp.tools_call"' "\"tool\":\"$tool\"" "\"repo\":\"$GH_ORG/e2e-reference\"" '"upstream":"pod"')
        pod=$((pod + n))
        expect_eq 6.8 0 "$(printf '%s\n' "$logs" | log_hits '"msg":"mcp.tools_call"' "\"tool\":\"$tool\"" '"upstream":"aep-api"')" "$tool lines forwarded to aep-api"
      done
      expect_ge 6.8 1 "$pod" "remote-git tool calls served in the pod between $T_START and ${turn_end:-the end of the log}"
      pass 6.8 "$(printf '%s\n' "$logs" | log_hits '"msg":"mcp.tools_call"' '"upstream":"aep-api"') call(s) of other tools forwarded to aep-api (information)"
    fi
  fi

  # 6.9: the turn-end commit.
  if need T_START 6.9 "the turn start time (YYYY-MM-DDTHH:MM:SSZ)" && sha=$(gh api "repos/$GH_ORG/$P/commits?path=specs" --jq '.[0].sha' 2>/dev/null); then
    if commit=$(gh api "repos/$GH_ORG/$P/commits/$sha" 2>/dev/null); then
      date=$(printf '%s' "$commit" | jq -r '.commit.committer.date' 2>/dev/null || true)
      if [[ "$date" > "$T_START" ]]; then pass 6.9 "newest specs commit $date is after the turn start $T_START"; else fail 6.9 "newest specs commit $date is not after $T_START"; fi
      expect_ge 6.9 1 "$(printf '%s' "$commit" | jq '[.commit.message | select(contains("Co-authored-by:"))] | length' 2>/dev/null || true)" "commit message has Co-authored-by"
      expect_ge 6.9 1 "$(printf '%s' "$commit" | jq '[.files[].filename | select(startswith("specs/design/"))] | length' 2>/dev/null || true)" "files touched under specs/design/"
    else
      fail 6.9 "could not read commit $sha"
    fi
  fi
fi

# 6.10: the ledger row of the design turn.
if row=$(psql_q "select status || '|' || flow || '|' || (input_tokens + output_tokens) || '|' || (cost_usd is not null) || '|' || coalesce(floor(extract(epoch from finished_at))::text, '') from agent_turns where id = '$T'" 2>/dev/null); then
  IFS='|' read -r status flow tokens costed finished <<<"$row"
  expect_eq 6.10 completed "$status" "design turn status"
  expect_eq 6.10 design "$flow" "design turn flow"
  expect_ge 6.10 1 "${tokens:-0}" "design turn tokens"
  expect_eq 6.10 true "$costed" "design turn cost_usd is set"
  if [ -n "$finished" ] && [ -n "$completed_at" ]; then
    d=$((finished - completed_at))
    [ "$d" -ge 0 ] || d=$((-d))
    if [ "$d" -le 10 ]; then pass 6.10 "finished_at is ${d}s from the turn-completed frame"; else fail 6.10 "finished_at is ${d}s from the turn-completed frame, want <= 10"; fi
  else
    fail 6.10 "finished_at or the turn-completed frame time is missing"
  fi
else
  fail 6.10 "psql on postgres-0 failed"
fi

finish
