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

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
source "$SCRIPT_DIR/env.sh"
source "$SCRIPT_DIR/utils.sh"

echo "=== Setting up AEP Platform ==="

# Verify Thunder is running and AEP client exists
kubectl get deployment thunder-deployment -n thunder &>/dev/null || {
    echo "❌ Thunder not found. Run setup-openchoreo.sh first."
    exit 1
}
echo "✅ Thunder is running"
echo "   AEP OAuth2 clients are bootstrapped via Thunder helm values"
echo "   (59-aep-oauth-apps.sh — registers console / api / workload-publisher / 3x bff→service clients)"

# ============================================================================
# Registry mirror for Docker builds
# ============================================================================
# The workflow-plane registry uses a Kubernetes service DNS name that kubelet
# (containerd) can't resolve. We configure a registry mirror that maps the
# service name to its ClusterIP. This requires a k3s restart, which resets
# DNS configuration — so we re-apply DNS fixes afterward.
echo ""
echo "🐳 Configuring container registry for Docker builds..."
configure_registry_mirror

# ============================================================================
# OpenChoreo workflows (dockerfile-builder)
# ============================================================================
echo ""
echo "🔨 Installing OpenChoreo workflows..."

# The ClusterWorkflow CR requires the OC controller-manager webhook to be ready.
# Wait for the controller-manager deployment to be available first.
echo "⏳ Waiting for controller-manager webhook to be ready..."
kubectl wait -n openchoreo-control-plane --for=condition=available --timeout=300s \
    deployment/controller-manager

apply_with_retry() {
    local manifest="$1" label="$2"
    for attempt in 1 2 3 4 5; do
        if kubectl apply -f "$manifest" 2>/dev/null; then
            return 0
        fi
        if [ "$attempt" -eq 5 ]; then
            echo "❌ Failed to apply $label after 5 attempts"
            return 1
        fi
        echo "   Webhook not ready yet, retrying $label in 10s (attempt $attempt/5)..."
        sleep 10
    done
}

apply_with_retry "${SCRIPT_DIR}/../manifests/docker-build-workflow.yaml" "docker-build-workflow"
echo "✅ ClusterWorkflow 'dockerfile-builder' installed"

# Coding-agent runs as an OpenChoreo Job Component (not a ClusterWorkflow).
# Build + import the runner image (ONE image, both task kinds: Debian + Go +
# Playwright + baked chromium). It has no published counterpart on this branch,
# so it's built locally once per machine and imported into the node —
# self-contained, no shared registry. Pre-importing also keeps the FIRST
# dispatch from cold-pulling a multi-GB image, which has taken long enough to
# blow past the Job's activeDeadlineSeconds (the pod is killed the moment it
# starts and the task fails with DeadlineExceeded). Guarded (skips when the
# image exists); non-fatal so a build hiccup never blocks platform setup.
# Dispatch reads the tag via compose AGENT_RUNNER_IMAGE, defaulted to the same
# tag. Rebuild manually with `make build-runner`.
echo ""
bash "$SCRIPT_DIR/build-runner.sh" \
    || echo "⚠️  runner image build/import failed — coding + validation dispatch stay disabled until fixed (make build-runner)"

# ============================================================================
# OpenChoreo infrastructure resources
# ============================================================================
echo ""
echo "📦 Setting up OpenChoreo infrastructure resources..."

# Label the default namespace so the OC API recognizes it as a control-plane namespace.
# Without this label, OC API calls against namespace "default" won't work.
kubectl label namespace default openchoreo.dev/control-plane=true --overwrite
echo "✅ Namespace 'default' labeled for OpenChoreo control plane"

# Workflow-plane namespace for the `default` org. git-service writes the
# per-org build credential Secret and per-org Anthropic key Secret here on
# each dispatch (single-tenant local dev — one org → one workflows-* ns).
# Onboarding more orgs would provision a `workflows-<ouHandle>` per org.
kubectl create namespace workflows-default --dry-run=client -o yaml | kubectl apply -f -
kubectl label namespace workflows-default openchoreo.dev/workflow-plane-name=default --overwrite
echo "✅ Namespace 'workflows-default' created (per-org build + anthropic Secrets land here)"

# ClusterComponentType: deployment/service — backend APIs with path-prefix routing
kubectl apply -f - <<'OCEOF'
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterComponentType
metadata:
  name: service
