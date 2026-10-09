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

package organization

import (
	"context"
	"fmt"
)

// RecordedOrgSecretRef returns the SecretReference name the row of s
// records, so a rotation never leaves a consumer mounting a reference the
// write already deleted (R7). ok is false when s has no row: the secret
// lives only in vault and the row is the record that it was written, so the
// caller has no reference to mount. A nil refs is a wiring error.
func RecordedOrgSecretRef(ctx context.Context, refs OrgSecretRefReader, ocOrgID string, s OrgSecret) (name string, ok bool, err error) {
	if refs == nil {
		return "", false, fmt.Errorf("read the %s row: org secret rows not configured", s)
	}
	row, err := refs.Get(ctx, ocOrgID, s)
	if err != nil {
		return "", false, fmt.Errorf("read the %s row: %w", s, err)
	}
	if row == nil || row.Name == "" {
		return "", false, nil
	}
	return row.Name, true, nil
}
