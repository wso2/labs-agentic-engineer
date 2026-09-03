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
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// TestProvisionForBuild_ByKind is the Task-3 contract: the workflow's provision
// step mints platform-resource gate issues once, then authors each dependency
// BY KIND — external via AuthorPreparedValues (with no collection gate) and
// platform-resource via the async Provision (gate left open
// for the readiness watcher). It must NOT route external through Provision (that
// would re-write secrets).
func TestProvisionForBuild_ByKind(t *testing.T) {
	issues := newFakeIssues(nil) // no gates yet — EnsureProvisionIssues mints them
	execs := &fakeExecStore{}
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config",
			Config: map[string]string{"region": "us"}, SecretRefByEnv: map[string]string{"development": "sm://x"}},
		{Component: "orders", Dependency: "orders-db", Kind: "platform-resource",
			Parameters: map[string]any{"instances": 1}, Approved: true},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}

	// Only the platform resource gets a build-time provision gate.
	if len(issues.created) != 1 {
		t.Fatalf("want 1 minted gate issue (orders-db), got %d", len(issues.created))
	}

	// External authored from the design — NOT from the carried request payload,
	// and NOT via Provision (which would write secrets).
	if ext.authorPreparedCalls != 1 {
		t.Fatalf("external dep must be authored via AuthorPreparedValues once, got %d", ext.authorPreparedCalls)
	}
	if ext.calls != 0 {
		t.Fatalf("external dep must NOT go through Provision (that re-writes secrets), got %d Provision calls", ext.calls)
	}
	if got := ext.authorByEnv["development"]; got.SecretStorePath != "" || got.Plain["region"] != "" {
		t.Fatalf("author must ignore request config/ref and use empty design values: %+v", got)
	}

	// Platform-resource authored via the async Provision path.
	if plat.calls != 1 {
		t.Fatalf("platform-resource dep must be authored via Provision once, got %d", plat.calls)
	}
	if plat.params["instances"] != 1 {
		t.Fatalf("platform-resource params must flow through, got %+v", plat.params)
	}

	if extGate := gateNumber(issues, "stripe"); extGate != 0 {
		t.Fatalf("external dependency must not mint a config-collection gate, got #%d", extGate)
	}
	platGate := gateNumber(issues, "orders-db")
	if _, closed := issues.closed[platGate]; closed {
		t.Fatalf("platform gate #%d must stay open for the readiness watcher", platGate)
	}
}

// TestProvisionForBuild_UsesMintedPlatformGateDespiteListRace is the #164 race regression:
// EnsureProvisionIssues CREATES the gate, but GitHub's label-filtered issue LIST is
// eventually consistent — a just-minted gate is often NOT yet in ListIssues. The old
// code re-looked-up the gate via that racy list (findProvisionIssue → 0) so NO
// platform provision run would miss its gate. The fix threads the number the
// CreateIssue result returns. Here the fake hides just-created issues from
// ListIssues; platform provisioning must still receive the captured number.
func TestProvisionForBuild_UsesMintedPlatformGateDespiteListRace(t *testing.T) {
	issues := newFakeIssues(nil)
	issues.raceNewIssues = true // just-minted gates are invisible to ListIssues (the race)
	execs := &fakeExecStore{}
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "orders-db", Kind: "platform-resource",
			Parameters: map[string]any{"instances": 1}},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	platformGate := gateNumber(issues, "orders-db")
	if platformGate == 0 {
		t.Fatalf("gate must have been minted")
	}
	if row := provisionRowFor(execs, "orders-db"); row == nil || row.IssueNumber != platformGate {
		t.Fatalf("platform provision row = %+v, want captured gate #%d", row, platformGate)
	}
}

