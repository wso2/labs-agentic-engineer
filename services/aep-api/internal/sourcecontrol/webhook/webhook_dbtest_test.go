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

// DBTEST tier (skips under -short; the DB lane runs it): the SQL-shaped webhook
// behavior against a pristine per-test Postgres (dbtest.New): the
// sourcecontrol.DeliveryStore dedup — the PK-on-delivery_id "at-most-once"
// guarantee that is inherently SQL-shaped (a unique-violation on the second
// INSERT is what distinguishes a fresh delivery from a replay). Cannot be
// faked.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ============================================================================
// sourcecontrol.DeliveryStore — PK-on-delivery_id dedup
// ============================================================================

func TestDeliveryStore_Persist_FirstDeliveryIsCreated(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	res, err := store.Persist(ctx, "delivery-1", "org-acme", "push", "", []byte(`{"ref":"refs/heads/main"}`), testLease)
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if !res.Created || res.AlreadyProcessed {
		t.Fatalf("first delivery must be Created (not AlreadyProcessed), got %+v", res)
	}

	// The dedup row AND the split payload row are both written.
	var deliv sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "delivery-1").First(&deliv).Error; err != nil {
		t.Fatalf("delivery row must exist: %v", err)
	}
	if deliv.OcOrgID != "org-acme" || deliv.Event != "push" || deliv.ProcessedAt != nil {
		t.Fatalf("delivery row shape wrong: %+v", deliv)
	}
	var payload sourcecontrol.WebhookPayload
	if err := db.Where("delivery_id = ?", "delivery-1").First(&payload).Error; err != nil {
		t.Fatalf("payload row must exist alongside the delivery: %v", err)
	}
	// jsonb canonicalizes formatting on write, so compare JSON semantics, not
	// bytes (the model documents "Handlers re-parse on read").
	var got, want map[string]any
	if err := json.Unmarshal(payload.Payload, &got); err != nil {
		t.Fatalf("persisted payload is not valid JSON: %v (%s)", err, payload.Payload)
	}
	if err := json.Unmarshal([]byte(`{"ref":"refs/heads/main"}`), &want); err != nil {
		t.Fatalf("unmarshal expectation: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload not persisted (JSON-equal): got %s", payload.Payload)
	}
}

// testLease is the lease every store test claims with. Its value is arbitrary;
// what the tests pin is the behaviour either side of its expiry.
const testLease = 3 * time.Minute

// testClock is a settable clock for the store, so lease expiry and backoff are
// driven through the store's own interface rather than by rewriting rows.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{now: time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestDeliveryStore_Persist_FreshDeliveryIsClaimed(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store := sourcecontrol.NewDeliveryStore(db)

	res, err := store.Persist(context.Background(), "claim-1", "org-acme", "push", "", []byte(`{}`), testLease)
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	// The receiver dispatches only what it holds the lease on, so a fresh row
	// must arrive already claimed, as its first attempt.
	if !res.Created || !res.Claimed || res.Attempts != 1 {
		t.Fatalf("a fresh delivery must be Created and Claimed as attempt 1, got %+v", res)
	}
}

func TestDeliveryStore_Persist_DuplicateWhileLeasedIsInFlight(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	if _, err := store.Persist(ctx, "dup-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("first Persist: %v", err)
	}
	// Same delivery id while the first attempt still holds its lease: a manual
	// GitHub redelivery landing mid-handler. It must not run the handler twice.
	clock.Advance(testLease - time.Second)
	res, err := store.Persist(ctx, "dup-1", "org-acme", "push", "", []byte(`{}`), testLease)
	if err != nil {
		t.Fatalf("duplicate Persist must not error, got %v", err)
	}
	if res.Created || res.AlreadyProcessed || res.Claimed {
		t.Fatalf("a duplicate of an in-flight delivery must be neither Created, AlreadyProcessed nor Claimed, got %+v", res)
	}
}

func TestDeliveryStore_Persist_DuplicateAfterLeaseExpiryReClaims(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	if _, err := store.Persist(ctx, "dup-2", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("first Persist: %v", err)
	}
	// The first attempt's holder died (pod restart) and its lease ran out: the
	// duplicate takes the delivery over as the second attempt.
	clock.Advance(testLease + time.Second)
	res, err := store.Persist(ctx, "dup-2", "org-acme", "push", "", []byte(`{}`), testLease)
	if err != nil {
		t.Fatalf("duplicate Persist: %v", err)
	}
	if !res.Claimed || res.Attempts != 2 || res.AlreadyProcessed {
		t.Fatalf("a duplicate after lease expiry must be Claimed as attempt 2, got %+v", res)
	}
}

