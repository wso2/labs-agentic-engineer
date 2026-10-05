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
	"time"

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

	readPaths []string          // GetSecretWithValue calls, by vault key
	readData  map[string]string // what GetSecretWithValue hands back
	readErr   error
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

// CreateSecretRef is the org secret writer's write (the GitHub PAT and the
// publisher): recorded with CreateSecret's calls, same result knobs.
func (f *fakeSMClient) CreateSecretRef(ctx context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	return f.CreateSecret(ctx, loc, data)
}

func (f *fakeSMClient) DeleteSecretRef(_ context.Context, loc secretmanagersvc.SecretLocation, name string) error {
	f.deleteCalls = append(f.deleteCalls, smDeleteCall{loc: loc, secretRefName: name})
	return f.deleteErr
}

// withOrgSecrets attaches the org secret writer production wires over the
// same client, with in-memory rows.
func withOrgSecrets(w *organization.SecretRefWriter, fake *fakeSMClient) *organization.SecretRefWriter {
	return w.WithOrgSecretWriter(organization.NewOrgSecretWriter(fake, newFakeRepo(), newFakeLock(), fixedClock))
}

func (f *fakeSMClient) PatchSecret(context.Context, secretmanagersvc.SecretLocation, map[string]string, []string) (string, error) {
	panic("fakeSMClient: PatchSecret is not on SecretRefWriter's path")
}

func (f *fakeSMClient) GetSecret(context.Context, string) (*secretmanagersvc.SecretInfo, error) {
	panic("fakeSMClient: GetSecret is not on SecretRefWriter's path")
}

func (f *fakeSMClient) GetSecretWithValue(_ context.Context, kvPath string) (map[string]string, error) {
	f.readPaths = append(f.readPaths, kvPath)
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.readData, nil
}

// --- Enabled / nil-safety -----------------------------------------------------

func TestSecretRefWriter_Enabled(t *testing.T) {
	t.Parallel()

	if w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil); w.Enabled() {
		t.Fatalf("nil client must report Enabled() == false")
	}
	if w := organization.NewSecretRefWriter(&fakeSMClient{}, nil, nil, nil, nil); !w.Enabled() {
		t.Fatalf("non-nil client must report Enabled() == true")
	}
	// Quirk: Enabled() is nil-receiver-safe (w != nil check first), so callers
	// can invoke it on a possibly-nil *organization.SecretRefWriter without a guard.
	var nilWriter *organization.SecretRefWriter
	if nilWriter.Enabled() {
		t.Fatalf("nil *organization.SecretRefWriter must report Enabled() == false, not panic")
	}
}

// --- WriteModelKey -------------------------------------------------------------

