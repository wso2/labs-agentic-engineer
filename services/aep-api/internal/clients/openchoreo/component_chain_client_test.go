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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

const (
	chainTestOrg     = "wc-abc"
	chainTestProject = "widgets"
	chainTestComp    = "ca-run"
	chainTestImage   = "ghcr.io/wso2/aep/remote-worker:latest"
	chainTestRelease = "widgets-ca-run-release"
)

const chainTestEnv = "development"

func newTestComponentClient(t *testing.T, srv *httptest.Server) ComponentClient {
	t.Helper()
	return NewComponentClient(Config{BaseURL: srv.URL})
}

func chainScopedName() string {
	return ScopedComponentName(chainTestProject, chainTestComp)
}

func sampleWorkloadInput() WorkloadInput {
	return WorkloadInput{
		ComponentName: chainTestComp,
		Image:         chainTestImage,
		Env:           []WorkflowEnvVarRef{{Key: "FOO", Value: "bar"}},
		Labels:        map[string]string{"aep.wso2.com/internal": "true"},
	}
}

// ---- EnsureWorkload ---------------------------------------------------------

func TestEnsureWorkload_Create(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"metadata": map[string]any{"name": chainScopedName()},
		})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.EnsureWorkload(context.Background(), chainTestOrg, chainTestProject, sampleWorkloadInput()); err != nil {
		t.Fatalf("EnsureWorkload: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/namespaces/wc-abc/workloads" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	meta, _ := gotBody["metadata"].(map[string]any)
	if meta["name"] != chainScopedName() {
		t.Errorf("unexpected workload name: %v", meta["name"])
	}
}

func TestEnsureWorkload_ConflictIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.EnsureWorkload(context.Background(), chainTestOrg, chainTestProject, sampleWorkloadInput()); err != nil {
		t.Fatalf("EnsureWorkload on 409: %v", err)
	}
}

func TestEnsureWorkload_ServerErrorWrapsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	err := c.EnsureWorkload(context.Background(), chainTestOrg, chainTestProject, sampleWorkloadInput())
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInternalServerError) {
		t.Errorf("expected ErrInternalServerError, got %v", err)
	}
}

// ---- EnsureRelease ----------------------------------------------------------

func TestEnsureRelease_Create(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	scoped := chainScopedName()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"metadata": map[string]any{"name": chainTestRelease},
		})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	got, err := c.EnsureRelease(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestRelease)
	if err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	wantPath := "/api/v1/namespaces/wc-abc/components/" + scoped + "/generate-release"
	if gotMethod != http.MethodPost || gotPath != wantPath {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if gotBody["releaseName"] != chainTestRelease {
		t.Errorf("unexpected releaseName: %v", gotBody["releaseName"])
	}
	if got != chainTestRelease {
		t.Errorf("got release %q, want %q", got, chainTestRelease)
	}
}

func TestEnsureRelease_ConflictIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	got, err := c.EnsureRelease(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestRelease)
	if err != nil {
		t.Fatalf("EnsureRelease on 409: %v", err)
	}
	if got != chainTestRelease {
		t.Errorf("got release %q, want caller-supplied name on conflict", got)
	}
}

// The failure this pins was live, not hypothetical: openchoreo-api answers a
// generate-release for a name that already exists with a bare 500, so the deploy
// stage's own retry — which re-cuts the releases it already cut — could never
// succeed again once it had half succeeded once. It retried every ~100 seconds
// for twenty minutes with the version stuck mid-stage.
//
// A 500 is therefore not taken at its word: the release is read back, and one
// that is there means the write it was refused for had already happened.
func TestEnsureRelease_ExistingReleaseSurvivesConflictAs500(t *testing.T) {
	var posts, gets int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			posts++
			writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		case http.MethodGet:
			gets++
			writeJSON(t, w, http.StatusOK, map[string]any{
				"metadata": map[string]any{"name": chainTestRelease},
			})
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	got, err := c.EnsureRelease(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestRelease)
	if err != nil {
		t.Fatalf("EnsureRelease with the release already cut: %v", err)
	}
	if got != chainTestRelease {
		t.Errorf("got release %q, want the caller-supplied name", got)
	}
	if gets == 0 {
		t.Error("the release was never read back; a 500 was taken as the final answer")
	}
	if posts == 0 {
		t.Error("no write was attempted; the read must be the fallback, not the pre-flight")
	}
}

