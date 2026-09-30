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
// sreParams. Secrets are pulled from OpenBao via ESO (never plaintext), through
// the platform chart's ClusterSecretStore (aep-platform), which reads the
// aep/* paths `aectl platform install` seeds.

// The SRE agent's own ExternalSecret, sourced from secret/data/aep/*. Applied
// whether or not aectl installs the plane itself. Carries only the OAuth
// client secret the chart's rca.secretName requires; the agent's LLM key,
// model and handoff token come from the AE-owned sre-agent-aep Secret
// aep-api's reconciler writes (wired via rca.extraEnvs, not this template).
const sreAgentSecretsTmpl = `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: rca-agent-secret
  namespace: {{.ObsNamespace}}
  annotations:
    force-sync: "{{.ForceSync}}"
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: {{.PlatformSecretStore}}
    kind: ClusterSecretStore
  target:
    name: rca-agent-secret
  data:
    - secretKey: OAUTH_CLIENT_SECRET
      remoteRef: { key: aep/thunder-clients/openchoreo-rca-agent, property: value }
`

// The observer's Thunder client secret, for a plane aectl did not install.
// `aectl platform install` registers openchoreo-observer-resource-reader-client
// in Thunder with aep/thunder-clients/oc-observer-reader, replacing whatever
// secret the plane's installer gave it, so the observer must log in with that
// one. Otherwise its project lookups 401 and every query the SRE agent makes
// through it comes back empty. Takes over the plane installer's ExternalSecret
// of the same name; srePlaneSecretsTmpl carries the same key.
const sreObserverClientSecretTmpl = `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: observer-secret
  namespace: {{.ObsNamespace}}
  annotations:
    force-sync: "{{.ForceSync}}"
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: {{.PlatformSecretStore}}
    kind: ClusterSecretStore
  target:
    name: observer-secret
  data:
    - secretKey: UID_RESOLVER_OAUTH_CLIENT_SECRET
      remoteRef: { key: aep/thunder-clients/oc-observer-reader, property: value }
`

// The plane's own secrets, applied only when aectl installs the plane: an
// existing plane brought its own, under the same names.
const srePlaneSecretsTmpl = `
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: opensearch-admin-credentials
  namespace: {{.ObsNamespace}}
  annotations:
    force-sync: "{{.ForceSync}}"
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: {{.PlatformSecretStore}}
    kind: ClusterSecretStore
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
  annotations:
    force-sync: "{{.ForceSync}}"
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: {{.PlatformSecretStore}}
    kind: ClusterSecretStore
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

// rcaExtraEnvsYAML is the RCA/SRE agent's rca.extraEnvs block, shared between
// sreObsPlaneValuesTmpl (fresh plane install) and sreAgentValuesTmpl (adopting
// an existing plane). The chart replaces the whole extraEnvs list rather than
// merging it, so this owns every entry the stock image needs: EXTENSIONS_DIR
// (the AE handoff extension mount point), AEP_MCP_URL (aep-mcp-server's MCP
// endpoint, reached over https through the control-plane gateway),
// SSL_CERT_FILE (the CA bundle Task 16's initContainer builds in-pod, so that
// https call verifies), and the four values aep-api's reconciler pushes into
// the AE-owned sre-agent-aep Secret (RCA_LLM_API_KEY, RCA_MODEL_NAME,
// RCA_LLM_BASE_URL, AEP_MCP_TOKEN — this command never carries them itself).
const rcaExtraEnvsYAML = `  extraEnvs:
    - name: EXTENSIONS_DIR
      value: /opt/aep/sre-agent-extensions
    - name: AEP_MCP_URL
      value: {{.AEMCPURL}}
    - name: SSL_CERT_FILE
      value: /opt/aep/ca/ca-bundle.crt
    - name: RCA_LLM_API_KEY
      valueFrom:
        secretKeyRef: {name: sre-agent-aep, key: RCA_LLM_API_KEY}
    - name: RCA_MODEL_NAME
      valueFrom:
        secretKeyRef: {name: sre-agent-aep, key: RCA_MODEL_NAME}
    - name: RCA_LLM_BASE_URL
      valueFrom:
        secretKeyRef: {name: sre-agent-aep, key: RCA_LLM_BASE_URL}
    - name: AEP_MCP_TOKEN
      valueFrom:
        secretKeyRef: {name: sre-agent-aep, key: AEP_MCP_TOKEN}
