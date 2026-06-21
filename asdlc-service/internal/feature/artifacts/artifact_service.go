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

package artifacts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// ----- Errors -----

var (
	// ErrArtifactNotFound is returned when the requested artifact (working-tree
	// file or tagged version) does not exist. Maps to 404 at the handler.
	ErrArtifactNotFound = errors.New("artifact not found")
	// ErrArtifactPathInvalid is returned for path traversal / illegal-shape
	// inputs. Maps to 400.
	ErrArtifactPathInvalid = errors.New("invalid artifact path")
	// ErrIfMatchFailed is returned by PutFile when the supplied If-Match sha
	// does not equal the current working-tree blob sha. Maps to 412.
	ErrIfMatchFailed = errors.New("if-match precondition failed")
	// ErrNoVersionToDiscard is returned by Discard when no tag exists for the
	// artifact type. Maps to 404.
	ErrNoVersionToDiscard = errors.New("no saved version to revert to")
	// ErrConcurrentTagWrite is returned when `git tag -a` fails because the
	// tag already exists locally with a different commit. Maps to 409.
	ErrConcurrentTagWrite = errors.New("tag created concurrently by another writer")
	// ErrNoRequirementsBaseline is returned by SaveDesign when no `v<N>` tag
	// exists yet — design tags must reference an existing requirements
	// version. Maps to 409.
	ErrNoRequirementsBaseline = errors.New("no requirements baseline — save requirements first")
	// ErrInvalidVersionTag is returned when a tag string in a path/query does
	// not parse as `v<N>` or `v<N>-<M>`. Maps to 400.
	ErrInvalidVersionTag = errors.New("invalid version tag")
	// ErrSpecNotFound / ErrDesignNotFound are the spec/design members of the
	// artifact-not-found family — raised when the requirements or design corpus
	// is absent in the project's clone.
	ErrSpecNotFound   = errors.New("spec not found")
	ErrDesignNotFound = errors.New("design not found")
)

// ----- Path constants -----

const (
	// RequirementsDir is the working-tree directory holding all requirement
	// markdown documents. Each file is one document; the bundle is versioned
	// together as a single artifact.
	RequirementsDir = "specs/requirements"
	// DesignDir is the working-tree directory holding all design files. The
	// architecture artifact is multi-file: a root `design.md` plus
	// `components/<name>/design.md` (+ optional `openapi.yaml`) per component.
	// Versioned as a single artifact under `v<N>-<M>` tags.
	DesignDir = "specs/design"
	// requirementsMainFile is the canonical "main" requirements document.
	// Cannot be deleted/renamed at the BFF layer.
	requirementsMainFile = "requirements.md"
	// designRootFile is the canonical root design document (system overview).
	// Cannot be deleted at the BFF layer.
	designRootFile = "design.md"
)

// ----- Wire shapes -----

// FileResult is the response shape for single-file reads.
type FileResult struct {
	Content string `json:"content"`
	SHA     string `json:"sha"`
}

// PutResult is the response shape for PutFile.
type PutResult struct {
	SHA string `json:"sha"`
}

// SaveRequest is the body of POST /artifacts/{kind}/save.
type SaveRequest struct {
	Message string `json:"message,omitempty"`
}

// RequirementsSaveResult is the response of POST /artifacts/requirements/save.
type RequirementsSaveResult struct {
	Status     string `json:"status"` // "approved" | "unchanged"
	Tag        string `json:"tag"`    // e.g. "v3"
	Version    int    `json:"version"`
	CommitHash string `json:"commitHash,omitempty"`
}

// DesignSaveResult is the response of POST /artifacts/design/save.
type DesignSaveResult struct {
	Status              string `json:"status"` // "approved" | "unchanged"
	Tag                 string `json:"tag"`    // e.g. "v1-2"
	RequirementsVersion int    `json:"requirementsVersion"`
	DesignRevision      int    `json:"designRevision"`
	CommitHash          string `json:"commitHash,omitempty"`
}

// RequirementsListResult is the response of GET /artifacts/requirements: a
// snapshot of every file under `specs/requirements/` keyed by basename.
type RequirementsListResult struct {
	Files map[string]string `json:"files"`
}

// VersionFileResult wraps content read at a specific tag.
type VersionFileResult struct {
	Content string `json:"content"`
}

