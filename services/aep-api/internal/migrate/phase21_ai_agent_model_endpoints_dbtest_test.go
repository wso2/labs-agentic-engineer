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

// The boot path creates the table with its composite key, and a re-run keeps
// the rows it holds: the step is CREATE ... IF NOT EXISTS and nothing else.
func TestPhase21AIAgentModelEndpoints_CreatesTheTableAndReRunsAsANoOp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	bootMigrate(t, db)

	insert := `INSERT INTO ai_agent_model_endpoints (oc_org_id, component, environment, endpoint, updated_at)
	           VALUES ('acme', 'checkout-agent', 'default', 'http://gw/aep-x/v1', now())`
	if err := db.Exec(insert).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.Exec(insert).Error; err == nil {
		t.Fatal("a second row for the same (org, component, environment) was accepted")
	}
	if err := db.Exec(`INSERT INTO ai_agent_model_endpoints (oc_org_id, component, environment, endpoint, updated_at)
	                   VALUES ('acme', 'checkout-agent', 'staging', 'http://gw/aep-y/v1', now())`).Error; err != nil {
		t.Fatalf("another environment of the same agent: %v", err)
	}

	if err := migrate.RunPhase21AIAgentModelEndpoints(ctx, db); err != nil {
		t.Fatalf("phase21 re-run: %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM ai_agent_model_endpoints`); n != 2 {
		t.Fatalf("rows after a re-run = %d, want both kept", n)
	}
}
