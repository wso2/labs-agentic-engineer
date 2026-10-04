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
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// The request/response DTOs exchanged across the git-provider ports (ports.go).
// They are part of the port contract: any provider implementation
// (clients/github today) marshals its wire format to and from these types, and
// consumers (gitrepo services, orgcreds, task) read them. Kept
// provider-neutral — only the fields our code actually consumes.

// ----- Repo / issue -----

// CreateOrgRepoRequest is what a new repository is created with. The
// repository's owner and name are the RepoRef's (the owner is the org's
// connected GitHub account); the pod always initialises it with a main
// branch.
type CreateOrgRepoRequest struct {
	Private     bool
	Description string
	// AdoptExisting answers the existing repository instead of
	// ErrRepoNameConflict when the name is taken (the AE Studio create-repo
	// op's adoptExisting).
	AdoptExisting bool
}

// CreateIssueRequest maps to the fields we send to POST /repos/{owner}/{repo}/issues.
//
// DedupeKey is legacy non-SRE deduplication context, cleared before host writes.
// SRE requests ignore it: trusted incident context, tenant/project, normalized
// component, and classification namespace establish their identity server-side.
// This struct is marshalled straight onto the wire by the host adapter. Every
// field except DedupeKey, ComponentName, and ActionStatuses is a GitHub field.
type CreateIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
	// ComponentName and ActionStatuses are handoff context owned by aep-api.
	// They are deliberately not forwarded to GitHub's issue-create endpoint.
	ComponentName  string    `json:"-"`
	ActionStatuses []*string `json:"-"`
	// Milestone assigns the issue to a milestone at creation time — one call
	// instead of create-then-patch, which is what keeps a plan's API cost at
	// 1+N. It is the milestone NUMBER; GitHub answers 422 to a title here. Nil
	// leaves the issue unassigned.
	Milestone *int   `json:"milestone,omitempty"`
	DedupeKey string `json:"dedupeKey,omitempty"`
}

// ----- Milestones -----

// A milestone is one spec version's delivery increment and ledger: the tag's
// issues (implementation, gate, validation, incident) join it over time.
//
// Number is the only stable key — titles are freely renamable, and while
// create-uniqueness is case-SENSITIVE the issues-list title filter is
// case-INSENSITIVE, so a case-twin pair would silently merge. Platform code
// therefore resolves by number and never matches on title.

// CreateMilestoneRequest maps to the fields we send to
// POST /repos/{owner}/{repo}/milestones.
type CreateMilestoneRequest struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// MilestoneResult is the outcome of a milestone create. Created reports whether
// this call minted the milestone; false means one with that title already
// existed (found by the case-insensitive pre-check, or recovered from GitHub's
// 422 already_exists) and Number refers to it. Either way the caller holds a
// usable number, which makes creation idempotent.
type MilestoneResult struct {
	Number  int
	Created bool
}

// Milestone is the subset of a GitHub milestone the platform reads. State is
// "open" | "closed" — display only: platform logic never branches on it, and
// closed milestones still accept new issues.
type Milestone struct {
	Number      int
	Title       string
	State       string
	Description string
	NodeID      string
}

// MilestoneIssuesFilter narrows a milestone's issue list.
// State is "open" | "closed" | "all" (empty ⇒ the host default, "open").
//
// Labels is AND-semantics — an issue must carry all of them. That is the REST
// endpoint behind this call. It does NOT generalise: the GraphQL query behind
// MilestoneIssueCounts filters on labels too, and there the argument is a
// UNION. Adding a label here narrows; adding one there widens.
type MilestoneIssuesFilter struct {
	Number int
	State  string
	Labels []string
}

// MachineCommentMarker brands a comment as WRITTEN BY THE PLATFORM.
//
// It exists because authorship cannot answer that question. The platform
// comments through the org's own credential, and that same credential is handed
// to the coding runner as GITHUB_TOKEN — so a machine comment and an agent's
// progress note arrive under one identity and no `author` test can separate
// them. The only durable discriminator is one the writer puts in the body
// itself, which is what this is.
//
// It is an HTML comment so it renders as nothing on the host: a person reading
// the issue sees the prose and never the brand. It is stamped in exactly one
// place — issueService, the single adapter every platform issue-comment write
// passes through — and stripped on read, so it never reaches a consumer.
//
// A comment written BEFORE this shipped carries no marker and therefore reads as
// human. That is a known and accepted gap: the alternative was pattern-matching
// the openers of five different writers, which drifts the first time one is
// reworded.
const MachineCommentMarker = "<!-- aep:machine -->"

// PublishedCredentialsMarker marks an issue comment whose body deliberately
// carries CREDENTIALS in plaintext — the test-user logins the build publishes on
// the roles gate ticket, which is where the validation agent reads them
// (ADR-0022).
//
// It lives here, beside MachineCommentMarker, because the writer is not the only
// party that has to know about it: GitHub delivers every comment we post back to
// us as an `issue_comment` webhook, and this receiver stores each verified
// delivery's raw body for audit. Persisting that body unchanged would copy every
// published password into the platform's own database in cleartext — the one
// place in this service where every other credential is sealed — and nothing
// ever reads those rows back. redactPublishedCredentials keys on this constant
// to keep the blast radius where the ADR bounds it: the repository, and not the
// database.
const PublishedCredentialsMarker = "<!-- aep:test-users -->"

