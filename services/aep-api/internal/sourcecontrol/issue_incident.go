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
	"strings"

	"github.com/wso2/aep/aep-api/internal/ops"
)

type incidentContextKey struct{}

// reservedIncidentLabel fences the server-owned identity namespace from both
// explicit labels and labels derived by the legacy dedupe-key path.
func reservedIncidentLabel(label string) bool {
	label = strings.ToLower(strings.TrimSpace(label))
	return strings.HasPrefix(label, "dedupe:sre-code-") || strings.HasPrefix(label, "dedupe:sre-config-")
}

// WithIncidentContext binds an incident identity established by the trusted
// transport. Callers must not populate this from an issue body or dedupe key.
// The identity is opaque; component normalization belongs to CreateIssue. The
// one production caller is internal/edge/sre_handoff_gate.go, which binds it
// on every request the SRE-handoff credential authenticates.
func WithIncidentContext(ctx context.Context, incidentID string) context.Context {
	return context.WithValue(ctx, incidentContextKey{}, incidentID)
}

func (s *issueService) createIncidentIssue(ctx context.Context, orgID, projectID string, req CreateIssueRequest, incidentID string) (*IssueResult, error) {
	if strings.TrimSpace(incidentID) == "" || strings.TrimSpace(req.ComponentName) == "" {
		return nil, ErrIncidentContextRequired
	}
	classification := ops.ClassifyActions(req.ActionStatuses)
	req.ComponentName = strings.ToLower(strings.Join(strings.Fields(req.ComponentName), "-"))
	req.DedupeKey = ""
	labels := []string{"bug", incidentTrackingLabel}
	for _, label := range req.Labels {
		normalized := strings.ToLower(strings.TrimSpace(label))
		switch normalized {
		case "", "aep", "bug", incidentTrackingLabel, legacyIncidentTrackingLabel, "development", "validation", "provision", "conflict":
			continue
		}
		if !strings.HasPrefix(normalized, "dedupe:") && !strings.HasPrefix(normalized, "aep:") {
			labels = append(labels, strings.TrimSpace(label))
		}
	}
	namespace := "code"
	if !ops.AdoptableClassification(classification) {
		namespace = "config"
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%q/%q/%q/%q/%q", orgID, projectID, incidentID, req.ComponentName, namespace)))
	label := "dedupe:sre-" + namespace + "-" + hex.EncodeToString(sum[:16])
	req.Labels = append(labels, label)
	owner, repo, cred, err := s.resolveRepoAndCredential(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	unlock := s.lockRepoCreates(owner, repo)
	defer unlock()
	existing, err := s.github.ListIssues(ctx, owner, repo, cred, []string{label})
	if err != nil {
		return nil, fmt.Errorf("look up incident: %w", err)
	}
	for _, issue := range existing {
		if strings.EqualFold(issue.State, "open") {
			return &IssueResult{Number: issue.Number, URL: issue.URL, Deduped: true, Classification: classification}, nil
		}
	}
	// A human rejection wins over completed matches if legacy duplicates exist.
	for _, issue := range existing {
		if IsNoChangeVerdict(issue) {
			return &IssueResult{Number: issue.Number, URL: issue.URL, Suppressed: true, Classification: classification}, nil
		}
	}
	// Any completed match recurs, even alongside ineligible closed duplicates.
	// Closed matches that are all ineligible fail closed: filing a fresh issue
	// would fork the incident's identity, and reopening one would override a
	// human's closure reason.
	closedIneligible := false
	for _, issue := range existing {
		if !strings.EqualFold(issue.State, "closed") {
			continue
		}
		if !canRecur(issue) {
			closedIneligible = true
			continue
		}
		count, err := s.incident.Recurrence.RecordRecurrence(ctx, orgID, projectID, issue, req)
		if err != nil {
			return nil, fmt.Errorf("record incident recurrence: %w", err)
		}
		if err := s.github.ReopenIssue(ctx, owner, repo, cred, issue.Number); err != nil {
			return nil, fmt.Errorf("reopen incident: %w", err)
		}
		result := &IssueResult{Number: issue.Number, URL: issue.URL, Reopened: true, RecurrenceCount: count, Classification: classification}
		s.adoptIncident(ctx, orgID, projectID, result)
		return result, nil
	}
	if closedIneligible {
		return nil, ErrIncidentRecurrenceIneligible
	}
	// Identity and ownership labels are required: GitHub silently drops absent
	// labels, which would make the next create miss this incident entirely.
	for _, label := range req.Labels {
		color := labelColor(label)
		key := owner + "/" + repo + "\x00" + label + "\x00" + color
		if _, done := s.ensuredLabels.Load(key); done {
			continue
		}
		if err := s.github.EnsureLabel(ctx, owner, repo, cred, label, color); err != nil {
			return nil, fmt.Errorf("ensure incident label %q: %w", label, err)
		}
		s.ensuredLabels.Store(key, struct{}{})
	}
	result, err := s.github.CreateIssue(ctx, owner, repo, cred, req)
	if err != nil {
		return nil, err
	}
	result.Classification = classification
	s.adoptIncident(ctx, orgID, projectID, result)
	return result, nil
}

func (s *issueService) adoptIncident(ctx context.Context, orgID, projectID string, result *IssueResult) {
	if ops.AdoptableClassification(result.Classification) {
		if s.incident.Adopter == nil {
			result.AdoptionError = "incident adoption is not configured"
		} else if err := s.incident.Adopter.AdoptIssue(ctx, orgID, projectID, result.Number); err != nil {
			result.AdoptionError = err.Error()
		} else {
			result.Adopted = true
		}
	}
}
