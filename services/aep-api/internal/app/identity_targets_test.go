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

package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/identity"
)

const (
	targetIssuer   = "http://default-idp.amp.localhost:8080"
	targetAdminURL = "http://thunder-acme-default-service.thunder-acme-default.svc.cluster.local:8090"
)

// fakeBindings is the OpenChoreo half: the Environment's binding annotations.
type fakeBindings struct {
	binding openchoreo.ThunderBinding
	err     error
	calls   int
}

func (f *fakeBindings) GetThunderBinding(_ context.Context, orgID, environment string) (openchoreo.ThunderBinding, error) {
	f.calls++
	if f.err != nil {
		return openchoreo.ThunderBinding{}, f.err
	}
	binding := f.binding
	binding.OrgID, binding.Environment = orgID, environment
	return binding, nil
}

// fakeCredentials is the secret-store half.
type fakeCredentials struct {
	fields map[string]string
	err    error
	calls  int
	refs   []string // "namespace/name" per read
}

func (f *fakeCredentials) ReadBindingCredential(_ context.Context, namespace, name string) (map[string]string, error) {
	f.calls++
	f.refs = append(f.refs, namespace+"/"+name)
	return f.fields, f.err
}

// fakeScopeTargets is the write-target half: which environment a project, or
// the org default pipeline, writes into.
type fakeScopeTargets struct {
	project string // answered by Resolve
	root    string // answered by OrgDefaultRoot
	err     error
	calls   []string
}

func (f *fakeScopeTargets) Resolve(_ context.Context, org, project string) (string, error) {
	f.calls = append(f.calls, "Resolve "+org+"/"+project)
	return f.project, f.err
}

func (f *fakeScopeTargets) OrgDefaultRoot(_ context.Context, org string) (string, error) {
	f.calls = append(f.calls, "OrgDefaultRoot "+org)
	return f.root, f.err
}

// acmeDefault is the scope most tests resolve.
var acmeDefault = identity.Scope{OrgID: "acme", Environment: "default"}

func newTestResolver(t *testing.T, route string) (*identityTargetResolver, *fakeBindings, *fakeCredentials) {
	t.Helper()
	bindings := &fakeBindings{binding: openchoreo.ThunderBinding{
		Issuer:                   targetIssuer,
		AdminURL:                 targetAdminURL,
		SystemResourceIdentifier: targetIssuer + "/mcp",
		SecretRef:                "thunder-binding-acme-default",
		Name:                     "thunder-binding-acme-default",
	}}
	credentials := &fakeCredentials{fields: map[string]string{
		"clientId": "aep-system-client", "clientSecret": "s3cret",
	}}
	return newIdentityTargetResolver(bindings, credentials, &fakeScopeTargets{}, route), bindings, credentials
}

