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

package validation

import (
	"context"
	"errors"
	"fmt"
)

// ErrCycleNotFound means the runner's cycle id does not resolve to a dispatched
// cycle in the caller's org — the endpoint surfaces it as 404. A cycle belonging
// to another org resolves the same way, so a cross-tenant probe cannot tell "not
// yours" from "does not exist".
var ErrCycleNotFound = errors.New("validation: cycle not found for org")

// ComponentEndpoint is one deployed component's reachable URL, the runner's
// e2e target.
type ComponentEndpoint struct {
	Component string `json:"component"`
	URL       string `json:"url"`
}

// ValidationContextResponse is the secure runtime-inputs payload the runner
// fetches at dispatch time (never carried in the public issue): the deployed
// endpoint URLs and the criteria file path.
//
// No test account rides here, not even a username. The build publishes the whole
// roster — logins included — on the roles gate ticket, and the agent reads it
// there (ADR-0022). A copy in this payload would land on disk for the run's
// lifetime and could disagree with the ticket.
type ValidationContextResponse struct {
	Endpoints    []ComponentEndpoint `json:"endpoints"`
	CriteriaPath string              `json:"criteriaPath"`
}

// CycleLocator resolves a runner's CYCLE id to its project, fenced by the
// caller's org (the INT-6 tenant fence). delivery.RunCycleRepository's
// GetByIDScoped satisfies the adapter wired at the composition root.
//
// The cycle id is what a dispatched pod carries (AEP_TASK_ID) and what its bearer
// token is bound to, so it is the only identity a runner callback can present.
// It used to be resolved against the executions table, which the milestone
// supervisor does not write — so every validation runner was told its own
// dispatch did not exist.
type CycleLocator interface {
	LookupCycleProject(ctx context.Context, orgHandle, cycleID string) (projectID string, found bool, err error)
}

// EndpointResolver resolves a project's deployed component endpoint URLs (first
// external URL per component, from OpenChoreo ReleaseBindings). The composition
// root adapts the design-component read + ComponentService.ListDeployments so
// this feature needs neither the artifacts nor the component edge.
type EndpointResolver interface {
	ResolveEndpoints(ctx context.Context, orgHandle, projectID string) ([]ComponentEndpoint, error)
}

// ContextService answers the runner's validation-context fetch.
type ContextService struct {
	cycles    CycleLocator
	endpoints EndpointResolver
}

// NewContextService wires the validation-context service.
func NewContextService(cycles CycleLocator, endpoints EndpointResolver) *ContextService {
	return &ContextService{cycles: cycles, endpoints: endpoints}
}

// ValidationContext resolves the runtime inputs for a runner's validation cycle:
// the deployed endpoint URLs. orgHandle is the verified caller org (the auth layer
// fences it against the cycle).
//
// Identity FIRST, then endpoints — and note that a failure here never reaches the
// endpoint resolution below, so an unreachable deployment and an unresolvable
// runner are entirely different failures with entirely different fixes.
func (s *ContextService) ValidationContext(ctx context.Context, cycleID, orgHandle string) (*ValidationContextResponse, error) {
	projectID, found, err := s.cycles.LookupCycleProject(ctx, orgHandle, cycleID)
	if err != nil {
		return nil, fmt.Errorf("validation context: resolve cycle: %w", err)
	}
	if !found {
		return nil, ErrCycleNotFound
	}
	eps, err := s.endpoints.ResolveEndpoints(ctx, orgHandle, projectID)
	if err != nil {
		return nil, fmt.Errorf("validation context: resolve endpoints: %w", err)
	}
	return &ValidationContextResponse{
		Endpoints:    eps,
		CriteriaPath: criteriaFilePath,
	}, nil
}
