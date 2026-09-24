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

# Standalone OpenChoreo v1.2.5 k3d install for `aectl platform install` —
# Thunder swapped for ThunderID 1.0.0, plus the WSO2 API Platform operator
# `aectl` requires that the official guide doesn't install at all.
#
# This is the official k3d single-cluster guide:
#   https://openchoreo.dev/docs/getting-started/try-it-out/on-k3d-locally/
# with two deviations, each called out at its own step below:
#   1. the "Install ThunderID Identity Provider" sub-step of Step 3 is
#      replaced (see "The Thunder step" below)
#   2. a WSO2 API Platform operator install is added at the end of Step 2 —
#      the official page has no equivalent step; it satisfies `aectl platform
#      install`'s checkAPIPlatform prerequisite (see that step's own comment)
# Step 4's sample resources are applied as published: the environment `aectl
# platform install` configures gateway ingress on is whichever one
# oc.pipeline_source_environment names (`development` on this sample), imported
# with `aectl platform config import` before the install runs.
# Every other step (cluster, prerequisites, control/data/workflow/
# observability planes, samples) is copied verbatim from the official page,
# version for version. It has NO dependency on this repo's own
# `deployments/scripts/` install chain (setup.sh, setup-thunder.sh, Agent
# Manager, ...) — it stands alone and produces a plain upstream OpenChoreo
# cluster, which is what a bare `aectl` (without AEP's own platform install)
# targets.
#
# ── The Thunder step ──────────────────────────────────────────────────────
#
# The official page installs Thunder from
#   oci://ghcr.io/asgardeo/helm-charts/thunder   version 0.28.0
# bootstrapped with imperative shell scripts (curl calls against Thunder's
# REST API) baked into values-thunder.yaml.
#
# ThunderID 1.0.0 ships from a different chart in a different registry:
#   oci://ghcr.io/thunder-id/helm-charts/thunderid   version 1.0.0
# Its bootstrap mechanism is `bootstrap.configMap` (declarative YAML
# "resource_type" documents, imported in-process) rather than
# `bootstrap.scripts` (shell). Its database config has 4 logical DBs
# (config/runtime_transient/entity/runtime_persistent) rather than 3
# (config/runtime/user).
#
# Everything below that isn't the Thunder install itself is UNCHANGED from the
# official page — same OpenChoreo version (v1.2.5 / release-v1.2), same
# prerequisite charts, same control/data/workflow/observability plane installs,
# same sample resources.
#
# ── What "full parity" means here ────────────────────────────────────────
#
# The official bootstrap scripts create: a default org unit + "openchoreo-user"
# schema, 4 users (admin/developer/platform-engineer/sre) + 4 groups, and 11
# OAuth applications (Backstage, Customer Portal, RCA Agent, OpenChoreo CLI,
# System App, User MCP App, Service MCP App, Workload Publisher, Observer
# Resource Reader, FinOps Agent, MCP E2E Subject) — all with FIXED client
# ids/secrets that the rest of the official docs (OpenBao's pre-seeded
# secrets, the observability/workflow-template installs) assume exist.
#
# This script recreates the same accounts, groups and applications — same
# client ids and secrets — as declarative documents instead of shell scripts,
# so every later official-docs step still works unmodified. The default org
# unit, the "Person" user type (username/email/given_name/family_name/
# password), and the built-in "System" resource server are NOT recreated: the
# chart already ships them baked into the image
# (backend/cmd/server/bootstrap/01-default-resources.yaml), and
# `bootstrap.configMap.files` mounts our documents ALONGSIDE those shipped
# defaults rather than replacing them.
#
# ── Open judgment calls ────────────────────────────────────────────────────
#
#  1. Application `type` for public/PKCE clients (OpenChoreo CLI, User MCP
#     App) is set to "browser" below, by analogy with this repo's own public
#     PKCE console client (single-cluster/thunder-resources/87-aep-console-app.yaml).
#     If the import job fails with APP-1040 ("invalid application type"), try
#     "mobile" for these two documents instead.
#  2. ThunderID 1.0.0 enforces a deny-first Content-Security-Policy by
#     default. This script does NOT ship a `csp` server_config document,
#     because the CDN allow-list this repo's own platform IdP uses
#     (single-cluster/thunder-resources/91-platform-csp.yaml) is specific to
#     AEP's own console, not vanilla ThunderID's /gate login page. If the
#     hosted login page renders unstyled or fails to load fonts/images, add a
#     `csp` server_config document for it (see that file for the mechanism).
#
# Usage: bash deployments/scripts/setup-env-for-aectl.sh
#   WITH_BUILD=0            skip the workflow plane (Step 6, optional upstream)
#   WITH_OBSERVABILITY=0    skip the observability plane (Step 7, optional upstream)
#   WITH_SKAFFOLD_CLIENT=1  also bootstrap ae-install-client (see step 3b) — the
#                           `make dev-env` local-dev path's own admin client for
#                           `aectl platform install`. Off by default: a bare
#                           `aectl` install brings its own bootstrap client some
#                           other way and must not silently pick this one up.

set -euo pipefail

