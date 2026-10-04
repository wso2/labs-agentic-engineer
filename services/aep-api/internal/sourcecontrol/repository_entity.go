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

package sourcecontrol

import (
	"regexp"
	"strings"
	"time"
)

// SkillsRepoSentinelProjectID is the reserved git_repositories.project_id
// under which the per-org skills repo row lives (so it is distinguishable
// from real project repos). See docs/design/skills-repo-storage.md §10.1.
const SkillsRepoSentinelProjectID = "_skills"

// IsPlatformRepo reports whether projectID is a reserved platform repository
// row (the skills repo, the resource-docs repo) rather than a project's: they
// live under project ids that start with "_", which an OpenChoreo project
// name (a DNS label) never does. Platform repos carry no hook.
func IsPlatformRepo(projectID string) bool {
	return strings.HasPrefix(projectID, "_")
}

// GitRepository stores metadata about a platform-provisioned git repository.
type GitRepository struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	// OrgID + ProjectID form a composite UNIQUE so two orgs can own a
	// same-named project and lookups must be org-scoped
	// (GetByOrgAndProjectID). The composite's leading org_id column also
	// serves org-only queries (no separate index needed).
	OrgID         string `gorm:"not null;uniqueIndex:ux_git_repositories_org_project,priority:1" json:"orgId"`
	ProjectID     string `gorm:"not null;uniqueIndex:ux_git_repositories_org_project,priority:2" json:"projectId"`
	RepoURL       string `gorm:"not null" json:"repoUrl"`
	DefaultBranch string `gorm:"default:main" json:"defaultBranch"`
	Status        string `gorm:"default:pending" json:"status"`
	ErrorMessage  string `gorm:"type:text" json:"errorMessage,omitempty"`
	// WebhookID is the GitHub-assigned hook ID for the repo's webhook.
	// Populated at repo provision. Used to deregister on repo cleanup or
	// re-register on rotation.
	WebhookID *int64 `json:"webhookId,omitempty"`
	// OcSecretRefName is unused on new rows: the build flow
	// (docs/design/build-credential-injection.md) pre-stages a
	// per-WorkflowRun K8s Secret named `<workflowRunName>-git-secret`
	// directly in workflows-<ocOrgID> and passes secretRef="" to the
	// workflow. Retained for the JSON contract and as a column on
	// older rows.
	OcSecretRefName *string `gorm:"column:oc_secret_ref_name" json:"ocSecretRefName,omitempty"`
	// RepoSlug is the SecretReference slug — `lower(<owner>-<repo>)`. Used
	// for OpenBao path keying (`secret/aep/{ocOrgId}/git/{repoSlug}`) and
	// the OC SecretReference CR name (`git-{ocOrgId}-{repoSlug}`). Nullable;
	// the dispatch path lazy-backfills from RepoURL.
	RepoSlug  string    `gorm:"column:repo_slug;index" json:"repoSlug,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// GitHub Projects v2 is dropped (tasks-github-native §4) — the
	// github_project_id cache column is removed by the tasks_github_native
	// migration and no longer modeled.
}

// RepoSlugFor returns the canonical repo slug for a GitHub HTTPS clone URL: the
// `owner/repo` path lowercased with `/` replaced by `-`, "" when the URL does
// not match. It is the single entry point for slug derivation from a URL.
func RepoSlugFor(url string) string {
	m := repoURLPattern.FindStringSubmatch(url)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(m[1], "/", "-"))
}

// repoURLPattern extracts `<owner>/<repo>` from a GitHub HTTPS URL, with or
// without a `.git` suffix.
var repoURLPattern = regexp.MustCompile(`github\.com/([^/]+/[^/]+?)(?:\.git)?/?$`)
