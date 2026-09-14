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

package build

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// The two drawer/gate item kinds an `external` dependency's computed status
// can raise, restoring the build-gate Task 1 orphaned (see the doc comment on
// dependencyBlocker below). "external-spec" is the pre-existing kind (its
// PreflightItemKind wire value already existed — Task 1 only stopped emitting
// it); the other is new.
const (
	kindExternalUnresolved = "external-unresolved"
	kindExternalSpec       = "external-spec"
)

// dependencyBlocker is the SINGLE place that maps an `external` dependency's
// already-computed Status/Reason onto a user-facing blocker: the drawer item
// kind + a plain-language description. Both preflight.externalItems (the
// drawer's GET-time item emission) and Service.dependencyGateFailures (the
// build-time hard gate) call this exact function, so the two surfaces can
// never drift apart.
//
// Status/Reason are NOT recomputed here — spec.ComputeDependencyStatus
// (internal/spec/dependency_status.go) is the single authority for
// deriving them, and it already ran, with fresh resolver-port lookups,
// upstream of every caller of this function: artifacts.ArtifactStore's
// resolveExternalDependencies calls it inside ReadDesign/AssembleDesignFrom,
// which is what both PreflightDesignReader.ReadDesignComponents (preflight.go)
// and the build hard gate's own fresh ReadDesignComponents call (below) return
// through. Re-deriving the precedence table here — or re-invoking
// ComputeDependencyStatus a second time with a second, independently-wired set
// of resolver ports — would duplicate live catalog/registry calls within one
// logical preflight/build computation and risk the two computations
// disagreeing if registry state changes mid-request; reading the
// already-computed Status/Reason field instead (exactly how the pre-existing
// org-service item, orgServiceItems, already works) keeps ComputeDependencyStatus
// invoked exactly once per fresh design read.
//
// Only `external` dependencies are classified here: `org-service`'s own
// unresolved/blocked states keep their existing "org-service" item
// kind (orgServiceItems, unchanged) and are gated at design-save time
// (design.SaveAndProceed's firstUnresolvedDependency) rather than here — the
// orphaned gate this task restores is specifically the `external` one Task 1
// dropped (dependency-management migration plan, the P4 proceed-gate note).
func dependencyBlocker(d spec.Dependency) (kind, description string, blocked bool) {
	if d.Kind != spec.DependencyKindExternal {
		return "", "", false
	}
	switch d.Status {
	case spec.DependencyStatusUnresolved:
		switch d.Reason {
		case spec.DependencyReasonNeedsContract:
			return kindExternalSpec, "No contract yet — provide the API document to continue.", true
		case spec.DependencyReasonNeedsAcceptance:
			return kindExternalUnresolved, "The agent wrote this contract from research — accept the assumption, or provide the document.", true
		case spec.DependencyReasonNeedsInput:
			return kindExternalUnresolved, "No provider chosen yet — choose which one to use.", true
		}
	}
	return "", "", false
}

// dependencyGateFailures is the build-time hard gate (dependency-management
// migration, restoring the gate Task 1 orphaned): a FRESH ReadDesignComponents
// read (never the client-supplied inputs, never a cached preflight response —
// "re-run the computation" means invoke, never copy) walks every component's
// `external` dependencies — service AND web-application alike (#252 Task 14:
// the guard that skipped non-service components here predates this feature
// and left a gap the drawer already closed for every component kind; a
// web-application's unresolved external dependency must block the build the
// same way a service's does) — and maps each still-blocked one (unresolved
// with needs-contract/needs-acceptance/needs-input) through the exact same
// dependencyBlocker used by preflight.externalItems, into the existing
// InputFailure shape (handlers_build.go's BuildResponse.failures). A doctored
// client that skips the drawer (or supplies no/insufficient inputs) cannot
// bypass this: the gate reads the design's Status/Reason at HEAD AFTER
// InputsCoordinator.ApplyPreTag has had a chance to commit any drawer-supplied
// resolution (e.g. a pasted external-spec) — Run() calls this AFTER ApplyPreTag
// but BEFORE the tag-cut, so a legitimate resolution submitted with THIS build
// request is reflected, while an unresolved dependency the client didn't
// actually fix still blocks.
//
// A nil design reader (composition root didn't wire one) fails OPEN — no
// design read means no dependency can be classified, mirroring every other
// nil-safe port in this package (Coord, Tasks).
func (s *Service) dependencyGateFailures(ctx context.Context, orgID, projectID string) ([]InputFailure, error) {
	if s.design == nil {
		return nil, nil
	}
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	var failures []InputFailure
	for _, c := range comps {
		for _, d := range c.Dependencies {
			if kind, desc, blocked := dependencyBlocker(d); blocked {
				failures = append(failures, InputFailure{
					Component:  c.Name,
					Dependency: d.Name,
					Kind:       kind,
					Reason:     desc,
				})
			}
		}
	}
	return failures, nil
}
