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

package reqspec

import (
	"fmt"
	"slices"
)

// A build is a selection of features (B1). The user picks features — and,
// for a product-wide requirement added since the last build, that item —
// and the plan is what the build must then carry so the picked features work:
// every unbuilt feature they need, every unbuilt product-wide item that
// reaches them, and nothing more. A story that needs a feature this build
// does not bring and no earlier build brought is held back until one does.
// The order is chosen at each build and never stored (skills/prd-contract,
// map ticket "How are feature dependencies and build order set?").

// Built is what earlier versions built.
type Built struct {
	Features    map[string]bool
	ProductWide map[string]bool
}

// Pick is what the user picked for this build.
type Pick struct {
	Features    []string
	ProductWide []string
}

// BuildPlan is what one build carries.
type BuildPlan struct {
	// Features is every feature the build carries, picked and pulled in, in
	// ID order.
	Features []string
	// PulledIn is the part of Features nobody picked: needed by one that was.
	PulledIn []string
	// ProductWide is every product-wide item the build carries, in ID order.
	ProductWide []string
	// HeldBack are the stories of carried features that wait on a feature
	// this build does not bring, in ID order.
	HeldBack []string
}

// Refusal is a feature the build cannot carry, and why, in words.
type Refusal struct {
	ID     string
	Reason string
}

// Stories is every story the plan builds: each carried feature's stories but
// the held-back ones, in ID order.
func (p BuildPlan) Stories(spec Spec) []string {
	var out []string
	for _, f := range spec.Features {
		if !slices.Contains(p.Features, f.ID) {
			continue
		}
		for _, st := range f.Stories {
			if !slices.Contains(p.HeldBack, st.ID) {
				out = append(out, st.ID)
			}
		}
	}
	return out
}

// PlanBuild works out what a pick carries. unavailable names features that
// cannot be built right now, with the reason (a design out of date, a
// dependency still open): they are refused when picked or needed, as is a
// feature that has not been interviewed or waits on a blocking question.
func PlanBuild(spec Spec, built Built, pick Pick, unavailable map[string]string) (BuildPlan, []Refusal) {
	byID := map[string]Feature{}
	for _, f := range spec.Features {
		byID[f.ID] = f
	}
	items := map[string]Item{}
	for _, it := range spec.ProductWide {
		items[it.ID] = it
	}

	var plan BuildPlan
	var refusals []Refusal
	refused := map[string]bool{}
	refuse := func(id, reason string) {
		if !refused[id] {
			refused[id] = true
			refusals = append(refusals, Refusal{ID: id, Reason: reason})
		}
	}
	included := map[string]bool{}
	picked := map[string]bool{}

	type want struct{ id, by string }
	var queue []want
	for _, id := range pick.Features {
		picked[id] = true
		queue = append(queue, want{id: id})
	}
	// A product-wide item picked on its own pulls in every feature it reaches
	// that has been built or can be: a rule built into only part of the
	// product breaks it.
	for _, id := range pick.ProductWide {
		it, ok := items[id]
		if !ok {
			refuse(id, fmt.Sprintf("%s is not a product-wide requirement", id))
			continue
		}
		for _, f := range spec.Features {
			if it.Reaches(f.ID) && (built.Features[f.ID] || f.Designable()) {
				queue = append(queue, want{id: f.ID, by: id})
			}
		}
	}
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if included[w.id] || refused[w.id] {
			continue
		}
		f, ok := byID[w.id]
		needed := ""
		if w.by != "" {
			needed = fmt.Sprintf(" (%s needs it)", w.by)
		}
		switch {
		case !ok:
			refuse(w.id, fmt.Sprintf("%s is not a feature of the requirements%s", w.id, needed))
			continue
		case len(f.Stories) == 0:
			refuse(w.id, fmt.Sprintf("%s %s has not been interviewed yet%s", f.ID, f.Name, needed))
			continue
		case len(f.Blocking) > 0:
			refuse(w.id, fmt.Sprintf("%s %s waits on a blocking question%s", f.ID, f.Name, needed))
			continue
		case unavailable[w.id] != "":
			refuse(w.id, fmt.Sprintf("%s %s: %s%s", f.ID, f.Name, unavailable[w.id], needed))
			continue
		}
		included[w.id] = true
		for _, n := range f.Needs {
			if !built.Features[n] {
				queue = append(queue, want{id: n, by: f.ID})
			}
		}
	}

	for _, f := range spec.Features {
		if !included[f.ID] {
			continue
		}
		plan.Features = append(plan.Features, f.ID)
		if !picked[f.ID] {
			plan.PulledIn = append(plan.PulledIn, f.ID)
		}
		for _, st := range f.Stories {
			for _, n := range st.Needs {
				if !built.Features[n] && !included[n] {
					plan.HeldBack = append(plan.HeldBack, st.ID)
					break
				}
			}
		}
	}
	for _, it := range spec.ProductWide {
		if slices.Contains(pick.ProductWide, it.ID) {
			plan.ProductWide = append(plan.ProductWide, it.ID)
			continue
		}
		if built.ProductWide[it.ID] {
			continue
		}
		if slices.ContainsFunc(plan.Features, func(id string) bool { return it.Reaches(id) }) {
			plan.ProductWide = append(plan.ProductWide, it.ID)
		}
	}
	return plan, refusals
}

// Reaches reports whether the product-wide item applies to a feature.
func (it Item) Reaches(featureID string) bool {
	return slices.Contains(it.AppliesTo, "all") || slices.Contains(it.AppliesTo, featureID)
}

// Waits is every feature this one waits on: the file's `Needs:` and each
// story's, once each, in ID order.
func (f Feature) Waits() []string {
	out := slices.Clone(f.Needs)
	for _, st := range f.Stories {
		out = append(out, st.Needs...)
	}
	slices.SortFunc(out, CompareIDs)
	return slices.Compact(out)
}
