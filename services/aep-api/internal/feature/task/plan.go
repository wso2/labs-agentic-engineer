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
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/taskplan"
)

// planInstruction is the steering directive the BFF composes server-side (§9.1:
// the request body is empty; the BFF assembles the whole generation directive).
// The design/requirements content is NOT inlined anymore — the agents service
// reads it from the workspace snapshot (shared-volume-clone D9); the existing-
// task renders + lineage diffs are appended to this instruction (they are
// platform state, not repository files, so they cannot ride in the snapshot —
// see renderPlanContext).
const planInstruction = "Plan the implementation Tasks for this project. Load the task-planning skill and follow it: create one Task per design component with planTask, wire dependsOn by component name, and write each Task's body with updateTask in the same turn. The design is under specs/design/ and the requirements under specs/requirements/. Existing open Tasks (if any) are listed at the end of this message for reference — add Tasks ONLY for components they do not cover, and do not recreate or update the listed Tasks in this turn. Never invent a component the design does not define."

// PlanService assembles the plan-turn context, starts the upstream turn, and
// hands back a PlanSession the HTTP edge streams. One active plan turn per
// project is enforced by an in-process in-flight set (§6) plus the upstream 409
// passthrough. (This guard is a separate concern from genai's durable
// one-active-turn row — plan turns commit nothing, so the in-process set is
// enough.)
type PlanService struct {
	repos      RepoResolver
	versions   VersionReader
	git        GitReader
	keys       AnthropicKeyResolver
	client     TurnClient
	issues     IssueClient
	snapshots  gitrepo.SnapshotProvider
	skillsRepo SkillsRepoResolver

	inflight sync.Map // projectKey → struct{}
}

// NewPlanService wires the plan service. git/snapshots/skillsRepo back the
// workspace dispatch (snapshot refs + lineage diffs).
func NewPlanService(repos RepoResolver, versions VersionReader, git GitReader, keys AnthropicKeyResolver, client TurnClient, issues IssueClient, snapshots gitrepo.SnapshotProvider, skillsRepo SkillsRepoResolver) *PlanService {
	return &PlanService{repos: repos, versions: versions, git: git, keys: keys, client: client, issues: issues, snapshots: snapshots, skillsRepo: skillsRepo}
}

// PlanSession is a started plan turn: the raw upstream SSE body, the tap that
// executes tool frames against GitHub, and a release for the in-flight lock.
type PlanSession struct {
	body    io.ReadCloser
	tap     *planTap
	release func()
}

// Stream forwards the turn to w verbatim while the tap performs the GitHub
// writes, then releases the per-project in-flight lock. Survives client
// disconnect (the tap drains upstream).
func (s *PlanSession) Stream(w io.Writer, flush func()) {
	defer s.release()
	s.tap.Stream(s.body, w, flush)
}

// StartPlan assembles context and starts the plan turn. Pre-stream failures are
// typed errors (ErrNoApprovedDesign, ErrNoAnthropicKey, ErrProjectRepoNotFound,
// ErrPlanInProgress) or an *agentsvc.UpstreamError; on any pre-stream failure
// the in-flight lock is released before returning.
func (s *PlanService) StartPlan(ctx context.Context, orgID, projectID string) (*PlanSession, error) {
	key := orgID + "/" + projectID
	if _, loaded := s.inflight.LoadOrStore(key, struct{}{}); loaded {
		return nil, ErrPlanInProgress
	}
	release := func() { s.inflight.Delete(key) }
	session, err := s.startPlanLocked(ctx, orgID, projectID, release)
	if err != nil {
		release()
		return nil, err
	}
	return session, nil
}