// designWithPlatformDeps is a local fixture for the fail-fast test: three
// platform-resource names that designWithDeps() does not carry, so the
// provisioner is reached instead of a 404 from findDepInProject.
func designWithPlatformDeps(names ...string) []spec.DesignComponent {
	deps := make([]spec.Dependency, 0, len(names))
	for _, n := range names {
		deps = append(deps, spec.Dependency{
			Kind:         spec.DependencyKindPlatformResource,
			Name:         n,
			ResourceType: "postgres-cnpg",
		})
	}
	return []spec.DesignComponent{{Name: "orders", Dependencies: deps}}
}

func TestProvisionForBuild_PermanentPlatformFailureSkipsRemainingPlatformWaits(t *testing.T) {
	issues := newFakeIssues(nil)
	plat := &fakePlatProv{err: fmt.Errorf("%w: no release", dependencies.ErrProvisionPermanent)}
	svc := newTestService(issues, &fakeExecStore{}, fakeDesign{comps: designWithPlatformDeps("bad-a", "bad-b", "bad-c")}, &fakeExtProv{}, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "bad-a", Kind: "platform-resource", Approved: true},
		{Component: "orders", Dependency: "bad-b", Kind: "platform-resource", Approved: true},
		{Component: "orders", Dependency: "bad-c", Kind: "platform-resource", Approved: true},
	})
	if err != nil {
		t.Fatalf("batch error: %v", err)
	}
	if plat.calls != 1 {
		t.Fatalf("permanent platform failure must skip remaining platform waits, got %d calls", plat.calls)
	}
	if len(fails) != 1 {
		t.Fatalf("want 1 failure, got %+v", fails)
	}
	if !errors.Is(fails[0].Err, dependencies.ErrProvisionPermanent) {
		t.Fatalf("failure must carry ErrProvisionPermanent, got %v", fails[0].Err)
	}
}

// TestProvisionForBuild_ExternalAuthorFailureContinues pins the batch semantics:
// a per-input author error becomes a ProvisionFailure (data, not an activity
// error) and the batch continues to the remaining inputs.
func TestProvisionForBuild_ExternalAuthorFailureContinues(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	ext := &fakeExtProv{authorErr: fmt.Errorf("author boom")}
	plat := &fakePlatProv{}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config",
			SecretRefByEnv: map[string]string{"development": "sm://x"}},
		{Component: "orders", Dependency: "orders-db", Kind: "platform-resource",
			Parameters: map[string]any{"instances": 1}},
	})
	if err != nil {
		t.Fatalf("a per-input author failure must not be an activity error, got %v", err)
	}
	if len(fails) != 1 {
		t.Fatalf("want exactly one ProvisionFailure, got %+v", fails)
	}
	if fails[0].Dependency != "stripe" || fails[0].Component != "orders" {
		t.Fatalf("failure identity wrong: %+v", fails[0])
	}
	if fails[0].Reason == "" {
		t.Fatalf("failure must carry a reason")
	}
	// The batch continued past the failure: the platform-resource still provisioned.
	if plat.calls != 1 {
		t.Fatalf("batch must continue after an external failure, got %d platform calls", plat.calls)
	}
}

// TestProvisionForBuild_OrgServiceUnapprovedIsNoop confirms an UNAPPROVED
// org-service input authors nothing (the user did not opt in) and never errors.
func TestProvisionForBuild_OrgServiceUnapprovedIsNoop(t *testing.T) {
	issues := newFakeIssues(nil)
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := newTestService(issues, &fakeExecStore{},
		fakeDesign{comps: []spec.DesignComponent{{Name: "web"}}}, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "web", Dependency: "inventory", Kind: "org-service", Approved: false},
	})
	if err != nil || len(fails) != 0 {
		t.Fatalf("unapproved org-service must be a silent no-op, got fails=%+v err=%v", fails, err)
	}
	if ext.authorPreparedCalls != 0 || plat.calls != 0 {
		t.Fatalf("org-service must author nothing")
	}
	if len(issues.created) != 0 {
		t.Fatalf("unapproved org-service must mint no gate, created %d", len(issues.created))
	}
}

