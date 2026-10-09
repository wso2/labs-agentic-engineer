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

// userTools is the Issues agent's tools/list. The agents service's filing gate
// keys on the literal name create_issue, so the names are a contract with
// services/agents: change them together. No schema declares a project, org or
// labels — the session fixes the first two and AE sets the last.
func userTools() []mcprpc.Tool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	enum := func(desc string, values ...string) map[string]any {
		return map[string]any{"type": "string", "enum": values, "description": desc}
	}
	return []mcprpc.Tool{
		{
			Name: "search_issues",
			Description: "Search the GitHub issues of this session's project to find an existing report before filing a new one. " +
				"The project is fixed by the session; you cannot search another. " +
				"Keyword-ranked: pass space-separated keywords (feature name + symptom terms), not a sentence. " +
				"Returns at most 25 issues; each hit carries number, title, state, labels, url and the first 500 characters of its body.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": str("Space-separated keywords (e.g. 'expense save button'), NOT a natural-language phrase. Omit to list the most recent issues (up to 25)."),
					"state": enum("Which issues to search: open (default), closed, or all.", userIssueStates...),
				},
			},
		},
		{
			Name: "create_issue",
			Description: "File a GitHub issue on this session's project. The project is fixed by the session, and AE sets the labels from kind " +
				"(plus a marker that a user reported it). Returns the new issue's number and url.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title": str("Issue title: one line naming the problem or request."),
					"body":  str("Issue body (markdown): what happens, what was expected, and how to reproduce or why it matters."),
					"kind":  enum("bug: something is broken. feature: something new. improvement: something existing could be better.", userIssueKinds...),
				},
				"required": []string{"title", "body", "kind"},
			},
		},
	}
}
