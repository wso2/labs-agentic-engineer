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

package sourcecontrol

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// DeliveryStore persists inbound webhook deliveries with free dedup via the
// PK on delivery_id. Payloads are stored in a sibling table so retention
// can drop the bulk while keeping dedup history.
//
// It is also the delivery LEDGER the handlers run from: a delivery is run only
// by the attempt holding its lease (WebhookDelivery.LeaseUntil), so the
// receiver, a duplicate delivery and the replay can never run one delivery's
// handlers at the same time. Every claim is a single conditional UPDATE, which
// Postgres serialises per row, so that holds across replicas too.
type DeliveryStore struct {
	db  *gorm.DB
	now func() time.Time
}

func NewDeliveryStore(db *gorm.DB) *DeliveryStore {
	return &DeliveryStore{db: db, now: time.Now}
}

// WithClock replaces the store's clock. Every lease and backoff decision reads
// it, so a test drives expiry through the store rather than by editing rows.
func (s *DeliveryStore) WithClock(now func() time.Time) *DeliveryStore {
	s.now = now
	return s
}

// PersistResult is what the receiver acts on to decide ack vs. dispatch.
type PersistResult struct {
	// Created is true when the delivery row was newly inserted (not a replay).
	Created bool
	// AlreadyProcessed is true when an existing row has ProcessedAt set —
	// the receiver acks 200 without re-running the handler.
	AlreadyProcessed bool
	// Claimed is true when this call took the delivery's lease: the caller now
	// owns one handler run of it and must settle it with MarkProcessed or
	// MarkFailed. A duplicate of an unprocessed delivery whose lease is still
	// held (in flight, or inside its retry backoff) is not Claimed.
	Claimed bool
	// Attempts is the attempt this claim is (1 for a fresh delivery). Zero
	// when nothing was claimed.
	Attempts int
	// ReceivedAt is when the delivery was first received — this call for a
	// fresh one, the original receipt for a duplicate. Zero when nothing was
	// claimed.
	ReceivedAt time.Time
}

// Persist atomically dedups + stores the delivery and the raw payload, and
// claims it for lease.
//
// A fresh insert is Created and Claimed as attempt 1. An existing processed row
// is AlreadyProcessed. An existing unprocessed row is a duplicate (a GitHub
// redelivery, which keeps the delivery id): it is Claimed only when no other
// attempt holds its lease, so a duplicate landing mid-handler never runs the
// handlers twice, while one arriving after a failure or a dead holder takes the
// delivery over. Handlers must still be idempotent: a claimed re-run repeats
// whatever the previous attempt got through before it failed.
func (s *DeliveryStore) Persist(ctx context.Context, deliveryID, ocOrgID, event, action string, payload []byte, lease time.Duration) (PersistResult, error) {
	if deliveryID == "" {
		return PersistResult{}, fmt.Errorf("delivery id required")
	}

	now := s.now().UTC()
	leaseUntil := now.Add(lease)
	row := WebhookDelivery{
		DeliveryID: deliveryID,
		OcOrgID:    ocOrgID,
		Event:      event,
		Action:     action,
		ReceivedAt: now,
		Attempts:   1,
		LeaseUntil: &leaseUntil,
	}
	res := s.db.WithContext(ctx).Clauses().Create(&row)
	if res.Error == nil {
		// Fresh insert. Persist the payload too.
		if err := s.db.WithContext(ctx).Create(&WebhookPayload{
			DeliveryID: deliveryID,
			Payload:    payload,
			CreatedAt:  now,
		}).Error; err != nil {
			return PersistResult{}, fmt.Errorf("persist payload: %w", err)
		}
		return PersistResult{Created: true, Claimed: true, Attempts: 1, ReceivedAt: now}, nil
	}

	// Conflict on PK (existing delivery). Look up to decide whether it was
	// already processed.
	if !isUniqueViolation(res.Error) {
		return PersistResult{}, fmt.Errorf("persist delivery: %w", res.Error)
	}

	var existing WebhookDelivery
	if err := s.db.WithContext(ctx).
		Where("delivery_id = ?", deliveryID).
		First(&existing).Error; err != nil {
		return PersistResult{}, fmt.Errorf("lookup existing: %w", err)
	}
	if existing.ProcessedAt != nil {
		return PersistResult{AlreadyProcessed: true}, nil
	}
	attempts, claimed, err := s.claim(ctx, deliveryID, now, leaseUntil)
	if err != nil {
		return PersistResult{}, err
	}
	if !claimed {
		return PersistResult{}, nil
	}
	return PersistResult{Claimed: true, Attempts: attempts, ReceivedAt: existing.ReceivedAt}, nil
}

