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
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeAccessStore is an in-memory accessGrantStore.
type fakeAccessStore struct {
	rows       []*models.AccessRequest
	findTarget *models.AccessRequest // returned by FindOpenForTarget
	findErr    error
}

func (f *fakeAccessStore) FindOpenForTarget(_ context.Context, _, _, _ string) (*models.AccessRequest, error) {
	return f.findTarget, f.findErr
}
func (f *fakeAccessStore) ListByProviderTask(_ context.Context, providerTaskID string) ([]models.AccessRequest, error) {
	var out []models.AccessRequest
	for _, r := range f.rows {
		if r.ProviderTaskID == providerTaskID {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (f *fakeAccessStore) UpdateStatus(_ context.Context, id, status string) error {
	for _, r := range f.rows {
		if r.ID == id {
			r.Status = status
			return nil
		}
	}
	return nil
}

// fakeDesignStore is an in-memory accessDesignStore.
type fakeDesignStore struct {
	design  *artifacts.DesignFile
	written []models.DesignComponent
}

func (f *fakeDesignStore) ReadDesign(_ context.Context, _, _ string) (*artifacts.DesignFile, error) {
	return f.design, nil
}
func (f *fakeDesignStore) WriteComponentDesign(_ context.Context, _, _ string, comp models.DesignComponent) error {
	f.written = append(f.written, comp)
	return nil
}

// fakeIssueCloser records the closed issue number.
type fakeIssueCloser struct{ closed []int }

func (f *fakeIssueCloser) CloseIssue(_ context.Context, _, _ string, number int, _ string) error {
	f.closed = append(f.closed, number)
	return nil
}

func TestGrantAccessRequests_NoOpenRequest(t *testing.T) {
	store := &fakeAccessStore{findTarget: nil}
	design := &fakeDesignStore{}
	h := &DispatchCascadeHook{}
	h.SetAccessGrant(store, design, &fakeIssueCloser{})

	h.grantAccessRequests(context.Background(), "acme", "hr-directory", "employee-api")

	if len(design.written) != 0 {
		t.Fatalf("normal deploy should not write design, got %d writes", len(design.written))
	}
}

func TestGrantAccessRequests_FlipsAllRowsSetsOrgPublishedClosesIssue(t *testing.T) {
	store := &fakeAccessStore{
		findTarget: &models.AccessRequest{ID: "a", ProviderTaskID: "task-1", ProviderIssueNumber: 7, Status: models.AccessRequestStatusRequested},
		rows: []*models.AccessRequest{
			{ID: "a", ProviderTaskID: "task-1", Status: models.AccessRequestStatusRequested},
			{ID: "b", ProviderTaskID: "task-1", Status: models.AccessRequestStatusInProgress},
		},
	}
	design := &fakeDesignStore{design: &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{Name: "employee-api", ComponentType: "service"}, // ExposesAPI nil
		},
	}}
	issues := &fakeIssueCloser{}
	h := &DispatchCascadeHook{}
	h.SetAccessGrant(store, design, issues)

	h.grantAccessRequests(context.Background(), "acme", "hr-directory", "employee-api")

	for _, r := range store.rows {
		if r.Status != models.AccessRequestStatusGranted {
			t.Fatalf("row %s status = %q, want granted", r.ID, r.Status)
		}
	}
	if len(design.written) != 1 {
		t.Fatalf("want one orgPublished durability write, got %d", len(design.written))
	}
	if design.written[0].ExposesAPI == nil || !design.written[0].ExposesAPI.OrgPublished {
		t.Fatalf("written component missing orgPublished:true: %+v", design.written[0].ExposesAPI)
	}
	if len(issues.closed) != 1 || issues.closed[0] != 7 {
		t.Fatalf("want issue 7 closed, got %v", issues.closed)
	}
}

func TestMarkOrgPublished_IdempotentWhenAlreadyTrue(t *testing.T) {
	design := &fakeDesignStore{design: &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{Name: "employee-api", ExposesAPI: &models.ExposesAPI{OrgPublished: true}},
		},
	}}
	h := &DispatchCascadeHook{}
	h.SetAccessGrant(&fakeAccessStore{}, design, &fakeIssueCloser{})

	h.markOrgPublished(context.Background(), "acme", "hr-directory", "employee-api")

	if len(design.written) != 0 {
		t.Fatalf("already-published component should not be rewritten, got %d writes", len(design.written))
	}
}

func TestMarkOrgPublished_MatchesOCComponentName(t *testing.T) {
	design := &fakeDesignStore{design: &artifacts.DesignFile{
		Components: []models.DesignComponent{
			{Name: "employee-api"},
		},
	}}
	h := &DispatchCascadeHook{}
	h.SetAccessGrant(&fakeAccessStore{}, design, &fakeIssueCloser{})

	// Provider identified by OC component name `<project>-<logical>`.
	h.markOrgPublished(context.Background(), "acme", "hr-directory", "hr-directory-employee-api")

	if len(design.written) != 1 || !design.written[0].ExposesAPI.OrgPublished {
		t.Fatalf("OC-name match should write orgPublished, got %+v", design.written)
	}
}