// The other half of the same rule: a 500 with NO release behind it is a genuine
// failure and must stay one, or a deploy that never cut anything would report
// success and the run would validate against nothing.
func TestEnsureRelease_ServerErrorWithNoReleaseStillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if _, err := c.EnsureRelease(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestRelease); err == nil {
		t.Fatal("a 500 with no release behind it was reported as success")
	}
}

func TestEnsureRelease_ServerErrorWrapsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	_, err := c.EnsureRelease(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestRelease)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInternalServerError) {
		t.Errorf("expected ErrInternalServerError, got %v", err)
	}
}

// ---- EnsureReleaseBinding ---------------------------------------------------

func TestEnsureReleaseBinding_Create(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	scoped := chainScopedName()
	bindingName := scoped + "-" + chainTestEnv
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"metadata": map[string]any{"name": bindingName},
		})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.EnsureReleaseBinding(context.Background(), chainTestOrg, chainTestProject,
		chainTestComp, chainTestEnv, chainTestRelease); err != nil {
		t.Fatalf("EnsureReleaseBinding: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/namespaces/wc-abc/releasebindings" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	meta, _ := gotBody["metadata"].(map[string]any)
	if meta["name"] != bindingName {
		t.Errorf("unexpected binding name: %v", meta["name"])
	}
	spec, _ := gotBody["spec"].(map[string]any)
	if spec["environment"] != chainTestEnv {
		t.Errorf("unexpected environment: %v", spec["environment"])
	}
	// MARKED internal, or the project-status poll folds this agent Job's binding
	// into the project's deploy status and reports it live.
	labels, _ := meta["labels"].(map[string]any)
	if labels[string(LabelKeyAepInternal)] != LabelValueAepInternal {
		t.Errorf("binding not marked internal: labels=%v", labels)
	}
}

func TestEnsureReleaseBinding_ConflictIsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.EnsureReleaseBinding(context.Background(), chainTestOrg, chainTestProject,
		chainTestComp, chainTestEnv, chainTestRelease); err != nil {
		t.Fatalf("EnsureReleaseBinding on 409: %v", err)
	}
}

// ---- ApplyReleaseBinding ----------------------------------------------------

// sampleDesiredBinding is a user component's full desired binding: the pin plus
// every field the deploy stage owns.
func sampleDesiredBinding() ReleaseBindingDesired {
	return ReleaseBindingDesired{
		ComponentName: chainTestComp,
		Environment:   chainTestEnv,
		ReleaseName:   chainTestRelease,
		State:         ReleaseBindingStateActive,
		TraitEnvironmentConfigs: map[string]map[string]interface{}{
			"widgets-http": {"jwtAuth": map[string]interface{}{"enabled": true}},
		},
		Env:   []WorkflowEnvVarRef{{Key: "API_URL", Value: "http://x"}},
		Files: []WorkflowFileVar{{Key: "env-config.js", MountPath: "/usr/share/nginx/html", Value: "window.x=1"}},
	}
}

// The create path must carry the WHOLE desired state in the POST body. That is
// the invariant the design rests on: a binding is never briefly renderable with
// a trait attached whose per-environment config has not landed yet.
func TestApplyReleaseBinding_CreateCarriesWholeDesiredState(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		writeJSON(t, w, http.StatusCreated, map[string]any{"metadata": map[string]any{"name": "x"}})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.ApplyReleaseBinding(context.Background(), chainTestOrg, chainTestProject, sampleDesiredBinding()); err != nil {
		t.Fatalf("ApplyReleaseBinding: %v", err)
	}
	spec, _ := gotBody["spec"].(map[string]any)
	if spec["releaseName"] != chainTestRelease {
		t.Errorf("releaseName = %v, want the pin in the create body", spec["releaseName"])
	}
	if spec["state"] != ReleaseBindingStateActive {
		t.Errorf("state = %v, want Active", spec["state"])
	}
	if _, ok := spec["traitEnvironmentConfigs"].(map[string]any); !ok {
		t.Errorf("traitEnvironmentConfigs missing from the create body: %v", spec)
	}
	overrides, ok := spec["workloadOverrides"].(map[string]any)
	if !ok {
		t.Fatalf("workloadOverrides missing from the create body: %v", spec)
	}
	container, _ := overrides["container"].(map[string]any)
	if container["env"] == nil || container["files"] == nil {
		t.Errorf("env/files missing from the create body: %v", container)
	}
}

