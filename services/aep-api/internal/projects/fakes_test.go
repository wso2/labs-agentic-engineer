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

// In-package (package component) hand fakes for the component/config unit +
// dbtest + watcher tiers. Only the methods a case
// programs are set; the rest panic loudly so an unexpected call fails the test
// rather than returning a silent zero value. The component tier (external
// package component_test) carries its own copies — see component_component_test.go.
package projects

import (
	"context"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// --- observability.Client ----------------------------------------------------

type stubObservClient struct {
	GetBuildLogsFunc func(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error)
}

var _ observability.Client = (*stubObservClient)(nil)

func (s *stubObservClient) GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, since time.Time) (*gen.BuildLogs, error) {
	if s.GetBuildLogsFunc == nil {
		panic("stubObservClient: GetBuildLogs not set")
	}
	return s.GetBuildLogsFunc(ctx, orgName, projectName, componentName, buildName, since)
}

func (s *stubObservClient) QueryComponentLogs(context.Context, observability.ComponentLogQuery) ([]observability.LogLine, error) {
	panic("stubObservClient: QueryComponentLogs not expected")
}

// --- sourcecontrol.RepoService (only GetRepo is consulted by TriggerBuild) ----------

type stubRepoSvc struct {
	GetRepoFunc func(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

var _ sourcecontrol.RepoService = (*stubRepoSvc)(nil)

func (s *stubRepoSvc) GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	if s.GetRepoFunc == nil {
		panic("stubRepoSvc: GetRepo not set")
	}
	return s.GetRepoFunc(ctx, orgID, projectID)
}
func (s *stubRepoSvc) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	panic("stubRepoSvc: ListByOrg not expected in component tests")
}
func (s *stubRepoSvc) CreateRepo(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("stubRepoSvc: CreateRepo not expected")
}
func (s *stubRepoSvc) EnsureBareRepo(context.Context, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("stubRepoSvc: EnsureBareRepo not expected")
}
func (s *stubRepoSvc) SetWebhookID(context.Context, string, string, int64) error {
	panic("stubRepoSvc: SetWebhookID not expected")
}
func (s *stubRepoSvc) DeleteRepo(context.Context, string, string) error {
	panic("stubRepoSvc: DeleteRepo not expected")
}

// --- BuildSecretStager --------------------------------------------------------

