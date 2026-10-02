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

// Package naming derives a repository's names from its GitHub URL: the
// on-disk slug of a clone (repos/<org>/<project>/<slug>/) and the owner/repo
// pair for log lines. It is a stdlib-only leaf beside the repo engine. The
// slug rule is the one aep-api applies to its git_repositories.repo_slug
// column, so the pod and aep-api agree on a repository's slug.
//
// Copied from services/aep-api/internal/platform/gitfs/naming; the aep-api
// copy is deleted in phase 4.
package naming

import (
	"regexp"
	"strings"
)

// repoURLPattern extracts `<owner>/<repo>` from a GitHub HTTPS URL. Matches both
// `https://github.com/owner/repo` and `.../repo.git`.
var repoURLPattern = regexp.MustCompile(`github\.com/([^/]+/[^/]+?)(?:\.git)?/?$`)

// SlugForURL returns the canonical repo slug for a GitHub HTTPS URL — the
// `owner/repo` path lowercased with `/` replaced by `-`. Returns "" if the URL
// doesn't match the GitHub HTTPS pattern (the caller decides whether to fail).
func SlugForURL(repoURL string) string {
	m := repoURLPattern.FindStringSubmatch(repoURL)
	if len(m) < 2 {
		return ""
	}
	return strings.ToLower(strings.ReplaceAll(m[1], "/", "-"))
}

// OwnerRepoFromURL extracts (owner, repo) from a GitHub HTTPS URL, preserving
// the original case (unlike SlugForURL, which lowercases). Returns empty strings
// if the URL doesn't match.
func OwnerRepoFromURL(repoURL string) (owner, repo string) {
	m := repoURLPattern.FindStringSubmatch(repoURL)
	if len(m) < 2 {
		return "", ""
	}
	parts := strings.SplitN(m[1], "/", 2)
	if len(parts) != 2 {
		return "", ""
	}
	return parts[0], parts[1]
}
