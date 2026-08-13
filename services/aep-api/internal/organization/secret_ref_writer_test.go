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

// UNIT + DBTEST tiers for SecretRefWriter: the
// SM-API client is faked at its edge (secretmanagersvc.SecretManagementClient
// — CreateSecret/DeleteSecret are the only two methods on SecretRefWriter's path;
// the rest panic on a stray call). The Write* happy path cannot be driven all
// the way to its DB stamp from this package: resolveVaultKey reads
// jwtassertion.GetTokenClaims(ctx), whose context key is an unexported type
// of package jwtassertion, so no test outside that package (short of a full
// JWKS-verified Authenticator run, which belongs at the HTTP/component tier)
// can populate it. Every Write* test below therefore exercises SM-API upload
// + the (deterministic, claims-less) resolveVaultKey failure, and proves the
// DB is never touched on that branch (db: nil is a poison pill — any
// accidental persistence call panics the test instead of silently passing).
// The Delete* methods have no such dependency (they only read ctx for
// db.WithContext), so their DB-shaped behavior (row load, triplet clear,
// "already gone" idempotency) is pinned for real against dbtest.New.
package organization_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// --- fake secretmanagersvc.SecretManagementClient ----------------------------

// fakeSMClient hand-fakes secretmanagersvc.SecretManagementClient. SecretRefWriter
// only ever calls CreateSecret/DeleteSecret; the other three methods are not
// on its path, so a call to one is a test bug — they panic.
type fakeSMClient struct {
	createCalls []smCreateCall
	deleteCalls []smDeleteCall

	createRef string // returned by CreateSecret on success; defaults to "ref-name"
	createErr error
	deleteErr error
}

type smCreateCall struct {
	loc  secretmanagersvc.SecretLocation
	data map[string]string
}

type smDeleteCall struct {
	loc           secretmanagersvc.SecretLocation
	secretRefName string
}

var _ secretmanagersvc.SecretManagementClient = (*fakeSMClient)(nil)

func (f *fakeSMClient) CreateSecret(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	f.createCalls = append(f.createCalls, smCreateCall{loc: loc, data: data})
	if f.createErr != nil {
		return "", f.createErr
	}
	if f.createRef != "" {
		return f.createRef, nil
	}
	return "ref-name", nil
}

func (f *fakeSMClient) DeleteSecret(_ context.Context, loc secretmanagersvc.SecretLocation, secretRefName string) error {
	f.deleteCalls = append(f.deleteCalls, smDeleteCall{loc: loc, secretRefName: secretRefName})
	return f.deleteErr
}

func (f *fakeSMClient) PatchSecret(context.Context, secretmanagersvc.SecretLocation, map[string]string, []string) (string, error) {
	panic("fakeSMClient: PatchSecret is not on SecretRefWriter's path")
}

func (f *fakeSMClient) GetSecret(context.Context, string) (*secretmanagersvc.SecretInfo, error) {
	panic("fakeSMClient: GetSecret is not on SecretRefWriter's path")
}

func (f *fakeSMClient) GetSecretWithValue(context.Context, string) (map[string]string, error) {
	panic("fakeSMClient: GetSecretWithValue is not on SecretRefWriter's path")
}

// --- Enabled / nil-safety -----------------------------------------------------

func TestSecretRefWriter_Enabled(t *testing.T) {
	t.Parallel()

	if w := organization.NewSecretRefWriter(nil, nil, nil, nil); w.Enabled() {
		t.Fatalf("nil client must report Enabled() == false")
	}
	if w := organization.NewSecretRefWriter(&fakeSMClient{}, nil, nil, nil); !w.Enabled() {
		t.Fatalf("non-nil client must report Enabled() == true")
	}
	// Quirk: Enabled() is nil-receiver-safe (w != nil check first), so callers
	// can invoke it on a possibly-nil *organization.SecretRefWriter without a guard.
	var nilWriter *organization.SecretRefWriter
	if nilWriter.Enabled() {
		t.Fatalf("nil *organization.SecretRefWriter must report Enabled() == false, not panic")
	}
}

