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

// COMPONENT tier (bff-component-testing.md §4): the REAL component + config
// services behind the REAL production handler chain — global middleware → faked
// auth at the jwt.WithClaims seam → orgensure → Huma parsing/validation → the
// tenant gate in ENFORCE → each handler's error mapping — driven in-process via
// the componenttest harness. Only out-of-process seams are mocked: the
// OpenChoreo ComponentClient, the observability client, the config repository,
// and the design tree behind a real ArtifactStore.
//
// ORG SCOPE: the active org is derived SOLELY from the token (no {orgHandle}
// path param), so the only runtime auth assertion this tier adds is the gate's
// no-claims 401 on an org-scoped op (proven once). There is no path-based
// cross-org 404 to sweep in the token-only model.
//
// GOLDEN FIELD SETS: the harvested goldens carry a Huma `$schema` link the
// current handler no longer emits, so field sets are compared with $schema
// excluded from both sides.
//
// OC-SENTINEL MAPPING: componentService passes OpenChoreo sentinels through
// untranslated, and mapComponentError now runs them through the shared ocerr
// classifier — so an OC 404 / 401 / 403 / 409 surfaces as that status (matching
// project), and a missing component (openchoreo.ErrNotFound) is a real 404. The
// former get-component ErrComponentNotFound branch (unreachable — the service
// never returns that feature-local sentinel) was removed with the fix.
// TestComponentComponent_ErrorMapping pins this.
//
// External test package: the harness imports api, which imports component — an
// in-package test file would be an import cycle.
package component_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/internal/feature/component"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

const compProjectPrefix = "/api/v1/projects/web/components"

// --- harness ------------------------------------------------------------------

type compFakes struct {
	oc         *ocmocks.ComponentClientMock // component OC client (defaulted when nil)
	observ     observability.Client         // nil ⇒ build-logs takes the not-configured 503 path
	store      *artifacts.ArtifactStore     // for the openapi read; nil is fine unless hit
	configRepo repositories.ConfigRepository
}

// newHarness assembles the real chain around the REAL component + config
// services, with only the supplied out-of-process seams programmed. TriggerBuild
// leaves the repo/stager ports nil (no secret staging) — that chain is
// unit-proven; here the build fires straight at the mocked OC client.
func newHarness(t *testing.T, f compFakes) *componenttest.Harness {
	t.Helper()
	if f.oc == nil {
		f.oc = &ocmocks.ComponentClientMock{}
	}
	compSvc := component.NewComponentService(f.oc, f.observ, f.store, nil, nil)
	var cfgSvc component.ConfigService
	if f.configRepo != nil {
		// The env-var mirror onto OC is unit-tested; disable it here (nil).
		cfgSvc = component.NewConfigService(f.configRepo, nil)
	}
	return componenttest.New(t, componenttest.Options{Deps: api.HumaDeps{
		ComponentSvc: compSvc,
		ConfigSvc:    cfgSvc,
	}})
}

// --- golden helpers -----------------------------------------------------------

