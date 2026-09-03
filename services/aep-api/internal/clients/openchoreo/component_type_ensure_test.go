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

func TestEnsureComponentType_Create(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"apiVersion": "openchoreo.dev/v1alpha1",
			"kind":       "ComponentType",
			"metadata":   map[string]any{"name": CodingAgentComponentTypeName},
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType())
	if err != nil {
		t.Fatalf("EnsureComponentType: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/namespaces/wc-abc/componenttypes" {
		t.Errorf("unexpected request: %s %s", gotMethod, gotPath)
	}
	if gotBody["kind"] != "ComponentType" {
		t.Errorf("unexpected body kind: %v", gotBody["kind"])
	}
	meta, _ := gotBody["metadata"].(map[string]any)
	if meta["name"] != CodingAgentComponentTypeName {
		t.Errorf("unexpected body name: %v", meta["name"])
	}
}

// storedSpec renders the shipped ComponentType's spec the way OpenChoreo would
// hand it back — over the wire, so every number is a float64 — and applies
// mutate to age it. It is how a "stored under an older platform build" row is
// simulated without hand-writing a second copy of the whole spec.
func storedSpec(t *testing.T, mutate func(props map[string]any)) map[string]any {
	t.Helper()
	raw, err := json.Marshal(CodingAgentComponentType()["spec"])
	if err != nil {
		t.Fatalf("marshal desired spec: %v", err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("unmarshal desired spec: %v", err)
	}
	if mutate != nil {
		params, _ := spec["parameters"].(map[string]any)
		schema, _ := params["openAPIV3Schema"].(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			t.Fatal("stored spec has no openAPIV3Schema.properties")
		}
		mutate(props)
	}
	return spec
}

// componentTypeFake is an OpenChoreo that already has the ComponentType: it
// answers the create with 409 and serves stored on the GET, counting methods and
// keeping whatever was PUT so a test can assert what convergence wrote.
type componentTypeFake struct {
	stored     map[string]any
	posts      int
	gets       int
	putBodies  []map[string]any
	updateCode int // PUT status; 0 → 200 OK
}

func (f *componentTypeFake) serve(t *testing.T) *httptest.Server {
	t.Helper()
	base := "/api/v1/namespaces/wc-abc/componenttypes"
	object := func() map[string]any {
		return map[string]any{
			"apiVersion": "openchoreo.dev/v1alpha1",
			"kind":       "ComponentType",
			"metadata":   map[string]any{"name": CodingAgentComponentTypeName},
			"spec":       f.stored,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			f.posts++
			if r.URL.Path != base {
				t.Errorf("unexpected POST path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "already exists"})
		case http.MethodGet:
			f.gets++
			if r.URL.Path != base+"/"+CodingAgentComponentTypeName {
				t.Errorf("unexpected GET path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(object())
		case http.MethodPut:
			if r.URL.Path != base+"/"+CodingAgentComponentTypeName {
				t.Errorf("unexpected PUT path: %s", r.URL.Path)
			}
			var got map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			f.putBodies = append(f.putBodies, got)
			if f.updateCode != 0 {
				w.WriteHeader(f.updateCode)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": http.StatusText(f.updateCode)})
				return
			}
			f.stored, _ = got["spec"].(map[string]any)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(object())
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *componentTypeFake) puts() int { return len(f.putBodies) }

// TestEnsureComponentType_ConflictLeavesACurrentTypeAlone: seeding runs on the
// dispatch path, so an already-current type must cost one refetch and no write.
// The stored copy carries a property we never sent — OpenChoreo defaults fields
// on write — and that must NOT read as drift.
func TestEnsureComponentType_ConflictLeavesACurrentTypeAlone(t *testing.T) {
	f := &componentTypeFake{stored: storedSpec(t, func(props map[string]any) {
		props["serverDefaultedKnob"] = map[string]any{"type": "string", "default": "whatever"}
	})}
	srv := f.serve(t)

	c := NewComponentClient(Config{BaseURL: srv.URL})
	if err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType()); err != nil {
		t.Fatalf("EnsureComponentType: %v", err)
	}
	if f.posts != 1 || f.gets != 1 {
		t.Errorf("expected 1 POST + 1 GET, got posts=%d gets=%d", f.posts, f.gets)
	}
	if f.puts() != 0 {
		t.Errorf("a current componenttype must not be rewritten, saw %d PUT(s)", f.puts())
	}
}

// TestEnsureComponentType_ConflictConvergesAStaleType is the whole point of the
// conflict branch: an org seeded by an older platform build stores the schema
// every one of its own dispatches is validated against. Left alone, its next
// dispatch is rejected by its own stale copy — and the create path, which
// already worked, proves nothing about that.
func TestEnsureComponentType_ConflictConvergesAStaleType(t *testing.T) {
	f := &componentTypeFake{stored: storedSpec(t, func(props map[string]any) {
		deadline, _ := props["activeDeadlineSeconds"].(map[string]any)
		deadline["maximum"] = float64(7200) // the pre-3h ceiling
	})}
	srv := f.serve(t)

	c := NewComponentClient(Config{BaseURL: srv.URL})
	if err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType()); err != nil {
		t.Fatalf("EnsureComponentType: %v", err)
	}
	if f.posts != 1 || f.gets != 1 || f.puts() != 1 {
		t.Fatalf("expected 1 POST + 1 GET + 1 PUT, got posts=%d gets=%d puts=%d", f.posts, f.gets, f.puts())
	}
	got := f.putBodies[0]
	meta, _ := got["metadata"].(map[string]any)
	if meta["name"] != CodingAgentComponentTypeName {
		t.Errorf("PUT body name = %v", meta["name"])
	}
	if max := storedDeadlineMaximum(t, got); max != float64(codingAgentDeadlineCeilingSeconds) {
		t.Errorf("converged maximum = %v, want %d", max, codingAgentDeadlineCeilingSeconds)
	}

	// The write settled it: a second ensure over the now-current type is a
	// read again, not a second write. Convergence must not be a per-dispatch
	// rewrite loop.
	if err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType()); err != nil {
		t.Fatalf("second EnsureComponentType: %v", err)
	}
	if f.puts() != 1 {
		t.Errorf("converged type rewritten again: %d PUT(s)", f.puts())
	}
}

// storedDeadlineMaximum digs activeDeadlineSeconds.maximum out of a ComponentType
// body as it appeared on the wire.
func storedDeadlineMaximum(t *testing.T, body map[string]any) float64 {
	t.Helper()
	spec, _ := body["spec"].(map[string]any)
	params, _ := spec["parameters"].(map[string]any)
	schema, _ := params["openAPIV3Schema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	deadline, _ := props["activeDeadlineSeconds"].(map[string]any)
	max, ok := deadline["maximum"].(float64)
	if !ok {
		t.Fatalf("activeDeadlineSeconds.maximum missing or not a number: %#v", deadline)
	}
	return max
}

// TestEnsureComponentType_ConflictSurfacesAFailedUpdate: a convergence that
// cannot be written must fail loudly. Returning nil would leave the org
// validating dispatches against the stale schema with nothing to explain why the
// dispatch after it was rejected.
func TestEnsureComponentType_ConflictSurfacesAFailedUpdate(t *testing.T) {
	f := &componentTypeFake{
		stored:     map[string]any{"workloadType": "job"},
		updateCode: http.StatusForbidden,
	}
	srv := f.serve(t)

	c := NewComponentClient(Config{BaseURL: srv.URL})
	err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType())
	if err == nil {
		t.Fatal("a refused update must not be reported as a successful ensure")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("error = %v, want it to carry ErrForbidden", err)
	}
}

// TestJSONCovers_ContainmentNotEquality pins the comparison the convergence gate
// rests on: extra keys on the stored side are the server's business, missing or
// changed ones are drift.
func TestJSONCovers_ContainmentNotEquality(t *testing.T) {
	want := map[string]any{"a": float64(1), "b": []any{"x", "y"}}
	cases := []struct {
		name string
		got  any
		ok   bool
	}{
		{"exact", map[string]any{"a": float64(1), "b": []any{"x", "y"}}, true},
		{"extra key", map[string]any{"a": float64(1), "b": []any{"x", "y"}, "c": true}, true},
		{"changed value", map[string]any{"a": float64(2), "b": []any{"x", "y"}}, false},
		{"missing key", map[string]any{"a": float64(1)}, false},
		{"short list", map[string]any{"a": float64(1), "b": []any{"x"}}, false},
		{"reordered list", map[string]any{"a": float64(1), "b": []any{"y", "x"}}, false},
	}
	for _, tc := range cases {
		if got := jsonCovers(tc.got, want); got != tc.ok {
			t.Errorf("%s: jsonCovers = %v, want %v", tc.name, got, tc.ok)
		}
	}
}