func TestSecretRefWriter_WriteModelKey(t *testing.T) {
	t.Parallel()
	noRepoint := func(organization.SecretRefTriplet) error { return nil }

	t.Run("disabled (nil client) refuses", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		if _, err := w.WriteModelKey(claimsCtx("ou-acme-uuid"), "acme", "sk-ant-key", "", noRepoint); err == nil {
			t.Fatal("disabled WriteModelKey must refuse: no copy to write")
		}
	})

	for name, args := range map[string][2]string{
		"empty ocOrgID": {"  ", "sk-ant-key"},
		"empty apiKey":  {"acme", "   "},
	} {
		t.Run(name+" is a validation error, SM-API never called", func(t *testing.T) {
			t.Parallel()
			fake := &fakeSMClient{}
			w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
			if _, err := w.WriteModelKey(claimsCtx("ou-acme-uuid"), args[0], args[1], "", noRepoint); err == nil {
				t.Fatal("want a validation error")
			}
			if len(fake.createCalls) != 0 {
				t.Fatal("no reference may be written on a validation failure")
			}
		})
	}

	t.Run("writes a default-key reference and hands its triplet to the repoint", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createRef: "acme-default-key-0000aaaa"}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
		var got organization.SecretRefTriplet
		written, err := w.WriteModelKey(claimsCtx("ou-acme-uuid"), "acme", "sk-ant-key", "model-connection-secrets",
			func(ref organization.SecretRefTriplet) error { got = ref; return nil })
		if err != nil {
			t.Fatalf("WriteModelKey: %v", err)
		}
		if written.Name != "acme-default-key-0000aaaa" || got.Name != written.Name || got.Property != "api-key" ||
			!strings.HasPrefix(got.KVPath, "user-app-secrets/") || !strings.HasSuffix(got.KVPath, "/"+written.Name) {
			t.Fatalf("written %q, repoint got %+v", written.Name, got)
		}
		call := fake.createCalls[0]
		if call.loc.EntityName != "default-key" || call.loc.OrgName != "ou-acme-uuid" || call.loc.ControlPlaneNamespace != "acme" ||
			len(call.data) != 1 || call.data["api-key"] != "sk-ant-key" {
			t.Fatalf("CreateSecretRef at %+v with %d keys", call.loc, len(call.data))
		}
		if len(fake.deleteCalls) != 0 {
			t.Fatal("the previous reference stays until the caller retires it")
		}
		written.Retire(context.Background())
		if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != "model-connection-secrets" {
			t.Fatalf("Retire deleted %+v, want the legacy copy by name", fake.deleteCalls)
		}
	})

	t.Run("no ouId claim fails before any write", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
		_, err := w.WriteModelKey(context.Background(), "acme", "sk-ant-key", "", noRepoint)
		if err == nil || !strings.Contains(err.Error(), "default-key upload") || !strings.Contains(err.Error(), "no ouId claim") {
			t.Fatalf("want a wrapped no-ouId error, got %v", err)
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("no reference may be written without ouId, got %d", len(fake.createCalls))
		}
	})

	t.Run("a vault error is wrapped and returned", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createErr: errors.New("sm-api: 503")}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
		written, err := w.WriteModelKey(claimsCtx("ou-acme-uuid"), "acme", "sk-ant-key", "", noRepoint)
		if err == nil || written.Name != "" || !strings.Contains(err.Error(), "default-key upload") {
			t.Fatalf("WriteModelKey = (%q, %v); want a wrapped error", written.Name, err)
		}
	})

	t.Run("the subscription token is a coding-agent-key reference", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createRef: "acme-coding-agent-key-0000aaaa"}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
		written, err := w.WriteAnthropic(claimsCtx("ou-acme-uuid"), "acme", organization.AnthropicRoleCoding, "sk-ant-oat-token", "", noRepoint)
		if err != nil || written.Name != "acme-coding-agent-key-0000aaaa" || fake.createCalls[0].loc.EntityName != "coding-agent-key" {
			t.Fatalf("WriteAnthropic = (%q, %v) at %+v", written.Name, err, fake.createCalls)
		}
	})
}

// --- WriteExternalResourceSecret -------------------------------------------------

