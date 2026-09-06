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
#   bash remove-environment-thunder.sh <org> <env> [--yes]
#   ORG_NAME=<org> ENV_NAME=<env> FORCE=1 bash remove-environment-thunder.sh
#
# Undoes setup-environment-thunder.sh and setup-environment-gateway.sh for ONE
# environment — the identity tier and the gateway that terminates against it,
# because neither is useful without the other. Call it before removing the
# environment itself.
#
# The order mirrors Agent Manager's own remove-environment-thunder.sh (release →
# HTTPRoute → namespace → the registrations in agent-manager-service), with
# AEP's objects removed in the reverse of the order they were created and the
# best-effort steps kept best-effort, so an environment can always be torn down
# even when Agent Manager or OpenBao is unreachable.
#
# ── The guard ───────────────────────────────────────────────────────────────
#
# The publisher contract cuts both ways: a publisher never provisions on top of
# another's release, and never REMOVES one either. So a Helm release is only
# uninstalled when this repo's scripts recorded that they installed it —
# `aep.wso2.com/release-created-by` on the namespace, written at install time by
# setup-environment-thunder.sh / setup-environment-gateway.sh (with the T2
# binding ConfigMap's own copy of the label as a backstop). Anything else, and
# only AEP's own artefacts are withdrawn: its system client and role on the
# instance, its Secrets and ConfigMaps, the OpenBao entry, the Environment
# annotations. Namespaces are removed under the same rule, from
# `aep.wso2.com/namespace-created-by`.
#
# Idempotent: every step is skipped gracefully when its object is already gone,
# so a re-run after a partial failure finishes the job.
#
# ── What it deliberately does NOT remove ────────────────────────────────────
#
#   * The OpenChoreo Environment itself. AEP does not own it alone — setup-aep.sh
#     creates it, Agent Manager's platform chart adopts that same object when
#     Agent Manager is installed, and a removed Environment takes every
#     Component's deployment with it. Only the `aep.wso2.com/thunder-*`
#     annotations this repo projected onto it are removed. Delete the
#     Environment separately when that is what you mean (after dropping its
#     promotion path from the DeploymentPipeline, which otherwise holds the
#     deletion open through OpenChoreo's finalizer).
#   * The platform IdP (T1). It is neutral cluster infrastructure shared by
#     every environment — see ADR-0028.
#
# Inputs (all optional):
#   --yes / -y / FORCE=1   skip the confirmation prompt
#   AMP_API_URL            Agent Manager's API (default in env.sh)
#   IDP_TOKEN_URL, IDP_CLIENT_ID, IDP_CLIENT_SECRET
#                          how Agent Manager's ams-auth.sh mints the token for amp-api
#   AM_SCRIPTS_DIR         local Agent Manager checkout's deployments/scripts (see am-scripts.sh)
#   OPENBAO_NS / OPENBAO_POD / OPENBAO_TOKEN
#                          where the binding's OpenBao copy lives (openbao / openbao-0 / root)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"
source "$SCRIPT_DIR/am-scripts.sh"

load_public_urls "$SCRIPT_DIR/../.env"

ASSUME_YES="${FORCE:-0}"
POSITIONAL=""
for arg in "$@"; do
    case "$arg" in
        --yes|-y) ASSUME_YES=1 ;;
        -*) echo "unknown option: $arg" >&2; exit 2 ;;
        *) POSITIONAL="${POSITIONAL} ${arg}" ;;
    esac
done
# shellcheck disable=SC2086
set -- ${POSITIONAL}

ORG_NAME="${1:-${ORG_NAME:-}}"
ENV_NAME="${2:-${ENV_NAME:-}}"
if [ -z "$ORG_NAME" ] || [ -z "$ENV_NAME" ]; then
    echo "usage: $(basename "$0") <org> <env> [--yes]   (or ORG_NAME=… ENV_NAME=…)" >&2
    exit 2
fi
validate_dns_label "$ORG_NAME" "$ENV_NAME" || exit 1

