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

// DBTEST tier (skips under -short; `make test-db` runs it): EnsureForOuHandle —
// the JIT org-row side-car the auth middleware calls on every authenticated
// request — driven over a pristine per-test Postgres (dbtest.New) with the REAL
// backfill / verify / cache / singleflight machinery. Only the out-of-process
// NamespaceClient is mocked. This is the tier the migration tracker names
// ("EnsureForOuHandle cache/backfill → dbtest"): the behaviours here are all
// SQL-shaped (they read and write the `organizations` table) and cannot be
// proven honestly with a fake DB.
//
// SECURITY NOTE — this feature owns the org→OU mapping (thunder_org_uuid), which
// downstream code turns into per-org namespaces, impersonation, and the runner's
// publisher OU binding. A phantom (Thunder-unknown) ouId written here poisons all
// three (the runner cc-token 401 root cause). The phantom-guard tests below pin
// EXACTLY which write paths the guard covers: the overwrite path
// (TestEnsureThunderUUID_PhantomOverwriteRefused_DB) AND the fresh-org NULL-set
// path (TestEnsureForOuHandle_PhantomOnFreshOrgIsRejected_DB) — the latter closed
// a gap where the guard was skipped for a just-backfilled NULL row.
//
// External test package: ou_validation_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle.
// fakeOUValidator is duplicated from ou_validation_test.go below (single
// consumer here). TestEnsureForOuHandle_CacheExpiryReverifies_DB previously
// simulated TTL expiry by writing straight into the unexported
// organizationService.ensureCache map; that's unreachable from a black-box
// test, so it now drives the SAME observable (OC-hit count before/after
// expiry) through organization.(*organizationService).WithEnsureCacheTTL — a
// small test-only production seam (organization_service.go) — plus a real,
// short sleep instead of rewriting the private map.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// fakeOUValidator duplicates ou_validation_test.go's fakeOUValidator (package
// organization, stays white-box — it also asserts on unexported
// organizationService.ouValidator directly).
type fakeOUValidator struct {
	exists bool
	err    error
}

func (f fakeOUValidator) OUExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.err
}

// nsMockReturning builds a NamespaceClient whose GetNamespace answers a live
// namespace for any handle (keyed by the requested name so concurrent
// different-handle calls stay correct). ListNamespaces is left nil — EnsureForOuHandle
// never calls it.
func nsMockReturning() *ocmocks.NamespaceClientMock {
	return &ocmocks.NamespaceClientMock{
		GetNamespaceFunc: func(_ context.Context, name string) (*gen.OrganizationView, error) {
			return &gen.OrganizationView{Name: name, DisplayName: name + " Inc", Status: "Active"}, nil
		},
	}
}

// loadOrg reads the single organizations row for name (t.Fatal if 0 or >1).
func loadOrg(t *testing.T, db *gorm.DB, name string) organization.Organization {
	t.Helper()
	var rows []organization.Organization
	if err := db.Where("name = ?", name).Find(&rows).Error; err != nil {
		t.Fatalf("load org %q: %v", name, err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly one row for %q, got %d", name, len(rows))
	}
	return rows[0]
}

func countOrg(t *testing.T, db *gorm.DB, name string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&organization.Organization{}).Where("name = ?", name).Count(&n).Error; err != nil {
		t.Fatalf("count org %q: %v", name, err)
	}
	return n
}

// TestEnsureForOuHandle_FirstCallBackfillsRow_DB pins the JIT backfill: the
// first authed sight of an org with a live OC namespace CREATES the local row,
// keyed by the handle, carrying the JWT's Thunder org UUID.
func TestEnsureForOuHandle_FirstCallBackfillsRow_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)
	thunder := uuid.NewString()

	if err := svc.EnsureForOuHandle(ctx, "acme", thunder); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	row := loadOrg(t, db, "acme")
	if row.Name != "acme" {
		t.Fatalf("row keyed by handle expected, got name=%q", row.Name)
	}
	if row.UUID == uuid.Nil {
		t.Fatal("row must get a local UUID")
	}
	if row.ThunderOrgUUID == nil || row.ThunderOrgUUID.String() != thunder {
		t.Fatalf("thunder_org_uuid: got %v, want %s", row.ThunderOrgUUID, thunder)
	}
	if got := len(ns.GetNamespaceCalls()); got != 1 {
		t.Fatalf("first call must verify against OC exactly once, got %d", got)
	}
}

