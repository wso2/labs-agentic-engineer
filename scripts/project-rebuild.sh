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

# Rebuild one component of a platform-created project from what is on its
# default branch — the local-dev trigger for
# POST /projects/{project}/components/{component}/builds.
#
# The loop it was written for: clone the repo the platform created for a project,
# change code by hand, push to the default branch, run this. The push alone does
# nothing — components are created AutoBuild=false, and while the platform
# subscribes to GitHub's `push` event no handler is registered for it, so the
# delivery is logged and dropped. This call is what makes a hand-edit real.
#
#   scripts/project-rebuild.sh my-project my-component
#   NO_LOGS=1 scripts/project-rebuild.sh my-project my-component   # trigger + verdict only
#
# Both arguments are required on purpose. A build costs a pod and a push to the
# registry, and a mistyped component silently rebuilds the wrong thing — so
# neither gets a default, unlike project-revalidate.sh's read-only trigger.
#
# THE BUILD IS NOT PINNED TO A COMMIT. Only the merged-PR fan-out pins a SHA;
# this clones the head of the repo's default branch at the moment the build pod
# runs. Push first, then run this.
#
# There is no deploy call to make afterwards, and none exists: components are
# created AutoDeploy=true, so OpenChoreo drives Workload → ComponentRelease →
# ReleaseBinding off a green build. The last section only watches that happen.
#
# Requires jq — the build log is a JSON array, which sed cannot walk honestly.
set -euo pipefail

usage() {
    echo "Usage: $(basename "$0") <project> <component>" >&2
    echo "  Builds <component> from the head of the project repo's default branch," >&2
    echo "  tails the build log, then reports the deploy AutoDeploy performs." >&2
}

PROJECT="${1:-}"
COMPONENT="${2:-}"
if [ -z "$PROJECT" ] || [ -z "$COMPONENT" ]; then
    usage
    exit 1
fi

BFF_URL="${BFF_URL:-http://localhost:9090}"
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SEEDER_CLIENT_ID="${SEEDER_CLIENT_ID:-aep-local-dev-seeder}"
SEEDER_CLIENT_SECRET="${SEEDER_CLIENT_SECRET:-aep-local-dev-seeder-secret}"
# Two separate budgets because they bound different things: a cold image build
# is minutes, while AutoDeploy reacting to a posted Workload is seconds.
BUILD_TIMEOUT="${BUILD_TIMEOUT:-900}"
DEPLOY_TIMEOUT="${DEPLOY_TIMEOUT:-300}"
POLL_INTERVAL="${POLL_INTERVAL:-3}"
# How often the status wait reports that nothing has changed. It exists because
# a build can sit in one state for minutes and silence is indistinguishable from
# a hang — see the wait loop below.
HEARTBEAT="${HEARTBEAT:-30}"

# All four feed `$(( ))` below, where a non-numeric value would fail as an
# arithmetic syntax error naming nothing. Refuse it here, where the message can
# name the variable.
for var in BUILD_TIMEOUT DEPLOY_TIMEOUT POLL_INTERVAL HEARTBEAT; do
    if ! [[ "${!var}" =~ ^[1-9][0-9]*$ ]]; then
        echo "❌ ${var} must be a positive integer (got '${!var}')." >&2
        exit 1
    fi
done

if ! command -v jq > /dev/null 2>&1; then
    echo "❌ jq is required (the build log is a JSON array)." >&2
    echo "   macOS: brew install jq" >&2
    exit 1
fi

if ! curl -fsS --max-time 3 "$BFF_URL/healthz" > /dev/null 2>&1; then
    echo "❌ BFF not reachable at $BFF_URL"
    echo "   Bring the compose stack up first: cd deployments && bash scripts/start.sh"
    exit 1
fi

