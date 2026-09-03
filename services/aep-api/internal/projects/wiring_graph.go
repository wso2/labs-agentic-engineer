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
	"fmt"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// deploymentWaves plans one reconcile pass: which components to promote, in
// which order, which to wait on, and which to leave alone (ADR-0026).
//
// WAVES, because every component in wave N has each of its hard providers in
// some earlier one. The stage waits for each wave to serve before starting the
// next, which is what makes a hard edge (spec.HardConfigEdges) mean what it
// says — the provider has an address by the time the consumer's config is
// composed. Promoting the whole set at once instead publishes the consumer with
// a config nothing could have filled, which for a web app is a blank page
// served to anyone who visits during the repair.
//
// PROMOTABLE is the rule that replaced "everything the cycle built". A
// component is promoted when it is BEHIND and every hard provider is either
// serving already or being promoted in this same pass:
//
//	promotable(c) = behind(c) ∧ ∀p ∈ providers(c): serving(p) ∨ promotable(p)
//
// Anything else behind is HELD, carrying the providers it waits on. That is the
// answer to "A needs B; A is green, B is red — deploy A?": no, and not as a
// special case. It is also why the edges are read over the WHOLE design rather
// than over the set being deployed: the incident this replaced promoted a web
// app with no api behind it precisely because the only edges considered were
// the ones inside a two-file diff, so a provider with no release at all was an
// assumed-satisfied edge instead of an unsatisfied one.
//
// Held is not a failure and is never waited on: the provider's own state has
// its own work — a red build's fix issue, an unwritten component's development
// issue — and this component is promoted by the first pass that finds its
// providers serving.
//
// The set is addressed in OpenChoreo's k8s-shaped names, because that is what
// the run loop carries and what the deployer writes; the design graph is keyed
// by design names, so the mapping happens here rather than at either end.
//
// A cycle among HARD edges is permanent, not slow: two components that each need
// the other's address before starting cannot both go first, and no amount of
// retrying changes that. It comes back wrapped in delivery.ErrDeployPermanent so
// the supervisor files it as a deploy failure on the first attempt rather than
// retrying an unsatisfiable order forever. Soft edges are absent from this graph
// by construction and may cycle freely — CORS always does.
func deploymentWaves(design *spec.DesignFile, state delivery.VersionState) (delivery.DeployPlan, error) {
	byName := make(map[string]delivery.ComponentState, len(state.Components))
	var behind []string
	for _, c := range state.Components {
		byName[c.Component] = c
		if c.State == delivery.ComponentStateBehind {
			behind = append(behind, c.Component)
		}
	}
	plan := delivery.DeployPlan{Waited: waitedOn(state)}
	if len(behind) == 0 {
		return plan, nil
	}

	// consumer -> hard providers, k8s-shaped, restricted to components the
	// version state knows about. Translating here is what makes the design graph
	// and the deploy set talk about the same components: the design names them as
	// the architect wrote them, the run loop carries what OpenChoreo is addressed
	// by, and an untranslated comparison would quietly find no edges at all —
	// every component in wave one, which is the exact behaviour the waves exist
	// to replace.
	//
	// A nil design yields no edges, which is the same answer it gave before: the
	// design is the ordering input, not the deploy's permission.
	providers := make(map[string][]string, len(state.Components))
	for consumer, deps := range spec.HardConfigEdges(design) {
		c := k8sname.ToK8sName(consumer)
		if _, known := byName[c]; !known {
			continue
		}
		for _, dep := range deps {
			p := k8sname.ToK8sName(dep)
			if _, known := byName[p]; !known || p == c {
				continue
			}
			providers[c] = append(providers[c], p)
		}
	}

	promotable, held := promotableSet(behind, providers, byName)
	plan.Held = held
	if len(promotable) == 0 {
		return plan, nil
	}
	// Only edges INSIDE the promoted set order anything. A provider that already
	// serves its consumers has an address, so waiting on it would be waiting on
	// something that has happened — and by the rule above, a provider that is
	// neither serving nor promoted here means its consumer is held, not ordered.
	promoting := make(map[string]struct{}, len(promotable))
	for _, name := range promotable {
		promoting[name] = struct{}{}
	}
	ordered := make(map[string][]string, len(promotable))
	for _, c := range promotable {
		for _, p := range providers[c] {
			if _, alsoPromoting := promoting[p]; alsoPromoting {
				ordered[c] = append(ordered[c], p)
			}
		}
	}
	waves, err := wavesFromEdges(promotable, ordered)
	if err != nil {
		return delivery.DeployPlan{}, err
	}
	for _, wave := range waves {
		targets := make([]delivery.DeployTarget, 0, len(wave))
		for _, name := range wave {
			targets = append(targets, delivery.DeployTarget{
				Component: name, CommitSHA: byName[name].DesiredSHA,
			})
			plan.Waited = append(plan.Waited, name)
		}
		plan.Waves = append(plan.Waves, targets)
	}
	sort.Strings(plan.Waited)
	return plan, nil
}

