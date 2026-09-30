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

# Wires the OpenChoreo SRE agent's alert → RCA → AE issue handoff onto a
# cluster built by setup-env-for-aectl.sh and provisioned by
# `aectl platform install`. make dev-env runs it unless WITH_SRE=0 or
# WITH_OBSERVABILITY=0.
#
#   bash deployments/scripts/setup-sre.sh
#
# The in-cluster pieces are all aectl's and the platform chart's; this script
# only runs them in the order the handoff needs. See
# docs/developer-guide/sre-handoff-runbook.md.
#
#   1. the observability-alert-rule ClusterTrait the chart's ComponentTypes
#      allow but `aectl platform install` does not apply (deployments/README.md).
#   2. `aectl sre install --org`, which enables the SRE agent on the
#      observability plane setup-env-for-aectl.sh installed (at that plane's
#      chart version, with the stock ghcr.io/openchoreo/sre-agent image) and
#      mounts the AE remediation extension. aep-api's own reconciler pushes
#      the org's LLM key/model and handoff token into the agent's Secret.
#
# The SRE agent has no key of its own: until the org's model connection is saved
# in the Console, its pod waits for aep-api to push one. Save the key, then
# re-run this script (only step 2 then changes anything).
#
# Optionally, set SRE_LLM_API_KEY_FILE (path to a file holding the key) and
# SRE_LLM_MODEL (and SRE_LLM_BASE_URL) to have `aectl sre install` seed an
# install-time SRE model connection instead of waiting on a Console/API save
# — see "Seed the SRE model at install" in
# docs/developer-guide/sre-handoff-runbook.md.
#
# Every step is idempotent.

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

CLUSTER_NAME="${CLUSTER_NAME:-openchoreo}"
CLUSTER_CONTEXT="${CLUSTER_CONTEXT:-k3d-${CLUSTER_NAME}}"
AEP_NS="${AEP_NS:-wso2-aep}"
OBS_NS="${OBS_NS:-openchoreo-observability-plane}"
AECTL="${AECTL:-$REPO_ROOT/tools/aectl/aectl-skaffold}"
ALERT_RULE_TRAIT="$REPO_ROOT/deployments/manifests/api-platform/observability-alert-rule-trait.yaml"
# `aectl sre install` uses this to pin the platform chart when it flips
# sreAgent.* on the platform release (via its own internal `aectl platform
# update` call) — the same local chart `make dev-env` installed the release
# from, so that step never drifts it to an unpinned GHCR "latest".
PLATFORM_CHART="${PLATFORM_CHART:-$REPO_ROOT/deployments/helm-charts/platform}"

kubectl() { command kubectl --context "$CLUSTER_CONTEXT" "$@"; }
fail() { echo "❌ $1" >&2; [ $# -gt 1 ] && echo "   $2" >&2; exit 1; }

echo "============================================"
echo "  SRE agent → AE handoff"
echo "============================================"

# ── Preflight ───────────────────────────────────────────────────────────────
# aectl talks to the current kubeconfig context, not CLUSTER_CONTEXT, so the
# two must agree or the script's checks and aectl's writes hit different
# clusters.
[ "$(command kubectl config current-context 2>/dev/null)" = "$CLUSTER_CONTEXT" ] \
    || fail "current kubectl context is not $CLUSTER_CONTEXT" \
            "kubectl config use-context $CLUSTER_CONTEXT"
[ -x "$AECTL" ] || fail "$AECTL not found" "make dev-env builds it (cd tools/aectl && go build -o aectl-skaffold .)"
kubectl get deployment aep-api -n "$AEP_NS" &>/dev/null \
    || fail "aep-api is not installed in $AEP_NS" "run aectl platform install first (make dev-env)"
kubectl get deployment observer -n "$OBS_NS" &>/dev/null \
    || fail "no observability plane in $OBS_NS" "setup-env-for-aectl.sh installs it unless WITH_OBSERVABILITY=0"

# ── 1. Alert-rule trait ────────────────────────────────────────────────────
echo "📐 Applying the observability-alert-rule ClusterTrait"
kubectl apply -f "$ALERT_RULE_TRAIT"

# ── 2. SRE agent on the observability plane ─────────────────────────────────
echo "🤖 Running aectl sre install"
SRE_INSTALL_ARGS=(--namespace "$AEP_NS" --obs-namespace "$OBS_NS" --assets-root "$REPO_ROOT" \
    --org "${AEP_ORG:-default}" --platform-chart "$PLATFORM_CHART")
# Install-time SRE model seed: only when both the key file and model are set
# (aectl itself requires the pair together; leaving either unset here means
# no seed, same as not passing the flags at all).
if [ -n "${SRE_LLM_API_KEY_FILE:-}" ] && [ -n "${SRE_LLM_MODEL:-}" ]; then
    SRE_INSTALL_ARGS+=(--llm-api-key-file "$SRE_LLM_API_KEY_FILE" --llm-model "$SRE_LLM_MODEL")
    [ -n "${SRE_LLM_BASE_URL:-}" ] && SRE_INSTALL_ARGS+=(--llm-base-url "$SRE_LLM_BASE_URL")
fi
"$AECTL" sre install "${SRE_INSTALL_ARGS[@]}"

echo ""
echo "✅ SRE handoff wired. Runbook: docs/developer-guide/sre-handoff-runbook.md"
