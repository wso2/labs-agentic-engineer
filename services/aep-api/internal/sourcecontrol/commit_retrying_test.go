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

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// CommitRetrying re-plans on a conflict, stops at CommitAttempts, ends at
// once on any other failure, and commits nothing for an empty plan.
func TestCommitRetrying(t *testing.T) {
	ctx := context.Background()
	ref := sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}
	write := func(context.Context) (sourcecontrol.CommitRequest, error) {
		return sourcecontrol.CommitRequest{Message: "m", Writes: []sourcecontrol.FileWrite{{Path: "a", Content: "1"}}}, nil
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
		f.SeedRepo(ref, map[string]string{})
		f.FailOp(aestudiotest.OpCommit, &sourcecontrol.CommitConflictError{})
		_, err := sourcecontrol.CommitRetrying(ctx, f, ref, write)
		if !errors.Is(err, sourcecontrol.ErrCommitConflict) || commits(f) != sourcecontrol.CommitAttempts {
			t.Fatalf("err %v after %d commits, want a conflict after %d", err, commits(f), sourcecontrol.CommitAttempts)
		}
	})
	t.Run("another failure ends it at once", func(t *testing.T) {
		f := aestudiotest.New()
		f.SeedRepo(ref, map[string]string{})
		f.FailOp(aestudiotest.OpCommit, sourcecontrol.ErrAEStudioUnavailable)
		_, err := sourcecontrol.CommitRetrying(ctx, f, ref, write)
		if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) || commits(f) != 1 {
			t.Fatalf("err %v after %d commits, want unavailable after 1", err, commits(f))
		}
	})
	t.Run("an empty plan commits nothing", func(t *testing.T) {
		f := aestudiotest.New()
		res, err := sourcecontrol.CommitRetrying(ctx, f, ref, func(context.Context) (sourcecontrol.CommitRequest, error) {
			return sourcecontrol.CommitRequest{Message: "nothing"}, nil
		})
		if err != nil || res.Changed || commits(f) != 0 {
			t.Fatalf("(%+v, %v) after %d commits, want nothing", res, err, commits(f))
		}
	})
}
