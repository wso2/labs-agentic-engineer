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

import "time"

// WebhookDelivery is the dedup row for an inbound GitHub event.
//
// The PK on DeliveryID (X-GitHub-Delivery, a UUID) gives us free dedup via
// `INSERT … ON CONFLICT DO NOTHING`. Payload retention is handled separately
// in WebhookPayload so a retention sweep can drop the bulk while preserving
// dedup history.
type WebhookDelivery struct {
	DeliveryID   string     `gorm:"primaryKey;type:text" json:"deliveryId"`
	OcOrgID      string     `gorm:"index;not null;type:text" json:"ocOrgId"`
	Event        string     `gorm:"index;not null;type:text" json:"event"`
	Action       string     `gorm:"index;type:text" json:"action,omitempty"`
	ReceivedAt   time.Time  `gorm:"index;not null" json:"receivedAt"`
	ProcessedAt  *time.Time `json:"processedAt,omitempty"`
	ProcessError string     `gorm:"type:text" json:"processError,omitempty"`

	// Attempts counts dispatches that have finished and failed. The receiver's
	// own first pass is attempt 1, so a row the sweeper has never touched
	// arrives here already at 1.
	Attempts int `gorm:"not null;default:0" json:"attempts"`
	// NextAttemptAt is when the retry sweep may claim this row. Null means the
	// row is not claimable: either it is still in flight on the receiver, or it
	// has exhausted its attempts. Indexed with ProcessedAt because the sweep's
	// only query filters on exactly those two columns.
	NextAttemptAt *time.Time `gorm:"index" json:"nextAttemptAt,omitempty"`
	// PayloadRedacted marks a row whose stored body is NOT what GitHub sent —
	// redactPublishedCredentials rewrote a comment/issue body that carried
	// published test-user credentials. The sweep refuses to replay these,
	// because re-dispatching a mutated body is not a retry of the original
	// event. They remain visible for audit and recoverable by GitHub
	// redelivery, which carries the true body.
	PayloadRedacted bool `gorm:"not null;default:false" json:"payloadRedacted,omitempty"`
}

// WebhookPayload holds the raw event body. Split from WebhookDelivery so
// payload retention can run independently of dedup-history retention.
//
// Stored as raw bytes (jsonb in Postgres) so we don't pay parse cost at write
// time. Handlers re-parse on read.
type WebhookPayload struct {
	DeliveryID string    `gorm:"primaryKey;type:text" json:"deliveryId"`
	Payload    []byte    `gorm:"type:jsonb;not null" json:"payload"`
	CreatedAt  time.Time `gorm:"index;not null" json:"createdAt"`
}
