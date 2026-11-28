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

package build

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/spec"
)

var ctx = context.Background()

// ----- coordinator fakes -----------------------------------------------------

// (fakeDesign — the canned design-component reader whose ConfigKey.Secret flags
// are the secret-vs-nonsecret source of truth the coordinator splits on — is
// declared in preflight_test.go.)

// stripeExternal is a service component with one external dependency whose
// schema marks STRIPE_KEY secret and STRIPE_ORG non-secret.
func stripeExternal() []spec.DesignComponent {
	return []spec.DesignComponent{{
		Name:          "o",
		ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{{
			Kind: spec.DependencyKindExternal,
			Name: "stripe",
			Config: []spec.ConfigKey{
				{Key: "STRIPE_KEY", Secret: true},
				{Key: "STRIPE_ORG", Secret: false, DefaultValue: "org_default"},
			},
		}},
	}}
}

type recordingSpec struct {
	calls []specCall
	err   error // when set, every CollectSpec call fails with it
}

type specCall struct {
	component, dep, url string
	raw                 []byte
}

func (s *recordingSpec) CollectSpec(_ context.Context, _, _, component, dep string, raw []byte, url string) (string, error) {
	s.calls = append(s.calls, specCall{component: component, dep: dep, url: url, raw: raw})
	if s.err != nil {
		return "", s.err
	}
	return "components/" + component + "/dependencies/" + dep + ".openapi.yaml", nil
}

type recordingAuth struct {
	calls int
	err   error
}

func (a *recordingAuth) DerivePlatformResourceFactsAtHead(context.Context, string, string) error {
	a.calls++
	return a.err
}

// ----- BuildProvisionInputs --------------------------------------------------

func TestBuildProvisionInputs_DerivesUnsetExternalConfigFromDesign(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: stripeExternal()}, status: fakeStatus{}}
	pins, err := c.BuildProvisionInputs(ctx, "acme", "shop", []BuildInputItem{
		{Component: "o", Dependency: "stripe", Kind: "external-config",
			Values: []ConfigValue{{Key: "STRIPE_KEY", Value: "sk_live"}, {Key: "STRIPE_ORG", Value: "org_1"}}}})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.Equal(t, "external-config", pins[0].Kind)
	require.Equal(t, "stripe", pins[0].Dependency)
	require.Equal(t, map[string]string{"STRIPE_ORG": "org_default"}, pins[0].Config)
	require.Empty(t, pins[0].SecretRefByEnv)
}

// readyByName reports a dependency as already provisioned or in flight when
// its name is in the set — the preflight "already handled" bool.
type readyByName map[string]bool

func (r readyByName) Ready(_ context.Context, _, _, dep string) (bool, error) {
	return r[dep], nil
}

// notesApp is a service and a web app sharing one Thunder sign-in client, with
// a CNPG database on the service.
func notesApp() []spec.DesignComponent {
	return []spec.DesignComponent{
		{Name: "api", ComponentType: spec.ComponentTypeService, Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "notes-db", ResourceType: "postgres",
				Parameters: map[string]any{"instances": 1, "storage": "1Gi"}},
			{Kind: spec.DependencyKindPlatformResource, Name: "user-auth", ResourceType: "idp-app"},
		}},
		{Name: "web", ComponentType: spec.ComponentTypeWebApplication, Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "user-auth", ResourceType: "idp-app"},
		}},
	}
}

// The console sends no inputs: the click is the consent. Every platform
// resource in the design becomes one approved provision carrying the
// design-authored parameters — the same item preflight reports — and a
// resource two components declare is provisioned once.
func TestBuildProvisionInputs_DerivesApprovedPlatformResourcesFromDesign(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: notesApp()}, status: fakeStatus{}}
	pins, err := c.BuildProvisionInputs(ctx, "acme", "notes", nil)
	require.NoError(t, err)
	require.Len(t, pins, 2)
	require.Equal(t, "platform-resource", pins[0].Kind)
	require.Equal(t, "api", pins[0].Component)
	require.Equal(t, "notes-db", pins[0].Dependency)
	require.Equal(t, map[string]any{"instances": 1, "storage": "1Gi"}, pins[0].Parameters)
	require.True(t, pins[0].Approved)
	require.Equal(t, "user-auth", pins[1].Dependency)
	require.True(t, pins[1].Approved)
}

// A resource already provisioned or in flight is left out, exactly as
// preflight leaves it out, so a later version does not re-provision it.
func TestBuildProvisionInputs_SkipsAlreadyProvisionedPlatformResource(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: notesApp()}, status: readyByName{"notes-db": true}}
	pins, err := c.BuildProvisionInputs(ctx, "acme", "notes", nil)
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.Equal(t, "user-auth", pins[0].Dependency)
}

