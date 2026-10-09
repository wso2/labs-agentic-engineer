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

// build_scope.go — the STORY SCOPE of one build (spec-agent redesign #369). A
// version tag snapshots the requirements + design, so the milestone is the
// version's ledger and task planning covers the requirements' stories.
// Computed here (the feature files declare the story set, read by reqspec;
// each component's design.json claims the stories it serves) and consumed by
// delivery/build (milestone identity) and delivery/task (delta planning + the
// Serves-stories stamp).

import (
	"context"
	"fmt"
	"slices"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// BuildScope is one tag's story scope. An empty InScope means the snapshot
// carries no readable stories (legacy or gate-bypassed content); consumers
// fall back to tag-scoped behavior.
type BuildScope struct {
	// Tag is the spec version this scope was computed at (e.g. "v3").
	Tag string
	// InScope is every story the version builds, by ID ("F2.3"), in ID order:
	// the stories of the features its annotation names, but the held-back
	// ones (B1). A version cut before builds were selections carried every
	// story the requirements define.
	InScope []string
	// StoryTitles maps a story ID to its words (reqspec.Story.Text).
	StoryTitles map[string]string
	// ComponentStories maps a deployable component id to the stories its
	// design.json claims (claims ∩ InScope), in ID order.
	ComponentStories map[string][]string
	// Features are the features the version carries, in ID order: the planner
	// cuts one Task per feature per component (B3).
	Features []ScopeFeature
	// ProductWide are the product-wide items the version carries, in ID order:
	// each component's Foundation Task builds them.
	ProductWide []ScopeItem
}

// ScopeFeature is one feature a version carries.
type ScopeFeature struct {
	ID   string
	Name string
	// Needs are the carried features this one is built after, in ID order;
	// one built by an earlier version is already in the code and is left out.
	// See featureOrder for how a story's own need joins them.
	Needs []string
}

// ScopeItem is one product-wide item a version carries.
type ScopeItem struct {
	ID   string
	Text string
	// AppliesTo is feature IDs, or the single entry "all".
	AppliesTo []string
}

// MilestoneTitle is the title of the milestone this scope claims — the tag,
// so there is one milestone per spec version.
func (s BuildScope) MilestoneTitle() string { return s.Tag }

// BuildScopeAtTag computes the tag's story scope from the tagged snapshot. A
// snapshot without a cell or readable stories yields an empty scope (the
// legacy one-milestone-per-version behavior); the build gate normally makes
// that impossible for freshly-cut tags.
func (s *artifactService) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (BuildScope, error) {
	scope := BuildScope{Tag: tag}
	// The same rule the save applies, for the same reason GetDesignAtTag applies
	// it: this tag becomes `tags/<name>` a few lines down, and a name that could
	// never have been created must not be able to walk out of that namespace.
	if verr := ValidateVersionName(tag); verr != nil {
		return scope, fmt.Errorf("%w: %q: %w", ErrInvalidVersionTag, tag, verr)
	}
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return scope, err
	}
	reqFiles, err := s.readBundleAtTag(ctx, ref, tag, requirementsBundle)
	if err != nil {
		return scope, fmt.Errorf("read requirements at %s: %w", tag, err)
	}
	designFiles, err := s.readBundleAtTag(ctx, ref, tag, designBundle)
	if err != nil {
		return scope, fmt.Errorf("read design at %s: %w", tag, err)
	}
	facts, err := parseCellFacts(designFiles[DesignRootFile])
	if err != nil {
		return scope, nil
	}
	spec := reqspec.Parse(reqFiles)
	stories := spec.Stories()
	if len(stories) == 0 {
		return scope, nil
	}
	plan, ok := s.versionScope(ctx, ref, tag)
	if !ok {
		plan = everything(spec)
	}
	carried := storySet(spec, plan)
	scope.Features, scope.ProductWide = scopeOf(spec, plan)
	scope.StoryTitles = map[string]string{}
	for _, st := range stories {
		if !carried[st.ID] {
			continue
		}
		scope.InScope = append(scope.InScope, st.ID)
		scope.StoryTitles[st.ID] = st.Text
	}
	scope.ComponentStories = map[string][]string{}
	for id, claims := range componentStoryClaims(facts, designFiles) {
		var served []string
		for _, sid := range claims {
			if _, ok := scope.StoryTitles[sid]; ok {
				served = append(served, sid)
			}
		}
		if len(served) > 0 {
			slices.SortFunc(served, reqspec.CompareIDs)
			scope.ComponentStories[id] = slices.Compact(served)
		}
	}
	return scope, nil
}

// everything is the plan of a version cut before builds were selections: it
// carried every feature with stories and every product-wide item.
func everything(spec reqspec.Spec) reqspec.BuildPlan {
	var plan reqspec.BuildPlan
	for _, f := range spec.Features {
		if len(f.Stories) > 0 {
			plan.Features = append(plan.Features, f.ID)
		}
	}
	for _, it := range spec.ProductWide {
		plan.ProductWide = append(plan.ProductWide, it.ID)
	}
	return plan
}

// scopeOf names what a plan carries for the planner: each carried feature
// with the carried features it is built after, and each carried product-wide
// item.
func scopeOf(spec reqspec.Spec, plan reqspec.BuildPlan) ([]ScopeFeature, []ScopeItem) {
	order := featureOrder(spec, plan.Features)
	var features []ScopeFeature
	for _, f := range spec.Features {
		if slices.Contains(plan.Features, f.ID) {
			features = append(features, ScopeFeature{ID: f.ID, Name: f.Name, Needs: order[f.ID]})
		}
	}
	var items []ScopeItem
	for _, it := range spec.ProductWide {
		if slices.Contains(plan.ProductWide, it.ID) {
			items = append(items, ScopeItem{ID: it.ID, Text: it.Text, AppliesTo: it.AppliesTo})
		}
	}
	return features, items
}

// featureOrder is the build order among the carried features: what each is
// built after. A feature file's `Needs:` comes first, as the whole feature
// waits on it; a story's own need joins only when it closes no loop — Claims'
// "see the decision" story needs Approvals while Approvals needs Claims, and
// Claims is still built first, its one story landing on top of a stub.
func featureOrder(spec reqspec.Spec, carried []string) map[string][]string {
	order := map[string][]string{}
	reaches := func(from, to string) bool {
		seen := map[string]bool{}
		stack := []string{from}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if n == to {
				return true
			}
			if !seen[n] {
				seen[n] = true
				stack = append(stack, order[n]...)
			}
		}
		return false
	}
	add := func(f, need string) {
		if need != f && slices.Contains(carried, f) && slices.Contains(carried, need) && !slices.Contains(order[f], need) && !reaches(need, f) {
			order[f] = append(order[f], need)
		}
	}
	for _, f := range spec.Features {
		for _, n := range f.Needs {
			add(f.ID, n)
		}
	}
	for _, f := range spec.Features {
		for _, n := range f.Waits() {
			add(f.ID, n)
		}
	}
	for id := range order {
		slices.SortFunc(order[id], reqspec.CompareIDs)
	}
	return order
}

// versionScope is a version's plan, read from its tag's annotation, or false
// when the tag carries none (a version cut before builds were selections) or
// cannot be listed.
func (s *artifactService) versionScope(ctx context.Context, ref sourcecontrol.RepoRef, tag string) (reqspec.BuildPlan, bool) {
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return reqspec.BuildPlan{}, false
	}
	for _, t := range tags {
		if t.Name == tag {
			return parseScope(t.Body)
		}
	}
	return reqspec.BuildPlan{}, false
}
