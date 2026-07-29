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

// DBTEST tier (skips under -short; `make test-db` runs it): the REAL idpService
// over a pristine per-test Postgres (dbtest.New) with a fake Thunder admin
// client — the SQL-shaped behavior under pin: GetOrCreateProfile's
// create-then-get idempotency + platform-field self-heal, the publisher
// lifecycle's row writes (Ensure sets publisher_* / Regenerate rotates the
// secret / Revoke clears the triplet), the append-only idp_audit_events trail
// with the right action per mutation, org scoping across many decoy orgs
// (mutation-verified — a dropped WHERE would coin-flip with 2 rows under random
// UUID PKs), and the thunder-nil "no partial damage" contract. The Organization
// OU lookup that feeds EnsurePublisherApp is exercised over real rows. The
// audit trail is READ directly off idp_audit_events (the table the service
// wrote) — the service has no audit reader, so this is not a hand-copied
// production query, it is the incident-response view of a table production owns.
//
// External test package: idp_service_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle.
// idpDBFakeThunder is a local duplicate of idp_service_test.go's fakeThunder
// (renamed to avoid colliding with config_actions_component_test.go's
// simpler, same-package fakeThunder). secretRefPath is unexported production
// logic (a one-line, no-internals path formatter, separately unit-pinned in
// idp_service_test.go); wantSecretRefPath duplicates just that formatting.

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// idpDBPlatform is the cluster-level IDP default a fresh profile is seeded with.
var idpDBPlatform = organization.PlatformIDPConfig{
	Issuer:  "http://thunder.test:8080",
	JWKSURL: "http://thunder.test:8090/oauth2/jwks",
}

// idpDBService wires the real service: per-test Postgres + the given Thunder
// admin client. Pass a literal nil (a true nil interface, not a typed-nil
// *idpDBFakeThunder) to exercise the ErrIDPThunderUnavailable path. The
// *gorm.DB is returned so tests can read the audit table the service writes.
// Returns the exported IDPService interface (NewIDPService's concrete return
// type is unexported and unreachable from this package).
func idpDBService(t *testing.T, thunder thundersvc.Client) (organization.IDPService, *gorm.DB) {
	t.Helper()
	db := dbtest.New(t)
	return organization.NewIDPService(organization.NewIDPRepository(db, nil), organization.NewOrganizationRepository(db), thunder, idpDBPlatform), db
}

// wantSecretRefPath mirrors idp_service.go's unexported secretRefPath — a
// pure one-line path formatter with no internal state, duplicated here since
// a black-box test can't call it directly. secretRefPath's own correctness is
// pinned by idp_service_test.go's TestSecretRefPath.
func wantSecretRefPath(orgID string) string {
	return "secret/aep/" + orgID + "/idp/publisher"
}

// idpDBFakeThunder is idp_service_test.go's fakeThunder, duplicated under a
// different name (this package already has a simpler fakeThunder in
// config_actions_component_test.go). idpService only ever calls three of
// thundersvc.Client's methods (EnsurePublisherApp, DeletePublisherApp,
// RegenerateClientSecret); OUExists is irrelevant to this feature, so a call
// to it is a test bug — it panics.
type idpDBFakeThunder struct {
	ensureFn func(ctx context.Context, orgHandle, orgOUID string) (string, string, bool, error)
	deleteFn func(ctx context.Context, orgHandle string) (bool, error)
	regenFn  func(ctx context.Context, orgHandle string) (string, error)

	ensureCalls []idpDBEnsureCall
	deleteCalls []string
	regenCalls  []string
}

type idpDBEnsureCall struct{ orgHandle, orgOUID string }

var _ thundersvc.Client = (*idpDBFakeThunder)(nil)

func (f *idpDBFakeThunder) EnsurePublisherApp(ctx context.Context, orgHandle, orgOUID string) (string, string, bool, error) {
	f.ensureCalls = append(f.ensureCalls, idpDBEnsureCall{orgHandle, orgOUID})
	if f.ensureFn == nil {
		return "cid-" + orgHandle, "secret-" + orgHandle, true, nil
	}
	return f.ensureFn(ctx, orgHandle, orgOUID)
}

func (f *idpDBFakeThunder) DeletePublisherApp(ctx context.Context, orgHandle string) (bool, error) {
	f.deleteCalls = append(f.deleteCalls, orgHandle)
	if f.deleteFn == nil {
		return true, nil
	}
	return f.deleteFn(ctx, orgHandle)
}

