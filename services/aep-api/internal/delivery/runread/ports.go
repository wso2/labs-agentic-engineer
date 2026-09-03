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
	"errors"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Pre-stream fence sentinels. They are the whole error vocabulary of this
// package, and both map to 404 — a run that belongs to another org resolves to
// nil through the org-scoped read, so a cross-tenant probe is indistinguishable
// from a typo and never leaks existence.
var (
	// ErrTagNotFound means the project has no run row for that spec tag, which
	// is also how "no such version" reads.
	ErrTagNotFound = errors.New("runread: no run for this tag")
	// ErrRunNotFound means no run with that id belongs to this org and project.
	ErrRunNotFound = errors.New("runread: run not found")
	// ErrCycleNotFound means no cycle with that id belongs to the version being
	// read. Same fence as the other two — a cycle of another org, or of another
	// version of this project, is simply absent.
	ErrCycleNotFound = errors.New("runread: cycle not found")
)

// RunReader is the run rows this surface serves. Satisfied by
// delivery.MilestoneRunRepository.
//
// Note what is NOT here: no state writes. A run's STATE is the workflow's to
// change, so cancel reaches it through the supervisor's signal; the durable
// cancellation REQUEST is a different fact and has its own port
// (CancelRequester).
type RunReader interface {
	// MilestoneNumberForTag resolves a `?tag=v<N>` to a milestone number THROUGH
	// THE RUN ROWS, never by title-matching against GitHub — titles are
	// renamable and GitHub's title filters are case-insensitive while its
	// create-uniqueness is not.
	MilestoneNumberForTag(ctx context.Context, orgID, projectID, tag string) (number int, found bool, err error)
	// ListByMilestone returns a milestone's runs, newest first.
	ListByMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) ([]delivery.MilestoneRun, error)
	// GetByIDScoped returns the run only when it belongs to orgID, missing with
	// (nil, nil) — the org fence behind "404, never 403".
	GetByIDScoped(ctx context.Context, orgID, id string) (*delivery.MilestoneRun, error)
}

// CycleReader is a run's cycle timeline. Satisfied by
// delivery.RunCycleRepository.
type CycleReader interface {
	ListByRun(ctx context.Context, orgID, runID string) ([]delivery.RunCycle, error)
}

// ProjectBuildLister reads every build WorkflowRun in a project, in ONE call —
// the read a cycle's builds are derived from. Project-wide rather than
// per-component because the read side does not know which components a merge
// touched, and the run names themselves say: an attempt of (component, commit)
// carries that pair in its name, so filtering the project's runs by the merge
// SHA recovers the fan-out without re-deriving the path diff (which would cost
// a GitHub call per read).
//
// The alternative — storing the fan-out when it is triggered — is deliberately
// not taken: see CycleBuilds.
type ProjectBuildLister interface {
	ListProjectBuildRuns(ctx context.Context, orgID, projectID string) ([]delivery.MergeBuild, error)
}

// CycleLogReader is one cycle's agent activity — live OpenChoreo pod logs while
// the Component exists, then the observability archive while it is retained, or
// a synthetic unavailable line when neither can answer. Satisfied by
// codingagent.AgentProgressReader, reached as a port because that is a sibling
// slice. nil → the stream carries cycles and no lines.
type CycleLogReader interface {
	CycleProgress(ctx context.Context, cycle *delivery.RunCycle, sinceMillis int64) (*contracts.ProgressResponse, error)
}

// RunCanceller is the write behind the console's cancel button, satisfied by
// *run.Supervisor.
//
// Cancel is a SIGNAL, not a workflow cancellation: a cancelled context could not
// run the activities that record the outcome, so the run settles its own row on
// the ordinary path. delivery.ErrTemporalUnavailable means nothing was
// cancelled and the caller may retry.
type RunCanceller interface {
	CancelRun(ctx context.Context, row *delivery.MilestoneRun) error
}

// CancelRequester makes a cancellation DURABLE, before it is signalled.
// Satisfied by delivery.MilestoneRunRepository.
//
// It is a separate port from RunCanceller because the two do different things
// to different stores, in an order that matters: this one records that a person
// asked, and the supervisor's signal only makes the run notice sooner. Signal
// delivery is best-effort by construction — the supervisor swallows a failed
// SignalWorkflow so a dead engine cannot wedge the console — so a cancel that
// existed ONLY as a signal could vanish silently while the reaper went on to
// kill the agent, which the loop then reads as agent death.
//
// A run that already settled is not an error: it answers (nil, nil), and the
// cancel is a no-op on work that is already over.
type CancelRequester interface {
	RequestCancel(ctx context.Context, runID string) (*delivery.MilestoneRun, error)
}

// CycleReaper deletes the cancelled run's in-flight agent Component. Satisfied
// by codingagent.CycleReaper, reached as a port because dispatch and its
// cleanup belong to that slice.
//
// Optional: a boot without the OpenChoreo client cancels without reaping (the
// run still settles; the leaked component is swept). Cancel never fails on a
// reap error — see Commands.Cancel.
type CycleReaper interface {
	ReapRunCycle(ctx context.Context, orgID, projectID, runID string) error
}

// RevalidateTarget is the version to re-judge, already resolved to the platform
// key. The milestone NUMBER is that key; the title rides along because the run
// row and the runner's own milestone discovery both want it, and this surface
// has it in hand from the rows it read to resolve the tag.
//
// Attempts and Ceiling are pass-through budgets: zero on either means the
// platform default. One attempt is what makes a revalidation a pure re-check.
type RevalidateTarget struct {
	MilestoneNumber int
	MilestoneTitle  string
	Attempts        int
	Ceiling         int
}

// Revalidator starts a fresh run that asks a version's validation criteria
// again, against the system already deployed. Satisfied by the event plane's
// Revalidate.
//
// It is a port for the same reason RunCanceller is: the decision needs GitHub
// (is there open work?) and the project repo (is there an oracle?), and this
// package touches neither — a read model that reached GitHub could no longer be
// polled for free, which is the property its whole design rests on. So the
// handler resolves the tag through the run rows it already reads, and every
// guard lives beside the collaborator that answers it.
type Revalidator interface {
	Revalidate(ctx context.Context, orgID, projectID string, target RevalidateTarget) (runID string, err error)
}
