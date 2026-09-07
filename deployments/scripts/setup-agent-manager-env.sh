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

# Second half of the Agent Manager install: Agent Manager joins the `default`
# environment — the one AEP already provisioned (setup-aep.sh), with its own
# Thunder and the API Platform gateway in front of it — by registering that
# environment's Thunder as its own.
#
# The gateway half is NOT installed here. It is one step —
# setup-environment-gateway.sh — shared with setup-aep.sh, because a
# per-environment gateway is platform infrastructure that exists whether or not
# Agent Manager does; this script only names the (org, env) and lets that script
# install or bind. See its header for the naming and the binding contract.
#
# Split from setup-agent-manager.sh because both steps talk to amp-api over its
# PUBLIC url and drive Agent Manager's own admin API — they need the platform to
# be not just installed but serving, and they fail in ways that have nothing to
# do with the chart installs before them. Keeping them separate means a failure
# here does not leave the core install looking broken.
#
# ── The second Thunder tier ─────────────────────────────────────────────────
#
# This is NOT the shared platform IdP. Agent Manager runs one Thunder per
# environment, for AgentID / workload identity, and pulls the UPSTREAM
# `thunderid` chart directly at its own pinned version — deliberately decoupled
# from AMP_VERSION and from whatever the platform tier happens to run. The
# API Platform gateway consumes it as a ThunderKeyManager to validate JWTs on
# agent endpoints.
#
# Convergence does not touch this tier. Only the platform tier is shared,
# because OpenChoreo's control plane has exactly one OIDC issuer.
#
# The one edge between the tiers is trust: each environment Thunder accepts the
# platform IdP as a trusted issuer, so a platform login can reach an
# environment's APIs. That trust is configured here (PLATFORM_THUNDER_*) and
# depends on the platform IdP's public hostname reaching its HTTPS gateway from
# inside the cluster, which setup-thunder.sh arranges (utils.sh
# ensure_platform_idp_in_coredns).

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"
source "$SCRIPT_DIR/am-scripts.sh"

load_public_urls "$SCRIPT_DIR/../.env"

ENV_NAME="${ENV_NAME:-default}"
ORG_NAME="${ORG_NAME:-default}"
AMP_API_URL="${AMP_API_URL:-http://api.amp.localhost:8080/api/v1}"
IDP_TOKEN_URL="${IDP_TOKEN_URL:-${PUBLIC_THUNDER_URL}/oauth2/token}"
# AM_REF / AM_SCRIPT_BASE come from am-scripts.sh, which also owns the staging.
# The gateway's names are not spelled here at all — setup-environment-gateway.sh
# derives every one of them from (org, env) and the chart's own helpers.

echo "============================================"
echo "  Agent Manager — '${ENV_NAME}' environment"
echo "============================================"

# amp-api has to be answering on its public URL, not merely Ready: the scripts
# below register resources through it.
echo ""
echo "⏳ Waiting for amp-api on ${AMP_API_URL}..."
for _ in $(seq 1 60); do
    if curl -fsS -o /dev/null -w '' "${AMP_API_URL%/api/v1}/health" 2>/dev/null \
       || curl -fsS -o /dev/null "${AMP_API_URL}" 2>/dev/null; then
        break
    fi
    sleep 5
done
echo "✅ amp-api reachable"