// --- WriteAnthropic ------------------------------------------------------------

func TestSecretRefWriter_WriteAnthropic(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		ref, err := w.WriteAnthropic(context.Background(), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
		if err != nil || ref != "" {
			t.Fatalf("disabled WriteAnthropic = (%q, %v); want (\"\", nil)", ref, err)
		}
	})

	t.Run("empty ocOrgID is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		if _, err := w.WriteAnthropic(context.Background(), "  ", organization.AnthropicRoleDefault, "sk-ant-key"); err == nil {
			t.Fatalf("want an error for empty ocOrgID")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("empty apiKey is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		if _, err := w.WriteAnthropic(context.Background(), "acme", organization.AnthropicRoleDefault, "   "); err == nil {
			t.Fatalf("want an error for empty apiKey")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("uploads to the anthropic location with the api-key payload", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedAnthropicRow(t, db, "acme", nil, nil, nil)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		ref, err := w.WriteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
		if err != nil {
			t.Fatalf("WriteAnthropic: %v", err)
		}
		if ref != "ref-name" {
			t.Fatalf("secretRefName = %q; want ref-name", ref)
		}
		if len(fake.createCalls) != 1 {
			t.Fatalf("want exactly 1 CreateSecret call, got %d", len(fake.createCalls))
		}
		call := fake.createCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "anthropic", SecretKey: secretmanagersvc.SecretKeyAPIKey}
		if call.loc != wantLoc {
			t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
		}
		if len(call.data) != 1 || call.data[secretmanagersvc.SecretKeyAPIKey] != "sk-ant-key" {
			t.Fatalf("payload = %v; want {%q: sk-ant-key}", call.data, secretmanagersvc.SecretKeyAPIKey)
		}
	})

	t.Run("no ouId claim fails before CreateSecret", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		_, err := w.WriteAnthropic(context.Background(), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
		if err == nil || !strings.Contains(err.Error(), "anthropic upload") {
			t.Fatalf("want a wrapped upload error, got %v", err)
		}
		if !strings.Contains(err.Error(), "no ouId claim") {
			t.Fatalf("want the underlying no-ouId-claim cause preserved, got %v", err)
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called without ouId, got %d", len(fake.createCalls))
		}
	})

	t.Run("CreateSecret error is wrapped and returned; db untouched", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createErr: errors.New("sm-api: 503")}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		ref, err := w.WriteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
		if err == nil || ref != "" {
			t.Fatalf("WriteAnthropic = (%q, %v); want (\"\", wrapped error)", ref, err)
		}
		if !strings.Contains(err.Error(), "anthropic upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

// --- WriteExternalResourceSecret -------------------------------------------------

func TestSecretRefWriter_WriteExternalResourceSecret(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(context.Background(), "acme", "proj", "extres-openweather-development", map[string]string{"K": "v"})
		if err != nil || vaultKey != "" || ref != "" {
			t.Fatalf("disabled WriteExternalResourceSecret = (%q, %q, %v); want (\"\", \"\", nil)", vaultKey, ref, err)
		}
	})

	t.Run("empty ocOrgID/projectName/entityName is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		for _, args := range [][3]string{
			{"  ", "proj", "extres-x-dev"},
			{"acme", "", "extres-x-dev"},
			{"acme", "proj", "   "},
		} {
			if _, _, err := w.WriteExternalResourceSecret(context.Background(), args[0], args[1], args[2], map[string]string{"K": "v"}); err == nil {
				t.Fatalf("want an error for args %v", args)
			}
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("empty data is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		if _, _, err := w.WriteExternalResourceSecret(context.Background(), "acme", "proj", "extres-x-dev", nil); err == nil {
			t.Fatalf("want an error for empty data")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("uploads to the project-scoped entity location with the full payload", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(claimsCtx("ou-acme-uuid"), "acme", "weatherproj", "extres-openweather-development",
			map[string]string{"OPENWEATHER_API_KEY": "k123"})
		if err != nil {
			t.Fatalf("WriteExternalResourceSecret: %v", err)
		}
		if ref != "ref-name" || vaultKey == "" {
			t.Fatalf("got vaultKey=%q ref=%q", vaultKey, ref)
		}
		if len(fake.createCalls) != 1 {
			t.Fatalf("want exactly 1 CreateSecret call, got %d", len(fake.createCalls))
		}
		call := fake.createCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", ProjectName: "weatherproj", EntityName: "extres-openweather-development"}
		if call.loc != wantLoc {
			t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
		}
		if len(call.data) != 1 || call.data["OPENWEATHER_API_KEY"] != "k123" {
			t.Fatalf("payload = %v; want the secret key/value map verbatim", call.data)
		}
	})

	t.Run("CreateSecret error is wrapped and returned", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createErr: errors.New("sm-api: 503")}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(claimsCtx("ou-acme-uuid"), "acme", "proj", "extres-x-dev", map[string]string{"K": "v"})
		if err == nil || vaultKey != "" || ref != "" {
			t.Fatalf("WriteExternalResourceSecret = (%q, %q, %v); want (\"\", \"\", wrapped error)", vaultKey, ref, err)
		}
		if !strings.Contains(err.Error(), "external-resource secret upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

// --- WriteGitHubPAT --------------------------------------------------------------

func TestSecretRefWriter_WriteGitHubPAT(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		ref, err := w.WriteGitHubPAT(context.Background(), "acme", "ghp_token")
		if err != nil || ref != "" {
			t.Fatalf("disabled WriteGitHubPAT = (%q, %v); want (\"\", nil)", ref, err)
		}
	})

	t.Run("empty ocOrgID is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		if _, err := w.WriteGitHubPAT(context.Background(), "", "ghp_token"); err == nil {
			t.Fatalf("want an error for empty ocOrgID")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("empty pat is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		if _, err := w.WriteGitHubPAT(context.Background(), "acme", ""); err == nil {
			t.Fatalf("want an error for empty pat")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("uploads to the github-pat location with the api-key payload", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", nil, nil)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		ref, err := w.WriteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme", "ghp_token")
		if err != nil {
			t.Fatalf("WriteGitHubPAT: %v", err)
		}
		if ref != "ref-name" {
			t.Fatalf("secretRefName = %q; want ref-name", ref)
		}
		if len(fake.createCalls) != 1 {
			t.Fatalf("want exactly 1 CreateSecret call, got %d", len(fake.createCalls))
		}
		call := fake.createCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "github-pat", SecretKey: secretmanagersvc.SecretKeyAPIKey}
		if call.loc != wantLoc {
			t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
		}
		if len(call.data) != 1 || call.data[secretmanagersvc.SecretKeyAPIKey] != "ghp_token" {
			t.Fatalf("payload = %v; want {%q: ghp_token}", call.data, secretmanagersvc.SecretKeyAPIKey)
		}
	})

	t.Run("CreateSecret error is wrapped and returned", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		ref, err := w.WriteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme", "ghp_token")
		if err == nil || ref != "" {
			t.Fatalf("WriteGitHubPAT = (%q, %v); want (\"\", wrapped error)", ref, err)
		}
		if !strings.Contains(err.Error(), "github-pat upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

// --- WritePublisher --------------------------------------------------------------

func TestSecretRefWriter_WritePublisher(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		ref, err := w.WritePublisher(context.Background(), "acme", "cid", "csecret")
		if err != nil || ref != "" {
			t.Fatalf("disabled WritePublisher = (%q, %v); want (\"\", nil)", ref, err)
		}
	})

	t.Run("empty ocOrgID/clientID/clientSecret are each validation errors", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		cases := []struct {
			name, org, id, secret string
		}{
			{"empty ocOrgID", "", "cid", "csecret"},
			{"empty clientID", "acme", "", "csecret"},
			{"empty clientSecret", "acme", "cid", ""},
		}
		for _, tc := range cases {
			if _, err := w.WritePublisher(context.Background(), tc.org, tc.id, tc.secret); err == nil {
				t.Errorf("%s: want a validation error", tc.name)
			}
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on any validation failure")
		}
	})

	t.Run("uploads to the publisher location as a 2-field payload with no SecretKey", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedIDPProfileRow(t, db, "acme", nil, nil)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		ref, err := w.WritePublisher(claimsCtx("ou-acme-uuid"), "acme", "cid", "csecret")
		if err != nil {
			t.Fatalf("WritePublisher: %v", err)
		}
		if ref != "ref-name" {
			t.Fatalf("secretRefName = %q; want ref-name", ref)
		}
		if len(fake.createCalls) != 1 {
			t.Fatalf("want exactly 1 CreateSecret call, got %d", len(fake.createCalls))
		}
		call := fake.createCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "publisher"}
		if call.loc != wantLoc {
			t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
		}
		wantData := map[string]string{
			organization.PublisherSecretFieldClientID:     "cid",
			organization.PublisherSecretFieldClientSecret: "csecret",
		}
		if len(call.data) != len(wantData) || call.data[organization.PublisherSecretFieldClientID] != "cid" || call.data[organization.PublisherSecretFieldClientSecret] != "csecret" {
			t.Fatalf("payload = %v; want %v", call.data, wantData)
		}
	})

	t.Run("CreateSecret error is wrapped and returned", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil)
		ref, err := w.WritePublisher(claimsCtx("ou-acme-uuid"), "acme", "cid", "csecret")
		if err == nil || ref != "" {
			t.Fatalf("WritePublisher = (%q, %v); want (\"\", wrapped error)", ref, err)
		}
		if !strings.Contains(err.Error(), "publisher upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

// --- resolveVaultKey (via the exported Write* surface) ----------------------

// TestSecretRefWriter_ResolveVaultKey_NoClaimsInContext pins resolveVaultKey's
// exact error text. resolveVaultKey itself is unexported and unreachable from
// this black-box test, so this drives it indirectly through WriteAnthropic
// (any Write* would do — see the WriteAnthropic "resolve-vault-key failure"
// subtest for the same path with a Contains check). Go's %w wrapping
// preserves the wrapped error's Error() text verbatim as a suffix, so the
// precise underlying message is still pinned exactly, just reached through
// the public API instead of the private method. resolveVaultKey never touches
// the DB (it derives the path from the JWT, deliberately not from the local
// `organizations.uuid` row — see its doc comment), so db: nil is safe here too.
func TestSecretRefWriter_ResolveVaultKey_NoClaimsInContext(t *testing.T) {
	t.Parallel()
	w := organization.NewSecretRefWriter(&fakeSMClient{}, nil, nil, nil)
	_, err := w.WriteAnthropic(context.Background(), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
	if err == nil {
		t.Fatalf("want an error when ctx carries no JWT claims")
	}
	if !strings.HasSuffix(err.Error(), "no ouId claim in JWT context") {
		t.Fatalf("resolveVaultKey error = %q; want it to end with the exact no-ouId-claim message", err.Error())
	}
}

// --- DeleteAnthropic (DB) -----------------------------------------------------

// seedAnthropicRow inserts a minimal valid org_anthropic_credentials row,
// optionally with the secret-ref triplet populated on BOTH column sets
// (EXPAND dual-write shape).
func seedAnthropicRow(t testing.TB, db *gorm.DB, ocOrgID string, refName, kvPath, prop *string) {
	t.Helper()
	row := organization.OrgAnthropicCredential{
		OcOrgID:            ocOrgID,
		KeyPrefix:          "sk-ant-api03-",
		KeyLast4:           "wxyz",
		Status:             "active",
		SecretRefName:      refName,
		SecretRefKVPath:    kvPath,
		SecretRefProperty:  prop,
		SMAPISecretRefName: refName,
		SMAPIKVPath:        kvPath,
		SMAPIProperty:      prop,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed anthropic row %s: %v", ocOrgID, err)
	}
}

func strPtr(s string) *string { return &s }

func TestSecretRefWriter_DeleteAnthropic_DB(t *testing.T) {
	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		if err := w.DeleteAnthropic(context.Background(), "acme", organization.AnthropicRoleDefault); err != nil {
			t.Fatalf("disabled DeleteAnthropic = %v; want nil", err)
		}
	})

	t.Run("no row for org is a no-op (idempotent), SM-API never called", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteAnthropic(context.Background(), "ghost-org", organization.AnthropicRoleDefault); err != nil {
			t.Fatalf("DeleteAnthropic on a missing row = %v; want nil", err)
		}
		if len(fake.deleteCalls) != 0 {
			t.Fatalf("DeleteSecret must not be called when no row exists")
		}
	})

	t.Run("clears the triplet after a successful SM-API delete", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedAnthropicRow(t, db, "acme", strPtr("acme-anthropic-secrets"), strPtr("user-app-secrets/wc-xxx/acme-anthropic-secrets"), strPtr("api-key"))

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault); err != nil {
			t.Fatalf("DeleteAnthropic: %v", err)
		}
		if len(fake.deleteCalls) != 1 {
			t.Fatalf("want 1 DeleteSecret call, got %d", len(fake.deleteCalls))
		}
		call := fake.deleteCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "anthropic", SecretKey: secretmanagersvc.SecretKeyAPIKey}
		if call.loc != wantLoc || call.secretRefName != "acme-anthropic-secrets" {
			t.Fatalf("DeleteSecret called with loc=%+v ref=%q; want loc=%+v ref=%q", call.loc, call.secretRefName, wantLoc, "acme-anthropic-secrets")
		}
		var got organization.OrgAnthropicCredential
		if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SecretRefName != nil || got.SecretRefKVPath != nil || got.SecretRefProperty != nil ||
			got.SMAPISecretRefName != nil || got.SMAPIKVPath != nil || got.SMAPIProperty != nil {
			t.Fatalf("triplet not cleared on both column sets: %+v", got)
		}
	})

	t.Run("nil SMAPISecretRefName on the row passes an empty refName to DeleteSecret", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedAnthropicRow(t, db, "acme", nil, nil, nil)

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault); err != nil {
			t.Fatalf("DeleteAnthropic: %v", err)
		}
		if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != "" {
			t.Fatalf("want DeleteSecret called with empty secretRefName, got %+v", fake.deleteCalls)
		}
	})

	t.Run("SM-API delete error propagates and the row is left untouched", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedAnthropicRow(t, db, "acme", strPtr("acme-anthropic-secrets"), strPtr("kv/path"), strPtr("api-key"))

		fake := &fakeSMClient{deleteErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault); err == nil {
			t.Fatalf("want the SM-API error to propagate")
		}
		var got organization.OrgAnthropicCredential
		if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SMAPISecretRefName == nil || *got.SMAPISecretRefName != "acme-anthropic-secrets" {
			t.Fatalf("row must be untouched on delete error: %+v", got)
		}
	})
}

