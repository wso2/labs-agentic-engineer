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

package gitfs

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// The mount layout (design §4). All helpers are pure functions of the
// workspace root and the RepoRef path key — the path is a function of the DB
// row, never of client input, and every segment is validated defensively
// anyway (defense in depth against a poisoned row).
//
//	<root>/repos/<orgId>/<projectId>/<repoSlug>/git/            bare mirror (never checked out)
//	<root>/repos/<orgId>/<projectId>/<repoSlug>/repo.lock       flock: SH reads, EX fetch/push/ref-move
//	<root>/repos/<orgId>/<projectId>/<repoSlug>/snapshots/<sha>/ immutable plain-file tree
//	<root>/trash/<id>/                                          two-phase delete staging
//	<root>/tmp/                                                 atomic clone/snapshot staging
//
// The skills repo is not special-cased: projectID "_skills", slug
// "org-skills" flow through the same derivation.

// segmentPattern is the allowed shape of one path segment (org, project,
// slug): dot, dash, underscore, alphanumerics — no separators, no traversal.
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

// sha40Pattern matches a full 40-hex git object name.
var sha40Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isHex40(s string) bool { return sha40Pattern.MatchString(s) }

// validateSegment rejects anything that is not a plain single path segment.
func validateSegment(kind, s string) error {
	if !segmentPattern.MatchString(s) || s == "." || s == ".." {
		return fmt.Errorf("gitfs: invalid %s path segment %q", kind, s)
	}
	return nil
}

// validateRef validates the three path-key segments of a RepoRef.
func validateRef(ref RepoRef) error {
	if err := validateSegment("org", ref.OrgID); err != nil {
		return err
	}
	if err := validateSegment("project", ref.ProjectID); err != nil {
		return err
	}
	return validateSegment("repo slug", ref.RepoSlug)
}

// ReposDir is <root>/repos.
func ReposDir(root string) string { return filepath.Join(root, "repos") }

// TrashDir is <root>/trash — renamed subtrees awaiting async purge.
func TrashDir(root string) string { return filepath.Join(root, "trash") }

// TmpDir is <root>/tmp — atomic clone/snapshot staging.
func TmpDir(root string) string { return filepath.Join(root, "tmp") }

// OrgDir is <root>/repos/<orgId> — the subtree TrashOrg renames away.
func OrgDir(root, orgID string) (string, error) {
	if err := validateSegment("org", orgID); err != nil {
		return "", err
	}
	return filepath.Join(ReposDir(root), orgID), nil
}

// RepoDir is <root>/repos/<orgId>/<projectId>/<repoSlug> — the renamable
// parent holding git/, repo.lock, and snapshots/.
func RepoDir(root string, ref RepoRef) (string, error) {
	if err := validateRef(ref); err != nil {
		return "", err
	}
	return filepath.Join(ReposDir(root), ref.OrgID, ref.ProjectID, ref.RepoSlug), nil
}

// ReferenceStoreDir is <repoDir>/references — the project's reference
// documents (console ADR-0017). They are NOT git content: nothing commits
// them, and `git archive` cannot carry them, so they live beside the mirror
// and are overlaid into each snapshot on the way out (see references.go).
//
// Placing them inside the repo dir rather than a sibling top-level tree buys
// two properties for free: `enforceOrgQuotas` does a whole-subtree `duDir` of
// repos/<orgId>, so their bytes already count against the per-org quota; and
// TrashRepo takes them with it, so "deleted with the project" needs no second
// delete hook. The reaper's slug-level walk is unaffected — it collects
// snapshots from snapshots/ and mirrors at the slug dir, never here.
func ReferenceStoreDir(root string, ref RepoRef) (string, error) {
	d, err := RepoDir(root, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "references"), nil
}

// SnapshotsDir is <repoDir>/snapshots.
func SnapshotsDir(root string, ref RepoRef) (string, error) {
	d, err := RepoDir(root, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "snapshots"), nil
}

// SnapshotDir is the immutable tree <repoDir>/snapshots/<sha>.
func SnapshotDir(root string, ref RepoRef, sha string) (string, error) {
	d, err := SnapshotsDir(root, ref)
	if err != nil {
		return "", err
	}
	if !isHex40(sha) {
		return "", fmt.Errorf("gitfs: invalid snapshot sha %q (want full 40-hex)", sha)
	}
	return filepath.Join(d, sha), nil
}

// GitSubdir and SnapshotsSubdir are the leaf-name helpers for callers that
// already hold the slug dir (repos/<orgId>/<projectId>/<repoSlug>) — the
// reaper walks the tree by hand and never reconstructs a RepoRef. They are
// the un-validated slugDir-relative twins of the canonical RepoRef-based
// SnapshotsDir above (which stays the single derivation point from a row);
// keeping the "git" / "snapshots" leaf names in one place.

// GitSubdir is <slugDir>/git.
func GitSubdir(slugDir string) string { return filepath.Join(slugDir, "git") }

// SnapshotsSubdir is <slugDir>/snapshots.
func SnapshotsSubdir(slugDir string) string { return filepath.Join(slugDir, "snapshots") }

// repoPaths bundles the derived per-repo paths one engine operation needs.
type repoPaths struct {
	repoDir      string
	gitDir       string
	lockPath     string
	snapshotsDir string
}

// pathsFor derives (and validates) every per-repo path for ref.
func (e *Engine) pathsFor(ref RepoRef) (repoPaths, error) {
	repoDir, err := RepoDir(e.root, ref)
	if err != nil {
		return repoPaths{}, err
	}
	return repoPaths{
		repoDir:      repoDir,
		gitDir:       filepath.Join(repoDir, "git"),
		lockPath:     filepath.Join(repoDir, "repo.lock"),
		snapshotsDir: filepath.Join(repoDir, "snapshots"),
	}, nil
}
