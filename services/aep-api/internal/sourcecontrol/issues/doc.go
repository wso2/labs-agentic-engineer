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

// Package issues files and searches a project's GitHub issues.
//
// Trigger: POST /projects/{projectName}/issues, GET /projects/{projectName}/issues.
// In→out:  bound org + project (+ labels/query) → the created issue, or a ranked list.
// Ports:   sourcecontrol.IssueService.
// The SRE agent's handoff reaches the same service through sre_mcp.go: two MCP
// tools, mounted by edge behind auth.SREHandoffVerifier.
// Invariant: IssueInfo's wire keys are CAPITALIZED — the shape the SRE
// handoff's search_related_issues answers too, which the agent's skill reads.
// The contract documents it; do not "fix" it.
package issues
