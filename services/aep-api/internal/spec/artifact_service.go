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

// Package artifacts is the requirements + design version store. It holds NO
// local per-project state: reads are served from the workspace-mounted bare
// mirror via the sourcecontrol.Workspace port (at the branch tip for the live draft,
// at a `v*` tag for an approved version), a save is the hard semantic gate
// followed by an annotated tag via Workspace.Tag (no commit — the accepted
// draft is already on `main` via the Files API), and a discard is one revert
// Mutate back to the last tag, pushed under origin's push-CAS. The feature
// holds no REST Git-Data dependency. Drafts live on the frontend; committed
// truth is the origin.
package spec

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ----- Errors -----

var (
	// ErrArtifactNotFound is returned when the requested artifact (bundle at
	// HEAD or at a tag) does not exist. Maps to 404 at the handler.
	ErrArtifactNotFound = errors.New("artifact not found")
	// ErrSpecTagNotFound is returned by ComponentCountAtTag when the spec tag
	// is absent from the local mirror — a data state (deleted tag, stale run
	// row from a recreated project), not an infrastructure failure; the
	// status read degrades instead of failing.
	ErrSpecTagNotFound = errors.New("spec tag not in local mirror")
	// ErrArtifactPathInvalid is returned for illegal-shape inputs and for the
	// save-gate layout failure (root file missing). Maps to 400.
	ErrArtifactPathInvalid = errors.New("invalid artifact path")
	// ErrNoRequirementsBaseline is returned by SaveDesign when no `v<N>` tag
	// exists yet — design tags must reference an existing requirements
	// version. Maps to 409.
	ErrNoRequirementsBaseline = errors.New("no requirements baseline — save requirements first")
	// ErrInvalidVersionTag is returned when a tag string in a path/query does
	// not parse as `v<N>` or `v<N>-<M>`. Maps to 400.
	ErrInvalidVersionTag = errors.New("invalid version tag")
	// ErrDesignNotFound is the design member of the artifact-not-found family —
	// raised when the design corpus is absent at HEAD / a tag.
	ErrDesignNotFound = errors.New("design not found")
)

// ----- Path constants -----

const (
	// RequirementsDir is the repo directory holding all requirement markdown
	// documents. Each file is one document; the bundle is versioned together as
	// a single artifact under `v<N>` tags.
	RequirementsDir = "specs/requirements"
	// DesignDir is the repo directory holding all design files. The
	// architecture artifact is multi-file: a root `design.cell` plus
	// `domain-model.md`, `flows/<slug>.md`, and `components/<name>/design.json`
	// (+ optional `openapi.yaml`) per component. Versioned as a single artifact
	// under `v<N>-<M>` tags.
	DesignDir = "specs/design"
	// requirementsMainFile is the canonical "main" requirements document (the
	// PRD). Its presence is the requirements save gate.
	requirementsMainFile = "prd.md"
	// designRootFile is the canonical root design document (the cell).
	// Its presence is part of the design save gate (layout).
	designRootFile = DesignRootFile
)

// ----- Wire shapes -----

// SaveRequest is the body of POST /artifacts/{kind}/save.
type SaveRequest struct {
	Message string `json:"message,omitempty"`
	// CommitSHA pins the commit the save gates and tags. The publish flow sets
	// it to the commit its files-apply just created so the save never re-reads
	// `heads/main` — GitHub's ref reads lag writes by seconds (observed live),
	// and a stale read here fails the gate or tags the wrong tree. Empty →
	// resolve HEAD (standalone save with no prior apply).
	CommitSHA string `json:"commitSha,omitempty"`
}

// commitSHAPattern is the accepted shape of a caller-provided CommitSHA
// (abbreviated or full hex object name).
var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

