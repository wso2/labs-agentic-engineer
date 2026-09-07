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
	"strings"
	"testing"
)

func newTestEnvironmentClient(t *testing.T, srv *httptest.Server) EnvironmentClient {
	t.Helper()
	return NewEnvironmentClient(Config{BaseURL: srv.URL})
}

func TestEnvironmentClient_ListNames_MapsMetadata(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items": []any{
				map[string]any{"metadata": map[string]any{"name": "default"}},
				map[string]any{"metadata": map[string]any{"name": "staging-local"}},
			},
			"pagination": map[string]any{},
		})
	}))
	defer srv.Close()

	got, err := newTestEnvironmentClient(t, srv).ListNames(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if len(got) != 2 || got[0] != "default" || got[1] != "staging-local" {
		t.Fatalf("ListNames = %#v", got)
	}
	if gotPath != "/api/v1/namespaces/acme/environments" {
		t.Fatalf("path = %q, want /api/v1/namespaces/acme/environments", gotPath)
	}
}

func TestEnvironmentClient_ListNames_EmptyItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"items":      []any{},
			"pagination": map[string]any{},
		})
	}))
	defer srv.Close()

	got, err := newTestEnvironmentClient(t, srv).ListNames(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ListNames: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty items = %#v, want non-nil empty slice", got)
	}
}

func TestEnvironmentClient_ListNames_EmptyOrg(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Errorf("empty org must not call OC, got %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	got, err := newTestEnvironmentClient(t, srv).ListNames(context.Background(), "")
	if err != nil {
		t.Fatalf("ListNames empty org: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("empty org = %#v, want non-nil empty slice", got)
	}
	if called {
		t.Fatal("empty org must not hit OC")
	}
}

func TestEnvironmentClient_ListNames_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusForbidden, map[string]any{"error": "denied"})
	}))
	defer srv.Close()

	_, err := newTestEnvironmentClient(t, srv).ListNames(context.Background(), "acme")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("want ErrForbidden, got %v", err)
	}
}

// ---- the Thunder binding ----------------------------------------------------

// The binding is read off the Environment's annotations, which is the only one
// of its three projections aep-api can reach: it runs outside the cluster, so
// the ConfigMap and the Secret the same script writes are both invisible to it.
func TestEnvironmentClient_GetThunderBinding_ReadsTheAnnotations(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusOK, map[string]any{
			"metadata": map[string]any{
				"name": "default",
				"annotations": map[string]string{
					"aep.wso2.com/thunder-issuer":                     "http://default-idp.amp.localhost:8080",
					"aep.wso2.com/thunder-admin-url":                  "http://thunder-acme-default-service.thunder-acme-default.svc.cluster.local:8090",
					"aep.wso2.com/thunder-system-resource-identifier": "http://default-idp.amp.localhost:8080/mcp",
					"aep.wso2.com/thunder-secret-path":                "secret/aep/thunder/acme/default",
					"aep.wso2.com/thunder-binding":                    "thunder-binding-acme-default",
					"openchoreo.dev/description":                      "an unrelated annotation",
				},
			},
		})
	}))
	defer srv.Close()

	got, err := newTestEnvironmentClient(t, srv).GetThunderBinding(context.Background(), "acme", "default")
	if err != nil {
		t.Fatalf("GetThunderBinding: %v", err)
	}
	if gotPath != "/api/v1/namespaces/acme/environments/default" {
		t.Fatalf("path = %q", gotPath)
	}
	want := ThunderBinding{
		OrgID: "acme", Environment: "default",
		Issuer:                   "http://default-idp.amp.localhost:8080",
		AdminURL:                 "http://thunder-acme-default-service.thunder-acme-default.svc.cluster.local:8090",
		SystemResourceIdentifier: "http://default-idp.amp.localhost:8080/mcp",
		SecretPath:               "secret/aep/thunder/acme/default",
		Name:                     "thunder-binding-acme-default",
	}
	if got != want {
		t.Fatalf("binding =\n%+v\nwant\n%+v", got, want)
	}
}

// An environment with no binding is ErrNoThunderBinding, not a transport error
// and not a zero-valued binding: "provision one" and "retry" are opposite
// recoveries, and a zero binding would send a mint at an empty issuer.
func TestEnvironmentClient_GetThunderBinding_UnboundEnvironment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"metadata": map[string]any{"name": "staging"},
		})
	}))
	defer srv.Close()

	_, err := newTestEnvironmentClient(t, srv).GetThunderBinding(context.Background(), "acme", "staging")
	if !errors.Is(err, ErrNoThunderBinding) {
		t.Fatalf("want ErrNoThunderBinding, got %v", err)
	}
	if !strings.Contains(err.Error(), "setup-environment-thunder.sh acme staging") {
		t.Fatalf("the error must name the fix, got: %v", err)
	}
}

// A HALF-written binding is treated as absent. Every required field feeds the
// mint, and a mint missing the resource indicator is the silent trap this
// platform has already been bitten by: the token endpoint answers 200 with a
// scope-less token, and every admin call 403s with nothing saying why.
func TestEnvironmentClient_GetThunderBinding_PartialBindingIsAbsent(t *testing.T) {
	for name, annotations := range map[string]map[string]string{
		"no resource indicator": {
			"aep.wso2.com/thunder-issuer":      "http://default-idp.amp.localhost:8080",
			"aep.wso2.com/thunder-secret-path": "secret/aep/thunder/acme/default",
		},
		"no credential path": {
			"aep.wso2.com/thunder-issuer":                     "http://default-idp.amp.localhost:8080",
			"aep.wso2.com/thunder-system-resource-identifier": "http://default-idp.amp.localhost:8080/mcp",
		},
		"no issuer": {
			"aep.wso2.com/thunder-system-resource-identifier": "http://default-idp.amp.localhost:8080/mcp",
			"aep.wso2.com/thunder-secret-path":                "secret/aep/thunder/acme/default",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := thunderBindingFromAnnotations("acme", "default", annotations)
			if !errors.Is(err, ErrNoThunderBinding) {
				t.Fatalf("want ErrNoThunderBinding for a %s, got %v", name, err)
			}
		})
	}
}

// The admin URL is optional: a caller that reaches the instance at its public
// issuer needs no in-cluster address, and refusing the binding for a missing one
// would make the whole feature unusable from outside the cluster.
func TestEnvironmentClient_GetThunderBinding_AdminURLIsOptional(t *testing.T) {
	got, err := thunderBindingFromAnnotations("acme", "default", map[string]string{
		"aep.wso2.com/thunder-issuer":                     "http://default-idp.amp.localhost:8080",
		"aep.wso2.com/thunder-system-resource-identifier": "http://default-idp.amp.localhost:8080/mcp",
		"aep.wso2.com/thunder-secret-path":                "secret/aep/thunder/acme/default",
	})
	if err != nil {
		t.Fatalf("a binding with no admin URL must still be usable: %v", err)
	}
	if got.AdminURL != "" {
		t.Fatalf("adminURL = %q, want empty", got.AdminURL)
	}
}