// --- DeletePublisher (DB) -----------------------------------------------------

// seedIDPProfileRow inserts a minimal valid organization_idp_profiles row,
// optionally with the secret-ref triplet populated on BOTH column sets.
func seedIDPProfileRow(t testing.TB, db *gorm.DB, orgID string, refName, kvPath *string) {
	t.Helper()
	row := organization.OrganizationIDPProfile{
		OrgID:              orgID,
		Kind:               "custom",
		Issuer:             "https://idp.test",
		JWKSURL:            "https://idp.test/jwks",
		PublisherClientID:  "aep-publisher-" + orgID,
		SecretRefName:      refName,
		SecretRefKVPath:    kvPath,
		SMAPISecretRefName: refName,
		SMAPIKVPath:        kvPath,
	}
	if refName != nil {
		row.SecretRefProperty = strPtr("publisher")
		row.SMAPIProperty = strPtr("publisher")
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed idp profile row %s: %v", orgID, err)
	}
}

func TestSecretRefWriter_DeletePublisher_DB(t *testing.T) {
	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		if err := w.DeletePublisher(context.Background(), "acme"); err != nil {
			t.Fatalf("disabled DeletePublisher = %v; want nil", err)
		}
	})

	t.Run("no row for org is a no-op (idempotent), SM-API never called", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeletePublisher(context.Background(), "ghost-org"); err != nil {
			t.Fatalf("DeletePublisher on a missing row = %v; want nil", err)
		}
		if len(fake.deleteCalls) != 0 {
			t.Fatalf("DeleteSecret must not be called when no row exists")
		}
	})

	t.Run("clears the triplet after a successful SM-API delete", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedIDPProfileRow(t, db, "acme", strPtr("acme-publisher-secrets"), strPtr("user-app-secrets/wc-xxx/acme-publisher-secrets"))

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeletePublisher(claimsCtx("ou-acme-uuid"), "acme"); err != nil {
			t.Fatalf("DeletePublisher: %v", err)
		}
		if len(fake.deleteCalls) != 1 {
			t.Fatalf("want 1 DeleteSecret call, got %d", len(fake.deleteCalls))
		}
		call := fake.deleteCalls[0]
		// Publisher location has no SecretKey (whole record addressed).
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "publisher"}
		if call.loc != wantLoc || call.secretRefName != "acme-publisher-secrets" {
			t.Fatalf("DeleteSecret called with loc=%+v ref=%q; want loc=%+v ref=%q", call.loc, call.secretRefName, wantLoc, "acme-publisher-secrets")
		}
		var got organization.OrganizationIDPProfile
		if err := db.Where("org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SecretRefName != nil || got.SecretRefKVPath != nil || got.SecretRefProperty != nil || got.SecretRefWrittenAt != nil ||
			got.SMAPISecretRefName != nil || got.SMAPIKVPath != nil || got.SMAPIProperty != nil || got.SMAPIWrittenAt != nil {
			t.Fatalf("triplet not cleared on both column sets: %+v", got)
		}
	})

	t.Run("nil SMAPISecretRefName on the row passes an empty refName to DeleteSecret", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedIDPProfileRow(t, db, "acme", nil, nil)

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeletePublisher(claimsCtx("ou-acme-uuid"), "acme"); err != nil {
			t.Fatalf("DeletePublisher: %v", err)
		}
		if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != "" {
			t.Fatalf("want DeleteSecret called with empty secretRefName, got %+v", fake.deleteCalls)
		}
	})

	t.Run("SM-API delete error propagates and the row is left untouched", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedIDPProfileRow(t, db, "acme", strPtr("acme-publisher-secrets"), strPtr("kv/path"))

		fake := &fakeSMClient{deleteErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeletePublisher(claimsCtx("ou-acme-uuid"), "acme"); err == nil {
			t.Fatalf("want the SM-API error to propagate")
		}
		var got organization.OrganizationIDPProfile
		if err := db.Where("org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SMAPISecretRefName == nil || *got.SMAPISecretRefName != "acme-publisher-secrets" {
			t.Fatalf("row must be untouched on delete error: %+v", got)
		}
	})
}

