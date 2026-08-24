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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// fakeProviderBuild spies the provider-build trigger (the automated visibility
// flow's best-effort kick).
type fakeProviderBuild struct {
	calls   int
	lastOrg string
	lastPrj string
	err     error
}

func (f *fakeProviderBuild) TriggerBuild(_ context.Context, orgID, projectID string) error {
	f.calls++
	f.lastOrg, f.lastPrj = orgID, projectID
	return f.err
}

// visibilitySpies counts the observable side effects of StartOrgServiceVisibility.
type visibilitySpies struct {
	issues *fakeIssues
	access *fakeAccess
	build  *fakeProviderBuild
}

// A gate issue no longer declares its dependency kind: it is prose with the `provision`
// marker and its aep:dep/<slug> label, and no platform code branches on which
// flavour of gate it is (resolving one is always the same act). The two
// flavours are told apart here by the platform-authored TITLE — which is what a
// human reading the milestone sees too.
const (
	visibilityGateTitlePrefix = "Awaiting org-service"
	orgPublishGateTitlePrefix = "Publish "
)

// consumerGateMinted counts minted org-service-visibility gate issues.
func (s visibilitySpies) consumerGateMinted() int {
	n := 0
	for _, req := range s.issues.created {
		if strings.HasPrefix(req.Title, visibilityGateTitlePrefix) {
			n++
		}
	}
	return n
}

// providerTaskCreated counts minted provider-side org-publish gate issues.
func (s visibilitySpies) providerTaskCreated() int {
	n := 0
	for _, req := range s.issues.created {
		if strings.HasPrefix(req.Title, orgPublishGateTitlePrefix) {
			n++
		}
	}
	return n
}

// newVisibilityService wires a provisioning service for the automated
// org-service visibility flow: consumer "shop" component "cart" depends on
// org-service "billing", provided by project "payments" component "payments-billing".
func newVisibilityService(t *testing.T) (*Service, visibilitySpies) {
	t.Helper()
	issues := newFakeIssues(nil)
	access := &fakeAccess{}
	build := &fakeProviderBuild{}
	consumer := spec.DesignComponent{Name: "cart", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindOrgService, Name: "billing"},
	}}
	svc := NewService(Deps{
		Issues: issues,
		Execs:  &fakeExecStore{},
		Design: fakeDesign{comps: []spec.DesignComponent{consumer}},
		Repos:  fakeRepos{},
		Access: access,
		Providers: fakeProviders{byName: map[string]openchoreo.WorkloadEndpointInfo{
			"billing": {Project: "payments", Component: "payments-billing", Name: "http"},
		}},
	})
	svc.SetProviderBuildTrigger(build)
	return svc, visibilitySpies{issues: issues, access: access, build: build}
}

func TestStartOrgServiceVisibility_CreatesTasksAndTriggersBuild(t *testing.T) {
	svc, spies := newVisibilityService(t)
	ctx := context.Background()

	if err := svc.StartOrgServiceVisibility(ctx, "acme", "shop", "billing"); err != nil {
		t.Fatalf("StartOrgServiceVisibility: %v", err)
	}
	if got := spies.consumerGateMinted(); got != 1 {
		t.Fatalf("want 1 consumer visibility gate minted, got %d", got)
	}
	if got := spies.providerTaskCreated(); got != 1 {
		t.Fatalf("want 1 provider org-publish issue, got %d", got)
	}
	if spies.build.calls != 1 {
		t.Fatalf("want provider build triggered once, got %d", spies.build.calls)
	}
	if spies.build.lastPrj != "payments" {
		t.Fatalf("provider build must target the provider project, got %q", spies.build.lastPrj)
	}
	// The access request records the consumer component that declares the dep.
	if len(spies.access.rows) != 1 || spies.access.rows[0].ConsumerComponentName != "cart" {
		t.Fatalf("access request must record consumer component 'cart', got %+v", spies.access.rows)
	}
	if spies.access.rows[0].OrgServiceName != "billing" || spies.access.rows[0].ProviderProjectID != "payments" {
		t.Fatalf("access request provider resolution wrong: %+v", spies.access.rows[0])
	}
}

// Idempotency: re-clicking Build reuses the open provider issue AND does not
// re-mint the consumer gate; the build trigger is called again (itself idempotent
// in the adapter).
func TestStartOrgServiceVisibility_Idempotent(t *testing.T) {
	svc, spies := newVisibilityService(t)
	ctx := context.Background()

	if err := svc.StartOrgServiceVisibility(ctx, "acme", "shop", "billing"); err != nil {
		t.Fatalf("StartOrgServiceVisibility #1: %v", err)
	}
	if err := svc.StartOrgServiceVisibility(ctx, "acme", "shop", "billing"); err != nil {
		t.Fatalf("StartOrgServiceVisibility #2: %v", err)
	}
	if got := spies.consumerGateMinted(); got != 1 {
		t.Fatalf("second call must not re-mint the consumer gate, got %d total", got)
	}
	if got := spies.providerTaskCreated(); got != 1 {
		t.Fatalf("second call must ride the existing provider issue, got %d total", got)
	}
	// One provider org-publish issue backs both access-request riders.
	if len(spies.access.rows) != 2 {
		t.Fatalf("want 2 access-request rows (one per call), got %d", len(spies.access.rows))
	}
	if spies.access.rows[0].ProviderIssueNumber != spies.access.rows[1].ProviderIssueNumber {
		t.Fatalf("both riders must reference the same provider issue")
	}
}

// The grant cascade completes a provision run on the consumer visibility gate so
// it derives deployed and closes the gate.
func TestGrantByProviderComponent_ResolvesConsumerVisibilityGate(t *testing.T) {
	svc, spies := newVisibilityService(t)
	ctx := context.Background()

	// Build kicks off the flow: mints the consumer gate + provider issue + records
	// the access request.
	if err := svc.StartOrgServiceVisibility(ctx, "acme", "shop", "billing"); err != nil {
		t.Fatalf("StartOrgServiceVisibility: %v", err)
	}
	execs := svc.execs.(*fakeExecStore)

	// The consumer gate issue number (the org-service-visibility gate).
	var consumerGate int
	for _, i := range spies.issues.list {
		if strings.HasPrefix(i.Title, visibilityGateTitlePrefix) {
			consumerGate = i.Number
		}
	}
	if consumerGate == 0 {
		t.Fatalf("consumer visibility gate not found")
	}

	// The provider deploys → grant cascade resolves the consumer gate.
	ar := spies.access.rows[0]
	if err := svc.GrantByProviderComponent(ctx, "acme", ar.ProviderProjectID, ar.ProviderComponentName); err != nil {
		t.Fatalf("GrantByProviderComponent: %v", err)
	}
	// A succeeded provision run now backs the consumer gate (derives deployed).
	r := latestProvisionRow(execs)
	if r == nil || r.Status != string(taskmeta.ExecSucceeded) {
		t.Fatalf("consumer visibility gate must have a succeeded provision run, got %+v", r)
	}
	if r.Component != "billing" {
		t.Fatalf("provision run must be keyed by the org-service dep name, got %q", r.Component)
	}
	// The gate issue closed (held consumer dispatches via gate-close webhook + sweep).
	if _, closed := spies.issues.closed[consumerGate]; !closed {
		t.Fatalf("consumer visibility gate must be closed on grant")
	}
	// The rider flipped to granted.
	if spies.access.rows[0].Status != dependencies.AccessRequestStatusGranted {
		t.Fatalf("rider must be granted, got %q", spies.access.rows[0].Status)
	}
}
