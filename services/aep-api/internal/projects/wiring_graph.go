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

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// deploymentWaves plans one reconcile pass: which components to promote, in
// which order, which to wait on, and which to leave alone (ADR-0026).
//
// WAVES, because every component in wave N has each of its providers in some
// earlier one. The stage waits for each wave to serve before starting the next,
// which is what makes a connection edge (spec.ConnectionEdges) mean what it
// says — the provider has a binding by the time the consumer's connection is
// resolved. Promoting the whole set at once instead publishes the consumer
// against a provider with no binding: OpenChoreo applies its pods without the
// provider's address and holds its Ready until the provider exists, which for
// a web app is a `/api` that answers 502 to anyone who visits during the wait.
//
// PROMOTABLE is the rule that replaced "everything the cycle built". A
// component is promoted when it is BEHIND and every provider is either
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
// A cycle among connections shares ONE wave. OpenChoreo publishes a component's
// own endpoint URLs before it checks its outbound connections, so two components
// that connect to each other converge once both have a binding; the wave's wait
// sees both resolve. Refusing the cycle would turn an ordinary shape (two
// services that call each other) into a permanent deploy failure. Soft edges are
// absent from this graph by construction and may cycle freely — CORS always does.
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

	// consumer -> providers, k8s-shaped, restricted to components the
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
	for consumer, deps := range spec.ConnectionEdges(design) {
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

	// The graph rides the plan: the readiness wait needs it at the deadline to
	// tell a consumer blocked behind an unmet provider from one whose own
	// connection cannot resolve.
	plan.Providers = providers
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
// component whose providers cannot be met, then drop the ones that were
// only waiting on THOSE, until nothing more falls out.
//
// One pass is not enough, and the case that proves it is a chain: a web app
// waiting on an api waiting on an unbuilt service. A single pass would hold the
// api and promote the web app into a config its api could not fill.
//
// A provider is met when it is SERVING or when it is being promoted in this
// pass. Any other state — unbuilt, held, or converging towards a release it has
// not reached — is a real unsatisfied edge: the stage's premise is that a
// provider's binding exists by the time the consumer's connection is resolved,
// and only a serving release proves that. A converging provider therefore costs its
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
				// serving, but it has no active release and therefore no binding
				// for this component's connection to resolve against.
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

// wavesFromEdges is the ordering itself: topological levels over consumer ->
// providers, with every strongly connected group collapsed into one node.
//
// A cycle is not refused. OpenChoreo publishes a component's own endpoint URLs
// before it checks its outbound connections, so two components that connect to
// each other converge once both have a binding: they share a wave, and the
// wave's wait sees both resolve. Refusing it would make an ordinary shape (two
// services that call each other) a permanent deploy failure — which the rule
// did while it covered web apps alone and could not meet a cycle.
//
// Split from the translation above so the graph's own behaviour is exercised on
// the edges directly rather than through a contrived design.
//
// Input order is preserved inside a wave so the plan is stable across attempts;
// a deploy order that reshuffles between retries is needlessly hard to read in a
// log.
func wavesFromEdges(components []string, providers map[string][]string) ([][]string, error) {
	group := stronglyConnected(components, providers)

	// Level the GROUPS: a group's providers are the groups its members' providers
	// fall in, itself excepted. Groups are numbered in first-appearance order so
	// the levelling below walks them in input order.
	var groups []int
	remaining := map[int]int{}
	for _, name := range components {
		if _, seen := remaining[group[name]]; !seen {
			groups = append(groups, group[name])
			remaining[group[name]] = 0
		}
	}
	dependents := map[int][]int{}
	counted := map[[2]int]bool{}
	for _, consumer := range components {
		for _, p := range providers[consumer] {
			pg, ok := group[p]
			if !ok || pg == group[consumer] {
				continue
			}
			edge := [2]int{group[consumer], pg}
			if counted[edge] {
				continue
			}
			counted[edge] = true
			dependents[pg] = append(dependents[pg], group[consumer])
			remaining[group[consumer]]++
		}
	}

	var waves [][]string
	placed := map[int]bool{}
	for len(placed) < len(groups) {
		level := map[int]bool{}
		for _, g := range groups {
			if !placed[g] && remaining[g] == 0 {
				level[g] = true
			}
		}
		if len(level) == 0 {
			// Unreachable: the group graph is acyclic by construction. Loud rather
			// than a plan missing components — a silent partial plan is the one
			// failure this stage must never produce — and permanent, because
			// retrying the same graph levels the same way.
			var stuck []string
			for _, name := range components {
				if !placed[group[name]] {
					stuck = append(stuck, name)
				}
			}
			return nil, fmt.Errorf("%w: deploy order could not be levelled among %v",
				delivery.ErrDeployPermanent, stuck)
		}
		var wave []string
		for _, name := range components {
			if level[group[name]] {
				wave = append(wave, name)
			}
		}
		for g := range level {
			placed[g] = true
			for _, dep := range dependents[g] {
				remaining[dep]--
			}
		}
		waves = append(waves, wave)
	}
	return waves, nil
}

// stronglyConnected labels each component with its strongly connected group
// over consumer -> providers (Tarjan's algorithm). Edges to a component outside
// the list are ignored: a provider that is not in the promoted set is not being
// ordered by this pass.
func stronglyConnected(components []string, providers map[string][]string) map[string]int {
	in := make(map[string]bool, len(components))
	for _, name := range components {
		in[name] = true
	}
	index := map[string]int{}
	low := map[string]int{}
	onStack := map[string]bool{}
	var stack []string
	group := make(map[string]int, len(components))
	next, label := 0, 0
	var visit func(string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range providers[v] {
			if !in[w] {
				continue
			}
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			group[w] = label
			if w == v {
				break
			}
		}
		label++
	}
	for _, name := range components {
		if _, seen := index[name]; !seen {
			visit(name)
		}
	}
	return group
}
