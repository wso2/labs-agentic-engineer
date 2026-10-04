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

package task

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/taskplan"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// PlanService assembles the plan-turn context, runs the Plan turn in the
// org's AE Studio pod (07 §12), and mints the Tasks its task-op events
// describe. One turn per project is the pod's lock: a different turn running
// is aestudiotools.ErrTurnInProgress, which the planning activity retries.
type PlanService struct {
	repos    sourcecontrol.ProjectRepoRows
	versions VersionReader
	turns    aestudiotools.Turns
	issues   IssueClient
	writer   *delivery.IssueWriter
	// paths resolves each component's appPath for the planned Task's body.
	// Optional; nil omits the App Path line.
	paths ComponentPathReader
}

// SetComponentPaths wires the design's component → appPath reader so a planned
// Task's body carries the App Path the agent works in. A nil reader is a
// documented no-op (the line is omitted).
func (s *PlanService) SetComponentPaths(r ComponentPathReader) { s.paths = r }

// NewPlanService wires the plan service. turns runs the Plan turn in the
// org's pod; issues is the READ half the turn's context is assembled from and
// writer is the domain's issue-write surface, which is what the tap mints
// each planned Task through.
func NewPlanService(repos sourcecontrol.ProjectRepoRows, versions VersionReader, turns aestudiotools.Turns, issues IssueClient, writer *delivery.IssueWriter) *PlanService {
	return &PlanService{repos: repos, versions: versions, turns: turns, issues: issues, writer: writer}
}

// PlanIntoMilestone plans the version's Tasks straight into its milestone and
// waits for the turn to finish. Every issue the turn mints joins the milestone
// AT CREATION (1+N calls) with the `aep` working-set label and a prose body.
//
// It is the plan path's half of the build click. A write the tap could not land
// is an error rather than a warning: the run this plan feeds is about to be
// supervised against the milestone's contents, so a silently short plan would
// become a run that settles early. So is a turn the pod ends failed, one that
// goes silent, and one whose stream breaks off.
//
// Pre-stream failures are typed: ErrProjectRepoNotFound, ErrNoSpecVersion,
// or the pod's (aestudiotools.ErrTurnInProgress, ErrAEStudioUnavailable,
// ErrAEStudioMisconfigured, ErrAEStudioAbsent, *StatusError).
func (s *PlanService) PlanIntoMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) error {
	// A row that is missing, or not provisioned yet, has nothing to plan in.
	ref, row, err := sourcecontrol.RepoRefFor(ctx, s.repos, orgID, projectID)
	if errors.Is(err, sourcecontrol.ErrRepoNotFound) || (err == nil && row.Status != "" && row.Status != "ready") {
		return ErrProjectRepoNotFound
	}
	if err != nil {
		return err
	}

	// Gate: a versioned (tagged) spec must exist (§6, build-first). The tag is
	// cut by the build endpoint AFTER the whole-spec hard gate, so its presence
	// certifies a buildable requirements+design pair.
	//
	// A version is whatever the user named it (ADR-0030), so membership is the
	// tag's own annotation, never the shape of its name — this gate used to
	// count `v<N>` matches and refused every named version outright.
	//
	// A read FAILURE is not an answer: reporting "build the project first" for
	// a fetch that did not complete sends the reader to re-do the one thing
	// they already did.
	versions, err := s.versions.ListSpecVersionTags(ctx, orgID, projectID)
	if err != nil {
		return fmt.Errorf("list spec versions: %w", err)
	}
	if versions == nil || versions.Latest == "" {
		return ErrNoSpecVersion
	}

	// Existing Tasks → turn context + tap preload + dedupe slugs. These are
	// platform state (GitHub issues), not repository files, so they ride in
	// the turn request.
	//
	// A version plans FRESH from the new spec (§6: supersede, no carry-over), so
	// the only context is the milestone's OWN issues — empty on a first pass and
	// non-empty only on a re-plan or a crash re-run, exactly the cases dedupe
	// exists for.
	contextFiles := map[string]string{}
	preload, slugs := s.assembleMilestoneTasks(ctx, orgID, projectID, milestoneNumber, contextFiles)
	// The tag's story scope (#369): the PRD's story set drives DELTA
	// planning — stories already covered by existing Tasks (their platform
	// stamps) need no new work. Best-effort: a scope-less read degrades to
	// the legacy plan-everything behavior.
	scope := spec.BuildScope{}
	if sc, serr := s.versions.BuildScopeAtTag(ctx, orgID, projectID, versions.Latest); serr == nil {
		scope = sc
	} else {
		slog.WarnContext(ctx, "plan: story scope read failed — planning without milestone scope",
			"project", projectID, "tag", versions.Latest, "error", serr)
	}
	covered := map[int]bool{}
	for _, p := range preload {
		for _, n := range delivery.ParseServesStories(p.Body) {
			covered[n] = true
		}
	}
	// Freeze the set of issue numbers the agent actually received as context: an
	// updateTask{issueNumber} ref is fenced to it (a hallucinated / out-of-context
	// number must never be written — plan_tap.resolveRef).
	contextNumbers := make(map[int]bool, len(preload))
	for n := range preload {
		contextNumbers[n] = true
	}

	// The stream lives under ctx, so a cancelled activity stops reading (the
	// pod runs the turn on; a caller going away only detaches). The cancel is
	// also the tap's idle abort, and the deferred call releases the stream's
	// open response body whatever path returns.
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A fresh turn id per plan: a Plan is one-shot, never resumed. No credit:
	// no run row records who asked for the build (C7).
	events, err := s.turns.StartTurn(streamCtx, ref, aestudiotools.TurnRequest{
		TurnID:      uuid.NewString(),
		Project:     projectID,
		Kind:        aestudiotools.TurnKindPlan,
		Scope:       planScopeFor(scope, covered),
		TaskContext: planContextFor(contextFiles),
	})
	if err != nil {
		return err
	}

	tap := newPlanTap(context.WithoutCancel(ctx), orgID, projectID, s.issues, s.writer)
	tap.milestone = milestoneNumber
	tap.componentStories = scope.ComponentStories
	tap.appPaths = s.componentPaths(ctx, orgID, projectID)
	tap.state = preload
	tap.existingSlugs = slugs
	tap.contextNumbers = contextNumbers

	if err := tap.Stream(events, cancel); err != nil {
		return err
	}
	if tap.failures > 0 {
		return fmt.Errorf("plan: %d issue write(s) failed for milestone %d", tap.failures, milestoneNumber)
	}
	return nil
}