func TestSecretRefWriter_WriteExternalResourceSecret(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(context.Background(), "acme", "proj", "extres-openweather-default", map[string]string{"K": "v"})
		if err != nil || vaultKey != "" || ref != "" {
			t.Fatalf("disabled WriteExternalResourceSecret = (%q, %q, %v); want (\"\", \"\", nil)", vaultKey, ref, err)
		}
	})

	t.Run("empty ocOrgID/projectName/entityName is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
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
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
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
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(claimsCtx("ou-acme-uuid"), "acme", "weatherproj", "extres-openweather-default",
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
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", ProjectName: "weatherproj", EntityName: "extres-openweather-default"}
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
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		vaultKey, ref, err := w.WriteExternalResourceSecret(claimsCtx("ou-acme-uuid"), "acme", "proj", "extres-x-dev", map[string]string{"K": "v"})
		if err == nil || vaultKey != "" || ref != "" {
			t.Fatalf("WriteExternalResourceSecret = (%q, %q, %v); want (\"\", \"\", wrapped error)", vaultKey, ref, err)
		}
		if !strings.Contains(err.Error(), "external-resource secret upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

func TestSecretRefWriter_WriteOrgCatalogSecret(t *testing.T) {
	t.Parallel()
	fake := &fakeSMClient{}
	w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
	if _, err := w.WriteOrgCatalogSecret(claimsCtx("ou-acme-uuid"), "acme", "stripe-default",
		map[string]string{"api_key": "sk_live"}); err != nil {
		t.Fatalf("WriteOrgCatalogSecret: %v", err)
	}
	if len(fake.createCalls) != 1 {
		t.Fatalf("want exactly 1 CreateSecret call, got %d", len(fake.createCalls))
	}
	call := fake.createCalls[0]
	wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", ProjectName: "org-catalog", EntityName: "stripe-default"}
	if call.loc != wantLoc {
		t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
	}
}

// Promote carries a project's own secret into the org catalog vault to
// vault: read by the binding's vault key, written as an org-catalog entity,
// the bytes never leaving the writer.
func TestSecretRefWriter_CopyOrgCatalogSecret(t *testing.T) {
	t.Parallel()
	fake := &fakeSMClient{readData: map[string]string{"OPENEXCHANGERATES_APP_ID": "dev-app-id"}}
	w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
	key, err := w.CopyOrgCatalogSecret(claimsCtx("ou-acme-uuid"), "acme", "secret/data/org-x/extres-fx-rates-development", "fx-rates-development")
	if err != nil {
		t.Fatalf("CopyOrgCatalogSecret: %v", err)
	}
	if len(fake.readPaths) != 1 || fake.readPaths[0] != "secret/data/org-x/extres-fx-rates-development" {
		t.Fatalf("read paths = %v", fake.readPaths)
	}
	if len(fake.createCalls) != 1 || fake.createCalls[0].loc.ProjectName != "org-catalog" || fake.createCalls[0].loc.EntityName != "fx-rates-development" || fake.createCalls[0].data["OPENEXCHANGERATES_APP_ID"] != "dev-app-id" {
		t.Fatalf("create calls = %+v, want the same bytes under the org-catalog entity", fake.createCalls)
	}
	if key == "" {
		t.Fatal("want the org-catalog vault key back")
	}

	empty := &fakeSMClient{readData: map[string]string{}}
	if _, err := organization.NewSecretRefWriter(empty, nil, nil, nil, nil).CopyOrgCatalogSecret(claimsCtx("ou-acme-uuid"), "acme", "secret/data/x", "e"); err == nil || len(empty.createCalls) != 0 {
		t.Fatalf("an empty source must be refused before anything is written: err=%v creates=%d", err, len(empty.createCalls))
	}
}

func TestSecretRefWriter_OrgCatalogVaultKey(t *testing.T) {
	t.Parallel()
	w := organization.NewSecretRefWriter(&fakeSMClient{}, nil, nil, nil, nil)
	got, err := w.OrgCatalogVaultKey(claimsCtx("ou-acme-uuid"), "acme", "github-default")
	if err != nil {
		t.Fatalf("OrgCatalogVaultKey: %v", err)
	}
	if !strings.HasPrefix(got, "user-app-secrets/") || !strings.HasSuffix(got, "/github-default-secrets") {
		t.Fatalf("OrgCatalogVaultKey = %q, want user-app-secrets/<ns>/github-default-secrets", got)
	}
}

// --- WriteGitHubPAT --------------------------------------------------------------

func TestSecretRefWriter_WriteGitHubPAT(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		ref, err := w.WriteGitHubPAT(context.Background(), "acme", "ghp_token")
		if err != nil || ref != "" {
			t.Fatalf("disabled WriteGitHubPAT = (%q, %v); want (\"\", nil)", ref, err)
		}
	})

	t.Run("empty ocOrgID is a validation error, SM-API never called", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
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
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		if _, err := w.WriteGitHubPAT(context.Background(), "acme", ""); err == nil {
			t.Fatalf("want an error for empty pat")
		}
		if len(fake.createCalls) != 0 {
			t.Fatalf("CreateSecret must not be called on validation failure")
		}
	})

	t.Run("uploads a new github-pat reference with the token and password keys", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", nil, nil)
		fake := &fakeSMClient{}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db)), fake)
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
		wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: "github-pat"}
		if call.loc != wantLoc {
			t.Fatalf("SecretLocation = %+v; want %+v", call.loc, wantLoc)
		}
		if len(call.data) != 2 || call.data["token"] != "ghp_token" || call.data["password"] != "ghp_token" {
			t.Fatalf("payload keys = %d; want token and password, one value", len(call.data))
		}
	})

	t.Run("without the org secret writer it refuses", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		if _, err := w.WriteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme", "ghp_token"); err == nil || len(fake.createCalls) != 0 {
			t.Fatalf("want a wiring error and no write, got %v", err)
		}
	})

	t.Run("CreateSecretRef error is wrapped and returned", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedUserPATRow(t, db, "acme", nil, nil)
		fake := &fakeSMClient{createErr: errors.New("sm-api: 500")}
		w := withOrgSecrets(organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), nil, nil, nil), fake)
		ref, err := w.WriteGitHubPAT(claimsCtx("ou-acme-uuid"), "acme", "ghp_token")
		if err == nil || ref != "" {
			t.Fatalf("WriteGitHubPAT = (%q, %v); want (\"\", wrapped error)", ref, err)
		}
		if !strings.Contains(err.Error(), "github-pat upload") {
			t.Fatalf("error not wrapped as expected: %v", err)
		}
	})
}

