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

// Package addons declares optional platform resource types that can be
// installed after `aectl platform install`. Each Addon bundles one or more
// Kubernetes manifests applied via server-side apply. To add a new addon,
// append an entry to Available and embed its manifest(s) as string literals.
package addons

// OperatorSpec describes a Helm-installed operator that must be present before
// the addon's manifests can be applied. A zero-value OperatorSpec (ReleaseName == "")
// means the addon has no operator dependency and the pre-install phase is skipped.
type OperatorSpec struct {
	// ReleaseName is the Helm release name (e.g. "cnpg", "thunder-app-operator").
	ReleaseName string
	// Chart is the OCI or local chart reference.
	Chart string
	// Version is the chart version string; empty omits --version (uses registry default).
	Version string
	// Namespace is the target namespace for the operator deployment.
	Namespace string
	// DisplayName is the human-readable label shown in the confirmation list and summary.
	DisplayName string
	// Sets is optional key=value pairs passed as --set flags to Helm.
	Sets []string
	// PreManifests are YAML strings server-side-applied in order before the Helm
	// chart is installed. The operator namespace is created first. Use this for
	// resources the operator Pod depends on at startup (e.g. a credentials Secret
	// managed by ESO rather than by the chart itself).
	PreManifests []string
	// WaitForSecrets lists Secret names (in Namespace) that must exist before
	// the Helm chart is installed. Use this when the platform chart creates an
	// ESO ExternalSecret for the operator — ESO sync is async, so install must
	// wait until the Secret is actually present or the operator Pod will fail to
	// start with a missing-secret error.
	WaitForSecrets []string
}

// Addon describes an optional platform resource type.
type Addon struct {
	ID          string
	Label       string
	Description string
	// Operator, when non-zero (ReleaseName != ""), is installed via Helm before
	// the addon manifests are applied.
	Operator OperatorSpec
	// Manifests is a list of YAML strings applied in order via server-side apply.
	// Each string may contain multiple documents separated by ---.
	Manifests []string
	// VerifyResources lists key objects that must exist after apply to confirm
	// the addon was actually accepted by the cluster.
	VerifyResources []VerifySpec
}

// VerifySpec identifies a single cluster object to GET after apply.
type VerifySpec struct {
	APIVersion string
	Kind       string
	Namespace  string // empty for cluster-scoped
	Name       string
}

