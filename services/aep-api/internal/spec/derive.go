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
	if err := rejectUnknownResourceTypes(designFile.Components, markers); err != nil {
		return err
	}
	if _, err := s.persistPlatformResourceDerivation(ctx, orgID, projectID, designFile, markers); err != nil {
		return err
	}
	return nil
}

// persistPlatformResourceDerivation runs every derivation over designFile's
// components and, for each component whose DERIVED state changed, commits the
// updated design.json to main via the committed-truth write surface (the same
// designFileCommitter port + per-component SplitDesign render CollectSpec uses).
// Only changed components are written, so a re-save that derives the same values
// commits nothing — the derivations are re-run on every save (they must be, since
// a rename or a catalog change moves them), and without this an unchanged design
// would churn a commit each time.
//
// Returns (true, nil) when at least one commit landed — the caller must then
// re-resolve HEAD (its designFile + any pinned commitSHA are now stale), the
// same convention SaveAndProceed's auto-fetch-on-save step already follows.
// Returns a non-nil error (wrapping ErrEndUserAuthConflict) with NO commit
// attempted when deriveEndUserAuth rejects the design — the save must stop
// there, exactly like the unresolved-dependency proceed-gate. Wiring derivation
// cannot reject: an underivable dependency is an absent wiring the coding agent
// reports, not a design the platform refuses to save.
//
// A nil fileCommitter (degraded boot — mirrors CollectSpec) is a best-effort
// no-op after a successful derivation: designFile.Components is still mutated
// in place so THIS response reflects the derived value, but nothing is
// persisted, so it will not survive to the next independent design read.
func (s *designService) persistPlatformResourceDerivation(ctx context.Context, orgID, projectID string, designFile *DesignFile, types map[string]CRTType) (bool, error) {
	// Snapshot COPIES of the derived state (never the pointers): both derivations
	// mutate through the pointers/slices the components already hold, so capturing
	// a pointer here would alias the post-mutation value and the change-detection
	// below would never see a diff.
	before := make([]derivedState, len(designFile.Components))
	for i, c := range designFile.Components {
		before[i] = snapshotDerived(c)
	}
	if err := deriveEndUserAuth(designFile.Components, types); err != nil {
		return false, fmt.Errorf("%w: %v", ErrEndUserAuthConflict, err)
	}
	deriveDependencyWiring(designFile.Components, types, projectID)
	if s.fileCommitter == nil {
		return false, nil
	}

	var writes []DesignFileWrite
	for i := range designFile.Components {
		if derivedStateEqual(before[i], snapshotDerived(designFile.Components[i])) {
			continue
		}
		comp := designFile.Components[i]
		rendered, rerr := SplitDesign(&DesignFile{Components: []DesignComponent{comp}})
		if rerr != nil {
			return false, fmt.Errorf("render component %q design.json: %w", comp.Name, rerr)
		}
		designSub := "components/" + comp.Name + "/design.json"
		content, ok := rendered[designSub]
		if !ok {
			return false, fmt.Errorf("render component %q design.json: %q missing from split", comp.Name, designSub)
		}
		designFull := DesignDir + "/" + designSub
		_, sha, exists, rerr := s.fileCommitter.ReadFile(ctx, orgID, projectID, designFull)
		if rerr != nil {
			return false, fmt.Errorf("read %q for CAS: %w", designFull, rerr)
		}
		if !exists {
			return false, fmt.Errorf("component %q design.json missing on disk", comp.Name)
		}
		writes = append(writes, DesignFileWrite{Path: designFull, Content: content, BaseSHA: sha})
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