OPERATOR_NS="thunder-app-operator-system"
MANAGED_BY="setup-environment-thunder.sh"
GATEWAY_MANAGED_BY="setup-environment-gateway.sh"
OPENBAO_NS="${OPENBAO_NS:-openbao}"
OPENBAO_POD="${OPENBAO_POD:-openbao-0}"
OPENBAO_TOKEN="${OPENBAO_TOKEN:-root}"
OPENBAO_MOUNT="secret"
OPENBAO_PATH="aep/thunder/${ORG_NAME}/${ENV_NAME}"
SECRET_PATH="${OPENBAO_MOUNT}/${OPENBAO_PATH}"

GW_NS="${ORG_NAME}-${ENV_NAME}"
GW_RELEASE="api-platform-${ORG_NAME}-${ENV_NAME}"

export AMP_API_URL
export IDP_TOKEN_URL="${IDP_TOKEN_URL:-${PUBLIC_THUNDER_URL}/oauth2/token}"

current_ctx="$(kubectl config current-context 2>/dev/null || true)"
if [ "$current_ctx" != "$CLUSTER_CONTEXT" ]; then
    echo "❌ kubectl context is '${current_ctx:-<none>}', expected '${CLUSTER_CONTEXT}' (env.sh CLUSTER_CONTEXT)." >&2
    echo "   kubectl config use-context ${CLUSTER_CONTEXT}" >&2
    exit 1
fi

echo "============================================"
echo "  Removing environment identity — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"

# ── helpers ─────────────────────────────────────────────────────────────────

ns_label() { # <namespace> <label key> — prints the label's value, or empty
    kubectl get namespace "$1" -o "jsonpath={.metadata.labels.${2//\./\\.}}" 2>/dev/null || true
}

# ensure_stage_dir — Agent Manager's scripts, staged once per run. Two steps
# below need them (the naming library when there is no binding to read the
# release name from, and the AMS auth helpers when deregistering) and either can
# be the first to ask.
STAGE_DIR=""
ensure_stage_dir() {
    if [ -n "$STAGE_DIR" ]; then return 0; fi
    STAGE_DIR="$(mktemp -d)"
    trap 'rm -rf "$STAGE_DIR"' EXIT
    stage_agent_manager_scripts "$STAGE_DIR"
}

# ── What exists ─────────────────────────────────────────────────────────────
# The binding record is the map: it names the release, its namespace, the
# mirrored Secret and the OpenBao path, and none of those can be derived
# reliably (the release name comes out of Agent Manager's naming library, which
# truncates and hashes long pairs). Without a binding the names are re-derived
# from that same library so a half-provisioned environment can still be cleaned.
binding="$(thunder_binding_configmap "$ORG_NAME" "$ENV_NAME")"
BINDING_SECRET_NAME="thunder-binding-${ORG_NAME}-${ENV_NAME}"
BINDING_SECRET_NS="$OPERATOR_NS"
T2_ISSUER=""
if [ -n "$binding" ]; then
    NS="${binding%%/*}"
    BINDING_NAME="${binding##*/}"
    # A local shorthand for the reader in utils.sh — every key below comes from
    # the record this branch just found.
    binding_key() { thunder_binding_value "$ORG_NAME" "$ENV_NAME" "$1"; }
    RELEASE="$(binding_key release)"
    T2_ISSUER="$(binding_key issuer)"
    v="$(binding_key secretPath)";      if [ -n "$v" ]; then SECRET_PATH="$v"; fi
    v="$(binding_key secretName)";      if [ -n "$v" ]; then BINDING_SECRET_NAME="$v"; fi
    v="$(binding_key secretNamespace)"; if [ -n "$v" ]; then BINDING_SECRET_NS="$v"; fi
    unset v
    OPENBAO_MOUNT="${SECRET_PATH%%/*}"
    OPENBAO_PATH="${SECRET_PATH#*/}"
    BINDING_OWNER="$(kubectl get configmap "$BINDING_NAME" -n "$NS" \
        -o 'jsonpath={.metadata.labels.aep\.wso2\.com/release-created-by}' 2>/dev/null || true)"
    if [ -z "$RELEASE" ]; then
        echo "❌ Binding ${NS}/${BINDING_NAME} carries no 'release' key — not a binding this script understands." >&2
        exit 1
    fi