// --- resolveVaultKey (via the exported Write* surface) ----------------------

// TestSecretRefWriter_ResolveVaultKey_NoClaimsInContext pins resolveVaultKey's
// exact error text. resolveVaultKey itself is unexported and unreachable from
// this black-box test, so this drives it indirectly through WriteModelKey.
// Go's %w wrapping preserves the wrapped error's Error() text verbatim as a
// suffix, so the precise underlying message is still pinned exactly, just
// reached through the public API instead of the private method.
// resolveVaultKey never touches the DB (it derives the path from the JWT,
// deliberately not from the local `organizations.uuid` row — see its doc
// comment), so db: nil is safe here too.
func TestSecretRefWriter_ResolveVaultKey_NoClaimsInContext(t *testing.T) {
	t.Parallel()
	fake := &fakeSMClient{}
	w := withOrgSecrets(organization.NewSecretRefWriter(fake, nil, nil, nil, nil), fake)
	_, err := w.WriteModelKey(context.Background(), "acme", "sk-ant-key", "", nil)
	if err == nil {
		t.Fatalf("want an error when ctx carries no JWT claims")
	}
	if !strings.HasSuffix(err.Error(), "no ouId claim in JWT context") {
		t.Fatalf("resolveVaultKey error = %q; want it to end with the exact no-ouId-claim message", err.Error())
	}
}

// --- DeleteModelKey, ForgetModelKey --------------------------------------------

// seedModelConnectionRow inserts a minimal valid org_model_connections row
// with no secret-ref triplet.
func seedModelConnectionRow(t testing.TB, db *gorm.DB, ocOrgID string) {
	t.Helper()
	now := time.Now().UTC()
	row := organization.OrgModelConnection{
		OcOrgID: ocOrgID, Format: "anthropic", BaseURL: "https://api.anthropic.com/v1", Host: "api.anthropic.com",
		Model: "claude-sonnet-5", AuthScheme: "x-api-key", ImageInput: "yes", KeyPreview: "sk-a…wxyz",
		ConnectedAt: now, UpdatedAt: now,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed model connection row %s: %v", ocOrgID, err)
	}
}

func strPtr(s string) *string { return &s }

