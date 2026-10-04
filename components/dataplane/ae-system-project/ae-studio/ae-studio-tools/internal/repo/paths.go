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

package repo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// The mount layout (design §4). All helpers are pure functions of the
// workspace root and the RepoRef path key (the GitHub owner/repo,
// lower-cased); every segment is validated, so a hostile name can never
// traverse out of its dir.
//
//	<root>/repos/<owner>/<repo>/git/                   bare clone (never checked out)
//	<root>/repos/<owner>/<repo>/repo.lock              flock: SH reads, EX fetch/push/ref-move
//	<root>/snapshots/projects/<project>/<sha>/         immutable plain-file tree of a project commit
//	<root>/snapshots/skills/<sha>/                     immutable plain-file tree of an Org skills commit
//	<root>/references/<owner>/<repo>/                  a repo's stored reference documents (never committed)
//	<root>/trash/<id>/                                 two-phase delete staging
//	<root>/tmp/                                        atomic clone and snapshot staging, askpass shim
//
// ae-design-agent mounts only <root>/snapshots (subPath, read-only), so it
// never sees a mirror or a reference store. The bind pins the snapshots dir's
// inode: nothing may remove or rename it or its projects/ and skills/
// children, or the agent sees an empty tree until it restarts. Only <sha>
// leaves are ever removed (TrashSnapshot).

// segmentPattern is the allowed shape of one path segment (owner, repo,
// project): dot, dash, underscore, alphanumerics — no separators, no
// traversal.
var segmentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,200}$`)

// sha40Pattern matches a full 40-hex git object name.
var sha40Pattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isHex40(s string) bool { return sha40Pattern.MatchString(s) }

// validateSegment rejects anything that is not a plain single path segment.
func validateSegment(kind, s string) error {
	if !segmentPattern.MatchString(s) || s == "." || s == ".." {
		return fmt.Errorf("repo: invalid %s path segment %q", kind, s)
	}
	return nil
}

// ownerRepoDir is <base>/<owner>/<repo>, lower-cased: GitHub names are
// case-insensitive, so one repository has one dir whichever spelling a caller
// has.
func ownerRepoDir(base, owner, name string) (string, error) {
	owner, name = strings.ToLower(owner), strings.ToLower(name)
	if err := validateSegment("owner", owner); err != nil {
		return "", err
	}
	if err := validateSegment("repo", name); err != nil {
		return "", err
	}
	return filepath.Join(base, owner, name), nil
}

// ReposDir is <root>/repos.
func ReposDir(root string) string { return filepath.Join(root, "repos") }

// TrashDir is <root>/trash — renamed subtrees awaiting async purge.
func TrashDir(root string) string { return filepath.Join(root, "trash") }

// TmpDir is <root>/tmp — atomic clone staging and the askpass shim.
func TmpDir(root string) string { return filepath.Join(root, "tmp") }

// SnapshotsDir is <root>/snapshots, the dir ae-design-agent mounts.
func SnapshotsDir(root string) string { return filepath.Join(root, "snapshots") }

// ProjectSnapshotsDir is <root>/snapshots/projects.
func ProjectSnapshotsDir(root string) string { return filepath.Join(SnapshotsDir(root), "projects") }

// SkillsSnapshotsDir is <root>/snapshots/skills.
func SkillsSnapshotsDir(root string) string { return filepath.Join(SnapshotsDir(root), "skills") }

// SnapshotDir is the immutable tree <root>/snapshots/projects/<project>/<sha>
// (the agent reads it as /snapshots/projects/<project>/<sha>).
func SnapshotDir(root, project, sha string) (string, error) {
	if err := validateSegment("project", project); err != nil {
		return "", err
	}
	if !isHex40(sha) {
		return "", fmt.Errorf("repo: invalid snapshot sha %q (want full 40-hex)", sha)
	}
	return filepath.Join(ProjectSnapshotsDir(root), project, sha), nil
}

// SkillsSnapshotDir is the immutable tree <root>/snapshots/skills/<sha>.
func SkillsSnapshotDir(root, sha string) (string, error) {
	if !isHex40(sha) {
		return "", fmt.Errorf("repo: invalid snapshot sha %q (want full 40-hex)", sha)
	}
	return filepath.Join(SkillsSnapshotsDir(root), sha), nil
}

// ReferencesDir is <root>/references, the reference document stores.
func ReferencesDir(root string) string { return filepath.Join(root, "references") }

// ReferenceStoreDir is <root>/references/<owner>/<repo>, lower-cased (see
// ownerRepoDir).
func ReferenceStoreDir(root string, r OwnerRepo) (string, error) {
	return ownerRepoDir(ReferencesDir(root), r.Owner, r.Repo)
}

// Validate reports whether ref's owner/repo can key a mirror: the check
// RepoDir applies, for a caller that classifies a bad name before any git.
func (r RepoRef) Validate() error {
	_, err := ownerRepoDir("", r.Owner, r.Repo)
	return err
}

// RepoDir is <root>/repos/<owner>/<repo>, lower-cased (see ownerRepoDir) —
// the renamable parent holding git/ and repo.lock.
func RepoDir(root string, ref RepoRef) (string, error) {
	return ownerRepoDir(ReposDir(root), ref.Owner, ref.Repo)
}

// GitSubdir is the leaf-name helper for callers holding the repo dir.
func GitSubdir(repoDir string) string { return filepath.Join(repoDir, "git") }

// repoPaths bundles the derived per-repo paths one engine operation needs.
type repoPaths struct {
	repoDir  string
	gitDir   string
	lockPath string
}

// pathsFor derives (and validates) every per-repo path for ref.
func (e *Engine) pathsFor(ref RepoRef) (repoPaths, error) {
	repoDir, err := RepoDir(e.root, ref)
	if err != nil {
		return repoPaths{}, err
	}
	return repoPaths{
		repoDir:  repoDir,
		gitDir:   filepath.Join(repoDir, "git"),
		lockPath: filepath.Join(repoDir, "repo.lock"),
	}, nil
}
