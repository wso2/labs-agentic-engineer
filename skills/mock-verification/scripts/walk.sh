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

# walk.sh — the dev server of a mock-verification walk, so the walker runs none
# of its mechanics. Run from the App Path.
#
#   walk.sh up        start `npm run dev:mock` on a free port → READY <url>
#   walk.sh restart   stop the server and start it again (same browser)
#   walk.sh down      stop the server, close the browser, confirm the port let
#                     go → STOPPED
#
# State lives at /tmp/walk-<app>.* where <app> is the App Path with its slashes
# turned to underscores, so two apps with the same directory name under
# different parents keep separate servers.
#
# Why a script and not four lines in the skill: `npm run` is three processes and
# the runner image has no `ps`/`pkill`, so the process GROUP is the only handle
# that reaps them all — `set -m` makes the background job its own group leader,
# which is why `$!` below is a group id, on Linux and on a macOS laptop alike.
# Every walk used to re-derive that, and the one that did not leaked four dev
# servers into a 3Gi cgroup. The port is searched rather than fixed so two
# web-application walks in one wave do not fight over 5173 — and because two
# `up` calls can still pick the same free port in the same instant, a launch
# that dies on a bind collision moves to the next port by itself. A stale server
# from this app's earlier attempt is reaped before a new one starts.

set -euo pipefail

app=$(printf '%s' "$PWD" | tr '/' '_')
state="/tmp/walk-$app"
log="$state.log"
pgid_file="$state.pgid"
port_file="$state.port"

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

# Launch on one port and wait for it. Returns 0 when the url answers, 2 when the
# launcher died on a bind collision (the caller tries the next port), 1 otherwise.
launch_on() {
  local port=$1
  : > "$log"
  # `</dev/null`: under job control a server that reads the terminal would be
  # stopped with SIGTTIN; Vite reads stdin for its keyboard shortcuts.
  bash -c 'set -m; npm run dev:mock -- --port "$1" --strictPort </dev/null >"$2" 2>&1 & echo $! > "$3"' \
    _ "$port" "$log" "$pgid_file"
  echo "$port" > "$port_file"
  for _ in $(seq 1 60); do
    if curl -sf "http://localhost:$port/" >/dev/null 2>&1; then
      return 0
    fi
    # The launcher died: stop waiting on a port nothing will ever answer.
    if ! kill -0 -- "-$(cat "$pgid_file")" 2>/dev/null; then
      rm -f "$pgid_file"
      grep -qiE "EADDRINUSE|already in use" "$log" && return 2
      return 1
    fi
    sleep 1
  done
  return 1
}

start_server() {
  stop_server
  local port rc
  for port in $(seq 5173 5199); do
    listening "$port" && continue
    launch_on "$port" && { echo "READY http://localhost:$port"; return 0; }
    rc=$?
    [ "$rc" -eq 2 ] && continue
    echo "dev:mock did not come up on port $port; log tail:" >&2
    tail -40 "$log" >&2
    stop_server
    exit 1
  done
  echo "no free port between 5173 and 5199" >&2
  exit 1
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
  down) down ;;
  *)
    sed -n '18,24p' "$0" >&2
    exit 2
    ;;
esac
