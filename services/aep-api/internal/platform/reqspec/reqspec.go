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

// Package reqspec reads the requirements folder, specs/requirements/, into the
// model the platform decides with: its features, their stories, the
// product-wide requirements, what each needs or applies to, what is still
// assumed, which questions block an interview, and which IDs are retired.
//
// The shape it reads is defined in one place, skills/prd-contract, which the
// agents write by. The console reads the same files with its own TypeScript
// reader; both are held to one fixture,
// packages/contracts/requirements/acme-expenses, and the parse it must yield
// (expected.json there).
//
// Reading is lenient by design: the files are written by people and agents,
// and a line the contract did not foresee is skipped, never an error. What a
// missing or malformed piece MEANS (a build with no stories to cover) is the
// caller's to decide.
package reqspec

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The files, relative to specs/requirements/.
const (
	ProductFile     = "prd.md"
	ProductWideFile = "product-wide.md"
)

var (
	featureFileRE      = regexp.MustCompile(`^features/(F\d+)-[^/]+\.md$`)
	productWideTopicRE = regexp.MustCompile(`^product-wide/[^/]+\.md$`)
)

// IsNestedFile reports whether rel (relative to specs/requirements/) is one of
// the contract's files below the top level: a feature file or a product-wide
// topic file.
func IsNestedFile(rel string) bool {
	return featureFileRE.MatchString(rel) || productWideTopicRE.MatchString(rel)
}

// Spec is the parsed requirements folder.
type Spec struct {
	// Features in ID order, one per features/F<n>-<slug>.md.
	Features []Feature `json:"features,omitempty"`
	// ProductWide is every P item across product-wide.md and its topic files,
	// in ID order.
	ProductWide []Item `json:"productWide,omitempty"`
	// RetiredFeatures are the feature IDs prd.md's Retired section records.
	RetiredFeatures []string `json:"retiredFeatures,omitempty"`
	// RetiredProductWide are the P IDs the product-wide files' Retired
	// sections record.
	RetiredProductWide []string `json:"retiredProductWide,omitempty"`

	// moves maps a retired ID to the ID that replaced it, from a Retired
	// entry ("F2.3 moved to F5.1") or a "was" record ("F5.1 (was F2.3)").
	moves map[string]string
	// retired holds every retired ID the files record.
	retired map[string]bool
	// problems are what the parse found wrong with the files themselves.
	problems []Problem
}

// Feature is one feature file.
type Feature struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// Path is the file, relative to specs/requirements/.
	Path string `json:"path,omitempty"`
	// Needs are the features every story of this one waits on (the Purpose
	// section's `Needs:` line).
	Needs   []string `json:"needs,omitempty"`
	Stories []Story  `json:"stories,omitempty"`
	// ToConfirm counts the file's lines tagged `*assumed*`.
	ToConfirm int `json:"toConfirm,omitempty"`
	// Blocking are the Open Questions tagged `*blocking*`: the feature's
	// interview waits on them.
	Blocking []Question `json:"blocking,omitempty"`
	// Retired are the story IDs its Retired section records.
	Retired []string `json:"retired,omitempty"`
}

// Story is one line of a feature's User Stories.
type Story struct {
	ID string `json:"id,omitempty"`
	// Was is the ID this story replaced when it moved here ("F2.3").
	Was string `json:"was,omitempty"`
	// Text is the story's words alone: no ID, sources, needs or tag.
	Text string `json:"text,omitempty"`
	// Needs are the features this story alone waits on.
	Needs   []string `json:"needs,omitempty"`
	Assumed bool     `json:"assumed,omitempty"`
}

// Item is one product-wide requirement.
type Item struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text,omitempty"`
	// AppliesTo is feature IDs, or the single entry "all".
	AppliesTo []string `json:"appliesTo,omitempty"`
	Assumed   bool     `json:"assumed,omitempty"`
	// path is the product-wide file it is in, for a problem to point at.
	path string
}

