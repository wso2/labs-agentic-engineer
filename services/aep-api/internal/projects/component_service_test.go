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

package projects

// UNIT tier: the REAL componentService with every
// out-of-process port mocked/faked — no HTTP, no DB. Proves the service's logic
// branches: passthrough + error propagation for the OC-delegating reads, the
// TriggerBuild build-secret pre-stage chain (incl. its best-effort GetRepo
// branches and the same-runName contract), GetBuildLogs' not-configured vs
// error paths, and GetComponentOpenAPI's design-tree read over the REAL
// ArtifactStore. The HTTP contract (status codes, validation, error→status
// mapping, gate) lives in component_component_test.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// --- List / Get / Deployments / Builds (OC passthrough + error propagation) ---

func TestComponentService_ListComponents_PassthroughAndError(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{
		ListComponentsFunc: func(_ context.Context, org, proj string, limit int, cursor string) (*gen.ComponentList, error) {
			return &gen.ComponentList{Items: []gen.Component{{Name: "svc"}}}, nil
		},
	}
	svc := NewComponentService(oc, nil, nil, nil, nil)
	list, err := svc.ListComponents(context.Background(), "acme", "web", 100, "")
	if err != nil || list == nil || len(list.Items) != 1 {
		t.Fatalf("list happy: got list=%+v err=%v", list, err)
	}
	// The service forwards limit/cursor verbatim (the HTTP op pins 100/"").
	if c := oc.ListComponentsCalls(); len(c) != 1 || c[0].OrgName != "acme" || c[0].ProjectName != "web" || c[0].Limit != 100 {
		t.Fatalf("OC ListComponents args: %+v", c)
	}

	ocErr := &ocmocks.ComponentClientMock{
		ListComponentsFunc: func(context.Context, string, string, int, string) (*gen.ComponentList, error) {
			return nil, openchoreo.ErrNotFound
		},
	}
	if _, err := NewComponentService(ocErr, nil, nil, nil, nil).ListComponents(context.Background(), "acme", "web", 100, ""); !errors.Is(err, openchoreo.ErrNotFound) {
		t.Fatalf("list error must propagate the raw OC sentinel, got %v", err)
	}
}

func TestComponentService_GetComponent_PassthroughAndError(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{
		GetComponentFunc: func(_ context.Context, org, proj, comp string) (*gen.Component, error) {
			return &gen.Component{Name: comp, ProjectName: proj}, nil
		},
	}
	svc := NewComponentService(oc, nil, nil, nil, nil)
	got, err := svc.GetComponent(context.Background(), "acme", "web", "hello-api")
	if err != nil || got == nil || got.Name != "hello-api" {
		t.Fatalf("get happy: got=%+v err=%v", got, err)
	}

	ocErr := &ocmocks.ComponentClientMock{GetComponentFunc: func(context.Context, string, string, string) (*gen.Component, error) {
		return nil, openchoreo.ErrNotFound
	}}
	if _, err := NewComponentService(ocErr, nil, nil, nil, nil).GetComponent(context.Background(), "acme", "web", "x"); !errors.Is(err, openchoreo.ErrNotFound) {
		t.Fatalf("get error must propagate, got %v", err)
	}
}

func TestComponentService_ListDeployments_PassthroughAndError(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
		return &gen.DeploymentList{Items: []gen.Deployment{{Name: "d1"}}}, nil
	}}
	list, err := NewComponentService(oc, nil, nil, nil, nil).ListDeployments(context.Background(), "acme", "web", "svc")
	if err != nil || list == nil || len(list.Items) != 1 {
		t.Fatalf("deployments happy: %+v %v", list, err)
	}

	ocErr := &ocmocks.ComponentClientMock{ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
		return nil, errors.New("oc down")
	}}
	if _, err := NewComponentService(ocErr, nil, nil, nil, nil).ListDeployments(context.Background(), "a", "p", "c"); err == nil {
		t.Fatal("deployments error must propagate")
	}
}

