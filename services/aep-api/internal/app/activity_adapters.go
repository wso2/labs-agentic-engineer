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

package app

import (
	"context"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/activityvocab"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/projects"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// This file wires the activity-feed producers (issue #239) onto the projects
// domain's activity service. They are app-root twins-mappers: delivery and spec
// must not import projects — projects already imports delivery — so each side
// speaks its own type and the mapping lives here.

// buildActivityRecorder implements build.SpecPublishedRecorder: the user
// published a spec version and kicked off the build. Actor = the signed-in
// user (email id → the console renders "You" for the author).
type buildActivityRecorder struct{ svc *projects.ActivityService }

func (r buildActivityRecorder) RecordSpecPublished(ctx context.Context, orgID, projectName, tag string) {
	email, name := userIdentityFromContext(ctx)
	r.svc.Record(ctx, projects.ActivityInput{
		OrgID:      orgID,
		ProjectID:  projectName,
		Type:       activityvocab.TypeSpecPublished,
		ActorKind:  activityvocab.ActorUser,
		ActorID:    email,
		ActorName:  name,
		Tag:        tag,
		DedupKey:   "spec:" + projectName + ":" + tag + ":published",
		OccurredAt: time.Now().UTC(),
	})
}

// specAuthorship bridges the async gap between a room-scoped genai turn
// finishing and the collab committer later flushing that turn's doc edits to git
// (issue #239). A room turn writes into the shared doc and commits nothing; the
// committer flushes via files/apply, under the user's own token — so at the git
// layer the agent's work is indistinguishable from a manual edit and would be
// mis-attributed to the user. The turn is the only place agent authorship is
// still known, so it marks the exact paths its manifest touched; an apply whose
// commit contains a marked path is carrying agent text, claims those marks, and
// suppresses its (wrong) user line, because the turn already recorded the agent
// line.
//
// Marks are per path, not per project, because the committer holds agent-marked
// markdown until the session-end force flush: a user's own edit can land in an
// interim commit between the turn and that flush, and a project-wide mark would
// suppress the user's line and then label the eventual agent commit as the
// user — both halves inverted. Path scoping keeps the two flushes independent:
// the user's disjoint commit records as the user, the agent's paths stay marked
// until they actually land. A commit that mixes a marked path with user edits is
// one commit and one feed line, attributed to the agent (the agent line exists;
// a second line would double-report the commit). The state is in-process,
// matching the single aep-api replica of the deployment; a restart between a
// turn and its flush at worst mislabels that one flush as manual.
type specAuthorship struct {
	mu sync.Mutex
	// pending maps orgID+"\x00"+projectID to the agent-edited paths whose
	// commit has not yet landed.
	pending map[string]map[string]struct{}
}

func specKey(orgID, projectID string) string { return orgID + "\x00" + projectID }

// markAgent records the paths an agent turn edited that are still awaiting the
// committer's flush. No paths, no mark — a committed (non-room) turn lands its
// own commit and needs no suppression.
func (a *specAuthorship) markAgent(orgID, projectID string, paths []string) {
	if len(paths) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = make(map[string]map[string]struct{})
	}
	key := specKey(orgID, projectID)
	set := a.pending[key]
	if set == nil {
		set = make(map[string]struct{})
		a.pending[key] = set
	}
	for _, p := range paths {
		set[p] = struct{}{}
	}
}

// claimAgent reports whether the commit now landing (touching paths) carries
// agent-authored edits, consuming the matched marks so a later commit to the
// same path records normally.
func (a *specAuthorship) claimAgent(orgID, projectID string, paths []string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	set := a.pending[specKey(orgID, projectID)]
	if len(set) == 0 {
		return false
	}
	claimed := false
	for _, p := range paths {
		if _, ok := set[p]; ok {
			delete(set, p)
			claimed = true
		}
	}
	if len(set) == 0 {
		delete(a.pending, specKey(orgID, projectID))
	}
	return claimed
}

// filesActivityRecorder implements files.SpecUpdatedRecorder: an apply landed a
// real commit, so the project's spec changed. This is the path the collab
// session flush and the spec editor's save share. A commit that contains an
// agent turn's doc edits is already on the feed as the agent (recorded at turn
// finish), so it is suppressed here; a genuine manual edit falls through and is
// attributed to the signed-in user. Deduped by commit sha.
type filesActivityRecorder struct {
	svc        *projects.ActivityService
	authorship *specAuthorship
}

