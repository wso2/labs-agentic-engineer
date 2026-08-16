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

package provisioning

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// codingIssue builds a working-set issue as the plan tap writes one: prose plus
// the `aep` label, and nothing else.
func codingIssue(number int, title, state string, extra ...string) sourcecontrol.IssueInfo {
	return sourcecontrol.IssueInfo{
		Number: number,
		Title:  title,
		Body:   "Build it.",
		State:  state,
		Labels: append([]string{delivery.LabelAgentWork}, extra...),
	}
}

// wiringDesign is a two-component design: `web` consumes its sibling `orders`
// (an ENDPOINT dependency — the half this comment still carries), while `orders`
// consumes only a platform resource (whose wiring travels in design.json, so it
// must NOT appear here). That split is what makes both halves testable.
func wiringDesign() []spec.DesignComponent {
	return []spec.DesignComponent{
		{Name: "orders", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg",
				Description: "Managed PostgreSQL via CloudNativePG"},
		}},
		{Name: "web", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindComponent, Name: "orders"},
		}},
	}
}

// siblingResolved is a providers fake in which `web`'s sibling endpoint resolves.
func siblingResolved() fakeProviders {
	return fakeProviders{projectEP: map[string]openchoreo.WorkloadEndpointInfo{
		"proj-orders": {Component: "proj-orders", Name: "http"},
	}}
}

// publishFor builds a service over the given fakes and publishes the wiring once,
// as a cycle dispatch does.
func publishFor(t *testing.T, seed []sourcecontrol.IssueInfo, design []spec.DesignComponent, providers fakeProviders) *fakeIssues {
	t.Helper()
	issues := newFakeIssues(append([]sourcecontrol.IssueInfo{provisionGateIssue(11, "orders-db")}, seed...))
	svc := NewService(Deps{
		Issues: issues, Execs: &fakeExecStore{},
		Design: fakeDesign{comps: design}, Repos: fakeRepos{},
		ExtProv: &fakeExtProv{}, PlatProv: &fakePlatProv{},
		Bindings: &fakeBindings{}, Providers: providers,
	})
	svc.PublishResolvedWiring(context.Background(), "org", "proj")
	return issues
}

func wiringComments(f *fakeIssues, number int) []string {
	var out []string
	for _, c := range f.comments[number] {
		if strings.Contains(c, "Platform-resolved dependencies") {
			out = append(out, c)
		}
	}
	return out
}

// The trigger, and the reason this design exists: the wiring reaches the agent at
// CYCLE DISPATCH, where the dispatch predicate has already guaranteed no gate is
// open and the working set is non-empty. The comment carries the block for the
// component that consumes the endpoint, named, so the agent knows which
// workload.yaml it belongs in.
func TestWiring_PublishedAtDispatch(t *testing.T) {
	issues := publishFor(t, []sourcecontrol.IssueInfo{codingIssue(21, "Implement web", "open")},
		wiringDesign(), siblingResolved())

	posted := wiringComments(issues, 21)
	if len(posted) != 1 {
		t.Fatalf("want exactly one wiring comment on the working-set issue, got %d: %v", len(posted), posted)
	}
	body := posted[0]
	for _, want := range []string{
		"**Platform-resolved dependencies**",
		"## Component `web`",
		"```yaml\ndependencies:\n",
		"visibility: project",
		"address: ORDERS_URL",
		"### Consumed API contract — orders (local)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wiring comment missing %q:\n%s", want, body)
		}
	}
	// `orders` consumes no endpoint — it must not get a block, or the agent would
	// write a dependencies: into a workload.yaml that has no endpoint deps.
	if strings.Contains(body, "## Component `orders`") {
		t.Errorf("a component with no endpoint dependency must have no block:\n%s", body)
	}
}

