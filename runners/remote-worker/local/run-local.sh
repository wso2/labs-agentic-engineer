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

# Run the coding-agent runner as a local one-shot container, standing in
# for the platform dispatch flow (BFF -> Argo -> pod). A loopback token
# stub (token-stub.mjs) plays the git-service credentials/refresh
# endpoint; everything else is the same code path as a cluster run.
#
# Usage:
#   cp env.local.example .env.local    # then fill in
#   ./run-local.sh [path/to/env-file]
#
# The repo-root skills/ library is bind-mounted read-only over /app/skills, so
# skill edits are live without an image rebuild (mirrors the k3d dev flow). A
# dispatched run reads its skills from the clone's .claude/skills/ mirror, not
# from here — this mount is what the playground's local mode mirrors FROM.
# Exit code mirrors the runner: 0 success, 1 agent failure, 2 provisioning.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKER_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="${1:-$SCRIPT_DIR/.env.local}"

if [ ! -f "$ENV_FILE" ]; then
  echo "env file not found: $ENV_FILE (cp env.local.example .env.local and fill it in)" >&2
  exit 2
fi
set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

for v in ANTHROPIC_API_KEY GITHUB_PAT AEP_REPO_URL AEP_PROMPT; do
  if [ -z "${!v:-}" ]; then
    echo "missing required var in $ENV_FILE: $v" >&2
    exit 2
  fi
done

# node (required by the stub anyway) generates UUIDs — uuidgen is not
# universally present. The bearer defaults to a fresh random value per
# run; the stub requires it on every refresh call, so the PAT is never
# handed out unauthenticated even if STUB_BIND is widened.
export AEP_TASK_ID="${AEP_TASK_ID:-$(node -e 'console.log(crypto.randomUUID())')}"
export AEP_ORG_ID="${AEP_ORG_ID:-local-org}"
export AEP_PROJECT_ID="${AEP_PROJECT_ID:-local-project}"
export AEP_COMPONENT_NAME="${AEP_COMPONENT_NAME:-demo-component}"
export AEP_IDENTITY_NAME="${AEP_IDENTITY_NAME:-AEP Local Agent}"
export AEP_IDENTITY_EMAIL="${AEP_IDENTITY_EMAIL:-aep-local@users.noreply.github.com}"
export AEP_BEARER="${AEP_BEARER:-$(node -e 'console.log(crypto.randomUUID())')}"
export AEP_TASK_KIND="${AEP_TASK_KIND:-implementation}"
export PUBLISHER_CLIENT_ID="${PUBLISHER_CLIENT_ID:-local-publisher}"
export PUBLISHER_CLIENT_SECRET="${PUBLISHER_CLIENT_SECRET:-local-publisher-secret}"
STUB_PORT="${STUB_PORT:-8377}"
STUB_BIND="${STUB_BIND:-127.0.0.1}"
IMAGE_TAG="${IMAGE_TAG:-aep-remote-worker:local}"
# One image serves both task kinds. Override only to try an experimental
# recipe; pair it with a distinct IMAGE_TAG so the variants don't clobber
# each other.
DOCKERFILE="${DOCKERFILE:-$WORKER_DIR/Dockerfile}"
# The container reaches the host-side stub via host.docker.internal.
export AEP_GIT_SERVICE_URL="http://host.docker.internal:${STUB_PORT}"
export PUBLISHER_TOKEN_URL="http://host.docker.internal:${STUB_PORT}/oauth2/token"
# AEP_PLATFORM_URL: for an implementation run it stays unset (oneshot.ts skips
# the per-task skills pull; credhelper/gh fall back to AEP_GIT_SERVICE_URL). A
# validation run points it at the same stub so the aep-validation skill can
# fetch its validation-context; the stub answers that path too. The skills-pull
# to the stub 404s and is a harmless best-effort warning.
if [ "${AEP_TASK_KIND}" = "validation" ]; then
  export AEP_PLATFORM_URL="${AEP_PLATFORM_URL:-http://host.docker.internal:${STUB_PORT}}"
else
  export AEP_PLATFORM_URL="${AEP_PLATFORM_URL:-}"
fi

if ! docker info >/dev/null 2>&1; then
  echo "docker daemon not reachable — start it first (e.g. 'colima start')" >&2
  exit 2