// resolveSaveCommit returns the commit a save operates on: the caller-provided
// CommitSHA when set (validated, never a ref read), else the current HEAD via
// the workspace mirror (the engine fetches origin first, so an unpinned save
// never sees a lagging ref).
func (s *artifactService) resolveSaveCommit(ctx context.Context, ref sourcecontrol.RepoRef, req SaveRequest) (string, error) {
	if req.CommitSHA != "" {
		if !commitSHAPattern.MatchString(req.CommitSHA) {
			return "", fmt.Errorf("%w: %q is not a commit sha", ErrArtifactPathInvalid, req.CommitSHA)
		}
		return req.CommitSHA, nil
	}
	head, err := s.git.Workspace().Head(ctx, ref, "")
	if err != nil {
		return "", fmt.Errorf("get head ref: %w", err)
	}
	return head, nil
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

// ----- Service -----

// ArtifactService is the typed entry-point for the artifact endpoints. Reads
// are workspace-at-HEAD / at-tag; saves cut tags; discards revert to the last
// tag.
type ArtifactService interface {
	// Design bundle at HEAD (recursive; keys relative to specs/design/). Only
	// consumer: ArtifactStore.ReadDesign, the shared design-read path used
	// throughout (component, project, provisioning, runtimeconfig, task, …).
	ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error)

	// Save / Discard.
	// SaveSpec is the single-tag save: whole-spec hard gate (requirements +
	// design) at the save commit, then the next `v<N>` tag covering the whole
	// specs/ tree. Validation failure returns *SpecValidationError (422).
	SaveSpec(ctx context.Context, orgID, projectID string, req SaveRequest) (*SpecSaveResult, error)
	// ValidateSpecAtTag re-runs the whole-spec gate at a `v<N>` tag — the dev
	// workflow's defensive pre-plan check.
	ValidateSpecAtTag(ctx context.Context, orgID, projectID, tag string) error
	// BuildScopeAtTag computes the tag's story scope (#369): the PRD's story
	// set + titles, and per-component claims from each design.json's
	// `stories`. Consumed by delivery/build (milestone identity) and
	// delivery/task (delta planning + the Serves-stories stamp).
	BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (BuildScope, error)
	// LatestSpecTag returns the newest `v<N>` tag name from the local mirror
	// WITHOUT a fetch, degrading to "" — the task stale-spec attention read.
	LatestSpecTag(ctx context.Context, orgID, projectID string) string
	SaveRequirements(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error)
	SaveDesign(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error)

	// Versions.
	ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error)
	ListDesignVersions(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error)
	// ListSpecVersionTags lists the `v<N>` spec version tags (newest first)
	// with the latest tag and whether specs/ moved since it (#117).
	ListSpecVersionTags(ctx context.Context, orgID, projectID string) (*TagList, error)
	GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	// GetDesignAtSpecTag reads the design bundle at a `v<N>` SPEC tag — the tag
	// a build actually has. GetDesignAtTag above takes the legacy `v<N>-<M>`
	// design-revision tag and REFUSES a spec tag outright, so a build-time
	// consumer must use this one.
	GetDesignAtSpecTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error)
	// GetDesignAtCommit reads the design bundle at an exact commit — the publish
	// flow's pinned-commit read (no ref resolution involved).
	GetDesignAtCommit(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error)

	// StatusSnapshot is the status poll's fetch-free git view: local head +
	// local tags, tree reads SHA-addressed (status_snapshot.go).
	StatusSnapshot(ctx context.Context, orgID, projectID string) (*StatusSnapshot, error)

	// RequirementsFingerprintAt reduces the requirements at one commit to a
	// comparable value (#575) — the staleness check reads them as the last
	// design run saw them. `at` is that run's recorded base commit; a commit
	// the mirror cannot resolve is an error, never a silent "unchanged",
	// because equal-by-accident is the failure that ships a stale design.
	RequirementsFingerprintAt(ctx context.Context, orgID, projectID, at string) (string, error)

	// SetDesignBaselineResolver wires the build gate's staleness input (#575):
	// the commit the newest successful design run read the project at. On the
	// interface because the composition root has to reach it, and it cannot be
	// a constructor argument — the turn store it reads from is built after this
	// service. nil is a documented no-op: the gate keeps working without it.
	SetDesignBaselineResolver(f func(ctx context.Context, orgID, projectID string) (string, error))
	// ComponentCountAtTag counts the design components at a spec tag — the
	// deploy stage's denominator. Local-only; unknown tag errors.
	ComponentCountAtTag(ctx context.Context, orgID, projectID, tag string) (int, error)
}

