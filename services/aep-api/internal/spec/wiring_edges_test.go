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

import (
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

func edgeWebApp(name string, deps ...string) DesignComponent {
	return DesignComponent{Name: name, ComponentType: ComponentTypeWebApplication, Dependencies: edgeDeps(deps)}
}

func edgeService(name string, deps ...string) DesignComponent {
	return DesignComponent{Name: name, ComponentType: ComponentTypeService, Dependencies: edgeDeps(deps)}
}

func edgeAgent(name string, deps ...string) DesignComponent {
	return DesignComponent{Name: name, ComponentType: ComponentTypeAIAgent, Dependencies: edgeDeps(deps)}
}

func edgeDeps(names []string) []Dependency {
	out := make([]Dependency, 0, len(names))
	for _, n := range names {
		out = append(out, contracts.Dependency{Name: n, Kind: DependencyKindComponent})
	}
	return out
}

func TestConnectionEdges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		design *DesignFile
		want   map[string][]string
	}{{
		name:   "a web app's sibling service is an edge — nginx proxies /api to its Service URL",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("shop-web", "shop-api"), edgeService("shop-api")}},
		want:   map[string][]string{"shop-web": {"shop-api"}},
	}, {
		// The edge the deploy USED to leave unordered. A service reaching a
		// sibling service is a workload connection like any other, and OpenChoreo
		// keeps the consumer's binding ConnectionsPending until the provider has
		// one — so the provider goes first, or the consumer is held.
		name:   "service to service is an edge — OpenChoreo waits on the connection",
		design: &DesignFile{Components: []DesignComponent{edgeService("orders", "payments"), edgeService("payments")}},
		want:   map[string][]string{"orders": {"payments"}},
	}, {
		// The incident: an agent promoted ahead of the api it connects to sat
		// ConnectionsPending for the whole deploy deadline and was filed as a
		// deployment failure.
		name:   "an ai-agent's sibling api is an edge, like any consumer's",
		design: &DesignFile{Components: []DesignComponent{edgeAgent("skill-agent", "allocation-api"), edgeService("allocation-api")}},
		want:   map[string][]string{"skill-agent": {"allocation-api"}},
	}, {
		// A dependency on a peer web app derives a connection to it too, so the
		// binding waits on it the same way; whether the dependency makes sense is
		// the design's question, not the scheduler's.
		name:   "a peer web app is an edge — the workload connects to it like any sibling",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("admin-web", "shop-web"), edgeWebApp("shop-web")}},
		want:   map[string][]string{"admin-web": {"shop-web"}},
	}, {
		name: "a web app with several backends waits for all of them, in declaration order",
		design: &DesignFile{Components: []DesignComponent{
			edgeWebApp("web", "orders", "catalog"), edgeService("catalog"), edgeService("orders"),
		}},
		want: map[string][]string{"web": {"orders", "catalog"}},
	}, {
		// Both directions are edges. The planner, not this rule, decides that a
		// cycle shares one wave rather than being refused.
		name: "two services that connect to each other are two edges",
		design: &DesignFile{Components: []DesignComponent{
			edgeService("orders", "payments"), edgeService("payments", "orders"),
		}},
		want: map[string][]string{"orders": {"payments"}, "payments": {"orders"}},
	}, {
		// A dangling dependency cannot be waited for: there is no component to
		// deploy first, so treating it as an edge would deadlock the order. The
		// wiring derivation emits no connection for it either.
		name:   "a dependency absent from the design is not an edge",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("web", "ghost")}},
		want:   map[string][]string{},
	}, {
		name:   "a component naming itself is not its own provider",
		design: &DesignFile{Components: []DesignComponent{edgeService("loop", "loop")}},
		want:   map[string][]string{},
	}, {
		name:   "no design, no edges",
		design: nil,
		want:   nil,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ConnectionEdges(tc.design)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("ConnectionEdges(nil) = %v, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ConnectionEdges() = %v, want %v", got, tc.want)
			}
		})
	}
}
