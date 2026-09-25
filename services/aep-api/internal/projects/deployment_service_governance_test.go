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

package projects

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

type stubGovernor struct {
	seen []delivery.GovernAgentInput
	err  error
	out  delivery.GovernAgentOutcome
}

func (s *stubGovernor) GovernAgent(_ context.Context, in delivery.GovernAgentInput) (delivery.GovernAgentOutcome, error) {
	s.seen = append(s.seen, in)
	return s.out, s.err
}

// Governance belongs to DEPLOY, not to the caller that asked for one.
//
// The workflow's promote is only one of three doors into Deploy — Converge's
// drift repair and a config change's redeploy are the others — and an earlier
// version of this hung governance off promote alone, which left those two
// deploying agents with no Agent Manager record and no gateway credential.
func TestSetGovernor_WiresGovernanceIntoDeployItself(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(nil, nil)
	if svc.governor != nil {
		t.Fatal("governor should start nil")
	}
	g := &stubGovernor{}
	svc.SetGovernor(g)
	if svc.governor == nil {
		t.Fatal("SetGovernor did not wire the governor")
	}
}

// A governance failure REFUSES the deploy. The environment's binding promised
// that its agents are governed; deploying anyway would produce an agent running
// on the org's raw key while Agent Manager believes it is governed.
func TestGovern_FailureRefusesTheDeploy(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(nil, nil)
	svc.SetGovernor(&stubGovernor{err: errors.New("amp unreachable")})

	err := svc.govern(context.Background(), "acme", "shop", []delivery.DeployTarget{
		{Component: "checkout-agent"},
	})
	if err == nil {
		t.Fatal("want an error: a deploy must not proceed when governance fails")
	}
}

// Every target is offered to the governor, which decides what is an agent.
// Splitting that judgement across two packages is how the two drift.
func TestGovern_OffersEveryTargetAndCarriesTheEnvironment(t *testing.T) {
	t.Parallel()
	g := &stubGovernor{out: delivery.GovernAgentOutcome{Skipped: true, Reason: "not an ai-agent"}}
	svc := NewDeploymentService(nil, nil)
	svc.SetGovernor(g)

	if err := svc.govern(context.Background(), "acme", "shop", []delivery.DeployTarget{
		{Component: "checkout-agent"}, {Component: "catalog-api"},
	}); err != nil {
		t.Fatalf("govern: %v", err)
	}
	if len(g.seen) != 2 {
		t.Fatalf("governor saw %d targets, want 2", len(g.seen))
	}
	for _, in := range g.seen {
		if in.OrgID != "acme" || in.ProjectID != "shop" {
			t.Errorf("input lost its coordinates: %+v", in)
		}
		// The environment the deploy writes into. An agent's key is issued
		// against a (provider, environment) pair, so governing it for another
		// environment would mint a credential it never uses.
		if in.Environment == "" {
			t.Errorf("input carries no environment: %+v", in)
		}
	}
}

// Unwired is a no-op, matching every other optional collaborator here: a
// deployment with no Agent Manager deploys exactly as it did before.
func TestGovern_UnwiredIsANoOp(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(nil, nil)
	if err := svc.govern(context.Background(), "acme", "shop", []delivery.DeployTarget{
		{Component: "checkout-agent"},
	}); err != nil {
		t.Fatalf("unwired governor must be a no-op: %v", err)
	}
}
