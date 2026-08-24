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

package validation

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The consumer ports the validation minter drives. Each is the narrow slice of
// a larger collaborator; concrete providers are wired at the composition root.
// The only feature edge is gitrepo (the issue wire types); criteria
// reads are adapted from artifacts/files by the composition root so this package
// imports neither.

// IssueClient is the GitHub issue READ surface this package needs: one
// MILESTONE's issues, which is how the version's own validation task is
// found. sourcecontrol.IssueService satisfies it.
//
// The read is milestone-scoped rather than project-wide because the milestone is
// the version pin: a project-wide question would answer with another version's
// issue.
//
// The writes beside it — minting the validation issue, reopening it for a repeat
// attempt, and filing a failed attempt's repair issues — are NOT here: they go
// through delivery.IssueWriter, the domain's one issue-write surface, so this
// package holds no second opinion about labels or dedupe keys.
type IssueClient interface {
	ListMilestoneIssues(ctx context.Context, orgID, projectID string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error)
}

// CriteriaReader reads the acceptance oracle (specs/validation/validation-criteria.json)
// at HEAD. found=false when the file does not exist yet (the design agent has
// not authored it) — the minter then skips, and a later planning pass re-mints
// once it exists. The composition root adapts the files feature's Read.
type CriteriaReader interface {
	ReadValidationCriteria(ctx context.Context, orgID, projectID string) (raw []byte, found bool, err error)
}

// ContextProvider is the internal validation-context endpoint's view of the
// context service (*ContextService satisfies it). cycleID is the run cycle the
// runner was dispatched for; the org is the verified caller's, bound into ctx by
// the internal runner-auth gate.
type ContextProvider interface {
	ValidationContext(ctx context.Context, cycleID, orgHandle string) (*ValidationContextResponse, error)
}

// CredentialRequester is the internal test-credentials endpoint's view of the
// credential service (*CredentialService satisfies it).
type CredentialRequester interface {
	RequestCredentials(ctx context.Context, cycleID, orgHandle string, req CredentialRequest) (*TestCredential, error)
}
