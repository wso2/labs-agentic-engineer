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

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const gatePRD = `# Lunch — PRD

## User Stories

1. As a member, I want to browse today's order, so that I can join.
2. As a member, I want to add my item, so that it is counted.
4. As a coordinator, I want the round locked at cutoff, so that the order is final.
7. As a member, I want a Slack message on close, so that I don't miss it.
`

const gateCell = `component lunch-api service
component lunch-web web-application
component slack-notifier service
component orders-db database
`

func enriched(id, typ string, stories string) string {
	return `{"name":"` + id + `","type":"` + typ + `","version":"0.1.0","language":"Ballerina","buildpack":"docker","appPath":"` + id + `","entrypoint":"deployment/` + typ + `","exposure":"intranet","stories":[` + stories + `],"dependencies":[],"description":"real responsibility text"}`
}

func completeDesignFiles() map[string]string {
	return map[string]string{
		"design.cell":                            gateCell,
		"components/lunch-api/design.json":       enriched("lunch-api", "service", "1, 2, 4"),
		"components/lunch-api/openapi.yaml":      "openapi: 3.0.3\n",
		"components/lunch-web/design.json":       enriched("lunch-web", "web-application", "1, 2"),
		"components/lunch-web/wireframes.dsl":    "screen home\n",
		"components/slack-notifier/design.json":  enriched("slack-notifier", "service", "7"),
		"components/slack-notifier/openapi.yaml": "openapi: 3.0.3\n",
	}
}

func gateErrors(t *testing.T, designFiles map[string]string) []FileValidationError {
	t.Helper()
	return validateBuildGate(map[string]string{requirementsMainFile: gatePRD}, designFiles)
}

func codesOf(errs []FileValidationError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, e.Code)
	}
	return out
}

func TestBuildGate_CompleteDesignPasses(t *testing.T) {
	errs := gateErrors(t, completeDesignFiles())
	if len(errs) != 0 {
		t.Fatalf("complete design should pass, got %+v", errs)
	}
}

func TestBuildGate_MissingCell(t *testing.T) {
	errs := validateBuildGate(map[string]string{requirementsMainFile: gatePRD}, map[string]string{})
	if len(errs) != 1 || errs[0].Code != "MISSING_DESIGN_CELL" {
		t.Fatalf("want MISSING_DESIGN_CELL, got %+v", errs)
	}
}

// Every PRD story must be claimed by some component's design.json `stories` —
// the anti-disappearance net between PRD and design.
func TestBuildGate_UncoveredStory(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = enriched("lunch-api", "service", "1, 4")
	files["components/lunch-web/design.json"] = enriched("lunch-web", "web-application", "1")
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNCOVERED_STORY" && strings.Contains(e.Message, "story 2") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNCOVERED_STORY for story 2, got %+v", errs)
	}
}

func TestBuildGate_DeployableComponentDemandsArtifactsAndEnrichment(t *testing.T) {
	files := completeDesignFiles()
	delete(files, "components/lunch-api/openapi.yaml")
	files["components/lunch-web/design.json"] = renderScaffold("lunch-web", "web-application")
	errs := gateErrors(t, files)
	codes := strings.Join(codesOf(errs), ",")
	if !strings.Contains(codes, "MISSING_COMPONENT_ARTIFACT") {
		t.Errorf("want MISSING_COMPONENT_ARTIFACT for lunch-api openapi.yaml, got %+v", errs)
	}
	if !strings.Contains(codes, "UNENRICHED_COMPONENT") {
		t.Errorf("want UNENRICHED_COMPONENT for scaffold-placeholder lunch-web, got %+v", errs)
	}
}

// Infrastructure nodes (database, cache, …) are not deployable: no design.json
// directory, no artifact, and the gate must never ask for one.
func TestBuildGate_InfrastructureExempt(t *testing.T) {
	files := completeDesignFiles()
	errs := gateErrors(t, files)
	for _, e := range errs {
		if strings.Contains(e.Path, "orders-db") {
			t.Errorf("infrastructure leaked into the gate: %+v", e)
		}
	}
}

