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
	"strings"
)

// The ID rules (skills/prd-contract, "IDs"): an ID is born once and retired
// once, never edited or reused, and everything that cites one — a design, an
// acceptance file, a role — cites a live one. This file answers both halves:
// what is wrong with the IDs in the requirements themselves (Problems), and
// what a cited ID stands for now (Cite).

// Problem codes. They join the save gate's error-code vocabulary.
const (
	CodeDuplicateID         = "DUPLICATE_ID"
	CodeReusedID            = "REUSED_ID"
	CodeStoryOutsideFeature = "STORY_OUTSIDE_FEATURE"
	CodeUnknownFeatureRef   = "UNKNOWN_FEATURE_REF"
)

// Problem is one thing wrong with the requirements' IDs. Path is the file,
// relative to specs/requirements/.
type Problem struct {
	Path    string
	Code    string
	Message string
}

// Problems is what the requirements get wrong about their own IDs, in file
// order.
func (s Spec) Problems() []Problem {
	out := slices.Clone(s.problems)
	slices.SortStableFunc(out, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// CitationStatus says what a cited ID stands for now.
type CitationStatus int

const (
	// Live: the ID names a feature, story or product-wide item that exists.
	Live CitationStatus = iota
	// Retired: the ID once existed; Replacement names what took its place,
	// when anything did.
	Retired
	// Unknown: the requirements have never had this ID.
	Unknown
)

// Citation is what a cited ID stands for now.
type Citation struct {
	Status      CitationStatus
	Replacement string
}

// Cite resolves a cited ID. A retired ID whose replacement was itself retired
// resolves to the live end of the chain.
func (s Spec) Cite(id string) Citation {
	if s.isLive(id) {
		return Citation{Status: Live}
	}
	if !s.retired[id] {
		return Citation{Status: Unknown}
	}
	next := s.moves[id]
	for hops := 0; next != "" && !s.isLive(next) && hops < 10; hops++ {
		next = s.moves[next]
	}
	return Citation{Status: Retired, Replacement: next}
}

// Describe says, for a message, why a cited ID is not live.
func (c Citation) Describe(id string) string {
	switch {
	case c.Status == Retired && c.Replacement != "":
		return fmt.Sprintf("%s is retired: it is now %s", id, c.Replacement)
	case c.Status == Retired:
		return fmt.Sprintf("%s is retired and nothing replaced it", id)
	default:
		return fmt.Sprintf("%s is not in the requirements", id)
	}
}

func (s Spec) isLive(id string) bool {
	for _, f := range s.Features {
		if f.ID == id {
			return true
		}
		for _, st := range f.Stories {
			if st.ID == id {
				return true
			}
		}
	}
	for _, it := range s.ProductWide {
		if it.ID == id {
			return true
		}
	}
	return false
}

// idProblems checks the parsed IDs against each other: one home per ID, no
// reuse of a retired ID, and every feature a need or an Applies to names is a
// live one.
func (s Spec) idProblems() []Problem {
	var out []Problem
	seen := map[string]string{}
	claim := func(id, path string) {
		if first, dup := seen[id]; dup {
			out = append(out, Problem{Path: path, Code: CodeDuplicateID,
				Message: fmt.Sprintf("%s is also in %s: an ID lives in one place — give this one the next unused ID", id, first)})
			return
		}
		seen[id] = path
		if s.retired[id] {
			out = append(out, Problem{Path: path, Code: CodeReusedID,
				Message: fmt.Sprintf("%s was retired and IDs are never reused — give this one the next unused ID", id)})
		}
	}
	for _, f := range s.Features {
		claim(f.ID, f.Path)
		for _, st := range f.Stories {
			claim(st.ID, f.Path)
		}
	}
	for _, it := range s.ProductWide {
		claim(it.ID, it.path)
	}

	features := map[string]bool{}
	for _, f := range s.Features {
		features[f.ID] = true
	}
	ref := func(path, from, id string) {
		if id == "all" || features[id] {
			return
		}
		out = append(out, Problem{Path: path, Code: CodeUnknownFeatureRef,
			Message: fmt.Sprintf("%s names %s — %s", from, id, s.Cite(id).Describe(id))})
	}
	for _, f := range s.Features {
		for _, id := range f.Needs {
			ref(f.Path, f.ID+" needs", id)
		}
		for _, st := range f.Stories {
			for _, id := range st.Needs {
				ref(f.Path, st.ID+" needs", id)
			}
		}
	}
	for _, it := range s.ProductWide {
		for _, id := range it.AppliesTo {
			ref(it.path, it.ID+" applies to", id)
		}
	}
	return out
}

// featureOf is the feature a story ID belongs to: F2 for F2.3.
func featureOf(storyID string) string {
	head, _, _ := strings.Cut(storyID, ".")
	return head
}
