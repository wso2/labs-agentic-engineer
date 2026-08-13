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

package codingagent

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// DefaultCodingAgentComponentRetention is how many coding-agent Components a
// project may hold when CODING_AGENT_COMPONENT_RETENTION is unset. A FINISHED
// component still occupies a billing concurrency slot, so the number is
// platform housekeeping rather than an operator SLA knob — cloud ships this
// default; local compose may lower it (via the env) so LRU prune is observable
// without eleven cycles. The real cloud cap is whichever is smaller — this, or
// the org's plan limit (wso2cloud#793), which the entitlement gate enforces
// with a 402 the dispatch path already reports.
//
// Ten keeps a useful window of recent cycles inspectable in the OC console
// without letting a busy project sit permanently at its cap.
const DefaultCodingAgentComponentRetention = 10

// OpenCycleReader reports which of a project's run cycles are still LIVE.
// Satisfied by delivery.RunCycleRepository.
//
// It is what makes the reap safe: OpenChoreo registers no health check for a
// `batch/v1 Job`, so a Component's own status cannot be trusted to say whether
// its pod is running (the pod-truth watcher that will know is phase 08b). The
// run-cycle rows DO know — a cycle with no ended_at is an agent mid-run — so
// liveness is read from the database and only the rest is reapable.
type OpenCycleReader interface {
	ListOpenCycleIDs(ctx context.Context, orgID, projectID string) ([]string, error)
}

// ComponentRetention keeps a project under DefaultCodingAgentComponentRetention
// coding-agent Components by deleting the oldest RETIRED ones before a new
// cycle is created.
//
// Pre-create is the only moment that works: deletion is what frees the billing
// slot, so a reap after the fact would leave the create to 402 against
// components nobody needs. It is an LRU because the newest retired cycles are
// the ones a human is most likely to still be looking at.
type ComponentRetention struct {
	oc     openchoreo.ComponentClient
	cycles OpenCycleReader
	limit  int
}

// NewComponentRetention wires the reaper. limit <= 0 falls back to
// DefaultCodingAgentComponentRetention.
func NewComponentRetention(oc openchoreo.ComponentClient, cycles OpenCycleReader, limit int) *ComponentRetention {
	if limit <= 0 {
		limit = DefaultCodingAgentComponentRetention
	}
	return &ComponentRetention{oc: oc, cycles: cycles, limit: limit}
}

// Enforce satisfies RetentionEnforcer so OCDispatcher.WithRetention can take
// *ComponentRetention directly.
func (r *ComponentRetention) Enforce(ctx context.Context, orgID, projectID string) error {
	return r.ReapBeforeCreate(ctx, orgID, projectID)
}

// ReapBeforeCreate deletes the oldest retired coding-agent Components until the
// project can hold one more.
//
// It is deliberately forgiving about the outcome: an org whose every slot is
// occupied by a LIVE cycle cannot be reaped, and that is not an error — the
// create that follows answers with a 402 the dispatcher reports as blocked,
// which is a far better message than "the reaper could not help". A failure to
// LIST or DELETE is returned, so the caller can log that the reap did not run.
func (r *ComponentRetention) ReapBeforeCreate(ctx context.Context, orgID, projectID string) error {
	if r == nil || r.oc == nil {
		return nil
	}
	all, err := r.oc.ListInternalComponents(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("retention: list internal components for %s/%s: %w", orgID, projectID, err)
	}

	agents := make([]openchoreo.InternalComponent, 0, len(all))
	for _, c := range all {
		// Match the lister: accept bare `coding-agent` and `job/coding-agent`.
		if openchoreo.IsCodingAgentTypeName(c.TypeName) {
			agents = append(agents, c)
		}
	}
	// Room for the create that follows, hence +1.
	surplus := len(agents) - r.limit + 1
	if surplus <= 0 {
		return nil
	}

	live, err := r.openCycleIDs(ctx, orgID, projectID)
	if err != nil {
		return err
	}

	retired := make([]openchoreo.InternalComponent, 0, len(agents))
	for _, c := range agents {
		if _, running := live[c.CycleID]; running {
			continue
		}
		retired = append(retired, c)
	}
	// Oldest first; name breaks ties so the order is deterministic under
	// same-second creation timestamps.
	sort.Slice(retired, func(i, j int) bool {
		if retired[i].CreatedAt.Equal(retired[j].CreatedAt) {
			return retired[i].Name < retired[j].Name
		}
		return retired[i].CreatedAt.Before(retired[j].CreatedAt)
	})
	if surplus > len(retired) {
		slog.WarnContext(ctx, "retention: cannot free enough agent-component slots — every remaining cycle is live",
			"org", orgID, "project", projectID, "components", len(agents),
			"retired", len(retired), "limit", r.limit)
		surplus = len(retired)
	}

	for _, c := range retired[:surplus] {
		// Through the OC API, never a Kubernetes client: an out-of-band delete
		// emits no billing decrement, so it would free the pod and keep the
		// slot. DeleteComponent is idempotent (404 is success), which is what
		// makes a re-run of this reap harmless.
		if err := r.oc.DeleteComponent(ctx, orgID, projectID, c.Name); err != nil {
			return fmt.Errorf("retention: delete retired agent component %q: %w", c.Name, err)
		}
		slog.InfoContext(ctx, "retention: deleted retired agent component",
			"org", orgID, "project", projectID, "component", c.Name, "cycle", c.CycleID)
	}
	return nil
}

// openCycleIDs is the live set, as a lookup. A missing reader is treated as
// "nothing is live" only when it is genuinely absent — the composition root
// always wires it, and a nil reader in a test means the test is about ordering,
// not liveness.
func (r *ComponentRetention) openCycleIDs(ctx context.Context, orgID, projectID string) (map[string]struct{}, error) {
	if r.cycles == nil {
		return map[string]struct{}{}, nil
	}
	ids, err := r.cycles.ListOpenCycleIDs(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("retention: list open cycles for %s/%s: %w", orgID, projectID, err)
	}
	live := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		live[id] = struct{}{}
	}
	return live, nil
}
