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
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// resolvedEnv is a write target unlike testWriteTarget, so a test can tell a
// resolved environment from a leftover constant.
const resolvedEnv = "development"

func noWriteTarget() error {
	return &openchoreo.ErrNoWriteTarget{Org: "acme", Project: "proj", Cause: openchoreo.ErrPipelineRefMissing}
}

func serviceWithTarget(wt WriteTargetResolver, issues *fakeIssues, execs *fakeExecStore, ext *fakeExtProv, plat *fakePlatProv, bindings *fakeBindings) *Service {
	return NewService(Deps{
		Issues: issues, Execs: execs, Design: fakeDesign{comps: designWithDeps()},
		Repos: fakeRepos{}, ExtProv: ext, PlatProv: plat, Bindings: bindings,
		WriteTargets: wt,
	})
}

// A build authors the project's dependencies into its write target: the
// external values, the platform resource's environment list, and the run each
// provision gate is pinned to.
func TestProvisionForBuild_AuthorsIntoTheWriteTarget(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	ext := &fakeExtProv{}
	plat := &fakePlatProv{result: &dependencies.PlatformProvisionResult{
		ResourceName: "o-orders-db",
		BindingByEnv: map[string]string{resolvedEnv: "o-orders-db-" + resolvedEnv},
	}}
	svc := serviceWithTarget(staticWriteTarget{env: resolvedEnv}, issues, execs, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: buildKindExternalConfig},
		{Component: "orders", Dependency: "orders-db", Kind: buildKindPlatformResrc},
	})
	if err != nil || len(fails) != 0 {
		t.Fatalf("ProvisionForBuild: fails=%+v err=%v", fails, err)
	}
	if got := sortedKeys(ext.authorByEnv); !reflect.DeepEqual(got, []string{resolvedEnv}) {
		t.Fatalf("external values authored into %v, want [%s]", got, resolvedEnv)
	}
	if !reflect.DeepEqual(plat.envs, []string{resolvedEnv}) {
		t.Fatalf("platform resource provisioned into %v, want [%s]", plat.envs, resolvedEnv)
	}
	if r := provisionRowFor(execs, "orders-db"); r == nil || r.RunName != "o-orders-db-"+resolvedEnv {
		t.Fatalf("provision run must be pinned to BindingByEnv[%q], got %+v", resolvedEnv, r)
	}
}

// An already-ready gate is settled against the write target's binding.
func TestProvisionForBuild_SettlesAgainstTheWriteTargetsBinding(t *testing.T) {
	issues := newFakeIssues(nil)
	execs := &fakeExecStore{}
	binding := ocname.ExternalResourceBindingName("proj", "orders-db", resolvedEnv)
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{binding: readyBinding("host")}}
	svc := serviceWithTarget(staticWriteTarget{env: resolvedEnv}, issues, execs, &fakeExtProv{}, &fakePlatProv{}, bindings)

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: buildKindExternalConfig},
	})
	if err != nil || len(fails) != 0 {
		t.Fatalf("ProvisionForBuild: fails=%+v err=%v", fails, err)
	}
	if r := provisionRowFor(execs, "orders-db"); r == nil || r.RunName != binding {
		t.Fatalf("settle run must be pinned to %q, got %+v", binding, r)
	}
}

// A project whose pipeline yields no write target fails every input it would
// author, permanently, and authors nothing.
func TestProvisionForBuild_NoWriteTargetFailsAuthoringPermanently(t *testing.T) {
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := serviceWithTarget(staticWriteTarget{err: noWriteTarget()}, newFakeIssues(nil), &fakeExecStore{}, ext, plat, &fakeBindings{})

	fails, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "stripe", Kind: buildKindExternalConfig},
		{Component: "orders", Dependency: "orders-db", Kind: buildKindPlatformResrc},
	})
	if err != nil {
		t.Fatalf("a configuration fact is not an activity retry, got err=%v", err)
	}
	if len(fails) != 2 {
		t.Fatalf("want one failure per authored input, got %+v", fails)
	}
	for _, f := range fails {
		var nwt *openchoreo.ErrNoWriteTarget
		if !errors.Is(f.Err, dependencies.ErrProvisionPermanent) || !errors.As(f.Err, &nwt) {
			t.Fatalf("failure %+v must be permanent and carry ErrNoWriteTarget", f)
		}
	}
	if ext.authorPreparedCalls != 0 || plat.calls != 0 {
		t.Fatalf("nothing may be authored: ext=%d plat=%d", ext.authorPreparedCalls, plat.calls)
	}
}