func TestComponentService_ListBuilds_DelegatesToWorkflowRuns(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{ListWorkflowRunsFunc: func(_ context.Context, org, proj, comp string, limit int, cursor string) (*gen.WorkflowRunList, error) {
		return &gen.WorkflowRunList{Items: []gen.WorkflowRun{{Name: "run-1"}}}, nil
	}}
	svc := NewComponentService(oc, nil, nil, nil, nil)
	list, err := svc.ListBuilds(context.Background(), "acme", "web", "svc", 20, "")
	if err != nil || list == nil || len(list.Items) != 1 {
		t.Fatalf("builds happy: %+v %v", list, err)
	}
	// ListBuilds is a thin alias over ListWorkflowRuns; the HTTP op pins limit 20.
	if c := oc.ListWorkflowRunsCalls(); len(c) != 1 || c[0].Limit != 20 {
		t.Fatalf("OC ListWorkflowRuns args: %+v", c)
	}

	ocErr := &ocmocks.ComponentClientMock{ListWorkflowRunsFunc: func(context.Context, string, string, string, int, string) (*gen.WorkflowRunList, error) {
		return nil, errors.New("boom")
	}}
	if _, err := NewComponentService(ocErr, nil, nil, nil, nil).ListBuilds(context.Background(), "a", "p", "c", 20, ""); err == nil {
		t.Fatal("builds error must propagate")
	}
}

// --- TriggerBuild: the build-secret pre-stage chain ---------------------------

func TestComponentService_TriggerBuild_NoStagerWhenPortsNil(t *testing.T) {
	t.Parallel()
	var sawSecretRef, sawRunName string
	oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(_ context.Context, org, proj, comp, secretRef, runName string) (*gen.WorkflowRun, error) {
		sawSecretRef, sawRunName = secretRef, runName
		return &gen.WorkflowRun{Name: runName}, nil
	}}
	// repoSvc + buildCredSvc nil ⇒ no staging: the build fires with an empty
	// secretRef and a freshly-generated runName.
	svc := NewComponentService(oc, nil, nil, nil, nil)
	run, err := svc.TriggerBuild(context.Background(), "acme", "web", "svc")
	if err != nil || run == nil {
		t.Fatalf("trigger happy: run=%+v err=%v", run, err)
	}
	if sawSecretRef != "" {
		t.Fatalf("no-stager path must pass an empty secretRef, got %q", sawSecretRef)
	}
	if sawRunName == "" {
		t.Fatal("service must generate a runName even without staging")
	}
}

func TestComponentService_TriggerBuild_StagesSecretWithSameRunName(t *testing.T) {
	t.Parallel()
	var stagerSlug, stagerRun, ocSecretRef, ocRun string
	oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(_ context.Context, org, proj, comp, secretRef, runName string) (*gen.WorkflowRun, error) {
		ocSecretRef, ocRun = secretRef, runName
		return &gen.WorkflowRun{Name: runName}, nil
	}}
	repo := &stubRepoSvc{GetRepoFunc: func(_ context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
		if orgID != "acme" || projectID != "web" {
			t.Errorf("GetRepo scope: (%q,%q)", orgID, projectID)
		}
		return &sourcecontrol.GitRepository{RepoSlug: "owner-repo"}, nil
	}}
	stager := &stubBuildStager{StageBuildSecretFunc: func(_ context.Context, ocOrgID, repoSlug, runName string) (string, error) {
		stagerSlug, stagerRun = repoSlug, runName
		return "git-secret-ref", nil
	}}
	svc := NewComponentService(oc, nil, nil, repo, stager)
	if _, err := svc.TriggerBuild(context.Background(), "acme", "web", "svc"); err != nil {
		t.Fatalf("trigger with staging: %v", err)
	}
	if stagerSlug != "owner-repo" {
		t.Fatalf("stager repoSlug: got %q", stagerSlug)
	}
	// The staged secretRef must reach OC verbatim, and the SAME generated
	// runName must be handed to both the stager and the build so the workflow
	// mounts the secret it staged.
	if ocSecretRef != "git-secret-ref" {
		t.Fatalf("OC secretRef: got %q, want the staged ref", ocSecretRef)
	}
	if stagerRun == "" || stagerRun != ocRun {
		t.Fatalf("runName must be identical for stage + build: stager=%q oc=%q", stagerRun, ocRun)
	}
}

