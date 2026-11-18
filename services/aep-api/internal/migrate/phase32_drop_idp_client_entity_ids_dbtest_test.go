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
package migrate_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// The step drops the two entity id columns from a database that has them (the
// pre-migration shape, rebuilt by adding them and a row that fills them) and
// keeps the row with its client ids. A second run changes nothing.
func TestPhase32DropIDPClientEntityIDs_DropsTheColumnsIdempotently(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	ctx := context.Background()

	for _, stmt := range []string{
		`ALTER TABLE organization_idp_profiles ADD COLUMN publisher_thunder_app_id TEXT`,
		`ALTER TABLE organization_idp_profiles ADD COLUMN studio_thunder_app_id TEXT`,
		`INSERT INTO organization_idp_profiles (org_id, kind, issuer, jwks_url, publisher_client_id, publisher_thunder_app_id, studio_client_id, studio_thunder_app_id)
		 VALUES ('acme', 'platform', 'https://idp.test', 'https://idp.test/jwks', 'aep-publisher-acme', 'entity-1', 'ae-studio-acme', 'entity-2')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the pre-migration shape (%s): %v", stmt, err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := migrate.RunPhase32DropIDPClientEntityIDs(ctx, db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
	dropIdleConnections(t, db)

	var left int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'organization_idp_profiles'
		  AND column_name IN ('publisher_thunder_app_id', 'studio_thunder_app_id')`).Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("%d entity id columns are left; want none", left)
	}
	var row struct{ PublisherClientID, StudioClientID string }
	if err := db.Raw(`SELECT publisher_client_id, studio_client_id FROM organization_idp_profiles WHERE org_id = 'acme'`).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.PublisherClientID != "aep-publisher-acme" || row.StudioClientID != "ae-studio-acme" {
		t.Fatalf("the row's client ids = %+v, want both kept", row)
	}
}

// A fresh database never gets the two columns: phase26 no longer adds them.
func TestFreshDatabase_HasNoIDPClientEntityIDColumns(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	var n int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'organization_idp_profiles'
		  AND column_name LIKE '%thunder_app_id'`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d thunder_app_id columns on a fresh database; want none", n)
	}
}
