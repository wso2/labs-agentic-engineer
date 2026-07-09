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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/models"
)

// codingExecutorFor builds a coding executor over the ClusterWorkflow path
// (proxy unset) with the coding-dispatch fakes + a scripted ensurer.
func codingExecutorFor(t *testing.T, ensurer ComponentEnsurer, oc openchoreo.ComponentClient, row *models.Execution) *CodingExecutor {
	t.Helper()
	e := NewCodingExecutor(oc, fakeRepos{repo: &models.GitRepository{RepoURL: "https://github.com/acme/widgets"}},
		fakeIdentities{}, nil, fakeTokens{}, newFakeExecRepo(row), "http://git", "http://platform")
	return e.WithComponentEnsurer(ensurer)
}

func codingRow(id string) *models.Execution {
	return &models.Execution{
		ID: id, OrgID: "acme", ProjectID: "widgets", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued), Component: "order-service",
	}
}

func codingDispatch(row *models.Execution) execution.DispatchRequest {
	return execution.DispatchRequest{
		Execution: row,
		Task: execution.TaskFacts{
			OrgID: "acme", ProjectID: "widgets", Component: "order-service",
			IssueNumber: 7, IssueURL: "https://github.com/acme/widgets/issues/7",
		},
	}
}

func TestRunCoding_PreflightEnsuresComponent_BeforeDispatch(t *testing.T) {
	ensurer := &fakeEnsurer{}
	triggered := false
	oc := &ocmocks.ComponentClientMock{
		TriggerCodingAgentFunc: func(_ context.Context, _ openchoreo.CodingAgentParams) (*models.WorkflowRun, error) {
			triggered = true
			return &models.WorkflowRun{Name: "ca-1"}, nil
		},
	}
	row := codingRow("c1")
	e := codingExecutorFor(t, ensurer, oc, row)

	if err := e.Run(context.Background(), codingDispatch(row)); err != nil {
		t.Fatalf("Run(coding): %v", err)
	}
	if got := ensurer.calls(); len(got) != 1 || got[0] != [3]string{"acme", "widgets", "order-service"} {
		t.Fatalf("EnsureComponent must be called once with (org,project,component), got %v", got)
	}
	if !triggered {
		t.Fatal("coding run must be triggered after a successful pre-flight")
	}
}

func TestRunCoding_PreflightFailure_BlocksDispatch(t *testing.T) {
	ensurer := &fakeEnsurer{err: errors.New("design missing")}
	triggered := false
	oc := &ocmocks.ComponentClientMock{
		TriggerCodingAgentFunc: func(_ context.Context, _ openchoreo.CodingAgentParams) (*models.WorkflowRun, error) {
			triggered = true
			return &models.WorkflowRun{Name: "ca-1"}, nil
		},
	}
	row := codingRow("c1")
	e := codingExecutorFor(t, ensurer, oc, row)

	err := e.Run(context.Background(), codingDispatch(row))
	if err == nil || !strings.Contains(err.Error(), "ensure component pre-flight") {
		t.Fatalf("pre-flight failure must block dispatch with a clear error, got %v", err)
	}
	if triggered {
		t.Error("coding run must NOT be triggered when the pre-flight fails")
	}
}

func TestRunCoding_ReDispatch_PreflightRunsEachTime(t *testing.T) {
	// The pre-flight is idempotent (EnsureComponent is 409-safe at the OC client),
	// so a re-dispatch simply calls it again and succeeds — no error, no duplicate.
	ensurer := &fakeEnsurer{}
	oc := &ocmocks.ComponentClientMock{
		TriggerCodingAgentFunc: func(_ context.Context, _ openchoreo.CodingAgentParams) (*models.WorkflowRun, error) {
			return &models.WorkflowRun{Name: "ca-1"}, nil
		},
	}
	e := codingExecutorFor(t, ensurer, oc, codingRow("c1"))
	for i := 0; i < 2; i++ {
		if err := e.Run(context.Background(), codingDispatch(codingRow("c1"))); err != nil {
			t.Fatalf("re-dispatch %d: %v", i, err)
		}
	}
	if got := ensurer.calls(); len(got) != 2 {
		t.Fatalf("pre-flight must run on each dispatch (idempotent), got %d calls", len(got))
	}
}

func TestRunCoding_EmitsRuntimeConfig_AtEnsurePreflight(t *testing.T) {
	// The ensure pre-flight fires env-config.js emission best-effort. The web-app
	// gate lives inside the emitter (which self-no-ops for non-web-apps), so the
	// executor calls it unconditionally with the dispatched component's name.
	ensurer := &fakeEnsurer{}
	rc := &fakeRuntimeConfig{}
	oc := &ocmocks.ComponentClientMock{
		TriggerCodingAgentFunc: func(_ context.Context, _ openchoreo.CodingAgentParams) (*models.WorkflowRun, error) {
			return &models.WorkflowRun{Name: "ca-1"}, nil
		},
	}
	row := codingRow("c1")
	e := codingExecutorFor(t, ensurer, oc, row).WithComponentRuntimeConfig(rc)

	if err := e.Run(context.Background(), codingDispatch(row)); err != nil {
		t.Fatalf("Run(coding): %v", err)
	}
	if got := rc.calls(); len(got) != 1 || got[0] != [3]string{"acme", "widgets", "order-service"} {
		t.Fatalf("EmitForComponent must be called once with (org,project,component), got %v", got)
	}
}

func TestRunCoding_RuntimeConfigEmitError_DoesNotFailDispatch(t *testing.T) {
	// Emission is best-effort: an emit failure warns but the coding run still
	// dispatches (the deploy cascade re-fires the emit later).
	ensurer := &fakeEnsurer{}
	rc := &fakeRuntimeConfig{err: errors.New("OC transient")}
	triggered := false
	oc := &ocmocks.ComponentClientMock{
		TriggerCodingAgentFunc: func(_ context.Context, _ openchoreo.CodingAgentParams) (*models.WorkflowRun, error) {
			triggered = true
			return &models.WorkflowRun{Name: "ca-1"}, nil
		},
	}
	row := codingRow("c1")
	e := codingExecutorFor(t, ensurer, oc, row).WithComponentRuntimeConfig(rc)

	if err := e.Run(context.Background(), codingDispatch(row)); err != nil {
		t.Fatalf("an emit failure must not fail the dispatch, got %v", err)
	}
	if !triggered {
		t.Fatal("coding run must still be triggered after a best-effort emit failure")
	}
}

func TestRunBuild_ComponentMissing_ActionableError(t *testing.T) {
	// The build path does NOT upsert; a missing Component surfaces a clear,
	// actionable error wrapping openchoreo.ErrNotFound.
	oc := &ocmocks.ComponentClientMock{
		TriggerBuildAtCommitFunc: func(_ context.Context, _, _, _, _, _, _ string) (*models.WorkflowRun, error) {
			return nil, openchoreo.ErrNotFound
		},
	}
	row := buildRow("b1")
	e := NewCodingExecutor(oc, fakeRepos{repo: &models.GitRepository{RepoURL: "https://github.com/acme/widgets"}},
		fakeIdentities{}, nil, fakeTokens{}, newFakeExecRepo(row), "http://git", "http://platform")

	err := e.Run(context.Background(), buildDispatch(row))
	if err == nil {
		t.Fatal("a missing Component must fail the build")
	}
	if !errors.Is(err, openchoreo.ErrNotFound) {
		t.Errorf("build error must wrap openchoreo.ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "coding execution must run first") {
		t.Errorf("build error must be actionable (re-run coding), got %v", err)
	}
}
