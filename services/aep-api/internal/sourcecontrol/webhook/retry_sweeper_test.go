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

// RetrySweeper over the real DeliveryStore and the real receiver, because the
// thing under test is the handover between them: the receiver records a failed
// dispatch as a due retry, and the sweep picks exactly those rows up.
package webhook

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// dueNow backdates a row's schedule so a pass claims it without the test
// waiting out the real backoff.
func dueNow(t *testing.T, h *receiverHarness, deliveryID string) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Minute)
	if err := h.db.Model(&sourcecontrol.WebhookDelivery{}).
		Where("delivery_id = ?", deliveryID).
		Update("next_attempt_at", &past).Error; err != nil {
		t.Fatalf("backdate next_attempt_at: %v", err)
	}
}

func TestRetrySweep_ReDispatchesAFailedDeliveryAndMarksItProcessed(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sweeper := NewRetrySweeper(h.ctrl, time.Minute, 10)

	h.handler.err = fmt.Errorf("openchoreo unreachable")
	h.post(t, "delivery-sweep", "push", sign(receiverSecret, pushBody), pushBody)
	if len(h.handler.calls) != 1 {
		t.Fatalf("the receiver's own pass must dispatch once, got %d", len(h.handler.calls))
	}

	// Nothing is due yet: the first failure schedules a backoff, and a pass
	// before it elapses must not re-dispatch.
	sweeper.Sweep(context.Background())
	if len(h.handler.calls) != 1 {
		t.Fatalf("a retry before its backoff elapsed must not dispatch, got %d calls", len(h.handler.calls))
	}

	dueNow(t, h, "delivery-sweep")
	h.handler.err = nil
	sweeper.Sweep(context.Background())

	if len(h.handler.calls) != 2 {
		t.Fatalf("a due retry must re-dispatch: want 2 calls, got %d", len(h.handler.calls))
	}
	row := h.loadDelivery(t, "delivery-sweep")
	if row.ProcessedAt == nil || row.ProcessError != "" {
		t.Fatalf("a successful retry must mark processed and clear the error, got %+v", row)
	}
	if row.NextAttemptAt != nil {
		t.Fatalf("a processed row must not stay claimable, got next_attempt_at=%v", row.NextAttemptAt)
	}

	// Processed rows are never claimed again, so a later pass is a no-op.
	sweeper.Sweep(context.Background())
	if len(h.handler.calls) != 2 {
		t.Fatalf("a processed delivery must not be re-dispatched, got %d calls", len(h.handler.calls))
	}
}

func TestRetrySweep_StopsClaimingOnceAttemptsAreExhausted(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sweeper := NewRetrySweeper(h.ctrl, time.Minute, 10)

	h.handler.err = fmt.Errorf("permanently poisoned")
	h.post(t, "delivery-exhaust", "push", sign(receiverSecret, pushBody), pushBody)

	// Drive every remaining attempt. Each pass re-fails, increments the count
	// and reschedules, until the budget is spent.
	for i := 1; i < maxDeliveryAttempts; i++ {
		dueNow(t, h, "delivery-exhaust")
		sweeper.Sweep(context.Background())
	}

	row := h.loadDelivery(t, "delivery-exhaust")
	if row.Attempts != maxDeliveryAttempts {
		t.Fatalf("want attempts capped at %d, got %d", maxDeliveryAttempts, row.Attempts)
	}
	if row.NextAttemptAt != nil {
		t.Fatalf("an exhausted delivery must stop being claimable, got next_attempt_at=%v", row.NextAttemptAt)
	}
	if row.ProcessedAt != nil {
		t.Fatal("an exhausted delivery must stay unprocessed so it reads as unfinished work")
	}

	// Backdating cannot resurrect it: ClaimRetryable filters on
	// next_attempt_at IS NOT NULL, which exhaustion cleared.
	before := len(h.handler.calls)
	sweeper.Sweep(context.Background())
	if len(h.handler.calls) != before {
		t.Fatalf("an exhausted delivery must not be re-dispatched, got %d extra calls", len(h.handler.calls)-before)
	}
}

func TestRetrySweep_RefusesToReplayARedactedPayload(t *testing.T) {
	t.Parallel()
	h := newReceiverHarness(t)
	sweeper := NewRetrySweeper(h.ctrl, time.Minute, 10)

	// An issues event whose body carries the published-credentials marker: the
	// receiver stores a REWRITTEN body, so replaying it would dispatch
	// something GitHub never sent. The row is still recorded and still
	// unprocessed — it is recoverable by GitHub redelivery, which carries the
	// true body — but the sweep must leave it alone.
	body := []byte(`{"repository":{"full_name":"acme/web"},"issue":{"body":"creds <!-- aep:test-users -->here"}}`)
	h.handler.err = fmt.Errorf("downstream boom")
	h.post(t, "delivery-redacted", "issues", sign(receiverSecret, body), body)

	row := h.loadDelivery(t, "delivery-redacted")
	if !row.PayloadRedacted {
		t.Fatalf("a rewritten payload must be marked redacted, got %+v", row)
	}

	dueNow(t, h, "delivery-redacted")
	h.handler.err = nil
	before := len(h.handler.calls)
	sweeper.Sweep(context.Background())

	if len(h.handler.calls) != before {
		t.Fatalf("a redacted payload must never be replayed, got %d extra calls", len(h.handler.calls)-before)
	}
	if row := h.loadDelivery(t, "delivery-redacted"); row.ProcessedAt != nil {
		t.Fatal("a delivery the sweep refuses to replay must not be marked processed")
	}
}
