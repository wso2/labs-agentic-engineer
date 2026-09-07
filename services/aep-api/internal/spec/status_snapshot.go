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

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The status poll's fetch-free git reads (design/project-status-stage-
// aggregates.md D1): resolve the local mirror's head once, then address every
// tree read by SHA. Correct because aep-api is the sole writer of specs/ —
// platform writes update the mirror synchronously; out-of-band GitHub edits
// lag until the next fetch-bearing operation, by design.

// StatusSnapshot is the one-shot local-git view GetProjectStatus derives the
// spec stage and the flat artifact fields from.
type StatusSnapshot struct {
	// HeadSHA is the local mirror's default-branch tip; "" only when the
	// repo has no commits yet.
	HeadSHA string
	// HasSpec: any requirements file at head (the flat hasSpec predicate —
	// top-level allowed-extension file under specs/requirements/).
	HasSpec bool
	// HasDesign: the root design.cell exists at head with non-blank content
	// (ReadDesign's presence predicate). Accepted deviation from the retired
	// read: a design.cell with malformed frontmatter counts as present here —
	// the old path failed the whole status read on it, which is worse for a
	// poll endpoint; the save gate remains the validity enforcer.
	HasDesign bool
	// SpecVersion is the newest v<N> spec tag on the mirror; "" when never
	// published.
	SpecVersion string
	// SpecDirty: the specs/ subtree at head differs from specs/ at
	// SpecVersion. Always false when SpecVersion is "".
	SpecDirty bool
	// HasDesignTag: any legacy v<N>-<M> design tag exists — the flat
	// designStatus="approved" predicate.
	HasDesignTag bool
	// RequirementsFingerprint is the requirements AS THEY STAND, reduced to one
	// comparable value (#575). Computed from the head listing this snapshot
	// already walks, so it costs nothing extra; the staleness check compares it
	// against the same value at the last design run's base commit.
	RequirementsFingerprint string
}

// StatusSnapshot implements ArtifactService: the status poll's git source
// group. One local head resolution + one tree listing + one local tag list
// (+ one SHA-addressed tag tree when a version exists) — no origin fetch.
func (s *artifactService) StatusSnapshot(ctx context.Context, orgID, projectID string) (*StatusSnapshot, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}

	head, err := s.git.Workspace().HeadLocal(ctx, ref)
	if errors.Is(err, sourcecontrol.ErrRefNotFound) {
		// No commits yet — an empty snapshot, not an error.
		return &StatusSnapshot{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolve local head: %w", err)
	}

	headEntries, _, err := s.git.Workspace().List(ctx, ref, head)
	if err != nil {
		return nil, fmt.Errorf("list tree at head: %w", err)
	}
	tags, err := s.listVersionTagsLocal(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list local tags: %w", err)
	}

	snap := &StatusSnapshot{HeadSHA: head}
	for _, e := range headEntries {
		if rel, ok := strings.CutPrefix(e.Path, requirementsPrefix); ok && rel != "" && requirementsBundleFilter(rel) {
			snap.HasSpec = true
		}
		if e.Path == designPrefix+DesignRootFile {
			snap.HasDesign = true
		}
	}
	if snap.HasDesign {
		// Blank root = no design (the ReadDesign gate) — one sha-addressed
		// blob read, local only, and only when the file exists at all.
		content, _, err := s.git.Workspace().ReadFile(ctx, ref, head, designPrefix+DesignRootFile)
		if err != nil {
			return nil, fmt.Errorf("read design root at head: %w", err)
		}
		snap.HasDesign = strings.TrimSpace(string(content)) != ""
	}

	snap.RequirementsFingerprint = RequirementsFingerprint(headEntries)
	snap.HasDesignTag = latestDesignTag(tags) != ""
	if latest, _, ok := latestRequirementsTagInfo(tags); ok {
		snap.SpecVersion = latest.Name
		// Sha-addressed (the peeled tag commit) — a local read, no fetch.
		tagEntries, _, err := s.git.Workspace().List(ctx, ref, latest.CommitHash)
		if err != nil {
			return nil, fmt.Errorf("list tree at %s: %w", latest.Name, err)
		}
		snap.SpecDirty = !specTreesEqual(headEntries, tagEntries)
	}
	return snap, nil
}

// RequirementsFingerprintAt implements ArtifactService: the same reduction as
// the snapshot's, taken at an arbitrary commit. One SHA-addressed tree listing
// against the local mirror — no fetch, matching the status poll's budget.
func (s *artifactService) RequirementsFingerprintAt(ctx context.Context, orgID, projectID, at string) (string, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	entries, _, err := s.git.Workspace().List(ctx, ref, at)
	if err != nil {
		return "", fmt.Errorf("list tree at %s: %w", at, err)
	}
	return RequirementsFingerprint(entries), nil
}

// ComponentCountAtTag implements ArtifactService: the deploy stage's
// denominator — how many components the design at a spec tag declares.
// Local-only (tag resolved from the mirror's tag list, tree read
// SHA-addressed); an unknown tag is an error, never a silent zero.
func (s *artifactService) ComponentCountAtTag(ctx context.Context, orgID, projectID, tag string) (int, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return 0, err
	}
	tags, err := s.listVersionTagsLocal(ctx, ref)
	if err != nil {
		return 0, fmt.Errorf("list local tags: %w", err)
	}
	sha := ""
	for _, t := range tags {
		if t.Name == tag {
			sha = t.CommitHash
			break
		}
	}
	if sha == "" {
		return 0, fmt.Errorf("%w: %q", ErrSpecTagNotFound, tag)
	}
	entries, _, err := s.git.Workspace().List(ctx, ref, sha)
	if err != nil {
		return 0, fmt.Errorf("list tree at %s: %w", tag, err)
	}
	return countDesignComponents(entries), nil
}

// countDesignComponents counts specs/design/components/<name>/design.json
// blobs — AssembleDesign's component predicate (design.json is the authored
// component model) applied to a tree listing, no content reads.
func countDesignComponents(entries []sourcecontrol.Entry) int {
	prefix := designPrefix + componentDirPrefix
	n := 0
	for _, e := range entries {
		rel, ok := strings.CutPrefix(e.Path, prefix)
		if !ok {
			continue
		}
		if parts := strings.Split(rel, "/"); len(parts) == 2 && parts[1] == "design.json" {
			n++
		}
	}
	return n
}
