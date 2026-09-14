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
	"log/slog"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Holds a component's deploy verdict at pending until its PUBLIC URL answers.
//
// OpenChoreo's binding Ready is a claim about the control plane: the release is
// rendered and the workload rolled out. Everything downstream reads it as a
// claim about the EDGE — the run supervisor dispatches validation against it,
// the console counts components live by it, and a person clicks the URL because
// of it. On a cloud plane those are minutes apart, because a component that has
// never been deployed gets a hostname that has never had a certificate, and
// cert-manager's ACME order is a second reconciler with its own timeline and no
// happens-before edge to the binding. Validation dispatched into that gap dies
// at its own preflight on a TLS alert, against a deployment where nothing is
// misconfigured and nothing is slow.
//
// So the fix is not a retry in the consumer — that would leave the console and
// the promote gate believing the same wrong claim. It is that `serving` means
// what every consumer already reads it to mean. A component held here is
// `converging`, which is ALREADY the state for "pinned, not ready yet": the
// deploy stage waits on it and the stage deadline turns one that never resolves
// into a deploy failure, so this adds a predicate rather than a mechanism.
//
// The local plane cannot see any of this — no per-host issuance step, so the
// gap is zero — which is why it shipped. It is also where the gate must NOT
// run: `*.openchoreoapis.localhost` resolves to loopback from inside the
// compose aep-api container (Docker's embedded resolver answers ::1, so being
// Go rather than curl does not help), and a probe that can never connect would
// hold every local web component at converging for ever. The composition root
// wires the probe only when the data-plane gateway fronts TLS, which is the
// same stated fact that decides which advertised URL is the live one.

const (
	// probeTimeout bounds ONE probe. It is deliberately short: this runs on the
	// console's status poll as well as the supervisor's, and a caller waiting on
	// a page render must not wear a long connect. A false "not answering" costs
	// one more retry window; the gateway answers in milliseconds once its
	// certificate exists, and fails its handshake fast before that.
	probeTimeout = 5 * time.Second
	// negativeRetryAfter is how long a NEGATIVE answer is trusted before the
	// gate probes again. Without it every poll through the issuance window pays
	// a fresh connect per unreachable component, which on the console's poll is
	// latency a person sees. A positive answer is kept for ever instead (see
	// EndpointGate.seen); only the "not yet" needs re-asking.
	negativeRetryAfter = 15 * time.Second
)

// endpointProbe answers whether a deployed endpoint responds at its public URL.
//
// ANY HTTP response means reachable; only a TRANSPORT failure does not. That is
// ADR-0006's rule, and this is the second implementation of it (the runner's
// `probeEndpoints` is the first, in TypeScript, so the rule is shared and the
// code cannot be). Status is deliberately not evidence: an endpoint behind the
// api-configuration trait legitimately answers 401, an API root legitimately
// answers 404, and reading either as unreachable would hold a healthy component
// out of `serving` for ever — manufacturing the deadlock this gate is supposed
// to prevent.
type endpointProbe interface {
	Answers(ctx context.Context, url string) bool
}