// TestEnsureForOuHandle_SecondCallIsCacheServed_DB proves the hot path: a second
// call inside the TTL does NOT re-verify against OC and does NOT re-backfill —
// the cache guards the expensive OC round-trip. The (idempotent, cheap) thunder
// upsert may re-read the row, but leaves it byte-for-byte unchanged.
func TestEnsureForOuHandle_SecondCallIsCacheServed_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)
	thunder := uuid.NewString()

	if err := svc.EnsureForOuHandle(ctx, "acme", thunder); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	before := loadOrg(t, db, "acme")

	if err := svc.EnsureForOuHandle(ctx, "acme", thunder); err != nil {
		t.Fatalf("second ensure: %v", err)
	}

	if got := len(ns.GetNamespaceCalls()); got != 1 {
		t.Fatalf("second call must be cache-served (no OC re-verify), got %d GetNamespace calls", got)
	}
	if countOrg(t, db, "acme") != 1 {
		t.Fatal("second call must not create a duplicate row")
	}
	after := loadOrg(t, db, "acme")
	if after.UUID != before.UUID || after.ThunderOrgUUID.String() != before.ThunderOrgUUID.String() {
		t.Fatalf("cache-served call rewrote the row: before=%+v after=%+v", before, after)
	}
}

// TestEnsureForOuHandle_ExistingRowSkipsOCVerify_DB pins the DB-first branch of
// verifyForOuHandle: with a cold cache but a row already present, the verify
// resolves from the local row alone and never hits OC.
func TestEnsureForOuHandle_ExistingRowSkipsOCVerify_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)

	// Pre-seed the row (simulating an earlier process/warm start).
	if err := db.Create(&organization.Organization{UUID: uuid.New(), Name: "acme"}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if err := svc.EnsureForOuHandle(ctx, "acme", ""); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if got := len(ns.GetNamespaceCalls()); got != 0 {
		t.Fatalf("existing row must short-circuit OC verify, got %d GetNamespace calls", got)
	}
}

// TestEnsureForOuHandle_MissingNamespaceIsNotProvisioned_DB pins the
// unprovisioned path: no local row + OC 404 → organization.ErrOrganizationNotProvisioned and
// NO row created (the BFF never fabricates a namespace-less org).
func TestEnsureForOuHandle_MissingNamespaceIsNotProvisioned_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := &ocmocks.NamespaceClientMock{
		GetNamespaceFunc: func(context.Context, string) (*gen.OrganizationView, error) {
			return nil, openchoreo.ErrNotFound
		},
	}
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)

	err := svc.EnsureForOuHandle(ctx, "ghost", uuid.NewString())
	if !errors.Is(err, organization.ErrOrganizationNotProvisioned) {
		t.Fatalf("want organization.ErrOrganizationNotProvisioned, got %v", err)
	}
	if countOrg(t, db, "ghost") != 0 {
		t.Fatal("unprovisioned org must not create a local row")
	}
}

// TestEnsureForOuHandle_CacheExpiryReverifies_DB pins the TTL boundary — the
// exact check a "flip the cache" mutation would break. Within the TTL a deleted
// namespace is NOT re-verified (cache still warm); once the entry ages past the
// TTL, the next call re-verifies against OC. Proven by GetNamespace call count.
//
// The production TTL is 5 minutes, far too long to elapse in a test, and this
// black-box test cannot reach into the unexported ensureCache map to fake
// elapsed time. It moves the TTL instead of the clock: the effective TTL is
// read per call, so the within-TTL leg runs under one long enough that no DB
// round-trip can outlast it, and the expiry leg shrinks it under the entry's
// age. Same observable (OC-hit count) proves the same property, with no sleep
// and nothing to lose a race with.
func TestEnsureForOuHandle_CacheExpiryReverifies_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns).WithEnsureCacheTTL(time.Hour)

	// Call 1: cold cache → verify + backfill (OC hit #1).
	if err := svc.EnsureForOuHandle(ctx, "acme", ""); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	if got := len(ns.GetNamespaceCalls()); got != 1 {
		t.Fatalf("call 1 OC hits: got %d, want 1", got)
	}
	// Delete the row so any RE-verify would have to hit OC again (the row-first
	// branch can no longer satisfy it).
	if err := db.Where("name = ?", "acme").Delete(&organization.Organization{}).Error; err != nil {
		t.Fatalf("delete row: %v", err)
	}

	// Call 2: still inside the TTL → cache-served, NO OC re-verify even though
	// the row is gone.
	if err := svc.EnsureForOuHandle(ctx, "acme", ""); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if got := len(ns.GetNamespaceCalls()); got != 1 {
		t.Fatalf("call 2 (within TTL) must be cache-served, got %d OC hits", got)
	}

	// Age the entry past the TTL by shrinking the TTL under it, then call
	// again → re-verify (OC hit #2).
	svc.WithEnsureCacheTTL(time.Nanosecond)

	if err := svc.EnsureForOuHandle(ctx, "acme", ""); err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if got := len(ns.GetNamespaceCalls()); got != 2 {
		t.Fatalf("call 3 (past TTL) must re-verify against OC, got %d OC hits, want 2", got)
	}
}

