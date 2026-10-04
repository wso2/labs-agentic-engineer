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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The issue service addresses the org's pod by the project's row, never by
// client input: the ref is RefForRow's.
func TestIssueService_ResolvesRefFromRow(t *testing.T) {
	f := aestudiotest.New()
	repos := newFakeRepoRepo()
	repos.preload(&sourcecontrol.GitRepository{OrgID: "default", ProjectID: "p", RepoURL: "https://github.com/acme/greeter", Status: "ready"})
	svc := sourcecontrol.NewIssueService(repos, f)
	if _, err := svc.CreateIssue(context.Background(), "default", "p", sourcecontrol.CreateIssueRequest{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if got := f.Issues(sourcecontrol.RepoRef{Org: "default", Owner: "acme", Repo: "greeter"}); len(got) != 1 {
		t.Fatalf("issues on the Fake = %d", len(got))
	}
}