// The whole resolution, end to end: the binding names the instance, the secret
// store holds its admin credential, and the Target that comes back carries the
// issuer a published login is valid at.
func TestIdentityTargetResolver_ResolvesFromTheBindingAndTheSecretStore(t *testing.T) {
	resolver, bindings, credentials := newTestResolver(t, adminRouteIssuer)

	target, err := resolver.Resolve(context.Background(), acmeDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if target.Scope() != (identity.Scope{OrgID: "acme", Environment: "default"}) {
		t.Fatalf("scope = %s", target.Scope())
	}
	if target.Issuer != targetIssuer {
		t.Fatalf("issuer = %q — the gate publishes this beside every password", target.Issuer)
	}
	if target.Directory == nil {
		t.Fatal("no directory on the resolved target")
	}
	if bindings.calls != 1 || credentials.calls != 1 {
		t.Fatalf("bindings=%d credentials=%d, want one read each", bindings.calls, credentials.calls)
	}
	if credentials.refs[0] != "acme/thunder-binding-acme-default" {
		t.Fatalf("credential read at %q, want the binding's secret in the org's namespace", credentials.refs[0])
	}
}

// The address admin calls go to is a POLICY, and it is the difference between
// working and not working depending on where aep-api runs: outside the cluster
// only the public issuer resolves, inside it only the binding's Service address
// does.
func TestIdentityTargetResolver_AdminRoutePicksTheAddress(t *testing.T) {
	for route, want := range map[string]string{
		adminRouteIssuer:  targetIssuer,
		adminRouteBinding: targetAdminURL,
		"nonsense":        targetIssuer, // anything unrecognised reads as `issuer`
	} {
		resolver, bindings, _ := newTestResolver(t, route)
		if got := resolver.adminBaseURL(bindings.binding); got != want {
			t.Errorf("route %q → %q, want %q", route, got, want)
		}
	}

	// A binding written before the admin-URL annotation existed still resolves:
	// the public issuer is the fallback, not an error.
	resolver, bindings, _ := newTestResolver(t, adminRouteBinding)
	bindings.binding.AdminURL = ""
	if got := resolver.adminBaseURL(bindings.binding); got != targetIssuer {
		t.Errorf("a binding with no admin URL → %q, want the issuer", got)
	}
}

// Resolving reads the OpenChoreo API and the secret store; a build with a dozen
// roles must not pay for a dozen of those. The cache is per (org, environment),
// so a second org still gets its own.
func TestIdentityTargetResolver_CachesPerScope(t *testing.T) {
	resolver, bindings, credentials := newTestResolver(t, adminRouteIssuer)
	ctx := context.Background()

	for range 3 {
		if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if bindings.calls != 1 || credentials.calls != 1 {
		t.Fatalf("bindings=%d credentials=%d after three resolves of one org, want one each",
			bindings.calls, credentials.calls)
	}

	if _, err := resolver.Resolve(ctx, identity.Scope{OrgID: "globex", Environment: "default"}); err != nil {
		t.Fatalf("Resolve for a second org: %v", err)
	}
	if bindings.calls != 2 {
		t.Fatalf("a second org must resolve its OWN identity provider (bindings=%d)", bindings.calls)
	}
}

// A cached client outlives the TTL only as long as the TTL: with a stale admin
// secret the token MINT fails, so no directory call ever returns a rejection to
// evict on, and the TTL is the only thing that ever re-reads the binding.
func TestIdentityTargetResolver_CacheExpires(t *testing.T) {
	resolver, bindings, _ := newTestResolver(t, adminRouteIssuer)
	now := time.Now()
	resolver.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	now = now.Add(identityTargetTTL - time.Second)
	if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bindings.calls != 1 {
		t.Fatalf("the cache expired early (bindings=%d)", bindings.calls)
	}

	now = now.Add(2 * time.Second)
	if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bindings.calls != 2 {
		t.Fatalf("the binding was not re-read past the TTL (bindings=%d)", bindings.calls)
	}
}

// A REJECTED credential drops the cached client, so the next call re-reads the
// binding and picks up a rotated secret. Nothing else does — evicting on an
// ordinary 404 would rebuild the client on every miss and turn one bad request
// into two network reads.
func TestIdentityTargetResolver_InvalidatesOnARejectedCredential(t *testing.T) {
	resolver, bindings, _ := newTestResolver(t, adminRouteIssuer)
	ctx := context.Background()

	target, err := resolver.Resolve(ctx, acmeDefault)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	dir, ok := target.Directory.(invalidatingDirectory)
	if !ok {
		t.Fatalf("the directory is not cache-aware: %T", target.Directory)
	}

	// An ordinary failure keeps the entry.
	if err := dir.check(errors.New("thunder is busy")); err == nil {
		t.Fatal("check swallowed an error")
	}
	if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bindings.calls != 1 {
		t.Fatalf("an ordinary error evicted the cached client (bindings=%d)", bindings.calls)
	}

	// A rejection drops it.
	dir.invalidate()
	if _, err := resolver.Resolve(ctx, acmeDefault); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if bindings.calls != 2 {
		t.Fatalf("a rejected credential did not force a re-read (bindings=%d)", bindings.calls)
	}
}

// An environment with no identity provider is named as such, with the command
// that fixes it. It is the error a whole build settles on, so it has to say what
// to do rather than surface as a bare 404 from the OpenChoreo API.
func TestIdentityTargetResolver_UnboundEnvironmentNamesTheFix(t *testing.T) {
	resolver, bindings, _ := newTestResolver(t, adminRouteIssuer)
	bindings.err = openchoreo.ErrNoThunderBinding

	_, err := resolver.Resolve(context.Background(), acmeDefault)
	if err == nil {
		t.Fatal("Resolve succeeded for an unbound environment")
	}
	if !errors.Is(err, openchoreo.ErrNoThunderBinding) {
		t.Fatalf("the sentinel was lost: %v", err)
	}
	// aectl platform install binds one environment only, so the error must not
	// offer it as the fix for any other.
	for _, want := range []string{"default", "acme", "aectl platform install", "oc.default_org_namespace", "oc.pipeline_source_environment"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

// A binding whose secret path holds nothing usable is reported the same way, and
// NOT as a working target with empty credentials — which would mint against the
// identity provider as an anonymous client and fail four requests later.
func TestIdentityTargetResolver_MissingCredentialIsNotAUsableTarget(t *testing.T) {
	for name, credentials := range map[string]*fakeCredentials{
		"nothing in the secret": {fields: nil},
		"no client secret":      {fields: map[string]string{"clientId": "aep-system-client"}},
		"read failed":           {err: errors.New("openchoreo: connection refused")},
	} {
		t.Run(name, func(t *testing.T) {
			bindings := &fakeBindings{binding: openchoreo.ThunderBinding{
				Issuer: targetIssuer, SystemResourceIdentifier: targetIssuer + "/mcp",
				SecretRef: "thunder-binding-acme-default",
			}}
			resolver := newIdentityTargetResolver(bindings, credentials, &fakeScopeTargets{}, adminRouteIssuer)
			if _, err := resolver.Resolve(context.Background(), acmeDefault); err == nil {
				t.Fatal("Resolve returned a target with no usable admin credential")
			}
		})
	}
}

// Scope names the environment the PROJECT writes into, read from its own
// pipeline, and makes no binding or credential read.
func TestIdentityTargetResolver_ScopeFollowsTheProjectsWriteTarget(t *testing.T) {
	bindings := &fakeBindings{}
	credentials := &fakeCredentials{}
	targets := &fakeScopeTargets{project: "development", root: "org-root"}
	resolver := newIdentityTargetResolver(bindings, credentials, targets, adminRouteIssuer)

	got, err := resolver.Scope(context.Background(), "acme", "shop")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if got != (identity.Scope{OrgID: "acme", Environment: "development"}) {
		t.Fatalf("Scope = %s", got)
	}
	if len(targets.calls) != 1 || targets.calls[0] != "Resolve acme/shop" {
		t.Fatalf("write-target calls = %v", targets.calls)
	}
	if bindings.calls != 0 || credentials.calls != 0 {
		t.Fatalf("Scope read the binding (bindings=%d credentials=%d)", bindings.calls, credentials.calls)
	}
}

// With no project in hand (the org's group catalog), the scope is the org
// default pipeline's root.
func TestIdentityTargetResolver_ScopeWithoutAProjectIsTheOrgDefault(t *testing.T) {
	targets := &fakeScopeTargets{project: "development", root: "org-root"}
	resolver := newIdentityTargetResolver(&fakeBindings{}, &fakeCredentials{}, targets, adminRouteIssuer)

	got, err := resolver.Scope(context.Background(), "acme", "")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if got != (identity.Scope{OrgID: "acme", Environment: "org-root"}) {
		t.Fatalf("Scope = %s", got)
	}
	if len(targets.calls) != 1 || targets.calls[0] != "OrgDefaultRoot acme" {
		t.Fatalf("write-target calls = %v", targets.calls)
	}
}

// The write target's error comes back unchanged, so a caller can still tell a
// configuration fact (ErrNoWriteTarget) from a transient failure.
func TestIdentityTargetResolver_ScopePropagatesTheWriteTargetError(t *testing.T) {
	boom := &openchoreo.ErrNoWriteTarget{}
	targets := &fakeScopeTargets{err: boom}
	resolver := newIdentityTargetResolver(&fakeBindings{}, &fakeCredentials{}, targets, adminRouteIssuer)

	for _, project := range []string{"shop", ""} {
		if _, err := resolver.Scope(context.Background(), "acme", project); !errors.Is(err, boom) {
			t.Fatalf("Scope(%q) error = %v, want the write target's", project, err)
		}
	}
}

// Resolve reads the binding of exactly the scope it was handed, and caches per
// scope: two environments of one org are two identity providers.
func TestIdentityTargetResolver_ResolveReadsTheScopesOwnBinding(t *testing.T) {
	resolver, _, _ := newTestResolver(t, adminRouteIssuer)
	bindings := &recordingBindings{fakeBindings: resolver.environments.(*fakeBindings)}
	resolver.environments = bindings
	ctx := context.Background()

	for _, env := range []string{"development", "staging", "development"} {
		target, err := resolver.Resolve(ctx, identity.Scope{OrgID: "acme", Environment: env})
		if err != nil {
			t.Fatalf("Resolve %s: %v", env, err)
		}
		if target.Environment != env {
			t.Fatalf("target environment = %q, want %q", target.Environment, env)
		}
	}
	if len(bindings.envs) != 2 || bindings.envs[0] != "development" || bindings.envs[1] != "staging" {
		t.Fatalf("binding reads = %v, want one per environment", bindings.envs)
	}
}

// recordingBindings records which environment each binding read addressed.
type recordingBindings struct {
	*fakeBindings
	envs []string
}

func (r *recordingBindings) GetThunderBinding(ctx context.Context, orgID, environment string) (openchoreo.ThunderBinding, error) {
	r.envs = append(r.envs, environment)
	return r.fakeBindings.GetThunderBinding(ctx, orgID, environment)
}

type fakeSecretValues struct {
	secretmanagersvc.SecretManagementClient
	values map[string]map[string]string // "namespace/name" -> data
	read   []secretmanagersvc.SecretLocation
}

func (f *fakeSecretValues) GetSecretWithValue(_ context.Context, loc secretmanagersvc.SecretLocation) (map[string]string, error) {
	f.read = append(f.read, loc)
	v, ok := f.values[loc.ControlPlaneNamespace+"/"+loc.SecretRefName()]
	if !ok {
		return nil, secretmanagersvc.ErrSecretNotFound
	}
	return v, nil
}

func TestOpenChoreoBindingCredentials_ReadsTheNamedSecret(t *testing.T) {
	store := &fakeSecretValues{values: map[string]map[string]string{
		"acme/thunder-binding-acme-default": {"clientId": "aep-system-client", "clientSecret": "s3cret"},
	}}
	got, err := openChoreoBindingCredentials{secrets: store}.ReadBindingCredential(context.Background(), "acme", "thunder-binding-acme-default")
	if err != nil || got["clientSecret"] != "s3cret" {
		t.Fatalf("ReadBindingCredential = (%v, %v)", got, err)
	}

	_, err = openChoreoBindingCredentials{secrets: store}.ReadBindingCredential(context.Background(), "acme", "thunder-binding-acme-staging")
	if !errors.Is(err, secretmanagersvc.ErrSecretNotFound) {
		t.Fatalf("missing secret: err = %v, want ErrSecretNotFound", err)
	}
}