type artifactService struct {
	repo sourcecontrol.RepoRepository
	git  GitGateway
	// designBaseline resolves the commit the newest successful design run read
	// the project at, or "" when it has never designed. The build gate needs it
	// to refuse a design the requirements have moved past (#575).
	//
	// A function rather than a port because it is one fact, and optional
	// because the gate must keep working without it: a service assembled with
	// no baseline resolver simply does not run the staleness check, which is
	// the pre-#575 behaviour rather than a build that can never start.
	designBaseline func(ctx context.Context, orgID, projectID string) (string, error)
}

// SetDesignBaselineResolver wires the staleness check's input (#575). Wired at
// the composition root from the agent-turn store; nil is a documented no-op.
func (s *artifactService) SetDesignBaselineResolver(
	f func(ctx context.Context, orgID, projectID string) (string, error),
) {
	s.designBaseline = f
}

// NewArtifactService builds the workspace-backed ArtifactService. `git` is the
// git-object surface + credential resolver + save identities (the concrete
// gitOpsService); `repo` resolves the project's repo row (slug + branch).
func NewArtifactService(repo sourcecontrol.RepoRepository, git GitGateway) ArtifactService {
	return &artifactService{repo: repo, git: git}
}

// ----- Allowed extensions -----

// allowedRequirementExts is the set of file extensions recognised inside
// `specs/requirements/`. Markdown holds prose; `.excalidraw` holds rendered
// Excalidraw scene JSON; `.dsl` is the source-of-truth canvas DSL.
var allowedRequirementExts = []string{".md", ".excalidraw", ".dsl"}

// allowedDesignExts is the set of file extensions recognised inside
// `specs/design/`. Markdown holds prose + frontmatter; YAML is OpenAPI specs;
// JSON is the post-#70 component `design.json` (structured facts, save-gated
// against the published schema) plus the FE-derived `*.gen.json` projections;
// `.cell` is the project-level cell-diagram DSL (design.cell) that drives the
// live architecture diagram; `.dsl` is the per-component wireframes DSL
// (wireframes.dsl) — the build gate demands it for deployable
// web-applications, so it must ride the bundle the gate reads.
var allowedDesignExts = []string{".md", ".yaml", ".yml", ".json", ".cell", ".dsl"}

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

// ----- Reads (GitHub-at-HEAD / at-tag) -----

func (s *artifactService) ListDesignFiles(ctx context.Context, orgID, projectID string) (map[string]string, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.readBundleAtHead(ctx, ref, designPrefix, designBundleFilter)
}

func (s *artifactService) GetRequirementsAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if _, ok := parseRequirementsTag(tag); !ok {
		return nil, fmt.Errorf("%w: %q is not a v<N> tag", ErrInvalidVersionTag, tag)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.readBundleAtTag(ctx, ref, tag, requirementsPrefix, requirementsBundleFilter)
}