# ============================================================================
# Versions — everything except THUNDER_* is copied straight from the official
# page for release-v1.2 / v1.2.5. Bump OC_BRANCH/OC_VERSION together if you
# track a newer release; the Thunder coordinates are independent of both.
# ============================================================================
OC_BRANCH="release-v1.2"
OC_VERSION="1.2.5"
CLUSTER_NAME="openchoreo"
CLUSTER_CONTEXT="k3d-${CLUSTER_NAME}"

THUNDER_CHART="oci://ghcr.io/thunder-id/helm-charts/thunderid"
THUNDER_VERSION="1.0.0"
# The ThunderID chart's own default username, kept rather than an email-shaped
# one: this is typed at every sign-in on a dev cluster. `sub` follows it — the
# chart's bootstrap sets the admin's subject claim to the username.
THUNDER_ADMIN_USER="admin"
THUNDER_ADMIN_PASSWORD="Admin@123"

WITH_BUILD="${WITH_BUILD:-1}"
WITH_OBSERVABILITY="${WITH_OBSERVABILITY:-1}"
WITH_SKAFFOLD_CLIENT="${WITH_SKAFFOLD_CLIENT:-0}"

RAW="https://raw.githubusercontent.com/openchoreo/openchoreo/${OC_BRANCH}"

echo "============================================"
echo "  OpenChoreo ${OC_VERSION} on k3d — Thunder swapped for ThunderID ${THUNDER_VERSION}"
echo "============================================"

# ============================================================================
# Prerequisites check (official page, Prerequisites section)
# ============================================================================
for bin in docker k3d kubectl helm; do
    command -v "$bin" >/dev/null 2>&1 || { echo "❌ $bin not found on PATH"; exit 1; }
done
docker info >/dev/null 2>&1 || { echo "❌ Docker is not running"; exit 1; }

# ============================================================================
# Step 1: Create the cluster (official page, Step 1 — unchanged)
# ============================================================================
echo ""
echo "1️⃣  Creating the k3d cluster"
if ! k3d cluster list "${CLUSTER_NAME}" >/dev/null 2>&1; then
    curl -fsSL "${RAW}/install/k3d/single-cluster/config.yaml" | k3d cluster create --config=-
else
    echo "⏭️  Cluster '${CLUSTER_NAME}' already exists"
fi
kubectl config use-context "${CLUSTER_CONTEXT}"

# ============================================================================
# Step 2: Prerequisites (official page, Step 2 — unchanged)
# ============================================================================
echo ""
echo "2️⃣  Prerequisites"

echo "   Gateway API CRDs"
kubectl apply --server-side \
  -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.5.1/standard-install.yaml

echo "   cert-manager"
helm upgrade --install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --namespace cert-manager --create-namespace --version v1.19.4 \
  --set crds.enabled=true --wait --timeout 180s

echo "   External Secrets Operator"
helm upgrade --install external-secrets oci://ghcr.io/external-secrets/charts/external-secrets \
  --namespace external-secrets --create-namespace --version 2.0.1 \
  --set installCRDs=true --wait --timeout 180s

echo "   kgateway"
helm upgrade --install kgateway-crds oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds \
  --create-namespace --namespace openchoreo-control-plane --version v2.3.1
helm upgrade --install kgateway oci://cr.kgateway.dev/kgateway-dev/charts/kgateway \
  --namespace openchoreo-control-plane --create-namespace --version v2.3.1

echo "   OpenBao"
helm upgrade --install openbao oci://ghcr.io/openbao/charts/openbao \
  --namespace openbao --create-namespace --version 0.25.6 \
  --values "${RAW}/install/k3d/common/values-openbao.yaml" \
  --wait --timeout 300s

echo "   ClusterSecretStore"
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: external-secrets-openbao
  namespace: openbao
---
apiVersion: external-secrets.io/v1
kind: ClusterSecretStore
metadata:
  name: default
spec:
  provider:
    vault:
      server: "http://openbao.openbao.svc:8200"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "openchoreo-secret-writer-role"
          serviceAccountRef:
            name: "external-secrets-openbao"
            namespace: "openbao"
EOF

echo "   CoreDNS rewrite"
kubectl apply -f "${RAW}/install/k3d/common/coredns-custom.yaml"

# ── WSO2 API Platform operator ──────────────────────────────────────────────
# Not part of the official k3d guide. `aectl platform install` requires the
# gateway.api-platform.wso2.com CRD group to be registered
# (tools/aectl/cmd/platform_apiplatform.go's checkAPIPlatform). This step
# installs the operator that registers it, at the same chart/version pins as
# deployments/scripts/setup-prerequisites.sh.
#
# This installs the OPERATOR only: a shared, cluster-wide controller. It does
# NOT create an APIGateway CR or run an actual gateway instance — OpenChoreo
# runs one gateway per (org, environment), created later by
# deployments/scripts/setup-environment-gateway.sh once that environment's own
# Thunder exists, which is out of scope for this vanilla-OC script.
echo ""
echo "   WSO2 API Platform operator"
API_PLATFORM_OPERATOR_VERSION="0.11.0"
API_PLATFORM_GATEWAY_CHART_VERSION="1.2.2"
API_PLATFORM_GATEWAY_IMAGE_VERSION="1.2.1"

