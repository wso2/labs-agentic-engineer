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
# The loop it was written for: change the `aep-validation` skill, rebuild and
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

# A project that exists at the time of writing, for a bare invocation; all three
# are positional. Project names rot as local stacks are rebuilt — pass your own.
PROJECT="${1:-p47-hello-world}"
TAG="${2:-v1}"
ATTEMPTS="${3:-1}"

BFF_URL="${BFF_URL:-http://localhost:9090}"
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SEEDER_CLIENT_ID="${SEEDER_CLIENT_ID:-aep-local-dev-seeder}"

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

# The local plane keeps CLUSTER_CONTEXT in one place; source it rather than
# restating "k3d-openchoreo" here. env.sh is pure assignments, no side effects —
# but it assigns UNCONDITIONALLY, so an exported override has to be carried
# across the source by hand or it is silently replaced by the default.
CLUSTER_CONTEXT_OVERRIDE="${CLUSTER_CONTEXT:-}"
if [ -f "${SCRIPT_DIR}/../deployments/scripts/env.sh" ]; then
    # shellcheck source=/dev/null
    . "${SCRIPT_DIR}/../deployments/scripts/env.sh"
fi
CLUSTER_CONTEXT="${CLUSTER_CONTEXT_OVERRIDE:-${CLUSTER_CONTEXT:-k3d-openchoreo}}"
# Not in env.sh — this one is restated from setup-local.sh, which creates it.
AEP_NS="${AEP_NS:-wso2-aep}"

# Read a key out of a cluster Secret; empty when kubectl, the cluster or the key
# is missing. Same read as deployments/scripts/setup-local.sh's existing_secret,
# because that script is what wrote the value.
cluster_secret() {
    kubectl --context "${CLUSTER_CONTEXT}" get secret "$1" \
        -n "${AEP_NS}" -o jsonpath="{.data.$2}" 2>/dev/null \
        | base64 -d 2>/dev/null || true
}

# TWO paths register this client and they disagree about its secret, so the
# secret is resolved rather than assumed: setup-local.sh generates a random one
# into the aep-thunder-secrets Secret and registers the app with it, while
# Thunder's own bootstrap (single-cluster/values-thunder.yaml) registers the
# literal below. Whichever ran last is the one Thunder will accept — hence the
# cluster read first, and the literal only as the fallback that path needs.
SECRET_SOURCE="the exported SEEDER_CLIENT_SECRET"
if [ -z "${SEEDER_CLIENT_SECRET:-}" ]; then
    SEEDER_CLIENT_SECRET="$(cluster_secret aep-thunder-secrets LOCAL_DEV_SEEDER_SECRET)"
    SECRET_SOURCE="the aep-thunder-secrets Secret (${CLUSTER_CONTEXT}/${AEP_NS})"
fi
if [ -z "$SEEDER_CLIENT_SECRET" ]; then
    SEEDER_CLIENT_SECRET="aep-local-dev-seeder-secret"
    SECRET_SOURCE="the values-thunder.yaml bootstrap default"
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
    echo "❌ Thunder did not return an access_token for '${SEEDER_CLIENT_ID}'." >&2
    echo "   Tried the secret from ${SECRET_SOURCE}." >&2
    echo "   Two paths register this client and they disagree on its secret:" >&2
    echo "     deployments/scripts/setup-local.sh  — a random one, in the Secret" >&2
    echo "     single-cluster/values-thunder.yaml  — the literal default" >&2
    echo "   Whichever ran last is the one Thunder honours, so read the live one" >&2
    echo "   and pass it explicitly:" >&2
    echo "     SEEDER_CLIENT_SECRET=\$(kubectl --context ${CLUSTER_CONTEXT} get secret \\" >&2
    echo "       aep-thunder-secrets -n ${AEP_NS} \\" >&2
    echo "       -o jsonpath='{.data.LOCAL_DEV_SEEDER_SECRET}' | base64 -d) \\" >&2
    echo "       bash scripts/project-revalidate.sh ${PROJECT} ${TAG}" >&2
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
