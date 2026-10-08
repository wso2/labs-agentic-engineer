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
import { deployedVersion } from "../buildsState";
import type { IssueChange } from "../fixtures/issueAgent";
import { mockIssueUrl, type FiledIssue } from "../fixtures/issuesAgent";

type IssueInfo = components["schemas"]["IssueInfo"];
type TaskDetail = components["schemas"]["TaskDetail"];
type RcaAgentReportList = components["schemas"]["RcaAgentReportList"];
type TimelineEvent = components["schemas"]["TimelineEvent"];

// Issues and Alerts in mock mode: Acme Expenses has one of each kind the
// Issues Page and the Dashboard tell apart (an escalated incident, a fix to
// review, a verdict, a person's issue, the platform's planned work); the
// other projects have none. No RCA reports, as on today's install. What an
// issue's own agent changed (a comment, a new title, closing it) shows once
// the turn that made the change has ended; a closed issue has no thread. An
// issue handed to the coding agent from its card is adopted as aep-api adopts
// it — armed (`aep`) and put in the deployed version's milestone, which the
// list shows — or refused in aep-api's words: no deployed version yet (the mock
// has built none), a closed issue, or one the coding agent does not take on.

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
      milestoneNumber: 1,
    },
    {
      Number: 12,
      Title: "Approvals page times out for large teams",
      Body: "The approvals list takes over 30 s for managers with more than 200 reports.",
      URL: `${REPO}/12`,
      State: "open",
      StateReason: "reopened",
      // Disarmed while its fix waits for review: still in the version's milestone, no longer worked.
      Labels: ["incident"],
      attentionReason: "unverified_fix",
      milestoneNumber: 1,
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
      milestoneNumber: 1,
    },
    {
      Number: 3,
      Title: "Validate v1",
      Body: "Check v1 against its acceptance criteria.",
      URL: `${REPO}/3`,
      State: "open",
      Labels: ["aep", "validation"],
      milestoneNumber: 1,
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

/** An issue the Issues agent filed: the issue, and when the turn that filed it ends and the list may show it. */
interface FiledRecord {
  issue: IssueInfo;
  visibleAt: number;
}

const FILED_KEY = "aep:mock:issues";

function readFiled(): Record<string, FiledRecord[]> {
  try {
    const raw = sessionStorage.getItem(FILED_KEY);
    if (raw) return JSON.parse(raw) as Record<string, FiledRecord[]>;
  } catch {
    // unreadable: start over
  }
  return {};
}

function writeFiled(filed: Record<string, FiledRecord[]>): void {
  try {
    sessionStorage.setItem(FILED_KEY, JSON.stringify(filed));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}

/** The number the next filed issue takes: after the highest the project has, filed ones included. */
export function nextIssueNumber(projectName: string): number {
  const numbers = [...(ISSUES[projectName] ?? []), ...(readFiled()[projectName] ?? []).map((f) => f.issue)].map((i) => i.Number);
  return Math.max(0, ...numbers) + 1;
}

/**
 * File an issue as the Issues agent would, labelled with its kind and as the
 * user's. Kept in sessionStorage, so it survives a reload as the chat does.
 * `visibleAt` holds it out of the list until the turn that files it has ended,
 * as the chat only says "Filed" then.
 */
export function fileMockIssue(projectName: string, filed: FiledIssue, visibleAt = Date.now()): IssueInfo {
  const number = nextIssueNumber(projectName);
  const issue: IssueInfo = {
    Number: number,
    Title: filed.title,
    Body: filed.body,
    URL: mockIssueUrl(projectName, number),
    State: "open",
    Labels: [filed.kind, "src/user"],
  };
  const all = readFiled();
  all[projectName] = [...(all[projectName] ?? []), { issue, visibleAt }];
  writeFiled(all);
  return issue;
}

/** A change an issue's agent made, and when the turn that made it ends and the issue shows it. */
interface ChangeRecord {
  number: number;
  change: IssueChange;
  visibleAt: number;
}

const CHANGES_KEY = "aep:mock:issue-changes";

function readChanges(): Record<string, ChangeRecord[]> {
  try {
    const raw = sessionStorage.getItem(CHANGES_KEY);
    if (raw) return JSON.parse(raw) as Record<string, ChangeRecord[]>;
  } catch {
    // unreadable: start over
  }
  return {};
}

/** Record what an issue's agent changed, shown once its turn has ended (`visibleAt`). Kept in sessionStorage. */
export function changeMockIssue(projectName: string, number: number, change: IssueChange, visibleAt = Date.now()): void {
  const all = readChanges();
  all[projectName] = [...(all[projectName] ?? []), { number, change, visibleAt }];
  try {
    sessionStorage.setItem(CHANGES_KEY, JSON.stringify(all));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}

/** An issue's changes that have happened by `now`, oldest first. */
function changesOf(projectName: string, number: number, now: number): IssueChange[] {
  return (readChanges()[projectName] ?? []).filter((c) => c.number === number && c.visibleAt <= now).map((c) => c.change);
}

/** The issue as its agent's changes left it: a new title, or closed. */
function changed(projectName: string, issue: IssueInfo, now: number): IssueInfo {
  return changesOf(projectName, issue.Number, now).reduce<IssueInfo>((i, c) => {
    if (c.kind === "edit") return { ...i, Title: c.title };
    if (c.kind === "close") return { ...i, State: "closed", StateReason: "completed" };
    return i;
  }, issue);
}

/** The project's issues: those the agent has filed (newest first), then the fixtures, as their agents and hand-offs left them. */
export function issuesOf(projectName: string, now = Date.now()): IssueInfo[] {
  const filed = (readFiled()[projectName] ?? []).filter((f) => f.visibleAt <= now).map((f) => f.issue);
  return [...filed.reverse(), ...(ISSUES[projectName] ?? [])].map((i) => adopted(projectName, changed(projectName, i, now)));
}

/** Whether the issue has a thread: it exists and is open (closing it removed the thread). */
export function issueThreadOpen(projectName: string, number: number, now = Date.now()): boolean {
  return issuesOf(projectName, now).find((i) => i.Number === number)?.State === "open";
}

/** The comments the issue's agent posted, as GitHub lists them. */
function agentComments(projectName: string, number: number): NonNullable<TaskDetail["comments"]> {
  const url = mockIssueUrl(projectName, number);
  return (readChanges()[projectName] ?? [])
    .filter((c) => c.number === number && c.change.kind === "comment" && c.visibleAt <= Date.now())
    .map((c, i) => ({
      id: `agent-${number}-${i}`,
      author: "aep-agent",
      body: c.change.kind === "comment" ? c.change.body : "",
      createdAt: new Date(c.visibleAt).toISOString(),
      url: `${url}#agent-${i}`,
    }));
}

/** An issue handed to the coding agent, and the milestone adoption put it in. */
interface Adoption {
  number: number;
  milestoneNumber: number;
}

const ADOPTED_KEY = "aep:mock:issue-adoptions";

function readAdopted(): Record<string, Adoption[]> {
  try {
    const raw = sessionStorage.getItem(ADOPTED_KEY);
    if (raw) return JSON.parse(raw) as Record<string, Adoption[]>;
  } catch {
    // unreadable: start over
  }
  return {};
}

/** Record that the issue was adopted into the milestone. Kept in sessionStorage, so a reload keeps it. */
function adopt(projectName: string, adoption: Adoption): void {
  const all = readAdopted();
  all[projectName] = [...(all[projectName] ?? []).filter((a) => a.number !== adoption.number), adoption];
  try {
    sessionStorage.setItem(ADOPTED_KEY, JSON.stringify(all));
  } catch {
    /* quota: non-fatal in mock mode */
  }
}

/** The issue as adoption left it: armed, in the milestone it joined. */
function adopted(projectName: string, issue: IssueInfo): IssueInfo {
  const adoption = (readAdopted()[projectName] ?? []).find((a) => a.number === issue.Number);
  if (!adoption) return issue;
  const labels = issue.Labels ?? [];
  return { ...issue, Labels: labels.includes("aep") ? labels : [...labels, "aep"], milestoneNumber: adoption.milestoneNumber };
}

/** The kinds, in aep-api's precedence (delivery/labels.go KindOf). */
const KIND_PRECEDENCE = ["provision", "validation", "conflict", "bug", "development"];

/**
 * Why aep-api's adoption refuses the issue (eventcore AdoptIssue), in its
 * words, or null when it takes it: closed, configuration-only, another
 * species' kind, then no deployed version.
 */
function adoptionRefusal(projectName: string, issue: IssueInfo): string | null {
  const labels = issue.Labels ?? [];
  if (issue.State === "closed") return "This issue is closed.";
  const kind = KIND_PRECEDENCE.find((k) => labels.includes(k));
  const configOnly = labels.some((l) => l.toLowerCase().startsWith("dedupe:sre-config-"));
  if (configOnly || kind === "provision" || kind === "validation" || kind === "development") {
    return "This issue is not one the coding agent takes on: the platform works this kind of issue another way.";
  }
  if (!deployedVersion(projectName)) return "Deploy a version first: the coding agent works in a deployed version's milestone.";
  return null;
}

export const issuesHandlers = [
  http.get("*/api/v1/projects/:projectName/issues", ({ params }) => HttpResponse.json(issuesOf(String(params.projectName)))),

  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber", ({ params }) => {
    const number = Number(params.issueNumber);
    const projectName = String(params.projectName);
    const issue = issuesOf(projectName).find((i) => i.Number === number);
    const comments = [...(COMMENTS[number] ?? []), ...agentComments(projectName, number)];
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
      ...(comments.length > 0 ? { comments } : {}),
    });
  }),

  http.post("*/api/v1/projects/:projectName/tasks/:issueNumber/promote-from-issue", ({ params }) => {
    const number = Number(params.issueNumber);
    const projectName = String(params.projectName);
    const issue = issuesOf(projectName).find((i) => i.Number === number);
    if (!issue) return HttpResponse.json({ code: "not_found", message: "issue not found" }, { status: 404 });
    const refusal = adoptionRefusal(projectName, issue);
    if (refusal) return HttpResponse.json({ code: "conflict", message: refusal }, { status: 409 });
    adopt(projectName, { number, milestoneNumber: deployedVersion(projectName)!.milestoneNumber });
    // No body, as aep-api answers it: a client reads an empty 202 by its length.
    return new HttpResponse(null, { status: 202, headers: { "Content-Length": "0" } });
  }),

  http.get("*/api/v1/projects/:projectName/tasks/:issueNumber/log", () =>
    new HttpResponse(taskLog(), { headers: { "Content-Type": "text/event-stream", "Cache-Control": "no-cache" } }),
  ),

  http.get("*/api/v1/rca-agent/reports", () => HttpResponse.json<RcaAgentReportList>({ items: [] })),
];
