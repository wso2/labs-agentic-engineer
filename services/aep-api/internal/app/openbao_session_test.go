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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/openbao"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// One OpenBao session per process. The OpenBao-direct provider and the
// environment Thunder binding reader are built with the same VaultAuth, so a
// write and a read log in once between them.
func TestEnvironmentThunderCredentials_SharesTheProcessSession(t *testing.T) {
	var mu sync.Mutex
	logins := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/v1/auth/kubernetes/login":
			logins++
			_, _ = w.Write([]byte(`{"auth":{"client_token":"tok","lease_duration":3600}}`))
		case strings.HasPrefix(r.URL.Path, "/v1/secret/data/aep/thunder/"):
			_, _ = w.Write([]byte(`{"data":{"data":{"clientId":"aep-system-client"}}}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	tokenFile := filepath.Join(t.TempDir(), "sa-token")
	if err := os.WriteFile(tokenFile, []byte("sa-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	auth := secrets.NewKubernetesAuth("aep-api", "kubernetes", tokenFile)
	cfg := config.Config{OpenBaoAddr: srv.URL}

	provider, err := openbao.NewProvider(deliveryOpenBaoConfigFromAppConfig(cfg), auth)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := environmentThunderCredentials(cfg, auth)
	if err != nil || reader == nil {
		t.Fatalf("reader = %v, err = %v; want a reader", reader, err)
	}
	client, err := provider.NewClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.PushSecret(context.Background(), secretmanagersvc.SecretLocation{OrgName: "ou-1", EntityName: "anthropic"}, []byte(`{"api-key":"v"}`), nil); err != nil {
		t.Fatalf("PushSecret: %v", err)
	}
	if _, err := reader.ReadBindingCredential(context.Background(), "secret/aep/thunder/acme/default"); err != nil {
		t.Fatalf("ReadBindingCredential: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if logins != 1 {
		t.Fatalf("logins = %d for one write and one read; want one shared session", logins)
	}
}

// No session, no reader: the binding reader exists only beside the
// OpenBao-direct provider NewOSSOptions builds.
func TestEnvironmentThunderCredentials_NoSessionNoReader(t *testing.T) {
	for name, tc := range map[string]struct {
		addr string
		auth secrets.VaultAuth
	}{
		"no address": {"", secrets.NewKubernetesAuth("aep-api", "kubernetes", "")},
		"no session": {"http://openbao.invalid", nil},
	} {
		t.Run(name, func(t *testing.T) {
			reader, err := environmentThunderCredentials(config.Config{OpenBaoAddr: tc.addr}, tc.auth)
			if err != nil || reader != nil {
				t.Fatalf("reader = %v, err = %v; want (nil, nil)", reader, err)
			}
		})
	}
}
