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
	"sync/atomic"
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

func TestEnsureComponentType_ConflictRefetches(t *testing.T) {
	var posts, gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodPost:
			atomic.AddInt32(&posts, 1)
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "already exists"})
		case http.MethodGet:
			atomic.AddInt32(&gets, 1)
			if r.URL.Path != "/api/v1/namespaces/wc-abc/componenttypes/"+CodingAgentComponentTypeName {
				t.Errorf("unexpected GET path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"apiVersion": "openchoreo.dev/v1alpha1",
				"kind":       "ComponentType",
				"metadata":   map[string]any{"name": CodingAgentComponentTypeName},
			})
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c := NewComponentClient(Config{BaseURL: srv.URL})
	err := c.EnsureComponentType(context.Background(), "wc-abc", CodingAgentComponentType())
	if err != nil {
		t.Fatalf("EnsureComponentType: %v", err)
	}
	if posts != 1 || gets != 1 {
		t.Errorf("expected 1 POST + 1 GET, got posts=%d gets=%d", posts, gets)
	}
}