// TestProvisionForBuild_OrgServiceApprovedStartsVisibility confirms an APPROVED
// org-service input drives StartOrgServiceVisibility (issue #164, Task 4): the
// consumer visibility gate + provider org-publish issue are minted and the
// provider build is triggered, without failing the batch.
func TestProvisionForBuild_OrgServiceApprovedStartsVisibility(t *testing.T) {
	issues := newFakeIssues(nil)
	access := &fakeAccess{}
	build := &fakeProviderBuild{}
	consumer := spec.DesignComponent{Name: "web", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindOrgService, Name: "inventory"},
	}}
	svc := NewService(Deps{
		Issues: issues,
		Execs:  &fakeExecStore{},
		Design: fakeDesign{comps: []spec.DesignComponent{consumer}},
		Repos:  fakeRepos{},
		Access: access,
		Providers: fakeProviders{byName: map[string]openchoreo.WorkloadEndpointInfo{
			"inventory": {Project: "warehouse", Component: "warehouse-inventory", Name: "http"},
		}},
	})
	svc.SetProviderBuildTrigger(build)

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "storefront", "v3", 0, []BuildProvisionInput{
		{Component: "web", Dependency: "inventory", Kind: "org-service", Approved: true},
	})
	if err != nil || len(fails) != 0 {
		t.Fatalf("approved org-service must not fail the batch, got fails=%+v err=%v", fails, err)
	}
	// One consumer visibility gate + one provider org-publish issue minted.
	var haveVisibility, haveOrgPublish bool
	for _, req := range issues.created {
		switch {
		case strings.HasPrefix(req.Title, visibilityGateTitlePrefix):
			haveVisibility = true
		case strings.HasPrefix(req.Title, orgPublishGateTitlePrefix):
			haveOrgPublish = true
		}
	}
	if !haveVisibility || !haveOrgPublish {
		t.Fatalf("approved org-service must mint the consumer visibility gate + provider org-publish issue, created %+v", issues.created)
	}
	if build.calls != 1 || build.lastPrj != "warehouse" {
		t.Fatalf("approved org-service must trigger the provider build once for the provider project, got calls=%d prj=%q", build.calls, build.lastPrj)
	}
}

// TestProvisionForBuild_SettlesReadyGateNotInInputs is the #164 regression: a
// dep whose OC binding is already Ready but which is NOT in the build drawer
// inputs still gets a freshly-minted provision gate (via EnsureProvisionIssues).
// Without a settle step that gate has no succeeded provision run → derives
// pending forever → the funnel strands every consumer coding task. The fix
// admits+completes a provision run for it so the gate derives deployed —
// WITHOUT re-authoring the already-Ready resource.
func TestProvisionForBuild_SettlesReadyGateNotInInputs(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	// orders-db (platform-resource) is already Ready in OC but NOT in the drawer inputs.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "orders-db", "development"): readyBinding("host", "port"),
	}}
	design := &countingDesign{comps: designWithDeps()}
	svc := newTestService(issues, execs, design, ext, plat, bindings)

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config",
			Config: map[string]string{"region": "us"}, SecretRefByEnv: map[string]string{"development": "sm://x"}},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}

	// orders-db was Ready but not in inputs → its gate must be settled (closed).
	dbGate := gateNumber(issues, "orders-db")
	if _, closed := issues.closed[dbGate]; !closed {
		t.Fatalf("already-ready dep gate #%d must be settled (closed) or consumers strand", dbGate)
	}
	// A succeeded provision run was admitted+completed so the gate derives deployed.
	if r := provisionRowFor(execs, "orders-db"); r == nil || r.Status != string(taskmeta.ExecSucceeded) {
		t.Fatalf("settle must admit+complete a provision run for orders-db, got %+v", r)
	}
	// The resource is already Ready — settle must NOT re-author it.
	if plat.calls != 0 {
		t.Fatalf("settle must NOT re-author the already-ready resource, got %d Provision calls", plat.calls)
	}
	// External authoring has no config-collection gate and therefore no provision row.
	if n := countProvisionRows(execs, "stripe"); n != 0 {
		t.Fatalf("stripe must not create a provision run, got %d", n)
	}
	// EnsureProvisionIssues, external authoring, and gate settlement each read
	// the design once. Binding status must not trigger a fourth read.
	if design.calls != 3 {
		t.Fatalf("design reads = %d, want 3", design.calls)
	}
}

