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

package repo_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// TestBareCloneThenMutatePushes guards the --mirror to --bare change: a fresh
// bare clone must be able to push (no leftover remote.origin.mirror), carry no
// refs/pull/*, and see its own pushed commit on the next fetch.
func TestBareCloneThenMutatePushes(t *testing.T) {
	fx := NewFixtureWithCred(t, seedFiles(), repo.StaticToken(testToken))
	fx.Origin.mustExec(t, nil, nil, "update-ref", "refs/pull/1/head", fx.Origin.HeadSHA(t))
	ctx := context.Background()

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	res, err := fx.Engine.Mutate(ctx, fx.Ref, func(tx repo.Tx) error {
		tx.Write("specs/a.md", []byte("a"))
		return nil
	}, repo.CommitOpts{Message: "m", Retry: repo.RetryPolicy{Attempts: 2}})
	if err != nil {
		t.Fatalf("Mutate: %v", err)
	}
	if !res.Changed {
		t.Fatalf("Mutate result = %+v, want a commit", res)
	}
	if got := fx.Origin.HeadSHA(t); got != res.CommitSHA {
		t.Fatalf("origin main = %s, want pushed commit %s", got, res.CommitSHA)
	}

	clone := mirrorGitDir(t, fx)
	if refs := gitOut(t, clone, "for-each-ref", "refs/pull/"); refs != "" {
		t.Fatalf("bare clone carries PR refs:\n%s", refs)
	}
	if cfg := gitOut(t, clone, "config", "--list"); strings.Contains(cfg, "remote.origin.mirror") {
		t.Fatalf("clone config carries remote.origin.mirror:\n%s", cfg)
	}

	// Origin moves out-of-band; Head("") fetches and must see the new tip.
	next := fx.Origin.Seed(t, map[string]string{"specs/b.md": "b"}, "out of band")
	sha, err := fx.Engine.Head(ctx, fx.Ref, "")
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if sha != next {
		t.Fatalf("Head = %s, want fetched origin tip %s", sha, next)
	}

	line := logs.String()
	for _, want := range []string{"repo.clone", "repo=", "mode=bare", "ms="} {
		if !strings.Contains(line, want) {
			t.Fatalf("clone log %q lacks %q", line, want)
		}
	}
	if strings.Contains(line, testToken) {
		t.Fatalf("clone log leaks the token: %q", line)
	}
}
