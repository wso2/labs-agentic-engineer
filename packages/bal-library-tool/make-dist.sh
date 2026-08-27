#!/usr/bin/env bash
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

# Assembles the offline distribution into dist/ — the jar, the package metadata,
# and the two installers, in exactly the layout the release zip carries.
#
# This is what a consumer copies when it cannot `bal tool pull library`: the tool
# is not on Ballerina Central (internal-docs/distribution.md), so a downstream
# repo has no way to fetch it and vendors this directory instead. Producing it
# with a script rather than by hand is what keeps a vendored copy honest — the
# jar and the VERSION beside it always come from the same build.
#
# Usage: ./make-dist.sh
#
# The installers stay UNMODIFIED between here and the target machine on purpose.
# `install.sh` stamps the bala's package.json with the distribution reported by
# the `bal` next to it, and `bal` refuses a tool whose recorded distribution is
# newer than the one running it — so the install has to happen where the tool
# will run, and a prebuilt bala tree cannot be shipped in its place.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

VERSION="$(awk -F= '/^version=/{print $2}' gradle.properties | tr -d '[:space:]')"
if [ -z "$VERSION" ]; then
    echo "ERROR: no version= line in gradle.properties" >&2
    exit 1
fi

echo "==> Building the tool jar ($VERSION)..."
./gradlew :native:jar

JAR="native/build/libs/native-$VERSION.jar"
if [ ! -f "$JAR" ]; then
    echo "ERROR: expected the jar at $JAR" >&2
    exit 1
fi

DIST="$SCRIPT_DIR/dist"
echo "==> Staging $DIST ..."
rm -rf "$DIST"
mkdir -p "$DIST"
cp "$JAR" "$DIST/"
cp Ballerina.toml "$DIST/"
cp release/install.sh release/install.ps1 "$DIST/"
chmod +x "$DIST/install.sh"
printf '%s\n' "$VERSION" > "$DIST/VERSION"

echo ""
echo "dist/ ($VERSION):"
ls -1 "$DIST"
echo ""
echo "Install it anywhere \`bal\` is on PATH:"
echo "  cd dist && ./install.sh"