`

// openchoreo-observability-plane values. Observer + RCA agent; the chart's own
// :11080 gateway is disabled (the AEP main kgateway route in srePlaneCRsTmpl exposes
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
    # ThunderID 1.0 puts a client_credentials token's subject in client_id,
    # not sub (setup-env-for-aectl.sh step 3c switches the control plane the
    # same way). Keyed on sub, the observer matches no service account, so
    # the SRE agent's log queries come back empty.
    subjectTypes:
      - type: user
        display_name: User
        priority: 1
        auth_mechanisms:
          - type: jwt
            entitlement:
              claim: groups
              display_name: User Group
      - type: service_account
        display_name: Service Account
        priority: 2
        auth_mechanisms:
          - type: jwt
            entitlement:
              claim: client_id
              display_name: Client ID
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
` + rcaExtraEnvsYAML + `
gateway:
  enabled: false
`

// SRE agent overlay for a plane aectl did not install, applied with
// --reuse-values at that release's own chart version: the rca block and the
// observer's service-account claim the agent's queries need, so the plane's
// installer keeps owning everything else.
const sreAgentValuesTmpl = `
observer:
  security:
    # ThunderID 1.0 puts a client_credentials token's subject in client_id,
    # not sub (setup-env-for-aectl.sh step 3c switches the control plane the
    # same way). Keyed on sub, the observer matches no service account, so
    # the SRE agent's log queries come back empty.
    subjectTypes:
      - type: user
        display_name: User
        priority: 1
        auth_mechanisms:
          - type: jwt
            entitlement:
              claim: groups
              display_name: User Group
      - type: service_account
        display_name: Service Account
        priority: 2
        auth_mechanisms:
          - type: jwt
            entitlement:
              claim: client_id
              display_name: Client ID
rca:
  enabled: true
  image:
    repository: {{.RcaImageRepo}}
    tag: {{.RcaImageTag}}
    pullPolicy: {{.RcaPullPolicy}}
  secretName: rca-agent-secret
  oauth:
    clientId: openchoreo-rca-agent
  resources:
    requests:
      cpu: 250m
      memory: 1Gi
    limits:
      cpu: "1"
      memory: 2Gi
` + rcaExtraEnvsYAML

// sreAgentAEOwnedSecretTmpl is the AE-owned Secret aep-api's reconciler pushes
// the SRE agent's LLM key/model/base URL and its AEP handoff token into
// (rca.extraEnvs above reads it via secretKeyRef). Applied only when the
// Secret does not already exist (see ensureSREAgentSecret): a re-run of
// `aectl sre install` must never overwrite values aep-api already pushed, so
// every key here is a placeholder empty string.
const sreAgentAEOwnedSecretTmpl = `
apiVersion: v1
kind: Secret
metadata:
  name: sre-agent-aep
  namespace: {{.ObsNamespace}}
type: Opaque
stringData:
  RCA_LLM_API_KEY: ""
  RCA_MODEL_NAME: ""
  RCA_LLM_BASE_URL: ""
  AEP_MCP_TOKEN: ""
`

// sreAgentPushRoleTmpl grants aep-api's ServiceAccount just enough to push
// into the AE-owned sre-agent-aep Secret and to bounce the RCA/SRE agent
// Deployment afterwards (patch its aep.wso2.com/sre-llm-hash annotation, scale
// it back up) — never blanket namespace access.
const sreAgentPushRoleTmpl = `
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: {name: aep-api-sre-push, namespace: {{.ObsNamespace}}}
rules:
  - {apiGroups: [""], resources: [secrets], resourceNames: ["sre-agent-aep"], verbs: [get, update, patch]}
  - {apiGroups: [apps], resources: [deployments], resourceNames: ["{{.RcaName}}"], verbs: [get, patch]}
  - {apiGroups: [apps], resources: [deployments/scale], resourceNames: ["{{.RcaName}}"], verbs: [get, patch]}
  - {apiGroups: [""], resources: [pods], verbs: [list]}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: {name: aep-api-sre-push, namespace: {{.ObsNamespace}}}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: aep-api-sre-push}
subjects:
  - {kind: ServiceAccount, name: aep-api, namespace: {{.AEPNamespace}}}
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

// Cross-namespace HTTPRoute + ClusterObservabilityPlane CR, applied only when
// aectl installs the plane: an existing plane's installer registered its own.
const srePlaneCRsTmpl = `
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

// Authz grants for the observer's reader client and the SRE agent's handoff
// (component:create for the coding-agent dispatch pre-check). Applied in both
// modes. Keyed on client_id, where ThunderID 1.0 puts a service account's
// subject, like the control plane's own service-account bindings.
const sreGrantsTmpl = `
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
    claim: client_id
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
    claim: client_id
    value: openchoreo-rca-agent
  roleMappings:
    - roleRef:
        kind: ClusterAuthzRole
        name: rca-agent-dispatch
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
