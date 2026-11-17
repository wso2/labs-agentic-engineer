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
// over a pristine per-test Postgres (dbtest.New) with the org's pre-created
// clients record and a fake vault — the SQL-shaped behavior under pin:
// GetOrCreateProfile's create-then-get idempotency + platform-field self-heal,
// the publisher's row writes (the client ensure records the client's ids and
// the ae-publisher-client row, and never a secret column), the read-only
// build gate, the append-only idp_audit_events trail with the right action
// per mutation, org scoping across many decoy orgs (mutation-verified — a
// dropped WHERE would coin-flip with 2 rows under random UUID PKs), and the
// "no partial damage" contract of an ensure with no clients record. The
// audit trail is READ directly off idp_audit_events (the table the service
// wrote) — the service has no audit reader, so this is not a hand-copied
// production query, it is the incident-response view of a table production owns.
//
// External test package: idp_service_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle.
// The vault is gitpat_submit's submitVault.

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// idpDBPlatform is the cluster-level IDP default a fresh profile is seeded with.
var idpDBPlatform = organization.PlatformIDPConfig{
	Issuer:  "http://thunder.test:8080",
	JWKSURL: "http://thunder.test:8090/oauth2/jwks",
}

// idpDBService wires the real service: per-test Postgres + the given record
// of the org's pre-created clients (nil: no record). The *gorm.DB is
// returned so tests can read the audit table the service writes.
// Returns the exported IDPService interface (NewIDPService's concrete return
// type is unexported and unreachable from this package).
//
// The service writes the org clients' credentials through a fake vault
// (submitVault) and records their references in the real org_secrets table,
// as production does; the vault path comes from idpDBCtx's ouId claim.
func idpDBService(t *testing.T, clients *organization.OrgClients) (organization.IDPService, *gorm.DB) {
	t.Helper()
	db := dbtest.New(t)
	vault := &submitVault{log: &submitLog{}, live: map[string]bool{}, data: map[string]map[string]string{}, writes: map[string]int{}}
	rows := organization.NewOrgSecretRepository(db)
	writer := organization.NewSecretRefWriter(vault, organization.NewIDPRepository(db)).
		WithOrgSecretWriter(organization.NewOrgSecretWriter(vault, rows, organization.NewOrgSecretLock(db), time.Now))
	svc := organization.NewIDPService(organization.NewIDPRepository(db), organization.NewOrganizationRepository(db), clients, idpDBPlatform).
		WithSecretRefWriter(writer).WithOrgSecretRefs(rows)
	return svc, db
}

// idpDBAcme is the install's clients record when it serves org "acme".
func idpDBAcme() *organization.OrgClients {
	return organization.NewOrgClients("acme", "publisher-secret-acme", "studio-secret-acme")
}

// idpDBOU is the IdP org id of the requests below (their ouId claim).
var idpDBOU = uuid.MustParse("6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b")

// idpDBCtx is a user request whose ouId claim names idpDBOU: the vault path
// every org secret write derives from.
func idpDBCtx() context.Context {
	return jwtassertion.ContextWithTokenClaims(context.Background(), &jwtassertion.TokenClaims{OrgID: idpDBOU.String()})
}

