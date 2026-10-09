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

package aestudio

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeReconciler records the orgs it reconciled and fails with err.
type fakeReconciler struct {
	orgs []string
	err  error
}

func (f *fakeReconciler) Reconcile(_ context.Context, org string) (int, error) {
	f.orgs = append(f.orgs, org)
	return 0, f.err
}

func TestSkillsRepositories_Lookup(t *testing.T) {
	ctx := context.Background()
	rows := &fakeRepoReader{rows: map[string]*sourcecontrol.GitRepository{
		"acme/_skills":   {RepoURL: "https://github.com/acme-gh/org-skills.git", DefaultBranch: "main"},
		"broken/_skills": {RepoURL: "not-a-repo-url", DefaultBranch: "main"},
	}}

	// The library is reconciled before the row is read, so platform skills
	// shipped since the org was provisioned are in the snapshot the turn reads.
	t.Run("reconciles, then answers the org's _skills row", func(t *testing.T) {
		rec := &fakeReconciler{}
		got, err := NewSkillsRepositories(rec, rows).Lookup(ctx, "acme")
		require.NoError(t, err)
		require.Equal(t, ProjectRepository{
			Owner: "acme-gh", Repo: "org-skills", DefaultBranch: "main",
			CloneURL: "https://github.com/acme-gh/org-skills.git",
		}, got)
		require.Equal(t, []string{"acme"}, rec.orgs)
	})

	t.Run("no row is ErrSkillsRepositoryNotFound", func(t *testing.T) {
		_, err := NewSkillsRepositories(&fakeReconciler{}, rows).Lookup(ctx, "other")
		require.ErrorIs(t, err, ErrSkillsRepositoryNotFound)
	})

	t.Run("a failed reconcile is ErrSkillsUnavailable and reads no row", func(t *testing.T) {
		boom := errors.New("github down")
		_, err := NewSkillsRepositories(&fakeReconciler{err: boom}, &fakeRepoReader{err: errors.New("must not be read")}).Lookup(ctx, "acme")
		require.ErrorIs(t, err, ErrSkillsUnavailable)
		require.ErrorIs(t, err, boom)
	})

	t.Run("unparseable repo url is an error, not a 404", func(t *testing.T) {
		_, err := NewSkillsRepositories(&fakeReconciler{}, rows).Lookup(ctx, "broken")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrSkillsRepositoryNotFound)
	})

	t.Run("store failure is an error, not a 404", func(t *testing.T) {
		boom := errors.New("db down")
		_, err := NewSkillsRepositories(&fakeReconciler{}, &fakeRepoReader{err: boom}).Lookup(ctx, "acme")
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, ErrSkillsRepositoryNotFound)
	})
}
