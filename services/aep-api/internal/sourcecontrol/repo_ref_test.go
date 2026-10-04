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

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// rowReader answers one org's rows by project, nil when absent (the
// RepoRepository contract).
type rowReader struct {
	org  string
	rows map[string]*sourcecontrol.GitRepository
	err  error
}

func (r rowReader) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	if r.err != nil {
		return nil, r.err
	}
	if org != r.org {
		return nil, nil
	}
	return r.rows[project], nil
}

func TestRefForRow(t *testing.T) {
	ref, err := sourcecontrol.RefForRow("default", &sourcecontrol.GitRepository{RepoURL: "https://github.com/acme/greeter.git", DefaultBranch: "trunk"})
	if err != nil || ref != (sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter", DefaultBranch: "trunk"}) {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	ref, _ = sourcecontrol.RefForRow("default", &sourcecontrol.GitRepository{RepoURL: "https://github.com/acme/greeter"})
	if ref.DefaultBranch != "main" {
		t.Fatalf("an unset default branch is main, got %q", ref.DefaultBranch)
	}
	if _, err := sourcecontrol.RefForRow("default", &sourcecontrol.GitRepository{RepoURL: ""}); err == nil {
		t.Fatal("a row without a URL has no ref")
	}
	if _, err := sourcecontrol.RefForRow("default", nil); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("nil row: err = %v", err)
	}
}

func TestRepoRefFor(t *testing.T) {
	row := &sourcecontrol.GitRepository{OrgID: "default", ProjectID: "p", RepoURL: "https://github.com/acme/greeter", DefaultBranch: "main"}
	repos := rowReader{org: "default", rows: map[string]*sourcecontrol.GitRepository{"p": row}}

	ref, got, err := sourcecontrol.RepoRefFor(context.Background(), repos, "default", "p")
	if err != nil || got != row || ref != (sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter", DefaultBranch: "main"}) {
		t.Fatalf("ref=%+v row=%v err=%v", ref, got, err)
	}
	// Another org's project reads exactly like a missing one.
	for _, org := range []string{"evil", "default"} {
		project := map[string]string{"evil": "p", "default": "missing"}[org]
		if _, _, err := sourcecontrol.RepoRefFor(context.Background(), repos, org, project); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
			t.Fatalf("%s/%s: err = %v, want ErrRepoNotFound", org, project, err)
		}
	}
	boom := errors.New("db down")
	if _, _, err := sourcecontrol.RepoRefFor(context.Background(), rowReader{err: boom}, "default", "p"); !errors.Is(err, boom) || errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("err = %v, want the read failure", err)
	}
}
