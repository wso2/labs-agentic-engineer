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

package organization

// ProvisionPublisherForBuild + the fail-closed RegenerateClientSecret
// SM-API write. package organization (not organization_test) because these
// tests reuse the unexported fakeThunder from idp_service_test.go. The
// in-memory IDPRepository below lets WritePublisher's real stamp path
// (SecretRefWriter -> IDPRepository.UpdateProfileColumns) run end to end
// without a database.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// --- in-memory IDPRepository -------------------------------------------------

// memIDPRepo is a minimal IDPRepository fake: one profile per org, held in a
// map, with UpdateProfileColumns applying exactly the columns real callers
// stamp (see secret_ref_columns.go / idp_service.go). No DB, no encryption.
type memIDPRepo struct {
	profiles map[string]*OrganizationIDPProfile
	audits   []IDPAuditEvent
}

func newMemIDPRepo() *memIDPRepo {
	return &memIDPRepo{profiles: map[string]*OrganizationIDPProfile{}}
}

var _ IDPRepository = (*memIDPRepo)(nil)

func (r *memIDPRepo) GetProfileByOrgID(_ context.Context, orgID string) (*OrganizationIDPProfile, error) {
	row, ok := r.profiles[orgID]
	if !ok {
		return nil, nil
	}
	cp := *row
	return &cp, nil
}

func (r *memIDPRepo) CreateProfile(_ context.Context, profile *OrganizationIDPProfile) error {
	cp := *profile
	r.profiles[profile.OrgID] = &cp
	return nil
}

// UpdateProfileColumns applies updates onto the stored row keyed by orgID
// (mirrors the real repository's Where("org_id = ?", orgID) — the passed-in
// profile pointer is only ever used by GORM to resolve the model's table,
// which SecretRefWriter.WritePublisher doesn't even bother populating).
func (r *memIDPRepo) UpdateProfileColumns(_ context.Context, _ *OrganizationIDPProfile, orgID string, updates map[string]interface{}) error {
	row, ok := r.profiles[orgID]
	if !ok {
		return errors.New("memIDPRepo: no profile for org " + orgID)
	}
	for k, v := range updates {
		switch k {
		case "secret_ref_name":
			row.SecretRefName = memColStrPtr(v)
		case "secret_ref_kv_path":
			row.SecretRefKVPath = memColStrPtr(v)
		case "secret_ref_property":
			row.SecretRefProperty = memColStrPtr(v)
		case "secret_ref_written_at":
			row.SecretRefWrittenAt = memColTimePtr(v)
		case "publisher_client_id":
			row.PublisherClientID = memColStr(v)
		case "publisher_client_secret":
			row.PublisherClientSecret = memColStr(v)
		case "publisher_secret_ref":
			row.PublisherSecretRef = memColStr(v)
		case "updated_at":
			if t, ok := v.(time.Time); ok {
				row.UpdatedAt = t
			}
		}
	}
	return nil
}

func (r *memIDPRepo) CreateAuditEvent(_ context.Context, event *IDPAuditEvent) error {
	r.audits = append(r.audits, *event)
	return nil
}

// memColStrPtr / memColStr normalise a map[string]interface{} column value
// that may arrive as nil, string, or *string — every shape a real UpdateColumns
// caller in this package uses.
func memColStrPtr(v interface{}) *string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		s := t
		return &s
	case *string:
		return t
	default:
		return nil
	}
}

func memColStr(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case *string:
		if t == nil {
			return ""
		}
		return *t
	default:
		return ""
	}
}

func memColTimePtr(v interface{}) *time.Time {
	switch t := v.(type) {
	case nil:
		return nil
	case time.Time:
		tt := t
		return &tt
	case *time.Time:
		return t
	default:
		return nil
	}
}

// --- stub OrganizationRepository ---------------------------------------------

// stubOrgRepo is an OrganizationRepository that never has an org row — the
// OU lookup EnsureOrgPublisher does (lookupOrgOUID) falls back to the
// default OU, which is fine for this feature (the publisher app registration
// OU is orthogonal to the SM-API write path under test).
type stubOrgRepo struct{}

var _ OrganizationRepository = stubOrgRepo{}

func (stubOrgRepo) ListByNames(context.Context, []string) ([]Organization, error) { return nil, nil }
func (stubOrgRepo) GetByName(context.Context, string) (*Organization, error)      { return nil, nil }
func (stubOrgRepo) Create(context.Context, *Organization) error                   { return nil }
func (stubOrgRepo) SetThunderOrgUUID(context.Context, string, uuid.UUID) error    { return nil }

// --- fake secretmanagersvc.SecretManagementClient ----------------------------