spec:
  workloadType: deployment
  allowedWorkflows:
    - kind: ClusterWorkflow
      name: dockerfile-builder
  # api-configuration lets a component opt into the WSO2 API Platform
  # gateway path (RestApi + kgateway Backend pointing at the AP router).
  # observability-alert-rule is auto-provisioned for service components
  # (gated by ResolveAutoRCAEnabled: ComponentType == service, opt-out via
  # design.json `disableAutoRca`) for error-log detection and AI RCA
  # triggering — not every component.
  # Both traits' CRDs are applied below via apply_with_retry.
  allowedTraits:
    - kind: ClusterTrait
      name: api-configuration
    - kind: ClusterTrait
      name: observability-alert-rule
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        replicas:
          type: integer
          default: 1
          minimum: 1
        # Default limit sized for a JVM-based service (Ballerina's `bal run`
        # recompiles from source on every pod start) rather than the leaner
        # Go binaries this same default also covers.
        resources:
          type: object
          default: {}
          properties:
            requests:
              type: object
              default: {}
              properties:
                cpu:
                  type: string
                  default: "50m"
                memory:
                  type: string
                  default: "128Mi"
            limits:
              type: object
              default: {}
              properties:
                cpu:
                  type: string
                  default: "500m"
                memory:
                  type: string
                  default: "768Mi"
  # The Pod template + the four `forEach` ConfigMap/ExternalSecret resources
  # below opt this CCT into OpenChoreo's `configurations.*` contract:
  #   - container.env / container.files declared in the Workload, AND
  #   - workloadOverrides.container.{env,files} declared in the ReleaseBinding
  # both land in the pod via OC-computed envFrom + volumeMounts + volumes
  # plus matching ConfigMap / ExternalSecret resources.
  #
  # Prior art: agent-manager's `agent-api` ComponentType
  # (agent-manager/deployments/helm-charts/.../component-types/agent-api.yaml)
  # uses the same pattern. OC docs:
  # https://openchoreo.dev/docs/tutorials/deploy-with-configurations
  #
  # Skipping any of these helpers will silently drop the corresponding
  # Workload / ReleaseBinding input — that is the failure mode Phase 1
  # caught.
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: "${metadata.componentName}"
          namespace: "${metadata.namespace}"
          labels: "${metadata.labels}"
        spec:
          replicas: "${environmentConfigs.replicas}"
          selector:
            matchLabels: "${metadata.podSelectors}"
          template:
            metadata:
              labels: "${metadata.podSelectors}"
            spec:
              containers:
                - name: main
                  image: "${workload.container.image}"
                  env: ${dependencies.toContainerEnvs()}
                  envFrom: ${configurations.toContainerEnvFrom()}
                  volumeMounts: ${configurations.toContainerVolumeMounts()}
                  resources:
                    requests:
                      cpu: "${environmentConfigs.resources.requests.cpu}"
                      memory: "${environmentConfigs.resources.requests.memory}"
                    limits:
                      cpu: "${environmentConfigs.resources.limits.cpu}"
                      memory: "${environmentConfigs.resources.limits.memory}"
              volumes: ${configurations.toVolumes()}

    - id: env-config
      forEach: ${configurations.toConfigEnvsByContainer()}
      var: envConfig
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${envConfig.resourceName}
          namespace: ${metadata.namespace}
        data: |
          ${envConfig.envs.transformMapEntry(index, env, {env.name: env.value})}

    - id: file-config
      forEach: ${configurations.toConfigFileList()}
      var: config
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${config.resourceName}
          namespace: ${metadata.namespace}
        data:
          ${config.name}: |
            ${config.value}

    - id: secret-env-external
      forEach: ${configurations.toSecretEnvsByContainer()}
      var: secretEnv
      template:
        apiVersion: external-secrets.io/v1
        kind: ExternalSecret
        metadata:
          name: ${secretEnv.resourceName}
          namespace: ${metadata.namespace}
        spec:
          refreshInterval: 15s
          secretStoreRef:
            name: ${dataplane.secretStore}
            kind: ClusterSecretStore
          target:
            name: ${secretEnv.resourceName}
            creationPolicy: Owner
          data: |
            ${secretEnv.envs.map(secret, {
              "secretKey": secret.name,
              "remoteRef": {
                "key": secret.remoteRef.key,
                "property": has(secret.remoteRef.property) ? secret.remoteRef.property : oc_omit()
              }
            })}

    - id: secret-file-external
      forEach: ${configurations.toSecretFileList()}
      var: file
      template:
        apiVersion: external-secrets.io/v1
        kind: ExternalSecret
        metadata:
          name: ${file.resourceName}
          namespace: ${metadata.namespace}
        spec:
          refreshInterval: 15s
          secretStoreRef:
            name: ${dataplane.secretStore}
            kind: ClusterSecretStore
          target:
            name: ${file.resourceName}
            creationPolicy: Owner
          data:
            - secretKey: ${file.name}
              remoteRef:
                key: ${file.remoteRef.key}
                property: |
                  ${has(file.remoteRef.property) ? file.remoteRef.property : oc_omit()}

    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: "${metadata.componentName}"
          namespace: "${metadata.namespace}"
        spec:
          selector: "${metadata.podSelectors}"
          ports: "${workload.toServicePorts()}"

    - id: httproute-external
      forEach: '${workload.endpoints.transformList(name, ep, ("external" in ep.visibility && ep.type in ["HTTP", "REST", "GraphQL", "Websocket"]) ? [name] : []).flatten()}'
      var: endpoint
      template:
        apiVersion: gateway.networking.k8s.io/v1
        kind: HTTPRoute
        metadata:
          name: ${oc_generate_name(metadata.componentName, endpoint)}
          namespace: "${metadata.namespace}"
          labels: '${oc_merge(metadata.labels, {"openchoreo.dev/endpoint-name": endpoint, "openchoreo.dev/endpoint-visibility": "external"})}'
        spec:
          parentRefs:
            - name: "${gateway.ingress.external.name}"
              namespace: "${gateway.ingress.external.namespace}"
          hostnames: |
            ${[gateway.ingress.external.?http, gateway.ingress.external.?https]
              .filter(g, g.hasValue()).map(g, g.value().host).distinct()
              .map(h, metadata.environmentName + "-" + metadata.componentNamespace + "." + h)}
          rules:
            - matches:
                - path:
                    type: PathPrefix
                    value: /${metadata.componentName}-${endpoint}
              filters:
                - type: URLRewrite
                  urlRewrite:
                    path:
                      type: ReplacePrefixMatch
                      replacePrefixMatch: '${workload.endpoints[endpoint].?basePath.orValue("") != "" ? workload.endpoints[endpoint].?basePath.orValue("") : "/"}'
              backendRefs:
                - name: "${metadata.componentName}"
                  port: "${workload.endpoints[endpoint].port}"
