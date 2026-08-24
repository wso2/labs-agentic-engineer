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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// IssueService creates and lists GitHub issues on project repositories.
type IssueService interface {
	CreateIssue(ctx context.Context, orgID, projectID string, req CreateIssueRequest) (*IssueResult, error)
	ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]IssueInfo, error)
	// GetIssue fetches a single issue by number — O(1) in repo size, unlike
	// ListIssues (which pages the repo). Returns ErrIssueNotFound when no issue
	// with that number exists on the project's repo.
	GetIssue(ctx context.Context, orgID, projectID string, number int) (*IssueInfo, error)
	// CloseIssue closes the issue, optionally posting a closing comment first.
	CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error
	// ReopenIssue reopens a closed issue. Idempotent on an already-open one.
	// Used by the validation issue across repeated validation attempts.
	ReopenIssue(ctx context.Context, orgID, projectID string, number int) error
	// CommentIssue posts a comment on the issue.
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	// EditIssueBody replaces the issue's body. Used by the tech-lead detail
	// phase to write the LLM-authored body after the placeholder issue was
	// created.
	EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error
	// EditIssueTitle replaces the issue's title. Used by the plan tap on a
	// planned-Task rename (updateTask set.title).
	EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error
	// SetIssueMilestone assigns an existing issue to a milestone by NUMBER —
	// adoption moving a bare issue into the deployed version's milestone.
	SetIssueMilestone(ctx context.Context, orgID, projectID string, number, milestoneNumber int) error
	// AddLabels adds labels to an existing issue (merges; a present label is a
	// no-op). Ensures each label exists in the repo first. Used by the platform
	// to stamp aep:status/* and aep:attention projections.
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	// RemoveLabel removes one label from an issue (404 = already absent = ok).
	RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error
	// SetLabels replaces the issue's entire label set. Used when the projection
	// must be authoritative (clearing stale aep:status/* in one call).
	SetLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
	// GetPullRequestState returns a pull request's live state — the sweep's
	// PR-state reconciliation input (§5).
	GetPullRequestState(ctx context.Context, orgID, projectID string, number int) (*PullRequestState, error)
	// MergePullRequest squash-merges an open pull request. Used by the devflow
	// task workflow's auto merge-pr gate.
	MergePullRequest(ctx context.Context, orgID, projectID string, number int) error
	// ListPullRequestFiles returns the paths of every file changed by a pull
	// request — the path-based build trigger's input for mapping a merged PR's
	// diff onto the components whose source it touched.
	ListPullRequestFiles(ctx context.Context, orgID, projectID string, number int) ([]string, error)

	// The milestone surface — one spec version's delivery increment and ledger.
	// Implementations in milestone_ops.go.

	// CreateMilestone creates the version's milestone idempotently, returning
	// its number and whether this call minted it.
	CreateMilestone(ctx context.Context, orgID, projectID string, req CreateMilestoneRequest) (*MilestoneResult, error)
	// CloseMilestone closes a milestone at settle. Display only.
	CloseMilestone(ctx context.Context, orgID, projectID string, number int) error
	// ReopenMilestone reopens a closed milestone — what a rebuild of an unchanged
	// spec does to the version it is working again. Display only, like the close.
	ReopenMilestone(ctx context.Context, orgID, projectID string, number int) error
	// ListMilestones returns the project's milestones in the given state
	// ("open" | "closed" | "all"; empty ⇒ "all").
	ListMilestones(ctx context.Context, orgID, projectID, state string) ([]Milestone, error)
	// ListMilestoneIssues returns a milestone's issues, filtered by state and
	// label — the version ledger's read. Excludes pull requests.
	ListMilestoneIssues(ctx context.Context, orgID, projectID string, filter MilestoneIssuesFilter) ([]IssueInfo, error)
	// MilestoneIssueCounts returns a milestone's open-issue populations — gates,
	// working set and total — in ONE call, the run supervisor's dispatch
	// predicate input.
	MilestoneIssueCounts(ctx context.Context, orgID, projectID string, number int) (*MilestoneIssueCounts, error)
}

