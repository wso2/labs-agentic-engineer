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

package sourcecontrol

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Sentinel errors. The artifacts tag loop keys off ErrTagAlreadyExists to
// drive tag-collision retries, and repoService keys off ErrRepoNameConflict
// (the RepoAdmin implementation MUST reproduce it; IsRepoNameConflict /
// IsHTTPStatus (wire.go) are the caller-side checks).
var (
	// ErrIncidentContextRequired rejects SRE handoffs without trusted identity
	// and a component, before any issue write.
	ErrIncidentContextRequired = errors.New("trusted incident identity and component are required")
	// ErrIncidentRecurrenceIneligible refuses an SRE handoff whose incident
	// identity matches only closed issues that cannot recur (a closure reason
	// other than completed or not_planned, or a non-SRE issue). The refusal is
	// stable until a human reopens or re-closes the match, so callers map it
	// to a conflict rather than a server failure.
	ErrIncidentRecurrenceIneligible = errors.New("the incident's closed issue is not eligible to recur; only completed SRE issues can recur")
	// ErrRepoNotFound is a gitrepo-domain error (no repo row) returned by
	// repoService/issueService lookups.
	ErrRepoNotFound = errors.New("repository not found")
	// ErrRepoNotReady is a gitrepo-domain error (repo row not in "ready" state).
	ErrRepoNotReady = errors.New("repository is not ready")
	// ErrRepoDeletePending refuses a CreateRepo that meets the project's row
	// still `deleting`: an earlier project delete stopped before it finished.
	// Project create answers it by finishing that delete's teardown and
	// asking once more; it reaches the user only if the row survives that.
	ErrRepoDeletePending = errors.New("the project's previous delete has not finished")
	// ErrIssueNotFound is returned by GetIssue when no issue with the given
	// number exists on the repo (the host answered 404). Callers map it to
	// their own not-found (e.g. task.ErrTaskNotFound → 404).
	ErrIssueNotFound = errors.New("issue not found")
	// ErrMilestoneNotFound is returned by the milestone reads when no milestone
	// with the given number exists on the repo — a run row pinned to a
	// milestone somebody deleted on GitHub. Distinguishes that recoverable
	// state from a transport failure.
	ErrMilestoneNotFound = errors.New("milestone not found")

	// ErrTagAlreadyExists — Git.Tag returns it for a taken name so the save
	// flow recomputes the next tag.
	ErrTagAlreadyExists = errors.New("tag already exists")
	// ErrRefNotFastForward — the pod's push lost a race on the branch tip
	// (not_fast_forward); the caller re-reads and retries.
	ErrRefNotFastForward = errors.New("github ref: update is not a fast-forward")
	// ErrRefNotFound: `at` (branch, tag, or sha) did not resolve to a commit.
	ErrRefNotFound = errors.New("git ref not found")
	// ErrRefInvalid: the AE Studio refused the `at` of a read outright (its
	// request validator, 400 validation_failed): the ref names no commit this
	// API accepts, and no retry of the same read changes that.
	ErrRefInvalid = errors.New("the ref names no commit this API accepts")
	// ErrPathNotFound: the path is not a file in the addressed tree.
	ErrPathNotFound = errors.New("git path not found")
	// ErrRepoNameConflict — port contract: CreateOrgRepo returns it when the
	// requested repo name is already taken (repoService retries with a fresh suffix).
	ErrRepoNameConflict = errors.New("repo name already taken")
)

// The AE Studio answers every caller of the git port (Git, the GitHub
// sub-ports, the adapter ports) branches on. The adapter (clients/aestudiotools)
// and the in-memory Fake return these exact values, so `errors.Is` holds across
// the seam.
var (
	// ErrAEStudioAbsent: the org has no AE Studio because GitHub is not
	// connected for it. Permanent until someone connects GitHub.
	ErrAEStudioAbsent = errors.New("ae studio absent: GitHub is not connected for this org")
	// ErrAEStudioUnavailable: the org's AE Studio is not serving right now
	// (provisioning, failed, unreachable, out of disk, its IdP down). Retry.
	ErrAEStudioUnavailable = errors.New("ae studio unavailable")
	// ErrAEStudioMisconfigured: aep-api's own AE-only client is refused by the
	// pod after one token refresh, or has no credentials (C3). Permanent: an
	// operator fixes it, no retry does.
	ErrAEStudioMisconfigured = errors.New("ae studio rejected the AE-only token: platform configuration error")
	// ErrCommitConflict: a Commit's baseSha precondition failed on at least
	// one path. *CommitConflictError carries the paths; callers re-read and
	// retry.
	ErrCommitConflict = errors.New("commit precondition failed")
	// ErrReferenceRejected: the pod refused a reference document; the error
	// text carries the pod's detail.
	ErrReferenceRejected = errors.New("reference document rejected")
	// ErrOwnerNotAllowed: the repository's owner is not the org's connected
	// GitHub owner, so the pod refuses to touch it.
	ErrOwnerNotAllowed = errors.New("repository owner is not the org's GitHub owner")
)

