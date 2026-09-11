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

package githubhost

import (
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The brand the RUNNER stamps, spelled out here rather than read from the
// constant — reading it back would make this tautological.
//
// It is one literal in two languages: this package classifies comments by it,
// and runners/remote-worker/src/lib/validation_status_line.ts writes it onto
// every line a validation run posts. Nothing links them at build time, and the
// failure is silent in the worst way — change one spelling and every observed
// line reclassifies as the agent's own words, with both test suites green. The
// TS side pins the same literal against its own constant.
func TestObservedCommentMarkerMatchesTheRunnersOwn(t *testing.T) {
	const marker = "<!-- aep:observed -->"
	if sourcecontrol.ObservedCommentMarker != marker {
		t.Fatalf("the runner stamps %q but this host classifies %q", marker, sourcecontrol.ObservedCommentMarker)
	}
}

// Three classes, one leading marker each, and the body handed on without it.
//
// Both read paths share this, so a class the host gets wrong is wrong on every
// surface at once: a machine comment reported as observed would put the
// platform's notes to the AGENT on a page meant for people, and an observed one
// reported as machine would be dropped — which is the whole run's narrative on a
// validation issue.
func TestClassifyComment_TellsThePlatformsTwoBrandsApartFromEverybodyElse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		machine  bool
		observed bool
		want     string
	}{
		{
			name:    "written for the agent",
			body:    sourcecontrol.MachineCommentMarker + "\nValidation run dispatched.",
			machine: true,
			want:    "Validation run dispatched.",
		},
		{
			name:     "written for a person, from what the run did",
			body:     sourcecontrol.ObservedCommentMarker + "\nRunning specs against the deployed system.",
			observed: true,
			want:     "Running specs against the deployed system.",
		},
		{
			name: "the agent's own words carry no brand",
			body: "Starting validation: 12 criteria, 9 to author.",
			want: "Starting validation: 12 criteria, 9 to author.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, machine, observed := classifyComment(tc.body)
			if machine != tc.machine || observed != tc.observed {
				t.Fatalf("machine=%v observed=%v, want machine=%v observed=%v", machine, observed, tc.machine, tc.observed)
			}
			if body != tc.want {
				t.Fatalf("body = %q, want %q", body, tc.want)
			}
		})
	}
}

// A brand is never BOTH. Only the leading marker is tested, so a body carrying
// two can be one class or the other but never a contradiction the caller has to
// resolve — which is what lets these stay two independent bools.
func TestClassifyComment_TheLeadingBrandWins(t *testing.T) {
	body := sourcecontrol.ObservedCommentMarker + "\n" + sourcecontrol.MachineCommentMarker + "\nboth"
	_, machine, observed := classifyComment(body)
	if machine || !observed {
		t.Fatalf("machine=%v observed=%v, want the LEADING brand (observed) alone", machine, observed)
	}
}

// The strict prefix test, on the case that actually motivates it. The agent is
// told to read its issue with `gh issue view --comments`, where a raw marker is
// visible, so a note QUOTING one must stay the agent's own. A `Contains` test
// would classify it as the platform's and drop it from every surface built for
// people — data loss nobody can see, against noise somebody can report.
func TestClassifyComment_QuotingABrandIsStillSomebodysOwnWords(t *testing.T) {
	for _, body := range []string{
		"The platform posted " + sourcecontrol.MachineCommentMarker + " above; ignoring it.",
		"Saw " + sourcecontrol.ObservedCommentMarker + " on the issue — that is not mine.",
	} {
		got, machine, observed := classifyComment(body)
		if machine || observed {
			t.Fatalf("a quoted brand was classified as the platform's: %q", body)
		}
		if got != body {
			t.Fatalf("a quoted brand was stripped out of the prose: %q", got)
		}
	}
}

// Whitespace either side of the brand goes with it. The writers put a marker on
// its own line, so leaving the newline behind would start every branded body
// blank — and the status line takes a body's FIRST non-empty line, which would
// then be prose the reader never sees the top of.
func TestClassifyComment_StripsTheBrandsOwnWhitespace(t *testing.T) {
	body, _, observed := classifyComment("\n  " + sourcecontrol.ObservedCommentMarker + "\n\nExploring the deployed app.")
	if !observed {
		t.Fatal("a brand behind leading whitespace was not recognised")
	}
	if strings.HasPrefix(body, "\n") || body != "Exploring the deployed app." {
		t.Fatalf("body = %q, want the prose alone", body)
	}
}
