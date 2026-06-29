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

package models

import "time"

// AccessRequest is the tracking/UX record for a cross-project request to publish
// an org service (marketplace P3.5). A consumer component depends on an
// `org-service` that exists but is published only project-only (dep status:
// `blocked`, reason: `access-required`); requesting access creates a publish
// ComponentTask on the provider's project + repo and writes this row. The
// functional resume is the existing org-service gate (the provider going
// namespace-visible), not this row — the AccessRequest is the tracking layer
// that drives the consumer-side chip and lets many consumers fan out from one
// provider publish task.
//
// One row per (OrgID, ConsumerProjectID, ConsumerComponentName, OrgServiceName).
type AccessRequest struct {
	ID    string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID string `gorm:"index;not null" json:"-"`

	// Consumer side — who is asking. OrgServiceName is the requested provider
	// component name (the catalog key the consumer's dependency references).
	ConsumerProjectID     string `gorm:"index;not null" json:"consumerProjectId"`
	ConsumerComponentName string `gorm:"not null" json:"consumerComponentName"`
	OrgServiceName        string `gorm:"not null" json:"orgServiceName"` // = provider OC component name

	// Provider side — the target whose visibility must widen. Resolved from the
	// catalog row at request time. The publish ComponentTask + its GitHub issue
	// live on the provider's project/repo; many consumer requests dedupe onto one
	// provider task (ProviderTaskID).
	ProviderProjectID     string `json:"providerProjectId,omitempty"`
	ProviderComponentName string `json:"providerComponentName,omitempty"`
	ProviderTaskID        string `gorm:"index" json:"providerTaskId,omitempty"`
	ProviderIssueNumber   int    `json:"providerIssueNumber,omitempty"`
	ProviderIssueURL      string `gorm:"type:text" json:"providerIssueUrl,omitempty"`

	// Status is the request lifecycle (P3.5 §3/§5). Driven off existing signals:
	// dispatch of the provider task → in_progress; provider component deployed +
	// catalog namespace-visible → granted; provider issue closed unmerged →
	// rejected.
	Status string `gorm:"index;not null;default:requested" json:"status"` // requested|in_progress|granted|rejected

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName pins the table name (GORM's default pluralization would also yield
// "access_requests", but the explicit name keeps it stable).
func (AccessRequest) TableName() string { return "access_requests" }

// AccessRequest lifecycle status values (P3.5). Mirrors the TaskStatus* style.
const (
	AccessRequestStatusRequested  = "requested"
	AccessRequestStatusInProgress = "in_progress"
	AccessRequestStatusGranted    = "granted"
	AccessRequestStatusRejected   = "rejected"
)