// THE REGRESSION GUARD. Gate resolution must no longer post: its audience was
// whatever working-set issues happened to exist at that instant, and on a first
// build (the run's planning phase provisions before it plans) that is none — the miss that
// let an agent ship SQLite instead of the Postgres it had provisioned. The gate
// still closes; only the comment moved.
func TestWiring_GateResolutionPostsNothing(t *testing.T) {
	issues := newFakeIssues([]sourcecontrol.IssueInfo{
		provisionGateIssue(11, "orders-db"),
		codingIssue(21, "Implement web", "open"),
	})
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{}}
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: wiringDesign()},
		&fakeExtProv{}, &fakePlatProv{}, bindings)

	if err := svc.Provision(context.Background(), "org", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	w := NewResourceWatcher(svc, nil, time.Second)
	w.now = func() time.Time { return time.Unix(1000, 0).Add(time.Minute) }
	ready := readyBinding("host", "port")
	bindings.byName["o-orders-db-development"] = ready
	bindings.byName["proj-orders-db-development"] = ready
	if err := w.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}

	if got := wiringComments(issues, 21); len(got) != 0 {
		t.Errorf("gate resolution must not post the wiring comment any more, got:\n%v", got)
	}
	if _, closed := issues.closed[11]; !closed {
		t.Errorf("the gate must still close on readiness")
	}
}

// The `resources:` half is gone from this comment: a resource's ref and env-var
// names are stamped into its dependency's `wiring` block in design.json at design
// save, so restating them here would be two channels for one file section — and
// the one that could silently miss.
func TestWiring_CarriesNoResourcesBlock(t *testing.T) {
	design := []spec.DesignComponent{{
		Name: "web",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindComponent, Name: "orders"},
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg"},
			{Kind: spec.DependencyKindExternal, Name: "stripe"},
		},
	}}
	issues := publishFor(t, []sourcecontrol.IssueInfo{codingIssue(21, "Implement web", "open")},
		design, siblingResolved())

	posted := wiringComments(issues, 21)
	if len(posted) != 1 {
		t.Fatalf("want one wiring comment, got %d", len(posted))
	}
	body := posted[0]
	// Scope the check to the YAML the agent copies: the prose deliberately NAMES
	// `resources:` to say where that half lives, which is not the same as carrying
	// it. Asserting over the whole body would forbid the pointer along with the
	// duplication.
	for _, forbidden := range []string{"resources:", "ref: proj-orders-db", "ORDERS_DB_HOST", "ref: proj-stripe"} {
		if strings.Contains(yamlBlockOf(t, body), forbidden) {
			t.Errorf("the workload block must not carry the resources half (%q):\n%s", forbidden, body)
		}
	}
	// It DOES still point the agent at where that half lives.
	if !strings.Contains(body, "`design.json`") {
		t.Errorf("the comment must name design.json as the home of the resources half:\n%s", body)
	}
	// Secret material never rides the comment.
	if strings.Contains(strings.ToLower(body), "password") {
		t.Errorf("no secret material may appear in the wiring comment:\n%s", body)
	}
}

// Targeting: the run's WORKING SET and nothing else. Gates are platform holds,
// the validation issue is worked by its own cycle, a closed issue is done, and
// an issue without `aep` is ledger-only — none of them is agent work.
func TestWiring_TargetsTheWorkingSetOnly(t *testing.T) {
	seed := []sourcecontrol.IssueInfo{
		codingIssue(21, "Implement web", "open"),
		codingIssue(22, "Implement orders", "open"),
		codingIssue(23, "Already merged", "closed"),
		codingIssue(24, "Validate the increment", "open", delivery.LabelValidationWork),
		{Number: 25, Title: "A human bug report", State: "open"}, // ledger-only: no `aep`
	}
	issues := publishFor(t, seed, wiringDesign(), siblingResolved())

	for _, n := range []int{21, 22} {
		if len(wiringComments(issues, n)) != 1 {
			t.Errorf("working-set issue #%d: want 1 wiring comment, got %d", n, len(wiringComments(issues, n)))
		}
	}
	for _, n := range []int{11, 23, 24, 25} {
		if got := wiringComments(issues, n); len(got) != 0 {
			t.Errorf("issue #%d is not agent work and must not get the wiring comment", n)
		}
	}
}

