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
	"errors"
	"sync"
	"testing"
	"time"

	vault "github.com/hashicorp/vault/api"
)

// scriptedLogin answers each login with the next result; a nil error yields
// tok-<n> with a one-hour TTL.
type scriptedLogin struct {
	mu      sync.Mutex
	calls   int
	results []error
}

func (l *scriptedLogin) login(context.Context, *vault.Client) (string, time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	var err error
	if l.calls <= len(l.results) {
		err = l.results[l.calls-1]
	}
	if err != nil {
		return "", 0, err
	}
	return "tok-" + string(rune('0'+l.calls)), time.Hour, nil
}

type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

func newTestSession(t *testing.T, results ...error) (*vaultSession, *scriptedLogin, *testClock, *vault.Client) {
	t.Helper()
	l := &scriptedLogin{results: results}
	clock := &testClock{t: time.Unix(1_700_000_000, 0)}
	c, err := vault.NewClient(vault.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	c.ClearToken()
	return &vaultSession{login: l.login, now: clock.now}, l, clock, c
}

// A renewal that fails while the token is near expiry but still valid keeps
// the token in use, and the next operation tries the login again.
func TestVaultSession_FailedNearExpiryRenewalKeepsTheValidToken(t *testing.T) {
	loginDown := &loginError{status: "503"}
	s, l, clock, c := newTestSession(t, nil, loginDown, nil)
	if _, err := s.ensure(ctx, c); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	clock.t = clock.t.Add(50 * time.Minute) // 10 of 60 minutes left: near expiry
	if _, err := s.ensure(ctx, c); err != nil {
		t.Fatalf("ensure with a still-valid token failed: %v", err)
	}
	if c.Token() != "tok-1" || l.calls != 2 {
		t.Fatalf("token=%q logins=%d, want tok-1 kept after one failed renewal", c.Token(), l.calls)
	}
	if _, err := s.ensure(ctx, c); err != nil || c.Token() != "tok-3" || l.calls != 3 {
		t.Fatalf("next ensure: err=%v token=%q logins=%d, want a fresh login (tok-3)", err, c.Token(), l.calls)
	}
}

// Once the token has expired, a failed renewal fails the operation with the
// login error.
func TestVaultSession_FailedRenewalOfAnExpiredTokenFails(t *testing.T) {
	loginDown := &loginError{status: "503"}
	s, _, clock, c := newTestSession(t, nil, loginDown)
	if _, err := s.ensure(ctx, c); err != nil {
		t.Fatal(err)
	}
	clock.t = clock.t.Add(61 * time.Minute)
	if _, err := s.ensure(ctx, c); !errors.Is(err, loginDown) {
		t.Fatalf("err = %v, want the login error", err)
	}
}

// A caller holding an older generation, after the session's latest re-login
// failed, gets the login error, never an empty token whose retry reads as a
// 403.
func TestVaultSession_StaleReloginAfterAFailedLoginSurfacesTheLoginError(t *testing.T) {
	loginDown := &loginError{status: "503"}
	s, _, _, c := newTestSession(t, nil, nil, loginDown, loginDown)
	gen1, err := s.ensure(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.relogin(ctx, c, gen1); err != nil { // another caller's 403: gen 2
		t.Fatal(err)
	}
	if err := s.relogin(ctx, c, gen1+1); !errors.Is(err, loginDown) { // gen 2 refused, login down
		t.Fatalf("relogin err = %v, want the login error", err)
	}
	err = s.relogin(ctx, c, gen1) // the stale caller
	if !errors.Is(err, loginDown) {
		t.Fatalf("stale relogin err = %v, want the login error", err)
	}
	if c.Token() == "tok-2" {
		t.Fatalf("the refused token must not be put back on the client")
	}
}
