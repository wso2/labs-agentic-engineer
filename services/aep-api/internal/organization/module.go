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

package organization

// Deps is what this domain must be handed to exist: typed ports / services,
// never concrete collaborators (§8). Constructor injection only.
//
// It lives in the domain ROOT, but the thing that CONSUMES it (the aggregator
// that builds the slice handlers) lives in httpapi/ — see httpapi/doc.go for why
// the domain's composition cannot sit here.
type Deps struct {
	// OrgSvc is the read-and-cache view of OC organizations, behind
	// list-organizations (the tenant-gate carve-out).
	OrgSvc OrganizationService
	// Config is the /config orchestrator behind the six org-config ops
	// (get/patch config + the four connect/disconnect/rotate/discover actions).
	Config *Service
	// AEStudio answers get-ae-studio. Required: an unwired reader panics at
	// the first request, like the other fail-loud collaborators.
	AEStudio AEStudioStatusReader
}
