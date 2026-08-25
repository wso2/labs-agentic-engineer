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
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
)

// The request/response DTOs exchanged across the git-provider ports (ports.go).
// They are part of the port contract: any provider implementation
// (clients/github today) marshals its wire format to and from these types, and
// consumers (gitrepo services, orgcreds, task) read them. Kept
// provider-neutral — only the fields our code actually consumes.

// ----- Repo / issue -----

// CreateOrgRepoRequest maps to the fields we send to POST /orgs/{org}/repos.
//
// The owning org/user is derived from the Credential's RepoOwner() — the
// caller does not pass it explicitly, which keeps the multi-tenant invariant
// (repo creation is parametrised by the credential, not by ambient config).
type CreateOrgRepoRequest struct {
	Name        string
	Private     bool
	AutoInit    bool
	Description string
}

// CreateIssueRequest maps to the fields we send to POST /repos/{owner}/{repo}/issues.
//
// DedupeKey is aep-api-only and never reaches GitHub (issueService clears it
// before the GitHub call; omitempty keeps it off the wire). When set, issue
// creation is idempotent per open issue: the key is normalised into a
// `dedupe:<normalised-key>` label (lowercased, whitespace runs collapsed to
// "-", then truncated to GitHub's 50-char label limit — see dedupeLabelFor in
// issue_service.go), and if an OPEN issue carrying that label already exists
// the existing issue is returned instead of creating a duplicate. Because the
// label is a lossy transform of the raw key, callers cannot reconstruct the
// exact label name from the key alone. This is the correctness layer for
// callers that may fire concurrently for one incident (e.g. the OpenChoreo
// SRE/RCA handoff, one run per alert rule) — they pass a stable key like
// `sre-rca/<component>` so only the first run files an issue.
// This struct is marshalled straight onto the wire by the host adapter, so
// every field but DedupeKey is a GitHub field.
type CreateIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels,omitempty"`
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

// IssueResult is the issue metadata returned after creation. Deduped reports
// that no issue was created because an open issue with the same DedupeKey
// already existed — Number/URL then refer to that existing issue (NodeID may
// be empty in that case; the list API doesn't return it).
type IssueResult struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	NodeID  string `json:"nodeId"`
	Deduped bool   `json:"deduped,omitempty"`
}

// IssueInfo represents an issue returned when listing.
type IssueInfo struct {
	Number int
	Title  string
	Body   string
	URL    string
	State  string
	Labels []string
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

// ----- Account / App installation -----

// GitHubUser is the subset of GET /user we consume.
type GitHubUser struct {
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
	ID    int64  `json:"id"`
}

// AppInstallationInfo is the subset of GET /app/installations/{id} we consume.
// account.login is the GitHub org/user the install belongs to; it can drift
// when the org is renamed on GitHub.
type AppInstallationInfo struct {
	ID      int64 `json:"id"`
	Account struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"account"`
	Suspended *string `json:"suspended_at,omitempty"`
}

// AppInstallationSummary is the flat projection of /app/installations[i]
// the discover endpoint returns to the BFF and console. Mirrors the wire
// shape used in the response (camelCase). Distinct from
// AppInstallationInfo (which preserves the nested account.* shape used
// by the validator's GetAppInstallation probe).
type AppInstallationSummary struct {
	InstallationID int64  `json:"installationId"`
	AccountLogin   string `json:"accountLogin"`
	AccountType    string `json:"accountType"`
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

// GraphQLError carries the errors[] array of a GraphQL response. GraphQL
// answers 200 with a populated errors[] rather than an HTTP status, so this is
// the GraphQL analogue of HTTPStatusError: the whole array is preserved (not
// flattened to a first message) because the machine-readable Type is what
// callers branch on — NOT_FOUND for a stale milestone number is recoverable,
// RATE_LIMITED is retryable, anything else is a bug.
type GraphQLError struct {
	Errors []GraphQLErrorDetail
	// Query is the operation that failed, for debug logging at the call site.
	Query string
}

// GraphQLErrorDetail is one entry of a GraphQL response's errors[]. Path is the
// response path the error applies to; its elements are field names or list
// indices, hence any.
type GraphQLErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Path    []any  `json:"path"`
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, d := range e.Errors {
		if d.Type != "" {
			msgs = append(msgs, d.Type+": "+d.Message)
			continue
		}
		msgs = append(msgs, d.Message)
	}
	return "github graphql error: " + strings.Join(msgs, "; ")
}

// IsGraphQLType reports true when err is a GraphQLError carrying at least one
// error of the given machine-readable type (e.g. "NOT_FOUND", "RATE_LIMITED").
//
// This is the discriminator IsPermanent branches on: the milestone predicate is
// a GraphQL call, so a deleted repository reaches the run supervisor as a
// NOT_FOUND entry here rather than as an HTTP 404.
func IsGraphQLType(err error, typ string) bool {
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		return false
	}
	for _, d := range ge.Errors {
		if d.Type == typ {
			return true
		}
	}
	return false
}
