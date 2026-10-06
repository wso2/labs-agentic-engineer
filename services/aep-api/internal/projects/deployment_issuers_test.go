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

// The deploy-time issuer read is read-only (06 §3, O-3): deploying a protected
// API never creates or heals the org's publisher app. The real
// organization IDP service is wired, over a Thunder admin client that counts
// every call, so a write slipping back into resolveIssuers shows as a call.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// countingThunder counts every Thunder admin call the deploy could make on the
// publisher's behalf; any other method is a test bug (nil panic).
type countingThunder struct {
	thundersvc.Client
	calls atomic.Int32
}

func (c *countingThunder) EnsurePublisherApp(context.Context, string, string, string) (thundersvc.OrgApp, error) {
	c.calls.Add(1)
	return thundersvc.OrgApp{}, nil
}

func (c *countingThunder) EnsureOrgApp(context.Context, thundersvc.OrgAppSpec) (thundersvc.OrgApp, error) {
	c.calls.Add(1)
	return thundersvc.OrgApp{}, nil
}

func (c *countingThunder) SetAppSecret(context.Context, string, string) error {
	c.calls.Add(1)
	return nil
}

func (c *countingThunder) DeletePublisherApp(context.Context, string, string) (bool, error) {
	c.calls.Add(1)
	return false, nil
}

// profileRows is an IDPRepository holding at most one profile; writes are
// counted, never applied. A set err fails every read.
type profileRows struct {
	profile *organization.OrganizationIDPProfile
	err     error
	writes  atomic.Int32
}

func (r *profileRows) GetProfileByOrgID(context.Context, string) (*organization.OrganizationIDPProfile, error) {
	return r.profile, r.err
}

func (r *profileRows) CreateProfile(context.Context, *organization.OrganizationIDPProfile) error {
	r.writes.Add(1)
	return nil
}

func (r *profileRows) UpdateProfileColumns(context.Context, *organization.OrganizationIDPProfile, string, map[string]interface{}) error {
	r.writes.Add(1)
	return nil
}

func (r *profileRows) CreateAuditEvent(context.Context, *organization.IDPAuditEvent) error {
	r.writes.Add(1)
	return nil
}

func TestDeploy_ProtectedAPIWithoutPublisherAppMakesNoThunderCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		profile *organization.OrganizationIDPProfile
	}{
		{"no profile", nil},
		{"platform profile, no publisher app", &organization.OrganizationIDPProfile{OrgID: "acme", Kind: "platform"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{
				spec.DesignRootFile:          traitRootMd(),
				"components/api/design.json": endUserServiceMd("api"),
			}
			oc := ocDeployments(map[string]string{})
			svc := newTestDeploymentService(oc, traitStoreWith(files))
			thunder := &countingThunder{}
			rows := &profileRows{profile: tc.profile}
			svc.SetIDPService(organization.NewIDPService(rows, nil, thunder, organization.PlatformIDPConfig{}))

			if _, err := svc.Deploy(context.Background(), "acme", "proj", promoting("abc123def456", "api")); err != nil {
				t.Fatalf("Deploy: %v", err)
			}
			if n := thunder.calls.Load(); n != 0 {
				t.Fatalf("deploy made %d Thunder calls; the issuer read is read-only", n)
			}
			if n := rows.writes.Load(); n != 0 {
				t.Fatalf("deploy wrote the IDP profile %d times; the issuer read is read-only", n)
			}
			if len(oc.ApplyReleaseBindingCalls()) != 1 {
				t.Fatalf("the protected API still deploys: %d binding writes", len(oc.ApplyReleaseBindingCalls()))
			}
		})
	}
}

// A BYO org's issuer still pins the protected API's JWT validation.
func TestResolveIssuers_ReadsTheProfileIssuer(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	design := traitReadDesign(t, files)
	svc := newTestDeploymentService(ocDeployments(nil), traitStoreWith(files))
	thunder := &countingThunder{}
	rows := &profileRows{profile: &organization.OrganizationIDPProfile{OrgID: "acme", Kind: "custom", Issuer: "https://idp.byo.example"}}
	svc.SetIDPService(organization.NewIDPService(rows, nil, thunder, organization.PlatformIDPConfig{}))

	got, err := svc.resolveIssuers(context.Background(), "acme", "proj", design)
	if err != nil {
		t.Fatalf("resolveIssuers: %v", err)
	}
	if len(got) != 1 || got[0] != "https://idp.byo.example" {
		t.Fatalf("issuers = %v, want the profile's issuer", got)
	}
	if thunder.calls.Load() != 0 || rows.writes.Load() != 0 {
		t.Fatalf("thunder calls %d, profile writes %d; want none", thunder.calls.Load(), rows.writes.Load())
	}
}

