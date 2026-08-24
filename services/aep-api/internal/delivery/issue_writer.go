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

package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// THE ISSUE-WRITE SURFACE of the delivery domain.
//
// Every platform-minted issue this domain files — a fix, a deploy fix, a merge
// conflict, a red main, a wiring defect, a Task, the validation issue, a
// validation repair — passes through the ONE writer here, and so does every
// close, reopen, comment and label write beside it. Before this existed the
// same three decisions (what the label vocabulary is, whether a mint dedupes,
// what a mint logs) were re-made in eight call sites across four sub-packages,
// which is a vocabulary change that has to be found eight times and is a
// regression if it is found seven.
//
// It lives at the domain ROOT rather than in a `delivery/issues` sub-package
// because of the layout rules the arch tests enforce: a slice may not import a
// sibling slice, so a sub-package would be unreachable from `eventcore` and
// `run` alike, while the root is legal for every slice. The corresponding
// constraint on this file is `root ⊥ slice`: it imports no sub-package of
// delivery, only `sourcecontrol` (the host it writes through).
//
// What it deliberately does NOT do is decide anything. It holds no policy about
// WHEN an issue is filed, does not inspect the milestone, and never invents or
// rewrites a caller's labels or dedupe key. Detection stays with the detector;
// only the write moved.

// IssueSpec is one platform-minted issue, as DELIVERY describes it — a title,
// prose, the label vocabulary of labels.go, the version it belongs to, and the
// key that makes filing it idempotent.
//
// It is not sourcecontrol.CreateIssueRequest with different field names: the
// milestone is an int here because "no milestone" is a domain fact (0), not a
// nil pointer the caller has to build a variable for, and every caller that
// used to write `&run.MilestoneNumber` was one aliasing bug away from assigning
// the wrong version.
type IssueSpec struct {
	Title string
	// Body is PROSE. Nothing platform-side parses an issue body back — not the
	// event plane, not the supervisor, not the reads — so a body may say
	// whatever a coding agent needs to hear.
	Body string
	// Labels is the population this issue joins (labels.go). ORDER IS
	// PRESERVED: the host appends the derived dedupe label after these, and a
	// reader of the resulting issue sees the caller's vocabulary first.
	Labels []string
	// Milestone is the version's milestone NUMBER; 0 leaves the issue
	// unassigned. Assignment RIDES the create, so an issue is never versionless
	// — not even for the beat a follow-up patch would take.
	Milestone int
	// DedupeKey makes the mint idempotent: the host resolves it against the OPEN
	// issues already carrying its derived label and files nothing when one
	// matches. Empty means no server-side dedupe, which is correct only for a
	// caller that dedupes some other way (the plan tap dedupes CLIENT-side on a
	// title slug).
	//
	// Use one of the DedupeKey* constructors below rather than composing a key
	// here: the derived label is a lossy, byte-fragile transform of this string.
	DedupeKey string
}

// IssueOps is the host capability the writer needs — the GitHub-side issue
// writes, on the org's own credential. sourcecontrol.IssueService satisfies it.
//
// It is declared here (rather than the writer taking the whole IssueService)
// because it is the complete list of what delivery is allowed to do to an
// issue: anything absent is a write this domain does not make.
type IssueOps interface {
	CreateIssue(ctx context.Context, orgID, projectID string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error)
	CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error
	ReopenIssue(ctx context.Context, orgID, projectID string, number int) error
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error
	SetIssueMilestone(ctx context.Context, orgID, projectID string, number, milestoneNumber int) error
}

// Compile-time proof the host satisfies the port. The composition root wires
// exactly this, and the assertion is what makes a change to either side fail
// here rather than at the one call site that happens to build it.
var _ IssueOps = (sourcecontrol.IssueService)(nil)

// IssueWriter is delivery's whole issue-write authority. Construct one at the
// composition root and hand it to every slice that files, closes or labels an
// issue.
type IssueWriter struct {
	ops IssueOps
}

// NewIssueWriter wires the writer over the host's issue surface.
func NewIssueWriter(ops IssueOps) *IssueWriter { return &IssueWriter{ops: ops} }

