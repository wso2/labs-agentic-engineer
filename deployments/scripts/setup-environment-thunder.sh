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

# Usage
#
#   bash setup-environment-thunder.sh <org> <env>
#   ORG_NAME=<org> ENV_NAME=<env> bash setup-environment-thunder.sh
#
# Gives an AEP environment its own Thunder (the second identity tier, "T2") and
# records how AEP reaches it. Idempotent: a second run changes nothing.
#
# Two modes, decided by whether the Helm release already exists:
#
#   CREATE  no release for (org, env) → install upstream thunderid at Agent
#           Manager's pinned chart version with the same shape Agent Manager's
#           add-environment-thunder.sh uses (sqlite, persistence, gateway route on
#           <env>-idp.amp.localhost, platform-IdP trusted issuer, CA bundle
#           mount), with a bootstrap bundle that holds AEP's own documents
#           (single-cluster/thunder-env-resources/) plus the two platform-layer
#           documents every T2 needs. When amp-api answers, the handle is also
#           registered there and Agent Manager's system client is bootstrapped
#           and handed to amp-api, so Agent Manager can bind to this instance.
#   BIND    a release exists (Agent Manager created it, or an earlier run did) →
#           no helm upgrade, nothing of Agent Manager's touched. AEP's system
#           client is ensured by running the chart's own bootstrap importer once
#           more as a plain Job against an AEP-owned ConfigMap (see
#           ensure_aep_system_client), only when a mint proves it missing.
#
# The two products share one environment on the local stack (`default`), and
# setup-aep.sh CREATEs its Thunder before amp-api exists. Agent Manager's own
# add-environment-thunder.sh later runs against that same (org, env) from
# setup-agent-manager-env.sh: an idempotent `helm upgrade --install` that
# registers the handle, stores Agent Manager's system client and imports it.
# CREATE mirrors Agent Manager's values shape exactly so that upgrade is
# values-identical, and the BIND run that follows it re-proves AEP's client.
#
# Both modes end with the same binding record and the same gate: a scope=system
# token minted WITH the resource indicator must reach GET /users with 200.
#
# ── The binding record ──────────────────────────────────────────────────────
# One credential, written to the three places its three consumers can actually
# reach. None of them can read the other two:
#
#   Secret <release>-aep-system-client in the T2 namespace
#       the record of what this instance was given; the source a re-run reuses
#       instead of rotating.
#   Secret thunder-binding-<org>-<env> in thunder-app-operator-system
#       thunder-app-operator watches ConfigMaps cluster-wide but Secrets only in
#       its OWN namespace (informer and RBAC both), so the credential has to be
#       mirrored there. It finds the pair by the labels below, not by name.
#   OpenBao secret/aep/thunder/<org>/<env>
#       aep-api runs OUTSIDE the cluster (docker compose, no kube access). It
#       reads secrets from OpenBao and everything else from the OpenChoreo API,
#       which is why the non-secret half is ALSO projected onto the Environment
#       CR as aep.wso2.com/thunder-* annotations.
#
# ConfigMap thunder-binding-<org>-<env> in the T2 namespace carries the
# non-secret half and names where the other two live (secretName/secretNamespace
# for the operator, secretPath for aep-api).
#
# Inputs (all optional):
#   THUNDER_RELEASE_PREFIX   leading segment of the release/namespace name
#                            (default: thunder). Honoured by Agent Manager's
#                            naming library only once the configurable prefix
#                            ships upstream; until then the library's own
#                            output is used and a debt notice printed.
#   THUNDER_HANDLE           hostname label under amp.localhost (default: <env>-idp)
#   AMP_API_URL              Agent Manager's API (default in env.sh)
#   IDP_TOKEN_URL, IDP_CLIENT_ID, IDP_CLIENT_SECRET
#                            how Agent Manager's ams-auth.sh mints the token for
#                            amp-api (defaults: the platform IdP's token endpoint,
#                            amp-api-client / amp-api-client-secret)
#   PLATFORM_THUNDER_ISSUER / PLATFORM_THUNDER_JWKS_URL / PLATFORM_THUNDER_TOKEN_AUDIENCE
#                            the trusted-issuer triple (defaults: PUBLIC_THUNDER_URL,
#                            https://<PUBLIC_THUNDER_HOST>:8443/oauth2/jwks, urn:wso2:amp)
#   CHART_VERSION            upstream thunderid chart version (default: 1.0.0, Agent
#                            Manager's pin); THUNDER_CHART overrides the chart ref
#   SKIP_CA_BUNDLE_TRUST     true when the platform IdP's certificate is publicly
#                            trusted already (no custom CA bundle mounted)
#   MOZILLA_CA_BUNDLE        path to a pre-downloaded https://curl.se/ca/cacert.pem
#   AM_SCRIPTS_DIR           local Agent Manager checkout's deployments/scripts (see am-scripts.sh)
#   OPENBAO_NS / OPENBAO_POD / OPENBAO_TOKEN
#                            where the binding's OpenBao copy is written
#                            (defaults: openbao / openbao-0 / root)
#   WAIT_TIMEOUT             (default: 180s)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"
source "$SCRIPT_DIR/am-scripts.sh"

load_public_urls "$SCRIPT_DIR/../.env"

ORG_NAME="${1:-${ORG_NAME:-}}"
ENV_NAME="${2:-${ENV_NAME:-}}"
if [ -z "$ORG_NAME" ] || [ -z "$ENV_NAME" ]; then
    echo "usage: $(basename "$0") <org> <env>   (or ORG_NAME=… ENV_NAME=…)" >&2
    exit 2
fi

AEP_RESOURCE_DIR="${SCRIPT_DIR}/../single-cluster/thunder-env-resources"
OPERATOR_NS="thunder-app-operator-system"
CHART="${THUNDER_CHART:-oci://ghcr.io/thunder-id/helm-charts/thunderid}"
CHART_VERSION="${CHART_VERSION:-1.0.0}"
WAIT_TIMEOUT="${WAIT_TIMEOUT:-180s}"
THUNDER_PORT=8090

