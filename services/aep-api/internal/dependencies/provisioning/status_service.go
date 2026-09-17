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

package provisioning

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

type ExternalDependencyValueState string

const (
	ValueStateNotProvisioned ExternalDependencyValueState = "not-provisioned"
	ValueStateUnset          ExternalDependencyValueState = "unset"
	ValueStateConfigured     ExternalDependencyValueState = "configured"
)

// DependencyStatus is the masked provisioning status of a dependency: the
// derived state, a readiness flag, and the output NAMES only. Output values +
// secret references are never surfaced — secrets live only in SM-API / the
// OC-rendered Secret (the no-secrets invariant).
type DependencyStatus struct {
	Status  string   `json:"status"`  // provisioning | ready | unknown
	Ready   bool     `json:"ready"`   //
	Outputs []string `json:"outputs"` // output names only (masked)
	// ValueState is set for external dependencies only.
	ValueState ExternalDependencyValueState `json:"valueState,omitempty"`
}

type ExternalDependencyReadiness struct {
	Name        string
	State       ExternalDependencyValueState
	MissingKeys []string
}

type ProjectDependencyReadiness struct {
	Configured   bool
	Dependencies []ExternalDependencyReadiness
}

// DeploymentReadiness keeps the two deploy-gate blockers separate: external
// configuration is user work, while platform-resource provisioning is work the
// platform is still completing. The run treats them differently — it parks on
// the first and polls the second — so collapsing them into one list would force
// the supervisor to guess which of the two it is looking at.
type DeploymentReadiness struct {
	Unconfigured []string
	Provisioning []string
}

// Status reads the dependency's per-env OC binding and reports its readiness +
// output names. External and platform-resource bindings share one naming form
// (ExternalResourceBindingName), so one read path serves both. A missing binding
// (not provisioned yet) reports "unknown", not an error — the status endpoint
// stays 200 mid-provision.
func (s *Service) Status(ctx context.Context, orgID, projectID, depName, env string) (*DependencyStatus, error) {
	out, binding, err := s.bindingStatus(ctx, orgID, projectID, depName, env)
	if err != nil {
		return nil, err
	}
	if s.design == nil {
		return out, nil
	}
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	if keys, external := spec.UnionExternalConfigFor(comps, depName); external {
		valueState, _, stateErr := externalValueState(binding, keys)
		if stateErr != nil {
			bindingName := ocname.ExternalResourceBindingName(projectID, depName, normalizedEnv(env))
			return nil, fmt.Errorf("provisioning: decode binding %q: %w", bindingName, stateErr)
		}
		out.ValueState = valueState
	}
	return out, nil
}

// bindingStatus reports only OpenChoreo binding state. Callers that already
// hold a design snapshot (notably platform gate settlement) use it to avoid a
// second design read and the resulting TOCTOU/error-masking path.
func (s *Service) bindingStatus(ctx context.Context, orgID, projectID, depName, env string) (*DependencyStatus, *openchoreo.ResourceReleaseBinding, error) {
	env = normalizedEnv(env)
	bindingName := ocname.ExternalResourceBindingName(projectID, depName, env)
	binding, err := s.bindings.GetBinding(ctx, orgID, bindingName)
	if err != nil {
		return nil, nil, fmt.Errorf("provisioning: read binding %q: %w", bindingName, err)
	}
	out := &DependencyStatus{Outputs: []string{}}
	if binding == nil {
		out.Status = "unknown"
		return out, nil, nil
	}
	out.Ready = binding.IsReady()
	if out.Ready {
		out.Status = "ready"
	} else {
		out.Status = "provisioning"
	}
	if binding.Status != nil {
		for _, o := range binding.Status.Outputs {
			out.Outputs = append(out.Outputs, o.Name) // MASKED: name only, never value/secretRef.
		}
	}
	return out, binding, nil
}

func normalizedEnv(env string) string {
	if env == "" {
		return defaultEnv()
	}
	return env
}