func (r filesActivityRecorder) RecordSpecUpdated(ctx context.Context, orgID, projectName, commitSHA string, paths []string) {
	if r.authorship.claimAgent(orgID, projectName, paths) {
		return // agent-authored — the turn already recorded the agent line.
	}
	email, name := userIdentityFromContext(ctx)
	r.svc.Record(ctx, projects.ActivityInput{
		OrgID:      orgID,
		ProjectID:  projectName,
		Type:       activityvocab.TypeSpecUpdated,
		ActorKind:  activityvocab.ActorUser,
		ActorID:    email,
		ActorName:  name,
		DedupKey:   "apply:" + commitSHA,
		OccurredAt: time.Now().UTC(),
	})
}

// turnActivityRecorder implements spec.TurnActivityRecorder: a genai turn
// authored spec changes. A turn is the agent working, so the actor is always the
// agent (the console renders "Spec agent updated the spec"). A room turn also
// marks its edited paths as agent-authored so the committer's later flush of
// those edits is suppressed rather than double-recorded as a manual edit (a
// committed turn passes no paths — its commit is its own). Deduped by turn id.
type turnActivityRecorder struct {
	svc        *projects.ActivityService
	authorship *specAuthorship
}

func (r turnActivityRecorder) RecordSpecUpdated(ctx context.Context, orgID, projectID, turnID, title string, editedPaths []string) {
	r.authorship.markAgent(orgID, projectID, editedPaths)
	r.svc.Record(ctx, projects.ActivityInput{
		OrgID:      orgID,
		ProjectID:  projectID,
		Type:       activityvocab.TypeSpecUpdated,
		ActorKind:  activityvocab.ActorAgent,
		ActorID:    "spec-agent",
		ActorName:  "Spec agent",
		Title:      title,
		DedupKey:   "turn:" + turnID + ":committed",
		OccurredAt: time.Now().UTC(),
	})
}

// userIdentityFromContext returns (email, displayName) for the signed-in user,
// for stamping a user-actor activity event. The email doubles as the stable
// actor id (it is what the console's #130 "You" comparison keys on).
//
// The verified claims projection (auth.Claims / jwtassertion.TokenClaims) never
// carries email or display name — only sub/ouId/ouName/ouHandle/client_id.
// Those fields only exist on the raw IdP token, so — exactly like the collab
// handlers — this re-parses the still-available raw bearer token (jwtassertion
// stashes it in ctx alongside the verified claims) with
// spec.ParseDisplayIdentity, a best-effort, unverified read of display fields
// only; the signature was already verified upstream by the JWT middleware.
func userIdentityFromContext(ctx context.Context) (email, name string) {
	tok := jwtassertion.GetJWTFromContext(ctx)
	if tok == "" {
		return "", "You"
	}
	name, email = spec.ParseDisplayIdentity("Bearer " + tok)
	if name == "" {
		name = "You"
	}
	return email, name
}

// runFailedActivityRecorder implements run.RunFailedRecorder: a version's run
// settled failed, so the project's feed carries one line saying so. The run
// row is read here rather than carried by the workflow — the settle just
// wrote it, and it already holds every fact the line needs: the version tag,
// the failure record's code and component, or the terminal reason when no
// record exists (a run failed before the record did). Actor = the platform's
// build agent; the dedup key is the run, so a Temporal retry of the settle
// records one line.
type runFailedActivityRecorder struct {
	svc  *projects.ActivityService
	runs delivery.MilestoneRunRepository
}

func (r runFailedActivityRecorder) RecordRunFailed(ctx context.Context, orgID, runID string) {
	if r.svc == nil || r.runs == nil {
		return
	}
	row, err := r.runs.GetByIDScoped(ctx, orgID, runID)
	if err != nil || row == nil {
		return
	}
	reason, component := row.TerminalReason, ""
	if row.Failure != nil && row.Failure.Code != "" {
		reason, component = row.Failure.Code, row.Failure.Component
	}
	r.svc.Record(ctx, projects.ActivityInput{
		OrgID:      orgID,
		ProjectID:  row.ProjectID,
		Type:       activityvocab.TypeRunFailed,
		ActorKind:  activityvocab.ActorAgent,
		ActorName:  "Build agent",
		Tag:        row.SpecTag(),
		Component:  component,
		Reason:     reason,
		DedupKey:   "run:" + runID + ":failed",
		OccurredAt: time.Now().UTC(),
	})
}
