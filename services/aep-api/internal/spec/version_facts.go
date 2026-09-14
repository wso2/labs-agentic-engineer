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

// WHAT THIS VERSION WOULD DO, read before it is cut (console ADR-0029).
//
// The Build click already waits on preflight, so preflight answers both halves
// of the question: what is still unresolved (its own job), and what this
// version is called and changes (this file). The change list is computed the
// same way the "spec is dirty" chip is — two `specs/` tree listings compared by
// blob sha — because the trees are already read on this path and a listing
// carries every fact a row needs.
//
// Rows are named by OWNER, not by file: `specs/design/components/orders-api/…`
// is the component and `specs/design/dependencies/currency-service/…` is the
// dependency. A platform resource owns no directory (it lives inside a
// component's `design.json`), so those rows come from reading the design at
// both trees — which is also the only way to see one arrive at all.
//
// The REQUIREMENTS are deliberately not a row. Every row here names something
// that exists once the version is built; the requirements are the input to
// that, and they move on nearly every version, so the row carried no signal
// while sitting among component names as though it were one of them.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Version change kinds and states — the wire vocabulary of BuildChange.
const (
	VersionChangeKindComponent = "component"
	VersionChangeKindExternal  = "external"
	VersionChangeKindResource  = "platform-resource"

	VersionChangeNew     = "new"
	VersionChangeChanged = "changed"
	VersionChangeRemoved = "removed"
)

// The two design directories a row can be named after, and the file a
// component's platform resources are declared in.
const (
	componentsPrefix    = designPrefix + "components/"
	dependenciesPrefix  = designPrefix + "dependencies/"
	componentDesignFile = "design.json"
)

// VersionChange is one row of "what changed since <version>".
type VersionChange struct {
	Name  string
	Kind  string
	State string
}

// VersionFacts is what the Start build dialog reads: the version's identity and
// what cutting it would carry.
type VersionFacts struct {
	// CurrentVersion is the newest version's name, empty when never built.
	CurrentVersion string
	// SuggestedVersion prefills the name field. Always set.
	SuggestedVersion string
	// SpecUnchanged: the tree matches the newest version's, so a build reuses
	// it. Changes is empty in that case, by construction.
	SpecUnchanged bool
	Changes       []VersionChange
}

// BuildVersionFacts implements ArtifactService.
func (s *artifactService) BuildVersionFacts(ctx context.Context, orgID, projectID string) (VersionFacts, error) {
	_, ref, err := s.readyRef(ctx, orgID, projectID)
	if err != nil {
		return VersionFacts{}, err
	}
	headEntries, head, err := s.git.Workspace().List(ctx, ref, "")
	if err != nil {
		return VersionFacts{}, fmt.Errorf("list head tree: %w", err)
	}
	tags, err := s.listVersionTags(ctx, ref)
	if err != nil {
		return VersionFacts{}, fmt.Errorf("list tags: %w", err)
	}
	facts := VersionFacts{SuggestedVersion: suggestedVersionName(tags)}

	latest, ok := latestVersionTag(tags)
	if !ok {
		// A first build creates everything it names.
		facts.Changes = versionChanges(nil, headEntries, s.designAt(ctx, ref, ""), s.designAt(ctx, ref, head))
		return facts, nil
	}
	facts.CurrentVersion = latest.Name

	tagEntries, _, err := s.git.Workspace().List(ctx, ref, latest.CommitHash)
	if err != nil {
		return VersionFacts{}, fmt.Errorf("list tree at %s: %w", latest.Name, err)
	}
	if specTreesEqual(headEntries, tagEntries) {
		facts.SpecUnchanged = true
		return facts, nil
	}
	facts.Changes = versionChanges(tagEntries, headEntries,
		s.designAt(ctx, ref, latest.CommitHash), s.designAt(ctx, ref, head))
	return facts, nil
}

