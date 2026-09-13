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

# scripts/seed-dev.sh — local-dev convenience that pre-connects the
# `default` org's GitHub PAT and Anthropic API key by POSTing to the
# BFF's Connect endpoints with a Thunder S2S token.
#
# Auto-invoked by start.sh (stage 9) whenever LOCAL_DEV_ADMIN_GITHUB_PAT
# and/or ANTHROPIC_API_KEY are set in .env; with neither set it's skipped
# and the Settings → GitHub / Settings → Anthropic clickthrough applies.
# SKIP_DEV_SEED=1 on start.sh disables the auto-run without touching .env.
# Idempotent: credentials already connected are skipped (each re-connect
# would mint a new SecretReference CR); set SEED_FORCE=1 to rotate them.
# Also fine to run standalone:
#
#   cd deployments && bash scripts/seed-dev.sh
#
# Inputs (read from deployments/.env — each branch is independently optional):
#   LOCAL_DEV_ADMIN_GITHUB_PAT   PAT to register (classic or fine-grained)
#   LOCAL_DEV_ADMIN_GITHUB_OWNER GitHub login the PAT is scoped to
#   ANTHROPIC_API_KEY            Anthropic key to register
#   AEP_CODING_ANTHROPIC_KEY optional; bills the CODING agent to its own
#                                key instead of the one above (ADR-0016)
#
# Knobs (env, with defaults):
#   ENV_FILE                     defaults to deployments/.env
#   ORG_HANDLE                   defaults to "default"
#   BFF_URL                      defaults to http://localhost:9090
#   THUNDER_URL                  defaults to http://thunder.openchoreo.localhost:8080
#   SEEDER_CLIENT_ID             defaults to aep-local-dev-seeder
#   SEEDER_CLIENT_SECRET         defaults to aep-local-dev-seeder-secret
#
# The seeder client is registered by the platform IdP bootstrap
# (single-cluster/thunder-resources/85-aep-local-dev-seeder.yaml). If the
# running IdP predates that document, re-import the bootstrap:
#   THUNDER_REIMPORT=1 bash scripts/setup-thunder.sh
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$SCRIPT_DIR/.."

ENV_FILE="${ENV_FILE:-$DEPLOY_DIR/.env}"
ORG_HANDLE="${ORG_HANDLE:-default}"
BFF_URL="${BFF_URL:-http://localhost:9090}"
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SEEDER_CLIENT_ID="${SEEDER_CLIENT_ID:-aep-local-dev-seeder}"
SEEDER_CLIENT_SECRET="${SEEDER_CLIENT_SECRET:-aep-local-dev-seeder-secret}"

# Whitelist-load a single env key from $ENV_FILE without sourcing the
# whole file (some values legitimately contain spaces / equals signs).
_load_env_key() {
    local key="$1"
    [ -n "${!key:-}" ] && return 0
    [ -f "$ENV_FILE" ] || return 0
    local val
    val=$(grep "^${key}=" "$ENV_FILE" | head -1 | cut -d= -f2-)
    val="${val#\"}"; val="${val%\"}"
    val="${val#\'}"; val="${val%\'}"
    [ -n "$val" ] && export "$key=$val" || true
}
_load_env_key LOCAL_DEV_ADMIN_GITHUB_PAT
_load_env_key LOCAL_DEV_ADMIN_GITHUB_OWNER
_load_env_key ANTHROPIC_API_KEY
_load_env_key AEP_CODING_ANTHROPIC_KEY

GITHUB_PAT="${LOCAL_DEV_ADMIN_GITHUB_PAT:-}"
GITHUB_OWNER="${LOCAL_DEV_ADMIN_GITHUB_OWNER:-}"
ANTHROPIC_KEY="${ANTHROPIC_API_KEY:-}"
# Optional: bills the coding agent to its own key. The SAME variable the
# playground reads, so one line in .env covers both flows.
CODING_ANTHROPIC_KEY="${AEP_CODING_ANTHROPIC_KEY:-}"

echo "=== seed-dev (local-dev convenience) ==="
echo "  BFF:       $BFF_URL"
echo "  Thunder:   $THUNDER_URL"
echo "  Org:       $ORG_HANDLE"

# 1. Pre-flight — both services reachable
if ! curl -fsS --max-time 3 "$BFF_URL/healthz" > /dev/null 2>&1; then
    echo ""
    echo "❌ BFF not reachable at $BFF_URL"
    echo "   Bring the compose stack up first: cd deployments && bash scripts/start.sh"
    exit 1
fi

