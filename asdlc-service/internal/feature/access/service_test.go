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

package access

import (
	"context"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/connections"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeRC embeds ResourceClient (nil) to satisfy the 12-method interface and
// overrides only ListWorkloadEndpoints — all the catalog needs.
type fakeRC struct {
	openchoreo.ResourceClient
	endpoints []openchoreo.WorkloadEndpointInfo
}

func (f *fakeRC) ListWorkloadEndpoints(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
	return f.endpoints, nil
}

// fakeIssueSvc records issues created; returns a fixed issue result.
type fakeIssueSvc struct {
	created   []gitrepo.CreateIssueRequest
	nextNum   int
	createErr error
}

func (f *fakeIssueSvc) CreateIssue(_ context.Context, _, _ string, req gitrepo.CreateIssueRequest) (*gitrepo.IssueResult, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	f.nextNum++
	return &gitrepo.IssueResult{Number: f.nextNum, URL: "https://github.com/acme/repo/issues/1", NodeID: "n"}, nil
}
func (f *fakeIssueSvc) ListIssues(context.Context, string, string, []string) ([]gitrepo.IssueInfo, error) {
	return nil, nil
}
func (f *fakeIssueSvc) CloseIssue(context.Context, string, string, int, string) error   { return nil }
func (f *fakeIssueSvc) CommentIssue(context.Context, string, string, int, string) error { return nil }
func (f *fakeIssueSvc) EditIssueBody(context.Context, string, string, int, string) error {
	return nil
}

// fakeTaskRepo records created tasks and assigns a stable ID.
type fakeTaskRepo struct {
	created []*models.ComponentTask
}

func (f *fakeTaskRepo) Create(_ context.Context, t *models.ComponentTask) error {
	t.ID = "task-1"
	f.created = append(f.created, t)
	return nil
}

// fakeRepo is an in-memory AccessRequest store (only the methods RequestAccess
// calls). It backs FindOpenForTarget / Create without a DB.
type fakeRepo struct {
	rows []*models.AccessRequest
}

func (r *fakeRepo) FindOpenForTarget(_ context.Context, orgID, providerProject, providerComponent string) (*models.AccessRequest, error) {
	for _, ar := range r.rows {
		if ar.OrgID == orgID && ar.ProviderProjectID == providerProject &&
			ar.ProviderComponentName == providerComponent &&
			(ar.Status == models.AccessRequestStatusRequested || ar.Status == models.AccessRequestStatusInProgress) {
			return ar, nil
		}
	}
	return nil, nil
}
func (r *fakeRepo) Create(_ context.Context, ar *models.AccessRequest) error {
	if ar.ID == "" {
		ar.ID = "ar-" + ar.ConsumerComponentName
	}
	r.rows = append(r.rows, ar)
	return nil
}
func (r *fakeRepo) ListByProviderTask(_ context.Context, providerTaskID string) ([]models.AccessRequest, error) {
	var out []models.AccessRequest
	for _, ar := range r.rows {
		if ar.ProviderTaskID == providerTaskID {
			out = append(out, *ar)
		}
	}
	return out, nil
}
func (r *fakeRepo) ListByConsumerProject(_ context.Context, orgID, projectID string) ([]models.AccessRequest, error) {
	var out []models.AccessRequest
	for _, ar := range r.rows {
		if ar.OrgID == orgID && ar.ConsumerProjectID == projectID {
			out = append(out, *ar)
		}
	}
	return out, nil
}
func (r *fakeRepo) UpdateStatus(_ context.Context, id, status string) error {
	for _, ar := range r.rows {
		if ar.ID == id {
			ar.Status = status
			return nil
		}
	}
	return ErrAccessRequestNotFound
}

func sampleEndpoints() []openchoreo.WorkloadEndpointInfo {
	return []openchoreo.WorkloadEndpointInfo{
		// OC component name = `<project>-<logical>`; published project-only.
		{Project: "hr-directory", Component: "hr-directory-employee-api",
			Workload: "hr-directory-employee-api-wl", Name: "http", Type: "HTTP",
			Port: 8080, Visibility: []string{"external"}},
	}
}

func newSvc(repo accessStore, cat *connections.OrgEndpointCatalog, iss gitrepo.IssueService, tr taskRepo) *AccessService {
	// store is nil — lookupAppPath tolerates it (best-effort).
	return NewAccessService(repo, cat, iss, tr, nil)
}

func TestRequestAccess_CreatesProviderTaskAndIssue(t *testing.T) {
	cat := connections.NewOrgEndpointCatalog(&fakeRC{endpoints: sampleEndpoints()})
	iss := &fakeIssueSvc{}
	tr := &fakeTaskRepo{}
	repo := &fakeRepo{}
	svc := newSvc(repo, cat, iss, tr)

	ar, err := svc.RequestAccess(context.Background(), RequestAccessInput{
		OrgHandle:         "acme",
		ConsumerProject:   "store-front",
		ConsumerComponent: "checkout",
		OrgServiceName:    "hr-directory-employee-api",
	})
	if err != nil {
		t.Fatalf("RequestAccess: %v", err)
	}
	if ar.ProviderProjectID != "hr-directory" {
		t.Fatalf("providerProject = %q, want hr-directory", ar.ProviderProjectID)
	}
	if ar.ProviderComponentName != "employee-api" {
		t.Fatalf("providerComponent = %q, want employee-api (logical)", ar.ProviderComponentName)
	}
	if ar.ProviderTaskID != "task-1" || ar.ProviderIssueNumber != 1 {
		t.Fatalf("provider task/issue not linked: %+v", ar)
	}
	if len(tr.created) != 1 || tr.created[0].Type != models.TaskTypeOrgPublish {
		t.Fatalf("want one org-publish task, got %+v", tr.created)
	}
	if len(iss.created) != 1 {
		t.Fatalf("want one issue created, got %d", len(iss.created))
	}
	hasLabel := false
	for _, l := range iss.created[0].Labels {
		if l == "access-request" {
			hasLabel = true
		}
	}
	if !hasLabel {
		t.Fatalf("issue missing access-request label: %v", iss.created[0].Labels)
	}
}

func TestRequestAccess_IdempotentDedupe(t *testing.T) {
	cat := connections.NewOrgEndpointCatalog(&fakeRC{endpoints: sampleEndpoints()})
	iss := &fakeIssueSvc{}
	tr := &fakeTaskRepo{}
	repo := &fakeRepo{}
	svc := newSvc(repo, cat, iss, tr)

	in1 := RequestAccessInput{OrgHandle: "acme", ConsumerProject: "store-front", ConsumerComponent: "checkout", OrgServiceName: "hr-directory-employee-api"}
	if _, err := svc.RequestAccess(context.Background(), in1); err != nil {
		t.Fatalf("first request: %v", err)
	}
	// Second consumer, same provider target → no new task/issue, new row riding
	// on the same provider task.
	in2 := RequestAccessInput{OrgHandle: "acme", ConsumerProject: "other", ConsumerComponent: "viewer", OrgServiceName: "hr-directory-employee-api"}
	ar2, err := svc.RequestAccess(context.Background(), in2)
	if err != nil {
		t.Fatalf("second request: %v", err)
	}
	if len(tr.created) != 1 {
		t.Fatalf("dedupe failed: want 1 provider task, got %d", len(tr.created))
	}
	if len(iss.created) != 1 {
		t.Fatalf("dedupe failed: want 1 issue, got %d", len(iss.created))
	}
	if ar2.ProviderTaskID != "task-1" {
		t.Fatalf("second row not linked to shared task: %q", ar2.ProviderTaskID)
	}
	if len(repo.rows) != 2 {
		t.Fatalf("want 2 access-request rows, got %d", len(repo.rows))
	}
}

func TestRejectByProviderTask_FlipsOpenRowsOnly(t *testing.T) {
	repo := &fakeRepo{rows: []*models.AccessRequest{
		{ID: "a", ProviderTaskID: "task-1", Status: models.AccessRequestStatusRequested},
		{ID: "b", ProviderTaskID: "task-1", Status: models.AccessRequestStatusInProgress},
		{ID: "c", ProviderTaskID: "task-1", Status: models.AccessRequestStatusGranted},
		{ID: "d", ProviderTaskID: "other", Status: models.AccessRequestStatusRequested},
	}}
	svc := newSvc(repo, connections.NewOrgEndpointCatalog(&fakeRC{}), &fakeIssueSvc{}, &fakeTaskRepo{})

	if err := svc.RejectByProviderTask(context.Background(), "task-1"); err != nil {
		t.Fatalf("RejectByProviderTask: %v", err)
	}
	want := map[string]string{
		"a": models.AccessRequestStatusRejected,  // open → rejected
		"b": models.AccessRequestStatusRejected,  // open → rejected
		"c": models.AccessRequestStatusGranted,   // already granted, untouched
		"d": models.AccessRequestStatusRequested, // different task, untouched
	}
	for _, ar := range repo.rows {
		if ar.Status != want[ar.ID] {
			t.Fatalf("row %s status = %q, want %q", ar.ID, ar.Status, want[ar.ID])
		}
	}
}

func TestListByConsumerProject(t *testing.T) {
	repo := &fakeRepo{rows: []*models.AccessRequest{
		{ID: "a", OrgID: "acme", ConsumerProjectID: "store-front", Status: models.AccessRequestStatusRequested},
		{ID: "b", OrgID: "acme", ConsumerProjectID: "other", Status: models.AccessRequestStatusGranted},
	}}
	svc := newSvc(repo, connections.NewOrgEndpointCatalog(&fakeRC{}), &fakeIssueSvc{}, &fakeTaskRepo{})

	rows, err := svc.ListByConsumerProject(context.Background(), "acme", "store-front")
	if err != nil {
		t.Fatalf("ListByConsumerProject: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != "a" {
		t.Fatalf("want only row a for store-front, got %+v", rows)
	}
}

func TestRequestAccess_NotFound(t *testing.T) {
	cat := connections.NewOrgEndpointCatalog(&fakeRC{endpoints: sampleEndpoints()})
	svc := newSvc(&fakeRepo{}, cat, &fakeIssueSvc{}, &fakeTaskRepo{})
	_, err := svc.RequestAccess(context.Background(), RequestAccessInput{
		OrgHandle: "acme", ConsumerProject: "p", ConsumerComponent: "c", OrgServiceName: "no-such-svc",
	})
	if err != ErrOrgServiceNotFound {
		t.Fatalf("want ErrOrgServiceNotFound, got %v", err)
	}
}