// TestProvisionForBuild_SkipsNotReadyGateNotInInputs pins that settle only acts
// on already-Ready deps: a dep not in inputs whose binding is NOT ready is left
// alone (its own drawer input drives it), so no run is admitted and its gate
// stays open.
func TestProvisionForBuild_SkipsNotReadyGateNotInInputs(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	// orders-db has NO binding (never provisioned) → Status reports not-ready.
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config",
			SecretRefByEnv: map[string]string{"development": "sm://x"}},
	})
	if err != nil || len(fails) != 0 {
		t.Fatalf("want clean run, got fails=%+v err=%v", fails, err)
	}
	// A not-ready dep not in inputs must NOT be settled.
	dbGate := gateNumber(issues, "orders-db")
	if _, closed := issues.closed[dbGate]; closed {
		t.Fatalf("a not-ready dep gate #%d must NOT be settled", dbGate)
	}
	if r := provisionRowFor(execs, "orders-db"); r != nil {
		t.Fatalf("a not-ready dep must have no provision run, got %+v", r)
	}
}

// TestSettleReadyGate_NoOpenGate pins the no-op path: a Ready dep with no open
// provision gate (findProvisionIssue → 0) admits nothing and closes nothing.
func TestSettleReadyGate_NoOpenGate(t *testing.T) {
	issues := newFakeIssues(nil) // no open gates
	execs := &fakeExecStore{}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "orders-db", "development"): readyBinding(),
	}}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, bindings)

	if err := svc.completeReadyGate(context.Background(), "acme", "proj", "orders-db", "orders"); err != nil {
		t.Fatalf("no open gate must be a no-op, got %v", err)
	}
	if len(execs.rows) != 0 {
		t.Fatalf("no open gate → no provision run admitted, got %d rows", len(execs.rows))
	}
	if len(issues.closed) != 0 {
		t.Fatalf("no open gate → nothing closed, got %d", len(issues.closed))
	}
}

// TestProvisionForBuild_EmptyInputsDoesNotMint pins that a build with no drawer
// inputs (a pure re-build) mints NO new gates — EnsureProvisionIssues is skipped
// so already-ready deps don't churn a fresh gate every build. The settle pass
// still runs (reconciling any existing open gate), but with no seeded gate here
// it is a no-op.
func TestProvisionForBuild_EmptyInputsDoesNotMint(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	// orders-db is Ready in OC, but there is no existing gate to settle.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "orders-db", "development"): readyBinding(),
	}}
	svc := newTestService(issues, execs, fakeDesign{comps: designWithDeps()}, &fakeExtProv{}, &fakePlatProv{}, bindings)

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v3", 0, nil)
	if err != nil {
		t.Fatalf("ProvisionForBuild (empty inputs): %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	if len(issues.list) != 0 {
		t.Fatalf("empty-input build must not mint any gate, got %d", len(issues.list))
	}
	if len(issues.closed) != 0 {
		t.Fatalf("no existing gate → nothing to settle, got %d closed", len(issues.closed))
	}
}

// provisionRowFor returns the first provision Execution row for a dep name.
func provisionRowFor(execs *fakeExecStore, depName string) *delivery.Execution {
	for _, r := range execs.rows {
		if r.Kind == string(taskmeta.KindProvision) && r.Component == depName {
			return r
		}
	}
	return nil
}

// countProvisionRows counts provision Execution rows admitted for a dep name.
func countProvisionRows(execs *fakeExecStore, depName string) int {
	n := 0
	for _, r := range execs.rows {
		if r.Kind == string(taskmeta.KindProvision) && r.Component == depName {
			n++
		}
	}
	return n
}

