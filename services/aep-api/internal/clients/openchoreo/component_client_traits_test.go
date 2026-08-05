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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// UNIT tier for the two trait write paths, driven end-to-end through the REAL
// generated client against a stand-in openchoreo-api. These reproduce the
// production failure that shipped a protected API with no jwtAuth policy: the
// BFF's own Component write makes OC's controller rewrite the ReleaseBinding,
// and the BFF's next write — the one carrying jwtAuth — died on the resulting
// conflict-as-500 with no retry.

// fakeOC records every request and serves a mutable ReleaseBinding + Component.
// `failPUTs` makes the first N writes to a path fail the way openchoreo-api
// reports a lost optimistic-concurrency race: HTTP 500, generic body.
type fakeOC struct {
	mu sync.Mutex
	t  *testing.T

	// rbTraitConfigs is the server-side state of spec.traitEnvironmentConfigs.
	rbTraitConfigs map[string]interface{}
	// rbReleaseName flips on each GET to model the controller rewriting the
	// binding underneath us; the BFF must not carry a stale copy forward.
	rbReleaseName string

	failPUTs int
	getCount int
	putCount int
	// putBodies is the traitEnvironmentConfigs seen on each accepted PUT.
	putBodies []map[string]interface{}
}

func (f *fakeOC) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")

		switch {
		// List bindings for a component — discovery only; returns names.
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/releasebindings"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{f.releaseBinding()},
			})

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/releasebindings/"):
			f.getCount++
			// The controller moved the binding on to a new release since the
			// last read: a caller that reused an older copy would write this
			// field back to its stale value.
			f.rbReleaseName = "release-rev-" + string(rune('a'+f.getCount))
			_ = json.NewEncoder(w).Encode(f.releaseBinding())

		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/releasebindings/"):
			f.putCount++
			if f.failPUTs > 0 {
				f.failPUTs--
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				f.t.Fatalf("decode PUT body: %v", err)
			}
			spec, _ := body["spec"].(map[string]any)
			cfgs, _ := spec["traitEnvironmentConfigs"].(map[string]interface{})
			// A PUT must be built from the newest read, so the releaseName it
			// echoes back has to be the one the last GET served.
			if got, _ := spec["releaseName"].(string); got != f.rbReleaseName {
				f.t.Errorf("PUT carried releaseName %q, want the freshly-read %q — the write did not re-read", got, f.rbReleaseName)
			}
			f.rbTraitConfigs = cfgs
			f.putBodies = append(f.putBodies, cfgs)
			_ = json.NewEncoder(w).Encode(f.releaseBinding())

		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/components/"):
			f.getCount++
			_ = json.NewEncoder(w).Encode(f.component())

		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/components/"):
			f.putCount++
			if f.failPUTs > 0 {
				f.failPUTs--
				w.WriteHeader(http.StatusInternalServerError)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
				return
			}
			_ = json.NewEncoder(w).Encode(f.component())

		default:
			f.t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
}

func (f *fakeOC) releaseBinding() map[string]any {
	spec := map[string]any{
		"environment": "development",
		"owner":       map[string]any{"componentName": "proj-api", "projectName": "proj"},
		"releaseName": f.rbReleaseName,
	}
	if f.rbTraitConfigs != nil {
		spec["traitEnvironmentConfigs"] = f.rbTraitConfigs
	}
	return map[string]any{
		"metadata": map[string]any{"name": "proj-api-development"},
		"spec":     spec,
	}
}

func (f *fakeOC) component() map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": "proj-api"},
		"spec": map[string]any{
			"componentType": map[string]any{"kind": "ClusterComponentType", "name": "service"},
			"owner":         map[string]any{"projectName": "proj"},
		},
	}
}

func newFakeOCClient(t *testing.T, f *fakeOC) (ComponentClient, func()) {
	t.Helper()
	f.t = t
	f.rbReleaseName = "release-rev-0"
	srv := httptest.NewServer(f.handler())
	return NewComponentClient(Config{BaseURL: srv.URL}), srv.Close
}

// TestUpdateComponentTraitEnvironmentConfigs_RetriesConflictAs500 — THE
// regression. In production this write lost a race with OC's Component
// controller, returned 500, and was never retried, so jwtAuth never reached the
// gateway and every request to a protected API passed through unauthenticated.
func TestUpdateComponentTraitEnvironmentConfigs_RetriesConflictAs500(t *testing.T) {
	noStaleWriteBackoff(t)
	f := &fakeOC{failPUTs: 2}
	c, closeSrv := newFakeOCClient(t, f)
	defer closeSrv()

	want := map[string]map[string]interface{}{
		"api-http": {"jwtAuth": map[string]interface{}{"enabled": true}},
	}
	if err := c.UpdateComponentTraitEnvironmentConfigs(context.Background(), "org", "proj", "api", want); err != nil {
		t.Fatalf("UpdateComponentTraitEnvironmentConfigs should survive two conflicts: %v", err)
	}
	if f.putCount != 3 {
		t.Errorf("want 3 PUT attempts (2 conflicts + success), got %d", f.putCount)
	}
	got, ok := f.rbTraitConfigs["api-http"].(map[string]interface{})
	if !ok {
		t.Fatalf("jwtAuth config never landed; server state = %#v", f.rbTraitConfigs)
	}
	jwt, _ := got["jwtAuth"].(map[string]interface{})
	if jwt == nil || jwt["enabled"] != true {
		t.Errorf("want jwtAuth.enabled=true on the binding, got %#v", got)
	}
}