func TestDeliveryStore_MarkFailed_HoldsTheDeliveryForItsBackoff(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	if _, err := store.Persist(ctx, "back-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "back-1", 1, "boom", time.Minute); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	// Inside the backoff nobody may claim it...
	clock.Advance(30 * time.Second)
	if res, err := store.Persist(ctx, "back-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil || res.Claimed {
		t.Fatalf("a delivery inside its backoff must not be claimable, got %+v, %v", res, err)
	}
	// ...and once it has passed, the next attempt may.
	clock.Advance(time.Minute)
	if res, err := store.Persist(ctx, "back-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil || !res.Claimed {
		t.Fatalf("a delivery past its backoff must be claimable, got %+v, %v", res, err)
	}
}

// replayQuery is the replay the store tests claim with.
var replayQuery = sourcecontrol.ReplayQuery{
	Window:      15 * time.Minute,
	MaxAttempts: 5,
	Lease:       testLease,
	Limit:       3,
}

// failedDelivery persists a delivery and fails its first attempt so it is
// immediately due for replay.
func failedDelivery(t *testing.T, store *sourcecontrol.DeliveryStore, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.Persist(ctx, id, "org-acme", "pull_request", "closed",
		[]byte(`{"n":"`+id+`"}`), testLease); err != nil {
		t.Fatalf("Persist %s: %v", id, err)
	}
	if err := store.MarkFailed(ctx, id, 1, "boom", 0); err != nil {
		t.Fatalf("MarkFailed %s: %v", id, err)
	}
}

func TestDeliveryStore_ClaimReplayable_ConcurrentReplaysClaimEachDeliveryOnce(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)
	const deliveries = 12
	for i := 0; i < deliveries; i++ {
		failedDelivery(t, store, fmt.Sprintf("replay-%02d", i))
	}

	// Several replayers (goroutines here, replicas in production) drain the
	// ledger at once. Each delivery must be handed to exactly one of them.
	var (
		mu     sync.Mutex
		claims = map[string]int{}
		wg     sync.WaitGroup
	)
	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				got, err := store.ClaimReplayable(context.Background(), replayQuery)
				if err != nil {
					t.Errorf("ClaimReplayable: %v", err)
					return
				}
				if len(got) == 0 {
					return
				}
				mu.Lock()
				for _, d := range got {
					claims[d.DeliveryID]++
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(claims) != deliveries {
		t.Fatalf("every due delivery must be claimed once, got %d distinct of %d: %v", len(claims), deliveries, claims)
	}
	for id, n := range claims {
		if n != 1 {
			t.Fatalf("delivery %s claimed %d times; a replay must never run twice concurrently", id, n)
		}
	}
}

func TestDeliveryStore_ClaimReplayable_ReturnsThePayloadAsTheNextAttempt(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)
	failedDelivery(t, store, "replay-payload")

	got, err := store.ClaimReplayable(context.Background(), replayQuery)
	if err != nil {
		t.Fatalf("ClaimReplayable: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want the one due delivery, got %+v", got)
	}
	d := got[0]
	if d.OcOrgID != "org-acme" || d.Event != "pull_request" || d.Action != "closed" || d.Attempts != 2 {
		t.Fatalf("claim must carry the delivery's routing facts as attempt 2, got %+v", d)
	}
	var body map[string]string
	if err := json.Unmarshal(d.Payload, &body); err != nil || body["n"] != "replay-payload" {
		t.Fatalf("claim must carry the stored payload, got %s (%v)", d.Payload, err)
	}
}

