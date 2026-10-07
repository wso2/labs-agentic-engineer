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

// acceptance_gate.go — the acceptance files' story tags (E3, with S2's ID
// rules). One Gherkin file per feature, `F<n>-<slug>.feature`, its rules
// tagged `@story-F<n>.<m>`. Before a tag is cut, every story ID a tag cites
// must be live — a retired one names its replacement — and every story of a
// feature that has a file must be on some rule of it, so the validation that
// drives these files covers what the feature asks for.
//
// A line scan, not a Gherkin parse: tags are whitespace-separated `@` tokens
// on their own lines, which is all this reads. Judging the scenarios is the
// validation agent's job.

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
)

// The acceptance oracle's directory, relative to the repo root.
const acceptancePrefix = "specs/validation/acceptance/"

const (
	// codeAcceptanceStaleStory — a `@story-` tag cites a story the
	// requirements do not have (retired: the message names its replacement).
	codeAcceptanceStaleStory = "ACCEPTANCE_STALE_STORY"
	// codeAcceptanceUncoveredStory — a feature's acceptance file leaves one of
	// its stories on no rule.
	codeAcceptanceUncoveredStory = "ACCEPTANCE_UNCOVERED_STORY"
)

var (
	acceptanceFeatureFileRE = regexp.MustCompile(`^(F\d+)-[^/]+\.feature$`)
	storyTagRE              = regexp.MustCompile(`(?:^|\s)@story-(\S+)`)
)

// acceptanceBundleFilter keeps the Gherkin files directly under the
// acceptance directory.
func acceptanceBundleFilter(rel string) bool {
	return !strings.Contains(rel, "/") && strings.HasSuffix(rel, ".feature")
}

// acceptanceFindings checks the acceptance files (keys relative to the
// acceptance directory) against the requirements. Paths come back
// repo-relative. No acceptance files at all is not this check's business:
// whether a project has an oracle is validation's.
//
// features is what the version carries: only their stories must be on a rule.
func acceptanceFindings(spec reqspec.Spec, files map[string]string, features []string) []FileValidationError {
	var out []FileValidationError
	tagged := map[string][]string{}
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		seen := map[string]bool{}
		for _, line := range strings.Split(files[rel], "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "@") {
				continue
			}
			for _, m := range storyTagRE.FindAllStringSubmatch(line, -1) {
				id := m[1]
				tagged[rel] = append(tagged[rel], id)
				if seen[id] {
					continue
				}
				seen[id] = true
				if !isStory(spec, id) {
					out = append(out, FileValidationError{
						Path: acceptancePrefix + rel, Code: codeAcceptanceStaleStory,
						Message: fmt.Sprintf("@story-%s — %s", id, notAStory(spec, id, "tag")),
					})
				}
			}
		}
	}
	for _, f := range spec.Features {
		rel := acceptanceFileFor(files, f.ID)
		if rel == "" || !slices.Contains(features, f.ID) {
			continue
		}
		for _, st := range f.Stories {
			if !slices.Contains(tagged[rel], st.ID) {
				out = append(out, FileValidationError{
					Path: acceptancePrefix + rel, Code: codeAcceptanceUncoveredStory,
					Message: fmt.Sprintf("%s is on no rule — add a rule tagged @story-%s, or drop the story", st.ID, st.ID),
				})
			}
		}
	}
	return out
}

// acceptanceFileFor is the feature's acceptance file, or "".
func acceptanceFileFor(files map[string]string, featureID string) string {
	for rel := range files {
		if m := acceptanceFeatureFileRE.FindStringSubmatch(rel); m != nil && m[1] == featureID {
			return rel
		}
	}
	return ""
}

func isStory(spec reqspec.Spec, id string) bool {
	return slices.ContainsFunc(spec.Stories(), func(st reqspec.Story) bool { return st.ID == id })
}
