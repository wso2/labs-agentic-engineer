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

package githubhost

// The milestone surface: one spec version == one milestone, so these ops are
// the host side of the version ledger.
//
// Three GitHub behaviours shape the code and are load-bearing:
//
//   - Milestone-title uniqueness is enforced case-SENSITIVELY at create, while
//     the issues-list ?milestone= filter resolves number→title and matches
//     case-INSENSITIVELY. A "v1"/"V1" pair therefore creates cleanly and then
//     returns a merged union on every read. CreateMilestone closes that hole
//     with a case-insensitive pre-check the API itself does not offer.
//   - The REST milestone's open_issues field counts pull requests as well as
//     issues, so nothing here reads it. Issue counts come from GraphQL, where
//     milestone.issues is a pure-issue connection.
//   - Milestone state has no side effects: closing one leaves its issues
//     untouched, and a closed milestone still accepts new ones. Close and reopen
//     are display only.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	urlpkg "net/url"
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// milestonePageSize is GitHub's maximum per_page for the milestone and issue
// list endpoints; a short page means the last page.
const milestonePageSize = 100

// milestoneWire is the subset of a GitHub milestone we decode.
type milestoneWire struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Description string `json:"description"`
	NodeID      string `json:"node_id"`
}

// CreateMilestone creates a milestone idempotently, returning its number and
// whether this call minted it.
//
// Idempotency is two-layered because GitHub offers none:
//
//  1. A case-insensitive pre-check over the repo's milestones (all states —
//     uniqueness spans closed ones too). A case-twin is adopted rather than
//     created, since the two would afterwards be indistinguishable to the
//     issues-list filter. A failed pre-check is fatal: creating blind here is
//     exactly the corruption the check exists to prevent.
//  2. POST, and on GitHub's 422 already_exists (a concurrent create of the same
//     exact title won the race) re-list and match the title to recover the
//     number, the same shape RegisterWebhook uses for its already-exists 422.
func (c *Client) CreateMilestone(ctx context.Context, owner, repo string, cred secrets.Credential, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("milestone title is required")
	}

	existing, err := c.ListMilestones(ctx, owner, repo, cred, "all")
	if err != nil {
		return nil, fmt.Errorf("milestone pre-check: %w", err)
	}
	for _, m := range existing {
		if strings.EqualFold(m.Title, title) {
			return &sourcecontrol.MilestoneResult{Number: m.Number, Created: false}, nil
		}
	}

	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/milestones", owner, repo)
	payload := sourcecontrol.CreateMilestoneRequest{Title: title, Description: req.Description}
	status, respBody, err := c.sendJSON(ctx, http.MethodPost, url, cred, payload)
	if err != nil {
		return nil, err
	}
	if status == http.StatusCreated {
		var created milestoneWire
		if err := json.Unmarshal(respBody, &created); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if created.Number == 0 {
			return nil, fmt.Errorf("github response missing milestone number: %s", respBody)
		}
		return &sourcecontrol.MilestoneResult{Number: created.Number, Created: true}, nil
	}
	if !isAlreadyExists(status, respBody) {
		return nil, fmt.Errorf("github milestone create failed (status %d): %s", status, respBody)
	}

	// Lost the race: someone created this exact title between our pre-check and
	// our POST. Recover the number by exact match — a case-twin cannot be the
	// cause, the pre-check already ruled that out.
	after, listErr := c.ListMilestones(ctx, owner, repo, cred, "all")
	if listErr != nil {
		return nil, fmt.Errorf("recover milestone %q after already_exists: %w", title, listErr)
	}
	for _, m := range after {
		if m.Title == title {
			return &sourcecontrol.MilestoneResult{Number: m.Number, Created: false}, nil
		}
	}
	return nil, fmt.Errorf("github reported milestone %q already exists but it is not in the list", title)
}

// sendJSON is the raw form of doJSON: it runs the same authenticated
// marshal-build-execute loop but hands back the status and body instead of
// collapsing a non-OK status into an opaque message. CreateMilestone needs
// that because the duplicate-title branch turns on a machine-readable code
// inside the 422 body.
func (c *Client) sendJSON(ctx context.Context, method, url string, cred secrets.Credential, payload any) (int, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	if err := authHeaders(ctx, req, cred); err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("github API request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, respBody, nil
}

// isAlreadyExists reports whether a response is GitHub's duplicate rejection:
// 422 with an errors[] entry whose code is "already_exists". Matching the code
// rather than the "Validation Failed" message — which every 422 shares — keeps
// a genuinely invalid payload from being mistaken for a duplicate.
func isAlreadyExists(status int, body []byte) bool {
	if status != http.StatusUnprocessableEntity {
		return false
	}
	var parsed struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false
	}
	for _, e := range parsed.Errors {
		if e.Code == "already_exists" {
			return true
		}
	}
	return false
}