# Where the binding's OpenBao copy is written. utils.sh `bao` reads these three.
OPENBAO_NS="${OPENBAO_NS:-openbao}"
OPENBAO_POD="${OPENBAO_POD:-openbao-0}"
OPENBAO_TOKEN="${OPENBAO_TOKEN:-root}"
OPENBAO_MOUNT="secret"

# What Agent Manager's sourced helpers read (get_ams_token, register_thunder_url,
# store_via_ams). Same defaults setup-agent-manager-env.sh passes.
export AMP_API_URL
export IDP_TOKEN_URL="${IDP_TOKEN_URL:-${PUBLIC_THUNDER_URL}/oauth2/token}"

# The trusted-issuer triple. The JWKS URL is HTTPS on 8443 on purpose: ThunderID
# rejects a plain-http JWKS URL for a trusted issuer, and the public hostname
# reaches the IdP's HTTPS gateway from inside the cluster through the CoreDNS
# rewrite setup-thunder.sh installs (utils.sh ensure_platform_idp_in_coredns).
PLATFORM_THUNDER_ISSUER="${PLATFORM_THUNDER_ISSUER:-${PUBLIC_THUNDER_URL}}"
PLATFORM_THUNDER_JWKS_URL="${PLATFORM_THUNDER_JWKS_URL:-https://${PUBLIC_THUNDER_HOST}:8443/oauth2/jwks}"
PLATFORM_THUNDER_TOKEN_AUDIENCE="${PLATFORM_THUNDER_TOKEN_AUDIENCE:-urn:wso2:amp}"

# Agent Manager's sourced functions run kubectl against the CURRENT context and
# take no --kube-context, so the context has to be the cluster env.sh names.
current_ctx="$(kubectl config current-context 2>/dev/null || true)"
if [ "$current_ctx" != "$CLUSTER_CONTEXT" ]; then
    echo "❌ kubectl context is '${current_ctx:-<none>}', expected '${CLUSTER_CONTEXT}' (env.sh CLUSTER_CONTEXT)." >&2
    echo "   kubectl config use-context ${CLUSTER_CONTEXT}" >&2
    exit 1
fi

echo "============================================"
echo "  Environment Thunder — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"

# ── Agent Manager's scripts: naming library + provisioning helpers ──────────
# Sourced, not executed: add-environment-thunder.sh guards its main with a
# BASH_SOURCE check and its functions (render_*, register_thunder_url,
# store_via_ams, apply_httproute, patch_ca_bundle_mount, platform_thunder_ca_cert,
# read_existing_secret, generate_admin_password) are exactly the pieces a second
# publisher needs to produce an instance Agent Manager recognises as its own
# shape. It sources thunder-naming.sh and ams-auth.sh from beside itself.
STAGE_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGE_DIR"' EXIT
stage_agent_manager_scripts "$STAGE_DIR"

# The design's product-neutral prefix. Exported before the library is sourced;
# thunder_release_name reads it at call time.
export THUNDER_RELEASE_PREFIX="${THUNDER_RELEASE_PREFIX:-thunder}"
# shellcheck source=/dev/null
source "$STAGE_DIR/add-environment-thunder.sh"

# Agent Manager's own check, applied to the same inputs: everything downstream
# (release, namespace, hostname label, ConfigMap and Secret names) is a DNS-1123
# label built from these two, and a bad one otherwise fails somewhere deep in a
# kubectl apply with the reason several layers away.
for _name in "$ORG_NAME" "$ENV_NAME"; do
    validate_name "$_name" || {
        echo "❌ '${_name}' is not a valid name — lowercase alphanumeric and hyphens, no leading or trailing hyphen." >&2
        exit 1
    }
done
unset _name

RELEASE="$(thunder_release_name "$ORG_NAME" "$ENV_NAME")"
NS="$(thunder_namespace "$ORG_NAME" "$ENV_NAME")"
if [ "${RELEASE#"${THUNDER_RELEASE_PREFIX}-"}" = "$RELEASE" ]; then
    echo ""
    echo "⚠️  DEBT: Agent Manager's thunder-naming.sh at ${AM_REF} ignores THUNDER_RELEASE_PREFIX"
    echo "   (asked for '${THUNDER_RELEASE_PREFIX}', got release '${RELEASE}'). The library is the"
    echo "   truth for names, so this run continues with '${RELEASE}'. The neutral prefix lands"
    echo "   when the upstream ask (configurable env-Thunder release prefix, prototyped on"
    echo "   agent-manager branch thunder-release-prefix) is released and AMP_VERSION moves to it."
fi

BINDING_NAME="thunder-binding-${ORG_NAME}-${ENV_NAME}"
AEP_SECRET_NAME="${RELEASE}-aep-system-client"
AEP_BOOTSTRAP_CM="${RELEASE}-aep-bootstrap"
INSTALL_BOOTSTRAP_CM="${RELEASE}-bootstrap"
ADMIN_URL="http://${RELEASE}-service.${NS}.svc.cluster.local:${THUNDER_PORT}"
MANAGED_BY="setup-environment-thunder.sh"
# Keyed by (org, env), not by release name: aep-api resolves this path from the
# Environment it is acting on, and never learns the Helm release's name.
OPENBAO_PATH="aep/thunder/${ORG_NAME}/${ENV_NAME}"
SECRET_PATH="${OPENBAO_MOUNT}/${OPENBAO_PATH}"

# helm_release_deployed, not a bare `helm status`: `status` succeeds for a
# release in ANY state, so an install this script (or Agent Manager's) left
# `failed` or half-applied would be bound to as though it were serving — and a
# binding record is a promise that an environment HAS a working Thunder. Every
# consumer of that record then fails somewhere else entirely: the operator
# 401s, the gateway trusts an issuer nothing answers on.
#
# The third state is refused rather than repaired. CREATE below is a plain
# `helm install` (the publisher contract: this script never upgrades a release
# it did not create), which cannot re-use a name that is still in use, so
# there is no automatic recovery to offer — only a deliberate one.
if helm_release_deployed "$RELEASE" "$NS"; then
    MODE=BIND
