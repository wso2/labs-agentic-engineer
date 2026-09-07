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

// build_scope.go — the STORY SCOPE of one build (spec-agent redesign #369): a
// v<N> tag snapshots the PRD + design, so the milestone is the version's
// ledger and task planning covers the PRD's stories. Computed here (the PRD's
// User Stories section declares the story set; each component's design.json
// claims the stories it serves) and consumed by delivery/build (milestone
// identity) and delivery/task (delta planning + the Serves-stories stamp).

import (
	"context"
	"fmt"
	"maps"
	"slices"
)

// BuildScope is one tag's story scope. An empty InScope means the snapshot
// carries no readable stories (legacy or gate-bypassed content); consumers
// fall back to tag-scoped behavior.
type BuildScope struct {
	// Tag is the spec version this scope was computed at (e.g. "v3").
	Tag string
	// InScope is the PRD's story set, ascending.
	InScope []int
	// StoryTitles maps a story number to its PRD story line (the text after
	// "N. ").
	StoryTitles map[int]string
	// ComponentStories maps a deployable component id to the stories its
	// design.json claims (claims ∩ InScope), ascending.
	ComponentStories map[string][]int
}

// MilestoneTitle is the title of the milestone this scope claims — the tag,
// so there is one milestone per spec version.
func (s BuildScope) MilestoneTitle() string { return s.Tag }

// BuildScopeAtTag computes the tag's story scope from the tagged snapshot. A
// snapshot without a cell or readable PRD stories yields an empty scope (the
// legacy one-milestone-per-version behavior); the build gate normally makes
// that impossible for freshly-cut tags.
func (s *artifactService) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (BuildScope, error) {
	scope := BuildScope{Tag: tag}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return scope, err
	}
	reqFiles, err := s.readBundleAtTag(ctx, ref, tag, requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return scope, fmt.Errorf("read requirements at %s: %w", tag, err)
	}
	designFiles, err := s.readBundleAtTag(ctx, ref, tag, designPrefix, designBundleFilter)
	if err != nil {
		return scope, fmt.Errorf("read design at %s: %w", tag, err)
	}
	facts, err := parseCellFacts(designFiles[DesignRootFile])
	if err != nil {
		return scope, nil
	}
	stories := parsePRDStories(reqFiles[requirementsMainFile])
	if len(stories) == 0 {
		return scope, nil
	}
	scope.InScope = slices.Sorted(maps.Keys(stories))
	scope.StoryTitles = stories
	scope.ComponentStories = map[string][]int{}
	for id, claims := range componentStoryClaims(facts, designFiles) {
		var served []int
		for _, n := range claims {
			if _, ok := stories[n]; ok {
				served = append(served, n)
			}
		}
		if len(served) > 0 {
			slices.Sort(served)
			scope.ComponentStories[id] = slices.Compact(served)
		}
	}
	return scope, nil
}
