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

// The artifact bundle reads, through the org's AE Studio pod (the
// sourcecontrol.Git port). aep-api holds no clone: the "working tree" is the
// untagged tip of the default branch, and every read is one ReadBundle — at
// the tip for the live draft, at a version tag for an approved version, or at
// an exact commit for the publish flow's pinned read. Downstream consumers key
// on tags, so intermediate commits on the branch are harmless by construction.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The repo-relative directory prefixes the two bundles live under (trailing
// slash so a prefix match never straddles a sibling like `specs/designs/`).
const (
	requirementsPrefix = RequirementsDir + "/"
	designPrefix       = DesignDir + "/"
)

// bundleFilter decides whether a path RELATIVE to the read prefix belongs to the
// artifact bundle. Requirements are the top-level files (markdown / dsl /
// excalidraw) plus the contract's feature and product-wide topic files
// (reqspec.IsNestedFile) — never references/, the user's source documents;
// design is a recursive tree (markdown / yaml at any depth).
type bundleFilter func(rel string) bool

func requirementsBundleFilter(rel string) bool {
	return (!strings.Contains(rel, "/") && hasAllowedRequirementExt(rel)) || reqspec.IsNestedFile(rel)
}

func designBundleFilter(rel string) bool {
	return hasAllowedDesignExt(rel)
}

// artifactBundle is one artifact's read: what the pod selects (prefix +
// extensions, so the reply carries only text files) and the layout rule
// applied to the reply (the requirements bundle is flat; the pod's filter has
// no depth).
type artifactBundle struct {
	filter sourcecontrol.BundleFilter
	keep   bundleFilter
}

var (
	// requirementsBundle: its readers consume the PRD only (the save gate and
	// the build scope), so markdown is all the pod need send.
	requirementsBundle = artifactBundle{
		filter: sourcecontrol.BundleFilter{Prefix: requirementsPrefix, Exts: []string{".md"}},
		keep:   requirementsBundleFilter,
	}
	designBundle = artifactBundle{
		filter: sourcecontrol.BundleFilter{Prefix: designPrefix, Exts: allowedDesignExts},
		keep:   designBundleFilter,
	}
	// acceptanceBundle: the Gherkin oracle the save gate checks stories
	// against. TODO(Task 47, API-7): compile stub from the main sync; pin the
	// pod's filter for it test-first.
	acceptanceBundle = artifactBundle{
		filter: sourcecontrol.BundleFilter{Prefix: acceptancePrefix, Exts: []string{".feature"}},
		keep:   acceptanceBundleFilter,
	}
)

// readBundleAt reads bundle b at `at` (the branch tip when empty, a
// "tags/<name>" ref, or a 40-hex commit sha), returning path→content keyed
// RELATIVE to the bundle's prefix. One ReadBundle call.
func (s *artifactService) readBundleAt(ctx context.Context, ref sourcecontrol.RepoRef, at string, b artifactBundle) (map[string]string, error) {
	files, _, err := s.git.ReadBundle(ctx, ref, at, b.filter)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for path, content := range files {
		rel, ok := strings.CutPrefix(path, b.filter.Prefix)
		if ok && rel != "" && b.keep(rel) {
			out[rel] = content
		}
	}
	return out, nil
}

// readBundleAtCommit reads the artifact bundle at an exact commit (the publish
// flow's pinned read — no ref resolution involved beyond object lookup).
func (s *artifactService) readBundleAtCommit(ctx context.Context, ref sourcecontrol.RepoRef, commitSHA string, b artifactBundle) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, commitSHA, b)
	if err != nil {
		return nil, fmt.Errorf("read bundle at %s: %w", commitSHA, err)
	}
	return files, nil
}

// readBundleAtHead reads the artifact bundle at the default branch's tip.
func (s *artifactService) readBundleAtHead(ctx context.Context, ref sourcecontrol.RepoRef, b artifactBundle) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, "", b)
	if err != nil {
		return nil, fmt.Errorf("read bundle at head: %w", err)
	}
	slog.DebugContext(ctx, "bundle read at head",
		"repo", ref.Owner+"/"+ref.Repo, "prefix", b.filter.Prefix, "files", len(files))
	return files, nil
}

// readBundleAtTag reads the artifact bundle at a version tag. The pod resolves
// the ref and peels an annotated tag to its commit; an absent tag surfaces as
// ErrArtifactNotFound.
func (s *artifactService) readBundleAtTag(ctx context.Context, ref sourcecontrol.RepoRef, tag string, b artifactBundle) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, "tags/"+tag, b)
	if err != nil {
		if errors.Is(err, sourcecontrol.ErrRefNotFound) {
			return nil, ErrArtifactNotFound
		}
		return nil, fmt.Errorf("read bundle at tag %s: %w", tag, err)
	}
	return files, nil
}

// listVersionTags returns EVERY tag with its Name, peeled commit SHA,
// annotation subject and creation time. Every tag, not `v*`: a version now
// carries the name the user gave it (ADR-0030), so the name cannot select
// them — `isVersionTag` reads the annotation subject instead, and the full
// listing is also what tells a suggestion which names are already claimed.
// The pod fetches GitHub first — the freshness-critical path (version lists,
// save prechecks).
func (s *artifactService) listVersionTags(ctx context.Context, ref sourcecontrol.RepoRef) ([]sourcecontrol.TagInfo, error) {
	return s.git.ListTags(ctx, ref, "")
}

// listVersionTagsLocal is listVersionTags without the fetch — the pod's
// mirror as it stands, for the status poll and the other reads that must not
// force a per-read network round-trip.
func (s *artifactService) listVersionTagsLocal(ctx context.Context, ref sourcecontrol.RepoRef) ([]sourcecontrol.TagInfo, error) {
	return s.git.ListTags(ctx, ref, "", sourcecontrol.Local())
}
