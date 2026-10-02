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
	"log/slog"
)

// orgSecretRefSource names where a consumer's SecretReference name came
// from. It is logged, never a value.
type orgSecretRefSource string

const (
	// orgSecretRefFromRow: the org_secrets row, which every write since
	// phase 1 records before it repoints and retires the previous reference.
	orgSecretRefFromRow orgSecretRefSource = "org_secrets"
	// orgSecretRefFromLegacyTriplet: the triplet columns of an org
	// connected before phase 1, which has no row yet. Removed in phase 6.
	orgSecretRefFromLegacyTriplet orgSecretRefSource = "legacy_triplet"
)

// RecordedOrgSecretRef returns the SecretReference name the row of s
// records, so a rotation never leaves a consumer mounting a reference the
// write already deleted (R7). ok is false when s has no row, or refs is nil:
// the caller then falls back to its own triplet columns, taking the name and
// the key from that one source. It logs, value-free, which source the
// caller uses.
func RecordedOrgSecretRef(ctx context.Context, refs OrgSecretRefReader, ocOrgID string, s OrgSecret) (name string, ok bool, err error) {
	if refs != nil {
		row, err := refs.Get(ctx, ocOrgID, s)
		if err != nil {
			return "", false, fmt.Errorf("read the %s row: %w", s, err)
		}
		if row != nil && row.Name != "" {
			logOrgSecretRefSource(ctx, ocOrgID, s, orgSecretRefFromRow)
			return row.Name, true, nil
		}
	}
	logOrgSecretRefSource(ctx, ocOrgID, s, orgSecretRefFromLegacyTriplet)
	return "", false, nil
}

// logOrgSecretRefSource logs the source: a row at Debug (the steady state), a
// legacy fallback at Info (an org phase 6 must migrate).
func logOrgSecretRefSource(ctx context.Context, ocOrgID string, s OrgSecret, source orgSecretRefSource) {
	level := slog.LevelDebug
	if source == orgSecretRefFromLegacyTriplet {
		level = slog.LevelInfo
	}
	slog.Log(ctx, level, "org secret reference resolved", "ocOrgId", ocOrgID, "secret", string(s), "source", string(source))
}
