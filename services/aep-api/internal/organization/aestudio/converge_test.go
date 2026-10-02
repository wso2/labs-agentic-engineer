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
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
)

func TestConverge_FreshOrgOrderAndPin(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	want := []string{"GET project ae-system", "POST project ae-system", "resolve write target", "PRB ae-system-development",
		"GET resourcetype ae-studio", "POST resourcetype ae-studio",
		"GET resource ae-studio", "POST resource ae-studio", "GET resource ae-studio", // the read before the apply, then the release wait
		"GET rrb ae-studio-development", "POST rrb ae-studio-development pin=" + f.oc.release}
	if !slices.Equal(f.oc.calls, want) {
		t.Fatalf("calls %v", f.oc.calls)
	}
	if f.oc.rt.Metadata.Annotations["aep.wso2.com/ae-studio-template-hash"] != TemplateHash() {
		t.Fatal("hash annotation")
	}
	if f.oc.rrb.Spec.Environment != "development" || f.oc.rrb.Spec.Owner.ProjectName != ProjectName || f.oc.rrb.Spec.Owner.ResourceName != ResourceName {
		t.Fatalf("binding spec %+v", f.oc.rrb.Spec)
	}
	if f.oc.res.Spec.Type.Name != ResourceName || f.oc.res.Spec.Owner.ProjectName != ProjectName {
		t.Fatalf("resource spec %+v", f.oc.res.Spec)
	}
}

func TestConverge_SecondPassWritesNothing(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	f.oc.resetCalls()
	st, _ := f.svc.Status(userCtx(), "default")
	if st.State == StateProvisioning || f.oc.writes() != 0 {
		t.Fatalf("state %s writes %v", st.State, f.oc.calls)
	}
	// A converge with nothing to do (a resubmit, a no-op save) writes nothing either.
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	if f.oc.writes() != 0 {
		t.Fatalf("a converge with nothing to do wrote: %v", f.oc.calls)
	}
}

// Review Focus 4: a key save (a new reference) rolls the pod: new
// parameters, a new release, the binding re-pinned to it.
func TestConverge_KeySaveRePins(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	before := f.oc.release
	f.withRef(organization.OrgSecretDefaultKey, "default-default-key-cccc0005")
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	if f.oc.release == before || f.oc.rrb.Spec.ResourceRelease != f.oc.release {
		t.Fatalf("release %s → %s, pin %s", before, f.oc.release, f.oc.rrb.Spec.ResourceRelease)
	}
	if !slices.Contains(f.oc.calls, "PUT resource ae-studio") {
		t.Fatalf("calls %v", f.oc.calls)
	}
}

// Review Focus 4: a new template is PUT in place (never deleted) and the
// pod re-pinned.
func TestConverge_TemplateChangePutsInPlace(t *testing.T) {
	f := newFixture(t).withAllRefs().converged().withLiveHash("old")
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning {
		t.Fatalf("state %s", st.State)
	}
	f.waitConverged(t)
	if !slices.Contains(f.oc.calls, "PUT resourcetype ae-studio") || f.oc.rt.Metadata.Annotations[templateHashAnnotation] != TemplateHash() {
		t.Fatalf("calls %v", f.oc.calls)
	}
	for _, c := range f.oc.calls {
		if strings.HasPrefix(c, "DELETE") || c == "POST resourcetype ae-studio" {
			t.Fatalf("the RT is PUT in place: %v", f.oc.calls)
		}
	}
}

// An image bump changes the parameters: a new release, re-pinned.
func TestConverge_ImageChangeRePins(t *testing.T) {
	f := newFixture(t).withAllRefs().converged().withImage("ae-collab:new")
	before := f.oc.release
	f.svc.Status(userCtx(), "default")
	f.waitConverged(t)
	var p params
	_ = json.Unmarshal(f.oc.res.Spec.Parameters, &p)
	if p.Images.Collab != "ae-collab:new" || f.oc.release == before || f.oc.rrb.Spec.ResourceRelease != f.oc.release {
		t.Fatalf("image %s release %s pin %s", p.Images.Collab, f.oc.release, f.oc.rrb.Spec.ResourceRelease)
	}
}

func TestTrigger_SingleFlight(t *testing.T) {
	f := newFixture(t).withAllRefs().slowOC(200 * time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.svc.Trigger(userCtx(), "default") }()
	}
	wg.Wait()
	f.waitConverged(t)
	if n := f.oc.count("POST resource ae-studio"); n != 1 {
		t.Fatalf("resource applied %d times", n)
	}
	if n := f.oc.count("POST project ae-system"); n != 1 {
		t.Fatalf("project created %d times", n)
	}
}

// A save during a converge is not lost: the converge runs once more.
func TestTrigger_DuringConvergeRunsOnceMore(t *testing.T) {
	f := newFixture(t).withAllRefs().slowOC(20 * time.Millisecond)
	f.svc.Trigger(userCtx(), "default")
	for f.oc.count("GET project ae-system") == 0 { // the converge has read its desired state
		time.Sleep(time.Millisecond)
	}
	f.withRef(organization.OrgSecretGitHubPAT, "default-github-pat-bbbb0001") // a PAT resubmit mid-converge
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	var p params
	_ = json.Unmarshal(f.oc.res.Spec.Parameters, &p)
	if p.Secrets.StudioTools.Data[0].Key != "user-app-secrets/ns/default-github-pat-bbbb0001" || f.oc.rrb.Spec.ResourceRelease != f.oc.release {
		t.Fatalf("the save made mid-converge did not land: %+v pin %s release %s", p.Secrets.StudioTools.Data[0], f.oc.rrb.Spec.ResourceRelease, f.oc.release)
	}
}

// The converge outlives the request and writes as aep-api's own identity
// (the fixture fails any write on a cancelled context or without it).
func TestConverge_DetachedFromTheRequest(t *testing.T) {
	f := newFixture(t).withAllRefs().slowOC(5 * time.Millisecond)
	req, cancel := context.WithCancel(userCtx())
	f.svc.Trigger(req, "default")
	cancel()
	f.waitConverged(t)
	if f.oc.rrb == nil {
		t.Fatal("the converge stopped with its request")
	}
}

func TestConverge_FailureIsFailedThenRetriedAfterBackoff(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.oc.mu.Lock()
	f.oc.project = true
	f.oc.mu.Unlock()
	f.withWriteTargetErr(errFake)
	f.svc.Trigger(userCtx(), "default")
	f.waitIdle(t)
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateFailed {
		t.Fatalf("a converge that failed moments ago: state %s", st.State)
	}
	f.withWriteTargetErr(nil)
	f.clock.advance(failureBackoff + time.Second)
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning {
		t.Fatalf("after the back-off: state %s", st.State)
	}
	f.waitConverged(t)
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateReady {
		t.Fatalf("after the retry: state %s", st.State)
	}
}

// A failure is retried at once when what it was asked to install changes.
func TestConverge_FailureRetriedAtOnceOnChange(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.oc.mu.Lock()
	f.oc.project = true
	f.oc.mu.Unlock()
	f.withWriteTargetErr(errFake)
	f.svc.Trigger(userCtx(), "default")
	f.waitIdle(t)
	f.withWriteTargetErr(nil).withImage("ae-collab:fixed")
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning {
		t.Fatalf("state %s", st.State)
	}
	f.waitConverged(t)
}
