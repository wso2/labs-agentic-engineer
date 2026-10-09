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

package github

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestUser_DecodesNameAndEmail(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK, nil, `{"login":"octo-bot","id":7,"name":"Octo Bot","email":"bot@acme.dev"}`)
	u, err := newClient(srv.URL, testPAT).User(context.Background())
	if err != nil || u != (User{Login: "octo-bot", ID: 7, Name: "Octo Bot", Email: "bot@acme.dev"}) {
		t.Fatalf("User = %+v, %v", u, err)
	}
}

// fakeUsers answers User from a fixed value or error and counts calls. With
// gate set, each call blocks until gate is closed.
type fakeUsers struct {
	mu    sync.Mutex
	user  User
	err   error
	calls int
	gate  chan struct{}
}

func (f *fakeUsers) User(context.Context) (User, error) {
	f.mu.Lock()
	f.calls++
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.user, f.err
}

func (f *fakeUsers) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestCommitAuthor_UsesNameAndEmail(t *testing.T) {
	a := NewCommitAuthor(&fakeUsers{user: User{Login: "octo-bot", ID: 7, Name: "Octo Bot", Email: "bot@acme.dev"}})
	name, email, err := a.Identity(context.Background())
	if err != nil || name != "Octo Bot" || email != "bot@acme.dev" {
		t.Fatalf("Identity = %q, %q, %v", name, email, err)
	}
}

// GitHub omits name and email when the user has not set them public: the
// author falls back to the login and the login's noreply address.
func TestCommitAuthor_FallsBackToLoginAndNoreply(t *testing.T) {
	a := NewCommitAuthor(&fakeUsers{user: User{Login: "octo-bot", ID: 7}})
	name, email, err := a.Identity(context.Background())
	if err != nil || name != "octo-bot" || email != "octo-bot@users.noreply.github.com" {
		t.Fatalf("Identity = %q, %q, %v", name, email, err)
	}
}

// A failure is remembered for identityFailureTTL, then asked again; a
// success is remembered for good.
func TestCommitAuthor_CachesFailureBrieflyAndSuccessForGood(t *testing.T) {
	users := &fakeUsers{err: &HTTPStatusError{StatusCode: http.StatusBadGateway}}
	a := NewCommitAuthor(users)
	clock := time.Unix(1_000_000, 0)
	a.now = func() time.Time { return clock }

	for range 2 {
		if _, _, err := a.Identity(context.Background()); err == nil {
			t.Fatal("a failed lookup must surface its error")
		}
	}
	if got := users.Calls(); got != 1 {
		t.Fatalf("GET /user calls = %d, want 1 (the failure is cached)", got)
	}

	users.mu.Lock()
	users.err, users.user = nil, User{Login: "octo-bot", ID: 7}
	users.mu.Unlock()
	clock = clock.Add(identityFailureTTL)
	for range 3 {
		if name, _, err := a.Identity(context.Background()); err != nil || name != "octo-bot" {
			t.Fatalf("Identity = %q, %v", name, err)
		}
	}
	if got := users.Calls(); got != 2 {
		t.Fatalf("GET /user calls = %d, want 2 (asked again after the TTL, then cached)", got)
	}
}

// Concurrent first callers share one GET /user.
func TestCommitAuthor_ConcurrentCallersShareOneLookup(t *testing.T) {
	gate := make(chan struct{})
	users := &fakeUsers{user: User{Login: "octo-bot", ID: 7}, gate: gate}
	a := NewCommitAuthor(users)
	const callers = 8
	names := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			names[i], _, errs[i] = a.Identity(context.Background())
		}()
	}
	for users.Calls() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let the other callers join the flight
	close(gate)
	wg.Wait()
	for i := range callers {
		if errs[i] != nil || names[i] != "octo-bot" {
			t.Fatalf("caller %d: %q, %v", i, names[i], errs[i])
		}
	}
	if got := users.Calls(); got != 1 {
		t.Fatalf("GET /user calls = %d, want 1", got)
	}
}

// A caller stops waiting when its ctx ends; the shared lookup carries on and
// a later caller gets its answer.
func TestCommitAuthor_WaitHonoursTheCallersContext(t *testing.T) {
	gate := make(chan struct{})
	users := &fakeUsers{user: User{Login: "octo-bot", ID: 7}, gate: gate}
	a := NewCommitAuthor(users)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := a.Identity(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the caller's deadline", err)
	}
	close(gate)
	if name, _, err := a.Identity(context.Background()); err != nil || name != "octo-bot" {
		t.Fatalf("Identity = %q, %v", name, err)
	}
	if got := users.Calls(); got != 1 {
		t.Fatalf("GET /user calls = %d, want 1 (the abandoned lookup's answer is reused)", got)
	}
}

func TestCommitAuthor_ErrorKeepsItsClass(t *testing.T) {
	a := NewCommitAuthor(&fakeUsers{err: &HTTPStatusError{StatusCode: http.StatusTooManyRequests, RetryAfter: time.Second}})
	_, _, err := a.Identity(context.Background())
	if _, ok := RateLimited(err); !ok {
		t.Fatalf("err = %v, want the rate limit", err)
	}
}
