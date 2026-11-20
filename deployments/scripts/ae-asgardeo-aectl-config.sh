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
# given base config with its `idp:` block set to imported clients, Asgardeo's
# claim names written out in full under `preset: "none"` (the shape a config
# for any other IdP takes), the organization's issuer and its handle, and
# with the four keys
# of its `thunder:` block commented out (aectl refuses them where it
# administers no ThunderID). `make dev-env AE_DEV_IDP=asgardeo` uses it so a
# developer's organization handle is never written into the repository;
# everything else (URLs, namespaces, toggles) stays the base's.
#
# With [clients] `asgardeo`, aectl creates the applications itself: the
# `idp:` block then says `clients: "asgardeo"`, and the base's commented
# `asgardeo:` block is turned on with the address of the organization's
# management API.
#
# Usage: ae-asgardeo-aectl-config.sh <base config> <organization handle> [base URL] [clients]
#   <base config>           skaffold/defaults.yaml, or a copy of it
#   <organization handle>   the Asgardeo organization's handle
#   [base URL]              default https://api.asgardeo.io
#   [clients]               imported (the default) or asgardeo
#
# It edits lines of the base that must be in the shape skaffold/defaults.yaml
# ships them: in the `idp:` block `  clients: "thunder"`,
# `    preset: "thunder"`, `  org_handle: "default"` and the commented
# `  # issuer: ""`; in the
# `thunder:` block its four keys; and, for [clients] `asgardeo`, the commented
# `# asgardeo:` line and its `#   api_url: "..."` line. A base without them is
# refused, not guessed at.
set -euo pipefail

usage="usage: ae-asgardeo-aectl-config.sh <base config> <organization handle> [base URL] [clients]"
base="${1:?$usage}"
tenant="${2:?$usage}"
url="${3:-https://api.asgardeo.io}"
clients="${4:-imported}"

[ -r "$base" ] || { echo "❌ cannot read the base config ${base}" >&2; exit 1; }
[[ "$tenant" =~ ^[a-z0-9][a-z0-9-]*$ ]] || { echo "❌ the organization handle must be lowercase letters, digits and hyphens" >&2; exit 1; }
[[ "$url" =~ ^https://[^/|]+ ]] || { echo "❌ the base URL must be an https:// URL" >&2; exit 1; }
case "$clients" in
    imported|asgardeo) ;;
    *) echo "❌ clients must be imported or asgardeo, not '${clients}'" >&2; exit 1 ;;
esac

issuer="${url%/}/t/${tenant}/oauth2/token"
# The thunder: block's own keys, each commented out; only inside that block,
# since other blocks have keys of the same names.
out="$(awk '
    /^thunder:/ { in_thunder = 1; print; next }
    /^[^ #]/    { in_thunder = 0 }
    in_thunder && /^  (namespace|url|admin_client_id|public_url):/ { print "  #" substr($0, 3); n++; next }
    { print }
    END { if (n != 4) exit 3 }
' "$base")" || { echo "❌ ${base}: its thunder: block does not hold the four keys this script comments out (see the header)" >&2; exit 1; }
out="$(printf '%s\n' "$out" | sed \
    -e 's|^  clients: "thunder"$|  clients: "'"${clients}"'"|' \
    -e 's|^  # issuer: ""$|  issuer: "'"${issuer}"'"|' \
    -e 's|^  org_handle: "default"$|  org_handle: "'"${tenant}"'"|')"

# The claims block: no preset, every value written out. These are the names
# an Asgardeo token carries (the values of aectl's own `asgardeo` preset).
claims='    preset: "none"\
    org:\
      handle: "org_handle"\
      id: "org_id"\
      name: "org_name"\
    caller_type:\
      claim: "aut"\
      machine: "APPLICATION"\
      user: "APPLICATION_USER"'
out="$(printf '%s\n' "$out" | sed -e 's|^    preset: "thunder"$|'"${claims}"'|')"

for line in "  clients: \"${clients}\"" '    preset: "none"' '      user: "APPLICATION_USER"' "  issuer: \"${issuer}\"" "  org_handle: \"${tenant}\""; do
    if [ "$(printf '%s\n' "$out" | grep -cxF -- "$line")" != "1" ]; then
        echo "❌ ${base}: its idp: block is not in the shape this script edits (see the header); could not set: ${line}" >&2
        exit 1
    fi
done
if [ "$clients" = "asgardeo" ]; then
    # The base's asgardeo: block is commented out (it is read only with
    # idp.clients asgardeo): its two lines are turned on.
    api="${url%/}/t/${tenant}/api/server/v1"
    out="$(printf '%s\n' "$out" | sed \
        -e 's|^# asgardeo:$|asgardeo:|' \
        -e 's|^#   api_url: ".*"$|  api_url: "'"${api}"'"|')"
    for line in 'asgardeo:' "  api_url: \"${api}\""; do
        if [ "$(printf '%s\n' "$out" | grep -cxF -- "$line")" != "1" ]; then
            echo "❌ ${base}: its commented asgardeo: block is not in the shape this script edits (see the header); could not set: ${line}" >&2
            exit 1
        fi
    done
fi
printf '%s\n' "$out"
