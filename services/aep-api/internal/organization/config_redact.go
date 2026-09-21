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

import (
	"slices"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// RedactConfigForPermissions nils out the ConfigProjection sections the
// caller's held permissions don't cover, in place. Shared by getconfig
// (GET /config) and patchconfig (PATCH /config): both hand a caller the full
// projection as their response body, and both are gated to require only ONE
// of ae:github-config/ae:model-config (an OR, so either half's caller is
// answered at all) — this is what turns that coarse allow into "only the
// section you actually hold permission for".
//
// codingAgent is trimmed rather than cleared. The section is always present by
// contract, and its runtime/model pair is enum-constrained state the console
// renders; what it should not disclose is updatedAt/updatedBy, the audit fields
// that exist precisely BECAUSE this endpoint's permissions are coarse (see
// OrgCodingAgentSetting.UpdatedBy). Writing the section needs ae:model-config,
// so a caller without it has no business reading who last changed it.
//
// idp is left whole: no AE permission describes identity configuration, so
// there is nothing to redact it against. Its write path is refused outright at
// the gate (see the edge's updateConfigPermissions), and the section discloses
// no credential — the live publisher secret is never projected, only whether
// one exists. Redacting it becomes possible, and worth doing, when identity
// config gets a permission.
func RedactConfigForPermissions(proj *orgconfig.ConfigProjection, held []authz.Permission) {
	if !slices.Contains(held, authz.PermissionGitHubConfig) {
		proj.GitProvider = nil
	}
	if !slices.Contains(held, authz.PermissionModelConfig) {
		proj.LLM = nil
		proj.CodingLLM = nil
		proj.CodingAgent.UpdatedAt = nil
		proj.CodingAgent.UpdatedBy = nil
	}
}