func (f *idpDBFakeThunder) RegenerateClientSecret(ctx context.Context, orgHandle string) (string, error) {
	f.regenCalls = append(f.regenCalls, orgHandle)
	if f.regenFn == nil {
		return "rotated-" + orgHandle, nil
	}
	return f.regenFn(ctx, orgHandle)
}

func (f *idpDBFakeThunder) OUExists(context.Context, string) (bool, error) {
	panic("idpDBFakeThunder: OUExists is not part of the idp feature")
}

// auditActions returns the ordered action list for an org from idp_audit_events
// (occurred_at asc, id asc as the tiebreak so same-instant rows keep insertion
// order). This is the console "Audit" tab's read.
func auditActions(t *testing.T, db *gorm.DB, org string) []string {
	t.Helper()
	var rows []organization.IDPAuditEvent
	if err := db.Where("org_id = ?", org).Order("occurred_at asc, id asc").Find(&rows).Error; err != nil {
		t.Fatalf("read audit for %s: %v", org, err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out
}

func auditRows(t *testing.T, db *gorm.DB, org string) []organization.IDPAuditEvent {
	t.Helper()
	var rows []organization.IDPAuditEvent
	if err := db.Where("org_id = ?", org).Order("id asc").Find(&rows).Error; err != nil {
		t.Fatalf("read audit rows for %s: %v", org, err)
	}
	return rows
}

// --- GetOrCreateProfile -----------------------------------------------------

func TestGetOrCreateProfile_CreateThenGetIdempotent_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, &idpDBFakeThunder{})
	ctx := context.Background()

	// No row yet: GetProfile returns (nil, nil).
	if p, err := svc.GetProfile(ctx, "acme"); err != nil || p != nil {
		t.Fatalf("absent org: want (nil,nil), got (%+v,%v)", p, err)
	}

	// First GetOrCreate seeds the default platform-kind row from the cluster
	// config; publisher_* stay empty until EnsureOrgPublisher.
	created, err := svc.GetOrCreateProfile(ctx, "acme")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Kind != "platform" || created.Issuer != idpDBPlatform.Issuer || created.JWKSURL != idpDBPlatform.JWKSURL {
		t.Fatalf("seeded profile drifted: %+v", created)
	}
	if created.PublisherClientID != "" || created.ID == "" {
		t.Fatalf("fresh profile must have an id and no publisher: %+v", created)
	}

	// Second call returns the SAME row (same id) — idempotent, no duplicate.
	again, err := svc.GetOrCreateProfile(ctx, "acme")
	if err != nil {
		t.Fatalf("second GetOrCreate: %v", err)
	}
	if again.ID != created.ID {
		t.Fatalf("GetOrCreate must be idempotent: first id %q, second %q", created.ID, again.ID)
	}
	// And GetProfile now serves that persisted row.
	got, err := svc.GetProfile(ctx, "acme")
	if err != nil || got == nil || got.ID != created.ID {
		t.Fatalf("GetProfile after create: got %+v err %v", got, err)
	}

	// Exactly one row (the one_profile_per_org unique constraint + idempotency).
	var n int64
	if err := gormDB.Model(&organization.OrganizationIDPProfile{}).Where("org_id = ?", "acme").Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("want exactly one profile row, got %d", n)
	}
}

