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

# Prints an aectl config whose platform IdP is an Asgardeo organization: the
# given base config with its `idp:` block set to kind asgardeo, the
# organization's issuer and its handle. `make dev-env AE_IDP_KIND=asgardeo`
# uses it so a developer's organization handle is never written into the
# repository; everything else (URLs, namespaces, toggles) stays the base's.
#
# Usage: ae-asgardeo-aectl-config.sh <base config> <organization handle> [base URL]
#   <base config>           skaffold/defaults.yaml, or a copy of it
#   <organization handle>   the Asgardeo organization's handle
#   [base URL]              default https://api.asgardeo.io
#
# It edits three lines of the base's `idp:` block, which must be in the shape
# skaffold/defaults.yaml ships them: `  kind: "thunder"`, and the commented
# `  # issuer: ""` and `  # org_handle: ""`. A base without them is refused,
# not guessed at.
set -euo pipefail

base="${1:?usage: ae-asgardeo-aectl-config.sh <base config> <organization handle> [base URL]}"
tenant="${2:?usage: ae-asgardeo-aectl-config.sh <base config> <organization handle> [base URL]}"
url="${3:-https://api.asgardeo.io}"

[ -r "$base" ] || { echo "❌ cannot read the base config ${base}" >&2; exit 1; }
[[ "$tenant" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "❌ the organization handle must be lowercase letters, digits and hyphens" >&2; exit 1; }
[[ "$url" =~ ^https://[^/|]+ ]] || { echo "❌ the base URL must be an https:// URL" >&2; exit 1; }

issuer="${url%/}/t/${tenant}/oauth2/token"
out="$(sed \
    -e 's|^  kind: "thunder"$|  kind: "asgardeo"|' \
    -e 's|^  # issuer: ""$|  issuer: "'"${issuer}"'"|' \
    -e 's|^  # org_handle: ""$|  org_handle: "'"${tenant}"'"|' \
    "$base")"

for line in '  kind: "asgardeo"' "  issuer: \"${issuer}\"" "  org_handle: \"${tenant}\""; do
    if [ "$(printf '%s\n' "$out" | grep -cxF -- "$line")" != "1" ]; then
        echo "❌ ${base}: its idp: block is not in the shape this script edits (see the header); could not set: ${line}" >&2
        exit 1
    fi
done
printf '%s\n' "$out"