// publisherRef is the org's ae-publisher-client row's reference name ("" =
// no row).
func publisherRef(t *testing.T, db *gorm.DB, org string) string {
	t.Helper()
	ref, err := organization.NewOrgSecretRepository(db).Get(context.Background(), org, organization.OrgSecretPublisherClient)
	if err != nil {
		t.Fatalf("read the ae-publisher-client row: %v", err)
	}
	if ref == nil {
		return ""
	}
	return ref.Name
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
	svc, gormDB := idpDBService(t, nil)
	ctx := context.Background()

	// No row yet: GetProfile returns (nil, nil).
	if p, err := svc.GetProfile(ctx, "acme"); err != nil || p != nil {
		t.Fatalf("absent org: want (nil,nil), got (%+v,%v)", p, err)
	}

	// First GetOrCreate seeds the default platform-kind row from the cluster
	// config; publisher_* stay empty until the client ensure.
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
	old := organization.NewIDPService(organization.NewIDPRepository(gormDB), organization.NewOrganizationRepository(gormDB), nil, organization.PlatformIDPConfig{
		Issuer:  "http://old-issuer:8080",
		JWKSURL: "http://old-jwks:8090/oauth2/jwks",
	})
	if _, err := old.GetOrCreateProfile(ctx, "acme"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A service running the NEW cluster config self-heals the cached fields on
	// the next GetOrCreate (issuer/jwks are cluster config, not per-org data).
	fresh := organization.NewIDPService(organization.NewIDPRepository(gormDB), organization.NewOrganizationRepository(gormDB), nil, organization.PlatformIDPConfig{
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

// --- EnsureClient(publisher) ------------------------------------------------

func TestEnsurePublisherClient_RecordsIDsAndRowAndAudits_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// The profile records the client's id (which stands in for the entity id
	// column too); the secret is only in the reference the
	// ae-publisher-client row names, never in a column.
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil {
		t.Fatalf("get after ensure: %v", err)
	}
	if row.PublisherClientID != "aep-publisher-acme" || row.PublisherThunderAppID != "aep-publisher-acme" {
		t.Fatalf("publisher ids not persisted: %+v", row)
	}
	if publisherRef(t, gormDB, "acme") == "" {
		t.Fatal("the ae-publisher-client row was not recorded")
	}

	// Exactly one audit row, action=ensure_publisher, the ensure's actor, no error.
	rows := auditRows(t, gormDB, "acme")
	if len(rows) != 1 || rows[0].Action != organization.IDPAuditEnsurePublisher {
		t.Fatalf("audit drifted: %+v", rows)
	}
	if rows[0].Actor != organization.ClientEnsureActor || rows[0].ErrorMessage != "" {
		t.Fatalf("audit actor/error drifted: %+v", rows[0])
	}
	if len(rows[0].AfterState) == 0 {
		t.Fatalf("successful ensure must record an after-state snapshot")
	}
}

// An org the install has no pre-created clients for is refused, and the
// refusal is audited.
func TestEnsurePublisherClient_AnotherOrgAuditsFailure_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "globex", organization.ClientPublisher); err == nil {
		t.Fatalf("ensure must refuse an org with no pre-created clients")
	}

	// The profile row WAS created (GetOrCreateProfile runs first) but stays
	// publisher-less, and no reference is recorded.
	row, err := svc.GetProfile(ctx, "globex")
	if err != nil || row == nil {
		t.Fatalf("profile row should still exist after a refused ensure: %+v err %v", row, err)
	}
	if row.PublisherClientID != "" || publisherRef(t, gormDB, "globex") != "" {
		t.Fatalf("refused ensure must not record a publisher: %+v", row)
	}

	// The failure is audited with the error message.
	rows := auditRows(t, gormDB, "globex")
	if len(rows) != 1 || rows[0].Action != organization.IDPAuditEnsurePublisher || rows[0].ErrorMessage == "" {
		t.Fatalf("the refusal must be audited with an error: %+v", rows)
	}
}

// A profile row from before the id columns existed (NULL) reads as "no id".
func TestProfile_NullIDColumnsReadAsEmpty_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := gormDB.Exec(`UPDATE organization_idp_profiles SET publisher_thunder_app_id = NULL,
		studio_client_id = NULL, studio_thunder_app_id = NULL WHERE org_id = 'acme'`).Error; err != nil {
		t.Fatalf("null the id columns: %v", err)
	}
	row, err := svc.GetProfile(ctx, "acme")
	if err != nil || row.PublisherThunderAppID != "" || row.StudioClientID != "" || row.StudioThunderAppID != "" {
		t.Fatalf("NULL id columns must read as empty: %+v err %v", row, err)
	}
}

// --- RequirePublisherForBuild ----------------------------------------------

// The build gate follows the ae-publisher-client row: missing before the
// ensure, present after.
func TestRequirePublisherForBuild_FollowsTheRow_DB(t *testing.T) {
	t.Parallel()
	svc, _ := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.RequirePublisherForBuild(ctx, "acme"); !errors.Is(err, delivery.ErrPublisherCredentialsMissing) {
		t.Fatalf("before the ensure: want ErrPublisherCredentialsMissing, got %v", err)
	}
	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.RequirePublisherForBuild(ctx, "acme"); err != nil {
		t.Fatalf("after the ensure: %v", err)
	}
}

// --- UpdateProfile ----------------------------------------------------------

func TestUpdateProfile_IssuerOnlyPreservesPublisher_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ref := publisherRef(t, gormDB, "acme")

	// Same kind, new issuer/jwks → publisher preserved.
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
	if updated.PublisherClientID != "aep-publisher-acme" || publisherRef(t, gormDB, "acme") != ref {
		t.Fatalf("same-kind update must preserve the publisher and its reference: %+v", updated)
	}
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

