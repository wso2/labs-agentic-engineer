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

import type { components } from "../../../generated/aep-api";

// A project's issues, as the Issues Page, the Issue card and the Dashboard's
// Alerts read them. Copied from the old console (features/issues/attention.ts,
// features/alerts/) and grown: which issues need attention, why, and which of
// those need a person; who opened an issue; and the Alerts across projects.
//
// Whether an issue needs attention is the platform's call (aep-api's
// AttentionReasonFor, from the incident's labels, state and recurrences); this
// module only says what each reason asks of a person.

export type IssueInfo = components["schemas"]["IssueInfo"];
type RcaAgentReport = components["schemas"]["RcaAgentReport"];
export type AttentionReason = NonNullable<IssueInfo["attentionReason"]>;

const ATTENTION: Record<AttentionReason, { label: string; why: string; needsPerson: boolean; rank: number }> = {
  escalated: {
    label: "Escalated",
    why: "This incident has come back again and again, and each fix the coding agent made failed to hold. It needs a person to look at it.",
    needsPerson: true,
    rank: 0,
  },
  unverified_fix: {
    label: "Needs review",
    why: "The coding agent was not sure its fix resolves this incident, so it left the issue open. Review the fix and close the issue once it holds.",
    needsPerson: true,
    rank: 1,
  },
  no_change_verdict: {
    label: "No code fix",
    why: "The coding agent closed this incident as not planned: no change to the code was warranted.",
    needsPerson: false,
    rank: 3,
  },
};

export function attentionLabel(reason: AttentionReason): string {
  return ATTENTION[reason].label;
}

/** Why the issue needs attention, and what it asks of a person. */
export function attentionWhy(reason: AttentionReason): string {
  return ATTENTION[reason].why;
}

/** Whether nothing moves on the issue until a person acts: a verdict the agent reached on its own only asks to be read. */
export function attentionNeedsPerson(reason: AttentionReason): boolean {
  return ATTENTION[reason].needsPerson;
}

/** The label that hands an issue to the coding agent (aep-api delivery/labels.go, the arming switch). */
const ARMED = "aep";

/** Whether the coding agent has taken the issue on: it carries the arming label. */
export function codingAgentTookOn(issue: Pick<IssueInfo, "Labels">): boolean {
  return (issue.Labels ?? []).includes(ARMED);
}

export type OriginKind = "incident" | "platform" | "person";

/** Who opened an issue: an incident, the platform for its own work, or a person. */
export interface IssueOrigin {
  kind: OriginKind;
  /** "the SRE agent, from an incident". */
  by: string;
}

const INCIDENT_LABELS = ["incident", "sre-agent", "src/incident"];

const SOURCE_BY: Record<string, IssueOrigin> = {
  "src/user": { kind: "person", by: "a person" },
  "src/validation": { kind: "platform", by: "validation, from a failed scenario" },
  "src/build": { kind: "platform", by: "the platform, from a failed build" },
  "src/deploy": { kind: "platform", by: "the platform, from a failed deploy" },
};

const KIND_BY: Record<string, IssueOrigin> = {
  development: { kind: "platform", by: "the planner, from the spec" },
  conflict: { kind: "platform", by: "the platform, for a pull request that would not merge" },
  validation: { kind: "platform", by: "the platform, for the version's validation" },
  provision: { kind: "platform", by: "the platform, for a dependency to set up" },
};

/**
 * Who opened the issue, from its labels: GitHub's author is not on the
 * platform's read, but every platform minter stamps its own labels
 * (delivery/labels.go). A bug with no source label is a person's, as the
 * platform reads it; an issue with none of the platform's labels was filed on
 * GitHub by someone the platform does not name.
 */
export function issueOrigin(labels: readonly string[] | null | undefined): IssueOrigin {
  const all = labels ?? [];
  if (all.some((l) => INCIDENT_LABELS.includes(l))) return { kind: "incident", by: "the SRE agent, from an incident" };
  for (const label of all) {
    const source = SOURCE_BY[label];
    if (source) return source;
  }
  if (all.includes("bug")) return SOURCE_BY["src/user"]!;
  for (const label of all) {
    const kind = KIND_BY[label];
    if (kind) return kind;
  }
  return { kind: "person", by: "someone on GitHub" };
}