// componentPaths reads each component's appPath, lowercased for lookup.
// Best-effort: a design-read hiccup costs the App Path line, never the plan.
func (s *PlanService) componentPaths(ctx context.Context, orgID, projectID string) map[string]string {
	if s.paths == nil {
		return nil
	}
	raw, err := s.paths.ComponentPaths(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "plan: read component app paths failed", "project", projectID, "error", err)
		return nil
	}
	out := make(map[string]string, len(raw))
	for name, path := range raw {
		out[strings.ToLower(strings.TrimSpace(name))] = path
	}
	return out
}

// assembleMilestoneTasks preloads the milestone's OWN issues: their title slugs
// are the additive-only dedupe set, and each renders as a context file so a
// re-plan can see (and updateTask) what the version already holds. Best-effort:
// a read failure plans as if the milestone were empty, which at worst re-mints
// an issue the human can close.
func (s *PlanService) assembleMilestoneTasks(ctx context.Context, orgID, projectID string, milestoneNumber int, files map[string]string) (map[int]plannedTask, map[string]bool) {
	preload := map[int]plannedTask{}
	slugs := map[string]bool{}

	issues, err := s.issues.ListMilestoneIssues(ctx, orgID, projectID, sourcecontrol.MilestoneIssuesFilter{
		Number: milestoneNumber,
		State:  "all",
	})
	if err != nil {
		slog.WarnContext(ctx, "plan: list milestone issues failed", "project", projectID, "milestone", milestoneNumber, "error", err)
		return preload, slugs
	}
	for _, issue := range issues {
		if slug := titleSlug(issue.Title); slug != "" {
			slugs[slug] = true
		}
		// The planner's context set is the DEV working set and nothing else: a
		// gate is the platform's, the validation task is the validation loop's,
		// and a ledger-only human issue is not a Task at all — none of them
		// belong in the set an updateTask ref is fenced to.
		if !delivery.InDevWorkingSet(issue.Labels) {
			continue
		}
		if !strings.EqualFold(issue.State, "open") {
			continue
		}
		path, content := taskplan.RenderTaskContextFile(taskplan.TaskContextFile{
			IssueNumber: issue.Number,
			Title:       issue.Title,
			Body:        issue.Body,
		})
		files[path] = content
		preload[issue.Number] = plannedTask{Body: issue.Body}
	}
	return preload, slugs
}

// planScopeFor states the milestone's story scope (#369) as facts: which
// stories the existing Tasks already cover, and which this plan must.
// Platform-computed — the model never decides coverage, and never sees this as
// anything but the section the design agent renders from it. nil when the
// tag carries no readable stories.
func planScopeFor(scope spec.BuildScope, covered map[int]bool) *aestudiotools.PlanScope {
	if len(scope.InScope) == 0 {
		return nil
	}
	stories := make([]aestudiotools.PlanStory, 0, len(scope.InScope))
	for _, n := range scope.InScope {
		stories = append(stories, aestudiotools.PlanStory{
			Number:  n,
			Title:   scope.StoryTitles[n],
			Covered: covered[n],
		})
	}
	return &aestudiotools.PlanScope{Tag: scope.Tag, Stories: stories}
}

// planContextFor carries the milestone's existing-Task renders as facts, sorted
// by path so the same inputs always produce the same turn. They keep their
// historical tasks/<n>.md names so the model's mental layout is unchanged.
func planContextFor(files map[string]string) []aestudiotools.PlanContextFile {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]aestudiotools.PlanContextFile, 0, len(paths))
	for _, p := range paths {
		out = append(out, aestudiotools.PlanContextFile{Path: p, Body: files[p]})
	}
	return out
}