// CommitConflictError is ErrCommitConflict with the paths whose baseSha did
// not match the tip.
type CommitConflictError struct{ Conflicts []Conflict }

//deadcode:keep returned by the aestudiotools adapter from Task 4.12 (the conflict code map)
func (e *CommitConflictError) Error() string {
	return fmt.Sprintf("commit precondition failed on %d path(s)", len(e.Conflicts))
}

// Is makes errors.Is(err, ErrCommitConflict) hold.
//
//deadcode:keep returned by the aestudiotools adapter from Task 4.12 (the conflict code map)
func (e *CommitConflictError) Is(target error) bool { return target == ErrCommitConflict }

// RateLimitedError is GitHub's rate limit as the pod relays it. RetryAfter
// is when GitHub said to come back (zero when it did not say). Transient.
type RateLimitedError struct{ RetryAfter time.Duration }

//deadcode:keep returned by the aestudiotools adapter from Task 4.12 (github_rate_limited)
func (e *RateLimitedError) Error() string { return "github rate limited" }

// IsRepoNameConflict reports whether err represents a host name-conflict rejection.
func IsRepoNameConflict(err error) bool {
	return errors.Is(err, ErrRepoNameConflict)
}

// IsPermanent reports whether err is an answer rather than a blip: a failure
// that repeating the same call cannot change.
//
// It exists for the callers that retry on a schedule they do not control —
// above all the Temporal run supervisor, whose activities retry with the SDK
// default of "forever". There, a deleted project or a revoked credential turns
// a one-second failure into an unbounded error storm that buries the cause;
// the supervisor asks this and stops.
//
// Permanent:
//   - ErrRepoNotFound / ErrIssueNotFound / ErrMilestoneNotFound — the subject
//     is gone, either from the repo ledger or from the host.
//   - 404 / 410 — the repository, issue or pull request no longer exists for
//     this credential.
//   - 401 — GitHub rejected the org's token (the pod's github_error); a
//     repeat presents the same token and the same rejection.
//   - ErrAEStudioAbsent — GitHub is not connected for the org; a person has
//     to connect it.
//   - ErrAEStudioMisconfigured — aep-api's AE-only token is refused (C3); an
//     operator has to fix the platform configuration.
//   - ErrOwnerNotAllowed — the repository is not under the org's GitHub owner.
//   - ErrReferenceRejected — the pod refused the document itself.
//   - 400 / 422 — GitHub refused the request itself (malformed, or failing
//     its validation); the same request is refused again.
//   - An error whose Permanent() says so: a refusal by the AE Studio pod
//     (clients/aestudiotools.StatusError, a 4xx other than 408 / 429), which
//     has no sentinel of its own here.
//
// Deliberately NOT permanent, and each for a reason:
//   - 403, because GitHub answers its SECONDARY RATE LIMIT with one, and that
//     clears on its own. A permission loss shares the status and will retry
//     pointlessly; the alternative — parsing the body — is worse, and a run
//     that stalls visibly on a real 403 is the safer of the two mistakes.
//   - 5xx and every transport error, which are the blips retries exist for.
//   - ErrRepoNotReady, which is a state the mirror heals out of.
//   - ErrAEStudioUnavailable and *RateLimitedError: the pod or GitHub is
//     busy or coming up, which is what retries are for.
//   - ErrCommitConflict, which the caller resolves by re-reading.
func IsPermanent(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, ErrRepoNotFound),
		errors.Is(err, ErrIssueNotFound),
		errors.Is(err, ErrMilestoneNotFound),
		errors.Is(err, ErrAEStudioAbsent),
		errors.Is(err, ErrAEStudioMisconfigured),
		errors.Is(err, ErrOwnerNotAllowed),
		errors.Is(err, ErrReferenceRejected),
		errors.Is(err, ErrRefInvalid):
		return true
	case IsHTTPStatus(err, http.StatusNotFound),
		IsHTTPStatus(err, http.StatusGone),
		IsHTTPStatus(err, http.StatusUnauthorized),
		IsHTTPStatus(err, http.StatusBadRequest),
		IsHTTPStatus(err, http.StatusUnprocessableEntity):
		return true
	}
	var p permanence
	return errors.As(err, &p) && p.Permanent()
}

// permanence is an error that classifies itself: an adapter's refusal type
// that this package cannot name without importing the adapter.
type permanence interface{ Permanent() bool }
