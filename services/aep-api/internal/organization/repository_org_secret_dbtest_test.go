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
// row the repository must never read, list or touch.

import (
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

	if got, err := repo.Get(ctx, "default", organization.OrgSecretGitHubPAT); err != nil || got != nil {
		t.Fatalf("Get unset = %v, %v; want nil, nil", got, err)
	}

	t1 := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	for _, w := range []struct {
		org string
		ref organization.OrgSecretRef
	}{
		{"default", organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "default-github-pat-00000001", WrittenAt: t1}},
		{"default", organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: "default-default-key-00000002", WrittenAt: t1}},
		{"other", organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "other-github-pat-00000003", WrittenAt: t1}},
		// A rewrite replaces the row.
		{"default", organization.OrgSecretRef{Secret: organization.OrgSecretGitHubPAT, Name: "default-github-pat-00000004", WrittenAt: t2}},
	} {
		if err := repo.Upsert(ctx, w.org, w.ref); err != nil {
			t.Fatalf("Upsert %s %+v: %v", w.org, w.ref, err)
		}
	}

	got, err := repo.Get(ctx, "default", organization.OrgSecretGitHubPAT)
	if err != nil || got == nil || got.Name != "default-github-pat-00000004" || !got.WrittenAt.Equal(t2) {
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

	if err := repo.Delete(ctx, "default", organization.OrgSecretGitHubPAT); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := repo.Delete(ctx, "default", organization.OrgSecretGitHubPAT); err != nil {
		t.Fatalf("Delete absent: %v", err)
	}
	if got, err := repo.Get(ctx, "default", organization.OrgSecretGitHubPAT); err != nil || got != nil {
		t.Fatalf("Get after Delete = %v, %v", got, err)
	}
	if got, _ := repo.Get(ctx, "other", organization.OrgSecretGitHubPAT); got == nil {
		t.Fatal("Delete crossed orgs")
	}

	var legacy string
	if err := db.Raw(`SELECT value FROM org_secrets WHERE oc_org_id = 'default' AND key = 'github/pat'`).Scan(&legacy).Error; err != nil || legacy != "sealed-bytes" {
		t.Fatalf("legacy row = %q, %v; want untouched", legacy, err)
	}
}
