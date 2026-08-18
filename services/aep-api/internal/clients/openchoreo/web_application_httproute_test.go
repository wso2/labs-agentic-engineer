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
	"sync/atomic"
	"testing"
)

func TestPatchWebApplicationHTTPRouteResources_ReplacesCatchAll(t *testing.T) {
	t.Parallel()
	in := []any{
		map[string]any{"id": "service", "template": map[string]any{"kind": "Service"}},
		map[string]any{
			"id":          "httproute",
			"includeWhen": "${size(workload.endpoints) > 0}",
			"template": map[string]any{
				"kind": "HTTPRoute",
				"spec": map[string]any{
					"parentRefs": []any{
						map[string]any{"name": "gateway-default", "namespace": "openchoreo-data-plane"},
					},
					"rules": []any{
						map[string]any{
							"backendRefs": []any{
								map[string]any{"name": "${metadata.componentName}", "port": "${workload.toServicePorts()[0].port}"},
							},
						},
					},
				},
			},
		},
		map[string]any{"id": "env-config"},
	}

	out, changed := patchWebApplicationHTTPRouteResources(in)
	if !changed {
		t.Fatal("expected the catch-all httproute to be replaced")
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3 (service, httproute-external, env-config)", len(out))
	}
	if resourceID(out[0]) != "service" || resourceID(out[2]) != "env-config" {
		t.Fatalf("surrounding resources moved: ids=%v", resourceIDs(out))
	}
	ext, ok := out[1].(map[string]any)
	if !ok {
		t.Fatalf("httproute-external is %T, want map", out[1])
	}
	if ext["id"] != "httproute-external" {
		t.Errorf("id = %v, want httproute-external", ext["id"])
	}
	if ext["includeWhen"] != nil {
		t.Errorf("includeWhen = %v, want omitted (visibility forEach replaces it)", ext["includeWhen"])
	}
	tmpl, _ := ext["template"].(map[string]any)
	spec, _ := tmpl["spec"].(map[string]any)
	hostnames, _ := spec["hostnames"].(string)
	if hostnames == "" || !strings.Contains(hostnames, "oc_dns_label") || !strings.Contains(hostnames, "gateway.ingress.external") {
		t.Errorf("hostnames = %q, want oc_dns_label on gateway.ingress.external", hostnames)
	}
	if _, ok := spec["parentRefs"]; !ok {
		t.Error("parentRefs missing")
	}
}

func TestPatchWebApplicationHTTPRouteResources_IdempotentWhenHostnamesExist(t *testing.T) {
	t.Parallel()
	in := []any{
		map[string]any{
			"id": "httproute-external",
			"template": map[string]any{
				"spec": map[string]any{
					"hostnames": "already-minted.example.com",
				},
			},
		},
	}
	out, changed := patchWebApplicationHTTPRouteResources(in)
	if changed {
		t.Fatal("already-hosted httproute-external must not be rewritten")
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
}

func TestPatchWebApplicationHTTPRouteResources_RewritesExternalWithoutHostnames(t *testing.T) {
	t.Parallel()
	in := []any{
		map[string]any{
			"id":       "httproute-external",
			"template": map[string]any{"spec": map[string]any{"rules": []any{}}},
		},
	}
	out, changed := patchWebApplicationHTTPRouteResources(in)
	if !changed {
		t.Fatal("expected hostname-less httproute-external to be rewritten")
	}
	hostnames := resourceHostnames(out[0])
	if hostnames == "" {
		t.Fatal("rewritten route has no hostnames")
	}
}

func TestPatchComponentTypeDoc_PreservesResourceVersion(t *testing.T) {
	t.Parallel()
	doc := map[string]any{
		"apiVersion": "openchoreo.dev/v1alpha1",
		"kind":       "ComponentType",
		"metadata": map[string]any{
			"name":            "web-application",
			"resourceVersion": "4242",
			"uid":             "abc",
		},
		"spec": map[string]any{
			"resources": []any{
				map[string]any{"id": "httproute", "template": map[string]any{"kind": "HTTPRoute"}},
			},
		},
	}
	changed, err := patchWebApplicationComponentTypeDoc(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected patch")
	}
	meta, _ := doc["metadata"].(map[string]any)
	if meta["resourceVersion"] != "4242" || meta["uid"] != "abc" {
		t.Errorf("metadata clobbered: %v", meta)
	}
}

func resourceIDs(resources []any) []string {
	ids := make([]string, len(resources))
	for i, r := range resources {
		ids[i] = resourceID(r)
	}
	return ids
}

func resourceHostnames(r any) string {
	m, _ := r.(map[string]any)
	tmpl, _ := m["template"].(map[string]any)
	spec, _ := tmpl["spec"].(map[string]any)
	h, _ := spec["hostnames"].(string)
	return h
}

func TestEnsureWebApplicationHTTPRouteHostnames_NoopOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL}).(*componentClient)
	if err := c.ensureWebApplicationHTTPRouteHostnames(context.Background(), "wc-abc"); err != nil {
		t.Fatalf("404 must be a no-op, got %v", err)
	}
}

