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
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// TestListOpenCycleIDs_OnlyTheProjectsUnendedCycles is the retention reaper's
// safety read: an ended cycle is reapable, an open one is an agent mid-run, and
// another project's cycles are none of this project's business.
func TestListOpenCycleIDs_OnlyTheProjectsUnendedCycles(t *testing.T) {
	db := dbtest.New(t)
	repo := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	mk := func(id, project string, ended bool) {
		t.Helper()
		c := &delivery.RunCycle{
			ID: id, OrgID: "acme", ProjectID: project, RunID: "run-" + project,
			Kind: delivery.CycleKindCoding,
		}
		if err := repo.Append(ctx, c); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
		if ended {
			if _, err := repo.Finish(ctx, id, "sha-"+id); err != nil {
				t.Fatalf("finish %s: %v", id, err)
			}
		}
	}
	mk("11111111-1111-1111-1111-111111111111", "widgets", false)
	mk("22222222-2222-2222-2222-222222222222", "widgets", true)
	mk("33333333-3333-3333-3333-333333333333", "gadgets", false)

	got, err := repo.ListOpenCycleIDs(ctx, "acme", "widgets")
	if err != nil {
		t.Fatalf("ListOpenCycleIDs: %v", err)
	}
	if len(got) != 1 || got[0] != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("ListOpenCycleIDs = %v, want only the project's unended cycle", got)
	}

	// The org fence: another org sees nothing.
	other, err := repo.ListOpenCycleIDs(ctx, "evil", "widgets")
	if err != nil {
		t.Fatalf("ListOpenCycleIDs(other org): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("cross-org read returned %v, want nothing", other)
	}
}
