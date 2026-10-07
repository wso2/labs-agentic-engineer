/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import type { components } from "../../generated/aep-api";

type ConversationMessage = components["schemas"]["ConversationMessage"];

// Acme Expenses' conversation so far: the brief, the agent's feature list,
// and the first interview done. Shaped the way the journal replays a thread:
// user content as a string, agent content as parts (tool parts included, which
// the panel does not draw).
export const acmeExpensesHistory: ConversationMessage[] = [
  {
    role: "user",
    author: { id: "u-developer", displayName: "Developer" },
    content:
      "Employees submit expenses with a receipt photo, managers approve them, and approved claims go to payroll every night.",
  },
  {
    role: "assistant",
    content: [
      { type: "text", text: "I've read your brief. I see five features: Submit expenses, Approvals, Payroll export, Spending reports and Mileage claims." },
      { type: "tool-call", toolCallId: "tc-1", toolName: "write_file", input: { path: "spec/prd.md" } },
    ],
  },
  {
    role: "tool",
    content: [{ type: "tool-result", toolCallId: "tc-1", output: "ok" }],
  },
  {
    role: "user",
    author: { id: "u-developer", displayName: "Developer" },
    content: "Looks right. Start with Submit expenses.",
  },
  {
    role: "assistant",
    content: [
      { type: "text", text: "Submit expenses is written up: five stories, each with its acceptance criteria. Approvals is next whenever you're ready." },
    ],
  },
];
