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

// SaveSpec is the build endpoint's tagging primitive: ONE tag versioning the
// whole specs/ tree, requirements and design together. The hard gate runs
// BEFORE the tag is cut, so every version names a buildable spec.
//
// The tag carries the name the user gave it (ADR-0030); order comes from when
// it was cut, never from its name.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
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
	// SpecSaveApproved: the specs/ tree moved, so a new version tag was cut.
	SpecSaveApproved = "approved"
	// SpecSaveUnchanged: the specs/ tree matches the latest tag, so no tag was
	// cut and Tag names the EXISTING version.
	SpecSaveUnchanged = "unchanged"
)

// SpecSaveResult is the outcome of SaveSpec.
//
// Tag IS the version's identity (ADR-0030) — there is no separate number. A
// count of versions was reported here alongside it and nothing ever read it:
// two identifiers for one thing, one of which shifts under its own reader.
type SpecSaveResult struct {
	Status     string `json:"status"` // SpecSaveApproved | SpecSaveUnchanged
	Tag        string `json:"tag"`    // the name the user gave, e.g. "m1"
	CommitHash string `json:"commitHash,omitempty"`
}

// SaveSpec runs the whole-spec hard gate (requirements main doc + design
// bundle) at the save commit and cuts one annotated tag — the name the caller
// asked for, or a suggestion. No commit is created — the draft is already on
// `main`. When the specs/ tree at the save commit matches the latest version's
// the save is a no-op ("unchanged"). Validation failures aggregate into a
// *SpecValidationError; nothing malformed acquires a tag.
func (s *artifactService) SaveSpec(ctx context.Context, orgID, projectID string, req SaveRequest) (*SpecSaveResult, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	commit, err := s.resolveSaveCommit(ctx, ref, req)
	if err != nil {
		return nil, err
	}
	reqFiles, err := s.readBundleAtCommit(ctx, ref, commit, requirementsBundle)
	if err != nil {
		return nil, err
	}
	designFiles, err := s.readBundleAtCommit(ctx, ref, commit, designBundle)
	if err != nil {
		return nil, err
	}
	acceptanceFiles, err := s.readBundleAtCommit(ctx, ref, commit, acceptancePrefix, acceptanceBundleFilter)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "spec save: commit read",
		"project", projectID, "repo", ref.Owner+"/"+ref.Repo, "commit", commit,
		"pinned", req.CommitSHA != "", "requirementsFiles", len(reqFiles), "designFiles", len(designFiles))

	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	// What this version carries (B1): the user's pick, the unbuilt features it
	// needs and the unbuilt product-wide items that reach it, planned against
	// what earlier versions built. A feature whose design is out of date
	// (#575, per feature) or that waits on an open dependency (E2) cannot be
	// carried: building it would hand the coding agents something the user has
	// already changed their mind about, or cannot yet reach. Every other
	// feature builds.
	spec := reqspec.Parse(reqFiles)
	unavailable := map[string]string{}
	if s.designRuns != nil {
		if runs, rerr := s.designRuns(ctx, orgID, projectID); rerr == nil {
			for id, why := range s.staleFeatures(ctx, ref, projectID, runs, reqFiles) {
				unavailable[id] = why
			}
		}
	}
	for id, why := range req.Blocked {
		if _, ok := unavailable[id]; !ok {
			unavailable[id] = why
		}
	}
	plan, planErr := planVersion(spec, builtSoFar(tags), req.Pick, unavailable)

	// Hard gate: what this version carries must be buildable BEFORE any tag
	// is cut. It runs even when the pick was refused, so one save reports
	// every reason at once.
	gateErr := validateSpecBundles(reqFiles, designFiles, acceptanceFiles, storySet(spec, plan), plan.Features)
	if verr := joinValidation(planErr, gateErr); verr != nil {
		slog.WarnContext(ctx, "spec save: hard gate failed",
			"project", projectID, "commit", commit, "error", verr)
		return nil, verr
	}

	// Unchanged detection over the WHOLE specs/ tree (not just requirements —
	// a design-only edit must bump the spec version). The name the caller asked
	// for is deliberately ignored here: a name labels a snapshot, it does not
	// make one (ADR-0030), so an identical tree reuses its version rather than
	// spending a whole planning turn to change a word.
	if latest, ok := latestVersionTag(tags); ok {
		same, cerr := s.specTreeUnchanged(ctx, ref, commit, latest.CommitHash)
		if cerr != nil {
			return nil, cerr
		}
		// The same tree is the same version only when it carries the same
		// scope: building F3 next to an unchanged spec is a new version.
		if latestPlan, ok := parseScope(latest.Body); same && (!ok || samePlan(latestPlan, plan)) {
			slog.InfoContext(ctx, "spec save: unchanged — specs/ matches latest tag",
				"project", projectID, "tag", latest.Name, "commit", commit)
			return &SpecSaveResult{
				Status: SpecSaveUnchanged,
				Tag:    latest.Name,
			}, nil
		}
	}

	// The name is the user's when they gave one, and only then is a collision
	// terminal: a suggestion may be re-suggested past a racing pusher, but a
	// name somebody typed must never turn into a different one.
	tagName, named := strings.TrimSpace(req.Name), true
	if tagName == "" {
		tagName, named = suggestedVersionName(tags), false
	} else if verr := ValidateVersionName(tagName); verr != nil {
		return nil, fmt.Errorf("%w: %w", ErrVersionNameInvalid, verr)
	}
	message := scopeBody(plan)
	if req.Message != "" {
		message = req.Message + "\n\n" + message
	}
	if err := s.createVersionTag(ctx, ref, &tags, &tagName, message, commit, !named); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "spec tagged", "project", projectID, "tag", tagName, "commit", commit, "named", named)
	return &SpecSaveResult{
		Status:     SpecSaveApproved,
		Tag:        tagName,
		CommitHash: commit,
	}, nil
}