elif helm status "$RELEASE" -n "$NS" >/dev/null 2>&1; then
    MODE=BROKEN
else
    MODE=CREATE
fi
echo ""
echo "  Release:   ${RELEASE}"
echo "  Namespace: ${NS}"
echo "  Mode:      ${MODE}"

if [ "$MODE" = BROKEN ]; then
    rel_state="$(helm status "$RELEASE" -n "$NS" --kube-context "$CLUSTER_CONTEXT" -o json 2>/dev/null \
        | grep -o '"status":"[a-z-]*"' | head -1 | cut -d'"' -f4)"
    echo ""
    echo "❌ Release ${RELEASE} exists in state '${rel_state:-unknown}', not 'deployed'."
    echo "   Binding to it would record a Thunder for ${ORG_NAME}/${ENV_NAME} that is not serving."
    echo "   Inspect it, then re-drive it deliberately:"
    echo "     helm status ${RELEASE} -n ${NS}"
    echo "     helm uninstall ${RELEASE} -n ${NS}   # then re-run this script"
    exit 1
fi

# ── helpers ──────────────────────────────────────────────────────────────────

# apply_labelled <label…> — reads a resource manifest on stdin, stamps the given
# key=value labels onto it and applies it. Re-applying identical content is a
# no-op, which is what makes a second run of this script leave nothing changed.
apply_labelled() {
    local labels=("$@")
    kubectl label --local -f - -o yaml "${labels[@]}" | kubectl apply -f - >/dev/null
}

# bundle_files_json <dir> — the bootstrap.configMap.files list, sorted because
# ThunderID imports in lexical order and the numeric prefixes encode dependencies.
bundle_files_json() {
    python3 -c 'import json, os, sys; print(json.dumps(sorted(os.listdir(sys.argv[1]))))' "$1"
}

# apply_bootstrap_configmap <name> <dir> — one ConfigMap key per file in <dir>.
apply_bootstrap_configmap() {
    local name="$1" dir="$2"
    kubectl create configmap "$name" -n "$NS" --from-file="$dir" --dry-run=client -o yaml \
        | apply_labelled "app.kubernetes.io/managed-by=${MANAGED_BY}" \
                         "aep.wso2.com/org=${ORG_NAME}" "aep.wso2.com/env=${ENV_NAME}"
}

