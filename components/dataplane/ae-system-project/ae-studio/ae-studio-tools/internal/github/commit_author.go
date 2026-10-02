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
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// identityFailureTTL is how long a failed GET /user is remembered: saves in
// that window commit as the platform default without asking GitHub again,
// so an outage or a rate limit costs one call per window, not one per save.
const identityFailureTTL = 30 * time.Second

// Users answers the gitpat's user. *Client satisfies it.
type Users interface {
	User(ctx context.Context) (User, error)
}

var _ Users = (*Client)(nil)

// CommitAuthor is the author and committer of the pod's commits: the gitpat
// user, so a save is attributed to the identity that pushes it (07 §11). A
// user without a public name commits as their login, and without a public
// email as <login>@users.noreply.github.com (the fallbacks aep-api records
// for the org credential).
//
// The first successful answer is cached for the process. A failure is cached
// for identityFailureTTL. Concurrent callers share one GET /user, and each
// waits only as long as its own ctx allows.
type CommitAuthor struct {
	users  Users
	flight singleflight.Group
	now    func() time.Time

	mu          sync.Mutex
	name, email string
	failErr     error
	failedAt    time.Time
}

// NewCommitAuthor returns a CommitAuthor that looks the user up through users.
func NewCommitAuthor(users Users) *CommitAuthor {
	return &CommitAuthor{users: users, now: time.Now}
}

// Identity returns the commit name and email. Errors are the lookup's
// (*StatusError, *ErrRateLimited or a transport failure, possibly the cached
// one), or ctx's when the caller stops waiting; never the token.
func (a *CommitAuthor) Identity(ctx context.Context) (name, email string, err error) {
	a.mu.Lock()
	switch {
	case a.name != "":
		name, email = a.name, a.email
		a.mu.Unlock()
		return name, email, nil
	case a.failErr != nil && a.now().Sub(a.failedAt) < identityFailureTTL:
		err = a.failErr
		a.mu.Unlock()
		return "", "", err
	}
	a.mu.Unlock()

	// The shared lookup must not die with whichever caller started it, so it
	// runs detached from that caller's cancellation; the GitHub client's own
	// timeout bounds it.
	ch := a.flight.DoChan("user", func() (any, error) {
		return nil, a.lookup(context.WithoutCancel(ctx))
	})
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return "", "", res.Err
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.name, a.email, nil
}

// lookup asks GitHub once and records the answer or the failure.
func (a *CommitAuthor) lookup(ctx context.Context) error {
	u, err := a.users.User(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	if err != nil {
		a.failErr, a.failedAt = err, a.now()
		return err
	}
	a.name, a.email = u.Name, u.Email
	if a.name == "" {
		a.name = u.Login
	}
	if a.email == "" {
		a.email = u.Login + "@users.noreply.github.com"
	}
	a.failErr = nil
	return nil
}
