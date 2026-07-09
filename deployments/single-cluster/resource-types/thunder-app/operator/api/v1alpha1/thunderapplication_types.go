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

// ThunderApplicationSpec — desired OAuth application on the platform Thunder.
type ThunderApplicationSpec struct {
	// DisplayName shown in the Thunder console. Defaults to the CR name.
	DisplayName string `json:"displayName,omitempty"`
	// Scopes is the space-separated OIDC scope set (e.g. "openid profile email").
	Scopes string `json:"scopes,omitempty"`
	// RedirectURIs is a comma-separated list of allowed OAuth redirect URIs.
	// Platform-managed: aep-api patches it via binding environmentConfigs once
	// the consuming SPA's public URL resolves. May be empty at creation.
	RedirectURIs string `json:"redirectUris,omitempty"`
	// NOTE (v1 scope): no instanceRef — the operator always targets the single
	// platform Thunder. A future BYO field slots in here additively.
}

// Reference-typed fields on ThunderApplicationSpec (slice/map/pointer) would
// require a matching update in deepcopy.go — see the doc comment there. (Not
// a doc comment on the type: keeping it detached avoids changing the
// generated CRD schema description.)

// ThunderApplicationStatus reports the observed state of a ThunderApplication.
type ThunderApplicationStatus struct {
	// Ready is the readyWhen gate the ClusterResourceType CEL reads.
	Ready bool `json:"ready"`
	// ClientID is the OAuth client_id assigned by Thunder.
	ClientID string `json:"clientId,omitempty"`
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