// VersionRequirementsResult is the response of
// GET /artifacts/requirements/versions/{tag}: the file map captured at that
// `v<N>` tag.
type VersionRequirementsResult struct {
	Tag     string            `json:"tag"`
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

// ----- Service -----

// ArtifactService is the typed entry-point for the artifact endpoints. It
// composes with gitOpsService so they share the per-project mutex +
// clone-readiness machinery.
type ArtifactService interface {
	// Generic file I/O — used for individual file reads/writes (both
	// requirements and design files). Path validation is the controller's
	// responsibility before calling these.
	GetFile(ctx context.Context, orgID, projectID, relPath string) (*FileResult, error)
	PutFile(ctx context.Context, orgID, projectID, relPath, content, ifMatch string) (*PutResult, error)

	// Requirements multi-file ops.
	ListRequirementFiles(ctx context.Context, orgID, projectID string) (map[string]string, error)
	DeleteRequirementFile(ctx context.Context, orgID, projectID, name string) error

	// Design multi-file ops. `sub` is relative to `specs/design/`.
	ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error)
	DeleteDesignFile(ctx context.Context, orgID, projectID, sub string) error
	DeleteDesignDirectory(ctx context.Context, orgID, projectID, sub string) error

	// Save / Discard.
	SaveRequirements(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error)
	SaveDesign(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error)
	DiscardRequirements(ctx context.Context, orgID, projectID string) (map[string]string, error)
	DiscardDesign(ctx context.Context, orgID, projectID string) (map[string]string, error)

	// Requirements directory snapshots (chat per-turn undo + chat
	// session baseline). Stored out-of-band under
	// `<clone>/.git/asdlc-reqchat-snapshots/` so they don't pollute the
	// working tree, get committed, or appear in tag lists. Auto-cleaned
	// when the clone directory is recreated.
	CaptureRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) (map[string]string, error)
	RestoreRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) (map[string]string, error)
	DeleteRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) error
	// ReadFileFromRequirementsSnapshot returns the content of a single
	// requirement file as captured in `snapshotID`. The `existed` flag
	// distinguishes "file was present in the snapshot" from "snapshot
	// existed but did not contain this file" — callers (per-file Revert)
	// use it to decide between write-back vs. delete. Returns
	// ErrArtifactNotFound only when the snapshot blob itself is missing.
	ReadFileFromRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID, filename string) (content string, existed bool, err error)

	// Versions.
	ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error)
	ListDesignVersions(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error)
	GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
}

// GitWorkspace is the narrow gitrepo capability surface the artifact save
// flow needs: the per-project clone lock, clone readiness, the authed-env
// helper, the GitHub client + credential resolver, and the save-identity /
// post-save-pull helpers. The GitOpsService port (and the concrete
// *gitOpsService) structurally satisfies it, so artifacts depends on this
// narrow consumer port instead of the concrete struct.
type GitWorkspace interface {
	RepoLock(projectID string) *sync.Mutex
	EnsureCloneReady(ctx context.Context, repoRecord *models.GitRepository) error
	PrepareAuthedEnv(ctx context.Context, repoRecord *models.GitRepository) ([]string, func(), error)
	ResolveSaveIdentities(cred credentials.Credential) (*gitrepo.GitIdentity, *gitrepo.GitIdentity)
	BestEffortPullDefaultBranch(ctx context.Context, repoRecord *models.GitRepository) error
	GitHubClient() gitrepo.GitHubClient
	Resolver() credentials.Resolver
}

type artifactService struct {
	repo   repositories.RepoRepository
	gitOps GitWorkspace
}

// NewArtifactService builds an ArtifactService that piggy-backs on the gitrepo
// GitWorkspace surface for shared infrastructure (per-project locks, clone
// readiness, credential resolution, and the Git Data API save helpers).
func NewArtifactService(repo repositories.RepoRepository, gitOps GitWorkspace) ArtifactService {
	return &artifactService{repo: repo, gitOps: gitOps}
}

// ----- Path validation -----

const maxArtifactBytes = 5 << 20 // 5 MiB cap

// validateRelPath ensures relPath is under specs/, has no .. segments, and
// after Clean still starts with specs/.
func validateRelPath(relPath string) error {
	if relPath == "" {
		return fmt.Errorf("%w: empty path", ErrArtifactPathInvalid)
	}
	clean := filepath.Clean(relPath)
	if clean != relPath {
		return fmt.Errorf("%w: non-canonical path %q", ErrArtifactPathInvalid, relPath)
	}
	if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") {
		return fmt.Errorf("%w: must be repo-relative under specs/", ErrArtifactPathInvalid)
	}
	parts := strings.Split(clean, string(filepath.Separator))
	if parts[0] != "specs" {
		return fmt.Errorf("%w: only specs/ paths are accessible via this API", ErrArtifactPathInvalid)
	}
	for _, p := range parts {
		if p == ".." {
			return fmt.Errorf("%w: traversal in path", ErrArtifactPathInvalid)
		}
	}
	return nil
}

