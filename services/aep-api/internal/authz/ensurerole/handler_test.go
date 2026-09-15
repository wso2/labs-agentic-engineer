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

package ensurerole

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/authz"
	"github.com/wso2/aep/aep-api/internal/gen"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// fakeOCAuthZClient records the context each call was made with, so tests can
// assert on how the call was authenticated. GetAuthzRole/GetAuthzRoleBinding
// report "not found" so the service always proceeds to create.
type fakeOCAuthZClient struct {
	sawServiceIdentity bool
}

func (f *fakeOCAuthZClient) GetAuthzRole(ctx context.Context, _ string, _ string) (authz.CreatedAuthzRole, error) {
	f.sawServiceIdentity = f.sawServiceIdentity || authn.IsServiceIdentity(ctx)
	return authz.CreatedAuthzRole{}, authz.ErrRoleNotFound
}

func (f *fakeOCAuthZClient) CreateAuthzRole(ctx context.Context, _ string, name string, actions []string) (authz.CreatedAuthzRole, error) {
	f.sawServiceIdentity = f.sawServiceIdentity || authn.IsServiceIdentity(ctx)
	return authz.CreatedAuthzRole{Name: name, Actions: actions}, nil
}

func (f *fakeOCAuthZClient) UpdateAuthzRole(_ context.Context, _ string, name string, actions []string) (authz.CreatedAuthzRole, error) {
	return authz.CreatedAuthzRole{Name: name, Actions: actions}, nil
}

func (f *fakeOCAuthZClient) DeleteAuthzRole(_ context.Context, _ string, _ string) error { return nil }

func (f *fakeOCAuthZClient) GetAuthzRoleBinding(ctx context.Context, _ string, _ string) (authz.CreatedAuthzRoleBinding, error) {
	f.sawServiceIdentity = f.sawServiceIdentity || authn.IsServiceIdentity(ctx)
	return authz.CreatedAuthzRoleBinding{}, authz.ErrRoleBindingNotFound
}

func (f *fakeOCAuthZClient) CreateAuthzRoleBinding(ctx context.Context, _ string, bindingName string, roleName string, entitlement authz.EntitlementClaim) (authz.CreatedAuthzRoleBinding, error) {
	f.sawServiceIdentity = f.sawServiceIdentity || authn.IsServiceIdentity(ctx)
	return authz.CreatedAuthzRoleBinding{Name: bindingName, RoleName: roleName, Entitlement: entitlement}, nil
}

func (f *fakeOCAuthZClient) DeleteAuthzRoleBinding(_ context.Context, _ string, _ string) error {
	return nil
}

// fakeResolver passes AE permissions through unchanged; ensureRole only needs
// a resolver to be present, not any particular mapping.
type fakeResolver struct{}

func (fakeResolver) ResolveOcPermissions(aePermissions []string) []string { return aePermissions }

// TestEnsureAuthzRole_UsesServiceIdentity guards the fix for the onboarding
// failure: a first-time user has no OC-side grants of their own yet, so
// bootstrapping their AE roles must run under the BFF's service identity
// rather than their forwarded JWT, or every OC call 403s.
func TestEnsureAuthzRole_UsesServiceIdentity(t *testing.T) {
	client := &fakeOCAuthZClient{}
	h := New(authz.NewAuthZService(fakeResolver{}, client))

	ctx := tenant.WithBoundOrg(context.Background(), "acme-org")
	if authn.IsServiceIdentity(ctx) {
		t.Fatal("test setup: ctx must not already carry a service identity")
	}

	resp, err := h.EnsureAuthzRole(ctx, gen.EnsureAuthzRoleRequestObject{})
	if err != nil {
		t.Fatalf("EnsureAuthzRole returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("EnsureAuthzRole returned a nil response")
	}
	if !client.sawServiceIdentity {
		t.Fatal("expected the OC client calls to run under the service identity, but none did")
	}
	if authn.IsServiceIdentity(ctx) {
		t.Fatal("the caller's own context must not be mutated by the handler")
	}
}
