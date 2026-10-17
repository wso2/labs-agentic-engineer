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

package spec

// Wiring edges, and what the deploy order turns on.
//
// A component's design dependencies describe what it TALKS TO. For a sibling
// component that is also what its workload CONNECTS TO: the platform derives one
// endpoint connection per sibling dependency (derive_wiring.go,
// siblingEndpointWiring), and OpenChoreo resolves that connection only once the
// provider has a ReleaseBinding with a resolved endpoint. Until then the
// consumer's own binding reports ConnectionsPending and is never Ready; its
// pods run without the provider's address, and a web app's nginx answers `/api`
// with 502 (the react-webapp skill's proxy drop-in).
//
//	CONNECTION  consumer → the sibling it connects to. ORDERS the deploy: the
//	            provider is promoted first, or the consumer is held while the
//	            provider has no serving release. A cycle among connections
//	            shares one wave — OpenChoreo publishes a component's own
//	            endpoint URLs before it checks its outbound connections, so two
//	            components that connect to each other converge once both have a
//	            binding.
//
//	SOFT        the fact flows the other way — a provider learning about its
//	            consumer. An OIDC resource needs the SPA's callback URL
//	            registered. That is not needed before the consumer serves, only
//	            before a later login. Soft edges order NOTHING; one converge
//	            pass at the end writes them.
//
// The rule used to be narrower: only a web app's sibling SERVICES ordered the
// deploy, on the premise that a service reaching a sibling "resolves it through
// OpenChoreo's own connection mechanism, so the platform stamps nothing and has
// no reason to serialise the two". The first half is true of every sibling
// dependency, and the conclusion was wrong for all of them: the connection is
// exactly what OpenChoreo waits on, whatever the consumer's type. An ai-agent
// promoted ahead of the api it connects to sat ConnectionsPending for the whole
// deploy deadline and was filed as a deployment failure (allocation-app-1,
// 2026-10-12).

// ConnectionEdges returns each component's providers: the sibling components
// its workload connects to, keyed by consumer name, in declaration order. Both
// sides are DESIGN names (the caller maps to k8s names if it addresses
// OpenChoreo with them).
//
// It is the single authority for that rule. projects orders the deploy by these
// edges so a consumer is not published before the provider its connection
// resolves to. A second spelling would let the writer and the scheduler
// disagree about which component blocks which — so the predicate is the one
// derive_wiring uses for the connection itself, a sibling dependency whose
// target is in the design, less a self-reference: the derivation would emit a
// connection for one, and it orders nothing.
//
// Components named as dependencies but absent from the design are skipped: an
// edge to a component that does not exist cannot be waited for, and the
// derivation emits no connection for it either.
func ConnectionEdges(design *DesignFile) map[string][]string {
	if design == nil {
		return nil
	}
	byName := componentsByName(design)
	out := make(map[string][]string, len(design.Components))
	for _, c := range design.Components {
		if providers := providersOf(c, byName); len(providers) > 0 {
			out[c.Name] = providers
		}
	}
	return out
}

func componentsByName(design *DesignFile) map[string]DesignComponent {
	out := make(map[string]DesignComponent, len(design.Components))
	for _, c := range design.Components {
		out[c.Name] = c
	}
	return out
}

// providersOf lists one component's providers in declaration order: every
// sibling dependency that names a component in the design, itself excepted.
func providersOf(c DesignComponent, byName map[string]DesignComponent) []string {
	var out []string
	for _, dep := range c.ComponentDependsOn() {
		if dep == c.Name {
			continue
		}
		if _, ok := byName[dep]; !ok {
			continue
		}
		out = append(out, dep)
	}
	return out
}