// A dispatch with nothing open to work posts nothing: there is no agent to tell.
func TestWiring_NoOpenWorkPostsNothing(t *testing.T) {
	issues := publishFor(t, []sourcecontrol.IssueInfo{codingIssue(21, "Implement web", "closed")},
		wiringDesign(), siblingResolved())

	for n := range issues.comments {
		if got := wiringComments(issues, n); len(got) != 0 {
			t.Errorf("no open work: nothing should be posted, but issue #%d got:\n%v", n, got)
		}
	}
}

// Idempotency: the publisher runs on EVERY cycle dispatch, and a complete comment
// must not pile up across them. The aep:wired marker is the guard.
func TestWiring_IdempotentAcrossDispatches(t *testing.T) {
	issues := newFakeIssues([]sourcecontrol.IssueInfo{
		provisionGateIssue(11, "orders-db"),
		codingIssue(21, "Implement web", "open"),
	})
	svc := NewService(Deps{
		Issues: issues, Execs: &fakeExecStore{},
		Design: fakeDesign{comps: wiringDesign()}, Repos: fakeRepos{},
		ExtProv: &fakeExtProv{}, PlatProv: &fakePlatProv{},
		Bindings: &fakeBindings{}, Providers: siblingResolved(),
	})

	for i := 0; i < 3; i++ {
		svc.PublishResolvedWiring(context.Background(), "org", "proj")
	}

	if got := wiringComments(issues, 21); len(got) != 1 {
		t.Fatalf("three dispatches must post one comment, got %d:\n%v", len(got), got)
	}
	if !delivery.HasLabel(issueByNumber(t, issues, 21).Labels, wiredLabel) {
		t.Errorf("the posted issue must carry the aep:wired marker")
	}
}

// The completeness rule, which is the old bug in miniature: a block that had to
// OMIT an unresolved sibling goes up unmarked, so the next dispatch supersedes it
// with the fuller version instead of treating a partial answer as final.
func TestWiring_PartialPostIsUnmarkedAndSupersededLater(t *testing.T) {
	design := []spec.DesignComponent{{
		Name: "web",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindComponent, Name: "orders"},
			{Kind: spec.DependencyKindOrgService, Name: "employee-api"}, // never resolves below
		},
	}}
	issues := newFakeIssues([]sourcecontrol.IssueInfo{codingIssue(21, "Implement web", "open")})
	providers := siblingResolved() // employee-api absent from nsVisible → unresolved
	deps := Deps{
		Issues: issues, Execs: &fakeExecStore{},
		Design: fakeDesign{comps: design}, Repos: fakeRepos{},
		ExtProv: &fakeExtProv{}, PlatProv: &fakePlatProv{},
		Bindings: &fakeBindings{}, Providers: providers,
	}
	svc := NewService(deps)

	svc.PublishResolvedWiring(context.Background(), "org", "proj")

	if got := len(wiringComments(issues, 21)); got != 1 {
		t.Fatalf("the resolvable half must still be posted, got %d comments", got)
	}
	if delivery.HasLabel(issueByNumber(t, issues, 21).Labels, wiredLabel) {
		t.Fatal("a PARTIAL block must not be marked complete — the next dispatch would never supersede it")
	}

	// The provider publishes, and the next dispatch posts the fuller block.
	providers.nsVisible = map[string]openchoreo.WorkloadEndpointInfo{
		"employee-api": {Project: "hr", Component: "hr-employee-api", Name: "http"},
	}
	deps.Providers = providers
	NewService(deps).PublishResolvedWiring(context.Background(), "org", "proj")

	posted := wiringComments(issues, 21)
	if len(posted) != 2 {
		t.Fatalf("the next dispatch must supersede a partial block, got %d comments", len(posted))
	}
	if !strings.Contains(posted[1], "address: EMPLOYEE_API_URL") {
		t.Errorf("the superseding block must carry the newly resolved endpoint:\n%s", posted[1])
	}
	if !delivery.HasLabel(issueByNumber(t, issues, 21).Labels, wiredLabel) {
		t.Errorf("a complete block must be marked so later dispatches stop re-posting")
	}
}