else
    echo ""
    echo "ℹ️  No thunder-binding ConfigMap for ${ORG_NAME}/${ENV_NAME} — deriving the names"
    echo "   from Agent Manager's naming library to clean up whatever is left."
    ensure_stage_dir
    export THUNDER_RELEASE_PREFIX="${THUNDER_RELEASE_PREFIX:-thunder}"
    # shellcheck source=/dev/null
    source "$STAGE_DIR/thunder-naming.sh"
    RELEASE="$(thunder_release_name "$ORG_NAME" "$ENV_NAME")"
    NS="$(thunder_namespace "$ORG_NAME" "$ENV_NAME")"
    BINDING_NAME="thunder-binding-${ORG_NAME}-${ENV_NAME}"
    BINDING_OWNER=""
fi

# Ownership. The namespace label is the primary record — it is written at
# install time, before anything else can fail — and the binding ConfigMap's own
# label is the backstop for an environment provisioned before the label existed.
T2_NS_OWNER="$(ns_label "$NS" "aep.wso2.com/namespace-created-by")"
T2_RELEASE_OWNER="$(ns_label "$NS" "aep.wso2.com/release-created-by")"
if [ -z "$T2_RELEASE_OWNER" ]; then T2_RELEASE_OWNER="$BINDING_OWNER"; fi
GW_NS_OWNER="$(ns_label "$GW_NS" "aep.wso2.com/namespace-created-by")"
GW_RELEASE_OWNER="$(ns_label "$GW_NS" "aep.wso2.com/release-created-by")"

# Every one of these is a plain boolean, spelled with an explicit if: under
# `set -e` a `cond && var=true` line that evaluates false is a failing command
# and would abort the script.
t2_release_exists=false
if helm status "$RELEASE" -n "$NS" >/dev/null 2>&1; then t2_release_exists=true; fi
gw_release_exists=false
if helm status "$GW_RELEASE" -n "$GW_NS" >/dev/null 2>&1; then gw_release_exists=true; fi

remove_t2_release=false
if [ "$t2_release_exists" = true ] && [ "$T2_RELEASE_OWNER" = "$MANAGED_BY" ]; then remove_t2_release=true; fi
remove_gw_release=false
if [ "$gw_release_exists" = true ] && [ "$GW_RELEASE_OWNER" = "$GATEWAY_MANAGED_BY" ]; then remove_gw_release=true; fi

echo ""
echo "  Environment Thunder release: ${RELEASE} (${NS})"
if [ "$t2_release_exists" = false ]; then
    echo "     not installed"
elif [ "$remove_t2_release" = true ]; then
    echo "     installed by ${T2_RELEASE_OWNER} → will be uninstalled"
else
    echo "     installed by ${T2_RELEASE_OWNER:-someone else} → KEPT; only AEP's artefacts are withdrawn"
fi
echo "  Environment gateway release: ${GW_RELEASE} (${GW_NS})"
if [ "$gw_release_exists" = false ]; then
    echo "     not installed"
elif [ "$remove_gw_release" = true ]; then
    echo "     installed by ${GW_RELEASE_OWNER} → will be uninstalled"
else
    echo "     installed by ${GW_RELEASE_OWNER:-someone else} → KEPT"
fi
echo "  Binding record:              configmap/${BINDING_NAME} in ${NS}"
echo "                               secret/${BINDING_SECRET_NAME} in ${BINDING_SECRET_NS}"
echo "                               ${SECRET_PATH} (OpenBao)"
echo "  Environment annotations:     environment/${ENV_NAME} in ${ORG_NAME} (the CR itself is kept)"

# ── The orphan warning ──────────────────────────────────────────────────────
# thunder-app releases its finalizer WITHOUT deleting the OAuth client when the
# binding is gone (it has no way to reach the instance any more), so removing a
# binding out from under live ThunderApplications leaves registered clients
# behind on an instance nothing points at. Deleting the environment's Components
# first is the way to avoid it.
strays="$(kubectl get thunderapplications -A \
    -l "openchoreo.dev/namespace=${ORG_NAME},openchoreo.dev/environment=${ENV_NAME}" \
    -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name} {end}' 2>/dev/null || true)"
if [ -n "$strays" ]; then
    echo ""
    echo "⚠️  ThunderApplications still bound to this environment:"
    echo "      ${strays}"
    echo "   thunder-app cannot deregister them once the binding is gone, so their OAuth"
    echo "   clients will be left behind. Delete their Components first if that matters."
fi