# render_aep_documents <dir> — AEP's publisher bundle (thunder-env-resources/)
# with the per-environment values substituted. Explicit variable list, like
# setup-thunder.sh, so nothing else that looks like a variable is touched.
render_aep_documents() {
    local dir="$1" f
    for f in "${AEP_RESOURCE_DIR}"/*.yaml; do
        AEP_SYSTEM_CLIENT_SECRET="$AEP_SYSTEM_CLIENT_SECRET" ORG_NAME="$ORG_NAME" ENV_NAME="$ENV_NAME" \
            envsubst '${AEP_SYSTEM_CLIENT_SECRET} ${ORG_NAME} ${ENV_NAME}' < "$f" > "${dir}/$(basename "$f")"
    done
}

# render_platform_documents <dir> <issuer> — the two documents EVERY T2 needs
# regardless of who publishes into it, rendered with Agent Manager's own
# functions so they never drift from its shape: the default resource server
# (so a scoped client_credentials request resolves) and the System resource
# server's identifier ("<issuer>/mcp" — ThunderID's shipped default hardcodes
# https://localhost:8090/mcp). Both are declarative upserts of the value the
# instance already holds when Agent Manager created it. They are ALSO required
# in any re-import bundle: the importer re-runs the shipped defaults alongside
# the mounted files, and without 13- the identifier would be reset to the
# localhost default and every resource-indicator mint would fail invalid_target.
render_platform_documents() {
    local dir="$1" issuer="$2"
    render_default_resource_server_config > "${dir}/11-default-resource-server-config.yaml"
    render_system_rs_identifier_fix "$issuer" > "${dir}/13-fix-thunder-system-rs-identifier.yaml"
}

# resolve_aep_secret — AEP's per-environment system-client secret: reuse the one
# in Secret <release>-aep-system-client (never rotate), else mint one.
resolve_aep_secret() {
    if AEP_SYSTEM_CLIENT_SECRET="$(read_existing_secret "$NS" "$AEP_SECRET_NAME")" \
        && [ -n "$AEP_SYSTEM_CLIENT_SECRET" ]; then
        echo "🔐 Reusing existing aep-system-client secret (${AEP_SECRET_NAME})"
    else
        AEP_SYSTEM_CLIENT_SECRET="$(openssl rand -hex 24)"
        echo "🔐 Generated a new aep-system-client secret (${AEP_SECRET_NAME})"
    fi
}

# fetch_mozilla_ca_bundle <dest> — https://curl.se/ca/cacert.pem, checksum
# verified, or the pre-downloaded MOZILLA_CA_BUNDLE. The platform CA is
# APPENDED to this so SSL_CERT_FILE stays a complete trust store, not one that
# trusts only the platform IdP.
fetch_mozilla_ca_bundle() {
    local dest="$1" attempt expected actual
    if [ -n "${MOZILLA_CA_BUNDLE:-}" ] && [ -f "$MOZILLA_CA_BUNDLE" ]; then
        grep -q "BEGIN CERTIFICATE" "$MOZILLA_CA_BUNDLE" \
            || { echo "❌ MOZILLA_CA_BUNDLE is not a PEM bundle: ${MOZILLA_CA_BUNDLE}" >&2; return 1; }
        cp "$MOZILLA_CA_BUNDLE" "$dest"
        return 0
    fi
    for attempt in 1 2 3; do
        if curl -fsSL --connect-timeout 30 https://curl.se/ca/cacert.pem -o "$dest" 2>/dev/null \
            && grep -q "BEGIN CERTIFICATE" "$dest"; then
            expected="$(curl -fsSL --connect-timeout 15 https://curl.se/ca/cacert.pem.sha256 2>/dev/null | awk '{print $1}' || true)"
            actual="$(_sha256 "$dest")"
            if [ -z "$expected" ] || [ "$expected" = "$actual" ]; then
                return 0
            fi
            echo "⚠️  Mozilla CA bundle checksum mismatch (attempt ${attempt}/3)"
        else
            echo "⚠️  Mozilla CA bundle download failed (attempt ${attempt}/3)"
        fi
        [ "$attempt" -lt 3 ] && sleep 5
    done
    echo "❌ Could not fetch https://curl.se/ca/cacert.pem. Download it elsewhere and set MOZILLA_CA_BUNDLE=<path>." >&2
    return 1
}

# ensure_ca_bundle_configmap — Mozilla roots + the platform IdP's CA, as the
# ConfigMap patch_ca_bundle_mount mounts into the Deployment. Skipped when the
# ConfigMap exists (a re-run) — the bundle is ~230KB.
CA_CM_NAME="aep-thunder-platform-ca"
ensure_ca_bundle_configmap() {
    if kubectl get configmap "$CA_CM_NAME" -n "$NS" >/dev/null 2>&1; then
        echo "🔐 CA bundle ConfigMap ${NS}/${CA_CM_NAME} already exists"
        return 0
    fi
    local ca_pem bundle
    if ! ca_pem="$(platform_thunder_ca_cert)" || [ -z "$ca_pem" ]; then
        echo "❌ The platform IdP's CA is not available (Secret amp-local-root-ca-secret in cert-manager)." >&2
        echo "   Without it this Thunder cannot fetch the trusted issuer's HTTPS JWKS. Set PLATFORM_THUNDER_CA_PEM to inject it." >&2
        return 1
    fi
    bundle="$(mktemp)"
    fetch_mozilla_ca_bundle "$bundle" || { rm -f "$bundle"; return 1; }
    printf '\n%s\n' "$ca_pem" >> "$bundle"
    # --from-file, not --from-literal: the bundle exceeds Linux's per-argument limit.
    kubectl create configmap "$CA_CM_NAME" -n "$NS" --from-file=ca-bundle.crt="$bundle" --dry-run=client -o yaml \
        | apply_labelled "app.kubernetes.io/managed-by=${MANAGED_BY}"
    rm -f "$bundle"
    echo "🔐 CA bundle (Mozilla + platform IdP CA) stored in ${NS}/${CA_CM_NAME}"
}

# binding_json — the binding's secret half as a JSON object, in the key names
# aep-api reads. Built by python3 so a generated secret can never break the
# quoting; consumed on stdin by both the comparison and the write below.
binding_json() {
    CLIENT_SECRET="$AEP_SYSTEM_CLIENT_SECRET" ISSUER="$ISSUER" ADMIN_URL="$ADMIN_URL" \
    SYSTEM_RS="$SYSTEM_RS_IDENTIFIER" python3 -c 'import json, os
print(json.dumps({
    "clientId": "aep-system-client",
    "clientSecret": os.environ["CLIENT_SECRET"],
    "issuer": os.environ["ISSUER"],
    "adminURL": os.environ["ADMIN_URL"],
    "systemResourceIdentifier": os.environ["SYSTEM_RS"],
}, sort_keys=True))'
}

# write_openbao_binding — the same record in OpenBao, for aep-api. `kv put` is a
# whole-document replace, so it is only issued when the stored document differs
# from what this run computed; an unchanged environment writes no new version.
write_openbao_binding() {
    if ! kubectl get pod "$OPENBAO_POD" -n "$OPENBAO_NS" >/dev/null 2>&1; then
        echo "❌ OpenBao pod ${OPENBAO_NS}/${OPENBAO_POD} not found — aep-api reads this binding's" >&2
        echo "   credential from ${SECRET_PATH} and has no other way to get it. Run setup-prerequisites.sh." >&2
        return 1
    fi
    local desired stored
    desired="$(binding_json)"
    stored="$(bao kv get -mount="$OPENBAO_MOUNT" -format=json "$OPENBAO_PATH" 2>/dev/null \
        | python3 -c 'import json, sys
try:
    print(json.dumps(json.load(sys.stdin)["data"]["data"], sort_keys=True))
except Exception:
    print("")' || true)"
    if [ "$stored" = "$desired" ]; then
        echo "   OpenBao ${SECRET_PATH} already current"
        return 0
    fi
    printf '%s' "$desired" | bao kv put -mount="$OPENBAO_MOUNT" "$OPENBAO_PATH" - >/dev/null
    echo "   OpenBao ${SECRET_PATH} written"
}

# mint_aep_token <issuer> — prints an access token for aep-system-client, minted
# the way every AEP consumer must: client_secret_post, scope=system AND
# resource=<System RS identifier>. Returns 1 when the token endpoint refuses.
mint_aep_token() {
    local issuer="$1" body
    body="$(curl -sS --max-time 20 -X POST "${issuer}/oauth2/token" \
        --data-urlencode "grant_type=client_credentials" \
        --data-urlencode "client_id=aep-system-client" \
        --data-urlencode "client_secret=${AEP_SYSTEM_CLIENT_SECRET}" \
        --data-urlencode "scope=system" \
        --data-urlencode "resource=${issuer}/mcp" 2>/dev/null)" || return 1
    printf '%s' "$body" | python3 -c 'import json, sys
try:
    tok = json.load(sys.stdin).get("access_token", "")
except ValueError:
    tok = ""
if not tok:
    sys.exit(1)
print(tok)'
}

# jwt_scope <token> — the token's scope claim, decoded without verification (the
# check is about what Thunder put in the token, not whether it is genuine).
jwt_scope() {
    printf '%s' "$1" | cut -d. -f2 | python3 -c 'import base64, json, sys
s = sys.stdin.read().strip()
s += "=" * (-len(s) % 4)
print(json.loads(base64.urlsafe_b64decode(s)).get("scope", ""))'
}

# run_aep_bootstrap_import <issuer> — publishes AEP's bundle into an EXISTING
# Thunder by running the chart's own setup Job once more, as a plain Job.
#
# ThunderID's setup Job is a pre-install hook, so a release created without
# AEP's documents never imports them however often it is upgraded — and
# upgrading someone else's release is off the table anyway. The chart exposes
# no re-import knob of its own. What it does expose is the Job template: render
# it from the release's OWN values (helm get values — so the admin password the
# importer re-asserts is the one the instance already has, not a fresh random
# one) with only the bootstrap ConfigMap swapped for AEP's, strip the hook
# markers, and run it. The Job mounts the release's PVCs and imports through
# Thunder's service layer — declarative upserts, the same property that makes
# the hook re-entrant. setup-thunder.sh does the identical thing for the
# platform IdP; here the ConfigMap is one AEP owns, so nothing of Agent
# Manager's is written to.
#
# backoffLimit 0: a bad document fails once with its logs readable rather than
# retrying into a BackOff.
#
# No rollout restart afterwards, unlike setup-thunder.sh's re-import of the
# platform IdP. That restart exists because `server_config` documents are read
# into Thunder's runtime configuration once, at startup, so a CHANGED one
# imports "successfully" into a process still serving the old value. Nothing in
# an AEP bundle changes a server_config: the only one it carries is
# defaultResourceServer, re-asserted at the value the instance already holds.
# Everything else here — the application, the role, the System resource server's
# identifier — is ordinary resource state, read per request. Restarting a
# Deployment Agent Manager owns to re-assert a value that did not change would
# be a write into someone else's release for no gain.
run_aep_bootstrap_import() {
    local issuer="$1"
    local job="${RELEASE}-aep-bootstrap-import" values chart_version bundle_dir files_json i state
    bundle_dir="$(mktemp -d)"
    render_platform_documents "$bundle_dir" "$issuer"
    render_aep_documents "$bundle_dir"
    apply_bootstrap_configmap "$AEP_BOOTSTRAP_CM" "$bundle_dir"
    files_json="$(bundle_files_json "$bundle_dir")"
    rm -rf "$bundle_dir"

    # Render against the release's actual chart version, not this script's pin.
    chart_version="$(helm get metadata "$RELEASE" -n "$NS" -o json | python3 -c 'import json, sys; print(json.load(sys.stdin)["version"])')"
    values="$(mktemp)"
    helm get values "$RELEASE" -n "$NS" -o yaml > "$values"

    kubectl -n "$NS" delete job "$job" --ignore-not-found --wait=true >/dev/null 2>&1
    echo "   running the bootstrap importer (${job}) with ${AEP_BOOTSTRAP_CM}..."
    helm template "$RELEASE" "$CHART" --version "$chart_version" -n "$NS" -f "$values" \
        --set-string "bootstrap.configMap.name=${AEP_BOOTSTRAP_CM}" \
        --set-json "bootstrap.configMap.files=${files_json}" \
        | JOB_NAME="$job" MANAGED_BY="$MANAGED_BY" python3 -c '
import os, sys, yaml
docs = [d for d in yaml.safe_load_all(sys.stdin) if d]
jobs = [d for d in docs if d["kind"] == "Job"]
if not jobs:
    sys.exit("no setup Job in the rendered chart — the chart layout changed")
job = jobs[0]
job["metadata"]["name"] = os.environ["JOB_NAME"]
job["metadata"].pop("annotations", None)          # drop the helm hook markers
job["metadata"].setdefault("labels", {})["app.kubernetes.io/managed-by"] = os.environ["MANAGED_BY"]
job["spec"]["backoffLimit"] = 0
job["spec"]["template"]["spec"]["restartPolicy"] = "Never"
yaml.safe_dump(job, sys.stdout)
' | kubectl apply -f - >/dev/null
    rm -f "$values"

    for i in $(seq 1 90); do
        state="$(kubectl -n "$NS" get job "$job" -o jsonpath='{.status.succeeded}/{.status.failed}' 2>/dev/null)"
        case "$state" in
            1/*) kubectl -n "$NS" delete job "$job" --ignore-not-found --wait=false >/dev/null 2>&1
                 echo "   ✅ AEP bundle imported"
                 return 0 ;;
            */1) echo "❌ bootstrap import failed:" >&2
                 kubectl -n "$NS" logs "job/$job" --tail=40 >&2 2>/dev/null
                 return 1 ;;
        esac
        sleep 4
    done
    echo "❌ bootstrap import did not finish within 6 minutes (kubectl -n ${NS} logs job/${job})" >&2
    return 1
}

