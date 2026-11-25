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

package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/config"
)

// injectedProvider stands in for an overlay's provider (Cloud's SM-API).
type injectedProvider struct{ secretmanagersvc.Provider }

func TestSecretsProvider_OpenChoreoStoresOnTheConfiguredPlane(t *testing.T) {
	var gotPath string
	var gotPlane map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetPlane map[string]string `json:"targetPlane"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotPath, gotPlane = r.Method+" "+r.URL.Path, body.TargetPlane
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Secret","metadata":{"name":"x"}}`))
	}))
	t.Cleanup(srv.Close)
	cfg := config.Config{SecretTargetPlane: config.SecretTargetPlaneConfig{Kind: "DataPlane", Name: "edge"}}

	provider := secretsProvider(cfg, Seam{OpenChoreoSecrets: true}, openchoreo.Config{BaseURL: srv.URL})
	if provider == nil {
		t.Fatal("secretsProvider = nil, want the OpenChoreo provider")
	}
	client, err := secretmanagersvc.NewSecretManagementClientWithConfig(secretmanagersvc.SecretManagementClientConfig{
		StoreConfig: &secretmanagersvc.StoreConfig{}, Provider: provider,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if _, err := client.CreateSecretRef(context.Background(), secretmanagersvc.SecretLocation{
		OrgName: "ou", ControlPlaneNamespace: "acme", EntityName: "github-pat",
	}, map[string]string{"token": "t"}); err != nil {
		t.Fatalf("CreateSecretRef: %v", err)
	}
	if gotPath != "POST /api/v1alpha1/namespaces/acme/secrets" {
		t.Errorf("request = %q, want POST to OpenChoreo's Secret API in acme", gotPath)
	}
	if gotPlane["kind"] != "DataPlane" || gotPlane["name"] != "edge" {
		t.Errorf("targetPlane = %v, want DataPlane/edge from config", gotPlane)
	}
}

func TestSecretsProvider_InjectedProviderWins(t *testing.T) {
	injected := injectedProvider{}
	got := secretsProvider(config.Config{}, Seam{SecretsProvider: injected, OpenChoreoSecrets: true}, openchoreo.Config{BaseURL: "http://unused"})
	if got != (secretmanagersvc.Provider)(injected) {
		t.Fatalf("secretsProvider = %T, want the injected provider", got)
	}
}

func TestSecretsProvider_NeitherIsDeliveryOff(t *testing.T) {
	if got := secretsProvider(config.Config{}, Seam{}, openchoreo.Config{BaseURL: "http://unused"}); got != nil {
		t.Fatalf("secretsProvider = %T, want nil (delivery off)", got)
	}
}
