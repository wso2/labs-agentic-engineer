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

import (
	"context"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// defaultReplayInterval is the replay cadence. The first retry backoff is 30s,
// so a faster tick would only find nothing due.
const defaultReplayInterval = 30 * time.Second

// replayBatch bounds the runs one pass makes. Deliveries left over wait for the
// next tick.
const replayBatch = 20

// Replayer re-runs deliveries that were persisted but never processed: a
// handler that failed, or a run lost with its pod. It is what makes a failed
// delivery come back without anyone redelivering it — GitHub never redelivers
// on its own, and before this nothing did.
//
// It claims through the delivery ledger (DeliveryStore.ClaimReplayable), so a
// replay never overlaps the receiver's own run, a duplicate delivery, or
// another replayer on another replica: every run holds the delivery's lease.
// Runs go through the same deliveryRunner the receiver uses, which settles each
// one (processed, or failed and held for deliveryBackoff) and logs the
// abandonment of a delivery whose last attempt failed.
//
// Within a pass deliveries run one at a time, oldest receipt first, and each is
// CLAIMED only as its run starts. Claiming a batch up front would let the later
// rows' leases lapse while they wait in line, and a duplicate delivery or another
// replica could then run one at the same moment. The order is one of
// convenience, not a guarantee the handlers rely on: GitHub itself promises no
// order, and the handlers re-read ground truth instead (see eventcore's
// Idempotency notes).
//
// Each pass first records the deliveries that aged out of replayWindow
// unprocessed as abandoned (DeliveryStore.AbandonExpired), logged once at ERROR,
// so a delivery the replay gives up on never just goes quiet.
type Replayer struct {
	deliveries *sourcecontrol.DeliveryStore
	runner     deliveryRunner
	interval   time.Duration
}

// NewReplayer wires the replay. interval ≤ 0 uses the default.
func NewReplayer(deliveries *sourcecontrol.DeliveryStore, router *Router, interval time.Duration) *Replayer {
	if interval <= 0 {
		interval = defaultReplayInterval
	}
	return &Replayer{
		deliveries: deliveries,
		runner:     deliveryRunner{deliveries: deliveries, router: router},
		interval:   interval,
	}
}

// Run ticks until ctx is cancelled (the app.Watcher shape).
func (r *Replayer) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Once(ctx); err != nil {
				slog.WarnContext(ctx, "webhook: replay pass failed", "error", err)
			}
		}
	}
}

// Once records the deliveries that aged out, then claims and runs the ones due
// now, one at a time, up to replayBatch. Exported so a test can drive a pass.
func (r *Replayer) Once(ctx context.Context) error {
	expired, err := r.deliveries.AbandonExpired(ctx, replayWindow)
	if err != nil {
		return err
	}
	for _, d := range expired {
		slog.ErrorContext(ctx, "webhook: delivery abandoned — it aged out of the replay window unprocessed",
			"deliveryId", d.DeliveryID, "event", d.Event, "action", d.Action, "ocOrgId", d.OcOrgID,
			"attempts", d.Attempts, "result", "abandoned")
	}
	for i := 0; i < replayBatch && ctx.Err() == nil; i++ {
		due, err := r.deliveries.ClaimReplayable(ctx, sourcecontrol.ReplayQuery{
			Window:      replayWindow,
			MaxAttempts: maxDeliveryAttempts,
			Lease:       deliveryLease,
			Limit:       1,
		})
		if err != nil {
			return err
		}
		if len(due) == 0 {
			return nil
		}
		d := due[0]
		slog.InfoContext(ctx, "webhook: replaying an unprocessed delivery",
			"deliveryId", d.DeliveryID, "event", d.Event, "action", d.Action, "ocOrgId", d.OcOrgID,
			"attempt", d.Attempts, "result", "replaying")
		r.runner.run(ctx, deliveryAttempt{
			deliveryID: d.DeliveryID, event: d.Event, action: d.Action, ocOrgID: d.OcOrgID,
			attempt: d.Attempts, receivedAt: d.ReceivedAt, payload: d.Payload, source: "replay",
		})
	}
	return nil
}
