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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ThunderApplicationSpec — desired OAuth application on the Thunder that serves
// the (organization, environment) this CR is rendered into.
type ThunderApplicationSpec struct {
	// DisplayName shown in the Thunder console. Defaults to the CR name.
	DisplayName string `json:"displayName,omitempty"`
	// Scopes is the space-separated scope set this client is expected to request
	// — the OIDC scopes plus the project's API permission handles. The operator
	// splits it and writes it to the application's
	// inboundAuthConfig[oauth2].config.scopes (a JSON array on the wire).
	//
	// It is a truthful RECORD, not a gate: ThunderID 1.0.0 stores the list and
	// never enforces it (spike P1 §6). What narrows a token is group → role →
	// permissions, intersected with the resource server the request names.
	Scopes string `json:"scopes,omitempty"`
	// ValidityPeriod is the ACCESS token lifetime in seconds. Empty/zero leaves
	// the operator's 24h default, which is what every app wants; a short value
	// exists so a fixture app can exercise the silent renew in minutes instead
	// of a day (spike P6 used 300).
	// +kubebuilder:validation:Minimum=0
	ValidityPeriod int `json:"validityPeriod,omitempty"`
	// RedirectURIs is a comma-separated list of allowed OAuth redirect URIs.
	// Platform-managed: aep-api patches it via binding environmentConfigs once
	// the consuming SPA's public URL resolves. May be empty at creation.
	RedirectURIs string `json:"redirectUris,omitempty"`
	// ClientType is either "public" (default) or "confidential".
	// "public" creates a PKCE authorization_code client (browser SPA).
	// "confidential" creates a client_credentials client (service-to-service);
	// SecretRef is required in that case.
	// +kubebuilder:validation:Enum=public;confidential
	ClientType string `json:"clientType,omitempty"`
	// ClientID overrides the derived aep-<namespace>-<name> identity. Use this
	// for platform clients that must have a stable, well-known client ID.
	ClientID string `json:"clientId,omitempty"`
	// SecretRef points to the Kubernetes Secret key holding the pre-generated
	// OAuth client secret. Required when clientType=confidential.
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
	// NOTE: no instanceRef. The target Thunder is not a property of the app —
	// it is a property of the (org, environment) the app is rendered into, and
	// the operator resolves it from that environment's binding record. A future
	// BYO-instance field slots in here additively.
}

// SecretKeyRef selects a key from a Kubernetes Secret in the same namespace
// as the ThunderApplication CR. Cross-namespace references are not supported.
type SecretKeyRef struct {
	// Name of the secret.
	Name string `json:"name"`
	// Key within the secret.
	Key string `json:"key"`
}

// ThunderApplicationSpec now contains a SecretRef pointer — deepcopy.go
// handles it explicitly. Adding any further slice, map, or pointer fields
// requires a matching update there.

// ThunderApplicationStatus reports the observed state of a ThunderApplication.
type ThunderApplicationStatus struct {
	// Ready is the readyWhen gate the ClusterResourceType CEL reads.
	Ready bool `json:"ready"`
	// ClientID is the OAuth client_id assigned by Thunder.
	ClientID string `json:"clientId,omitempty"`
	// Issuer is the OIDC issuer of the Thunder instance this application was
	// registered on — the environment's own Thunder, resolved from the (org,
	// environment) binding record. Published so a consumer reads WHERE the
	// application lives instead of assuming one cluster-wide IdP.
	Issuer string `json:"issuer,omitempty"`
	// JWKSURL is that issuer's JWKS endpoint (issuer + /oauth2/jwks).
	JWKSURL string `json:"jwksUrl,omitempty"`
	// AdminURL is the in-cluster admin API base of the same instance. It is
	// the operator's own record of where the application was created, so a
	// later delete goes to that instance and not to whatever the binding
	// happens to name by then.
	AdminURL string `json:"adminUrl,omitempty"`
	// Message carries a human-readable status/error detail.
	Message string `json:"message,omitempty"`
	// ObservedGeneration is the most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// Reference-typed fields on ThunderApplicationStatus (slice/map/pointer)
// would require a matching update in deepcopy.go — see the doc comment
// there. (Not a doc comment on the type: keeping it detached avoids changing
// the generated CRD schema description.)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ThunderApplication is the Schema for the thunderapplications API.
type ThunderApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ThunderApplicationSpec   `json:"spec,omitempty"`
	Status ThunderApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ThunderApplicationList contains a list of ThunderApplication.
type ThunderApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ThunderApplication `json:"items"`
}
