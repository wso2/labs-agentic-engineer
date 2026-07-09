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

// Package task is the GitHub-facing half of the Task/Execution split
// (docs/design/tasks-github-native.md §1, §10.1). A Task IS a GitHub issue —
// its labels, body (machine block + rationale + scope), open/closed state, and
// linked PRs. This package reads Tasks live from GitHub joined with their
// executions to compute derived status (reads.go), plans Tasks on the agents
// service and executes the tool-call stream against GitHub mid-flight (plan.go /
// plan_tap.go), stamps command labels (commands.go), composes issue bodies over
// the machine block (issue_compose.go), and handles issues.* webhooks — task
// birth, block validation/repair, close/reopen, attention (events.go). It never
// stores a Task status (that is derived) and never imports the platform-owned
// execution half — the §1 split is a package boundary (arch-locked).
package task

import "errors"

var (
	// ErrTaskNotFound is returned when no Task issue with the given number
	// exists in the project (or it is not a Task — no aep:task marker). The HTTP
	// edge maps it to 404.
	ErrTaskNotFound = errors.New("task not found")
	// ErrProjectRepoNotFound is returned when the project has no provisioned git
	// repository yet. Mapped to 404.
	ErrProjectRepoNotFound = errors.New("project repository not found")
	// ErrNoApprovedDesign is returned when a plan turn is requested but the
	// project has no approved (tagged) design version. Mapped to 400 (the
	// approve-first gate, as with design generation).
	ErrNoApprovedDesign = errors.New("planning requires an approved (tagged) design version")
	// ErrPlanInProgress is returned when a plan turn is already running for the
	// project (the one-active-plan-turn invariant, §6). Mapped to 409
	// {code:"plan_in_progress"}.
	ErrPlanInProgress = errors.New("a plan turn is already running for this project")
	// ErrIssueClosed is returned when execute is requested on a closed issue
	// (closed = no new dispatches, §4). Mapped to 409.
	ErrIssueClosed = errors.New("issue is closed")
	// ErrNoAnthropicKey is returned pre-stream when the org has no Anthropic key.
	// Mapped to 400.
	ErrNoAnthropicKey = errors.New("organization has no Anthropic API key configured")
	// ErrSkillsRepoUnavailable means the org's _skills repo (the plan turn's
	// SkillsRef source) could not be resolved — its row is missing or
	// unprovisionable, or the backing repo is gone/unreachable (live incident:
	// the GitHub repo was deleted externally while its git_repositories row
	// lingered). Mapped to a LOGGED 503 with a clear message instead of an
	// opaque 500. Recovery is a manual operator action today: delete the stale
	// `_skills` git_repositories row — the next resolve re-provisions and
	// re-seeds the repo.
	ErrSkillsRepoUnavailable = errors.New("org skills repository unavailable")
)