func TestComponentService_TriggerBuild_GetRepoFailuresAreBestEffort(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		repo *stubRepoSvc
	}{
		{"GetRepo errors", &stubRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return nil, errors.New("db down")
		}}},
		{"no repo row", &stubRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return nil, nil
		}}},
		{"empty repoSlug", &stubRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{RepoSlug: ""}, nil
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var sawSecretRef string
			built := false
			oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(_ context.Context, _, _, _, secretRef, _ string) (*gen.WorkflowRun, error) {
				sawSecretRef = secretRef
				built = true
				return &gen.WorkflowRun{}, nil
			}}
			stager := &stubBuildStager{StageBuildSecretFunc: func(context.Context, string, string, string) (string, error) {
				t.Error("StageBuildSecret must not run when there is no usable repo")
				return "", nil
			}}
			svc := NewComponentService(oc, nil, nil, tc.repo, stager)
			if _, err := svc.TriggerBuild(context.Background(), "acme", "web", "svc"); err != nil {
				t.Fatalf("a missing/failed repo lookup must NOT fail the build: %v", err)
			}
			if !built || sawSecretRef != "" {
				t.Fatalf("build must still fire with an empty secretRef; built=%v secretRef=%q", built, sawSecretRef)
			}
		})
	}
}

func TestComponentService_TriggerBuild_StagerFailureAborts(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(context.Context, string, string, string, string, string) (*gen.WorkflowRun, error) {
		t.Error("build must not fire when the secret staging failed")
		return nil, nil
	}}
	repo := &stubRepoSvc{GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
		return &sourcecontrol.GitRepository{RepoSlug: "owner-repo"}, nil
	}}
	stager := &stubBuildStager{StageBuildSecretFunc: func(context.Context, string, string, string) (string, error) {
		return "", errors.New("openbao unreachable")
	}}
	svc := NewComponentService(oc, nil, nil, repo, stager)
	_, err := svc.TriggerBuild(context.Background(), "acme", "web", "svc")
	if err == nil || !strings.Contains(err.Error(), "stage-build-secret") {
		t.Fatalf("stager failure must abort the build with a wrapped error, got %v", err)
	}
}

func TestComponentService_TriggerBuild_OCErrorPropagates(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(context.Context, string, string, string, string, string) (*gen.WorkflowRun, error) {
		return nil, openchoreo.ErrNotFound
	}}
	if _, err := NewComponentService(oc, nil, nil, nil, nil).TriggerBuild(context.Background(), "acme", "web", "svc"); !errors.Is(err, openchoreo.ErrNotFound) {
		t.Fatalf("OC trigger error must propagate, got %v", err)
	}
}

// --- GetBuildLogs -------------------------------------------------------------

func TestComponentService_GetBuildLogs_NotConfigured(t *testing.T) {
	t.Parallel()
	// nil observability client ⇒ the local ErrLogsUnavailable sentinel (the HTTP
	// op maps it to 503).
	svc := NewComponentService(&ocmocks.ComponentClientMock{}, nil, nil, nil, nil)
	if _, err := svc.GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 0); !errors.Is(err, ErrLogsUnavailable) {
		t.Fatalf("nil observ client must return ErrLogsUnavailable, got %v", err)
	}
}

func TestComponentService_GetBuildLogs_SuccessAndError(t *testing.T) {
	t.Parallel()
	observ := &stubObservClient{GetBuildLogsFunc: func(_ context.Context, org, proj, comp, build string, _ time.Time) (*gen.BuildLogs, error) {
		if org != "acme" || build != "run-1" {
			t.Errorf("observ args: org=%q build=%q", org, build)
		}
		return &gen.BuildLogs{TotalCount: 2, Logs: []gen.BuildLogEntry{{Log: "a"}, {Log: "b"}}}, nil
	}}
	// The service reads the build's terminal state before its log, so the OC
	// client is exercised here too.
	terminal := &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(context.Context, string, string) (*gen.WorkflowRun, error) {
			return &gen.WorkflowRun{Name: "run-1", Completed: true}, nil
		},
	}
	logs, err := NewComponentService(terminal, observ, nil, nil, nil).GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 0)
	if err != nil || logs == nil || logs.TotalCount != 2 {
		t.Fatalf("logs happy: logs=%+v err=%v", logs, err)
	}

	observErr := &stubObservClient{GetBuildLogsFunc: func(context.Context, string, string, string, string, time.Time) (*gen.BuildLogs, error) {
		return nil, errors.New("observ 500")
	}}
	_, err = NewComponentService(terminal, observErr, nil, nil, nil).GetBuildLogs(context.Background(), "a", "p", "c", "b", 0)
	if err == nil || !strings.Contains(err.Error(), "get build logs") {
		t.Fatalf("observ error must wrap with 'get build logs', got %v", err)
	}
}

