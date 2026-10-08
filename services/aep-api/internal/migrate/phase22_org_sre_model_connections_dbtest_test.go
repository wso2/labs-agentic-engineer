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

// The boot path creates org_sre_model_connections with its 7 columns, and a
// re-run is a no-op (CREATE TABLE IF NOT EXISTS).
func TestPhase22OrgSreModelConnections(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	if err := migrate.RunPhase22OrgSreModelConnections(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := migrate.RunPhase22OrgSreModelConnections(ctx, db); err != nil {
		t.Fatalf("not idempotent: %v", err)
	}
	var n int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name = 'org_sre_model_connections'`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Fatalf("columns = %d, want 7", n)
	}
}