// ConfigurationReadiness returns the whole project's external dependency value
// state for one environment. The design is authoritative: bindings are only
// looked up for the union keys still declared there.
//
// It enumerates every external the PROJECT can supply, and only those — the same
// population DeploymentReadiness gates on, so the console's section and the
// deploy gate can never disagree about what is outstanding. Registered Externals
// are therefore omitted, not reported unconfigured: their values live on the org
// record, the project's own values endpoint refuses one with 409 "values live on
// the org record", and a row inviting a project member to supply one offers a
// button that cannot work and a headline that contradicts a gate which is not
// blocked. See DeploymentReadiness's header for the full reasoning and its cost.
//
// Configured therefore means "every external this project can supply is
// configured", which is exactly the question the builds page asks.
func (s *Service) ConfigurationReadiness(ctx context.Context, orgID, projectID, env string) (*ProjectDependencyReadiness, error) {
	env = normalizedEnv(env)
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	result := &ProjectDependencyReadiness{Configured: true, Dependencies: []ExternalDependencyReadiness{}}
	union := spec.UnionExternalConfigKeys(comps)
	names := make([]string, 0, len(union))
	for name := range union {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		// Org-held values: not this project's to supply. See the header.
		if s.HasOrgEnvCells(ctx, orgID, name) {
			continue
		}
		keys := union[name]
		bindingName := ocname.ExternalResourceBindingName(projectID, name, env)
		binding, berr := s.bindings.GetBinding(ctx, orgID, bindingName)
		if berr != nil {
			return nil, fmt.Errorf("provisioning: read binding %q: %w", bindingName, berr)
		}
		state, missing, derr := externalValueState(binding, keys)
		if derr != nil {
			return nil, fmt.Errorf("provisioning: decode binding %q: %w", bindingName, derr)
		}
		if state != ValueStateConfigured {
			result.Configured = false
		}
		result.Dependencies = append(result.Dependencies, ExternalDependencyReadiness{Name: name, State: state, MissingKeys: missing})
	}
	return result, nil
}

