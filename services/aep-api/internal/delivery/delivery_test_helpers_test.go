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

package delivery_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// appendCycle inserts one dispatched cycle under a run and returns it.
func appendCycle(t *testing.T, cycles delivery.RunCycleRepository, run *delivery.MilestoneRun, kind, jobRef string) *delivery.RunCycle {
	t.Helper()
	ctx := context.Background()
	row := &delivery.RunCycle{OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: kind}
	if err := cycles.Append(ctx, row); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if jobRef != "" {
		if _, err := cycles.NoteDispatch(ctx, row.ID, jobRef); err != nil {
			t.Fatalf("NoteDispatch: %v", err)
		}
		row.JobRef = jobRef
	}
	return row
}
