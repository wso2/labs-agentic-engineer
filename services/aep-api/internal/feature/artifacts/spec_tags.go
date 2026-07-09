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
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
)

// GET /projects/{p}/tags (#117). The spec is versioned as ONE incrementing
// `v<N>` sequence covering the whole specs/ tree (requirements + design +
// validation together); legacy `v<N>-<M>` design-revision tags are not part
// of the sequence and are excluded here.

// specTreePrefix scopes the dirtiness check: only blobs under specs/ count.
const specTreePrefix = "specs/"

// TagList is the wire shape of list-project-tags. Field semantics match the
// contract (packages/contracts/api/v1/openapi.yaml TagList).
type TagList struct {
	Tags []string `json:"tags" doc:"Spec version tags (v<N>), newest first."`
	// Latest is the newest user-tagged spec version; absent when nothing is
	// tagged yet.
	Latest string `json:"latest,omitempty" doc:"Newest spec version tag (e.g. v3); absent when nothing is tagged."`
	// SpecDirty is true when the specs/ tree at HEAD differs from the specs/
	// tree at Latest — the spec changed after it was last versioned.
	SpecDirty bool `json:"specDirty,omitempty" doc:"True when specs/ changed after latest was tagged."`
}

// ListSpecVersionTags lists the project's `v<N>` spec version tags, newest
// first, with the latest tag and whether specs/ moved since it. One origin
// fetch (the HEAD tree read; its refspec also freshens all tags), then
// local-mirror reads: the tag list and the sha-addressed tag tree.
func (s *artifactService) ListSpecVersionTags(ctx context.Context, orgID, projectID string) (*TagList, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}

	headEntries, _, err := s.git.Workspace().List(ctx, ref, "")
	if err != nil {
		return nil, fmt.Errorf("list head tree: %w", err)
	}
	tags, err := s.listVersionTagsLocal(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}

	type versionTag struct {
		n    int
		info gitrepo.TagInfo
	}
	var versions []versionTag
	for _, t := range tags {
		if n, ok := parseRequirementsTag(t.Name); ok {
			versions = append(versions, versionTag{n: n, info: t})
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].n > versions[j].n })

	out := &TagList{Tags: make([]string, 0, len(versions))}
	for _, v := range versions {
		out.Tags = append(out.Tags, v.info.Name)
	}
	if len(versions) == 0 {
		return out, nil
	}

	latest := versions[0]
	out.Latest = latest.info.Name
	// Sha-addressed (the peeled tag commit) — a local read, no second fetch.
	tagEntries, _, err := s.git.Workspace().List(ctx, ref, latest.info.CommitHash)
	if err != nil {
		return nil, fmt.Errorf("list tree at %s: %w", latest.info.Name, err)
	}
	out.SpecDirty = !specTreesEqual(headEntries, tagEntries)
	return out, nil
}

// specTreesEqual compares the specs/ subtrees of two blob listings by
// path→blob-sha — content-identical trees compare equal without any reads.
func specTreesEqual(a, b []gitrepo.Entry) bool {
	as, bs := specTreeShas(a), specTreeShas(b)
	if len(as) != len(bs) {
		return false
	}
	for path, sha := range as {
		if bs[path] != sha {
			return false
		}
	}
	return true
}

func specTreeShas(entries []gitrepo.Entry) map[string]string {
	out := map[string]string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Path, specTreePrefix) {
			out[e.Path] = e.SHA
		}
	}
	return out
}
