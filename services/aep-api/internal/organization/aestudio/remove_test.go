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

package aestudio

import (
	"slices"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
)

// The gitpat disconnect: the org's Resource ae-studio is deleted (its
// binding, release and pod go with it in OpenChoreo), as aep-api's own
// identity. Nothing else is touched: the Project ae-system and the
// ResourceType stay (Cloud cannot delete a ResourceType), and a
// reconnect converges a new Resource.
func TestRemove_DeletesOnlyTheResource(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	f.oc.resetCalls()
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if want := []string{"DELETE resource ae-studio"}; !slices.Equal(f.oc.calls, want) {
		t.Fatalf("calls %v, want %v", f.oc.calls, want)
	}
	if f.oc.res != nil || f.oc.rrb != nil || f.oc.rt == nil || !f.oc.project {
		t.Fatalf("after Remove: resource=%v binding=%v resourcetype=%v project=%v", f.oc.res != nil, f.oc.rrb != nil, f.oc.rt != nil, f.oc.project)
	}
	if len(f.oc.violations) != 0 {
		t.Fatalf("violations: %v", f.oc.violations)
	}
	// Already gone is the state Remove exists to reach.
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("second Remove: %v", err)
	}
}

// C-1: after the disconnect cascade (Remove, the gitpat rows gone, the
// credential disconnected, Release) no Status read re-creates the pod.
func TestRemove_AfterTheDisconnectStatusIsAbsentAndConvergesNothing(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	// While held (the cascade still running, the rows still there), a
	// Status that sees the Resource gone starts nothing.
	if _, err := f.svc.Status(userCtx(), "default"); err != nil {
		t.Fatal(err)
	}
	f.waitIdle(t)
	f.withoutRef(organization.OrgSecretGitHubPAT).withoutRef(organization.OrgSecretGitHubWebhookSecret)
	f.org.mu.Lock()
	f.org.disconnected = true
	f.org.mu.Unlock()
	f.svc.Release("default")

	st, err := f.svc.Status(userCtx(), "default")
	if err != nil || st.State != StateAbsent {
		t.Fatalf("Status = %+v, %v; want absent", st, err)
	}
	f.waitIdle(t)
	if n := f.oc.count("POST resource ae-studio"); n != 0 || f.oc.res != nil {
		t.Fatalf("the Resource came back: posts=%d calls=%v", n, f.oc.calls)
	}
}

// Defence in depth: a credential that is not active is no GitHub connection,
// even with the gitpat's row still recorded (a half-run disconnect).
func TestDesired_InactiveCredentialIsAbsent(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.org.mu.Lock()
	f.org.disconnected = true
	f.org.mu.Unlock()
	st, err := f.svc.Status(userCtx(), "default")
	if err != nil || st.State != StateAbsent {
		t.Fatalf("Status = %+v, %v; want absent", st, err)
	}
	f.waitIdle(t)
	if f.oc.writes() != 0 {
		t.Fatalf("an inactive credential converged: %v", f.oc.calls)
	}
}

// Remove waits for a converge already running (a save just before the
// disconnect), so that converge cannot re-create the Resource after the
// delete; while held, Trigger starts nothing.
func TestRemove_WaitsForTheRunningConvergeAndHoldsTheOrg(t *testing.T) {
	f := newFixture(t).withAllRefs().slowOC(20 * time.Millisecond)
	f.svc.Trigger(userCtx(), "default")
	if err := f.svc.Remove(userCtx(), "default"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if f.svc.busy("default") {
		t.Fatal("Remove returned while a converge was running")
	}
	f.svc.Trigger(userCtx(), "default")
	if f.svc.busy("default") {
		t.Fatal("a held org started a converge")
	}
	if f.oc.res != nil {
		t.Fatal("the Resource survived Remove")
	}
	f.svc.Release("default")
}