OCEOF
echo "✅ ClusterComponentType 'deployment/service' created"

# ClusterTrait: api-configuration — opts a component endpoint into the
# WSO2 API Platform path. Creates a kgateway Backend pointing at the AP
# router, a RestApi CR registering the API, and patches the HTTPRoute's
# backendRef + URL rewrite. Per-environment toggles for CORS / jwtAuth /
# rateLimit / addHeaders flow through the ReleaseBinding's
# `traitEnvironmentConfigs.<instance>` block.
apply_with_retry "${SCRIPT_DIR}/../manifests/api-platform/api-configuration-trait.yaml" "api-configuration-trait"
echo "✅ ClusterTrait 'api-configuration' installed"

# ClusterTrait: observability-alert-rule — auto-provisioned on components to detect error-log patterns and trigger AI RCA incidents. The trait's CRD is applied here so the OC API can accept it in a Workload; the trait's controller is part of the control-plane chart, so it doesn't need to be installed separately. The trait's spec defines a log pattern to match and an incident template to create when the pattern is detected. The control-plane chart's rca-agent role grants the controller permission to create incidents, and the BFF's aep-api-client service account has permission to create Workload CRs with this trait. The trait is used by the coding-agent
# component to detect error-log patterns and trigger AI RCA incidents.
apply_with_retry "${SCRIPT_DIR}/../manifests/api-platform/observability-alert-rule-trait.yaml" "observability-alert-rule-trait"
echo "✅ ClusterTrait 'observability-alert-rule' installed"

# ClusterComponentType: deployment/web-application — frontends with subdomain routing
# Web-apps get their own subdomain via oc_dns_label so SPAs work correctly
# (no subpath issues with asset references).
kubectl apply -f - <<'OCEOF'
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterComponentType
metadata:
  name: web-application
