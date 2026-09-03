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

package delivery

import (
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
)

// The VERSION's deploy state: what each of a version's components should be
// serving, what it is serving, and the difference between those two (ADR-0026).
//
// It lives at the delivery ROOT for the same reason the build fan-out naming
// does. Two peer packages have to agree on it exactly — the RUN SUPERVISOR
// classifies and decides, `projects` orders and writes — and they may not
// import each other. A second spelling of "behind" would let the planner and
// the promoter disagree about which components a pass owns, which is the class
// of bug this whole model exists to remove.
//
// The deploy set used to be the cycle's path diff: whichever components the
// merge happened to touch. That made "what is serving" a function of which
// files a fix edited rather than of what has been built, so a component whose
// build went green in a cycle a SIBLING failed was never promoted by any later
// cycle either — its files had stopped changing. The states below replace that
// with a difference between desired and actual, re-derived from ground truth on
// every pass.

// Component deploy states. Exactly one holds per component per read.
const (
	// ComponentStateServing — the binding pins the release of the component's
	// newest succeeded build, and OpenChoreo reports it Ready. This is the only
	// state that satisfies a hard consumer, and the only one serving(V) accepts.
	ComponentStateServing = "serving"
	// ComponentStateBehind — a succeeded build exists whose release is not what
	// the binding pins (including a component with no binding at all). The ONLY
	// state a reconcile writes to.
	ComponentStateBehind = "behind"
	// ComponentStateConverging — the binding pins the right release and has not
	// reported Ready yet.
	//
	// It covers a binding that will NEVER be ready as well as one that is thirty
	// seconds away: the two are indistinguishable from a pin, and the readiness
	// poll is what separates them — a terminal Ready reason arrives as a deploy
	// failure within one tick of the wait. Classifying on the pin alone is what
	// keeps this function pure.
	ComponentStateConverging = "converging"
	// ComponentStateHeld — behind, but a hard provider is not serving and is not
	// being promoted in this pass. Not a failure: the provider's own state has
	// its own work (a fix issue, or an open development issue), and this
	// component is promoted by the first pass that finds its providers serving.
	ComponentStateHeld = "held"
	// ComponentStateUnbuilt — no succeeded build at any commit, so there is
	// nothing to promote. Not a failure either: a red build already minted its
	// fix issue, and a component nobody has written yet has its development
	// issue open.
	ComponentStateUnbuilt = "unbuilt"
)

// ComponentState is one component's desired state, its actual state, and the
// verdict comparing them.
type ComponentState struct {
	Component string `json:"component"`
	State     string `json:"state"`
	// DesiredSHA is the commit of the component's newest SUCCEEDED build, ""
	// when it has none. Newest succeeded rather than newest: a component whose
	// re-trigger failed is still built, at the commit that worked.
	DesiredSHA string `json:"desiredSha,omitempty"`
	// DesiredRelease is the release DesiredSHA names — carried rather than
	// recomposed at every comparison site, because it is the value the binding's
	// pin is compared against.
	DesiredRelease string `json:"desiredRelease,omitempty"`
	// Pinned is the release the binding pins, "" when there is no binding.
	Pinned string `json:"pinned,omitempty"`
	Ready  bool   `json:"ready"`
	// Undeploy is a binding somebody took out of the environment on purpose.
	//
	// Such a component reads as `serving` — nothing is owed, and promoting over a
	// deliberate withdrawal would be the platform overruling the person who asked
	// for it — but it is NOT a satisfied hard provider: it has no active release,
	// so it has no address for a consumer's start-up config to carry. The two
	// facts are separate for exactly that reason, and a planner that could not
	// tell them apart would publish a web app against a backend nobody is running.
	Undeploy bool `json:"undeploy,omitempty"`
	// Reason is OpenChoreo's own Ready reason, verbatim. Carried for the log and
	// the issue body; never branched on here.
	Reason string `json:"reason,omitempty"`
	// WaitingOn names the hard providers holding this component back. Set only
	// on a held component, by the planner that decided it.
	WaitingOn []string `json:"waitingOn,omitempty"`
}

// VersionState is every component in the design, classified.
//
// Every component, not the cycle's: a version is delivered when everything the
// design declares is serving, so a state that only described what one merge
// touched could never answer that question.
type VersionState struct {
	Components []ComponentState `json:"components"`
}

// Serving reports whether every component in the design is serving — the
// predicate a version's delivery and its validation both wait on.
//
// An EMPTY state is serving, and deliberately: a project with no design has no
// component that could fail to serve, and a plane with no OpenChoreo behind it
// reads every component as ready for the same reason DeployCycle degrades to a
// no-op. Refusing to deliver in either case would fail runs over a gate that
// has nothing to gate.
func (v VersionState) Serving() bool {
	for _, c := range v.Components {
		if c.State != ComponentStateServing {
			return false
		}
	}
	return true
}

