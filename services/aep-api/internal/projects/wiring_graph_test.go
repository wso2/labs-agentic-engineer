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

// The version states the plan is computed over. A behind component's desired
// commit is derived from its name so a wrong pairing is visible in a failure
// message rather than being a plausible-looking sha.
func wiringBehind(name string) delivery.ComponentState {
	return delivery.ComponentState{
		Component: name, State: delivery.ComponentStateBehind, DesiredSHA: "sha-" + name,
	}
}

func wiringServing(name string) delivery.ComponentState {
	return delivery.ComponentState{
		Component: name, State: delivery.ComponentStateServing,
		DesiredSHA: "sha-" + name, Pinned: "release-" + name, Ready: true,
	}
}

// wiringWithdrawn is a component somebody undeployed on purpose: it reads as
// serving (the version owes it nothing) but offers no address.
func wiringWithdrawn(name string) delivery.ComponentState {
	st := wiringServing(name)
	st.Undeploy = true
	return st
}

func wiringUnbuilt(name string) delivery.ComponentState {
	return delivery.ComponentState{Component: name, State: delivery.ComponentStateUnbuilt}
}

func wiringConverging(name string) delivery.ComponentState {
	return delivery.ComponentState{
		Component: name, State: delivery.ComponentStateConverging,
		DesiredSHA: "sha-" + name, Pinned: "release-" + name,
	}
}

func versionOf(states ...delivery.ComponentState) delivery.VersionState {
	return delivery.VersionState{Components: states}
}

// waveNames renders a plan's waves as names, and asserts in passing that every
// target carries its OWN commit — the whole point of the target type.
func waveNames(t *testing.T, plan delivery.DeployPlan) [][]string {
	t.Helper()
	var out [][]string
	for _, wave := range plan.Waves {
		names := make([]string, 0, len(wave))
		for _, target := range wave {
			if target.CommitSHA != "sha-"+target.Component {
				t.Errorf("target %q promotes at %q, not its own newest green build",
					target.Component, target.CommitSHA)
			}
			names = append(names, target.Component)
		}
		out = append(out, names)
	}
	return out
}

// heldOn renders the held set as `component→providers` pairs.
func heldOn(plan delivery.DeployPlan) map[string][]string {
	if len(plan.Held) == 0 {
		return nil
	}
	out := make(map[string][]string, len(plan.Held))
	for _, c := range plan.Held {
		out[c.Component] = c.WaitingOn
	}
	return out
}

func TestDeploymentWaves(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		design     *spec.DesignFile
		version    delivery.VersionState
		want       [][]string
		wantHeld   map[string][]string
		wantWaited []string
	}{{
		// The case that blanked a page: promoted together, the SPA composes its
		// config while its backend has no address. The provider goes first.
		name:       "a SPA waits for the backend whose address it carries",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		version:    versionOf(wiringBehind("todo-api"), wiringBehind("todo-webapp")),
		want:       [][]string{{"todo-api"}, {"todo-webapp"}},
		wantWaited: []string{"todo-api", "todo-webapp"},
	}, {
		// Order in the STATE must not decide the order of the DEPLOY, or the plan
		// would depend on however the read happened to list things.
		name:       "the consumer listed first still deploys second",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		version:    versionOf(wiringBehind("todo-webapp"), wiringBehind("todo-api")),
		want:       [][]string{{"todo-api"}, {"todo-webapp"}},
		wantWaited: []string{"todo-api", "todo-webapp"},
	}, {
		name:       "no hard edges is one wave — the ordinary case pays nothing",
		design:     designWith(wiringService("orders"), wiringService("payments")),
		version:    versionOf(wiringBehind("orders"), wiringBehind("payments")),
		want:       [][]string{{"orders", "payments"}},
		wantWaited: []string{"orders", "payments"},
	}, {
		// A provider that is already SERVING has an address, so waiting on it
		// would be waiting for something that has happened.
		name:       "a serving provider does not split the wave",
		design:     designWith(wiringWebApp("todo-webapp", "todo-api"), wiringService("todo-api")),
		version:    versionOf(wiringServing("todo-api"), wiringBehind("todo-webapp")),
		want:       [][]string{{"todo-webapp"}},
		wantWaited: []string{"todo-webapp"},
	}, {
		name: "independent components share a wave; only the dependent one waits",
		design: designWith(wiringWebApp("web", "api"), wiringService("api"),
			wiringService("worker")),
		version:    versionOf(wiringBehind("api"), wiringBehind("web"), wiringBehind("worker")),
		want:       [][]string{{"api", "worker"}, {"web"}},
		wantWaited: []string{"api", "web", "worker"},
	}, {
		name:       "no design is one wave rather than a refusal",
		design:     nil,
		version:    versionOf(wiringBehind("api"), wiringBehind("web")),
		want:       [][]string{{"api", "web"}},
		wantWaited: []string{"api", "web"},
	}, {
		name:    "an empty state plans nothing",
		design:  designWith(wiringService("api")),
		version: delivery.VersionState{},
	}, {
		// THE INCIDENT. The webapp's build went red, the api's went green: the api
		// is promoted on its own, in the same cycle, rather than waiting for a
		// later cycle whose diff would never mention it again.
		name:       "a green provider is promoted while its consumer is unbuilt",
		design:     designWith(wiringWebApp("onboarding-webapp", "onboarding-api"), wiringService("onboarding-api")),
		version:    versionOf(wiringBehind("onboarding-api"), wiringUnbuilt("onboarding-webapp")),
		want:       [][]string{{"onboarding-api"}},
		wantWaited: []string{"onboarding-api"},
	}, {
		// THE OBJECTION, and the answer: A needs B, A is green, B is red. A is
		// HELD — not promoted, not waited on, not a deploy failure — because a
		// SPA published against an api with no address is a blank page.
		name:     "a green consumer is held when its provider is unbuilt",
		design:   designWith(wiringWebApp("web", "api"), wiringService("api")),
		version:  versionOf(wiringBehind("web"), wiringUnbuilt("api")),
		wantHeld: map[string][]string{"web": {"api"}},
	}, {
		// A provider still rolling out has a binding but has not been observed
		// serving, and the wave rule's whole premise is that a provider serves
		// before its consumer is composed. The consumer costs one cycle, which is
		// the conservative direction; the provider is waited on either way.
		name:       "a converging provider holds its consumer and is waited on",
		design:     designWith(wiringWebApp("web", "api"), wiringService("api")),
		version:    versionOf(wiringConverging("api"), wiringBehind("web")),
		wantHeld:   map[string][]string{"web": {"api"}},
		wantWaited: []string{"api"},
	}, {
		// A provider somebody UNDEPLOYED is not a satisfied hard edge. It reads
		// as serving — nothing is owed and nothing may promote over the decision
		// — but it has no active release, so it has no address for the web app's
		// start-up config to carry. Promoting the consumer anyway is the blank
		// page, reached through the one state that says "serving" and means
		// "nothing is running".
		name:     "an undeployed provider does not satisfy its consumer",
		design:   designWith(wiringWebApp("web", "api"), wiringService("api")),
		version:  versionOf(wiringWithdrawn("api"), wiringBehind("web")),
		wantHeld: map[string][]string{"web": {"api"}},
	}, {
		// The idempotence that lets this run on every cycle: nothing behind means
		// nothing written and nothing waited on.
		name:    "a fully serving version plans no writes at all",
		design:  designWith(wiringWebApp("web", "api"), wiringService("api")),
		version: versionOf(wiringServing("api"), wiringServing("web")),
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := deploymentWaves(tc.design, tc.version)
			if err != nil {
				t.Fatalf("deploymentWaves: %v", err)
			}
			if got := waveNames(t, plan); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("waves = %v, want %v", got, tc.want)
			}
			if got := heldOn(plan); !reflect.DeepEqual(got, tc.wantHeld) {
				t.Errorf("held = %v, want %v", got, tc.wantHeld)
			}
			if !reflect.DeepEqual(plan.Waited, tc.wantWaited) {
				t.Errorf("waited = %v, want %v", plan.Waited, tc.wantWaited)
			}
		})
	}
}