// Mint files an issue, or resolves onto the open one already carrying the same
// dedupe key, and reports which of the two happened.
//
// It does NOT pre-check for an existing issue itself, and that is the single
// most load-bearing line in this file. The host's CreateIssue holds a per-repo
// lock that makes its own check-then-create atomic in-process; a check here
// would sit OUTSIDE that lock and reopen exactly the duplicate-issue race the
// lock exists to close (two alert rules firing for one incident, both passing
// the check, both filing). The key is passed straight through, byte for byte,
// and the host owns the decision.
//
// A nil writer, or one with no host wired, answers (0, false, nil): the same
// degrade-to-nothing every mint site had before this existed, so a partially
// wired composition root files no issues rather than panicking on a webhook.
func (w *IssueWriter) Mint(ctx context.Context, orgID, projectID string, spec IssueSpec) (number int, deduped bool, err error) {
	if w == nil || w.ops == nil {
		return 0, false, nil
	}
	req := sourcecontrol.CreateIssueRequest{
		Title:     spec.Title,
		Body:      spec.Body,
		Labels:    spec.Labels,
		DedupeKey: spec.DedupeKey,
	}
	if spec.Milestone > 0 {
		n := spec.Milestone
		req.Milestone = &n
	}
	res, err := w.ops.CreateIssue(ctx, orgID, projectID, req)
	if err != nil {
		return 0, false, err
	}
	if res == nil {
		return 0, false, nil
	}
	if res.Deduped {
		slog.DebugContext(ctx, "delivery: issue already open — not minting a second",
			"issue", res.Number, "title", spec.Title)
		return res.Number, true, nil
	}
	slog.InfoContext(ctx, "delivery: minted a platform issue",
		"issue", res.Number, "title", spec.Title, "milestone", spec.Milestone)
	return res.Number, false, nil
}

// Close closes the issue, posting comment first when it is non-empty. The
// comment is the host's best-effort: a comment that fails to post still closes
// the issue, because the close is the state change and the comment is courtesy.
func (w *IssueWriter) Close(ctx context.Context, orgID, projectID string, number int, comment string) error {
	if w == nil || w.ops == nil {
		return nil
	}
	return w.ops.CloseIssue(ctx, orgID, projectID, number, comment)
}

// Reopen reopens a closed issue. Idempotent on an already-open one, which is
// what lets a repeat validation attempt adopt the version's own issue instead
// of filing a second one carrying a second snapshot of the oracle.
func (w *IssueWriter) Reopen(ctx context.Context, orgID, projectID string, number int) error {
	if w == nil || w.ops == nil {
		return nil
	}
	return w.ops.ReopenIssue(ctx, orgID, projectID, number)
}

// Comment posts a comment on the issue.
func (w *IssueWriter) Comment(ctx context.Context, orgID, projectID string, number int, body string) error {
	if w == nil || w.ops == nil {
		return nil
	}
	return w.ops.CommentIssue(ctx, orgID, projectID, number, body)
}

// Label adds labels to an existing issue (a merge: a label already present is a
// no-op). Labelling nothing is a no-op rather than a call, so a caller may pass
// a computed set without guarding it.
func (w *IssueWriter) Label(ctx context.Context, orgID, projectID string, number int, labels ...string) error {
	if w == nil || w.ops == nil || len(labels) == 0 {
		return nil
	}
	return w.ops.AddLabels(ctx, orgID, projectID, number, labels)
}

// Unlabel removes one label from an issue. A label that is already absent is
// not an error — the host reconciles that to success, so a redelivered webhook
// can unlabel twice.
func (w *IssueWriter) Unlabel(ctx context.Context, orgID, projectID string, number int, label string) error {
	if w == nil || w.ops == nil {
		return nil
	}
	return w.ops.RemoveLabel(ctx, orgID, projectID, number, label)
}

// SetMilestone moves an existing issue into a milestone — the only write here
// that changes which VERSION an issue belongs to.
//
// It exists for exactly one rule: a defect is not superseded by anything. When a
// build cuts the next version, the previous milestone's planned work is closed
// (a plan is replaced by a plan) but its open bugs are MOVED, because they are
// still broken and the new version is what will ship the fix. A conflict issue is
// closed rather than moved: it names a branch of the version being superseded.
//
// Moving is NOT arming. An unarmed bug — a red-main incident nobody adopted —
// arrives in the new milestone still unarmed and still ledger-only, so carrying a
// human's defect forward can never turn it into agent work nobody asked for.
//
// The destination milestone must already exist, which is why the plan path mints
// the new version's milestone BEFORE it supersedes the old one.
func (w *IssueWriter) SetMilestone(ctx context.Context, orgID, projectID string, number, milestoneNumber int) error {
	if w == nil || w.ops == nil {
		return nil
	}
	return w.ops.SetIssueMilestone(ctx, orgID, projectID, number, milestoneNumber)
}