TOKEN=$(curl -sS -X POST "${THUNDER_URL%/}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=${SEEDER_CLIENT_ID}" \
    -d "client_secret=${SEEDER_CLIENT_SECRET}" 2> /dev/null \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
    echo "❌ Thunder did not return an access_token for '${SEEDER_CLIENT_ID}'."
    echo "   The client is registered by deployments/scripts/setup-local.sh / start.sh."
    exit 1
fi

API="${BFF_URL}/api/v1/projects/${PROJECT}/components/${COMPONENT}"

# --- Baseline the deploy rows ------------------------------------------------
#
# Read BEFORE the build, because AutoDeploy ROLLS THE EXISTING ReleaseBinding
# rather than creating a new one — so "a deploy happened" is only visible as a
# change against what was there beforehand, never as a row appearing.
deployments_json() {
    curl -sS -H "Authorization: Bearer ${TOKEN}" "${API}/deployments" 2> /dev/null || true
}

# Sorted keys so the comparison is over content, not field order.
deploy_fingerprint() {
    printf '%s' "$1" | jq -rSc '[.items[]? | {environment, releaseName, status}]' 2> /dev/null || printf ''
}

BEFORE_JSON="$(deployments_json)"
BEFORE="$(deploy_fingerprint "$BEFORE_JSON")"

# --- Trigger -----------------------------------------------------------------

echo "🔨 Building ${PROJECT}/${COMPONENT} from the default branch head"

# Body and status separately, so a refusal prints its reason instead of being
# swallowed by a non-2xx exit.
RESP=$(curl -sS -X POST "${API}/builds" \
    -H "Authorization: Bearer ${TOKEN}" \
    -w '\n%{http_code}' 2>&1)
CODE="${RESP##*$'\n'}"
JSON="${RESP%$'\n'*}"

if [ "$CODE" != "201" ]; then
    echo "❌ HTTP ${CODE}"
    echo "   ${JSON}"
    # The refusal a hand-edit earns most often, and the one whose message does
    # not explain itself: a component the platform has never built has no
    # OpenChoreo Component CR, and only the merged-PR fan-out creates one.
    case "$JSON" in
        *"not found"* | *"Not Found"*)
            echo "   If this component has never been built, it has no OpenChoreo Component CR yet —"
            echo "   only a merged pull request's fan-out provisions one."
            ;;
    esac
    exit 1
fi

RUN=$(printf '%s' "$JSON" | jq -r '.name // empty')
if [ -z "$RUN" ]; then
    echo "❌ Build accepted but no run name came back: ${JSON}"
    exit 1
fi
echo "✅ Started build ${RUN}"
echo

# --- Tail the build log ------------------------------------------------------
#
# `complete` is the endpoint's own "this build is terminal AND this response
# carries everything there will ever be", so it — not a status poll — is what
# ends the tail. Observability is optional in a local plane: a 503 means the log
# service is not wired, which is no reason to stop watching the build.
CURSOR=0
if [ "${NO_LOGS:-0}" = "1" ]; then
    echo "⏭  Skipping the log tail (NO_LOGS=1)."
else
    LOG_DEADLINE=$((SECONDS + BUILD_TIMEOUT))
    while true; do
        if [ "$SECONDS" -ge "$LOG_DEADLINE" ]; then
            echo "⏱  Build log still open after ${BUILD_TIMEOUT}s — giving up on the tail."
            break
        fi
        PAGE=$(curl -sS -H "Authorization: Bearer ${TOKEN}" \
            "${API}/builds/${RUN}/logs?since=${CURSOR}" -w '\n%{http_code}' 2>&1)
        PCODE="${PAGE##*$'\n'}"
        PJSON="${PAGE%$'\n'*}"
        if [ "$PCODE" != "200" ]; then
            echo "⚠️  Build logs unavailable (HTTP ${PCODE}) — falling back to the status poll."
            break
        fi
        printf '%s' "$PJSON" | jq -r '.logs[]? | .log' || true
        # An empty page carries no cursor; the previous one then stands.
        NEXT=$(printf '%s' "$PJSON" | jq -r '.nextCursor // 0')
        if [ -n "$NEXT" ] && [ "$NEXT" != "0" ] && [ "$NEXT" != "null" ]; then
            CURSOR="$NEXT"
        fi
        if [ "$(printf '%s' "$PJSON" | jq -r '.complete')" = "true" ]; then
            break
        fi
        sleep "$POLL_INTERVAL"
    done
fi

# --- The verdict -------------------------------------------------------------
#
# The log ending is not the verdict; the run's terminal condition Reason is. It
# is read off the builds listing, which the BFF caps at 20 runs with no ordering
# guarantee — so a run that is not in the page yet counts as still pending rather
# than as a failure.
build_row() {
    curl -sS -H "Authorization: Bearer ${TOKEN}" "${API}/builds" 2> /dev/null \
        | jq -r --arg run "$RUN" '.items[]? | select(.name == $run) | "\(.status)\t\(.completed)"' \
        || true
}

mmss() { printf '%02d:%02d' $(($1 / 60)) $(($1 % 60)); }