func (s *PlanService) startPlanLocked(ctx context.Context, orgID, projectID string, release func()) (*PlanSession, error) {
	// Resolved directly (not via resolveProjectRepo): the plan turn keys on
	// the workspace ref, so it needs no owner/name split — only a ready row.
	repo, err := s.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		if errors.Is(err, gitrepo.ErrRepoNotFound) {
			return nil, ErrProjectRepoNotFound
		}
		return nil, err
	}
	if repo == nil {
		return nil, ErrProjectRepoNotFound
	}
	// The plan turn reads its context from a workspace snapshot — a repo that
	// is not ready yet cannot back one.
	if repo.Status != "" && repo.Status != "ready" {
		return nil, ErrProjectRepoNotFound
	}

	// Gate: an approved (tagged) design version must exist (§6, approve-first).
	designVersions, err := s.versions.ListDesignVersions(ctx, orgID, projectID)
	if err != nil || len(designVersions) == 0 {
		return nil, ErrNoApprovedDesign
	}
	currentDesignTag := designVersions[0].Tag
	var currentSpecTag string
	if reqVersions, verr := s.versions.ListRequirementsVersions(ctx, orgID, projectID); verr == nil && len(reqVersions) > 0 {
		currentSpecTag = reqVersions[0].Tag
	}

	apiKey, err := s.keys(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("resolve anthropic key: %w", err)
	}
	if apiKey == "" {
		return nil, ErrNoAnthropicKey
	}

	ref, err := gitrepo.ResolveWorkspaceRef(ctx, s.git.Resolver(), orgID, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace ref: %w", err)
	}

	// Existing open Tasks → instruction context + tap preload + lineage diff.
	// These are platform state (GitHub issues), not repository files, so they
	// ride in the instruction — the snapshot carries only committed content.
	contextFiles := map[string]string{}
	preload, existingKeys, olderTags := s.assembleExistingTasks(ctx, orgID, projectID, currentDesignTag, contextFiles)
	s.appendLineageDiffs(ctx, ref, currentDesignTag, olderTags, contextFiles)
	// Freeze the set of issue numbers the agent actually received as context: an
	// updateTask{issueNumber} ref is fenced to it (a hallucinated / out-of-context
	// number must never be written — plan_tap.resolveRef).
	contextNumbers := make(map[int]bool, len(preload))
	for n := range preload {
		contextNumbers[n] = true
	}

	// Workspace snapshot refs (D9): the design/requirements context is read
	// from snapshots/<baseRef>/; the task-planning skill is a flow skill
	// seeded into the org's _skills repo (Phase 1), read from its snapshot.
	ws := s.git.Workspace()
	baseRef, err := ws.Head(ctx, ref, "")
	if err != nil {
		return nil, fmt.Errorf("resolve base ref: %w", err)
	}
	// Skills resolve failures are typed: both arms mean the org's _skills repo
	// is unusable right now (row missing/unprovisionable, or the backing repo
	// gone/unreachable — e.g. deleted externally under a lingering row). The
	// edge maps this to a logged 503 rather than an opaque 500.
	skillsRow, err := s.skillsRepo(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve repo row: %w", ErrSkillsRepoUnavailable, err)
	}
	skillsRepoRef := gitrepo.WorkspaceRefFor(orgID, skillsRow, ref.Cred)
	skillsRef, err := ws.Head(ctx, skillsRepoRef, "")
	if err != nil {
		return nil, fmt.Errorf("%w: resolve head: %w", ErrSkillsRepoUnavailable, err)
	}
	if err := s.snapshots.Ensure(ctx, ref, baseRef); err != nil {
		return nil, fmt.Errorf("ensure repo snapshot: %w", err)
	}
	if err := s.snapshots.Ensure(ctx, skillsRepoRef, skillsRef); err != nil {
		return nil, fmt.Errorf("ensure skills snapshot: %w", err)
	}

	// A fresh, namespaced conversation id per plan turn — plan turns are one-shot
	// (never rehydrated), so the id only needs to be unique within the tenant.
	conversationID := agentsvc.ConversationID(orgID, projectID, "task-plan",
		strconv.FormatInt(time.Now().UnixNano(), 10))

	// Detached context so the turn drains even if the client disconnects (§6).
	detached := context.WithoutCancel(ctx)
	body, err := s.client.Turn(detached, conversationID, orgID, apiKey, agentsvc.TurnRequest{
		Instruction: planInstruction + renderPlanContext(contextFiles),
		Workspace: agentsvc.WorkspaceRef{
			ConversationID: conversationID,
			TurnID:         uuid.NewString(),
			RepoSlug:       ref.RepoSlug,
			Ref:            baseRef,
			SkillsRef:      skillsRef,
		},
		Toolset: "task-plan",
	})
	if err != nil {
		return nil, err // typed *agentsvc.UpstreamError (409 → plan_in_progress passthrough)
	}

	tap := &planTap{
		ctx:            detached,
		orgID:          orgID,
		projectID:      projectID,
		specTag:        currentSpecTag,
		designTag:      currentDesignTag,
		issues:         s.issues,
		state:          preload,
		existingKeys:   existingKeys,
		contextNumbers: contextNumbers,
		titleToNumber:  map[string]int{},
		createdKeys:    map[string]bool{},
	}
	return &PlanSession{body: body, tap: tap, release: release}, nil
}