# ensure_aep_system_client <issuer> — aep-system-client can mint on this
# Thunder, or AEP's bundle is imported until it can. The mint is the probe: it
# is exactly what every consumer will do, and it is cheap, so the ~40s importer
# only runs when it is actually missing (a T2 Agent Manager created; an
# interrupted earlier run). In CREATE mode the install hook already imported
# the bundle and the first mint succeeds.
ensure_aep_system_client() {
    local issuer="$1"
    if mint_aep_token "$issuer" >/dev/null 2>&1; then
        echo "✅ aep-system-client is registered on ${issuer}"
        return 0
    fi
    echo "ℹ️  aep-system-client cannot mint on ${issuer} yet — publishing AEP's bundle"
    run_aep_bootstrap_import "$issuer"
}

# ── CREATE ───────────────────────────────────────────────────────────────────
create_release() {
    local handle host issuer amp_present=0
    local am_secret admin_secret_name admin_password bundle_dir files_json

    ensure_ca_bundle_configmap_or_skip() {
        if [ "${SKIP_CA_BUNDLE_TRUST:-false}" = "true" ]; then
            echo "🔐 SKIP_CA_BUNDLE_TRUST=true — no custom CA bundle mounted"
            return 0
        fi
        ensure_ca_bundle_configmap
    }

    # The handle is registered BEFORE any cluster mutation, as Agent Manager does,
    # so a taken handle fails fast. Registration is by HANDLE, never by URL:
    # amp-api's URL path is SSRF-hardened and rejects *.localhost.
    if amp_api_present; then
        amp_present=1
        echo "ℹ️  amp-api answers on ${AMP_API_URL} — registering the handle and Agent Manager's system client"
        handle="$(register_thunder_url "$ORG_NAME" "$ENV_NAME" "${THUNDER_HANDLE:-${ENV_NAME}-idp}")" || exit 1
    else
        handle="${THUNDER_HANDLE:-${ENV_NAME}-idp}"
        echo "ℹ️  amp-api is not answering on ${AMP_API_URL} — using handle '${handle}' locally,"
        echo "   skipping the thunder-url registration and the system-client hand-off."
    fi
    host="$(thunder_host "$handle")"
    issuer="$(thunder_issuer "$handle")"
    echo "  Issuer:    ${issuer}"

    # Ownership markers, on the namespace, written BEFORE the install: they are
    # what remove-environment-thunder.sh reads to decide whether it may
    # uninstall this release and delete this namespace, and an interrupted run
    # has to leave behind something that still says "AEP's". The binding
    # ConfigMap carries the same release-created-by label further down, but it
    # is written last and cannot answer for a run that never got there.
    local ns_created=false
    kubectl get namespace "$NS" >/dev/null 2>&1 || ns_created=true
    kubectl create namespace "$NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    if [ "$ns_created" = true ]; then
        kubectl label namespace "$NS" "aep.wso2.com/namespace-created-by=${MANAGED_BY}" --overwrite >/dev/null
    fi
    kubectl label namespace "$NS" "aep.wso2.com/release-created-by=${MANAGED_BY}" --overwrite >/dev/null
    ensure_ca_bundle_configmap_or_skip

    resolve_aep_secret

    bundle_dir="$(mktemp -d)"
    render_platform_documents "$bundle_dir" "$issuer"
    render_aep_documents "$bundle_dir"
    # AEP's own bundle, kept apart from the install-time ConfigMap so a later
    # re-import (ensure_aep_system_client) has an AEP-owned source to mount.
    apply_bootstrap_configmap "$AEP_BOOTSTRAP_CM" "$bundle_dir"

    if [ "$amp_present" = 1 ]; then
        # Agent Manager's client, exactly as its own script would bootstrap it,
        # handed to amp-api so Agent Manager finds this instance as its own.
        if am_secret="$(read_existing_secret "$NS" "${RELEASE}-system-client")" && [ -n "$am_secret" ]; then
            echo "🔐 Reusing existing Agent Manager system-client secret"
        else
            am_secret="$(openssl rand -hex 24)"
            kubectl create secret generic "${RELEASE}-system-client" -n "$NS" \
                --from-literal=client-secret="$am_secret" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
        fi
        store_via_ams "$ORG_NAME" "$ENV_NAME" "amp-system-client" "$am_secret" || exit 1
        render_system_client_bootstrap_resource "$am_secret" > "${bundle_dir}/10-amp-system-client.yaml"
        render_system_client_role > "${bundle_dir}/12-amp-system-client-thunder-admin-role.yaml"
    fi
    apply_bootstrap_configmap "$INSTALL_BOOTSTRAP_CM" "$bundle_dir"
    files_json="$(bundle_files_json "$bundle_dir")"
    rm -rf "$bundle_dir"
    echo "🔐 Bootstrap ConfigMap ${INSTALL_BOOTSTRAP_CM} prepared: ${files_json}"

    # The instance's native superadmin, stored where Agent Manager's script looks
    # for it, so a later run of either script reuses it instead of rotating it.
    admin_secret_name="${RELEASE}-admin-credentials"
    if admin_password="$(read_existing_secret "$NS" "$admin_secret_name" "password")" && [ -n "$admin_password" ]; then
        echo "🔐 Reusing existing admin password (${admin_secret_name})"
    else
        admin_password="${THUNDER_ADMIN_PASSWORD:-$(generate_admin_password)}"
        kubectl create secret generic "$admin_secret_name" -n "$NS" \
            --from-literal=password="$admin_password" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
        echo "🔐 Stored a new admin password in Secret ${NS}/${admin_secret_name}"
    fi

    # Same values Agent Manager's add-environment-thunder.sh sets, in the same
    # order, so an instance created here is indistinguishable from one it
    # created (its later bind is a values-identical upgrade). Kept in step by
    # hand — the list lives inside its main() and cannot be sourced.
    # configuration.cors.allowedOrigins is carried for parity; ThunderID 1.0.0's
    # deployment.yaml has no CORS section, so only a `cors` server_config
    # document actually sets origins (see thunder-resources/89-platform-cors-config.yaml).
    # AEP publishes no `cors` document into a T2, and that is now a positive
    # choice rather than an assumption that nothing needs one: browsers DO talk
    # to an environment Thunder — every deployed SPA fetches its discovery
    # document and exchanges its code from JavaScript — but their origins are
    # per-app, born at deploy time, and known only to the ThunderApplication CR
    # that carries the redirect URIs. So the thunder-app operator projects them
    # into this instance's WRITABLE cors layer at runtime
    # (resource-types/thunder-app/operator/internal/thunder/cors.go), which the
    # bootstrap-declared readOnly layer never collides with. A `cors` document
    # here would be a static list that could not name any of them.
    local set_args=(
        --set-string "fullnameOverride=${RELEASE}"
        --set-string "deployment.image.tag=${CHART_VERSION}"
        --set "deployment.replicaCount=1"
        --set "deployment.securityContext.readOnlyRootFilesystem=false"
        --set "hpa.enabled=false"
        --set "ingress.enabled=false"
        --set-string "configuration.server.publicUrl=${issuer}"
        --set "configuration.server.httpOnly=true"
        --set-string "configuration.jwt.issuer=${issuer}"
        --set-string "configuration.gateClient.hostname=${host}"
        --set "configuration.gateClient.port=8080"
        --set-string "configuration.gateClient.scheme=http"
        --set "configuration.database.config.type=sqlite"
        --set "configuration.database.runtime_transient.type=sqlite"
        --set "configuration.database.entity.type=sqlite"
        --set "configuration.database.runtime_persistent.type=sqlite"
        --set "configuration.consent.database.type=sqlite"
        --set "configuration.cache.disabled=false"
        --set "configuration.cors.allowedOrigins={http://localhost:3000,http://console.amp.localhost:8080,${PLATFORM_THUNDER_ISSUER}}"
        --set "persistence.enabled=true"
        --set "persistence.size=${PERSISTENCE_SIZE:-1Gi}"
        --set-string "setup.admin.username=admin"
        --set-string "setup.admin.password=${admin_password}"
        --set-string "bootstrap.configMap.name=${INSTALL_BOOTSTRAP_CM}"
        --set-json "bootstrap.configMap.files=${files_json}"
        --set-string "configuration.server.security.trustedIssuer.issuer=${PLATFORM_THUNDER_ISSUER}"
        --set-string "configuration.server.security.trustedIssuer.jwksUrl=${PLATFORM_THUNDER_JWKS_URL}"
        --set-string "configuration.server.security.trustedIssuer.audience=${PLATFORM_THUNDER_TOKEN_AUDIENCE}"
    )
    if [ -n "${STORAGE_CLASS:-}" ]; then
        set_args+=(--set-string "persistence.storageClass=${STORAGE_CLASS}")
    fi

    echo ""
    echo "📦 Installing ${RELEASE} from ${CHART} ${CHART_VERSION}..."
    # `helm install`, not `upgrade --install`: this branch only runs when the
    # release does not exist, and the publisher contract is that AEP never
    # upgrades a release it did not create.
    helm install "$RELEASE" "$CHART" --version "$CHART_VERSION" \
        --namespace "$NS" --create-namespace "${set_args[@]}"

    if [ "${SKIP_CA_BUNDLE_TRUST:-false}" != "true" ]; then
        echo "🔐 Mounting the CA bundle into the Deployment..."
        patch_ca_bundle_mount "$RELEASE" "$NS" "$CA_CM_NAME"
    fi

    echo "🌐 Routing ${host}:8080 to ${RELEASE}..."
    apply_httproute "$RELEASE" "$NS" "$host" "$THUNDER_PORT"

    echo "⏳ Waiting for ${RELEASE} to be ready..."
    kubectl wait --for=condition=available --timeout="$WAIT_TIMEOUT" \
        deployment -l "app.kubernetes.io/instance=${RELEASE}" -n "$NS" >/dev/null
    echo "✅ ${RELEASE} is ready"

    RELEASE_CREATED_BY="$MANAGED_BY"
}

