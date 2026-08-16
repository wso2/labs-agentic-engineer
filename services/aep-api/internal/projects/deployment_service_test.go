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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// UNIT tier for DeploymentService.Deploy + siblingSPAOrigins — the promote the
// run supervisor drives once a cycle's builds are green. The two seams are
// doubled at the process boundary: the openchoreo.ComponentClient (generated
// moq — ListDeployments to read a web-app's external URL, EnsureRelease and
// ApplyReleaseBinding to promote) and the artifacts store (the REAL
// spec.NewArtifactStore decorator over a fake service serving a design
// working-tree, so the frontmatter -> DesignComponent parse is the real one,
// not a stub). No idp is wired (SetIDPService not called) so the
// publisher-provisioning branch is skipped.

// traitStoreWith wraps the REAL spec.NewArtifactStore over a fake
// artifact service serving the given design working-tree map. ReadDesign's
// frontmatter parse therefore runs for real.
func traitStoreWith(files map[string]string) *spec.ArtifactStore {
	return spec.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

// traitReadDesign parses a design working-tree map through the real store.
func traitReadDesign(t *testing.T, files map[string]string) *spec.DesignFile {
	t.Helper()
	d, err := traitStoreWith(files).ReadDesign(context.Background(), "acme", "proj")
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}
	if d == nil {
		t.Fatalf("ReadDesign returned nil design for fixture")
	}
	return d
}

// ocDeployments builds a ComponentClient moq whose ListDeployments returns the
// mapped external URL for a component (keyed by k8s component name), an empty
// deployment list for any unmapped component, and canned success for the two
// promote writes. EnsureRelease / ApplyReleaseBinding start as success no-ops;
// a case can override them to inject failure.
func ocDeployments(urlsByComponent map[string]string) *mocks.ComponentClientMock {
	return &mocks.ComponentClientMock{
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*gen.DeploymentList, error) {
			if u, ok := urlsByComponent[componentName]; ok && u != "" {
				return &gen.DeploymentList{Items: []gen.Deployment{{EndpointURL: u}}}, nil
			}
			return &gen.DeploymentList{}, nil
		},
		EnsureReleaseFunc: func(_ context.Context, _, _, _, releaseName string) (string, error) {
			return releaseName, nil
		},
		ApplyReleaseBindingFunc: func(context.Context, string, string, openchoreo.ReleaseBindingDesired) error {
			return nil
		},
	}
}

// design fixtures -------------------------------------------------------------

func traitRootMd() string { return "---\nsourceSpec: v1\n---\n\nOverview.\n" }

func endUserServiceMd(name string) string {
	return traitServiceJSON(name, "end-user-required")
}

func serviceToServiceMd(name string) string {
	return traitServiceJSON(name, "service-required")
}

func plainServiceMd(name string) string {
	return traitServiceJSON(name, "")
}

// webAppMd renders a web-application component design.json (canonical type:
// spec.ComponentTypeWebApplication — OpenChoreo's own term).
func webAppMd(name string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"web-application\",\n  \"description\": \"SPA.\",\n  \"dependencies\": []\n}\n"
}

// traitServiceJSON renders a service component design.json with an optional
// exposesAPI.auth policy (empty auth ⇒ no exposesAPI block).
func traitServiceJSON(name, auth string) string {
	var b strings.Builder
	b.WriteString("{\n  \"name\": \"" + name + "\",\n  \"type\": \"service\",\n  \"description\": \"API.\",\n  \"dependencies\": []")
	if auth != "" {
		b.WriteString(",\n  \"exposesAPI\": {\n    \"auth\": \"" + auth + "\"\n  }")
	}
	b.WriteString("\n}\n")
	return b.String()
}

// --- Deploy -------------------------------------------------------------------