// EndpointGate answers "does this release's URL answer yet", once per release.
//
// A VALUE rather than a method set, because two readers ask the same question
// on the same window: the run supervisor's readiness poll (DeploymentState) and
// the console's project status. Sharing one gate makes the first answer serve
// both — whichever probes first pays — and, more importantly, stops the two
// from disagreeing about whether a component is up, which is the split that put
// "2 of 2 components live" on screen while validation was failing to reach one
// of them.
//
// It probes the ONE URL the binding's summary carries, which is the same URL
// the deployments read produces and the one a person clicks. That is a
// deliberate narrowing of ADR-0006, whose runner probes every endpoint in the
// validation context: the certificate this gate waits on is issued per HOST, a
// component's endpoints share theirs, and the summary's stated contract is that
// it carries exactly what the deployments read does.
type EndpointGate struct {
	probe endpointProbe
	// seen memoises the FIRST positive answer per (release, url).
	//
	// The window this gate exists for is crossed exactly once per release.
	// Re-probing afterwards would put an outbound request per component on
	// every poll, for ever, to re-answer a question whose answer cannot go
	// back. This is deliberately NOT health monitoring: a component whose
	// endpoint breaks after it first served still reads reachable here, exactly
	// as it does today on the binding alone. Watching for that is a different
	// job with a different owner.
	//
	// Keyed on the release too, so promoting a new one re-opens the question —
	// a rollout can move which pods serve the same URL.
	seen sync.Map
	// failed holds the time of the last NEGATIVE answer per key, so polls
	// arriving inside negativeRetryAfter answer from the memo rather than each
	// paying probeTimeout. Negative answers expire; positive ones do not.
	failed sync.Map
	// inflight collapses CONCURRENT misses on one key into a single probe.
	//
	// The memos above are read-then-write, so without this two readers arriving
	// together both miss and both probe — and the sentence above, that the two
	// cannot disagree about whether a component is up, would only be true when
	// their polls happen not to overlap. They are independent pollers on the
	// same window, so overlapping is the normal case, not the rare one.
	// Same pattern as organization's ensureInflight.
	inflight singleflight.Group
	// now is the clock, so the retry window is testable without sleeping.
	now func() time.Time
}

// NewEndpointGate builds the gate. A nil probe makes every question answer
// "reachable", which is the OC-only verdict this replaced — the off-switch that
// keeps a plane with no certificate window, and every test that wires nothing,
// behaving exactly as before.
func NewEndpointGate(p endpointProbe) *EndpointGate {
	return &EndpointGate{probe: p, now: time.Now}
}

func (g *EndpointGate) clock() time.Time {
	if g.now == nil {
		return time.Now()
	}
	return g.now()
}

// Reachable reports whether the release's URL has answered. A nil gate, a gate
// with no probe, and an empty URL all answer true: none of them is evidence
// that a component is DOWN, and holding on an absence would deadlock every
// caller that has no probe wired.
func (g *EndpointGate) Reachable(ctx context.Context, release, url string) bool {
	if g == nil || g.probe == nil || url == "" {
		return true
	}
	key := release + "\x00" + url
	if g.memoised(key) {
		return g.reachableByMemo(key)
	}
	// One probe per key, however many readers arrive at once; the others wait
	// on it and read the same verdict. The func returns no error, so the value
	// is the bool the shared call produced.
	v, _, _ := g.inflight.Do(key, func() (any, error) {
		// Re-checked INSIDE the flight: a caller that queued behind a probe
		// which has just answered must read that answer, not launch another.
		if g.memoised(key) {
			return g.reachableByMemo(key), nil
		}
		if !g.probe.Answers(ctx, url) {
			// A cancelled or timed-out CALLER is not evidence about the
			// endpoint. Caching it would let one abandoned console request tell
			// every other reader the component is down for the whole retry
			// window — including the supervisor, which would hold the deploy.
			//
			// The verdict still comes back false for everyone sharing this
			// flight, which is the safe direction: holding a component for one
			// more poll costs a poll, and nothing was memoised, so the next
			// caller probes afresh.
			if ctx.Err() == nil {
				g.failed.Store(key, g.clock())
			}
			return false, nil
		}
		g.seen.Store(key, struct{}{})
		g.failed.Delete(key)
		return true, nil
	})
	reachable, _ := v.(bool)
	return reachable
}

// memoised reports whether this key already has an answer worth reusing — a
// positive one, which is kept for ever, or a negative one still inside its
// retry window.
func (g *EndpointGate) memoised(key string) bool {
	if _, ok := g.seen.Load(key); ok {
		return true
	}
	return g.withinNegativeWindow(key)
}

