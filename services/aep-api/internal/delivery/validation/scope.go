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

package validation

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// scope.go — what one version validates (B4).
//
// A version validates its built scope: every feature built in it or an earlier
// version, minus the stories no version has built yet (held back because they
// wait on a feature nobody built). Design runs ahead of the build, so the
// acceptance files at a version's tag can hold a feature nobody asked to build
// yet; running it would file repair issues for code that does not exist.

// Scope is what one version validates.
type Scope struct {
	// Version is the version judged ("v2").
	Version string
	// Features are the features built in it or an earlier version, by ID.
	Features []string
	// HeldBack are stories of those features no version has built yet.
	HeldBack []string
	// rules maps a rule ("F2 / Claims over $1,000 need Finance") to the
	// stories its `@story-` tags name, from the acceptance files.
	rules map[string][]string
}

var (
	featureIDRE = regexp.MustCompile(`^(F\d+)\b`)
	storyTagRE  = regexp.MustCompile(`@story-(\S+)`)
)

// NewScope reads the rules' story tags from the version's acceptance files.
func NewScope(version string, features, heldBack []string, criteria []AcceptanceCriteriaFile) *Scope {
	s := &Scope{Version: version, Features: features, HeldBack: heldBack, rules: map[string][]string{}}
	for _, f := range criteria {
		feature := ""
		var tags, featureTags []string
		for _, line := range strings.Split(f.Content, "\n") {
			t := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(t, "@"):
				for _, m := range storyTagRE.FindAllStringSubmatch(t, -1) {
					tags = append(tags, m[1])
				}
			case strings.HasPrefix(t, "Feature:"):
				feature = featureID(strings.TrimSpace(strings.TrimPrefix(t, "Feature:")), f.Path)
				featureTags, tags = tags, nil
			case strings.HasPrefix(t, "Rule:"):
				rule := strings.TrimSpace(strings.TrimPrefix(t, "Rule:"))
				s.rules[feature+" / "+rule] = append(slices.Clone(featureTags), tags...)
				tags = nil
			case t != "" && !strings.HasPrefix(t, "#"):
				tags = nil
			}
		}
	}
	return s
}

// featureID is a feature's ID from its Gherkin `Feature:` line ("F2
// Approvals"), else from its file name ("F2-approvals.feature"); the line's
// text when neither carries one.
func featureID(feature, file string) string {
	if m := featureIDRE.FindStringSubmatch(strings.TrimSpace(feature)); m != nil {
		return m[1]
	}
	if m := featureIDRE.FindStringSubmatch(path.Base(file)); m != nil {
		return m[1]
	}
	return strings.TrimSpace(feature)
}

// Stories are the stories a scenario's rule stands for, from its tags.
func (s *Scope) stories(sc reportScenario) []string {
	if s == nil {
		return nil
	}
	return s.rules[featureID(sc.Feature, sc.FeatureFile)+" / "+strings.TrimSpace(sc.Rule)]
}

// runs reports whether the version validates a scenario: its feature is
// built, and its rule stands for at least one story that is not held back (a
// rule with no story tag stands for its whole feature).
func (s *Scope) runs(sc reportScenario) bool {
	if s == nil {
		return true
	}
	if !slices.Contains(s.Features, featureID(sc.Feature, sc.FeatureFile)) {
		return false
	}
	stories := s.stories(sc)
	return len(stories) == 0 || slices.ContainsFunc(stories, func(id string) bool { return !slices.Contains(s.HeldBack, id) })
}

// files are the acceptance files of the features the version built, by their
// `Feature:` line.
func (s *Scope) files(criteria []AcceptanceCriteriaFile) []AcceptanceCriteriaFile {
	if s == nil {
		return criteria
	}
	var out []AcceptanceCriteriaFile
	for _, f := range criteria {
		feature := ""
		for _, line := range strings.Split(f.Content, "\n") {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "Feature:") {
				feature = featureID(strings.TrimSpace(strings.TrimPrefix(t, "Feature:")), f.Path)
				break
			}
		}
		if slices.Contains(s.Features, feature) {
			out = append(out, f)
		}
	}
	return out
}
