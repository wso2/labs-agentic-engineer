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

package cmd

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPollAlterPostgresRolePassword_RetriesThroughColdStart covers the race
// alterPostgresPasswordRetryWindow exists for: the official postgres image
// keeps the pod Running through initdb and a temporary internal server
// before the real one accepts connections, so the ALTER can fail a few
// times before it succeeds. This chart's postgres StatefulSet defines no
// readinessProbe (Ready and Running are the same signal), so retrying the
// ALTER itself — not waiting for a Ready that never differs from Running —
// is what actually covers this window.
func TestPollAlterPostgresRolePassword_RetriesThroughColdStart(t *testing.T) {
	orig := runAlterPostgresRolePassword
	t.Cleanup(func() { runAlterPostgresRolePassword = orig })

	var calls int
	runAlterPostgresRolePassword = func(_ context.Context, _, _, _ string) ([]byte, error) {
		calls++
		if calls < 3 {
			return []byte("psql: error: connection to server failed: the database system is starting up"), errors.New("exit status 2")
		}
		return nil, nil
	}

	err := pollAlterPostgresRolePassword(context.Background(), "ns", "pw", time.Now().Add(time.Second), time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 3 {
		t.Errorf("expected the ALTER to be retried at least 3 times before succeeding, got %d attempt(s)", calls)
	}
}

// TestPollAlterPostgresRolePassword_TimesOutOnPersistentFailure verifies a
// failure that never clears — not just a transient cold-start one — is
// still surfaced as an error once the retry window elapses, rather than
// retrying forever.
func TestPollAlterPostgresRolePassword_TimesOutOnPersistentFailure(t *testing.T) {
	orig := runAlterPostgresRolePassword
	t.Cleanup(func() { runAlterPostgresRolePassword = orig })

	var calls int
	runAlterPostgresRolePassword = func(_ context.Context, _, _, _ string) ([]byte, error) {
		calls++
		return []byte("boom"), errors.New("exit status 2")
	}

	err := pollAlterPostgresRolePassword(context.Background(), "ns", "pw", time.Now().Add(5*time.Millisecond), time.Millisecond)
	if err == nil {
		t.Fatal("expected an error once the retry window elapses, got nil")
	}
	if calls < 2 {
		t.Errorf("expected more than one attempt before timing out, got %d", calls)
	}
}
