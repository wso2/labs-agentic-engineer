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

package execution

import (
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

// Reason sentinels stamped on execution rows. The closed-unmerged sentinel
// (taskmeta.ReasonPRClosedUnmerged) and the derived PR-state reconstruction
// (taskmeta.PRStateFromFacts) live in the shared encoding, so the read path can
// reconstruct GitHub PR state from the executions rows without a live PR query
// (§8 keeps reads live but bounded).
const (
	// reasonNoExecutor annotates canceled rows for the read path and the
	// timeline.
	reasonNoExecutor = "no executor for class"

	// reasonPROpenPrefix + the PR number is stamped on a succeeded coding row
	// when its PR opens, so the sweep can GET the live PR state to reconcile a
	// missed close/merge webhook (§5 — PR state is native GitHub truth, healed
	// by the sweep, never a DB opinion).
	reasonPROpenPrefix = "pr#"
)

// OpenPRNumber returns the PR number a succeeded coding row claims is open, or
// 0 when the row is not a succeeded coding row with a pr# reason. Exported so
// the orchestration TaskDriver (§R3.5 auto-merge) can recover a task's open PR
// number from its latest coding Execution without re-parsing GitHub itself.
func OpenPRNumber(row *models.Execution) int {
	if row == nil || row.Kind != string(taskmeta.KindCoding) || row.Status != string(taskmeta.ExecSucceeded) {
		return 0
	}
	rest, ok := strings.CutPrefix(row.Reason, reasonPROpenPrefix)
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(rest)
	return n
}

// factsFromIssue parses a live GitHub issue into TaskFacts + its machine block.
// ok is false when the issue is not a Task (no aep:task marker) or its block is
// absent/mangled — the funnel/sweep skip those (unlabeled issues are inert;
// mangled blocks are the events handler's aep:attention responsibility).
func factsFromIssue(issue gitrepo.IssueInfo, orgID, projectID, repoFullName string) (TaskFacts, taskmeta.Block, bool) {
	labels := taskmeta.ParseLabels(issue.Labels)
	if !labels.IsTask {
		return TaskFacts{}, taskmeta.Block{}, false
	}
	block, err := taskmeta.ParseBlock(issue.Body)
	if err != nil {
		return TaskFacts{}, taskmeta.Block{}, false
	}
	f := TaskFacts{
		OrgID:       orgID,
		ProjectID:   projectID,
		Repo:        repoFullName,
		IssueNumber: issue.Number,
		IssueURL:    issue.URL,
		Title:       issue.Title,
		Class:       labels.Class,
		Component:   block.Component,
		Operation:   block.Operation,
		DependsOn:   block.DependsOn,
		Origin:      block.Origin,
		SpecTag:     block.SpecTag,
		DesignTag:   block.DesignTag,
		IssueOpen:   strings.EqualFold(issue.State, "open"),
		HoldActive:  labels.Hold,
	}
	return f, block, true
}

// deriveStatus fuses a Task's live GitHub facts with its executions into the
// computed, never-stored status (§4). execs is latest-per-kind; the PR state is
// reconstructed from the rows (taskmeta.PRStateFromFacts).
func deriveStatus(f TaskFacts, execs map[string]*models.Execution) taskmeta.DerivedStatus {
	facts := repositories.ExecutionFacts(execs)
	gh := taskmeta.GitHubFacts{
		IssueOpen:   f.IssueOpen,
		HoldPresent: f.HoldActive,
		PR:          taskmeta.PRStateFromFacts(facts),
	}
	return taskmeta.Derive(gh, facts)
}
