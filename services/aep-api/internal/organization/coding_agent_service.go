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

// coding_agent_service.go — the org's coding-agent runtime and model.
//
// The odd one out among the /config sections. The other three are credential
// -shaped: a write-only secret, an external probe before anything is persisted,
// and a projection that cannot echo what was written. This one is a plain pair
// of enum values, which changes two things:
//
//   - There is NOTHING TO PROBE, so the whole of `Patch`'s probe phase is local
//     validation. That is not a weaker guarantee, it is a different one: the
//     values either name something this build can run or they do not, and the
//     answer does not depend on a third party being up.
//   - A change here does not take effect now. Dispatch COPIES the setting onto
//     the run it starts (`AEP_AGENT_RUNTIME` / `AEP_AGENT_MODEL`), so a run
//     already in flight keeps what it was launched with and the next cycle picks
//     the new values up. Reading the setting mid-run would leave a feed whose
//     model names disagree with the tokens they were billed for.

package organization

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// CodingAgentService reads and writes the org's coding-agent setting.
type CodingAgentService struct {
	repo OrgCodingAgentRepository
	now  func() time.Time
}

// NewCodingAgentService wires the service. `now` is injected by tests only.
func NewCodingAgentService(repo OrgCodingAgentRepository) *CodingAgentService {
	return &CodingAgentService{repo: repo, now: time.Now}
}

// WithClock replaces the clock. Test seam; production keeps time.Now.
func (s *CodingAgentService) WithClock(now func() time.Time) *CodingAgentService {
	s.now = now
	return s
}

// Effective returns the runtime and model this org's next cycle will run with.
//
// Never fails over to nothing: an org with no row is on the platform defaults,
// which is a real answer and the one every org starts with. The projection's
// UpdatedBy is what distinguishes the two, so a caller that needs to know
// whether anybody chose has the fact rather than a guess.
func (s *CodingAgentService) Effective(ctx context.Context, ocOrgID string) (orgconfig.CodingAgentProjection, error) {
	row, err := s.repo.GetByOrg(ctx, ocOrgID)
	if err != nil {
		return orgconfig.CodingAgentProjection{}, fmt.Errorf("coding agent setting: %w", err)
	}
	if row == nil {
		return orgconfig.DefaultCodingAgent(), nil
	}
	updatedAt := row.UpdatedAt
	updatedBy := row.UpdatedBy
	return orgconfig.CodingAgentProjection{
		Runtime:   row.Runtime,
		Model:     row.Model,
		UpdatedAt: &updatedAt,
		UpdatedBy: &updatedBy,
	}, nil
}

// Validate answers whether a write would be accepted, WITHOUT writing.
//
// Split out because `Patch` promises never to half-apply: every section is
// checked before any section is persisted, and this is this section's share of
// that promise. It resolves the patch against the org's current setting first,
// because a patch naming only a model must be judged with the runtime the org
// actually has — not with the default it may have moved off.
func (s *CodingAgentService) Validate(ctx context.Context, ocOrgID string, w orgconfig.CodingAgentWrite) error {
	_, err := s.resolve(ctx, ocOrgID, w)
	return err
}

// Set persists the org's choice, resolving an omitted field against what the
// org already has.
func (s *CodingAgentService) Set(ctx context.Context, ocOrgID, actor string, w orgconfig.CodingAgentWrite) error {
	resolved, err := s.resolve(ctx, ocOrgID, w)
	if err != nil {
		return err
	}
	row := &OrgCodingAgentSetting{
		OcOrgID:   ocOrgID,
		Runtime:   resolved.Runtime,
		Model:     resolved.Model,
		UpdatedBy: actor,
		UpdatedAt: s.now().UTC(),
	}
	if err := s.repo.Upsert(ctx, row); err != nil {
		return fmt.Errorf("coding agent setting: %w", err)
	}
	return nil
}

// Reset puts the org back on the platform defaults by removing its row.
//
// Deleting rather than writing the default values, so `Effective` keeps telling
// "on the defaults" apart from "chose the defaults" — see the entity.
func (s *CodingAgentService) Reset(ctx context.Context, ocOrgID string) error {
	if err := s.repo.DeleteByOrg(ctx, ocOrgID); err != nil {
		return fmt.Errorf("coding agent setting: %w", err)
	}
	return nil
}

// resolve fills an omitted field from the org's current setting and validates
// the result. One function, so a check can never be applied on one path and
// forgotten on the other — which is exactly how a validate/persist pair usually
// drifts.
func (s *CodingAgentService) resolve(
	ctx context.Context,
	ocOrgID string,
	w orgconfig.CodingAgentWrite,
) (orgconfig.CodingAgentProjection, error) {
	current, err := s.Effective(ctx, ocOrgID)
	if err != nil {
		return orgconfig.CodingAgentProjection{}, err
	}
	out := orgconfig.CodingAgentProjection{Runtime: current.Runtime, Model: current.Model}
	if runtime := strings.TrimSpace(w.Runtime); runtime != "" {
		if err := validateRuntime(runtime); err != nil {
			return orgconfig.CodingAgentProjection{}, err
		}
		out.Runtime = runtime
	}
	if model := strings.TrimSpace(w.Model); model != "" {
		if !slices.Contains(orgconfig.CodingAgentModels, model) {
			return orgconfig.CodingAgentProjection{}, &ValidationError{
				Code: "coding_agent_model_unknown",
				Message: fmt.Sprintf("model %q is not one this platform offers (%s)",
					model, strings.Join(orgconfig.CodingAgentModels, ", ")),
			}
		}
		out.Model = model
	}
	return out, nil
}

// validateRuntime separates the two failures a runtime name can have, because
// they mean different things to whoever chose it.
//
// A name outside the enum is a client that made something up. A name IN the enum
// that this build cannot run is the platform's own gap, and the message says so
// — the alternative, silently running the one runtime we do have, would bill an
// organization for a runtime it did not choose and never tell it. The runner
// refuses the same name for the same reason (`runtime/registry.ts`); this is
// that refusal moved to the moment of choosing, so nobody waits three hours for
// a pod to explain it.
func validateRuntime(runtime string) error {
	if !slices.Contains(orgconfig.AgentRuntimes, runtime) {
		return &ValidationError{
			Code: "coding_agent_runtime_unknown",
			Message: fmt.Sprintf("runtime %q does not exist (%s)",
				runtime, strings.Join(orgconfig.AgentRuntimes, ", ")),
		}
	}
	if !slices.Contains(orgconfig.SupportedAgentRuntimes, runtime) {
		return &ValidationError{
			Code: "coding_agent_runtime_unavailable",
			Message: fmt.Sprintf(
				"this platform ships no %s adapter yet, so it cannot be selected — the only runtime it can run is %s",
				runtime, strings.Join(orgconfig.SupportedAgentRuntimes, ", ")),
		}
	}
	return nil
}