if [ "$ASSUME_YES" != 1 ]; then
    if [ ! -t 0 ]; then
        echo "" >&2
        echo "❌ Refusing to remove anything without a confirmation. Re-run with --yes (or FORCE=1)." >&2
        exit 1
    fi
    echo ""
    printf "Remove the above? [y/N] "
    read -r reply
    case "$reply" in
        y|Y|yes|YES) ;;
        *) echo "Aborted — nothing was removed."; exit 0 ;;
    esac
fi

# ── 1. The environment's API Platform gateway ───────────────────────────────
# First, because it is the consumer: its ThunderKeyManager is the T2 that the
# next step takes away.
echo ""
echo "1️⃣  API Platform gateway"
if [ "$remove_gw_release" = true ]; then
    echo "🗑️  Uninstalling ${GW_RELEASE}..."
    helm uninstall "$GW_RELEASE" -n "$GW_NS" --kube-context "$CLUSTER_CONTEXT" --wait --timeout 5m >/dev/null 2>&1 \
        || echo "   ⚠️  uninstall reported an error — continuing"
    echo "   ✅ gateway release uninstalled"
elif [ "$gw_release_exists" = true ]; then
    echo "   ⏭️  ${GW_RELEASE} was not installed by ${GATEWAY_MANAGED_BY} — left alone"
else
    echo "   ℹ️  no ${GW_RELEASE} release — already removed or never installed"
fi
if [ "$GW_NS_OWNER" = "$GATEWAY_MANAGED_BY" ] && kubectl get namespace "$GW_NS" >/dev/null 2>&1; then
    echo "🗑️  Deleting namespace ${GW_NS}..."
    kubectl delete namespace "$GW_NS" --wait=false >/dev/null
    echo "   ✅ namespace deletion requested"
elif kubectl get namespace "$GW_NS" >/dev/null 2>&1; then
    echo "   ⏭️  namespace ${GW_NS} was not created by ${GATEWAY_MANAGED_BY} — left alone"
fi

# ── 2. The environment Thunder ──────────────────────────────────────────────
echo ""
echo "2️⃣  Environment Thunder"
if [ "$remove_t2_release" = true ]; then
    echo "🗑️  Uninstalling ${RELEASE}..."
    helm uninstall "$RELEASE" -n "$NS" >/dev/null
    echo "   ✅ Thunder release uninstalled"
    # setup-environment-thunder.sh applies the HTTPRoute with kubectl (it lives
    # in openchoreo-control-plane, not in the release's namespace), so
    # `helm uninstall` does not take it.
    if kubectl get httproute "$RELEASE" -n openchoreo-control-plane >/dev/null 2>&1; then
        kubectl delete httproute "$RELEASE" -n openchoreo-control-plane >/dev/null
        echo "   ✅ HTTPRoute ${RELEASE} deleted from openchoreo-control-plane"
    else
        echo "   ℹ️  no HTTPRoute ${RELEASE} in openchoreo-control-plane"
    fi
elif [ "$t2_release_exists" = true ]; then
    echo "   ⏭️  ${RELEASE} was not installed by ${MANAGED_BY} — left running"
    # AEP published a system client and a role into someone else's instance. Its
    # own objects come back out, best-effort: an unreachable instance must not
    # block the teardown, and a client left on an instance that survives is a
    # nuisance, not a broken environment.
    if [ -n "$T2_ISSUER" ]; then
        secret="$(kubectl get secret "${RELEASE}-aep-system-client" -n "$NS" \
            -o jsonpath='{.data.client-secret}' 2>/dev/null | base64 -d 2>/dev/null || true)"
        token=""
        if [ -n "$secret" ]; then
            token="$(curl -sS --max-time 20 -X POST "${T2_ISSUER}/oauth2/token" \
                --data-urlencode "grant_type=client_credentials" \
                --data-urlencode "client_id=aep-system-client" \
                --data-urlencode "client_secret=${secret}" \
                --data-urlencode "scope=system" \
                --data-urlencode "resource=${T2_ISSUER}/mcp" 2>/dev/null \
                | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("access_token",""))