// CloseMilestone sets a milestone's state to closed
// (PATCH /repos/{owner}/{repo}/milestones/{number}). Display only: member
// issues are untouched and the milestone keeps accepting new ones.
func (c *Client) CloseMilestone(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/milestones/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "milestone close", cred,
		map[string]string{"state": "closed"}, nil, http.StatusOK)
}

// ReopenMilestone sets a milestone's state back to open, the same PATCH as the
// close. Display only in the same way: a closed milestone never stopped
// accepting issues, so this changes nothing but what a reader sees.
//
// It exists because a milestone OUTLIVES the run that closed it. A build of an
// unchanged spec works the same version again — the same milestone a cancel
// closed — and leaving it closed would show a version being actively worked
// under a heading that says it is finished.
func (c *Client) ReopenMilestone(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	url := fmt.Sprintf(c.apiBase+"/repos/%s/%s/milestones/%d", owner, repo, number)
	return c.doJSON(ctx, http.MethodPatch, url, "milestone reopen", cred,
		map[string]string{"state": "open"}, nil, http.StatusOK)
}

// ListMilestones returns every milestone on the repo in the given state
// ("open" | "closed" | "all"; empty ⇒ "all"), following pagination to the end.
// The walk must be complete — a truncated list would let CreateMilestone's
// uniqueness pre-check pass on a title that already exists further down.
func (c *Client) ListMilestones(ctx context.Context, owner, repo string, cred secrets.Credential, state string) ([]sourcecontrol.Milestone, error) {
	if state == "" {
		state = "all"
	}
	base := fmt.Sprintf(c.apiBase+"/repos/%s/%s/milestones", owner, repo)
	query := urlpkg.Values{"state": {state}, "per_page": {strconv.Itoa(milestonePageSize)}}

	var out []sourcecontrol.Milestone
	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))
		var raw []milestoneWire
		if err := c.getJSON(ctx, base+"?"+query.Encode(), cred, &raw); err != nil {
			return nil, err
		}
		for _, m := range raw {
			out = append(out, sourcecontrol.Milestone{
				Number:      m.Number,
				Title:       m.Title,
				State:       m.State,
				Description: m.Description,
				NodeID:      m.NodeID,
			})
		}
		if len(raw) < milestonePageSize {
			return out, nil
		}
	}
}

// ListMilestoneIssues returns a milestone's issues, filtered by state and by
// label (AND semantics), following pagination to the end.
//
// The milestone is addressed by NUMBER — a title 422s. Pull requests are
// dropped: GitHub's issues endpoint returns them alongside issues (each
// carrying a pull_request member), and counting a member PR as an issue is the
// same mistake that makes the milestone's open_issues field unusable.
func (c *Client) ListMilestoneIssues(ctx context.Context, owner, repo string, cred secrets.Credential, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	if filter.Number <= 0 {
		return nil, fmt.Errorf("milestone number is required")
	}
	base := fmt.Sprintf(c.apiBase+"/repos/%s/%s/issues", owner, repo)
	query := urlpkg.Values{
		"milestone": {strconv.Itoa(filter.Number)},
		"per_page":  {strconv.Itoa(milestonePageSize)},
	}
	if filter.State != "" {
		query.Set("state", filter.State)
	}
	if len(filter.Labels) > 0 {
		// Comma-joined = AND semantics; escaped because label names are free text.
		// NOTE the asymmetry with GraphQL, whose labels: argument over the same
		// resource is a UNION (see milestoneIssueCountsQuery). REST narrows as you
		// add labels, GraphQL widens — do not carry an assumption between them.
		query.Set("labels", strings.Join(filter.Labels, ","))
	}

	var out []sourcecontrol.IssueInfo
	for page := 1; ; page++ {
		query.Set("page", strconv.Itoa(page))
		var raw []struct {
			Number  int    `json:"number"`
			Title   string `json:"title"`
			Body    string `json:"body"`
			HTMLURL string `json:"html_url"`
			State   string `json:"state"`
			Labels  []struct {
				Name string `json:"name"`
			} `json:"labels"`
			// PullRequest is present only on pull requests.
			PullRequest *struct{} `json:"pull_request"`
		}
		if err := c.getJSON(ctx, base+"?"+query.Encode(), cred, &raw); err != nil {
			return nil, err
		}
		for _, r := range raw {
			if r.PullRequest != nil {
				continue
			}
			labels := make([]string, 0, len(r.Labels))
			for _, l := range r.Labels {
				labels = append(labels, l.Name)
			}
			out = append(out, sourcecontrol.IssueInfo{
				Number: r.Number,
				Title:  r.Title,
				Body:   r.Body,
				URL:    r.HTMLURL,
				State:  r.State,
				Labels: labels,
			})
		}
		// Page length counts PRs too, so it — not len(out) — decides the walk.
		if len(raw) < milestonePageSize {
			return out, nil
		}
	}
}