# ── BIND ─────────────────────────────────────────────────────────────────────
bind_release() {
    echo "ℹ️  Release exists — binding to it. No helm upgrade; nothing Agent Manager created is touched."
    resolve_aep_secret
    # Who created the release is remembered on the namespace by the run that
    # created it, with the binding ConfigMap's copy as the backstop for an
    # environment provisioned before the namespace label existed. A release
    # found without either was created elsewhere.
    RELEASE_CREATED_BY="$(kubectl get namespace "$NS" \
        -o jsonpath='{.metadata.labels.aep\.wso2\.com/release-created-by}' 2>/dev/null || true)"
    if [ -z "$RELEASE_CREATED_BY" ]; then
        RELEASE_CREATED_BY="$(kubectl get configmap "$BINDING_NAME" -n "$NS" \
            -o jsonpath='{.metadata.labels.aep\.wso2\.com/release-created-by}' 2>/dev/null || true)"
    fi
    RELEASE_CREATED_BY="${RELEASE_CREATED_BY:-external}"
}

RELEASE_CREATED_BY=""
if [ "$MODE" = CREATE ]; then
    create_release
else
    bind_release
fi

# ── The instance as it actually is ───────────────────────────────────────────
# Read back from the release's values in BOTH modes — the binding records what
# the instance was installed with, not what this script would have chosen.
RELEASE_VALUES="$(helm get values "$RELEASE" -n "$NS" -o json)"
read_value() {
    printf '%s' "$RELEASE_VALUES" | python3 -c 'import json, sys
v = json.load(sys.stdin)
for k in sys.argv[1].split("."):
    v = v.get(k, {}) if isinstance(v, dict) else {}
print(v if isinstance(v, str) else "")' "$1"
}
ISSUER="$(read_value configuration.server.publicUrl)"
[ -n "$ISSUER" ] || { echo "❌ release ${RELEASE} has no configuration.server.publicUrl — not a Thunder this script understands" >&2; exit 1; }
PUBLIC_HOST="${ISSUER#*://}"; PUBLIC_HOST="${PUBLIC_HOST%%/*}"; PUBLIC_HOST="${PUBLIC_HOST%%:*}"
TRUSTED_ISSUER="$(read_value configuration.server.security.trustedIssuer.issuer)"
TRUSTED_JWKS="$(read_value configuration.server.security.trustedIssuer.jwksUrl)"
TRUSTED_AUDIENCE="$(read_value configuration.server.security.trustedIssuer.audience)"
SYSTEM_RS_IDENTIFIER="${ISSUER}/mcp"
if [ -z "$TRUSTED_ISSUER" ]; then
    echo "⚠️  ${RELEASE} has no trusted issuer configured — platform IdP tokens will be rejected there."
