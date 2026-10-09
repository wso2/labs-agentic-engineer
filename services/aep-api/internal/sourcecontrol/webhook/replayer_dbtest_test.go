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

package webhook

// REPLAYER tier (DB lane): the real Replayer over a real DeliveryStore on a
// per-test Postgres and the real Router, with router_test.go's recordingHandler
// as the only handler. What is pinned is the replay contract: an unprocessed
// delivery is run again exactly once per due attempt, a failed replay backs
// off, and the attempt cap ends it.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

type replayHarness struct {
	db       *gorm.DB
	store    *sourcecontrol.DeliveryStore
	handler  *recordingHandler
	replayer *Replayer
	clock    *testClock
}

func newReplayHarness(t *testing.T) *replayHarness {
	t.Helper()
	clock := newTestClock()
	db := dbtest.New(t)
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)
	handler := &recordingHandler{}
	router := NewRouter()
	router.Register("pull_request", "", handler)
	return &replayHarness{
		db:       db,
		store:    store,
		handler:  handler,
		replayer: NewReplayer(store, router, 0),
		clock:    clock,
	}
}

// receivedAndFailed is a delivery whose receiver attempt failed: it is
// persisted, and its first run recorded a failure with the given backoff.
func (h *replayHarness) receivedAndFailed(t *testing.T, id string, backoff time.Duration) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.store.Persist(ctx, id, "org-acme", "pull_request", "closed",
		[]byte(`{"action":"closed"}`), deliveryLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := h.store.MarkFailed(ctx, id, 1, "context canceled", backoff); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
}

func (h *replayHarness) row(t *testing.T, id string) sourcecontrol.WebhookDelivery {
	t.Helper()
	var rows []sourcecontrol.WebhookDelivery
	if err := h.db.Where("delivery_id = ?", id).Find(&rows).Error; err != nil || len(rows) != 1 {
		t.Fatalf("load delivery %s: %v (%d rows)", id, err, len(rows))
	}
	return rows[0]
}

func TestReplayer_ConcurrentPassesRunAnUnprocessedDeliveryOnce(t *testing.T) {
	t.Parallel()
	h := newReplayHarness(t)
	h.receivedAndFailed(t, "lost-fanout", 0)

	// Passes racing each other, as on two replicas ticking together.
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := h.replayer.Once(context.Background()); err != nil {
				t.Errorf("Once: %v", err)
			}
		}()
	}
	wg.Wait()

	if h.handler.count() != 1 {
		t.Fatalf("the unprocessed delivery must be replayed exactly once, got %d runs", h.handler.count())
	}
	if got := h.handler.call(0); got.event != "pull_request" || got.action != "closed" {
		t.Fatalf("replay must dispatch the stored delivery, got %+v", got)
	}
	if row := h.row(t, "lost-fanout"); row.ProcessedAt == nil || row.ProcessError != "" {
		t.Fatalf("a successful replay must mark the delivery processed, got %+v", row)
	}
	// And a processed delivery is not replayed again.
	h.clock.Advance(time.Hour)
	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if h.handler.count() != 1 {
		t.Fatalf("a processed delivery must never be replayed, got %d runs", h.handler.count())
	}
}

func TestReplayer_WaitsOutTheBackoff(t *testing.T) {
	t.Parallel()
	h := newReplayHarness(t)
	h.receivedAndFailed(t, "backing-off", deliveryBackoff(1))

	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if h.handler.count() != 0 {
		t.Fatalf("a delivery inside its backoff must not be replayed, got %d runs", h.handler.count())
	}
	h.clock.Advance(deliveryBackoff(1) + time.Second)
	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if h.handler.count() != 1 {
		t.Fatalf("a delivery past its backoff must be replayed, got %d runs", h.handler.count())
	}
}

// A replay runs long after the delivery arrived; its handlers still see the
// original receipt, the event time of last resort for a payload without one.
func TestReplayer_HandlersSeeTheOriginalReceipt(t *testing.T) {
	t.Parallel()
	h := newReplayHarness(t)
	received := h.clock.Now()
	h.receivedAndFailed(t, "late-replay", 0)
	h.clock.Advance(10 * time.Minute)

	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if h.handler.count() != 1 {
		t.Fatalf("want one replay run, got %d", h.handler.count())
	}
	if got := h.handler.call(0).receivedAt; !got.Equal(received) {
		t.Fatalf("handler ReceivedAt = %v, want the original receipt %v", got, received)
	}
}