// platform → custom: the publisher client is the platform IdP's whatever the
// org's own IdP kind is, so its ids and its reference are kept and the build
// gate still passes.
func TestUpdateProfile_KindChangeKeepsThePublisher_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, idpDBAcme())
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ref := publisherRef(t, gormDB, "acme")

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
	if updated.PublisherClientID != "aep-publisher-acme" || updated.PublisherThunderAppID != "aep-publisher-acme" || publisherRef(t, gormDB, "acme") != ref {
		t.Fatalf("kind switch must keep the publisher ids and its reference: %+v", updated)
	}
	if err := svc.RequirePublisherForBuild(ctx, "acme"); err != nil {
		t.Fatalf("after the kind switch the build gate must still pass, got %v", err)
	}
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditEnsurePublisher, organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("audit actions drifted: %v", got)
	}
}

// --- no clients record: no partial damage -----------------------------------

func TestEnsureClient_NoClientsRecordNoPartialDamage_DB(t *testing.T) {
	t.Parallel()
	// A real db but no clients record: the ensure must fail BEFORE creating a
	// profile row or writing an audit event.
	svc, gormDB := idpDBService(t, nil)
	ctx := idpDBCtx()

	if err := svc.EnsureClient(ctx, "acme", organization.ClientPublisher); err == nil {
		t.Fatal("ensure passed with no clients record")
	}

	if p, err := svc.GetProfile(ctx, "acme"); err != nil || p != nil {
		t.Fatalf("a refused ensure must not create a profile row: %+v err %v", p, err)
	}
	var n int64
	if err := gormDB.Model(&organization.IDPAuditEvent{}).Where("org_id = ?", "acme").Count(&n).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if n != 0 {
		t.Fatalf("a refused ensure must not write an audit row, got %d", n)
	}
}

// --- org scoping across many decoys (mutation-verified) ---------------------

func TestOrgScoping_ManyDecoys_DB(t *testing.T) {
	t.Parallel()
	svc, gormDB := idpDBService(t, nil)
	ctx := idpDBCtx()

	// Eight orgs, each a BYO profile with its own issuer. Random UUID primary
	// keys mean a dropped org_id filter in GetProfile would return an arbitrary
	// row — with eight decoys that reliably mismatches (a 2-row test would
	// coin-flip). This is the mutation-verification target. (A custom-kind
	// profile keeps its issuer: only platform-kind rows self-heal to the shared
	// platform config.)
	orgs := []string{"acme", "globex", "initech", "umbrella", "hooli", "stark", "wayne", "cyberdyne"}
	issuerOf := func(o string) string { return "https://idp." + o + ".example" }

	for _, o := range orgs {
		if _, err := svc.UpdateProfile(ctx, o, "ada@x.io", organization.UpdateProfileRequest{Kind: "custom", Issuer: issuerOf(o)}); err != nil {
			t.Fatalf("seed %s: %v", o, err)
		}
	}

	// Every org resolves to its OWN row — org_id and issuer both agree.
	for _, o := range orgs {
		row, err := svc.GetProfile(ctx, o)
		if err != nil || row == nil {
			t.Fatalf("GetProfile(%s): %+v err %v", o, row, err)
		}
		if row.OrgID != o {
			t.Fatalf("GetProfile(%s) returned another org's row: org_id=%q", o, row.OrgID)
		}
		if row.Issuer != issuerOf(o) {
			t.Fatalf("GetProfile(%s) issuer=%q, want %s (cross-org bleed)", o, row.Issuer, issuerOf(o))
		}
	}

	// A mutation on ONE org touches only its own row + audit trail.
	if _, err := svc.UpdateProfile(ctx, "acme", "ada@x.io", organization.UpdateProfileRequest{Issuer: "https://moved.acme.example"}); err != nil {
		t.Fatalf("update acme: %v", err)
	}
	if row, _ := svc.GetProfile(ctx, "acme"); row.Issuer != "https://moved.acme.example" {
		t.Fatalf("acme issuer should have moved: %q", row.Issuer)
	}
	for _, o := range orgs[1:] {
		if row, _ := svc.GetProfile(ctx, o); row.Issuer != issuerOf(o) {
			t.Fatalf("acme's update bled into %s: issuer now %q", o, row.Issuer)
		}
	}

	// Audit isolation: acme carries two updates; a decoy carries only its own.
	if got := auditActions(t, gormDB, "acme"); !equalStrings(got, []string{
		organization.IDPAuditUpdateProfile, organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("acme audit drifted: %v", got)
	}
	if got := auditActions(t, gormDB, "globex"); !equalStrings(got, []string{
		organization.IDPAuditUpdateProfile,
	}) {
		t.Fatalf("globex audit should not see acme's update: %v", got)
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
	seed := organization.NewIDPService(organization.NewIDPRepository(gormDB), organization.NewOrganizationRepository(gormDB), nil, organization.PlatformIDPConfig{
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
