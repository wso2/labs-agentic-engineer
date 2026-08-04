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

# One-time local dev setup: creates K8s Secrets, registers AEP OAuth clients in
# Thunder, and installs the PE resource-type catalog (postgres, postgres-cnpg, thunder-app).
# Run once per cluster, after setup-k3d.sh. Idempotent — safe to re-run.
#
# No Anthropic key is needed here: agents resolve one per turn from the calling
# org's connected credential (Settings → Anthropic Integration), so there is
# nothing platform-wide to seed. The observability plane's RCA agent is the one
# exception and reads its key from deployments/.env.
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

# openchoreo-workload-publisher-client uses a FIXED secret — NOT random and NOT
# preserved. The OpenChoreo build's generate-workload-cr step (baked into the
# openchoreo-cli image / dockerfile-builder workflow) authenticates to Thunder as
# this client with the hardcoded secret below. A random/preserved value makes the
# build's OAuth token request 401 (curl exit 22) → generate-workload-cr fails →
# the component never deploys. See memory project_workload_publisher_fixed_secret.
OC_WORKLOAD_PUBLISHER_SECRET="openchoreo-workload-publisher-secret"

# Other Thunder client secrets — preserve so registered apps keep working.
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
# Registers all AEP OAuth clients in OC's Thunder via port-forward.
# Temporarily lifts THUNDER_SKIP_SECURITY to bypass Bearer auth (local dev only).
# Using port-forward (not kubectl exec) so pod restarts during bootstrap don't
# kill the registration script.
step "Registering AEP OAuth clients in Thunder"

SKIP_SEC_CURRENT=$(kubectl --context "${CLUSTER_CONTEXT}" get deploy \
  -n "${THUNDER_NS}" "${THUNDER_DEPLOYMENT}" \
  -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="THUNDER_SKIP_SECURITY")].value}' 2>/dev/null || echo "false")

THUNDER_PF_PORT=18090
THUNDER_URL="http://localhost:${THUNDER_PF_PORT}"
THUNDER_PF_PID=""
THUNDER_SECURITY_RESTORED=false

restore_thunder_security() {
  [ -n "${THUNDER_PF_PID}" ] && { kill "${THUNDER_PF_PID}" 2>/dev/null || true; THUNDER_PF_PID=""; }
  [ "${THUNDER_SECURITY_RESTORED}" = "true" ] && return
  THUNDER_SECURITY_RESTORED=true
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
trap 'restore_thunder_security; rm -f "${SIGNING_KEY_FILE}"' EXIT

if [ "${SKIP_SEC_CURRENT}" != "true" ]; then
  log "Lifting THUNDER_SKIP_SECURITY for client bootstrap..."
  kubectl --context "${CLUSTER_CONTEXT}" set env \
    deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" \
    THUNDER_SKIP_SECURITY=true >/dev/null
  kubectl --context "${CLUSTER_CONTEXT}" rollout status \
    deploy/"${THUNDER_DEPLOYMENT}" -n "${THUNDER_NS}" --timeout=120s >/dev/null
fi

# Start port-forward to Thunder service in the background.
kubectl --context "${CLUSTER_CONTEXT}" port-forward \
  -n "${THUNDER_NS}" svc/thunder-service \
  "${THUNDER_PF_PORT}:8090" >/dev/null 2>&1 &
THUNDER_PF_PID=$!

# ── Wait for Thunder internal readiness ─────────────────────────────────────
# k8s pod-ready != Thunder fully started (DB connections, schema init).
OU_RESP=""
OU_ID=""
for i in $(seq 1 30); do
  OU_RESP=$(curl -s "${THUNDER_URL}/organization-units/tree/default" 2>/dev/null || true)
  OU_ID=$(echo "$OU_RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
  [ -n "$OU_ID" ] && break
  sleep 2
done
[ -z "$OU_ID" ] && { log_err "Could not fetch OU ID after 60s. Response: $OU_RESP"; exit 1; }
log "OU ID: $OU_ID"

# ── Fetch auth flow ID ───────────────────────────────────────────────────────
FLOWS_RESP=$(curl -s "${THUNDER_URL}/flows?flowType=AUTHENTICATION&limit=200" 2>/dev/null || true)
AUTH_FLOW_ID=$(echo "$FLOWS_RESP" | tr '\n' ' ' \
  | grep -o '"id":"[^"]*"[^}]*"handle":"default-basic-flow"' \
  | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
[ -z "$AUTH_FLOW_ID" ] && { log_err "Could not find default-basic-flow. Response: $FLOWS_RESP"; exit 1; }
log "Auth flow ID: $AUTH_FLOW_ID"

# ── Load existing apps ───────────────────────────────────────────────────────
APPS=$(curl -s "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)

# find_app_id <json_blob> <client_id>
# Proven wrapper-agnostic lookup (mirrors the Thunder bootstrap 59-aep-oauth-apps.sh):
# split the app array into one line per object, grep the line carrying our clientId,
# then pull that object's "id". Works regardless of any {"applications":[...]} wrapper.
find_app_id() {
  local json="$1" cid="$2"
  echo "$json" | sed 's/},{/}\n{/g' \
    | grep "\"clientId\":\"${cid}\"" \
    | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true
}

thunder_upsert_app() {
  local client_id="$1" payload="$2"
  local app_id resp http_code body
  app_id=$(find_app_id "$APPS" "$client_id")
  if [ -n "$app_id" ]; then
    resp=$(curl -s -X PUT \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications/${app_id}" -w "\n%{http_code}" 2>/dev/null || true)
    http_code=$(echo "$resp" | tail -1)
    body=$(echo "$resp" | head -1)
    case "$http_code" in 2*) ;; *) log_err "Update failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
    LAST_APP_ID=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "$app_id")
  else
    resp=$(curl -s -X POST \
      -H "Content-Type: application/json" \
      -d "$payload" "${THUNDER_URL}/applications" -w "\n%{http_code}" 2>/dev/null || true)
    http_code=$(echo "$resp" | tail -1)
    body=$(echo "$resp" | head -1)
    if echo "$body" | grep -q "APP-1020"; then
      # App pre-registered by Thunder bootstrap — fetch fresh list and update
      local fresh_apps
      fresh_apps=$(curl -s "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)
      app_id=$(find_app_id "$fresh_apps" "$client_id")
      if [ -z "$app_id" ]; then
        log_err "APP-1020 but could not find existing app for ${client_id}"; exit 1
      fi
      resp=$(curl -s -X PUT \
        -H "Content-Type: application/json" \
        -d "$payload" "${THUNDER_URL}/applications/${app_id}" -w "\n%{http_code}" 2>/dev/null || true)
      http_code=$(echo "$resp" | tail -1)
      body=$(echo "$resp" | head -1)
      case "$http_code" in 2*) ;; *) log_err "Update (after APP-1020) failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
    else
      case "$http_code" in 2*) ;; *) log_err "Create failed for ${client_id} (HTTP ${http_code}): ${body}"; exit 1 ;; esac
    fi
    LAST_APP_ID=$(echo "$body" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('id',''))" 2>/dev/null || echo "$app_id")
  fi
  log_ok "${client_id}"
}
LAST_APP_ID=""

