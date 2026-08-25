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

package projects

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// spec.agent (#562) is the one spec-stage fact git cannot supply: a kickoff
// writes nothing until it lands, so exists/version/dirty are all false through
// the busiest moment of the journey.
func TestSpecAgentState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		newest *spec.AgentTurn
		want   string
	}{
		{
			// Its own state, not more idle: this project needs a way to BEGIN,
			// while one merely between turns is mid-interview and must not be
			// offered a restart that would supersede it.
			name:   "never run",
			newest: nil,
			want:   "never-started",
		},
		{
			name:   "a turn is in flight",
			newest: &spec.AgentTurn{Status: spec.TurnStatusRunning},
			want:   "working",
		},
		{
			name:   "the newest turn died",
			newest: &spec.AgentTurn{Status: spec.TurnStatusFailed},
			want:   "failed",
		},
		{
			// A completed turn's output is in git, so exists/version/dirty
			// already describe it — a second vocabulary for the same fact
			// could only disagree with them.
			name:   "a completed turn reads as idle, not as done",
			newest: &spec.AgentTurn{Status: "completed"},
			want:   "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := specAgentState(c.newest); got != c.want {
				t.Fatalf("specAgentState = %q, want %q", got, c.want)
			}
		})
	}
}

// An UNWIRED source says nothing rather than claiming never-started: it has no
// way to know a project's turn history, and the documented degradation is the
// pre-#562 reading.
func TestSpecAgentOf_UnwiredSourceClaimsNothing(t *testing.T) {
	t.Parallel()
	if got := specAgentOf(nil, nil); got != "" {
		t.Fatalf("specAgentOf(nil source) = %q, want the empty reading", got)
	}
}

func TestApplyRepoToProjectStatus(t *testing.T) {
	cases := []struct {
		name       string
		repo       *sourcecontrol.GitRepository
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
			repo:      &sourcecontrol.GitRepository{Status: "pending", RepoURL: "https://github.com/o/r"},
			wantPhase: "repo-cloning",
			wantDone:  true,
		},
		{
			name:      "cloning",
			repo:      &sourcecontrol.GitRepository{Status: "cloning", RepoURL: "https://github.com/o/r"},
			wantPhase: "repo-cloning",
			wantDone:  true,
		},
		{
			name:       "error",
			repo:       &sourcecontrol.GitRepository{Status: "error", RepoURL: "https://github.com/o/r", ErrorMessage: "create directory: permission denied"},
			wantPhase:  "repo-error",
			wantDone:   true,
			wantErrMsg: "create directory: permission denied",
		},
		{
			name:      "ready continues",
			repo:      &sourcecontrol.GitRepository{Status: "ready", RepoURL: "https://github.com/o/r"},
			wantPhase: "",
			wantDone:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status := &gen.ProjectStatus{}
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
