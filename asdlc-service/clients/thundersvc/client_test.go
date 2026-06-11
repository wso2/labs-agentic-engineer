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

package thundersvc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// thunderMock is a minimal in-memory Thunder admin API for exercising
// EnsurePublisherApp's OU self-heal without a live cluster.
type thunderMock struct {
	appID    string // internal id of the existing publisher app ("" = none)
	appName  string
	clientID string
	ouID     string // OU the existing app is registered under

	deleted      bool
	deleteStatus int    // override delete response code (0 = 204); app is removed regardless
	createdOU    string // OU passed to the create call
	createCount  int
}

func (m *thunderMock) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok", "expires_in": 3600})

		case r.Method == http.MethodGet && r.URL.Path == "/applications":
			var apps []map[string]any
			if m.appID != "" {
				apps = append(apps, map[string]any{"id": m.appID, "name": m.appName, "clientId": m.clientID})
			}
			_ = json.NewEncoder(w).Encode(apps)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/applications/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": m.appID, "name": m.appName, "ouId": m.ouID,
			})

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/applications/"):
			m.deleted = true
			m.appID = "" // removed server-side regardless of the response code
			if m.deleteStatus != 0 {
				w.WriteHeader(m.deleteStatus)
				return
			}
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && r.URL.Path == "/applications":
			var body struct {
				OuID string `json:"ouId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.createdOU = body.OuID
			m.createCount++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"clientId": m.appName, "clientSecret": "fresh-secret",
			})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func newTestClient(base string) *client {
	return New(Config{BaseURL: base, ClientID: "sys", ClientSecret: "sec"}).(*client)
}

// Wrong OU → delete + recreate under the org OU, created=true, secret rotated.
func TestEnsurePublisherApp_HealsWrongOU(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "asdlc-publisher-org1", clientID: "asdlc-publisher-org1", ouID: "default-ou"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	id, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.deleted {
		t.Error("expected the wrong-OU app to be deleted")
	}
	if m.createdOU != "org-ou-1" {
		t.Errorf("recreated under OU %q, want org-ou-1", m.createdOU)
	}
	if !created || secret != "fresh-secret" {
		t.Errorf("want created=true with rotated secret, got created=%v secret=%q", created, secret)
	}
	if id != "asdlc-publisher-org1" {
		t.Errorf("client_id changed: got %q", id)
	}
}

// Wrong OU + Thunder returns 500 on delete but the app is actually gone →
// the heal must still recreate under the org OU (one-pass durability).
func TestEnsurePublisherApp_HealsWrongOU_DeleteReturns500ButGone(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "asdlc-publisher-org1", clientID: "asdlc-publisher-org1", ouID: "default-ou", deleteStatus: 500}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, secret, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("heal must tolerate a 500-but-deleted delete, got error: %v", err)
	}
	if m.createdOU != "org-ou-1" || !created || secret != "fresh-secret" {
		t.Errorf("want recreate under org-ou-1 (created+secret), got ou=%q created=%v secret=%q", m.createdOU, created, secret)
	}
}

// Correct OU → no delete, no recreate, created=false.
func TestEnsurePublisherApp_CorrectOU_NoHeal(t *testing.T) {
	m := &thunderMock{appID: "app-1", appName: "asdlc-publisher-org1", clientID: "asdlc-publisher-org1", ouID: "org-ou-1"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, _, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.deleted || m.createCount != 0 {
		t.Errorf("must not heal when OU already matches (deleted=%v creates=%d)", m.deleted, m.createCount)
	}
	if created {
		t.Error("want created=false for an existing correct-OU app")
	}
}

// No existing app + org OU known → create under the org OU.
func TestEnsurePublisherApp_CreatesUnderOrgOU(t *testing.T) {
	m := &thunderMock{appName: "asdlc-publisher-org1"}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, _, created, err := c.EnsurePublisherApp(context.Background(), "org1", "org-ou-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created || m.createdOU != "org-ou-1" {
		t.Errorf("want fresh create under org-ou-1, got created=%v ou=%q", created, m.createdOU)
	}
	if m.deleted {
		t.Error("must not delete when no app existed")
	}
}
