// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package cmd

// Manifest + Helm-values templates for `aectl sre install`. Rendered against
// sreParams. Secrets are pulled from OpenBao via ESO (never plaintext); the
// obs-namespace SecretStore authenticates as the ESO controller SA, which is
// already bound to the eso-reader OpenBao role by `aectl init`.

// obs-namespace SecretStore + ExternalSecrets. All sourced from secret/data/aep/*.
const sreSecretsTmpl = `
apiVersion: external-secrets.io/v1
kind: SecretStore
metadata:
  name: openbao
  namespace: {{.ObsNamespace}}
spec:
  provider:
    vault:
      server: "{{.OpenBaoAddr}}"
      path: "secret"
      version: "v2"
      auth:
        kubernetes:
          mountPath: "kubernetes"
          role: "eso-reader"
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: rca-agent-secret
  namespace: {{.ObsNamespace}}
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: openbao
    kind: SecretStore
  target:
    name: rca-agent-secret
  data:
    - secretKey: RCA_LLM_API_KEY
      remoteRef: { key: aep/anthropic-api-key, property: value }
    - secretKey: OAUTH_CLIENT_SECRET
      remoteRef: { key: aep/thunder-clients/openchoreo-rca-agent, property: value }
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: opensearch-admin-credentials
  namespace: {{.ObsNamespace}}
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: openbao
    kind: SecretStore
  target:
    name: opensearch-admin-credentials
  data:
    - secretKey: username
      remoteRef: { key: aep/opensearch-username, property: value }
    - secretKey: password
      remoteRef: { key: aep/opensearch-password, property: value }
---
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: observer-secret
  namespace: {{.ObsNamespace}}
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: openbao
    kind: SecretStore
  target:
    name: observer-secret
  data:
    - secretKey: OPENSEARCH_USERNAME
      remoteRef: { key: aep/opensearch-username, property: value }
    - secretKey: OPENSEARCH_PASSWORD
      remoteRef: { key: aep/opensearch-password, property: value }
    - secretKey: UID_RESOLVER_OAUTH_CLIENT_SECRET
      remoteRef: { key: aep/thunder-clients/oc-observer-reader, property: value }
`

// openchoreo-observability-plane values. Observer + RCA agent; the chart's own
// :11080 gateway is disabled (the AEP main kgateway route in sreCRsTmpl exposes
// the Observer instead).
const sreObsPlaneValuesTmpl = `
observer:
  openSearchSecretName: opensearch-admin-credentials
  secretName: observer-secret
  http:
    hostnames:
      - {{.ObserverHost}}
  controlPlaneApiUrl: "{{.OCApiURL}}"
security:
  enabled: true
  oidc:
    jwksUrl: "{{.ThunderJwksURL}}"
    tokenUrl: "{{.ThunderTokenURL}}"
    authServerBaseUrl: "{{.ThunderAuthURL}}"
rca:
  enabled: true
  image:
    repository: {{.RcaImageRepo}}
    tag: {{.RcaImageTag}}
    pullPolicy: {{.RcaPullPolicy}}
  llm:
    modelName: {{.RcaModel}}
  secretName: rca-agent-secret
  oauth:
    clientId: openchoreo-rca-agent
  openchoreoApiUrl: "{{.OCApiURL}}"
  resources:
    requests:
      cpu: 250m
      memory: 1Gi
    limits:
      cpu: "1"
      memory: 2Gi
  http:
    hostnames:
      - {{.RcaHost}}
gateway:
  enabled: false
`

// observability-logs-opensearch values. OpenSearch + Fluent Bit + logs-adapter.
// Dev-grade sizing (see plan security notes: prod sizing is a follow-up).
const sreObsLogsValuesTmpl = `
openSearchSetup:
  openSearchSecretName: opensearch-admin-credentials
openSearch:
  opensearchJavaOpts: "-Xmx256M -Xms256M"
  # The OpenSearch server's admin password MUST match what the clients (setup
  # job, adapter, observer, RCA) authenticate with. The chart defaults this to a
  # hardcoded literal; point it at our opensearch-admin-credentials secret so the
  # generated password is authoritative everywhere. (Replaces the chart's single
  # default extraEnvs entry.)
  extraEnvs:
    - name: OPENSEARCH_INITIAL_ADMIN_PASSWORD
      valueFrom:
        secretKeyRef:
          name: opensearch-admin-credentials
          key: password
  resources:
    requests:
      cpu: 200m
      memory: 512Mi
    limits:
      memory: 768Mi
fluent-bit:
  enabled: true
adapter:
  openSearchSecretName: opensearch-admin-credentials
  image:
    repository: {{.AdapterRepo}}
    tag: {{.AdapterTag}}
`