// TestEnsureThunderUUID_PhantomOverwriteRefused_DB pins the guard WORKING for
// the overwrite path: an existing row already carries a validated Thunder UUID,
// and a request arrives bearing a DIFFERENT ouId that Thunder says does not
// exist (phantom). The row's thunder_org_uuid MUST be left intact — overwriting
// it with a phantom is the poisoning this guard exists to stop.
func TestEnsureThunderUUID_PhantomOverwriteRefused_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)
	svc.SetOUValidator(fakeOUValidator{exists: false}) // Thunder: this ouId does NOT exist

	good := uuid.New()
	if err := db.Create(&organization.Organization{UUID: uuid.New(), Name: "acme", ThunderOrgUUID: &good}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	phantom := uuid.NewString()
	if err := svc.EnsureForOuHandle(ctx, "acme", phantom); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	row := loadOrg(t, db, "acme")
	if row.ThunderOrgUUID == nil || *row.ThunderOrgUUID != good {
		t.Fatalf("phantom must NOT overwrite a validated UUID: got %v, want %s", row.ThunderOrgUUID, good)
	}
	if got := len(ns.GetNamespaceCalls()); got != 0 {
		t.Fatalf("existing row → no OC verify expected, got %d", got)
	}
}

// TestEnsureThunderUUID_ValidatedDriftOverwrites_DB pins the complementary
// branch: when the incoming ouId is DIFFERENT from the stored one but Thunder
// CONFIRMS it exists, the row is updated (Thunder is authoritative; genuine OU
// migrations must heal). This is the "trust guard, but only against phantoms"
// contract — a mutation that inverted the trust check would flip this.
func TestEnsureThunderUUID_ValidatedDriftOverwrites_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)
	svc.SetOUValidator(fakeOUValidator{exists: true}) // Thunder: the new ouId is real

	old := uuid.New()
	if err := db.Create(&organization.Organization{UUID: uuid.New(), Name: "acme", ThunderOrgUUID: &old}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	next := uuid.NewString()
	if err := svc.EnsureForOuHandle(ctx, "acme", next); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	row := loadOrg(t, db, "acme")
	if row.ThunderOrgUUID == nil || row.ThunderOrgUUID.String() != next {
		t.Fatalf("validated drift must overwrite: got %v, want %s", row.ThunderOrgUUID, next)
	}
}

// TestEnsureForOuHandle_PhantomOnFreshOrgIsRejected_DB pins the FIXED guard for
// the fresh-org path: a first request bearing a Thunder-unknown ouId must NOT
// poison thunder_org_uuid.
//
// backfillRow already refuses to stamp a phantom onto a NEW row (leaves the
// column NULL). EnsureForOuHandle then calls ensureThunderUUID, whose trust
// check now covers the NULL-set write too (previously it was gated behind
// `if row.ThunderOrgUUID != nil`, so the fresh NULL row skipped it). The row's
// thunder_org_uuid stays NULL until a valid login re-backfills it.
func TestEnsureForOuHandle_PhantomOnFreshOrgIsRejected_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)
	svc.SetOUValidator(fakeOUValidator{exists: false}) // Thunder: this ouId does NOT exist

	phantom := uuid.NewString()
	if err := svc.EnsureForOuHandle(ctx, "acme", phantom); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	row := loadOrg(t, db, "acme")
	// The fresh-org NULL path is now trust-checked: an untrusted OU is refused,
	// so the column stays NULL rather than being poisoned with the phantom.
	if row.ThunderOrgUUID != nil {
		t.Fatalf("fresh-org phantom must be rejected: got thunder_org_uuid=%v, want nil", row.ThunderOrgUUID)
	}
}

