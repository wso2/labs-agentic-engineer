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

import { http, HttpResponse } from "msw";
import type { components } from "../../generated/aep-api";

type IssueInfo = components["schemas"]["IssueInfo"];
type TaskDetail = components["schemas"]["TaskDetail"];
type RcaAgentReportList = components["schemas"]["RcaAgentReportList"];
type TimelineEvent = components["schemas"]["TimelineEvent"];

// Issues and Alerts in mock mode: Acme Expenses has one of each kind the
// Issues Page and the Dashboard tell apart (an escalated incident, a fix to
// review, a verdict, a person's issue, the platform's planned work); the
// other projects have none. No RCA reports, as on today's install.

const REPO = "https://github.com/acme/acme-expenses/issues";

const ISSUES: Record<string, IssueInfo[]> = {
  "acme-expenses": [
    {
      Number: 14,
      Title: "Expense API returns 500 on receipt upload",
      Body: "Uploads over 5 MB fail with a 500 from the expense API.\n\n## Recurrence 1\n\n<!-- aep:recurrence-closure:1a -->\n\nThe earlier merged fix failed to resolve this incident.\n\n## Recurrence 2\n\n<!-- aep:recurrence-closure:2b -->\n\nThe earlier merged fix failed to resolve this incident.\n\n## Recurrence 3\n\n<!-- aep:recurrence-closure:3c -->\n\nThe earlier merged fix failed to resolve this incident.",
      URL: `${REPO}/14`,
      State: "open",
      StateReason: "reopened",
      Labels: ["incident", "aep"],
      attentionReason: "escalated",
    },
    {
      Number: 12,
      Title: "Approvals page times out for large teams",
      Body: "The approvals list takes over 30 s for managers with more than 200 reports.",
      URL: `${REPO}/12`,
      State: "open",
      StateReason: "reopened",
      Labels: ["incident"],
      attentionReason: "unverified_fix",
    },
    {
      Number: 11,
      Title: "Should receipts accept PDFs?",
      Body: "Finance asked whether a PDF invoice can stand in for a receipt photo.",
      URL: `${REPO}/11`,
      State: "open",
      Labels: [],
    },
    {
      Number: 9,
      Title: "Spike in 404s from the payroll export",
      Body: "The nightly export logged 404s from Xero for three claims.",
      URL: `${REPO}/9`,
      State: "closed",
      StateReason: "not_planned",
      Labels: ["incident"],
      attentionReason: "no_change_verdict",
    },
    {
      Number: 4,
      Title: "F1: Submit expenses",
      Body: "Employees record expenses with a receipt photo and submit them as a claim.",
      URL: `${REPO}/4`,
      State: "closed",
      StateReason: "completed",
      Labels: ["aep", "development"],
    },
  ],
};

const COMMENTS: Record<number, NonNullable<TaskDetail["comments"]>> = {
  14: [
    {
      id: "c14",
      author: "aep-coding-agent",
      body: "Raised the upload limit again; the 500s came back within the hour.\nDetails in the pull request.",
      createdAt: new Date(Date.now() - 40 * 60_000).toISOString(),
      url: `${REPO}/14#c14`,
    },
  ],
  12: [
    {
      id: "c12",
      author: "aep-coding-agent",
      body: "Paged the approvals query; not sure it covers every team size, leaving this open for review.",
      createdAt: new Date(Date.now() - 3 * 3_600_000).toISOString(),
      url: `${REPO}/12#c12`,
    },
  ],
};

/** A settled task's log: a few of the coding agent's lines, then the end. */
function taskLog(): string {
  const at = (minutesAgo: number) => new Date(Date.now() - minutesAgo * 60_000).toISOString();
  const lines: TimelineEvent[] = [
    { executionId: "e1", executionKind: "coding", kind: "log", schemaVersion: 1, seq: 1, ts: at(50), message: "Reading the incident and the upload handler" },
    { executionId: "e1", executionKind: "coding", kind: "log", schemaVersion: 1, seq: 2, ts: at(46), message: "Raising the request body limit to 10 MB" },
    { executionId: "e1", executionKind: "coding", kind: "log", schemaVersion: 1, seq: 3, ts: at(41), message: "Opened a pull request" },
  ];
  const frames = lines.map((line) => `data: ${JSON.stringify({ type: "line", line })}\n\n`);
  return [...frames, "data: [DONE]\n\n"].join("");
}

function issuesOf(projectName: string): IssueInfo[] {
  return ISSUES[projectName] ?? [];
}

export const issuesHandlers = [
  http.get("*/api/v1/projects/:projectName/issues", ({ params }) => HttpResponse.json(issuesOf(String(params.projectName)))),

  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber", ({ params }) => {
    const number = Number(params.issueNumber);
    const issue = issuesOf(String(params.projectName)).find((i) => i.Number === number);
    if (!issue) return HttpResponse.json({ code: "not_found", message: "task not found" }, { status: 404 });
    return HttpResponse.json<TaskDetail>({
      issueNumber: number,
      title: issue.Title,
      issueUrl: issue.URL,
      body: issue.Body,
      executorClass: issue.Labels?.includes("aep") ? "coding" : "ledger",
      derivedStatus: issue.State,
      hold: false,
      attention: null,
      dependsOn: null,
      executions: {},
      executionHistory: [],
      lineage: {},
      ...(COMMENTS[number] ? { comments: COMMENTS[number] } : {}),
    });
  }),

  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber/log", () =>
    new HttpResponse(taskLog(), { headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" } }),
  ),

  http.get("*/api/v1/rca-agent/reports", () => HttpResponse.json<RcaAgentReportList>({ items: [] })),
];