// A binding that already exists is CONVERGED, not left alone — re-pinning it is
// what a redeploy is. This is the behaviour that separates Apply from Ensure.
func TestApplyReleaseBinding_ConflictConvergesViaPut(t *testing.T) {
	var methods []string
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			writeJSON(t, w, http.StatusOK, map[string]any{
				"metadata": map[string]any{"name": ReleaseBindingName(chainTestProject, chainTestComp, chainTestEnv)},
				"spec": map[string]any{
					"environment": chainTestEnv,
					"owner":       map[string]any{"componentName": chainScopedName(), "projectName": chainTestProject},
					"releaseName": "an-older-release",
					// A field this caller does not own — it must survive the write.
					"componentTypeEnvironmentConfigs": map[string]any{"keep": "me"},
				},
			})
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			writeJSON(t, w, http.StatusOK, map[string]any{"metadata": map[string]any{"name": "x"}})
		}
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	if err := c.ApplyReleaseBinding(context.Background(), chainTestOrg, chainTestProject, sampleDesiredBinding()); err != nil {
		t.Fatalf("ApplyReleaseBinding: %v", err)
	}
	if len(methods) != 3 || methods[0] != http.MethodPost || methods[1] != http.MethodGet || methods[2] != http.MethodPut {
		t.Fatalf("expected POST→GET→PUT, got %v", methods)
	}
	spec, _ := putBody["spec"].(map[string]any)
	if spec["releaseName"] != chainTestRelease {
		t.Errorf("releaseName = %v, want the new pin", spec["releaseName"])
	}
	if _, ok := spec["componentTypeEnvironmentConfigs"]; !ok {
		t.Error("the update dropped a field this caller does not own")
	}
}

// A caller that manages only the pin (the ephemeral coding-agent path) must not
// erase the fields it left nil.
func TestApplyReleaseBinding_NilFieldsAreUnmanaged(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			writeJSON(t, w, http.StatusOK, map[string]any{
				"metadata": map[string]any{"name": ReleaseBindingName(chainTestProject, chainTestComp, chainTestEnv)},
				"spec": map[string]any{
					"environment":             chainTestEnv,
					"owner":                   map[string]any{"componentName": chainScopedName(), "projectName": chainTestProject},
					"traitEnvironmentConfigs": map[string]any{"someone-elses": map[string]any{"a": 1}},
				},
			})
		case http.MethodPut:
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			writeJSON(t, w, http.StatusOK, map[string]any{"metadata": map[string]any{"name": "x"}})
		}
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	err := c.ApplyReleaseBinding(context.Background(), chainTestOrg, chainTestProject, ReleaseBindingDesired{
		ComponentName: chainTestComp,
		Environment:   chainTestEnv,
		ReleaseName:   chainTestRelease,
	})
	if err != nil {
		t.Fatalf("ApplyReleaseBinding: %v", err)
	}
	spec, _ := putBody["spec"].(map[string]any)
	configs, ok := spec["traitEnvironmentConfigs"].(map[string]any)
	if !ok || configs["someone-elses"] == nil {
		t.Errorf("a nil TraitEnvironmentConfigs erased the existing map: %v", spec)
	}
}

// ---- GetReleaseBindingStatus ------------------------------------------------

func TestGetReleaseBindingStatus_ReadsReadyCondition(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"metadata": map[string]any{"name": ReleaseBindingName(chainTestProject, chainTestComp, chainTestEnv)},
			"spec": map[string]any{
				"environment": chainTestEnv,
				"owner":       map[string]any{"componentName": chainScopedName(), "projectName": chainTestProject},
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{"type": "Ready", "status": "False", "reason": "RenderFailed"},
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	got, err := c.GetReleaseBindingStatus(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestEnv)
	if err != nil {
		t.Fatalf("GetReleaseBindingStatus: %v", err)
	}
	if got == nil || got.ReadyStatus != "False" || got.ReadyReason != "RenderFailed" {
		t.Errorf("unexpected summary: %+v", got)
	}
}

// A binding that has not been admitted yet is "not ready", not an error — the
// poll has to be able to keep waiting on it.
func TestGetReleaseBindingStatus_NotFoundIsNilNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	got, err := c.GetReleaseBindingStatus(context.Background(), chainTestOrg, chainTestProject, chainTestComp, chainTestEnv)
	if err != nil {
		t.Fatalf("expected no error for an absent binding, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil summary, got %+v", got)
	}
}

func TestEnsureReleaseBinding_ServerErrorWrapsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestComponentClient(t, srv)
	err := c.EnsureReleaseBinding(context.Background(), chainTestOrg, chainTestProject,
		chainTestComp, chainTestEnv, chainTestRelease)
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInternalServerError) {
		t.Errorf("expected ErrInternalServerError, got %v", err)
	}
}