func TestDeliveryStore_ClaimReplayable_SkipsWhatIsNotDue(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	// Outside the window: received 20 minutes ago and failed.
	failedDelivery(t, store, "too-old")
	clock.Advance(20 * time.Minute)
	// Processed: finished work is never replayed.
	if _, err := store.Persist(ctx, "processed", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkProcessed(ctx, "processed", 1); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	// In flight: the receiver's own attempt still holds the lease.
	if _, err := store.Persist(ctx, "in-flight", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	// Backing off: failed, next attempt a minute from now.
	if _, err := store.Persist(ctx, "backing-off", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "backing-off", 1, "boom", time.Minute); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	// Exhausted: failed on every one of its attempts.
	failedDelivery(t, store, "exhausted")
	for i := 1; i < replayQuery.MaxAttempts; i++ {
		got, err := store.ClaimReplayable(ctx, replayQuery)
		if err != nil || len(got) != 1 || got[0].DeliveryID != "exhausted" {
			t.Fatalf("attempt %d of the exhausted delivery must be claimable, got %+v, %v", i+1, got, err)
		}
		if err := store.MarkFailed(ctx, "exhausted", got[0].Attempts, "boom", 0); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}
	}

	got, err := store.ClaimReplayable(ctx, replayQuery)
	if err != nil {
		t.Fatalf("ClaimReplayable: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("nothing is due (old, processed, in flight, backing off, exhausted), got %+v", got)
	}
}

// An attempt settles only the claim it holds. Once its lease lapses and another
// attempt takes the delivery over, the old holder's outcome is refused rather
// than written over the new holder's: otherwise its backoff would release a
// lease the new holder still needs, and a third run could start.
func TestDeliveryStore_StaleHolderCannotSettleATakenOverDelivery(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	if _, err := store.Persist(ctx, "fence-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	clock.Advance(testLease + time.Second)
	res, err := store.Persist(ctx, "fence-1", "org-acme", "push", "", []byte(`{}`), testLease)
	if err != nil || !res.Claimed || res.Attempts != 2 {
		t.Fatalf("attempt 2 must take the lapsed delivery over, got %+v, %v", res, err)
	}

	if err := store.MarkFailed(ctx, "fence-1", 1, "late failure", 0); !errors.Is(err, sourcecontrol.ErrDeliveryLeaseLost) {
		t.Fatalf("attempt 1 failing after a takeover = %v, want ErrDeliveryLeaseLost", err)
	}
	if err := store.MarkProcessed(ctx, "fence-1", 1); !errors.Is(err, sourcecontrol.ErrDeliveryLeaseLost) {
		t.Fatalf("attempt 1 finishing after a takeover = %v, want ErrDeliveryLeaseLost", err)
	}
	// Attempt 2 still holds its lease: nobody else may claim the delivery.
	if res, err := store.Persist(ctx, "fence-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil || res.Claimed {
		t.Fatalf("the stale holder must not have released attempt 2's lease, got %+v, %v", res, err)
	}
	if err := store.MarkProcessed(ctx, "fence-1", 2); err != nil {
		t.Fatalf("the current holder settles its own attempt: %v", err)
	}
}

// A delivery that leaves the replay window unprocessed is given up on, and that
// is recorded once: nothing replays it again, and the next pass reports nothing.
func TestDeliveryStore_AbandonExpired_RecordsEachAgedOutDeliveryOnce(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	clock := newTestClock()
	store := sourcecontrol.NewDeliveryStore(db).WithClock(clock.Now)

	failedDelivery(t, store, "aged-out")
	if _, err := store.Persist(ctx, "done", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkProcessed(ctx, "done", 1); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	clock.Advance(replayQuery.Window + time.Minute)
	failedDelivery(t, store, "still-in-window")

	got, err := store.AbandonExpired(ctx, replayQuery.Window)
	if err != nil {
		t.Fatalf("AbandonExpired: %v", err)
	}
	if len(got) != 1 || got[0].DeliveryID != "aged-out" {
		t.Fatalf("only the unprocessed delivery past the window is abandoned, got %+v", got)
	}
	if again, err := store.AbandonExpired(ctx, replayQuery.Window); err != nil || len(again) != 0 {
		t.Fatalf("an abandonment is recorded once, got %+v, %v", again, err)
	}
	var row sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "aged-out").First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.AbandonedAt == nil || row.ProcessedAt != nil || row.ProcessError == "" {
		t.Fatalf("an abandoned delivery keeps its error and stays unprocessed, stamped abandoned: %+v", row)
	}
}

func TestDeliveryStore_Persist_DuplicateAfterProcessingIsDeduped(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "done-1", "org-acme", "pull_request", "closed", []byte(`{}`), testLease); err != nil {
		t.Fatalf("first Persist: %v", err)
	}
	if err := store.MarkProcessed(ctx, "done-1", 1); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	// Replay of finished work: the existing row has processed_at set → dedup.
	// This is the assertion that catches a broken PK-dedup (a mutation that lets
	// the second INSERT succeed would re-run the handler → double-process).
	res, err := store.Persist(ctx, "done-1", "org-acme", "pull_request", "closed", []byte(`{}`), testLease)
	if err != nil {
		t.Fatalf("replay Persist: %v", err)
	}
	if !res.AlreadyProcessed {
		t.Fatalf("a replay of processed work must be AlreadyProcessed, got %+v", res)
	}
}

func TestDeliveryStore_MarkProcessed_ClearsErrorAndStampsTime(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "mp-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "mp-1", 1, "boom", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := store.MarkProcessed(ctx, "mp-1", 1); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	var row sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "mp-1").First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.ProcessedAt == nil {
		t.Fatal("MarkProcessed must stamp processed_at")
	}
	if row.ProcessError != "" {
		t.Fatalf("MarkProcessed must clear a prior process_error, got %q", row.ProcessError)
	}
}

func TestDeliveryStore_MarkFailed_RecordsErrorLeavesUnprocessed(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "mf-1", "org-acme", "push", "", []byte(`{}`), testLease); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "mf-1", 1, "image push denied", 0); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	var row sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "mf-1").First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	// process_error recorded for audit; processed_at stays NULL so a GitHub
	// redelivery re-enters the handler.
	if row.ProcessError != "image push denied" {
		t.Fatalf("process_error = %q; want the recorded message", row.ProcessError)
	}
	if row.ProcessedAt != nil {
		t.Fatal("a failed delivery must remain unprocessed (processed_at NULL)")
	}
}

func TestDeliveryStore_Persist_EmptyDeliveryIDRejected(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "", "org-acme", "push", "", []byte(`{}`), testLease); err == nil {
		t.Fatal("an empty delivery id must be rejected (it is the dedup PK)")
	}
}
