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

package openchoreo

import (
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// derefStr returns *s or "" if s is nil. The generated OC types
// (gen.ObjectMeta.Uid, gen.ObjectMeta.Namespace, …) are pointer-everywhere
// per oapi-codegen's `omitempty`-default rule, so unwrap helpers DRY the
// per-method conversion. Matches the role agent-manager's
// `utils.StrPointerAsStr` plays.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// derefTimeRFC3339 returns t formatted in RFC3339 UTC, or "" if t is nil.
// gen.Project.CreatedAt is a string; OC surfaces *time.Time.
func derefTimeRFC3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// derefTime returns *t, or the zero Time if t is nil — for the callers that
// order objects by when the cluster admitted them rather than render a date.
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// annotation reads key from a pointer-to-map without panicking on nil.
// Annotations are `*map[string]string` on every gen ObjectMeta.
func annotation(ann *map[string]string, key string) string {
	if ann == nil {
		return ""
	}
	return (*ann)[key]
}

// label reads key from a pointer-to-map without panicking on nil. Labels
// are `*map[string]string` on every gen ObjectMeta.
func label(lbls *map[string]string, key string) string {
	if lbls == nil {
		return ""
	}
	return (*lbls)[key]
}

// latestConditionReason returns the Reason of the last entry in conds, or "".
// Reads against the gen Condition shape (Type/Status/Reason/Message/…).
func latestConditionReason(conds *[]gen.Condition) string {
	if conds == nil || len(*conds) == 0 {
		return ""
	}
	c := *conds
	return c[len(c)-1].Reason
}