// allowedRequirementExts is the set of file extensions recognised inside
// `specs/requirements/`. Markdown holds prose; `.excalidraw` holds
// rendered Excalidraw scene JSON for wireframes / domain models; `.dsl` is
// the source-of-truth canvas DSL the architect agent reads.
var allowedRequirementExts = []string{".md", ".excalidraw", ".dsl"}

// allowedDesignExts is the set of file extensions recognised inside
// `specs/design/`. Markdown holds prose + frontmatter for component design;
// YAML is for OpenAPI specs.
var allowedDesignExts = []string{".md", ".yaml", ".yml"}

func hasAllowedDesignExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowedDesignExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

func hasAllowedRequirementExt(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowedRequirementExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// validateRequirementFilename ensures `name` is a single basename ending in
// one of the allowed requirement extensions (no path separators, no traversal).
func validateRequirementFilename(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty filename", ErrArtifactPathInvalid)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("%w: filename must not contain path separators", ErrArtifactPathInvalid)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: invalid filename", ErrArtifactPathInvalid)
	}
	if !hasAllowedRequirementExt(name) {
		return fmt.Errorf("%w: requirement files must end with %s", ErrArtifactPathInvalid, strings.Join(allowedRequirementExts, " or "))
	}
	return nil
}

// RequirementFilePath returns the repo-relative path for a requirement file
// after validating its name. Exported so HTTP handlers can validate without
// duplicating the rules.
func RequirementFilePath(name string) (string, error) {
	if err := validateRequirementFilename(name); err != nil {
		return "", err
	}
	return filepath.Join(RequirementsDir, name), nil
}

// validateDesignSubPath validates a path relative to `specs/design/`. The
// path may contain forward slashes (e.g. `components/user-api/design.md`)
// but must not have backslashes, traversal segments, or trailing slashes.
// The leaf must end in an allowed design extension.
func validateDesignSubPath(sub string) error {
	if sub == "" {
		return fmt.Errorf("%w: empty design path", ErrArtifactPathInvalid)
	}
	if strings.Contains(sub, "\\") {
		return fmt.Errorf("%w: backslashes not allowed", ErrArtifactPathInvalid)
	}
	clean := filepath.ToSlash(filepath.Clean(sub))
	if clean != filepath.ToSlash(sub) {
		return fmt.Errorf("%w: non-canonical design path %q", ErrArtifactPathInvalid, sub)
	}
	if strings.HasPrefix(clean, "/") {
		return fmt.Errorf("%w: design path must be relative", ErrArtifactPathInvalid)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: invalid segment %q", ErrArtifactPathInvalid, seg)
		}
	}
	if !hasAllowedDesignExt(clean) {
		return fmt.Errorf("%w: design files must end with %s",
			ErrArtifactPathInvalid, strings.Join(allowedDesignExts, " or "))
	}
	return nil
}

// validateDesignSubDir validates a directory path relative to
// `specs/design/` (used by DeleteDesignDirectory). No extension is required.
func validateDesignSubDir(sub string) error {
	if sub == "" {
		return fmt.Errorf("%w: empty design directory path", ErrArtifactPathInvalid)
	}
	if strings.Contains(sub, "\\") {
		return fmt.Errorf("%w: backslashes not allowed", ErrArtifactPathInvalid)
	}
	clean := filepath.ToSlash(filepath.Clean(sub))
	if clean != filepath.ToSlash(sub) {
		return fmt.Errorf("%w: non-canonical design directory %q", ErrArtifactPathInvalid, sub)
	}
	if strings.HasPrefix(clean, "/") {
		return fmt.Errorf("%w: design directory must be relative", ErrArtifactPathInvalid)
	}
	for _, seg := range strings.Split(clean, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("%w: invalid segment %q", ErrArtifactPathInvalid, seg)
		}
	}
	return nil
}

// DesignFilePath returns the repo-relative path for a design file after
// validating its sub-path. Exported so HTTP handlers can validate without
// duplicating the rules.
func DesignFilePath(sub string) (string, error) {
	if err := validateDesignSubPath(sub); err != nil {
		return "", err
	}
	return filepath.Join(DesignDir, sub), nil
}

// ----- Generic file ops -----