except Exception: print("")' 2>/dev/null || true)"
        fi
        if [ -n "$token" ]; then
            # Both ids are AEP's own, fixed by its bundle
            # (single-cluster/thunder-env-resources/): the role first, because it
            # is the client's assignment, then the client itself.
            for obj in "roles/aep-system-client-thunder-admin" "applications/aep-system-client"; do
                code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 20 -X DELETE \
                    "${T2_ISSUER}/${obj}" -H "Authorization: Bearer ${token}" 2>/dev/null || true)"
                case "${code:-000}" in
                    200|204|404) echo "   ✅ withdrew ${obj} from ${T2_ISSUER} (HTTP ${code})" ;;
                    *) echo "   ⚠️  could not withdraw ${obj} from ${T2_ISSUER} (HTTP ${code}) — continuing" ;;
                esac
            done
        else
            echo "   ⚠️  could not mint on ${T2_ISSUER} — leaving aep-system-client registered there"
        fi
    fi
    # Everything this repo created inside someone else's namespace: the system
    # client Secret, the AEP bootstrap ConfigMap, the CA bundle, the binding.
    # Selected by the managed-by label the setup script stamps on every one.
    for kind in secret configmap job; do
        names="$(kubectl get "$kind" -n "$NS" -l "app.kubernetes.io/managed-by=${MANAGED_BY}" \
            -o jsonpath='{range .items[*]}{.metadata.name} {end}' 2>/dev/null || true)"
        for n in $names; do
            kubectl delete "$kind" "$n" -n "$NS" >/dev/null
            echo "   ✅ deleted ${kind}/${n} from ${NS}"
        done
    done
else
    echo "   ℹ️  no ${RELEASE} release — already removed or never installed"
fi
if [ "$T2_NS_OWNER" = "$MANAGED_BY" ] && kubectl get namespace "$NS" >/dev/null 2>&1; then
    echo "🗑️  Deleting namespace ${NS}..."
    kubectl delete namespace "$NS" --wait=false >/dev/null
    echo "   ✅ namespace deletion requested"
elif kubectl get namespace "$NS" >/dev/null 2>&1; then
    echo "   ⏭️  namespace ${NS} was not created by ${MANAGED_BY} — left alone"
fi

# ── 3. The binding's other two projections ──────────────────────────────────
# The ConfigMap and the T2-namespace Secret went with the namespace above when
# it was AEP's; the mirror in the operator's namespace and the OpenBao entry
# never live there and always have to be removed explicitly.
echo ""
echo "3️⃣  Binding record"
if kubectl get secret "$BINDING_SECRET_NAME" -n "$BINDING_SECRET_NS" >/dev/null 2>&1; then
    kubectl delete secret "$BINDING_SECRET_NAME" -n "$BINDING_SECRET_NS" >/dev/null
    echo "   ✅ deleted secret/${BINDING_SECRET_NAME} from ${BINDING_SECRET_NS}"
else
    echo "   ℹ️  no secret/${BINDING_SECRET_NAME} in ${BINDING_SECRET_NS}"
fi
if kubectl get configmap "$BINDING_NAME" -n "$NS" >/dev/null 2>&1; then
    kubectl delete configmap "$BINDING_NAME" -n "$NS" >/dev/null
    echo "   ✅ deleted configmap/${BINDING_NAME} from ${NS}"
else
    echo "   ℹ️  no configmap/${BINDING_NAME} in ${NS}"
fi

# aep-api reads the credential from here and has no kube access, so a stale
# entry is a live credential for an instance that no longer exists. `kv metadata
# delete` removes every version, not just the current one.
echo "🗑️  OpenBao ${SECRET_PATH}..."
if ! kubectl get pod "$OPENBAO_POD" -n "$OPENBAO_NS" >/dev/null 2>&1; then
    echo "   ⚠️  OpenBao pod ${OPENBAO_NS}/${OPENBAO_POD} not found — ${SECRET_PATH} left in place."
elif bao kv metadata delete -mount="$OPENBAO_MOUNT" "$OPENBAO_PATH" >/dev/null 2>&1; then
    echo "   ✅ ${SECRET_PATH} deleted"
else
    echo "   ⚠️  could not delete ${SECRET_PATH} — continuing"
fi

