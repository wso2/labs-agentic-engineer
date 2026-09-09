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

package authz

// OcActionCatalog is the AE-permission -> OpenChoreo AuthzRole action mapping
// fed to NewAuthZBridge. PLACEHOLDER (issue #743 decision): both AE
// permissions currently defined in rolePermissionsCatalog resolve to the same
// starter action pair — component:view, component:create — pending a real
// per-permission OC action design. Revisit before this grants anything beyond
// the coding agent's own component read/create needs.
var OcActionCatalog = map[string][]string{
	"ae:model-config": {"component:view", "component:create"},
	"ae:skill-config": {"component:view", "component:create"},
}
