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

package edge

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// designDependencyReader is the edge's narrow consumer port over the spec
// domain: read every component's dependencies in the current design, with the
// read-time computed status/reason. The spec domain deliberately exports no
// service interface (internal/spec/design_service.go: "every caller holds the
// concrete type") — mirroring how spec itself declares narrow consumer ports
// for ITS OWN collaborators, this port is declared consumer-side instead;
// *spec.designService (spec.NewDesignService's return value) satisfies it
// structurally.
type designDependencyReader interface {
	ListDependencies(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error)
}

// ListDesignDependencies is the spec domain's read-only dependency-status
// surface: every component's dependencies in the current design, with the
// read-time computed status/reason (ReadDesign → AssembleDesignFrom already
// ran spec.ComputeDependencyStatus per dependency) plus the stored intent
// fields — the single read model the console's dependency-status views poll.
// A nil service answers 503 (the spec domain is unwired), mirroring every
// other feature's nil-guard.
func (s *apiServer) ListDesignDependencies(ctx context.Context, request gen.ListDesignDependenciesRequestObject) (gen.ListDesignDependenciesResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	if s.designSvc == nil {
		return nil, errServiceUnavailable("design service is not configured")
	}
	components, err := s.designSvc.ListDependencies(ctx, org, request.ProjectName)
	if err != nil {
		if errors.Is(err, spec.ErrDesignNotFound) {
			return nil, errNotFound("design not found")
		}
		return nil, errInternal("failed to read design dependencies")
	}
	out := make([]gen.ComponentDependencies, 0, len(components))
	for _, c := range components {
		deps := c.Dependencies
		if deps == nil {
			deps = []spec.Dependency{}
		}
		deps = withAgentToolOperations(deps, c.AgentToolStatuses)
		out = append(out, gen.ComponentDependencies{
			ComponentName: c.Name,
			Dependencies:  deps,
		})
	}
	return gen.ListDesignDependencies200JSONResponse(out), nil
}

// withAgentToolOperations attaches each dependency's read-time computed
// agent-tool operation resolution (spec.ComputeAgentToolStatus, run at
// design-save — see deriveAgentToolStatuses) onto the matching Dependency's
// new Operations field. statuses is only non-empty on an ai-agent component
// (the only ComponentType deriveAgentToolStatuses ever populates); every
// other component's deps pass through untouched. Grouped by
// AgentToolStatus.Component — the PROVIDER component name, which is exactly
// the `component` dependency's own Name within this same component's
// Dependencies list, since ComputeAgentToolStatus rule 1 already requires it
// to be one for a resolved/unresolved (non-unchecked) result to exist at all.
// Returns a new slice; the input is never mutated in place.
func withAgentToolOperations(deps []spec.Dependency, statuses []spec.AgentToolStatus) []spec.Dependency {
	if len(statuses) == 0 {
		return deps
	}
	byProvider := make(map[string][]gen.DependencyOperationStatus, len(statuses))
	for _, st := range statuses {
		byProvider[st.Component] = append(byProvider[st.Component], gen.DependencyOperationStatus{
			Operation: st.Operation,
			Status:    st.Status,
			Reason:    st.Reason,
		})
	}
	out := make([]spec.Dependency, len(deps))
	for i, d := range deps {
		if ops, ok := byProvider[d.Name]; ok {
			d.Operations = ops
		}
		out[i] = d
	}
	return out
}
