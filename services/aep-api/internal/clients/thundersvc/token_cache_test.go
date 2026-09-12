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
	"strings"
	"testing"
	"time"
)

// The system token is cached in-process, so these tests pin down the two
// ways a cached token can outlive Thunder's acceptance of it: the clock the
// cache trusts, and what happens when Thunder says 401 anyway.

// The cache must judge expiry on the wall clock. Go's time.Now carries a
// monotonic reading that time.Before prefers, and a VM paused with a sleeping
// laptop stops that clock while the wall clock (and Thunder's view of `exp`)
// moves on. After such a pause the cache believed a dead token had minutes
// left, and every admin call answered 401 until the monotonic deadline came.
func TestSystemToken_CacheExpiresOnTheWallClock(t *testing.T) {
	m := &thunderMock{}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if m.mints != 1 {
		t.Fatalf("mints after first call = %d, want 1", m.mints)
	}

	// The wall clock jumps past the token's life; the process's monotonic
	// clock (which the test cannot move) has barely advanced.
	c.now = func() time.Time { return time.Now().Add(2 * time.Hour) }

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.mints != 2 {
		t.Errorf("mints after the wall clock passed expiry = %d, want 2 (a fresh token)", m.mints)
	}
}

// Thunder's `exp` claim is the issuer's own statement of when the token dies;
// `expires_in` is a courtesy copy. When the two disagree the claim wins.
func TestSystemToken_CacheFollowsTheExpClaim(t *testing.T) {
	m := &thunderMock{expIn: 10} // exp in 10 s, expires_in still says 3600
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// 10 s minus the refresh skew is already in the past: the next call must
	// not reuse the token.
	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if m.mints != 2 {
		t.Errorf("mints = %d, want 2: a token whose exp claim is inside the skew must not be reused", m.mints)
	}
}

// A 401 on an admin call means the token in hand is no good, whatever the
// cache believed. The client drops it, mints once, and repeats the call, so
// a token that died early costs one extra round-trip rather than a failed
// build.
func TestAdminCall_401_EvictsTheCachedTokenAndRetriesOnce(t *testing.T) {
	m := &thunderMock{revoked: map[string]bool{}}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	if _, err := c.OUExists(context.Background(), "ou-1"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	m.revoked[m.issued[0]] = true // Thunder stops accepting the cached token

	exists, err := c.OUExists(context.Background(), "ou-1")
	if err != nil {
		t.Fatalf("call with a revoked cached token should recover, got: %v", err)
	}
	if !exists {
		t.Error("the retried call's answer was lost")
	}
	if m.mints != 2 {
		t.Errorf("mints = %d, want 2 (one fresh token after the 401)", m.mints)
	}
	if c.cachedToken != m.issued[1] {
		t.Error("the cache should now hold the fresh token")
	}
}

// One retry, not a loop: when the fresh token is refused too, the 401 is the
// answer and the caller hears it.
func TestAdminCall_401_Persistent_RetriesExactlyOnce(t *testing.T) {
	m := &thunderMock{revokeAll: true}
	srv := m.server(t)
	defer srv.Close()
	c := newTestClient(srv.URL)

	_, err := c.OUExists(context.Background(), "ou-1")
	if err == nil {
		t.Fatal("expected the persistent 401 to surface")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should carry the status, got %q", err)
	}
	if m.mints != 2 {
		t.Errorf("mints = %d, want exactly 2 (the original and one retry)", m.mints)
	}
	if c.cachedToken != "" {
		t.Error("a token Thunder refused must not stay cached")
	}
}
