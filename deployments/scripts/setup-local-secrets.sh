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

# One-time local dev setup: creates all K8s Secrets the platform chart expects
# and registers AEP OAuth clients in Thunder. Run once per cluster, after
# setup-k3d.sh. Idempotent — safe to re-run.
#
# Required env var:
#   ANTHROPIC_API_KEY  — your Anthropic API key
#
# Optional env vars (auto-generated if absent):
#   GITHUB_WEBHOOK_SECRET  — HMAC secret for GitHub webhook validation
#   POSTGRES_PASSWORD      — Postgres password for aep user

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/env.sh"

AEP_NS="wso2-aep"
THUNDER_NS="thunder"
THUNDER_DEPLOYMENT="thunder-deployment"
CONSOLE_URL="http://console.openchoreo.localhost:8080"

log()     { echo "  $*"; }
log_ok()  { echo "✅ $*"; }
log_err() { echo "❌ $*" >&2; }
step()    { echo; echo "── $* ──"; }

# ── Preflight ────────────────────────────────────────────────────────────────
if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  log_err "ANTHROPIC_API_KEY is required"
  echo "  export ANTHROPIC_API_KEY=sk-ant-..." >&2
  exit 1
fi

if ! kubectl --context "${CLUSTER_CONTEXT}" cluster-info &>/dev/null; then
  log_err "Cannot reach cluster (context: ${CLUSTER_CONTEXT})"
  echo "  Run deployments/scripts/setup-k3d.sh first." >&2
  exit 1
fi

THUNDER_POD=$(kubectl --context "${CLUSTER_CONTEXT}" get pod -n "${THUNDER_NS}" \
  -l app.kubernetes.io/name=thunder -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
if [ -z "${THUNDER_POD}" ]; then
  log_err "Thunder pod not found in namespace '${THUNDER_NS}'"
  echo "  Run deployments/scripts/setup-k3d.sh first." >&2
  exit 1
fi

# ── Generate secrets ─────────────────────────────────────────────────────────
step "Generating secrets"
rand32() { openssl rand -hex 32; }
rand16() { openssl rand -hex 16; }

# Read an existing secret key from the cluster — returns empty string if absent.
existing_secret() {
  local secret="$1" key="$2"
  kubectl --context "${CLUSTER_CONTEXT}" get secret "${secret}" \
    -n "${AEP_NS}" -o jsonpath="{.data.${key}}" 2>/dev/null \
    | base64 -d 2>/dev/null || true
}

# Stable secrets: preserve existing values so postgres data survives Helm
# reinstalls and existing webhook registrations stay valid.
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(existing_secret postgres-secrets POSTGRES_PASSWORD)}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-$(rand16)}"

GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-$(existing_secret aep-webhook-secrets GITHUB_WEBHOOK_SECRET)}"
GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-$(rand32)}"

AGENTS_JWT_SECRET="$(existing_secret aep-agents-secrets AGENTS_JWT_SECRET)"
AGENTS_JWT_SECRET="${AGENTS_JWT_SECRET:-$(rand32)}"

OAUTH_STATE_KEY="$(existing_secret aep-oauth-secrets OAUTH_STATE_SIGNING_KEY)"
OAUTH_STATE_KEY="${OAUTH_STATE_KEY:-$(rand32)}"