spec:
  workloadType: deployment
  allowedWorkflows:
    - kind: ClusterWorkflow
      name: dockerfile-builder
  allowedTraits:
    - kind: ClusterTrait
      name: observability-alert-rule
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        replicas:
          type: integer
          default: 1
          minimum: 1
        resources:
          type: object
          default: {}
          properties:
            requests:
              type: object
              default: {}
              properties:
                cpu:
                  type: string
                  default: "50m"
                memory:
                  type: string
                  default: "128Mi"
            limits:
              type: object
              default: {}
              properties:
                cpu:
                  type: string
                  default: "200m"
                memory:
                  type: string
                  default: "256Mi"
  # See the `service` CCT above for why these `configurations.*` helpers
  # and forEach resources are mandatory. Same contract, same prior art
  # (agent-manager's agent-api ComponentType).
  resources:
    - id: deployment
      template:
        apiVersion: apps/v1
        kind: Deployment
        metadata:
          name: "${metadata.componentName}"
          namespace: "${metadata.namespace}"
          labels: "${metadata.labels}"
        spec:
          replicas: "${environmentConfigs.replicas}"
          selector:
            matchLabels: "${metadata.podSelectors}"
          template:
            metadata:
              labels: "${metadata.podSelectors}"
            spec:
              containers:
                - name: main
                  image: "${workload.container.image}"
                  env: ${dependencies.toContainerEnvs()}
                  envFrom: ${configurations.toContainerEnvFrom()}
                  volumeMounts: ${configurations.toContainerVolumeMounts()}
                  resources:
                    requests:
                      cpu: "${environmentConfigs.resources.requests.cpu}"
                      memory: "${environmentConfigs.resources.requests.memory}"
                    limits:
                      cpu: "${environmentConfigs.resources.limits.cpu}"
                      memory: "${environmentConfigs.resources.limits.memory}"
              volumes: ${configurations.toVolumes()}

    - id: env-config
      forEach: ${configurations.toConfigEnvsByContainer()}
      var: envConfig
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${envConfig.resourceName}
          namespace: ${metadata.namespace}
        data: |
          ${envConfig.envs.transformMapEntry(index, env, {env.name: env.value})}

    - id: file-config
      forEach: ${configurations.toConfigFileList()}
      var: config
      template:
        apiVersion: v1
        kind: ConfigMap
        metadata:
          name: ${config.resourceName}
          namespace: ${metadata.namespace}
        data:
          ${config.name}: |
            ${config.value}

    - id: secret-env-external
      forEach: ${configurations.toSecretEnvsByContainer()}
      var: secretEnv
      template:
        apiVersion: external-secrets.io/v1
        kind: ExternalSecret
        metadata:
          name: ${secretEnv.resourceName}
          namespace: ${metadata.namespace}
        spec:
          refreshInterval: 15s
          secretStoreRef:
            name: ${dataplane.secretStore}
            kind: ClusterSecretStore
          target:
            name: ${secretEnv.resourceName}
            creationPolicy: Owner
          data: |
            ${secretEnv.envs.map(secret, {
              "secretKey": secret.name,
              "remoteRef": {
                "key": secret.remoteRef.key,
                "property": has(secret.remoteRef.property) ? secret.remoteRef.property : oc_omit()
              }
            })}

    - id: secret-file-external
      forEach: ${configurations.toSecretFileList()}
      var: file
      template:
        apiVersion: external-secrets.io/v1
        kind: ExternalSecret
        metadata:
          name: ${file.resourceName}
          namespace: ${metadata.namespace}
        spec:
          refreshInterval: 15s
          secretStoreRef:
            name: ${dataplane.secretStore}
            kind: ClusterSecretStore
          target:
            name: ${file.resourceName}
            creationPolicy: Owner
          data:
            - secretKey: ${file.name}
              remoteRef:
                key: ${file.remoteRef.key}
                property: |
                  ${has(file.remoteRef.property) ? file.remoteRef.property : oc_omit()}

    - id: service
      template:
        apiVersion: v1
        kind: Service
        metadata:
          name: "${metadata.componentName}"
          namespace: "${metadata.namespace}"
        spec:
          selector: "${metadata.podSelectors}"
          ports: "${workload.toServicePorts()}"

    - id: httproute-external
      forEach: '${workload.endpoints.transformList(name, ep, ("external" in ep.visibility && ep.type in ["HTTP", "REST", "GraphQL", "Websocket"]) ? [name] : []).flatten()}'
      var: endpoint
      template:
        apiVersion: gateway.networking.k8s.io/v1
        kind: HTTPRoute
        metadata:
          name: ${oc_generate_name(metadata.componentName, endpoint)}
          namespace: "${metadata.namespace}"
          labels: '${oc_merge(metadata.labels, {"openchoreo.dev/endpoint-name": endpoint, "openchoreo.dev/endpoint-visibility": "external"})}'
        spec:
          parentRefs:
            - name: "${gateway.ingress.external.name}"
              namespace: "${gateway.ingress.external.namespace}"
          hostnames: |
            ${[gateway.ingress.external.?http, gateway.ingress.external.?https]
              .filter(g, g.hasValue()).map(g, g.value().host).distinct()
              .map(h, oc_dns_label(endpoint, metadata.componentName, metadata.environmentName, metadata.componentNamespace) + "." + h)}
          rules:
            - matches:
                - path:
                    type: PathPrefix
                    value: /
              backendRefs:
                - name: "${metadata.componentName}"
                  port: "${workload.endpoints[endpoint].port}"
OCEOF
echo "✅ ClusterComponentType 'deployment/web-application' created"

# ── Sample platform-resource: postgres-cnpg ClusterResourceType (P5) ────────
# The cluster PE installs the platform-resource catalog; app-factory's BFF only
# DISCOVERS and REFERENCES these types (it never authors a ClusterResourceType).
# postgres-cnpg renders a CloudNativePG `Cluster`; its RBAC grant lets OC's
# data-plane agent apply that foreign CRD into the `dp-*` namespace (without it,
# provisioning fails with a `clusters.postgresql.cnpg.io is forbidden` denial).
kubectl apply -f "${SCRIPT_DIR}/../single-cluster/resource-types/postgres-cnpg/rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/../single-cluster/resource-types/postgres-cnpg/resourcetype.yaml"
echo "✅ ClusterResourceType 'postgres-cnpg' + CNPG data-plane RBAC created"

# ── thunder-app-operator (reconciles ThunderApplication CRs → Thunder apps) ──
# Builds the operator image from its self-contained module, imports it into the
# k3d nodes (Never pull policy — no registry involved), and installs the chart.
# The chart's LOCAL DEV credentials default to the aep-system-client the Thunder
# bootstrap registers; a real cluster must override thunder.systemClient* (see
# the chart's values.yaml). CRD ships under the chart's crds/.
echo ""
echo "🔧 Building + installing thunder-app-operator..."
docker build -t thunder-app-operator:local "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/operator"
k3d image import thunder-app-operator:local -c "${CLUSTER_NAME}"
# pullPolicy=Never below means an image-GC eviction of this local-only tag is fatal
# (ErrImageNeverPull, no registry to recover from) — pin it. See utils.sh.
# Exit 2 means the import silently did not land, which `set -e` already treats as
# fatal when the import command itself fails; the helm install below would otherwise
# deploy an operator whose pod can never start. Exit 1 leaves the image usable but
# collectible, which is a warning, not a reason to abort setup.
PIN_RC=0
pin_node_image thunder-app-operator:local || PIN_RC=$?
if [ "$PIN_RC" = "2" ]; then
    echo "❌ thunder-app-operator:local did not land in the node — the operator would"
    echo "   stay ErrImageNeverPull (pullPolicy: Never, no registry). Re-run setup-aep.sh."
    exit 1
