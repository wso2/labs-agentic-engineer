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
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	chainTestOrg     = "wc-abc"
	chainTestProject = "widgets"
	chainTestComp    = "ca-run"
	chainTestImage   = "ghcr.io/wso2/aep/remote-worker:latest"
	chainTestRelease = "widgets-ca-run-release"
	chainTestEnv     = DevEnvironmentName
)

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