# Thunder client secrets — preserve so registered apps keep working.
OC_WORKLOAD_PUBLISHER_SECRET="$(existing_secret aep-thunder-secrets OC_WORKLOAD_PUBLISHER_SECRET)"
OC_WORKLOAD_PUBLISHER_SECRET="${OC_WORKLOAD_PUBLISHER_SECRET:-$(rand32)}"
OC_OBSERVER_READER_SECRET="$(existing_secret aep-thunder-secrets OC_OBSERVER_READER_SECRET)"
OC_OBSERVER_READER_SECRET="${OC_OBSERVER_READER_SECRET:-$(rand32)}"
AEP_API_CLIENT_SECRET="$(existing_secret aep-thunder-secrets AEP_API_CLIENT_SECRET)"
AEP_API_CLIENT_SECRET="${AEP_API_CLIENT_SECRET:-$(rand32)}"
BFF_TO_GIT_SERVICE_SECRET="$(existing_secret aep-thunder-secrets BFF_TO_GIT_SERVICE_SECRET)"
BFF_TO_GIT_SERVICE_SECRET="${BFF_TO_GIT_SERVICE_SECRET:-$(rand32)}"
BFF_TO_REMOTE_WORKER_SECRET="$(existing_secret aep-thunder-secrets BFF_TO_REMOTE_WORKER_SECRET)"
BFF_TO_REMOTE_WORKER_SECRET="${BFF_TO_REMOTE_WORKER_SECRET:-$(rand32)}"
LOCAL_DEV_SEEDER_SECRET="$(existing_secret aep-thunder-secrets LOCAL_DEV_SEEDER_SECRET)"
LOCAL_DEV_SEEDER_SECRET="${LOCAL_DEV_SEEDER_SECRET:-$(rand32)}"
THUNDER_SYSTEM_CLIENT_SECRET="$(existing_secret aep-thunder-secrets THUNDER_SYSTEM_CLIENT_SECRET)"
THUNDER_SYSTEM_CLIENT_SECRET="${THUNDER_SYSTEM_CLIENT_SECRET:-$(rand32)}"
OC_RCA_AGENT_SECRET="$(existing_secret aep-thunder-secrets OC_RCA_AGENT_SECRET)"
OC_RCA_AGENT_SECRET="${OC_RCA_AGENT_SECRET:-$(rand32)}"

# Task-signing key — preserve so in-flight task JWTs keep verifying.
SIGNING_KEY_FILE=$(mktemp /tmp/aep-task-signing-XXXXXX.pem)
trap 'rm -f "${SIGNING_KEY_FILE}"' EXIT
EXISTING_SIGNING_KEY="$(existing_secret aep-task-signing-key task-signing.pem)"
if [ -n "${EXISTING_SIGNING_KEY}" ]; then
  printf '%s' "${EXISTING_SIGNING_KEY}" > "${SIGNING_KEY_FILE}"
  log "Preserved existing RS256 task-signing key"
else
  openssl genrsa -out "${SIGNING_KEY_FILE}" 2048 2>/dev/null
  log "Generated RS256 task-signing key"
fi

# ── Namespace ────────────────────────────────────────────────────────────────
step "Creating namespace ${AEP_NS}"
kubectl --context "${CLUSTER_CONTEXT}" create namespace "${AEP_NS}" \
  --dry-run=client -o yaml | kubectl --context "${CLUSTER_CONTEXT}" apply -f -

# ── K8s Secrets ──────────────────────────────────────────────────────────────
step "Creating K8s Secrets in ${AEP_NS}"

ksecret() {
  local name="$1"; shift
  kubectl --context "${CLUSTER_CONTEXT}" create secret generic "${name}" \
    -n "${AEP_NS}" "$@" --dry-run=client -o yaml \
    | kubectl --context "${CLUSTER_CONTEXT}" apply -f -
  log "secret/${name}"
}

ksecret aep-agents-secrets \
  --from-literal=ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}" \
  --from-literal=AGENTS_JWT_SECRET="${AGENTS_JWT_SECRET}"

ksecret aep-webhook-secrets \
  --from-literal=GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET}"

ksecret postgres-secrets \
  --from-literal=POSTGRES_PASSWORD="${POSTGRES_PASSWORD}"

ksecret aep-task-signing-key \
  --from-file=task-signing.pem="${SIGNING_KEY_FILE}"

ksecret aep-oauth-secrets \
  --from-literal=OAUTH_STATE_SIGNING_KEY="${OAUTH_STATE_KEY}"

ksecret aep-thunder-secrets \
  --from-literal=OC_WORKLOAD_PUBLISHER_SECRET="${OC_WORKLOAD_PUBLISHER_SECRET}" \
  --from-literal=OC_OBSERVER_READER_SECRET="${OC_OBSERVER_READER_SECRET}" \
  --from-literal=AEP_API_CLIENT_SECRET="${AEP_API_CLIENT_SECRET}" \
  --from-literal=BFF_TO_GIT_SERVICE_SECRET="${BFF_TO_GIT_SERVICE_SECRET}" \
  --from-literal=BFF_TO_REMOTE_WORKER_SECRET="${BFF_TO_REMOTE_WORKER_SECRET}" \
  --from-literal=LOCAL_DEV_SEEDER_SECRET="${LOCAL_DEV_SEEDER_SECRET}" \
  --from-literal=THUNDER_SYSTEM_CLIENT_SECRET="${THUNDER_SYSTEM_CLIENT_SECRET}" \
  --from-literal=OC_RCA_AGENT_SECRET="${OC_RCA_AGENT_SECRET}"