// DeleteModelKey deletes a pre-phase-1 copy by its deterministic name (the
// rename's retire), reading nothing.
func TestSecretRefWriter_DeleteModelKey(t *testing.T) {
	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		if err := w.DeleteModelKey(context.Background(), "acme", "model-connection-secrets"); err != nil {
			t.Fatalf("disabled DeleteModelKey = %v; want nil", err)
		}
	})

	t.Run("SM-API delete error propagates", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{deleteErr: errors.New("sm-api: 500")}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		if err := w.DeleteModelKey(claimsCtx("ou-acme-uuid"), "acme", "model-connection-secrets"); err == nil {
			t.Fatalf("want the SM-API error to propagate")
		}
	})

	// The entity follows the reference name: a row still on the Anthropic-era
	// copy deletes that copy's vault path, never the new name's.
	for _, tc := range []struct{ ref, entity string }{
		{"model-connection-secrets", "model-connection"},
		{"anthropic-secrets", "anthropic"},
	} {
		t.Run("model key "+tc.ref+" deletes entity "+tc.entity, func(t *testing.T) {
			t.Parallel()
			fake := &fakeSMClient{}
			w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
			if err := w.DeleteModelKey(claimsCtx("ou-acme-uuid"), "acme", tc.ref); err != nil {
				t.Fatalf("DeleteModelKey: %v", err)
			}
			wantLoc := secretmanagersvc.SecretLocation{OrgName: "ou-acme-uuid", ControlPlaneNamespace: "acme", EntityName: tc.entity, SecretKey: secretmanagersvc.SecretKeyAPIKey}
			if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].loc != wantLoc || fake.deleteCalls[0].secretRefName != tc.ref {
				t.Fatalf("DeleteSecret calls = %+v; want one at %+v named %q", fake.deleteCalls, wantLoc, tc.ref)
			}
		})
	}
}

func seedIDPProfileRow(t testing.TB, db *gorm.DB, orgID string, refName, kvPath *string) {
	t.Helper()
	row := organization.OrganizationIDPProfile{
		OrgID:             orgID,
		Kind:              "custom",
		Issuer:            "https://idp.test",
		JWKSURL:           "https://idp.test/jwks",
		PublisherClientID: "aep-publisher-" + orgID,
		SecretRefName:     refName,
		SecretRefKVPath:   kvPath,
	}
	if refName != nil {
		row.SecretRefProperty = strPtr("publisher")
		written := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		row.SecretRefWrittenAt = &written
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed idp profile row %s: %v", orgID, err)
	}
}

func TestSecretRefWriter_DeletePublisher_DB(t *testing.T) {
	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		if err := w.DeletePublisher(context.Background(), "acme"); err != nil {
			t.Fatalf("disabled DeletePublisher = %v; want nil", err)
		}
	})

	t.Run("no row for org is a no-op (idempotent), SM-API never called", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db))
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
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db))
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
		if got.SecretRefName != nil || got.SecretRefKVPath != nil || got.SecretRefProperty != nil || got.SecretRefWrittenAt != nil {
			t.Fatalf("triplet not cleared: %+v", got)
		}
	})

	t.Run("nil SecretRefName on the row passes an empty refName to DeleteSecret", func(t *testing.T) {
		t.Parallel()
		db := dbtest.New(t)
		seedIDPProfileRow(t, db, "acme", nil, nil)

		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db))
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
		w := organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db))
		if err := w.DeletePublisher(claimsCtx("ou-acme-uuid"), "acme"); err == nil {
			t.Fatalf("want the SM-API error to propagate")
		}
		var got organization.OrganizationIDPProfile
		if err := db.Where("org_id = ?", "acme").First(&got).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.SecretRefName == nil || *got.SecretRefName != "acme-publisher-secrets" {
			t.Fatalf("row must be untouched on delete error: %+v", got)
		}
		if got.SecretRefWrittenAt == nil {
			t.Fatalf("written_at must be untouched on delete error")
		}
	})
}

// seedUserPATRow inserts a minimal valid org_credentials row of kind
// user-pat (the CHECK constraints require webhook_secrets to be a non-empty
// array for this kind, and installation_id/selected_repos to be NULL).
// When refName is set, secret_ref_* columns are stamped.
func seedUserPATRow(t testing.TB, db *gorm.DB, ocOrgID string, refName, kvPath *string) {
	t.Helper()
	row := organization.OrgCredential{
		OcOrgID:         ocOrgID,
		Kind:            "user-pat",
		GitHubLogin:     "ada",
		IdentityName:    "Ada Lovelace",
		IdentityEmail:   "ada@example.com",
		IdentityLogin:   "ada",
		WebhookSecrets:  organization.WebhookSecrets{{Secret: "seed-secret"}},
		SecretRefName:   refName,
		SecretRefKVPath: kvPath,
	}
	if refName != nil {
		row.SecretRefProperty = strPtr("api-key")
		written := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		row.SecretRefWrittenAt = &written
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("seed user-pat row %s: %v", ocOrgID, err)
	}
}