// --- GetComponentOpenAPI (real ArtifactStore over a faked design tree) --------

// designFiles builds the working-tree file map ReadDesign assembles: a root
// design.cell (required, else ReadDesign returns nil) plus one component dir with
// the given type + optional openapi.yaml.
func designFiles(componentDir, componentType, openapi string) map[string]string {
	files := map[string]string{
		spec.DesignRootFile: "# Overview\n",
		"components/" + componentDir + "/design.json": "{\n  \"name\": \"" + componentDir +
			"\",\n  \"type\": \"" + componentType + "\",\n  \"description\": \"body\",\n  \"dependencies\": []\n}\n",
	}
	if openapi != "" {
		files["components/"+componentDir+"/openapi.yaml"] = openapi
	}
	return files
}

func openAPISvc(t *testing.T, files map[string]string, listErr error) ComponentService {
	t.Helper()
	fake := &artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, listErr
		},
	}
	// The REAL ArtifactStore decorator wraps the fake, so ReadDesign +
	// AssembleDesign run for real over the crafted tree.
	return NewComponentService(&ocmocks.ComponentClientMock{}, nil, spec.NewArtifactStore(fake), nil, nil)
}

func TestComponentService_GetComponentOpenAPI_NoArtifactStore(t *testing.T) {
	t.Parallel()
	svc := NewComponentService(&ocmocks.ComponentClientMock{}, nil, nil, nil, nil)
	if _, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "svc"); err == nil || !strings.Contains(err.Error(), "artifact store not configured") {
		t.Fatalf("nil artifact store must error, got %v", err)
	}
}

func TestComponentService_GetComponentOpenAPI_NotFoundPaths(t *testing.T) {
	t.Parallel()
	// No design.cell at all ⇒ ReadDesign returns (nil,nil) ⇒ ErrComponentNotFound.
	svc := openAPISvc(t, map[string]string{}, nil)
	if _, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "svc"); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("empty tree: want ErrComponentNotFound, got %v", err)
	}

	// Design exists but names a DIFFERENT component ⇒ ErrComponentNotFound.
	svc = openAPISvc(t, designFiles("other", "service", "openapi: 3.0.3"), nil)
	if _, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "svc"); !errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("no matching component: want ErrComponentNotFound, got %v", err)
	}

	// A non-NotFound list error is wrapped, not silently swallowed.
	svc = openAPISvc(t, nil, errors.New("git wedged"))
	if _, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "svc"); err == nil || errors.Is(err, ErrComponentNotFound) {
		t.Fatalf("read error must propagate (not NotFound), got %v", err)
	}
}

func TestComponentService_GetComponentOpenAPI_NotServiceReturnsTypedBody(t *testing.T) {
	t.Parallel()
	// A web-application component (non-service) returns ErrComponentNotService
	// PLUS a body carrying the type so the UI renders a typed empty state — the
	// HTTP op maps this pair to a 409 that still ships componentType.
	svc := openAPISvc(t, designFiles("web-ui", "web-application", ""), nil)
	spec, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "web-ui")
	if !errors.Is(err, ErrComponentNotService) {
		t.Fatalf("want ErrComponentNotService, got %v", err)
	}
	if spec == nil || spec.ComponentType != "web-application" || spec.ComponentName != "web-ui" || spec.Spec != "" {
		t.Fatalf("not-service body must carry the type with no spec: %+v", spec)
	}
}

