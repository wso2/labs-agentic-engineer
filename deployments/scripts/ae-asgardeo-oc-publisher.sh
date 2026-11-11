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

# LOCAL DEV CLUSTER ONLY. Points OpenChoreo's build at an Asgardeo
# organization, on a k3d cluster built with `make dev-env AE_IDP_KIND=asgardeo`.
#
# The last step of an OpenChoreo build (ClusterWorkflowTemplate
# `generate-workload`, upstream's generate-workload-k3d.yaml) signs in as
# OpenChoreo's own client, openchoreo-workload-publisher-client, and registers
# the built workload with OpenChoreo's API. The template carries the token
# address, the Host header for that call and the client's secret as default
# values of its inputs, and upstream's defaults name the in-cluster ThunderID,
# which an Asgardeo dev cluster does not have. This sets those three to the
# organization's token endpoint (from its OIDC discovery document), that
# endpoint's own host, and the secret of the client created by
#   ae-asgardeo-apps.sh --with-openchoreo-publisher
# The client id is left as it is: OpenChoreo's role binding for the publisher
# matches the `client_id` claim with that id.
#
# Usage:
#   ASGARDEO_TENANT=<organization handle> \
#   deployments/scripts/ae-asgardeo-oc-publisher.sh [--dry-run] <secret file>
#
# <secret file> is the oc-workload-publisher file ae-asgardeo-apps.sh wrote.
# Optional: ASGARDEO_BASE_URL (default https://api.asgardeo.io).
#
# It refuses any kubectl context that is not a k3d cluster. On a real cluster
# the publisher client and the build template are the cluster owner's to set
# up, and a client secret does not belong in a template's default value: this
# script puts it there only because that is where upstream's template reads
# it, on a cluster that lives on a developer's machine. The secret reaches jq
# and kubectl through files, never argv, and is never printed.
#
# Idempotent. Run it again after anything re-applies the upstream template (a
# new `make dev-env` does, and runs this itself when the secret file exists).
set -euo pipefail
umask 077

TEMPLATE="clusterworkflowtemplate.argoproj.io/generate-workload"
STEP="generate-workload-cr"

die() { echo "❌ $*" >&2; exit 1; }

DRY_RUN=0
SECRET_FILE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    -h|--help) sed -n '18,49p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*) die "unknown argument: $1" ;;
    *) [ -z "$SECRET_FILE" ] || die "one secret file, not several"; SECRET_FILE="$1" ;;
  esac
  shift
done

for tool in curl jq kubectl; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found on PATH"
done