type issueService struct {
	repo     RepoRepository
	github   IssueOps
	resolver secrets.Resolver
	// createLocks serializes dedupe-checked creation per "owner/repo" so two
	// concurrent CreateIssue calls with the same DedupeKey can't both pass the
	// existing-issue check before either creates — the exact race that produced
	// duplicate SRE/RCA issues when multiple alert rules fired for one incident.
	// In-process: correct within this aep-api instance (the deployment runs
	// one). Across replicas the check-then-create window shrinks from the
	// caller's whole run to a single list+create roundtrip, not zero — a DB
	// unique constraint would be needed for cross-replica atomicity.
	//
	// keyedMutex evicts a repo's lock once no create is in flight for it, so the
	// map stays bounded by concurrent dedup creates rather than growing once per
	// distinct repo this long-lived process has ever served.
	createLocks keyedMutex
	// ensuredLabels memoises the (repo, label, colour) triples this process has
	// already created, so a batch that stamps the SAME label on every issue
	// pays for it once instead of once per issue.
	//
	// It is a rate-limit property, not an optimisation: a plan mints N issues
	// all labelled `aep` against a budget of 80 content-generating requests per
	// minute, and without the memo that batch costs 2N requests rather than N.
	// Skipping a repeat is safe because label creation is monotone — nothing in
	// the platform deletes a label — and a process restart re-ensures anyway.
	ensuredLabels sync.Map // "owner/repo\x00name\x00color" → struct{}
}

// keyedMutex is a per-key mutex pool that deletes a key's entry once no
// goroutine holds or is waiting on it, keeping the map bounded by the number
// of concurrently-locked keys (not by every distinct key ever seen — the
// unbounded-growth risk of a naive sync.Map of mutexes in a long-lived,
// many-repo process). The zero value is ready to use.
type keyedMutex struct {
	mu    sync.Mutex
	locks map[string]*refCountedLock
}

type refCountedLock struct {
	mu   sync.Mutex
	refs int // holders + waiters, guarded by keyedMutex.mu
}

// lock acquires the mutex for key and returns its release func. refs is bumped
// under keyedMutex.mu at acquire time — before waiting on the inner mutex — so
// a waiter is always counted before the current holder can release and try to
// evict, keeping the count consistent. The entry is deleted when the last
// holder/waiter releases.
func (k *keyedMutex) lock(key string) func() {
	k.mu.Lock()
	if k.locks == nil {
		k.locks = make(map[string]*refCountedLock)
	}
	rc, ok := k.locks[key]
	if !ok {
		rc = &refCountedLock{}
		k.locks[key] = rc
	}
	rc.refs++
	k.mu.Unlock()

	rc.mu.Lock()
	return func() {
		rc.mu.Unlock()
		k.mu.Lock()
		rc.refs--
		if rc.refs == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}

func NewIssueService(repo RepoRepository, github IssueOps, resolver secrets.Resolver) IssueService {
	return &issueService{
		repo:     repo,
		github:   github,
		resolver: resolver,
	}
}

func (s *issueService) CreateIssue(ctx context.Context, orgID, projectID string, req CreateIssueRequest) (*IssueResult, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}

	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}

	if key := strings.TrimSpace(req.DedupeKey); key != "" {
		req.DedupeKey = "" // aep-api-only field; must not reach GitHub
		label := dedupeLabelFor(key)

		unlock := s.lockRepoCreates(owner, repoName)
		defer unlock()

		existing, listErr := s.github.ListIssues(ctx, owner, repoName, cred, []string{label})
		if listErr != nil {
			// Best-effort: a failed lookup must not block filing the issue; at
			// worst we regress to a possible duplicate.
			slog.WarnContext(ctx, "dedupe lookup failed, creating without dedup check",
				"project", projectID, "label", label, "error", listErr)
		} else {
			for _, iss := range existing {
				if strings.EqualFold(iss.State, "open") {
					slog.InfoContext(ctx, "issue create deduped to existing open issue",
						"project", projectID, "label", label, "issue", iss.Number)
					return &IssueResult{Number: iss.Number, URL: iss.URL, Deduped: true}, nil
				}
			}
		}
		req.Labels = append(req.Labels, label)
	}

	// Ensure all requested labels exist in the repo before creating the issue.
	// GitHub silently drops labels that don't exist, so we create them up-front.
	s.ensureLabels(ctx, owner, repoName, cred, req.Labels)

	// GitHub Projects v2 is dropped (tasks-github-native §4): no lazy board
	// create/link/add on issue creation. Tasks are plain GitHub issues.
	return s.github.CreateIssue(ctx, owner, repoName, cred, req)
}