func TestEnsureWebApplicationHTTPRouteHostnames_PutsHostnamesAndKeepsResourceVersion(t *testing.T) {
	var putBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/componenttypes/web-application"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"apiVersion": "openchoreo.dev/v1alpha1",
				"kind":       "ComponentType",
				"metadata": map[string]any{
					"name":            "web-application",
					"resourceVersion": "7",
				},
				"spec": map[string]any{
					"resources": []any{
						map[string]any{"id": "httproute", "template": map[string]any{"kind": "HTTPRoute"}},
					},
				},
			})
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/componenttypes/web-application"):
			decodeJSONBody(t, r, &putBody)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(putBody)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL}).(*componentClient)
	if err := c.ensureWebApplicationHTTPRouteHostnames(context.Background(), "wc-abc"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	meta, _ := putBody["metadata"].(map[string]any)
	if meta["resourceVersion"] != "7" {
		t.Errorf("PUT dropped resourceVersion: %v", meta)
	}
	spec, _ := putBody["spec"].(map[string]any)
	resources, _ := spec["resources"].([]any)
	if len(resources) != 1 || resourceID(resources[0]) != "httproute-external" {
		t.Errorf("PUT resources = %v", resources)
	}
	if resourceHostnames(resources[0]) == "" {
		t.Fatal("PUT route has no hostnames")
	}
}

func TestCreateComponent_WebApplicationEnsuresHTTPRouteHostnames(t *testing.T) {
	var gotTypeGet, gotComponentPost atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/componenttypes/web-application"):
			gotTypeGet.Store(true)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"name": "web-application"},
				"spec": map[string]any{
					"resources": []any{
						map[string]any{
							"id": "httproute-external",
							"template": map[string]any{
								"spec": map[string]any{"hostnames": "already.example.com"},
							},
						},
					},
				},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/components"):
			gotComponentPost.Store(true)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"metadata": map[string]any{"name": "widgets-storefront"},
				"spec": map[string]any{
					"componentType": map[string]any{"name": "deployment/web-application"},
					"owner":         map[string]any{"projectName": "widgets"},
				},
			})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	if _, err := c.CreateComponent(context.Background(), "wc-abc", "widgets", &CreateComponentRequest{
		Name: "storefront",
		Type: "deployment/web-application",
	}); err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
	if !gotTypeGet.Load() {
		t.Fatal("web-application create must GET the org ComponentType before POSTing the Component")
	}
	if !gotComponentPost.Load() {
		t.Fatal("Component POST never happened")
	}
}

func TestCreateComponent_ServiceDoesNotTouchWebApplicationType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/componenttypes/") {
			t.Errorf("service create must not touch ComponentTypes, got %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{"name": "widgets-api"},
			"spec": map[string]any{
				"componentType": map[string]any{"name": "deployment/service"},
				"owner":         map[string]any{"projectName": "widgets"},
			},
		})
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	if _, err := c.CreateComponent(context.Background(), "wc-abc", "widgets", &CreateComponentRequest{
		Name: "api",
		Type: "deployment/service",
	}); err != nil {
		t.Fatalf("CreateComponent: %v", err)
	}
}
