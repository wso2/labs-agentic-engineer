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

package thunder

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// fakeJWT is an unsigned JWT carrying the given claims — enough for code that
// only reads them.
func fakeJWT(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"none"}`)) + "." + enc(payload) + ".sig"
}

func (f *fakeThunder) mints() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenCalls
}

// A lookup that never finds the app: one list call, no writes.
func lookup(t *testing.T, c AdminClient) error {
	t.Helper()
	return c.DeleteApplication(context.Background(), "aep-default-token-cache-probe")
}

// The cache is judged by the wall clock, not by how long this process thinks
// it has been running. A paused VM stops Go's monotonic clock while Thunder's
// clock — and the token's exp — run on; the observed failure was a token
// served from the cache long after it had expired, 401 after 401, with no
// re-mint. Here the clock is stepped forward as the wall does on resume.
func TestSystemToken_ExpiryFollowsTheWallClock(t *testing.T) {
	f := newFakeThunder(t)
	c := newTestClient(f).(*client)
	now := time.Date(2026, 10, 2, 8, 28, 27, 0, time.UTC)
	c.now = func() time.Time { return now }

	if err := lookup(t, c); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	now = now.Add(40 * time.Minute)
	if err := lookup(t, c); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if f.mints() != 1 {
		t.Fatalf("mints after 40 min = %d, want 1 (still within the hour)", f.mints())
	}
	now = now.Add(20*time.Minute + time.Second) // 60m01s: past expires_in minus the skew
	if err := lookup(t, c); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if f.mints() != 2 {
		t.Fatalf("mints after the hour = %d, want 2 (a re-mint)", f.mints())
	}
}

// The token's own exp is the server's verdict and wins over expires_in when
// it is sooner.
func TestSystemToken_TheTokensOwnExpWins(t *testing.T) {
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	f := newFakeThunder(t)
	f.tokenExp = now.Add(2 * time.Minute).Unix() // expires_in still says 3600
	c := newTestClient(f).(*client)
	c.now = func() time.Time { return now }

	if err := lookup(t, c); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	now = now.Add(91 * time.Second) // past exp minus the 30s skew
	if err := lookup(t, c); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if f.mints() != 2 {
		t.Fatalf("mints = %d, want 2: exp two minutes out must beat expires_in of an hour", f.mints())
	}
}

// A 401 answered to the system token is Thunder saying it no longer honours
// it. The token is evicted so the NEXT call mints afresh, instead of every
// call failing until the cache would have expired — 28 minutes of 401s on
// every ThunderApplication was the observed cost.
func TestSystemToken_EvictedWhenThunderAnswers401(t *testing.T) {
	f := newFakeThunder(t)
	f.listUnauthorized = 1
	c := newTestClient(f)

	if err := lookup(t, c); err == nil {
		t.Fatal("the 401 must surface to this caller")
	}
	if err := lookup(t, c); err != nil {
		t.Fatalf("the next call must succeed with a fresh token: %v", err)
	}
	if f.mints() != 2 {
		t.Fatalf("mints = %d, want 2 (the 401 evicts, the next call re-mints)", f.mints())
	}
}

// The expiry the cache stores must hold no monotonic reading: Before() uses
// the monotonic clock when both sides carry one, and that clock stands still
// while a paused VM's wall clock runs on. A stripped expiry makes every check
// a wall-clock comparison, even against a real time.Now().
func TestSystemTokenExpiry_CarriesNoMonotonicReading(t *testing.T) {
	got := systemTokenExpiry("opaque-token", 3600, time.Now())
	if got != got.Round(0) {
		t.Fatalf("expiry %v still carries a monotonic reading", got)
	}
}