func (s *artifactService) GetFile(ctx context.Context, orgID, projectID, relPath string) (*FileResult, error) {
	if err := validateRelPath(relPath); err != nil {
		return nil, err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	abs := filepath.Join(repoRecord.ClonePath, relPath)
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}

	sha, err := blobSHAFor(ctx, repoRecord.ClonePath, data)
	if err != nil {
		slog.WarnContext(ctx, "hash-object failed", "path", relPath, "error", err)
	}
	return &FileResult{Content: string(data), SHA: sha}, nil
}

func (s *artifactService) PutFile(ctx context.Context, orgID, projectID, relPath, content, ifMatch string) (*PutResult, error) {
	if err := validateRelPath(relPath); err != nil {
		return nil, err
	}
	if len(content) > maxArtifactBytes {
		return nil, fmt.Errorf("%w: content exceeds %d bytes", ErrArtifactPathInvalid, maxArtifactBytes)
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	abs := filepath.Join(repoRecord.ClonePath, relPath)

	if ifMatch != "" {
		current, err := os.ReadFile(abs)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("read for if-match: %w", err)
		}
		var currentSHA string
		if err == nil {
			currentSHA, _ = blobSHAFor(ctx, repoRecord.ClonePath, current)
		}
		if currentSHA != ifMatch {
			return nil, ErrIfMatchFailed
		}
	}

	if err := atomicWrite(abs, []byte(content)); err != nil {
		return nil, fmt.Errorf("write %s: %w", relPath, err)
	}

	sha, err := blobSHAFor(ctx, repoRecord.ClonePath, []byte(content))
	if err != nil {
		return nil, fmt.Errorf("hash-object: %w", err)
	}
	return &PutResult{SHA: sha}, nil
}

// ----- Requirements multi-file ops -----

func (s *artifactService) ListRequirementFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	dir := filepath.Join(repoRecord.ClonePath, RequirementsDir)
	return readMarkdownDir(dir)
}

// readMarkdownDir reads all *.md files at the top level of `dir`. A missing
// directory yields an empty map (not an error) so first-time projects
// surface as "no documents yet".
func readMarkdownDir(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !hasAllowedRequirementExt(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", dir, name, err)
		}
		out[name] = string(data)
	}
	return out, nil
}

func (s *artifactService) DeleteRequirementFile(ctx context.Context, orgID, projectID, name string) error {
	relPath, err := RequirementFilePath(name)
	if err != nil {
		return err
	}
	if name == requirementsMainFile {
		return fmt.Errorf("%w: %s cannot be deleted", ErrArtifactPathInvalid, requirementsMainFile)
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return fmt.Errorf("ensure clone: %w", err)
	}

	abs := filepath.Join(repoRecord.ClonePath, relPath)
	if err := os.Remove(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrArtifactNotFound
		}
		return fmt.Errorf("remove %s: %w", relPath, err)
	}
	return nil
}

// ----- Save -----

// SaveRequirements persists the working-tree `specs/requirements/` snapshot
// as a new commit on remote main and creates the next `v<N>` annotated tag
// via GitHub API calls (Git Data API path) per docs/design/artifact-store-v2.md.
//
// The local clone's HEAD provides the "what we last saved" baseline for
// computing the changeset (adds / modifies / deletes), so users' explicit
// deletions still land as tombstones. Unrelated files on remote main are
// preserved by `base_tree=current main tree`.
func (s *artifactService) SaveRequirements(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	commitMsg := req.Message
	if commitMsg == "" {
		commitMsg = "Update requirements"
	}
	return s.saveRequirementsViaAPI(ctx, repoRecord, repoRecord.ClonePath, commitMsg)
}

// SaveDesign stages every file under `specs/design/` (root `design.md` plus
// per-component `design.md` and optional `openapi.yaml`), pushes the
// changeset to remote main via the GitHub Git Data API, then creates the
// next `v<N>-<M>` annotated tag where N is the latest requirements version.
// See docs/design/artifact-store-v2.md.
//
// Returns ErrNoRequirementsBaseline (409) if no `v<N>` tag exists yet, and
// ErrArtifactPathInvalid (400) if the root `design.md` is missing — a save
// must produce at least that file.
func (s *artifactService) SaveDesign(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	commitMsg := req.Message
	if commitMsg == "" {
		commitMsg = "Update design"
	}
	return s.saveDesignViaAPI(ctx, repoRecord, repoRecord.ClonePath, commitMsg)
}

// ----- Discard -----

