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
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// VaultAuth is how this process holds an OpenBao session. There is no static
// token: the only implementation logs in by Kubernetes auth with the pod's
// service-account token (NewKubernetesAuth), so OpenBao, not aep-api, decides
// what the process may touch (the aep-api-writer policy).
//
// One VaultAuth per process: every DeliveryKV built with it shares its token,
// so the process logs in once rather than once per client. The methods are
// package-private, so only this package can implement it.
type VaultAuth interface {
	// ensure puts a live token on c, logging in first when the session has
	// none or less than a third of its TTL is left. It returns the session
	// generation the token belongs to.
	ensure(ctx context.Context, c *vault.Client) (generation uint64, err error)
	// relogin replaces the token OpenBao refused (403) for generation, and
	// puts the new one on c. A caller holding an older generation finds the
	// session already renewed and logs in no second time.
	relogin(ctx context.Context, c *vault.Client, generation uint64) error
}

// serviceAccountTokenPath is where Kubernetes projects a pod's
// service-account token (the default for OPENBAO_AUTH_TOKEN_PATH).
const serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

// NewKubernetesAuth returns the session that logs in with
// PUT /v1/auth/{mount}/login {"role": role, "jwt": <tokenPath contents>}.
// The login is lazy: nothing is read or sent until the first operation, so
// aep-api boots with OpenBao unreachable or the role missing. The token file
// is re-read at every login because Kubernetes rotates projected tokens.
func NewKubernetesAuth(role, mount, tokenPath string) VaultAuth {
	if tokenPath == "" {
		tokenPath = serviceAccountTokenPath
	}
	k := kubernetesLogin{role: role, mount: mount, tokenPath: tokenPath}
	return &vaultSession{login: k.login, now: time.Now}
}

// kubernetesLogin performs one Kubernetes-auth login.
type kubernetesLogin struct {
	role, mount, tokenPath string
}

// login returns the client token and its TTL. The service-account token never
// leaves this function except in the login request body.
func (k kubernetesLogin) login(ctx context.Context, c *vault.Client) (string, time.Duration, error) {
	raw, err := os.ReadFile(k.tokenPath)
	if err != nil {
		return "", 0, loginFailed("token_file_unreadable")
	}
	jwt := strings.TrimSpace(string(raw))
	if jwt == "" {
		return "", 0, loginFailed("token_file_empty")
	}
	// A clone without a token: the login endpoint is unauthenticated, and the
	// refused token has no business riding along on it.
	lc, err := c.Clone()
	if err != nil {
		return "", 0, loginFailed("client_clone_failed")
	}
	lc.ClearToken()
	secret, err := lc.Logical().WriteWithContext(ctx, "auth/"+k.mount+"/login", map[string]interface{}{
		"role": k.role,
		"jwt":  jwt,
	})
	if err != nil {
		return "", 0, loginFailed(vaultStatus(err))
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return "", 0, loginFailed("no_client_token")
	}
	ttl := time.Duration(secret.Auth.LeaseDuration) * time.Second
	slog.Info("openbao.login", "role", k.role, "mount", k.mount, "ttl", ttl.String())
	return secret.Auth.ClientToken, ttl, nil
}

// loginError is a failed login. It carries a status only (an HTTP status or a
// short reason), never the request, the response body or either token.
type loginError struct{ status string }

func (e *loginError) Error() string { return "openbao login failed: " + e.status }

// loginFailed logs the value-free failure event and returns the matching error.
func loginFailed(status string) error {
	slog.Warn("openbao.login_failed", "status", status)
	return &loginError{status: status}
}

// vaultSession caches one login's token and renews it before it runs out or
// once OpenBao refuses it.
type vaultSession struct {
	login func(ctx context.Context, c *vault.Client) (token string, ttl time.Duration, err error)
	now   func() time.Time

	mu         sync.Mutex
	token      string
	ttl        time.Duration // 0 = the token does not expire
	expires    time.Time
	generation uint64
}

func (s *vaultSession) ensure(ctx context.Context, c *vault.Client) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == "" || s.nearExpiry() {
		if err := s.renew(ctx, c); err != nil {
			return 0, err
		}
	}
	if c.Token() != s.token {
		c.SetToken(s.token)
	}
	return s.generation, nil
}

func (s *vaultSession) relogin(ctx context.Context, c *vault.Client, generation uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if generation == s.generation {
		if err := s.renew(ctx, c); err != nil {
			return err
		}
	}
	c.SetToken(s.token)
	return nil
}

// nearExpiry reports less than a third of the token's TTL left. Caller holds mu.
func (s *vaultSession) nearExpiry() bool {
	return s.ttl > 0 && s.expires.Sub(s.now()) < s.ttl/3
}

// renew logs in and replaces the cached token. Caller holds mu. On failure the
// old token is dropped, so the next operation logs in again.
func (s *vaultSession) renew(ctx context.Context, c *vault.Client) error {
	token, ttl, err := s.login(ctx, c)
	if err != nil {
		s.token = ""
		return err
	}
	s.token, s.ttl, s.expires = token, ttl, s.now().Add(ttl)
	s.generation++
	return nil
}
