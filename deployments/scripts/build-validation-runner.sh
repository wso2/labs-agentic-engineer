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

# Builds the validation-task runner image (Debian + Playwright + baked chromium,
# runners/remote-worker/Dockerfile.validation) and imports it into the local k3d
# cluster. This is the LOCAL/DEV path: every machine builds the :dev tag once —
# self-contained, no shared registry needed. Validation dispatch (proxy path
# only) reads it via the compose VALIDATION_RUNNER_IMAGE env, which defaults to
# the same tag built here.
#
# For released platforms the image is published to GHCR as
# ghcr.io/wso2/aep/remote-worker-validation:<version> by .github/workflows/release.yml
# and wired into aep-api via the platform Helm chart's validationRunner.image.
#
# Idempotent: the (multi-minute, downloads chromium) build is skipped when the
# image already exists. FORCE=1 rebuilds — use it after changing Dockerfile.validation
# or the runner's TS/toolchain (skill edits are picked up live via the plugin
# hostPath overlay and never need a rebuild).
#
# Called by setup-aep.sh (build + import at setup) and `make build-validation-runner`.
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

IMAGE="${VALIDATION_RUNNER_IMAGE:-aep-validation-runner:dev}"
WORKER_DIR="$SCRIPT_DIR/../../runners/remote-worker"
DOCKERFILE="$WORKER_DIR/Dockerfile.validation"

if [ "${FORCE:-0}" = "1" ] || ! docker image inspect "$IMAGE" &>/dev/null; then
    echo "🐳 Building validation runner image ($IMAGE)..."
    echo "   First build downloads Playwright + a baked chromium — expect a few minutes."
    # --provenance=false --sbom=false: with the containerd image store (colima/
    # Docker Desktop) buildx defaults to emitting an OCI image index with
    # attestation manifests. `k3d image import` reports success on such an index
    # but the k3d node's containerd can't resolve it to a runnable image, so the
    # kubelet falls back to a registry pull of this local-only tag and the pod
    # hangs in ImagePullBackOff. Emitting a plain single-manifest image keeps the
    # local build importable.
    docker build --provenance=false --sbom=false -f "$DOCKERFILE" -t "$IMAGE" "$WORKER_DIR"
    echo "✅ built $IMAGE"
else
    echo "✅ validation runner image already present ($IMAGE) — skipping build (FORCE=1 to rebuild)"
fi

# Import into the k3d node so the runner Job can start without a cold registry
# pull (the :dev tag is local-only ⇒ imagePullPolicy IfNotPresent). Mirrors the
# coding-runner pre-import in setup-aep.sh. Skipped (with a note) when k3d or the
# cluster isn't up — e.g. building the image ahead of cluster setup.
if command -v k3d &>/dev/null && k3d cluster list "$CLUSTER_NAME" &>/dev/null; then
    k3d image import "$IMAGE" -c "$CLUSTER_NAME" \
        && echo "✅ imported $IMAGE into k3d cluster '$CLUSTER_NAME'" \
        || echo "⚠️  k3d image import failed; first validation dispatch may cold-pull"
else
    echo "ℹ️  k3d cluster '$CLUSTER_NAME' not found — built the image only; setup-aep.sh imports it at cluster setup."
fi
