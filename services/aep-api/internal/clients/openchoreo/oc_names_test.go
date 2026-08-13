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

package openchoreo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
)

// TestNewBuildRunNameFitsLabelBudget guards the console "Build" path, which
// composes its own run name. Before the budget was enforced this sat at EXACTLY
// 63 for a 32-char project and a 16-char component, so any slightly longer name
// would have broken it the same silent way the fan-out path broke: accepted by
// OpenChoreo, then no build pod, then pending forever.
func TestNewBuildRunNameFitsLabelBudget(t *testing.T) {
	cases := []struct{ name, project, component string }{
		{"the pair that sat on exactly 63", "invoicing-freelancers-creates621", "invoicing-webapp"},
		{"short names", "shop", "api"},
		{"absurd project", strings.Repeat("p", 500), "api"},
		{"absurd both", strings.Repeat("p", 500), strings.Repeat("c", 500)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NewBuildRunName(tc.project, tc.component)
			if len(got) > k8sname.MaxLabelValueLen {
				t.Errorf("NewBuildRunName = %q (%d chars), over the %d-char label budget",
					got, len(got), k8sname.MaxLabelValueLen)
			}
			// The staged build Secret is named `<runName>-git-secret`. That is a
			// resource NAME, not a label value, so it only has to clear 253.
			if secret := got + "-git-secret"; len(secret) > 253 {
				t.Errorf("derived secret name %q is %d chars, over the 253-char name limit", secret, len(secret))
			}
		})
	}
}

// TestNewCodingAgentRunNameFitsOCJobLabelBudget is the coding-agent counterpart
// of TestNewBuildRunNameFitsLabelBudget. OpenChoreo names the dataplane Job
// `{scopedComponent}-{development}-{hash8}` and stamps that into pod-template
// labels; overflowing 63 is ResourceApplyFailed with no runner pod.
func TestNewCodingAgentRunNameFitsOCJobLabelBudget(t *testing.T) {
	cycle := "08d05715-b1aa-444e-88e9-0875ff58d5c0"
	cases := []struct {
		name, project string
		mustFit       bool
	}{
		{"the project that overflowed in local e2e", "hello-world-api-4", true},
		{"short project", "shop", true},
		{"18-char project head", "invoicing-freelance", true},
		// Project alone leaves no room for ca-+digest under the Job budget —
		// CreateComponent refuses these; the generator still returns a name.
		{"absurd project", strings.Repeat("p", 500), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			friendly := NewCodingAgentRunName(tc.project, cycle)
			scoped := ScopedComponentName(tc.project, friendly)
			if !tc.mustFit {
				return
			}
			if !strings.HasPrefix(friendly, "ca-") {
				t.Errorf("NewCodingAgentRunName = %q, want ca- prefix", friendly)
			}
			if len(scoped) > CodingAgentComponentNameBudget {
				t.Errorf("scoped name %q is %d chars, over CodingAgentComponentNameBudget=%d",
					scoped, len(scoped), CodingAgentComponentNameBudget)
			}
			jobLabel := scoped + "-" + DevEnvironmentName + "-" + strings.Repeat("a", ocJobNameHashLen)
			if len(jobLabel) > k8sname.MaxLabelValueLen {
				t.Errorf("OC Job label %q is %d chars, over MaxLabelValueLen=%d",
					jobLabel, len(jobLabel), k8sname.MaxLabelValueLen)
			}
		})
	}
}

// TestNewCodingAgentRunNameIsStableAcrossRetries: a Temporal retry after a
// crash must recreate the same Component name so CreateComponent's 409 path
// re-reads instead of minting a second billed Component.
func TestNewCodingAgentRunNameIsStableAcrossRetries(t *testing.T) {
	a := NewCodingAgentRunName("hello-world-api-4", "08d05715-b1aa-444e-88e9-0875ff58d5c0")
	b := NewCodingAgentRunName("hello-world-api-4", "08d05715-b1aa-444e-88e9-0875ff58d5c0")
	if a != b {
		t.Fatalf("unstable run name: %q then %q", a, b)
	}
	if a == NewCodingAgentRunName("hello-world-api-4", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee") {
		t.Fatal("distinct cycle ids must not share a run name")
	}
}

// TestCreateComponentRejectsOverlongCodingAgentName pins the choke-point guard:
// an over-budget coding-agent Component must never be POSTed to OpenChoreo.
func TestCreateComponentRejectsOverlongCodingAgentName(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL}).(*componentClient)
	overlong := strings.Repeat("x", CodingAgentComponentNameBudget) // friendly alone fills the scoped budget once project is prefixed
	_, err := c.CreateComponent(context.Background(), "default", "hello-world-api-4", &CreateComponentRequest{
		Name: overlong,
		Type: CodingAgentComponentTypeRef,
	})
	if err == nil {
		t.Fatal("CreateComponent accepted an over-budget coding-agent name")
	}
	if !strings.Contains(err.Error(), "coding-agent name") {
		t.Errorf("error = %q, want it to name the coding-agent budget", err)
	}
	if n := called.Load(); n != 0 {
		t.Errorf("server was called %d times, want 0", n)
	}
}

// TestCreateWorkflowRunRejectsOverlongName pins the guard at the choke point
// every WorkflowRun create passes through.
//
// The point is that the request must never be SENT: OpenChoreo answers 201 for a
// name it cannot later render, so a client that optimistically POSTs and trusts
// the status code produces a run that is stuck forever with nothing on its
// status to explain it. The fake server therefore records whether it was called
// at all, rather than asserting on a response.
func TestCreateWorkflowRunRejectsOverlongName(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL}).(*componentClient)

	// 64 chars: the exact overflow that wedged a real build, one over the limit.
	overlong := "invoicing-freelancers-creates621-invoicing-webapp-4b4fede2508f-1"
	if len(overlong) != 64 {
		t.Fatalf("fixture is %d chars, want the 64-char production overflow", len(overlong))
	}

	_, err := c.createWorkflowRun(context.Background(), "default", ocgen.CreateWorkflowRunJSONRequestBody{
		Metadata: ocgen.ObjectMeta{Name: overlong},
	}, "trigger build")

	if err == nil {
		t.Fatal("createWorkflowRun accepted a 64-char name, want it refused before the request is sent")
	}
	if !strings.Contains(err.Error(), "label-value limit") {
		t.Errorf("error = %q, want it to name the label-value limit so the cause is obvious", err)
	}
	if n := called.Load(); n != 0 {
		t.Errorf("server was called %d times, want 0 — an unrenderable run must never be created", n)
	}
}

// TestCreateWorkflowRunAcceptsNameAtLimit pins the boundary from the other side,
// so the guard cannot drift into rejecting names that are actually fine.
func TestCreateWorkflowRunAcceptsNameAtLimit(t *testing.T) {
	var called atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"ok"}}`))
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL}).(*componentClient)

	atLimit := strings.Repeat("a", k8sname.MaxLabelValueLen)
	if _, err := c.createWorkflowRun(context.Background(), "default", ocgen.CreateWorkflowRunJSONRequestBody{
		Metadata: ocgen.ObjectMeta{Name: atLimit},
	}, "trigger build"); err != nil {
		t.Fatalf("createWorkflowRun rejected a %d-char name (exactly the limit): %v", len(atLimit), err)
	}
	if n := called.Load(); n != 1 {
		t.Errorf("server was called %d times, want 1", n)
	}
}