// ObservedCommentMarker brands a comment the platform wrote FOR A PERSON, from
// what it observed of a run.
//
// A third class exists because MachineCommentMarker answers a different
// question. Both say "the platform wrote this", but a machine comment is
// written for the AGENT — a resolved-dependency block, a provisioning note, the
// line that closes a task — which is why every read surface for people drops
// them. A validation run's status line is the opposite: the platform derives it
// from the run's own tool calls precisely so a person watching has something to
// read (the runner's validation_status_line.ts). Dropping it would delete the
// only thing on the issue between the agent's opening line and its last.
//
// So it must be told apart from the two neighbours it sits between, and only
// the body can tell: authorship cannot, for the reason MachineCommentMarker
// gives above, and the agent's OWN notes carry no marker at all.
//
// `aep:status` was the obvious name and is taken: `aep:status/*` is the issue
// LABEL namespace the run projects its state onto. "Observed" is the honest
// word anyway — this is inferred from calls the run had to make, never declared
// by it.
const ObservedCommentMarker = "<!-- aep:observed -->"

// IssueComment is one comment on an issue, exactly as the host holds it.
//
// It carries no issue number: the read that produces these buckets them by
// issue (map[int][]IssueComment), so a number on the row would be a second copy
// of the map key, free to disagree with it.
//
// Author is a LOGIN, and empty is a real answer — the host reports a null author
// for a comment whose account is gone.
type IssueComment struct {
	// ID is the host's node id — stable across reads, and the consumer's list key.
	ID        string
	Author    string
	Body      string
	URL       string
	CreatedAt time.Time
	// Machine reports that the PLATFORM wrote this comment (MachineCommentMarker).
	//
	// It is a fact the host reports, not a decision it acts on: what a given read
	// surface does with a machine comment is that surface's policy, and the task
	// list drops them. Reporting rather than filtering here is what keeps a future
	// reader — a debug view, an audit — able to ask for them without the host
	// changing.
	Machine bool
	// Observed reports that the PLATFORM wrote this comment for a person, from
	// what it saw the run do (ObservedCommentMarker).
	//
	// Mutually exclusive with Machine, and structurally so rather than by
	// convention: classification tests which marker LEADS the body, and only one
	// can. Reported for the same reason Machine is — the host states the fact and
	// each surface decides what to do with it.
	Observed bool
}

// MilestoneIssueCounts is the run supervisor's dispatch predicate input: the
// OPEN-issue populations of one milestone, gathered in a single host round trip
// so the per-cycle-boundary predicate stays one call.
//
// Each field is the count of ONE label — never a union of several, and never an
// intersection. That is what the host can answer honestly: its GraphQL labels:
// argument is a UNION filter, so a multi-label alias counts issues carrying ANY
// of them, and an intersection cannot be expressed at all.
//
// The working sets are then plain SUBTRACTION, and it is exact because every
// workable kind carries "aep": each excluded kind is a strict SUBSET of the
// "aep" population, so subtracting its count removes each member exactly once.
// Read them through OpenDevWork / OpenTaskWork, never by subtracting fields by
// hand — the arithmetic lives in one place so the dispatch predicate and the
// settle check cannot drift apart on what "work" means.
//
// The one population that is NOT a subset is the gates: they carry no "aep" at
// all, so they are counted on their own field and are never subtracted from
// anything. A gate holds the next dispatch; it must never erase the work behind
// it, which is the live failure the old inclusion-exclusion arithmetic caused.
//
// These are issue counts, never pull-request counts — the reason the predicate
// is a GraphQL query over milestone.issues rather than the REST milestone's
// open_issues field, which counts PRs too.
type MilestoneIssueCounts struct {
	// OpenProvision is every open dispatch gate ("provision"). One open gate
	// holds the next dispatch. Gates carry no "aep", so this count overlaps
	// nothing else here.
	OpenProvision int
	// OpenTotal is every open issue in the milestone, ledger included. It says
	// whether the milestone is finished, not whether it is workable.
	OpenTotal int
	// OpenAgentWork is every open ARMED issue ("aep"): planned work, bugs,
	// conflicts and the validation task together. Every working set is this
	// population minus one or more of the kinds below.
	OpenAgentWork int
	// OpenDevelopment is every open planned-work issue ("development") — the
	// planner's output. A subset of OpenAgentWork.
	OpenDevelopment int
	// OpenValidation is every open validation task ("validation"). A subset of
	// OpenAgentWork: the validation task IS armed, it is simply worked by the
	// validation loop rather than by a coding cycle.
	OpenValidation int
	// OpenValidationRepairs is every open issue carrying the `src/validation`
	// SOURCE — the repair work a failed verdict filed, one issue per failed
	// criterion.
	//
	// It is the only source counted here, and it is a SIGNAL rather than a
	// population: nothing subtracts it, and it overlaps the working sets freely
	// (a repair issue is an ordinary armed bug and is counted as one above).
	// It exists because a bug-fix run has to know whether the defects it worked
	// came from a verdict — that is what decides whether the version's validation
	// task is reopened when the run drains its working set — and answering it
	// from the counts is what keeps the cycle-boundary poll ONE round trip.
	OpenValidationRepairs int
}