// ---- SuspendJobBinding ------------------------------------------------------

// ocStub is a canned OpenChoreo API: one response per method+path, and a log of
// every request it saw (with the decoded body) so a test can assert on what was
// — and was not — written.
type ocStub struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	routes map[string]func(w http.ResponseWriter, body []byte)
	seen   []ocStubRequest
}

type ocStubRequest struct {
	method, path string
	body         map[string]any
}

func newOCStub(t *testing.T) *ocStub {
	t.Helper()
	s := &ocStub{t: t, routes: map[string]func(http.ResponseWriter, []byte){}}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		s.mu.Lock()
		s.seen = append(s.seen, ocStubRequest{method: r.Method, path: r.URL.Path, body: body})
		route := s.routes[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if route == nil {
			writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "no stub route"})
			return
		}
		route(w, raw)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// on answers method+path with status and a JSON body; nil sends a bare
// status with no body and no JSON content type.
func (s *ocStub) on(method, path string, status int, body any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[method+" "+path] = func(w http.ResponseWriter, _ []byte) {
		if body == nil {
			w.WriteHeader(status)
			return
		}
		writeJSON(s.t, w, status, body)
	}
}

func (s *ocStub) onJSON(method, path string, status int, body map[string]any) {
	s.on(method, path, status, body)
}

// onEcho answers method+path with the request's own body.
func (s *ocStub) onEcho(method, path string, status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[method+" "+path] = func(w http.ResponseWriter, raw []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(raw)
	}
}

func (s *ocStub) count(method, path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.seen {
		if r.method == method && r.path == path {
			n++
		}
	}
	return n
}

func (s *ocStub) countMethod(method string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.seen {
		if r.method == method {
			n++
		}
	}
	return n
}

// lastBody is the decoded body of the last request with that method.
func (s *ocStub) lastBody(method string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.seen) - 1; i >= 0; i-- {
		if s.seen[i].method == method {
			return s.seen[i].body
		}
	}
	s.t.Fatalf("no %s request was made", method)
	return nil
}

func (s *ocStub) componentClient() ComponentClient {
	return NewComponentClient(Config{BaseURL: s.srv.URL})
}

const (
	suspendTestBindingPath = "/api/v1/namespaces/acme/releasebindings/shop-ca-c1-development"
	suspendTestRelease     = "shop-ca-c1-release"
	suspendTestReleasePath = "/api/v1/namespaces/acme/componentreleases/" + suspendTestRelease
)

// codingAgentReleaseFixture is a ComponentRelease as openchoreo-api's GET
// returns it: the CR converted to the API type, so spec.componentType is the
// frozen snapshot {kind, name, spec} and spec.componentType.spec is the
// ComponentType spec at release time. The snapshot is today's coding-agent
// spec; withSuspend=false removes its environmentConfigs, which is what a
// release cut before the suspend schema carries.
func codingAgentReleaseFixture(t *testing.T, withSuspend bool) map[string]any {
	t.Helper()
	raw, err := json.Marshal(CodingAgentComponentType()["spec"])
	if err != nil {
		t.Fatal(err)
	}
	var ctSpec map[string]any
	if err := json.Unmarshal(raw, &ctSpec); err != nil {
		t.Fatal(err)
	}
	if !withSuspend {
		delete(ctSpec, "environmentConfigs")
	}
	return map[string]any{
		"apiVersion": "openchoreo.dev/v1alpha1",
		"kind":       "ComponentRelease",
		"metadata":   map[string]any{"name": suspendTestRelease, "namespace": "acme"},
		"spec": map[string]any{
			"owner": map[string]any{"componentName": "shop-ca-c1", "projectName": "shop"},
			"componentType": map[string]any{
				"kind": "ComponentType",
				"name": CodingAgentComponentTypeRef,
				"spec": ctSpec,
			},
			"componentProfile": map[string]any{"parameters": map[string]any{"runtime": "claude-code"}},
			"workload":         map[string]any{"container": map[string]any{"image": "img"}},
		},
	}
}

// suspendTestBinding is a coding-agent binding pinned to suspendTestRelease.
func suspendTestBinding(configs map[string]any) map[string]any {
	spec := map[string]any{
		"environment": "development",
		"releaseName": suspendTestRelease,
		"owner":       map[string]any{"componentName": "shop-ca-c1", "projectName": "shop"},
	}
	if configs != nil {
		spec["componentTypeEnvironmentConfigs"] = configs
	}
	return map[string]any{"metadata": map[string]any{"name": "shop-ca-c1-development"}, "spec": spec}
}