// promotableSet is the fixpoint behind the promotable rule: drop every behind
// component whose hard providers cannot be met, then drop the ones that were
// only waiting on THOSE, until nothing more falls out.
//
// One pass is not enough, and the case that proves it is a chain: a web app
// waiting on an api waiting on an unbuilt service. A single pass would hold the
// api and promote the web app into a config its api could not fill.
//
// A provider is met when it is SERVING or when it is being promoted in this
// pass. Any other state — unbuilt, held, or converging towards a release it has
// not reached — is a real unsatisfied edge: the stage's premise is that a
// provider's address exists by the time the consumer's config is composed, and
// only a serving release proves that. A converging provider therefore costs its
// consumer one cycle, which is the conservative direction.
func promotableSet(behind []string, providers map[string][]string,
	byName map[string]delivery.ComponentState) (promotable []string, held []delivery.ComponentState) {
	candidates := make(map[string]struct{}, len(behind))
	for _, name := range behind {
		candidates[name] = struct{}{}
	}
	waiting := map[string][]string{}
	for {
		var dropped []string
		for _, name := range behind {
			if _, still := candidates[name]; !still {
				continue
			}
			var unmet []string
			for _, p := range providers[name] {
				// ServesConsumers, not `State == serving`: a component somebody
				// UNDEPLOYED on purpose owes the version nothing and so reads as
				// serving, but it has no active release and therefore no address
				// for this component's start-up config to carry.
				if byName[p].ServesConsumers() {
					continue
				}
				if _, promoting := candidates[p]; promoting {
					continue
				}
				unmet = append(unmet, p)
			}
			if len(unmet) > 0 {
				dropped = append(dropped, name)
				waiting[name] = unmet
			}
		}
		if len(dropped) == 0 {
			break
		}
		for _, name := range dropped {
			delete(candidates, name)
		}
	}
	// Input order is preserved so the plan is stable across attempts; a deploy
	// order that reshuffles between retries is needlessly hard to read in a log.
	for _, name := range behind {
		if _, ok := candidates[name]; ok {
			promotable = append(promotable, name)
			continue
		}
		st := byName[name]
		st.State, st.WaitingOn = delivery.ComponentStateHeld, waiting[name]
		held = append(held, st)
	}
	return promotable, held
}

// waitedOn is the components already converging towards the release they pin —
// what a pass waits for besides its own promotes.
//
// Behind, held and unbuilt components are deliberately absent. The stage has ONE
// deadline, and spending it on a binding this pass did not write, or on a
// component with no binding at all, would settle `deploy-budget` on a run whose
// only problem is work somebody still has to do.
func waitedOn(state delivery.VersionState) []string {
	var out []string
	for _, c := range state.Components {
		if c.State == delivery.ComponentStateConverging {
			out = append(out, c.Component)
		}
	}
	return out
}

// wavesFromEdges is the ordering itself: Kahn's algorithm over consumer ->
// providers, level by level.
//
// Split from the translation above so the graph's own behaviour — including the
// cycle refusal, which today's edge rule cannot produce and a future one might —
// is exercised on the edges directly rather than through a contrived design.
//
// Input order is preserved inside a wave so the plan is stable across attempts;
// a deploy order that reshuffles between retries is needlessly hard to read in a
// log.
func wavesFromEdges(components []string, providers map[string][]string) ([][]string, error) {
	remaining := make(map[string]int, len(components))
	for _, name := range components {
		remaining[name] = 0
	}
	dependents := make(map[string][]string, len(components))
	for consumer, deps := range providers {
		for _, p := range deps {
			dependents[p] = append(dependents[p], consumer)
			remaining[consumer]++
		}
	}

	var waves [][]string
	placed := 0
	for placed < len(components) {
		var wave []string
		for _, name := range components {
			if n, ok := remaining[name]; ok && n == 0 {
				wave = append(wave, name)
			}
		}
		if len(wave) == 0 {
			return nil, fmt.Errorf("%w: hard dependency cycle among components %s",
				delivery.ErrDeployPermanent, describeCycle(remaining, providers))
		}
		for _, name := range wave {
			delete(remaining, name)
			placed++
		}
		for _, name := range wave {
			for _, dep := range dependents[name] {
				if _, ok := remaining[dep]; ok {
					remaining[dep]--
				}
			}
		}
		waves = append(waves, wave)
	}
	return waves, nil
}

// describeCycle names the components still waiting on each other, with the edges
// that hold them, so the error says what to change rather than merely that
// something is wrong.
func describeCycle(remaining map[string]int, providers map[string][]string) string {
	stuck := make([]string, 0, len(remaining))
	for name := range remaining {
		stuck = append(stuck, name)
	}
	sort.Strings(stuck)

	parts := make([]string, 0, len(stuck))
	for _, name := range stuck {
		waiting := make([]string, 0, len(providers[name]))
		for _, p := range providers[name] {
			if _, ok := remaining[p]; ok {
				waiting = append(waiting, p)
			}
		}
		sort.Strings(waiting)
		parts = append(parts, fmt.Sprintf("%s needs [%s]", name, strings.Join(waiting, " ")))
	}
	return strings.Join(parts, "; ")
}