// specGateDisabled turns the whole-spec gate off at the build click.
//
// It was set while the design agent did not reliably emit each component's
// `stories`, which failed every Build with UNCOVERED_STORY. That reason is
// gone: the design agent emits stories, and the security-design write and save
// gates now reject an uncovered story at the point the file is written rather
// than at the build click.
//
// The flag stays as a kill switch: flip it back to true to let builds through
// unvalidated if the gate ever starts refusing specs the platform itself
// authored. The cost of that is what it always was — a `v<N>` tag no longer
// promises a buildable spec.
const specGateDisabled = false

// validateSpecBundles is the shared spec gate: the requirements main doc must
// exist, its IDs must hold, the design bundle must pass the design hard gate,
// and the acceptance files' story tags must hold (keys of each bundle relative
// to its directory). inScope (story IDs) and features are what the version
// carries: only their stories must be claimed by the design and on an
// acceptance rule. All failures aggregate into ONE *SpecValidationError with
// repo-relative paths.
func validateSpecBundles(reqFiles, designFiles, acceptanceFiles map[string]string, inScope map[string]bool, features []string) error {
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
	// The requirements' own IDs (skills/prd-contract, "IDs"): one home each,
	// never reused, and every feature a need or an Applies to names is live.
	for _, p := range reqspec.Parse(reqFiles).Problems() {
		files = append(files, FileValidationError{
			Path: RequirementsDir + "/" + p.Path, Code: p.Code, Message: p.Message,
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
		for _, f := range validateBuildGate(reqFiles, designFiles, inScope) {
			files = append(files, FileValidationError{
				Path: DesignDir + "/" + f.Path, Code: f.Code, Message: f.Message,
			})
		}
		files = append(files, acceptanceFindings(reqspec.Parse(reqFiles), acceptanceFiles, features)...)
	}
	if len(files) > 0 {
		return &SpecValidationError{Files: files}
	}
	return nil
}

// staleFeatures names each feature whose design predates its requirements
// (#575; per feature since E1), with the reason in words. Such a feature
// cannot be built until a design run covers it again; every other feature's
// design stands.
//
// A feature's design is made from its file and the product-wide items that
// reach it (reqspec.Basis). The run that designed it is the newest completed
// design run that covered it — one that named it (`/design F1 F2`), or a bare
// `/design`, which covered every feature designable at the commit it read.
// When the feature's basis then and now differ, its design is out of date. A
// feature no run has designed is not out of date: it has no design, which the
// coverage check reports.
//
// Nothing is stored to answer this: every commit is a permanent snapshot and
// every agent turn records the commit it read the project at.
//
// Empty when the resolver is unwired or no design run is on record. A run
// whose commit is unreadable is skipped: a build refused because an old commit
// has been garbage-collected would be unfixable by the user.
func (s *artifactService) staleFeatures(
	ctx context.Context, ref sourcecontrol.RepoRef, projectID string, runs []DesignRun, reqFiles map[string]string,
) map[string]string {
	if len(runs) == 0 {
		return nil
	}
	read := map[string]map[string]string{}
	filesAt := func(commit string) map[string]string {
		if files, ok := read[commit]; ok {
			return files
		}
		files, err := s.readBundleAtCommit(ctx, ref, commit, requirementsPrefix, requirementsBundleFilter)
		if err != nil {
			slog.WarnContext(ctx, "spec save: a design run's commit is unreadable; its features' staleness unchecked",
				"project", projectID, "base", commit, "error", err)
			files = nil
		}
		read[commit] = files
		return files
	}
	stale := map[string]string{}
	for _, f := range reqspec.Parse(reqFiles).Features {
		if !f.Designable() {
			continue
		}
		was := designedFrom(runs, f.ID, filesAt)
		if was == nil || reqspec.Basis(was, f.ID) == reqspec.Basis(reqFiles, f.ID) {
			continue
		}
		stale[f.ID] = fmt.Sprintf("it has changed since it was designed — update the design for %s first", f.ID)
	}
	return stale
}

// designedFrom is the requirements the newest run that designed a feature
// read, or nil when no readable run designed it.
func designedFrom(runs []DesignRun, featureID string, filesAt func(commit string) map[string]string) map[string]string {
	for _, run := range runs {
		if run.Features != nil {
			if !slices.Contains(run.Features, featureID) {
				continue
			}
			if files := filesAt(run.BaseRef); files != nil {
				return files
			}
			continue
		}
		files := filesAt(run.BaseRef)
		if files == nil {
			continue
		}
		for _, f := range reqspec.Parse(files).Features {
			if f.ID == featureID && f.Designable() {
				return files
			}
		}
	}
	return nil
}

// specTreeUnchanged reports whether the specs/ subtrees at the two commits are
// content-identical (path→blob-sha comparison, sha-addressed cacheable reads).
func (s *artifactService) specTreeUnchanged(ctx context.Context, ref sourcecontrol.RepoRef, commit, tagCommit string) (bool, error) {
	headEntries, _, err := s.git.List(ctx, ref, commit)
	if err != nil {
		return false, fmt.Errorf("list tree at %s: %w", commit, err)
	}
	tagEntries, _, err := s.git.List(ctx, ref, tagCommit)
	if err != nil {
		return false, fmt.Errorf("list tree at %s: %w", tagCommit, err)
	}
	return specTreesEqual(headEntries, tagEntries), nil
}