// ensureLabels pre-creates every label that this process has not already
// created on this repo, because GitHub silently DROPS labels that do not exist.
// A failure is non-fatal and unmemoised: the issue lands without that label and
// the next call tries again.
func (s *issueService) ensureLabels(ctx context.Context, owner, repoName string, cred secrets.Credential, labels []string) {
	for _, label := range labels {
		color := labelColor(label)
		key := owner + "/" + repoName + "\x00" + label + "\x00" + color
		if _, done := s.ensuredLabels.Load(key); done {
			continue
		}
		if ensureErr := s.github.EnsureLabel(ctx, owner, repoName, cred, label, color); ensureErr != nil {
			slog.WarnContext(ctx, "ensure github label failed", "label", label, "error", ensureErr)
			continue
		}
		s.ensuredLabels.Store(key, struct{}{})
	}
}

// lockRepoCreates acquires the per-repo creation lock and returns its release
// func. See the createLocks field doc for why this exists.
func (s *issueService) lockRepoCreates(owner, repo string) func() {
	return s.createLocks.lock(owner + "/" + repo)
}

// dedupeLabelPrefix namespaces the dedupe label. GitHub caps label names at 50
// characters.
const (
	dedupeLabelPrefix = "dedupe:"
	maxGitHubLabelLen = 50
	dedupeHashLen     = 10 // hex chars of the key hash appended on overflow
)

// dedupeLabelFor maps a dedupe key to its GitHub label. Short keys map to a
// readable `dedupe:<normalised-key>` label unchanged (so component-only keys
// stay stable). Keys that would exceed GitHub's 50-char label limit — e.g. a
// long component name plus an error fingerprint — keep a readable prefix and
// append a hash of the FULL normalised key, so two distinct long keys can never
// collide by being truncated to the same 50-char string.
func dedupeLabelFor(key string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(key), "-"))
	label := dedupeLabelPrefix + norm
	if len(label) <= maxGitHubLabelLen {
		return label
	}
	sum := sha256.Sum256([]byte(norm))
	hash := hex.EncodeToString(sum[:])[:dedupeHashLen]
	// Room for the prefix, a '-' separator, and the hash suffix.
	keep := maxGitHubLabelLen - len(dedupeLabelPrefix) - 1 - dedupeHashLen
	if keep > len(norm) {
		keep = len(norm)
	}
	return dedupeLabelPrefix + norm[:keep] + "-" + hash
}

func (s *issueService) ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]IssueInfo, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListIssues(ctx, owner, repoName, cred, labels)
}

func (s *issueService) GetIssue(ctx context.Context, orgID, projectID string, number int) (*IssueInfo, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.GetIssue(ctx, owner, repoName, cred, number)
}

func (s *issueService) CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}

	// Post the closing comment first (best-effort: log and continue on failure).
	if strings.TrimSpace(comment) != "" {
		if commentErr := s.github.CommentIssue(ctx, owner, repoName, cred, number, comment); commentErr != nil {
			slog.WarnContext(ctx, "failed to post closing comment", "project", projectID, "issue", number, "error", commentErr)
		}
	}

	return s.github.CloseIssue(ctx, owner, repoName, cred, number)
}

func (s *issueService) ReopenIssue(ctx context.Context, orgID, projectID string, number int) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.ReopenIssue(ctx, owner, repoName, cred, number)
}

func (s *issueService) CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("comment body is required")
	}

	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}

	return s.github.CommentIssue(ctx, owner, repoName, cred, number, body)
}

