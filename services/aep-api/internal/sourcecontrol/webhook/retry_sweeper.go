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
)

// defaultSweepInterval is how often the sweep looks for due retries. It only
// bounds how late a retry runs, never how soon it is eligible — that is
// next_attempt_at — so it is set well under retryBaseDelay rather than tuned.
const defaultSweepInterval = 15 * time.Second

// defaultSweepBatch caps one pass. A backlog drains over several passes
// instead of dispatching unboundedly in one, since each dispatch can fan out
// into git and OpenChoreo work.
const defaultSweepBatch = 20

// RetrySweeper re-drives webhook deliveries whose dispatch never completed.
//
// It exists because the receiver acks GitHub before the handler runs. That ack
// is what stops a slow merge from being killed at GitHub's 10-second delivery
// deadline, but it also means GitHub's own redelivery no longer recovers a
// failure — GitHub was told the delivery was fine. The unfinished work is the
// delivery row (processed_at null, next_attempt_at set), and this is what
// drives it.
//
// It does NOT replace the event plane's reconcile backstop, which heals
// milestones from observed state. This one replays the event itself, so it
// recovers deliveries whose effects never began.
type RetrySweeper struct {
	ctrl     *webhookController
	interval time.Duration
	batch    int
}

// NewRetrySweeper wires the sweep. interval and batch take their defaults at
// zero, matching the other watchers' constructors.
func NewRetrySweeper(ctrl WebhookController, interval time.Duration, batch int) *RetrySweeper {
	impl, ok := ctrl.(*webhookController)
	if !ok {
		return nil
	}
	if interval <= 0 {
		interval = defaultSweepInterval
	}
	if batch <= 0 {
		batch = defaultSweepBatch
	}
	return &RetrySweeper{ctrl: impl, interval: interval, batch: batch}
}

// Run drives the sweep until ctx is canceled, matching the Watcher convention.
//
// One pass runs at startup: a pod that restarts mid-dispatch leaves rows whose
// in-flight attempt died with the process, and those are exactly the ones worth
// picking up first rather than after a full interval.
func (s *RetrySweeper) Run(ctx context.Context) {
	if s == nil {
		return
	}
	s.Sweep(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Sweep(ctx)
		}
	}
}

// Sweep runs one pass. Exported so a test can drive a pass without the ticker.
func (s *RetrySweeper) Sweep(ctx context.Context) {
	if s == nil {
		return
	}
	due, err := s.ctrl.deliveries.ClaimRetryable(ctx, time.Now().UTC(), s.batch)
	if err != nil {
		slog.ErrorContext(ctx, "webhook retry: claim failed", "error", err)
		return
	}
	for _, d := range due {
		// Stop on cancellation rather than finishing the batch: a shutting-down
		// pod would otherwise start dispatches it cannot complete, and each one
		// would burn an attempt against its budget.
		if ctx.Err() != nil {
			return
		}
		slog.InfoContext(ctx, "webhook retry: re-dispatching",
			"deliveryId", d.DeliveryID, "event", d.Event, "action", d.Action,
			"ocOrgId", d.OcOrgID, "priorAttempts", d.Attempts)
		s.ctrl.finish(ctx, d.DeliveryID, d.OcOrgID, d.Event, d.Action, d.Payload, d.Attempts)
	}
}
