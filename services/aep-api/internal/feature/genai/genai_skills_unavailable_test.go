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

package genai_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/models"
)

// TestSkillsRepoGone_Clear503 reproduces the live incident's failure shape: the
// org's _skills git_repositories row lingers but the backing repo is gone, so
// the turn's skills-ref resolve fails. The user must get a clear 503 ("org
// skills repository unavailable"), NOT the old opaque unlogged 500 — and agents
// must never be dispatched, no turn row created.
func TestSkillsRepoGone_Clear503(t *testing.T) {
	staleRow := &models.GitRepository{
		OrgID:         testOrg,
		ProjectID:     models.SkillsRepoSentinelProjectID,
		RepoURL:       "file:///nonexistent/skills-repo-gone.git",
		DefaultBranch: "main",
		Status:        "ready",
		RepoSlug:      "org-skills",
	}
	r := newGenaiRig(t, map[string]string{"specs/requirements/requirements.md": "# Reqs\n"},
		withSkillsRepo(func(context.Context, string) (*models.GitRepository, error) {
			return staleRow, nil
		}))

	rec := r.post(t, convUUID, "requirements-chat", "x")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("gone-skills-repo POST: code %d, want 503 (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "org skills repository unavailable") {
		t.Errorf("503 body must carry the clear message, got %s", rec.Body.String())
	}
	if r.fake.turns(t) != 0 {
		t.Error("agents dispatched despite an unavailable skills repo")
	}
	if rec := r.h.AsOrg(testOrg).Get(turnPath("active")); rec.Code != http.StatusNoContent {
		t.Errorf("no turn row should exist: active = %d, want 204", rec.Code)
	}
}