// ---- the dedupe-key vocabulary --------------------------------------------
//
// Every key the domain mints with is composed HERE, for the same reason the
// label vocabulary is: a key is an identity, and an identity that is spelled at
// its call site is an identity nobody can see the whole of.
//
// The failure a changed key produces is silent and expensive. The host
// normalises a key into a `dedupe:<key>` label (lowercased, whitespace runs
// collapsed to "-", hashed on overflow), matches it against the milestone's
// OPEN issues, and files nothing when it hits. Edit one of these templates and
// nothing breaks, nothing logs, and no test that only reads bodies notices:
// every redelivered webhook and every retried activity simply starts filing a
// SECOND issue for work that is already open. TestIssueDedupeKeysAreFrozen
// pins each one against its literal so that edit has to be deliberate.
//
// Scoping is the other half of the contract, and it differs per key on purpose:
// a fix is keyed to (component, commit) so the next version's failure is
// genuinely new work; a repair is keyed to the ATTEMPT so a criterion that
// fails again next attempt files fresh work; the validation issue is keyed to
// the VERSION so a later version is never deduped onto an older one.

// DedupeKeyFix identifies the fix issue for a component whose build stayed red
// through its automatic re-trigger, by (component, commit).
func DedupeKeyFix(component, shortSHA string) string {
	return fmt.Sprintf("aep fix %s %s", component, shortSHA)
}

// DedupeKeyDeploy identifies the fix issue for a component that built but never
// became ready, by (component, commit): a redeploy of the same commit failing
// the same way finds the open issue, while the next version's failure is new.
func DedupeKeyDeploy(component, shortSHA string) string {
	return fmt.Sprintf("aep deploy %s %s", component, shortSHA)
}

// DedupeKeyConflict identifies the rebase issue for a pull request that would
// not merge, by the pull request itself — every synchronize on that branch
// re-runs the merge policy, and each one must find the same issue.
func DedupeKeyConflict(prNumber int) string {
	return fmt.Sprintf("aep conflict pr-%d", prNumber)
}

// DedupeKeyRedMain identifies the incident issue for a component whose build
// went red on main outside any run, by (component, commit).
func DedupeKeyRedMain(component, shortSHA string) string {
	return fmt.Sprintf("aep red-main %s %s", component, shortSHA)
}

// DedupeKeyUnwiredResources identifies the wiring-conformance issue for the
// resources a component's shipped workload.yaml does not consume. The MISSING
// SET is part of the key: a component that later fails to wire a different
// resource has a different defect, and deduping it onto the first would hide it.
func DedupeKeyUnwiredResources(component string, missing []string) string {
	return fmt.Sprintf("aep unwired %s %s", component, strings.Join(missing, ","))
}

// DedupeKeyUnwiredEndpoints identifies the wiring-conformance issue for the
// sibling endpoints a component's shipped workload.yaml does not target. It is
// deliberately a DIFFERENT prefix from the resources half: the two are
// independent defects fixed in different blocks of the same file, and a shared
// key would let whichever was detected first swallow the other.
func DedupeKeyUnwiredEndpoints(component string, missing []string) string {
	return fmt.Sprintf("aep unwired-endpoints %s %s", component, strings.Join(missing, ","))
}

// DedupeKeyValidationFix identifies the repair issue for one failed acceptance
// criterion, by (criterion, ATTEMPT). The cycle id is what makes a retried
// activity file nothing while a criterion that fails again on the NEXT attempt
// files fresh work rather than being suppressed by the closed issue the last
// repair produced.
func DedupeKeyValidationFix(criterionID, cycleID string) string {
	return fmt.Sprintf("aep validation-fix %s %s", criterionID, cycleID)
}

// DedupeKeyValidationIssue identifies a VERSION's validation issue. Colon
// delimited rather than space delimited like the rest, mirroring the provision
// gate's `gate:<project>:<tag>:<dep>` — the two are the platform's only
// per-version singletons and they read as one family on a milestone.
func DedupeKeyValidationIssue(projectID string, milestoneNumber int) string {
	return "validation:" + projectID + ":" + strconv.Itoa(milestoneNumber)
}