// milestoneIssueCountsQuery is the dispatch predicate: every OPEN-issue
// population of one milestone in a single round trip. milestone.issues is a
// pure-issue connection (pull requests hang off milestone.pullRequests), which
// is what makes the counts trustworthy where REST's open_issues is not. first:1
// keeps the payload minimal — only totalCount is read.
//
// One call is load-bearing: this runs at every cycle boundary, and fanning the
// populations out into a query per label would multiply the rate-limit cost of
// the loop's hottest read.
//
// EVERY ALIAS FILTERS ON ONE LABEL. The labels: argument is a UNION filter — an
// issue matches when it carries ANY of the listed labels — so a multi-label
// alias counts a WIDER population than its name suggests, and an intersection
// cannot be expressed here at all. With one label per alias there is no
// inclusion-exclusion subtlety left to get wrong, and the working sets are plain
// subtraction in Go (sourcecontrol.MilestoneIssueCounts, which owns the
// arithmetic):
//
//	dev working set  = aep - validation
//	task working set = aep - validation - development
//
// Both are exact because every workable kind carries "aep", so each subtracted
// kind is a strict SUBSET of the aep population. Gates are the deliberate
// exception: they carry no "aep", so they are counted on their own alias and
// subtracted from nothing — a gate holds the next dispatch, it must never erase
// the work behind it.
//
// The `src/validation` alias is not a population at all and nothing subtracts
// it: it answers ONE question a bug-fix run asks at its boundary — did any of
// the defects in this milestone come from a verdict — because that is what
// decides whether draining the working set reopens the version's validation
// task. It rides this query rather than a second call for the same reason
// everything else here does: this is the loop's hottest read.
//
// Do not "fix" an alias by listing several labels expecting an AND; that widens
// the population and silently empties a working set. The label literals mirror
// internal/delivery's vocabulary; they are spelled here because the host adapter
// may not import a domain.
const milestoneIssueCountsQuery = `query($owner: String!, $repo: String!, $m: Int!) {
  repository(owner: $owner, name: $repo) {
    milestone(number: $m) {
      provision:     issues(states: [OPEN], labels: ["provision"], first: 1) { totalCount }
      allOpen:       issues(states: [OPEN], first: 1) { totalCount }
      agentWork:     issues(states: [OPEN], labels: ["aep"], first: 1) { totalCount }
      development:   issues(states: [OPEN], labels: ["development"], first: 1) { totalCount }
      validation:    issues(states: [OPEN], labels: ["validation"], first: 1) { totalCount }
      srcValidation: issues(states: [OPEN], labels: ["src/validation"], first: 1) { totalCount }
    }
  }
}`

// MilestoneIssueCounts returns a milestone's open-issue populations — the run
// supervisor's dispatch predicate input (dispatch iff no gate is open and the
// working set is non-empty; see sourcecontrol.MilestoneIssueCounts, which owns
// the arithmetic). Returns ErrMilestoneNotFound when the repo has no milestone
// with that number.
func (c *Client) MilestoneIssueCounts(ctx context.Context, owner, repo string, cred secrets.Credential, number int) (*sourcecontrol.MilestoneIssueCounts, error) {
	type countAlias struct {
		TotalCount int `json:"totalCount"`
	}
	var data struct {
		Repository *struct {
			Milestone *struct {
				Provision     countAlias `json:"provision"`
				AllOpen       countAlias `json:"allOpen"`
				AgentWork     countAlias `json:"agentWork"`
				Development   countAlias `json:"development"`
				Validation    countAlias `json:"validation"`
				SrcValidation countAlias `json:"srcValidation"`
			} `json:"milestone"`
		} `json:"repository"`
	}
	vars := map[string]any{"owner": owner, "repo": repo, "m": number}
	if err := c.graphQL(ctx, cred, milestoneIssueCountsQuery, vars, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil || data.Repository.Milestone == nil {
		return nil, sourcecontrol.ErrMilestoneNotFound
	}
	ms := data.Repository.Milestone
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision:         ms.Provision.TotalCount,
		OpenTotal:             ms.AllOpen.TotalCount,
		OpenAgentWork:         ms.AgentWork.TotalCount,
		OpenDevelopment:       ms.Development.TotalCount,
		OpenValidation:        ms.Validation.TotalCount,
		OpenValidationRepairs: ms.SrcValidation.TotalCount,
	}, nil
}