// The binding a deploy writes must carry the release pin AND the trait config in
// the SAME value. That is the whole reason the platform took deploy over: a
// binding written in two steps is briefly renderable-but-wrong, and while it is
// wrong a protected API serves unauthenticated.
func TestDeploy_BindingCarriesPinAndTraitConfigTogether(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	oc := ocDeployments(map[string]string{})
	svc := NewDeploymentService(oc, traitStoreWith(files))

	out, err := svc.Deploy(context.Background(), "acme", "proj", []string{"api"}, "abc123def456")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if len(out) != 1 || out[0].Component != "api" || out[0].Release == "" {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	calls := oc.ApplyReleaseBindingCalls()
	if len(calls) != 1 {
		t.Fatalf("want one binding write, got %d", len(calls))
	}
	got := calls[0].In
	if got.ReleaseName != out[0].Release {
		t.Errorf("binding pinned %q, want the release the deploy cut (%q)", got.ReleaseName, out[0].Release)
	}
	if got.State != openchoreo.ReleaseBindingStateActive {
		t.Errorf("state = %q, want Active", got.State)
	}
	cfg, ok := got.TraitEnvironmentConfigs[APIConfigurationInstanceName("api", "http")]
	if !ok {
		t.Fatalf("protected component's binding carries no api-configuration config: %+v", got.TraitEnvironmentConfigs)
	}
	if _, hasJWT := cfg["jwtAuth"]; !hasJWT {
		t.Errorf("trait config carries no jwtAuth: %+v", cfg)
	}
}

// The release name is derived from the commit, which is what makes a retried
// deploy activity re-pin the same release rather than stack a new one.
func TestDeploy_ReleaseNameIsDerivedFromTheCommit(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": plainServiceMd("api"),
	}
	oc := ocDeployments(map[string]string{})
	svc := NewDeploymentService(oc, traitStoreWith(files))

	first, err := svc.Deploy(context.Background(), "acme", "proj", []string{"api"}, "abc123def456")
	if err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	second, err := svc.Deploy(context.Background(), "acme", "proj", []string{"api"}, "abc123def456")
	if err != nil {
		t.Fatalf("Deploy (retry): %v", err)
	}
	if first[0].Release != second[0].Release {
		t.Errorf("same commit produced two release names: %q vs %q", first[0].Release, second[0].Release)
	}
	if first[0].Release != ReleaseNameFor("proj", "api", "abc123def456") {
		t.Errorf("release %q is not the derived name", first[0].Release)
	}
}

// A protected API's CORS allowlist has to carry its sibling SPA's origin, and
// the deploy is where that gets resolved.
func TestDeploy_ProtectedAPICarriesSiblingSPAOrigin(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
		"components/web/design.json": webAppMd("web"),
	}
	oc := ocDeployments(map[string]string{"web": "http://web.local/app/"})
	svc := NewDeploymentService(oc, traitStoreWith(files))

	if _, err := svc.Deploy(context.Background(), "acme", "proj", []string{"api"}, "abc123def456"); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	cfg := oc.ApplyReleaseBindingCalls()[0].In.TraitEnvironmentConfigs[APIConfigurationInstanceName("api", "http")]
	cors, _ := cfg["cors"].(map[string]interface{})
	origins, _ := cors["allowedOrigins"].([]interface{})
	if len(origins) != 1 || origins[0] != "http://web.local" {
		t.Errorf("allowedOrigins = %v, want the sibling SPA origin", origins)
	}
}

// One component failing must not stop the rest of a version deploying — but the
// pass still has to report that it did not fully succeed.
func TestDeploy_PerComponentFailureContinuesThenSurfaces(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": plainServiceMd("api"),
		"components/two/design.json": plainServiceMd("two"),
	}
	oc := ocDeployments(map[string]string{})
	oc.ApplyReleaseBindingFunc = func(_ context.Context, _, _ string, in openchoreo.ReleaseBindingDesired) error {
		if in.ComponentName == "api" {
			return errors.New("boom")
		}
		return nil
	}
	svc := NewDeploymentService(oc, traitStoreWith(files))

	out, err := svc.Deploy(context.Background(), "acme", "proj", []string{"api", "two"}, "abc123def456")
	if err == nil {
		t.Fatal("want the failure surfaced, got nil")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error should name the failed component: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("want an outcome per component even on failure, got %d", len(out))
	}
	if len(oc.ApplyReleaseBindingCalls()) != 2 {
		t.Errorf("the second component was not attempted: %d writes", len(oc.ApplyReleaseBindingCalls()))
	}
}

