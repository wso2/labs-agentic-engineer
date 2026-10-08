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

// build_selection.go — a version is a selection of features (B1). The save
// that cuts a version plans what it carries from what the user picked
// (reqspec.PlanBuild) and records that plan in the tag's annotation, under the
// subject line:
//
//	Spec v3
//
//	Features: F1 F2
//	Product-wide: P1 P4
//	Held back: F2.4
//
// The annotation is the record of what each version built: what the next
// build may skip, and what a version's validation covers. Nothing else stores
// it, and a version tag says it whatever happens to the database.

import (
	"errors"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// codeFeatureNotBuildable — a picked feature, or one it needs, cannot be built
// right now; the message says why.
const codeFeatureNotBuildable = "FEATURE_NOT_BUILDABLE"

// codeNothingToBuild — the pick carries no feature at all.
const codeNothingToBuild = "NOTHING_TO_BUILD"

const (
	scopeFeatures    = "Features:"
	scopeProductWide = "Product-wide:"
	scopeHeldBack    = "Held back:"
	// scopeFixes names the version a repair build fixes (B4).
	scopeFixes = "Fixes:"
)

// fixesOf is the version a repair version's annotation says it fixes, or "".
func fixesOf(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), scopeFixes); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// scopeBody renders a plan as the annotation lines that follow the subject.
func scopeBody(plan reqspec.BuildPlan) string {
	return strings.Join([]string{
		scopeFeatures + " " + strings.Join(plan.Features, " "),
		scopeProductWide + " " + strings.Join(plan.ProductWide, " "),
		scopeHeldBack + " " + strings.Join(plan.HeldBack, " "),
	}, "\n")
}

// parseScope reads a version's plan back from its annotation body, or false
// for a version cut before builds were selections (it carried every story).
func parseScope(body string) (reqspec.BuildPlan, bool) {
	var plan reqspec.BuildPlan
	found := false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		for prefix, dst := range map[string]*[]string{
			scopeFeatures: &plan.Features, scopeProductWide: &plan.ProductWide, scopeHeldBack: &plan.HeldBack,
		} {
			if rest, ok := strings.CutPrefix(line, prefix); ok {
				*dst = strings.Fields(rest)
				found = true
			}
		}
	}
	return plan, found
}

// builtSoFar is what the versions before this build carried: the union of
// their scopes. A version without a scope carried nothing a selection can
// skip.
func builtSoFar(tags []sourcecontrol.TagInfo) reqspec.Built {
	built := reqspec.Built{Features: map[string]bool{}, ProductWide: map[string]bool{}}
	for _, t := range tags {
		if !isVersionTag(t) {
			continue
		}
		plan, ok := parseScope(t.Body)
		if !ok {
			continue
		}
		for _, id := range plan.Features {
			built.Features[id] = true
		}
		for _, id := range plan.ProductWide {
			built.ProductWide[id] = true
		}
	}
	return built
}

// planVersion plans what the version carries, or refuses with the reasons.
// No pick means every feature that can be designed — the whole product, as a
// build was before it was a selection.
func planVersion(spec reqspec.Spec, built reqspec.Built, pick *reqspec.Pick, unavailable map[string]string) (reqspec.BuildPlan, error) {
	p := reqspec.Pick{}
	if pick != nil {
		p = *pick
	} else {
		for _, f := range spec.Features {
			if f.Designable() {
				p.Features = append(p.Features, f.ID)
			}
		}
	}
	plan, refusals := reqspec.PlanBuild(spec, built, p, unavailable)
	if len(refusals) > 0 {
		paths := map[string]string{}
		for _, f := range spec.Features {
			paths[f.ID] = RequirementsDir + "/" + f.Path
		}
		var rows []FileValidationError
		for _, r := range refusals {
			path := paths[r.ID]
			if path == "" {
				path = RequirementsDir + "/" + reqspec.ProductFile
			}
			rows = append(rows, FileValidationError{Path: path, Code: codeFeatureNotBuildable, Message: r.Reason})
		}
		return plan, &SpecValidationError{Files: rows}
	}
	if len(plan.Features) == 0 {
		return plan, &SpecValidationError{Files: []FileValidationError{{
			Path: RequirementsDir + "/" + reqspec.ProductFile, Code: codeNothingToBuild,
			Message: "nothing to build — interview and design a feature first",
		}}}
	}
	return plan, nil
}

// samePlan reports whether two plans carry the same scope.
func samePlan(a, b reqspec.BuildPlan) bool {
	return slices.Equal(a.Features, b.Features) && slices.Equal(a.ProductWide, b.ProductWide) &&
		slices.Equal(a.HeldBack, b.HeldBack)
}

// storySet is the plan's stories as a set, for the gate's in-scope checks.
func storySet(spec reqspec.Spec, plan reqspec.BuildPlan) map[string]bool {
	out := map[string]bool{}
	for _, id := range plan.Stories(spec) {
		out[id] = true
	}
	return out
}

// joinValidation folds the plan's refusals and the gate's failures into one
// refusal, nil when there is neither.
func joinValidation(errs ...error) error {
	var rows []FileValidationError
	for _, err := range errs {
		var ve *SpecValidationError
		switch {
		case err == nil:
		case errors.As(err, &ve):
			rows = append(rows, ve.Files...)
		default:
			return err
		}
	}
	if len(rows) == 0 {
		return nil
	}
	return &SpecValidationError{Files: rows}
}
