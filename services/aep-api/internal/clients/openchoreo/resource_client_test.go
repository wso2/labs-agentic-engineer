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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestResourceClient builds a resourceClient pointed at srv with no auth
// configured (Config zero value beyond BaseURL) — the fake OC server below
// doesn't assert on auth headers, only on method/path/body.
func newTestResourceClient(t *testing.T, srv *httptest.Server) ResourceClient {
	t.Helper()
	return NewResourceClient(Config{BaseURL: srv.URL})
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode fake response: %v", err)
	}
}

// ---- EnsureResourceType -----------------------------------------------------

func TestEnsureResourceType_Create(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		var rt ResourceType
		_ = json.NewDecoder(r.Body).Decode(&rt)
		if rt.APIVersion != ocResourceAPIVersion || rt.Kind != kindResourceType {
			t.Errorf("unexpected apiVersion/kind: %s/%s", rt.APIVersion, rt.Kind)
		}
		rt.Metadata.Name = "conn-postgres-v1"
		writeJSON(t, w, http.StatusCreated, rt)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &ResourceType{Metadata: OCObjectMeta{Name: "conn-postgres-v1"}}
	got, err := c.EnsureResourceType(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("EnsureResourceType: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/namespaces/wc-abc/resourcetypes" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if got.Metadata.Name != "conn-postgres-v1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestEnsureResourceType_ConflictRefetchesExisting(t *testing.T) {
	var posts, gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			atomic.AddInt32(&posts, 1)
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			atomic.AddInt32(&gets, 1)
			writeJSON(t, w, http.StatusOK, ResourceType{Metadata: OCObjectMeta{Name: "conn-postgres-v1"}})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &ResourceType{Metadata: OCObjectMeta{Name: "conn-postgres-v1"}}
	got, err := c.EnsureResourceType(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("EnsureResourceType: %v", err)
	}
	if got.Metadata.Name != "conn-postgres-v1" {
		t.Errorf("unexpected result: %+v", got)
	}
	if posts != 1 || gets != 1 {
		t.Errorf("expected 1 POST + 1 GET, got posts=%d gets=%d", posts, gets)
	}
}

func TestEnsureResourceType_ServerErrorWrapsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	_, err := c.EnsureResourceType(context.Background(), "wc-abc", &ResourceType{Metadata: OCObjectMeta{Name: "x"}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrInternalServerError) {
		t.Errorf("expected ErrInternalServerError, got %v", err)
	}
}

// ---- ApplyResource -----------------------------------------------------------

func TestApplyResource_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/resources" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var res Resource
		_ = json.NewDecoder(r.Body).Decode(&res)
		writeJSON(t, w, http.StatusCreated, res)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &Resource{Metadata: OCObjectMeta{Name: "proj-conn1"}}
	got, err := c.ApplyResource(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("ApplyResource: %v", err)
	}
	if got.Metadata.Name != "proj-conn1" || got.APIVersion != ocResourceAPIVersion || got.Kind != kindResource {
		t.Errorf("unexpected result: %+v", got)
	}
}

// TestApplyResource_ConflictReconcilesViaPUT proves the 409 path GET-checks the
// existing spec.type (matching here), then PUTs the DESIRED spec so an
// RT-version bump propagates — the PUT body must carry the NEW RT name, not the
// stale one. The pre-reconcile release rides back on status.latestRelease so the
// caller can wait-for-change.
func TestApplyResource_ConflictReconcilesViaPUT(t *testing.T) {
	var putBody Resource
	var puts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			// Existing resource: SAME kind, OLD RT name, a stale release cut.
			writeJSON(t, w, http.StatusOK, Resource{
				Metadata: OCObjectMeta{Name: "proj-conn1"},
				Spec:     ResourceSpec{Type: ResourceTypeRef{Kind: "ResourceType", Name: "conn-postgres-v1"}},
				Status:   &ResourceStatus{LatestRelease: &ResourceLatestRelease{Name: "proj-conn1-rel1"}},
			})
		case http.MethodPut:
			atomic.AddInt32(&puts, 1)
			if r.URL.Path != "/api/v1/namespaces/wc-abc/resources/proj-conn1" {
				t.Fatalf("unexpected PUT path: %s", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			// The server keeps the (still stale) status on a spec PUT.
			putBody.Status = &ResourceStatus{LatestRelease: &ResourceLatestRelease{Name: "proj-conn1-rel1"}}
			writeJSON(t, w, http.StatusOK, putBody)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	// Desired: SAME kind, NEW RT name (a version bump).
	desired := &Resource{
		Metadata: OCObjectMeta{Name: "proj-conn1"},
		Spec:     ResourceSpec{Type: ResourceTypeRef{Kind: "ResourceType", Name: "conn-postgres-v2"}},
	}
	got, err := c.ApplyResource(context.Background(), "wc-abc", desired)
	if err != nil {
		t.Fatalf("ApplyResource: %v", err)
	}
	if puts != 1 {
		t.Fatalf("expected exactly 1 PUT reconcile, got %d", puts)
	}
	if putBody.Spec.Type.Name != "conn-postgres-v2" {
		t.Errorf("PUT body must carry the NEW RT name, got %q", putBody.Spec.Type.Name)
	}
	// The pre-reconcile release rides back so the caller waits for a NEW one.
	if ReleaseName(got) != "proj-conn1-rel1" {
		t.Errorf("expected pre-reconcile release to ride back, got %q", ReleaseName(got))
	}
}

// TestApplyResource_ConflictMismatchedTypeErrors proves the cross-kind
// collision guard: a 409 whose existing spec.type differs from the desired one
// is a HARD error naming both types — never a silent overwrite (no PUT issued).
func TestApplyResource_ConflictMismatchedTypeErrors(t *testing.T) {
	var puts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			// Existing is a ClusterResourceType (platform-resource) named "postgres-cnpg".
			writeJSON(t, w, http.StatusOK, Resource{
				Metadata: OCObjectMeta{Name: "proj-conn1"},
				Spec:     ResourceSpec{Type: ResourceTypeRef{Kind: "ClusterResourceType", Name: "postgres-cnpg"}},
			})
		case http.MethodPut:
			atomic.AddInt32(&puts, 1)
			writeJSON(t, w, http.StatusOK, nil)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	// Desired is an external ResourceType named "conn-postgres-v1" — a different kind.
	desired := &Resource{
		Metadata: OCObjectMeta{Name: "proj-conn1"},
		Spec:     ResourceSpec{Type: ResourceTypeRef{Kind: "ResourceType", Name: "conn-postgres-v1"}},
	}
	_, err := c.ApplyResource(context.Background(), "wc-abc", desired)
	if err == nil {
		t.Fatal("expected a hard error on mismatched spec.type")
	}
	if puts != 0 {
		t.Errorf("must NOT PUT on a type mismatch, got %d PUTs", puts)
	}
	for _, want := range []string{"ClusterResourceType/postgres-cnpg", "ResourceType/conn-postgres-v1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must name both types, missing %q: %v", want, err)
		}
	}
}

// TestApplyResource_ConflictUnchangedSpecSkipsPUT proves the no-op reconcile
// path: a 409 whose existing spec is IDENTICAL to the desired one issues no PUT
// (nothing to propagate) and reports create-equivalent semantics — an empty
// ReleaseName — so callers wait-for-nonempty and pin the EXISTING release on
// their first poll instead of waiting for a release change the controller will
// never cut. Regression: an idempotent re-save of unchanged external-resource
// values hung the /values endpoint for the full poll timeout (caught live in
// E2E scenario S1).
func TestApplyResource_ConflictUnchangedSpecSkipsPUT(t *testing.T) {
	var puts int32
	existingSpec := ResourceSpec{
		Type: ResourceTypeRef{Kind: "ResourceType", Name: "conn-postgres-v2"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "already exists"})
		case http.MethodGet:
			// Existing resource: spec identical to desired, release already cut.
			writeJSON(t, w, http.StatusOK, Resource{
				Metadata: OCObjectMeta{Name: "proj-conn1"},
				Spec:     existingSpec,
				Status:   &ResourceStatus{LatestRelease: &ResourceLatestRelease{Name: "proj-conn1-rel1"}},
			})
		case http.MethodPut:
			atomic.AddInt32(&puts, 1)
			writeJSON(t, w, http.StatusOK, nil)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	desired := &Resource{
		Metadata: OCObjectMeta{Name: "proj-conn1"},
		Spec:     existingSpec,
	}
	got, err := c.ApplyResource(context.Background(), "wc-abc", desired)
	if err != nil {
		t.Fatalf("ApplyResource: %v", err)
	}
	if puts != 0 {
		t.Errorf("must NOT PUT when the spec is unchanged, got %d PUTs", puts)
	}
	// Create-equivalent: empty release → callers wait-for-nonempty, which their
	// first GetResource poll satisfies with the existing (still-correct) release.
	if ReleaseName(got) != "" {
		t.Errorf("no-op reconcile must report an empty ReleaseName (create-equivalent), got %q", ReleaseName(got))
	}
}

// ---- GetResource --------------------------------------------------------------

func TestGetResource_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/resources/proj-conn1" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, Resource{
			Metadata: OCObjectMeta{Name: "proj-conn1"},
			Status:   &ResourceStatus{LatestRelease: &ResourceLatestRelease{Name: "rel-1"}},
		})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.GetResource(context.Background(), "wc-abc", "proj-conn1")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if got.Status.LatestRelease.Name != "rel-1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestGetResource_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	_, err := c.GetResource(context.Background(), "wc-abc", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetResource_DecodesConditions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"metadata": map[string]any{"name": "r1"},
			"status": map[string]any{
				"conditions": []map[string]any{{
					"type": "Ready", "status": "False",
					"reason":  "ResourceTypeNotFound",
					"message": `ClusterResourceType "object-storage" not found`,
				}},
			},
		})
	}))
	defer srv.Close()
	c := newTestResourceClient(t, srv)
	got, err := c.GetResource(context.Background(), "ns", "r1")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if got.Status == nil || len(got.Status.Conditions) != 1 || got.Status.Conditions[0].Reason != "ResourceTypeNotFound" {
		t.Fatalf("conditions not decoded: %+v", got.Status)
	}
}

