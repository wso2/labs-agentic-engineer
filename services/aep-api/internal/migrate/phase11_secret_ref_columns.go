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

// RunPhase11SecretRefColumns is a RETIRED tombstone. It added the
// secret_ref_name/kv_path/property(/written_at) triplet to
// org_anthropic_credentials, org_credentials and organization_idp_profiles,
// and copied leftover sm_api_* names into it. The org_secrets reference rows
// replaced the triplet, and phase26 drops it; re-adding it on every boot only
// for phase26 to drop it again would be churn. The step stays in the list
// because the list is frozen (a removal would break the golden order).
// Leftover sm_api_* columns are still dropped by phase14.
func RunPhase11SecretRefColumns(context.Context, *gorm.DB) error { return nil }