func TestGetOrCreateProfile_SelfHealsPlatformFields_DB(t *testing.T) {
	t.Parallel()
	gormDB := dbtest.New(t)
	ctx := context.Background()

	// Seed a profile with the OLD cluster config.
	old := organization.NewIDPService(organization.NewIDPRepository(gormDB, nil), organization.NewOrganizationRepository(gormDB), &idpDBFakeThunder{}, organization.PlatformIDPConfig{
		Issuer:  "http://old-issuer:8080",
		JWKSURL: "http://old-jwks:8090/oauth2/jwks",
	})
	if _, err := old.GetOrCreateProfile(ctx, "acme"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A service running the NEW cluster config self-heals the cached fields on
	// the next GetOrCreate (issuer/jwks are cluster config, not per-org data).
	fresh := organization.NewIDPService(organization.NewIDPRepository(gormDB, nil), organization.NewOrganizationRepository(gormDB), &idpDBFakeThunder{}, organization.PlatformIDPConfig{
		Issuer:  "http://new-issuer:8080",
		JWKSURL: "http://new-jwks:8090/oauth2/jwks",
	})
	healed, err := fresh.GetOrCreateProfile(ctx, "acme")
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if healed.Issuer != "http://new-issuer:8080" || healed.JWKSURL != "http://new-jwks:8090/oauth2/jwks" {
		t.Fatalf("platform fields not self-healed in memory: %+v", healed)
	}
	// Persisted, not just returned.
	reread, err := fresh.GetProfile(ctx, "acme")
	if err != nil || reread.Issuer != "http://new-issuer:8080" || reread.JWKSURL != "http://new-jwks:8090/oauth2/jwks" {
		t.Fatalf("self-heal not persisted: got %+v err %v", reread, err)
	}
}

// --- EnsureOrgPublisher -----------------------------------------------------

func TestEnsureOrgPublisher_PersistsAndAudits_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "aep-publisher-" + org, "secret-xyz", true, nil
		},
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	clientID, secret, created, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io")
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if clientID != "aep-publisher-acme" || secret != "secret-xyz" || !created {
		t.Fatalf("ensure return drifted: id=%q secret=%q created=%v", clientID, secret, created)
	}

	// The profile row carries the publisher triplet: id, secret (created=true),
	// and the logical secret-ref path.
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil {
		t.Fatalf("get after ensure: %v", err)
	}
	if row.PublisherClientID != "aep-publisher-acme" || row.PublisherClientSecret != "secret-xyz" {
		t.Fatalf("publisher creds not persisted: %+v", row)
	}
	if row.PublisherSecretRef != wantSecretRefPath("acme") {
		t.Fatalf("secret ref path drifted: %q", row.PublisherSecretRef)
	}

	// Exactly one audit row, action=ensure_publisher, actor preserved, no error.
	rows := auditRows(t, gormDB, "acme")
	if len(rows) != 1 || rows[0].Action != organization.IDPAuditEnsurePublisher {
		t.Fatalf("audit drifted: %+v", rows)
	}
	if rows[0].Actor != "ada@x.io" || rows[0].ErrorMessage != "" {
		t.Fatalf("audit actor/error drifted: %+v", rows[0])
	}
	if len(rows[0].AfterState) == 0 {
		t.Fatalf("successful ensure must record an after-state snapshot")
	}
}

func TestEnsureOrgPublisher_ThunderErrorAuditsFailure_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(context.Context, string, string) (string, string, bool, error) {
			return "", "", false, errors.New("thunder boom")
		},
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	_, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io")
	if err == nil {
		t.Fatalf("ensure must surface the thunder error")
	}

	// The profile row WAS created (GetOrCreateProfile runs before the Thunder
	// call) but stays publisher-less — pin what the code actually guarantees.
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil || row == nil {
		t.Fatalf("profile row should still exist after a thunder failure: %+v err %v", row, err)
	}
	if row.PublisherClientID != "" || row.PublisherClientSecret != "" {
		t.Fatalf("failed ensure must not persist publisher creds: %+v", row)
	}

	// The failure is audited with the error message.
	rows := auditRows(t, gormDB, "acme")
	if len(rows) != 1 || rows[0].Action != organization.IDPAuditEnsurePublisher || rows[0].ErrorMessage == "" {
		t.Fatalf("thunder failure must be audited with an error: %+v", rows)
	}
}

func TestEnsureOrgPublisher_ResolvesOrgOU_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	// Org row carrying a Thunder OU id → the publisher app is registered under it.
	ou := uuid.New()
	if err := gormDB.Create(&organization.Organization{UUID: uuid.New(), Name: "acme", ThunderOrgUUID: &ou}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("ensure acme: %v", err)
	}

	// Org with NO row → the OU falls back to "" (default OU).
	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "noou", "ada@x.io"); err != nil {
		t.Fatalf("ensure noou: %v", err)
	}

	if len(thunder.ensureCalls) != 2 {
		t.Fatalf("want two EnsurePublisherApp calls, got %d", len(thunder.ensureCalls))
	}
	byOrg := map[string]string{}
	for _, c := range thunder.ensureCalls {
		byOrg[c.orgHandle] = c.orgOUID
	}
	if byOrg["acme"] != ou.String() {
		t.Fatalf("acme OU: got %q, want the seeded org OU %q", byOrg["acme"], ou.String())
	}
	if byOrg["noou"] != "" {
		t.Fatalf("noou OU: got %q, want empty (default OU fallback)", byOrg["noou"])
	}
}