// provFakeSM hand-fakes secretmanagersvc.SecretManagementClient for the
// provisioner tests. WritePublisher only ever calls CreateSecret on this
// path; DeleteSecret/PatchSecret/GetSecret/GetSecretWithValue are not part of
// the provision feature — a call to one is a test bug and panics.
type provFakeSM struct {
	ref string // secretRefName returned by CreateSecret on success; defaults to "ref-name"
	err error

	createCalls []provSMCreateCall
}

type provSMCreateCall struct {
	loc  secretmanagersvc.SecretLocation
	data map[string]string
}

var _ secretmanagersvc.SecretManagementClient = (*provFakeSM)(nil)

func (f *provFakeSM) CreateSecret(_ context.Context, loc secretmanagersvc.SecretLocation, data map[string]string) (string, error) {
	f.createCalls = append(f.createCalls, provSMCreateCall{loc: loc, data: data})
	if f.err != nil {
		return "", f.err
	}
	if f.ref != "" {
		return f.ref, nil
	}
	return "ref-name", nil
}

func (f *provFakeSM) DeleteSecret(context.Context, secretmanagersvc.SecretLocation, string) error {
	panic("provFakeSM: DeleteSecret is not part of the provision feature")
}

func (f *provFakeSM) PatchSecret(context.Context, secretmanagersvc.SecretLocation, map[string]string, []string) (string, error) {
	panic("provFakeSM: PatchSecret is not part of the provision feature")
}

func (f *provFakeSM) GetSecret(context.Context, string) (*secretmanagersvc.SecretInfo, error) {
	panic("provFakeSM: GetSecret is not part of the provision feature")
}

func (f *provFakeSM) GetSecretWithValue(context.Context, string) (map[string]string, error) {
	panic("provFakeSM: GetSecretWithValue is not part of the provision feature")
}

// --- ProvisionPublisherForBuild -----------------------------------------

func TestProvisionPublisherForBuild_FreshCreateWritesSecretRef(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	sm := &provFakeSM{ref: "cred-publisher-acme"}
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "aep-publisher-acme", "secret-once", true, nil
	}}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	if err := svc.ProvisionPublisherForBuild(ctx, "acme"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	row, _ := svc.GetProfile(ctx, "acme")
	if row == nil || row.SecretRefName == nil || *row.SecretRefName != "cred-publisher-acme" {
		t.Fatalf("secret_ref_name not stamped: %+v", row)
	}
	if len(thunder.regenCalls) != 0 {
		t.Fatalf("fresh create must not rotate, regenCalls=%v", thunder.regenCalls)
	}
	if len(sm.createCalls) != 1 {
		t.Fatalf("WritePublisher once, got %d", len(sm.createCalls))
	}
}

func TestProvisionPublisherForBuild_ExistingRefDoesNotRotate(t *testing.T) {
	t.Parallel()
	name := "already-there"
	repo := newMemIDPRepo()
	_ = repo.CreateProfile(context.Background(), &OrganizationIDPProfile{
		OrgID: "acme", PublisherClientID: "aep-publisher-acme", SecretRefName: &name,
	})
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "aep-publisher-acme", "", false, nil
	}}
	sm := &provFakeSM{ref: "should-not-write"}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	if err := svc.ProvisionPublisherForBuild(ctx, "acme"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if len(thunder.regenCalls) != 0 {
		t.Fatalf("must not rotate when secret_ref_name is set")
	}
	if len(sm.createCalls) != 0 {
		t.Fatalf("must not WritePublisher when triplet exists")
	}
}

func TestProvisionPublisherForBuild_CreatedFalseEmptyRefRotatesOnce(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	thunder := &fakeThunder{
		ensureFn: func(context.Context, string, string) (string, string, bool, error) {
			return "aep-publisher-acme", "", false, nil
		},
		regenFn: func(context.Context, string) (string, error) { return "rotated-secret", nil },
	}
	sm := &provFakeSM{ref: "cred-after-rotate"}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	if err := svc.ProvisionPublisherForBuild(ctx, "acme"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if len(thunder.regenCalls) != 1 {
		t.Fatalf("rotate once, got %d", len(thunder.regenCalls))
	}
	row, _ := svc.GetProfile(ctx, "acme")
	if row.SecretRefName == nil || *row.SecretRefName != "cred-after-rotate" {
		t.Fatalf("triplet after rotate: %+v", row)
	}
}

func TestProvisionPublisherForBuild_WritePublisherErrorFails(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "aep-publisher-acme", "secret-once", true, nil
	}}
	sm := &provFakeSM{err: errors.New("sm-api: no JWT in context")}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	err := svc.ProvisionPublisherForBuild(ctx, "acme")
	if err == nil {
		t.Fatal("SM-API write must fail the provisioner")
	}
	if !strings.Contains(err.Error(), "sm-api") && !strings.Contains(err.Error(), "JWT") && !strings.Contains(err.Error(), "publisher") {
		t.Fatalf("error must name SM-API/JWT/publisher, got %v", err)
	}
}

