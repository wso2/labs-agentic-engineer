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
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// THE MINT IS IDEMPOTENT ACROSS A GATE IT ALREADY CLOSED, which is the property
// its callers always assumed and did not have.
//
// A live incident is the specification here. ProvisionGates retried under
// Temporal's unbounded default because one dependency named a
// ClusterResourceType nobody had installed. Each attempt minted the version's
// gates, settled the ones whose resources were already Ready — closing them —
// and failed on the unsatisfiable one. The next attempt then saw no OPEN gate for
// any dependency and minted them all again: 33 issues in 22 minutes, three per
// attempt, into one milestone.
//
// Both narrowings were state-based. The Go-side lookup skipped anything not
// open, and CreateIssue's DedupeKey is resolved host-side against open issues
// too. The version LABEL is what lets the lookup widen to every state without
// breaking the next version (see gateVersionLabelPrefix).
func TestEnsureProvisionIssues_AClosedGateForThisVersionSuppressesAReMint(t *testing.T) {
	issues := newFakeIssues(nil)
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	first, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v1", 7)
	if err != nil {
		t.Fatalf("first mint: %v", err)
	}
	gate := first["orders-db"]
	if gate == 0 {
		t.Fatal("setup: the first mint filed no gate for orders-db")
	}

	// What the activity itself does to a dependency that is already Ready.
	if cerr := issues.CloseIssue(context.Background(), "org", "proj", gate, "provisioned"); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	issues.created = nil
	second, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v1", 7)
	if err != nil {
		t.Fatalf("second mint: %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("a retry must file nothing over a gate this version already has, filed %d", len(issues.created))
	}
	// And it is NOT reported as a live hold. A closed gate holds no dispatch, so
	// threading its number into provisioning would admit a provision run against
	// an issue nothing derives from.
	if second["orders-db"] != 0 {
		t.Errorf("a CLOSED gate must not be returned as a hold, got %d", second["orders-db"])
	}
}

// THE REGRESSION GUARD, and the reason the version label exists at all.
//
// Widening the lookup to every state without it would match the closed gate the
// PREVIOUS version left behind — supersede closes them — so v2's build would
// decline to mint its own gates and its run would wait on a hold that does not
// exist. Every version re-derives its own gates; the identity is (version,
// dependency), never dependency alone.
func TestEnsureProvisionIssues_APreviousVersionsClosedGateDoesNotSuppressThisOne(t *testing.T) {
	issues := newFakeIssues(nil)
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	v1, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v1", 7)
	if err != nil {
		t.Fatalf("v1 mint: %v", err)
	}
	if cerr := issues.CloseIssue(context.Background(), "org", "proj", v1["orders-db"], "superseded"); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}

	issues.created = nil
	v2, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v2", 8)
	if err != nil {
		t.Fatalf("v2 mint: %v", err)
	}
	if len(issues.created) != 1 {
		t.Fatalf("v2 must mint its own gate, filed %d", len(issues.created))
	}
	if v2["orders-db"] == 0 || v2["orders-db"] == v1["orders-db"] {
		t.Errorf("v2's gate must be a NEW open gate, got %d (v1 had %d)", v2["orders-db"], v1["orders-db"])
	}
	if !gateIsForVersion(issues.created[0].Labels, "v2") {
		t.Errorf("a per-version gate must carry its version label: %v", issues.created[0].Labels)
	}
}

// An OPEN gate still suppresses the mint whatever version filed it, and whether
// or not it carries a version label at all.
//
// That is the pre-label behaviour, kept deliberately: it is what makes a gate
// minted before this label existed still work, and a live hold is a live hold
// regardless of which version put it there.
func TestEnsureProvisionIssues_AnUnlabelledOpenGateStillSuppressesTheMint(t *testing.T) {
	// A gate as an older binary would have filed it: kind + dep label, no version.
	issues := newFakeIssues([]sourcecontrol.IssueInfo{{
		Number: 41,
		Title:  "Provision resource: orders-db",
		State:  "open",
		Labels: []string{delivery.KindProvision, gateDepLabel("orders-db")},
	}})
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	got, err := svc.EnsureProvisionIssues(context.Background(), "org", "proj", "v9", 7)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("an open gate is a live hold — nothing should be filed over it, filed %d", len(issues.created))
	}
	if got["orders-db"] != 41 {
		t.Errorf("the existing open gate must be reported as the hold, got %d", got["orders-db"])
	}
}