// TestUpdateComponentTraitEnvironmentConfigs_MergePreservesOtherInstances — the
// write owns only the instances it names. A sibling trait's config (here the
// auto-RCA rule's mandatory notification channel) must survive, or clearing one
// trait's config would break another trait's render.
func TestUpdateComponentTraitEnvironmentConfigs_MergePreservesOtherInstances(t *testing.T) {
	noStaleWriteBackoff(t)
	f := &fakeOC{rbTraitConfigs: map[string]interface{}{
		"api-auto-rca-error": map[string]interface{}{"enabled": true},
	}}
	c, closeSrv := newFakeOCClient(t, f)
	defer closeSrv()

	err := c.UpdateComponentTraitEnvironmentConfigs(context.Background(), "org", "proj", "api",
		map[string]map[string]interface{}{"api-http": {"jwtAuth": map[string]interface{}{"enabled": true}}})
	if err != nil {
		t.Fatalf("UpdateComponentTraitEnvironmentConfigs: %v", err)
	}
	if _, ok := f.rbTraitConfigs["api-auto-rca-error"]; !ok {
		t.Errorf("untouched instance was clobbered; got keys %v", mapKeys(f.rbTraitConfigs))
	}
	if _, ok := f.rbTraitConfigs["api-http"]; !ok {
		t.Errorf("named instance not written; got keys %v", mapKeys(f.rbTraitConfigs))
	}
}

// TestUpdateComponentTraitEnvironmentConfigs_EmptyValueDeletes — a tombstone
// removes just its own key.
func TestUpdateComponentTraitEnvironmentConfigs_EmptyValueDeletes(t *testing.T) {
	noStaleWriteBackoff(t)
	f := &fakeOC{rbTraitConfigs: map[string]interface{}{
		"api-http":           map[string]interface{}{"jwtAuth": map[string]interface{}{"enabled": true}},
		"api-auto-rca-error": map[string]interface{}{"enabled": true},
	}}
	c, closeSrv := newFakeOCClient(t, f)
	defer closeSrv()

	err := c.UpdateComponentTraitEnvironmentConfigs(context.Background(), "org", "proj", "api",
		map[string]map[string]interface{}{"api-http": nil})
	if err != nil {
		t.Fatalf("UpdateComponentTraitEnvironmentConfigs: %v", err)
	}
	if _, ok := f.rbTraitConfigs["api-http"]; ok {
		t.Errorf("tombstoned instance still present: %v", mapKeys(f.rbTraitConfigs))
	}
	if _, ok := f.rbTraitConfigs["api-auto-rca-error"]; !ok {
		t.Errorf("tombstone removed the wrong key: %v", mapKeys(f.rbTraitConfigs))
	}
}

// TestUpdateComponentTraits_RetriesConflictAs500 — the Component half of the
// same hazard: this write is the one that PROVOKES the controller rewrite, so it
// races too.
func TestUpdateComponentTraits_RetriesConflictAs500(t *testing.T) {
	noStaleWriteBackoff(t)
	f := &fakeOC{failPUTs: 1}
	c, closeSrv := newFakeOCClient(t, f)
	defer closeSrv()

	traits := []ComponentTrait{{InstanceName: "api-http", Kind: "ClusterTrait", Name: "api-configuration"}}
	if err := c.UpdateComponentTraits(context.Background(), "org", "proj", "api", traits); err != nil {
		t.Fatalf("UpdateComponentTraits should survive one conflict: %v", err)
	}
	if f.putCount != 2 {
		t.Errorf("want 2 PUT attempts, got %d", f.putCount)
	}
	if f.getCount != 2 {
		t.Errorf("each attempt must re-GET the component; want 2 GETs, got %d", f.getCount)
	}
}

// TestUpdateComponentTraitEnvironmentConfigs_NoBindingsIsSoftNoOp — before the
// first build there is nothing to patch; that is not an error.
func TestUpdateComponentTraitEnvironmentConfigs_NoBindingsIsSoftNoOp(t *testing.T) {
	noStaleWriteBackoff(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/releasebindings") {
			t.Fatalf("no write may be attempted when no binding exists; got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	err := c.UpdateComponentTraitEnvironmentConfigs(context.Background(), "org", "proj", "api",
		map[string]map[string]interface{}{"api-http": {"jwtAuth": map[string]interface{}{"enabled": true}}})
	if err != nil {
		t.Fatalf("absent binding must be a soft no-op, got %v", err)
	}
}

func mapKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