// DiscardRequirements reverts the working-tree `specs/requirements/`
// directory to its content at the latest `v<N>` tag. Files added since that
// tag are removed; deletions are restored. Returns ErrNoVersionToDiscard if
// no `v<N>` tag exists.
func (s *artifactService) DiscardRequirements(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}
	clonePath := repoRecord.ClonePath

	authedEnv, cleanup, err := s.gitOps.PrepareAuthedEnv(ctx, repoRecord)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := bestEffortFetchTags(ctx, clonePath, authedEnv); err != nil {
		slog.WarnContext(ctx, "discard: fetch --tags failed (continuing)",
			"project", projectID, "error", err)
	}

	tags, err := listAllTags(ctx, clonePath)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	tagName := latestRequirementsTag(tags)
	if tagName == "" {
		return nil, ErrNoVersionToDiscard
	}

	if err := restoreDirAtTag(ctx, clonePath, tagName, RequirementsDir); err != nil {
		return nil, err
	}
	return readMarkdownDir(filepath.Join(clonePath, RequirementsDir))
}

// DiscardDesign reverts the working-tree `specs/design/` directory to its
// content at the latest `v<N>-<M>` tag. Files added since that tag are
// removed; deletions are restored. Returns ErrNoVersionToDiscard if no
// design tag exists.
func (s *artifactService) DiscardDesign(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}
	clonePath := repoRecord.ClonePath

	authedEnv, cleanup, err := s.gitOps.PrepareAuthedEnv(ctx, repoRecord)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	if err := bestEffortFetchTags(ctx, clonePath, authedEnv); err != nil {
		slog.WarnContext(ctx, "discard: fetch --tags failed (continuing)",
			"project", projectID, "error", err)
	}

	tags, err := listAllTags(ctx, clonePath)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	tagName := latestDesignTag(tags)
	if tagName == "" {
		return nil, ErrNoVersionToDiscard
	}

	if err := restoreDirAtTag(ctx, clonePath, tagName, DesignDir); err != nil {
		return nil, err
	}
	return readDesignDirRecursive(filepath.Join(clonePath, DesignDir))
}

// ----- Versions -----

func (s *artifactService) ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error) {
	tags, err := s.fetchAndListAllTags(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return tagsToRequirementsVersions(tags), nil
}

func (s *artifactService) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error) {
	tags, err := s.fetchAndListAllTags(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return tagsToDesignVersions(tags), nil
}

func (s *artifactService) GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	n, ok := parseRequirementsTag(tag)
	if !ok {
		return nil, fmt.Errorf("%w: %q is not a v<N> tag", ErrInvalidVersionTag, tag)
	}
	_ = n // version is implicit in tag

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}
	clonePath := repoRecord.ClonePath

	out, err := readMarkdownDirAtTag(ctx, clonePath, tag, RequirementsDir)
	if err != nil {
		if errors.Is(err, ErrArtifactNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("read %s at %s: %w", RequirementsDir, tag, err)
	}
	return out, nil
}

func (s *artifactService) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if _, _, ok := parseDesignTag(tag); !ok {
		return nil, fmt.Errorf("%w: %q is not a v<N>-<M> tag", ErrInvalidVersionTag, tag)
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}
	clonePath := repoRecord.ClonePath

	out, err := readDesignDirAtTagRecursive(ctx, clonePath, tag, DesignDir)
	if err != nil {
		if errors.Is(err, ErrArtifactNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("read %s at %s: %w", DesignDir, tag, err)
	}
	return out, nil
}

// ----- Design multi-file ops -----

func (s *artifactService) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	dir := filepath.Join(repoRecord.ClonePath, DesignDir)
	return readDesignDirRecursive(dir)
}

func (s *artifactService) DeleteDesignFile(ctx context.Context, orgID, projectID, sub string) error {
	if err := validateDesignSubPath(sub); err != nil {
		return err
	}
	if sub == designRootFile {
		return fmt.Errorf("%w: %s cannot be deleted", ErrArtifactPathInvalid, designRootFile)
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return fmt.Errorf("ensure clone: %w", err)
	}

	abs := filepath.Join(repoRecord.ClonePath, DesignDir, sub)
	if err := os.Remove(abs); err != nil {
		if os.IsNotExist(err) {
			return ErrArtifactNotFound
		}
		return fmt.Errorf("remove %s/%s: %w", DesignDir, sub, err)
	}

	// Best-effort: if the parent directory is now empty (e.g. last file under
	// `components/<name>/` was removed), clean it up so the tree doesn't show
	// an empty folder.
	parent := filepath.Dir(abs)
	designAbs := filepath.Join(repoRecord.ClonePath, DesignDir)
	for parent != designAbs && strings.HasPrefix(parent, designAbs) {
		entries, err := os.ReadDir(parent)
		if err != nil || len(entries) > 0 {
			break
		}
		if err := os.Remove(parent); err != nil {
			break
		}
		parent = filepath.Dir(parent)
	}
	return nil
}

