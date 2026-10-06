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

import "errors"

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
)

// useCaseIssues is the conversation use case of the Issues view. Like
// UseCaseGeneral it is baked into conversation identities and turn rows.
const useCaseIssues = "issues"

var (
	// ErrUnknownChatView rejects a view outside the enum (a 400). The edge's
	// request validator already refuses one on the JSON and query arms; the
	// multipart arm is parsed by hand, so this is the check that holds there.
	ErrUnknownChatView = errors.New("unknown chat view")
	// ErrViewTurnFields rejects a turn that carries what its view cannot use:
	// a spec scope or prototype feedback on an Issues turn (a 400).
	ErrViewTurnFields = errors.New("the issues chat takes no scope or prototype feedback")
)

// useCaseFor maps a view onto the conversation use case its thread and
// active-turn slot live under.
func useCaseFor(v ChatView) (string, error) {
	switch v {
	case ChatViewMain:
		return UseCaseGeneral, nil
	case ChatViewIssues:
		return useCaseIssues, nil
	default:
		return "", ErrUnknownChatView
	}
}