func goldenPath(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "harvest", "golden", name)
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(goldenPath(name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return b
}

// sansSchema drops the Huma `$schema` link key so a golden's field set (which
// still carries it) compares against the current handler's (which omits it).
func sansSchema(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "$schema" {
			out = append(out, k)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nestedElementKeys returns element[0]'s sorted keys of the array under the
// given object field (e.g. the `items` array of a list envelope).
func nestedElementKeys(t *testing.T, raw []byte, field string) []string {
	t.Helper()
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, string(raw))
	}
	var arr []map[string]any
	if err := json.Unmarshal(obj[field], &arr); err != nil {
		t.Fatalf("field %q is not a JSON array of objects: %v", field, err)
	}
	if len(arr) == 0 {
		t.Fatalf("field %q is an empty array: %s", field, string(raw))
	}
	return sortedKeys(arr[0])
}

// designFilesFor builds the working-tree file map ReadDesign assembles: a root
// design.md plus one component dir with the given type + optional openapi.yaml.
func designFilesFor(componentDir, componentType, openapi string) map[string]string {
	files := map[string]string{
		artifacts.DesignRootFile: "# Overview\n",
		"components/" + componentDir + "/design.json": "{\n  \"name\": \"" + componentDir +
			"\",\n  \"type\": \"" + componentType + "\",\n  \"description\": \"body\",\n  \"dependencies\": []\n}\n",
	}
	if openapi != "" {
		files["components/"+componentDir+"/openapi.yaml"] = openapi
	}
	return files
}

func storeReturning(files map[string]string) *artifacts.ArtifactStore {
	return artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

// --- golden-shaped fixtures ---------------------------------------------------

// goldenComponent mirrors testdata/harvest/golden/get_component.json's populated
// field set (autoBuild is omitted there, so it stays false → dropped by omitempty).
func goldenComponent() models.Component {
	return models.Component{
		UID:         "bd8b1849-98d5-4dc3-812a-568183cddfbd",
		Name:        "hello-api",
		ProjectName: "hello-world-api",
		DisplayName: "hello-api",
		Description: "Implement hello-api Go service with greeting endpoint",
		Type:        "deployment/service",
		AutoDeploy:  true,
		CreatedAt:   "2026-07-01T05:41:54Z",
		Status:      "ComponentReleaseReady",
	}
}

func goldenWorkflowRun() models.WorkflowRun {
	return models.WorkflowRun{
		Name:          "hello-world-api-hello-api-1782887236864",
		Status:        "WorkflowSucceeded",
		StartedAt:     "2026-07-01T06:27:17Z",
		ComponentName: "hello-api",
		ProjectName:   "hello-world-api",
		Completed:     true,
		Tasks:         []models.WorkflowRunTask{{Name: "checkout-source", Phase: "Succeeded", StartedAt: "2026-07-01T06:27:20Z", CompletedAt: "2026-07-01T06:27:54Z"}},
	}
}

func goldenDeployment() models.Deployment {
	return models.Deployment{
		Name:          "hello-world-api-hello-api-development",
		Environment:   "development",
		ReleaseName:   "hello-world-api-hello-api-77767bbd6",
		ComponentName: "hello-api",
		EndpointURL:   "http://development-default.openchoreoapis.localhost:19080/hello-world-api-hello-api-http",
		CreatedAt:     "2026-07-01T06:30:57Z",
		Status:        "Ready",
	}
}

// --- list / get ---------------------------------------------------------------

func TestComponentComponent_ListMatchesGoldenElementShape(t *testing.T) {
	t.Parallel()
	var sawOrg, sawProject string
	oc := &ocmocks.ComponentClientMock{ListComponentsFunc: func(_ context.Context, org, proj string, _ int, _ string) (*models.ComponentList, error) {
		sawOrg, sawProject = org, proj
		return &models.ComponentList{Items: []models.Component{goldenComponent()}}, nil
	}}
	h := newHarness(t, compFakes{oc: oc})

	resp := h.AsOrg("acme").Get(compProjectPrefix)
	if resp.Code != 200 {
		t.Fatalf("list: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := nestedElementKeys(t, resp.Body.Bytes(), "items")
	want := nestedElementKeys(t, readGolden(t, "get_components.json"), "items")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list element shape drifted from golden:\n got %v\nwant %v", got, want)
	}
	// The org the service saw MUST be the token org, bound by the gate from claims;
	// the project comes from the path.
	if sawOrg != "acme" || sawProject != "web" {
		t.Fatalf("service scope: got (%q,%q), want (acme,web)", sawOrg, sawProject)
	}
}

func TestComponentComponent_GetMatchesGoldenFieldSet(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{GetComponentFunc: func(_ context.Context, _, _, comp string) (*models.Component, error) {
		c := goldenComponent()
		return &c, nil
	}}
	h := newHarness(t, compFakes{oc: oc})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api")
	if resp.Code != 200 {
		t.Fatalf("get: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := sansSchema(componenttest.FieldSet(t, resp.Body.String()))
	want := sansSchema(componenttest.GoldenFieldSet(t, goldenPath("get_component.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("get field set drifted from golden:\n got %v\nwant %v", got, want)
	}
}

// --- builds / deployments ------------------------------------------------------

func TestComponentComponent_ListBuildsMatchesGoldenElementShape(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{ListWorkflowRunsFunc: func(context.Context, string, string, string, int, string) (*models.WorkflowRunList, error) {
		return &models.WorkflowRunList{Items: []models.WorkflowRun{goldenWorkflowRun()}}, nil
	}}
	h := newHarness(t, compFakes{oc: oc})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/builds")
	if resp.Code != 200 {
		t.Fatalf("builds: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := nestedElementKeys(t, resp.Body.Bytes(), "items")
	want := nestedElementKeys(t, readGolden(t, "get_component_builds.json"), "items")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("builds element shape drifted from golden:\n got %v\nwant %v", got, want)
	}
}

func TestComponentComponent_ListDeploymentsMatchesGoldenElementShape(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ComponentClientMock{ListDeploymentsFunc: func(context.Context, string, string, string) (*models.DeploymentList, error) {
		return &models.DeploymentList{Items: []models.Deployment{goldenDeployment()}}, nil
	}}
	h := newHarness(t, compFakes{oc: oc})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/deployments")
	if resp.Code != 200 {
		t.Fatalf("deployments: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := nestedElementKeys(t, resp.Body.Bytes(), "items")
	want := nestedElementKeys(t, readGolden(t, "get_component_deployments.json"), "items")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deployments element shape drifted from golden:\n got %v\nwant %v", got, want)
	}
}

// --- openapi -------------------------------------------------------------------

func TestComponentComponent_OpenAPIService_MatchesGoldenFieldSet(t *testing.T) {
	t.Parallel()
	// A service component with a spec → 200 carrying {componentName, componentType, spec}.
	h := newHarness(t, compFakes{store: storeReturning(designFilesFor("hello-api", "service", "openapi: 3.0.3\ninfo:\n  title: X\n"))})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/openapi")
	if resp.Code != 200 {
		t.Fatalf("openapi service: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := sansSchema(componenttest.FieldSet(t, resp.Body.String()))
	want := sansSchema(componenttest.GoldenFieldSet(t, goldenPath("get_component_openapi.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("openapi field set drifted from golden:\n got %v\nwant %v", got, want)
	}
}

func TestComponentComponent_OpenAPINotServiceReturns409WithType(t *testing.T) {
	t.Parallel()
	// A non-service component → 409 whose body still carries the componentType so
	// the UI renders a typed empty state.
	h := newHarness(t, compFakes{store: storeReturning(designFilesFor("web-ui", "web-application", ""))})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/web-ui/openapi")
	if resp.Code != 409 {
		t.Fatalf("openapi non-service: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"componentType":"web-application"`) {
		t.Fatalf("409 body must carry the component type: %s", resp.Body.String())
	}
}

func TestComponentComponent_OpenAPINoComponentIs404(t *testing.T) {
	t.Parallel()
	// The design names a different component → ErrComponentNotFound → 404.
	h := newHarness(t, compFakes{store: storeReturning(designFilesFor("hello-api", "service", "openapi: 3.0.3"))})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/ghost/openapi")
	if resp.Code != 404 {
		t.Fatalf("openapi missing: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "no OpenAPI spec for this component" {
		t.Fatalf("404 detail: got %q", p.Detail)
	}
}

// --- trigger-build -------------------------------------------------------------

func TestComponentComponent_TriggerBuild_HappyAndError(t *testing.T) {
	t.Parallel()
	// Happy → 201 with the WorkflowRun body; exactly one OC TriggerBuild call.
	oc := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(_ context.Context, org, proj, comp, secretRef, runName string) (*models.WorkflowRun, error) {
		return &models.WorkflowRun{Name: runName, Status: "Pending"}, nil
	}}
	h := newHarness(t, compFakes{oc: oc})
	resp := h.AsOrg("acme").Post(compProjectPrefix+"/hello-api/builds", "")
	if resp.Code != 201 {
		t.Fatalf("trigger: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	if c := oc.TriggerBuildCalls(); len(c) != 1 || c[0].OrgName != "acme" || c[0].ProjectName != "web" || c[0].ComponentName != "hello-api" {
		t.Fatalf("OC TriggerBuild call: %+v", c)
	}

	// OC error → 500 (opaque, no internal leak).
	ocErr := &ocmocks.ComponentClientMock{TriggerBuildFunc: func(context.Context, string, string, string, string, string) (*models.WorkflowRun, error) {
		return nil, errors.New("oc: workflow rejected")
	}}
	resp = newHarness(t, compFakes{oc: ocErr}).AsOrg("acme").Post(compProjectPrefix+"/hello-api/builds", "")
	if resp.Code != 500 {
		t.Fatalf("trigger error: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "workflow rejected") {
		t.Fatalf("500 body leaks internals: %s", resp.Body.String())
	}
}

// --- build logs ----------------------------------------------------------------

func TestComponentComponent_BuildLogs_HarvestedError500(t *testing.T) {
	t.Parallel()
	// Reproduces the harvested golden (get_component_build_logs.json): the
	// observability client is configured but its fetch fails → the service wraps
	// the error → mapComponentError's default 500 with detail "failed to get build logs".
	observ := &extObservClient{GetBuildLogsFunc: func(context.Context, string, string, string, string) (*models.BuildLogs, error) {
		return nil, errors.New("observability service 500")
	}}
	h := newHarness(t, compFakes{observ: observ})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/builds/run-1/logs")
	if resp.Code != 500 {
		t.Fatalf("build logs error: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := componenttest.DecodeProblem(t, resp.Body.String())
	var golden componenttest.Problem
	if err := json.Unmarshal(readGolden(t, "get_component_build_logs.json"), &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if got.Status != golden.Status || got.Title != golden.Title || got.Detail != golden.Detail {
		t.Fatalf("build-logs 500 drifted from golden:\n got %+v\nwant %+v", got, golden)
	}
	if strings.Contains(resp.Body.String(), "observability service 500") {
		t.Fatalf("500 body leaks internals: %s", resp.Body.String())
	}
}

func TestComponentComponent_BuildLogs_NotConfigured503(t *testing.T) {
	t.Parallel()
	// nil observability client ⇒ ErrLogsUnavailable ⇒ the handler's 503 branch.
	h := newHarness(t, compFakes{observ: nil})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/builds/run-1/logs")
	if resp.Code != 503 {
		t.Fatalf("build logs not-configured: want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "build logs service not available" {
		t.Fatalf("503 detail: got %q", p.Detail)
	}
}

// --- config get (the 200-null quirk) + error ----------------------------------

func TestComponentComponent_ConfigGet_NullBodyWhenNoRow(t *testing.T) {
	t.Parallel()
	// The harvested golden get_component_config.json is literal `null`: a 200 with
	// a JSON-null body when no config row exists (a nil *ComponentConfig marshals
	// to null). Pin that exact quirk.
	repo := &extConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*models.ComponentConfig, error) {
		return nil, nil
	}}
	h := newHarness(t, compFakes{configRepo: repo})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/configs")
	if resp.Code != 200 {
		t.Fatalf("config get: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if body := strings.TrimSpace(resp.Body.String()); body != "null" {
		t.Fatalf("config get with no row must be a JSON null body, got %q", body)
	}
	// And it matches the harvested golden byte-for-byte (both are `null`).
	if strings.TrimSpace(string(readGolden(t, "get_component_config.json"))) != strings.TrimSpace(resp.Body.String()) {
		t.Fatalf("config-null body drifted from golden")
	}
}

func TestComponentComponent_ConfigGet_ErrorIs500(t *testing.T) {
	t.Parallel()
	repo := &extConfigRepo{GetByComponentFunc: func(context.Context, string, string, string) (*models.ComponentConfig, error) {
		return nil, errors.New("pg: connection refused")
	}}
	h := newHarness(t, compFakes{configRepo: repo})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/hello-api/configs")
	if resp.Code != 500 {
		t.Fatalf("config get error: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "failed to get config" {
		t.Fatalf("500 detail: got %q", p.Detail)
	}
	if strings.Contains(resp.Body.String(), "connection refused") {
		t.Fatalf("500 body leaks internals: %s", resp.Body.String())
	}
}

// --- config update: happy + validation ----------------------------------------

func TestComponentComponent_ConfigUpdate_Happy(t *testing.T) {
	t.Parallel()
	var saved *models.ComponentConfig
	repo := &extConfigRepo{UpsertFunc: func(_ context.Context, c *models.ComponentConfig) error {
		saved = c
		return nil
	}}
	h := newHarness(t, compFakes{configRepo: repo})
	resp := h.AsOrg("acme").Put(compProjectPrefix+"/hello-api/configs", `{"envVars":[{"key":"DB_HOST","value":"db"}]}`)
	if resp.Code != 200 {
		t.Fatalf("config update: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"DB_HOST"`) {
		t.Fatalf("update body must echo the persisted config: %s", resp.Body.String())
	}
	// The write is scoped to the token org + path component, never client input.
	if saved == nil || saved.OrgID != "acme" || saved.ProjectName != "web" || saved.ComponentName != "hello-api" {
		t.Fatalf("Upsert scope: %+v", saved)
	}
}

func TestComponentComponent_ConfigUpdate_ValidationIs400(t *testing.T) {
	t.Parallel()
	// The legacy contract maps any update error (validation or repo) to 400 with
	// the error string. Upsert must never run on invalid input.
	repo := &extConfigRepo{UpsertFunc: func(context.Context, *models.ComponentConfig) error {
		t.Error("Upsert must not run on invalid env vars")
		return nil
	}}
	h := newHarness(t, compFakes{configRepo: repo})

	// Empty key → 400.
	resp := h.AsOrg("acme").Put(compProjectPrefix+"/hello-api/configs", `{"envVars":[{"key":"","value":"v"}]}`)
	if resp.Code != 400 {
		t.Fatalf("empty key: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.Contains(p.Detail, "cannot be empty") {
		t.Fatalf("400 detail: got %q", p.Detail)
	}

	// Duplicate key → 400 naming the key.
	resp = h.AsOrg("acme").Put(compProjectPrefix+"/hello-api/configs", `{"envVars":[{"key":"DB","value":"1"},{"key":"DB","value":"2"}]}`)
	if resp.Code != 400 {
		t.Fatalf("duplicate key: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.Contains(p.Detail, "duplicate environment variable key: DB") {
		t.Fatalf("400 detail: got %q", p.Detail)
	}
}

// --- slug validation -----------------------------------------------------------

func TestComponentComponent_MalformedSlugIs400(t *testing.T) {
	t.Parallel()
	// A componentName that isn't a DNS-label slug is rejected at the handler's
	// requireComponentSlugs guard — before the service (OC client) is touched.
	oc := &ocmocks.ComponentClientMock{GetComponentFunc: func(context.Context, string, string, string) (*models.Component, error) {
		t.Error("service must not be reached for a malformed slug")
		return nil, nil
	}}
	h := newHarness(t, compFakes{oc: oc})
	resp := h.AsOrg("acme").Get(compProjectPrefix + "/UPPER") // uppercase fails the slug regex
	if resp.Code != 400 {
		t.Fatalf("malformed slug: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); !strings.Contains(p.Detail, "componentName") {
		t.Fatalf("400 detail should name componentName: got %q", p.Detail)
	}
}

// --- error mapping (OC sentinel → RFC-9457 status) ----------------------------

// TestComponentComponent_ErrorMapping pins the FIXED behavior: every OpenChoreo
// sentinel that reaches componentService is now translated to its HTTP status by
// the shared ocerr classifier (component_huma.go's mapComponentError), matching
// project. An opaque error still collapses to a fixed-message 500 with no
// internal leak.
func TestComponentComponent_ErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"oc not-found → 404", openchoreo.ErrNotFound, 404},
		{"oc unauthorized → 401", openchoreo.ErrUnauthorized, 401},
		{"oc forbidden → 403", openchoreo.ErrForbidden, 403},
		{"oc conflict → 409", openchoreo.ErrConflict, 409},
		{"opaque → 500", errors.New("pg: connection refused"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			oc := &ocmocks.ComponentClientMock{GetComponentFunc: func(context.Context, string, string, string) (*models.Component, error) {
				return nil, tc.err
			}}
			resp := newHarness(t, compFakes{oc: oc}).AsOrg("acme").Get(compProjectPrefix + "/hello-api")
			if resp.Code != tc.wantStatus {
				t.Fatalf("%s: got %d body=%s", tc.name, resp.Code, resp.Body.String())
			}
			p := componenttest.DecodeProblem(t, resp.Body.String())
			if p.Status != tc.wantStatus {
				t.Fatalf("%s: problem.status=%d, want %d", tc.name, p.Status, tc.wantStatus)
			}
			if tc.wantStatus == 500 {
				// Opaque errors keep the fixed internal message and never leak.
				if p.Detail != "failed to get component" {
					t.Fatalf("500 detail: got %q", p.Detail)
				}
				if strings.Contains(resp.Body.String(), "connection refused") {
					t.Fatalf("500 must not leak internals: %s", resp.Body.String())
				}
			}
		})
	}
}

// --- auth ----------------------------------------------------------------------

func TestComponentComponent_NoClaimsDeniedByEnforceGate(t *testing.T) {
	t.Parallel()
	h := newHarness(t, compFakes{})
	resp := h.NoAuth().Get(compProjectPrefix)
	if resp.Code != 401 {
		t.Fatalf("no-claims: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	// The gate aggregates its resolver error into one RFC-9457 problem carrying
	// "authentication required" — NOT the JWT middleware's pre-gate rejection
	// (that path is integration-owned, see §3).
	p := componenttest.DecodeProblem(t, resp.Body.String())
	if p.Status != 401 || len(p.Errors) == 0 || !strings.Contains(p.Errors[0].Message, "authentication required") {
		t.Fatalf("gate 401 problem shape: got %s", resp.Body.String())
	}
}
