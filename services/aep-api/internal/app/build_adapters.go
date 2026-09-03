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

package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/build"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// buildSpecTagger adapts spec.SaveSpec onto the build feature's
// SpecTagger port. Errors pass through unwrapped — the build handler unpacks
// *spec.SpecValidationError into the 422 detail.
type buildSpecTagger struct {
	art spec.ArtifactService
}

func (t buildSpecTagger) TagSpec(ctx context.Context, orgID, projectID string) (*spec.SpecSaveResult, error) {
	return t.art.SaveSpec(ctx, orgID, projectID, spec.SaveRequest{Message: "Build"})
}

func (t buildSpecTagger) BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (spec.BuildScope, error) {
	return t.art.BuildScopeAtTag(ctx, orgID, projectID, tag)
}

// designFactDeriver is the composition root's narrow consumer view of the
// concrete *design service — the pre-tag step the thin POST /build path reuses
// (issue #164). The design package no longer exports an interface (its read
// HTTP surface was retired); *designService satisfies this structurally, so app
// wires the concrete value straight in.
type designFactDeriver interface {
	DerivePlatformResourceFactsAtHead(ctx context.Context, orgID, projectID string) error
}

// buildDesignDeriver adapts design's DerivePlatformResourceFactsAtHead onto the build
// feature's DesignFactDeriver port, translating design's domain sentinels into the
// build-local ones the handler maps to 409 / 503 (build cannot import design —
// arch allowlist). Everything else passes through so the handler 500s it.
type buildDesignDeriver struct {
	svc designFactDeriver
}

func (d buildDesignDeriver) DerivePlatformResourceFactsAtHead(ctx context.Context, orgID, projectID string) error {
	err := d.svc.DerivePlatformResourceFactsAtHead(ctx, orgID, projectID)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, spec.ErrEndUserAuthConflict):
		return fmt.Errorf("%w: %v", build.ErrEndUserAuthConflict, err)
	case errors.Is(err, spec.ErrUnknownResourceType):
		return fmt.Errorf("%w: %v", build.ErrUnknownResourceType, err)
	case errors.Is(err, spec.ErrResourceCatalogUnavailable):
		return fmt.Errorf("%w: %v", build.ErrResourceCatalogUnavailable, err)
	default:
		return err
	}
}

// buildProvisionStatus adapts provisioning.Service.Status onto the build
// preflight's ProvisionStatusReader port. It collapses the provisioning
// tri-state onto the preflight bool: a dependency is "already handled" (Ready ==
// true, so the drawer does NOT re-ask for it) whenever its status is anything
// other than "unknown". This is intentionally NOT DependencyStatus.Ready, which
// is false for BOTH "unknown" (nothing started) AND "provisioning" (in-flight) —
// returning .Ready would re-ask a dependency that is already being provisioned.
// A Status error surfaces the item (safe direction: preflight over-asks rather
// than silently dropping a dependency).
type buildProvisionStatus struct {
	svc *provisioning.Service
}

func (b buildProvisionStatus) Ready(ctx context.Context, orgID, projectID, depName string) (bool, error) {
	st, err := b.svc.Status(ctx, orgID, projectID, depName, "")
	if err != nil {
		return false, err
	}
	return st.Status != "unknown", nil
}

// buildOrgCatalog adapts provisioning.Service onto preflight's OrgCatalogReader:
// a Registered External (org env cells, or a catalog RT that still carries
// consumption instructions after the value plane was wiped) must not collect
// values. Nil service is fail-open (HasOrgEnvCells false).
type buildOrgCatalog struct {
	svc *provisioning.Service
}

func (b buildOrgCatalog) HasOrgEnvCells(ctx context.Context, orgID, name string) bool {
	if b.svc == nil {
		return false
	}
	return b.svc.HasOrgEnvCells(ctx, orgID, name)
}

// buildGateResolver adapts the provisioning feature onto the build plan path's
// GateResolver port: author the version's dependencies and mint its
// `provision` gates INTO the version's milestone, so the run's dispatch
// predicate sees them.
//
// It collapses provisioning's per-dependency failure list into one error on
// purpose: a gate that could not be authored means the version's run would wait
// on a hold that will never lift, so the run settles instead.
type buildGateResolver struct {
	prov *provisioning.Service
}

func (b buildGateResolver) ProvisionForBuild(ctx context.Context, orgID, projectID, tag string, milestoneNumber int, inputs []delivery.ProvisionInput) error {
	fails, err := b.prov.ProvisionForBuild(ctx, orgID, orgID, projectID, tag, milestoneNumber, mapProvisionInputs(inputs))
	if err != nil {
		return err
	}
	return aggregateProvisionFailures(fails)
}

func aggregateProvisionFailures(fails []provisioning.ProvisionFailure) error {
	if len(fails) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(fails))
	permanent := false
	var cause error
	for _, f := range fails {
		reasons = append(reasons, f.Dependency+": "+f.Reason)
		if errors.Is(f.Err, dependencies.ErrProvisionPermanent) {
			permanent = true
			cause = f.Err
		}
	}
	joined := fmt.Errorf("provision %d dependenc(ies) failed: %s", len(fails), strings.Join(reasons, "; "))
	if !permanent {
		return joined
	}
	return fmt.Errorf("%w: %w: %w", delivery.ErrProvisionPermanent, cause, joined)
}

// mapProvisionInputs maps the delivery-root payload onto the provisioning
// feature's twin. The two structs are field-identical by design — delivery must
// not import the provisioning feature — so the copy lives here at the
// composition root.
func mapProvisionInputs(inputs []delivery.ProvisionInput) []provisioning.BuildProvisionInput {
	mapped := make([]provisioning.BuildProvisionInput, 0, len(inputs))
	for _, in := range inputs {
		mapped = append(mapped, provisioning.BuildProvisionInput{
			Component:      in.Component,
			Dependency:     in.Dependency,
			Kind:           in.Kind,
			Config:         in.Config,
			SecretRefByEnv: in.SecretRefByEnv,
			Parameters:     in.Parameters,
			Approved:       in.Approved,
		})
	}
	return mapped
}

// providerBuildTrigger adapts build.StartProjectBuild onto provisioning's
// ProviderBuildTrigger port (issue #164, Task 4): the automated org-service
// visibility flow kicks a not-yet-published provider project's build so it
// deploys (and publishes org-wide). StartProjectBuild is idempotent — an
// already-running provider build is treated as success.
type providerBuildTrigger struct {
	build *build.Service
}

func (t providerBuildTrigger) TriggerBuild(ctx context.Context, orgID, projectID string) error {
	return t.build.StartProjectBuild(ctx, orgID, projectID)
}