// Both endpoint kinds, asserted literally — this is the contract the coding agent
// copies verbatim, so env-var names and visibility values cannot drift silently.
func TestWiring_ResolvesEveryEndpointKind(t *testing.T) {
	design := []spec.DesignComponent{{
		Name: "web",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindOrgService, Name: "employee-api"},
			{Kind: spec.DependencyKindComponent, Name: "orders"},
			{Kind: spec.DependencyKindExternal, Name: "stripe", SpecPath: "dependencies/stripe.openapi.yaml"},
		},
	}}
	providers := siblingResolved()
	providers.nsVisible = map[string]openchoreo.WorkloadEndpointInfo{
		"employee-api": {Project: "hr", Component: "hr-employee-api", Name: "http"},
	}
	issues := publishFor(t, []sourcecontrol.IssueInfo{codingIssue(21, "Implement web", "open")}, design, providers)

	posted := wiringComments(issues, 21)
	if len(posted) != 1 {
		t.Fatalf("want one wiring comment, got %d", len(posted))
	}
	body := posted[0]
	for _, want := range []string{
		"visibility: namespace", "address: EMPLOYEE_API_URL",
		"visibility: project", "address: ORDERS_URL",
		"### Consumed API contract — employee-api",
		"project `hr`, component `hr-employee-api`, endpoint `http`",
		"list_org_component_endpoints",
		"### Consumed API contract — orders (local)",
		"dependencies/stripe.openapi.yaml",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wiring comment missing %q:\n%s", want, body)
		}
	}
}

func TestOrgServiceURLEnv(t *testing.T) {
	// Byte parity with endpoints.OrgServiceURLEnv.
	cases := map[string]string{"employee-api": "EMPLOYEE_API_URL", "todo": "TODO_URL", "order-svc-2": "ORDER_SVC_2_URL"}
	for in, want := range cases {
		if got := orgServiceURLEnv(in); got != want {
			t.Errorf("orgServiceURLEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

// The gate index and the wiring marker are DIFFERENT labels: overloading
// aep:dep/<slug> onto a coding issue would make gateDepFromLabels answer for
// issues that are not gates.
func TestWiredLabel_IsNotTheGateIndex(t *testing.T) {
	if wiredLabel == gateDepLabel("orders-db") {
		t.Errorf("the wiring marker must not collide with the gate index")
	}
	if got := gateDepFromLabels([]string{wiredLabel}); got != "" {
		t.Errorf("the wiring marker must not read back as a gate's dependency, got %q", got)
	}
}

// yamlBlockOf returns the contents of the comment's first ```yaml fence — the
// bytes the coding agent actually copies into workload.yaml, as distinct from the
// prose around them.
func yamlBlockOf(t *testing.T, body string) string {
	t.Helper()
	const fence = "```yaml\n"
	start := strings.Index(body, fence)
	if start < 0 {
		t.Fatalf("comment carries no yaml block:\n%s", body)
	}
	rest := body[start+len(fence):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatalf("unterminated yaml block:\n%s", body)
	}
	return rest[:end]
}

func issueByNumber(t *testing.T, f *fakeIssues, number int) sourcecontrol.IssueInfo {
	t.Helper()
	for _, i := range f.list {
		if i.Number == number {
			return i
		}
	}
	t.Fatalf("issue #%d not found", number)
	return sourcecontrol.IssueInfo{}
}