// Question is a blocking question and the answers offered for it.
type Question struct {
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// Section titles the contract fixes.
const (
	sectionUserStories   = "User Stories"
	sectionPurpose       = "Purpose"
	sectionOpenQuestions = "Open Questions"
	sectionRequirements  = "Requirements"
	sectionRetired       = "Retired"
)

// Parse reads the requirements folder. files maps a path relative to
// specs/requirements/ to its content; files outside the contract are ignored.
func Parse(files map[string]string) Spec {
	spec := Spec{moves: map[string]string{}, retired: map[string]bool{}}
	for rel, content := range files {
		if m := featureFileRE.FindStringSubmatch(rel); m != nil {
			f, problems := parseFeature(m[1], rel, content, &spec)
			spec.Features = append(spec.Features, f)
			spec.problems = append(spec.problems, problems...)
		}
	}
	slices.SortFunc(spec.Features, func(a, b Feature) int { return CompareIDs(a.ID, b.ID) })

	spec.RetiredFeatures = spec.retire(readDoc(files[ProductFile]))

	for rel, content := range files {
		if rel != ProductWideFile && !productWideTopicRE.MatchString(rel) {
			continue
		}
		doc := readDoc(content)
		for _, it := range doc.section(sectionRequirements).items {
			l := parseLine(it.text)
			if !strings.HasPrefix(l.id, "P") {
				continue
			}
			spec.ProductWide = append(spec.ProductWide, Item{ID: l.id, Text: l.text, AppliesTo: l.appliesTo, Assumed: l.tag == tagAssumed, path: rel})
		}
		spec.RetiredProductWide = append(spec.RetiredProductWide, spec.retire(doc)...)
	}
	slices.SortFunc(spec.ProductWide, func(a, b Item) int { return CompareIDs(a.ID, b.ID) })
	slices.SortFunc(spec.RetiredProductWide, CompareIDs)
	spec.problems = append(spec.problems, spec.idProblems()...)
	return spec
}

func parseFeature(id, rel, content string, spec *Spec) (Feature, []Problem) {
	var problems []Problem
	doc := readDoc(content)
	f := Feature{ID: id, Name: doc.title, Path: rel}
	if f.Name == "" {
		f.Name = id
	}
	for _, p := range doc.section(sectionPurpose).paragraphs {
		if needs := parseLine(p).needs; len(needs) > 0 && needsLineRE.MatchString(p) {
			f.Needs = needs
		}
	}
	for _, it := range doc.section(sectionUserStories).items {
		l := parseLine(it.text)
		if l.id == "" {
			continue
		}
		if !strings.HasPrefix(l.id, id+".") {
			problems = append(problems, Problem{Path: rel, Code: CodeStoryOutsideFeature,
				Message: fmt.Sprintf("%s is a story of %s, not %s: a story's ID starts with its own feature's — move it with a new ID, recording the old one (\"(was %s)\")", l.id, featureOf(l.id), id, l.id)})
			continue
		}
		if l.was != "" {
			spec.moves[l.was] = l.id
			spec.retired[l.was] = true
		}
		f.Stories = append(f.Stories, Story{ID: l.id, Was: l.was, Text: l.text, Needs: l.needs, Assumed: l.tag == tagAssumed})
	}
	for _, s := range doc.sections {
		if s.title == sectionRetired {
			continue
		}
		for _, text := range s.lines() {
			if parseLine(text).tag == tagAssumed {
				f.ToConfirm++
			}
		}
	}
	for _, it := range doc.section(sectionOpenQuestions).items {
		l := parseLine(it.text)
		if l.tag != tagBlocking {
			continue
		}
		f.Blocking = append(f.Blocking, Question{Question: l.text, Options: it.children})
	}
	f.Retired = spec.retire(doc)
	return f, problems
}

var (
	retiredLeadRE = regexp.MustCompile(`^(F\d+(?:\.\d+)?|P\d+)\b`)
	retiredToRE   = regexp.MustCompile(`(?i)\b(?:moved to|merged into)\s+(F\d+(?:\.\d+)?|P\d+)\b`)
)

// retire reads a file's Retired section: each entry leads with the ID it
// retires, and names its replacement when it moved ("F2.3 moved to F5.1",
// "F4 Spending reports merged into F3", "F6 Budget alerts dropped"). It records
// both on the spec and returns the retired IDs.
func (s *Spec) retire(doc document) []string {
	var out []string
	for _, it := range doc.section(sectionRetired).items {
		m := retiredLeadRE.FindStringSubmatch(it.text)
		if m == nil {
			continue
		}
		out = append(out, m[1])
		s.retired[m[1]] = true
		if to := retiredToRE.FindStringSubmatch(it.text); to != nil {
			s.moves[m[1]] = to[1]
		}
	}
	return out
}

// Stories is every story of every feature, in ID order.
func (s Spec) Stories() []Story {
	var out []Story
	for _, f := range s.Features {
		out = append(out, f.Stories...)
	}
	return out
}

// CompareIDs orders requirement IDs numerically part by part, so F2.10 sorts
// after F2.9 and F10 after F9. IDs of different kinds (F, P) order by kind.
func CompareIDs(a, b string) int {
	if c := strings.Compare(a[:min(1, len(a))], b[:min(1, len(b))]); c != 0 {
		return c
	}
	pa, pb := idParts(a), idParts(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return len(pa) - len(pb)
}

func idParts(id string) []int {
	var out []int
	for _, p := range strings.Split(strings.TrimLeft(id, "FP"), ".") {
		n, _ := strconv.Atoi(p)
		out = append(out, n)
	}
	return out
}