func (s *artifactService) DeleteDesignDirectory(ctx context.Context, orgID, projectID, sub string) error {
	if err := validateDesignSubDir(sub); err != nil {
		return err
	}
	// Refuse to delete the design dir root (would wipe the whole artifact).
	if sub == "." || sub == "" {
		return fmt.Errorf("%w: cannot delete the design root", ErrArtifactPathInvalid)
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return fmt.Errorf("ensure clone: %w", err)
	}

	abs := filepath.Join(repoRecord.ClonePath, DesignDir, sub)
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrArtifactNotFound
		}
		return fmt.Errorf("stat %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrArtifactPathInvalid, sub)
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("remove %s: %w", abs, err)
	}
	return nil
}

// ----- Internal helpers -----

func (s *artifactService) requireReadyRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error) {
	repoRecord, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repoRecord == nil {
		return nil, gitrepo.ErrRepoNotFound
	}
	if repoRecord.Status != "ready" {
		return nil, gitrepo.ErrRepoNotReady
	}
	return repoRecord, nil
}

// fetchAndListAllTags acquires the repo lock, ensures the clone is ready,
// best-effort fetches remote tags, and returns the full local tag list.
func (s *artifactService) fetchAndListAllTags(ctx context.Context, orgID, projectID string) ([]gitrepo.TagInfo, error) {
	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}
	clonePath := repoRecord.ClonePath

	authedEnv, cleanup, err := s.gitOps.PrepareAuthedEnv(ctx, repoRecord)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	_ = bestEffortFetchTags(ctx, clonePath, authedEnv)
	return listAllTags(ctx, clonePath)
}

// restoreDirAtTag rewrites the working-tree directory at `relPath` to match
// the tagged version: removes the current contents (to handle files added
// since the tag) and runs `git checkout <tag> -- <relPath>` to restore the
// snapshot.
func restoreDirAtTag(ctx context.Context, clonePath, tag, relPath string) error {
	abs := filepath.Join(clonePath, relPath)
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("clear %s: %w", relPath, err)
	}
	if err := runGit(ctx, clonePath, "checkout", tag, "--", relPath); err != nil {
		return fmt.Errorf("git checkout %s -- %s: %w", tag, relPath, err)
	}
	return nil
}

// readDesignDirRecursive walks `dir` and returns every file with an
// allowed design extension, keyed by path RELATIVE to `dir` (forward
// slashes). Missing dir → empty map (no error).
func readDesignDirRecursive(dir string) (map[string]string, error) {
	out := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == dir {
				return filepath.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !hasAllowedDesignExt(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		out[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", dir, err)
	}
	return out, nil
}

// readDesignDirAtTagRecursive reads every allowed-extension file under
// `relPath` at `tag` from the git object store (no working-tree
// side-effects). Returns ErrArtifactNotFound when the directory entry
// doesn't exist at that tag.
func readDesignDirAtTagRecursive(ctx context.Context, clonePath, tag, relPath string) (map[string]string, error) {
	// `git ls-tree -r` lists every blob recursively under the tree.
	out, err := runGitOutput(ctx, clonePath, "ls-tree", "-r", "--name-only", tag+":"+relPath)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Not a valid object name") || strings.Contains(errMsg, "does not exist") {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("ls-tree: %w", err)
	}
	files := make(map[string]string)
	for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !hasAllowedDesignExt(name) {
			continue
		}
		content, err := runGitOutput(ctx, clonePath, "show", tag+":"+relPath+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("show %s/%s: %w", relPath, name, err)
		}
		files[name] = content
	}
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return files, nil
}