// TestEnsureForOuHandle_ConcurrentSameHandleCollapses_DB proves singleflight: N
// goroutines racing the FIRST sight of the same handle collapse into ONE OC
// verify and ONE backfill (one row), with no data race on the cache/inflight
// maps (run under -race). The GetNamespace mock blocks long enough for every
// caller to join the same flight before the leader completes.
func TestEnsureForOuHandle_ConcurrentSameHandleCollapses_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	release := make(chan struct{})
	ns := &ocmocks.NamespaceClientMock{
		GetNamespaceFunc: func(_ context.Context, name string) (*gen.OrganizationView, error) {
			// Hold the flight open so all racers pile into singleflight.Do before
			// the leader finishes and warms the cache.
			<-release
			return &gen.OrganizationView{Name: name, Status: "Active"}, nil
		},
	}
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)

	const n = 10
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = svc.EnsureForOuHandle(ctx, "acme", "")
		}(i)
	}
	close(start)                       // launch all racers together
	time.Sleep(150 * time.Millisecond) // let them all block inside the single flight
	close(release)                     // let the leader complete
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	if got := len(ns.GetNamespaceCalls()); got != 1 {
		t.Fatalf("singleflight must collapse to ONE OC verify, got %d", got)
	}
	if countOrg(t, db, "acme") != 1 {
		t.Fatalf("singleflight must produce exactly one row, got %d", countOrg(t, db, "acme"))
	}
}

// TestEnsureForOuHandle_DifferentHandlesDoNotInterfere_DB proves the singleflight
// KEY is the handle, not a constant: two handles raced concurrently each get
// their own verify + row. A mutation collapsing the key would starve one handle
// of its backfill (missing row), which this catches. Run under -race.
func TestEnsureForOuHandle_DifferentHandlesDoNotInterfere_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)

	handles := []string{"acme", "globex", "initech", "umbrella"}
	start := make(chan struct{})
	errs := make([]error, len(handles))
	var wg sync.WaitGroup
	wg.Add(len(handles))
	for i, h := range handles {
		go func(i int, h string) {
			defer wg.Done()
			<-start
			errs[i] = svc.EnsureForOuHandle(ctx, h, "")
		}(i, h)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("handle %q: %v", handles[i], err)
		}
	}
	for _, h := range handles {
		if countOrg(t, db, h) != 1 {
			t.Fatalf("handle %q must get its own row, got count=%d", h, countOrg(t, db, h))
		}
	}
	if got := len(ns.GetNamespaceCalls()); got != len(handles) {
		t.Fatalf("each distinct handle must verify once: got %d OC hits, want %d", got, len(handles))
	}
}

// TestEnsureForOuHandle_IsNameScopedAmongDecoys_DB proves every per-org query in
// the ensure path is scoped by the handle: with a table full of decoy orgs (each
// with its own thunder_org_uuid), ensuring ONE handle must touch ONLY that
// handle's row — no decoy's UUID may change and no decoy row may vanish. This is
// the mutation net for a dropped/broadened WHERE clause in verify / backfill /
// ensureThunderUUID.
func TestEnsureForOuHandle_IsNameScopedAmongDecoys_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	ns := nsMockReturning()
	svc := organization.NewOrganizationService(organization.NewOrganizationRepository(db), ns)

	// Seed several decoys, each with a distinct, recorded thunder_org_uuid.
	decoyUUID := map[string]uuid.UUID{}
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("decoy-%d", i)
		u := uuid.New()
		decoyUUID[name] = u
		if err := db.Create(&organization.Organization{UUID: uuid.New(), Name: name, ThunderOrgUUID: &u}).Error; err != nil {
			t.Fatalf("seed decoy %q: %v", name, err)
		}
	}

	target := uuid.NewString()
	if err := svc.EnsureForOuHandle(ctx, "target", target); err != nil {
		t.Fatalf("ensure target: %v", err)
	}

	// The target row exists and carries its own UUID.
	tr := loadOrg(t, db, "target")
	if tr.ThunderOrgUUID == nil || tr.ThunderOrgUUID.String() != target {
		t.Fatalf("target row thunder: got %v, want %s", tr.ThunderOrgUUID, target)
	}
	// Every decoy is byte-for-byte untouched.
	for name, want := range decoyUUID {
		row := loadOrg(t, db, name)
		if row.ThunderOrgUUID == nil || *row.ThunderOrgUUID != want {
			t.Fatalf("decoy %q was mutated: got %v, want %s", name, row.ThunderOrgUUID, want)
		}
	}
}