// --- RegenerateClientSecret -------------------------------------------------

func TestRegenerateClientSecret_RotatesAndAudits_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "aep-publisher-" + org, "secret-v1", true, nil
		},
		regenFn: func(context.Context, string) (string, error) { return "secret-v2", nil },
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	newSecret, err := svc.RegenerateClientSecret(ctx, "acme", "ada@x.io")
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if newSecret != "secret-v2" {
		t.Fatalf("rotate return: got %q, want secret-v2", newSecret)
	}
	if len(thunder.regenCalls) != 1 || thunder.regenCalls[0] != "acme" {
		t.Fatalf("thunder regenerate calls: %+v", thunder.regenCalls)
	}

	// The rotated secret is persisted; client_id is unchanged.
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil || row.PublisherClientSecret != "secret-v2" || row.PublisherClientID != "aep-publisher-acme" {
		t.Fatalf("rotated secret not persisted: %+v err %v", row, err)
	}

	// Audit trail: ensure_publisher THEN regenerate_secret.
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditRegenerateSecret,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

func TestRegenerateClientSecret_NoPublisherErrsWithoutAudit_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, &idpDBFakeThunder{})
	ctx := context.Background()

	// No publisher app provisioned → a hard error, and (pinning the code) NO
	// audit row: the guard returns before audit().
	_, err := svc.RegenerateClientSecret(ctx, "acme", "ada@x.io")
	if err == nil {
		t.Fatalf("rotate without a publisher must error")
	}
	if got := auditActions(t, gormDB, "acme"); len(got) != 0 {
		t.Fatalf("no-publisher rotate must not write an audit row, got %v", got)
	}
}

// --- RevokeOrgPublisher -----------------------------------------------------

func TestRevokeOrgPublisher_ClearsAndAudits_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "aep-publisher-" + org, "secret-v1", true, nil
		},
		deleteFn: func(context.Context, string) (bool, error) { return true, nil },
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	deleted, err := svc.RevokeOrgPublisher(ctx, "acme", "ada@x.io")
	if err != nil || !deleted {
		t.Fatalf("revoke: deleted=%v err=%v", deleted, err)
	}

	// The publisher triplet is cleared; the row itself survives (kind stays).
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil || row == nil {
		t.Fatalf("profile row must survive revoke: %+v err %v", row, err)
	}
	if row.PublisherClientID != "" || row.PublisherClientSecret != "" || row.PublisherSecretRef != "" {
		t.Fatalf("revoke must clear the publisher triplet: %+v", row)
	}

	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditRevokePublisher,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

func TestRevokeOrgPublisher_NoProfileIsNoop_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, &idpDBFakeThunder{})
	ctx := context.Background()

	// Nothing to revoke (no row) → (false, nil), no Thunder call, no audit.
	deleted, err := svc.RevokeOrgPublisher(ctx, "acme", "ada@x.io")
	if err != nil || deleted {
		t.Fatalf("no-profile revoke: want (false,nil), got (%v,%v)", deleted, err)
	}
	if got := auditActions(t, gormDB, "acme"); len(got) != 0 {
		t.Fatalf("no-op revoke must not audit, got %v", got)
	}
}

// --- UpdateProfile ----------------------------------------------------------