// A suspend on a Component that is already gone must never resurrect it:
// ApplyReleaseBinding would POST a missing binding back (and so a Job).
func TestSuspendJobBinding_MissingBindingIsNotFoundAndCreatesNothing(t *testing.T) {
	srv := newOCStub(t)
	srv.on("GET", suspendTestBindingPath, 404, nil)
	c := srv.componentClient()
	err := c.SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if srv.count("POST", "/api/v1/namespaces/acme/releasebindings") != 0 || srv.countMethod("PUT") != 0 {
		t.Fatal("suspend must never create or write a binding that is not there")
	}

	// OpenChoreo's own 404 carries a JSON error body; same answer.
	srv.on("GET", suspendTestBindingPath, 404, map[string]string{"error": "release binding not found"})
	if err := c.SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if srv.countMethod("POST") != 0 || srv.countMethod("PUT") != 0 {
		t.Fatal("suspend must never create or write a binding that is not there")
	}
}

func TestSuspendJobBinding_KeepsEveryOtherFieldAndIsIdempotent(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, map[string]any{
		"metadata": map[string]any{"name": "shop-ca-c1-development", "resourceVersion": "42"},
		"spec": map[string]any{"releaseName": "shop-ca-c1-release", "environment": "development",
			"owner":                           map[string]any{"componentName": "shop-ca-c1", "projectName": "shop"},
			"traitEnvironmentConfigs":         map[string]any{"t": map[string]any{"a": 1.0}},
			"componentTypeEnvironmentConfigs": map[string]any{"other": "x"},
			// A field this client's generated schema does not know: it must
			// survive too, so the write is a raw read-modify-write.
			"futureField": "kept"},
	})
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, true))
	srv.onEcho("PUT", suspendTestBindingPath, 200)
	c := srv.componentClient()
	if err := c.SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development"); err != nil {
		t.Fatal(err)
	}
	put := srv.lastBody("PUT")
	spec := put["spec"].(map[string]any)
	if spec["releaseName"] != "shop-ca-c1-release" {
		t.Fatal("the pin must survive the PUT")
	}
	ctec := spec["componentTypeEnvironmentConfigs"].(map[string]any)
	if ctec["suspend"] != true || ctec["other"] != "x" {
		t.Fatalf("componentTypeEnvironmentConfigs = %v", ctec)
	}
	for _, key := range []string{"environment", "owner", "traitEnvironmentConfigs", "futureField"} {
		if _, ok := spec[key]; !ok {
			t.Errorf("spec.%s dropped by the PUT: %v", key, spec)
		}
	}
	meta := put["metadata"].(map[string]any)
	if meta["resourceVersion"] != "42" {
		t.Errorf("metadata.resourceVersion = %v, want the read's", meta["resourceVersion"])
	}
	if srv.countMethod("POST") != 0 {
		t.Error("suspend must never POST")
	}

	// Already suspended: a read, no write.
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(map[string]any{"suspend": true}))
	if err := c.SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development"); err != nil {
		t.Fatal(err)
	}
	if got := srv.countMethod("PUT"); got != 1 {
		t.Fatalf("PUTs = %d, want 1: an already-suspended binding is not rewritten", got)
	}
}

// A binding with no componentTypeEnvironmentConfigs at all gets the map.
func TestSuspendJobBinding_CreatesTheConfigMapWhenAbsent(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(nil))
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, true))
	srv.onEcho("PUT", suspendTestBindingPath, 200)
	if err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development"); err != nil {
		t.Fatal(err)
	}
	ctec, _ := srv.lastBody("PUT")["spec"].(map[string]any)["componentTypeEnvironmentConfigs"].(map[string]any)
	if ctec["suspend"] != true {
		t.Fatalf("componentTypeEnvironmentConfigs = %v", ctec)
	}
}

// A 400 is a malformed request, not a legacy release (OpenChoreo accepts the
// key on any binding; see the ErrSuspendUnsupported tests). It wraps
// ErrBadRequest and is not retried.
func TestSuspendJobBinding_BadRequestWrapsErrBadRequest(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(nil))
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, true))
	srv.on("PUT", suspendTestBindingPath, 400, map[string]string{"error": "malformed body"})
	err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if got := srv.countMethod("PUT"); got != 1 {
		t.Fatalf("PUTs = %d, want 1: a 400 is not a stale write to retry", got)
	}
}

