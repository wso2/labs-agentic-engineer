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

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/auth"

	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/build"
	"github.com/wso2/aep/aep-api/internal/feature/component"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/internal/feature/files"
	"github.com/wso2/aep/aep-api/internal/feature/genai"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/feature/idp"
	"github.com/wso2/aep/aep-api/internal/feature/organization"
	"github.com/wso2/aep/aep-api/internal/feature/orgconfig"
	"github.com/wso2/aep/aep-api/internal/feature/orgcreds"
	"github.com/wso2/aep/aep-api/internal/feature/project"
	"github.com/wso2/aep/aep-api/internal/feature/provisioning"
	"github.com/wso2/aep/aep-api/internal/feature/rcaagent"
	"github.com/wso2/aep/aep-api/internal/feature/requirements"
	"github.com/wso2/aep/aep-api/internal/feature/skills"
	"github.com/wso2/aep/aep-api/internal/feature/task"
)

// HumaDeps carries every dependency the code-first feature registrations need.
// main.go fills it with the real services; the OpenAPI spec generator passes
// the zero value (nil deps — registration never invokes them). Keeping the
// registration list in one place (RegisterAllHuma) is what keeps the served
// handler, the spec artifact, and the tests in lockstep.
type HumaDeps struct {
	ProjectSvc        project.ProjectService
	OrgSvc            organization.OrganizationService
	ComponentSvc      component.ComponentService
	ConfigSvc         component.ConfigService
	CollabRepo        gitrepo.RepoService
	IssueSvc          gitrepo.IssueService
	ProvisioningSvc     *provisioning.Service
	ResourceTypeCatalog dependencies.ResourceTypeLister
	TaskReads         *task.Reads
	TaskCommands      *task.Commands
	TaskPlan          *task.PlanService
	TaskStream        *execution.TaskStreamService
	ComponentClient   openchoreo.ComponentClient
	IDPSvc            idp.IDPService
	CredentialSvc     *orgcreds.CredentialService
	DisconnectSvc     *orgcreds.OrgDisconnectService
	BearerSvc         *orgcreds.BearerService
	AnthropicSvc      *orgcreds.AnthropicCredentialService
	OrgConfigSvc      *orgconfig.Service
	TaskTokens        *auth.TaskTokenManager
	SkillSvc          *skills.SkillService
	SkillMutationSvc  *skills.SkillMutationService
	SkillImportSvc    *skills.SkillImportService
	FilesSvc          files.FilesService
	ArtifactSvc       artifacts.ArtifactService
	GenAISvc          genai.GenAIService
	BuildSvc          *build.Service
	RcaAgentReportSvc rcaagent.RcaAgentReportService
	PreflightSvc      *build.PreflightService
	GitHubAppSlug     string
	BFFPublicURL      string
	GitHubAppClientID string
}

// RegisterAllHuma registers every migrated feature's operations on the Huma API.
// This is the single canonical list — used by NewHandler (real deps), the spec
// generator (zero deps), and the registration test.
func RegisterAllHuma(api huma.API, d HumaDeps) {
	project.RegisterProject(api, d.ProjectSvc)
	organization.RegisterOrganization(api, d.OrgSvc)
	component.RegisterComponent(api, d.ComponentSvc)
	component.RegisterConfig(api, d.ConfigSvc)
	requirements.RegisterCollab(api, d.CollabRepo)
	gitrepo.RegisterIssue(api, d.IssueSvc)
	provisioning.RegisterResources(api, d.ProvisioningSvc)
	dependencies.RegisterResourceTypes(api, d.ResourceTypeCatalog)
	task.RegisterTask(api, d.TaskReads, d.TaskCommands, d.TaskPlan)
	execution.RegisterTaskStream(api, d.TaskStream)
	orgconfig.RegisterConfig(api, d.OrgConfigSvc)
	skills.RegisterSkill(api, d.SkillSvc, d.SkillMutationSvc, d.SkillImportSvc)
	files.RegisterFiles(api, d.FilesSvc)
	artifacts.RegisterTags(api, d.ArtifactSvc)
	genai.RegisterGenAI(api, d.GenAISvc)
	build.RegisterBuild(api, d.BuildSvc)
	rcaagent.RegisterRcaAgentReports(api, d.RcaAgentReportSvc)
	build.RegisterPreflight(api, d.PreflightSvc)
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
