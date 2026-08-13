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

# Builds the runner image (runners/remote-worker/Dockerfile — ONE image for both
# task kinds: Debian + Go + Playwright + baked chromium) and imports it into the
# local k3d cluster. This is the LOCAL/DEV path: every machine builds the :dev
# tag once — self-contained, no shared registry needed. Dispatch reads it via
# the compose AGENT_RUNNER_IMAGE env, which defaults to the same tag built here.
#
# For released platforms the image is published to GHCR as
# ghcr.io/wso2/aep/remote-worker:<version> by .github/workflows/release.yml
# and wired into aep-api via the platform Helm chart's codingAgentRunner.image.
#
# Idempotent: the (multi-minute, downloads chromium) build is skipped when the
# image already exists. FORCE=1 rebuilds — use it after changing the Dockerfile
# or the runner's TS/toolchain, and after `make vendor-bal-library-tool` (skill
# edits are picked up live via the skills hostPath overlay and never need a
# rebuild; so is the `bal library` tool, but only for playground runs — see
# playground/src/engine/coding-run.ts).
#
# SKIP_IMPORT=1 builds without importing — used by setup.sh, which starts this
# build in the background before the cluster exists and leaves the import to
# setup-aep.sh so the multi-GB node import runs exactly once.
#
# Called by setup-aep.sh (build + import at setup) and `make build-runner`.
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

IMAGE="${AGENT_RUNNER_IMAGE:-aep-runner:dev}"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKER_DIR="$REPO_ROOT/runners/remote-worker"
DOCKERFILE="$WORKER_DIR/Dockerfile"
# The `bal library` tool the image installs. CHECKED IN (the tool is in its own
# repository and not on Ballerina Central, so there is nothing to build here and
# nothing to pull), which means the only way it can be missing is a botched
# vendor refresh — worth naming here rather than as a COPY failure 300 layers in.
VENDORED_TOOL="$WORKER_DIR/vendor/bal-library-tool"

if [ ! -f "$VENDORED_TOOL/install.sh" ] || [ -z "$(ls "$VENDORED_TOOL"/*.jar 2>/dev/null)" ]; then
    echo "❌ vendored bal library tool incomplete at $VENDORED_TOOL" >&2
    echo "   Refresh it: make vendor-bal-library-tool" >&2
    exit 1
fi

if [ "${FORCE:-0}" = "1" ] || ! docker image inspect "$IMAGE" &>/dev/null; then
    echo "🐳 Building runner image ($IMAGE)..."
    echo "   First build downloads Playwright + a baked chromium — expect a few minutes."
    # --provenance=false --sbom=false: with the containerd image store (colima/
    # Docker Desktop) buildx defaults to emitting an OCI image index with
    # attestation manifests. `k3d image import` reports success on such an index
    # but the k3d node's containerd can't resolve it to a runnable image, so the
    # kubelet falls back to a registry pull of this local-only tag and the pod
    # hangs in ImagePullBackOff. Emitting a plain single-manifest image keeps the
    # local build importable.
    # --build-context skills=<repo>/skills: the authored skill library lives at
    # the repo root, outside this image's build context, and the runner bakes it
    # at /app/skills (see the Dockerfile). Same mechanism aep-api uses. The
    # `bal library` tool needs no such context — it is vendored INSIDE the
    # worker directory, so a plain COPY reaches it.
    docker build --provenance=false --sbom=false \
        --build-context "skills=$REPO_ROOT/skills" \
        -f "$DOCKERFILE" -t "$IMAGE" "$WORKER_DIR"
    echo "✅ built $IMAGE"
else
    echo "✅ runner image already present ($IMAGE) — skipping build (FORCE=1 to rebuild)"
fi

# Import into the k3d node so the runner Job can start without a cold registry
# pull (the :dev tag is local-only ⇒ imagePullPolicy IfNotPresent). A cold pull
# of a multi-GB image has taken long enough to blow past the Job's
# activeDeadlineSeconds, killing the pod the moment it starts. Skipped (with a
# note) when k3d or the cluster isn't up — e.g. building ahead of cluster setup.
if [ "${SKIP_IMPORT:-0}" = "1" ]; then
    echo "⏭️  node import skipped (SKIP_IMPORT=1) — the caller owns it"
elif command -v k3d &>/dev/null && k3d cluster list "$CLUSTER_NAME" &>/dev/null; then
    k3d image import "$IMAGE" -c "$CLUSTER_NAME" \
        && echo "✅ imported $IMAGE into k3d cluster '$CLUSTER_NAME'" \
        || echo "⚠️  k3d image import failed; first dispatch may cold-pull"
else
    echo "ℹ️  k3d cluster '$CLUSTER_NAME' not found — built the image only; setup-aep.sh imports it at cluster setup."
fi
