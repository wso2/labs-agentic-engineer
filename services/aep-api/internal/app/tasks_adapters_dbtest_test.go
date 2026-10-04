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

package app

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

func seedRepo(t *testing.T, db *gorm.DB, org, project, url string) {
	t.Helper()
	if err := db.Create(&sourcecontrol.GitRepository{OrgID: org, ProjectID: project, RepoURL: url, Status: "ready"}).Error; err != nil {
		t.Fatalf("seed %s/%s: %v", org, project, err)
	}
}

// A webhook handler resolving a repository's full name stays in the delivery
// org: the same repository URL on another org's row is never the answer.
func TestRepoLocator_UsesDeliveryOrg(t *testing.T) {
	db := dbtest.New(t)
	seedRepo(t, db, "org-a", "p1", "https://github.com/acme/greeter")
	seedRepo(t, db, "org-b", "p9", "https://github.com/acme/greeter")
	org, project, err := repoLocator{db: db}.ByFullName(webhook.WithDeliveryOrg(context.Background(), "org-b"), "acme/greeter")
	if err != nil || org != "org-b" || project != "p9" {
		t.Fatalf("got %s/%s %v, want org-b/p9", org, project, err)
	}
}

// The reverse lookup of the declined org-publish gate (repoNamer) and the
// eventcore one (repoLocator) answer nothing for a repository the delivery org
// does not own, and a .git clone URL matches like a bare one.
func TestRepoLookups_StayInTheDeliveryOrg(t *testing.T) {
	db := dbtest.New(t)
	seedRepo(t, db, "org-a", "p1", "https://github.com/acme/greeter.git")
	lookups := map[string]func(context.Context, string) (string, string, error){
		"repoLocator": repoLocator{db: db}.ByFullName,
		"repoNamer":   repoNamer{db: db}.ByFullName,
	}
	for name, byFullName := range lookups {
		org, project, err := byFullName(webhook.WithDeliveryOrg(context.Background(), "org-a"), "acme/greeter")
		if err != nil || org != "org-a" || project != "p1" {
			t.Errorf("%s in org-a: got %q/%q %v, want org-a/p1", name, org, project, err)
		}
		org, project, err = byFullName(webhook.WithDeliveryOrg(context.Background(), "org-b"), "acme/greeter")
		if err != nil || org != "" || project != "" {
			t.Errorf("%s in org-b: got %q/%q %v, want nothing", name, org, project, err)
		}
	}
}