// claim takes an unprocessed delivery's lease when nobody holds it, in one
// conditional UPDATE: of two callers racing for the same row, Postgres lets
// exactly one match. It reports the attempt the claim became.
func (s *DeliveryStore) claim(ctx context.Context, deliveryID string, now, leaseUntil time.Time) (int, bool, error) {
	var claimed []WebhookDelivery
	err := s.db.WithContext(ctx).Raw(`
UPDATE webhook_deliveries
   SET attempts = attempts + 1, lease_until = ?, abandoned_at = NULL
 WHERE delivery_id = ?
   AND processed_at IS NULL
   AND (lease_until IS NULL OR lease_until <= ?)
RETURNING delivery_id, attempts`, leaseUntil, deliveryID, now).Scan(&claimed).Error
	if err != nil {
		return 0, false, fmt.Errorf("claim delivery: %w", err)
	}
	if len(claimed) == 0 {
		return 0, false, nil
	}
	return claimed[0].Attempts, true, nil
}

// ReplayQuery selects the deliveries a replay pass may claim.
type ReplayQuery struct {
	// Window is how far back a delivery's receipt may lie. Older unprocessed
	// deliveries are left alone: replaying them would act on a world that has
	// moved on, and the reconcile sweeps own what they were for.
	Window time.Duration
	// MaxAttempts caps the handler runs per delivery, the receiver's included.
	MaxAttempts int
	// Lease is the claim each returned delivery is taken under.
	Lease time.Duration
	// Limit bounds one claim.
	Limit int
}

// ClaimedDelivery is one delivery a replay pass now holds the lease on.
type ClaimedDelivery struct {
	DeliveryID string
	OcOrgID    string
	Event      string
	Action     string
	// Attempts is the attempt this claim is.
	Attempts int
	// ReceivedAt is when the delivery was first received.
	ReceivedAt time.Time
	// Payload is the stored body, which is the REDACTED copy
	// (redactPublishedCredentials): a handler that reads a published-credential
	// comment body sees the notice instead.
	Payload []byte
}

// ClaimReplayable claims up to q.Limit unprocessed deliveries that are due:
// received within q.Window, under q.MaxAttempts, and with no live lease (not in
// flight and not inside a retry backoff). Oldest receipt first, which is the
// order the replay runs them in.
//
// The claim is one UPDATE over a FOR UPDATE SKIP LOCKED selection, with the
// due-ness re-checked on the updated row itself: concurrent callers skip each
// other's candidates, and a row whose lease changed after it was selected is
// not matched. So a delivery is handed to exactly one caller, across replicas.
func (s *DeliveryStore) ClaimReplayable(ctx context.Context, q ReplayQuery) ([]ClaimedDelivery, error) {
	now := s.now().UTC()
	var claimed []WebhookDelivery
	err := s.db.WithContext(ctx).Raw(`
UPDATE webhook_deliveries
   SET attempts = attempts + 1, lease_until = @lease
 WHERE delivery_id IN (
         SELECT delivery_id FROM webhook_deliveries
          WHERE processed_at IS NULL
            AND abandoned_at IS NULL
            AND received_at > @since
            AND attempts < @max
            AND (lease_until IS NULL OR lease_until <= @now)
          ORDER BY received_at
          LIMIT @limit
          FOR UPDATE SKIP LOCKED)
   AND processed_at IS NULL
   AND abandoned_at IS NULL
   AND attempts < @max
   AND (lease_until IS NULL OR lease_until <= @now)
RETURNING delivery_id, oc_org_id, event, action, received_at, attempts`,
		map[string]any{
			"lease": now.Add(q.Lease),
			"since": now.Add(-q.Window),
			"max":   q.MaxAttempts,
			"now":   now,
			"limit": q.Limit,
		}).Scan(&claimed).Error
	if err != nil {
		return nil, fmt.Errorf("claim replayable deliveries: %w", err)
	}
	if len(claimed) == 0 {
		return nil, nil
	}
	// RETURNING carries no order, so the receipt order is restored here.
	sort.Slice(claimed, func(i, j int) bool { return claimed[i].ReceivedAt.Before(claimed[j].ReceivedAt) })

	ids := make([]string, len(claimed))
	for i, d := range claimed {
		ids[i] = d.DeliveryID
	}
	var payloads []WebhookPayload
	if err := s.db.WithContext(ctx).Where("delivery_id IN ?", ids).Find(&payloads).Error; err != nil {
		return nil, fmt.Errorf("load replay payloads: %w", err)
	}
	byID := make(map[string][]byte, len(payloads))
	for _, p := range payloads {
		byID[p.DeliveryID] = p.Payload
	}
	out := make([]ClaimedDelivery, len(claimed))
	for i, d := range claimed {
		out[i] = ClaimedDelivery{
			DeliveryID: d.DeliveryID,
			OcOrgID:    d.OcOrgID,
			Event:      d.Event,
			Action:     d.Action,
			Attempts:   d.Attempts,
			ReceivedAt: d.ReceivedAt,
			Payload:    byID[d.DeliveryID],
		}
	}
	return out, nil
}

