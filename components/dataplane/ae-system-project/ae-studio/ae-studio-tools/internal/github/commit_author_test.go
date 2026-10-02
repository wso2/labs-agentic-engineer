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
)

func TestUser_DecodesNameAndEmail(t *testing.T) {
	srv := fakeGitHub(t, http.StatusOK, nil, `{"login":"octo-bot","id":7,"name":"Octo Bot","email":"bot@acme.dev"}`)
	u, err := newClient(srv.URL, testPAT).User(context.Background())
	if err != nil || u != (User{Login: "octo-bot", ID: 7, Name: "Octo Bot", Email: "bot@acme.dev"}) {
		t.Fatalf("User = %+v, %v", u, err)
	}
}

// fakeUsers answers User from a fixed value or error and counts calls.
type fakeUsers struct {
	mu    sync.Mutex
	user  User
	err   error
	calls int
}

func (f *fakeUsers) User(context.Context) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.user, f.err
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

func TestCommitAuthor_CachesSuccessOnly(t *testing.T) {
	users := &fakeUsers{err: &StatusError{Status: http.StatusBadGateway}}
	a := NewCommitAuthor(users)
	if _, _, err := a.Identity(context.Background()); err == nil {
		t.Fatal("a failed lookup must surface its error")
	}
	users.mu.Lock()
	users.err, users.user = nil, User{Login: "octo-bot", ID: 7}
	users.mu.Unlock()
	for range 3 {
		if name, _, err := a.Identity(context.Background()); err != nil || name != "octo-bot" {
			t.Fatalf("Identity = %q, %v", name, err)
		}
	}
	if users.calls != 2 {
		t.Fatalf("GET /user calls = %d, want 2 (the failure is not cached, the success is)", users.calls)
	}
}

func TestCommitAuthor_ErrorKeepsItsClass(t *testing.T) {
	a := NewCommitAuthor(&fakeUsers{err: &ErrRateLimited{RetryAfter: 1}})
	_, _, err := a.Identity(context.Background())
	var rl *ErrRateLimited
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}
