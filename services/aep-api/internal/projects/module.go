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

package projects

// Deps is what this domain must be handed to exist: the concrete project
// service plus the component/config service ports the slices call. Constructor
// injection only.
//
// It lives in the domain ROOT, but the thing that CONSUMES it (the aggregator
// that builds the slice handlers) lives in httpapi/ — see httpapi/doc.go for why
// the domain's composition cannot sit here.
type Deps struct {
	// ProjectSvc is the concrete project CRUD + status service behind the five
	// project ops (list / create / get / delete / status).
	ProjectSvc *Service
	// ComponentSvc is the component read + build + deploy service behind the
	// component and build ops.
	ComponentSvc ComponentService
	// ConfigSvc is the component env-var config service behind the two config ops.
	ConfigSvc ConfigService
	// UsageSvc is the org-wide agent-usage roll-up behind list-project-usage
	// (#291): the Settings → Usage page's per-project cost cards.
	UsageSvc *UsageService
}