log_ok "All platform secrets created"

# ── Thunder client registration ───────────────────────────────────────────────
# Registers all AEP OAuth clients in OC's Thunder via kubectl exec.
# Temporarily lifts THUNDER_SKIP_SECURITY to bypass Bearer auth (local dev only).
step "Registering AEP OAuth clients in Thunder"

SKIP_SEC_CURRENT=$(kubectl --context "${CLUSTER_CONTEXT}" get deploy \
  -n "${THUNDER_NS}" "${THUNDER_DEPLOYMENT}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="THUNDER_SKIP_SECURITY")].value}' 2>/dev/null || echo "false")

restore_thunder_security() {
  local val="${SKIP_SEC_CURRENT:-false}"
  if [ "${val}" != "true" ]; then
    log "Restoring THUNDER_SKIP_SECURITY=${val}"
    kubectl --context "${CLUSTER_CONTEXT}" set env \
      deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" \
      THUNDER_SKIP_SECURITY="${val}" >/dev/null
    kubectl --context "${CLUSTER_CONTEXT}" rollout status \
      deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" --timeout=120s >/dev/null
  fi
}

if [ "${SKIP_SEC_CURRENT}" != "true" ]; then
  log "Lifting THUNDER_SKIP_SECURITY for client bootstrap..."
  kubectl --context "${CLUSTER_CONTEXT}" set env \
    deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" \
    THUNDER_SKIP_SECURITY=true >/dev/null
  kubectl --context "${CLUSTER_CONTEXT}" rollout status \
    deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" --timeout=120s >/dev/null
  trap 'restore_thunder_security; rm -f "${SIGNING_KEY_FILE}"' EXIT
  THUNDER_POD=$(kubectl --context "${CLUSTER_CONTEXT}" get pod -n "${THUNDER_NS}" \
    -l app.kubernetes.io/name=thunder -o jsonpath='{.items[0].metadata.name}')
  log "Thunder pod (post-rollout): ${THUNDER_POD}"
fi

# Run client registration inside the Thunder pod where localhost:8090 is reachable.
kubectl --context "${CLUSTER_CONTEXT}" exec -i -n "${THUNDER_NS}" "${THUNDER_POD}" -- \
  sh -s -- \
    "${OC_WORKLOAD_PUBLISHER_SECRET}" \
    "${OC_OBSERVER_READER_SECRET}" \
    "${AEP_API_CLIENT_SECRET}" \
    "${BFF_TO_GIT_SERVICE_SECRET}" \
    "${BFF_TO_REMOTE_WORKER_SECRET}" \
    "${LOCAL_DEV_SEEDER_SECRET}" \
    "${THUNDER_SYSTEM_CLIENT_SECRET}" \
    "${OC_RCA_AGENT_SECRET}" \
    "${CONSOLE_URL}" \
<<'POD_SCRIPT'
set -e
OC_WORKLOAD_PUBLISHER_SECRET="$1"
OC_OBSERVER_READER_SECRET="$2"
AEP_API_CLIENT_SECRET="$3"
BFF_TO_GIT_SERVICE_SECRET="$4"
BFF_TO_REMOTE_WORKER_SECRET="$5"
LOCAL_DEV_SEEDER_SECRET="$6"
THUNDER_SYSTEM_CLIENT_SECRET="$7"
OC_RCA_AGENT_SECRET="$8"
CONSOLE_URL="$9"

THUNDER_URL="http://localhost:8090"

log()    { echo "[INFO]    $*"; }
log_ok() { echo "[SUCCESS] $*"; }
log_err(){ echo "[ERROR]   $*" >&2; }

# ── Fetch default OU ID ──────────────────────────────────────────────────────
OU_RESP=$(curl -sf --noproxy "*" "${THUNDER_URL}/organization-units/tree/default")
OU_ID=$(echo "$OU_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -z "$OU_ID" ] && { log_err "Could not fetch OU ID. Response: $OU_RESP"; exit 1; }
log "OU ID: $OU_ID"

# ── Fetch auth flow ID ───────────────────────────────────────────────────────
FLOWS_RESP=$(curl -sf --noproxy "*" \
  "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200")
AUTH_FLOW_ID=$(echo "$FLOWS_RESP" | tr '\n' ' ' \
  | grep -o '"id":"[^"]*"[^}]*"handle":"default-basic-flow"' \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
[ -z "$AUTH_FLOW_ID" ] && { log_err "Could not find default-basic-flow"; exit 1; }
log "Auth flow ID: $AUTH_FLOW_ID"

# ── Load existing apps ───────────────────────────────────────────────────────
APPS=$(curl -sf --noproxy "*" "${THUNDER_URL}/applications?limit=200")

upsert_app() {
  local client_id="$1" payload="$2"
  local app_id
  app_id=$(echo "$APPS" | tr '\n' ' ' | sed 's/" *: *"/":"/g' \
    | grep -o "\"client_id\":\"${client_id}\"[^}]*\"id\":\"[^\"]*\"\|\"id\":\"[^\"]*\"[^}]*\"client_id\":\"${client_id}\"" \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  if [ -n "$app_id" ]; then
    curl -sf --noproxy "*" -X PUT \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications/${app_id}" -o /dev/null \
      || { log_err "Update failed for ${client_id}"; exit 1; }
  else
    curl -sf --noproxy "*" -X POST \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications" -o /dev/null \
      || { log_err "Create failed for ${client_id}"; exit 1; }
  fi
  log_ok "${client_id}"
}

confidential() {
  local name="$1" desc="$2" cid="$3" secret="$4"
  upsert_app "$cid" "{
    \"name\":\"$name\",\"description\":\"$desc\",\"ou_id\":\"$OU_ID\",
    \"inbound_auth_config\":[{\"type\":\"oauth2\",\"config\":{
      \"client_id\":\"$cid\",\"client_secret\":\"$secret\",
      \"grant_types\":[\"client_credentials\"],
      \"token_endpoint_auth_method\":\"client_secret_post\",
      \"pkce_required\":false,\"public_client\":false,
      \"token\":{\"access_token\":{\"validity_period\":3600}}
    }}]}"
}