// A transient resolve failure retries the activity before anything is minted.
func TestProvisionForBuild_TransientResolveFailureRetriesWithNothingMinted(t *testing.T) {
	issues := newFakeIssues(nil)
	blip := errors.New("openchoreo: 503")
	svc := serviceWithTarget(staticWriteTarget{err: blip}, issues, &fakeExecStore{}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})

	_, err := svc.ProvisionForBuild(context.Background(), "acme", "acme", "proj", "v1", 0, []BuildProvisionInput{
		{Component: "orders", Dependency: "orders-db", Kind: buildKindPlatformResrc},
	})
	if !errors.Is(err, blip) {
		t.Fatalf("want the transient error back, got %v", err)
	}
	if len(issues.created) != 0 {
		t.Fatalf("no gate may be minted before the write target resolves, got %d", len(issues.created))
	}
}

func TestProvision_EmptyEnvsProvisionsIntoTheWriteTarget(t *testing.T) {
	plat := &fakePlatProv{}
	svc := serviceWithTarget(staticWriteTarget{env: resolvedEnv}, newFakeIssues(nil), &fakeExecStore{}, &fakeExtProv{}, plat, &fakeBindings{})

	if err := svc.Provision(context.Background(), "acme", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !reflect.DeepEqual(plat.envs, []string{resolvedEnv}) {
		t.Fatalf("provisioned into %v, want [%s]", plat.envs, resolvedEnv)
	}
}

func TestProvision_NoWriteTargetProvisionsNothing(t *testing.T) {
	plat := &fakePlatProv{}
	svc := serviceWithTarget(staticWriteTarget{err: noWriteTarget()}, newFakeIssues(nil), &fakeExecStore{}, &fakeExtProv{}, plat, &fakeBindings{})

	err := svc.Provision(context.Background(), "acme", "proj", "orders-db", nil, nil)
	var nwt *openchoreo.ErrNoWriteTarget
	if !errors.As(err, &nwt) {
		t.Fatalf("want ErrNoWriteTarget, got %v", err)
	}
	if plat.calls != 0 {
		t.Fatalf("nothing may be provisioned, got %d calls", plat.calls)
	}
}

func TestDeprovisionProject_TearsDownTheWriteTarget(t *testing.T) {
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	svc := serviceWithTarget(staticWriteTarget{env: resolvedEnv}, newFakeIssues(nil), &fakeExecStore{}, ext, plat, &fakeBindings{})

	if err := svc.DeprovisionProject(context.Background(), "acme", "proj"); err != nil {
		t.Fatalf("DeprovisionProject: %v", err)
	}
	want := [][]string{{resolvedEnv}}
	if !reflect.DeepEqual(ext.deprovisionedEnvs, want) || !reflect.DeepEqual(plat.deprovisionedEnvs, want) {
		t.Fatalf("deprovisioned envs ext=%v plat=%v, want %v", ext.deprovisionedEnvs, plat.deprovisionedEnvs, want)
	}
}

// No environment is guessed: an unresolvable write target is returned (the
// project delete logs it and carries on) with no provisioner called.
func TestDeprovisionProject_NoWriteTargetDeprovisionsNothing(t *testing.T) {
	ext := &fakeExtProv{}
	plat := &fakePlatProv{}
	cause := noWriteTarget()
	svc := serviceWithTarget(staticWriteTarget{err: cause}, newFakeIssues(nil), &fakeExecStore{}, ext, plat, &fakeBindings{})

	if err := svc.DeprovisionProject(context.Background(), "acme", "proj"); !errors.Is(err, cause) {
		t.Fatalf("want the resolve error back, got %v", err)
	}
	if len(ext.deprovisioned) != 0 || len(plat.deprovisioned) != 0 {
		t.Fatalf("no provisioner may be called: ext=%v plat=%v", ext.deprovisioned, plat.deprovisioned)
	}
}

// The console reads degrade to "nothing provisioned" with a nil error.
func TestConsoleReads_NoWriteTargetReadAsNothingProvisioned(t *testing.T) {
	// A binding under the old constant must not be read: there is no
	// environment to read it in.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "orders-db", testWriteTarget): readyBinding("host"),
	}}
	svc := serviceWithTarget(staticWriteTarget{err: noWriteTarget()}, newFakeIssues(nil), &fakeExecStore{}, &fakeExtProv{}, &fakePlatProv{}, bindings)
	ctx := context.Background()

	st, err := svc.Status(ctx, "acme", "proj", "orders-db", "")
	if err != nil || st.Status != "unknown" || st.Ready {
		t.Fatalf("Status = %+v, %v; want unknown, nil", st, err)
	}
	cr, err := svc.ConfigurationReadiness(ctx, "acme", "proj", "")
	if err != nil {
		t.Fatalf("ConfigurationReadiness: %v", err)
	}
	if cr.Configured || len(cr.Dependencies) != 1 || cr.Dependencies[0].State != ValueStateNotProvisioned {
		t.Fatalf("ConfigurationReadiness = %+v, want stripe not-provisioned", cr)
	}
}

