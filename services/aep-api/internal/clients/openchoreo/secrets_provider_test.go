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
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
)

// fakeSecretAPI is an in-memory stand-in for OpenChoreo's Secret API
// (/api/v1alpha1/namespaces/{ns}/secrets[/{name}]). It keeps what was stored
// per namespace and name, so tests assert on what OpenChoreo ends up holding.
type fakeSecretAPI struct {
	mu      sync.Mutex
	secrets map[string]storedSecret // key: ns + "/" + name
	// takenCreates answers that many POSTs with 409, as if each name were
	// already in use by another secret.
	takenCreates int
	// halfDoneCreates fails that many POSTs with 500 AFTER the value is
	// copied to the plane but BEFORE the SecretReference exists, as
	// OpenChoreo's CreateSecret can. The copy is then invisible to DELETE
	// (no reference → 404) until a repeated POST of the same name
	// completes it.
	halfDoneCreates int
	orphans         map[string]storedSecret // plane copies with no reference
	// onHalfDone, when set, runs as a half-done create fails (e.g. to cancel
	// the caller's context, as a request timeout would).
	onHalfDone func()
}

type storedSecret struct {
	SecretType  string
	Data        map[string]string
	TargetPlane map[string]string
}

func newFakeSecretAPI(t *testing.T) (*fakeSecretAPI, *httptest.Server) {
	t.Helper()
	f := &fakeSecretAPI{secrets: map[string]storedSecret{}, orphans: map[string]storedSecret{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeSecretAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1alpha1/namespaces/")
	if !ok {
		replyJSON(w, http.StatusNotFound, map[string]string{"error": "no route " + r.URL.Path})
		return
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "secrets" {
		replyJSON(w, http.StatusNotFound, map[string]string{"error": "no route " + r.URL.Path})
		return
	}
	ns := parts[0]
	switch {
	case r.Method == http.MethodPost && len(parts) == 2:
		var body struct {
			SecretName  string            `json:"secretName"`
			SecretType  string            `json:"secretType"`
			Data        map[string]string `json:"data"`
			TargetPlane map[string]string `json:"targetPlane"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			replyJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		key := ns + "/" + body.SecretName
		if f.takenCreates > 0 {
			f.takenCreates--
			replyJSON(w, http.StatusConflict, map[string]string{"error": "already exists"})
			return
		}
		if _, exists := f.secrets[key]; exists {
			replyJSON(w, http.StatusConflict, map[string]string{"error": "already exists"})
			return
		}
		stored := storedSecret{SecretType: body.SecretType, Data: body.Data, TargetPlane: body.TargetPlane}
		if f.halfDoneCreates > 0 {
			f.halfDoneCreates--
			f.orphans[key] = stored
			if f.onHalfDone != nil {
				f.onHalfDone()
			}
			replyJSON(w, http.StatusInternalServerError, map[string]string{"error": "create secret reference failed"})
			return
		}
		delete(f.orphans, key)
		f.secrets[key] = stored
		replyJSON(w, http.StatusCreated, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": body.SecretName, "namespace": ns}})
	case r.Method == http.MethodPut && len(parts) == 3:
		key := ns + "/" + parts[2]
		existing, exists := f.secrets[key]
		if !exists {
			replyJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			replyJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		existing.Data = body.Data // the API replaces data, it does not merge
		f.secrets[key] = existing
		replyJSON(w, http.StatusOK, map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{"name": parts[2], "namespace": ns}})
	case r.Method == http.MethodDelete && len(parts) == 3:
		key := ns + "/" + parts[2]
		if _, exists := f.secrets[key]; !exists {
			replyJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		delete(f.secrets, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		replyJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": r.Method + " " + r.URL.Path})
	}
}

func (f *fakeSecretAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.secrets)
}

func (f *fakeSecretAPI) orphanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.orphans)
}

func (f *fakeSecretAPI) stored(ns, name string) (storedSecret, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.secrets[ns+"/"+name]
	return s, ok
}

func replyJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func newSecretClientOverOC(t *testing.T, srv *httptest.Server) secretmanagersvc.SecretManagementClient {
	t.Helper()
	provider := NewSecretsProvider(Config{BaseURL: srv.URL}, SecretTargetPlane{Kind: "ClusterDataPlane", Name: "default"})
	client, err := secretmanagersvc.NewSecretManagementClientWithConfig(secretmanagersvc.SecretManagementClientConfig{
		StoreConfig: &secretmanagersvc.StoreConfig{},
		Provider:    provider,
	})
	if err != nil {
		t.Fatalf("build secret management client: %v", err)
	}
	return client
}

func orgSecretLocation() secretmanagersvc.SecretLocation {
	return secretmanagersvc.SecretLocation{
		OrgName:               "6f1c2b3a-0000-4000-8000-000000000001",
		ControlPlaneNamespace: "acme",
		EntityName:            "github-pat",
	}
}

func TestSecretsOverOpenChoreo_CreateSecretRef_StoresValueUnderNewName(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	client := newSecretClientOverOC(t, srv)

	name, err := client.CreateSecretRef(context.Background(), orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if name == "" {
		t.Fatal("CreateSecretRef returned no name")
	}

	got, ok := api.stored("acme", name)
	if !ok {
		t.Fatalf("OpenChoreo holds no secret acme/%s", name)
	}
	if got.Data["token"] != "ghp_example" || len(got.Data) != 1 {
		t.Errorf("stored data = %v, want {token: ghp_example}", got.Data)
	}
	if got.SecretType != "Opaque" {
		t.Errorf("secretType = %q, want Opaque", got.SecretType)
	}
	if got.TargetPlane["kind"] != "ClusterDataPlane" || got.TargetPlane["name"] != "default" {
		t.Errorf("targetPlane = %v, want ClusterDataPlane/default", got.TargetPlane)
	}
}

func TestSecretsOverOpenChoreo_CreateSecretRef_NameTakenTriesAnotherName(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	api.takenCreates = 1
	client := newSecretClientOverOC(t, srv)

	name, err := client.CreateSecretRef(context.Background(), orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if got, ok := api.stored("acme", name); !ok || got.Data["token"] != "ghp_example" {
		t.Fatalf("OpenChoreo holds %v (found=%v) under %q, want {token: ghp_example}", got.Data, ok, name)
	}
	if n := api.count(); n != 1 {
		t.Errorf("OpenChoreo holds %d secrets, want 1", n)
	}
}

func TestSecretsOverOpenChoreo_DeleteSecretRef_RemovesTheNamedSecret(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	client := newSecretClientOverOC(t, srv)
	ctx := context.Background()
	name, err := client.CreateSecretRef(ctx, orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}

	if err := client.DeleteSecretRef(ctx, orgSecretLocation(), name); err != nil {
		t.Fatalf("DeleteSecretRef: %v", err)
	}
	if _, ok := api.stored("acme", name); ok {
		t.Errorf("OpenChoreo still holds acme/%s", name)
	}
}

func TestSecretsOverOpenChoreo_DeleteSecretRef_AlreadyGoneIsSuccess(t *testing.T) {
	_, srv := newFakeSecretAPI(t)
	client := newSecretClientOverOC(t, srv)

	if err := client.DeleteSecretRef(context.Background(), orgSecretLocation(), "acme-github-pat-00000000"); err != nil {
		t.Fatalf("DeleteSecretRef of a missing secret: %v, want nil", err)
	}
}

func TestSecretsOverOpenChoreo_CreateSecretRef_FailedHalfwayLeavesNothingBehind(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	api.halfDoneCreates = 1
	client := newSecretClientOverOC(t, srv)

	_, err := client.CreateSecretRef(context.Background(), orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if err == nil {
		t.Fatal("CreateSecretRef succeeded, want the failure reported")
	}
	if n := api.count(); n != 0 {
		t.Errorf("OpenChoreo holds %d secrets, want 0", n)
	}
	if n := api.orphanCount(); n != 0 {
		t.Errorf("OpenChoreo holds %d half-written copies, want 0", n)
	}
}

func TestSecretsOverOpenChoreo_CreateSecretRef_CancelledHalfwayLeavesNothingBehind(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	api.halfDoneCreates = 1
	api.onHalfDone = cancel // the caller gives up while OpenChoreo is half done
	client := newSecretClientOverOC(t, srv)

	_, err := client.CreateSecretRef(ctx, orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if err == nil {
		t.Fatal("CreateSecretRef succeeded, want the failure reported")
	}
	if n := api.count(); n != 0 {
		t.Errorf("OpenChoreo holds %d secrets, want 0", n)
	}
	if n := api.orphanCount(); n != 0 {
		t.Errorf("OpenChoreo holds %d half-written copies, want 0", n)
	}
}

func ampKeyLocation() secretmanagersvc.SecretLocation {
	return secretmanagersvc.SecretLocation{
		OrgName:               "6f1c2b3a-0000-4000-8000-000000000001",
		ControlPlaneNamespace: "acme",
		RefName:               "",                      // fixed name derived from the location
		EntityName:            "amp-model-billing-dev", // ampModelKeyEntity("billing", "dev")
	}
}

func TestSecretsOverOpenChoreo_CreateSecret_FixedNameSavedAgainReplacesValue(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	client := newSecretClientOverOC(t, srv)
	ctx := context.Background()

	first, err := client.CreateSecret(ctx, ampKeyLocation(), map[string]string{"api-key": "old", "url": "https://proxy/a"})
	if err != nil {
		t.Fatalf("first CreateSecret: %v", err)
	}
	second, err := client.CreateSecret(ctx, ampKeyLocation(), map[string]string{"api-key": "new"})
	if err != nil {
		t.Fatalf("second CreateSecret: %v", err)
	}
	if first != second {
		t.Fatalf("names differ: %q then %q, want the same fixed name", first, second)
	}
	got, ok := api.stored("acme", second)
	if !ok {
		t.Fatalf("OpenChoreo holds no secret acme/%s", second)
	}
	if len(got.Data) != 1 || got.Data["api-key"] != "new" {
		t.Errorf("stored data = %v, want exactly {api-key: new}", got.Data)
	}
}

func TestSecretsOverOpenChoreo_CreateSecretRef_NameTakenTwiceIsAConflict(t *testing.T) {
	api, srv := newFakeSecretAPI(t)
	api.takenCreates = 2
	client := newSecretClientOverOC(t, srv)

	_, err := client.CreateSecretRef(context.Background(), orgSecretLocation(), map[string]string{"token": "ghp_example"})
	if !errors.Is(err, secretmanagersvc.ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if n := api.count(); n != 0 {
		t.Errorf("OpenChoreo holds %d secrets, want 0", n)
	}
}