# ── 1. The environment's own Thunder ────────────────────────────────────────
# Fetched from Agent Manager's release rather than vendored: the script and its
# two helper libraries are versioned together with the charts, and a stale copy
# here would drift from the chart it provisions against. CHART_VERSION is left
# unset on purpose so the script pins its own validated ThunderID release —
# AMP_VERSION has no bearing on which ThunderID an env-Thunder runs.
#
# The release already exists when this runs: setup-aep.sh created it for the
# shared `default` environment before amp-api was up, so the handle was never
# registered and Agent Manager's system client never imported. Agent Manager's
# script is an idempotent `helm upgrade --install`: this is where it registers
# the handle with amp-api, stores its system client and imports it, through a
# values-identical upgrade of AEP's release — setup-environment-thunder.sh
# mirrors Agent Manager's values shape exactly so that this upgrade changes
# nothing else. (The one Agent Manager-owned deviation is the ORDER: a fresh
# Agent Manager-first environment would have been created by this step.)
echo ""
echo "1️⃣  Provisioning the environment's Thunder"
# Staged through am-scripts.sh, which is the ONE place in this repo that knows
# how to obtain Agent Manager's environment-Thunder scripts (local checkout via
# AM_SCRIPTS_DIR, else fetched at AM_REF) and puts all four of them in one
# directory as siblings, because they source each other from beside themselves.
AM_STAGE_DIR="$(mktemp -d)"
trap 'rm -rf "$AM_STAGE_DIR"' EXIT
stage_agent_manager_scripts "$AM_STAGE_DIR"
ENV_THUNDER_SCRIPT="$AM_STAGE_DIR/add-environment-thunder.sh"

# The environment Thunder trusts the platform IdP as an issuer, and the script
# takes that IdP's coordinates from PLATFORM_THUNDER_ISSUER / _JWKS_URL — with
# defaults naming Agent Manager's own thunder.amp.localhost, which this
# deployment does not publish. Without these two the env-Thunder is configured
# to trust an issuer that never signs anything, and the failure is a later 401
# on the environment's APIs with nothing pointing back here.
#
# The JWKS URL is HTTPS on 8443 on purpose: ThunderID rejects a plain-http JWKS
# URL for a trusted issuer. The public hostname is what the certificate is
# issued for, and it reaches the IdP's HTTPS gateway from inside the cluster
# only through the CoreDNS rewrite setup-thunder.sh installs.
#
# The CA that signed that certificate is read by the script itself. It waits on
# the Certificate under its chart's default name (amp-thunder-extension-local-tls)
# only when that object exists; with the neutral release name (env.sh) it does
# not, so the wait is skipped and the script falls through to reading the
# chart's root CA secret directly — the same root the gateway's certificate
# chains to, and one the chart hardcodes regardless of release name. The wait
# itself moved to setup-thunder.sh, so the CA is issued before this runs.
ENV_NAME="$ENV_NAME" \
DISPLAY_NAME="${DISPLAY_NAME:-Default}" \
ORG_NAME="$ORG_NAME" \
THUNDER_HANDLE="${THUNDER_HANDLE:-${ENV_NAME}-idp}" \
AMP_API_URL="$AMP_API_URL" \
IDP_TOKEN_URL="$IDP_TOKEN_URL" \
PLATFORM_THUNDER_ISSUER="${PUBLIC_THUNDER_URL}" \
PLATFORM_THUNDER_JWKS_URL="https://${PUBLIC_THUNDER_HOST}:8443/oauth2/jwks" \
SCRIPT_BASE_URL="$AM_SCRIPT_BASE" \
    bash "$ENV_THUNDER_SCRIPT"
echo "✅ environment Thunder provisioned"

# ── 2. The binding record for that Thunder ──────────────────────────────────
# Agent Manager's script does not write AEP's binding record.
# setup-environment-thunder.sh does; finding the release already there it BINDs
# — no helm from AEP's side — re-proves that AEP's own client still mints after
# Agent Manager's upgrade (re-importing it if not), and re-projects the record.
# That record is what the gateway step below and thunder-app-operator and
# aep-api all read.
echo ""
echo "2️⃣  Binding record"
bash "$SCRIPT_DIR/setup-environment-thunder.sh" "$ORG_NAME" "$ENV_NAME"

# ── 3. The API Platform gateway for that environment ────────────────────────
# One step, shared with setup-aep.sh. Everything that used to be inline here —
# the namespace and its egress label, the per-namespace encryption key, the
# ThunderKeyManager wiring, the waits — lives there, and the keymanager comes
# from the binding written above rather than from a name derivation performed
# twice.
echo ""
echo "3️⃣  API Platform gateway"
bash "$SCRIPT_DIR/setup-environment-gateway.sh" "$ORG_NAME" "$ENV_NAME"

echo ""
echo "✅ '${ENV_NAME}' environment ready — Thunder, binding record and API Platform gateway"
