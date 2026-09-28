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

# Pre-warms OpenCode's home at IMAGE BUILD time, as the user a pod runs as, so a
# pod starts without reaching npm, GitHub or models.dev (the runner-opencode
# stage of the Dockerfile; ADR-0015).
#
# On its first start with an empty home OpenCode fetches models.dev into
# ~/.cache/opencode/models.json, downloads ripgrep to ~/.cache/opencode/bin/rg,
# and npm-installs @opencode-ai/plugin into ~/.config/opencode/. Measured: a
# bare `opencode serve` does only the FIRST of the three (spike S5); the plugin
# dependency installs when a PROJECT INSTANCE boots — the first request naming a
# directory — and ripgrep only when something first searches (below). So this
# boots one against a throwaway directory, asks it for its config and for a text
# search, waits until all three are on disk, and fails the
# build if they never are: a pod that has to fetch 63 MB of npm at start is a pod
# that fails on a bad day.
set -euo pipefail

cache="$HOME/.cache/opencode"
conf="$HOME/.config/opencode"
port=47990
dir="$(mktemp -d)"
git -C "$dir" init -q
log="$(mktemp)"

cd "$dir"
opencode serve --hostname=127.0.0.1 --port="$port" >"$log" 2>&1 &
pid=$!
# Bounded: the server does not exit on SIGTERM while its own npm install is
# running (measured — an unbounded `wait` hung the build), so TERM, give it ten
# seconds, then KILL.
cleanup() {
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -rf "$dir" "$log"
}
trap cleanup EXIT

query="directory=$(node -p 'encodeURIComponent(process.argv[1])' "$dir")"
base="http://127.0.0.1:$port"

up=0
for _ in $(seq 1 60); do
    if curl -fsS -m 20 "$base/config?$query" >/dev/null 2>&1; then up=1; break; fi
    sleep 1
done
if [ "$up" != 1 ]; then
    echo "opencode prewarm: the server never answered" >&2
    tail -50 "$log" >&2
    exit 1
fi
# Ripgrep is fetched LAZILY, the first time a ripgrep-backed feature runs (the
# binary looks for `rg` on PATH, then ~/.cache/opencode/bin/rg, then downloads
# it). A TEXT search is one; a file-name search (`/find/file`) is not — measured.
# The search request BLOCKS on that download, so every request is bounded and
# re-issued from the loop below: an unbounded one hung a build for ten minutes
# on a slow download, where a bounded retry just asks again.
search() { curl -fsS -m 30 "$base/find?pattern=prewarm&$query" >/dev/null 2>&1 || true; }
search

deadline=$((SECONDS + 240))
# The plugin dependency counts once npm has FINISHED: its directory appears
# mid-install and is moved again before the end, so the test is the package's
# own manifest plus the lockfile npm writes last.
installed() {
    [ -f "$conf/node_modules/@opencode-ai/plugin/package.json" ] && [ -f "$conf/package-lock.json" ]
}
until [ -s "$cache/models.json" ] && [ -x "$cache/bin/rg" ] && installed; do
    if [ "$SECONDS" -ge "$deadline" ]; then
        echo "opencode prewarm: incomplete after 240s" >&2
        ls -la "$cache" "$cache/bin" "$conf" "$conf/node_modules" 2>&1 | head -40 >&2 || true
        tail -50 "$log" >&2
        exit 1
    fi
    [ -x "$cache/bin/rg" ] || search
    sleep 2
done

cleanup
trap - EXIT
# The instance's own state about the throwaway directory (its sqlite, its
# snapshot store, its logs) is not the image's to carry; OpenCode recreates it.
rm -rf "$HOME/.local/share/opencode"
echo "opencode prewarm: models.json, rg and @opencode-ai/plugin are in place"