func TestProvisionPublisherForBuild_DisabledWriterFailsClosed(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "aep-publisher-acme", "secret-once", true, nil
	}}
	// No WithSecretRefWriter call: secretRefWriter stays nil, matching a
	// process with no SecretsProvider wired.
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{})
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	err := svc.ProvisionPublisherForBuild(ctx, "acme")
	if err == nil {
		t.Fatal("expected error when SecretRefWriter is disabled")
	}
	if !strings.Contains(err.Error(), "SecretsProvider") && !strings.Contains(err.Error(), "secrets delivery") {
		t.Fatalf("error must mention SecretsProvider/secrets delivery, got %v", err)
	}
	if len(thunder.ensureCalls) != 0 {
		t.Fatalf("disabled writer must not touch Thunder (Ensure), got %d calls", len(thunder.ensureCalls))
	}
	if len(thunder.regenCalls) != 0 {
		t.Fatalf("disabled writer must not rotate, regenCalls=%v", thunder.regenCalls)
	}
}

func TestProvisionPublisherForBuild_DisabledWriterViaNewSecretRefWriter(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "aep-publisher-acme", "secret-once", true, nil
	}}
	// NewSecretRefWriter(nil, nil, nil, repo) is Enabled()==false (no
	// SecretManagementClient) — same fail-closed contract as a nil writer.
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(nil, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	err := svc.ProvisionPublisherForBuild(ctx, "acme")
	if err == nil {
		t.Fatal("expected error when SecretRefWriter is disabled")
	}
	if !strings.Contains(err.Error(), "SecretsProvider") && !strings.Contains(err.Error(), "secrets delivery") {
		t.Fatalf("error must mention SecretsProvider/secrets delivery, got %v", err)
	}
	if len(thunder.regenCalls) != 0 {
		t.Fatalf("disabled writer must not rotate, regenCalls=%v", thunder.regenCalls)
	}
}

func TestProvisionPublisherForBuild_EnsureErrorPropagates(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	ensureErr := errors.New("thunder: connection refused")
	thunder := &fakeThunder{ensureFn: func(context.Context, string, string) (string, string, bool, error) {
		return "", "", false, ensureErr
	}}
	sm := &provFakeSM{}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	err := svc.ProvisionPublisherForBuild(ctx, "acme")
	if err == nil {
		t.Fatal("expected EnsureOrgPublisher error to propagate")
	}
	if !strings.Contains(err.Error(), "thunder: connection refused") {
		t.Fatalf("error must propagate the Thunder failure untouched, got %v", err)
	}
	if len(sm.createCalls) != 0 {
		t.Fatalf("must not write to SM-API when Ensure fails, got %d calls", len(sm.createCalls))
	}
}

func TestProvisionPublisherForBuild_EmptyOrgID(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	thunder := &fakeThunder{}
	sm := &provFakeSM{}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	err := svc.ProvisionPublisherForBuild(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty orgID")
	}
	if !strings.Contains(err.Error(), "orgID") {
		t.Fatalf("error must name orgID, got %v", err)
	}
	if len(thunder.ensureCalls) != 0 {
		t.Fatalf("empty orgID must short-circuit before touching Thunder, got %d calls", len(thunder.ensureCalls))
	}
}

// --- RegenerateClientSecret fail-closed SM-API write -------------------------

func TestRegenerateClientSecret_WritePublisherErrorReturned(t *testing.T) {
	t.Parallel()
	repo := newMemIDPRepo()
	stale := "acme-publisher-secrets"
	_ = repo.CreateProfile(context.Background(), &OrganizationIDPProfile{
		OrgID: "acme", PublisherClientID: "aep-publisher-acme", SecretRefName: &stale,
	})
	thunder := &fakeThunder{regenFn: func(context.Context, string) (string, error) { return "rotated", nil }}
	sm := &provFakeSM{err: errors.New("sm-api down")}
	svc := NewIDPService(repo, stubOrgRepo{}, thunder, PlatformIDPConfig{}).
		WithSecretRefWriter(NewSecretRefWriter(sm, nil, nil, repo))
	ctx := jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OuId: "ou-acme-uuid"})
	_, err := svc.RegenerateClientSecret(ctx, "acme", "ada@x.io")
	if err == nil {
		t.Fatal("RegenerateClientSecret must return WritePublisher errors")
	}
	row, _ := repo.GetProfileByOrgID(context.Background(), "acme")
	if HasPublisherSecretRef(row) {
		t.Fatalf("failed rewrite must clear secret_ref_name, got %+v", row.SecretRefName)
	}
}
