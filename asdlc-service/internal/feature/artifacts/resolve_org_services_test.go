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

package artifacts

import (
	"context"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeOrgServiceResolver is a static OrgServiceResolver: `visible` lists the
// org-service names published namespace-visible, `exists` lists every name that
// has any endpoint in the catalog (regardless of visibility). A name in
// `visible` is implicitly in `exists`.
type fakeOrgServiceResolver struct {
	visible map[string]bool
	exists  map[string]bool
}

func (f fakeOrgServiceResolver) IsNamespaceVisible(_ context.Context, _, name string) (bool, error) {
	return f.visible[name], nil
}

func (f fakeOrgServiceResolver) ExistsAnyVisibility(_ context.Context, _, name string) (bool, error) {
	return f.exists[name] || f.visible[name], nil
}

// TestResolveOrgServices_BlockedAccessRequired asserts the 4-state model
// (task A2b): a project-only org-service (exists but NOT namespace-visible)
// must produce status="blocked" / reason="access-required".
func TestResolveOrgServices_BlockedAccessRequired(t *testing.T) {
	store := &ArtifactStore{}
	store.SetOrgServiceResolver(fakeOrgServiceResolver{
		visible: map[string]bool{},
		exists:  map[string]bool{"payroll-internal": true},
	})

	d := &DesignFile{Components: []models.DesignComponent{{
		Name: "consumer",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindOrgService, Name: "payroll-internal"},
		},
	}}}

	store.resolveOrgServices(context.Background(), "org", d)

	dep := d.Components[0].Dependencies[0]
	if dep.Status != "blocked" {
		t.Errorf("project-only org-service: status = %q, want %q", dep.Status, "blocked")
	}
	if dep.Reason != "access-required" {
		t.Errorf("project-only org-service: reason = %q, want %q", dep.Reason, "access-required")
	}
}

// TestResolveOrgServices_AbsentNotFound asserts the 4-state model (task A2b):
// an org-service absent from the catalog must produce status="unresolved" /
// reason="not-found".
func TestResolveOrgServices_AbsentNotFound(t *testing.T) {
	store := &ArtifactStore{}
	store.SetOrgServiceResolver(fakeOrgServiceResolver{
		visible: map[string]bool{},
		exists:  map[string]bool{},
	})

	d := &DesignFile{Components: []models.DesignComponent{{
		Name: "consumer",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindOrgService, Name: "ghost-svc"},
		},
	}}}

	store.resolveOrgServices(context.Background(), "org", d)

	dep := d.Components[0].Dependencies[0]
	if dep.Status != "unresolved" {
		t.Errorf("absent org-service: status = %q, want %q", dep.Status, "unresolved")
	}
	if dep.Reason != "not-found" {
		t.Errorf("absent org-service: reason = %q, want %q", dep.Reason, "not-found")
	}
}

// TestResolveOrgServices_ReasonSplit asserts the P3.5 reason refinement on top
// of the resolved/unresolved/blocked status (4-state model, task A2b):
// namespace-visible → resolved/"";
// exists-but-project-only → blocked/"access-required"; absent → unresolved/"not-found".
func TestResolveOrgServices_ReasonSplit(t *testing.T) {
	store := &ArtifactStore{}
	store.SetOrgServiceResolver(fakeOrgServiceResolver{
		visible: map[string]bool{"employee-api": true},
		exists:  map[string]bool{"payroll-internal": true},
	})

	d := &DesignFile{Components: []models.DesignComponent{{
		Name: "web",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindOrgService, Name: "employee-api"},     // namespace-visible
			{Kind: models.DependencyKindOrgService, Name: "payroll-internal"}, // exists, project-only
			{Kind: models.DependencyKindOrgService, Name: "ghost"},            // not in catalog
		},
	}}}

	store.resolveOrgServices(context.Background(), "org", d)

	deps := d.Components[0].Dependencies
	cases := []struct {
		name       string
		wantStatus string
		wantReason string
	}{
		{"employee-api", "resolved", ""},
		{"payroll-internal", "blocked", "access-required"},
		{"ghost", "unresolved", "not-found"},
	}
	for i, c := range cases {
		if deps[i].Name != c.name {
			t.Fatalf("dep[%d]: name = %q, want %q", i, deps[i].Name, c.name)
		}
		if deps[i].Status != c.wantStatus {
			t.Errorf("%s: status = %q, want %q", c.name, deps[i].Status, c.wantStatus)
		}
		if deps[i].Reason != c.wantReason {
			t.Errorf("%s: reason = %q, want %q", c.name, deps[i].Reason, c.wantReason)
		}
	}
}
