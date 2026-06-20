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

package project

import (
	"testing"

	"github.com/wso2/asdlc/asdlc-service/models"
)

func TestApplyRepoToProjectStatus(t *testing.T) {
	cases := []struct {
		name       string
		repo       *models.GitRepository
		wantPhase  string
		wantDone   bool
		wantErrMsg string
	}{
		{
			name:      "nil repo",
			repo:      nil,
			wantPhase: "no-repo",
			wantDone:  true,
		},
		{
			name:      "pending",
			repo:      &models.GitRepository{Status: "pending", RepoURL: "https://github.com/o/r"},
			wantPhase: "repo-cloning",
			wantDone:  true,
		},
		{
			name:      "cloning",
			repo:      &models.GitRepository{Status: "cloning", RepoURL: "https://github.com/o/r"},
			wantPhase: "repo-cloning",
			wantDone:  true,
		},
		{
			name:       "error",
			repo:       &models.GitRepository{Status: "error", RepoURL: "https://github.com/o/r", ErrorMessage: "create directory: permission denied"},
			wantPhase:  "repo-error",
			wantDone:   true,
			wantErrMsg: "create directory: permission denied",
		},
		{
			name:      "ready continues",
			repo:      &models.GitRepository{Status: "ready", RepoURL: "https://github.com/o/r"},
			wantPhase: "",
			wantDone:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := &models.ProjectStatus{}
			done := applyRepoToProjectStatus(status, c.repo)
			if done != c.wantDone {
				t.Fatalf("done: got %v want %v", done, c.wantDone)
			}
			if c.wantPhase != "" && status.Phase != c.wantPhase {
				t.Fatalf("phase: got %q want %q", status.Phase, c.wantPhase)
			}
			if c.repo != nil && status.RepoStatus != c.repo.Status {
				t.Fatalf("repoStatus: got %q want %q", status.RepoStatus, c.repo.Status)
			}
			if status.RepoErrorMessage != c.wantErrMsg {
				t.Fatalf("repoErrorMessage: got %q want %q", status.RepoErrorMessage, c.wantErrMsg)
			}
		})
	}
}
