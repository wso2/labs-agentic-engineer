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

echo "============================================"
echo "  AEP Platform — Full Setup"
echo "============================================"
echo ""
echo "This script sets up everything needed to run AEP:"
echo "  1. k3d cluster"
echo "  2. Prerequisites (cert-manager, Kgateway, ESO, OpenBao)"
echo "  3. OpenChoreo (Control Plane, Data Plane, Workflow Plane, Thunder)"
echo "  4. Observability Plane (Observer + OpenSearch + Fluent Bit +"
echo "     logs-adapter + AI RCA agent — in-UI Live Progress streaming,"
echo "     plus the alert → AI-RCA → coding-agent handoff pipeline:"
echo "     docs/developer-guide/sre-handoff-runbook.md)"
echo "     Skipped by default (heaviest install: OpenSearch StatefulSet +"
echo "     Fluent Bit DaemonSet + RCA agent) — set ENABLE_OBSERVABILITY=1 to"
echo "     install it. Live Progress streaming and the alert→RCA pipeline are"
echo "     unavailable until scripts/setup-observability.sh is run."
echo "  5. Temporal workflow engine (drives the devflow workflows; aep-api"
echo "     runs the worker in-process)"
echo "  6. AEP-specific config (build ClusterWorkflows, ComponentTypes,"
echo "     Environment, AuthzRoleBindings, .env file)"
echo ""

# The runner image (Debian + Go + Playwright + baked chromium, multi-GB) has no
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

if [ "${ENABLE_OBSERVABILITY:-0}" = "1" ]; then
    bash "$SCRIPT_DIR/setup-observability.sh"
else
    echo "⏭️  Observability Plane skipped (set ENABLE_OBSERVABILITY=1 to install it, or run scripts/setup-observability.sh manually when needed)"
fi
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