# 2. Mint S2S token via Thunder client_credentials
echo ""
echo "🔐 Minting seeder token (client=$SEEDER_CLIENT_ID)..."
TOKEN_RESP=$(curl -sS -X POST "${THUNDER_URL%/}/oauth2/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    -d "grant_type=client_credentials" \
    -d "client_id=${SEEDER_CLIENT_ID}" \
    -d "client_secret=${SEEDER_CLIENT_SECRET}" 2>/dev/null || true)
TOKEN=$(printf '%s' "$TOKEN_RESP" \
    | sed -n 's/.*"access_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

if [ -z "$TOKEN" ]; then
    echo "❌ Thunder did not return an access_token."
    echo "   Response (first 200 chars):"
    printf '   %s\n' "$(printf '%s' "$TOKEN_RESP" | head -c 200)"
    echo
    echo "   The most likely cause: the '${SEEDER_CLIENT_ID}' OAuth app is not"
    echo "   registered in the IdP. See the re-import hint in this script's"
    echo "   header comment (THUNDER_REIMPORT=1 bash scripts/setup-thunder.sh)."
    exit 1
fi
echo "✅ Token minted"

# The three org integrations (Anthropic, GitHub, IDP) are one consolidated
# resource now: GET /config reads all sections; PATCH /config writes them
# (docs/design/org-config-consolidation.md). We read once, then send the
# unconnected + env-provided sections as ONE atomic PATCH.

# Read the current config once. Each section (llm|gitProvider|idp) is a null
# JSON value when unconnected, an object when connected.
_config_json=$(curl -fsS --max-time 5 -H "Authorization: Bearer ${TOKEN}" \
    "${BFF_URL%/}/api/v1/config" 2>/dev/null || echo '{}')

# True when the given /config section is already connected (a non-null object).
# Each connect mints a fresh SecretReference CR + OpenBao entry without deleting
# the previous one, so blind re-connects (start.sh auto-seeds every run) would
# accumulate orphans. GET failures / null sections count as unconnected so the
# PATCH still runs. Bypass with SEED_FORCE=1 to rotate an already-connected one.
_section_connected() {
    [ "${SEED_FORCE:-0}" = "1" ] && return 1
    printf '%s' "$_config_json" | grep -qE "\"$1\"[[:space:]]*:[[:space:]]*\{"
}

# Soft-fail atomic PATCH /config with the assembled section body.
_patch_config() {
    local label="$1" body="$2" code resp_tmp
    resp_tmp=$(mktemp -t seed-dev-XXXXXX.json)
    code=$(curl -sS -o "$resp_tmp" -w '%{http_code}' \
        -X PATCH "${BFF_URL%/}/api/v1/config" \
        -H "Authorization: Bearer ${TOKEN}" \
        -H "Content-Type: application/json" \
        -d "$body" 2>/dev/null || echo 000)
    case "$code" in
        2*) echo "✅ ${label} connected (HTTP $code)" ;;
        *)
            echo "⚠️  ${label} connect failed (HTTP $code). Response (first 300 chars):"
            printf '   %s\n' "$(head -c 300 "$resp_tmp")"
            echo "   You can connect manually via the console UI."
            ;;
    esac
    rm -f "$resp_tmp"
}

# 3. Org integrations — Anthropic + GitHub PAT, sent as ONE atomic PATCH /config.
# Only sections whose env vars are set AND aren't already connected are included.
echo ""
_sections=""   # comma-joined JSON section fragments
_labels=""     # human label of what's being connected

if [ -n "$ANTHROPIC_KEY" ]; then
    if _section_connected llm; then
        echo "✅ Anthropic already connected — skipping (SEED_FORCE=1 to re-connect)"
    else
        _sections="${_sections:+$_sections,}$(printf '"llm":{"kind":"anthropic","apiKey":"%s"}' "$ANTHROPIC_KEY")"
        _labels="${_labels:+$_labels + }Anthropic"
    fi
else
    echo "⏭️  ANTHROPIC_API_KEY not set in $ENV_FILE — skipping Anthropic seed"
fi

# The coding-agent key is an OVERRIDE on the key above (ADR-0016), so it can
# only be seeded when the org has a default key — either already connected, or
# being connected by this very PATCH. The BFF applies llm before codingLlm
# within one request, so "both at once" works; "coding alone, no default" is a
# 400 we pre-empt here with a readable message instead.
if [ -n "$CODING_ANTHROPIC_KEY" ]; then
    if _section_connected codingLlm; then
        echo "✅ Coding-agent Anthropic key already connected — skipping (SEED_FORCE=1 to re-connect)"
    elif [ -z "$ANTHROPIC_KEY" ] && ! _section_connected llm; then
        echo "⏭️  AEP_CODING_ANTHROPIC_KEY is set but ANTHROPIC_API_KEY is not — skipping"
        echo "   the coding-agent key overrides the org's Anthropic key; connect that one first"
    else
        _sections="${_sections:+$_sections,}$(printf '"codingLlm":{"kind":"anthropic","apiKey":"%s"}' "$CODING_ANTHROPIC_KEY")"
        _labels="${_labels:+$_labels + }Anthropic (coding agent)"
    fi
else
    echo "⏭️  AEP_CODING_ANTHROPIC_KEY not set in $ENV_FILE — the coding agent will reuse the org's Anthropic key"
fi

if [ -n "$GITHUB_PAT" ] && [ -n "$GITHUB_OWNER" ]; then
    if _section_connected gitProvider; then
        echo "✅ GitHub already connected — skipping (SEED_FORCE=1 to re-connect)"
    else
        _sections="${_sections:+$_sections,}$(printf '"gitProvider":{"kind":"github","mode":"pat","pat":"%s","githubLogin":"%s"}' "$GITHUB_PAT" "$GITHUB_OWNER")"
        _labels="${_labels:+$_labels + }GitHub PAT"
    fi
else
    echo "⏭️  LOCAL_DEV_ADMIN_GITHUB_PAT / LOCAL_DEV_ADMIN_GITHUB_OWNER not set in $ENV_FILE — skipping GitHub seed"
    echo "   To enable, append to $ENV_FILE:"
    echo "       LOCAL_DEV_ADMIN_GITHUB_PAT=<pat>"
    echo "       LOCAL_DEV_ADMIN_GITHUB_OWNER=<github-login>"
fi

if [ -n "$_sections" ]; then
    echo "🔗 Connecting org integrations ($_labels) via one atomic PATCH /config..."
    _patch_config "$_labels" "{${_sections}}"
fi

echo ""
echo "=== seed-dev complete ==="