// ---- WaitForReleaseChange -----------------------------------------------------

func TestWaitForReleaseChange_ReadyFalseResourceTypeNotFoundIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, Resource{
			Metadata: OCObjectMeta{Name: "order-processing-dca538e7"},
			Status: &ResourceStatus{
				Conditions: []OCCondition{{
					Type:    "Ready",
					Status:  "False",
					Reason:  "ResourceTypeNotFound",
					Message: `ClusterResourceType "object-storage" not found`,
				}},
			},
		})
	}))
	defer srv.Close()
	c := newTestResourceClient(t, srv)

	_, err := WaitForReleaseChange(context.Background(), c, "default", "order-processing-dca538e7", "", time.Hour, time.Hour)
	if err == nil {
		t.Fatal("want permanent error, got nil")
	}
	if !errors.Is(err, ErrResourceTypeNotFound) {
		t.Fatalf("want ErrResourceTypeNotFound wait-answer sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), "object-storage") && !strings.Contains(err.Error(), "ResourceTypeNotFound") {
		t.Fatalf("error must quote the terminal condition, got %v", err)
	}
}

func TestWaitForReleaseChange_ReadyFalseOtherReasonStillWaitsUntilTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, Resource{
			Metadata: OCObjectMeta{Name: "r1"},
			Status: &ResourceStatus{
				Conditions: []OCCondition{{
					Type:   "Ready",
					Status: "False",
					Reason: "Reconciling",
				}},
			},
		})
	}))
	defer srv.Close()
	c := newTestResourceClient(t, srv)

	start := time.Now()
	_, err := WaitForReleaseChange(context.Background(), c, "ns", "r1", "", 5*time.Millisecond, 20*time.Millisecond)
	if err == nil {
		t.Fatal("want timeout, got nil")
	}
	if !errors.Is(err, ErrReleaseWaitTimeout) {
		t.Fatalf("deadline expiry must wrap ErrReleaseWaitTimeout, got %v", err)
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatalf("non-terminal Ready=False must not return immediately: %v after %s", err, time.Since(start))
	}
}

