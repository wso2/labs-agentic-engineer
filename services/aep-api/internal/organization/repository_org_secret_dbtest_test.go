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

// DBTEST tier (skips under -short; `make test-db` runs it): the org secrets'
// reference rows against a pristine migrated Postgres, beside a legacy value
// row the repository must never read, list or touch, and the compare-and-swap
// writes that make concurrent writers of one secret lose instead of clobber.

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func TestRepository_RoundTrip(t *testing.T) {
	db := dbtest.New(t)
	repo := organization.NewOrgSecretRepository(db)
	if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, value) VALUES ('default', 'github/pat', 'sealed-bytes')`).Error; err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	gp, dk := organization.OrgSecretGitHubPAT, organization.OrgSecretDefaultKey

	if got, err := repo.Get(ctx, "default", gp); err != nil || got != nil {
		t.Fatalf("Get unset = %v, %v; want nil, nil", got, err)
	}

	t1 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	for _, w := range []struct {
		org, expectPrev string
		ref             organization.OrgSecretRef
	}{
		{"default", "", organization.OrgSecretRef{Secret: gp, Name: "default-github-pat-00000001", WrittenAt: &t1}},
		{"default", "", organization.OrgSecretRef{Secret: dk, Name: "default-default-key-00000002", WrittenAt: &t1}},
		{"other", "", organization.OrgSecretRef{Secret: gp, Name: "other-github-pat-00000003", WrittenAt: &t1}},
		// A rewrite replaces the row it read.
		{"default", "default-github-pat-00000001", organization.OrgSecretRef{Secret: gp, Name: "default-github-pat-00000004", WrittenAt: &t2}},
	} {
		if err := repo.Upsert(ctx, w.org, w.ref, w.expectPrev); err != nil {
			t.Fatalf("Upsert %s %+v: %v", w.org, w.ref, err)
		}
	}

	got, err := repo.Get(ctx, "default", gp)
	if err != nil || got == nil || got.Name != "default-github-pat-00000004" || got.WrittenAt == nil || !got.WrittenAt.Equal(t2) {
		t.Fatalf("Get = %+v, %v; want the rewritten row", got, err)
	}

	list, err := repo.List(ctx, "default")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	names := make([]string, 0, len(list))
	for _, r := range list {
		names = append(names, string(r.Secret)+"="+r.Name)
	}
	if want := []string{"default-key=default-default-key-00000002", "github-pat=default-github-pat-00000004"}; !slices.Equal(names, want) {
		t.Fatalf("List = %v, want %v (no legacy row, no other org)", names, want)
	}

	if err := repo.Delete(ctx, "default", gp, "default-github-pat-00000004"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, err := repo.Get(ctx, "default", gp); err != nil || got != nil {
		t.Fatalf("Get after Delete = %v, %v", got, err)
	}
	if got, _ := repo.Get(ctx, "other", gp); got == nil {
		t.Fatal("Delete crossed orgs")
	}

	var legacy string
	if err := db.Raw(`SELECT value FROM org_secrets WHERE oc_org_id = 'default' AND key = 'github/pat'`).Scan(&legacy).Error; err != nil || legacy != "sealed-bytes" {
		t.Fatalf("legacy row = %q, %v; want untouched", legacy, err)
	}
}

func TestRepository_CompareAndSwap(t *testing.T) {
	db := dbtest.New(t)
	repo := organization.NewOrgSecretRepository(db)
	dk := organization.OrgSecretDefaultKey
	at := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	ref := func(name string) organization.OrgSecretRef {
		return organization.OrgSecretRef{Secret: dk, Name: name, WrittenAt: &at}
	}
	conflict := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, organization.ErrOrgSecretConflict) {
			t.Fatalf("%s: err = %v, want ErrOrgSecretConflict", what, err)
		}
	}
	nameIs := func(want string) {
		t.Helper()
		got, err := repo.Get(ctx, "default", dk)
		if err != nil || got == nil || got.Name != want {
			t.Fatalf("row = %+v, %v; want %s", got, err, want)
		}
	}

	if err := repo.Upsert(ctx, "default", ref("P"), ""); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Two writers that both read "no row": the second insert loses.
	conflict("second insert", repo.Upsert(ctx, "default", ref("X"), ""))
	nameIs("P")
	// Two writers that both read P: the first swap wins, the second loses.
	if err := repo.Upsert(ctx, "default", ref("N1"), "P"); err != nil {
		t.Fatalf("swap P→N1: %v", err)
	}
	conflict("stale swap P→N2", repo.Upsert(ctx, "default", ref("N2"), "P"))
	nameIs("N1")
	// A rollback that expects its own name leaves a later writer's row.
	conflict("stale restore", repo.Upsert(ctx, "default", ref("P"), "N2"))
	conflict("stale delete", repo.Delete(ctx, "default", dk, "P"))
	nameIs("N1")
	// The legacy value row is not a reference row: a swap never matches it.
	if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, value) VALUES ('legacy', 'default-key', 'sealed-bytes')`).Error; err != nil {
		t.Fatalf("seed a value-only row: %v", err)
	}
	conflict("delete a value-only row", repo.Delete(ctx, "legacy", dk, "x"))
	conflict("insert over a value-only row", repo.Upsert(ctx, "legacy", ref("Y"), ""))
}