type stubBuildStager struct {
	StageBuildSecretFunc func(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (string, error)
}

var _ BuildSecretStager = (*stubBuildStager)(nil)

func (s *stubBuildStager) StageBuildSecret(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (string, error) {
	if s.StageBuildSecretFunc == nil {
		panic("stubBuildStager: StageBuildSecret not set")
	}
	return s.StageBuildSecretFunc(ctx, ocOrgID, repoSlug, workflowRunName)
}

// --- ComponentService (the config-mirror seam; only UpdateWorkflowEnvVars) ----

type stubComponentSvc struct {
	UpdateWorkflowEnvVarsFunc func(ctx context.Context, orgName, projectName, componentName string, envVars []openchoreo.WorkflowEnvVarRef) error
}

var _ ComponentService = (*stubComponentSvc)(nil)

// No key configured is the honest default for a fake: (nil, nil) is what a
// real org without a connected key yields, and it keeps MODEL_* out of every
// test that is not about model access.
func (s *stubComponentSvc) ModelAccessEnvVars(context.Context, string, string) ([]openchoreo.WorkflowEnvVarRef, error) {
	return nil, nil
}

func (s *stubComponentSvc) UpdateWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []openchoreo.WorkflowEnvVarRef) error {
	if s.UpdateWorkflowEnvVarsFunc == nil {
		panic("stubComponentSvc: UpdateWorkflowEnvVars not set")
	}
	return s.UpdateWorkflowEnvVarsFunc(ctx, orgName, projectName, componentName, envVars)
}
func (s *stubComponentSvc) ListComponents(context.Context, string, string, int, string) (*gen.ComponentList, error) {
	panic("stubComponentSvc: ListComponents not expected")
}
func (s *stubComponentSvc) GetComponent(context.Context, string, string, string) (*gen.Component, error) {
	panic("stubComponentSvc: GetComponent not expected")
}
func (s *stubComponentSvc) EnsureComponent(context.Context, string, string, string) error {
	return nil
}
func (s *stubComponentSvc) SyncProjectModelAccess(context.Context, string, string) error {
	return nil
}
func (s *stubComponentSvc) CreateComponent(context.Context, string, string, *openchoreo.CreateComponentRequest) (*gen.Component, error) {
	panic("stubComponentSvc: CreateComponent not expected")
}
func (s *stubComponentSvc) ListDeployments(context.Context, string, string, string) (*gen.DeploymentList, error) {
	panic("stubComponentSvc: ListDeployments not expected")
}
func (s *stubComponentSvc) GetComponentOpenAPI(context.Context, string, string, string) (*gen.ComponentOpenAPI, error) {
	panic("stubComponentSvc: GetComponentOpenAPI not expected")
}
func (s *stubComponentSvc) TriggerBuild(context.Context, string, string, string) (*gen.WorkflowRun, error) {
	panic("stubComponentSvc: TriggerBuild not expected")
}
func (s *stubComponentSvc) ListBuilds(context.Context, string, string, string, int, string) (*gen.WorkflowRunList, error) {
	panic("stubComponentSvc: ListBuilds not expected")
}
func (s *stubComponentSvc) GetBuildLogs(context.Context, string, string, string, string, int64) (*gen.BuildLogs, error) {
	panic("stubComponentSvc: GetBuildLogs not expected")
}

// --- AnthropicKeyResolver (ai-agent model access) -----------------------------

type stubAnthropicKeyResolver struct {
	DefaultKeyRefFunc func(ctx context.Context, ocOrgID string) (organization.SecretRefTriplet, error)
}

var _ AnthropicKeyResolver = (*stubAnthropicKeyResolver)(nil)

func (s *stubAnthropicKeyResolver) DefaultKeyRef(ctx context.Context, ocOrgID string) (organization.SecretRefTriplet, error) {
	if s.DefaultKeyRefFunc == nil {
		panic("stubAnthropicKeyResolver: DefaultKeyRef not set")
	}
	return s.DefaultKeyRefFunc(ctx, ocOrgID)
}

// --- secretmanagersvc.OpenChoreoSecretReferenceClient (model access SecretReference) ---

type stubSecretReferenceClient struct {
	GetSecretReferenceFunc    func(ctx context.Context, orgNS, name string) (*secretmanagersvc.SecretReference, error)
	CreateSecretReferenceFunc func(ctx context.Context, orgNS string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error)
	UpdateSecretReferenceFunc func(ctx context.Context, orgNS, name string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error)
	DeleteSecretReferenceFunc func(ctx context.Context, orgNS, name string) error
}

var _ secretmanagersvc.OpenChoreoSecretReferenceClient = (*stubSecretReferenceClient)(nil)

func (s *stubSecretReferenceClient) GetSecretReference(ctx context.Context, orgNS, name string) (*secretmanagersvc.SecretReference, error) {
	if s.GetSecretReferenceFunc == nil {
		panic("stubSecretReferenceClient: GetSecretReference not set")
	}
	return s.GetSecretReferenceFunc(ctx, orgNS, name)
}
func (s *stubSecretReferenceClient) CreateSecretReference(ctx context.Context, orgNS string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	if s.CreateSecretReferenceFunc == nil {
		panic("stubSecretReferenceClient: CreateSecretReference not set")
	}
	return s.CreateSecretReferenceFunc(ctx, orgNS, req)
}
func (s *stubSecretReferenceClient) UpdateSecretReference(ctx context.Context, orgNS, name string, req secretmanagersvc.CreateSecretReferenceRequest) (*secretmanagersvc.SecretReference, error) {
	if s.UpdateSecretReferenceFunc == nil {
		panic("stubSecretReferenceClient: UpdateSecretReference not set")
	}
	return s.UpdateSecretReferenceFunc(ctx, orgNS, name, req)
}
func (s *stubSecretReferenceClient) DeleteSecretReference(ctx context.Context, orgNS, name string) error {
	if s.DeleteSecretReferenceFunc == nil {
		panic("stubSecretReferenceClient: DeleteSecretReference not set")
	}
	return s.DeleteSecretReferenceFunc(ctx, orgNS, name)
}

// --- ConfigRepository (hand fake for the config unit tier) -------

type stubConfigRepo struct {
	GetByComponentFunc func(ctx context.Context, orgID, projectName, componentName string) (*ComponentConfig, error)
	UpsertFunc         func(ctx context.Context, config *ComponentConfig) error
}

var _ ConfigRepository = (*stubConfigRepo)(nil)

func (s *stubConfigRepo) GetByComponent(ctx context.Context, orgID, projectName, componentName string) (*ComponentConfig, error) {
	if s.GetByComponentFunc == nil {
		panic("stubConfigRepo: GetByComponent not set")
	}
	return s.GetByComponentFunc(ctx, orgID, projectName, componentName)
}
func (s *stubConfigRepo) Upsert(ctx context.Context, config *ComponentConfig) error {
	if s.UpsertFunc == nil {
		panic("stubConfigRepo: Upsert not set")
	}
	return s.UpsertFunc(ctx, config)
}
func (s *stubConfigRepo) DeleteAll(context.Context) error {
	panic("stubConfigRepo: DeleteAll not expected")
}