// NotServing lists the components that are not serving, with their state — what
// the `version-incomplete` settle names, and what the console shows beside a
// partially serving version. Sorted, so two reads of the same world read alike.
func (v VersionState) NotServing() []ComponentState {
	out := make([]ComponentState, 0, len(v.Components))
	for _, c := range v.Components {
		if c.State != ComponentStateServing {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

// ServesConsumers reports whether this component's address exists for a hard
// consumer to be composed against: serving, and not deliberately withdrawn.
//
// Deliberately narrower than `State == serving`, which answers "does the version
// owe this component anything". An undeployed component owes nothing and offers
// nothing, and only the planner cares about the difference.
func (c ComponentState) ServesConsumers() bool {
	return c.State == ComponentStateServing && !c.Undeploy
}

// ServingCount is how many of the version's components are serving, out of how
// many the design declares.
func (v VersionState) ServingCount() (serving, total int) {
	for _, c := range v.Components {
		if c.State == ComponentStateServing {
			serving++
		}
	}
	return serving, len(v.Components)
}

// Describe renders the non-serving components as `component=state` pairs, for a
// log line and for the run's live status. It names the state rather than the
// release, because which of the five a component is in is the actionable fact.
func (v VersionState) Describe() string {
	off := v.NotServing()
	parts := make([]string, 0, len(off))
	for _, c := range off {
		part := c.Component + "=" + c.State
		if len(c.WaitingOn) > 0 {
			part += " (waiting on " + strings.Join(c.WaitingOn, ", ") + ")"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, ", ")
}

// DeployTarget is one component's promote: the component, and the commit whose
// release is to be pinned.
//
// The commit is PER COMPONENT, which is the whole difference from the deploy
// this replaced. One commit for a whole list is only right when the list is
// what a single merge built; a reconcile promotes each component at its own
// newest green build, which is a different commit per component in exactly the
// case that motivated the change.
//
// An EMPTY CommitSHA is a CONVERGE: re-assert the wiring at whatever release is
// already serving, without moving the pin (see ConvergeTargets).
type DeployTarget struct {
	Component string `json:"component"`
	CommitSHA string `json:"commitSha,omitempty"`
}

// ConvergeTargets turns component names into converge targets — no commit, so
// nothing is re-cut and no component's live release can move.
func ConvergeTargets(components []string) []DeployTarget {
	out := make([]DeployTarget, 0, len(components))
	for _, name := range components {
		out = append(out, DeployTarget{Component: name})
	}
	return out
}

// TargetNames lists the components a target set addresses, in order.
func TargetNames(targets []DeployTarget) []string {
	out := make([]string, 0, len(targets))
	for _, t := range targets {
		out = append(out, t.Component)
	}
	return out
}

// DeployPlan is what one reconcile pass will do: the waves to promote, the
// components to wait on, and the ones it deliberately leaves alone.
type DeployPlan struct {
	// Waves are the promotes, in order: every component in a wave has each of
	// its hard providers serving already or promoted in an earlier wave.
	Waves [][]DeployTarget `json:"waves,omitempty"`
	// Waited is what the readiness poll waits for: everything promoted, plus the
	// components already converging towards the release they pin.
	//
	// Held and unbuilt components are never here. The stage has ONE deadline,
	// and spending it on a binding this pass did not write — or on a component
	// that has no binding to wait for — would settle `deploy-budget` on a run
	// whose only problem is work somebody still has to do.
	Waited []string `json:"waited,omitempty"`
	// Held is the behind components this pass refused to promote, each carrying
	// the providers it is waiting on. Reported for the log and the run's status;
	// nothing waits on it and nothing is filed against it.
	Held []ComponentState `json:"held,omitempty"`
}

// Writes reports whether the plan promotes anything. A plan that writes nothing
// starts no deadline — a fully serving version must reconcile in one read.
func (p DeployPlan) Writes() bool {
	for _, wave := range p.Waves {
		if len(wave) > 0 {
			return true
		}
	}
	return false
}

// ReleaseNameFor names the release a component's deployment pins at a commit.
//
// Derived from the commit rather than server-generated so the whole deploy is
// idempotent: the same cycle re-running its deploy activity cuts the same
// release name, which OpenChoreo answers with a 409 the client treats as
// success. Bounded through k8sname for the same reason build run names are — a
// name one character over the label budget is accepted and then never renders.
//
// It lives HERE, beside BuildRunName, because two packages compose it and must
// compose it identically: `projects` writes the name into the binding, and the
// supervisor recomposes it from a build's commit to ask whether the binding
// pins the release it should. The name is not parseable back into a commit —
// k8sname.Bounded truncates a long readable head and appends a digest — so
// composing the desired name and comparing is the only sound direction.
func ReleaseNameFor(projectID, componentName, commitSHA string) string {
	return k8sname.Bounded(k8sname.MaxLabelValueLen,
		k8sname.Capped(projectID, releaseNameProjectWidth),
		k8sname.Capped(componentName, releaseNameComponentWidth),
		k8sname.Whole(ShortSHA(commitSHA)),
	)
}

// Widths of the readable head of a release name. The commit is never truncated
// — matching a release to the commit it froze is the main reason anyone reads
// one of these names.
const (
	releaseNameProjectWidth   = 18
	releaseNameComponentWidth = 18
)