func TestUpdateProfile_IssuerOnlyPreservesPublisher_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "aep-publisher-" + org, "secret-v1", true, nil
		},
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// Same kind, new issuer/jwks → publisher app preserved (no kind switch).
	updated, err := svc.UpdateProfile(ctx, "acme", "ada@x.io", organization.UpdateProfileRequest{
		Kind:    "platform",
		Issuer:  "http://new-issuer:8080",
		JWKSURL: "http://new-jwks:8090/jwks",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Issuer != "http://new-issuer:8080" || updated.JWKSURL != "http://new-jwks:8090/jwks" {
		t.Fatalf("issuer/jwks not persisted: %+v", updated)
	}
	if updated.PublisherClientID != "aep-publisher-acme" {
		t.Fatalf("same-kind update must preserve the publisher: %+v", updated)
	}
	// No Thunder cleanup on a same-kind update.
	if len(thunder.deleteCalls) != 0 {
		t.Fatalf("same-kind update must not call Thunder delete, got %v", thunder.deleteCalls)
	}
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

func TestUpdateProfile_KindChangeClearsPublisherAndCallsThunder_DB(t *testing.T) {
	t.Parallel()
	thunder := &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "aep-publisher-" + org, "secret-v1", true, nil
		},
		deleteFn: func(context.Context, string) (bool, error) { return true, nil },
	}
	svc, gormDB := idpDBService(t, thunder)
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// platform → custom: the previous publisher app belongs to the old IDP, so
	// it is best-effort deleted on Thunder AND the triplet is cleared.
	updated, err := svc.UpdateProfile(ctx, "acme", "ada@x.io", organization.UpdateProfileRequest{
		Kind:    "custom",
		Issuer:  "https://byo-idp.example",
		JWKSURL: "https://byo-idp.example/jwks",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Kind != "custom" {
		t.Fatalf("kind not switched: %+v", updated)
	}
	if updated.PublisherClientID != "" || updated.PublisherClientSecret != "" || updated.PublisherSecretRef != "" {
		t.Fatalf("kind switch must clear the publisher triplet: %+v", updated)
	}
	if len(thunder.deleteCalls) != 1 || thunder.deleteCalls[0] != "acme" {
		t.Fatalf("kind switch must call Thunder delete once for the org: %v", thunder.deleteCalls)
	}
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

// --- Thunder-unavailable: no partial damage ---------------------------------

func TestMutations_ThunderNilNoPartialDamage_DB(t *testing.T) {
	t.Parallel()
	// A real db but no Thunder: every mutating call must fail with the sentinel
	// BEFORE creating a profile row or writing an audit event.
	svc, gormDB := idpDBService(t, nil)
	ctx := context.Background()

	if _, _, _, err := svc.EnsureOrgPublisher(ctx, "acme", "ada@x.io"); !errors.Is(err, organization.ErrIDPThunderUnavailable) {
		t.Fatalf("ensure: want ErrIDPThunderUnavailable, got %v", err)
	}
	if _, err := svc.RevokeOrgPublisher(ctx, "acme", "ada@x.io"); !errors.Is(err, organization.ErrIDPThunderUnavailable) {
		t.Fatalf("revoke: want ErrIDPThunderUnavailable, got %v", err)
	}
	if _, err := svc.RegenerateClientSecret(ctx, "acme", "ada@x.io"); !errors.Is(err, organization.ErrIDPThunderUnavailable) {
		t.Fatalf("regenerate: want ErrIDPThunderUnavailable, got %v", err)
	}

	// No profile row, no audit row — the sentinel fires before any write.
	if p, err := svc.GetProfile(ctx, "acme"); err != nil || p != nil {
		t.Fatalf("thunder-nil mutation must not create a profile row: %+v err %v", p, err)
	}
	var n int64
	if err := gormDB.Model(&organization.IDPAuditEvent{}).Where("org_id = ?", "acme").Count(&n).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 0 {
		t.Fatalf("thunder-nil mutation must not write an audit row, got %d", n)
	}
}

// --- org scoping across many decoys (mutation-verified) ---------------------

func TestOrgScoping_ManyDecoys_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, &idpDBFakeThunder{
		ensureFn: func(_ context.Context, org, _ string) (string, string, bool, error) {
			return "pub-" + org, "sec-" + org, true, nil
		},
		deleteFn: func(context.Context, string) (bool, error) { return true, nil },
	})
	ctx := context.Background()

	// Eight orgs, each with a per-org publisher client id. Random UUID primary
	// keys mean a dropped org_id filter in GetProfile would return an arbitrary
	// row — with eight decoys that reliably mismatches (a 2-row test would
	// coin-flip). This is the mutation-verification target. (Issuer/jwksUrl are
	// NOT usable discriminators here: GetOrCreateProfile self-heals them back to
	// the shared platform config, so org_id + publisher_client_id are the
	// per-org fields that survive.)
	orgs := []string{"acme", "globex", "initech", "umbrella", "hooli", "stark", "wayne", "cyberdyne"}

	for _, o := range orgs {
		if _, _, _, err := svc.EnsureOrgPublisher(ctx, o, "ada@x.io"); err != nil {
			t.Fatalf("ensure %s: %v", o, err)
		}
	}

	// Every org resolves to its OWN row — org_id and client id both agree.
	for _, o := range orgs {
		row, err := svc.GetProfile(ctx, o)
		if err != nil || row == nil {
			t.Fatalf("GetProfile(%s): %+v err %v", o, row, err)
		}
		if row.OrgID != o {
			t.Fatalf("GetProfile(%s) returned another org's row: org_id=%q", o, row.OrgID)
		}
		if row.PublisherClientID != "pub-"+o {
			t.Fatalf("GetProfile(%s) client id=%q, want pub-%s (cross-org bleed)", o, row.PublisherClientID, o)
		}
	}

	// A mutation on ONE org (revoke) touches only its own row + audit trail.
	if _, err := svc.RevokeOrgPublisher(ctx, "acme", "ada@x.io"); err != nil {
		t.Fatalf("revoke acme: %v", err)
	}
	if row, _ := svc.GetProfile(ctx, "acme"); row.PublisherClientID != "" {
		t.Fatalf("acme publisher should be cleared: %q", row.PublisherClientID)
	}
	for _, o := range orgs[1:] {
		if row, _ := svc.GetProfile(ctx, o); row.PublisherClientID != "pub-"+o {
			t.Fatalf("acme revoke bled into %s: client id now %q", o, row.PublisherClientID)
		}
	}

	// Audit isolation: acme carries ensure+revoke; a decoy carries only ensure
	// (acme's revoke did not fan out).
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditRevokePublisher,
	}) {
		t.Fatalf("acme audit drifted: %v", got)
	}
	if got := auditActions(t, gormDB, "globex"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher,
	}) {
		t.Fatalf("globex audit should not see acme's revoke: %v", got)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ============================================================================
// REGRESSION GUARD (was a pinned latent bug, now fixed): GetOrCreateProfile's
// platform-field self-heal is gated on `existing.Kind == "platform"`. A BYO org
// (kind=custom/asgardeo) owns its own issuer/JWKS URL; those must SURVIVE the
// self-heal — an unconditional heal silently reverted them to the cluster
// platform default on the next read (data loss), which fired on nearly every
// read path. The complementary platform-kind path still self-heals (proven by
// TestGetOrCreateProfile_SelfHealsPlatformFields_DB and re-asserted below).
// ============================================================================
func TestGetOrCreateProfile_SelfHealPreservesCustomIssuer_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, nil)
	ctx := context.Background()

	// A BYO org: kind=custom with its own issuer/jwks.
	if _, err := svc.UpdateProfile(ctx, "byo-org", "tester", organization.UpdateProfileRequest{
		Kind:    "custom",
		Issuer:  "https://login.byo.example",
		JWKSURL: "https://login.byo.example/jwks",
	}); err != nil {
		t.Fatalf("seed BYO profile: %v", err)
	}

	// The next plain read-path call must NOT touch the BYO issuer/jwks.
	got, err := svc.GetOrCreateProfile(ctx, "byo-org")
	if err != nil {
		t.Fatalf("GetOrCreateProfile: %v", err)
	}
	if got.Kind != "custom" {
		t.Fatalf("kind must survive, got %q", got.Kind)
	}
	if got.Issuer != "https://login.byo.example" || got.JWKSURL != "https://login.byo.example/jwks" {
		t.Fatalf("BYO issuer/jwks must survive self-heal: issuer=%q jwks=%q", got.Issuer, got.JWKSURL)
	}
	// And it must be persisted — a re-read still shows the BYO values.
	reread, err := svc.GetProfile(ctx, "byo-org")
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if reread.Issuer != "https://login.byo.example" || reread.JWKSURL != "https://login.byo.example/jwks" {
		t.Fatalf("BYO issuer/jwks clobbered in DB: issuer=%q jwks=%q", reread.Issuer, reread.JWKSURL)
	}

	// Complementary: a platform-kind org whose cached fields drifted from the
	// current cluster config still self-heals (the gate lets platform through).
	seed := organization.NewIDPService(organization.NewIDPRepository(gormDB, nil), organization.NewOrganizationRepository(gormDB), nil, organization.PlatformIDPConfig{
		Issuer:  "http://old-issuer:8080",
		JWKSURL: "http://old-jwks:8090/oauth2/jwks",
	})
	if _, err := seed.GetOrCreateProfile(ctx, "platform-org"); err != nil {
		t.Fatalf("seed platform profile: %v", err)
	}
	healed, err := svc.GetOrCreateProfile(ctx, "platform-org")
	if err != nil {
		t.Fatalf("heal platform: %v", err)
	}
	if healed.Kind != "platform" ||
		healed.Issuer != idpDBPlatform.Issuer || healed.JWKSURL != idpDBPlatform.JWKSURL {
		t.Fatalf("platform-kind org must still self-heal: %+v", healed)
	}
}
