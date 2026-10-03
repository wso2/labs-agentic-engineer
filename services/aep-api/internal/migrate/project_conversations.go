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
	"context"

	"gorm.io/gorm"
)

// RunProjectConversations is a RETIRED tombstone. It created the
// one-current-thread-per-scope partial unique index on project_conversations
// (#430), the conversation store of aep-api's in-process turn engine. Turns
// and their conversations live in the org's AE Studio pod now (phase 3), the
// model is gone, and phase24 drops the table. The step stays in the list
// because the list is frozen (a removal would break the golden order), and it
// does nothing so an upgrade boot never re-creates the index on a table
// phase24 is about to drop.
func RunProjectConversations(context.Context, *gorm.DB) error { return nil }