API_PLATFORM_VALUES="$(mktemp)"
BOOTSTRAP_DIR="$(mktemp -d)"
trap 'rm -rf "$BOOTSTRAP_DIR"; rm -f "$API_PLATFORM_VALUES"' EXIT
cat > "$API_PLATFORM_VALUES" <<'YAML'
# Bumped for the Agent Manager convergence (operator 0.6.0 -> 0.11.0, gateway
# chart 1.0.1 -> 1.2.2). What changed in the gateway chart's value schema:
#   * `gateway.router` and `gateway.policyEngine` are GONE — 1.2.x ships only
#     `controller` and `gatewayRuntime`. Their old image-tag overrides were
#     silently dead keys and have been removed.
#   * `gateway.developmentMode` is GONE. It used to auto-generate the AES-GCM
#     at-rest encryption key on first boot; the key must now be
#     pre-provisioned — not done here, since no gateway instance is created by
#     this script (see the comment above this step).
#   * The controller's liveness/readiness probes already default to
#     /api/admin/v1/health on the `admin` port, so no probe overrides are needed.
gateway:
  helm:
    chartName: "oci://ghcr.io/wso2/api-platform/helm-charts/gateway"
    # chartVersion comes from API_PLATFORM_GATEWAY_CHART_VERSION via --set.
  values:
    gateway:
      controller:
        image:
          repository: ghcr.io/wso2/api-platform/gateway-controller
          # tag comes from API_PLATFORM_GATEWAY_IMAGE_VERSION via --set.
        encryptionKeys:
          enabled: true
          secretName: api-platform-controller-aesgcm-key
          mountPath: /app/data/aesgcm-keys
      gatewayRuntime:
        image:
          repository: ghcr.io/wso2/api-platform/gateway-runtime
          # tag comes from API_PLATFORM_GATEWAY_IMAGE_VERSION via --set.
        service:
          # Pinned explicitly because the chart's DEFAULT flipped from
          # ClusterIP (1.0.1) to LoadBalancer (1.2.2). On k3d a LoadBalancer
          # Service makes klipper bind its ports as hostPorts on the node, and
          # these are 8080/8443 — the ports k3d already publishes for the
          # OpenChoreo control-plane gateway.
          type: ClusterIP
        deployment:
          # OC's component NetworkPolicies (auto-generated per Component)
          # allow ingress only from pods labeled
          # `openchoreo.dev/system-component`. Tag the runtime so
          # gateway → upstream traffic isn't blocked.
          podLabels:
            openchoreo.dev/system-component: api-gateway
YAML

helm upgrade --install api-platform-operator \
    oci://ghcr.io/wso2/api-platform/helm-charts/gateway-operator \
    --version "${API_PLATFORM_OPERATOR_VERSION}" \
    --namespace openchoreo-data-plane --create-namespace \
    --set gatewayApi.installStandardCRDs=false \
    --set "gateway.helm.chartVersion=${API_PLATFORM_GATEWAY_CHART_VERSION}" \
    --set "gateway.values.gateway.controller.image.tag=${API_PLATFORM_GATEWAY_IMAGE_VERSION}" \
    --set "gateway.values.gateway.gatewayRuntime.image.tag=${API_PLATFORM_GATEWAY_IMAGE_VERSION}" \
    --values "$API_PLATFORM_VALUES"
kubectl wait --for=condition=available deployment \
    -l app.kubernetes.io/instance=api-platform-operator \
    -n openchoreo-data-plane --timeout=180s || true

