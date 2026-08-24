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
# or the runner's TS/toolchain, or the bal library tool (skill edits are picked
# up live via the skills hostPath overlay and never need a rebuild; so is the
# `bal library` tool, but only for playground runs — see
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
# pin_node_image — keeps the imported tag out of kubelet's image GC.
source "$SCRIPT_DIR/utils.sh"

IMAGE="${AGENT_RUNNER_IMAGE:-aep-runner:dev}"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKER_DIR="$REPO_ROOT/runners/remote-worker"
DOCKERFILE="$WORKER_DIR/Dockerfile"
# The `bal library` tool is BUILT by the image's first stage from
# packages/bal-library-tool (ADR-0008), so there is no artifact to check for and
# no refresh step to have forgotten. What the build does need is a token that can
# read ballerina-platform's GitHub Packages, because `org.ballerinalang:ballerina-cli`
# is published nowhere else — checked here rather than surfacing as a Gradle
# "Username must not be null!" from inside a build stage.
#
# `gh auth token` is the path of least friction for a developer who already has
# `gh` set up; it needs the `read:packages` scope, which `gh auth login` does not
# grant by default.
BAL_TOOL_DIR="$REPO_ROOT/packages/bal-library-tool"
# EXPORTED, because `--secret env=PACKAGE_PAT` is read from the docker CLI's own
# environment and a plain shell assignment is not inherited by a child process.
# Without the export the mount resolves empty and Gradle reports a 401 from
# inside a build stage — and a cached tool stage hides it entirely.
export PACKAGE_PAT="${packagePAT:-${GITHUB_TOKEN:-$(gh auth token 2>/dev/null || true)}}"

if [ -z "$PACKAGE_PAT" ]; then
    echo "❌ no token to read ballerina-platform's GitHub Packages." >&2
    echo "   The image builds the bal library tool, whose ballerina-cli dependency" >&2
    echo "   is published only there. Any of these works:" >&2
    echo "     gh auth refresh -h github.com -s read:packages   # then re-run" >&2
    echo "     export packagePAT=<PAT with read:packages>" >&2
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
    # --build-context bal-library-tool=<repo>/packages/bal-library-tool: same
    # mechanism, for the same reason — the tool's source is outside this image's
    # context and its first stage compiles it.
    # --secret: never a --build-arg. Build args land in image history, and
    # release.yml publishes this image's builder stages to a public buildcache.
    docker build --provenance=false --sbom=false \
        --build-context "skills=$REPO_ROOT/skills" \
        --build-context "bal-library-tool=$BAL_TOOL_DIR" \
        --secret "id=packagePAT,env=PACKAGE_PAT" \
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
    # A missing image is a FAILURE, not a warning: this tag is local-only, so a pod
    # that cannot find it has no registry to fall back on and the dispatch is dead.
    # Both callers already expect a non-zero exit here and turn it into their own
    # message (setup-aep.sh's "dispatch stays disabled until fixed", setup.sh's
    # background-build branch), so exiting non-zero is what makes those fire.
    if k3d image import "$IMAGE" -c "$CLUSTER_NAME"; then
        # A successful import is not durable on its own: an idle local-only tag is
        # collected early by kubelet's image GC, and the next Job then has nothing to
        # pull. pin_node_image both pins it and verifies it actually landed on every
        # node — `k3d image import` is known to flake and still exit 0, which shows
        # up here as exit 2 and is an import failure rather than a pinning one.
        PIN_RC=0
        pin_node_image "$IMAGE" || PIN_RC=$?
        case "$PIN_RC" in
            0) echo "✅ imported $IMAGE into k3d cluster '$CLUSTER_NAME' (verified in node containerd)" ;;
            2) echo "❌ import reported success but the image is not in the node — re-run 'make build-runner'"
               exit 1 ;;
            # Pinned-but-unlabelled: the image IS there, so dispatch works today. Not
            # worth failing a build over — it only means GC can still evict it.
            *) echo "✅ imported $IMAGE into k3d cluster '$CLUSTER_NAME' (unpinned — see the warning above)" ;;
        esac
    else
        echo "❌ k3d image import failed — $IMAGE is not in the node and has no registry to pull from"
        exit 1
    fi
else
    echo "ℹ️  k3d cluster '$CLUSTER_NAME' not found — built the image only; setup-aep.sh imports it at cluster setup."
fi