# ── Confidential clients ─────────────────────────────────────────────────────
confidential "Workload Publisher"                   "OC Workload Publisher Client"              "openchoreo-workload-publisher-client" "$OC_WORKLOAD_PUBLISHER_SECRET"
confidential "OpenChoreo Observer Resource Reader"  "BFF token for OC Observer service"         "openchoreo-observer-resource-reader-client" "$OC_OBSERVER_READER_SECRET"
confidential "AEP API Service"                      "AEP API service-to-service client"         "aep-api-client"             "$AEP_API_CLIENT_SECRET"
confidential "AEP BFF to git-service"               "BFF outbound JWT, audience: git-service"   "aep-bff-to-git-service"     "$BFF_TO_GIT_SERVICE_SECRET"
confidential "AEP BFF to remote-worker"             "BFF outbound JWT, audience: remote-worker" "aep-bff-to-remote-worker"   "$BFF_TO_REMOTE_WORKER_SECRET"
confidential "AEP Local Dev Seeder"                 "Local-dev convenience client"              "aep-local-dev-seeder"       "$LOCAL_DEV_SEEDER_SECRET"
confidential "AEP System Client"                    "System-level Thunder admin client"         "aep-system-client"          "$THUNDER_SYSTEM_CLIENT_SECRET"
confidential "OpenChoreo RCA Agent"                 "SRE/RCA agent service-account identity"    "openchoreo-rca-agent"       "$OC_RCA_AGENT_SECRET"