// Available is the ordered list of optional addons shown to the operator after
// platform install. Add new entries here to surface them in the interactive
// selector.
var Available = []Addon{
	{
		ID:          "thunder-app",
		Label:       "thunder-app",
		Description: "ThunderApplication ClusterResourceType + RBAC",
		Operator: OperatorSpec{
			ReleaseName: "thunder-app-operator",
			Chart:       "oci://ghcr.io/wso2/thunder-app-operator",
			Namespace:   "thunder-app-operator-system",
			DisplayName: "thunder-app-operator",
			// The platform chart creates an ESO ExternalSecret that syncs to
			// this Secret. ESO sync is async; wait before Helm install so the
			// operator Pod starts with credentials already present.
			WaitForSecrets: []string{"thunder-app-operator-credentials"},
		},
		Manifests: []string{thunderAppResourceType, thunderAppRBAC},
		VerifyResources: []VerifySpec{
			{APIVersion: "openchoreo.dev/v1alpha1", Kind: "ClusterResourceType", Name: "thunder-app"},
			{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "openchoreo-dataplane-thunder-app"},
			{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding", Name: "openchoreo-dataplane-thunder-app"},
		},
	},
	{
		ID:          "postgres-cnpg",
		Label:       "postgres-cnpg",
		Description: "PostgreSQL via CloudNativePG (ClusterResourceType + RBAC)",
		Operator: OperatorSpec{
			ReleaseName: "cnpg",
			Chart:       "oci://ghcr.io/cloudnative-pg/charts/cloudnative-pg",
			Version:     "0.29.0", // keep in sync with deployments/scripts/env.sh CNPG_VERSION
			Namespace:   "cnpg-system",
			DisplayName: "CloudNativePG v0.29.0",
		},
		Manifests: []string{postgresCNPGResourceType, postgresCNPGRBAC},
		VerifyResources: []VerifySpec{
			{APIVersion: "openchoreo.dev/v1alpha1", Kind: "ClusterResourceType", Name: "postgres-cnpg"},
			{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRole", Name: "openchoreo-dataplane-cnpg"},
			{APIVersion: "rbac.authorization.k8s.io/v1", Kind: "ClusterRoleBinding", Name: "openchoreo-dataplane-cnpg"},
		},
	},
}

// thunderAppResourceType is the ClusterResourceType that makes the thunder-app
// OAuth provisioning available as a platform-resource dependency type in AEP.
// Source: deployments/single-cluster/resource-types/thunder-app/resourcetype.yaml
const thunderAppResourceType = `
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterResourceType
metadata:
  name: thunder-app
  labels:
    aep.wso2.com/role: end-user-auth
  annotations:
    aep.wso2.com/description: >-
      End-user sign-in for this project's apps: provisions an OAuth (PKCE)
      client on the platform IdP. Declare on both the web app that signs
      users in and the service whose API it protects.
    aep.wso2.com/consumer-url-env-config: redirectUris
    aep.wso2.com/skill: thunder-authentication
spec:
  retainPolicy: Delete
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        displayName:
          type: string
          default: ""
        scopes:
          type: string
          default: "openid profile email group ou"
  environmentConfigs:
    openAPIV3Schema:
      type: object
      properties:
        redirectUris:
          type: string
          default: ""
  resources:
    - id: app
      readyWhen: '${true}'
      template:
        apiVersion: aep.wso2.com/v1alpha1
        kind: ThunderApplication
        metadata:
          name: ${metadata.name}
          namespace: ${metadata.namespace}
          labels: ${metadata.labels}
        spec:
          displayName: ${parameters.displayName}
          scopes: ${parameters.scopes}
          redirectUris: ${environmentConfigs.redirectUris}
  outputs:
    - name: client_id
      value: aep-${metadata.namespace}-${metadata.name}
    - name: issuer
      value: http://thunder.openchoreo.localhost:8080
    - name: jwks_url
      value: http://thunder.openchoreo.localhost:8080/oauth2/jwks
    - name: scopes
      value: ${parameters.scopes}
`

// thunderAppRBAC grants the OpenChoreo data-plane agent permission to manage
// ThunderApplication objects in project namespaces.
// Source: deployments/single-cluster/resource-types/thunder-app/rbac.yaml
const thunderAppRBAC = `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: openchoreo-dataplane-thunder-app
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
rules:
  - apiGroups: ["aep.wso2.com"]
    resources: ["thunderapplications"]
    verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: openchoreo-dataplane-thunder-app
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: openchoreo-dataplane-thunder-app
subjects:
  - kind: ServiceAccount
    name: cluster-agent-dataplane
    namespace: openchoreo-data-plane
`

// postgresCNPGResourceType is the ClusterResourceType that makes postgres-cnpg
// available as a platform-resource dependency type in the AEP console.
// Source: deployments/single-cluster/resource-types/postgres-cnpg/resourcetype.yaml
const postgresCNPGResourceType = `
apiVersion: openchoreo.dev/v1alpha1
kind: ClusterResourceType
metadata:
  name: postgres-cnpg
  annotations:
    aep.wso2.com/description: >-
      A dedicated PostgreSQL database cluster provisioned inside the platform
      (CloudNativePG). Declare on the service that owns the data.
spec:
  retainPolicy: Delete
  parameters:
    openAPIV3Schema:
      type: object
      properties:
        version:
          type: string
          default: "16"
          enum: ["16", "15"]
        storage:
          type: string
          default: "1Gi"
          enum: ["1Gi", "5Gi", "10Gi"]
        instances:
          type: integer
          default: 1
          minimum: 1
          maximum: 3
  resources:
    - id: cluster
      template:
        apiVersion: postgresql.cnpg.io/v1
        kind: Cluster
        metadata:
          name: ${metadata.name}
          namespace: ${metadata.namespace}
          labels: ${metadata.labels}
        spec:
          instances: ${parameters.instances}
          imageName: ghcr.io/cloudnative-pg/postgresql:${parameters.version}
          storage:
            size: ${parameters.storage}
          bootstrap:
            initdb:
              database: appdb
              owner: appuser
  outputs:
    - name: host
      value: ${metadata.name}-rw.${metadata.namespace}.svc.cluster.local
    - name: port
      value: "5432"
    - name: dbname
      value: appdb
    - name: user
      secretKeyRef:
        name: ${metadata.name}-app
        key: username
    - name: password
      secretKeyRef:
        name: ${metadata.name}-app
        key: password
`

// postgresCNPGRBAC grants the OpenChoreo data-plane agent permission to manage
// CloudNativePG Cluster objects in project namespaces.
// Source: deployments/single-cluster/resource-types/postgres-cnpg/rbac.yaml
const postgresCNPGRBAC = `
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: openchoreo-dataplane-cnpg
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
rules:
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters"]
    verbs: ["create", "get", "list", "watch", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: openchoreo-dataplane-cnpg
  labels:
    app.kubernetes.io/part-of: wso2-agentic-engineer
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: openchoreo-dataplane-cnpg
subjects:
  - kind: ServiceAccount
    name: cluster-agent-dataplane
    namespace: openchoreo-data-plane
`