// Converge re-asserts wiring WITHOUT promoting: a user editing env vars must
// never move which release is serving.
func TestConverge_DoesNotCutOrPinARelease(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	oc := ocDeployments(map[string]string{})
	oc.GetReleaseBindingStatusFunc = func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
		return &openchoreo.ReleaseBindingSummary{ReadyStatus: "True"}, nil
	}
	svc := NewDeploymentService(oc, traitStoreWith(files))

	if err := svc.Converge(context.Background(), "acme", "proj", []string{"api"}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	if len(oc.EnsureReleaseCalls()) != 0 {
		t.Errorf("converge cut a release: %d calls", len(oc.EnsureReleaseCalls()))
	}
	calls := oc.ApplyReleaseBindingCalls()
	if len(calls) != 1 {
		t.Fatalf("want one binding write, got %d", len(calls))
	}
	if calls[0].In.ReleaseName != "" {
		t.Errorf("converge re-pinned the binding to %q", calls[0].In.ReleaseName)
	}
}

// A component with no binding yet is skipped: writing one with no release
// pinned produces an object OpenChoreo cannot render.
func TestConverge_SkipsComponentsWithNoBinding(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": plainServiceMd("api"),
	}
	oc := ocDeployments(map[string]string{})
	oc.GetReleaseBindingStatusFunc = func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
		return nil, nil
	}
	svc := NewDeploymentService(oc, traitStoreWith(files))

	if err := svc.Converge(context.Background(), "acme", "proj", []string{"api"}); err != nil {
		t.Fatalf("Converge: %v", err)
	}
	if len(oc.ApplyReleaseBindingCalls()) != 0 {
		t.Errorf("converge wrote a binding for a component that has none")
	}
}

// --- DeploymentState ----------------------------------------------------------

// The three-way answer is the point: "still rolling out" must not read as either
// verdict, or the supervisor gives up on a slow deploy or waits on a broken one.
func TestDeploymentState_ClassifiesReadyFailedAndPending(t *testing.T) {
	t.Parallel()
	byComponent := map[string]*openchoreo.ReleaseBindingSummary{
		"ready":    {ReadyStatus: "True"},
		"failed":   {ReadyStatus: "False", ReadyReason: "RenderingFailed"},
		"rolling":  {ReadyStatus: "Unknown"},
		"absent":   nil,
		"undeploy": {Undeploy: true},
	}
	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(_ context.Context, _, _, componentName, _ string) (*openchoreo.ReleaseBindingSummary, error) {
			return byComponent[componentName], nil
		},
	}
	svc := NewDeploymentService(oc, nil)

	got, err := svc.DeploymentState(context.Background(), "acme", "proj",
		[]string{"ready", "failed", "rolling", "unknown", "absent", "undeploy"})
	if err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	want := map[string][2]bool{ // component -> {ready, failed}
		"ready":    {true, false},
		"failed":   {false, true},
		"rolling":  {false, false},
		"unknown":  {false, false},
		"absent":   {false, false},
		"undeploy": {true, false},
	}
	for _, st := range got {
		w := want[st.Component]
		if st.Ready != w[0] || st.Failed != w[1] {
			t.Errorf("%s: ready/failed = %v/%v, want %v/%v", st.Component, st.Ready, st.Failed, w[0], w[1])
		}
	}
	if got[1].Reason != "RenderingFailed" {
		t.Errorf("the failure reason is not carried through: %q", got[1].Reason)
	}
}

