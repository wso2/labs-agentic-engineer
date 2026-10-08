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

package spec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// IssueReader reads one of a project's issues: the issue view has a thread
// only while its issue exists and is open. sourcecontrol's issue service
// satisfies it; defined here (consumer side) so the chat needs nothing else of
// it.
type IssueReader interface {
	// GetIssue returns sourcecontrol.ErrIssueNotFound when the project has no
	// such issue.
	GetIssue(ctx context.Context, orgID, projectID string, number int) (*sourcecontrol.IssueInfo, error)
}

// ErrIssueReaderUnavailable means the service was assembled without an issue
// reader (ServiceDeps.Issues is a nil-tolerated test seam) yet an issue chat
// was addressed — a wiring bug, not a client error.
var ErrIssueReaderUnavailable = errors.New("issue reader not configured")

// chatUseCase admits a chat for a thread to be resolved or a turn run on it,
// and names its use case. The issue view also needs its issue to be the
// project's (sourcecontrol.ErrIssueNotFound otherwise) and known to be open
// (ErrIssueClosed otherwise), so a closed issue's thread is never minted or
// run.
func (s *Service) chatUseCase(ctx context.Context, orgID, projectID string, chat ChatScope) (string, error) {
	useCase, err := useCaseFor(chat)
	if err != nil || chat.View != ChatViewIssue {
		return useCase, err
	}
	if s.issues == nil {
		return "", ErrIssueReaderUnavailable
	}
	issue, err := s.issues.GetIssue(ctx, orgID, projectID, chat.IssueNumber)
	if err != nil {
		return "", fmt.Errorf("read issue #%d: %w", chat.IssueNumber, err)
	}
	if issue == nil {
		return "", fmt.Errorf("read issue #%d: %w", chat.IssueNumber, sourcecontrol.ErrIssueNotFound)
	}
	// Fail closed: only an issue known to be open has a thread.
	if !strings.EqualFold(issue.State, "open") {
		return "", ErrIssueClosed
	}
	return useCase, nil
}