// ---- EnsureBinding -------------------------------------------------------------

func TestEnsureBinding_Create(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/resourcereleasebindings" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var b ResourceReleaseBinding
		_ = json.NewDecoder(r.Body).Decode(&b)
		writeJSON(t, w, http.StatusCreated, b)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &ResourceReleaseBinding{
		Metadata: OCObjectMeta{Name: "proj-conn1-dev"},
		Spec:     ResourceReleaseBindingSpec{Environment: "dev", ResourceRelease: "rel-1"},
	}
	got, err := c.EnsureBinding(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	if got.Metadata.Name != "proj-conn1-dev" || got.APIVersion != ocResourceAPIVersion || got.Kind != kindResourceReleaseBind {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestEnsureBinding_ConflictPutsReconciledPin(t *testing.T) {
	var lastPutBody ResourceReleaseBinding
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			writeJSON(t, w, http.StatusConflict, map[string]string{"error": "exists"})
		case http.MethodPut:
			if r.URL.Path != "/api/v1/namespaces/wc-abc/resourcereleasebindings/proj-conn1-dev" {
				t.Fatalf("unexpected PUT path: %s", r.URL.Path)
			}
			_ = json.NewDecoder(r.Body).Decode(&lastPutBody)
			writeJSON(t, w, http.StatusOK, lastPutBody)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &ResourceReleaseBinding{
		Metadata: OCObjectMeta{Name: "proj-conn1-dev"},
		Spec:     ResourceReleaseBindingSpec{Environment: "dev", ResourceRelease: "rel-2"},
	}
	got, err := c.EnsureBinding(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("EnsureBinding: %v", err)
	}
	if got.Spec.ResourceRelease != "rel-2" {
		t.Errorf("expected PUT to reconcile the pin to rel-2, got %+v", got.Spec)
	}
}

// ---- GetBinding -----------------------------------------------------------------

func TestGetBinding_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, ResourceReleaseBinding{
			Metadata: OCObjectMeta{Name: "proj-conn1-dev"},
			Status:   &ResourceReleaseBindingStatus{Conditions: []OCCondition{{Type: "Ready", Status: "True"}}},
		})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.GetBinding(context.Background(), "wc-abc", "proj-conn1-dev")
	if err != nil {
		t.Fatalf("GetBinding: %v", err)
	}
	if !got.IsReady() {
		t.Errorf("expected binding to be ready, got %+v", got.Status)
	}
}

func TestGetBinding_NotFoundReturnsNilNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.GetBinding(context.Background(), "wc-abc", "missing")
	if err != nil {
		t.Fatalf("expected nil error on 404, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil binding on 404, got %+v", got)
	}
}

