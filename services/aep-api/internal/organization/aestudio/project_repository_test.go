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

// fakeRepoReader answers GetRepo from rows keyed "org/project", the way the
// git_repositories table is keyed (org_id, project_id).
type fakeRepoReader struct {
	rows map[string]*sourcecontrol.GitRepository
	err  error
}

func (f *fakeRepoReader) GetRepo(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if f.err != nil {
		return nil, f.err
	}
	row, ok := f.rows[org+"/"+project]
	if !ok {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	return row, nil
}

func TestProjectRepositories_Lookup(t *testing.T) {
	repos := &fakeRepoReader{rows: map[string]*sourcecontrol.GitRepository{
		"acme/greeter": {RepoURL: "https://github.com/acme-gh/greeter.git", DefaultBranch: "main"},
		"acme/broken":  {RepoURL: "not-a-repo-url", DefaultBranch: "main"},
	}}
	lookup := NewProjectRepositories(repos)
	ctx := context.Background()

	t.Run("own project", func(t *testing.T) {
		got, err := lookup.Lookup(ctx, "acme", "greeter")
		require.NoError(t, err)
		require.Equal(t, ProjectRepository{
			Owner: "acme-gh", Repo: "greeter", DefaultBranch: "main",
			CloneURL: "https://github.com/acme-gh/greeter.git",
		}, got)
	})

	// Another org's project is not found, never a hint that it exists.
	t.Run("another org's project", func(t *testing.T) {
		_, err := lookup.Lookup(ctx, "evil", "greeter")
		require.ErrorIs(t, err, ErrProjectNotFound)
	})

	t.Run("unknown project", func(t *testing.T) {
		_, err := lookup.Lookup(ctx, "acme", "nope")
		require.ErrorIs(t, err, ErrProjectNotFound)
	})

	t.Run("unparseable repo url is an error, not a 404", func(t *testing.T) {
		_, err := lookup.Lookup(ctx, "acme", "broken")
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrProjectNotFound)
	})

	t.Run("store failure is an error, not a 404", func(t *testing.T) {
		boom := errors.New("db down")
		_, err := NewProjectRepositories(&fakeRepoReader{err: boom}).Lookup(ctx, "acme", "greeter")
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, ErrProjectNotFound)
	})
}
