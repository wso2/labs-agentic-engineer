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
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
)

// UNIT tier for the endpoint deploy-wait: DeploymentState is not Ready for a
// component whose advertised external URL does not answer yet, however happy
// OpenChoreo is about the binding. Seam is DeploymentService.DeploymentState
// and Service.holdUnreachable — the two readers that used to hold different
// definitions of "up"; the OC status read and the probe are fakes.
//
// The downstream half needs no test here: Ready=false already classifies as
// `converging` (delivery/run: policy_test.go), which is the state the deploy
// stage waits on.

const (
	endpointWaitOrg     = "acme"
	endpointWaitProject = "proj"
	endpointWaitComp    = "web"
	endpointWaitRelease = "proj-web-abc1234"
	endpointWaitURL     = "https://web.example.test/"
)

// fakeEndpointProbe is a hand double of endpointProbe that counts calls, so a
// test can assert the probe was NOT consulted as well as what it answered.
//
// Guarded, because the concurrency tests below call it from several goroutines
// at once — an unsynchronised counter there would race rather than fail.
type fakeEndpointProbe struct {
	mu      sync.Mutex
	answers bool
	calls   int
	urls    []string
	// block, when set, holds every probe until it is closed, so a test can be
	// sure several callers are inside the gate at the same moment.
	block chan struct{}
}

func (f *fakeEndpointProbe) Answers(_ context.Context, url string) bool {
	f.mu.Lock()
	f.calls++
	f.urls = append(f.urls, url)
	answers, block := f.answers, f.block
	f.mu.Unlock()
	if block != nil {
		<-block
	}
	return answers
}

func (f *fakeEndpointProbe) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeEndpointProbe) probedURLs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

type endpointWaitOpts struct {
	// summary is the binding read; nil means a Ready one.
	summary *openchoreo.ReleaseBindingSummary
	// url is what the binding advertises; empty means none, which is how a
	// worker or an internal-only component reads.
	url string
	// probe is the gate; nil leaves it unwired, which is the OC-only verdict.
	probe *fakeEndpointProbe
}

type endpointWaitHarness struct {
	svc   *DeploymentService
	probe *fakeEndpointProbe
}

func newEndpointWaitHarness(t *testing.T, opts endpointWaitOpts) *endpointWaitHarness {
	t.Helper()
	if opts.summary == nil {
		opts.summary = &openchoreo.ReleaseBindingSummary{
			ReadyStatus: "True",
			ReleaseName: endpointWaitRelease,
		}
	}
	// The URL rides the SUMMARY — the same object the readiness poll already
	// read — so the fake sets it there rather than on a deployments list.
	opts.summary.ExternalURL = opts.url
	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return opts.summary, nil
		},
	}
	svc := NewDeploymentService(oc, nil)
	if opts.probe != nil {
		svc.SetEndpointGate(NewEndpointGate(opts.probe))
	}
	return &endpointWaitHarness{svc: svc, probe: opts.probe}
}

func (h *endpointWaitHarness) ready(t *testing.T) bool {
	t.Helper()
	got, err := h.svc.DeploymentState(context.Background(), endpointWaitOrg, endpointWaitProject,
		[]string{endpointWaitComp})
	if err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 component, got %d", len(got))
	}
	return got[0].Ready
}

// The whole point: OpenChoreo says Ready, the edge does not answer, and the
// component is held. This is the certificate-issuance window, in which
// validation used to be dispatched against a system that could not be reached.
func TestEndpointWaitHoldsAReadyBindingWhoseURLDoesNotAnswer(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	h := newEndpointWaitHarness(t, endpointWaitOpts{url: endpointWaitURL, probe: probe})

	if h.ready(t) {
		t.Fatal("component is Ready while its endpoint does not answer — validation would dispatch against an unreachable system")
	}
	if probe.callCount() != 1 {
		t.Fatalf("probe calls = %d, want 1", probe.calls)
	}
	if probe.probedURLs()[0] != endpointWaitURL {
		t.Errorf("probed %q, want the advertised URL %q", probe.probedURLs()[0], endpointWaitURL)
	}
}

func TestEndpointWaitPassesAReadyBindingWhoseURLAnswers(t *testing.T) {
	h := newEndpointWaitHarness(t, endpointWaitOpts{
		url:   endpointWaitURL,
		probe: &fakeEndpointProbe{answers: true},
	})

	if !h.ready(t) {
		t.Fatal("component is not Ready though its endpoint answers")
	}
}

// A worker or an internal-only service advertises no external URL. It is
// vacuously exposed — holding it would hang the deploy stage's wave wait on
// every project that has one.
func TestEndpointWaitPassesAComponentWithNoExternalURL(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	h := newEndpointWaitHarness(t, endpointWaitOpts{url: "", probe: probe})

	if !h.ready(t) {
		t.Fatal("a component with no external URL was held — the wave wait would never finish")
	}
	if probe.callCount() != 0 {
		t.Fatalf("probe calls = %d, want 0 — there is no URL to probe", probe.calls)
	}
}