func (s *issueService) EditIssueBody(ctx context.Context, orgID, projectID string, number int, body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("body is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.EditIssueBody(ctx, owner, repoName, cred, number, body)
}

func (s *issueService) EditIssueTitle(ctx context.Context, orgID, projectID string, number int, title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.EditIssueTitle(ctx, owner, repoName, cred, number, title)
}

func (s *issueService) SetIssueMilestone(ctx context.Context, orgID, projectID string, number, milestoneNumber int) error {
	if milestoneNumber <= 0 {
		return fmt.Errorf("milestone number is required")
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.SetIssueMilestone(ctx, owner, repoName, cred, number, milestoneNumber)
}

func (s *issueService) AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error {
	if len(labels) == 0 {
		return nil
	}
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	// Ensure each label exists first — GitHub silently drops unknown labels.
	s.ensureLabels(ctx, owner, repoName, cred, labels)
	return s.github.AddIssueLabels(ctx, owner, repoName, cred, number, labels)
}

func (s *issueService) RemoveLabel(ctx context.Context, orgID, projectID string, number int, label string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.RemoveIssueLabel(ctx, owner, repoName, cred, number, label)
}

func (s *issueService) SetLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	s.ensureLabels(ctx, owner, repoName, cred, labels)
	return s.github.SetIssueLabels(ctx, owner, repoName, cred, number, labels)
}

func (s *issueService) GetPullRequestState(ctx context.Context, orgID, projectID string, number int) (*PullRequestState, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.GetPullRequest(ctx, owner, repoName, cred, number)
}

func (s *issueService) MergePullRequest(ctx context.Context, orgID, projectID string, number int) error {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	return s.github.MergePullRequest(ctx, owner, repoName, cred, number)
}

func (s *issueService) ListPullRequestFiles(ctx context.Context, orgID, projectID string, number int) ([]string, error) {
	owner, repoName, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	return s.github.ListPullRequestFiles(ctx, owner, repoName, cred, number)
}

// resolveRepoAndCredential looks up the project's git repository, parses its
// owner/repo from the clone URL, and resolves the org's credential. Every
// GitHub-bound op routes through here — the multi-tenant invariant
// (operations parametrised by ocOrgID) is enforced at one place.
func (s *issueService) resolveRepoAndCredential(ctx context.Context, orgID, projectID string) (owner, repo string, cred secrets.Credential, err error) {
	gitRepo, err := s.repo.GetByOrgAndProjectID(ctx, orgID, projectID)
	if err != nil {
		return "", "", nil, fmt.Errorf("get repo: %w", err)
	}
	if gitRepo == nil {
		return "", "", nil, ErrRepoNotFound
	}

	owner, repo, err = ParseOwnerRepo(gitRepo.RepoURL)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse repo url %q: %w", gitRepo.RepoURL, err)
	}

	cred, err = s.resolver.Resolve(ctx, gitRepo.OrgID)
	if err != nil {
		return "", "", nil, fmt.Errorf("resolve credential for org %q: %w", gitRepo.OrgID, err)
	}
	return owner, repo, cred, nil
}

// ParseOwnerRepo extracts the "owner" and "repo" segments from a GitHub clone URL.
// Supports https://github.com/owner/repo.git and https://github.com/owner/repo forms.
func ParseOwnerRepo(cloneURL string) (owner, repo string, err error) {
	u := strings.TrimSpace(cloneURL)
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		if strings.HasPrefix(u, prefix) {
			u = strings.TrimPrefix(u, prefix)
			break
		}
	}
	u = strings.TrimSuffix(u, ".git")
	u = strings.Trim(u, "/")

	parts := strings.SplitN(u, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("not a github repo url")
	}
	return parts[0], parts[1], nil
}

// labelColor returns a hex color (without #) for well-known AEP labels,
// falling back to a neutral grey for anything else (e.g. phase-N labels).
//
// Every label in the platform's vocabulary belongs here: the create / add / set
// paths pre-create each label through EnsureLabel before use because GitHub
// silently DROPS labels that do not exist, so an unlisted label still lands —
// just in the default grey. A missing entry is a colour bug, never a
// correctness one.
//
// The names are RE-SPELLED here rather than imported: this package sits below
// internal/delivery and may not depend on a domain. A colour listed for a name
// delivery no longer uses is harmless — EnsureLabel POSTs to /labels and never
// recolours an existing one, so a repo keeps whatever colour it was first given
// and a retired name simply stops being asked for.
//
// The colour families say what a reader should look for: blue arms an issue,
// the kinds are graded by who works them, and the markers are grey because they
// qualify an issue rather than routing it.
func labelColor(name string) string {
	switch name {
	case "aep":
		return "0075ca" // blue — the arming switch
	case "development":
		return "7057ff" // purple — planned work from the spec
	case "bug":
		return "d73a4a" // red — a defect (GitHub's own default for this name)
	case "conflict":
		return "b60205" // dark red — a pull request that will not merge
	case "validation":
		return "0e8a16" // green — judging the deployed system
	case "provision":
		return "d93f0b" // red-orange — a gate holding dispatch
	case "src/user", "src/incident", "src/validation", "src/build", "src/deploy":
		return "c5def5" // pale blue — where a bug came from
	case "aep:cancelled", "aep:halted":
		return "bfbfbf" // grey — a marker on an issue already routed
	case "aep:wired":
		return "1d76db" // steel blue — endpoint wiring published on this issue
	case "implementation":
		return "7057ff" // purple
	case "pending":
		return "e4e669" // yellow
	default:
		return "ededed" // light grey for phase-N and other dynamic labels
	}
}
