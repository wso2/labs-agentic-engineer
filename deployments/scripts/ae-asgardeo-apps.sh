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

# Creates AE's six OAuth applications in an Asgardeo organization. Run once
# by that organization's admin, with the admin's own credentials, before AE is
# installed with Asgardeo as its platform IdP. AE holds no management rights
# on the organization, so nothing in AE can create these; and they need exact
# settings: AE's fixed client ids, JWT access tokens (Asgardeo's default is
# opaque, which AE cannot verify) and, for the console, the user attributes AE
# reads from the access token.
#
#   client id                    kind     secret file (vault path aep/idp-clients/<file>)
#   aep-console-client           browser  none (public client, PKCE; redirects
#                                         <console>/callback after sign-in and
#                                         <console> after sign-out)
#   aep-api-client               M2M      aep-api-client
#   ae-studio-internal-client    M2M      ae-studio-internal
#   aectl-openchoreo-client      M2M      aectl-openchoreo
#   aep-publisher-<namespace>    M2M      org-publisher
#   ae-studio-<namespace>        M2M      org-studio
#
# <namespace> is the org's OpenChoreo namespace: aep-api names an org's two
# clients after it. OpenChoreo's own clients are the cluster owner's and are
# not created here, with two exceptions for a developer's own cluster:
#
#   --with-openchoreo-publisher   LOCAL DEV CLUSTER ONLY. Also creates
#       openchoreo-workload-publisher-client, the client OpenChoreo's build
#       signs in as to register a built workload, and writes its secret to the
#       file oc-workload-publisher. That secret is not imported into AE's
#       vault: deployments/scripts/ae-asgardeo-oc-publisher.sh puts it into the
#       build template of a local k3d cluster. On a real cluster this client
#       and that template are the cluster owner's to set up.
#
#   --with-openchoreo-observer    LOCAL DEV CLUSTER ONLY. Also creates
#       openchoreo-observer-resource-reader-client, the client the observer
#       (the log query service of OpenChoreo's observability plane) signs in
#       as to read OpenChoreo's API, and writes its secret to the file
#       oc-observer-reader. That secret is not imported into AE's vault:
#       `make dev-env AE_DEV_IDP=asgardeo` makes the observer's Secret of a
#       local k3d cluster from it, and installs the logs part of the plane
#       only when the file is there
#       (deployments/scripts/setup-env-for-aectl.sh, step 7). On a real
#       cluster this client and the plane are the cluster owner's to set up.
#
# Prerequisite: an M2M application in the organization, authorized (its "API
# Authorization" tab) for the Application Management API with the scopes
# internal_application_mgt_create and internal_application_mgt_view. Its
# client id and secret are read from two files and are never given to AE.
#
# Usage:
#   ASGARDEO_TENANT=<organization handle> \
#   AE_CONSOLE_URL=<the console's public URL> \
#   ASGARDEO_ADMIN_CLIENT_ID_FILE=<file> ASGARDEO_ADMIN_CLIENT_SECRET_FILE=<file> \
#   AE_ASGARDEO_OUT_DIR=<directory outside any repository> \
#   deployments/scripts/ae-asgardeo-apps.sh [--dry-run] [--confirm-tenant <handle>]
#       [--with-openchoreo-publisher] [--with-openchoreo-observer]
#
# Optional: AE_ORG_NAMESPACE (default "default"), ASGARDEO_BASE_URL (default
# https://api.asgardeo.io).
#
# It lists what it will create and asks for the organization handle to be
# typed before any write; without a terminal, pass --confirm-tenant <handle>.
# --dry-run prints the list and sends nothing.
#
# Idempotent: an application whose client id exists is reported and left
# alone, its secret included. To replace one, delete it in the Asgardeo
# console and run this again; to rotate a secret, do it in the console and
# import the new value.
#
# Each M2M secret is generated here and written to AE_ASGARDEO_OUT_DIR, one
# file per secret (directory 700, files 600). Secrets and the management token
# reach curl and jq through files and stdin, never argv, and are never
# printed. The script ends with the `aectl platform secret import` commands
# for the files AE imports; delete those files once imported. Keep the files
# of OpenChoreo's two clients: every `make dev-env` of a local cluster reads
# them.
set -euo pipefail
umask 077