func (s *artifactService) GetDesignAtCommit(ctx context.Context, orgID, projectID, commitSHA string) (map[string]string, error) {
	if !commitSHAPattern.MatchString(commitSHA) {
		return nil, fmt.Errorf("%w: %q is not a commit sha", ErrArtifactPathInvalid, commitSHA)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.readBundleAtCommit(ctx, ref, commitSHA, designPrefix, designBundleFilter)
}

func (s *artifactService) GetDesignAtTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if _, _, ok := parseDesignTag(tag); !ok {
		return nil, fmt.Errorf("%w: %q is not a v<N>-<M> tag", ErrInvalidVersionTag, tag)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.readBundleAtTag(ctx, ref, tag, designPrefix, designBundleFilter)
}

// GetDesignAtSpecTag reads the design bundle at a `v<N>` spec tag.
//
// It exists because GetDesignAtTag next door parses its argument as a
// design-REVISION tag (`v<N>-<M>`, the legacy per-design sequence) and rejects
// anything else. A build only ever knows the spec tag, so wiring a build-time
// consumer to the other method fails on every real build with "not a v<N>-<M>
// tag" — which is exactly what happened to the roles ensure.
func (s *artifactService) GetDesignAtSpecTag(ctx context.Context, orgID, projectID, tag string) (map[string]string, error) {
	if _, ok := parseRequirementsTag(tag); !ok {
		return nil, fmt.Errorf("%w: %q is not a v<N> spec tag", ErrInvalidVersionTag, tag)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.readBundleAtTag(ctx, ref, tag, designPrefix, designBundleFilter)
}

// ----- Versions -----

func (s *artifactService) ListRequirementsVersions(ctx context.Context, orgID, projectID string) ([]RequirementsVersionInfo, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tagsToRequirementsVersions(tags), nil
}

func (s *artifactService) ListDesignVersions(ctx context.Context, orgID, projectID string) ([]DesignVersionInfo, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tagsToDesignVersions(tags), nil
}

// ----- Save (hard gate → tag at HEAD) -----

// SaveRequirements runs the requirements hard gate (prd.md must exist
// at HEAD) and cuts the next `v<N>` annotated tag pointing at HEAD. No commit is
// created — the accepted draft is already on `main`. When HEAD already matches
// the latest tag the save is a no-op ("unchanged").
func (s *artifactService) SaveRequirements(ctx context.Context, orgID, projectID string, req SaveRequest) (*RequirementsSaveResult, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	head, err := s.resolveSaveCommit(ctx, ref, req)
	if err != nil {
		return nil, err
	}
	files, err := s.readBundleAtCommit(ctx, ref, head, requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return nil, err
	}
	// The commit this save gates + tags (pinned by the publish flow's apply,
	// or the freshly-fetched branch tip when unpinned).
	slog.InfoContext(ctx, "requirements save: head read",
		"project", projectID, "repo", ref.OrgID+"/"+ref.ProjectID+"/"+ref.RepoSlug, "commit", head,
		"pinned", req.CommitSHA != "", "files", len(files))
	// Hard gate: requirements.md must exist.
	if strings.TrimSpace(files[requirementsMainFile]) == "" {
		slog.WarnContext(ctx, "requirements save: hard gate failed — requirements.md missing at HEAD",
			"project", projectID, "commit", head, "files", len(files))
		return nil, fmt.Errorf("%w: %s/%s missing — populate requirements before saving",
			ErrArtifactPathInvalid, RequirementsDir, requirementsMainFile)
	}

	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	// Unchanged detection: HEAD bundle == latest requirements tag's bundle.
	if latest := latestRequirementsTag(tags); latest != "" {
		if tagged, terr := s.readBundleAtTag(ctx, ref, latest, requirementsPrefix, requirementsBundleFilter); terr == nil && trimmedMapsEqual(files, tagged) {
			slog.InfoContext(ctx, "requirements save: unchanged — HEAD matches latest tag",
				"project", projectID, "tag", latest, "commit", head)
			return &RequirementsSaveResult{
				Status:  "unchanged",
				Tag:     latest,
				Version: latestRequirementsVersion(tags),
			}, nil
		}
	}

	nextN, tagName := nextRequirementsTag(tags)
	tagBody := fmt.Sprintf("Requirements v%d", nextN)
	if req.Message != "" && req.Message != "Update requirements" {
		tagBody = fmt.Sprintf("%s\n\n%s", tagBody, req.Message)
	}
	if err := s.createAnnotatedTag(ctx, ref, &tags, &nextN, &tagName, tagBody, head, 0, "requirements"); err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}

	slog.InfoContext(ctx, "requirements tagged at HEAD", "project", projectID, "tag", tagName, "commit", head)
	return &RequirementsSaveResult{
		Status:     "approved",
		Tag:        tagName,
		Version:    nextN,
		CommitHash: head,
	}, nil
}

// SaveDesign runs the design hard gate (layout + component design.json schema +
// OpenAPI parseability) and cuts the next `v<N>-<M>` annotated tag pointing at
// HEAD, where N is the latest requirements version. A validation failure returns
// a *DesignValidationError (422); ErrNoRequirementsBaseline when no `v<N>` tag
// exists. When HEAD already matches the latest design tag the save is
// "unchanged".
func (s *artifactService) SaveDesign(ctx context.Context, orgID, projectID string, req SaveRequest) (*DesignSaveResult, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	head, err := s.resolveSaveCommit(ctx, ref, req)
	if err != nil {
		return nil, err
	}
	files, err := s.readBundleAtCommit(ctx, ref, head, designPrefix, designBundleFilter)
	if err != nil {
		return nil, err
	}
	// The commit this save gates + tags (see the requirements twin).
	slog.InfoContext(ctx, "design save: head read",
		"project", projectID, "repo", ref.OrgID+"/"+ref.ProjectID+"/"+ref.RepoSlug, "commit", head,
		"pinned", req.CommitSHA != "", "files", len(files))
	// Hard gate: nothing malformed may acquire a tag.
	if err := validateDesignBundle(files); err != nil {
		slog.WarnContext(ctx, "design save: hard gate failed",
			"project", projectID, "commit", head, "files", len(files), "error", err)
		return nil, err
	}

	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	parentN := latestRequirementsVersion(tags)
	if parentN == 0 {
		slog.WarnContext(ctx, "design save: no requirements baseline tag",
			"project", projectID, "tags", len(tags))
		return nil, ErrNoRequirementsBaseline
	}

	// Unchanged detection: HEAD bundle == latest v<parentN>-<M> tag's bundle.
	if latestM := latestDesignRevision(tags, parentN); latestM > 0 {
		latestTag := designTagFor(parentN, latestM)
		if tagged, terr := s.readBundleAtTag(ctx, ref, latestTag, designPrefix, designBundleFilter); terr == nil && trimmedMapsEqual(files, tagged) {
			slog.InfoContext(ctx, "design save: unchanged — HEAD matches latest tag",
				"project", projectID, "tag", latestTag, "commit", head)
			return &DesignSaveResult{
				Status:              "unchanged",
				Tag:                 latestTag,
				RequirementsVersion: parentN,
				DesignRevision:      latestM,
			}, nil
		}
	}

	nextRev, tagName := nextDesignTag(tags, parentN)
	tagBody := fmt.Sprintf("Design v%d-%d", parentN, nextRev)
	if req.Message != "" && req.Message != "Update design" {
		tagBody = fmt.Sprintf("%s\n\n%s", tagBody, req.Message)
	}
	if err := s.createAnnotatedTag(ctx, ref, &tags, &nextRev, &tagName, tagBody, head, parentN, "design"); err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}

	slog.InfoContext(ctx, "design tagged at HEAD", "project", projectID, "tag", tagName, "commit", head)
	return &DesignSaveResult{
		Status:              "approved",
		Tag:                 tagName,
		RequirementsVersion: parentN,
		DesignRevision:      nextRev,
		CommitHash:          head,
	}, nil
}

// ----- Internal helpers -----

func (s *artifactService) requireReadyRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	repoRecord, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repoRecord == nil {
		return nil, sourcecontrol.ErrRepoNotFound
	}
	if repoRecord.Status != "ready" {
		return nil, sourcecontrol.ErrRepoNotReady
	}
	return repoRecord, nil
}

// readyRef resolves the ready repo row + its workspace-mount address in one
// step — every entrypoint's resolution (reads, saves, discards). orgID is the
// authenticated org; the mount path is derived from the row alone (design D6).
func (s *artifactService) readyRef(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, sourcecontrol.RepoRef, error) {
	repo, err := s.requireReadyRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, sourcecontrol.RepoRef{}, err
	}
	ref, err := sourcecontrol.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return nil, sourcecontrol.RepoRef{}, err
	}
	return repo, ref, nil
}

// trimmedMapsEqual compares two file maps for byte-equality after trimming
// surrounding whitespace on each value — the same equality the save flow uses to
// decide "unchanged".
func trimmedMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		if strings.TrimSpace(va) != strings.TrimSpace(vb) {
			return false
		}
	}
	return true
}
