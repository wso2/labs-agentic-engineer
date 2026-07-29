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

package openbao_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/openbao"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

func TestProvider_CompileAsserts(t *testing.T) {
	var _ secretsprovider.Provider = (*openbao.Provider)(nil)
	var _ secretsprovider.SecretReferenceManager = (*openbao.Provider)(nil)
	var _ secretsprovider.SecretsClient = (*openbao.Client)(nil)
}

func TestProvider_Capabilities_WriteOnly(t *testing.T) {
	p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
		Server: "http://example.invalid",
		Path:   "secret",
		Auth:   &secretsprovider.OpenBaoAuth{Token: "tok"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if got := p.Capabilities(); got != secretsprovider.StoreCapabilityWriteOnly {
		t.Fatalf("Capabilities = %q; want WriteOnly", got)
	}
}

func TestProvider_ManagesSecretReferences_False(t *testing.T) {
	p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
		Server: "http://example.invalid",
		Path:   "secret",
		Auth:   &secretsprovider.OpenBaoAuth{Token: "tok"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p.ManagesSecretReferences() {
		t.Fatal("ManagesSecretReferences = true; want false")
	}
}

func TestClient_PushSecret_ReturnsVaultPath(t *testing.T) {
	const orgUUID = "550e8400-e29b-41d4-a716-446655440000"
	ns := tenant.OrgBaseNamespace(orgUUID)

	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)

	p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
		Server: srv.URL,
		Path:   "secret",
		Auth:   &secretsprovider.OpenBaoAuth{Token: "tok"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	client, err := p.NewClient(nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	loc := secretsprovider.SecretLocation{
		OrgName:    orgUUID,
		EntityName: "anthropic",
	}
	value, _ := json.Marshal(map[string]string{"api-key": "sk-test"})
	ref, err := client.PushSecret(context.Background(), loc, value, nil)
	if err != nil {
		t.Fatalf("PushSecret: %v", err)
	}

	wantName := loc.SecretRefName()
	wantRef := "user-app-secrets/" + ns + "/" + wantName
	if ref != wantRef {
		t.Fatalf("PushSecret ref = %q; want %q", ref, wantRef)
	}
	wantVault := "/v1/secret/data/" + wantRef
	if gotPath != wantVault {
		t.Errorf("vault request path = %q; want %q", gotPath, wantVault)
	}
	inner, _ := gotBody["data"].(map[string]any)
	if inner["api-key"] != "sk-test" {
		t.Errorf("vault data = %#v; want api-key=sk-test", gotBody)
	}
}

func TestClient_GetSecretWithValue_NotSupported(t *testing.T) {
	p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
		Server: "http://example.invalid",
		Path:   "secret",
		Auth:   &secretsprovider.OpenBaoAuth{Token: "tok"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	client, err := p.NewClient(nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.GetSecretWithValue(context.Background(), secretsprovider.SecretLocation{
		OrgName: "org", EntityName: "anthropic",
	})
	if !errors.Is(err, secretsprovider.ErrNotSupported) {
		t.Fatalf("GetSecretWithValue = %v; want ErrNotSupported", err)
	}
}

func TestClient_PushSecret_ErrorOmitsSecretValues(t *testing.T) {
	const secretValue = "super-secret-value-do-not-leak"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":["bad"]}`))
	}))
	t.Cleanup(srv.Close)

	p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
		Server: srv.URL,
		Path:   "secret",
		Auth:   &secretsprovider.OpenBaoAuth{Token: "tok"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	client, err := p.NewClient(nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	value, _ := json.Marshal(map[string]string{"api-key": secretValue})
	_, err = client.PushSecret(context.Background(), secretsprovider.SecretLocation{
		OrgName: "550e8400-e29b-41d4-a716-446655440000", EntityName: "anthropic",
	}, value, nil)
	if err == nil {
		t.Fatal("PushSecret: want error")
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Errorf("error leaked secret value: %v", err)
	}
}
