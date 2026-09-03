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

package runread

import (
	"context"
	"fmt"
	"log/slog"
)

// Commands is the run surface's two writes: cancel a run, and ask a version's
// validation criteria again. Neither decides anything — both resolve the target
// through the org-scoped read first, then hand off.
type Commands struct {
	runs       RunReader
	record     CancelRequester
	cancel     RunCanceller
	revalidate Revalidator
	// reaper stops the cancelled cycle's agent pod by deleting its Component.
	// nil → the run still settles, nothing is reaped.
	reaper CycleReaper
}

// NewCommands wires the command service. A nil collaborator leaves its own
// command answering "not configured" rather than panicking.
func NewCommands(runs RunReader, record CancelRequester, cancel RunCanceller, revalidate Revalidator) *Commands {
	return &Commands{runs: runs, record: record, cancel: cancel, revalidate: revalidate}
}

// WithCycleReaper enables reaping the cancelled run's agent Component. Returns
// the receiver for chained construction.
func (c *Commands) WithCycleReaper(r CycleReaper) *Commands {
	c.reaper = r
	return c
}

// Cancel abandons an increment — the only expiry the run's unbounded wait state
// has.
//
// Three writes, in an order that is the whole design: RECORD, then SIGNAL, then
// REAP.
//
//	record  the request lands on the run row first, so from here on a lost
//	        signal costs latency rather than correctness. Cancel is the one fact
//	        the loop cannot re-derive from the world — a pod the reap killed and
//	        an agent that died on its own are indistinguishable from inside the
//	        workflow — so it is given the same shape as every other fact the loop
//	        acts on: durable state the loop re-reads, with the signal as the
//	        wake-up rather than the evidence.
//	signal  the run stops at its next safe point instead of at its next poll.
//	reap    the agent's Component goes, best-effort.
//
// It resolves the run through the org-scoped read FIRST, so a run in another org
// or another project is a 404 before anything is written. The run still settles
// its OWN row — cancel is a signal and not a workflow cancellation precisely so
// the loop can run the activities that record the outcome — and this write is a
// request, never the outcome.
func (c *Commands) Cancel(ctx context.Context, orgID, projectID, runID string) error {
	if c == nil || c.runs == nil || c.cancel == nil || c.record == nil {
		return fmt.Errorf("runread: cancel not configured")
	}
	row, err := c.runs.GetByIDScoped(ctx, orgID, runID)
	if err != nil {
		return err
	}
	if row == nil || row.ProjectID != projectID {
		return ErrRunNotFound
	}
	// A settled run answers (nil, nil) — nothing to cancel, and nothing below
	// changes that, so the signal and the reap still run harmlessly against a
	// workflow that has already finished.
	if _, err := c.record.RequestCancel(ctx, row.ID); err != nil {
		return err
	}
	if err := c.cancel.CancelRun(ctx, row); err != nil {
		return err
	}
	// The signal settles the RUN; this stops the POD. Ordered last deliberately:
	// until the request is recorded nothing is cancelled and the caller retries,
	// so killing the agent first would abandon a run that is about to carry on.
	//
	// A failed reap does NOT fail the cancel: the run is already stopping, and
	// the only cost is a component that keeps holding a billing slot until it
	// is swept — answering "cancel failed" would invite a retry that changes
	// nothing.
	if c.reaper != nil {
		if err := c.reaper.ReapRunCycle(ctx, orgID, row.ProjectID, row.ID); err != nil {
			slog.WarnContext(ctx, "cancel: could not delete the cycle's agent component; the run is still cancelled",
				"org", orgID, "project", row.ProjectID, "run", row.ID, "error", err)
		}
	}
	return nil
}

// Revalidate asks a version's validation criteria again, against the system
// already deployed, and answers with the run that will produce the verdict.
//
// This surface's whole job is the RESOLUTION: a `v<N>` tag becomes a milestone
// number through the platform's own run rows — never by matching titles against
// GitHub, which are renamable and case-folded — and a version this project has
// never run is a 404 before anything is started. Every substantive guard (a run
// already live, work still open, no oracle) belongs to the collaborator that can
// answer it, and rides back as an error.
//
// The title comes from the version's OWN runs rather than being re-read: those
// rows are what the tag resolved through, so their milestone title is the one
// GitHub was given at creation, which is what the runner's milestone discovery
// needs.
func (c *Commands) Revalidate(ctx context.Context, orgID, projectID, tag string, attempts, ceiling int) (runID string, milestoneNumber int, err error) {
	if c == nil || c.runs == nil || c.revalidate == nil {
		return "", 0, fmt.Errorf("runread: revalidate not configured")
	}
	number, found, err := c.runs.MilestoneNumberForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return "", 0, err
	}
	if !found {
		return "", 0, ErrTagNotFound
	}
	rows, err := c.runs.ListByMilestone(ctx, orgID, projectID, number)
	if err != nil {
		return "", 0, err
	}
	if len(rows) == 0 {
		// The tag resolved through a run row, so this is unreachable short of a
		// concurrent purge — and returning "not found" is the honest answer if it is.
		return "", 0, ErrTagNotFound
	}
	id, err := c.revalidate.Revalidate(ctx, orgID, projectID, RevalidateTarget{
		MilestoneNumber: number,
		MilestoneTitle:  rows[0].MilestoneTitle,
		Attempts:        attempts,
		Ceiling:         ceiling,
	})
	if err != nil {
		return "", 0, err
	}
	return id, number, nil
}
