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

# walk.sh — the mechanics of a mock-verification walk, so the walker writes none
# of them. Run from the App Path.
#
#   walk.sh up               start `npm run dev:mock` on a free port
#                            → READY <url> · checklist <file>
#   walk.sh restart          stop the server and start it again (same browser)
#   walk.sh post [<issue#>]  rewrite the checklist's first line with the counts;
#                            with an issue, publish the file as ONE comment on
#                            it, edited in place on every later call
#   walk.sh down             stop the server, close the browser, confirm the
#                            port let go → STOPPED
#
# State lives at /tmp/walk-<app>.* where <app> is the App Path's basename; the
# checklist the walker writes is /tmp/walk-<app>.md and `up` prints that path.
#
# Why a script and not four lines in the skill: `npm run` is three processes and
# the runner image has no `ps`/`pkill`, so the process GROUP is the only handle
# that reaps them all — `set -m` makes the background job its own group leader,
# which is why `$!` below is a group id, on Linux and on a macOS laptop alike.
# Every walk used to re-derive that, and the one that did not leaked four dev
# servers into a 3Gi cgroup. The port is searched rather than fixed so two
# web-application walks in one wave do not fight over 5173; a stale server from
# this app's earlier attempt is reaped before a new one starts.

set -euo pipefail

app=$(basename "$PWD")
state="/tmp/walk-$app"
checklist="$state.md"
log="$state.log"
pgid_file="$state.pgid"
port_file="$state.port"
comment_file="$state.comment"

listening() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

stop_server() {
  [ -f "$pgid_file" ] || return 0
  local pgid
  pgid=$(cat "$pgid_file")
  kill -TERM -- "-$pgid" 2>/dev/null || true
  for _ in $(seq 1 20); do
    kill -0 -- "-$pgid" 2>/dev/null || break
    sleep 0.25
  done
  kill -KILL -- "-$pgid" 2>/dev/null || true
  rm -f "$pgid_file"
}

start_server() {
  stop_server
  local port
  for port in $(seq 5173 5199); do
    listening "$port" || break
  done
  if listening "$port"; then
    echo "no free port between 5173 and 5199" >&2
    exit 1
  fi
  : > "$log"
  # `</dev/null`: under job control a server that reads the terminal would be
  # stopped with SIGTTIN; Vite reads stdin for its keyboard shortcuts.
  bash -c 'set -m; npm run dev:mock -- --port "$1" --strictPort </dev/null >"$2" 2>&1 & echo $! > "$3"' \
    _ "$port" "$log" "$pgid_file"
  echo "$port" > "$port_file"
  for _ in $(seq 1 60); do
    if curl -sf "http://localhost:$port/" >/dev/null 2>&1; then
      echo "READY http://localhost:$port · checklist $checklist"
      return 0
    fi
    # The launcher died: stop waiting on a port nothing will ever answer.
    kill -0 -- "-$(cat "$pgid_file")" 2>/dev/null || break
    sleep 1
  done
  echo "dev:mock did not come up on port $port; log tail:" >&2
  tail -40 "$log" >&2
  stop_server
  exit 1
}

# Rewrite line 1 from the marks. Whatever the walker wrote before the first
# " · " is kept as the title; everything after it is derived, so the counts can
# never disagree with the lines. A screen listed in two flows is two lines and
# two walks, so it counts twice; the once-per-app block is not screens.
recount() {
  local tmp
  tmp=$(mktemp)
  awk '
    NR == 1 { head = $0; sub(/ · .*$/, "", head); next }
    /^flow "/ { flows++ }
    /^once per app$/ { once = 1 }
    /^- / {
      if (!once) screens++
      if ($0 ~ /^- \[x\] .*FIXED/) fixed++
      else if ($0 ~ /^- \[x\]/) pass++
      else if ($0 ~ /^- \[ \]/) open++
      else if ($0 ~ /^- \[~\]/) outside++
      else towalk++
    }
    { body[NR] = $0 }
    END {
      line = head " · " screens + 0 " screens, " flows + 0 " flows · " \
             pass + 0 " pass, " fixed + 0 " fixed, " open + 0 " open, " outside + 0 " outside"
      if (towalk) line = line ", " towalk " to walk"
      print line
      for (i = 2; i <= NR; i++) print body[i]
    }' "$checklist" > "$tmp"
  mv "$tmp" "$checklist"
}

post() {
  local issue=${1:-}
  if [ ! -f "$checklist" ]; then
    echo "no checklist at $checklist — write it first" >&2
    exit 1
  fi
  recount
  if [ -z "$issue" ]; then
    echo "recounted $checklist (no issue given, not published)"
    head -1 "$checklist"
    return 0
  fi
  if [ -f "$comment_file" ]; then
    gh api -X PATCH "repos/{owner}/{repo}/issues/comments/$(cat "$comment_file")" \
      -F "body=@$checklist" > /dev/null
  else
    local url id
    url=$(gh issue comment "$issue" --body-file "$checklist")
    id=${url##*issuecomment-}
    if [[ $id =~ ^[0-9]+$ ]]; then
      echo "$id" > "$comment_file"
    else
      echo "could not read the comment id from '$url'; the next post will create a new comment" >&2
    fi
  fi
  echo "published to #$issue: $(head -1 "$checklist")"
}

down() {
  stop_server
  if command -v agent-browser > /dev/null 2>&1; then
    agent-browser close --all > /dev/null 2>&1 || true
  fi
  local port
  port=$(cat "$port_file" 2>/dev/null || true)
  if [ -n "$port" ]; then
    for _ in $(seq 1 20); do
      listening "$port" || break
      sleep 0.25
    done
    if listening "$port"; then
      echo "STILL UP on port $port" >&2
      exit 1
    fi
  fi
  rm -f "$port_file" "$log"
  echo STOPPED
}

case "${1:-}" in
  up | restart) start_server ;;
  post) post "${2:-}" ;;
  down) down ;;
  *)
    sed -n '18,27p' "$0" >&2
    exit 2
    ;;
esac
