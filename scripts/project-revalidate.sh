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

# Ask a deployed version's acceptance criteria again — the local-dev trigger for
# POST /projects/{project}/builds/{tag}/revalidate.
#
# It exists because the revalidation has no console button: the endpoint is the
# whole interface, and the only friction in calling it is getting a token. This
# is that curl with a user sign-in folded in: /api/v1 refuses client_credentials
# tokens, so it signs in as the local admin through the console's Thunder client
# (AEP_USER / AEP_PASSWORD override the user, CONSOLE_URL names the console the
# client was registered for; scripts/lib/thunder-user-login.sh).
#
# The loop it was written for: change the `validation-task` skill, rebuild and
# deploy aep-api — the BFF carries the skills mirror a dispatched runner reads,
# so `make build-runner` does NOT reach it — confirm the mirror landed, then run
# this and watch the agent work against the app already deployed. Nothing is
# rebuilt to answer the question: a validation run has no working set and skips
# the build and deploy stages outright.
#
#   scripts/project-revalidate.sh my-project            # tag v1, re-check only
#   scripts/project-revalidate.sh my-project v2
#   scripts/project-revalidate.sh my-project v2 3       # let it REPAIR — see below
#
# The third argument is the validation attempt budget, and since the delivery
# loop split into three workflows it caps the VERSION's total validation runs
# rather than this run's attempts: the workflow counts every `validation`-kind
# run on the milestone, the one it is running in included. The default 1 is the
# safe one — a fatal verdict settles the run before the repair mint, so the run
# reports and stops, touching neither the repo nor the deployment.
#
# Raising it is opt-in because it changes the project, and it has to clear the
# runs already spent: the reconcile sweep judges every version once by itself at
# deployed-green, so a version judged N times needs at least N+2 here before a
# `failed` verdict files an issue per failed criterion. The repair is then a
# SEPARATE run — this one settles `failed` the moment it files — and the task run
# that fixes those issues reopens the validation task, so a third run re-judges.
#
# Refusals are the endpoint's, and each is actionable: 409 while a run is already
# working the version or while its milestone still has open dev work, 422 when the
# version has no acceptance criteria to validate against, 404 when the tag never
# ran here — a tag resolves through the platform's own run rows, never GitHub.
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib/thunder-user-login.sh
. "${SCRIPT_DIR}/lib/thunder-user-login.sh"

# A project that exists at the time of writing, for a bare invocation; all three
# are positional. Project names rot as local stacks are rebuilt — pass your own.
PROJECT="${1:-p47-hello-world}"
TAG="${2:-v1}"
ATTEMPTS="${3:-1}"

BFF_URL="${BFF_URL:-http://localhost:9090}"
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"

# Both budgets are interpolated into hand-built JSON below, so a non-numeric or
# leading-zero value would ship a malformed body and come back as an opaque 400.
# Refuse it here, where the message can name the variable.
if ! [[ "$ATTEMPTS" =~ ^[1-9][0-9]*$ ]]; then
    echo "❌ attempts must be a positive integer (got '${ATTEMPTS}')." >&2
    exit 1
fi
if [ -n "${CEILING:-}" ] && ! [[ "$CEILING" =~ ^[1-9][0-9]*$ ]]; then
    echo "❌ CEILING must be a positive integer (got '${CEILING}')." >&2
    exit 1
fi

if ! curl -fsS --max-time 3 "$BFF_URL/healthz" > /dev/null 2>&1; then
    echo "❌ BFF not reachable at $BFF_URL"
    echo "   Bring the compose stack up first: cd deployments && bash scripts/start.sh"
    exit 1
fi

# The token lives in a 0600 header file curl reads with -H @file, so it never
# reaches an argv.
AUTH_HEADER="$(mktemp)"
trap 'rm -f "$AUTH_HEADER"' EXIT
if ! thunder_user_auth_header "$AUTH_HEADER"; then
    exit 1
fi

BODY="{\"validationAttempts\":${ATTEMPTS}}"
[ -n "${CEILING:-}" ] && BODY="{\"validationAttempts\":${ATTEMPTS},\"cycleCeiling\":${CEILING}}"

echo "🔁 Revalidating ${PROJECT} ${TAG} (validationAttempts=${ATTEMPTS})"

# Body and status separately, so a refusal prints its reason instead of being
# swallowed by a non-2xx exit.
RESP=$(curl -sS -X POST "${BFF_URL}/api/v1/projects/${PROJECT}/builds/${TAG}/revalidate" \
    -H "@${AUTH_HEADER}" \
    -H "Content-Type: application/json" \
    -d "$BODY" -w '\n%{http_code}' 2>&1)
CODE="${RESP##*$'\n'}"
JSON="${RESP%$'\n'*}"

if [ "$CODE" != "202" ]; then
    echo "❌ HTTP ${CODE}"
    echo "   ${JSON}"
    exit 1
fi

RUN_ID=$(printf '%s' "$JSON" | sed -n 's/.*"runId"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
echo "✅ Started run ${RUN_ID}"
echo
echo "   Watch it:  Validation page for ${PROJECT}, or"
echo "              docker exec aep-db psql -U aep -d aep -c \\"
echo "                \"select kind, validation_verdict, ended_at is null as open\\"
echo "                   from run_cycles where run_id='${RUN_ID}';\""