func TestGetBinding_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	_, err := c.GetBinding(context.Background(), "wc-abc", "x")
	if !errors.Is(err, ErrInternalServerError) {
		t.Fatalf("expected ErrInternalServerError, got %v", err)
	}
}

// ---- PatchBindingEnvironmentConfigs --------------------------------------------

// decodeEnvConfigs decodes a binding's resourceTypeEnvironmentConfigs raw JSON
// into a string map for assertions.
func decodeEnvConfigs(t *testing.T, raw json.RawMessage) map[string]string {
	t.Helper()
	if len(raw) == 0 {
		return map[string]string{}
	}
	m := map[string]string{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode env configs: %v", err)
	}
	return m
}

// The patch reads the binding, overlays the given keys onto the existing
// env-config map (unrelated keys survive), and re-applies via EnsureBinding.
func TestPatchBindingEnvironmentConfigs_MergePreservesOtherKeys(t *testing.T) {
	const name = "proj-auth-development"
	var gotWriteBody ResourceReleaseBinding
	wrote := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces/wc-abc/resourcereleasebindings/"+name:
			existing := ResourceReleaseBinding{
				Metadata: OCObjectMeta{Name: name},
				Spec: ResourceReleaseBindingSpec{
					Environment:                    "development",
					ResourceRelease:                "rel-1",
					ResourceTypeEnvironmentConfigs: json.RawMessage(`{"keepMe":"stay","redirectUris":"http://old/callback"}`),
				},
				Status: &ResourceReleaseBindingStatus{Conditions: []OCCondition{{Type: "Ready", Status: "True"}}},
			}
			writeJSON(t, w, http.StatusOK, existing)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/namespaces/wc-abc/resourcereleasebindings":
			wrote = true
			_ = json.NewDecoder(r.Body).Decode(&gotWriteBody)
			writeJSON(t, w, http.StatusCreated, gotWriteBody)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	if err := c.PatchBindingEnvironmentConfigs(context.Background(), "wc-abc", name,
		map[string]string{"redirectUris": "http://new/callback"}); err != nil {
		t.Fatalf("PatchBindingEnvironmentConfigs: %v", err)
	}
	if !wrote {
		t.Fatalf("expected EnsureBinding to be invoked when a value changed")
	}
	got := decodeEnvConfigs(t, gotWriteBody.Spec.ResourceTypeEnvironmentConfigs)
	if got["redirectUris"] != "http://new/callback" {
		t.Errorf("redirectUris = %q; want the patched value", got["redirectUris"])
	}
	if got["keepMe"] != "stay" {
		t.Errorf("merge dropped an unrelated key: %v", got)
	}
	// The pin + environment carry through the re-apply untouched.
	if gotWriteBody.Spec.ResourceRelease != "rel-1" || gotWriteBody.Spec.Environment != "development" {
		t.Errorf("re-apply mutated spec pin/env: %+v", gotWriteBody.Spec)
	}
}