// OpenChoreo v1.2.5 answers 200 to suspend on a binding whose release predates
// the schema and then drops the key at render (the release's frozen
// ComponentType snapshot has no environmentConfigs), so the Job stays live.
// The client therefore reads the bound release itself and refuses to write.
func TestSuspendJobBinding_ReleaseWithoutTheSchemaIsUnsupportedAndWritesNothing(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(map[string]any{"other": "x"}))
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, false))
	srv.onEcho("PUT", suspendTestBindingPath, 200)
	err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
	if !errors.Is(err, ErrSuspendUnsupported) {
		t.Fatalf("err = %v, want ErrSuspendUnsupported", err)
	}
	if errors.Is(err, ErrBadRequest) || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v must not read as a request error or a missing binding", err)
	}
	if got := srv.countMethod("PUT"); got != 0 {
		t.Fatalf("PUTs = %d, want 0: a write the render ignores must not be made", got)
	}
	if got := srv.count("GET", suspendTestReleasePath); got != 1 {
		t.Fatalf("release GETs = %d, want 1", got)
	}
}

// A binding that already reads suspend=true over a legacy release is still
// unsupported: the value is there but nothing renders it.
func TestSuspendJobBinding_AlreadyTrueOverALegacyReleaseIsUnsupported(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(map[string]any{"suspend": true}))
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, false))
	err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
	if !errors.Is(err, ErrSuspendUnsupported) {
		t.Fatalf("err = %v, want ErrSuspendUnsupported", err)
	}
	if got := srv.countMethod("PUT"); got != 0 {
		t.Fatalf("PUTs = %d, want 0", got)
	}
}

// The release read failing is an error of its own: no write, and not taken for
// "unsupported" (which would let a caller settle a Job that may still run).
func TestSuspendJobBinding_ReleaseReadFailurePropagatesAndWritesNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"forbidden", http.StatusForbidden, ErrForbidden},
		{"missing release", http.StatusNotFound, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOCStub(t)
			srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(nil))
			srv.on("GET", suspendTestReleasePath, tc.status, map[string]string{"error": "nope"})
			srv.onEcho("PUT", suspendTestBindingPath, 200)
			err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
			if err == nil {
				t.Fatal("want an error")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if errors.Is(err, ErrSuspendUnsupported) {
				t.Fatalf("err = %v: a failed read is not unsupported", err)
			}
			// A missing RELEASE under a present binding is not "the binding is
			// gone": callers read ErrNotFound as the Component being deleted.
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v must not read as a missing binding", err)
			}
			if got := srv.countMethod("PUT"); got != 0 {
				t.Fatalf("PUTs = %d, want 0", got)
			}
		})
	}
}

// OC reports a lost race with its own controllers as a 500; the write re-reads
// and retries (retryStaleWrite).
func TestSuspendJobBinding_StaleWriteRereadsAndRetries(t *testing.T) {
	srv := newOCStub(t)
	srv.onJSON("GET", suspendTestBindingPath, 200, suspendTestBinding(nil))
	srv.onJSON("GET", suspendTestReleasePath, 200, codingAgentReleaseFixture(t, true))
	var puts int
	srv.mu.Lock()
	srv.routes["PUT "+suspendTestBindingPath] = func(w http.ResponseWriter, raw []byte) {
		puts++
		if puts == 1 {
			writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "conflict"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(raw)
	}
	srv.mu.Unlock()
	if err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development"); err != nil {
		t.Fatal(err)
	}
	if got := srv.count("GET", suspendTestBindingPath); got != 2 {
		t.Fatalf("GETs = %d, want 2 (one re-read per attempt)", got)
	}
}

// A binding that names no release renders no Job, so there is nothing a
// suspend could act on; it is unsupported and nothing is written.
func TestSuspendJobBinding_BindingWithoutAReleaseIsUnsupported(t *testing.T) {
	srv := newOCStub(t)
	b := suspendTestBinding(nil)
	delete(b["spec"].(map[string]any), "releaseName")
	srv.onJSON("GET", suspendTestBindingPath, 200, b)
	srv.onEcho("PUT", suspendTestBindingPath, 200)
	err := srv.componentClient().SuspendJobBinding(context.Background(), "acme", "shop", "ca-c1", "development")
	if !errors.Is(err, ErrSuspendUnsupported) {
		t.Fatalf("err = %v, want ErrSuspendUnsupported", err)
	}
	if srv.countMethod("PUT") != 0 || srv.count("GET", suspendTestReleasePath) != 0 {
		t.Fatal("no release read and no write for a binding without a release")
	}
}