# RBAC: lets OC's cluster-agent-dataplane SA (created later, when the data
# plane installs in Step 5) reconcile RestApi CRs — k8s allows binding to a
# subject that doesn't exist yet.
kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: wso2-api-platform-gateway-module
rules:
  - apiGroups: ["gateway.api-platform.wso2.com"]
    resources: ["restapis"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: wso2-api-platform-gateway-module
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: wso2-api-platform-gateway-module
subjects:
  - kind: ServiceAccount
    name: cluster-agent-dataplane
    namespace: openchoreo-data-plane
EOF
echo "✅ WSO2 API Platform operator at gateway-operator-${API_PLATFORM_OPERATOR_VERSION}"

# ============================================================================
# Step 3: Control Plane (official page, Step 3)
# 3a and 3d are unchanged. 3b (Thunder) and 3c (entitlement claim) are new —
# see the header comment for why.
# ============================================================================
echo ""
echo "3️⃣  Control Plane"

echo "   backstage-secrets"
kubectl apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: backstage-secrets
  namespace: openchoreo-control-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: backstage-secrets
  data:
  - secretKey: backend-secret
    remoteRef:
      key: backstage-backend-secret
      property: value
  - secretKey: client-secret
    remoteRef:
      key: backstage-client-secret
      property: value
  - secretKey: jenkins-api-key
    remoteRef:
      key: backstage-jenkins-api-key
      property: value
EOF

# ── 3b. ThunderID 1.0.0 (REPLACES the official page's asgardeo/thunder:0.28.0 step) ──
echo ""
echo "3️⃣.2  Installing ThunderID ${THUNDER_VERSION} (replaces the official 0.28.0 step)"

# Users + groups. `admin` is left to the chart's OWN built-in bootstrap
# document (backend/cmd/server/bootstrap/01-default-resources.yaml, id
# 01900000-0000-7000-8000-000000000030) — setting setup.admin.username/
# password below (Thunder templates {{ .ADMIN_USERNAME }}/{{ .ADMIN_PASSWORD }}
# into that document) gives it the official login without a second
# credential write, which Thunder refuses (USR-1028) on a user that already
# has one. Groups use the LITERAL names "admins"/"developers"/
# "platform-engineers"/"sres" — NOT the chart's built-in "Administrators"
# group — because openchoreo-control-plane's shipped ClusterAuthzRoleBindings
# (admin-binding, developer-binding, ...) key on entitlement.claim=groups,
# value="admins" etc. verbatim. A different group name is a silent 403, not
# an error.
cat > "${BOOTSTRAP_DIR}/70-users-and-groups.yaml" <<'YAML'
resource_type: user
id: openchoreo-developer-user
type: Person
ouHandle: default
attributes:
  username: "developer@openchoreo.dev"
  email: "developer@openchoreo.dev"
  given_name: "Developer"
  family_name: "User"
credentials:
  password: "Dev@123"
---
resource_type: user
id: openchoreo-platform-engineer-user
type: Person
ouHandle: default
attributes:
  username: "platform-engineer@openchoreo.dev"
  email: "platform-engineer@openchoreo.dev"
  given_name: "Platform"
  family_name: "Engineer"
credentials:
  password: "PE@123"
---
resource_type: user
id: openchoreo-sre-user
type: Person
ouHandle: default
attributes:
  username: "sre@openchoreo.dev"
  email: "sre@openchoreo.dev"
  given_name: "SRE"
  family_name: "User"
credentials:
  password: "SRE@123"
---
# `admin` here is the id Thunder's own 01-default-resources.yaml assigns its
# built-in admin account (01900000-0000-7000-8000-000000000030) — referencing
# it by that fixed id, not by re-declaring the user.
resource_type: group
id: openchoreo-admins-group
name: admins
description: OpenChoreo admins group
ouHandle: default
members:
  - id: "01900000-0000-7000-8000-000000000030"
    type: user
---
resource_type: group
id: openchoreo-developers-group
name: developers
description: OpenChoreo developers group
ouHandle: default
members:
  - id: openchoreo-developer-user
    type: user
---
resource_type: group
id: openchoreo-platform-engineers-group
name: platform-engineers
description: OpenChoreo platform engineers group
ouHandle: default
members:
  - id: openchoreo-platform-engineer-user
    type: user
---
resource_type: group
id: openchoreo-sres-group
name: sres
description: OpenChoreo SREs group
ouHandle: default
members:
  - id: openchoreo-sre-user
    type: user
YAML

# Applications. Client ids/secrets match the official page's literal values
# byte for byte (values-openbao.yaml's pre-seeded secrets, and the workflow
# templates the official docs apply later, both assume these exact strings).
# `ouId` (not `ouHandle`) on every application: the importer resolves the
# handle for users/groups/roles but NOT for applications — an app bootstrapped
# with `ouHandle` gets no OU at all, and its client_credentials tokens then
# carry no ouId/ouHandle claim.
DEFAULT_OU_ID="01900000-0000-7000-8000-000000000001"

cat > "${BOOTSTRAP_DIR}/71-fix-system-resource-server-identifier.yaml" <<YAML
# ThunderID's own built-in "System" resource server
# (backend/cmd/server/bootstrap/01-default-resources.yaml, id
# 01900000-0000-7000-8000-000000000020) hardcodes
# identifier: https://localhost:8090/mcp — NOT templated to this
# deployment's actual publicUrl. The native /console app derives its OWN
# OAuth resource_identifier from configuration.server.publicUrl
# ("http://thunder.openchoreo.localhost:8080/mcp" here), so login redirects
# back with "invalid_target" unless the two match. This document
# re-declares the SAME resource server (same id -> update, not duplicate),
# resources tree preserved verbatim, only identifier corrected.
resource_type: resource_server
id: "01900000-0000-7000-8000-000000000020"
name: System
description: System resource server
identifier: "http://thunder.openchoreo.localhost:8080/mcp"
ouHandle: default
resources:
  - name: System
    handle: system
    description: System resource
YAML

cat > "${BOOTSTRAP_DIR}/80-backstage-app.yaml" <<YAML
resource_type: application
id: openchoreo-backstage-client
type: fullstack
name: "Backstage"
description: "OpenChoreo Backstage Portal"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-backstage-client"
      clientSecret: "backstage-portal-secret"
      redirectUris: ["http://openchoreo.localhost:8080/api/auth/openchoreo-auth/handler/frame"]
      grantTypes: ["authorization_code","client_credentials","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          userConfig:
            validityPeriod: 86400
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 86400
          userAttributes: ["given_name","family_name","username","groups"]
YAML

cat > "${BOOTSTRAP_DIR}/81-customer-portal-app.yaml" <<YAML
resource_type: application
id: customer-portal-client
type: m2m
name: "Customer Portal"
description: "Customer Portal Application"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "customer-portal-client"
      clientSecret: "supersecret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/82-rca-agent-app.yaml" <<YAML
resource_type: application
id: openchoreo-rca-agent
type: m2m
name: "RCA Agent"
description: "OpenChoreo RCA Agent Client. Bound by rca-agent-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-rca-agent"
      clientSecret: "openchoreo-rca-agent-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# type: browser is a judgment call for this public/PKCE loopback client — see
# header comment item 1. Switch to "mobile" if the import job rejects it.
cat > "${BOOTSTRAP_DIR}/83-cli-app.yaml" <<YAML
resource_type: application
id: openchoreo-cli
type: browser
name: "OpenChoreo CLI"
description: "OpenChoreo CLI Default Application"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-cli"
      redirectUris: ["http://127.0.0.1:55152/auth-callback"]
      grantTypes: ["authorization_code","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "none"
      pkceRequired: true
      publicClient: true
      token:
        accessToken:
          userConfig:
            validityPeriod: 3600
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 3600
          userAttributes: ["given_name","family_name","username","groups"]
YAML

cat > "${BOOTSTRAP_DIR}/84-system-app.yaml" <<YAML
resource_type: application
id: openchoreo-system-app
type: m2m
name: "System Application"
description: "Generic system application for automation and integrations"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-system-app"
      clientSecret: "openchoreo-system-app-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# type: browser is the same judgment call as the CLI app above (header
# comment item 1) — this client also opens a system browser for its redirect,
# but one of its redirect URIs is a custom URL scheme (cursor://...), which is
# more typically a "mobile" app shape. Untested either way.
cat > "${BOOTSTRAP_DIR}/85-user-mcp-app.yaml" <<YAML
resource_type: application
id: user_mcp_client
type: browser
name: "User MCP App"
description: "User MCP app to be used by terminals and user facing AI agents"
ouId: "${DEFAULT_OU_ID}"
allowedUserTypes: ["Person"]
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "user_mcp_client"
      redirectUris:
        - "http://localhost:8075/callback"
        - "cursor://anysphere.cursor-mcp/oauth/callback"
        - "http://127.0.0.1:19876/mcp/oauth/callback"
      grantTypes: ["authorization_code","refresh_token"]
      responseTypes: ["code"]
      tokenEndpointAuthMethod: "none"
      pkceRequired: true
      publicClient: true
      token:
        accessToken:
          userConfig:
            validityPeriod: 86400
            attributes: ["given_name","family_name","username","groups"]
        idToken:
          validityPeriod: 86400
          userAttributes: ["given_name","family_name","username","groups"]
YAML

# No authFlowHandle here. The official payload's auth_flow_graph_id:
# auth_flow_config_basic does not translate to an authFlowHandle field on
# this schema; the client works as a plain client_credentials m2m app
# without it. Note: the importer aborts its entire batch on the first
# invalid document, so a bad field here would block every other
# user/group/application in this bundle from being created too.
cat > "${BOOTSTRAP_DIR}/86-service-mcp-app.yaml" <<YAML
resource_type: application
id: service_mcp_client
type: m2m
name: "Service MCP App"
description: "Service MCP app to be used by backend services which can securely store the secret"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "service_mcp_client"
      clientSecret: "service_mcp_client_secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_basic"
      token:
        accessToken:
          clientConfig:
            validityPeriod: 86400
YAML

cat > "${BOOTSTRAP_DIR}/87-workload-publisher-app.yaml" <<YAML
resource_type: application
id: openchoreo-workload-publisher-client
type: m2m
name: "Workload Publisher"
description: "OpenChoreo Workload Publisher Client for creating workloads from CI workflows. Bound by workload-publisher-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-workload-publisher-client"
      clientSecret: "openchoreo-workload-publisher-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/88-observer-app.yaml" <<YAML
resource_type: application
id: openchoreo-observer-resource-reader-client
type: m2m
name: "OpenChoreo Observer Resource Reader"
description: "OpenChoreo Observer Resource Reader Client. Bound by observer-resource-reader-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-observer-resource-reader-client"
      clientSecret: "openchoreo-observer-resource-reader-client-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/89-finops-agent-app.yaml" <<YAML
resource_type: application
id: openchoreo-finops-agent
type: m2m
name: "FinOps Agent"
description: "OpenChoreo FinOps Agent Client. Bound by finops-agent-binding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "openchoreo-finops-agent"
      clientSecret: "openchoreo-finops-agent-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

cat > "${BOOTSTRAP_DIR}/90-mcp-e2e-subject-app.yaml" <<YAML
resource_type: application
id: mcp-e2e-subject-client
type: m2m
name: "MCP E2E Subject"
description: "Permission-less client_credentials subject for MCP e2e authorization tests. Intentionally has NO ClusterAuthzRoleBinding."
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "mcp-e2e-subject-client"
      clientSecret: "mcp-e2e-subject-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
YAML

# CORS as a server_config document — REQUIRED, not optional. ThunderID 1.0.0's
# static config has no CORS section; `--set thunder.configuration.cors.*`
# writes a key nothing reads. Without this document the browser's very first
# call (fetching /.well-known/openid-configuration from the console/portal
# origin) has no Access-Control-Allow-Origin header and the sign-in silently
# fails. Origins match the official page's values-thunder.yaml.
cat > "${BOOTSTRAP_DIR}/95-cors.yaml" <<'YAML'
resource_type: server_config
name: cors
value:
  allowedOrigins:
    - "http://openchoreo.localhost:8080"
    - "http://localhost:7007"
YAML

# ── 3c. ae-install-client — make dev-env's own Thunder admin client ─────────
#
# `aectl platform install` authenticates to Thunder as thunder.admin_client_id
# (skaffold/defaults.yaml: "ae-install-client") to register every OTHER AEP
# OAuth app — see tools/aectl/internal/thunder/client.go's New(), which mints
# a client_credentials token before it can call any admin endpoint. That
# client therefore has to exist, and already hold Thunder's `system` scope
# plus its built-in Administrator role, BEFORE aectl ever runs — a
# chicken-and-egg aectl itself cannot resolve (there is no privileged token
# yet to create the first privileged client). Bootstrapping it here, as one
# more declarative document loaded in-process at chart install, needs no
# auth at all, which is what breaks the cycle.
#
# The role assignment below targets ThunderID's OWN built-in "Administrator"
# role (fixed id, same pattern as deployments/single-cluster/thunder-resources/
# 84-aep-system-role.yaml) rather than a role AEP owns, so ae-install-client
# holds every permission that role carries — not a hand-picked copy of them.
if [ "$WITH_SKAFFOLD_CLIENT" = "1" ]; then
cat > "${BOOTSTRAP_DIR}/86-ae-install-client.yaml" <<YAML
resource_type: application
id: ae-install-client
type: m2m
name: "AE Install Client"
description: "Bootstrap admin client for aectl platform install (make dev-env / skaffold/defaults.yaml thunder.admin_client_id)"
ouId: "${DEFAULT_OU_ID}"
inboundAuthConfig:
  - type: oauth2
    config:
      clientId: "ae-install-client"
      clientSecret: "ae-install-client-secret"
      grantTypes: ["client_credentials"]
      tokenEndpointAuthMethod: "client_secret_post"
      pkceRequired: false
      publicClient: false
      scopes: ["openid", "profile", "email", "system"]
      token:
        accessToken:
          clientConfig:
            validityPeriod: 3600
            attributes: ["ouId", "ouHandle"]
YAML

cat > "${BOOTSTRAP_DIR}/87-ae-install-client-admin-role.yaml" <<YAML
# id/name/description/permissions restate ThunderID's built-in Administrator
# role's own fixed values verbatim — the importer REPLACES those fields
# wholesale on update, so restating them keeps this a no-op re-assertion
# rather than an accidental rename. Assignments are additive, so this only
# adds ae-install-client alongside whatever else is already assigned.
resource_type: role
id: "01900000-0000-7000-8000-000000000050"
name: Administrator
description: System administrator role with full permissions
ouHandle: default
permissions:
  - resourceServerId: "01900000-0000-7000-8000-000000000020"
    permissions:
      - system
assignments:
  - id: ae-install-client
    type: app
YAML
fi

BOOTSTRAP_CM="openchoreo-thunderid-bootstrap"
kubectl create namespace thunder --dry-run=client -o yaml | kubectl apply -f - >/dev/null
kubectl -n thunder create configmap "${BOOTSTRAP_CM}" \
    --from-file="${BOOTSTRAP_DIR}" --dry-run=client -o yaml | kubectl apply -f -

BOOTSTRAP_FILES_JSON="$(python3 -c "import json,os; print(json.dumps(sorted(os.listdir('${BOOTSTRAP_DIR}'))))")"

# Database: 4 logical DBs in this chart (config / runtime_transient / entity /
# runtime_persistent), vs. the old chart's 3 (config / runtime / user). Each
# `.sqlite.path`/`.sqlite.options` already default to sensible per-DB values
# in the chart — only `.type` needs overriding to sqlite for a single-writer
# k3d pod, matching the official page's single-replica SQLite setup.
helm upgrade --install thunder "${THUNDER_CHART}" \
    --version "${THUNDER_VERSION}" \
    --namespace thunder --create-namespace \
    --set-string "fullnameOverride=thunder" \
    --set "deployment.replicaCount=1" \
    --set "hpa.enabled=false" \
    --set "ingress.enabled=false" \
    --set "httproute.enabled=true" \
    --set "httproute.parentRefs[0].name=gateway-default" \
    --set "httproute.parentRefs[0].namespace=openchoreo-control-plane" \
    --set "httproute.hostnames[0]=thunder.openchoreo.localhost" \
    --set "configuration.server.httpOnly=true" \
    --set "configuration.server.publicUrl=http://thunder.openchoreo.localhost:8080" \
    --set "configuration.jwt.issuer=http://thunder.openchoreo.localhost:8080" \
    --set "configuration.database.config.type=sqlite" \
    --set "configuration.database.runtime_transient.type=sqlite" \
    --set "configuration.database.entity.type=sqlite" \
    --set "configuration.database.runtime_persistent.type=sqlite" \
    --set "configuration.passkey.allowedOrigins[0]=http://openchoreo.localhost:8080" \
    --set "persistence.enabled=true" \
    --set "setup.enabled=true" \
    --set-string "setup.admin.username=${THUNDER_ADMIN_USER}" \
    --set-string "setup.admin.password=${THUNDER_ADMIN_PASSWORD}" \
    --set "bootstrap.configMap.name=${BOOTSTRAP_CM}" \
    --set-json "bootstrap.configMap.files=${BOOTSTRAP_FILES_JSON}" \
    --wait --timeout 10m

echo "⏳ Waiting for ThunderID..."
kubectl wait -n thunder --for=condition=available --timeout=300s deployment -l app.kubernetes.io/name=thunderid

echo "✅ ThunderID ready at http://thunder.openchoreo.localhost:8080 (${THUNDER_ADMIN_USER} / ${THUNDER_ADMIN_PASSWORD})"

# ── 3c. Entitlement claim: sub -> client_id ──────────────────────────────
# ThunderID 1.0.0 puts a client_credentials token's subject in the `client_id`
# claim, not `sub`. openchoreo-control-plane's chart-shipped
# ClusterAuthzRoleBindings for service accounts (mcp-tryout-client-binding,
# backstage-catalog-reader-binding, finops-agent-binding, rca-agent-binding,
# workload-publisher-binding, observer-resource-reader-binding) are keyed on
# claim: sub, and so is openchoreo-api-config's own entitlement mapping. Left
# as `sub`, every one of those service accounts 403s. The four human-login
# bindings (admin-binding, developer-binding, platform-engineer-binding,
# sre-binding) key on claim: groups and are NOT touched — that claim is
# unaffected.
#
# This has to run AFTER the control-plane install below creates these objects,
# so it is applied there, not here. See the "openchoreo-control-plane" section.

echo ""
echo "   Control Plane"
helm upgrade --install openchoreo-control-plane \
    oci://ghcr.io/openchoreo/helm-charts/openchoreo-control-plane \
    --version "${OC_VERSION}" \
    --namespace openchoreo-control-plane --create-namespace \
    --values "${RAW}/install/k3d/single-cluster/values-cp.yaml" \
    --wait --timeout 600s

echo "⏳ Waiting for Control Plane..."
kubectl wait -n openchoreo-control-plane --for=condition=available --timeout=300s deployment --all

echo "🔧 Switching the service-account entitlement claim to client_id..."
patched_api_config="$(kubectl get configmap openchoreo-api-config -n openchoreo-control-plane -o yaml \
    | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g")"
echo "$patched_api_config" | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null
kubectl rollout restart deployment/openchoreo-api -n openchoreo-control-plane
kubectl rollout status deployment/openchoreo-api -n openchoreo-control-plane --timeout=120s

for binding in $(kubectl get clusterauthzrolebindings.openchoreo.dev -o jsonpath='{.items[*].metadata.name}' 2>/dev/null); do
    claim="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    [ "$claim" = "sub" ] || continue
    kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o yaml \
        | sed -E "s/claim:[[:space:]]*['\"]?sub['\"]?/claim: client_id/g" \
        | kubectl apply --server-side --field-manager=helm --force-conflicts -f - >/dev/null 2>&1 || true
    now="$(kubectl get clusterauthzrolebinding.openchoreo.dev "$binding" -o jsonpath='{.spec.entitlement.claim}' 2>/dev/null)"
    if [ "$now" != "client_id" ]; then
        echo "❌ ClusterAuthzRoleBinding ${binding} is still on claim '${now}' — service accounts through it will 403." >&2
        exit 1
    fi
    echo "   ✓ ${binding} -> client_id"
done
echo "✅ Entitlement claim is client_id"

echo "✅ Control Plane ready"

# ============================================================================
# Step 4: Default resources (official page, Step 4 — unchanged)
# ============================================================================
echo ""
echo "4️⃣  Default resources"
kubectl label namespace default openchoreo.dev/control-plane=true --overwrite
kubectl apply -f "${RAW}/samples/getting-started/all.yaml"

# ============================================================================
# Step 5: Data Plane (official page, Step 5 — unchanged)
# ============================================================================
echo ""
echo "5️⃣  Data Plane"
kubectl create namespace openchoreo-data-plane --dry-run=client -o yaml | kubectl apply -f -
kubectl wait -n openchoreo-control-plane --for=condition=Ready certificate/cluster-gateway-ca --timeout=120s
CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
    -n openchoreo-data-plane --dry-run=client -o yaml | kubectl apply -f -

helm upgrade --install openchoreo-data-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-data-plane \
  --version "${OC_VERSION}" --namespace openchoreo-data-plane --create-namespace \
  --values "${RAW}/install/k3d/single-cluster/values-dp.yaml"

kubectl wait -n openchoreo-data-plane --for=condition=Ready certificate/cluster-agent-dataplane-tls --timeout=120s
AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-data-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterDataPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  secretStoreRef:
    name: default
  gateway:
    ingress:
      external:
        http:
          host: openchoreoapis.localhost
          listenerName: http
          port: 19080
        name: gateway-default
        namespace: openchoreo-data-plane
EOF
echo "✅ Data Plane ready"

# ============================================================================
# Step 6: Workflow Plane — optional (official page, Step 6 — unchanged)
# ============================================================================
if [ "$WITH_BUILD" = "1" ]; then
    echo ""
    echo "6️⃣  Workflow Plane"
    kubectl create namespace openchoreo-workflow-plane --dry-run=client -o yaml | kubectl apply -f -
    CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
        -n openchoreo-workflow-plane --dry-run=client -o yaml | kubectl apply -f -

    helm repo add twuni https://twuni.github.io/docker-registry.helm >/dev/null 2>&1 || true
    helm repo update twuni >/dev/null
    helm upgrade --install registry twuni/docker-registry \
      --namespace openchoreo-workflow-plane --create-namespace \
      --values "${RAW}/install/k3d/single-cluster/values-registry.yaml"

    helm upgrade --install openchoreo-workflow-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-workflow-plane \
      --version "${OC_VERSION}" --namespace openchoreo-workflow-plane \
      --values "${RAW}/install/k3d/single-cluster/values-wp.yaml"

    kubectl apply \
      -f "${RAW}/samples/getting-started/workflow-templates/checkout-source.yaml" \
      -f "${RAW}/samples/getting-started/workflow-templates.yaml" \
      -f "${RAW}/samples/getting-started/workflow-templates/publish-image-k3d.yaml" \
      -f "${RAW}/samples/getting-started/workflow-templates/generate-workload-k3d.yaml"

    kubectl wait -n openchoreo-workflow-plane --for=condition=Ready certificate/cluster-agent-workflowplane-tls --timeout=120s
    AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-workflow-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterWorkflowPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  secretStoreRef:
    name: default
EOF
    echo "✅ Workflow Plane ready"
else
    echo "⏭️  Skipping Workflow Plane (WITH_BUILD=0)"
fi

# ============================================================================
# Step 7: Observability Plane — optional (official page, Step 7 — unchanged)
# ============================================================================
if [ "$WITH_OBSERVABILITY" = "1" ]; then
    echo ""
    echo "7️⃣  Observability Plane"
    kubectl create namespace openchoreo-observability-plane --dry-run=client -o yaml | kubectl apply -f -
    CA_CRT=$(kubectl get secret cluster-gateway-ca -n openchoreo-control-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl create configmap cluster-gateway-ca --from-literal=ca.crt="$CA_CRT" \
        -n openchoreo-observability-plane --dry-run=client -o yaml | kubectl apply -f -

    kubectl apply -f - <<EOF
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: opensearch-admin-credentials
  namespace: openchoreo-observability-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: opensearch-admin-credentials
  data:
  - secretKey: username
    remoteRef:
      key: opensearch-username
      property: value
  - secretKey: password
    remoteRef:
      key: opensearch-password
      property: value
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: observer-secret
  namespace: openchoreo-observability-plane
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: default
  target:
    name: observer-secret
  data:
  - secretKey: UID_RESOLVER_OAUTH_CLIENT_SECRET
    remoteRef:
      key: observer-oauth-client-secret
      property: value
EOF
    kubectl wait -n openchoreo-observability-plane --for=condition=Ready \
        externalsecret/opensearch-admin-credentials externalsecret/observer-secret --timeout=60s

    docker exec "k3d-${CLUSTER_NAME}-server-0" sh -c \
        "cat /proc/sys/kernel/random/uuid | tr -d '-' > /etc/machine-id"

    helm upgrade --install openchoreo-observability-plane oci://ghcr.io/openchoreo/helm-charts/openchoreo-observability-plane \
      --version "${OC_VERSION}" --namespace openchoreo-observability-plane \
      --values "${RAW}/install/k3d/single-cluster/values-op.yaml" --timeout 25m

    helm upgrade --install observability-logs-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch \
      --create-namespace --namespace openchoreo-observability-plane --version 0.5.3 \
      --set openSearchSetup.openSearchSecretName="opensearch-admin-credentials" \
      --set adapter.openSearchSecretName="opensearch-admin-credentials"

    helm upgrade --install observability-traces-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-tracing-opensearch \
      --create-namespace --namespace openchoreo-observability-plane --version 0.6.0 \
      --set openSearch.enabled=false \
      --set openSearchSetup.openSearchSecretName="opensearch-admin-credentials"

    helm upgrade --install observability-metrics-prometheus \
      oci://ghcr.io/openchoreo/helm-charts/observability-metrics-prometheus \
      --create-namespace --namespace openchoreo-observability-plane --version 0.6.1

    helm upgrade observability-logs-opensearch \
      oci://ghcr.io/openchoreo/helm-charts/observability-logs-opensearch \
      --namespace openchoreo-observability-plane --version 0.5.3 --reuse-values \
      --set fluent-bit.enabled=true

    kubectl wait -n openchoreo-observability-plane --for=condition=Ready \
        certificate/cluster-agent-observabilityplane-tls --timeout=120s
    AGENT_CA=$(kubectl get secret cluster-agent-tls -n openchoreo-observability-plane -o jsonpath='{.data.ca\.crt}' | base64 -d)
    kubectl apply -f - <<EOF
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterObservabilityPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      value: |
$(echo "$AGENT_CA" | sed 's/^/        /')
  observerURL: http://observer.openchoreo.localhost:11080
EOF
    kubectl patch clusterdataplane default --type merge \
        -p '{"spec":{"observabilityPlaneRef":{"kind":"ClusterObservabilityPlane","name":"default"}}}'
    if [ "$WITH_BUILD" = "1" ]; then
        kubectl patch clusterworkflowplane default --type merge \
            -p '{"spec":{"observabilityPlaneRef":{"kind":"ClusterObservabilityPlane","name":"default"}}}'
    fi
    echo "✅ Observability Plane ready"
else
    echo "⏭️  Skipping Observability Plane (WITH_OBSERVABILITY=0)"
fi

echo ""
echo "============================================"
echo "  ✅ Setup complete"
echo "============================================"
echo "  Console:  http://openchoreo.localhost:8080  (${THUNDER_ADMIN_USER} / ${THUNDER_ADMIN_PASSWORD})"
echo "  Thunder:  http://thunder.openchoreo.localhost:8080"
echo ""
echo "  Cleanup:  k3d cluster delete ${CLUSTER_NAME}"