// --- DeleteGitHubPAT (DB) -----------------------------------------------------

// seedUserPATRow inserts a minimal valid org_credentials row of kind
// user-pat (the CHECK constraints require webhook_secrets to be a non-empty
// array for this kind, and installation_id/selected_repos to be NULL).
// When refName is set, both secret_ref_* and sm_api_* column sets are stamped.
func seedUserPATRow(t testing.TB, db *gorm.DB, ocOrgID string, refName, kvPath *string) {
	t.Helper()
	row := organization.OrgCredential{
		OcOrgID:            ocOrgID,
		Kind:               "user-pat",
		GitHubLogin:        "ada",
		IdentityName:       "Ada Lovelace",
		IdentityEmail:      "ada@example.com",
		IdentityLogin:      "ada",
		WebhookSecrets:     organization.WebhookSecrets{{Secret: "seed-secret"}},
		SecretRefName:      refName,
		SecretRefKVPath:    kvPath,
		SMAPISecretRefName: refName,
		SMAPIKVPath:        kvPath,
	}
	if refName != nil {
		row.SecretRefProperty = strPtr("api-key")
		row.SMAPIProperty = strPtr("api-key")
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed user-pat row %s: %v", ocOrgID, err)
	}
}

func TestSecretRefWriter_DeleteGitHubPAT_DB(t *testing.T) {
	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil)
		if err := w.DeleteGitHubPAT(context.Background(), "acme"); err != nil {
			t.Fatalf("disabled DeleteGitHubPAT = %v; want nil", err)
		}
	})

	t.Run("no row for org is a no-op (idempotent), SM-API never called", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteGitHubPAT(context.Background(), "ghost-org"); err != nil {
			t.Fatalf("DeleteGitHubPAT on a missing row = %v; want nil", err)
		}
		if len(fake.deleteCalls) != 0 {
			t.Fatalf("DeleteSecret must not be called when no row exists")
		}
	})

	t.Run("clears the triplet after a successful SM-API delete", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", strPtr("acme-github-pat-secrets"), strPtr("user-app-secrets/wc-xxx/acme-github-pat-secrets"))

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme"); err != nil {
			t.Fatalf("DeleteGitHubPAT: %v", err)
		}
		if len(fake.deleteCalls) != 1 {
			t.Fatalf("want 1 DeleteSecret call, got %d", len(fake.deleteCalls))
		}
		call := fake.deleteCalls[0]
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "github-pat", SecretKey: secretmanagersvc.SecretKeyAPIKey}
		if call.loc != wantLoc || call.secretRefName != "acme-github-pat-secrets" {
			t.Fatalf("DeleteSecret called with loc=%+v ref=%q; want loc=%+v ref=%q", call.loc, call.secretRefName, wantLoc, "acme-github-pat-secrets")
		}
		var got organization.OrgCredential
		if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SecretRefName != nil || got.SecretRefKVPath != nil || got.SecretRefProperty != nil || got.SecretRefWrittenAt != nil ||
			got.SMAPISecretRefName != nil || got.SMAPIKVPath != nil || got.SMAPIProperty != nil || got.SMAPIWrittenAt != nil {
			t.Fatalf("triplet not cleared on both column sets: %+v", got)
		}
	})

	t.Run("nil SMAPISecretRefName on the row passes an empty refName to DeleteSecret", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", nil, nil)

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme"); err != nil {
			t.Fatalf("DeleteGitHubPAT: %v", err)
		}
		if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != "" {
			t.Fatalf("want DeleteSecret called with empty secretRefName, got %+v", fake.deleteCalls)
		}
	})

	t.Run("SM-API delete error propagates and the row is left untouched", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", strPtr("acme-github-pat-secrets"), strPtr("kv/path"))

		fake := &fakeSMClient{deleteErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
		if err := w.DeleteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme"); err == nil {
			t.Fatalf("want the SM-API error to propagate")
		}
		var got organization.OrgCredential
		if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SMAPISecretRefName == nil || *got.SMAPISecretRefName != "acme-github-pat-secrets" {
			t.Fatalf("row must be untouched on delete error: %+v", got)
		}
	})
}