// Withdrawn on purpose: nothing is owed, so the gate does not ask.
func TestEndpointWaitLeavesAnUndeployedComponentAlone(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	h := newEndpointWaitHarness(t, endpointWaitOpts{
		summary: &openchoreo.ReleaseBindingSummary{Undeploy: true, ReleaseName: endpointWaitRelease},
		url:     endpointWaitURL,
		probe:   probe,
	})

	if !h.ready(t) {
		t.Fatal("an undeployed component was held — nothing is owed there")
	}
	if probe.callCount() != 0 {
		t.Fatalf("probe calls = %d, want 0", probe.calls)
	}
}

// A binding that is not Ready yet is already pending. A probe cannot improve
// that verdict, and spending a request per poll to re-learn it is waste.
func TestEndpointWaitDoesNotProbeAPendingBinding(t *testing.T) {
	probe := &fakeEndpointProbe{answers: true}
	h := newEndpointWaitHarness(t, endpointWaitOpts{
		summary: &openchoreo.ReleaseBindingSummary{ReadyStatus: "False", ReleaseName: endpointWaitRelease},
		url:     endpointWaitURL,
		probe:   probe,
	})

	if h.ready(t) {
		t.Fatal("a pending binding read as Ready")
	}
	if probe.callCount() != 0 {
		t.Fatalf("probe calls = %d, want 0 — the binding is not Ready", probe.calls)
	}
}

// A POSITIVE answer is kept for ever: the window is crossed once per release,
// and re-probing would put an outbound request per component on every readiness
// poll for the life of the run.
func TestEndpointWaitProbesOncePerRelease(t *testing.T) {
	probe := &fakeEndpointProbe{answers: true}
	h := newEndpointWaitHarness(t, endpointWaitOpts{url: endpointWaitURL, probe: probe})

	for i := range 3 {
		if !h.ready(t) {
			t.Fatalf("poll %d: component is not Ready though its endpoint answered", i+1)
		}
	}
	if probe.callCount() != 1 {
		t.Fatalf("probe calls = %d over 3 polls, want 1", probe.calls)
	}
}

// No probe wired is the OC-only verdict — the off-switch that keeps every
// existing DeploymentState test green without new wiring.
func TestEndpointWaitSkippedWithoutAProbe(t *testing.T) {
	h := newEndpointWaitHarness(t, endpointWaitOpts{url: endpointWaitURL})

	if !h.ready(t) {
		t.Fatal("component was held with no probe wired")
	}
}

// The default probe's rule, which has to match the runner's (ADR-0006): any
// HTTP answer counts, and a status is never evidence. An endpoint behind the
// api-configuration trait answers 401 and an API root answers 404, and reading
// either as unreachable would hold a healthy component out of serving for ever.
func TestHTTPEndpointProbeAcceptsAnyAnswer(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusUnauthorized, http.StatusNotFound,
		http.StatusFound, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		if !NewHTTPEndpointProbe().Answers(context.Background(), srv.URL) {
			t.Errorf("status %d read as unreachable — only a transport failure may", code)
		}
		srv.Close()
	}
}

// And the other half of the rule: a transport failure is the only thing that
// means unreachable. Closing the server first is the closest stand-in for the
// gateway that answered a TLS alert.
func TestHTTPEndpointProbeRejectsATransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if NewHTTPEndpointProbe().Answers(context.Background(), url) {
		t.Error("a refused connection read as reachable")
	}
}

// -- the console's half -------------------------------------------------------

// reachableBinding is devBinding's (project_stages_test.go) sibling for this
// concern: same dev-environment shape, but carrying the advertised URL the gate
// keys on instead of a Ready reason.
func reachableBinding(name, ready, url string) openchoreo.ReleaseBindingSummary {
	return openchoreo.ReleaseBindingSummary{
		ComponentName: name,
		Environment:   openchoreo.DevEnvironmentName,
		ReadyStatus:   ready,
		ReleaseName:   endpointWaitRelease,
		ExternalURL:   url,
	}
}