// reachableByMemo reads the memoised verdict. Only meaningful when memoised
// said yes: a positive answer wins over a stale negative one, since a URL that
// has answered cannot go back to never having answered.
func (g *EndpointGate) reachableByMemo(key string) bool {
	_, ok := g.seen.Load(key)
	return ok
}

// withinNegativeWindow reports whether the last negative answer for this key is
// still fresh enough to reuse instead of re-probing.
func (g *EndpointGate) withinNegativeWindow(key string) bool {
	at, ok := g.failed.Load(key)
	if !ok {
		return false
	}
	last, isTime := at.(time.Time)
	return isTime && g.clock().Sub(last) < negativeRetryAfter
}

// SetEndpointGate wires the reachability gate. A nil gate skips it, which is
// today's OC-only verdict — the same off-switch the thunder wait uses, so
// existing DeploymentState tests stay green without new wiring.
func (s *DeploymentService) SetEndpointGate(g *EndpointGate) {
	if s != nil {
		s.endpoint = g
	}
}

// applyEndpointWait holds a component at pending until its external URL answers.
//
// Skipped for a component with NO external URL, and that is not a shortcut: a
// worker or an internal-only service is vacuously exposed, and waiting for an
// endpoint it will never advertise would hang the deploy stage's wave wait on
// every project that has one. Undeploy, failed and already-pending verdicts are
// left exactly as they were — a withdrawn component owes nothing, and a probe
// cannot improve a verdict that is already not Ready.
func (s *DeploymentService) applyEndpointWait(ctx context.Context, orgID, projectID, componentName string,
	summary *openchoreo.ReleaseBindingSummary, st *delivery.ComponentDeploy) {
	if s == nil || s.endpoint == nil {
		return
	}
	// A nil summary is a binding OpenChoreo has not admitted yet — already
	// pending, and the only shape that could reach the URL read below without
	// one.
	if summary == nil || summary.Undeploy {
		return
	}
	if st == nil || !st.Ready || st.Failed {
		return
	}

	// The URL off the SUMMARY, not a second read: GetReleaseBindingStatus
	// already carried it back on the same object, picked by the same scheme
	// preference the deployments read uses. Asking ListDeployments again would
	// spend an OpenChoreo request per component per poll to re-learn a fact this
	// call already has.
	url := summary.ExternalURL
	if s.endpoint.Reachable(ctx, st.Release, url) {
		return
	}

	st.Ready = false
	// Cleared with the verdict it described: componentDeployFrom copied
	// OpenChoreo's Ready-TRUE reason onto st before this gate ran, and leaving
	// it behind would caption a held component with the reason it was up. Same
	// reason holdUnreachable clears ReadyReason on the status path.
	st.Reason = ""
	slog.InfoContext(ctx, "deployment: binding is Ready but the endpoint does not answer yet — holding at converging",
		"org", orgID, "project", projectID, "component", componentName, "url", url, "release", st.Release)
}

// HTTPEndpointProbe is the default probe: one request, any answer counts.
//
// Redirects are NOT followed. A login redirect points at the IdP on the control
// plane, which is a different hop with its own resolution story; chasing it
// would turn an answered endpoint into a false negative. A 302 is an answer.
//
// TLS verification stays ON. A certificate that does not verify is precisely
// the condition this gate exists to catch, so skipping verification would make
// the probe pass in the window it was written for. A plane whose gateway does
// not terminate TLS advertises the http URL instead
// (openchoreo.Config.PreferPlainHTTPEndpoints), so nothing here needs to be
// lenient about certificates to work locally.
type HTTPEndpointProbe struct {
	client *http.Client
}

// NewHTTPEndpointProbe builds the default probe, bounded by probeTimeout.
func NewHTTPEndpointProbe() *HTTPEndpointProbe {
	return &HTTPEndpointProbe{client: &http.Client{
		Timeout: probeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (p *HTTPEndpointProbe) Answers(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return false
	}
	// Nothing here reads the body; closing it returns the connection.
	_ = resp.Body.Close()
	return true
}