// An idempotent re-patch carrying identical values is a no-op: EnsureBinding is
// never called, so a per-cascade re-run does not churn the CR.
func TestPatchBindingEnvironmentConfigs_IdempotentNoOp(t *testing.T) {
	const name = "proj-auth-development"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("EnsureBinding must NOT run when values are unchanged; got %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, ResourceReleaseBinding{
			Metadata: OCObjectMeta{Name: name},
			Spec: ResourceReleaseBindingSpec{
				ResourceTypeEnvironmentConfigs: json.RawMessage(`{"redirectUris":"http://web/callback"}`),
			},
		})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	if err := c.PatchBindingEnvironmentConfigs(context.Background(), "wc-abc", name,
		map[string]string{"redirectUris": "http://web/callback"}); err != nil {
		t.Fatalf("PatchBindingEnvironmentConfigs: %v", err)
	}
}

// A missing binding (the provisioner authors it first) is a hard error so the
// caller defers.
func TestPatchBindingEnvironmentConfigs_BindingNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	err := c.PatchBindingEnvironmentConfigs(context.Background(), "wc-abc", "missing",
		map[string]string{"redirectUris": "http://web/callback"})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want a not-found error; got %v", err)
	}
}

// ---- DeleteBinding / DeleteResource (404-tolerant) -----------------------------

func TestDeleteBinding_SuccessAnd404Tolerant(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		status := status
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/namespaces/wc-abc/resourcereleasebindings/proj-conn1-dev" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				writeJSON(t, w, status, nil)
			}))
			defer srv.Close()

			c := newTestResourceClient(t, srv)
			if err := c.DeleteBinding(context.Background(), "wc-abc", "proj-conn1-dev"); err != nil {
				t.Fatalf("DeleteBinding: %v", err)
			}
		})
	}
}

func TestDeleteBinding_ServerErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "boom"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	err := c.DeleteBinding(context.Background(), "wc-abc", "x")
	if !errors.Is(err, ErrInternalServerError) {
		t.Fatalf("expected ErrInternalServerError, got %v", err)
	}
}

func TestDeleteResource_SuccessAnd404Tolerant(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound} {
		status := status
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/namespaces/wc-abc/resources/proj-conn1" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				writeJSON(t, w, status, nil)
			}))
			defer srv.Close()

			c := newTestResourceClient(t, srv)
			if err := c.DeleteResource(context.Background(), "wc-abc", "proj-conn1"); err != nil {
				t.Fatalf("DeleteResource: %v", err)
			}
		})
	}
}

// ---- ListClusterResourceTypes ----------------------------------------------------

