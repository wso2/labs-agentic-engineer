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

package connections

import (
	"context"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
)

func sampleEndpoints() []openchoreo.WorkloadEndpointInfo {
	return []openchoreo.WorkloadEndpointInfo{
		// org-published: external + namespace → an org-service target.
		{Project: "hr", Component: "employee-api", Workload: "hr-employee-api-workload",
			Name: "http", Type: "HTTP", Port: 8080, Visibility: []string{"external", "namespace"}},
		// project-only: no namespace visibility → NOT an org-service target.
		{Project: "hr", Component: "payroll-internal", Workload: "hr-payroll-internal-workload",
			Name: "http", Type: "HTTP", Port: 8081, Visibility: []string{"external"}},
		// same-project sibling with ONLY implicit project visibility (no
		// namespace/external) → resolvable by ResolveProjectEndpoint, but NOT
		// namespace-visible.
		{Project: "org-roster", Component: "org-roster-todo-api", Workload: "org-roster-todo-api-workload",
			Name: "http", Type: "HTTP", Port: 8082, Visibility: nil},
	}
}

func TestOrgEndpointCatalog_ResolveProjectEndpoint(t *testing.T) {
	cat := NewOrgEndpointCatalog(&fakeRC{workloadEndpoints: sampleEndpoints()})

	// A project-only sibling (NOT namespace-visible) resolves by project+component.
	got, ok, err := cat.ResolveProjectEndpoint(context.Background(), "ns", "org-roster", "org-roster-todo-api")
	if err != nil || !ok {
		t.Fatalf("org-roster-todo-api: want resolved, got ok=%v err=%v", ok, err)
	}
	if got.Name != "http" || got.Component != "org-roster-todo-api" {
		t.Fatalf("resolved wrong target: %+v", got)
	}
	// Sanity: this endpoint is project-only — it must NOT be namespace-visible.
	if got.NamespaceVisible() {
		t.Fatalf("expected project-only endpoint, got namespace-visible: %+v", got)
	}

	// Unknown component must not resolve.
	if _, ok, _ := cat.ResolveProjectEndpoint(context.Background(), "ns", "org-roster", "org-roster-nope"); ok {
		t.Fatalf("unknown component must not resolve")
	}
}

func TestOrgEndpointCatalog_ResolveNamespaceVisible(t *testing.T) {
	cat := NewOrgEndpointCatalog(&fakeRC{workloadEndpoints: sampleEndpoints()})

	got, ok, err := cat.ResolveNamespaceVisible(context.Background(), "ns", "employee-api")
	if err != nil || !ok {
		t.Fatalf("employee-api: want resolved, got ok=%v err=%v", ok, err)
	}
	if got.Project != "hr" || got.Name != "http" {
		t.Fatalf("resolved wrong target: %+v", got)
	}

	// project-only target must not resolve as an org-service.
	if _, ok, _ := cat.ResolveNamespaceVisible(context.Background(), "ns", "payroll-internal"); ok {
		t.Fatalf("payroll-internal is project-only — must not resolve")
	}
	// unknown name.
	if _, ok, _ := cat.ResolveNamespaceVisible(context.Background(), "ns", "nope"); ok {
		t.Fatalf("unknown org-service must not resolve")
	}
}

func TestOrgEndpointCatalog_ExistsAnyVisibility(t *testing.T) {
	cat := NewOrgEndpointCatalog(&fakeRC{workloadEndpoints: sampleEndpoints()})

	// A project-only component (NOT namespace-visible) still exists in the
	// catalog → ExistsAnyVisibility true (the P3.5 `blocked`/`access-required` case).
	got, err := cat.ExistsAnyVisibility(context.Background(), "ns", "org-roster-todo-api")
	if err != nil {
		t.Fatalf("org-roster-todo-api: unexpected error: %v", err)
	}
	if !got {
		t.Fatalf("org-roster-todo-api: want exists=true (project-only but present)")
	}

	// An unknown component does not exist at any visibility → the `not-found` case.
	got, err = cat.ExistsAnyVisibility(context.Background(), "ns", "nope")
	if err != nil {
		t.Fatalf("nope: unexpected error: %v", err)
	}
	if got {
		t.Fatalf("nope: want exists=false")
	}
}

func TestOrgEndpointCatalog_FindByComponent(t *testing.T) {
	cat := NewOrgEndpointCatalog(&fakeRC{workloadEndpoints: sampleEndpoints()})

	// A project-only component (NOT namespace-visible) is still found — P3.5
	// resolves the provider row regardless of visibility to derive its project.
	got, ok, err := cat.FindByComponent(context.Background(), "ns", "payroll-internal")
	if err != nil || !ok {
		t.Fatalf("payroll-internal: want found, got ok=%v err=%v", ok, err)
	}
	if got.Project != "hr" || got.Name != "http" || got.Type != "HTTP" {
		t.Fatalf("resolved wrong row: %+v", got)
	}

	// An org-published component is also found.
	if _, ok, _ := cat.FindByComponent(context.Background(), "ns", "employee-api"); !ok {
		t.Fatalf("employee-api: want found")
	}

	// Unknown component must not resolve (the `not-found` case).
	if _, ok, _ := cat.FindByComponent(context.Background(), "ns", "nope"); ok {
		t.Fatalf("unknown component must not resolve")
	}
}

func TestOrgServiceURLEnv(t *testing.T) {
	cases := map[string]string{
		"employee-api": "EMPLOYEE_API_URL",
		"todo":         "TODO_URL",
		"order-svc-2":  "ORDER_SVC_2_URL",
	}
	for in, want := range cases {
		if got := orgServiceURLEnv(in); got != want {
			t.Errorf("orgServiceURLEnv(%q) = %q, want %q", in, got, want)
		}
	}
}
