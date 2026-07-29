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

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
)

func newTestSecretReferenceClient(t *testing.T, srv *httptest.Server) secretmanagersvc.OpenChoreoSecretReferenceClient {
	t.Helper()
	return NewSecretReferenceClient(Config{BaseURL: srv.URL})
}

func TestSecretReferenceClient_Get_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		if r.URL.Path != "/api/v1/namespaces/wc-org/secretreferences/anthropic-secrets" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	_, err := c.GetSecretReference(context.Background(), "wc-org", "anthropic-secrets")
	if !errors.Is(err, secretmanagersvc.ErrNotFound) {
		t.Fatalf("expected secretmanagersvc.ErrNotFound, got %v", err)
	}
}

func TestSecretReferenceClient_Create_Conflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "already exists"})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	_, err := c.CreateSecretReference(context.Background(), "wc-org", secretmanagersvc.CreateSecretReferenceRequest{
		Namespace:  "wc-org",
		Name:       "anthropic-secrets",
		KVPath:     "user-app-secrets/wc-org/anthropic-secrets",
		SecretKeys: []string{"api-key"},
	})
	if !errors.Is(err, secretmanagersvc.ErrConflict) {
		t.Fatalf("expected secretmanagersvc.ErrConflict, got %v", err)
	}
}

func TestSecretReferenceClient_Create_BuildsBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"name":      "anthropic-secrets",
				"namespace": "wc-org",
			},
		})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	got, err := c.CreateSecretReference(context.Background(), "wc-org", secretmanagersvc.CreateSecretReferenceRequest{
		Namespace:       "wc-org",
		Name:            "anthropic-secrets",
		KVPath:          "user-app-secrets/wc-org/anthropic-secrets",
		SecretKeys:      []string{"api-key"},
		RefreshInterval: "1h",
	})
	if err != nil {
		t.Fatalf("CreateSecretReference: %v", err)
	}
	if got.Name != "anthropic-secrets" || got.Namespace != "wc-org" {
		t.Fatalf("unexpected result: %+v", got)
	}

	meta, _ := gotBody["metadata"].(map[string]any)
	if meta["name"] != "anthropic-secrets" || meta["namespace"] != "wc-org" {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	spec, _ := gotBody["spec"].(map[string]any)
	if spec["refreshInterval"] != "1h" {
		t.Fatalf("unexpected refreshInterval: %v", spec["refreshInterval"])
	}
	tmpl, _ := spec["template"].(map[string]any)
	if tmpl["type"] != "Opaque" {
		t.Fatalf("unexpected template: %+v", tmpl)
	}
	data, _ := spec["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 data entry, got %d", len(data))
	}
	entry, _ := data[0].(map[string]any)
	if entry["secretKey"] != "api-key" {
		t.Fatalf("unexpected secretKey: %v", entry["secretKey"])
	}
	remote, _ := entry["remoteRef"].(map[string]any)
	if remote["key"] != "user-app-secrets/wc-org/anthropic-secrets" {
		t.Fatalf("unexpected remoteRef.key: %v", remote["key"])
	}
	if remote["property"] != "api-key" {
		t.Fatalf("unexpected remoteRef.property: %v", remote["property"])
	}
}

func TestSecretReferenceClient_Update_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut && r.Method != http.MethodPatch {
			// gen Update uses PUT
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method %s", r.Method)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing"})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	_, err := c.UpdateSecretReference(context.Background(), "wc-org", "anthropic-secrets", secretmanagersvc.CreateSecretReferenceRequest{
		Namespace:  "wc-org",
		Name:       "anthropic-secrets",
		KVPath:     "user-app-secrets/wc-org/anthropic-secrets",
		SecretKeys: []string{"api-key"},
	})
	if !errors.Is(err, secretmanagersvc.ErrNotFound) {
		t.Fatalf("expected secretmanagersvc.ErrNotFound, got %v", err)
	}
}

func TestSecretReferenceClient_Delete_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing"})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	err := c.DeleteSecretReference(context.Background(), "wc-org", "anthropic-secrets")
	if !errors.Is(err, secretmanagersvc.ErrNotFound) {
		t.Fatalf("expected secretmanagersvc.ErrNotFound, got %v", err)
	}
}

func TestSecretReferenceClient_Get_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"metadata": map[string]any{
				"name":      "anthropic-secrets",
				"namespace": "wc-org",
			},
		})
	}))
	defer srv.Close()

	c := newTestSecretReferenceClient(t, srv)
	got, err := c.GetSecretReference(context.Background(), "wc-org", "anthropic-secrets")
	if err != nil {
		t.Fatalf("GetSecretReference: %v", err)
	}
	if got.Name != "anthropic-secrets" || got.Namespace != "wc-org" {
		t.Fatalf("unexpected result: %+v", got)
	}
}
