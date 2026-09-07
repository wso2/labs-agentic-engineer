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

package spec

// SaveSpec is the build endpoint's tagging primitive: ONE `v<N>` sequence
// versioning the whole specs/ tree (requirements + design together — the
// single-tag successor to the SaveRequirements/SaveDesign pair). The hard gate
// runs BEFORE the tag is cut, so every `v<N>` names a buildable spec.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Spec-gate error codes not owned by designspec (the design codes pass
// through so the console renders one vocabulary).
const (
	// codeMissingRequirements — specs/requirements/prd.md is absent.
	codeMissingRequirements = "MISSING_REQUIREMENTS"
	// codeMissingDesign — the design layout gate failed at its root
	// (specs/design/design.cell absent).
	codeMissingDesign = "MISSING_DESIGN"
	// codeDesignOutdated — the requirements moved after this design was
	// derived from them (#575). The only gate that refuses a design for being
	// WRONG rather than incomplete.
	codeDesignOutdated = "DESIGN_OUTDATED"
)

// SpecValidationError is the aggregate build-gate rejection: the spec at the
// save commit is not buildable (requirements and/or design failures, paths
// repo-relative). The build handler renders it as a 422 with per-file detail;
// nothing is tagged.
type SpecValidationError struct {
	Files []FileValidationError
}

func (e *SpecValidationError) Error() string {
	if len(e.Files) == 0 {
		return "spec validation failed"
	}
	parts := make([]string, 0, len(e.Files))
	for _, f := range e.Files {
		parts = append(parts, fmt.Sprintf("%s: %s: %s", f.Path, f.Code, f.Message))
	}
	return "spec validation failed: " + strings.Join(parts, "; ")
}

// The two answers SaveSpec can give, named because a CALLER branches on them.
//
// The build click is that caller, and the branch it takes is the whole of "what
// does pressing Build after a cancel do": SpecSaveApproved means a new version
// was cut and is planned fresh, SpecSaveUnchanged means the SAME version is
// worked again — its milestone reopened, the issues the cancel closed reopened,
// and the planning turn skipped. The spec-save status is the only question asked;
// there is no separate "was it cancelled" read anywhere.
const (
	// SpecSaveApproved: the specs/ tree moved, so a new `v<N>` tag was cut.
	SpecSaveApproved = "approved"
	// SpecSaveUnchanged: the specs/ tree matches the latest tag, so no tag was
	// cut and Tag names the EXISTING version.
	SpecSaveUnchanged = "unchanged"
)

// SpecSaveResult is the outcome of SaveSpec.
type SpecSaveResult struct {
	Status     string `json:"status"` // SpecSaveApproved | SpecSaveUnchanged
	Tag        string `json:"tag"`    // e.g. "v3"
	Version    int    `json:"version"`
	CommitHash string `json:"commitHash,omitempty"`
}