// DeploymentReadiness derives the whole project's deploy gate (ADR-0023) from
// one design snapshot: every external dependency configured, every platform
// resource provisioned. External dependencies are decoded against their union
// config schema; platform resources use the OpenChoreo binding's Ready
// condition.
//
// The design is authoritative for BOTH halves, for the reason ADR-0023 states:
// the resource pipeline does not prune, so a key — or a whole dependency —
// dropped from a design lingers on its binding forever. Reading the binding
// first would report a stale value as configuration.
//
// REGISTERED EXTERNALS ARE NOT COUNTED AS UNCONFIGURED, and the omission is the
// gate's most consequential judgement. A Registered External holds its values on
// the ORG record, not on the project binding: the project's own values endpoint
// refuses one outright (SaveValues answers 409 "values live on the org record"),
// and build authoring fills the project binding from the org value plane rather
// than from anything a project member typed. So naming one here would park the
// run on a blocker nobody looking at that project's builds page can clear —
// which, since the values park is unbounded and exits only on cancellation, is a
// permanent deadlock rather than a delay. It would also contradict the same
// project's own build preflight, which already suppresses the external-config
// item for a Registered name on exactly this reasoning (HasOrgEnvCells).
//
// The cost is stated plainly: an org-registered dependency whose org record is
// itself short a value deploys with an empty credential, and the gate does not
// catch it. That failure belongs to the org catalog surface, which is the only
// place it can be seen or fixed; a project-scoped park can express neither.
func (s *Service) DeploymentReadiness(ctx context.Context, orgID, projectID, env string) (*DeploymentReadiness, error) {
	env = normalizedEnv(env)
	comps, err := s.design.ReadDesignComponents(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("provisioning: read design: %w", err)
	}
	out := &DeploymentReadiness{Unconfigured: []string{}, Provisioning: []string{}}

	externals := spec.UnionExternalConfigKeys(comps)
	for _, name := range sortedKeys(externals) {
		// Org-held values: not this project's to supply. See the header.
		if s.HasOrgEnvCells(ctx, orgID, name) {
			continue
		}
		// Nothing declared, so nothing to configure and nothing that could ever
		// clear a park: the console renders no row for a dependency with an empty
		// schema, because a row exists to collect values. Blocking on one would
		// be a deadlock of exactly the Registered External's shape. Skipped
		// before the binding read, which the gate repeats every fifteen seconds
		// and which could not change the answer.
		if len(externals[name]) == 0 {
			continue
		}
		bindingName := ocname.ExternalResourceBindingName(projectID, name, env)
		binding, readErr := s.bindings.GetBinding(ctx, orgID, bindingName)
		if readErr != nil {
			return nil, fmt.Errorf("provisioning: read binding %q: %w", bindingName, readErr)
		}
		state, _, decodeErr := externalValueState(binding, externals[name])
		if decodeErr != nil {
			return nil, fmt.Errorf("provisioning: decode binding %q: %w", bindingName, decodeErr)
		}
		if state != ValueStateConfigured {
			out.Unconfigured = append(out.Unconfigured, name)
		}
	}

	platform := map[string]struct{}{}
	for _, comp := range comps {
		for _, dep := range comp.Dependencies {
			if dep.Kind == spec.DependencyKindPlatformResource {
				platform[dep.Name] = struct{}{}
			}
		}
	}
	for _, name := range sortedKeys(platform) {
		bindingName := ocname.ExternalResourceBindingName(projectID, name, env)
		binding, readErr := s.bindings.GetBinding(ctx, orgID, bindingName)
		if readErr != nil {
			return nil, fmt.Errorf("provisioning: read binding %q: %w", bindingName, readErr)
		}
		if binding == nil || !binding.IsReady() {
			out.Provisioning = append(out.Provisioning, name)
		}
	}
	return out, nil
}

// sortedKeys returns a map's keys in name order, so the gate reports the same
// list on every pass. The run writes that list onto the run row and the console
// renders it verbatim; an order that shuffled between polls would rewrite the
// row — and redraw the notice — on every pass for no change in fact.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// externalValueState answers ONE question — are the declared keys set? — and
// answers it from the design's key list, never from what the binding happens to
// carry (ADR-0023: the resource pipeline does not prune).
//
// An empty key set is `configured`, and deliberately so, including when no
// binding exists at all. The state names whether every DECLARED key holds a
// value, which is vacuously true when nothing is declared; there is no key a
// person could type to move such a dependency out of any other state, so any
// other answer is one no caller can ever act on. It is not a claim about the
// binding — whether that exists is DependencyStatus.Status, which stays a
// separate fact for the reason the ADR gives: `configured` and `ready` name
// different things and must not be merged.
func externalValueState(binding *openchoreo.ResourceReleaseBinding, keys []spec.ConfigKey) (ExternalDependencyValueState, []string, error) {
	missing := make([]string, 0, len(keys))
	if len(keys) == 0 {
		return ValueStateConfigured, missing, nil
	}
	if binding == nil {
		for _, key := range keys {
			missing = append(missing, key.Key)
		}
		return ValueStateNotProvisioned, missing, nil
	}
	values := map[string]string{}
	if len(binding.Spec.ResourceTypeEnvironmentConfigs) > 0 {
		if err := json.Unmarshal(binding.Spec.ResourceTypeEnvironmentConfigs, &values); err != nil {
			return "", nil, err
		}
	}
	for _, key := range keys {
		lookup := key.Key
		if key.Secret {
			lookup = openchoreo.SecretStorePathField
		}
		if values[lookup] == "" {
			missing = append(missing, key.Key)
		}
	}
	if len(missing) > 0 {
		return ValueStateUnset, missing, nil
	}
	return ValueStateConfigured, missing, nil
}
