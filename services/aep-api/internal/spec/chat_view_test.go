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
	"testing"
)

// A chat view names the conversation use case its thread and active-turn slot
// live under; the main chat keeps the use case every existing thread was
// namespaced with, so no conversation is stranded. An issue's thread is its
// own use case, named by the issue number; only the issue view takes one.
func TestUseCaseFor(t *testing.T) {
	cases := []struct {
		chat    ChatScope
		want    string
		wantErr error
	}{
		{chat: ChatScope{View: ChatViewMain}, want: "general"},
		{chat: ChatScope{View: ChatViewIssues}, want: "issues"},
		{chat: ChatScope{View: ChatViewIssue, IssueNumber: 7}, want: "issue-7"},
		{chat: ChatScope{View: ChatViewIssue, IssueNumber: 1234}, want: "issue-1234"},
		{chat: ChatScope{View: ChatViewIssue}, wantErr: ErrIssueNumber},
		{chat: ChatScope{View: ChatViewIssue, IssueNumber: -3}, wantErr: ErrIssueNumber},
		{chat: ChatScope{View: ChatViewMain, IssueNumber: 7}, wantErr: ErrIssueNumber},
		{chat: ChatScope{View: ChatViewIssues, IssueNumber: 7}, wantErr: ErrIssueNumber},
		{chat: ChatScope{View: "boards"}, wantErr: ErrUnknownChatView},
	}
	for _, tc := range cases {
		got, err := useCaseFor(tc.chat)
		if !errors.Is(err, tc.wantErr) {
			t.Errorf("useCaseFor(%+v) err = %v, want %v", tc.chat, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("useCaseFor(%+v) = %q, want %q", tc.chat, got, tc.want)
		}
	}
}