// ErrDeliveryLeaseLost is returned when an attempt tries to settle a delivery
// that another attempt has since taken over: its lease lapsed while it ran.
// The newer attempt owns the outcome, so the stale one writes nothing.
var ErrDeliveryLeaseLost = errors.New("webhook delivery: lease lost to a newer attempt")

// MarkProcessed records the attempt's success so a duplicate is acked without
// re-running, and releases the lease. It settles only `attempt`'s own claim
// (ErrDeliveryLeaseLost otherwise).
func (s *DeliveryStore) MarkProcessed(ctx context.Context, deliveryID string, attempt int) error {
	now := s.now().UTC()
	return s.settle(ctx, deliveryID, attempt, map[string]any{
		"processed_at":  &now,
		"process_error": "",
		"lease_until":   nil,
	})
}

// MarkFailed records the attempt's error for audit and holds the delivery for
// retryIn: the lease moves to that moment, which is the retry backoff.
// processed_at stays null, so the replay (or a duplicate delivery) re-runs the
// handler once the backoff has passed. It settles only `attempt`'s own claim.
func (s *DeliveryStore) MarkFailed(ctx context.Context, deliveryID string, attempt int, errMsg string, retryIn time.Duration) error {
	retryAt := s.now().UTC().Add(retryIn)
	return s.settle(ctx, deliveryID, attempt, map[string]any{
		"process_error": errMsg,
		"lease_until":   &retryAt,
	})
}

// MarkAbandoned records that the replay gives the delivery up after `attempt`
// failed as its last: nothing will replay it again.
func (s *DeliveryStore) MarkAbandoned(ctx context.Context, deliveryID string, attempt int) error {
	now := s.now().UTC()
	return s.settle(ctx, deliveryID, attempt, map[string]any{"abandoned_at": &now})
}

// settle writes an attempt's outcome, fenced on the attempt still being the
// delivery's current one. attempts only grows, and only by a claim, so a match
// proves no other attempt has taken the delivery over since.
func (s *DeliveryStore) settle(ctx context.Context, deliveryID string, attempt int, fields map[string]any) error {
	res := s.db.WithContext(ctx).
		Model(&WebhookDelivery{}).
		Where("delivery_id = ? AND attempts = ?", deliveryID, attempt).
		Updates(fields)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDeliveryLeaseLost
	}
	return nil
}

// AbandonedDelivery is a delivery AbandonExpired gave up on.
type AbandonedDelivery struct {
	DeliveryID string
	OcOrgID    string
	Event      string
	Action     string
	Attempts   int
}

// AbandonExpired stamps abandoned every unprocessed delivery received longer ago
// than window and not yet abandoned, and returns them, so each is reported
// exactly once. One still inside its lease is left to its holder.
func (s *DeliveryStore) AbandonExpired(ctx context.Context, window time.Duration) ([]AbandonedDelivery, error) {
	now := s.now().UTC()
	var rows []WebhookDelivery
	err := s.db.WithContext(ctx).Raw(`
UPDATE webhook_deliveries
   SET abandoned_at = @now
 WHERE processed_at IS NULL
   AND abandoned_at IS NULL
   AND received_at <= @since
   AND (lease_until IS NULL OR lease_until <= @now)
RETURNING delivery_id, oc_org_id, event, action, attempts`,
		map[string]any{"now": now, "since": now.Add(-window)}).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("abandon expired deliveries: %w", err)
	}
	out := make([]AbandonedDelivery, len(rows))
	for i, r := range rows {
		out[i] = AbandonedDelivery{DeliveryID: r.DeliveryID, OcOrgID: r.OcOrgID, Event: r.Event, Action: r.Action, Attempts: r.Attempts}
	}
	return out, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") || strings.Contains(msg, "unique constraint")
}
