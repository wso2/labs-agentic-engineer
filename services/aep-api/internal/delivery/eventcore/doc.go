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

// Package eventcore is the delivery domain's EVENT PLANE: it detects, mints
// and signals — the run supervisor decides.
//
// Everything it does is a reaction to ground truth arriving from outside:
// GitHub webhooks (pull requests, issues) and OpenChoreo build terminals. Its
// outputs are exactly three kinds: a squash-merge, an issue minted into a
// milestone, and a signal to the run supervisor.
//
// It DETECTS the issues it files but does not write them: every mint goes
// through delivery.IssueWriter (Ports.Writer), the domain's one issue-write
// surface, so this package holds no private opinion about labels or dedupe.
//
// Two writes here are triggered from OUTSIDE — by the supervisor, through a port,
// because only the run knows the fact behind them — while the prose, the labels
// and the dedupe stay this package's: the deploy-fix mint (a ReleaseBinding that
// never became Ready delivers no webhook) and the HALT of a failed run's
// unfinished work (halt.go), without which the reconcile sweep restarts what a
// run just gave up on, with fresh budgets, forever.
//
// # No Temporal
//
// This package imports no workflow engine, deliberately. The dependency
// direction of the architecture — supervisor consumes the event plane, never
// the reverse — is expressed here as a package boundary: the supervisor is
// reachable only through the RunSignaler and RunStarter ports, so no decision
// about a run's loop can be smuggled in here. What lands in this package is
// only ever "this happened", never "therefore do that".
//
// # The run row IS the gate
//
// Every handler resolves a milestone RUN ROW first and returns without side
// effects when there is none. That is not defensive coding, it is the safety
// property that lets this package be fully wired and fully tested while the
// legacy pull_request handlers are still live: the webhook router runs ALL
// handlers registered for a key, and two auto-mergers racing on one PR would
// be a genuine disaster. Nothing creates milestone run rows until the plan
// path mints them, so in production every handler here is inert — provably,
// by a lookup, rather than by a feature flag somebody can flip early.
//
// The rule holds even where a run is deliberately absent. Adoption of a bare
// issue resolves the DEPLOYED version's milestone through run rows; the red-main
// incident path does the same. No run rows, no milestone, no write.
//
// # Idempotency
//
// A webhook delivery whose handler failed is REDELIVERED and re-run (the
// receiver's Persist reports the row as neither fresh nor processed, and
// dispatch happens again), so every handler here must be safe to run twice.
// Three mechanisms carry that weight, and none of them is a "have we seen
// this" table:
//
//   - merging re-reads the live pull request first, so a second delivery of a
//     merged PR merges nothing;
//   - minting passes a DedupeKey — composed from the domain's frozen key
//     vocabulary and written through delivery.IssueWriter — which resolves to
//     an existing OPEN issue carrying the derived dedupe label instead of
//     filing a second one;
//   - triggering a build counts the WorkflowRuns OpenChoreo already holds for
//     (component, commit) and refuses to exceed the allowance — which is the
//     SAME mechanism as the automatic re-trigger budget, so idempotency and
//     the budget can never disagree.
//
// # Echo suppression is issues-only
//
// The issues.* handlers drop deliveries whose sender is the platform's own
// GitHub identity: every label, comment and milestone assignment the platform
// writes fires an issues.* delivery straight back at it.
//
// It is deliberately NOT applied to pull_request.*. In App mode the coding
// runner opens its PR as <slug>[bot] — the SAME login as the platform identity
// — so suppressing self-sender PR deliveries would drop the runner's own
// pull_request.opened and strand the run waiting for a PR that already exists.
// The PR is the runner's report, not a platform projection, so its deliveries
// are always acted on.
package eventcore