// The console's count and chip come off the same gate now. Before this, the
// overview read "Deployed · 2 of 2 components live" during the exact window in
// which validation could not reach one of them.
func TestHoldUnreachableDowngradesAReadyBindingThatDoesNotAnswer(t *testing.T) {
	svc := &Service{}
	svc.SetEndpointGate(NewEndpointGate(&fakeEndpointProbe{answers: false}))

	dev := []openchoreo.ReleaseBindingSummary{
		reachableBinding("api", "True", ""),
		reachableBinding("web", "True", endpointWaitURL),
	}
	got := svc.holdUnreachable(context.Background(), dev)

	if n := countReady(got); n != 1 {
		t.Errorf("countReady = %d, want 1 — the web component cannot be reached", n)
	}
	if st := deployStageStatus(got); st != deployDeploying {
		t.Errorf("deployStageStatus = %q, want %q", st, deployDeploying)
	}
	// A downgrade must read as PENDING, never as a failure: nothing is broken,
	// the certificate has not been issued yet.
	for _, b := range got {
		if bindingFailed(b) {
			t.Errorf("%s read as failed; want pending", b.ComponentName)
		}
	}
}

func TestHoldUnreachablePassesBindingsThatAnswer(t *testing.T) {
	svc := &Service{}
	svc.SetEndpointGate(NewEndpointGate(&fakeEndpointProbe{answers: true}))

	dev := []openchoreo.ReleaseBindingSummary{reachableBinding("web", "True", endpointWaitURL)}
	got := svc.holdUnreachable(context.Background(), dev)

	if n := countReady(got); n != 1 {
		t.Errorf("countReady = %d, want 1", n)
	}
	if st := deployStageStatus(got); st != deployDeployed {
		t.Errorf("deployStageStatus = %q, want %q", st, deployDeployed)
	}
}

// The caller's slice is the status poll's own read. Rewriting it in place would
// leak a downgrade into whatever else that read feeds.
func TestHoldUnreachableDoesNotMutateTheCallersSlice(t *testing.T) {
	svc := &Service{}
	svc.SetEndpointGate(NewEndpointGate(&fakeEndpointProbe{answers: false}))

	dev := []openchoreo.ReleaseBindingSummary{reachableBinding("web", "True", endpointWaitURL)}
	_ = svc.holdUnreachable(context.Background(), dev)

	if dev[0].ReadyStatus != "True" {
		t.Errorf("caller's slice was rewritten: ReadyStatus = %q", dev[0].ReadyStatus)
	}
}

func TestHoldUnreachableWithoutAGateCountsAsBefore(t *testing.T) {
	svc := &Service{}
	dev := []openchoreo.ReleaseBindingSummary{reachableBinding("web", "True", endpointWaitURL)}

	if n := countReady(svc.holdUnreachable(context.Background(), dev)); n != 1 {
		t.Errorf("countReady = %d with no gate wired, want 1 — the binding-only count", n)
	}
}

// The reason the gate is a shared VALUE: whichever reader probes first, the
// other gets the answer without a second request — and, more importantly, the
// two can never report different verdicts for one component.
func TestEndpointGateIsSharedBetweenTheTwoReaders(t *testing.T) {
	probe := &fakeEndpointProbe{answers: true}
	gate := NewEndpointGate(probe)

	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return &openchoreo.ReleaseBindingSummary{
				ReadyStatus: "True", ReleaseName: endpointWaitRelease, ExternalURL: endpointWaitURL,
			}, nil
		},
	}
	deploySvc := NewDeploymentService(oc, nil)
	deploySvc.SetEndpointGate(gate)
	statusSvc := &Service{}
	statusSvc.SetEndpointGate(gate)

	if _, err := deploySvc.DeploymentState(context.Background(), endpointWaitOrg, endpointWaitProject,
		[]string{endpointWaitComp}); err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	got := statusSvc.holdUnreachable(context.Background(),
		[]openchoreo.ReleaseBindingSummary{reachableBinding("web", "True", endpointWaitURL)})

	if n := countReady(got); n != 1 {
		t.Errorf("countReady = %d, want 1 — the supervisor already proved this URL answers", n)
	}
	if probe.callCount() != 1 {
		t.Errorf("probe calls = %d across both readers, want 1", probe.calls)
	}
}

// A NEGATIVE answer is memoised too, but only for negativeRetryAfter. Without
// that window every console poll through the issuance gap pays a fresh connect
// per unreachable component, which is latency a person waiting on the project
// page sees.
func TestEndpointGateDoesNotReprobeInsideTheNegativeWindow(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	gate := NewEndpointGate(probe)
	now := time.Now()
	gate.now = func() time.Time { return now }

	for i := range 5 {
		if gate.Reachable(context.Background(), endpointWaitRelease, endpointWaitURL) {
			t.Fatalf("poll %d: unreachable URL read as reachable", i+1)
		}
	}
	if probe.callCount() != 1 {
		t.Fatalf("probe calls = %d over 5 polls inside the window, want 1", probe.calls)
	}
}