BROWSER_TEMPLATE="6a90e4b0-fbff-42d7-bfde-1efd98f07cd7"
M2M_TEMPLATE="m2m-application"
SCOPES="internal_application_mgt_create internal_application_mgt_view"
# What the console app's access token must carry, and the local claims behind
# them that the application must be allowed to read.
TOKEN_ATTRIBUTES='["email","family_name","given_name","groups","username"]'
REQUESTED_CLAIMS='["http://wso2.org/claims/emailaddress","http://wso2.org/claims/groups","http://wso2.org/claims/givenname","http://wso2.org/claims/lastname","http://wso2.org/claims/username"]'

die() { echo "❌ $*" >&2; exit 1; }

# OpenChoreo's workload publisher: its client id is fixed by OpenChoreo's
# build template and role binding; its secret file is not one AE imports.
OC_PUBLISHER_CLIENT="openchoreo-workload-publisher-client"
OC_PUBLISHER_FILE="oc-workload-publisher"
# OpenChoreo's observer: its client id is fixed by the observability plane's
# chart and by OpenChoreo's role binding; its secret file is not one AE
# imports either.
OC_OBSERVER_CLIENT="openchoreo-observer-resource-reader-client"
OC_OBSERVER_FILE="oc-observer-reader"

DRY_RUN=0
CONFIRM_TENANT=""
WITH_OC_PUBLISHER=0
WITH_OC_OBSERVER=0
while [ $# -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --confirm-tenant) shift; CONFIRM_TENANT="${1:-}" ;;
    --with-openchoreo-publisher) WITH_OC_PUBLISHER=1 ;;
    --with-openchoreo-observer) WITH_OC_OBSERVER=1 ;;
    -h|--help) sed -n '18,90p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
  shift
done

for tool in curl jq openssl; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found on PATH"
done

TENANT="${ASGARDEO_TENANT:-}"
BASE_URL="${ASGARDEO_BASE_URL:-https://api.asgardeo.io}"
NAMESPACE="${AE_ORG_NAMESPACE:-default}"
CONSOLE_URL="${AE_CONSOLE_URL:-}"
OUT_DIR="${AE_ASGARDEO_OUT_DIR:-}"