// gateNumber finds the gate issue for a dep name — by its aep:dep/<slug> label,
// which is how the platform itself resolves it.
func gateNumber(issues *fakeIssues, depName string) int {
	for _, i := range issues.list {
		if delivery.IsDispatchGate(i.Labels) && gateDepFromLabels(i.Labels) == gateDepFromLabels(gateLabels(depName)) {
			return i.Number
		}
	}
	return 0
}

// TestProvisionForBuild_RegisteredExternal_AuthorsFromOrgCells: a Registered
// External resource (non-empty EnvCells) authors the project's Resource
// instance from org non-secret cells — not design defaults — and records the
// instance on the value plane. No project OpenBao Provision.
func TestProvisionForBuild_RegisteredExternal_AuthorsFromOrgCells(t *testing.T) {
	plane := NewMemoryValuePlane()
	plane.PutEnvCells("acme", "stripe", []EnvCell{
		{Environment: "development", Key: "api_key", Status: "configured"},
		{Environment: "development", Key: "region", Status: "configured", Value: "us"},
	})
	ext := &fakeExtProv{}
	svc := NewService(Deps{
		Issues:            newFakeIssues(nil),
		Execs:             &fakeExecStore{},
		Design:            fakeDesign{comps: designWithDeps()},
		Repos:             fakeRepos{},
		ExtProv:           ext,
		PlatProv:          &fakePlatProv{},
		Bindings:          &fakeBindings{},
		CatalogValuePlane: plane,
	})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config"},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	if ext.authorPreparedCalls != 1 {
		t.Fatalf("Registered build must AuthorPreparedValues once, got %d", ext.authorPreparedCalls)
	}
	if ext.calls != 0 {
		t.Fatalf("Registered build must not Provision (project OpenBao), got %d", ext.calls)
	}
	got := ext.authorByEnv["development"]
	if got.Plain["region"] != "us" {
		t.Fatalf("Registered author Plain = %+v, want region=us from org cells", got.Plain)
	}
	if _, hasSecret := got.Plain["api_key"]; hasSecret {
		t.Fatalf("secret cell must not appear in Plain: %+v", got.Plain)
	}
	inst := plane.Instances("acme", "stripe")
	if len(inst) != 1 || inst[0].Project != "proj" || inst[0].Environment != "development" {
		t.Fatalf("instances after Registered author = %+v, want {proj, development}", inst)
	}
}

// fakeOrgSecrets is an OrgSecretWriter that returns a caller-chosen vault key
// without talking to SM-API. Tests assert the key is forwarded, not its format.
type fakeOrgSecrets struct {
	key string
}

func (f *fakeOrgSecrets) WriteOrgCatalogSecret(context.Context, string, string, map[string]string) (string, error) {
	return f.key, nil
}

func (f *fakeOrgSecrets) OrgCatalogVaultKey(context.Context, string, string) (string, error) {
	return f.key, nil
}

type fakeEnvs struct {
	names []string
}

func (f fakeEnvs) ListNames(context.Context, string) ([]string, error) {
	return f.names, nil
}