// --- Write* dual-write (DB) ---------------------------------------------------

func claimsCtx(ouID string) context.Context {
	return jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: ouID})
}

// TestSecretRefWriter_WriteAnthropic_DualWrite proves UpdateColumns stamps
// BOTH secret_ref_* and sm_api_* column sets (EXPAND dual-write).
func TestSecretRefWriter_WriteAnthropic_DualWrite(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	seedAnthropicRow(t, db, "acme", nil, nil, nil)

	fake := &fakeSMClient{createRef: "acme-anthropic-secrets"}
	w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
	ref, err := w.WriteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleDefault, "sk-ant-key")
	if err != nil {
		t.Fatalf("WriteAnthropic: %v", err)
	}
	if ref != "acme-anthropic-secrets" {
		t.Fatalf("ref = %q", ref)
	}

	var got organization.OrgAnthropicCredential
	if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	wantPath := "user-app-secrets/" // prefix; full path includes OrgBaseNamespace(ouId)
	if got.SecretRefName == nil || *got.SecretRefName != "acme-anthropic-secrets" ||
		got.SMAPISecretRefName == nil || *got.SMAPISecretRefName != "acme-anthropic-secrets" {
		t.Fatalf("dual-write name: secret_ref=%v sm_api=%v", got.SecretRefName, got.SMAPISecretRefName)
	}
	if got.SecretRefKVPath == nil || got.SMAPIKVPath == nil ||
		*got.SecretRefKVPath != *got.SMAPIKVPath ||
		!strings.HasPrefix(*got.SecretRefKVPath, wantPath) ||
		!strings.HasSuffix(*got.SecretRefKVPath, "/acme-anthropic-secrets") {
		t.Fatalf("dual-write kv_path: secret_ref=%v sm_api=%v", got.SecretRefKVPath, got.SMAPIKVPath)
	}
	if got.SecretRefProperty == nil || got.SMAPIProperty == nil ||
		*got.SecretRefProperty != "api-key" || *got.SMAPIProperty != "api-key" {
		t.Fatalf("dual-write property: secret_ref=%v sm_api=%v", got.SecretRefProperty, got.SMAPIProperty)
	}
}

