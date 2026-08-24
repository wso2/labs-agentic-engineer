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

package delivery

// ProvisionInput is one dependency's resolved provisioning payload, produced by
// the build click from the drawer inputs and handed to the gate resolver. It is
// the wire contract between POST /build (which stages secrets to SM-API and
// derives references) and `dependencies/provisioning` (which authors the OC
// Resource model + the milestone's `provision` gate issues).
//
// It lives at the domain ROOT because the two ends may not name each other: the
// build slice produces it and another domain consumes it through the build's
// GateResolver port.
//
// A raw secret value is NEVER placed here — SecretRefByEnv holds the SM-API
// reference per env instead.
type ProvisionInput struct {
	Component  string `json:"component"`
	Dependency string `json:"dependency"`
	Kind       string `json:"kind"`
	// external non-secret config by key.
	Config map[string]string `json:"config,omitempty"`
	// external: the SM-API secret reference per env (NOT the secret value).
	SecretRefByEnv map[string]string `json:"secretRefByEnv,omitempty"`
	// platform-resource: provisioning params (mixed scalar types).
	Parameters map[string]any `json:"parameters,omitempty"`
	// platform-resource / org-service: the user's approval.
	Approved bool `json:"approved,omitempty"`
}
