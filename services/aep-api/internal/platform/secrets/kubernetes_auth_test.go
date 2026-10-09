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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

// writeTemp writes content to a 0600 file in the test's temp dir and returns
// its path: the stand-in for the projected service-account token.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeOpenBao answers the Kubernetes-auth login with tok-<n> (lease 3600 s)
// and accepts every KV write, except that forbid (when set) can refuse a write
// the way OpenBao refuses a revoked or expired token.
type fakeOpenBao struct {
	mu       sync.Mutex
	logins   int
	writes   int
	loginReq map[string]any
	forbid   func(token string, write int) bool
	srv      *httptest.Server
}

func newFakeOpenBao(t *testing.T) *fakeOpenBao {
	t.Helper()
	f := &fakeOpenBao{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/auth/kubernetes/login":
			f.logins++
			_ = json.NewDecoder(r.Body).Decode(&f.loginReq)
			_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{
				"client_token": fmt.Sprintf("tok-%d", f.logins), "lease_duration": 3600,
			}})
		case strings.HasPrefix(r.URL.Path, "/v1/secret/data/user-app-secrets/"):
			f.writes++
			if f.forbid != nil && f.forbid(r.Header.Get("X-Vault-Token"), f.writes) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOpenBao) counts() (logins, writes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.logins, f.writes
}

// A token minted at boot expires after the role's ttl; the
// session must re-login on the first 403 and before it runs out, never answer
// a write with a 403 because its token aged.
func TestKubernetesAuth_ReloginsOn403AndNearExpiry(t *testing.T) {
	t.Run("a 403 re-logs in once and retries the write", func(t *testing.T) {
		f := newFakeOpenBao(t)
		f.forbid = func(token string, write int) bool { return token == "tok-1" && write == 2 } // the first token was revoked
		tokenFile := writeTemp(t, "sa-token")
		kv, err := NewDeliveryKV(f.srv.URL, "secret", NewKubernetesAuth("aep-api", "kubernetes", tokenFile))
		if err != nil {
			t.Fatal(err)
		}
		_ = kv.Put(ctx, "user-app-secrets/ns/a", map[string]string{"k": "v"})
		if err := kv.Put(ctx, "user-app-secrets/ns/b", map[string]string{"k": "v"}); err != nil {
			t.Fatalf("second write after a 403: %v", err)
		}
		if logins, writes := f.counts(); logins != 2 || writes != 3 {
			t.Fatalf("logins = %d, writes = %d; want a re-login on 403 and one retry (2, 3)", logins, writes)
		}
	})

	t.Run("less than a third of the TTL left re-logs in before the write", func(t *testing.T) {
		f := newFakeOpenBao(t)
		f.forbid = func(token string, _ int) bool { return token != fmt.Sprintf("tok-%d", f.logins) } // only the newest token works
		auth := NewKubernetesAuth("aep-api", "kubernetes", writeTemp(t, "sa-token"))
		clock := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
		auth.(*vaultSession).now = func() time.Time { return clock }
		kv, err := NewDeliveryKV(f.srv.URL, "secret", auth)
		if err != nil {
			t.Fatal(err)
		}
		put := func() {
			t.Helper()
			if err := kv.Put(ctx, "user-app-secrets/ns/a", map[string]string{"k": "v"}); err != nil {
				t.Fatalf("Put: %v", err)
			}
		}
		put()
		clock = clock.Add(39 * time.Minute) // 21 min of 60 left: more than a third
		put()
		if logins, _ := f.counts(); logins != 1 {
			t.Fatalf("logins = %d with 21 of 60 minutes left; want the boot token kept", logins)
		}
		clock = clock.Add(2 * time.Minute) // 19 min left: less than a third
		put()
		if logins, writes := f.counts(); logins != 2 || writes != 3 {
			t.Fatalf("logins = %d, writes = %d; want a re-login BEFORE the write, no 403 round trip (2, 3)", logins, writes)
		}
	})
}

func TestNewDeliveryKV_NoStaticTokenPath(t *testing.T) {
	if _, err := NewDeliveryKV("http://x", "secret", nil); err == nil {
		t.Fatal("an auth source is required")
	}
}

// The login is lazy: aep-api boots with OpenBao unreachable or its role
// missing, and builds the client without a network call.
func TestKubernetesAuth_LoginIsLazy(t *testing.T) {
	f := newFakeOpenBao(t)
	if _, err := NewDeliveryKV(f.srv.URL, "secret", NewKubernetesAuth("aep-api", "kubernetes", "/nonexistent/token")); err != nil {
		t.Fatalf("construction must not need the token file or OpenBao: %v", err)
	}
	if logins, _ := f.counts(); logins != 0 {
		t.Fatalf("logins = %d at construction; want 0", logins)
	}
}

// The login request is the documented Kubernetes-auth shape: the role and the
// projected token's contents, under auth/<mount>/login.
func TestKubernetesAuth_LoginSendsRoleAndServiceAccountToken(t *testing.T) {
	f := newFakeOpenBao(t)
	kv, err := NewDeliveryKV(f.srv.URL, "secret", NewKubernetesAuth("aep-api", "kubernetes", writeTemp(t, "sa-token\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := kv.Put(ctx, "user-app-secrets/ns/a", map[string]string{"k": "v"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loginReq["role"] != "aep-api" || f.loginReq["jwt"] != "sa-token" {
		t.Fatalf("login body = role %v, jwt %q; want role aep-api and the trimmed token file", f.loginReq["role"], f.loginReq["jwt"])
	}
}

// One session per process: the clients that share a VaultAuth share its token,
// so the process logs in once, not once per client.
func TestKubernetesAuth_OneSessionAcrossClients(t *testing.T) {
	f := newFakeOpenBao(t)
	auth := NewKubernetesAuth("aep-api", "kubernetes", writeTemp(t, "sa-token"))
	for _, p := range []string{"user-app-secrets/ns/a", "user-app-secrets/ns/b"} {
		kv, err := NewDeliveryKV(f.srv.URL, "secret", auth)
		if err != nil {
			t.Fatal(err)
		}
		if err := kv.Put(ctx, p, map[string]string{"k": "v"}); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	if logins, _ := f.counts(); logins != 1 {
		t.Fatalf("logins = %d across two clients of one session; want 1", logins)
	}
}

// A failed login fails the operation loudly, and neither the error nor the
// failure carries the service-account token.
func TestKubernetesAuth_LoginFailureFailsTheWriteWithoutTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // e.g. role "aep-api" not configured; 400 avoids client retries
		_, _ = w.Write([]byte(`{"errors":["invalid role name"]}`))
	}))
	t.Cleanup(srv.Close)
	const jwt = "sa-token-do-not-leak"
	kv, err := NewDeliveryKV(srv.URL, "secret", NewKubernetesAuth("aep-api", "kubernetes", writeTemp(t, jwt)))
	if err != nil {
		t.Fatal(err)
	}
	err = kv.Put(ctx, "user-app-secrets/ns/a", map[string]string{"k": "v"})
	if err == nil {
		t.Fatal("a refused login must fail the write")
	}
	if strings.Contains(err.Error(), jwt) || !strings.Contains(err.Error(), "status 400") {
		t.Fatalf("error = %q; want the status and no token", err)
	}

	_, err = NewKubernetesAuth("aep-api", "kubernetes", filepath.Join(t.TempDir(), "missing")).(*vaultSession).ensure(ctx, kv.client)
	if err == nil {
		t.Fatal("a missing token file must fail the login")
	}
}