thunder_confidential() {
  local name="$1" desc="$2" cid="$3" secret="$4"
  thunder_upsert_app "$cid" "{
    \"name\":\"$name\",\"description\":\"$desc\",\"ouId\":\"$OU_ID\",
    \"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{
      \"clientId\":\"$cid\",\"clientSecret\":\"$secret\",
      \"grantTypes\":[\"client_credentials\"],
      \"tokenEndpointAuthMethod\":\"client_secret_post\",
      \"pkceRequired\":false,\"publicClient\":false,
      \"token\":{\"accessToken\":{\"validityPeriod\":3600}}
    }}]}"
}

# ── Confidential clients ─────────────────────────────────────────────────────
thunder_confidential "Workload Publisher"                   "OC Workload Publisher Client"              "openchoreo-workload-publisher-client" "${OC_WORKLOAD_PUBLISHER_SECRET}"
thunder_confidential "OpenChoreo Observer Resource Reader"  "BFF token for OC Observer service"         "openchoreo-observer-resource-reader-client" "${OC_OBSERVER_READER_SECRET}"
thunder_confidential "AEP API Service"                      "AEP API service-to-service client"         "aep-api-client"             "${AEP_API_CLIENT_SECRET}"
thunder_confidential "AEP BFF to git-service"               "BFF outbound JWT, audience: git-service"   "aep-bff-to-git-service"     "${BFF_TO_GIT_SERVICE_SECRET}"
thunder_confidential "AEP BFF to remote-worker"             "BFF outbound JWT, audience: remote-worker" "aep-bff-to-remote-worker"   "${BFF_TO_REMOTE_WORKER_SECRET}"
thunder_confidential "AEP Local Dev Seeder"                 "Local-dev convenience client"              "aep-local-dev-seeder"       "${LOCAL_DEV_SEEDER_SECRET}"
thunder_confidential "AEP System Client"                    "System-level Thunder admin client"         "aep-system-client"          "${THUNDER_SYSTEM_CLIENT_SECRET}"
thunder_confidential "OpenChoreo RCA Agent"                 "SRE/RCA agent service-account identity"    "openchoreo-rca-agent"       "${OC_RCA_AGENT_SECRET}"