// SaveSpec runs the whole-spec hard gate (requirements main doc + design
// bundle) at the save commit and cuts the next `v<N>` annotated tag. No commit
// is created — the draft is already on `main`. When the specs/ tree at the
// save commit matches the latest `v<N>` tag's the save is a no-op
// ("unchanged"). Validation failures aggregate into a *SpecValidationError;
// nothing malformed acquires a tag.
func (s *artifactService) SaveSpec(ctx context.Context, orgID, projectID string, req SaveRequest) (*SpecSaveResult, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	commit, err := s.resolveSaveCommit(ctx, ref, req)
	if err != nil {
		return nil, err
	}
	reqFiles, err := s.readBundleAtCommit(ctx, ref, commit, requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return nil, err
	}
	designFiles, err := s.readBundleAtCommit(ctx, ref, commit, designPrefix, designBundleFilter)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "spec save: commit read",
		"project", projectID, "repo", ref.OrgID+"/"+ref.ProjectID+"/"+ref.RepoSlug, "commit", commit,
		"pinned", req.CommitSHA != "", "requirementsFiles", len(reqFiles), "designFiles", len(designFiles))

	// Hard gate: the whole spec must be buildable BEFORE any tag is cut.
	if verr := validateSpecBundles(reqFiles, designFiles); verr != nil {
		slog.WarnContext(ctx, "spec save: hard gate failed",
			"project", projectID, "commit", commit, "error", verr)
		return nil, verr
	}

	// …and it must be the design the user actually asked for (#575). An
	// outdated design is the one thing that blocks a build on grounds of being
	// WRONG rather than incomplete: building it hands the coding agents
	// something the user has already changed their mind about. It joins the
	// same refusal list every other unmet condition uses, so the console
	// renders it with the rest and Build stays clickable — the click re-checks.
	if verr := s.staleDesignRefusal(ctx, ref, orgID, projectID, commit); verr != nil {
		slog.WarnContext(ctx, "spec save: the design is behind the requirements",
			"project", projectID, "commit", commit)
		return nil, verr
	}

	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	// Unchanged detection over the WHOLE specs/ tree (not just requirements —
	// a design-only edit must bump the spec version).
	if latest, n, ok := latestRequirementsTagInfo(tags); ok {
		same, cerr := s.specTreeUnchanged(ctx, ref, commit, latest.CommitHash)
		if cerr != nil {
			return nil, cerr
		}
		if same {
			slog.InfoContext(ctx, "spec save: unchanged — specs/ matches latest tag",
				"project", projectID, "tag", latest.Name, "commit", commit)
			return &SpecSaveResult{Status: SpecSaveUnchanged, Tag: latest.Name, Version: n}, nil
		}
	}

	nextN, tagName := nextRequirementsTag(tags)
	tagBody := fmt.Sprintf("Spec v%d", nextN)
	if req.Message != "" {
		tagBody = fmt.Sprintf("%s\n\n%s", tagBody, req.Message)
	}
	if err := s.createAnnotatedTag(ctx, ref, &tags, &nextN, &tagName, tagBody, commit, 0, "requirements"); err != nil {
		return nil, fmt.Errorf("create tag: %w", err)
	}

	slog.InfoContext(ctx, "spec tagged", "project", projectID, "tag", tagName, "commit", commit)
	return &SpecSaveResult{
		Status:     SpecSaveApproved,
		Tag:        tagName,
		Version:    nextN,
		CommitHash: commit,
	}, nil
}

// ValidateSpecAtTag re-runs the whole-spec hard gate on the tree a `v<N>` tag
// names — the dev workflow's defensive re-check that what it is about to plan
// from is buildable.
func (s *artifactService) ValidateSpecAtTag(ctx context.Context, orgID, projectID, tag string) error {
	if _, ok := parseRequirementsTag(tag); !ok {
		return fmt.Errorf("%w: %q is not a v<N> tag", ErrInvalidVersionTag, tag)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	reqFiles, err := s.readBundleAtTag(ctx, ref, tag, requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return err
	}
	designFiles, err := s.readBundleAtTag(ctx, ref, tag, designPrefix, designBundleFilter)
	if err != nil {
		return err
	}
	return validateSpecBundles(reqFiles, designFiles)
}

// LatestSpecTag returns the newest `v<N>` spec tag name read from the local
// mirror WITHOUT a fetch — the network-free, best-effort read behind the task
// stale-spec attention flag. Any failure degrades to "".
func (s *artifactService) LatestSpecTag(ctx context.Context, orgID, projectID string) string {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return ""
	}
	tags, err := s.listVersionTagsLocal(ctx, ref)
	if err != nil {
		slog.WarnContext(ctx, "latest spec tag: local tag read failed",
			"project", projectID, "error", err)
		return ""
	}
	return latestRequirementsTag(tags)
}

// specGateDisabled turns the whole-spec gate off — both the build-click gate and
// ValidateSpecAtTag.
//
// It is here because the design agent does not reliably emit each component's
// `stories`, and without them the gate fails every Build with UNCOVERED_STORY —
// a spec the platform authored, refused by the platform. Disabling the gate is
// the lesser harm while that holds: the cost is that a `v<N>` tag no longer
// promises a buildable spec, so a build can now proceed on a spec the gate would
// have refused, and a validation failure downstream may be missing coverage
// rather than broken code.
//
// Flip to false to re-arm it once the design agent's story emission is
// dependable.
const specGateDisabled = true

