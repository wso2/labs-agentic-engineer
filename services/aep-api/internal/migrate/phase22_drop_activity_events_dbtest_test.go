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
	"testing"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func TestPhase22DropActivityEvents(t *testing.T) {
	db := dbtest.New(t)
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS activity_events (id text primary key)`).Error; err != nil {
		t.Fatalf("seed table: %v", err)
	}
	for i := 0; i < 2; i++ { // idempotent
		if err := migrate.RunPhase22DropActivityEvents(db); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	var n int64
	db.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='activity_events'`).Scan(&n)
	if n != 0 {
		t.Fatalf("activity_events still exists")
	}
}