// readMarkdownDirAtTag reads every *.md file under `relPath` at `tag` from
// the git object store (no working-tree side-effects). Returns
// ErrArtifactNotFound when the directory entry doesn't exist at that tag.
func readMarkdownDirAtTag(ctx context.Context, clonePath, tag, relPath string) (map[string]string, error) {
	out, err := runGitOutput(ctx, clonePath, "ls-tree", "--name-only", tag+":"+relPath)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "Not a valid object name") || strings.Contains(errMsg, "does not exist") {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("ls-tree: %w", err)
	}
	files := make(map[string]string)
	for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || !hasAllowedRequirementExt(name) {
			continue
		}
		content, err := runGitOutput(ctx, clonePath, "show", tag+":"+filepath.Join(relPath, name))
		if err != nil {
			return nil, fmt.Errorf("show %s/%s: %w", relPath, name, err)
		}
		files[name] = content
	}
	// Stable iteration for tests (callers shouldn't rely on order in a map,
	// but keep determinism for snapshot diffs).
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return files, nil
}

// listAllTags lists every tag in the local clone (used by callers that want
// to filter by regex rather than by glob prefix).
func listAllTags(ctx context.Context, clonePath string) ([]gitrepo.TagInfo, error) {
	output, err := runGitOutput(ctx, clonePath, "tag", "-l", "--sort=-version:refname")
	if err != nil {
		return nil, fmt.Errorf("git tag -l: %w", err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return []gitrepo.TagInfo{}, nil
	}
	lines := strings.Split(output, "\n")
	tags := make([]gitrepo.TagInfo, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		hash, err := runGitOutput(ctx, clonePath, "rev-list", "-1", name)
		if err != nil {
			continue
		}
		msg, _ := runGitOutput(ctx, clonePath, "tag", "-l", name, "--format=%(contents)")
		tags = append(tags, gitrepo.TagInfo{
			Name:       name,
			CommitHash: strings.TrimSpace(hash),
			Message:    strings.TrimSpace(msg),
		})
	}
	return tags, nil
}

// atomicWrite writes data via a sibling temp file + rename so a partial
// write never leaves the target file truncated. Creates parent dirs as
// needed.
func atomicWrite(absPath string, data []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".tmp-"+filepath.Base(absPath)+"-"+hex.EncodeToString(suffix))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, absPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// blobSHAFor computes `git hash-object` for the given content.
func blobSHAFor(ctx context.Context, clonePath string, data []byte) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "hash-object", "--stdin")
	cmd.Dir = clonePath
	cmd.Stdin = bytes.NewReader(data)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("hash-object: %s: %w", stderr.String(), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// bestEffortFetchTags refreshes our local view of remote tags. The caller
// logs a warning on failure rather than aborting.
func bestEffortFetchTags(ctx context.Context, clonePath string, authedEnv []string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "--tags", "origin")
	cmd.Dir = clonePath
	cmd.Env = authedEnv
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("fetch --tags: %s", stderr.String())
	}
	return nil
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := runGitOutput(ctx, dir, args...)
	return err
}

// runGitOutput runs a git command and returns its stdout. On failure, stderr
// is included in the returned error so caller logs show git's own diagnostic.
func runGitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", msg, err)
	}
	return string(out), nil
}

// ----- Requirements snapshots (chat per-turn undo) -----

// snapshotIDPattern accepts the same `t_...` / ULID-ish shape the BFF
// generates. Conservative to prevent path traversal — IDs only ever come
// from the BFF, never from the user.
var snapshotIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func validateSnapshotID(id string) error {
	if !snapshotIDPattern.MatchString(id) {
		return fmt.Errorf("%w: invalid snapshot id", ErrArtifactPathInvalid)
	}
	return nil
}

// snapshotDir returns `<clonePath>/.git/asdlc-reqchat-snapshots`. The
// snapshots live under `.git` so git ignores them, they're not committed,
// and they're wiped when the clone is recreated.
func snapshotDir(clonePath string) string {
	return filepath.Join(clonePath, ".git", "asdlc-reqchat-snapshots")
}

func snapshotPath(clonePath, id string) string {
	return filepath.Join(snapshotDir(clonePath), id+".json")
}

// CaptureRequirementsSnapshot writes the current working-tree contents of
// `specs/requirements/` to a JSON blob keyed by `snapshotID`. Returns the
// captured file map. Idempotent — re-capturing the same id overwrites the
// blob.
func (s *artifactService) CaptureRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) (map[string]string, error) {
	if err := validateSnapshotID(snapshotID); err != nil {
		return nil, err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	files, err := readMarkdownDir(filepath.Join(repoRecord.ClonePath, RequirementsDir))
	if err != nil {
		return nil, err
	}

	dir := snapshotDir(repoRecord.ClonePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir snapshots: %w", err)
	}
	payload, err := json.Marshal(files)
	if err != nil {
		return nil, fmt.Errorf("encode snapshot: %w", err)
	}
	if err := atomicWrite(snapshotPath(repoRecord.ClonePath, snapshotID), payload); err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}
	slog.InfoContext(ctx, "captured requirements snapshot",
		"project", projectID, "snapshot", snapshotID, "files", len(files))
	return files, nil
}