func claimsCtx(ouID string) context.Context {
	return jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: ouID})
}

// The github-pat row (here the fake repository) is the only record of the
// new reference: the org_credentials triplet is no longer stamped (it keeps naming the
// pre-phase-1 reference, which is retired by that stored name).
func TestSecretRefWriter_WriteGitHubPAT_RecordsOnlyTheRow(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	// A pre-phase-1 triplet: its deterministic reference is the one retired.
	seedUserPATRow(t, db, "acme", strPtr("github-pat-secrets"), strPtr("user-app-secrets/ns/github-pat-secrets"))

	fake := &fakeSMClient{createRef: "acme-github-pat-secrets"}
	w := withOrgSecrets(organization.NewSecretRefWriter(fake, organization.NewOrgCredentialRepository(db, nil), organization.NewOrgAnthropicRepository(db), organization.NewIDPRepository(db, nil), organization.NewOrgModelConnectionRepository(db)), fake)
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
	if got.SecretRefName == nil || *got.SecretRefName != "github-pat-secrets" ||
		got.SecretRefKVPath == nil || *got.SecretRefKVPath != "user-app-secrets/ns/github-pat-secrets" ||
		got.SecretRefProperty == nil || *got.SecretRefProperty != "api-key" {
		t.Fatalf("the triplet must not be stamped: %v %v %v", got.SecretRefName, got.SecretRefKVPath, got.SecretRefProperty)
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != "github-pat-secrets" {
		t.Fatalf("the legacy reference is deleted by its stored name: %+v", fake.deleteCalls)
	}
}

// --- WriteAMPModelKey ---------------------------------------------------------

func TestSecretRefWriter_WriteAMPModelKey(t *testing.T) {
	t.Parallel()

	t.Run("disabled (nil client) is a no-op", func(t *testing.T) {
		t.Parallel()
		w := organization.NewSecretRefWriter(nil, nil, nil, nil, nil)
		name, prop, err := w.WriteAMPModelKey(claimsCtx("ou-1"), "acme", "checkout-agent", "default", "amp-key", "http://gw/aep-default-anthropic")
		if err != nil || name != "" || prop != "" {
			t.Fatalf("disabled writer must no-op: name=%q prop=%q err=%v", name, prop, err)
		}
	})

	t.Run("stores per agent and per environment in the org's CP namespace", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{createRef: "amp-model-checkout-agent-default"}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)

		name, prop, err := w.WriteAMPModelKey(claimsCtx("ou-1"), "acme", "checkout-agent", "default", "amp-key-value", "http://gw/aep-default-anthropic")
		if err != nil {
			t.Fatalf("WriteAMPModelKey: %v", err)
		}
		if name != "amp-model-checkout-agent-default" || prop != secretmanagersvc.SecretKeyAPIKey {
			t.Fatalf("name=%q prop=%q", name, prop)
		}
		if len(fake.createCalls) != 1 {
			t.Fatalf("CreateSecret calls = %d, want 1", len(fake.createCalls))
		}
		call := fake.createCalls[0]

		// The CONTROL-PLANE namespace is the org id, not a vault path segment.
		// A SecretReference must live where the ReleaseBinding that
		// secretKeyRefs it lives; write it elsewhere and the create SUCCEEDS
		// while OpenChoreo fails the render with "SecretReference not found".
		if call.loc.ControlPlaneNamespace != "acme" {
			t.Errorf("ControlPlaneNamespace = %q, want the org's CP namespace", call.loc.ControlPlaneNamespace)
		}
		if call.loc.OrgName != "ou-1" {
			t.Errorf("OrgName = %q, want the ouId claim (the vault path segment)", call.loc.OrgName)
		}
		// Per agent AND per environment: AMP issues one key per model config per
		// environment, and two agents sharing an entity would share a credential,
		// defeating the per-agent revocation this whole path exists for.
		if !strings.Contains(call.loc.EntityName, "checkout-agent") ||
			!strings.Contains(call.loc.EntityName, "default") {
			t.Errorf("EntityName = %q, want it to name both agent and environment", call.loc.EntityName)
		}
		if call.data[secretmanagersvc.SecretKeyAPIKey] != "amp-key-value" {
			t.Errorf("payload = %v, want the issued key under the api-key key", call.data)
		}
		// The endpoint rides in the same secret: the key authenticates against
		// that address alone, and one secret means the deployment composes both
		// MODEL_API_KEY and MODEL_ENDPOINT from a single SecretReference.
		if call.data[organization.AMPModelURLKey] != "http://gw/aep-default-anthropic" {
			t.Errorf("payload = %v, want the endpoint stored beside the key", call.data)
		}
	})

	t.Run("validation errors never reach SM-API", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name                             string
			org, component, environment, key string
		}{
			{"no org", "  ", "agent", "default", "k"},
			{"no component", "acme", " ", "default", "k"},
			{"no environment", "acme", "agent", "", "k"},
			{"no key", "acme", "agent", "default", "  "},
		} {
			fake := &fakeSMClient{}
			w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
			if _, _, err := w.WriteAMPModelKey(claimsCtx("ou-1"), tc.org, tc.component, tc.environment, tc.key, "http://gw/ctx"); err == nil {
				t.Errorf("%s: want a validation error", tc.name)
			}
			if len(fake.createCalls) != 0 {
				t.Errorf("%s: CreateSecret must not be called", tc.name)
			}
		}
	})

	t.Run("without an ouId claim it refuses rather than writing to the wrong path", func(t *testing.T) {
		t.Parallel()
		fake := &fakeSMClient{}
		w := organization.NewSecretRefWriter(fake, nil, nil, nil, nil)
		if _, _, err := w.WriteAMPModelKey(context.Background(), "acme", "agent", "default", "k", "http://gw/ctx"); err == nil {
			t.Fatal("want an error: SM-API derives the namespace from the JWT, so a missing claim cannot be guessed")
		}
		if len(fake.createCalls) != 0 {
			t.Error("CreateSecret must not be called without an ouId claim")
		}
	})
}