// One pass of the promotable rule is not enough, and this is the case that
// proves it: a consumer waiting on a provider that is ITSELF held has to fall
// out too, or it ships against a config its provider could not fill.
//
// Asserted on the edges rather than through a design, for the same reason the
// cycle refusal below is: today's edge rule gives hard providers only to a web
// app, and only for its sibling SERVICES, so a three-deep chain cannot be
// expressed as a design at all. The fixpoint is the graph's own invariant and
// has to hold for whatever edge kind is added next.
func TestPromotableSet_AHeldProviderHoldsItsOwnConsumer(t *testing.T) {
	t.Parallel()
	behind := []string{"web", "api"}
	providers := map[string][]string{"web": {"api"}, "api": {"worker"}}
	byName := map[string]delivery.ComponentState{
		"web": wiringBehind("web"), "api": wiringBehind("api"), "worker": wiringUnbuilt("worker"),
	}

	promotable, held := promotableSet(behind, providers, byName)
	if len(promotable) != 0 {
		t.Errorf("promoted %v; a consumer whose provider is held must not be written", promotable)
	}
	got := map[string][]string{}
	for _, c := range held {
		if c.State != delivery.ComponentStateHeld {
			t.Errorf("%s is in the held set with state %q", c.Component, c.State)
		}
		got[c.Component] = c.WaitingOn
	}
	want := map[string][]string{"web": {"api"}, "api": {"worker"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("held = %v, want %v", got, want)
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
	plan, err := deploymentWaves(design, versionOf(wiringBehind("todo-web-app"), wiringBehind("todo-api")))
	if err != nil {
		t.Fatalf("deploymentWaves: %v", err)
	}
	want := [][]string{{"todo-api"}, {"todo-web-app"}}
	if got := waveNames(t, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("waves = %v, want %v", got, want)
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

	plan, err := svc.PlanDeploymentWaves(context.Background(), "acme", "proj",
		versionOf(wiringBehind("web"), wiringBehind("api")))
	if err != nil {
		t.Fatalf("PlanDeploymentWaves: %v", err)
	}
	want := [][]string{{"api"}, {"web"}}
	if got := waveNames(t, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("waves = %v, want %v — the SPA must wait for the backend it carries", got, want)
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

	plan, err := svc.PlanDeploymentWaves(context.Background(), "acme", "proj",
		versionOf(wiringBehind("api"), wiringBehind("web")))
	if err != nil {
		t.Fatalf("PlanDeploymentWaves with no design: %v", err)
	}
	want := [][]string{{"api", "web"}}
	if got := waveNames(t, plan); !reflect.DeepEqual(got, want) {
		t.Errorf("waves = %v, want %v", got, want)
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