TENANT="${ASGARDEO_TENANT:-}"
BASE_URL="${ASGARDEO_BASE_URL:-https://api.asgardeo.io}"
[[ "$TENANT" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "ASGARDEO_TENANT must be the organization handle (lowercase letters, digits, hyphens)"
[[ "$BASE_URL" =~ ^https://[^/]+ ]] || die "ASGARDEO_BASE_URL must be an https:// URL"
[ -n "$SECRET_FILE" ] || die "usage: ae-asgardeo-oc-publisher.sh [--dry-run] <secret file>"
[ -s "$SECRET_FILE" ] || die "${SECRET_FILE} is missing or empty: run ae-asgardeo-apps.sh --with-openchoreo-publisher first"

context="$(kubectl config current-context 2>/dev/null)" || die "kubectl has no current context"
case "$context" in
  k3d-*) ;;
  *) die "the kubectl context is ${context}, not a k3d cluster: this script is for a local dev cluster only" ;;
esac

# The organization's token endpoint, as its discovery document names it.
ISSUER="${BASE_URL%/}/t/${TENANT}/oauth2/token"
discovery="$(curl -fsSL --max-time 15 "${ISSUER}/.well-known/openid-configuration")" \
  || die "could not read ${ISSUER}/.well-known/openid-configuration"
TOKEN_URL="$(printf '%s' "$discovery" | jq -r --arg issuer "$ISSUER" \
  'if (.issuer // "" | rtrimstr("/")) == ($issuer | rtrimstr("/")) then (.token_endpoint // "") else "" end')"
[[ "$TOKEN_URL" =~ ^https://[^/]+ ]] || die "the discovery document at ${ISSUER} does not name that issuer with an https token_endpoint"
TOKEN_HOST="$(printf '%s' "$TOKEN_URL" | sed -E 's#^https://([^/]+).*#\1#')"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

kubectl get "$TEMPLATE" -o json > "${WORK}/template.json" \
  || die "${TEMPLATE} is not in the cluster ${context}: is the workflow plane installed (WITH_BUILD=1)?"

# Where each of the three inputs sits in the template; a template upstream has
# reshaped is refused, not guessed at.
paths="$(jq -r --arg step "$STEP" '
  (.spec.templates | map(.name) | index($step)) as $t
  | if $t == null then error("no template named " + $step) else . end
  | [ "oauth-token-url", "oauth-host-header", "oauth-client-secret" ] as $names
  | [ $names[] as $n
      | (.spec.templates[$t].inputs.parameters | map(.name) | index($n)) as $p
      | if $p == null then error("no input named " + $n) else "/spec/templates/\($t)/inputs/parameters/\($p)/default" end ]
  | .[]' "${WORK}/template.json")" || die "${TEMPLATE} is not in the shape this script edits (see the header)"
url_path="$(printf '%s\n' "$paths" | sed -n 1p)"
host_path="$(printf '%s\n' "$paths" | sed -n 2p)"
secret_path="$(printf '%s\n' "$paths" | sed -n 3p)"

current() { jq -r --arg step "$STEP" --arg n "$1" '.spec.templates[] | select(.name == $step) | .inputs.parameters[] | select(.name == $n) | .default // ""' "${2:-${WORK}/template.json}"; }

echo "Cluster               : ${context}"
echo "Build template        : ${TEMPLATE} (step ${STEP})"
echo "Token address         : $(current oauth-token-url)  ->  ${TOKEN_URL}"
echo "Host header           : $(current oauth-host-header)  ->  ${TOKEN_HOST}"
echo "Client id             : $(current oauth-client-id) (unchanged)"
echo "Client secret         : (not shown)  ->  the contents of ${SECRET_FILE} (not shown)"

if [ "$DRY_RUN" = "1" ]; then
  echo "Dry run: nothing was changed."
  exit 0
fi

jq -n --arg up "$url_path" --arg url "$TOKEN_URL" --arg hp "$host_path" --arg host "$TOKEN_HOST" \
      --arg sp "$secret_path" --rawfile secret "$SECRET_FILE" '
  [ { op: "replace", path: $up, value: $url },
    { op: "replace", path: $hp, value: $host },
    { op: "replace", path: $sp, value: ($secret | gsub("\\s+$"; "")) } ]' > "${WORK}/patch.json"
kubectl patch "$TEMPLATE" --type=json --patch-file "${WORK}/patch.json" >/dev/null \
  || die "patching ${TEMPLATE} failed"

kubectl get "$TEMPLATE" -o json > "${WORK}/after.json"
ok="$(jq -r --arg step "$STEP" --arg url "$TOKEN_URL" --arg host "$TOKEN_HOST" --rawfile secret "$SECRET_FILE" '
  [ .spec.templates[] | select(.name == $step) | .inputs.parameters[] | { (.name): (.default // "") } ] | add
  | (.["oauth-token-url"] == $url) and (.["oauth-host-header"] == $host)
    and (.["oauth-client-secret"] == ($secret | gsub("\\s+$"; "")))' "${WORK}/after.json")"
[ "$ok" = "true" ] || die "${TEMPLATE} does not hold the three values after the patch"
echo "✅ OpenChoreo's build signs in at ${TOKEN_URL} as $(current oauth-client-id "${WORK}/after.json")"
