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

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

# Fail here, not fifteen minutes in: every chart below installs with whichever
# `helm` PATH finds first, and two of the later scripts need Helm 4 flags.
require_helm_v4

echo "============================================"
echo "  AEP Platform — Full Setup"
echo "============================================"
echo ""
echo "This script sets up everything needed to run AEP:"
echo "  1. k3d cluster"
echo "  2. Prerequisites (cert-manager, Kgateway, ESO, OpenBao)"
echo "  3. OpenChoreo (Control Plane, Data Plane, Workflow Plane) + the"
echo "     platform IdP (ThunderID), shared with Agent Manager"
echo "  4. Observability Plane (Observer + OpenSearch + Fluent Bit +"
echo "     logs-adapter + AI RCA agent — in-UI Live Progress streaming,"
echo "     plus the alert → AI-RCA → coding-agent handoff pipeline:"
echo "     docs/developer-guide/sre-handoff-runbook.md). Agent Manager's"
echo "     console reads its logs, traces and metrics from it."
echo "  5. Temporal workflow engine (drives the devflow workflows; aep-api"
echo "     runs the worker in-process)"
echo "  6. AEP-specific config (build ClusterWorkflows, ComponentTypes,"
echo "     Environment, AuthzRoleBindings, .env file)"
if [ "${ENABLE_AGENT_MANAGER:-1}" = "1" ]; then
    echo "  7. Agent Management Platform, on this same cluster from WSO2's"
    echo "     published charts. Reversible: scripts/teardown-agent-manager.sh"
else
    echo "  7. Agent Management Platform skipped (ENABLE_AGENT_MANAGER=0)"
fi
if [ "${PARK_OBSERVABILITY_AFTER_SETUP:-}" = "" ]; then
    if [ "${ENABLE_AGENT_MANAGER:-1}" = "1" ]; then
        PARK_OBSERVABILITY_AFTER_SETUP=1
    else
        PARK_OBSERVABILITY_AFTER_SETUP=0
    fi
fi
if [ "$PARK_OBSERVABILITY_AFTER_SETUP" = "1" ]; then
    echo "  8. Park the observability plane's heavy workloads — OpenSearch,"
    echo "     Prometheus, Alertmanager, the RCA agent, Fluent Bit, the collector"
    echo "     and the adapters go to zero replicas (installed, idle, ~2 GB of"
    echo "     requests saved). Turn them on any time, no reinstall:"
    echo "     bash scripts/park-observability.sh up   (down parks again)"
else
    echo "  8. Keep the observability plane running. This is the default when"
    echo "     ENABLE_AGENT_MANAGER=0 so the alert → RCA → coding-agent handoff"
    echo "     works immediately after setup."
fi
echo ""

# The runner image (Debian + Go + a baked chromium, multi-GB) has no
# cluster dependency — only its `k3d image import` does. Building it in the
# background from step 1 overlaps it with the prerequisites / OpenChoreo /
# Temporal installs, which take longer than the build, so it costs nothing on
# the critical path instead of adding minutes at the tail of setup-aep.sh.
# setup-aep.sh keeps ownership of the import (SKIP_IMPORT=1 here), so the 4 GB
# node import still happens exactly once. PREBUILD_RUNNER=0 restores the serial
# build inside setup-aep.sh.
RUNNER_BUILD_LOG="${TMPDIR:-/tmp}/aep-runner-build.log"
RUNNER_BUILD_PID=""
if [ "${PREBUILD_RUNNER:-1}" = "1" ]; then
    echo "🐳 Pre-building the runner image in the background → $RUNNER_BUILD_LOG"
    SKIP_IMPORT=1 bash "$SCRIPT_DIR/build-runner.sh" > "$RUNNER_BUILD_LOG" 2>&1 &
    RUNNER_BUILD_PID=$!
    echo ""
fi

bash "$SCRIPT_DIR/setup-k3d.sh"
echo ""

bash "$SCRIPT_DIR/setup-prerequisites.sh"
echo ""

bash "$SCRIPT_DIR/setup-openchoreo.sh"
echo ""

# The observability plane is part of the base install: Agent Manager's console
# reads its logs, traces and metrics from it, and its charts install against it.
# Its heavy half is parked at the end of this script, so installing it costs
# disk and a few minutes of setup, not running memory.
bash "$SCRIPT_DIR/setup-observability.sh"
echo ""

bash "$SCRIPT_DIR/setup-temporal.sh"
echo ""