func TestListClusterResourceTypes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clusterresourcetypes" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, resourceTypeList{Items: []ResourceType{
			{Metadata: OCObjectMeta{Name: "postgres-cluster-rt"}},
			{Metadata: OCObjectMeta{Name: "redis-cluster-rt"}},
		}})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListClusterResourceTypes(context.Background())
	if err != nil {
		t.Fatalf("ListClusterResourceTypes: %v", err)
	}
	if len(got) != 2 || got[0].Metadata.Name != "postgres-cluster-rt" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---- ListResourceTypes / GetResourceType ------------------------------------------

func TestListResourceTypes_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/resourcetypes" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, resourceTypeList{Items: []ResourceType{
			{Metadata: OCObjectMeta{Name: "postgres-rt"}},
			{Metadata: OCObjectMeta{Name: "redis-rt"}},
		}})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListResourceTypes(context.Background(), "wc-abc")
	if err != nil {
		t.Fatalf("ListResourceTypes: %v", err)
	}
	if len(got) != 2 || got[0].Metadata.Name != "postgres-rt" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestGetResourceType_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/resourcetypes/postgres-rt" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(t, w, http.StatusOK, ResourceType{Metadata: OCObjectMeta{Name: "postgres-rt"}})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.GetResourceType(context.Background(), "wc-abc", "postgres-rt")
	if err != nil {
		t.Fatalf("GetResourceType: %v", err)
	}
	if got.Metadata.Name != "postgres-rt" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestGetResourceType_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	_, err := c.GetResourceType(context.Background(), "wc-abc", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestUpdateResourceType_IssuesPUT(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		var rt ResourceType
		_ = json.NewDecoder(r.Body).Decode(&rt)
		if rt.APIVersion != ocResourceAPIVersion || rt.Kind != kindResourceType {
			t.Errorf("unexpected apiVersion/kind: %s/%s", rt.APIVersion, rt.Kind)
		}
		writeJSON(t, w, http.StatusOK, rt)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	in := &ResourceType{Metadata: OCObjectMeta{Name: "stripe-abc123defg-t2"}}
	got, err := c.UpdateResourceType(context.Background(), "wc-abc", in)
	if err != nil {
		t.Fatalf("UpdateResourceType: %v", err)
	}
	if gotMethod != http.MethodPut || gotPath != "/api/v1/namespaces/wc-abc/resourcetypes/stripe-abc123defg-t2" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if got.Metadata.Name != "stripe-abc123defg-t2" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ---- ListWorkloadEndpoints --------------------------------------------------------

func TestListWorkloadEndpoints_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/workloads" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{"items":[
			{
				"metadata":{"name":"wl-orders"},
				"spec":{
					"owner":{"projectName":"orders","componentName":"orders-api"},
					"endpoints":{
						"http":{"type":"HTTP","port":8080,"basePath":"/","visibility":["namespace"],"schema":{"type":"openapi","content":"openapi: 3.0.0"}},
						"admin":{"type":"HTTP","port":9090,"basePath":"/admin","visibility":[]}
					}
				}
			}
		]}`)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListWorkloadEndpoints(context.Background(), "wc-abc")
	if err != nil {
		t.Fatalf("ListWorkloadEndpoints: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 endpoints, got %d: %+v", len(got), got)
	}
	byName := map[string]WorkloadEndpointInfo{}
	for _, e := range got {
		byName[e.Name] = e
	}
	httpEP, ok := byName["http"]
	if !ok {
		t.Fatalf("missing http endpoint in %+v", got)
	}
	if httpEP.Project != "orders" || httpEP.Component != "orders-api" || httpEP.Port != 8080 || !httpEP.NamespaceVisible() {
		t.Errorf("unexpected http endpoint: %+v", httpEP)
	}
	if httpEP.SchemaType != "openapi" || httpEP.SchemaContent != "openapi: 3.0.0" {
		t.Errorf("expected schema decoded onto http endpoint, got SchemaType=%q SchemaContent=%q", httpEP.SchemaType, httpEP.SchemaContent)
	}
	adminEP, ok := byName["admin"]
	if !ok {
		t.Fatalf("missing admin endpoint in %+v", got)
	}
	if adminEP.NamespaceVisible() {
		t.Errorf("admin endpoint should not be namespace-visible: %+v", adminEP)
	}
	if adminEP.SchemaType != "" || adminEP.SchemaContent != "" {
		t.Errorf("expected admin endpoint (no schema in payload) to have empty schema fields, got SchemaType=%q SchemaContent=%q", adminEP.SchemaType, adminEP.SchemaContent)
	}
}

func TestListWorkloadEndpoints_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, workloadList{Items: nil})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListWorkloadEndpoints(context.Background(), "wc-abc")
	if err != nil {
		t.Fatalf("ListWorkloadEndpoints: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func TestListWorkloadConsumerDeps_FiltersProjectAndParsesRefs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces/wc-abc/workloads" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		fmt.Fprint(w, `{
			"items": [
				{
					"metadata": {"name": "shop-orders"},
					"spec": {
						"owner": {"projectName": "shop", "componentName": "orders"},
						"endpoints": {"http": {"type": "HTTP", "port": 8080}},
						"dependencies": {
							"resources": [{"ref": "shop-pg"}, {"ref": "shop-stripe"}],
							"endpoints": [
								{"project": "inventory", "component": "inventory-api", "name": "http", "visibility": "namespace"},
								{"component": "web", "name": "http", "visibility": "project"}
							]
						}
					}
				},
				{
					"metadata": {"name": "other-api"},
					"spec": {
						"owner": {"projectName": "other", "componentName": "api"},
						"dependencies": {"resources": [{"ref": "other-pg"}]}
					}
				}
			]
		}`)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListWorkloadConsumerDeps(context.Background(), "wc-abc", "shop")
	if err != nil {
		t.Fatalf("ListWorkloadConsumerDeps: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 shop workload, got %+v", got)
	}
	w := got[0]
	if w.OwnerProject != "shop" || w.OwnerComponent != "orders" {
		t.Errorf("owner = %s/%s", w.OwnerProject, w.OwnerComponent)
	}
	if len(w.ResourceRefs) != 2 || w.ResourceRefs[0] != "shop-pg" || w.ResourceRefs[1] != "shop-stripe" {
		t.Errorf("resource refs = %v", w.ResourceRefs)
	}
	if len(w.Endpoints) != 2 {
		t.Fatalf("endpoints = %+v", w.Endpoints)
	}
	if w.Endpoints[0].Project != "inventory" || w.Endpoints[0].Component != "inventory-api" || w.Endpoints[0].Visibility != "namespace" {
		t.Errorf("cross-project endpoint = %+v", w.Endpoints[0])
	}
	if w.Endpoints[1].Project != "" || w.Endpoints[1].Component != "web" || w.Endpoints[1].Visibility != "project" {
		t.Errorf("same-project endpoint = %+v", w.Endpoints[1])
	}
}

func TestListWorkloadConsumerDeps_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListWorkloadConsumerDeps(context.Background(), "wc-abc", "shop")
	if err != nil {
		t.Fatalf("ListWorkloadConsumerDeps: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("expected empty slice, got %+v", got)
	}
}

func TestListWorkloadEndpoints_IgnoresConsumerDependencies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{
			"items": [{
				"metadata": {"name": "shop-orders"},
				"spec": {
					"owner": {"projectName": "shop", "componentName": "orders"},
					"endpoints": {"http": {"type": "HTTP", "port": 8080, "visibility": ["namespace"]}},
					"dependencies": {"resources": [{"ref": "shop-pg"}]}
				}
			}]
		}`)
	}))
	defer srv.Close()

	c := newTestResourceClient(t, srv)
	got, err := c.ListWorkloadEndpoints(context.Background(), "wc-abc")
	if err != nil {
		t.Fatalf("ListWorkloadEndpoints: %v", err)
	}
	if len(got) != 1 || got[0].Name != "http" || got[0].Project != "shop" {
		t.Fatalf("provider endpoints changed when consumer deps present: %+v", got)
	}
}

// ---- constructor -------------------------------------------------------------------

func TestNewResourceClient_PanicsWithoutBaseURL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when BaseURL is empty")
		}
	}()
	NewResourceClient(Config{})
}
