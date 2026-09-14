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
	"log/slog"
	"time"
)

// Read-page bounds for the feed endpoint.
const (
	defaultLimit = 50
	maxLimit     = 200
)

// ActivityService records activity events (best-effort) and serves the feed. It also
// owns the live-tail hub so callers need only one dependency.
type ActivityService struct {
	repo ActivityRepository
	hub  *ActivityHub
}

// NewActivityService wires the store-backed service.
func NewActivityService(repo ActivityRepository, hub *ActivityHub) *ActivityService {
	return &ActivityService{repo: repo, hub: hub}
}

// Record appends the event best-effort: a storage failure is logged and
// swallowed — recording activity must never fail or delay the SDLC operation
// that produced it. On a real (non-duplicate) insert it wakes the project's
// live-tail subscribers. Satisfies Recorder.
func (s *ActivityService) Record(ctx context.Context, e ActivityInput) {
	if s == nil || s.repo == nil {
		return
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC() // fallback only; producers pass a real time
	}
	row := &ActivityEvent{
		OrgID: e.OrgID, ProjectID: e.ProjectID, Type: e.Type,
		ActorKind: e.ActorKind, ActorID: e.ActorID, ActorName: e.ActorName,
		Issue: e.Issue, Title: e.Title, Component: e.Component,
		Environment: e.Environment, Tag: e.Tag, Reason: e.Reason,
		DedupKey: e.DedupKey, OccurredAt: e.OccurredAt,
	}
	inserted, err := s.repo.Insert(ctx, row)
	if err != nil {
		slog.WarnContext(ctx, "activity: record failed",
			"type", e.Type, "project", e.ProjectID, "error", err)
		return
	}
	if inserted {
		s.hub.Notify(e.OrgID, e.ProjectID)
	}
}

// List returns a project's events newest-first, at most limit (clamped to
// [1, maxLimit], default when non-positive), paged before the (beforeTime,
// beforeID) cursor.
func (s *ActivityService) List(ctx context.Context, orgID, projectID string, limit int, beforeTime time.Time, beforeID string) ([]ActivityEvent, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return s.repo.ListByProject(ctx, orgID, projectID, limit, beforeTime, beforeID)
}

// Subscribe registers a live-tail listener for (org, project); cancel deregisters.
func (s *ActivityService) Subscribe(orgID, projectID string) (<-chan struct{}, func()) {
	return s.hub.Subscribe(orgID, projectID)
}
