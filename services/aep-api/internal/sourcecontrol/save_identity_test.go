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

package sourcecontrol_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type identityCred struct{ id secrets.Identity }

func (c identityCred) Token(context.Context) (string, time.Time, error) { return "", time.Time{}, nil }
func (c identityCred) Identity() secrets.Identity                       { return c.id }
func (identityCred) RepoOwner() string                                  { return "acme" }
func (identityCred) WebhookStrategy() secrets.WebhookStrategy           { return secrets.WebhookPlatform }

type identityResolver struct {
	cred secrets.Credential
	err  error
}

func (r identityResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return r.cred, r.err
}

var identityRef = sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}

func lastCall(t *testing.T, f *aestudiotest.Fake, op string) aestudiotest.Call {
	t.Helper()
	calls := f.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Op == op {
			return calls[i]
		}
	}
	t.Fatalf("no %s call", op)
	return aestudiotest.Call{}
}

// Commits and tags that name no identity go out as the org credential's,
// author and committer alike; the AEP default fills what it does not carry.
func TestWithSaveIdentity_StampsTheCredentialIdentity(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	f.SeedRepo(identityRef, map[string]string{})
	git := sourcecontrol.WithSaveIdentity(f, identityResolver{cred: identityCred{id: secrets.Identity{Name: "Ada"}}})

	if _, err := git.Commit(ctx, identityRef, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "1"}}}); err != nil {
		t.Fatal(err)
	}
	want := sourcecontrol.GitIdentity{Name: "Ada", Email: "noreply@aep.dev"}
	c := lastCall(t, f, aestudiotest.OpCommit)
	if c.Author == nil || *c.Author != want || c.Committer == nil || *c.Committer != want {
		t.Fatalf("author %+v committer %+v, want %+v for both", c.Author, c.Committer, want)
	}
	if c.Author == c.Committer {
		t.Fatal("author and committer share one pointer")
	}
	if err := git.Tag(ctx, identityRef, sourcecontrol.TagSpec{Name: "v1"}); err != nil {
		t.Fatal(err)
	}
	if tc := lastCall(t, f, aestudiotest.OpTag); tc.Tagger == nil || *tc.Tagger != want {
		t.Fatalf("tagger %+v, want %+v", tc.Tagger, want)
	}
}

// An identity the caller names is kept.
func TestWithSaveIdentity_KeepsANamedIdentity(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	f.SeedRepo(identityRef, map[string]string{})
	git := sourcecontrol.WithSaveIdentity(f, identityResolver{cred: identityCred{id: secrets.Identity{Name: "Ada", Email: "ada@example.com"}}})
	mine := &sourcecontrol.GitIdentity{Name: "Me", Email: "me@example.com"}

	if _, err := git.Commit(ctx, identityRef, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "1"}}, Author: mine}); err != nil {
		t.Fatal(err)
	}
	if c := lastCall(t, f, aestudiotest.OpCommit); c.Author != mine {
		t.Fatalf("author %+v, want the caller's", c.Author)
	}
}

// A credential that cannot be resolved leaves the request unstamped: the pod
// authors it as the org's GitHub user, and the write still lands.
func TestWithSaveIdentity_UnresolvedCredentialGoesOutUnstamped(t *testing.T) {
	ctx := context.Background()
	f := aestudiotest.New()
	f.SeedRepo(identityRef, map[string]string{})
	git := sourcecontrol.WithSaveIdentity(f, identityResolver{err: errors.New("vault down")})

	if _, err := git.Commit(ctx, identityRef, sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "1"}}}); err != nil {
		t.Fatal(err)
	}
	if c := lastCall(t, f, aestudiotest.OpCommit); c.Author != nil {
		t.Fatalf("author %+v, want none", c.Author)
	}
}

// CommitRetrying re-plans on a conflict, stops at CommitAttempts, and
// commits nothing for an empty plan.
func TestCommitRetrying(t *testing.T) {
	ctx := context.Background()
	write := func(context.Context) (sourcecontrol.CommitRequest, error) {
		return sourcecontrol.CommitRequest{Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "1"}}}, nil
	}
	commits := func(f *aestudiotest.Fake) int {
		n := 0
		for _, c := range f.Calls() {
			if c.Op == aestudiotest.OpCommit {
				n++
			}
		}
		return n
	}

	t.Run("conflicts until the last attempt", func(t *testing.T) {
		f := aestudiotest.New()
		f.SeedRepo(identityRef, map[string]string{})
		f.FailOp(aestudiotest.OpCommit, &sourcecontrol.CommitConflictError{})
		_, err := sourcecontrol.CommitRetrying(ctx, f, identityRef, write)
		if !errors.Is(err, sourcecontrol.ErrCommitConflict) || commits(f) != sourcecontrol.CommitAttempts {
			t.Fatalf("err %v after %d commits, want a conflict after %d", err, commits(f), sourcecontrol.CommitAttempts)
		}
	})
	t.Run("another failure ends it at once", func(t *testing.T) {
		f := aestudiotest.New()
		f.SeedRepo(identityRef, map[string]string{})
		f.FailOp(aestudiotest.OpCommit, sourcecontrol.ErrAEStudioUnavailable)
		_, err := sourcecontrol.CommitRetrying(ctx, f, identityRef, write)
		if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) || commits(f) != 1 {
			t.Fatalf("err %v after %d commits, want unavailable after 1", err, commits(f))
		}
	})
	t.Run("an empty plan commits nothing", func(t *testing.T) {
		f := aestudiotest.New()
		res, err := sourcecontrol.CommitRetrying(ctx, f, identityRef, func(context.Context) (sourcecontrol.CommitRequest, error) {
			return sourcecontrol.CommitRequest{Message: "nothing"}, nil
		})
		if err != nil || res.Changed || commits(f) != 0 {
			t.Fatalf("(%+v, %v) after %d commits, want nothing", res, err, commits(f))
		}
	})
}