// Authz grants + cross-namespace HTTPRoute + ClusterObservabilityPlane CR.
// Mirrors setup-observability.sh §4/§4b/§5 and setup-aep.sh's rca-agent-dispatch.
const sreCRsTmpl = `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: observer-mainkgw
  namespace: {{.ObsNamespace}}
spec:
  parentRefs:
    - name: gateway-default
      namespace: openchoreo-control-plane
      sectionName: http
  hostnames:
    - {{.ObserverHost}}
  rules:
    - matches:
        - path: { type: PathPrefix, value: / }
      backendRefs:
        - name: observer
          port: 8080
      timeouts:
        request: "0s"
        backendRequest: "0s"
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRole
metadata:
  name: aep-observer-reader
spec:
  actions:
    - "logs:view"
    - "workflowrun:view"
    - "component:view"
    - "project:view"
    - "namespace:view"
    - "environment:view"
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterAuthzRoleBinding
metadata:
  name: aep-observer-reader-binding
spec:
  effect: allow
  entitlement:
    claim: sub
    value: openchoreo-observer-resource-reader-client
  roleMappings:
    - roleRef:
        kind: ClusterAuthzRole
        name: aep-observer-reader
---
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
---
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterObservabilityPlane
metadata:
  name: default
spec:
  planeID: default
  clusterAgent:
    clientCA:
      secretKeyRef:
        key: ca.crt
        name: cluster-agent-tls
        namespace: {{.ObsNamespace}}
  observerURL: http://{{.ObserverHost}}:11080
  rcaAgentURL: http://{{.RcaHost}}:11080
`

// openSearchBootstrapScript is the detect+self-heal body from
// setup-observability.sh step 6 (verbatim). It does NOT PUT a template — the
// chart's own hook owns the container-logs template; this only deletes indices
// created under a wrong mapping so they get recreated correctly.
const openSearchBootstrapScript = `
set -eu
OS="https://${OS_HOST}:${OS_PORT}"
CURL="curl -sk -u ${OS_USER}:${OS_PASS} -H Content-Type:application/json"
echo "Waiting for OpenSearch ready..."
for i in $(seq 1 60); do
  if $CURL "${OS}/_cluster/health?wait_for_status=yellow&timeout=5s" >/dev/null 2>&1; then break; fi
  sleep 5
done
echo "Verifying chart template is in place (log must be wildcard-typed)..."
tpl_log=$($CURL "${OS}/_index_template/container-logs" 2>/dev/null \
  | grep -o '"log":{"type":"[a-z_]*"}' | head -1 | cut -d'"' -f6)
if [ "$tpl_log" != "wildcard" ]; then
  echo "WARNING: container-logs template maps log as '${tpl_log:-absent}' (expected 'wildcard')."
  echo "         The module chart's opensearch-setup-logs hook job should own this template."
fi
echo "Scanning indices for wrong mappings (pod_name/labels != keyword, log != wildcard)..."
for idx in $($CURL "${OS}/_cat/indices/container-logs-*?h=index" 2>/dev/null); do
  t=$($CURL "${OS}/${idx}/_mapping/field/kubernetes.pod_name" 2>/dev/null \
    | grep -o '"type":"[a-z]*"' | head -1 | cut -d'"' -f4)
  lt=$($CURL "${OS}/${idx}/_mapping/field/kubernetes.labels.openchoreo_dev%2Fcomponent-uid" 2>/dev/null \
    | grep -o '"type":"[a-z]*"' | head -1 | cut -d'"' -f4)
  lg=$($CURL "${OS}/${idx}/_mapping/field/log" 2>/dev/null \
    | grep -o '"type":"[a-z_]*"' | head -1 | cut -d'"' -f4)
  if [ "$t" = "text" ] || [ "$lt" = "text" ] || { [ -n "$lg" ] && [ "$lg" != "wildcard" ]; }; then
    echo "  - ${idx}: pod_name='$t' component-uid='$lt' log='$lg', recreating"
    $CURL -X DELETE "${OS}/${idx}" >/dev/null
  else echo "  - ${idx}: pod_name='$t' component-uid='${lt:-unset}' log='${lg:-unset}', ok"; fi
done
echo "Bootstrap complete."
`