fi

# ── AEP's system client on that instance ─────────────────────────────────────
echo ""
ensure_aep_system_client "$ISSUER"

# ── The binding record ───────────────────────────────────────────────────────
echo ""
echo "📝 Writing the binding record"
kubectl create namespace "$OPERATOR_NS" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
binding_labels=(
    "aep.wso2.com/kind=thunder-binding"
    "aep.wso2.com/org=${ORG_NAME}"
    "aep.wso2.com/env=${ENV_NAME}"
    "app.kubernetes.io/managed-by=${MANAGED_BY}"
)
# The Secret in the T2 namespace, and its mirror where thunder-app-operator can
# read it: the operator's Secret informer and RBAC are restricted to its own
# namespace, deliberately, so a binding has to be delivered there. It selects on
# aep.wso2.com/kind=thunder-binding, so the labels are the contract, not the name.
for target in "${AEP_SECRET_NAME}:${NS}" "${BINDING_NAME}:${OPERATOR_NS}"; do
    kubectl create secret generic "${target%%:*}" -n "${target##*:}" \
        --from-literal=client-id=aep-system-client \
        --from-literal=client-secret="$AEP_SYSTEM_CLIENT_SECRET" \
        --from-literal=issuer="$ISSUER" \
        --from-literal=admin-url="$ADMIN_URL" \
        --from-literal=system-resource-identifier="$SYSTEM_RS_IDENTIFIER" \
        --dry-run=client -o yaml | apply_labelled "${binding_labels[@]}"