[[ "$TENANT" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "ASGARDEO_TENANT must be the organization handle (lowercase letters, digits, hyphens)"
[[ "$NAMESPACE" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || die "AE_ORG_NAMESPACE must be a namespace name (lowercase letters, digits, hyphens)"
[[ "$CONSOLE_URL" =~ ^https?://[^/]+ ]] || die "AE_CONSOLE_URL must be the console's public URL (http:// or https://)"
[[ "$CONSOLE_URL" =~ [[:space:]\|\(\)\\] ]] && die "AE_CONSOLE_URL must not contain spaces, '|', parentheses or backslashes"
[[ "$BASE_URL" =~ ^https:// ]] || die "ASGARDEO_BASE_URL must be an https:// URL"
[ -n "$OUT_DIR" ] || die "AE_ASGARDEO_OUT_DIR is required: the directory the generated secrets are written to"

CONSOLE_URL="${CONSOLE_URL%/}"
CONSOLE_ORIGIN="$(printf '%s' "$CONSOLE_URL" | sed -E 's#^(https?://[^/]+).*#\1#')"
CALLBACK_URL="${CONSOLE_URL}/callback"
# Where the console asks to be sent back after sign-out: its origin. Asgardeo
# refuses a sign-out whose return address is not registered on the
# application, so it is registered beside the sign-in callback.
POST_LOGOUT_URL="${CONSOLE_ORIGIN}"
API="${BASE_URL%/}/t/${TENANT}/api/server/v1"
ISSUER="${BASE_URL%/}/t/${TENANT}/oauth2/token"

# client id | kind | secret file name ("" for the public client)
APPS="aep-console-client|browser|
aep-api-client|m2m|aep-api-client
ae-studio-internal-client|m2m|ae-studio-internal
aectl-openchoreo-client|m2m|aectl-openchoreo
aep-publisher-${NAMESPACE}|m2m|org-publisher
ae-studio-${NAMESPACE}|m2m|org-studio"
if [ "$WITH_OC_PUBLISHER" = "1" ]; then
  APPS="${APPS}
${OC_PUBLISHER_CLIENT}|m2m|${OC_PUBLISHER_FILE}"
fi
if [ "$WITH_OC_OBSERVER" = "1" ]; then
  APPS="${APPS}
${OC_OBSERVER_CLIENT}|m2m|${OC_OBSERVER_FILE}"
fi

echo "Asgardeo organization : ${TENANT} (${BASE_URL})"
echo "Org namespace         : ${NAMESPACE}"
echo "Console               : ${CONSOLE_URL} (redirects ${CALLBACK_URL} and ${POST_LOGOUT_URL}, origin ${CONSOLE_ORIGIN})"
echo "Secrets written to    : ${OUT_DIR}"
echo "Applications (each created only if its client id does not exist):"
printf '%s\n' "$APPS" | while IFS='|' read -r id kind file; do
  if [ "$kind" = "browser" ]; then
    echo "  - ${id}  (browser: authorization code + PKCE, JWT access token with ${TOKEN_ATTRIBUTES})"
  elif [ "$id" = "$OC_PUBLISHER_CLIENT" ]; then
    echo "  - ${id}  (M2M: OpenChoreo's build client, for a LOCAL DEV CLUSTER only; secret file ${file})"
  elif [ "$id" = "$OC_OBSERVER_CLIENT" ]; then
    echo "  - ${id}  (M2M: OpenChoreo's observer client, for a LOCAL DEV CLUSTER only; secret file ${file})"
  else
    echo "  - ${id}  (M2M: client credentials, JWT access token; secret file ${file})"
  fi
done

if [ "$DRY_RUN" = "1" ]; then
  echo "Dry run: nothing was sent."
  exit 0
fi

[ -r "${ASGARDEO_ADMIN_CLIENT_ID_FILE:-}" ] || die "ASGARDEO_ADMIN_CLIENT_ID_FILE must name a readable file holding the admin M2M application's client id"
[ -r "${ASGARDEO_ADMIN_CLIENT_SECRET_FILE:-}" ] || die "ASGARDEO_ADMIN_CLIENT_SECRET_FILE must name a readable file holding the admin M2M application's client secret"

if [ -z "$CONFIRM_TENANT" ]; then
  [ -t 0 ] || die "no terminal to confirm on: pass --confirm-tenant ${TENANT}"
  printf 'Type the organization handle to create these applications in it: '
  read -r CONFIRM_TENANT
fi
[ "$CONFIRM_TENANT" = "$TENANT" ] || die "the handle typed does not match ASGARDEO_TENANT; nothing was sent"

mkdir -p "$OUT_DIR"
chmod 700 "$OUT_DIR"
if git -C "$OUT_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  die "AE_ASGARDEO_OUT_DIR is inside a git repository; choose a directory outside any repository"
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
AUTH_CFG="${WORK}/auth.cfg"
BODY="${WORK}/body"
HEADERS="${WORK}/headers"

# --- the management token ----------------------------------------------------
# Basic client authentication, built by jq from the two files and handed to
# curl as a config on stdin.
token_json="${WORK}/token.json"
status=$(jq -rn --rawfile id "$ASGARDEO_ADMIN_CLIENT_ID_FILE" --rawfile secret "$ASGARDEO_ADMIN_CLIENT_SECRET_FILE" \
    '"header = \"Authorization: Basic " + ((($id | gsub("\\s+$"; "")) + ":" + ($secret | gsub("\\s+$"; ""))) | @base64) + "\""' \
  | curl -sS --max-time 30 -K - -o "$token_json" -w '%{http_code}' \
      -X POST "${BASE_URL%/}/t/${TENANT}/oauth2/token" \
      --data-urlencode grant_type=client_credentials --data-urlencode "scope=${SCOPES}")
[ "$status" = "200" ] || die "the management token request answered HTTP ${status}: $(jq -r '[.error, .error_description] | map(select(. != null)) | join(": ")' "$token_json" 2>/dev/null || true)"
granted=" $(jq -r '.scope // ""' "$token_json") "
for scope in $SCOPES; do
  case "$granted" in
    *" ${scope} "*) ;;
    *) die "the admin application was not granted ${scope}: authorize it for the Application Management API (API Authorization tab) with that scope" ;;
  esac
done
jq -r '"header = \"Authorization: Bearer " + .access_token + "\""' "$token_json" > "$AUTH_CFG"
rm -f "$token_json"
echo "✅ Management token issued"

# api METHOD URL [curl args...]: the response body lands in $BODY, its headers
# in $HEADERS, and the HTTP status is printed.
api() {
  local method="$1" url="$2"
  shift 2
  curl -sS --max-time 30 -K "$AUTH_CFG" -o "$BODY" -D "$HEADERS" -w '%{http_code}' -X "$method" "$url" "$@"
}

api_error() {
  jq -r '[.code, .message, .description] | map(select(. != null)) | join(" | ")' "$BODY" 2>/dev/null || true
}

# app_id_of CLIENT_ID: the application's id, or nothing when there is none.
app_id_of() {
  local status
  status=$(api GET "${API}/applications" -G --data-urlencode "filter=clientId eq $1" --data-urlencode "limit=2")
  [ "$status" = "200" ] || die "looking up $1 answered HTTP ${status}: $(api_error)"
  jq -r 'if (.totalResults // 0) > 1 then error("more than one application has this client id") else (.applications[0].id // "") end' "$BODY"
}

# JQ_RX is the jq filter rx, which escapes every regular-expression
# metacharacter of a URL. create_app writes the console's redirect URLs with
# it and check_app compares with it, so a redirect entry matches those URLs
# and nothing else: an unescaped "." would also accept a look-alike host.
JQ_RX='def rx: gsub("(?<c>[.+*?(){}|^$\\[\\]\\\\])"; "\\\(.c)");'

# check_app CLIENT_ID KIND APP_ID: reads the application back and prints every
# setting that is not what AE needs; prints nothing when all are right.
check_app() {
  local id="$1" kind="$2" app="$3" status
  status=$(api GET "${API}/applications/${app}/inbound-protocols/oidc")
  [ "$status" = "200" ] || die "reading ${id}'s OIDC settings answered HTTP ${status}: $(api_error)"
  if [ "$kind" = "m2m" ]; then
    jq -r --arg id "$id" '
      [ (if .clientId != $id then "client id is \(.clientId)" else empty end),
        (if .accessToken.type != "JWT" then "access token type is \(.accessToken.type), not JWT" else empty end),
        (if ((.grantTypes // []) | index("client_credentials")) == null then "the client_credentials grant is off" else empty end)
      ] | .[]' "$BODY"
    return
  fi
  # Asgardeo stores several redirect URLs as one entry, regexp=(a|b), which
  # it matches as a regular expression. A URL counts as registered when it is
  # a plain entry, or an alternative escaped as create_app escapes it. An
  # alternative that names the URL with its metacharacters unescaped is
  # reported: it also matches other hosts.
  jq -r --arg id "$id" --arg cb "$CALLBACK_URL" --arg logout "$POST_LOGOUT_URL" --arg origin "$CONSOLE_ORIGIN" --argjson attrs "$TOKEN_ATTRIBUTES" "$JQ_RX"'
    [ (.callbackURLs // [])[] | select((startswith("regexp=(") and endswith(")")) | not) ] as $plain
    | [ (.callbackURLs // [])[] | select(startswith("regexp=(") and endswith(")")) | (.[8:-1] | split("|"))[] ] as $alternatives
    | def redirect_problem($what; $url):
        if ($plain | index($url)) != null or ($alternatives | index($url | rx)) != null then empty
        elif ($alternatives | map(gsub("\\\\(?<c>.)"; "\(.c)")) | index($url)) != null
        then "the \($what) redirect URL \($url) is registered as a regular expression with unescaped characters, which also matches look-alike hosts; register it as \($url | rx)"
        else "the \($what) redirect URL \($url) is not registered" end;
      [ (if .clientId != $id then "client id is \(.clientId)" else empty end),
      (if .accessToken.type != "JWT" then "access token type is \(.accessToken.type), not JWT" else empty end),
      (if .publicClient != true then "it is not a public client" else empty end),
      (if .pkce.mandatory != true then "PKCE is not mandatory" else empty end),
      (if ((.grantTypes // []) | index("authorization_code")) == null then "the authorization_code grant is off" else empty end),
      (if ((.grantTypes // []) | index("refresh_token")) == null then "the refresh_token grant is off" else empty end),
      redirect_problem("sign-in"; $cb),
      redirect_problem("sign-out"; $logout),
      (if ((.allowedOrigins // []) | index($origin)) == null then "the origin \($origin) is not allowed" else empty end),
      ($attrs - (.accessToken.accessTokenAttributes // []) | map("the access token lacks the attribute \(.)") | .[])
    ] | .[]' "$BODY"
  status=$(api GET "${API}/applications/${app}")
  [ "$status" = "200" ] || die "reading ${id} answered HTTP ${status}: $(api_error)"
  jq -r --argjson claims "$REQUESTED_CLAIMS" '
    $claims - [(.claimConfiguration.requestedClaims // [])[].claim.uri] | map("the application may not read the claim \(.)") | .[]' "$BODY"
}

# create_app CLIENT_ID KIND SECRET_FILE: POSTs the application and prints its id.
create_app() {
  local id="$1" kind="$2" secret_file="$3" status
  if [ "$kind" = "m2m" ]; then
    status=$(jq -n --arg id "$id" --arg template "$M2M_TEMPLATE" --rawfile secret "$secret_file" '
        { name: $id, templateId: $template,
          inboundProtocolConfiguration: { oidc: {
            clientId: $id, clientSecret: $secret, grantTypes: ["client_credentials"],
            accessToken: { type: "JWT", userAccessTokenExpiryInSeconds: 3600, applicationAccessTokenExpiryInSeconds: 3600 } } } }' \
      | api POST "${API}/applications" -H 'Content-Type: application/json' -d @-)
  else
    status=$(jq -n --arg id "$id" --arg template "$BROWSER_TEMPLATE" --arg cb "$CALLBACK_URL" --arg logout "$POST_LOGOUT_URL" --arg origin "$CONSOLE_ORIGIN" \
        --argjson attrs "$TOKEN_ATTRIBUTES" --argjson claims "$REQUESTED_CLAIMS" "$JQ_RX"'
        # The two redirect URLs go in one regexp entry, which Asgardeo matches
        # as a regular expression, each escaped with rx.
        { name: $id, templateId: $template,
          advancedConfigurations: { skipLoginConsent: true, skipLogoutConsent: true },
          claimConfiguration: {
            dialect: "LOCAL",
            requestedClaims: ($claims | map({ claim: { uri: . }, mandatory: false })),
            claimMappings: ($claims | map({ applicationClaim: ., localClaim: { uri: . } })),
            subject: { claim: { uri: "http://wso2.org/claims/userid" } } },
          inboundProtocolConfiguration: { oidc: {
            clientId: $id, grantTypes: ["authorization_code", "refresh_token"], publicClient: true,
            pkce: { mandatory: true, supportPlainTransformAlgorithm: false },
            callbackURLs: ["regexp=(\($logout | rx)|\($cb | rx))"], allowedOrigins: [$origin],
            accessToken: { type: "JWT", bindingType: "sso-session", revokeTokensWhenIDPSessionTerminated: true,
              validateTokenBinding: false, userAccessTokenExpiryInSeconds: 3600,
              applicationAccessTokenExpiryInSeconds: 3600, accessTokenAttributes: $attrs },
            refreshToken: { expiryInSeconds: 86400, renewRefreshToken: true, extendRenewedRefreshTokenExpiryTime: true } } } }' \
      | api POST "${API}/applications" -H 'Content-Type: application/json' -d @-)
  fi
  [ "$status" = "201" ] || die "creating ${id} answered HTTP ${status}: $(api_error)"
  tr -d '\r' < "$HEADERS" | sed -n 's#^[Ll]ocation: .*/applications/\([^/[:space:]]*\)$#\1#p' | tail -1
}

failed=0
imports=""
oc_publisher_step=""
oc_observer_file=""
absent=""
# The list is read on fd 3, so nothing in the loop can consume it from stdin.
while IFS='|' read -r id kind file <&3; do
  app="$(app_id_of "$id")"
  if [ -n "$app" ]; then
    state="exists, left alone"
    if [ -n "$file" ] && [ ! -s "${OUT_DIR}/${file}" ]; then
      state="${state}; its secret is not in ${OUT_DIR}, so it is not in the import list below"
      absent="${absent} ${id}"
    fi
  else
    secret_file=""
    if [ "$kind" = "m2m" ]; then
      secret_file="${WORK}/${file}"
      # printf, not a redirect of openssl's output: no trailing newline in the file.
      printf '%s' "$(openssl rand -hex 32)" > "$secret_file"
    fi
    app="$(create_app "$id" "$kind" "$secret_file")"
    [ -n "$app" ] || die "${id} was created but Asgardeo returned no application id; look it up in the console"
    if [ -n "$secret_file" ]; then
      mv -f "$secret_file" "${OUT_DIR}/${file}"
      chmod 600 "${OUT_DIR}/${file}"
    fi
    state="created"
  fi
  problems="$(check_app "$id" "$kind" "$app")"
  if [ -n "$problems" ]; then
    failed=1
    echo "❌ ${id} (${state}) is not set up as AE needs:"
    printf '%s\n' "$problems" | sed 's/^/     - /'
  else
    echo "✅ ${id} (${state})"
  fi
  if [ -n "$file" ] && [ -s "${OUT_DIR}/${file}" ]; then
    # %q: the path is printed as one shell word, so a directory name with a
    # space survives being copied into a terminal.
    printf -v quoted_path '%q' "${OUT_DIR}/${file}"
    if [ "$id" = "$OC_PUBLISHER_CLIENT" ]; then
      # Not AE's secret: it goes into the local cluster's build template.
      oc_publisher_step="ASGARDEO_TENANT=${TENANT} ASGARDEO_BASE_URL=${BASE_URL} deployments/scripts/ae-asgardeo-oc-publisher.sh ${quoted_path}"
    elif [ "$id" = "$OC_OBSERVER_CLIENT" ]; then
      # Not AE's secret either: the dev cluster's observer signs in with it.
      oc_observer_file="${quoted_path}"
    else
      imports="${imports}aectl platform secret import --path aep/idp-clients/${file} --value-file ${quoted_path}
"
    fi
  fi
done 3<<EOF
$APPS
EOF

echo
if [ -n "$imports" ]; then
  echo "Import each secret into the install's vault, then delete the files:"
  printf '%s' "$imports" | sed 's/^/  /'
fi
if [ -n "$oc_publisher_step" ]; then
  echo "Local dev cluster only: point its build template at this organization with"
  echo "  ${oc_publisher_step}"
fi
if [ -n "$oc_observer_file" ]; then
  printf -v quoted_dir '%q' "$OUT_DIR"
  echo "Local dev cluster only: the observer's secret is in ${oc_observer_file}. Keep the file:"
  echo "  make dev-env AE_DEV_IDP=asgardeo AE_ASGARDEO_SECRETS_DIR=${quoted_dir} ..."
  echo "  installs the logs part of the observability plane when the file is there."
fi
echo "Values for the install:"
echo "  issuer            ${ISSUER}"
echo "  organization      ${TENANT}"
echo "  org namespace     ${NAMESPACE}"
echo "  console client id aep-console-client"

if [ "$failed" = "1" ]; then
  die "one or more applications need fixing: correct the settings listed above in the Asgardeo console, or delete the application there and run this again"
fi
oc_clients=""
[ "$WITH_OC_PUBLISHER" = "1" ] && oc_clients="publisher"
[ "$WITH_OC_OBSERVER" = "1" ] && oc_clients="${oc_clients:+${oc_clients} and }observer"
case "$oc_clients" in
  "") echo "✅ All six applications are set up" ;;
  *" and "*) echo "✅ All six applications and OpenChoreo's ${oc_clients} clients are set up" ;;
  *) echo "✅ All six applications and OpenChoreo's ${oc_clients} client are set up" ;;
esac
if [ -n "$absent" ]; then
  # An existing application's secret cannot be read back. Its file being gone
  # is the normal state after an import; it is a problem only if the secret
  # was never imported, and this script cannot tell which.
  echo
  echo "Note: no secret file in ${OUT_DIR} for:${absent}"
  echo "  That is expected once a secret has been imported and its file deleted."
  echo "  A secret that was never imported cannot be read back: delete that"
  echo "  application in the Asgardeo console and run this again to get a new one."
  echo "  The file of one of OpenChoreo's clients (${OC_PUBLISHER_FILE}, ${OC_OBSERVER_FILE}) is"
  echo "  never imported: a local dev cluster needs it at every make dev-env. If"
  echo "  it is gone, delete that application and run this again."
fi