// TestBuildGate_LanguageSentinelRefused pins that the platform never decides a
// component's language: a design.json enriched everywhere EXCEPT the
// scaffold's "TBD" language sentinel still refuses the tag — the agent must
// set it (org Tech stack default → requirements → platform default).
func TestBuildGate_LanguageSentinelRefused(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = strings.Replace(
		enriched("lunch-api", "service", "1, 2, 4"), `"language":"Ballerina"`, `"language":"TBD"`, 1)
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNENRICHED_COMPONENT" && strings.Contains(e.Message, "language") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNENRICHED_COMPONENT for the TBD language sentinel, got %+v", errs)
	}
}

func TestParsePRDStories(t *testing.T) {
	stories := parsePRDStories(gatePRD)
	if got := slices.Sorted(maps.Keys(stories)); !reflect.DeepEqual(got, []int{1, 2, 4, 7}) {
		t.Fatalf("story numbers = %v, want [1 2 4 7]", got)
	}
	if !strings.Contains(stories[4], "locked at cutoff") {
		t.Errorf("story 4 title = %q", stories[4])
	}
	if len(parsePRDStories("# PRD\n\nno stories section")) != 0 {
		t.Error("PRD without a User Stories section should yield no stories")
	}
	// Markdown authors indent list items; the gate and the console preview
	// must read them the same way.
	indented := "## User Stories\n\n  1. As a user, I want A, so that a.\n  2. As a user, I want B, so that b.\n"
	if got := slices.Sorted(maps.Keys(parsePRDStories(indented))); !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("indented story numbers = %v, want [1 2]", got)
	}
	// A numbered item with a whitespace-only title is not a story — the same
	// rule the console preview applies, or the drawer would preview no story
	// while the gate demands coverage for it.
	blank := "## User Stories\n\n1. As a user, I want A, so that a.\n2.   \n"
	if got := slices.Sorted(maps.Keys(parsePRDStories(blank))); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("whitespace-only item counted as a story: %v, want [1]", got)
	}
}

// TestBuildGate_FormattedSentinelRefused pins the STRUCTURED enrichment read:
// design.json is stored byte-verbatim as the agent wrote it, so a formatting
// variant a substring check would miss ("language" : "TBD") must still refuse
// the tag.
func TestBuildGate_FormattedSentinelRefused(t *testing.T) {
	files := completeDesignFiles()
	files["components/lunch-api/design.json"] = "{\n  \"name\": \"lunch-api\",\n  \"type\": \"service\",\n  \"version\": \"0.1.0\",\n  \"language\" : \"TBD\",\n  \"buildpack\": \"docker\",\n  \"appPath\": \"lunch-api\",\n  \"entrypoint\": \"deployment/service\",\n  \"exposure\": \"intranet\",\n  \"stories\": [1, 2, 4],\n  \"dependencies\": [],\n  \"description\": \"real responsibility text\"\n}"
	errs := gateErrors(t, files)
	found := false
	for _, e := range errs {
		if e.Code == "UNENRICHED_COMPONENT" && strings.Contains(e.Message, "language") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want UNENRICHED_COMPONENT for the formatted TBD sentinel, got %+v", errs)
	}
}

// A PRD whose User Stories section yields no numbered stories must refuse the
// tag rather than silently disarm the coverage check.
func TestBuildGate_UnparseableStoriesRefused(t *testing.T) {
	files := completeDesignFiles()
	errs := validateBuildGate(map[string]string{
		requirementsMainFile: "# PRD\n\n## User Stories\n\n- As a user, I want bullets, so that no numbers parse.\n",
	}, files)
	found := false
	for _, e := range errs {
		if e.Code == "MISSING_USER_STORIES" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want MISSING_USER_STORIES, got %+v", errs)
	}
}

// A design.json with malformed JSON or no stories field claims nothing — the
// write-gates own rejecting bad JSON; the gate only collects claims.
func TestDesignJSONStories(t *testing.T) {
	if got := designJSONStories(`{"stories": [3, 1]}`); !reflect.DeepEqual(got, []int{3, 1}) {
		t.Errorf("stories = %v, want [3 1]", got)
	}
	for _, content := range []string{"", "not json", `{"name":"x"}`} {
		if got := designJSONStories(content); len(got) != 0 {
			t.Errorf("designJSONStories(%q) = %v, want none", content, got)
		}
	}
}
