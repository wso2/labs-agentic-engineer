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
//	      configuration. A web app's nginx reverse-proxy is the case that
//	      exists: OpenChoreo injects the sibling Service URL as pod env
//	      `<DEP>_URL`, and `/api` 502s until that Service exists. Hard edges
//	      ORDER the deploy, and they must form a DAG.
//
//	SOFT  the fact flows the other way — a provider learning about its consumer.
//	      An OIDC resource needs the SPA's callback URL registered. That is not
//	      needed before the consumer serves, only before a later login. Soft
//	      edges order NOTHING and are free to cycle.
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
// It is the single authority for that rule. projects orders the deploy by
// these edges so a SPA is not published before the sibling Service nginx
// will proxy to. A second spelling would let the writer and the scheduler
// disagree about which component blocks which.
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

func componentsByName(design *DesignFile) map[string]DesignComponent {
	out := make(map[string]DesignComponent, len(design.Components))
	for _, c := range design.Components {
		out[c.Name] = c
	}
	return out
}

// hardProvidersOf lists one component's hard providers in declaration order.
//
// Today the only consumer that needs a sibling address at start-up is a web
// app: nginx reverse-proxies `/api` to that sibling's Service URL. A peer web
// app is not called over HTTP, so its URL is nothing to wait for. When a second
// stamped-config shape appears (a worker handed a queue's address, say) it is
// added here, and the deploy order follows it.
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
