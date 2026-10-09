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
	"errors"
	"strconv"
)

// ChatView is the main-panel view whose agent the chat talks to. Each view
// owns its own conversation: its thread (project_conversations), its history
// namespace in the agents service, and its one-active-turn slot. The main chat
// is the zero value, so a request that names no view is the main chat — every
// client that predates views keeps talking to the thread it always did.
type ChatView string

const (
	// ChatViewMain is the project's main (spec) chat.
	ChatViewMain ChatView = ""
	// ChatViewIssues is the Issues page's chat: a plain chat turn on its own
	// thread, which never edits the spec and never waits on the spec chat.
	ChatViewIssues ChatView = "issues"
	// ChatViewIssue is one issue's own chat: every open issue of the project
	// has its own thread and active-turn slot, named by the issue number.
	ChatViewIssue ChatView = "issue"
)

// ChatScope names one chat: the view, and for the issue view the issue it is
// about. IssueNumber is zero on every other view.
type ChatScope struct {
	View        ChatView
	IssueNumber int
}

// useCaseIssues is the conversation use case of the Issues view. Like
// UseCaseGeneral it is baked into conversation identities and turn rows.
const useCaseIssues = "issues"

// useCaseIssuePrefix + the issue number is the use case of one issue's thread
// ("issue-7"). Single dash: the agents-service id joins its parts with "--".
const useCaseIssuePrefix = "issue-"

var (
	// ErrUnknownChatView rejects a view outside the enum (a 400). The edge's
	// request validator refuses one on the query arms; create-turn's bodies
	// are not schema-validated (the route also takes multipart, which the
	// validator skips), so this is the check that holds there.
	ErrUnknownChatView = errors.New("unknown chat view")
	// ErrViewTurnFields rejects a turn that carries what its view cannot use:
	// a spec scope or prototype feedback on an Issues or issue turn (a 400).
	ErrViewTurnFields = errors.New("the issues chats take no scope or prototype feedback")
	// ErrIssueNumber rejects an issue view without a positive issue number, an
	// issue number on any other view, or a present one below 1 (a 400).
	ErrIssueNumber = errors.New("issueNumber is required with view=issue (a positive integer) and refused with any other view")
	// ErrIssueClosed refuses the issue view for a closed issue: a closed issue
	// has no thread, so none is resolved, read or run (a 409 issue_closed).
	ErrIssueClosed = errors.New("the issue is closed")
)

// useCaseFor maps a chat onto the conversation use case its thread and
// active-turn slot live under.
func useCaseFor(c ChatScope) (string, error) {
	var useCase string
	switch c.View {
	case ChatViewMain:
		useCase = UseCaseGeneral
	case ChatViewIssues:
		useCase = useCaseIssues
	case ChatViewIssue:
		if c.IssueNumber < 1 {
			return "", ErrIssueNumber
		}
		return useCaseIssuePrefix + strconv.Itoa(c.IssueNumber), nil
	default:
		return "", ErrUnknownChatView
	}
	if c.IssueNumber != 0 {
		return "", ErrIssueNumber
	}
	return useCase, nil
}
