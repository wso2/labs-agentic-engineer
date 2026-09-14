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

package projects

import (
	"context"
	"time"
)

// Repository is the storage port the activity ActivityService depends on — narrow
// enough to fake in unit tests without a database. The concrete
// repositories.ActivityEventRepository satisfies it structurally.
type ActivityRepository interface {
	// Insert appends an event, ignoring a duplicate (org_id, project_id,
	// dedup_key) via ON CONFLICT DO NOTHING. inserted is false on a duplicate
	// no-op, so the caller can skip a redundant live-tail notify.
	Insert(ctx context.Context, row *ActivityEvent) (inserted bool, err error)

	// ListByProject returns a project's events newest-first (occurred_at DESC,
	// id DESC), at most limit. When beforeTime is non-zero it returns only rows
	// strictly older than the (beforeTime, beforeID) cursor — the "show more" page.
	ListByProject(ctx context.Context, orgID, projectID string, limit int, beforeTime time.Time, beforeID string) ([]ActivityEvent, error)
}

// Event is the input to Record — one activity to append. The service stamps it
// into a ActivityEvent row. Producers set OccurredAt from a real clock
// (in a Temporal workflow: workflow.Now(ctx), passed into the recording activity).
type ActivityInput struct {
	OrgID     string
	ProjectID string
	Type      string
	ActorKind string
	ActorID   string
	ActorName string

	Issue       int
	Title       string
	Component   string
	Environment string
	Tag         string
	// Reason is a code, never prose: `run_failed` carries the run's failure
	// code (or its terminal reason) and the console owns the sentence.
	Reason string

	DedupKey   string
	OccurredAt time.Time
}

// Recorder is what event producers call to append an activity (best-effort:
// implementations never return an error — a storage failure is logged and
// swallowed so it can never fail the SDLC operation that produced it).
type ActivityRecorder interface {
	Record(ctx context.Context, e ActivityInput)
}
