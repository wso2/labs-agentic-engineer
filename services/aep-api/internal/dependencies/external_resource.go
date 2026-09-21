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

package dependencies

import (
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ConfigKeySlice is a slice of spec.ConfigKey.
type ConfigKeySlice []spec.ConfigKey

// ExternalResource is the in-memory definition of an external dependency —
// name + description + config-key schema — that the provisioner authors an
// OpenChoreo ResourceType from. It is built at request time from the project's
// committed design (design.json); AEP no longer persists external-resource
// definitions in its own database. The authored, org-namespaced ResourceType
// is the durable org-level registry (read back via
// openchoreo.ExternalDefinitionFromRT).
type ExternalResource struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Provider is the concrete system the resource is ("Open Exchange Rates"),
	// carried onto the authored type's record annotation.
	Provider string `json:"provider,omitempty"`
	// Registered marks a project's COPY of the organization's registered
	// resource. Its build must bind to the organization's type — the same
	// name register authored, reached by get-or-create — never author a
	// project-scoped one, which would sit beside the record under the same
	// logical name.
	Registered bool `json:"registered,omitempty"`
	// ConfigKeys is the resource's key schema (which env-var keys, which are
	// secret). This alone drives the OC ResourceType (no separate auth descriptor).
	ConfigKeys ConfigKeySlice `json:"config"`
}