// RestoreRequirementsSnapshot rewrites `specs/requirements/` to the
// contents captured under `snapshotID`. Files added since the snapshot are
// removed; files deleted since the snapshot are restored.
func (s *artifactService) RestoreRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) (map[string]string, error) {
	if err := validateSnapshotID(snapshotID); err != nil {
		return nil, err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return nil, fmt.Errorf("ensure clone: %w", err)
	}

	data, err := os.ReadFile(snapshotPath(repoRecord.ClonePath, snapshotID))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snapshot map[string]string
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}

	reqDir := filepath.Join(repoRecord.ClonePath, RequirementsDir)
	if err := os.RemoveAll(reqDir); err != nil {
		return nil, fmt.Errorf("clear %s: %w", RequirementsDir, err)
	}
	if err := os.MkdirAll(reqDir, 0o755); err != nil {
		return nil, fmt.Errorf("recreate %s: %w", RequirementsDir, err)
	}
	for name, content := range snapshot {
		// Defence in depth: only write recognised filenames. A malformed
		// snapshot blob can't smuggle traversal segments through.
		if err := validateRequirementFilename(name); err != nil {
			slog.WarnContext(ctx, "snapshot file skipped",
				"name", name, "error", err)
			continue
		}
		if err := atomicWrite(filepath.Join(reqDir, name), []byte(content)); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	slog.InfoContext(ctx, "restored requirements snapshot",
		"project", projectID, "snapshot", snapshotID, "files", len(snapshot))
	return readMarkdownDir(reqDir)
}

// ReadFileFromRequirementsSnapshot reads `filename` from the snapshot JSON
// blob (see CaptureRequirementsSnapshot). Returns:
//   - (content, true, nil) when the file existed in the snapshot.
//   - ("", false, nil)     when the snapshot exists but did not include
//     this file (the agent created it post-baseline). The caller treats
//     a Revert as "delete the working-tree file".
//   - ("", false, ErrArtifactNotFound) when the snapshot blob itself is
//     missing.
func (s *artifactService) ReadFileFromRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID, filename string) (string, bool, error) {
	if err := validateSnapshotID(snapshotID); err != nil {
		return "", false, err
	}
	if err := validateRequirementFilename(filename); err != nil {
		return "", false, err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return "", false, err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return "", false, fmt.Errorf("ensure clone: %w", err)
	}

	data, err := os.ReadFile(snapshotPath(repoRecord.ClonePath, snapshotID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, ErrArtifactNotFound
		}
		return "", false, fmt.Errorf("read snapshot: %w", err)
	}
	var snapshot map[string]string
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return "", false, fmt.Errorf("decode snapshot: %w", err)
	}
	content, ok := snapshot[filename]
	if !ok {
		return "", false, nil
	}
	return content, true, nil
}

// DeleteRequirementsSnapshot removes a stored snapshot. Idempotent.
func (s *artifactService) DeleteRequirementsSnapshot(ctx context.Context, orgID, projectID, snapshotID string) error {
	if err := validateSnapshotID(snapshotID); err != nil {
		return err
	}

	mu := s.gitOps.RepoLock(projectID)
	mu.Lock()
	defer mu.Unlock()

	repoRecord, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	if err := s.gitOps.EnsureCloneReady(ctx, repoRecord); err != nil {
		return fmt.Errorf("ensure clone: %w", err)
	}
	if err := os.Remove(snapshotPath(repoRecord.ClonePath, snapshotID)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove snapshot: %w", err)
	}
	return nil
}

// ConcatRequirementBundle joins all requirement files into a single corpus
// for agent input. Files are emitted in alphabetical order with a heading
// prefix so the LLM sees consistent boundaries between documents.
//
// Only Markdown content is included in the spec. `.excalidraw` JSON is
// noisy for the LLM (it's the rendered scene, not the DSL); `.dsl` files
// are surfaced separately via the architect's `read_wireframe` tool.
func ConcatRequirementBundle(files map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	names := make([]string, 0, len(files))
	for k := range files {
		if !strings.HasSuffix(strings.ToLower(k), ".md") {
			continue
		}
		names = append(names, k)
	}
	sort.Strings(names)
	var sb strings.Builder
	for i, name := range names {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("# %s\n\n", name))
		sb.WriteString(files[name])
	}
	return sb.String()
}