func TestSecretRefWriter_DeletePublisher_RemovesTheRecordedReference_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	const minted = "acme-ae-publisher-client-0000000a"
	seedIDPProfileRow(t, db, "acme", strPtr(minted), strPtr("user-app-secrets/ns/"+minted))
	fake := &fakeSMClient{}
	rows := newFakeRepo()
	rows.set("acme", organization.OrgSecretPublisherClient, minted)
	w := organization.NewSecretRefWriter(fake, nil, nil, organization.NewIDPRepository(db, nil), nil).
		WithOrgSecretWriter(organization.NewOrgSecretWriter(fake, rows, newFakeLock(), fixedClock))
	if err := w.DeletePublisher(claimsCtx("ou-acme-uuid"), "acme"); err != nil {
		t.Fatalf("DeletePublisher: %v", err)
	}
	if len(fake.deleteCalls) != 1 || fake.deleteCalls[0].secretRefName != minted || fake.deleteCalls[0].loc.EntityName != "ae-publisher-client" {
		t.Fatalf("the minted reference is deleted by its stored name: %+v", fake.deleteCalls)
	}
	if rows.name("acme", organization.OrgSecretPublisherClient) != "" {
		t.Fatal("the row is unset")
	}
	var got organization.OrganizationIDPProfile
	if err := db.Where("org_id = ?", "acme").First(&got).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.SecretRefName != nil {
		t.Fatalf("triplet not cleared: %v", got.SecretRefName)
	}
}