// DeploymentReadiness is the run's deploy gate, not a console read: degrading
// would park the run on values that are not the cause, so both a missing write
// target and a transient failure come back as errors for the caller to classify.
func TestDeploymentReadiness_ResolveFailureIsReturned(t *testing.T) {
	for name, cause := range map[string]error{
		"no write target": noWriteTarget(),
		"transient":       errors.New("openchoreo: 503"),
	} {
		t.Run(name, func(t *testing.T) {
			svc := serviceWithTarget(staticWriteTarget{err: cause}, newFakeIssues(nil), &fakeExecStore{}, &fakeExtProv{}, &fakePlatProv{}, &fakeBindings{})
			got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "")
			if !errors.Is(err, cause) || got != nil {
				t.Fatalf("DeploymentReadiness = %+v, %v; want nil, %v", got, err, cause)
			}
		})
	}
}

func TestResolveComponentRunnerSecrets_EmptyEnvResolvesTheWriteTarget(t *testing.T) {
	ext := &fakeExtProv{}
	svc := serviceWithTarget(staticWriteTarget{env: resolvedEnv}, newFakeIssues(nil), &fakeExecStore{}, ext, &fakePlatProv{}, &fakeBindings{})

	if _, err := svc.ResolveComponentRunnerSecrets(context.Background(), "acme", "proj", "orders", " "); err != nil {
		t.Fatalf("ResolveComponentRunnerSecrets: %v", err)
	}
	if ext.runnerEnv != resolvedEnv {
		t.Fatalf("runner secrets read in %q, want %q", ext.runnerEnv, resolvedEnv)
	}
}

func TestSignInCoordinates_ReadsTheWriteTargetsBinding(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindPlatformResource, Name: "user-auth", ResourceType: "idp-app"},
	}}}}
	binding := &openchoreo.ResourceReleaseBinding{Status: &openchoreo.ResourceReleaseBindingStatus{
		Outputs: []openchoreo.ResolvedOutput{{Name: "client_id", Value: "cid"}},
	}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "user-auth", resolvedEnv): binding,
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings, Markers: endUserAuthMarkers(),
		WriteTargets: staticWriteTarget{env: resolvedEnv}})
	if got := svc.SignInCoordinates(context.Background(), "acme", "proj"); got.ClientID != "cid" {
		t.Fatalf("SignInCoordinates = %+v, want client cid from the %s binding", got, resolvedEnv)
	}

	svc = NewService(Deps{Design: design, Bindings: bindings, Markers: endUserAuthMarkers(),
		WriteTargets: staticWriteTarget{err: noWriteTarget()}})
	if got := svc.SignInCoordinates(context.Background(), "acme", "proj"); got != (SignInClient{}) {
		t.Fatalf("no write target must read as no client, got %+v", got)
	}
}

// With no environment list, org cells fall back to the org's own write target.
func TestSynthesizeRegisteredEnvCells_FallsBackToTheOrgWriteTarget(t *testing.T) {
	def := openchoreo.ExternalResourceDefinition{Name: "stripe", Config: []openchoreo.ExternalResourceConfigKey{{Key: "region"}}}
	svc := NewService(Deps{
		Environments: failingEnvs{err: errors.New("openchoreo: 503")},
		WriteTargets: staticWriteTarget{orgRoot: resolvedEnv},
	})
	cells := svc.synthesizeRegisteredEnvCells(context.Background(), "acme", def)
	if len(cells) != 1 || cells[0].Environment != resolvedEnv {
		t.Fatalf("cells = %+v, want one %s cell", cells, resolvedEnv)
	}

	svc = NewService(Deps{
		Environments: failingEnvs{err: errors.New("openchoreo: 503")},
		WriteTargets: staticWriteTarget{orgErr: noWriteTarget()},
	})
	if cells := svc.synthesizeRegisteredEnvCells(context.Background(), "acme", def); cells != nil {
		t.Fatalf("no org write target must synthesize no cells, got %+v", cells)
	}
}

type failingEnvs struct{ err error }

func (f failingEnvs) List(context.Context, string) ([]EnvironmentInfo, error) { return nil, f.err }
