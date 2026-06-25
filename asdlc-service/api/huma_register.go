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

package api

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/auth"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/component"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/connections"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/design"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/idp"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/organization"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/project"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/requirements"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/skills"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/task"
)

// HumaDeps carries every dependency the code-first feature registrations need.
// main.go fills it with the real services; the OpenAPI spec generator passes
// the zero value (nil deps — registration never invokes them). Keeping the
// registration list in one place (RegisterAllHuma) is what keeps the served
// handler, the spec artifact, and the tests in lockstep.
type HumaDeps struct {
	ProjectSvc          project.ProjectService
	OrgSvc              organization.OrganizationService
	ComponentSvc        component.ComponentService
	ConfigSvc           component.ConfigService
	RequirementsSvc     requirements.RequirementsService
	RequirementsChatSvc requirements.RequirementsChatService
	CollabRepo          gitrepo.RepoService
	DesignSvc           design.DesignService
	TaskSvc             task.TaskService
	TaskDispatcher      task.TaskDispatcher
	TaskProgress        task.ProgressReader
	ComponentClient     openchoreo.ComponentClient
	BoardSvc            task.BoardService
	IDPSvc              idp.IDPService
	CredentialSvc       *orgcreds.CredentialService
	DisconnectSvc       *orgcreds.OrgDisconnectService
	BearerSvc           *orgcreds.BearerService
	AnthropicSvc        *orgcreds.AnthropicCredentialService
	TaskTokens          *auth.TaskTokenManager
	SkillSvc            *skills.SkillService
	SkillMutationSvc    *skills.SkillMutationService
	SkillImportSvc      *skills.SkillImportService
	GitHubAppSlug       string
	BFFPublicURL        string
	GitHubAppClientID   string
	ConnectionValueSvc  *connections.ValueService
}

// RegisterAllHuma registers every migrated feature's operations on the Huma API.
// This is the single canonical list — used by NewHandler (real deps), the spec
// generator (zero deps), and the registration test.
func RegisterAllHuma(api huma.API, d HumaDeps) {
	project.RegisterProject(api, d.ProjectSvc)
	organization.RegisterOrganization(api, d.OrgSvc)
	component.RegisterComponent(api, d.ComponentSvc)
	component.RegisterConfig(api, d.ConfigSvc)
	requirements.RegisterRequirements(api, d.RequirementsSvc)
	requirements.RegisterRequirementsChat(api, d.RequirementsChatSvc)
	requirements.RegisterCollab(api, d.CollabRepo)
	design.RegisterDesign(api, d.DesignSvc)
	task.RegisterTask(api, d.TaskSvc, d.TaskDispatcher, d.TaskProgress, d.ComponentClient)
	task.RegisterBoard(api, d.BoardSvc)
	idp.RegisterIDP(api, d.IDPSvc)
	orgcreds.RegisterOrgGitHub(api, d.CredentialSvc, d.DisconnectSvc, d.BearerSvc, d.GitHubAppSlug, d.BFFPublicURL, d.GitHubAppClientID)
	orgcreds.RegisterOrgAnthropic(api, d.AnthropicSvc)
	skills.RegisterSkill(api, d.SkillSvc, d.SkillMutationSvc, d.SkillImportSvc)
	connections.RegisterConnections(api, d.ConnectionValueSvc)
	registerInfraHuma(api, d.TaskTokens, d.AnthropicSvc)
}

// GenerateOpenAPIYAML builds the full OpenAPI document (all migrated features)
// and returns it as YAML. Used by the `make openapi` generator and the
// spec-freshness drift guard. Deps are nil — registration is pure.
func GenerateOpenAPIYAML() ([]byte, error) {
	apiMux := http.NewServeMux()
	humaAPI := newHumaAPI(apiMux)
	RegisterAllHuma(humaAPI, HumaDeps{})
	return humaAPI.OpenAPI().YAML()
}