// An unreadable profile fails the deploy closed: an unpinned trait would let
// any cluster-registered keymanager mint a token the API honours. The error is
// a plain one, so Temporal retries the promote until the profile reads, and
// nothing is written in the meantime.
func TestDeploy_UnreadableIDPProfileFailsRetryablyAndWritesNothing(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	oc := ocDeployments(map[string]string{})
	svc := newTestDeploymentService(oc, traitStoreWith(files))
	rows := &profileRows{err: errors.New("connection refused")}
	svc.SetIDPService(organization.NewIDPService(rows, nil, &countingThunder{}, organization.PlatformIDPConfig{}))

	_, err := svc.Deploy(context.Background(), "acme", "proj", promoting("abc123def456", "api"))
	if err == nil {
		t.Fatal("Deploy succeeded with an unreadable IDP profile; want the deploy refused")
	}
	if errors.Is(err, delivery.ErrDeployPermanent) {
		t.Fatalf("Deploy error is permanent (%v); a profile read failure must be retried", err)
	}
	if n := len(oc.EnsureReleaseCalls()); n != 0 {
		t.Fatalf("%d releases cut; want none before the issuer is known", n)
	}
	if n := len(oc.ApplyReleaseBindingCalls()); n != 0 {
		t.Fatalf("%d bindings written; want none (an unpinned trait trusts every keymanager)", n)
	}
	if n := len(oc.ApplyComponentSpecCalls()); n != 0 {
		t.Fatalf("%d component specs written; want none", n)
	}
}

// The issuer read comes before governance, so a deploy refused on it does not
// re-register the wave's agents with Agent Manager on every retry.
func TestDeploy_UnreadableIDPProfileRefusesBeforeGovernance(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:          traitRootMd(),
		"components/api/design.json": endUserServiceMd("api"),
	}
	svc := newTestDeploymentService(ocDeployments(map[string]string{}), traitStoreWith(files))
	g := &stubGovernor{}
	svc.SetGovernor(g)
	rows := &profileRows{err: errors.New("connection refused")}
	svc.SetIDPService(organization.NewIDPService(rows, nil, &countingThunder{}, organization.PlatformIDPConfig{}))

	if _, err := svc.Deploy(context.Background(), "acme", "proj", promoting("abc123def456", "api")); err == nil {
		t.Fatal("Deploy succeeded with an unreadable IDP profile; want the deploy refused")
	}
	if len(g.seen) != 0 {
		t.Fatalf("governor saw %d targets; the issuer read must refuse the deploy first", len(g.seen))
	}
}

// A saved BYO profile with no issuer has nothing to pin, and an unpinned trait
// trusts every keymanager on the cluster. The deploy is refused and writes
// nothing, with a retryable error, so it goes through once the admin saves an
// issuer.
func TestDeploy_BYOProfileWithoutIssuerFailsRetryablyAndWritesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, kind, issuer string }{
		{"custom, empty issuer", "custom", ""},
		{"custom, blank issuer", "custom", "   "},
		{"asgardeo, empty issuer", "asgardeo", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{
				spec.DesignRootFile:          traitRootMd(),
				"components/api/design.json": endUserServiceMd("api"),
			}
			oc := ocDeployments(map[string]string{})
			svc := newTestDeploymentService(oc, traitStoreWith(files))
			rows := &profileRows{profile: &organization.OrganizationIDPProfile{OrgID: "acme", Kind: tc.kind, Issuer: tc.issuer}}
			svc.SetIDPService(organization.NewIDPService(rows, nil, &countingThunder{}, organization.PlatformIDPConfig{}))

			_, err := svc.Deploy(context.Background(), "acme", "proj", promoting("abc123def456", "api"))
			if err == nil {
				t.Fatal("Deploy succeeded with no issuer to pin; want the deploy refused")
			}
			if errors.Is(err, delivery.ErrDeployPermanent) {
				t.Fatalf("Deploy error is permanent (%v); saving an issuer must let the retry through", err)
			}
			if n := len(oc.EnsureReleaseCalls()) + len(oc.ApplyReleaseBindingCalls()) + len(oc.ApplyComponentSpecCalls()); n != 0 {
				t.Fatalf("%d OpenChoreo writes; want none", n)
			}
		})
	}
}