// And it does expire — a gate that never re-asked would hold a component whose
// certificate has since been issued at converging until the stage deadline.
func TestEndpointGateReprobesAfterTheNegativeWindow(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	gate := NewEndpointGate(probe)
	now := time.Now()
	gate.now = func() time.Time { return now }

	if gate.Reachable(context.Background(), endpointWaitRelease, endpointWaitURL) {
		t.Fatal("unreachable URL read as reachable")
	}
	now = now.Add(negativeRetryAfter + time.Second)
	probe.answers = true

	if !gate.Reachable(context.Background(), endpointWaitRelease, endpointWaitURL) {
		t.Fatal("the URL answers now, but the gate never re-asked")
	}
	if probe.callCount() != 2 {
		t.Fatalf("probe calls = %d, want 2 — one per side of the window", probe.calls)
	}
}

// A downgrade carries no reason, so bindingFailed cannot read one. OpenChoreo's
// Ready-True reason describes the verdict holdUnreachable has just withdrawn.
func TestHoldUnreachableClearsTheWithdrawnReadyReason(t *testing.T) {
	svc := &Service{}
	svc.SetEndpointGate(NewEndpointGate(&fakeEndpointProbe{answers: false}))

	dev := []openchoreo.ReleaseBindingSummary{{
		ComponentName: "web",
		Environment:   openchoreo.DevEnvironmentName,
		ReadyStatus:   "True",
		ReadyReason:   "ReleaseReady",
		ReleaseName:   endpointWaitRelease,
		ExternalURL:   endpointWaitURL,
	}}
	got := svc.holdUnreachable(context.Background(), dev)

	if got[0].ReadyReason != "" {
		t.Errorf("ReadyReason = %q, want empty — the Ready verdict was withdrawn", got[0].ReadyReason)
	}
}

// -- concurrency and cancellation --------------------------------------------

// The two readers poll independently, so they arrive together as a matter of
// course. One probe must serve both: without single-flight each would miss the
// memo and probe, and the claim that they cannot disagree about whether a
// component is up would hold only when their polls happened not to overlap.
func TestEndpointGateCollapsesConcurrentMissesIntoOneProbe(t *testing.T) {
	probe := &fakeEndpointProbe{answers: true, block: make(chan struct{})}
	gate := NewEndpointGate(probe)

	const readers = 8
	verdicts := make([]bool, readers)
	var wg sync.WaitGroup
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			verdicts[i] = gate.Reachable(context.Background(), endpointWaitRelease, endpointWaitURL)
		}()
	}
	// Let every reader reach the gate, then release the single probe.
	for probe.callCount() == 0 {
		runtime.Gosched()
	}
	close(probe.block)
	wg.Wait()

	if n := probe.callCount(); n != 1 {
		t.Errorf("probe calls = %d across %d concurrent readers, want 1", n, readers)
	}
	for i, v := range verdicts {
		if !v {
			t.Errorf("reader %d read unreachable; every reader must share the one verdict", i)
		}
	}
}

// A caller giving up is not evidence about the endpoint. Caching it would let
// one abandoned console request tell every other reader — the supervisor
// included — that the component is down for the whole retry window.
func TestEndpointGateDoesNotCacheCallerCancellation(t *testing.T) {
	probe := &fakeEndpointProbe{answers: false}
	gate := NewEndpointGate(probe)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if gate.Reachable(cancelled, endpointWaitRelease, endpointWaitURL) {
		t.Fatal("a cancelled probe read as reachable")
	}

	// A live caller must get a fresh probe, not the cancelled caller's verdict.
	probe.answers = true
	if !gate.Reachable(context.Background(), endpointWaitRelease, endpointWaitURL) {
		t.Error("the cancelled caller's failure was cached and held a reachable endpoint")
	}
	if n := probe.callCount(); n != 2 {
		t.Errorf("probe calls = %d, want 2 — the live caller must probe afresh", n)
	}
}

// The deploy path clears the withdrawn reason too, not just the status path.
// componentDeployFrom copies OpenChoreo's Ready-True reason before the gate
// runs, and a held component captioned with the reason it was up is a lie.
func TestEndpointWaitClearsTheWithdrawnReasonOnTheDeployPath(t *testing.T) {
	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return &openchoreo.ReleaseBindingSummary{
				ReadyStatus: "True",
				ReadyReason: "ReleaseReady",
				ReleaseName: endpointWaitRelease,
				ExternalURL: endpointWaitURL,
			}, nil
		},
	}
	svc := NewDeploymentService(oc, nil)
	svc.SetEndpointGate(NewEndpointGate(&fakeEndpointProbe{answers: false}))

	got, err := svc.DeploymentState(context.Background(), endpointWaitOrg, endpointWaitProject,
		[]string{endpointWaitComp})
	if err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	if got[0].Ready {
		t.Fatal("component is Ready while its endpoint does not answer")
	}
	if got[0].Reason != "" {
		t.Errorf("Reason = %q, want empty — the Ready verdict was withdrawn", got[0].Reason)
	}
}