fi
helm upgrade --install thunder-app-operator \
    "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/operator/helm" \
    -n thunder-app-operator-system --create-namespace \
    --set image.repository=thunder-app-operator \
    --set image.tag=local \
    --set image.pullPolicy=Never
echo "✅ thunder-app-operator installed (ns: thunder-app-operator-system)"

# ── Sample platform-resource: thunder-app ClusterResourceType (P5) ──────────
# Companion to the operator above: the type an architect's platform-resource
# dependency names to get an OAuth app. thunder-app renders a `ThunderApplication`
# (aep.wso2.com/v1alpha1) which the operator reconciles into a real Thunder app;
# its RBAC grant lets OC's data-plane agent apply that foreign CRD into the `dp-*`
# namespace (without it, provisioning fails with a
# `thunderapplications.aep.wso2.com is forbidden` denial).
kubectl apply -f "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/rbac.yaml"
kubectl apply -f "${SCRIPT_DIR}/../single-cluster/resource-types/thunder-app/resourcetype.yaml"
echo "✅ ClusterResourceType 'thunder-app' + thunder-app data-plane RBAC created"

# ── Per-org NAMESPACED ComponentTypes (local stand-in for cloud's
#    platform-api ProvisionOrgUnit) ────────────────────────────────────────
# The BFF references the per-org namespaced ComponentType (kind=ComponentType),
# not the cluster-scoped ClusterComponentType — see
# aep-service/clients/openchoreo/component_client.go. In dev cloud,
# platform-api's ProvisionOrgUnit creates `service`/`web-application`
# ComponentTypes inside each org's namespace; locally nothing did, so a
# kind=ComponentType reference resolved to `ComponentTypeNotFound` and user
# components never deployed. Derive namespaced copies (in the org control-plane
# ns `default`) from the cluster-scoped definitions above so the two can't
# drift. Same NAME (`service`/`web-application`); only kind + namespace differ.
echo ""
echo "🧩 Provisioning per-org namespaced ComponentTypes (local ProvisionOrgUnit stand-in)..."
for _ct in service web-application; do
    kubectl get clustercomponenttype "$_ct" -o json \
        | python3 -c 'import sys, json
c = json.load(sys.stdin)
print(json.dumps({
    "apiVersion": c["apiVersion"],
    "kind": "ComponentType",
    "metadata": {"name": c["metadata"]["name"], "namespace": "default"},
    "spec": c["spec"],
}))' \
        | kubectl apply -f -
done
echo "✅ Namespaced ComponentTypes 'service' + 'web-application' created in ns 'default'"

# Environment: development — backed by the default ClusterDataPlane
kubectl apply -f - <<'OCEOF'
apiVersion: openchoreo.dev/v1alpha1
kind: Environment
metadata:
  name: development
  namespace: default
spec:
  dataPlaneRef:
    kind: ClusterDataPlane
    name: default
OCEOF
echo "✅ Environment 'development' created"

# DeploymentPipeline: default — single environment pipeline
kubectl apply -f - <<'OCEOF'
apiVersion: openchoreo.dev/v1alpha1
kind: DeploymentPipeline
metadata:
  name: default
  namespace: default
spec:
  promotionPaths:
    - sourceEnvironmentRef:
        name: development
      targetEnvironmentRefs: []
OCEOF
echo "✅ DeploymentPipeline 'default' created"

# RBAC: bind both the BFF service account AND human admin users to the
# OC admin role. The first is what the BFF presents on its outbound
# client_credentials calls; the second binds Thunder's default
# `Administrators` group (which `admin/admin` is a member of) so an
# operator logging into the console immediately has admin rights.
kubectl apply -f - <<'OCEOF'
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: aep-api-client-binding
spec:
  effect: allow
  entitlement:
    claim: sub
    value: aep-api-client
  roleMappings:
  - roleRef:
      kind: ClusterAuthzRole
      name: admin
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: administrators-group-binding
spec:
  effect: allow
  entitlement:
    claim: groups
    value: Administrators
  roleMappings:
  - roleRef:
      kind: ClusterAuthzRole
      name: admin
---
# The coding-agent build's `generate-workload-cr` step authenticates as the
# Thunder client `openchoreo-workload-publisher-client` (its own client_credentials
# grant, NOT the per-task BFF JWT) and POSTs the Workload CR to the OC API. OC
# authorizes that via this binding → the `workload-publisher` role (workload:
# create/update/view). The role is Helm-bootstrapped by the control-plane chart,
# but its BINDING is only re-applied on a clean first install — a control-plane
# install that fails on the controller-manager webhook race and is then recovered
# with `helm upgrade --install` skips it, leaving builds to 403 at workload create
# ("FORBIDDEN") on a reseeded cluster. Assert it here so the local setup is
# self-sufficient regardless of the chart bootstrap path.
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: workload-publisher-binding
spec:
  effect: allow
  entitlement:
    claim: sub
    value: openchoreo-workload-publisher-client
  roleMappings:
  - roleRef:
      kind: ClusterAuthzRole
      name: workload-publisher
