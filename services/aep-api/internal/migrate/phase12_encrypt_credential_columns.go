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

// RunPhase12EncryptCredentialColumns is a RETIRED tombstone. It sealed
// organization_idp_profiles.publisher_client_secret and the
// org_credentials.webhook_secrets entries in place with the column cipher.
// Both columns are gone (phase29): the publisher client secret lives only in
// the vault, and nothing verifies against the webhook list. The step stays in
// the list because the list is frozen (a removal would break the golden
// order).
func RunPhase12EncryptCredentialColumns(context.Context, *gorm.DB) error { return nil }
