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

# Refreshes the checked-in copy of the `bal library` tool that the runner image
# installs — runners/remote-worker/vendor/bal-library-tool.
#
# WHY THERE IS A COPY AT ALL: the tool is a Ballerina CLI tool living in its own
# repository, and it is not published to Ballerina Central. So there is nothing a
# fresh clone of THIS repo can build (the source is not here) and nothing the
# image can pull (`bal tool pull library` does not resolve). The distribution is
# checked in instead, and this script is how it gets there.
# See runners/remote-worker/design/decisions/ADR-0006-the-bal-library-tool-is-vendored.md.
#
# Usage:
#   bash deployments/scripts/vendor-bal-library-tool.sh          # from the tool repo beside this one
#   BAL_LIBRARY_TOOL_DIR=<path> bash …/vendor-bal-library-tool.sh
#
# Needs JDK 21 and the tool's own repository — it runs the tool's `make-dist.sh`,
# which is the ONE place that decides what a distribution contains. An unzipped
# release zip has the same layout, so a machine with neither can drop one in by
# hand; nothing here is generated in a way that only this script understands.
#
# After refreshing, rebuild the image so a dispatched run gets the new jar:
#   make build-runner FORCE=1
# The playground does not need that rebuild — it mounts the tool's working-tree
# jar over the installed one (playground/src/engine/coding-run.ts).
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TOOL_DIR="${BAL_LIBRARY_TOOL_DIR:-$REPO_ROOT/packages/bal-library-tool}"
VENDOR_DIR="$REPO_ROOT/runners/remote-worker/vendor/bal-library-tool"

if [ ! -x "$TOOL_DIR/make-dist.sh" ]; then
    echo "❌ no bal library tool repository at $TOOL_DIR" >&2
    echo "   Clone it there, or point BAL_LIBRARY_TOOL_DIR at your checkout:" >&2
    echo "     git clone https://github.com/xlight05/bal-library-tool $TOOL_DIR" >&2
    echo "   Or unzip a release into $VENDOR_DIR — same layout, no build needed." >&2
    exit 1
fi

echo "🔧 building the tool distribution in $TOOL_DIR ..."
"$TOOL_DIR/make-dist.sh"

DIST="$TOOL_DIR/dist"
VERSION="$(tr -d '[:space:]' < "$DIST/VERSION")"

# Replaced wholesale rather than copied over: the jar's name carries the version,
# so an update would otherwise leave the previous one behind and the image would
# install a directory with two jars in it.
rm -rf "$VENDOR_DIR"
mkdir -p "$VENDOR_DIR"
cp -R "$DIST/." "$VENDOR_DIR/"

# Generated with the payload so it cannot disagree with it about the version.
cat > "$VENDOR_DIR/README.md" <<EOF
# \`bal library\` tool — vendored distribution

**Generated. Do not edit by hand.** Version \`$VERSION\`, assembled by the tool's own
\`make-dist.sh\` and copied here by \`deployments/scripts/vendor-bal-library-tool.sh\`.

\`\`\`bash
make vendor-bal-library-tool     # refresh this directory
make build-runner FORCE=1        # then rebuild the image that installs it
\`\`\`

The runner image installs this with \`install.sh\`, the tool's own offline installer,
so the bala's \`package.json\` records the distribution of the \`bal\` in the image —
\`bal\` refuses a tool stamped with a newer distribution than the one running it,
which is why a prebuilt bala tree is not what gets checked in.

Why a copy and not a dependency: the tool lives in its own repository and is not on
Ballerina Central, so there is nothing for a fresh clone to build and nothing for the
image to pull. See
[ADR-0006](../../design/decisions/ADR-0006-the-bal-library-tool-is-vendored.md).
EOF

echo ""
echo "✅ vendored $VERSION into runners/remote-worker/vendor/bal-library-tool"
ls -1 "$VENDOR_DIR"
echo ""
echo "Next: make build-runner FORCE=1   (a dispatched run reads the baked copy)"
