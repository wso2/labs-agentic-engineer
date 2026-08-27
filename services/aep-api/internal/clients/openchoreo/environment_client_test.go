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
				map[string]any{"metadata": map[string]any{"name": "development"}},
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
	if len(got) != 2 || got[0] != "development" || got[1] != "staging-local" {
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