# ── Console PKCE client ──────────────────────────────────────────────────────
USER_ATTRS='["given_name","family_name","username","groups","ouId","ouName","ouHandle"]'
thunder_upsert_app "aep-console-client" "{
  \"name\":\"AEP Console\",\"description\":\"AEP Platform Console\",
  \"ouId\":\"$OU_ID\",\"authFlowId\":\"$AUTH_FLOW_ID\",
  \"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{
    \"clientId\":\"aep-console-client\",
    \"redirectUris\":[\"${CONSOLE_URL}\",\"${CONSOLE_URL}/\",\"${CONSOLE_URL}/callback\"],
    \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
    \"responseTypes\":[\"code\"],
    \"tokenEndpointAuthMethod\":\"none\",
    \"pkceRequired\":true,\"publicClient\":true,
    \"token\":{
      \"accessToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}},
      \"idToken\":{\"validityPeriod\":86400,\"userAttributes\":${USER_ATTRS}}
    }
  }}]}"

# ── CLI PKCE client ──────────────────────────────────────────────────────────
thunder_upsert_app "aep-cli-client" "{
  \"name\":\"AEP CLI\",\"description\":\"AEP CLI tool — PKCE login\",
  \"ouId\":\"$OU_ID\",\"authFlowId\":\"$AUTH_FLOW_ID\",
  \"inboundAuthConfig\":[{\"type\":\"oauth2\",\"config\":{
    \"clientId\":\"aep-cli-client\",
    \"redirectUris\":[\"http://localhost\",\"http://127.0.0.1\"],
    \"grantTypes\":[\"authorization_code\",\"refresh_token\"],
    \"responseTypes\":[\"code\"],
    \"tokenEndpointAuthMethod\":\"none\",
    \"pkceRequired\":true,\"publicClient\":true,
    \"token\":{\"accessToken\":{\"validityPeriod\":86400}}
  }}]}"

# ── Grant aep-system-client the Thunder 'system' permission ──────────────────
# The thunder-app operator authenticates as aep-system-client (client_credentials,
# scope=system) to manage per-resource OAuth apps. Without this grant every
# reconcile fails "thunder list apps returned 403", leaving thunder-app resource
# bindings stuck (applied.app.status.clientId: no such key).
#
# On this Thunder build the POST /roles/{id}/assignments/add sub-endpoint 500s
# (ROL-5000) when the assignment target is an app, so we authorize the client by
# CREATING a dedicated role with the app assigned INLINE (POST /roles) — the
# supported path (assignments at creation time work; the /add sub-resource does
# not). Idempotent: skip when the role already exists.
APPS2=$(curl -s "${THUNDER_URL}/applications?limit=200" 2>/dev/null || true)
SYS_APP_ID=$(find_app_id "$APPS2" "aep-system-client")
if [ -z "$SYS_APP_ID" ]; then
  log_err "Could not resolve aep-system-client app id for the system-permission grant"
  exit 1
fi
# NB: /roles rejects a limit param (ROL-1008) — fetch without one.
ROLES=$(curl -s "${THUNDER_URL}/roles" 2>/dev/null || true)
if echo "$ROLES" | sed 's/},{/}\n{/g' | grep -q '"name":"aep-system"'; then
  log_ok "aep-system role exists (aep-system-client already has system permission)"
else
  # System resource server (identifier=system) carries the 'system' permission.
  SYS_RS_ID=$(curl -s "${THUNDER_URL}/resource-servers" 2>/dev/null | sed 's/},{/}\n{/g' \
    | grep '"identifier":"system"' | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4 || true)
  if [ -z "$OU_ID" ] || [ -z "$SYS_RS_ID" ]; then
    log_err "Could not resolve OU/resource-server for the system grant: OU='${OU_ID}' RS='${SYS_RS_ID}'"
    exit 1
  fi
  GRANT_CODE=$(curl -s -X POST -H "Content-Type: application/json" -d "{
    \"name\": \"aep-system\",
    \"description\": \"Grants aep-system-client the Thunder 'system' permission (thunder-app operator).\",
    \"ouId\": \"${OU_ID}\",
    \"permissions\": [{\"resourceServerId\": \"${SYS_RS_ID}\", \"permissions\": [\"system\"]}],
    \"assignments\": [{\"id\": \"${SYS_APP_ID}\", \"type\": \"app\"}]
  }" "${THUNDER_URL}/roles" -o /dev/null -w "%{http_code}" 2>/dev/null || true)
  case "$GRANT_CODE" in
    200|201) log_ok "aep-system role created (aep-system-client granted system permission)" ;;
    *)       log_err "Failed to create aep-system role (HTTP ${GRANT_CODE})"; exit 1 ;;
  esac
fi

log_ok "AEP Thunder OAuth clients registered"

kill "${THUNDER_PF_PID}" 2>/dev/null || true
THUNDER_PF_PID=""
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