// designAt reads the platform-resource dependency names the design declares at
// one commit. Best-effort by design: a resource row is an EXTRA the tree cannot
// show, so a read that fails costs that row and never the whole answer — the
// build click must not fail because one design.json would not parse.
func (s *artifactService) designAt(ctx context.Context, ref sourcecontrol.RepoRef, commit string) map[string]bool {
	if commit == "" {
		return nil
	}
	entries, _, err := s.git.Workspace().List(ctx, ref, commit)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, e := range entries {
		dir, ok := pathSegmentUnder(e.Path, componentsPrefix)
		if !ok || !strings.HasSuffix(e.Path, "/"+componentDesignFile) {
			continue
		}
		raw, _, rerr := s.git.Workspace().ReadFile(ctx, ref, commit, e.Path)
		if rerr != nil {
			continue
		}
		comp, perr := parseComponentDesignJSON(dir, string(raw))
		if perr != nil {
			continue
		}
		for _, d := range comp.Dependencies {
			if d.Kind == DependencyKindPlatformResource && d.Name != "" {
				out[d.Name] = true
			}
		}
	}
	return out
}

// owner is a change row's identity while the sets are being compared.
type owner struct {
	kind string
	name string
}

// versionChanges compares two `specs/` listings and returns one row per owner
// that differs, plus the platform resources the two designs disagree about.
//
// A nil `before` is a first build: every owner at HEAD is new.
func versionChanges(before, after []sourcecontrol.Entry, resourcesBefore, resourcesAfter map[string]bool) []VersionChange {
	beforeOwners, afterOwners := ownerShas(before), ownerShas(after)

	var rows []VersionChange
	for own, sha := range afterOwners {
		prior, existed := beforeOwners[own]
		switch {
		case !existed:
			rows = append(rows, VersionChange{Name: own.name, Kind: own.kind, State: VersionChangeNew})
		case prior != sha:
			rows = append(rows, VersionChange{Name: own.name, Kind: own.kind, State: VersionChangeChanged})
		}
	}
	for own := range beforeOwners {
		if _, still := afterOwners[own]; !still {
			rows = append(rows, VersionChange{Name: own.name, Kind: own.kind, State: VersionChangeRemoved})
		}
	}
	// A platform resource has no directory of its own, so it is neither new nor
	// gone in the tree — only the design says so. It is never "changed": a
	// parameter edit is the component's change, and the row would say nothing a
	// reader could act on.
	for name := range resourcesAfter {
		if !resourcesBefore[name] {
			rows = append(rows, VersionChange{Name: name, Kind: VersionChangeKindResource, State: VersionChangeNew})
		}
	}
	for name := range resourcesBefore {
		if !resourcesAfter[name] {
			rows = append(rows, VersionChange{Name: name, Kind: VersionChangeKindResource, State: VersionChangeRemoved})
		}
	}

	// By kind, then by name — a stable order, and the one the dialog groups on.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

// ownerShas reduces a tree listing to one fingerprint per owner: the owner's
// blob shas, joined. Two trees agree about an owner exactly when every file it
// owns is byte-identical, which is what a "changed" row means.
func ownerShas(entries []sourcecontrol.Entry) map[owner]string {
	paths := map[owner][]string{}
	for _, e := range entries {
		own, ok := ownerOf(e.Path)
		if !ok {
			continue
		}
		paths[own] = append(paths[own], e.Path+":"+e.SHA)
	}
	out := make(map[owner]string, len(paths))
	for own, ps := range paths {
		sort.Strings(ps)
		out[own] = strings.Join(ps, "\n")
	}
	return out
}

// ownerOf names what a `specs/` path belongs to, or ok=false for a path no row
// speaks for.
func ownerOf(path string) (owner, bool) {
	if name, ok := pathSegmentUnder(path, componentsPrefix); ok {
		return owner{kind: VersionChangeKindComponent, name: name}, true
	}
	if name, ok := pathSegmentUnder(path, dependenciesPrefix); ok {
		return owner{kind: VersionChangeKindExternal, name: name}, true
	}
	return owner{}, false
}

// pathSegmentUnder returns the directory name directly under `prefix`, when
// `path` names a file inside one.
func pathSegmentUnder(path, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok {
		return "", false
	}
	name, _, found := strings.Cut(rest, "/")
	if !found || name == "" {
		return "", false
	}
	return name, true
}
