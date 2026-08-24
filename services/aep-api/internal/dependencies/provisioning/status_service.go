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
		return defaultEnv
	}
	return env
}

// ConfigurationReadiness returns the whole project's external dependency value
// state for one environment. The design is authoritative: bindings are only
// looked up for the union keys still declared there.
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

func externalValueState(binding *openchoreo.ResourceReleaseBinding, keys []spec.ConfigKey) (ExternalDependencyValueState, []string, error) {
	missing := make([]string, 0, len(keys))
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
