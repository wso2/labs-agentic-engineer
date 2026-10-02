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

# Sourced, not run. Signs a local user in to the console's Thunder client and
# leaves a curl header file carrying the user's access token, because /api/v1
# accepts user tokens only: a client_credentials token is refused there.
#
# The login is the console's own flow, driven without a browser:
# /oauth2/authorize (PKCE) → /flow/execute (username + password) →
# /oauth2/auth/callback → /oauth2/token. The user defaults to the local admin
# that deployments/scripts/setup-env-for-aectl.sh creates; AEP_USER and
# AEP_PASSWORD override it.
#
# Neither the password nor the token reaches an argv or the terminal: both
# travel through environment variables into jq and over stdin to curl, and the
# token is written to a 0600 file that the caller's curl reads with -H @file.
#
# Inputs (all defaulted): THUNDER_URL, CONSOLE_URL (whose /callback is a
# redirect URI registered on the client; nothing is served there),
# CONSOLE_CLIENT_ID, AEP_USER, AEP_PASSWORD. Requires curl, jq and openssl.

_LOGIN_LIB_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The fixed local admin pair, read from the script that sets it, so a change
# there cannot leave this one signing in with a stale password.
_local_admin() {
    sed -n "s/^$1=\"\(.*\)\"$/\1/p" "${_LOGIN_LIB_DIR}/../../deployments/scripts/setup-env-for-aectl.sh" 2> /dev/null
}

# thunder_user_auth_header <file>: writes "Authorization: Bearer <token>" to
# <file> (mode 0600). Returns non-zero with a reason on stderr on any failure.
thunder_user_auth_header() {
    local out="$1"
    local thunder="${THUNDER_URL%/}"
    local client="${CONSOLE_CLIENT_ID:-aep-console-client}"
    local redirect="${CONSOLE_URL:-http://console.ae.localhost:8080}"
    redirect="${redirect%/}/callback"

    local user="${AEP_USER:-$(_local_admin THUNDER_ADMIN_USER)}"
    if [ -z "$user" ]; then
        echo "❌ No user to sign in as: set AEP_USER and AEP_PASSWORD." >&2
        return 1
    fi
    local verifier challenge location exec_id auth_id step code
    verifier="$(openssl rand -hex 32)"
    challenge="$(printf '%s' "$verifier" | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '=')"

    location="$(curl -sS -o /dev/null -w '%{redirect_url}' -G "${thunder}/oauth2/authorize" \
        --data-urlencode response_type=code \
        --data-urlencode "client_id=${client}" \
        --data-urlencode "redirect_uri=${redirect}" \
        --data-urlencode scope=openid \
        --data-urlencode state=cli \
        --data-urlencode "code_challenge=${challenge}" \
        --data-urlencode code_challenge_method=S256 2> /dev/null)" || true
    exec_id="$(printf '%s' "$location" | sed -n 's/.*[?&]executionId=\([^&]*\).*/\1/p')"
    auth_id="$(printf '%s' "$location" | sed -n 's/.*[?&]authId=\([^&]*\).*/\1/p')"
    if [ -z "$exec_id" ] || [ -z "$auth_id" ]; then
        echo "❌ Thunder at ${thunder} did not start a sign-in for '${client}'." >&2
        echo "   Redirected to: ${location:-nothing}" >&2
        echo "   Is ${redirect} a redirect URI of that client? Set CONSOLE_URL to the console it was registered for." >&2
        return 1
    fi

    # The first step only returns the credential prompt and its challenge token.
    step="$(jq -n --arg e "$exec_id" '{executionId: $e}' \
        | curl -sS -X POST "${thunder}/flow/execute" -H 'Content-Type: application/json' -d @- 2> /dev/null)" || true
    local next_id challenge_token
    next_id="$(jq -r '.executionId // empty' <<< "$step" 2> /dev/null)"
    challenge_token="$(jq -r '.challengeToken // empty' <<< "$step" 2> /dev/null)"
    if [ -z "$next_id" ]; then
        echo "❌ Thunder's sign-in flow did not answer with a credential prompt." >&2
        return 1
    fi

    step="$(LOGIN_EXEC="$next_id" LOGIN_CHALLENGE="$challenge_token" LOGIN_USER="$user" \
        LOGIN_PASSWORD="${AEP_PASSWORD:-$(_local_admin THUNDER_ADMIN_PASSWORD)}" \
        jq -n '{executionId: env.LOGIN_EXEC, challengeToken: env.LOGIN_CHALLENGE, action: "action_001",
                inputs: {username: env.LOGIN_USER, password: env.LOGIN_PASSWORD}}' \
        | curl -sS -X POST "${thunder}/flow/execute" -H 'Content-Type: application/json' -d @- 2> /dev/null)" || true
    if [ "$(jq -r '.flowStatus // empty' <<< "$step" 2> /dev/null)" != "COMPLETE" ]; then
        echo "❌ Thunder refused the sign-in for '${user}' (flow status: $(jq -r '.flowStatus // "none"' <<< "$step" 2> /dev/null))." >&2
        echo "   Set AEP_USER / AEP_PASSWORD if this stack's admin is not the setup-env-for-aectl.sh default." >&2
        return 1
    fi

    code="$(LOGIN_AUTH="$auth_id" LOGIN_ASSERTION="$(jq -r '.assertion // empty' <<< "$step")" \
        jq -n '{authId: env.LOGIN_AUTH, assertion: env.LOGIN_ASSERTION}' \
        | curl -sS -X POST "${thunder}/oauth2/auth/callback" -H 'Content-Type: application/json' -d @- 2> /dev/null \
        | jq -r '.redirect_uri // empty' 2> /dev/null \
        | sed -n 's/.*[?&]code=\([^&]*\).*/\1/p')" || true
    if [ -z "$code" ]; then
        echo "❌ Thunder completed the sign-in but issued no authorization code." >&2
        return 1
    fi

    local token
    token="$(printf 'grant_type=authorization_code&client_id=%s&code=%s&redirect_uri=%s&code_verifier=%s' \
        "$client" "$code" "$(jq -rn --arg r "$redirect" '$r|@uri')" "$verifier" \
        | curl -sS -X POST "${thunder}/oauth2/token" \
            -H 'Content-Type: application/x-www-form-urlencoded' -d @- 2> /dev/null \
        | jq -r '.access_token // empty' 2> /dev/null)" || true
    if [ -z "$token" ]; then
        echo "❌ Thunder did not exchange the authorization code for an access token." >&2
        return 1
    fi

    (umask 077 && printf 'Authorization: Bearer %s\n' "$token" > "$out")
}
