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
	"fmt"
	"log/slog"
	"maps"
)

// THE DESIGN-SAVE DERIVATION PASS.
//
// Some of a design is authored by the architect and some is DERIVED by the
// platform: `exposesAPI.auth` off a resource type's role marker (derive_auth.go)
// and each resource dependency's consumer-side wiring off its declared outputs
// (derive_wiring.go). Both read the same one catalog call, both mutate the design
// in place, and both must be on disk before anything downstream acts on the
// tagged version — so they are ONE pass with one commit, not two.
//
// This file owns that pass: read HEAD, derive, detect what actually changed, and
// commit only those components. The individual derivations own their own rules.

// DerivePlatformResourceFactsAtHead reads the design at HEAD, runs every
// platform-side derivation over it, and commits what changed — the pre-tag step
// the thin POST /build path runs BEFORE its own tag-cut, so the derived values
// are captured in the version tag (issue #164) and are already what the NEXT
// design read sees. Returns ErrEndUserAuthConflict on an explicit conflicting
// service-required, ErrUnknownResourceType when a platform-resource dependency
// names a resourceType absent from the installed CRT catalog, and
// ErrResourceCatalogUnavailable when a platform-resource dependency exists but
// the CRT catalog is unreachable (fail-closed). A design with nothing to derive
// (missing/empty design, no resource dependency) is a no-op returning nil. The
// caller re-reads HEAD after (its TagSpec re-resolves HEAD), so this does not
// return the mutated design.
func (s *designService) DerivePlatformResourceFactsAtHead(ctx context.Context, orgID, projectID string) error {
	designFile, err := s.store.ReadDesign(ctx, orgID, projectID)
	if err != nil {
		if IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("read design: %w", err)
	}
	if designFile == nil {
		return nil
	}
	markers, err := s.resourceTypesForDerivation(ctx, designFile)
	if err != nil {
		return err
	}
	if _, err := s.persistPlatformResourceDerivation(ctx, orgID, projectID, designFile, markers); err != nil {
		return err
	}
	// Agent tool resolution (derive_agent_tools.go) already ran above, inside
	// s.store.ReadDesign → AssembleDesignFrom — it is read-time computed
	// (never a write rejection; see that file's doc comment for why) exactly
	// like Dependency.Status/Reason, so every design read already carries it
	// and no second pass is needed here.
	return nil
}

// DerivedDesignFile is one file under specs/design/ that the derivation pass
// changed, rendered exactly as it is committed.
type DerivedDesignFile struct {
	// Path is relative to DesignDir, with forward slashes
	// (`components/<name>/design.json`, `dependencies/<name>/dependency.json`).
	Path string
	// Content is the canonical render: SplitDesign for a component,
	// the dependency-definition codec for a lifted definition.
	Content string
	// CreateOnly marks a definition lifted from a legacy component (ADR-0027):
	// it is written only when no file exists at Path yet. Every other entry
	// replaces a component design.json that must already exist.
	CreateOnly bool
}

// DerivePlatformResourceFacts derives a design's platform facts in place and
// returns the design files that changed, rendered.
//
// types is the installed resource-type catalog; nil or empty skips the
// membership check (the disabled-catalog path) and qualifies nothing. projectID
// is the OC name prefix of every derived ref and scoped component name.
//
// Errors: ErrUnknownResourceType, or ErrEndUserAuthConflict on an explicit
// conflicting service-required. On an error nothing in designFile is mutated
// and no file is returned. A component is returned only when its DERIVED state
// changed or it is a legacy carrier (whose re-render is the migration), so a
// second run over its own output returns nothing.
func DerivePlatformResourceFacts(designFile *DesignFile, types map[string]CRTType, projectID string) ([]DerivedDesignFile, error) {
	if err := rejectUnknownResourceTypes(designFile.Components, types); err != nil {
		return nil, err
	}
	// Snapshot COPIES of the derived state (never the pointers): both derivations
	// mutate through the pointers/slices the components already hold, so capturing
	// a pointer here would alias the post-mutation value and the change-detection
	// below would never see a diff.
	before := make([]derivedState, len(designFile.Components))
	for i, c := range designFile.Components {
		before[i] = snapshotDerived(c)
	}
	if err := deriveEndUserAuth(designFile.Components, types); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEndUserAuthConflict, err)
	}
	deriveDependencyWiring(designFile.Components, types, projectID)

	legacy := make(map[string]bool, len(designFile.LegacyCarriers))
	for _, name := range designFile.LegacyCarriers {
		legacy[name] = true
	}
	var files []DerivedDesignFile
	for i := range designFile.Components {
		// A component still carrying an external dependency's definition
		// fields is re-rendered as a bare reference — that drop is the
		// migration (ADR-0027), and the lifted definitions are returned below.
		if derivedStateEqual(before[i], snapshotDerived(designFile.Components[i])) && !legacy[designFile.Components[i].Name] {
			continue
		}
		comp := designFile.Components[i]
		rendered, rerr := SplitDesign(&DesignFile{Components: []DesignComponent{comp}})
		if rerr != nil {
			return nil, fmt.Errorf("render component %q design.json: %w", comp.Name, rerr)
		}
		designSub := "components/" + comp.Name + "/design.json"
		content, ok := rendered[designSub]
		if !ok {
			return nil, fmt.Errorf("render component %q design.json: %q missing from split", comp.Name, designSub)
		}
		files = append(files, DerivedDesignFile{Path: designSub, Content: content})
	}
	// Definitions lifted from legacy components have no file yet: return them,
	// so the directory exists before the next read strips the components.
	for _, def := range designFile.Dependencies {
		body, err := marshalDependencyDefinitionJSON(def.Name, def)
		if err != nil {
			return nil, fmt.Errorf("render dependency %q: %w", def.Name, err)
		}
		files = append(files, DerivedDesignFile{Path: dependencyDesignKey(def.Name), Content: string(body), CreateOnly: true})
	}
	return files, nil
}

