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

package clustergatewayproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func esManifest() map[string]any {
	return map[string]any{
		"apiVersion": "external-secrets.io/v1",
		"kind":       "ExternalSecret",
		"metadata":   map[string]any{"name": "run1-anthropic-es", "namespace": "wc-x"},
		"spec":       map[string]any{"secretStoreRef": map[string]any{"name": "default"}},
	}
}

// TestApplyExternalSecret_ConflictRecoversWithResourceVersion pins the
// queued-then-dispatched fix: when the per-run ExternalSecret already exists
// (POST → 409), the client GETs the live resourceVersion and PUTs WITH it, so
// the update no longer 422s ("resourceVersion must be specified for an update").
func TestApplyExternalSecret_ConflictRecoversWithResourceVersion(t *testing.T) {
	var sawPut bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			// The object already exists — reject with 409 (the double-apply).
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"reason":"AlreadyExists"}`))
		case r.Method == http.MethodGet:
			// Serve the live object with its resourceVersion.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"metadata":{"name":"run1-anthropic-es","resourceVersion":"424242"}}`))
		case r.Method == http.MethodPut:
			sawPut = true
			body, _ := io.ReadAll(r.Body)
			var obj struct {
				Metadata struct {
					ResourceVersion string `json:"resourceVersion"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal(body, &obj); err != nil {
				t.Errorf("PUT body not JSON: %v", err)
			}
			if obj.Metadata.ResourceVersion != "424242" {
				// This is the exact k8s failure mode the fix prevents.
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"message":"metadata.resourceVersion: Invalid value: 0x0: must be specified for an update"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"metadata":{"name":"run1-anthropic-es","resourceVersion":"424243"}}`))
		default:
			t.Errorf("unexpected method %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	if err := c.ApplyExternalSecret(context.Background(), "wc-x", esManifest()); err != nil {
		t.Fatalf("ApplyExternalSecret must recover from a 409 by injecting resourceVersion, got %v", err)
	}
	if !sawPut {
		t.Error("expected a PUT after the 409")
	}
}

// TestApplyExternalSecret_FreshPOSTNoConflict pins the happy path: a first-time
// create (POST 2xx) never GETs or PUTs.
func TestApplyExternalSecret_FreshPOSTNoConflict(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"run1-anthropic-es","resourceVersion":"1"}}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	if err := c.ApplyExternalSecret(context.Background(), "wc-x", esManifest()); err != nil {
		t.Fatalf("ApplyExternalSecret(fresh): %v", err)
	}
	if len(methods) != 1 || methods[0] != http.MethodPost {
		t.Fatalf("a fresh apply must be a single POST, got %v", methods)
	}
}

// TestApplyExternalSecret_ConflictThenPUTError surfaces a PUT failure clearly.
func TestApplyExternalSecret_ConflictThenPUTError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"metadata":{"resourceVersion":"9"}}`))
		case http.MethodPut:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	err := c.ApplyExternalSecret(context.Background(), "wc-x", esManifest())
	if err == nil || !strings.Contains(err.Error(), "PUT") {
		t.Fatalf("a failing PUT after 409 must surface, got %v", err)
	}
}

func quotaManifest() map[string]any {
	return map[string]any{
		"apiVersion": "v1",
		"kind":       "ResourceQuota",
		"metadata":   map[string]any{"name": "remote-worker-jobs", "namespace": "wc-x"},
		"spec":       map[string]any{"hard": map[string]any{"count/jobs.batch": "5"}},
	}
}

// TestApplyResourceQuota_FreshPOSTNoConflict mirrors
// TestApplyExternalSecret_FreshPOSTNoConflict for the §R3.4 quota ensure.
func TestApplyResourceQuota_FreshPOSTNoConflict(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"remote-worker-jobs","resourceVersion":"1"}}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	if err := c.ApplyResourceQuota(context.Background(), "wc-x", quotaManifest()); err != nil {
		t.Fatalf("ApplyResourceQuota: %v", err)
	}
	if gotPath != "/cloud-dp-cgw/api/v1/namespaces/wc-x/resourcequotas" {
		t.Errorf("path = %s", gotPath)
	}
}

// TestApplyResourceQuota_ConflictRecoversWithResourceVersion mirrors the
// ExternalSecret upsert-on-409 behavior for ResourceQuota.
func TestApplyResourceQuota_ConflictRecoversWithResourceVersion(t *testing.T) {
	var sawPut bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusConflict)
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"metadata":{"name":"remote-worker-jobs","resourceVersion":"7"}}`))
		case http.MethodPut:
			sawPut = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	if err := c.ApplyResourceQuota(context.Background(), "wc-x", quotaManifest()); err != nil {
		t.Fatalf("ApplyResourceQuota(conflict): %v", err)
	}
	if !sawPut {
		t.Error("expected a PUT after the 409")
	}
}

// TestApplyLimitRange_FreshPOSTNoConflict is the LimitRange sibling.
func TestApplyLimitRange_FreshPOSTNoConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"metadata":{"name":"remote-worker-limits","resourceVersion":"1"}}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	manifest := map[string]any{
		"apiVersion": "v1", "kind": "LimitRange",
		"metadata": map[string]any{"name": "remote-worker-limits", "namespace": "wc-x"},
	}
	if err := c.ApplyLimitRange(context.Background(), "wc-x", manifest); err != nil {
		t.Fatalf("ApplyLimitRange: %v", err)
	}
}

// TestApplyJob_QuotaExceeded_ReturnsErrQuotaExceeded covers the §R3.4
// retriable-error mapping: a 403 whose body is k8s's ResourceQuota rejection
// surfaces as the typed ErrQuotaExceeded sentinel, not a generic error.
func TestApplyJob_QuotaExceeded_ReturnsErrQuotaExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"jobs.batch \"run-1\" is forbidden: exceeded quota: remote-worker-jobs, requested: count/jobs.batch=1, used: count/jobs.batch=5, limited: count/jobs.batch=5"}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	manifest := map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{"name": "run-1", "namespace": "wc-x"},
	}
	err := c.ApplyJob(context.Background(), "wc-x", manifest)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("ApplyJob on quota-exceeded 403 = %v, want ErrQuotaExceeded", err)
	}
}

// TestApplyJob_ForbiddenNotQuota_PlainError covers the negative case: a 403
// that is NOT a quota rejection (e.g. RBAC) must NOT be misclassified as
// ErrQuotaExceeded — Temporal would then retry a permanently-broken RBAC
// setup forever instead of failing loudly.
func TestApplyJob_ForbiddenNotQuota_PlainError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"User \"system:serviceaccount:aep:aep-api\" cannot create resource \"jobs\""}`))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL})
	manifest := map[string]any{
		"apiVersion": "batch/v1", "kind": "Job",
		"metadata": map[string]any{"name": "run-1", "namespace": "wc-x"},
	}
	err := c.ApplyJob(context.Background(), "wc-x", manifest)
	if err == nil || errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("ApplyJob on RBAC 403 = %v, want a plain (non-quota) error", err)
	}
}
