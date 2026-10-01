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
	"strings"
	"time"

	"gorm.io/gorm"
)

// DeliveryStore persists inbound webhook deliveries with free dedup via the
// PK on delivery_id. Payloads are stored in a sibling table so retention
// can drop the bulk while keeping dedup history.
type DeliveryStore struct {
	db *gorm.DB
}

func NewDeliveryStore(db *gorm.DB) *DeliveryStore {
	return &DeliveryStore{db: db}
}

// PersistResult is what the receiver acts on to decide ack vs. dispatch.
type PersistResult struct {
	// Created is true when the delivery row was newly inserted (not a replay).
	Created bool
	// AlreadyProcessed is true when an existing row has ProcessedAt set —
	// the receiver acks 200 without re-running the handler.
	AlreadyProcessed bool
}

// Persist atomically dedups + stores the delivery and the raw payload.
// Returns Created=true on a fresh insert, AlreadyProcessed=true when a row
// exists with processed_at set, and (false, false) when the row exists but
// processing failed earlier — the receiver re-runs the handler in that case
// (GitHub-redelivery semantics: handler must be idempotent).
//
// payloadRedacted records that `payload` is not byte-identical to what GitHub
// sent; see WebhookDelivery.PayloadRedacted for what the retry sweep does with
// it.
func (s *DeliveryStore) Persist(ctx context.Context, deliveryID, ocOrgID, event, action string, payload []byte, payloadRedacted bool) (PersistResult, error) {
	if deliveryID == "" {
		return PersistResult{}, fmt.Errorf("delivery id required")
	}

	now := time.Now().UTC()
	row := WebhookDelivery{
		DeliveryID:      deliveryID,
		OcOrgID:         ocOrgID,
		Event:           event,
		Action:          action,
		ReceivedAt:      now,
		PayloadRedacted: payloadRedacted,
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
		return PersistResult{Created: true}, nil
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
	return PersistResult{AlreadyProcessed: existing.ProcessedAt != nil}, nil
}

// MarkProcessed records successful processing on the delivery row so a
// redelivery is acked without re-running. Clears the retry schedule: a
// processed row is never claimable again.
func (s *DeliveryStore) MarkProcessed(ctx context.Context, deliveryID string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).
		Model(&WebhookDelivery{}).
		Where("delivery_id = ?", deliveryID).
		Updates(map[string]any{
			"processed_at":    &now,
			"process_error":   "",
			"next_attempt_at": nil,
		}).Error
}

// MarkFailed records a processing error so on-call can audit; the row's
// processed_at stays null so GitHub redelivery re-runs the handler.
//
// nextAttemptAt schedules the retry sweep's next claim. A nil value takes the
// row out of the sweep's reach — used both when attempts are exhausted and for
// a payload the sweep must not replay — while leaving processed_at null so a
// GitHub redelivery still re-runs it.
func (s *DeliveryStore) MarkFailed(ctx context.Context, deliveryID string, errMsg string, attempts int, nextAttemptAt *time.Time) error {
	return s.db.WithContext(ctx).
		Model(&WebhookDelivery{}).
		Where("delivery_id = ?", deliveryID).
		Updates(map[string]any{
			"process_error":   errMsg,
			"attempts":        attempts,
			"next_attempt_at": nextAttemptAt,
		}).Error
}

// RetryableDelivery is one claimed row plus the body to re-dispatch.
type RetryableDelivery struct {
	DeliveryID string
	OcOrgID    string
	Event      string
	Action     string
	Attempts   int
	Payload    []byte
}

// ClaimRetryable returns deliveries whose dispatch never completed and whose
// backoff has elapsed, oldest first.
//
// Redacted payloads are excluded in SQL rather than filtered afterwards, so a
// backlog of them cannot crowd out the rows the sweep can actually act on.
// Claiming is not exclusive: two replicas can take the same row, which is
// acceptable because handlers are already required to be idempotent for
// GitHub redelivery and the alternative — a lock column — buys nothing the
// idempotency requirement does not already cover.
func (s *DeliveryStore) ClaimRetryable(ctx context.Context, now time.Time, limit int) ([]RetryableDelivery, error) {
	if limit <= 0 {
		return nil, nil
	}
	var rows []WebhookDelivery
	if err := s.db.WithContext(ctx).
		Where("processed_at IS NULL").
		Where("next_attempt_at IS NOT NULL AND next_attempt_at <= ?", now.UTC()).
		Where("payload_redacted = ?", false).
		Order("received_at ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("claim retryable deliveries: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.DeliveryID
	}
	var payloads []WebhookPayload
	if err := s.db.WithContext(ctx).
		Where("delivery_id IN ?", ids).
		Find(&payloads).Error; err != nil {
		return nil, fmt.Errorf("load retry payloads: %w", err)
	}
	byID := make(map[string][]byte, len(payloads))
	for _, p := range payloads {
		byID[p.DeliveryID] = p.Payload
	}

	out := make([]RetryableDelivery, 0, len(rows))
	for _, r := range rows {
		// A row whose payload retention already ran has nothing to replay.
		// Skipped rather than failed: the delivery row is dedup history at
		// that point, not work.
		body, ok := byID[r.DeliveryID]
		if !ok {
			continue
		}
		out = append(out, RetryableDelivery{
			DeliveryID: r.DeliveryID,
			OcOrgID:    r.OcOrgID,
			Event:      r.Event,
			Action:     r.Action,
			Attempts:   r.Attempts,
			Payload:    body,
		})
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