// A binding reports Ready=False from the moment it is created, while it renders.
// Reading that as failure declared two healthy components dead two seconds after
// they were pinned and filed a fix issue for each — the defect this pins.
//
// Only a reason that waiting cannot fix is a verdict; everything else is the
// deadline's business.
func TestDeploymentState_FreshBindingIsPendingNotFailed(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"", "Progressing", "Reconciling", "NotReady", "PendingRollout"} {
		oc := &mocks.ComponentClientMock{
			GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
				return &openchoreo.ReleaseBindingSummary{ReadyStatus: "False", ReadyReason: reason}, nil
			},
		}
		got, err := NewDeploymentService(oc, nil).DeploymentState(context.Background(), "acme", "proj", []string{"api"})
		if err != nil {
			t.Fatalf("DeploymentState(%q): %v", reason, err)
		}
		if got[0].Failed {
			t.Errorf("reason %q read as FAILED; a rollout in progress must stay pending", reason)
		}
		if got[0].Ready {
			t.Errorf("reason %q read as READY; it is not serving yet", reason)
		}
	}
}

// --- siblingSPAOrigins --------------------------------------------------------

func Test_siblingSPAOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("collects each web-app origin, trims path, dedups", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:           traitRootMd(),
			"components/web1/design.json": webAppMd("web1"),
			"components/web2/design.json": webAppMd("web2"),
			"components/api/design.json":  endUserServiceMd("api"),
		}
		design := traitReadDesign(t, files)
		oc := ocDeployments(map[string]string{
			"web1": "http://web1.local/todo/",    // path trimmed → scheme+host
			"web2": "http://web2.local:8080/app", // port preserved
		})
		svc := NewDeploymentService(oc, traitStoreWith(files))

		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		want := map[string]bool{"http://web1.local": true, "http://web2.local:8080": true}
		if len(got) != 2 {
			t.Fatalf("want 2 origins; got %v", got)
		}
		for _, o := range got {
			if !want[o] {
				t.Errorf("unexpected origin %q; want one of %v", o, want)
			}
		}
		// The service component is never consulted for origins — only web-apps.
		for _, c := range oc.ListDeploymentsCalls() {
			if c.ComponentName == "api" {
				t.Errorf("siblingSPAOrigins must not list the service component's deployments")
			}
		}
	})

	t.Run("web-app with no deployment yet contributes nothing", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := ocDeployments(map[string]string{}) // web → empty list
		svc := NewDeploymentService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("undeployed SPA should contribute no origins; got %v", got)
		}
	})

	t.Run("duplicate deployment URLs dedup to one origin", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{
			ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
				return &gen.DeploymentList{Items: []gen.Deployment{
					{EndpointURL: "http://web.local/a"},
					{EndpointURL: "http://web.local/b"}, // same origin
				}}, nil
			},
		}
		svc := NewDeploymentService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 1 || got[0] != "http://web.local" {
			t.Errorf("same-origin deployments must dedup; got %v", got)
		}
	})

	t.Run("transient ListDeployments error surfaces (no partial allowlist)", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{
			ListDeploymentsFunc: func(context.Context, string, string, string) (*gen.DeploymentList, error) {
				return nil, errors.New("oc: transient")
			},
		}
		svc := NewDeploymentService(oc, traitStoreWith(files))
		if _, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design); err == nil {
			t.Fatalf("a sibling lookup error must surface — a partial CORS allowlist would silently block the missing SPA")
		}
	})

	t.Run("no web-apps yields empty slice, nil error", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			spec.DesignRootFile:          traitRootMd(),
			"components/api/design.json": endUserServiceMd("api"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{} // ListDeployments must never be called
		svc := NewDeploymentService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("want empty origins with no web-apps; got %v", got)
		}
		if len(oc.ListDeploymentsCalls()) != 0 {
			t.Errorf("ListDeployments must not be called when there are no web-apps; got %d", len(oc.ListDeploymentsCalls()))
		}
	})
}