---
# The SRE/RCA agent's handoff stage auto-dispatches a coding-agent run for a
# code-level incident (AE_AUTO_DISPATCH): ae_dispatch_coding_agent → aep-api's
# PromoteAndExecute, whose SYNCHRONOUS EnsureComponent pre-check forwards the
# agent's own service-account token (sub=openchoreo-rca-agent) to the OC API's
# CreateComponent. The control-plane chart's `rca-agent` role is read-only
# (*:view + incidents:update), so that one call 403s and dispatch never fires.
# Grant the agent's identity `component:create` via this ADDITIVE role/binding
# (Casbin unions it with the chart's rca-agent role — we don't edit the
# Helm-managed role, so a control-plane `helm upgrade` can't revert it) so the
# whole alert→RCA→issue→dispatch loop completes for every org/project under the
# one shared service identity — no per-user provisioning. Everything downstream
# of the pre-check (the funnel's own EnsureComponent, workflowrun:create, the
# Anthropic secret write) runs in aep-api's detached goroutine as
# sub=aep-api-client (admin *), so `component:create` is the ONLY action this
# identity needs. A normal user hitting the same promote-from-issue route still
# forwards THEIR token and is still gated by their own OC permissions — this
# grant is scoped to the SRE agent's dedicated credential, which no human holds.
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRole
metadata:
  name: rca-agent-dispatch
spec:
  description: "SRE/RCA agent handoff: create the Component CR when auto-dispatching a coding-agent run"
  actions:
  - component:create
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: rca-agent-dispatch-binding
spec:
  effect: allow
  entitlement:
    claim: sub
    value: openchoreo-rca-agent
  roleMappings:
  - roleRef:
      kind: ClusterAuthzRole
      name: rca-agent-dispatch
OCEOF
echo "✅ AEP API service account + Administrators group + workload publisher + RCA-agent dispatch authorized"

# ============================================================================
# Generate .env file
# ============================================================================
echo ""
echo "📝 Generating .env file..."

ENV_FILE="${SCRIPT_DIR}/../.env"

# Generate Phase 0 secrets and a smee.io channel for local webhook delivery.
# The webhook secret lives only in this file; smee.io is the public callback
# URL we register on each repo's GitHub webhook.
gen_hex32() {
    openssl rand -hex 32 2>/dev/null || python3 -c 'import secrets; print(secrets.token_hex(32))'
}

# Preserve existing secret values across re-runs so already-registered webhooks
# don't suddenly fail HMAC validation (and the BFF's task signing key keeps the
# same JWKS so in-flight Task JWTs still verify).
existing_val() {
    [ -f "$ENV_FILE" ] || return 0
    grep -E "^$1=" "$ENV_FILE" | head -1 | cut -d= -f2-
}

WEBHOOK_SECRET="$(existing_val GITHUB_WEBHOOK_SECRET)"
[ -z "$WEBHOOK_SECRET" ] && WEBHOOK_SECRET="$(gen_hex32)"
OAUTH_STATE_KEY="$(existing_val OAUTH_STATE_SIGNING_KEY)"
[ -z "$OAUTH_STATE_KEY" ] && OAUTH_STATE_KEY="$(gen_hex32)"
echo "🔐 Using GITHUB_WEBHOOK_SECRET (preserved) and OAUTH_STATE_SIGNING_KEY (preserved)"

# Generate the BFF Task JWT signing keypair if it doesn't already exist.
# Idempotent — on re-runs the existing key is left untouched so existing
# task workspaces continue to verify against the same JWKS. The matching
# public key is published by the BFF at /auth/external/jwks.json.
KEYS_DIR="${SCRIPT_DIR}/../keys"
TASK_KEY_PATH="${KEYS_DIR}/task-signing.pem"
mkdir -p "$KEYS_DIR"
if [[ ! -f "$TASK_KEY_PATH" ]]; then
    # `openssl genpkey` emits PKCS#8 by default; task_token_manager.go falls
    # back to PKCS#1 if needed, so this is forward-compatible.
    openssl genpkey -algorithm RSA -out "$TASK_KEY_PATH" -pkeyopt rsa_keygen_bits:2048 2>/dev/null
    chmod 600 "$TASK_KEY_PATH"
    echo "🔐 Generated BFF Task JWT signing key (RSA 2048, PKCS#8) at $TASK_KEY_PATH"
else
    echo "🔐 BFF Task JWT signing key already present at $TASK_KEY_PATH (re-using)"
fi

# Provision a fresh smee.io channel only if we don't already have one — reusing
# the existing URL keeps any GitHub webhook registrations valid across re-runs.
SMEE_URL="$(existing_val GITHUB_WEBHOOK_PROXY_URL)"
if [ -z "$SMEE_URL" ]; then
    SMEE_URL="$(curl -fsS -D - -o /dev/null https://smee.io/new 2>/dev/null | awk '/^location:/ {print $2}' | tr -d '\r')"
    if [ -z "$SMEE_URL" ]; then
        echo "⚠️  Could not auto-create smee.io channel — set GITHUB_WEBHOOK_PROXY_URL manually"
        SMEE_URL=""
    else
        echo "🌐 Provisioned smee.io channel: $SMEE_URL"
    fi
