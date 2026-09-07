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

package secrets

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNewDeliveryKV_RequiresAddrAndToken(t *testing.T) {
	if _, err := NewDeliveryKV("", "tok", "secret"); err == nil {
		t.Fatal("empty addr: want error")
	}
	if _, err := NewDeliveryKV("http://localhost", "", "secret"); err == nil {
		t.Fatal("empty token: want error")
	}
}

func TestDeliveryKV_Put_PathConstruction(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}

	secretPath := "user-app-secrets/wc-abc12345-deadbeef/anthropic-secrets"
	data := map[string]string{"api-key": "sk-test"}
	if err := kv.Put(context.Background(), secretPath, data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	wantPath := "/v1/secret/data/" + secretPath
	if gotPath != wantPath {
		t.Errorf("request path = %q; want %q", gotPath, wantPath)
	}
	if gotMethod != http.MethodPut && gotMethod != http.MethodPost {
		t.Errorf("method = %q; want PUT or POST", gotMethod)
	}
	inner, ok := gotBody["data"].(map[string]any)
	if !ok {
		t.Fatalf("body missing KV-v2 data wrap: %#v", gotBody)
	}
	if inner["api-key"] != "sk-test" {
		t.Errorf("data[api-key] = %v; want sk-test", inner["api-key"])
	}
}

func TestDeliveryKV_Delete_PathConstruction(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}

	secretPath := "user-app-secrets/wc-abc12345-deadbeef/anthropic-secrets"
	if err := kv.Delete(context.Background(), secretPath); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	wantPath := "/v1/secret/metadata/" + secretPath
	if gotPath != wantPath {
		t.Errorf("request path = %q; want %q", gotPath, wantPath)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q; want DELETE", gotMethod)
	}
}

func TestDeliveryKV_Delete_IdempotentOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":["not found"]}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	if err := kv.Delete(context.Background(), "user-app-secrets/wc-x/y"); err != nil {
		t.Fatalf("Delete 404: %v; want nil (idempotent)", err)
	}
}

func TestDeliveryKV_Put_ErrorOmitsSecretValues(t *testing.T) {
	const secretValue = "super-secret-value-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// 400 avoids vault client retries on 5xx.
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["bad request"]}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	err = kv.Put(context.Background(), "user-app-secrets/wc-x/y", map[string]string{
		"api-key": secretValue,
	})
	if err == nil {
		t.Fatal("Put: want error")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Errorf("error leaked secret value: %v", err)
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Errorf("error leaked token: %v", err)
	}
}

func TestDeliveryKV_Put_RejectsEmptyOrAbsolutePath(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "tok", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	for _, p := range []string{"", "/abs/path"} {
		if err := kv.Put(context.Background(), p, map[string]string{"k": "v"}); err == nil {
			t.Errorf("Put(%q): want error", p)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 0 {
		t.Errorf("vault was hit %d times; want 0 for rejected paths", hits)
	}
}

// Get is the ONE read on this helper, and it exists for the environment-tier
// Thunder binding: aep-api runs outside the cluster, so the admin credential
// for an environment's identity provider reaches it only through OpenBao.
func TestDeliveryKV_Get_ReadsTheKVv2Fields(t *testing.T) {
	var gotPath, gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotToken = r.URL.Path, r.Header.Get("X-Vault-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"data":{"clientId":"aep-system-client","clientSecret":"s3cret"},` +
			`"metadata":{"version":1}}}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}

	got, err := kv.Get(context.Background(), "aep/thunder/acme/default")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if gotPath != "/v1/secret/data/aep/thunder/acme/default" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotToken != "test-token" {
		t.Errorf("token header = %q", gotToken)
	}
	if got["clientId"] != "aep-system-client" || got["clientSecret"] != "s3cret" {
		t.Fatalf("fields = %#v; want the credential's two keys", got)
	}
}

// A path with no secret is (nil, nil): absence is an answer the caller branches
// on — "this environment's binding was never written" — not a failure to retry.
func TestDeliveryKV_Get_AbsentIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[]}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	got, err := kv.Get(context.Background(), "aep/thunder/acme/nowhere")
	if err != nil {
		t.Fatalf("an absent secret must not be an error: %v", err)
	}
	if got != nil {
		t.Fatalf("fields = %#v; want nil", got)
	}
}

// A KV-v1 mount answers without the "data" wrap. Read as v2 it would yield an
// empty map and the caller would report a missing credential with no cause, so
// the shape is checked rather than assumed.
func TestDeliveryKV_Get_RejectsANonKVv2Shape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"clientId":"aep-system-client"}}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	if _, err := kv.Get(context.Background(), "aep/thunder/acme/default"); err == nil {
		t.Fatal("a KV-v1 response was read as KV-v2")
	}
}

// The error names neither the value nor the token — this helper's whole error
// discipline, and the read must not be the one path that breaks it.
func TestDeliveryKV_Get_ErrorCarriesNoSecretMaterial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"errors":["something with the token test-token in it"]}`))
	}))
	t.Cleanup(srv.Close)

	kv, err := NewDeliveryKV(srv.URL, "test-token", "secret")
	if err != nil {
		t.Fatalf("NewDeliveryKV: %v", err)
	}
	_, err = kv.Get(context.Background(), "aep/thunder/acme/default")
	if err == nil {
		t.Fatal("a 500 must be an error")
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("the error echoes the auth token: %v", err)
	}
}