/** Open or closed, and how it closed. */
export function issueStateLabel(issue: Pick<IssueInfo, "State" | "StateReason">): string {
  if (issue.State !== "closed") return issue.StateReason === "reopened" ? "Open, reopened" : "Open";
  if (issue.StateReason === "not_planned") return "Closed as not planned";
  return "Closed";
}

/** An issue's text as a person reads it: without the HTML comments the platform keeps its own markers in. */
export function issueText(body: string): string {
  return body
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\n{3,}/g, "\n\n")
    .trim();
}

export interface IssueSections {
  /** Needs attention, those that need a person first. */
  attention: IssueInfo[];
  open: IssueInfo[];
  closed: IssueInfo[];
}

/** The Issues Page's sections, each in the platform's order (newest first) save attention's. */
export function issueSections(issues: readonly IssueInfo[]): IssueSections {
  const attention = issues.filter((i) => i.attentionReason);
  const rest = issues.filter((i) => !i.attentionReason);
  return {
    attention: [...attention].sort((a, b) => ATTENTION[a.attentionReason!].rank - ATTENTION[b.attentionReason!].rank),
    open: rest.filter((i) => i.State !== "closed"),
    closed: rest.filter((i) => i.State === "closed"),
  };
}

/** One of the Dashboard's Alerts: an issue that needs attention, or an RCA report the platform holds. */
export interface AlertItem {
  key: string;
  projectName: string;
  /** The issue it opens; null for a report filed without one. */
  issueNumber: number | null;
  title: string;
  /** The issue's attention, or null for a report. */
  reason: AttentionReason | null;
  /** What the line under the title says. */
  note: string;
  needsPerson: boolean;
}

/** A report waits on a person when it calls for a change and no agent was dispatched at it. */
function reportNeedsPerson(report: RcaAgentReport): boolean {
  return report.classification !== "none" && !report.dispatched && !report.deployed;
}

const REPORT_RANK = { person: 2, info: 4 } as const;

/**
 * The Alerts, across projects: every issue that needs attention, and every
 * RCA report whose issue is not already one of them. Those that need a person
 * come first (an escalation before a fix to review, before a report waiting
 * on a person); then what only asks to be read. Within a rank, by project,
 * then newest first.
 */
export function alertItems(
  issuesByProject: ReadonlyMap<string, readonly IssueInfo[]>,
  reports: readonly RcaAgentReport[],
): AlertItem[] {
  const ranked: { item: AlertItem; rank: number; order: number }[] = [];
  for (const [projectName, issues] of issuesByProject) {
    for (const issue of issues) {
      if (!issue.attentionReason) continue;
      const attention = ATTENTION[issue.attentionReason];
      ranked.push({
        item: {
          key: `${projectName}#${issue.Number}`,
          projectName,
          issueNumber: issue.Number,
          title: issue.Title,
          reason: issue.attentionReason,
          note: attention.label,
          needsPerson: attention.needsPerson,
        },
        rank: attention.rank,
        order: -issue.Number,
      });
    }
  }
  const listed = new Set(ranked.map((r) => r.item.key));
  for (const report of reports) {
    const key = report.issueNumber ? `${report.project}#${report.issueNumber}` : `report:${report.id}`;
    if (listed.has(key)) continue;
    listed.add(key);
    const needsPerson = reportNeedsPerson(report);
    ranked.push({
      item: {
        key,
        projectName: report.project,
        issueNumber: report.issueNumber ?? null,
        title: report.issueTitle || report.title,
        reason: null,
        note: report.summary,
        needsPerson,
      },
      rank: needsPerson ? REPORT_RANK.person : REPORT_RANK.info,
      order: -Date.parse(report.createdAt) || 0,
    });
  }
  return ranked
    .sort((a, b) => a.rank - b.rank || a.item.projectName.localeCompare(b.item.projectName) || a.order - b.order)
    .map((r) => r.item);
}

/** How many Alerts need a person: the count on the rail's logo. */
export function needsPersonCount(items: readonly AlertItem[]): number {
  return items.filter((i) => i.needsPerson).length;
}