# This wait can run for minutes with nothing to print, and when the log tail is
# unavailable it is the ONLY thing on screen — so it reports every status change
# and heartbeats in between, which is what makes a stall look like a stall.
STATUS=""
COMPLETED="false"
LAST_STATUS=""
HINTED=0
WAIT_FROM=$SECONDS
LAST_BEAT=$SECONDS
STATUS_DEADLINE=$((SECONDS + BUILD_TIMEOUT))
echo "⏳ Waiting for ${RUN} to finish…"
while [ "$SECONDS" -lt "$STATUS_DEADLINE" ]; do
    ROW="$(build_row)"
    if [ -n "$ROW" ]; then
        STATUS="${ROW%%$'\t'*}"
        COMPLETED="${ROW##*$'\t'}"
        ELAPSED=$((SECONDS - WAIT_FROM))
        if [ "$STATUS" != "$LAST_STATUS" ]; then
            echo "   [$(mmss "$ELAPSED")] ${STATUS}"
            LAST_STATUS="$STATUS"
            LAST_BEAT=$SECONDS
        elif [ $((SECONDS - LAST_BEAT)) -ge "$HEARTBEAT" ]; then
            echo "   [$(mmss "$ELAPSED")] still ${STATUS}…"
            LAST_BEAT=$SECONDS
        fi
        # A build that has not left Pending is not slow, it is blocked — most
        # often a step image that will not pull. Say where to look, once.
        if [ "$HINTED" = "0" ] && [ "$STATUS" = "WorkflowPending" ] && [ "$ELAPSED" -ge 120 ]; then
            echo "   ↳ pending for 2m — the step pods say why:"
            echo "     kubectl get pods -A | grep ${RUN}"
            HINTED=1
        fi
        if [ "$COMPLETED" = "true" ]; then
            break
        fi
    fi
    sleep "$POLL_INTERVAL"
done

echo
if [ "$COMPLETED" != "true" ]; then
    echo "⏱  Build ${RUN} reported no terminal state within ${BUILD_TIMEOUT}s (last status: ${STATUS:-unknown})."
    echo "   Re-check:  curl -sS -H \"Authorization: Bearer \$TOKEN\" ${API}/builds"
    exit 1
fi

if [ "$STATUS" != "WorkflowSucceeded" ]; then
    echo "❌ Build ${RUN} finished ${STATUS}. Nothing was deployed."
    exit 1
fi
echo "✅ Build ${RUN} succeeded."

# --- The deploy that follows -------------------------------------------------
#
# Nothing is triggered here. What is worth waiting for is EVIDENCE the green
# build reached the cluster, and the deploy rows changing is the only honest one
# available: `status` is the ReleaseBinding's LATEST CONDITION REASON — a display
# string whose array order OpenChoreo does not guarantee — so this waits for
# movement and then prints what the rows say rather than declaring the app ready.
echo
echo "⏳ Waiting for AutoDeploy to move the ReleaseBinding…"
AFTER_JSON="$BEFORE_JSON"
MOVED=0
DEPLOY_DEADLINE=$((SECONDS + DEPLOY_TIMEOUT))
while [ "$SECONDS" -lt "$DEPLOY_DEADLINE" ]; do
    NOW_JSON="$(deployments_json)"
    NOW_FP="$(deploy_fingerprint "$NOW_JSON")"
    # An unparseable read is a transient error, not a change — keep the last good
    # rows rather than reporting a deploy that did not happen.
    if [ -n "$NOW_FP" ]; then
        AFTER_JSON="$NOW_JSON"
        if [ "$NOW_FP" != "$BEFORE" ]; then
            MOVED=1
            break
        fi
    fi
    sleep "$POLL_INTERVAL"
done

if [ "$MOVED" = "1" ]; then
    echo "✅ The deploy rows changed."
else
    echo "⏱  The deploy rows did not change within ${DEPLOY_TIMEOUT}s."
    echo "   Either the binding was already at this release, or the rollout is still in flight."
fi

echo
echo "   Deployments:"
printf '%s' "$AFTER_JSON" | jq -r '
    if ((.items // []) | length) == 0 then
        "     (no ReleaseBinding for this component yet)"
    else
        .items[]
        | "     \(.environment // "?"): \(.status // "?")"
          + (if (.endpointUrl // "") != "" then "  → " + .endpointUrl else "" end)
    end'
