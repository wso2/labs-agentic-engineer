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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// setOSSEnv sets the variables config.Load requires.
func setOSSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENV_FILE_PATH", "")
	t.Setenv("PLATFORM_API_SERVICE_BASE_URL", "http://platform-api.invalid")
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("JWKS_URL", "http://idp.invalid/jwks")
}

// With OPENBAO_ADDR set, NewOSSOptions builds the OpenBao-direct provider and
// the one session it shares with Assemble's binding reader, and talks to
// OpenBao not at all: aep-api boots with OpenBao down or its role missing.
func TestNewOSSOptions_OpenBaoSessionIsBuiltLazily(t *testing.T) {
	setOSSEnv(t)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENBAO_ADDR", srv.URL)
	t.Setenv("OPENBAO_AUTH_TOKEN_PATH", "/nonexistent/token")

	opts, err := NewOSSOptions()
	if err != nil {
		t.Fatalf("NewOSSOptions: %v", err)
	}
	if opts.SecretsProvider == nil || opts.openBaoAuth == nil {
		t.Fatalf("provider = %v, session = %v; want both", opts.SecretsProvider, opts.openBaoAuth)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("OpenBao was called %d times at boot; want 0", n)
	}
}

func TestNewOSSOptions_NoOpenBaoAddrNoProviderNoSession(t *testing.T) {
	setOSSEnv(t)
	t.Setenv("OPENBAO_ADDR", "")
	opts, err := NewOSSOptions()
	if err != nil {
		t.Fatalf("NewOSSOptions: %v", err)
	}
	if opts.SecretsProvider != nil || opts.openBaoAuth != nil {
		t.Fatalf("provider = %v, session = %v; want neither", opts.SecretsProvider, opts.openBaoAuth)
	}
}
