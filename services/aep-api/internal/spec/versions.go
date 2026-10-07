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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/reqspec"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// GET /projects/{p}/versions (B5): what each version built. A build keeps no
// copy of what it built; the tag does — its annotation names the features
// and product-wide items it carried (build_selection.go), and its tree holds
// each feature's file as it was. The console compares those lines with the
// live ones to say what changed since a feature was last built.

// Version is what one version built.
type Version struct {
	// Name is the tag the build cut.
	Name string
	// Features are the features it carried, picked and pulled in, in ID order.
	Features []VersionFeature
	// ProductWide are the product-wide items it carried, in ID order.
	ProductWide []string
	// HeldBack are carried features' stories it did not build.
	HeldBack []string
	// Fixes is the version a repair build fixes ("v1" for v1.1); "" otherwise.
	Fixes string
}

// VersionFeature is a feature as a version built it.
type VersionFeature struct {
	ID   string
	Name string
	// Lines are its file's lines at the version's tag (reqspec.FeatureLines).
	Lines []reqspec.Line
}

// ListVersions lists what each version built, oldest first. A version cut
// before builds were selections names nothing it built, so it is not listed
// (new model only). One origin fetch (the tag list), then a local read of
// the requirements at each listed tag.
func (s *artifactService) ListVersions(ctx context.Context, orgID, projectID string) ([]Version, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	versions := versionTags(tags)
	slices.Reverse(versions)
	out := []Version{}
	for _, t := range versions {
		plan, ok := parseScope(t.Body)
		if !ok {
			continue
		}
		files, err := s.readBundleAtTag(ctx, ref, t.Name, requirementsPrefix, requirementsBundleFilter)
		if err != nil {
			return nil, fmt.Errorf("read requirements at %s: %w", t.Name, err)
		}
		v := Version{Name: t.Name, ProductWide: plan.ProductWide, HeldBack: plan.HeldBack, Fixes: fixesOf(t.Body)}
		for _, f := range reqspec.Parse(files).Features {
			if slices.Contains(plan.Features, f.ID) {
				v.Features = append(v.Features, VersionFeature{ID: f.ID, Name: f.Name, Lines: reqspec.FeatureLines(files, f.ID)})
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// ValidationScope is what a version validates (B4): every feature built in it
// or an earlier version, minus the stories no version has built yet.
type ValidationScope struct {
	// Features are the features built in this version or an earlier one.
	Features []string
	// HeldBack are stories of those features held back by the version that
	// last built each: they wait on a feature nobody built.
	HeldBack []string
	// Built names what this version itself built ("F3 Payroll export").
	Built []string
	// Earlier are the versions before it, newest first: where "was passing"
	// looks.
	Earlier []string
}

// ValidationScope reads what a version validates from the tag annotations of
// it and the versions before it. ok is false when the version names nothing it
// built (cut before builds were selections): it validates the whole oracle.
func (s *artifactService) ValidationScope(ctx context.Context, orgID, projectID, version string) (ValidationScope, bool, error) {
	var out ValidationScope
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return out, false, err
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return out, false, fmt.Errorf("list tags: %w", err)
	}
	versions := versionTags(tags) // newest first
	at := slices.IndexFunc(versions, func(t sourcecontrol.TagInfo) bool { return t.Name == version })
	if at < 0 {
		return out, false, nil
	}
	plan, ok := parseScope(versions[at].Body)
	if !ok {
		return out, false, nil
	}
	for _, t := range versions[at+1:] {
		out.Earlier = append(out.Earlier, t.Name)
	}
	// Oldest first, so the version that last built a feature has the last word
	// on which of its stories it held back.
	heldBy := map[string][]string{}
	for i := len(versions) - 1; i >= at; i-- {
		p, ok := parseScope(versions[i].Body)
		if !ok {
			continue
		}
		for _, f := range p.Features {
			heldBy[f] = slices.DeleteFunc(slices.Clone(p.HeldBack), func(id string) bool { return !strings.HasPrefix(id, f+".") })
		}
	}
	for f, held := range heldBy {
		out.Features = append(out.Features, f)
		out.HeldBack = append(out.HeldBack, held...)
	}
	slices.SortFunc(out.Features, reqspec.CompareIDs)
	slices.SortFunc(out.HeldBack, reqspec.CompareIDs)

	files, err := s.readBundleAtTag(ctx, ref, version, requirementsPrefix, requirementsBundleFilter)
	if err != nil {
		return out, false, fmt.Errorf("read requirements at %s: %w", version, err)
	}
	for _, f := range reqspec.Parse(files).Features {
		if slices.Contains(plan.Features, f.ID) {
			out.Built = append(out.Built, f.ID+" "+f.Name)
		}
	}
	return out, true, nil
}

// ErrNothingToRepair: the version named is not one a repair can fix.
var ErrNothingToRepair = errors.New("not a version that can be repaired")

// TagRepair cuts a repair version of `of` (B4): "v1.1", then "v1.2", at the
// same commit as `of`, so it carries the same specs and builds the same
// features. Its annotation is `of`'s with a `Fixes:` line. A repair of a
// repair fixes the version the repair fixed.
func (s *artifactService) TagRepair(ctx context.Context, orgID, projectID, of string) (string, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return "", err
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("list tags: %w", err)
	}
	versions := versionTags(tags)
	at := slices.IndexFunc(versions, func(t sourcecontrol.TagInfo) bool { return t.Name == of })
	if at < 0 {
		return "", fmt.Errorf("%w: no version named %q", ErrNothingToRepair, of)
	}
	fixed := versions[at]
	if base := fixesOf(fixed.Body); base != "" {
		of = base
	}
	plan, ok := parseScope(fixed.Body)
	if !ok {
		return "", fmt.Errorf("%w: %s names nothing it built", ErrNothingToRepair, of)
	}
	n := 1
	for _, t := range versions {
		if fixesOf(t.Body) == of {
			n++
		}
	}
	name := fmt.Sprintf("%s.%d", of, n)
	if verr := ValidateVersionName(name); verr != nil {
		return "", fmt.Errorf("%w: %w", ErrVersionNameInvalid, verr)
	}
	message := scopeBody(plan) + "\n" + scopeFixes + " " + of
	if err := s.createVersionTag(ctx, ref, &tags, &name, message, fixed.CommitHash, false); err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "spec tagged a repair", "project", projectID, "tag", name, "fixes", of, "commit", fixed.CommitHash)
	return name, nil
}