# ── PE resource-type catalog (local samples) ──────────────────────────────────
# These are the cluster PE's responsibility in production; here they are applied
# as local-only samples so the app-factory BFF can discover them during dev.
step "Installing PE resource-type catalog"

log "Installing CloudNativePG operator (required by postgres-cnpg resource type)..."
helm upgrade --install cnpg \
  oci://ghcr.io/cloudnative-pg/charts/cloudnative-pg \
  --version "${CNPG_VERSION}" \
  -n cnpg-system --create-namespace \
  --kube-context "${CLUSTER_CONTEXT}" >/dev/null
kubectl wait --for=condition=available deployment --all \
  -n cnpg-system --context "${CLUSTER_CONTEXT}" --timeout=120s >/dev/null
log_ok "CloudNativePG operator ready"

kubectl --context "${CLUSTER_CONTEXT}" apply \
  -f "${SCRIPT_DIR}/../single-cluster/resource-types/postgres-cnpg/rbac.yaml"
kubectl --context "${CLUSTER_CONTEXT}" apply \
  -f "${SCRIPT_DIR}/../single-cluster/resource-types/postgres-cnpg/resourcetype.yaml"
log_ok "ClusterResourceType 'postgres-cnpg' + data-plane RBAC"

log "Building thunder-app-operator image..."
docker build -t thunder-app-operator:local \
  "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/operator"
k3d image import thunder-app-operator:local -c "${CLUSTER_NAME}"
helm upgrade --install thunder-app-operator \
  "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/operator/helm" \
  -n thunder-app-operator-system --create-namespace \
  --set image.repository=thunder-app-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never \
  --set thunder.systemClientSecret="${THUNDER_SYSTEM_CLIENT_SECRET}"

# The operator caches an aep-system-client token at startup. On a re-run the
# helm upgrade above is a no-op (same :local image) so the pod keeps a token
# minted BEFORE the Administrator role assignment above — every reconcile then
# 403s ("thunder list apps returned 403") until the token expires (~1h), which
# fails the platform's provisioning step for the first project. Force a fresh
# pod so it mints a token that carries the just-assigned role.
kubectl --context "${CLUSTER_CONTEXT}" rollout restart \
  deployment/thunder-app-operator -n thunder-app-operator-system >/dev/null
kubectl --context "${CLUSTER_CONTEXT}" rollout status \
  deployment/thunder-app-operator -n thunder-app-operator-system --timeout=120s >/dev/null
log_ok "thunder-app-operator installed (ns: thunder-app-operator-system)"

kubectl --context "${CLUSTER_CONTEXT}" apply \
  -f "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/rbac.yaml"
kubectl --context "${CLUSTER_CONTEXT}" apply \
  -f "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/resourcetype.yaml"
log_ok "ClusterResourceType 'thunder-app' + data-plane RBAC"

kubectl --context "${CLUSTER_CONTEXT}" apply \
  -f "${SCRIPT_DIR}/../single-cluster/resource-types/postgres/resourcetype.yaml"
log_ok "ClusterResourceType 'postgres'"

# ── Let Helm adopt setup-aep.sh's cluster-scoped resources ───────────────────
# setup.sh → setup-aep.sh creates these BEFORE the platform Helm chart is
# installed (they are shared with the Compose flow, which never installs the
# chart). On the Skaffold flow the chart ALSO defines them, so `helm install`
# aborts with "invalid ownership metadata ... must be set to Helm" unless the
# existing objects carry Helm's ownership labels/annotations. Stamp them so the
# chart adopts (and reconciles) them instead of failing. Idempotent.
step "Adopting setup-aep resources into the aep-platform Helm release"
adopt_for_helm() {
  local rn="$1"
  kubectl --context "${CLUSTER_CONTEXT}" get "$rn" >/dev/null 2>&1 || { return 0; }
  kubectl --context "${CLUSTER_CONTEXT}" annotate "$rn" --overwrite \
    meta.helm.sh/release-name=aep-platform \
    meta.helm.sh/release-namespace="${AEP_NS}" >/dev/null
  kubectl --context "${CLUSTER_CONTEXT}" label "$rn" --overwrite \
    app.kubernetes.io/managed-by=Helm >/dev/null
  log "adopted ${rn}"
}
adopt_for_helm clusterauthzrolebinding/aep-api-client-binding
adopt_for_helm clustertrait/api-configuration
adopt_for_helm componenttype/web-application
adopt_for_helm componenttype/service
log_ok "setup-aep resources adopted"

echo
echo "✅ Local setup complete. Run: make dev-cluster"
echo
echo "ℹ️  To trigger component builds on PR merge, GitHub webhooks must reach the"
echo "   local BFF. Configure a delivery URL (e.g. a smee.io channel) in the"
echo "   git-ignored values.local.dev.yaml override — see"
echo "   deployments/helm-charts/platform/values.local.dev.yaml.example"