func TestComponentService_GetComponentOpenAPI_ServiceReturnsSpec(t *testing.T) {
	t.Parallel()
	svc := openAPISvc(t, designFiles("hello-api", "service", "openapi: 3.0.3\ninfo:\n  title: X\n"), nil)
	spec, err := svc.GetComponentOpenAPI(context.Background(), "acme", "web", "hello-api")
	if err != nil {
		t.Fatalf("service openapi: %v", err)
	}
	if spec == nil || spec.ComponentType != "service" || !strings.Contains(spec.Spec, "openapi: 3.0.3") {
		t.Fatalf("service body must carry the spec: %+v", spec)
	}
}

// MapComponentError's sentinel mapping is pinned in the projects package
// (internal/projects/httperrors_test.go) — the shared mapper lives in the domain
// root the slices import (projects HTTP handler extraction, P7b).

// TestComponentService_CreateComponent_PassthroughAndError mirrors its sibling
// passthrough tests (review follow-up): CreateComponent has no HTTP surface —
// codingagent's dispatch calls it — so the unit tier owns its contract.
func TestComponentService_CreateComponent_PassthroughAndError(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{
		CreateComponentFunc: func(_ context.Context, org, proj string, req *openchoreo.CreateComponentRequest) (*gen.Component, error) {
			return &gen.Component{Name: req.Name}, nil
		},
	}
	svc := NewComponentService(oc, nil, nil, nil, nil)
	comp, err := svc.CreateComponent(context.Background(), "acme", "web", &openchoreo.CreateComponentRequest{Name: "svc-a"})
	if err != nil || comp == nil || comp.Name != "svc-a" {
		t.Fatalf("create happy: comp=%+v err=%v", comp, err)
	}
	if c := oc.CreateComponentCalls(); len(c) != 1 || c[0].OrgName != "acme" || c[0].ProjectName != "web" || c[0].Req.Name != "svc-a" {
		t.Fatalf("OC CreateComponent args: %+v", c)
	}

	ocErr := &ocmocks.ComponentClientMock{
		CreateComponentFunc: func(context.Context, string, string, *openchoreo.CreateComponentRequest) (*gen.Component, error) {
			return nil, openchoreo.ErrConflict
		},
	}
	if _, err := NewComponentService(ocErr, nil, nil, nil, nil).CreateComponent(context.Background(), "acme", "web", &openchoreo.CreateComponentRequest{Name: "svc-a"}); !errors.Is(err, openchoreo.ErrConflict) {
		t.Fatalf("create error must propagate the OC sentinel verbatim, got %v", err)
	}
}

// TestOcEntrypoint_CanonicalWebAppKind guards against the vocabulary drift
// bug: the canonical kind is "web-application" — OpenChoreo's own term
// (spec.ComponentTypeWebApplication) — and ocEntrypoint merely re-attaches
// OC's `deployment/` prefix. Retired spellings ("webapp", "web-app") are NOT
// understood anywhere; designs carrying them must be migrated. Unknown kinds
// keep falling back to deployment/service.
func TestOcEntrypoint_CanonicalWebAppKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		componentType string
		want          string
	}{
		{"canonical web-application", "web-application", "deployment/web-application"},
		{"retired webapp spelling is not a web application", "webapp", "deployment/service"},
		{"retired web-app spelling is not a web application", "web-app", "deployment/service"},
		{"service", "service", "deployment/service"},
		{"empty", "", "deployment/service"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ocEntrypoint(tc.componentType); got != tc.want {
				t.Errorf("ocEntrypoint(%q) = %q, want %q", tc.componentType, got, tc.want)
			}
		})
	}
}

// --- GetBuildLogs cursor -------------------------------------------------------
//
// The cursor is what lets ONE endpoint serve a live build and a finished one.
// These pin the three rules that make it safe: entries at the cursor are not
// replayed, the cursor only ever moves forward, and "complete" is never claimed
// on a build that might still be writing.

func logsAt(times ...string) *gen.BuildLogs {
	out := &gen.BuildLogs{}
	for i, ts := range times {
		out.Logs = append(out.Logs, gen.BuildLogEntry{Timestamp: ts, Log: fmt.Sprintf("line %d", i)})
	}
	return out
}