// persistPlatformResourceDerivation runs DerivePlatformResourceFacts over
// designFile and commits the files it returns to main via the committed-truth
// write surface (the same designFileCommitter port CollectSpec uses).
//
// Returns (true, nil) when at least one commit landed — the caller must then
// re-resolve HEAD (its designFile + any pinned commitSHA are now stale).
// Returns a non-nil error (wrapping ErrUnknownResourceType or
// ErrEndUserAuthConflict) with NO commit attempted when the derivation refuses
// the design; the save stops there (ADR-0041).
//
// A nil fileCommitter (degraded boot) commits nothing; designFile is still
// mutated in place.
func (s *designService) persistPlatformResourceDerivation(ctx context.Context, orgID, projectID string, designFile *DesignFile, types map[string]CRTType) (bool, error) {
	derived, err := DerivePlatformResourceFacts(designFile, types, projectID)
	if err != nil {
		return false, err
	}
	if s.fileCommitter == nil {
		return false, nil
	}

	var writes []DesignFileWrite
	for _, f := range derived {
		full := DesignDir + "/" + f.Path
		_, sha, exists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, full)
		if rerr != nil {
			return false, fmt.Errorf("read %q for CAS: %w", full, rerr)
		}
		if f.CreateOnly {
			if !exists {
				writes = append(writes, DesignFileWrite{Path: full, Content: f.Content})
			}
			continue
		}
		if !exists {
			return false, fmt.Errorf("%s missing on disk", f.Path)
		}
		writes = append(writes, DesignFileWrite{Path: full, Content: f.Content, BaseSHA: sha})
	}
	if len(writes) == 0 {
		return false, nil
	}
	if err := s.fileCommitter.Commit(ctx, orgID, projectID, writes,
		"Derive platform-resource facts (exposesAPI.auth, dependency wiring)"); err != nil {
		return false, err
	}
	slog.InfoContext(ctx, "design save: platform-resource derivation persisted",
		"org", orgID, "project", projectID, "components", len(writes))
	return true, nil
}

// derivedState is the platform-derived slice of one component — everything design
// save computes rather than the architect authoring it. Change detection compares
// these, not whole rendered files: a re-render can differ for formatting reasons
// that would commit-churn on every save, while this diffs exactly what the
// derivations write.
type derivedState struct {
	exposesAPI *ExposesAPI
	// wiring is keyed by dependency name — dependencies are name-unique within a
	// component, and keying by index would report a diff for a pure reorder.
	wiring map[string]*DependencyWiring
}

// snapshotDerived copies a component's derived state by value, so a later
// in-place derivation cannot mutate the snapshot through a shared pointer.
func snapshotDerived(c DesignComponent) derivedState {
	st := derivedState{wiring: make(map[string]*DependencyWiring, len(c.Dependencies))}
	if c.ExposesAPI != nil {
		v := *c.ExposesAPI
		st.exposesAPI = &v
	}
	for _, d := range c.Dependencies {
		if d.Wiring != nil {
			v := *d.Wiring
			v.EnvBindings = maps.Clone(d.Wiring.EnvBindings)
			// The endpoints[] variant is a POINTER on the copied struct, so the
			// shallow copy above still shares it. Clone it too, or a later
			// in-place derivation mutates the "before" snapshot through it and the
			// diff can never see an endpoint change.
			if d.Wiring.Endpoint != nil {
				ep := *d.Wiring.Endpoint
				ep.EnvBindings = maps.Clone(d.Wiring.Endpoint.EnvBindings)
				v.Endpoint = &ep
			}
			st.wiring[d.Name] = &v
		}
	}
	return st
}

// derivedStateEqual reports whether two snapshots agree on every derived field.
func derivedStateEqual(a, b derivedState) bool {
	if !exposesAPIEqual(a.exposesAPI, b.exposesAPI) || len(a.wiring) != len(b.wiring) {
		return false
	}
	for name, w := range a.wiring {
		if !dependencyWiringEqual(w, b.wiring[name]) {
			return false
		}
	}
	return true
}
