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

package issues

import "github.com/wso2/aep/aep-api/internal/platform/mcprpc"

// issueFixed opens every issue tool's description: the agent never picks the
// issue, the project or the org.
const issueFixed = "The issue (and its project) is fixed by the session; you cannot name another. "

// issueWrite closes every write tool's description: the agents service gates
// each write on the user's answer to its confirmation question.
const issueWrite = " This is a write: ask the user to confirm it first; the agents service refuses it until they do."

// issueTools is an issue agent's tools/list. The names are a contract with
// services/agents (the issue agent's toolset and its confirmation gate key on
// them): change them together. No schema declares a project, org, issue number
// or labels — the session fixes the first three and AE owns the last.
func issueTools() []mcprpc.Tool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	object := func(props map[string]any, required ...string) map[string]any {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	}
	return []mcprpc.Tool{
		{
			Name: "get_issue",
			Description: issueFixed + "Read the issue: number, title, body, labels, state (open or closed), url, " +
				"and its newest 10 comments (author, body, createdAt, and machine: true when the platform wrote it).",
			InputSchema: object(map[string]any{}),
		},
		{
			Name: "list_components",
			Description: "List the components of this session's project design, by the names the design gives them. " +
				"hand_to_coding_agent takes one of these. Empty when the project has no design yet.",
			InputSchema: object(map[string]any{}),
		},
		{
			Name:        "comment_issue",
			Description: issueFixed + "Post a comment on the issue." + issueWrite,
			InputSchema: object(map[string]any{
				"body": str("The comment (markdown)."),
			}, "body"),
		},
		{
			Name:        "edit_issue",
			Description: issueFixed + "Rewrite the issue's title, body, or both; give at least one." + issueWrite,
			InputSchema: object(map[string]any{
				"title": str("The new title: one line. Omit to keep the current one."),
				"body":  str("The new body (markdown); replaces the whole body. Omit to keep the current one."),
			}),
		},
		{
			Name:        "close_issue",
			Description: issueFixed + "Close the issue, posting the reason as its closing comment. Closing removes this chat." + issueWrite,
			InputSchema: object(map[string]any{
				"reason": str("Why the issue is closed (markdown), posted as the closing comment."),
			}, "reason"),
		},
		{
			Name:        "reopen_issue",
			Description: issueFixed + "Reopen the issue." + issueWrite,
			InputSchema: object(map[string]any{}),
		},
		{
			Name: "hand_to_coding_agent",
			Description: issueFixed + "Hand the issue to the coding agent: it joins the deployed version's milestone and a run works it. " +
				"Ask the user which component first (the options are list_components). " +
				"Fails when no version is deployed yet." + issueWrite,
			InputSchema: object(map[string]any{
				"component": str("The component the issue is about: one of list_components."),
			}, "component"),
		},
	}
}
