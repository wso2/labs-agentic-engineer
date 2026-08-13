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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
)

// fakeOpenCycles is the DB's answer to "which cycles are still live".
type fakeOpenCycles struct {
	ids []string
	err error
}

func (f fakeOpenCycles) ListOpenCycleIDs(context.Context, string, string) ([]string, error) {
	return f.ids, f.err
}

// reapHarness records the deletes so their ORDER can be asserted: the reap is
// an LRU, and deleting the newest first would evict a component a user may
// still be reading.
type reapHarness struct {
	mu      sync.Mutex
	deleted []string
	listErr error
	delErr  error
	comps   []openchoreo.InternalComponent
}

func (h *reapHarness) client() *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		ListInternalComponentsFunc: func(context.Context, string, string) ([]openchoreo.InternalComponent, error) {
			if h.listErr != nil {
				return nil, h.listErr
			}
			return h.comps, nil
		},
		DeleteComponentFunc: func(_ context.Context, _, _, componentName string) error {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.deleted = append(h.deleted, componentName)
			return h.delErr
		},
	}
}

func (h *reapHarness) deletes() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.deleted...)
}

// agentComponents makes n coding-agent components, oldest first, named ca-<i>
// with cycle id cycle-<i>.
func agentComponents(n int) []openchoreo.InternalComponent {
	base := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	out := make([]openchoreo.InternalComponent, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, openchoreo.InternalComponent{
			Name:      fmt.Sprintf("ca-%d", i),
			RunName:   fmt.Sprintf("ca-%d", i),
			TypeName:  openchoreo.CodingAgentComponentTypeRef,
			CycleID:   fmt.Sprintf("cycle-%d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	return out
}

// TestReapBeforeCreate_UnderTheLimitDeletesNothing — the common case must cost
// one list and no deletes.
func TestReapBeforeCreate_UnderTheLimitDeletesNothing(t *testing.T) {
	h := &reapHarness{comps: agentComponents(3)}
	r := NewComponentRetention(h.client(), fakeOpenCycles{}, DefaultCodingAgentComponentRetention)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); err != nil {
		t.Fatalf("ReapBeforeCreate: %v", err)
	}
	if got := h.deletes(); len(got) != 0 {
		t.Errorf("deleted %v, want nothing", got)
	}
}

// TestReapBeforeCreate_EvictsTheOldestRetiredUntilTheCreateFits is the rule: a
// FINISHED component still holds a billing slot, so the reap must leave room
// for one more — hence len-limit+1 deletions, oldest first.
func TestReapBeforeCreate_EvictsTheOldestRetiredUntilTheCreateFits(t *testing.T) {
	h := &reapHarness{comps: agentComponents(12)}
	r := NewComponentRetention(h.client(), fakeOpenCycles{}, 10)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); err != nil {
		t.Fatalf("ReapBeforeCreate: %v", err)
	}
	want := []string{"ca-1", "ca-2", "ca-3"}
	if fmt.Sprint(h.deletes()) != fmt.Sprint(want) {
		t.Errorf("deleted %v, want %v (oldest first, leaving room for the create)", h.deletes(), want)
	}
}

// TestReapBeforeCreate_NeverDeletesALiveCycle is the correctness bound: a
// component whose cycle is still open is an agent MID-RUN, and deleting it
// would kill work the platform is waiting on.
func TestReapBeforeCreate_NeverDeletesALiveCycle(t *testing.T) {
	h := &reapHarness{comps: agentComponents(12)}
	// The two oldest are still running.
	r := NewComponentRetention(h.client(), fakeOpenCycles{ids: []string{"cycle-1", "cycle-2"}}, 10)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); err != nil {
		t.Fatalf("ReapBeforeCreate: %v", err)
	}
	want := []string{"ca-3", "ca-4", "ca-5"}
	if fmt.Sprint(h.deletes()) != fmt.Sprint(want) {
		t.Errorf("deleted %v, want %v (live cycles skipped)", h.deletes(), want)
	}
}

// TestReapBeforeCreate_AllLiveDeletesNothing: an org whose slots are all in
// flight cannot be helped by a reap. It must not delete anything — the create's
// 402 is the correct, actionable answer.
func TestReapBeforeCreate_AllLiveDeletesNothing(t *testing.T) {
	comps := agentComponents(11)
	live := make([]string, 0, len(comps))
	for _, c := range comps {
		live = append(live, c.CycleID)
	}
	h := &reapHarness{comps: comps}
	r := NewComponentRetention(h.client(), fakeOpenCycles{ids: live}, 10)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); err != nil {
		t.Fatalf("an unreapable org is not an error: %v", err)
	}
	if got := h.deletes(); len(got) != 0 {
		t.Errorf("deleted %v, want nothing", got)
	}
}

// TestReapBeforeCreate_IgnoresOtherInternalComponentTypes: the reaper owns the
// coding-agent type and nothing else. Another internal component that happens
// to carry the marker must be invisible to it.
func TestReapBeforeCreate_IgnoresOtherInternalComponentTypes(t *testing.T) {
	comps := agentComponents(11)
	comps[0].TypeName = "deployment/some-other-internal-thing"
	h := &reapHarness{comps: comps}
	r := NewComponentRetention(h.client(), fakeOpenCycles{}, 10)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); err != nil {
		t.Fatalf("ReapBeforeCreate: %v", err)
	}
	if got := h.deletes(); len(got) != 1 || got[0] != "ca-2" {
		t.Errorf("deleted %v, want [ca-2] — the foreign type is neither counted nor deleted", got)
	}
}

// TestReapBeforeCreate_ListFailureIsReported keeps the reap honest: the caller
// logs and continues, but it must know the reap did not happen.
func TestReapBeforeCreate_ListFailureIsReported(t *testing.T) {
	boom := errors.New("oc down")
	h := &reapHarness{listErr: boom}
	r := NewComponentRetention(h.client(), fakeOpenCycles{}, 10)

	if err := r.ReapBeforeCreate(context.Background(), "acme", "widgets"); !errors.Is(err, boom) {
		t.Errorf("ReapBeforeCreate = %v, want the list failure", err)
	}
}
