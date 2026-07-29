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

package organization_test

// DBTEST tier: split out of credential_installations_test.go. That file stays
// package organization (it drives fetchInstallation/fetchAppBotIdentity,
// unexported methods with no test seam reachable from outside the package),
// but this one test needs a real Postgres (it filters
// ResolveUserInstallations' candidates by reading org_credentials directly),
// so it must live in the external test package — an in-package dbtest file
// would reintroduce the organization→dbtest→migrate→organization cycle.
//
// fakeAppInstallOps and newConfiguredMinter/genTestRSAKeyPEM are duplicated
// from credential_installations_test.go (used only here among the black-box
// files, so they're kept local rather than promoted to dbtest_helpers_test.go).

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// genTestRSAKeyPEM generates a fresh 2048-bit RSA key for App-JWT signing.
func genTestRSAKeyPEM(t testing.TB) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// newConfiguredMinter builds an AppTokenMinter with real key material.
func newConfiguredMinter(t testing.TB, appID int64) *secrets.AppTokenMinter {
	t.Helper()
	m, err := secrets.NewAppTokenMinter(&secrets.AppKeyMaterial{AppID: appID, PrivateKeyPEM: genTestRSAKeyPEM(t)})
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}
	return m
}

// fakeAppInstallOps hand-fakes sourcecontrol.AppInstallOps. ResolveUserInstallations
// only calls ExchangeOAuthCode, GetUserInstallations, and ListAppInstallations;
// the other two methods are not on its path, so a call to one is a test bug —
// they panic.
type fakeAppInstallOps struct {
	exchangeFn     func(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error)
	userInstallsFn func(ctx context.Context, userToken string) ([]int64, error)
	listAppFn      func(ctx context.Context, minter *secrets.AppTokenMinter) ([]sourcecontrol.AppInstallationSummary, error)
}

var _ sourcecontrol.AppInstallOps = (*fakeAppInstallOps)(nil)

func (f *fakeAppInstallOps) GetUser(context.Context, secrets.Credential) (*sourcecontrol.GitHubUser, error) {
	panic("fakeAppInstallOps: GetUser is not on the ResolveUserInstallations path")
}

func (f *fakeAppInstallOps) GetAppInstallation(context.Context, *secrets.AppTokenMinter, int64) (*sourcecontrol.AppInstallationInfo, error) {
	panic("fakeAppInstallOps: GetAppInstallation is not on the ResolveUserInstallations path")
}

func (f *fakeAppInstallOps) ListAppInstallations(ctx context.Context, minter *secrets.AppTokenMinter) ([]sourcecontrol.AppInstallationSummary, error) {
	if f.listAppFn != nil {
		return f.listAppFn(ctx, minter)
	}
	return nil, nil
}

func (f *fakeAppInstallOps) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error) {
	if f.exchangeFn != nil {
		return f.exchangeFn(ctx, clientID, clientSecret, code, redirectURI)
	}
	return "user-token", nil
}

func (f *fakeAppInstallOps) GetUserInstallations(ctx context.Context, userToken string) ([]int64, error) {
	if f.userInstallsFn != nil {
		return f.userInstallsFn(ctx, userToken)
	}
	return nil, nil
}

func (f *fakeAppInstallOps) DeleteInstallation(context.Context, *secrets.AppTokenMinter, int64) error {
	panic("fakeAppInstallOps: DeleteInstallation is not on the ResolveUserInstallations path")
}

// TestResolveUserInstallations_FiltersByUserAccessAndOrgBinding_DB pins the
// cross-tenant filter: only installations the user administers (per
// GetUserInstallations) AND that are either unbound or bound to the
// REQUESTING org survive; installs bound to a different org are dropped so
// their existence isn't leaked to an admin who happens to share GitHub
// access. This filter reads org_credentials directly, so it needs a real DB.
func TestResolveUserInstallations_FiltersByUserAccessAndOrgBinding_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)

	// installation 1: bound to "acme" (the requesting org) — kept.
	insertAppRow(t, db, "acme", 1, "active", nil)
	// installation 2: bound to "other-org" — must be dropped.
	insertAppRow(t, db, "other-org", 2, "active", nil)
	// installation 3: bound to a DIFFERENT other org but disconnected — status
	// filter (`IN ('active','suspended')`) means a disconnected row doesn't
	// count as "bound elsewhere", so this installation is NOT filtered out.
	insertAppRow(t, db, "other-org-2", 3, "disconnected", nil)
	// installation 4: no row at all — unbound, kept.
	// installation 5: the user does NOT administer this one on GitHub — must
	// be dropped regardless of binding.

	minter := newConfiguredMinter(t, 1)
	gh := &fakeAppInstallOps{
		userInstallsFn: func(context.Context, string) ([]int64, error) {
			return []int64{1, 2, 3, 4}, nil // user administers 1-4, not 5
		},
		listAppFn: func(context.Context, *secrets.AppTokenMinter) ([]sourcecontrol.AppInstallationSummary, error) {
			return []sourcecontrol.AppInstallationSummary{
				{InstallationID: 1, AccountLogin: "acme-org", AccountType: "Organization"},
				{InstallationID: 2, AccountLogin: "other-org", AccountType: "Organization"},
				{InstallationID: 3, AccountLogin: "other-org-disconnected", AccountType: "Organization"},
				{InstallationID: 4, AccountLogin: "fresh-org", AccountType: "Organization"},
				{InstallationID: 5, AccountLogin: "no-access-org", AccountType: "Organization"},
			}, nil
		},
	}
	svc := organization.NewCredentialService(organization.NewOrgCredentialRepository(db, nil), nil, minter, "", "cid", "csecret", gh)

	got, err := svc.ResolveUserInstallations(context.Background(), "acme", "code", "https://cb")
	if err != nil {
		t.Fatalf("ResolveUserInstallations: %v", err)
	}
	ids := map[int64]bool{}
	for _, c := range got {
		ids[c.InstallationID] = true
	}
	if len(got) != 3 || !ids[1] || !ids[3] || !ids[4] {
		t.Fatalf("filtered candidates = %+v; want installations {1,3,4}", got)
	}
	if ids[2] {
		t.Fatalf("installation 2 (bound to another active org) must be filtered out: %+v", got)
	}
	if ids[5] {
		t.Fatalf("installation 5 (user has no GitHub access) must be filtered out: %+v", got)
	}
}