func buildLogsSvc(t *testing.T, completed bool, entries *gen.BuildLogs, capture *time.Time) ComponentService {
	t.Helper()
	oc := &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(context.Context, string, string) (*gen.WorkflowRun, error) {
			return &gen.WorkflowRun{Name: "run-1", Completed: completed}, nil
		},
	}
	observ := &stubObservClient{GetBuildLogsFunc: func(_ context.Context, _, _, _, _ string, since time.Time) (*gen.BuildLogs, error) {
		if capture != nil {
			*capture = since
		}
		return entries, nil
	}}
	return NewComponentService(oc, observ, nil, nil, nil)
}

func TestComponentService_GetBuildLogs_TerminalBuildIsComplete(t *testing.T) {
	t.Parallel()
	svc := buildLogsSvc(t, true, logsAt("2026-07-27T10:42:01Z", "2026-07-27T10:42:04Z"), nil)

	logs, err := svc.GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 0)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !logs.Complete {
		t.Error("a terminal build must report complete so the client stops polling")
	}
	want := time.Date(2026, 7, 27, 10, 42, 4, 0, time.UTC).UnixMilli()
	if logs.NextCursor != want {
		t.Errorf("nextCursor = %d, want the newest entry's millis %d", logs.NextCursor, want)
	}
}

func TestComponentService_GetBuildLogs_RunningBuildIsIncomplete(t *testing.T) {
	t.Parallel()
	svc := buildLogsSvc(t, false, logsAt("2026-07-27T10:42:01Z"), nil)

	logs, err := svc.GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 0)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if logs.Complete {
		t.Error("a running build must report incomplete — there is more to come")
	}
}

// An unreadable WorkflowRun must NOT be read as complete: the client polling
// once more is recoverable, a truncated log silently is not.
func TestComponentService_GetBuildLogs_UnreadableRunIsNotComplete(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{
		GetWorkflowRunFunc: func(context.Context, string, string) (*gen.WorkflowRun, error) {
			return nil, errors.New("cluster down")
		},
	}
	observ := &stubObservClient{GetBuildLogsFunc: func(context.Context, string, string, string, string, time.Time) (*gen.BuildLogs, error) {
		return logsAt("2026-07-27T10:42:01Z"), nil
	}}
	logs, err := NewComponentService(oc, observ, nil, nil, nil).
		GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 0)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if logs.Complete {
		t.Error("an unreadable run must not be reported complete")
	}
}

// The observability window is inclusive of its start, so the entry the cursor
// names would otherwise be re-emitted on every poll.
func TestComponentService_GetBuildLogs_CursorDoesNotReplayItsOwnEntry(t *testing.T) {
	t.Parallel()
	at := func(s string) int64 {
		ts, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatalf("fixture: %v", err)
		}
		return ts.UnixMilli()
	}
	var narrowed time.Time
	svc := buildLogsSvc(t, false, logsAt("2026-07-27T10:42:01Z", "2026-07-27T10:42:04Z"), &narrowed)

	cursor := at("2026-07-27T10:42:01Z")
	logs, err := svc.GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", cursor)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(logs.Logs) != 1 || logs.Logs[0].Timestamp != "2026-07-27T10:42:04Z" {
		t.Fatalf("entries = %+v, want only the one after the cursor", logs.Logs)
	}
	// The window is narrowed at the source too, so a tail poll is cheap there.
	if narrowed.UnixMilli() != cursor {
		t.Errorf("observability start = %v, want the cursor %d", narrowed, cursor)
	}
}

// An entry with no parseable timestamp cannot be ordered against the cursor, so
// it is kept: a duplicated line is a smaller harm than a dropped one.
func TestComponentService_GetBuildLogs_UnparseableTimestampIsKept(t *testing.T) {
	t.Parallel()
	entries := &gen.BuildLogs{Logs: []gen.BuildLogEntry{{Log: "no timestamp"}}}
	svc := buildLogsSvc(t, true, entries, nil)

	logs, err := svc.GetBuildLogs(context.Background(), "acme", "web", "svc", "run-1", 1)
	if err != nil {
		t.Fatalf("logs: %v", err)
	}
	if len(logs.Logs) != 1 {
		t.Fatalf("entries = %+v, want the untimestamped line kept", logs.Logs)
	}
	if logs.NextCursor != 0 {
		t.Errorf("nextCursor = %d, want it unmoved when nothing could be timed", logs.NextCursor)
	}
}
