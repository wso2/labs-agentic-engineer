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

// Wiring edges, and the one distinction the deploy order turns on.
//
// A component's design dependencies describe what it TALKS TO. Deployment cares
// about something narrower: which of those facts the platform has to know before
// the component can serve its first byte, versus which it can supply afterwards.
//
//	HARD  the provider's address is stamped into the consumer's own start-up
//	      configuration. A web app's `window._env_` is the case that exists: the
//	      bundle reads API_BASE_URL at module load and throws without it, so a
//	      SPA published before its backend has an address is a blank page. Hard
//	      edges ORDER the deploy, and they must form a DAG.
//
//	SOFT  the fact flows the other way — a provider learning about its consumer.
//	      A protected API's CORS allowlist is the project's SPA origins; an OIDC
//	      resource needs the SPA's callback URL registered. Neither is needed
//	      before the consumer serves, only before a later browser call or login.
//	      Soft edges order NOTHING and are free to cycle, which is exactly what
//	      they do: the SPA needs the API's address, and the API needs the SPA's.
//
// Collapsing the two is what makes the wiring look circular and therefore
// unsolvable. Separated, the hard half is a DAG the deploy can walk in waves and
// the soft half is one converge pass afterwards.
//
// A hard edge is deliberately NOT "any dependency": a service reaching a sibling
// service resolves it through OpenChoreo's own connection mechanism at render
// time, so the platform stamps nothing and has no reason to serialise the two.
// Ordering those would refuse a legal design — two services that call each other
// is an ordinary shape — for no gain. The rule is precisely: the platform stamps
// the address, therefore the platform must know it first.

// HardConfigEdges returns each component's hard providers: the sibling
// components whose address this component cannot start without, keyed by
// consumer name. Both sides are DESIGN names (the caller maps to k8s names if
// it addresses OpenChoreo with them).
//
// It is the single authority for that rule. runtimeconfig composes the file
// these edges exist for, and projects orders the deploy by them; a second
// spelling would let the writer and the scheduler disagree about which component
// blocks which — the failure mode being a SPA the scheduler thinks is
// independent, published with a config the composer could not fill.
//
// Components named as dependencies but absent from the design are skipped: an
// edge to a component that does not exist cannot be waited for.
func HardConfigEdges(design *DesignFile) map[string][]string {
	if design == nil {
		return nil
	}
	byName := componentsByName(design)
	out := make(map[string][]string, len(design.Components))
	for _, c := range design.Components {
		if providers := hardProvidersOf(c, byName); len(providers) > 0 {
			out[c.Name] = providers
		}
	}
	return out
}

// HardProvidersFor is HardConfigEdges for ONE consumer — what the composer wants,
// since it is filling in a single component's file and has no use for the rest of
// the project's edges.
//
// Both spellings run the same rule (hardProvidersOf), which is the point: the
// composer that needs an address and the planner that orders around it must never
// be able to disagree about which addresses are needed.
func HardProvidersFor(design *DesignFile, componentName string) []string {
	if design == nil {
		return nil
	}
	for _, c := range design.Components {
		if c.Name == componentName {
			return hardProvidersOf(c, componentsByName(design))
		}
	}
	return nil
}

func componentsByName(design *DesignFile) map[string]DesignComponent {
	out := make(map[string]DesignComponent, len(design.Components))
	for _, c := range design.Components {
		out[c.Name] = c
	}
	return out
}

// hardProvidersOf lists one component's hard providers in declaration order.
//
// Today the only consumer that carries a stamped address is a web app, and the
// only providers whose address is stamped are its sibling SERVICES — a peer web
// app is not called over HTTP, so its URL is nothing to wait for. When a second
// stamped-config shape appears (a worker handed a queue's address, say) it is
// added here, and both the composer and the deploy order follow it at once.
func hardProvidersOf(c DesignComponent, byName map[string]DesignComponent) []string {
	if c.ComponentType != ComponentTypeWebApplication {
		return nil
	}
	var out []string
	for _, dep := range c.ComponentDependsOn() {
		sibling, ok := byName[dep]
		if !ok || sibling.ComponentType != ComponentTypeService {
			continue
		}
		out = append(out, dep)
	}
	return out
}
