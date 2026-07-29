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

package codingagent

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func strPtr(s string) *string { return &s }

type fakeOrgRepo struct {
	org *organization.Organization
}

func (f fakeOrgRepo) ListByNames(context.Context, []string) ([]organization.Organization, error) {
	return nil, nil
}
func (f fakeOrgRepo) GetByName(context.Context, string) (*organization.Organization, error) {
	return f.org, nil
}
func (f fakeOrgRepo) Create(context.Context, *organization.Organization) error { return nil }
func (f fakeOrgRepo) SetThunderOrgUUID(context.Context, string, uuid.UUID) error {
	return nil
}

type fakeAnthropicCreds struct {
	row *organization.OrgAnthropicCredential
}

func (f fakeAnthropicCreds) GetByOrg(context.Context, string) (*organization.OrgAnthropicCredential, error) {
	return f.row, nil
}
func (f fakeAnthropicCreds) UpdateColumns(context.Context, string, map[string]any) error { return nil }
func (f fakeAnthropicCreds) Tx(context.Context, func(organization.OrgAnthropicTx) error) error {
	return nil
}

type fakeGitHubCreds struct {
	row *organization.OrgCredential
}

func (f fakeGitHubCreds) GetByOrg(context.Context, string) (*organization.OrgCredential, error) {
	return f.row, nil
}
func (f fakeGitHubCreds) GetByInstallationID(context.Context, int64) (*organization.OrgCredential, error) {
	return nil, nil
}
func (f fakeGitHubCreds) UpdateColumns(context.Context, string, map[string]any) error { return nil }
func (f fakeGitHubCreds) ListActiveRows(context.Context) ([]organization.OrgCredential, error) {
	return nil, nil
}
func (f fakeGitHubCreds) ListBoundInstallations(context.Context) ([]organization.BoundInstallation, error) {
	return nil, nil
}
func (f fakeGitHubCreds) OrgIDByRepoURL(context.Context, string) (string, error) { return "", nil }
func (f fakeGitHubCreds) Tx(context.Context, func(organization.OrgCredentialTx) error) error {
	return nil
}

func fullSecretRefs() (*organization.OrgAnthropicCredential, *organization.OrgCredential) {
	return &organization.OrgAnthropicCredential{
			SMAPISecretRefName: strPtr("acme-anthropic-secrets"),
			SMAPIKVPath:        strPtr("user-app-secrets/wc-acme/acme-anthropic-secrets"),
			SMAPIProperty:      strPtr("api-key"),
		}, &organization.OrgCredential{
			SMAPISecretRefName: strPtr("acme-github-pat-secrets"),
			SMAPIKVPath:        strPtr("user-app-secrets/wc-acme/acme-github-pat-secrets"),
			SMAPIProperty:      strPtr("token"),
		}
}

func newCodingDispatchExecutor(anthropic *organization.OrgAnthropicCredential, github *organization.OrgCredential, k8sJob *K8sJobDispatcher, proxyConfigured bool) *CodingExecutor {
	orgUUID := uuid.MustParse("d3adbeef-1234-4321-abcd-c0ffee123456")
	e := NewCodingExecutor(
		nil,
		fakeRepos{repo: &sourcecontrol.GitRepository{RepoURL: "https://github.com/acme/widgets", RepoSlug: "acme-widgets"}},
		fakeIdentities{},
		nil,
		fakeTokens{},
		newFakeExecRepo(&delivery.Execution{ID: "exec-1", OrgID: "acme", ProjectID: "widgets", Component: "svc", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued)}),
		"http://git",
		"http://platform",
		fakeOrgRepo{org: &organization.Organization{Name: "acme", UUID: orgUUID}},
		fakeAnthropicCreds{row: anthropic},
		fakeGitHubCreds{row: github},
		nil,
	)
	e.runnerImage = "runner:1"
	e.clusterSecretStore = "default"
	if proxyConfigured {
		e.proxy = &Dispatcher{}
	}
	if k8sJob != nil {
		e.WithK8sJobDispatch(k8sJob)
	}
	return e
}

func codingDispatchReq() delivery.DispatchRequest {
	return delivery.DispatchRequest{
		Execution: &delivery.Execution{ID: "exec-1", OrgID: "acme", ProjectID: "widgets", Component: "svc", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued)},
		Task:      delivery.TaskFacts{OrgID: "acme", ProjectID: "widgets", Component: "svc", IssueURL: "https://github.com/acme/widgets/issues/1", IssueNumber: 1},
	}
}

func TestRunCoding_ProxyConfigured_MissingAnthropicRef_ErrorsNoFallback(t *testing.T) {
	anthropic, github := fullSecretRefs()
	anthropic.SMAPIKVPath = nil
	e := newCodingDispatchExecutor(anthropic, github, &K8sJobDispatcher{}, true)

	err := e.Run(context.Background(), codingDispatchReq())
	if err == nil {
		t.Fatal("expected error when anthropic secret ref is missing")
	}
	if !strings.Contains(err.Error(), "anthropic") || !strings.Contains(err.Error(), "sm_api_kv_path") {
		t.Fatalf("error must name missing anthropic ref, got: %v", err)
	}
}

func TestRunCoding_ProxyConfigured_MissingGitHubRef_ErrorsNoFallback(t *testing.T) {
	anthropic, github := fullSecretRefs()
	github.SMAPISecretRefName = nil
	e := newCodingDispatchExecutor(anthropic, github, &K8sJobDispatcher{}, true)

	err := e.Run(context.Background(), codingDispatchReq())
	if err == nil {
		t.Fatal("expected error when github secret ref is missing")
	}
	if !strings.Contains(err.Error(), "github") {
		t.Fatalf("error must name missing github ref, got: %v", err)
	}
}

func TestRunCoding_K8sJobOnly_ErrorsSecretDeliveryRemoved(t *testing.T) {
	anthropic, github := fullSecretRefs()
	rec := newRecordingK8sClient()
	k8s := NewK8sJobDispatcher(rec, "http://platform", "runner:1")
	e := newCodingDispatchExecutor(anthropic, github, k8s, false)

	err := e.Run(context.Background(), codingDispatchReq())
	if err == nil {
		t.Fatal("expected error when only k8s-job path is configured")
	}
	if !strings.Contains(err.Error(), "plaintext secret delivery removed") {
		t.Fatalf("error must refuse k8s-job secret delivery, got: %v", err)
	}
	if len(rec.ops) != 0 {
		t.Fatalf("k8s-job dispatch must not write Secret/ExternalSecret, saw ops: %+v", rec.ops)
	}
}