// TestProvisionForBuild_RegisteredExternal_AuthorsOrgSecretStorePath: register
// with a wired OrgSecretWriter persists the returned vault key on the value
// plane; build authoring passes it as SecretStorePath. No project OpenBao
// Provision. Secret cells stay out of Plain.
func TestProvisionForBuild_RegisteredExternal_AuthorsOrgSecretStorePath(t *testing.T) {
	plane := NewMemoryValuePlane()
	ext := &fakeExtProv{}
	writer := &fakeOrgSecrets{key: "from-org-secret-writer"}
	svc := NewService(Deps{
		Issues:            newFakeIssues(nil),
		Execs:             &fakeExecStore{},
		Design:            fakeDesign{comps: designWithDeps()},
		Repos:             fakeRepos{},
		RTCatalog:         &fakeRTCatalog{},
		ExtProv:           ext,
		PlatProv:          &fakePlatProv{},
		Bindings:          &fakeBindings{},
		CatalogValuePlane: plane,
		Environments:      fakeEnvs{names: []string{"development"}},
		OrgSecrets:        writer,
	})

	_, err := svc.RegisterExternalResource(context.Background(), "acme", gen.RegisterExternalResourceRequest{
		Name:                    "stripe",
		Description:             "Stripe payments",
		ConsumptionInstructions: "Use the secret as Bearer.",
		Config: []gen.ConfigKeyDTO{
			{Key: "api_key", Description: "Secret API key", Secret: true},
			{Key: "region", Description: "Account region"},
		},
		EnvValues: []struct {
			Environment string `json:"environment"`
			Key         string `json:"key"`
			Value       string `json:"value"`
		}{
			{Environment: "development", Key: "api_key", Value: "sk_live"},
			{Environment: "development", Key: "region", Value: "us"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterExternalResource: %v", err)
	}

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config"},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	if ext.authorPreparedCalls != 1 {
		t.Fatalf("Registered build must AuthorPreparedValues once, got %d", ext.authorPreparedCalls)
	}
	if ext.calls != 0 {
		t.Fatalf("Registered build must not Provision (project OpenBao), got %d", ext.calls)
	}
	got := ext.authorByEnv["development"]
	if got.SecretStorePath == "" {
		t.Fatal("AuthorPreparedValues must receive a non-empty SecretStorePath from the org-catalog writer")
	}
	if got.SecretStorePath != writer.key {
		t.Fatalf("SecretStorePath = %q, want the vault key OrgSecretWriter returned", got.SecretStorePath)
	}
	if got.Plain["region"] != "us" {
		t.Fatalf("Registered author Plain = %+v, want region=us from org cells", got.Plain)
	}
	if _, hasSecret := got.Plain["api_key"]; hasSecret {
		t.Fatalf("secret cell must not appear in Plain: %+v", got.Plain)
	}
}

// TestProvisionForBuild_RegisteredAfterRestart_AuthorsOrgSecretStorePath:
// after aep-api restart the value plane is empty. Consumption instructions
// on the RT still mark the name Registered; synthesize + OrgCatalogVaultKey
// must pin the org-catalog path so build does not mint a project secret.
func TestProvisionForBuild_RegisteredAfterRestart_AuthorsOrgSecretStorePath(t *testing.T) {
	plane := NewMemoryValuePlane()
	ext := &fakeExtProv{}
	writer := &fakeOrgSecrets{key: "org-catalog-github-development"}
	svc := NewService(Deps{
		Issues: newFakeIssues(nil),
		Execs:  &fakeExecStore{},
		Design: fakeDesign{comps: designWithDeps()},
		Repos:  fakeRepos{},
		RTCatalog: &fakeRTCatalog{defs: []openchoreo.ExternalResourceDefinition{{
			Name: "stripe",
			Config: []openchoreo.ExternalResourceConfigKey{
				{Key: "api_key", Secret: true},
				{Key: "region"},
			},
			ConsumptionInstructions: "Use the secret as Bearer.",
		}}},
		ExtProv:           ext,
		PlatProv:          &fakePlatProv{},
		Bindings:          &fakeBindings{},
		CatalogValuePlane: plane,
		Environments:      fakeEnvs{names: []string{"development"}},
		OrgSecrets:        writer,
	})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: "external-config"},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	got := ext.authorByEnv["development"]
	if got.SecretStorePath != writer.key {
		t.Fatalf("SecretStorePath = %q, want reconstructed org-catalog key %q", got.SecretStorePath, writer.key)
	}
	if ext.calls != 0 {
		t.Fatalf("Registered restart build must not Provision (project OpenBao), got %d", ext.calls)
	}
}
