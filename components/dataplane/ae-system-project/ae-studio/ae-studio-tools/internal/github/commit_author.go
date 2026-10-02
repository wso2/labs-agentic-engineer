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
)

// Users answers the gitpat's user. *Client satisfies it.
type Users interface {
	User(ctx context.Context) (User, error)
}

var _ Users = (*Client)(nil)

// CommitAuthor is the author and committer of the pod's commits: the gitpat
// user, so a save is attributed to the identity that pushes it (07 §11). A
// user without a public name commits as their login, and without a public
// email as <login>@users.noreply.github.com (the fallbacks aep-api records
// for the org credential). The first successful answer is cached for the
// process; a failed lookup is not, so the next save asks again.
type CommitAuthor struct {
	users Users

	mu          sync.Mutex
	name, email string
}

// NewCommitAuthor returns a CommitAuthor that looks the user up through users.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func NewCommitAuthor(users Users) *CommitAuthor {
	return &CommitAuthor{users: users}
}

// Identity returns the commit name and email. Errors are the lookup's
// (*StatusError, *ErrRateLimited or a transport failure), never the token.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func (a *CommitAuthor) Identity(ctx context.Context) (name, email string, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.name != "" {
		return a.name, a.email, nil
	}
	u, err := a.users.User(ctx)
	if err != nil {
		return "", "", err
	}
	a.name, a.email = u.Name, u.Email
	if a.name == "" {
		a.name = u.Login
	}
	if a.email == "" {
		a.email = u.Login + "@users.noreply.github.com"
	}
	return a.name, a.email, nil
}