fi

echo ">> starting token stub on ${STUB_BIND}:${STUB_PORT}"
GITHUB_PAT="$GITHUB_PAT" STUB_PORT="$STUB_PORT" STUB_BIND="$STUB_BIND" \
  STUB_BEARER="$AEP_BEARER" \
  STUB_CLIENT_ID="$PUBLISHER_CLIENT_ID" STUB_CLIENT_SECRET="$PUBLISHER_CLIENT_SECRET" \
  node "$SCRIPT_DIR/token-stub.mjs" &
STUB_PID=$!
trap 'kill "$STUB_PID" 2>/dev/null || true' EXIT

for _ in $(seq 1 20); do
  curl -sf "http://127.0.0.1:${STUB_PORT}/healthz" >/dev/null 2>&1 && break
  sleep 0.25
done
if ! curl -sf "http://127.0.0.1:${STUB_PORT}/healthz" >/dev/null 2>&1; then
  echo "token stub failed to start on port ${STUB_PORT}" >&2
  exit 2
fi

echo ">> building runner image ${IMAGE_TAG} (${DOCKERFILE##*/})"
# The Dockerfile COPYs from two BuildKit named contexts that live outside its
# build context — the skill library, and the `bal library` tool's source, which
# its first stage compiles. Every build path has to pass both or BuildKit fails
# to resolve them; the others are deployments/scripts/build-runner.sh and
# release.yml's matrix row.
#
# The tool stage resolves `org.ballerinalang:ballerina-cli` from ballerina-platform's
# GitHub Packages, so it needs a token with `read:packages` — as a secret mount,
# never a build arg. See ADR-0008.
export PACKAGE_PAT="${packagePAT:-${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}}"
if [ -z "$PACKAGE_PAT" ]; then
  echo "no token to read ballerina-platform's GitHub Packages." >&2
  echo "  gh auth refresh -h github.com -s read:packages   # then re-run" >&2
  echo "  export packagePAT=<PAT with read:packages>" >&2
  exit 2
fi
docker build \
  --build-context "skills=$WORKER_DIR/../../skills" \
  --build-context "bal-library-tool=$WORKER_DIR/../../packages/bal-library-tool" \
  --secret "id=packagePAT,env=PACKAGE_PAT" \
  -f "$DOCKERFILE" -t "$IMAGE_TAG" "$WORKER_DIR"

mkdir -p "$SCRIPT_DIR/workspace"

echo ">> dispatching task ${AEP_TASK_ID} on ${AEP_REPO_URL}"
EXIT_CODE=0
docker run --rm \
  --add-host=host.docker.internal:host-gateway \
  --shm-size=1g \
  -v "$SCRIPT_DIR/workspace:/home/aep/aep-workspace" \
  -v "$WORKER_DIR/../../skills:/app/skills:ro" \
  -e ANTHROPIC_API_KEY \
  -e AEP_TASK_ID -e AEP_ORG_ID -e AEP_PROJECT_ID -e AEP_COMPONENT_NAME \
  -e AEP_REPO_URL -e AEP_PROMPT -e AEP_BEARER -e AEP_GIT_SERVICE_URL \
  -e AEP_IDENTITY_NAME -e AEP_IDENTITY_EMAIL -e AEP_TASK_KIND -e AEP_PLATFORM_URL \
  -e PUBLISHER_CLIENT_ID -e PUBLISHER_CLIENT_SECRET -e PUBLISHER_TOKEN_URL \
  "$IMAGE_TAG" || EXIT_CODE=$?

WS="$SCRIPT_DIR/workspace/$AEP_ORG_ID/$AEP_PROJECT_ID/$AEP_TASK_ID"
case "$EXIT_CODE" in
  0) STATUS="success" ;;
  1) STATUS="agent reported failure" ;;
  2) STATUS="provisioning error (agent never ran)" ;;
  *) STATUS="unexpected exit" ;;
esac
echo ""
echo ">> runner finished: ${STATUS} (exit code ${EXIT_CODE})"
echo ">> workspace kept at: $WS"
echo ">> full SDK transcript: $WS/.logs/claude.log"
exit "$EXIT_CODE"