else
    echo "🌐 Reusing existing smee.io channel: $SMEE_URL"
fi

# Preserve any operator-supplied values across re-runs.
ANTHROPIC_KEY="$(existing_val ANTHROPIC_API_KEY)"
CODING_ANTHROPIC_KEY_VAL="$(existing_val AEP_CODING_ANTHROPIC_KEY)"
GITHUB_APP_ID_VAL="$(existing_val GITHUB_APP_ID)"
GITHUB_CLIENT_ID_VAL="$(existing_val GITHUB_CLIENT_ID)"
GITHUB_CLIENT_SECRET_VAL="$(existing_val GITHUB_CLIENT_SECRET)"
GITHUB_APP_SLUG_VAL="$(existing_val GITHUB_APP_SLUG)"
[ -z "$GITHUB_APP_SLUG_VAL" ] && GITHUB_APP_SLUG_VAL="aep-platform"

# Local-dev seed convenience — consumed exclusively by scripts/seed-dev.sh.
# Preserved across re-runs so the operator doesn't have to re-append them
# every time setup-aep.sh regenerates the .env from scratch.
LOCAL_DEV_ADMIN_GITHUB_PAT_VAL="$(existing_val LOCAL_DEV_ADMIN_GITHUB_PAT)"
LOCAL_DEV_ADMIN_GITHUB_OWNER_VAL="$(existing_val LOCAL_DEV_ADMIN_GITHUB_OWNER)"

cat > "$ENV_FILE" <<EOF
# Auto-generated by setup-aep.sh — $(date -u +"%Y-%m-%dT%H:%M:%SZ")
#
# Re-running setup-aep.sh preserves secrets, the smee.io channel, and any
# values you've hand-edited (ANTHROPIC_API_KEY, GitHub App credentials).

# ── OpenChoreo Platform API ────────────────────────────────────────────────
# aep-api (in compose) reaches OC via the k3d Docker network. The Host
# header is what kgateway routes on.
PLATFORM_API_SERVICE_BASE_URL=http://k3d-${CLUSTER_NAME}-serverlb:8080
PLATFORM_API_SERVICE_HOST=api.openchoreo.localhost

# ── Public URLs ─────────────────────────────────────────────────────────────
# Single source of truth for the browser-facing Thunder + console hostnames.
# Change these (and re-run start.sh) to expose over ngrok / a public URL.
PUBLIC_THUNDER_URL=http://thunder.openchoreo.localhost:8080
PUBLIC_CONSOLE_URL=http://localhost:8090

# ── Thunder OAuth client (consumed by the console at runtime) ──────────────
VITE_THUNDER_CLIENT_ID=aep-console-client
VITE_THUNDER_SCOPES=openid profile email

# ── Agents service ─────────────────────────────────────────────────────────
AGENT_MODEL=claude-sonnet-5

# ── GitHub App (optional — only for App-mode Connect) ──────────────────────
# Each org connects via Settings → GitHub Integration using either GitHub
# App or a Personal Access Token. App credentials are only needed for the
# App connect flow; PAT mode works without them.
GITHUB_APP_ID=${GITHUB_APP_ID_VAL}
GITHUB_CLIENT_ID=${GITHUB_CLIENT_ID_VAL}
GITHUB_CLIENT_SECRET=${GITHUB_CLIENT_SECRET_VAL}
GITHUB_APP_SLUG=${GITHUB_APP_SLUG_VAL}
GITHUB_APP_PRIVATE_KEY_PATH=/etc/github-app/private-key.pem
GITHUB_REPO_VISIBILITY=private

# ── GitHub webhook secrets ─────────────────────────────────────────────────
# WEBHOOK_SECRET is the HMAC key the receiver validates events with;
# OAUTH_STATE_SIGNING_KEY signs the GitHub App connect-state JWT
# (CSRF protection on the connect callback). Generated once at setup;
# rotate by clearing the values here and re-running setup-aep.sh.
GITHUB_WEBHOOK_SECRET=${WEBHOOK_SECRET}
OAUTH_STATE_SIGNING_KEY=${OAUTH_STATE_KEY}

# Local-dev webhook delivery — GitHub posts events to this smee.io channel,
# which the smee-client compose service forwards to /api/v1/webhooks/github.
GITHUB_WEBHOOK_PROXY_URL=${SMEE_URL}

# ── Committer identity (for platform-driven commits + tags) ────────────────
GIT_COMMITTER_NAME=AEP Bot
GIT_COMMITTER_EMAIL=bot@aep.dev

# Dev gate — the BFF refuses some destructive seed paths unless tier=dev.
DEPLOYMENT_TIER=dev

