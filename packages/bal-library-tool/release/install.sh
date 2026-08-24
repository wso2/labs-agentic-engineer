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

# Installs the prebuilt bal library tool from a released distribution zip.
# Run from the unzipped release directory:
#   unzip bal-library-tool-<version>.zip
#   cd bal-library-tool-<version>
#   ./install.sh
#
# Fully offline — does not invoke gradle, does not contact the network.

set -euo pipefail

TOOL_ID="library"
ORG="ballerinax"
NAME="tool_library"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ ! -f "$SCRIPT_DIR/VERSION" ]; then
    echo "ERROR: VERSION file not found next to install.sh." >&2
    echo "Run this script from inside the unzipped release directory." >&2
    exit 1
fi
VERSION="$(tr -d '[:space:]' < "$SCRIPT_DIR/VERSION")"

JAR="$SCRIPT_DIR/native-$VERSION.jar"
TOML="$SCRIPT_DIR/Ballerina.toml"
if [ ! -f "$JAR" ] || [ ! -f "$TOML" ]; then
    echo "ERROR: Release artifacts missing (expected $JAR and $TOML)." >&2
    exit 1
fi

if ! command -v bal >/dev/null 2>&1; then
    echo "ERROR: 'bal' not found on PATH. Install Ballerina first: https://ballerina.io/downloads/" >&2
    exit 1
fi

BAL_VERSION=$(bal version | grep "^Ballerina" | awk '{print $2}')
BALA_HOME="$HOME/.ballerina/repositories/local/bala"
TOOL_BALA="$BALA_HOME/$ORG/$NAME/$VERSION/any"
TOOL_LIBS="$TOOL_BALA/tool/libs"
BAL_TOOLS_TOML="$HOME/.ballerina/.config/bal-tools.toml"

echo "==> Cleaning up any prior installation of $ORG/$NAME..."
rm -rf "$BALA_HOME/$ORG/$NAME"

echo "==> Installing JAR into $TOOL_LIBS ..."
mkdir -p "$TOOL_LIBS"
cp "$JAR" "$TOOL_LIBS/"

echo "==> Writing package metadata..."
cp "$TOML" "$TOOL_BALA/"
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
mkdir -p "$(dirname "$BAL_TOOLS_TOML")"
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
echo "Installed $ORG/$NAME:$VERSION."
echo "Try:"
echo "  bal library --help"
echo "  bal library search kafka messaging"
echo "  bal library overview ballerinax/kafka"