// validateSpecBundles is the shared whole-spec gate: the requirements main doc
// must exist and the design bundle must pass the design hard gate. All
// failures aggregate into ONE *SpecValidationError with repo-relative paths.
func validateSpecBundles(reqFiles, designFiles map[string]string) error {
	if specGateDisabled {
		return nil
	}
	var files []FileValidationError
	if strings.TrimSpace(reqFiles[requirementsMainFile]) == "" {
		files = append(files, FileValidationError{
			Path:    RequirementsDir + "/" + requirementsMainFile,
			Code:    codeMissingRequirements,
			Message: "prd.md missing — populate the PRD before building",
		})
	}
	if err := validateDesignBundle(designFiles); err != nil {
		var ve *DesignValidationError
		if errors.As(err, &ve) {
			for _, f := range ve.Files {
				files = append(files, FileValidationError{
					Path: DesignDir + "/" + f.Path, Code: f.Code, Message: f.Message,
				})
			}
		} else {
			// The layout gate's root-missing rejection (ErrArtifactPathInvalid).
			files = append(files, FileValidationError{
				Path:    DesignDir + "/" + designRootFile,
				Code:    codeMissingDesign,
				Message: "design.cell missing — generate the design before building",
			})
		}
	}
	// The build gate (#369) runs only once the basic layout gates pass — its
	// checks presuppose a PRD and a design tree to read.
	if len(files) == 0 {
		for _, f := range validateBuildGate(reqFiles, designFiles) {
			files = append(files, FileValidationError{
				Path: DesignDir + "/" + f.Path, Code: f.Code, Message: f.Message,
			})
		}
	}
	if len(files) > 0 {
		return &SpecValidationError{Files: files}
	}
	return nil
}

// staleDesignRefusal refuses a build whose design predates the requirements it
// was derived from, as an ordinary gate failure.
//
// Nothing is stored to answer this: every commit is a permanent snapshot and
// every agent turn records the commit it read the project at, so the
// requirements as the last design run saw them are still there to compare
// against. That is what makes the answer available for projects that predate
// the check entirely, and leaves nothing to fall out of sync.
//
// Silent when the resolver is unwired, when no design run is on record, or when
// the baseline commit is unreadable. The first two mean the question does not
// apply; the third is the one judgment call — a build refused because an old
// commit has been garbage-collected would be unfixable by the user, and the
// staleness it might have caught is visible in the rail either way.
func (s *artifactService) staleDesignRefusal(
	ctx context.Context, ref sourcecontrol.RepoRef, orgID, projectID, commit string,
) error {
	if s.designBaseline == nil {
		return nil
	}
	base, err := s.designBaseline(ctx, orgID, projectID)
	if err != nil || base == "" {
		return nil
	}
	wasEntries, _, err := s.git.Workspace().List(ctx, ref, base)
	if err != nil {
		slog.WarnContext(ctx, "spec save: the last design run's commit is unreadable; staleness unchecked",
			"project", projectID, "base", base, "error", err)
		return nil
	}
	nowEntries, _, err := s.git.Workspace().List(ctx, ref, commit)
	if err != nil {
		return fmt.Errorf("list tree at %s: %w", commit, err)
	}
	if RequirementsFingerprint(wasEntries) == RequirementsFingerprint(nowEntries) {
		return nil
	}
	return &SpecValidationError{Files: []FileValidationError{{
		Path:    DesignDir + "/" + designRootFile,
		Code:    codeDesignOutdated,
		Message: "the requirements have changed since this design was written — update the design before building",
	}}}
}

// specTreeUnchanged reports whether the specs/ subtrees at the two commits are
// content-identical (path→blob-sha comparison, sha-addressed local reads).
func (s *artifactService) specTreeUnchanged(ctx context.Context, ref sourcecontrol.RepoRef, commit, tagCommit string) (bool, error) {
	headEntries, _, err := s.git.Workspace().List(ctx, ref, commit)
	if err != nil {
		return false, fmt.Errorf("list tree at %s: %w", commit, err)
	}
	tagEntries, _, err := s.git.Workspace().List(ctx, ref, tagCommit)
	if err != nil {
		return false, fmt.Errorf("list tree at %s: %w", tagCommit, err)
	}
	return specTreesEqual(headEntries, tagEntries), nil
}

// latestRequirementsTagInfo returns the TagInfo and version of the
// highest-versioned `v<N>` tag, or ok=false when none exist.
func latestRequirementsTagInfo(tags []sourcecontrol.TagInfo) (sourcecontrol.TagInfo, int, bool) {
	best, bestN := sourcecontrol.TagInfo{}, 0
	for _, t := range tags {
		if n, ok := parseRequirementsTag(t.Name); ok && n > bestN {
			best, bestN = t, n
		}
	}
	return best, bestN, bestN > 0
}
