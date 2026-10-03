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

// sreTools is the SRE handoff's tools/list. The descriptions are the
// remediation agent's whole account of each tool, and its handoff skill
// (deployments/sre-agent-extensions/remediation/skills/coding-agent-handoff)
// is written against them: change them together.
func sreTools() []mcprpc.Tool {
	str := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	strs := func(desc string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
	}
	namespaceArg := str("The OpenChoreo namespace of the alert you are handling (its organization), exactly as the alert names it. " +
		"AE acts on it only if an alert for that namespace, project and component really fired recently.")
	return []mcprpc.Tool{
		{
			Name: "search_related_issues",
			Description: "Search existing GitHub issues on a project's repo to find related/duplicate issues before filing a new one. " +
				"Keyword-ranked: pass space-separated keywords (component name + symptom terms), not a sentence; results come back ranked by keyword overlap for you to judge. " +
				"An issue marked `PlatformRecord: true` is AE's own plan for what to BUILD, not a defect report: read `ReadAs` on it before you treat it as evidence about whether something is broken.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"namespace": namespaceArg,
					"project":   str("OpenChoreo/AE project name"),
					"query": str("Space-separated keywords (e.g. 'service1 service2 timeout'), NOT a natural-language phrase. " +
						"Tokenised and matched against issue title/body; issues are returned ranked by how many keywords they contain. Omit to list all issues."),
					"labels": strs("Filter by GitHub labels"),
				},
				"required": []string{"namespace", "project"},
			},
		},
		{
			Name: "create_issue",
			Description: "Create a GitHub issue on a project's repo and hand it to AE for downstream handling. Creating the issue IS the hand-off — there is no second call. " +
				"File every report that reaches you. What the work IS, and whether a coding agent gets it, are not yours to decide: the platform derives both automatically and answers the classification it chose. " +
				"A `config-level` answer means the remediation agent already expressed every action as configuration — the issue is still filed, as a ledger entry, and nothing is dispatched over it. " +
				"Deduplication is automatic and server-owned: aep-api derives the stable incident key from the trusted request context and validated create fields, so if an OPEN issue for the same incident exists it is returned with `deduped: true`, nothing is created, and nothing is dispatched (the run that created that issue owns its dispatch). An issue already carrying a no-change verdict for this incident answers `suppressed: true`, and nothing is created. " +
				"If instead a CLOSED issue with that key is found — the same component's failure recurring after a fix was merged — it is reopened with this call's body appended as a `## Recurrence <n>` section, moved into the currently deployed version's milestone and handed back to the coding agent; the result then carries `reopened: true` and `recurrence` (which attempt this is). " +
				"The result's `adopted` says whether anything will actually work the issue, and `adoptionError` says why not when it will not — a project with no built version yet gets its issue recorded but not worked. Those outcomes are decided here and in aep-api code, not by the caller's skill.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"namespace": namespaceArg,
					"project":   str("OpenChoreo/AE project name"),
					"title":     str("Issue title"),
					"body":      str("Issue body (markdown)"),
					"labels":    strs("GitHub labels to apply"),
					"componentName": str("The component this issue is about. AE's design names it unprefixed ('service1'), and a name carrying its project prefix ('myproject-service1') is resolved to the design name for you, so pass whichever your world uses. " +
						"Checked before the issue is filed — a name the design carries under neither form fails this call rather than surfacing later inside a coding cycle."),
					"actionStatuses": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": []string{"string", "null"}, "enum": []any{"revised", "suggested", nil}},
						"description": "Your own remediation verdict for each of the RCA report's recommended_actions, in that same order: 'revised' when you expressed it as an OpenChoreo config change, 'suggested' when you could not, null for one you did not address. " +
							"Required on every call — this is what AE classifies code-level vs config-level vs none from.",
					},
				},
				"required": []string{"namespace", "project", "title", "body", "componentName", "actionStatuses"},
			},
		},
	}
}