// OpenDevWork is the size of a DEV run's working set: armed issues that are not
// the validation task — planned work, plus the bugs and conflicts that working
// it threw up.
//
// This is the count whose reaching zero SETTLES a version, so the failure it
// must not have is undercounting. Nil-tolerant: an unknown milestone has no
// work.
func (c *MilestoneIssueCounts) OpenDevWork() int {
	if c == nil {
		return 0
	}
	return clampWork(c.OpenAgentWork - c.OpenValidation)
}

// OpenTaskWork is the size of a TASK run's working set: armed issues that are
// neither the validation task nor planned work.
//
// A bug-fix run works the DEPLOYED version, so planned work for the version
// currently being built is deliberately not its business — subtracting it here
// is what keeps two live runs on one repository from picking up each other's
// issues. It is also what makes a budget mean something: a dev run that gave up
// leaves its planned work OPEN, and a task run that could continue it would be
// the same work restarted with fresh budgets by a run that never planned it.
func (c *MilestoneIssueCounts) OpenTaskWork() int {
	if c == nil {
		return 0
	}
	return clampWork(c.OpenAgentWork - c.OpenValidation - c.OpenDevelopment)
}

// clampWork floors a working set at zero.
//
// Unreachable against a consistent host: every kind subtracted above is a subset
// of the armed population, so the difference cannot go negative. Clamped anyway
// so a host that answers inconsistently degrades to "nothing to work" rather
// than inventing a negative working set that would read as workable in one
// comparison and empty in another.
func clampWork(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// IssueResult reports creation or the existing issue chosen by server identity.
// SRE outcomes include open deduplication, terminal suppression, and recurrence.
// Adopted means this call successfully handed work to delivery; an open dedupe
// never dispatches again. NodeID may be empty for existing issues.
type IssueResult struct {
	Number          int    `json:"number"`
	URL             string `json:"url"`
	NodeID          string `json:"nodeId"`
	Deduped         bool   `json:"deduped,omitempty"`
	Classification  string `json:"classification,omitempty"`
	Suppressed      bool   `json:"suppressed,omitempty"`
	Reopened        bool   `json:"reopened,omitempty"`
	Adopted         bool   `json:"adopted,omitempty"`
	AdoptionError   string `json:"adoptionError,omitempty"`
	RecurrenceCount int64  `json:"recurrenceCount,omitempty"`
}

// IssueInfo represents an issue returned when listing.
type IssueInfo struct {
	Number      int
	Title       string
	Body        string
	URL         string
	State       string
	StateReason string
	// ClosedAt is the host closure identity, used only to make recurrence writes
	// idempotent across failed reopen requests. It is not an API response field.
	ClosedAt        string `json:"-"`
	Labels          []string
	AttentionReason string
}

// CompareResult is the per-file change summary between two refs the lineage
// diff consumes (§6) — produced by the Workspace engine's local
// `git diff base...head` (Workspace.Diff). Alias of the gitfs definition
// (identical fields). Truncated is always false there (a local diff never
// truncates); the field survives from the retired GitHub compare shape.
type CompareResult = gitfs.CompareResult

// ChangedFile is one entry of a compare's files[] list. Alias of the gitfs
// definition. Status vocabulary is GitHub-compatible: added | removed |
// modified | renamed | copied | changed | unchanged.
type ChangedFile = gitfs.ChangedFile

// ----- Account -----

// GitHubUser is the subset of GET /user we consume.
type GitHubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
	ID    int64  `json:"id"`
}

// ----- Git identity -----

// GitIdentity mirrors a git author/committer/tagger identity. Date is
// optional (defaults to the commit/tag time when omitted). Named with the
// `Git` prefix to avoid collision with the `Identity` type already declared
// in credential_service.go. Alias of the gitfs definition — consumers keep
// importing sourcecontrol.GitIdentity while the engine owns the type.
type GitIdentity = gitfs.GitIdentity

// PullRequestState is the subset of a pull request the sweep's PR-state
// reconciliation reads (§5): open/closed + merged + the merge commit SHA.
type PullRequestState struct {
	State          string // "open" | "closed"
	Merged         bool
	MergeCommitSHA string
}

// ----- Status errors -----

// HTTPStatusError surfaces HTTP status codes from the git host client so the
// validator can branch on 401 / 404 / 410. Wraps the response body for
// debug logging at the call site.
type HTTPStatusError struct {
	StatusCode int
	Body       string
	URL        string
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("github API %s: status %d: %s", e.URL, e.StatusCode, e.Body)
}

// IsHTTPStatus reports true when err is an HTTPStatusError with the given code.
func IsHTTPStatus(err error, code int) bool {
	var he *HTTPStatusError
	if errors.As(err, &he) {
		return he.StatusCode == code
	}
	return false
}
