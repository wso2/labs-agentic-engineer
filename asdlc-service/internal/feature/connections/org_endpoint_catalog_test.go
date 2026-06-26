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

func TestConsumerWiring_WireOrgServiceConsumer(t *testing.T) {
	rc := &fakeRC{workloadEndpoints: sampleEndpoints()}
	w := NewConsumerWiring(rc, nil, NewOrgEndpointCatalog(rc), nil)

	// org-chart in project `directory` consumes the org-published employee-api
	// plus a project-only one (skipped) and an unknown one (skipped).
	err := w.WireOrgServiceConsumer(context.Background(), "ns", "directory", "org-chart",
		[]string{"employee-api", "payroll-internal", "nope"})
	if err != nil {
		t.Fatalf("WireOrgServiceConsumer: %v", err)
	}
	if rc.patchedEPWorkload != "directory-org-chart-workload" {
		t.Fatalf("patched wrong workload: %q", rc.patchedEPWorkload)
	}
	if len(rc.patchedEndpointDeps) != 1 {
		t.Fatalf("want 1 endpoint dep (only the namespace-visible target), got %d: %+v",
			len(rc.patchedEndpointDeps), rc.patchedEndpointDeps)
	}
	dep := rc.patchedEndpointDeps[0]
	if dep.Project != "hr" || dep.Component != "employee-api" || dep.Name != "http" ||
		dep.Visibility != "namespace" || dep.AddressEnv != "EMPLOYEE_API_URL" {
		t.Fatalf("wrong WorkloadConnection: %+v", dep)
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
