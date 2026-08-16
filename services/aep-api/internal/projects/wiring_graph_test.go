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
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

func designWith(components ...spec.DesignComponent) *spec.DesignFile {
	return &spec.DesignFile{Components: components}
}

func wiringWebApp(name string, deps ...string) spec.DesignComponent {
	return spec.DesignComponent{Name: name, ComponentType: spec.ComponentTypeWebApplication,
		Dependencies: wiringDeps(deps)}
}

func wiringService(name string, deps ...string) spec.DesignComponent {
	return spec.DesignComponent{Name: name, ComponentType: spec.ComponentTypeService,
		Dependencies: wiringDeps(deps)}
}

func wiringDeps(names []string) []spec.Dependency {
	out := make([]spec.Dependency, 0, len(names))
	for _, n := range names {
		out = append(out, contracts.Dependency{Name: n, Kind: spec.DependencyKindComponent})
	}
	return out
}

func TestDeploymentWaves(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		design     *spec.DesignFile
		components []string
		want       [][]string
	}{{
		// The case that blanked a page: promoted together, the SPA composes its
		// config while its backend has no address. The provider goes first.
		name:       "a SPA waits for the backend whose address it carries",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		components: []string{"todo-api", "todo-webapp"},
		want:       [][]string{{"todo-api"}, {"todo-webapp"}},
	}, {
		// Order in the SET must not decide the order of the DEPLOY, or the plan
		// would depend on however the build fan-out happened to list things.
		name:       "the consumer listed first still deploys second",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		components: []string{"todo-webapp", "todo-api"},
		want:       [][]string{{"todo-api"}, {"todo-webapp"}},
	}, {
		name:       "no hard edges is one wave — the ordinary case pays nothing",
		design:     designWith(wiringService("orders"), wiringService("payments")),
		components: []string{"orders", "payments"},
		want:       [][]string{{"orders", "payments"}},
	}, {
		// A provider outside the set is already deployed, so its address already
		// exists. Waiting on it would be waiting for something that has happened.
		name:       "a provider outside the deploy set does not split the wave",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		components: []string{"todo-webapp"},
		want:       [][]string{{"todo-webapp"}},
	}, {
		name: "independent components share a wave; only the dependent one waits",
		design: designWith(wiringWebApp("web", "api"), wiringService("api"),
			wiringService("worker")),
		components: []string{"api", "web", "worker"},
		want:       [][]string{{"api", "worker"}, {"web"}},
	}, {
		name:       "no design is one wave rather than a refusal",
		design:     nil,
		components: []string{"api", "web"},
		want:       [][]string{{"api", "web"}},
	}, {
		name:       "an empty set plans nothing",
		design:     designWith(wiringService("api")),
		components: nil,
		want:       nil,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := deploymentWaves(tc.design, tc.components)
			if err != nil {
				t.Fatalf("deploymentWaves: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("deploymentWaves() = %v, want %v", got, tc.want)
			}
		})
	}
}

// A design names components as the architect wrote them; the run loop only ever
// carries what OpenChoreo is addressed by. An order computed against the raw
// names finds no edges at all and puts everything in wave one — the exact
// behaviour the waves replace, reached through a naming mismatch instead.
func TestDeploymentWaves_MatchesComponentsByK8sName(t *testing.T) {
	t.Parallel()
	design := designWith(
		spec.DesignComponent{Name: "Todo Web App", ComponentType: spec.ComponentTypeWebApplication,
			Dependencies: wiringDeps([]string{"Todo API"})},
		spec.DesignComponent{Name: "Todo API", ComponentType: spec.ComponentTypeService},
	)
	got, err := deploymentWaves(design, []string{"todo-web-app", "todo-api"})
	if err != nil {
		t.Fatalf("deploymentWaves: %v", err)
	}
	want := [][]string{{"todo-api"}, {"todo-web-app"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("deploymentWaves() = %v, want %v", got, want)
	}
}

// The service method, over the REAL design read — the pure graph above never
// touches a store, so nothing else covers the parse or the no-design fallback.
func TestPlanDeploymentWaves_OrdersFromTheStoredDesign(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": plainServiceMd("api"),
		"components/web/design.json": webAppDependingOn("web", "api"),
	}
	svc := NewDeploymentService(&mocks.ComponentClientMock{}, traitStoreWith(files))

	got, err := svc.PlanDeploymentWaves(context.Background(), "acme", "proj", []string{"web", "api"})
	if err != nil {
		t.Fatalf("PlanDeploymentWaves: %v", err)
	}
	want := [][]string{{"api"}, {"web"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlanDeploymentWaves() = %v, want %v — the SPA must wait for the backend it carries", got, want)
	}
}

// A project with no design yet deploys rather than refusing: the design is the
// ordering input, not the deploy's precondition. Deploy answers a missing design
// the same way, and the two must not disagree about it.
func TestPlanDeploymentWaves_NoDesignIsOneWave(t *testing.T) {
	t.Parallel()
	svc := NewDeploymentService(&mocks.ComponentClientMock{},
		spec.NewArtifactStore(&artifactstest.FakeArtifactService{
			ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
				return nil, spec.ErrArtifactNotFound
			},
		}))

	got, err := svc.PlanDeploymentWaves(context.Background(), "acme", "proj", []string{"api", "web"})
	if err != nil {
		t.Fatalf("PlanDeploymentWaves with no design: %v", err)
	}
	want := [][]string{{"api", "web"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlanDeploymentWaves() = %v, want %v", got, want)
	}
}

// webAppDependingOn renders a web-application design.json that declares a
// sibling-component dependency — the shape the hard edge is read off.
func webAppDependingOn(name, dep string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"web-application\",\n  \"description\": \"SPA.\",\n" +
		"  \"dependencies\": [{\"kind\": \"component\", \"name\": \"" + dep + "\"}]\n}\n"
}

// Two components that each need the other's address before starting cannot both
// go first. Retrying an unsatisfiable order forever is the failure mode this
// closes: the refusal comes back permanent, so the supervisor files it on the
// first attempt with the edges named instead of waiting out a deadline.
//
// Asserted on the edges rather than through a design, because today's edge rule
// (a web app's sibling services) cannot produce a cycle — a component has one
// type. The refusal is the graph's own invariant and has to hold for whatever
// edge kind is added next.
func TestWavesFromEdges_HardCycleIsPermanent(t *testing.T) {
	t.Parallel()
	_, err := wavesFromEdges([]string{"a", "b", "loner"}, map[string][]string{
		"a": {"b"},
		"b": {"a"},
	})
	if err == nil {
		t.Fatal("a hard dependency cycle was accepted; it can never be satisfied")
	}
	if !errors.Is(err, delivery.ErrDeployPermanent) {
		t.Errorf("cycle error is not permanent, so it would be retried forever: %v", err)
	}
	for _, want := range []string{"a needs [b]", "b needs [a]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not name the edge %q that holds the cycle: %v", want, err)
		}
	}
	// The component that was free to go is not blamed for the pair that was not.
	if strings.Contains(err.Error(), "loner") {
		t.Errorf("a component with no unmet edge is named in the cycle: %v", err)
	}
}
