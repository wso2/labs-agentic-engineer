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

package codingagent

import (
	"context"
	"errors"
	"testing"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// fakeLatestCycle answers with one cycle (or none).
type fakeLatestCycle struct {
	row *delivery.RunCycle
	err error
}

func (f fakeLatestCycle) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	return f.row, f.err
}

// deleteRecorder captures the component deletes.
type deleteRecorder struct {
	args []string
	err  error
}

func (d *deleteRecorder) client() *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		DeleteComponentFunc: func(_ context.Context, orgName, projectName, componentName string) error {
			d.args = append(d.args, orgName+"/"+projectName+"/"+componentName)
			return d.err
		},
	}
}

// TestReapRunCycle_DeletesTheLiveCyclesComponent is the cancel contract: the
// button already stops the RUN, and this is what actually stops the POD — and
// frees the billing slot, which only an API-path delete does.
func TestReapRunCycle_DeletesTheLiveCyclesComponent(t *testing.T) {
	rec := &deleteRecorder{}
	r := NewCycleReaper(rec.client(), fakeLatestCycle{row: &delivery.RunCycle{
		ID: "cycle-1", OrgID: "acme", ProjectID: "widgets", RunID: "run-1",
		JobRef: "ca-11111111-2608061200",
	}})

	if err := r.ReapRunCycle(context.Background(), "acme", "widgets", "run-1"); err != nil {
		t.Fatalf("ReapRunCycle: %v", err)
	}
	if len(rec.args) != 1 || rec.args[0] != "acme/widgets/ca-11111111-2608061200" {
		t.Errorf("deletes = %v, want the cycle's component once", rec.args)
	}
}

// TestReapRunCycle_NothingToReapIsNotAnError covers the three benign cases: a
// run that never dispatched, a cycle with no Job, and a legacy ref that is not
// a Component at all.
func TestReapRunCycle_NothingToReapIsNotAnError(t *testing.T) {
	for name, cycles := range map[string]fakeLatestCycle{
		"never dispatched": {row: nil},
		"no job ref":       {row: &delivery.RunCycle{ID: "cycle-1", RunID: "run-1"}},
		"not a component":  {row: &delivery.RunCycle{ID: "cycle-1", RunID: "run-1", JobRef: "widgets-api-1754400000000"}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &deleteRecorder{}
			r := NewCycleReaper(rec.client(), cycles)
			if err := r.ReapRunCycle(context.Background(), "acme", "widgets", "run-1"); err != nil {
				t.Fatalf("ReapRunCycle: %v", err)
			}
			if len(rec.args) != 0 {
				t.Errorf("deleted %v, want nothing", rec.args)
			}
		})
	}
}

// TestReapRunCycle_DeleteFailureIsReported: the caller decides what a failed
// reap means for the user's request, so it must be told.
func TestReapRunCycle_DeleteFailureIsReported(t *testing.T) {
	boom := errors.New("oc down")
	rec := &deleteRecorder{err: boom}
	r := NewCycleReaper(rec.client(), fakeLatestCycle{row: &delivery.RunCycle{
		ID: "cycle-1", RunID: "run-1", JobRef: "ca-1",
	}})

	if err := r.ReapRunCycle(context.Background(), "acme", "widgets", "run-1"); !errors.Is(err, boom) {
		t.Errorf("ReapRunCycle = %v, want the delete failure", err)
	}
}