// The design is authoritative: a request entry for the same resource, even an
// unapproved one with other parameters, neither duplicates nor overrides it.
func TestBuildProvisionInputs_RequestPlatformResourceDoesNotDuplicateOrOverride(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: notesApp()}, status: readyByName{"user-auth": true}}
	pins, err := c.BuildProvisionInputs(ctx, "acme", "notes", []BuildInputItem{
		{Component: "api", Dependency: "notes-db", Kind: "platform-resource",
			Parameters: map[string]any{"instances": 3}, Approved: false}})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.Equal(t, "notes-db", pins[0].Dependency)
	require.Equal(t, map[string]any{"instances": 1, "storage": "1Gi"}, pins[0].Parameters)
	require.True(t, pins[0].Approved)
}

// An org-service entry carries the resolution drawer's approval through verbatim.
func TestBuildProvisionInputs_OrgServicePassThrough(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: nil}, status: fakeStatus{}}
	pins, err := c.BuildProvisionInputs(ctx, "acme", "shop", []BuildInputItem{
		{Component: "api", Dependency: "billing", Kind: "org-service", Approved: true}})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	require.Equal(t, "org-service", pins[0].Kind)
	require.Equal(t, "billing", pins[0].Dependency)
	require.True(t, pins[0].Approved)
}

// A status read failure refuses the payload rather than guessing: provisioning
// a resource that already exists, or skipping one that does not, are both wrong.
func TestBuildProvisionInputs_StatusErrorReturnsError(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: notesApp()}, status: fakeStatus{err: errors.New("oc down")}}
	_, err := c.BuildProvisionInputs(ctx, "acme", "notes", nil)
	require.Error(t, err)
}

func TestBuildProvisionInputs_MissingStatusReaderReturnsError(t *testing.T) {
	c := &InputsCoordinator{design: fakeDesign{comps: notesApp()}}
	_, err := c.BuildProvisionInputs(ctx, "acme", "notes", nil)
	require.Error(t, err)
}

func TestBuildProvisionInputs_MissingDesignReaderReturnsError(t *testing.T) {
	c := &InputsCoordinator{}
	_, err := c.BuildProvisionInputs(ctx, "acme", "shop", nil)
	require.Error(t, err)
}

// ----- ApplyPreTag -----------------------------------------------------------

// Pre-tag fans out: one CollectSpec per external-spec input (content → rawSpec,
// url → specURL), and the end-user-auth derivation runs exactly ONCE.
func TestApplyPreTag_FansOut(t *testing.T) {
	spec := &recordingSpec{}
	auth := &recordingAuth{}
	c := &InputsCoordinator{spec: spec, auth: auth}
	fails, err := c.ApplyPreTag(ctx, "acme", "shop", []BuildInputItem{
		{Component: "o", Dependency: "stripe", Kind: "external-spec", SpecContent: "openapi: 3.0.0"},
		{Component: "o", Dependency: "weather", Kind: "external-spec", SpecURL: "https://x/openapi.yaml"},
		{Component: "o", Dependency: "orders-db", Kind: "platform-resource"},
	})
	require.NoError(t, err)
	require.Empty(t, fails)
	require.Len(t, spec.calls, 2) // only the two external-spec inputs
	require.Equal(t, 1, auth.calls)
	require.Equal(t, []byte("openapi: 3.0.0"), spec.calls[0].raw)
	require.Empty(t, spec.calls[0].url)
	require.Equal(t, "https://x/openapi.yaml", spec.calls[1].url)
	require.Nil(t, spec.calls[1].raw)
}

// An auth-derivation conflict propagates as the error (the handler maps it to
// 409) — it is NOT a per-input failure.
func TestApplyPreTag_AuthConflictReturnsError(t *testing.T) {
	auth := &recordingAuth{err: ErrEndUserAuthConflict}
	c := &InputsCoordinator{spec: &recordingSpec{}, auth: auth}
	_, err := c.ApplyPreTag(ctx, "acme", "shop", nil)
	require.ErrorIs(t, err, ErrEndUserAuthConflict)
}

// An unknown resourceType is the same shape: a pre-tag error, not a drawer
// failure, so the handler can map it to 409 and cut no tag.
func TestApplyPreTag_UnknownResourceTypeReturnsError(t *testing.T) {
	auth := &recordingAuth{err: ErrUnknownResourceType}
	c := &InputsCoordinator{spec: &recordingSpec{}, auth: auth}
	_, err := c.ApplyPreTag(ctx, "acme", "shop", nil)
	require.ErrorIs(t, err, ErrUnknownResourceType)
}

// When a spec collection fails the build is aborting, so auth derivation must
// NOT run (it would commit to HEAD for a build that cuts no tag) and the spec
// failures must be returned intact rather than masked by an auth error.
func TestApplyPreTag_SpecFailureSkipsAuthDerivation(t *testing.T) {
	spec := &recordingSpec{err: errors.New("fetch failed")}
	auth := &recordingAuth{}
	c := &InputsCoordinator{spec: spec, auth: auth}
	fails, err := c.ApplyPreTag(ctx, "acme", "shop", []BuildInputItem{
		{Component: "o", Dependency: "stripe", Kind: "external-spec", SpecURL: "https://x/openapi.yaml"},
	})
	require.NoError(t, err)
	require.Len(t, fails, 1)
	require.Equal(t, "stripe", fails[0].Dependency)
	require.Equal(t, 0, auth.calls) // auth derivation skipped on spec failure
}
