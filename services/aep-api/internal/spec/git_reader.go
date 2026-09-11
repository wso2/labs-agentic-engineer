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

// Workspace-mount bundle reads. The per-project local clone is gone: the "working tree" is the
// untagged tip of `main`, and every read is one `Workspace.ReadBundle` against
// the shared bare mirror — at the branch tip for the live draft, at a `v*` tag
// for an approved version (the engine peels annotated tags), or at an exact
// commit for the publish flow's pinned read. Downstream consumers key on tags,
// so intermediate commits on main are harmless by construction.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// GitGateway is the git surface + credential resolver + save identities the
// artifact store drives. Everything — bundle reads, the save tag, the discard
// revert — goes through the Workspace port (the mounted bare mirror); the
// feature holds no REST Git-Data dependency. The concrete
// *sourcecontrol.gitOpsService satisfies it structurally — the same port the files
// feature consumes. No clone primitives.
type GitGateway interface {
	// Workspace is the mount-backed git engine serving all reads and writes.
	Workspace() sourcecontrol.Workspace
	Resolver() secrets.Resolver
	ResolveSaveIdentities(cred secrets.Credential) (*sourcecontrol.GitIdentity, *sourcecontrol.GitIdentity)
}

// The repo-relative directory prefixes the two bundles live under (trailing
// slash so a prefix match never straddles a sibling like `specs/designs/`).
const (
	requirementsPrefix = RequirementsDir + "/"
	designPrefix       = DesignDir + "/"
)

// bundleFilter decides whether a path RELATIVE to the read prefix belongs to the
// artifact bundle. Requirements are flat (top-level markdown / dsl / excalidraw);
// design is a recursive tree (markdown / yaml at any depth).
type bundleFilter func(rel string) bool

func requirementsBundleFilter(rel string) bool {
	return !strings.Contains(rel, "/") && hasAllowedRequirementExt(rel)
}

func designBundleFilter(rel string) bool {
	return hasAllowedDesignExt(rel)
}

// readBundleAt reads every blob under prefix at `at` (branch tip when empty, a
// "tags/<name>" ref, or an exact commit sha), returning path→content keyed
// RELATIVE to prefix, filtered by keep. One ReadBundle call — the engine
// freshens the mirror and streams blobs via plumbing; there is no per-blob
// fan-out to bound anymore.
func (s *artifactService) readBundleAt(ctx context.Context, ref sourcecontrol.RepoRef, at, prefix string, keep bundleFilter) (map[string]string, error) {
	files, _, err := s.git.Workspace().ReadBundle(ctx, ref, at, func(path string) bool {
		rel, ok := strings.CutPrefix(path, prefix)
		return ok && rel != "" && keep(rel)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(files))
	for path, content := range files {
		out[strings.TrimPrefix(path, prefix)] = content
	}
	return out, nil
}

// readBundleAtCommit reads the artifact bundle at an exact commit (the publish
// flow's pinned read — no ref resolution involved beyond object lookup).
func (s *artifactService) readBundleAtCommit(ctx context.Context, ref sourcecontrol.RepoRef, commitSHA, prefix string, keep bundleFilter) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, commitSHA, prefix, keep)
	if err != nil {
		return nil, fmt.Errorf("read bundle at %s: %w", commitSHA, err)
	}
	return files, nil
}

// readBundleAtHead reads the artifact bundle at the default branch's tip.
func (s *artifactService) readBundleAtHead(ctx context.Context, ref sourcecontrol.RepoRef, prefix string, keep bundleFilter) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, "", prefix, keep)
	if err != nil {
		return nil, fmt.Errorf("read bundle at head: %w", err)
	}
	slog.DebugContext(ctx, "bundle read at head",
		"repo", ref.OrgID+"/"+ref.ProjectID+"/"+ref.RepoSlug, "prefix", prefix, "files", len(files))
	return files, nil
}

// readBundleAtTag reads the artifact bundle at a `v*` tag. The engine resolves
// the ref and peels an annotated tag to its commit; an absent tag surfaces as
// ErrArtifactNotFound.
func (s *artifactService) readBundleAtTag(ctx context.Context, ref sourcecontrol.RepoRef, tag, prefix string, keep bundleFilter) (map[string]string, error) {
	files, err := s.readBundleAt(ctx, ref, "tags/"+tag, prefix, keep)
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
// The local read restores the Message the Git Data refs API could not expose
// (empty only for lightweight tags). Fetches origin first — the
// freshness-critical path (version lists, save prechecks).
func (s *artifactService) listVersionTags(ctx context.Context, ref sourcecontrol.RepoRef) ([]sourcecontrol.TagInfo, error) {
	return s.git.Workspace().ListTags(ctx, ref, "")
}

// listVersionTagsLocal is listVersionTags without the origin fetch — for the
// best-effort LatestDesignTag read (the task stale-design attention flag),
// which must not force a per-read network round-trip.
func (s *artifactService) listVersionTagsLocal(ctx context.Context, ref sourcecontrol.RepoRef) ([]sourcecontrol.TagInfo, error) {
	return s.git.Workspace().ListTagsLocal(ctx, ref, "")
}
