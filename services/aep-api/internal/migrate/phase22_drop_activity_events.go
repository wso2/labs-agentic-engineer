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
package migrate

import (
	"fmt"

	"gorm.io/gorm"
)

// RunPhase22DropActivityEvents drops the activity feed's table. The feed had
// no reader and its writers are deleted; AutoMigrate created the table and
// never drops one.
func RunPhase22DropActivityEvents(db *gorm.DB) error {
	if err := db.Exec(`DROP TABLE IF EXISTS activity_events CASCADE`).Error; err != nil {
		return fmt.Errorf("phase22_drop_activity_events: %w", err)
	}
	return nil
}
