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

// Package run is the MILESTONE RUN SUPERVISOR: the Temporal workflows that work
// one milestone until it settles.
//
// There are THREE run species, and each is its own top-level workflow — a
// species is a workflow, not a branch (ADR-0020):
//
//	dev-<org>-<project>-<milestone>          DevRunWorkflow
//	task-<org>-<project>-<milestone>          TaskRunWorkflow
//	validation-<org>-<project>-<milestone>    ValidationRunWorkflow
//
// The id is keyed by the MILESTONE and prefixed by the KIND. A milestone sees
// sequential runs of one kind across its life, so the id is reused after a
// terminal run; the prefix is what keeps the three apart, because ids are reused
// under ALLOW_DUPLICATE and a stale signal aimed at a settled dev run would
// otherwise land on the validation run that claimed the id afterwards. The run
// ROW is the routing table: the event plane resolves a row before it signals
// anything, and the row's kind gives both the prefix and the workflow type.
//
// # dev and task: the cycle loop
//
//	WAIT ──► dispatch the coding agent ──► PR opened ──► auto-merge ──► builds
//	 ▲        (prompt = milestone reference only)                          │
//	 │                                        RECONCILE the version ◄──────┘
//	 │                                        (on EITHER build verdict)    │
//	 │  all green, open issues remain ─────────────► next cycle (re-WAIT)  │
//	 │  red after the one automatic re-trigger ─► FIX issue ─► next cycle  │
//	 │  merge conflict ─────────────────────────► CONFLICT issue ─► next   │
//	 └─ working set empty ──► the run's own BOOKEND ──► settle
//	    budgets exhausted / no progress / cancel ─► failed | blocked | cancelled
//	    working set empty but the version is not serving ─► version-incomplete
//
// They are the SAME loop with different bookends — one `bookends` value, never
// two cycle loops that drift apart:
//
//	dev   work:    DevWork  — armed, kind ∈ development/bug/conflict
//	      before:  provisionGates → planTasks (it owns the version it is filling)
//	      onEmpty: ASSERT the version is serving, mint its validation task →
//	               settle succeeded, and LEAVE THE MILESTONE OPEN: the version is
//	               deployed and unjudged. A version that is not serving settles
//	               FAILED `version-incomplete` (ADR-0026) — the read is what makes
//	               "delivered" a claim about the cluster rather than about the
//	               sequence of cycles that led here
//	task  work:    TaskWork — armed, kind ∈ bug/conflict, NEVER development
//	      before:  nothing (the milestone was filled by the build that shipped it)
//	      onEmpty: reopen the validation task IFF it worked a `src/validation`
//	               bug → settle succeeded
//
// The WORKING SET is the third bookend and the most consequential: it decides
// what a dispatch is spent on and what an empty milestone means. Planned work is
// dev-workflow's alone, because a dev run owns the version and holds the build
// mutex — so planned issues a build gave up on must wait for another build rather
// than be continued by a run that never planned them and carries different
// budgets. Both counts ride ONE boundary poll (the host returns them in one
// GraphQL call), so answering both costs nothing.
//
// # The repair chain closes
//
// A failed verdict files one bug per failed criterion, `bug` + `src/validation`.
// An ordinary task run works them, and when its working set drains it REOPENS the
// version's validation task — so the reconcile sweep starts another validation
// run and the SAME oracle judges the repair. Bounded by the version's attempt
// allowance and by the identical-digest rule, both of which live in the validation
// workflow.
//
// That single conditional is the ONLY place a `src/*` source routes anything;
// everywhere else a source is provenance. An incident or user fix deploys and the
// standing verdict holds: an incident is not priced like a release. The
// attribution is a LATCHED flag over the run's own polls, never a settle-time
// read — by then the repair issues are closed, and asking "does a CLOSED
// src/validation issue exist" is true forever after the first repair, which would
// reopen the task after every later run without end.
//
// # A failed run halts the work it could not finish
//
// Every FAILED settle stamps `aep:halted` and a comment naming the terminal
// reason on each working-set issue the run could not finish — the recovery bugs it
// filed itself included. The reconcile sweep skips halted issues.
//
// Without it every budget in the platform is defeated: a failed run leaves its
// working set OPEN (the milestone stays open, because the way forward is more work
// in the same version), and the sweep's trigger is "open work of a species, no
// live run, start one" — so the run that just gave up is replaced within a tick by
// a fresh one with fresh budgets, forever. The mark is cleared by a rebuild or by
// a person removing the label. A VALIDATION run halts nothing: its own work is the
// task it closes on every ending, and the repair and conflict issues it leaves
// behind are deliberately a task run's work.
//
// # A milestone closes on a GREEN ENDING, not on a settle
//
// Every terminal state is a statement about the RUN. Only some of them are a
// statement about the VERSION, and delivery.SettleClosesTheMilestone is the one
// place that mapping lives:
//
//	dev succeeded         the milestone STAYS OPEN. The version is deployed and
//	                      unjudged, and the validation task the run just filed is
//	                      what will judge it. The exception is a run that filed
//	                      none — no acceptance oracle, or a plan that minted
//	                      nothing — where nothing is coming and the milestone has
//	                      nothing left to wait for.
//	validation succeeded  the milestone CLOSES. The version has its verdict, and a
//	                      succeeded validation run is a green ending by
//	                      construction: every fatal verdict settles it `failed`.
//	task succeeded        never. A defect fixed inside a version somebody else
//	                      delivered says nothing about that version.
//	failed                never, of any kind: the way forward from a failed
//	                      increment is more work in the same version.
//	cancelled             a DEV run's, because that increment is abandoned.
//	blocked               never. A quota block is a wait somebody else clears.
//
// Closing at the dev run's hand-off is not merely early, it BREAKS the hand-off:
// the validation agent discovers its work with `gh issue list --milestone`, which
// resolves the milestone by title and sees only OPEN ones, so the task would be
// undiscoverable by the only agent meant to work it.
//
// # A cancelled run closes the work it had in flight
//
// The same mechanism, reached from the other ending. A CANCELLED settle comments
// on, stamps `aep:cancelled` on and CLOSES the issues it abandons — because the
// sweep's trigger is that same "open work of a species, no live run, start one",
// so a cancel that only recorded itself would be undone within a tick: the button
// would stop the run and pay for its replacement a minute later.
//
// What it abandons is per species, and the differences are the design:
//
//	dev         everything the INCREMENT was carrying — the working set and the
//	            dispatch gates — and the milestone CLOSES, because the increment is
//	            abandoned. (The halt leaves the gates alone, because a failed run
//	            may be retried in the same version.) NOT the version's validation
//	            task, a handle on software still deployed, and NOT the LEDGER: a
//	            human's unarmed note is never the platform's to close.
//	task        its bugs and conflicts only; the milestone stays OPEN, because the
//	            version it works is the DEPLOYED one and is not being withdrawn.
//	validation  nothing here — its consequence is the task it ADOPTED, closed on
//	            every ending, which is what leaves a cancel before the first read
//	            with the version's task still open for the next trigger.
//
// Nothing is reverted: merged commits stay on `main` and promoted components keep
// serving, so closing the milestone says the INCREMENT was abandoned rather than
// that the release was withdrawn.
//
// Only issues OPEN at cancel time are marked, which is the whole reason the marker
// exists: work a cycle genuinely finished stays closed and unmarked, so the way
// back cannot resurrect it. That way back is a rebuild: a changed spec cuts a new
// tag and plans it fresh, an unchanged one reuses the same milestone, reopens
// exactly the marked set and clears the label. Whether that run also SKIPS the
// planning turn is a second question, asked of the MILESTONE rather than of the
// spec status — it skips only a milestone that holds planned work (see
// RunInput.Rebuild). Both readings are load-bearing: a re-plan over a filled
// milestone mints nothing, because plan dedupe is the title slug against its
// issues in ANY state, while a skip over an EMPTY one hands the loop an empty
// working set. Either way the loop would settle an unbuilt version as delivered.
//
// A dev run therefore **settles at deployed-green having minted the validation
// task, and never validates**. Its verdict column stays EMPTY, which is the
// honest reading of "delivered, not yet judged" — the exception is a project with
// no acceptance oracle, where no task is filed, nothing will ever judge the
// version, and `skipped` says so.
//
// # validation: its own shape, and no working set
//
//	adopt-or-mint the validation task (it is the VERSION's persistent handle)
//	  └─► one agent stage, anchored at that issue, AEP_TASK_KIND=validation
//	        └─► read the verdict at the cycle's OWN merge SHA
//	              ├─ not fatal ──────────────► close the task · succeeded
//	              ├─ unreported, budget left ► re-dispatch inside this workflow
//	              └─ failed ────────────────► one repair issue per failed criterion
//	                                          · close the task · failed
//
// It does not share the cycle loop at all: it polls no working set, and it BUILDS
// AND DEPLOYS NOTHING — its pull request touches only `tests/`, so the merge's
// path diff yields no components and both later stages were already silent
// no-ops for it. Skipping them outright is the honest form of that.
//
// It is started by the reconcile sweep, because an open ARMED `validation`-kind
// issue exists, or by a human asking a shipped version's criteria again. The task is
// closed on EVERY ending, verdict or not: the sweep's trigger IS that open issue,
// so a run that gave up and left it open would be restarted within a tick,
// forever, with nothing outside the workflow able to repair a dead dispatch.
//
// Two facts span validation runs — the version's attempt allowance and the
// previous report's digest — and both are DERIVED from the milestone's own
// validation runs rather than carried, because each attempt is its own execution
// and the previous one's state is gone.
//
// # Division of labour
//
// The supervisor DECIDES; it detects nothing. Every fact about the world
// arrives from `delivery/eventcore` as a run signal, and the supervisor
// re-derives that fact from GROUND TRUTH before acting on it — a signal is a
// wake-up, never evidence.
//
// The DEPLOY is where that rule earns the most. What is serving is derived from
// the cluster and from the builds, never accumulated in workflow state: a
// component's desired release is its newest succeeded build's, its actual
// release is what its binding pins, and the stage writes the difference
// (ADR-0026). State carried across cycles instead would fix nothing that
// matters — a rebuild, a task run or a crash resume starts empty — which is why
// the version state is READ twice per cycle rather than remembered once. That is what makes a lost delivery cost latency
// instead of correctness, and it is why the wait state can be unbounded: the
// cycle-boundary poll and the reconcile sweep both re-read the milestone.
//
// The two packages never import each other. They share the milestone
// vocabulary (labels, run signals, the run and cycle rows, the build-fan-out
// naming) through the delivery ROOT, and they reach each other only through
// ports: `eventcore.RunSignaler`/`RunStarter` inbound, and nothing outbound.
//
// # Internal shape
//
// Separated by FILE, one package, over one shared `loop` struct — it owns the
// signal channels, the budgets and the cycle state, and every workflow wants all
// three. Sub-packages would be LESS protected: `internal/arch` gives siblings a
// blanket import ban with no layer concept, and second-level packages are
// unchecked in both directions.
//
//	activities.go            the single Activities struct — ONE RegisterActivity
//	stage_agent.go           append cycle · dispatch · await landing · re-dispatch
//	stage_build.go           await the merge's fan-out — the CYCLE's verdict only
//	stage_deploy.go          reconcile the version: classify · plan · promote each
//	                         component at its own commit · await Ready · converge
//	stage_boundary.go        the shared loop · poll · dispatchable? · budgets · park
//	workflow_dev.go          gates + plan (skipped on a rebuild) + boundary loop +
//	                         mint the validation task
//	workflow_task.go         boundary loop + reopen the validation task
//	workflow_validation.go   one agent stage + verdict + repair issues + close
//	register.go              RegisterWorkflow per workflow, one RegisterActivity
//	worker.go                one task queue, one worker
//
// ONE `Activities` struct is not a style choice. Temporal registers an activity
// by its reflected METHOD NAME, so two activity structs sharing any method name
// panic the worker at Start — and three structs carved out of one loop would
// share a great many. Three workflows taking method expressions off one struct is
// the only shape that cannot break that way.
//
// # What this package does not hold
//
// No gorm (persistence is reached through ports onto the root repositories),
// no GitHub client, no Kubernetes, and no issue-body parsing. The agent
// dispatch is the root `delivery.MilestoneDispatcher` port, satisfied by the
// coding agent — the supervisor hands over a milestone reference and learns
// what happened from webhooks.
package run