# ── Local-dev seed (scripts/seed-dev.sh) ────────────────────────────────────
# Optional. When set, scripts/seed-dev.sh pre-connects the default org's
# GitHub + Anthropic credentials so you don't have to clickthrough
# Settings → GitHub / Settings → Anthropic after every fresh setup. These
# are org-level credentials connected exactly as a user would; not read by
# any platform code path.
LOCAL_DEV_ADMIN_GITHUB_PAT=${LOCAL_DEV_ADMIN_GITHUB_PAT_VAL}
LOCAL_DEV_ADMIN_GITHUB_OWNER=${LOCAL_DEV_ADMIN_GITHUB_OWNER_VAL}
ANTHROPIC_API_KEY=${ANTHROPIC_KEY}

# Optional. Bills the CODING agent to its own Anthropic key, leaving everything
# else on ANTHROPIC_API_KEY above (ADR-0016). Read by BOTH seed-dev.sh (which
# connects it as the org's coding-agent key) and the playground (which hands it
# to local coding runs) — leave it empty and the coding agent reuses the key
# above, which is the default everywhere.
AEP_CODING_ANTHROPIC_KEY=${CODING_ANTHROPIC_KEY_VAL}
EOF

echo "✅ .env file generated at $(realpath "$ENV_FILE")"

# ──────────────────────────────────────────────────────────────────────────
# Local-only: pre-create the default org's vault-path namespace `wc-<…>`
# (`tenant.OrgBaseNamespace(ouId)`). Vault keys are
# `user-app-secrets/<this-ns>/<secretRefName>`. OpenBao-direct SecretReference
# CRs do *not* land here — they go in the OC org control-plane namespace
# (`default`), same as Workload/ReleaseBinding. This Namespace object is
# still created so leftover pre-fix CRs (and any other wc-… lookups) do not
# fail with `namespaces wc-… not found`.
#
# On cloud, `ou-service` creates this NS at org-onboard time. Locally
# there is no equivalent.
#
# Derives the NS deterministically from Thunder's ouId for the default
# org (= `wc-<ouId8>-<sha256(ouId)[:8]>`), matching
# `services/aep-api/internal/platform/tenant/namespace.go::OrgBaseNamespace`.
echo ""
echo "🪪 Pre-creating default org base namespace (local-only, ou-service equivalent)..."
THUNDER_URL="${THUNDER_URL:-http://thunder.openchoreo.localhost:8080}"
SEEDER_CLIENT_ID="${SEEDER_CLIENT_ID:-aep-local-dev-seeder}"
SEEDER_CLIENT_SECRET="${SEEDER_CLIENT_SECRET:-aep-local-dev-seeder-secret}"
# Thunder may have come up moments ago and not yet be ready to serve
# /oauth2/token (especially on the first setup after a fresh k3d
# cluster). Retry up to ~30s with backoff before giving up — the
# original one-shot curl left the NS unpopulated, which then surfaced
# downstream as SM-API mirror 500s during the user's first Connect.
TOKEN=""
for ATTEMPT in 1 2 3 4 5 6 7 8 9 10; do
    TOKEN_JSON=$(curl -sS -X POST "${THUNDER_URL}/oauth2/token" \
        -d "grant_type=client_credentials&client_id=${SEEDER_CLIENT_ID}&client_secret=${SEEDER_CLIENT_SECRET}&scope=openid" 2>/dev/null || true)
    TOKEN=$(printf '%s' "$TOKEN_JSON" | python3 -c 'import sys,json;d=json.load(sys.stdin);print(d.get("access_token",""))' 2>/dev/null || true)
    if [ -n "$TOKEN" ]; then
        break
    fi
    if [ "$ATTEMPT" -lt 10 ]; then
        sleep 3
    fi
done
if [ -z "$TOKEN" ]; then
    echo "⚠️  could not mint seeder token from Thunder after 30s; skipping NS pre-create"
    echo "    (re-run setup.sh once Thunder is reachable, or kubectl create the wc-* NS manually)"
else
    # Decode JWT payload (middle base64url segment), extract ouId.
    PAYLOAD=$(printf '%s' "$TOKEN" | cut -d. -f2)
    # base64url → base64 with padding (Python handles missing padding).
    OUID=$(printf '%s' "$PAYLOAD" | python3 -c '
import sys, base64, json
s = sys.stdin.read().strip().replace("-", "+").replace("_", "/")
s += "=" * (-len(s) % 4)
print(json.loads(base64.b64decode(s)).get("ouId", ""))' 2>/dev/null)
    if [ -z "$OUID" ]; then
        echo "⚠️  no ouId claim in seeder JWT; skipping NS pre-create"
    else
        # Compute NS = wc-<ouId8>-<sha256(ouId)[:8]>
        CLEAN=$(printf '%s' "$OUID" | tr -d '-')
        PREFIX=$(printf '%s' "$CLEAN" | cut -c1-8)
        SALT=$(printf '%s' "$OUID" | shasum -a 256 | cut -c1-8)
        ORG_NS="wc-${PREFIX}-${SALT}"
        kubectl create namespace "$ORG_NS" --dry-run=client -o yaml | kubectl apply -f -
        echo "✅ org base namespace ready: $ORG_NS (Thunder ouId=$OUID)"
    fi
fi

echo ""
echo "✅ AEP setup complete!"
echo ""
echo "   Default login credentials:"
echo "     Username: admin"
echo "     Password: admin"
echo ""
echo "   To start AEP:"
echo "     cd deployments && bash scripts/start.sh"
