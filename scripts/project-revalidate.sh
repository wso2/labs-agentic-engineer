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
# whole interface, and the only friction in calling it is minting a token. This
# is that curl with the Thunder client_credentials dance folded in.
#
# The loop it was written for: change the `aep-validation` skill, rebuild the
# runner image (`make build-runner FORCE=1`), re-run this, watch the agent work
# against the app already deployed. Nothing is rebuilt to answer the question.
#
#   scripts/project-revalidate.sh                       # the defaults below
#   scripts/project-revalidate.sh my-project v2
#   scripts/project-revalidate.sh my-project v2 2       # let it REPAIR — see below
#
# The third argument is the validation attempt budget. It defaults to 1, and that
# default is the safe one: a single attempt is spent by the first fatal verdict,
# which settles the run before the loop reaches the point where it would file
# repair work — so the run reports and stops, touching neither the repo nor the
# deployment. Raise it and a `failed` verdict becomes an issue per failed
# criterion, an ordinary coding cycle, a build and a redeploy. That is a real
# change to the project, so it is opt-in.
#
# Refusals are the endpoint's, and each is actionable: 409 while a run is already
# working the version or while its milestone still has open work, 422 when the
# version has no acceptance criteria to validate against.
set -e

# The project this repo's author reaches for most; all three are positional.
PROJECT="${1:-p18-bare-minimum-hello}"
TAG="${2:-v1}"
ATTEMPTS="${3:-1}"

BFF_URL="${BFF_URL:-http://localhost:9090}"
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SEEDER_CLIENT_ID="${SEEDER_CLIENT_ID:-aep-local-dev-seeder}"
SEEDER_CLIENT_SECRET="${SEEDER_CLIENT_SECRET:-aep-local-dev-seeder-secret}"

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

TOKEN=$(curl -sS -X POST "${THUNDER_URL%/}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=${SEEDER_CLIENT_ID}" \
    -d "client_secret=${SEEDER_CLIENT_SECRET}" 2>/dev/null \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
    echo "❌ Thunder did not return an access_token for '${SEEDER_CLIENT_ID}'."
    echo "   The client is registered by deployments/scripts/setup-local.sh / start.sh."
    exit 1
fi

BODY="{\"validationAttempts\":${ATTEMPTS}}"
[ -n "${CEILING:-}" ] && BODY="{\"validationAttempts\":${ATTEMPTS},\"cycleCeiling\":${CEILING}}"

echo "🔁 Revalidating ${PROJECT} ${TAG} (validationAttempts=${ATTEMPTS})"

# Body and status separately, so a refusal prints its reason instead of being
# swallowed by a non-2xx exit.
RESP=$(curl -sS -X POST "${BFF_URL}/api/v1/projects/${PROJECT}/builds/${TAG}/revalidate" \
    -H "Authorization: Bearer ${TOKEN}" \
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
