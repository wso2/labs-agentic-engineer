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

# Builds the JAR and installs it as a local bal tool named "library".
# Usage: ./install-local.sh

set -euo pipefail

TOOL_ID="library"
ORG="ballerinax"
NAME="tool_library"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Read, not restated. `make-dist.sh` and the playground's jar overlay both derive
# the version from gradle.properties; a literal here is another copy that goes
# stale silently and installs to a path nothing else looks in.
#
# Guarded, because `awk` exits 0 when it matches nothing — so `set -e` would let
# an empty VERSION through and install to `.../tool_library//any`.
VERSION="$(awk -F= '/^version=/{print $2}' "$SCRIPT_DIR/gradle.properties" | tr -d '[:space:]')"
if [ -z "$VERSION" ]; then
    echo "ERROR: no version= line in $SCRIPT_DIR/gradle.properties" >&2
    exit 1
fi
BALA_HOME="$HOME/.ballerina/repositories/local/bala"
TOOL_BALA="$BALA_HOME/$ORG/$NAME/$VERSION/any"
TOOL_LIBS="$TOOL_BALA/tool/libs"
BAL_TOOLS_TOML="$HOME/.ballerina/.config/bal-tools.toml"

echo "==> Cleaning up old installation..."
rm -rf "$BALA_HOME/$ORG/$NAME"

echo "==> Building JAR..."
cd "$SCRIPT_DIR"
./gradlew :native:jar

JAR="$SCRIPT_DIR/native/build/libs/native-$VERSION.jar"
if [ ! -f "$JAR" ]; then
    echo "ERROR: JAR not found at $JAR"
    exit 1
fi

echo "==> Installing tool JAR into $TOOL_LIBS ..."
mkdir -p "$TOOL_LIBS"
cp "$JAR" "$TOOL_LIBS/"

echo "==> Writing package.json..."
cp "$SCRIPT_DIR/Ballerina.toml" "$TOOL_BALA/"
BAL_VERSION=$(bal version | grep "^Ballerina" | awk '{print $2}')
cat > "$TOOL_BALA/package.json" <<JSON
{
  "organization": "$ORG",
  "name": "$NAME",
  "version": "$VERSION",
  "ballerina_version": "$BAL_VERSION",
  "platform": "java"
}
JSON

echo "==> Registering in bal-tools.toml..."
if [ -f "$BAL_TOOLS_TOML" ]; then
    python3 - <<PYEOF
import re
path = "$BAL_TOOLS_TOML"
with open(path) as f:
    content = f.read()
pattern = r'\[\[tool\]\][^\[]*id\s*=\s*"$TOOL_ID"[^\[]*'
content = re.sub(pattern, '', content)
content = content.strip() + "\n"
with open(path, 'w') as f:
    f.write(content)
PYEOF
fi

cat >> "$BAL_TOOLS_TOML" <<TOML

[[tool]]
id = "$TOOL_ID"
org = "$ORG"
name = "$NAME"
version = "$VERSION"
repository = "local"
active = true
TOML

echo ""
echo "Done! Try:"
echo "  bal library --help"
echo "  bal library find kafka messaging"
echo "  bal library overview ballerinax/kafka"