func TestSecretRefWriter_WriteGitHubPAT_DualWrite(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	seedUserPATRow(t, db, "acme", nil, nil)

	fake := &fakeSMClient{createRef: "acme-github-pat-secrets"}
	w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil))
	ref, err := w.WriteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme", "ghp_live")
	if err != nil {
		t.Fatalf("WriteGitHubPAT: %v", err)
	}
	if ref != "acme-github-pat-secrets" {
		t.Fatalf("ref = %q", ref)
	}

	var got organization.OrgCredential
	if err := db.Where("oc_org_id = ?", "acme").First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SecretRefName == nil || *got.SecretRefName != "acme-github-pat-secrets" ||
		got.SMAPISecretRefName == nil || *got.SMAPISecretRefName != "acme-github-pat-secrets" {
		t.Fatalf("dual-write name: secret_ref=%v sm_api=%v", got.SecretRefName, got.SMAPISecretRefName)
	}
	if got.SecretRefKVPath == nil || got.SMAPIKVPath == nil || *got.SecretRefKVPath != *got.SMAPIKVPath {
		t.Fatalf("dual-write kv_path: secret_ref=%v sm_api=%v", got.SecretRefKVPath, got.SMAPIKVPath)
	}
	if got.SecretRefWrittenAt == nil || got.SMAPIWrittenAt == nil {
		t.Fatalf("dual-write written_at: secret_ref=%v sm_api=%v", got.SecretRefWrittenAt, got.SMAPIWrittenAt)
	}
}

func TestResolvedSecretRef_PreferNewFallBackOld(t *testing.T) {
	t.Parallel()
	newName, oldName := "new-ref", "old-ref"
	row := organization.OrgAnthropicCredential{
		SMAPISecretRefName: &oldName,
		SMAPIKVPath:        strPtr("old/path"),
		SMAPIProperty:      strPtr("old-prop"),
	}
	if got := row.ResolvedSecretRefName(); got == nil || *got != "old-ref" {
		t.Fatalf("fallback to sm_api: got %v", got)
	}
	row.SecretRefName = &newName
	row.SecretRefKVPath = strPtr("new/path")
	row.SecretRefProperty = strPtr("new-prop")
	if got := row.ResolvedSecretRefName(); got == nil || *got != "new-ref" {
		t.Fatalf("prefer secret_ref: got %v", got)
	}
	if got := row.ResolvedSecretRefKVPath(); got == nil || *got != "new/path" {
		t.Fatalf("prefer kv_path: got %v", got)
	}
}