// assembleExistingTasks renders each open Task as a tasks/<n>.md context file
// (with machine-block facts), preloads the tap state for updateTask{issueNumber},
// collects the dedupe key set, and reports the distinct older design tags whose
// lineage diff the assembler should include (§6).
func (s *PlanService) assembleExistingTasks(ctx context.Context, orgID, projectID, currentDesignTag string, files map[string]string) (map[int]taskState, map[string]bool, map[string]bool) {
	preload := map[int]taskState{}
	existingKeys := map[string]bool{}
	olderTags := map[string]bool{}

	issues, err := s.issues.ListIssues(ctx, orgID, projectID, []string{taskmeta.LabelMarker})
	if err != nil {
		slog.WarnContext(ctx, "plan: list existing tasks failed", "error", err)
		return preload, existingKeys, olderTags
	}
	for _, issue := range issues {
		if !strings.EqualFold(issue.State, "open") {
			continue
		}
		block, human, berr := taskmeta.ParseBody(issue.Body)
		if berr != nil {
			continue // mangled/missing block — the events handler flags it
		}
		cf := taskplan.TaskContextFile{
			IssueNumber: issue.Number,
			Component:   block.Component,
			Title:       issue.Title,
			DependsOn:   block.DependsOn,
			Origin:      block.Origin,
			SpecTag:     block.SpecTag,
			DesignTag:   block.DesignTag,
			Body:        human.Body,
		}
		path, content := taskplan.RenderTaskContextFile(cf)
		files[path] = content

		preload[issue.Number] = taskState{block: block, human: human}
		if block.Key != "" {
			existingKeys[block.Key] = true
		}
		if block.DesignTag != "" && block.DesignTag != currentDesignTag {
			olderTags[block.DesignTag] = true
		}
	}
	return preload, existingKeys, olderTags
}

// appendLineageDiffs includes the spec/design delta between each older lineage
// tag and the current design tag so incremental planning reasons over the real
// change (§6 — Workspace.Diff on the local mirror, was GitHub CompareRefs).
func (s *PlanService) appendLineageDiffs(ctx context.Context, ref gitrepo.RepoRef, currentDesignTag string, olderTags map[string]bool, files map[string]string) {
	if s.git == nil || len(olderTags) == 0 {
		return
	}
	for oldTag := range olderTags {
		cmp, cerr := s.git.Workspace().Diff(ctx, ref, oldTag, currentDesignTag)
		if cerr != nil {
			slog.WarnContext(ctx, "plan: lineage compare failed", "from", oldTag, "to", currentDesignTag, "error", cerr)
			continue
		}
		files["context/lineage-diff-"+oldTag+".md"] = renderCompare(oldTag, currentDesignTag, cmp)
	}
}

// renderPlanContext renders the existing-task + lineage-diff context files as
// deterministic instruction sections. They keep their historical
// tasks/<n>.md / context/… names so the model's mental layout is unchanged.
func renderPlanContext(files map[string]string) string {
	if len(files) == 0 {
		return ""
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var sb strings.Builder
	sb.WriteString("\n\n## Existing open Tasks and lineage diffs (reference)\n")
	for _, p := range paths {
		fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", p, files[p])
	}
	return sb.String()
}

// renderCompare renders a compare result as a compact markdown summary for the
// planner's context (changed files + hunks).
func renderCompare(from, to string, cmp *gitrepo.CompareResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Lineage diff: %s → %s\n\n", from, to)
	fmt.Fprintf(&sb, "Status: %s (%d commit(s), +%d/-%d)\n\n", cmp.Status, cmp.TotalCommits, cmp.AheadBy, cmp.BehindBy)
	if cmp.Truncated {
		sb.WriteString("> NOTE: this compare was capped at a file limit — the change list below is INCOMPLETE. Treat components not shown as possibly-changed and re-verify against the current design rather than assuming they are untouched.\n\n")
	}
	for _, f := range cmp.Files {
		fmt.Fprintf(&sb, "## %s (%s, +%d/-%d)\n", f.Filename, f.Status, f.Additions, f.Deletions)
		if f.Patch != "" {
			sb.WriteString("```diff\n")
			sb.WriteString(f.Patch)
			if !strings.HasSuffix(f.Patch, "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString("```\n")
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}
