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

package delivery

// build_scope.go — the platform-stamped "Serves stories" traceability block on
// task issues (spec-agent redesign #369): platform-authored with zero LLM
// discretion — the planner never writes it, the plan tap stamps it from the
// design's citations, and delta planning reads it back to compute coverage.

import (
	"regexp"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
)

// ---- the platform-stamped "Serves stories" block (#369) ---------------------

// servesStoriesHeader opens the platform-stamped traceability block on a task
// issue body. The stamp is platform-authored with zero LLM discretion — the
// planner never writes it, the tap appends it from the design's citations.
const servesStoriesHeader = "**Serves stories:**"

var (
	servesStoriesLinePattern = regexp.MustCompile(`(?m)^\*\*Serves stories:\*\*[ \t]*([^\n]*)$`)
	storyIDPattern           = regexp.MustCompile(`^F\d+\.\d+$`)
)

// ServesStoriesBlock renders the stamp for a task issue body; "" when the
// component cites no in-scope stories.
func ServesStoriesBlock(stories []string) string {
	if len(stories) == 0 {
		return ""
	}
	return "\n\n" + servesStoriesHeader + " " + strings.Join(stories, ", ") + "\n"
}

// StampServesStories returns body with the stamp present exactly once: an
// existing stamp is replaced (a body rewrite must not duplicate or drop it),
// otherwise the block is appended. An empty stories list strips the stamp.
func StampServesStories(body string, stories []string) string {
	stripped := servesStoriesLinePattern.ReplaceAllString(body, "")
	stripped = strings.TrimRight(stripped, "\n")
	block := ServesStoriesBlock(stories)
	if block == "" {
		return stripped + "\n"
	}
	return stripped + block
}

// ParseServesStories extracts the stamped story IDs ("F2.3") from a task issue
// body, in ID order; nil when no stamp is present. A token that is not a story
// ID is skipped.
func ParseServesStories(body string) []string {
	m := servesStoriesLinePattern.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	var out []string
	for _, tok := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\r' }) {
		if storyIDPattern.MatchString(tok) {
			out = append(out, tok)
		}
	}
	slices.SortFunc(out, reqspec.CompareIDs)
	return out
}
