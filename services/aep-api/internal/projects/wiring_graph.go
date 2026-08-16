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

// deploymentWaves orders one deploy set into the levels it can be promoted in:
// every component in wave N has each of its hard providers in some earlier wave.
//
// The deploy stage walks these in order and waits for each wave to serve before
// starting the next, which is what makes a hard edge (spec.HardConfigEdges) mean
// what it says — the provider has an address by the time the consumer's config
// is composed. Promoting the whole set at once instead publishes the consumer
// with a config nothing could have filled, which for a web app is a blank page
// served to anyone who visits during the repair.
//
// Only edges INSIDE the set constrain anything. A provider that is not being
// deployed by this cycle is already deployed — its address exists — so waiting
// on it would be waiting on something that has already happened. This is what
// keeps the ordinary case (a cycle that touched one component) a single wave.
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
func deploymentWaves(design *spec.DesignFile, components []string) ([][]string, error) {
	if len(components) == 0 {
		return nil, nil
	}
	if design == nil {
		// No design to order by. One wave preserves today's behaviour rather than
		// refusing to deploy: the design is the ordering input, not the deploy's.
		return [][]string{components}, nil
	}

	inSet := make(map[string]struct{}, len(components))
	for _, name := range components {
		inSet[name] = struct{}{}
	}

	// consumer -> providers, both k8s-shaped and both known to be in the set.
	// Translating here is what makes the design graph and the deploy set talk
	// about the same components: the design names them as the architect wrote
	// them, the run loop carries what OpenChoreo is addressed by, and an untranslated
	// comparison would quietly find no edges at all — every component in wave one,
	// which is the exact behaviour the waves exist to replace.
	edges := make(map[string][]string, len(components))
	for consumer, deps := range spec.HardConfigEdges(design) {
		c := k8sname.ToK8sName(consumer)
		if _, ok := inSet[c]; !ok {
			continue
		}
		for _, dep := range deps {
			p := k8sname.ToK8sName(dep)
			if _, ok := inSet[p]; !ok || p == c {
				continue
			}
			edges[c] = append(edges[c], p)
		}
	}
	return wavesFromEdges(components, edges)
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