# ── Console PKCE client ──────────────────────────────────────────────────────
USER_ATTRS='["given_name","family_name","username","groups","ouId","ouName","ouHandle"]'
upsert_app "aep-console-client" "{
  \"name\":\"AEP Console\",\"description\":\"AEP Platform Console\",
  \"ou_id\":\"$OU_ID\",\"auth_flow_id\":\"$AUTH_FLOW_ID\",
  \"inbound_auth_config\":[{\"type\":\"oauth2\",\"config\":{
    \"client_id\":\"aep-console-client\",
    \"redirect_uris\":[\"${CONSOLE_URL}\",\"${CONSOLE_URL}/\",\"${CONSOLE_URL}/callback\"],
    \"grant_types\":[\"authorization_code\",\"refresh_token\"],
    \"response_types\":[\"code\"],
    \"token_endpoint_auth_method\":\"none\",
    \"pkce_required\":true,\"public_client\":true,
    \"token\":{
      \"access_token\":{\"validity_period\":86400,\"user_attributes\":$USER_ATTRS},
      \"id_token\":{\"validity_period\":86400,\"user_attributes\":$USER_ATTRS}
    }
  }}]}"

# ── CLI PKCE client ──────────────────────────────────────────────────────────
upsert_app "aep-cli-client" "{
  \"name\":\"AEP CLI\",\"description\":\"AEP CLI tool — PKCE login\",
  \"ou_id\":\"$OU_ID\",\"auth_flow_id\":\"$AUTH_FLOW_ID\",
  \"inbound_auth_config\":[{\"type\":\"oauth2\",\"config\":{
    \"client_id\":\"aep-cli-client\",
    \"redirect_uris\":[\"http://localhost\",\"http://127.0.0.1\"],
    \"grant_types\":[\"authorization_code\",\"refresh_token\"],
    \"response_types\":[\"code\"],
    \"token_endpoint_auth_method\":\"none\",
    \"pkce_required\":true,\"public_client\":true,
    \"token\":{\"access_token\":{\"validity_period\":86400}}
  }}]}"

# ── Assign aep-system-client to Administrator role ───────────────────────────
APPS2=$(curl -sf --noproxy "*" "${THUNDER_URL}/applications?limit=200")
SYS_APP_ID=$(echo "$APPS2" | tr '\n' ' ' | sed 's/" *: *"/":"/g' \
  | grep -o '"client_id":"aep-system-client"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"client_id":"aep-system-client"' \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
ROLES=$(curl -sf --noproxy "*" "${THUNDER_URL}/roles?limit=200" || true)
ADMIN_ROLE_ID=$(echo "$ROLES" | tr '\n' ' ' \
  | grep -o '"name":"Administrator"[^}]*"id":"[^"]*"\|"id":"[^"]*"[^}]*"name":"Administrator"' \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -n "$SYS_APP_ID" ] && [ -n "$ADMIN_ROLE_ID" ]; then
  curl -sf --noproxy "*" -X POST \
    -H "Content-Type: application/json" \
    -d "{\"role_id\":\"$ADMIN_ROLE_ID\",\"application_id\":\"$SYS_APP_ID\"}" \
    "${THUNDER_URL}/role-assignments" -o /dev/null 2>/dev/null || true
  log_ok "aep-system-client -> Administrator"
fi

log_ok "AEP Thunder OAuth clients registered"
POD_SCRIPT

restore_thunder_security

# ── Thunder CORS patch ────────────────────────────────────────────────────────
step "Patching Thunder CORS for ${CONSOLE_URL}"

CURRENT_YAML=$(kubectl --context "${CLUSTER_CONTEXT}" get configmap thunder-config-map \
  -n "${THUNDER_NS}" -o jsonpath='{.data.deployment\.yaml}')

PATCHED_YAML=$(echo "${CURRENT_YAML}" | python3 -c "
import sys, yaml
cfg = yaml.safe_load(sys.stdin.read()) or {}
origins = cfg.setdefault('cors', {}).setdefault('allowed_origins', [])
url = '${CONSOLE_URL}'
if url not in origins:
    origins.append(url)
print(yaml.dump(cfg, default_flow_style=False))
")

ESCAPED=$(echo "${PATCHED_YAML}" | python3 -c "import sys, json; print(json.dumps(sys.stdin.read()))")
kubectl --context "${CLUSTER_CONTEXT}" patch configmap thunder-config-map \
  -n "${THUNDER_NS}" --type=merge \
  -p "{\"data\":{\"deployment.yaml\":${ESCAPED}}}"

kubectl --context "${CLUSTER_CONTEXT}" rollout restart \
  deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}"
kubectl --context "${CLUSTER_CONTEXT}" rollout status \
  deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" --timeout=120s >/dev/null

log_ok "Thunder CORS updated"

echo
echo "✅ Local setup complete. Run: make dev-cluster"