done
echo "   Secret ${NS}/${AEP_SECRET_NAME} (mirrored to ${OPERATOR_NS}/${BINDING_NAME})"

# The third copy: aep-api runs outside the cluster and cannot read either Secret.
write_openbao_binding

# The non-secret half. aep-api sees only the OpenChoreo API, so the same fields
# are also projected onto the Environment CR as annotations below.
kubectl create configmap "$BINDING_NAME" -n "$NS" \
    --from-literal=issuer="$ISSUER" \
    --from-literal=adminURL="$ADMIN_URL" \
    --from-literal=systemResourceIdentifier="$SYSTEM_RS_IDENTIFIER" \
    --from-literal=secretName="$BINDING_NAME" \
    --from-literal=secretNamespace="$OPERATOR_NS" \
    --from-literal=secretPath="$SECRET_PATH" \
    --from-literal=trustedIssuer="$TRUSTED_ISSUER" \
    --from-literal=trustedIssuerJwksUrl="$TRUSTED_JWKS" \
    --from-literal=trustedIssuerAudience="$TRUSTED_AUDIENCE" \
    --from-literal=release="$RELEASE" \
    --from-literal=org="$ORG_NAME" \
    --from-literal=env="$ENV_NAME" \
    --from-literal=publicHost="$PUBLIC_HOST" \
    --dry-run=client -o yaml \
    | apply_labelled "${binding_labels[@]}" "aep.wso2.com/release-created-by=${RELEASE_CREATED_BY}"
echo "   ConfigMap ${NS}/${BINDING_NAME}"

if kubectl get environment "$ENV_NAME" -n "$ORG_NAME" >/dev/null 2>&1; then
    kubectl annotate environment "$ENV_NAME" -n "$ORG_NAME" --overwrite \
        "aep.wso2.com/thunder-issuer=${ISSUER}" \
        "aep.wso2.com/thunder-admin-url=${ADMIN_URL}" \
        "aep.wso2.com/thunder-system-resource-identifier=${SYSTEM_RS_IDENTIFIER}" \
        "aep.wso2.com/thunder-secret-path=${SECRET_PATH}" \
        "aep.wso2.com/thunder-binding=${BINDING_NAME}" >/dev/null
    echo "   Environment ${ORG_NAME}/${ENV_NAME} annotated"
else
    echo "   ⚠️  OpenChoreo Environment ${ORG_NAME}/${ENV_NAME} does not exist yet — annotations skipped."
    echo "      Re-run this script once it exists to project the binding onto it."
fi

# ── Gate ─────────────────────────────────────────────────────────────────────
# The mint every consumer performs, asserted end to end: the token carries
# `system`, and Thunder's admin API accepts it.
echo ""
echo "🔎 Verifying aep-system-client on ${ISSUER}"
if ! TOKEN="$(mint_aep_token "$ISSUER")"; then
    echo "❌ aep-system-client could not mint a token from ${ISSUER}/oauth2/token" >&2
    echo "   (client_secret_post, scope=system, resource=${SYSTEM_RS_IDENTIFIER})." >&2
    exit 1
fi
SCOPE="$(jwt_scope "$TOKEN")"
case " ${SCOPE} " in
    *" system "*) ;;
    *)
        echo "❌ Token minted but its scope is '${SCOPE}', not 'system'." >&2
        echo "   This is the resource-indicator trap: the request named resource=${SYSTEM_RS_IDENTIFIER}," >&2
        echo "   so either that is not this instance's System resource server identifier (compare" >&2
        echo "   ${INSTALL_BOOTSTRAP_CM}/13-fix-thunder-system-rs-identifier.yaml) or the role granting" >&2
        echo "   aep-system-client the system permission (84-aep-system-role.yaml) did not import." >&2
        exit 1 ;;
esac
USERS_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 "${ISSUER}/users" -H "Authorization: Bearer ${TOKEN}")"
if [ "$USERS_CODE" != "200" ]; then
    echo "❌ GET ${ISSUER}/users returned ${USERS_CODE} with a scope=system token (expected 200)." >&2
    exit 1
fi
echo "✅ scope=system token minted with resource=${SYSTEM_RS_IDENTIFIER}; GET /users → 200"

echo ""
echo "============================================"
echo "  ✅ Environment Thunder bound — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"
echo "  Mode:            ${MODE}"
echo "  Release:         ${RELEASE}"
echo "  Namespace:       ${NS}"
echo "  Issuer:          ${ISSUER}"
echo "  Admin URL:       ${ADMIN_URL}"
echo "  System RS:       ${SYSTEM_RS_IDENTIFIER}"
echo "  Trusted issuer:  ${TRUSTED_ISSUER:-<none>}"
echo "  Binding:         configmap/${BINDING_NAME} in ${NS}"
echo "  Secret:          secret/${AEP_SECRET_NAME} in ${NS}, mirrored to secret/${BINDING_NAME} in ${OPERATOR_NS}"
echo "  Secret path:     ${SECRET_PATH} (OpenBao)"
echo "  Release owner:   ${RELEASE_CREATED_BY}"
