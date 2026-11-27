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

// fakeIdP issues "tok-1" for the one client it knows, by client_secret_post.
func fakeIdP(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if r.PostForm.Get("grant_type") != "client_credentials" ||
			r.PostForm.Get("client_id") != "aectl-openchoreo-client" || r.PostForm.Get("client_secret") != "s3cret" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok-1","token_type":"Bearer","expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeSecretAPI is OpenChoreo's Secret API for one namespace: it answers 409
// for a name it already holds, replaces data on PUT, and records every
// request's method, path and bearer.
type fakeSecretAPI struct {
	mu       sync.Mutex
	secrets  map[string]map[string]string // name -> data
	planes   map[string]TargetPlane
	requests []string // "METHOD path bearer"
	status   int      // when set, every request answers it
}

func newFakeSecretAPI(t *testing.T) (*fakeSecretAPI, *httptest.Server) {
	t.Helper()
	f := &fakeSecretAPI{secrets: map[string]map[string]string{}, planes: map[string]TargetPlane{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeSecretAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
	reply := func(status int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
	if f.status != 0 {
		reply(f.status, map[string]string{"error": "refused by the fake"})
		return
	}
	const prefix = "/api/v1alpha1/namespaces/default/secrets"
	switch {
	case r.Method == http.MethodPost && r.URL.Path == prefix:
		var body struct {
			SecretName  string            `json:"secretName"`
			SecretType  string            `json:"secretType"`
			Data        map[string]string `json:"data"`
			TargetPlane TargetPlane       `json:"targetPlane"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := f.secrets[body.SecretName]; ok {
			reply(http.StatusConflict, map[string]string{"error": "already exists"})
			return
		}
		if body.SecretType != "Opaque" {
			reply(http.StatusBadRequest, map[string]string{"error": "secretType must be Opaque"})
			return
		}
		f.secrets[body.SecretName], f.planes[body.SecretName] = body.Data, body.TargetPlane
		reply(http.StatusCreated, map[string]string{"kind": "Secret"})
	case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, prefix+"/"):
		name := strings.TrimPrefix(r.URL.Path, prefix+"/")
		if _, ok := f.secrets[name]; !ok {
			reply(http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		var body struct {
			Data map[string]string `json:"data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.secrets[name] = body.Data
		reply(http.StatusOK, map[string]string{"kind": "Secret"})
	default:
		reply(http.StatusNotFound, map[string]string{"error": "no route " + r.Method + " " + r.URL.Path})
	}
}

func newTestClient(t *testing.T, idp, api *httptest.Server) *Client {
	t.Helper()
	return &Client{
		BaseURL: api.URL,
		Token: ClientCredentials{
			TokenURL: idp.URL, ClientID: "aectl-openchoreo-client", ClientSecret: "s3cret",
		},
	}
}

var plane = TargetPlane{Kind: "ClusterDataPlane", Name: "default"}

func TestPutSecret_CreatesWithTheClientsToken(t *testing.T) {
	api, apiSrv := newFakeSecretAPI(t)
	c := newTestClient(t, fakeIdP(t), apiSrv)

	if err := c.PutSecret(context.Background(), "default", "thunder-binding-default-development", plane,
		map[string]string{"clientId": "aep-system-client", "clientSecret": "v1"}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	got := api.secrets["thunder-binding-default-development"]
	if got["clientId"] != "aep-system-client" || got["clientSecret"] != "v1" || len(got) != 2 {
		t.Errorf("stored data = %v", got)
	}
	if api.planes["thunder-binding-default-development"] != plane {
		t.Errorf("targetPlane = %+v, want %+v", api.planes["thunder-binding-default-development"], plane)
	}
	if len(api.requests) != 1 || api.requests[0] != "POST /api/v1alpha1/namespaces/default/secrets Bearer tok-1" {
		t.Errorf("requests = %v, want one POST carrying the client's token", api.requests)
	}
}

func TestPutSecret_ExistingSecretIsUpdated(t *testing.T) {
	api, apiSrv := newFakeSecretAPI(t)
	api.secrets["thunder-binding-default-development"] = map[string]string{"clientId": "aep-system-client", "clientSecret": "old"}
	c := newTestClient(t, fakeIdP(t), apiSrv)

	if err := c.PutSecret(context.Background(), "default", "thunder-binding-default-development", plane,
		map[string]string{"clientId": "aep-system-client", "clientSecret": "new"}); err != nil {
		t.Fatalf("PutSecret: %v", err)
	}
	if got := api.secrets["thunder-binding-default-development"]["clientSecret"]; got != "new" {
		t.Errorf("clientSecret = %q, want new", got)
	}
	want := []string{
		"POST /api/v1alpha1/namespaces/default/secrets Bearer tok-1",
		"PUT /api/v1alpha1/namespaces/default/secrets/thunder-binding-default-development Bearer tok-1",
	}
	if strings.Join(api.requests, "|") != strings.Join(want, "|") {
		t.Errorf("requests = %v, want %v", api.requests, want)
	}
}

func TestPutSecret_RefusalIsAnErrorWithOpenChoreosMessage(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		api, apiSrv := newFakeSecretAPI(t)
		api.status = status
		c := newTestClient(t, fakeIdP(t), apiSrv)

		err := c.PutSecret(context.Background(), "default", "x", plane, map[string]string{"k": "v"})
		if err == nil || !strings.Contains(err.Error(), "refused by the fake") || !strings.Contains(err.Error(), http.StatusText(status)) {
			t.Errorf("status %d: err = %v, want the status and OpenChoreo's message", status, err)
		}
	}
}

func TestPutSecret_RefusedTokenIsAnErrorAndNothingIsSent(t *testing.T) {
	api, apiSrv := newFakeSecretAPI(t)
	c := newTestClient(t, fakeIdP(t), apiSrv)
	c.Token = ClientCredentials{TokenURL: c.Token.(ClientCredentials).TokenURL, ClientID: "aectl-openchoreo-client", ClientSecret: "wrong"}

	err := c.PutSecret(context.Background(), "default", "x", plane, map[string]string{"k": "v"})
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Fatalf("err = %v, want the IdP's refusal", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("OpenChoreo got %v without a token", api.requests)
	}
}