func TestReplayer_StopsAtTheAttemptCap(t *testing.T) {
	t.Parallel()
	h := newReplayHarness(t)
	h.handler.setErr(errors.New("github 502"))
	h.receivedAndFailed(t, "always-failing", deliveryBackoff(1))

	// Each due pass runs the delivery once more, then it backs off again.
	for attempt := 2; attempt <= maxDeliveryAttempts; attempt++ {
		h.clock.Advance(deliveryBackoff(attempt-1) + time.Second)
		if err := h.replayer.Once(context.Background()); err != nil {
			t.Fatalf("Once: %v", err)
		}
		if got := h.handler.count(); got != attempt-1 {
			t.Fatalf("after attempt %d the replay must have run %d times, got %d", attempt, attempt-1, got)
		}
	}
	// The receiver's run plus every replay is the whole allowance.
	h.clock.Advance(time.Minute)
	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if got := h.handler.count(); got != maxDeliveryAttempts-1 {
		t.Fatalf("a delivery that used its %d attempts must not run again, got %d replays", maxDeliveryAttempts, got)
	}
	row := h.row(t, "always-failing")
	if row.ProcessedAt != nil || row.Attempts != maxDeliveryAttempts || row.ProcessError == "" || row.AbandonedAt == nil {
		t.Fatalf("a delivery that failed its last attempt is stamped abandoned and keeps its attempts and last error, got %+v", row)
	}
}

func TestReplayer_LeavesDeliveriesOlderThanTheWindow(t *testing.T) {
	t.Parallel()
	h := newReplayHarness(t)
	h.receivedAndFailed(t, "from-yesterday", 0)
	h.clock.Advance(replayWindow + time.Minute)

	if err := h.replayer.Once(context.Background()); err != nil {
		t.Fatalf("Once: %v", err)
	}
	if h.handler.count() != 0 {
		t.Fatalf("a delivery older than the replay window is the reconcile's, not the replay's, got %d runs", h.handler.count())
	}
	// And it is recorded as given up on, not left looking like work in progress.
	if row := h.row(t, "from-yesterday"); row.AbandonedAt == nil {
		t.Fatalf("a delivery that aged out of the window must be stamped abandoned, got %+v", row)
	}
}

// clockHandler advances the shared test clock by `advance` on its first call and
// runs `then`, so a test can stand inside a long handler run and look around.
type clockHandler struct {
	mu      sync.Mutex
	calls   []string
	clock   *testClock
	advance time.Duration
	then    func()
}

func (h *clockHandler) Handle(_ context.Context, _, _ string, payload []byte) error {
	h.mu.Lock()
	h.calls = append(h.calls, string(payload))
	first := len(h.calls) == 1
	h.mu.Unlock()
	if first {
		h.clock.Advance(h.advance)
		if h.then != nil {
			h.then()
		}
	}
	return nil
}

// A delivery is claimed when its run starts, never while it waits behind
// another one. Claiming a whole batch up front let the later rows' leases lapse
// in the queue, so a duplicate delivery or a second replica could take one over
// and run it at the same moment this pass did.
func TestReplayer_ClaimsADeliveryOnlyWhenItsRunStarts(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	db := dbtest.New(t)
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		if _, err := store.Persist(ctx, id, "org-acme", "pull_request", "closed",
			[]byte(`{"action":"closed","id":"`+id+`"}`), deliveryLease); err != nil {
			t.Fatalf("Persist: %v", err)
		}
		if err := store.MarkFailed(ctx, id, 1, "boom", 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
		clock.Advance(time.Second) // receipt order: first, then second
	}

	var duplicate sourcecontrol.PersistResult
	handler := &clockHandler{clock: clock, advance: deliveryLease + time.Minute}
	handler.then = func() {
		// Mid-way through the first run, past any lease the pass could have
		// taken at its start, the second delivery is redelivered by hand.
		res, err := store.Persist(ctx, "second", "org-acme", "pull_request", "closed", []byte(`{}`), deliveryLease)
		if err != nil {
			t.Errorf("duplicate Persist: %v", err)
		}
		duplicate = res
	}
	router := NewRouter()
	router.Register("pull_request", "", handler)
	if err := NewReplayer(store, router, 0).Once(ctx); err != nil {
		t.Fatalf("Once: %v", err)
	}

	if !duplicate.Claimed {
		t.Fatalf("the waiting delivery must still be free for the duplicate to claim, got %+v", duplicate)
	}
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.calls) != 1 {
		t.Fatalf("the pass must run only what it claimed; the duplicate owns the second delivery now, got runs %v", handler.calls)
	}
}
