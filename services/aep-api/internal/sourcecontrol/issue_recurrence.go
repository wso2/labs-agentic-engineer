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
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var recurrenceHeading = regexp.MustCompile(`(?m)^## Recurrence ([1-9][0-9]*)\r?$`)

const (
	incidentTrackingLabel       = "incident"
	legacyIncidentTrackingLabel = "sre-agent"
)

func HasIncidentLabel(labels []string) bool {
	return slices.Contains(labels, incidentTrackingLabel) || slices.Contains(labels, legacyIncidentTrackingLabel)
}

// recurrenceCount reads the numbered entries in the durable issue ledger.
func recurrenceCount(body string) int64 {
	return int64(len(recurrenceHeading.FindAllStringIndex(body, -1)))
}

// IsNoChangeVerdict identifies an SRE incident's terminal no-code-change closure.
func IsNoChangeVerdict(issue IssueInfo) bool {
	return HasIncidentLabel(issue.Labels) && issue.State == "closed" && issue.StateReason == "not_planned"
}

// IsUnverifiedFix identifies a reopened SRE incident left for human review.
func IsUnverifiedFix(issue IssueInfo) bool {
	return HasIncidentLabel(issue.Labels) && issue.State == "open" &&
		issue.StateReason == "reopened" && !slices.Contains(issue.Labels, "aep")
}

// AttentionReasonFor projects GitHub evidence into the public attention enum.
// The original attempt plus three recurrences escalates; it does not prevent
// another attempt. A terminal verdict takes precedence, and completed fixes
// no longer require attention.
func AttentionReasonFor(issue IssueInfo) string {
	if IsNoChangeVerdict(issue) {
		return "no_change_verdict"
	}
	if HasIncidentLabel(issue.Labels) && issue.State == "open" && recurrenceCount(issue.Body) >= 3 {
		return "escalated"
	}
	if IsUnverifiedFix(issue) {
		return "unverified_fix"
	}
	return ""
}

// RecordRecurrence persists the next attempt before the caller reopens it.
// A closure's host timestamp identifies a retry, not a time window: a later
// completed closure can recur even when its handoff evidence is identical.
// Both the retry marker and numbered history live in the issue body, so retry
// safety survives a process restart without another persistence authority.
func (s *issueService) RecordRecurrence(ctx context.Context, orgID, projectID string, issue IssueInfo, req CreateIssueRequest) (int64, error) {
	if !canRecur(issue) {
		return 0, ErrIncidentRecurrenceIneligible
	}
	if issue.ClosedAt == "" {
		return 0, fmt.Errorf("incident closure identity is missing")
	}
	count := recurrenceCount(issue.Body)
	marker := fmt.Sprintf("<!-- aep:recurrence-closure:%x -->", sha256.Sum256([]byte(issue.ClosedAt)))
	if strings.Contains(issue.Body, marker) && count > 0 {
		return count, nil
	}
	count++
	body := issue.Body + fmt.Sprintf("\n\n## Recurrence %d\n\n%s\n\nThe earlier merged fix failed to resolve this incident.\n\n%s", count, marker, req.Body)
	if err := s.EditIssueBody(ctx, orgID, projectID, issue.Number, body); err != nil {
		return 0, err
	}
	return count, nil
}

func canRecur(issue IssueInfo) bool {
	return HasIncidentLabel(issue.Labels) && issue.State == "closed" && issue.StateReason == "completed"
}
