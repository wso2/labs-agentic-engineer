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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A live pipeline 404 must classify as ErrNotFound: the write-target resolver
// treats it as a configuration fact, not a transient failure to retry.
func TestProjectCellClientGetPipeline_ClassifiesNotFound(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		wantMiss bool
	}{
		{"404 is ErrNotFound", http.StatusNotFound, true},
		{"503 stays transient", http.StatusServiceUnavailable, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, `{"error":"nope"}`, tc.status)
			}))
			defer srv.Close()
			c := &projectCellClient{baseURL: srv.URL, http: srv.Client()}
			_, err := c.getPipeline(context.Background(), "acme", "gone")
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrNotFound); got != tc.wantMiss {
				t.Fatalf("errors.Is(ErrNotFound) = %v, want %v (err: %v)", got, tc.wantMiss, err)
			}
		})
	}
}

// A served pipeline must decode into promotion paths and resolve to its root.
// This is the only test that drives getPipeline over a real 200 body.
func TestProjectCellClientGetPipeline_DecodesPromotionPaths(t *testing.T) {
	path := func(src string, targets ...string) map[string]any {
		refs := []map[string]any{}
		for _, n := range targets {
			refs = append(refs, map[string]any{"name": n})
		}
		return map[string]any{
			"sourceEnvironmentRef":  map[string]any{"name": src},
			"targetEnvironmentRefs": refs,
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if want := nsBase("acme") + "/deploymentpipelines/linear"; r.URL.Path != want {
			t.Errorf("path %q, want %q", r.URL.Path, want)
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"metadata": map[string]any{"name": "linear"},
			"spec": map[string]any{"promotionPaths": []any{
				path("development", "staging"),
				path("staging", "production"),
			}},
		})
	}))
	defer srv.Close()

	c := &projectCellClient{baseURL: srv.URL, http: srv.Client()}
	p, err := c.getPipeline(context.Background(), "acme", "linear")
	if err != nil {
		t.Fatalf("getPipeline: %v", err)
	}
	got, err := PipelineRoot("linear", p)
	if err != nil || got != "development" {
		t.Fatalf("PipelineRoot = %q, err=%v, want development", got, err)
	}
}

// The readiness reads return the resource's Ready condition as OpenChoreo
// reports it, from the path the resource lives at.
func TestProjectCellClient_Readiness(t *testing.T) {
	ready := func(status, reason, message string) map[string]any {
		return map[string]any{"conditions": []any{
			map[string]any{"type": "Synced", "status": "True", "reason": "ReleaseSynced"},
			map[string]any{"type": "Ready", "status": status, "reason": reason, "message": message},
		}}
	}
	cases := []struct {
		name   string
		path   string
		status map[string]any
		read   func(*projectCellClient) (Readiness, error)
		want   Readiness
	}{
		{
			name:   "project type missing",
			path:   nsBase("acme") + "/projects/shop",
			status: ready("False", "ProjectTypeNotFound", `ClusterProjectType "default" not found`),
			read: func(c *projectCellClient) (Readiness, error) {
				return c.ProjectReadiness(context.Background(), "acme", "shop")
			},
			want: Readiness{Reason: "ProjectTypeNotFound", Message: `ClusterProjectType "default" not found`},
		},
		{
			name:   "binding ready",
			path:   nsBase("acme") + "/projectreleasebindings/shop-development",
			status: ready("True", "Ready", ""),
			read: func(c *projectCellClient) (Readiness, error) {
				return c.ProjectReleaseBindingReadiness(context.Background(), "acme", "shop", "development")
			},
			want: Readiness{Ready: true, Reason: "Ready"},
		},
		{
			name:   "no conditions yet",
			path:   nsBase("acme") + "/projects/shop",
			status: map[string]any{},
			read: func(c *projectCellClient) (Readiness, error) {
				return c.ProjectReadiness(context.Background(), "acme", "shop")
			},
			want: Readiness{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != tc.path {
					t.Errorf("%s %q, want GET %q", r.Method, r.URL.Path, tc.path)
					http.Error(w, "bad path", http.StatusNotFound)
					return
				}
				writeJSON(t, w, http.StatusOK, map[string]any{
					"metadata": map[string]any{"name": "x"},
					"status":   tc.status,
				})
			}))
			defer srv.Close()
			got, err := tc.read(&projectCellClient{baseURL: srv.URL, http: srv.Client()})
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got != tc.want {
				t.Errorf("readiness = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A failed read is an error, never a not-ready verdict: the caller must not
// mistake an unreachable API for a resource that reconciled badly.
func TestProjectCellClient_ReadinessReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := &projectCellClient{baseURL: srv.URL, http: srv.Client()}
	if _, err := c.ProjectReadiness(context.Background(), "acme", "shop"); err == nil {
		t.Error("ProjectReadiness: want error on 503")
	}
	if _, err := c.ProjectReleaseBindingReadiness(context.Background(), "acme", "shop", "development"); err == nil {
		t.Error("ProjectReleaseBindingReadiness: want error on 503")
	}
}

// A missing resource is ErrNotFound, so a caller can tell it from a failed
// read (AE Studio treats a missing ProjectReleaseBinding as drift).
func TestProjectCellClient_ReadinessNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	c := &projectCellClient{baseURL: srv.URL, http: srv.Client()}
	if _, err := c.ProjectReleaseBindingReadiness(context.Background(), "acme", "ae-system", "development"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ProjectReleaseBindingReadiness on 404: err %v, want ErrNotFound", err)
	}
	if _, err := c.ProjectReadiness(context.Background(), "acme", "shop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ProjectReadiness on 404: err %v, want ErrNotFound", err)
	}
}

// A failed read is not ErrNotFound.
func TestProjectCellClient_ReadinessFailureIsNotNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := &projectCellClient{baseURL: srv.URL, http: srv.Client()}
	if _, err := c.ProjectReleaseBindingReadiness(context.Background(), "acme", "ae-system", "development"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("on 503: err %v, want a non-ErrNotFound error", err)
	}
}
