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
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// InputsCoordinator turns the build drawer's inputs into the pre-tag side
// effects a build requires: it collects external specs and runs the design
// derivations (end-user auth + dependency wiring — ADR-0013) BEFORE the tag-cut
// (ApplyPreTag, which runs on EVERY build, drawer inputs or not, because the
// derivations depend on the design rather than on the inputs), derives unset
// external-config authoring from that design, and passes platform resource
// params + approvals through (BuildProvisionInputs). It is a thin
// orchestrator — it holds no state and authors no OC resources
// (that is the workflow's job, Task 3).
type InputsCoordinator struct {
	spec   SpecCollector
	auth   DesignFactDeriver
	design PreflightDesignReader
	skills SkillMirror
}

// SkillMirror refreshes the project repo's `.claude/skills/` copies from the
// org library. Run pre-tag so the version the build cuts carries the guidance
// the build was designed against. Diff-first; nil → skipped.
type SkillMirror interface {
	SyncProjectSkills(ctx context.Context, orgID, projectID string) error
}

// NewInputsCoordinator wires the coordinator.
func NewInputsCoordinator(spec SpecCollector, auth DesignFactDeriver, design PreflightDesignReader) *InputsCoordinator {
	return &InputsCoordinator{spec: spec, auth: auth, design: design}
}

// WithSkillMirror enables the pre-tag skills refresh (nil → skipped). Returns
// the receiver for chained construction.
func (c *InputsCoordinator) WithSkillMirror(m SkillMirror) *InputsCoordinator {
	c.skills = m
	return c
}

// ApplyPreTag runs the side effects that MUST land on HEAD before the tag-cut
// captures the spec: every external-spec input is collected (content →
// rawSpec, else url → specURL), then the design derivations run exactly once —
// end-user auth AND each resource dependency's wiring (ADR-0013). A per-input
// CollectSpec failure is reported as an InputFailure (the handler returns
// {failures} and cuts no tag); a derivation error (auth conflict /
// catalog-unavailable) propagates as err for the handler to map to 409 / 503.
//
// The derivation step is NOT conditional on inputs: it reads the design, not the
// drawer, so a plain re-build with an empty drawer still stamps. That is what
// makes the wiring present in every version tag rather than only in ones whose
// build happened to carry inputs.
//
// When any spec collection fails the build is already aborting, so we return
// the failures WITHOUT deriving — deriving would commit to HEAD for a build that
// never cuts a tag, and a derivation error would mask the spec failures the user
// actually needs to see. The next Build re-derives idempotently.
func (c *InputsCoordinator) ApplyPreTag(ctx context.Context, orgID, projectID string, inputs []BuildInputItem) ([]InputFailure, error) {
	var failures []InputFailure
	for _, in := range inputs {
		if in.Kind != "external-spec" {
			continue
		}
		var raw []byte
		if in.SpecContent != "" {
			raw = []byte(in.SpecContent)
		}
		if _, err := c.spec.CollectSpec(ctx, orgID, projectID, in.Component, in.Dependency, raw, in.SpecURL); err != nil {
			failures = append(failures, InputFailure{
				Component:  in.Component,
				Dependency: in.Dependency,
				Kind:       in.Kind,
				Reason:     err.Error(),
			})
		}
	}
	if len(failures) > 0 {
		return failures, nil
	}
	if err := c.auth.DerivePlatformResourceFactsAtHead(ctx, orgID, projectID); err != nil {
		return failures, err
	}
	// Refresh `.claude/skills/` last, so the tag this build is about to cut
	// captures the guidance the build was designed against. Best-effort and
	// deliberately NOT propagated: stale skill copies degrade a build, whereas
	// returning here would abort one over a mirror that the next dispatch's
	// diff-first refresh repairs anyway.
	if c.skills != nil {
		if err := c.skills.SyncProjectSkills(ctx, orgID, projectID); err != nil {
			slog.WarnContext(ctx, "skills: pre-tag mirror refresh failed; tagging with the copies already in the repo",
				"org", orgID, "project", projectID, "error", err)
		}
	}
	return failures, nil
}

// BuildProvisionInputs derives one unset external-config payload per external
// dependency from the design's union schema. Request external-config entries are
// ignored: builds no longer collect or stage user values. Platform-resource and
// org-service inputs still pass through; external-spec is handled pre-tag.
func (c *InputsCoordinator) BuildProvisionInputs(ctx context.Context, orgID, projectID string, inputs []BuildInputItem) ([]delivery.ProvisionInput, []InputFailure, error) {
	if c.design == nil {
		return nil, nil, fmt.Errorf("build inputs: design reader is not configured")
	}
	comps, err := c.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, nil, err
	}

	var (
		out      []delivery.ProvisionInput
		failures []InputFailure
	)
	union := spec.UnionExternalConfigKeys(comps)
	unionByName := make(map[string][]spec.ConfigKey, len(union))
	for name, keys := range union {
		unionByName[strings.ToLower(name)] = keys
	}
	seenExternal := map[string]bool{}
	for _, component := range comps {
		for _, dep := range component.Dependencies {
			if dep.Kind != spec.DependencyKindExternal {
				continue
			}
			nameKey := strings.ToLower(dep.Name)
			if seenExternal[nameKey] {
				continue
			}
			seenExternal[nameKey] = true
			config := map[string]string{}
			keys := unionByName[nameKey]
			for _, key := range keys {
				if !key.Secret {
					config[key.Key] = key.DefaultValue
				}
			}
			out = append(out, delivery.ProvisionInput{
				Component:  component.Name,
				Dependency: dep.Name,
				Kind:       "external-config",
				Config:     config,
			})
		}
	}
	for _, in := range inputs {
		switch in.Kind {
		case "external-config":
			// Ignored. The design-derived entry above is authoritative.
		case "platform-resource", "org-service":
			out = append(out, delivery.ProvisionInput{
				Component:  in.Component,
				Dependency: in.Dependency,
				Kind:       in.Kind,
				Parameters: in.Parameters,
				Approved:   in.Approved,
			})
		case "external-spec":
			// Collected in ApplyPreTag; no provision payload.
		}
	}
	return out, failures, nil
}
