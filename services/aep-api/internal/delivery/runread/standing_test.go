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

package runread

import (
	"fmt"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// The snapshot carries the version's reading of the attempt (B4): its scope,
// its baseline, and each failure's standing by key.
func TestWithStanding(t *testing.T) {
	out := &gen.ValidationSnapshot{}
	withStanding(out, ValidationStanding{
		Scoped: true, Features: []string{"F2"}, BaselineVersion: "v2", BaselineCommit: "abc",
		Regressions: []string{"F2 / Queue / The queue"}, StillFailing: []string{"F2 / Deputy / Deputy approves"},
	})
	got := fmt.Sprintln(out.Scope.Features, out.Scope.HeldBack, out.Baseline.Version, out.Regressions, out.StillFailing)
	if want := "[F2] [] v2 [F2 / Queue / The queue] [F2 / Deputy / Deputy approves]\n"; got != want {
		t.Errorf("standing = %s, want %s", got, want)
	}
	unscoped := &gen.ValidationSnapshot{}
	withStanding(unscoped, ValidationStanding{})
	if unscoped.Scope != nil || unscoped.Baseline != nil {
		t.Errorf("an unscoped attempt with no baseline = %+v, want neither", unscoped)
	}
}