# Join the background prebuild before setup-aep.sh reaches its own
# build-runner.sh call — otherwise both would build the same tag concurrently.
# Non-fatal (mirrors setup-aep.sh): a build hiccup must not block platform
# setup, it only leaves coding + validation dispatch disabled.
if [ -n "$RUNNER_BUILD_PID" ]; then
    echo "⏳ Waiting for the background runner-image build..."
    if wait "$RUNNER_BUILD_PID"; then
        echo "✅ runner image pre-built (setup-aep.sh imports it into the node next)"
        # The prebuild just produced a fresh image, so setup-aep.sh's own
        # build-runner.sh call must not build it a SECOND time. It normally
        # doesn't — the build is skipped when the tag exists — but FORCE=1 is
        # exactly what you pass after changing the Dockerfile, and it is
        # inherited, so `FORCE=1 bash setup.sh` otherwise pays for the
        # multi-minute build twice and imports the second one. Cleared only on
        # success: a failed prebuild must leave FORCE alone so the serial build
        # still gets its chance.
        export FORCE=0
    else
        echo "⚠️  background runner-image build failed — see $RUNNER_BUILD_LOG"
        tail -5 "$RUNNER_BUILD_LOG" 2>/dev/null || true
    fi
    echo ""
fi

bash "$SCRIPT_DIR/setup-aep.sh"
echo ""

# Agent Manager is part of the default base install, after the observability
# plane it installs against. Set ENABLE_AGENT_MANAGER=0 for an AEP-only stack
# like the SRE integration setup.
if [ "${ENABLE_AGENT_MANAGER:-1}" = "1" ]; then
    # The second half — the default environment's own Thunder and its API
    # Platform gateway — is a separate script because it drives Agent Manager's
    # admin API over its public URL, and fails for reasons unrelated to the
    # chart installs.
    bash "$SCRIPT_DIR/setup-agent-manager.sh"
    echo ""
    bash "$SCRIPT_DIR/setup-agent-manager-env.sh"
    echo ""
else
    echo "⏭️  Skipping Agent Manager install (ENABLE_AGENT_MANAGER=0)"
    echo ""
fi

# Park the observability plane's heavy workloads only when the operator asks for
# the memory-saving profile. Running them costs about 2 GB of requests on an
# 8 GB VM, but parking OpenSearch and the SRE agent also turns off the
# alert→RCA→coding-agent path. Default to parking for the full Agent Manager
# stack, where traces/metrics are optional during ordinary local work, but keep
# the plane running for AEP-only/SRE setups (ENABLE_AGENT_MANAGER=0). Override
# either default explicitly with PARK_OBSERVABILITY_AFTER_SETUP=0|1.
if [ "${PARK_OBSERVABILITY_AFTER_SETUP:-1}" = "1" ]; then
    bash "$SCRIPT_DIR/park-observability.sh" down
else
    bash "$SCRIPT_DIR/park-observability.sh" up
fi
echo ""

echo "============================================"
echo "  ✅ Setup Complete!"
echo "============================================"
echo ""
echo "  Run the AEP services with EITHER local-dev flow:"
echo ""
echo "  A) Docker Compose (default, host containers):"
echo "       bash deployments/scripts/start.sh   (stop: scripts/stop.sh)"
echo "       Console: http://localhost:8090  (admin / admin)"
echo ""
echo "  B) Skaffold + k3d (in-cluster):"
echo "       make setup-local"
echo "       make dev-cluster"
echo "       Console: http://console.openchoreo.localhost:8080"
echo ""
echo "  Coding-agent: OpenChoreo Job Component in the project dataplane"
echo "                (image from AGENT_RUNNER_IMAGE / aep-runner:dev)."
echo ""
echo "  Agent Manager console: http://console.amp.localhost:8080"
echo "  Agent Manager API:     http://api.amp.localhost:8080"
echo "  Same login as the AEP console — one platform IdP serves both."
echo ""
if [ "${PARK_OBSERVABILITY_AFTER_SETUP:-1}" = "1" ]; then
    echo "  Observability plane:   installed, heavy workloads PARKED (no traces,"
    echo "                         metrics, log archive or alert→RCA until"
    echo "                         bash scripts/park-observability.sh up)"
else
    echo "  Observability plane:   installed and RUNNING (OpenSearch, logs-adapter,"
    echo "                         Fluent Bit, and SRE agent active for alerts/RCA)"
fi
echo ""