# ── 4. The Environment's annotations ────────────────────────────────────────
echo ""
echo "4️⃣  Environment annotations"
if kubectl get environment "$ENV_NAME" -n "$ORG_NAME" >/dev/null 2>&1; then
    kubectl annotate environment "$ENV_NAME" -n "$ORG_NAME" \
        aep.wso2.com/thunder-issuer- \
        aep.wso2.com/thunder-admin-url- \
        aep.wso2.com/thunder-system-resource-identifier- \
        aep.wso2.com/thunder-secret-path- \
        aep.wso2.com/thunder-binding- >/dev/null
    echo "   ✅ aep.wso2.com/thunder-* removed from environment/${ENV_NAME} (the CR is kept)"
else
    echo "   ℹ️  no environment/${ENV_NAME} in ${ORG_NAME}"
fi

# ── 5. Agent Manager's registrations ────────────────────────────────────────
# Only for an instance AEP created and has now removed: those are the
# registrations setup-environment-thunder.sh made on Agent Manager's behalf. An
# instance Agent Manager created keeps its own, and its own script removes them.
#
# Best-effort and never fatal, exactly as Agent Manager's script has it: an
# environment must be removable with amp-api down. The handle is only freed once
# the system-client credential is confirmed gone — freeing it first would let a
# re-provision claim a NEW handle while the old credential still names the old,
# immutable issuer.
echo ""
echo "5️⃣  Agent Manager registrations"
if [ "$t2_release_exists" != true ]; then
    echo "   ℹ️  no instance of AEP's was removed — nothing to deregister"
elif [ "$remove_t2_release" != true ]; then
    echo "   ⏭️  the instance was not AEP's — its registrations belong to whoever made them"
elif ! amp_api_present; then
    echo "   ℹ️  amp-api is not answering on ${AMP_API_URL} — nothing to deregister"
else
    ensure_stage_dir
    # shellcheck source=/dev/null
    source "$STAGE_DIR/ams-auth.sh"
    if access_token="$(get_ams_token 3)"; then
        system_client_deleted=false
        code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 30 -X DELETE \
            "${AMP_API_URL}/orgs/${ORG_NAME}/environments/${ENV_NAME}/thunder-system-client" \
            -H "Authorization: Bearer ${access_token}" 2>/dev/null || true)"
        case "${code:-000}" in
            200|204) echo "   ✅ removed the system-client credential from agent-manager-service"
                     system_client_deleted=true ;;
            *) echo "   ⚠️  could not remove the system-client credential (HTTP ${code:-000}) — harmless, continuing" ;;
        esac
        if [ "$system_client_deleted" = true ]; then
            code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 30 -X DELETE \
                "${AMP_API_URL}/orgs/${ORG_NAME}/environments/${ENV_NAME}/thunder-url" \
                -H "Authorization: Bearer ${access_token}" 2>/dev/null || true)"
            case "${code:-000}" in
                200|204) echo "   ✅ removed the thunder url handle from agent-manager-service" ;;
                *) echo "   ⚠️  could not remove the thunder url handle (HTTP ${code:-000}) — harmless, continuing" ;;
            esac
        else
            echo "   ⏭️  skipping the handle — freeing it while the credential survives would let a"
            echo "      re-provision claim a new handle against the old instance's immutable issuer."
        fi
    else
        echo "   ⚠️  could not obtain a token for agent-manager-service — skipping both registrations"
    fi
fi

echo ""
echo "============================================"
echo "  ✅ Environment identity removed — ${ORG_NAME}/${ENV_NAME}"
echo "============================================"
release_verdict() { # <existed> <removed>
    if [ "$1" != true ]; then echo "was not installed"
    elif [ "$2" = true ]; then echo "uninstalled"
    else echo "kept — not AEP's to remove"; fi
}
echo "  Thunder release:  ${RELEASE} ($(release_verdict "$t2_release_exists" "$remove_t2_release"))"
echo "  Gateway release:  ${GW_RELEASE} ($(release_verdict "$gw_release_exists" "$remove_gw_release"))"
echo "  Binding record:   removed (ConfigMap, mirrored Secret, ${SECRET_PATH})"
echo "  Environment CR:   kept — delete it separately if that is what you meant:"
echo "                      kubectl delete environment ${ENV_NAME} -n ${ORG_NAME}"
echo "                    OpenChoreo's finalizer holds that deletion while a"
echo "                    DeploymentPipeline still promotes through the environment —"
echo "                    drop the promotion path first (DeploymentPipeline/default)."
