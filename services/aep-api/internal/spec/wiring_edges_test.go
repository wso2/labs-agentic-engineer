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

func edgeDeps(names []string) []Dependency {
	out := make([]Dependency, 0, len(names))
	for _, n := range names {
		out = append(out, contracts.Dependency{Name: n, Kind: DependencyKindComponent})
	}
	return out
}

func TestHardConfigEdges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		design *DesignFile
		want   map[string][]string
	}{{
		name:   "a web app's sibling service is hard — nginx needs its Service URL",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("shop-web", "shop-api"), edgeService("shop-api")}},
		want:   map[string][]string{"shop-web": {"shop-api"}},
	}, {
		// The edge the deploy must NOT order. A service reaching a sibling service
		// is resolved by OpenChoreo's own connection mechanism, so nothing is
		// stamped and nothing has to go first — and ordering it would refuse two
		// services that call each other, which is an ordinary shape.
		name:   "service to service is not hard",
		design: &DesignFile{Components: []DesignComponent{edgeService("orders", "payments"), edgeService("payments")}},
		want:   map[string][]string{},
	}, {
		name:   "a peer web app is not hard — a SPA does not call another SPA over HTTP",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("admin-web", "shop-web"), edgeWebApp("shop-web")}},
		want:   map[string][]string{},
	}, {
		name: "a web app with several backends waits for all of them, in declaration order",
		design: &DesignFile{Components: []DesignComponent{
			edgeWebApp("web", "orders", "catalog"), edgeService("catalog"), edgeService("orders"),
		}},
		want: map[string][]string{"web": {"orders", "catalog"}},
	}, {
		// A dangling dependency cannot be waited for: there is no component to
		// deploy first, so treating it as an edge would deadlock the order.
		name:   "a dependency absent from the design is not an edge",
		design: &DesignFile{Components: []DesignComponent{edgeWebApp("web", "ghost")}},
		want:   map[string][]string{},
	}, {
		name:   "no design, no edges",
		design: nil,
		want:   nil,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := HardConfigEdges(tc.design)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("HardConfigEdges(nil) = %v, want nil", got)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("HardConfigEdges() = %v, want %v", got, tc.want)
			}
		})
	}
}
