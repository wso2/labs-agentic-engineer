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

import { useCallback, useMemo, useState } from "react";
import { useQueries } from "@tanstack/react-query";
import type { components } from "../../../generated/aep-api";
import { useSession } from "../../../auth/SessionContext";
import { BELL_POLL_MS } from "../../alerts/api/queries";
import { projectIssuesQueryOptions, type IssueInfo, type IssueAttentionReason } from "../api/queries";

type RcaAgentReport = components["schemas"]["RcaAgentReport"];

// Seen state is per signed-in account and active organization: a shared
// browser must not hand one user's seen items to the next, and item ids
// (project, issue number, reason) repeat across organizations. Email is the
// only stable account identifier the session carries (see
// agent-chat/currentUser.ts).
function seenStorageKey(orgHandle: string | null, accountId: string): string {
  return `aep:issues:attentionSeen:${orgHandle ?? ""}:${accountId}`;
}

export type AttentionItem = {
  id: string;
  projectName: string;
  issueNumber: number;
  title: string;
  reason: IssueAttentionReason;
};

function readSeen(storageKey: string): string[] {
  try {
    return JSON.parse(localStorage.getItem(storageKey) ?? "[]") as string[];
  } catch {
    return [];
  }
}

function writeSeen(storageKey: string, ids: string[]) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(ids));
  } catch {
    // Storage unavailable — unread state just won't persist across reloads.
  }
}

export function attentionItemId(projectName: string, issueNumber: number, reason: IssueAttentionReason): string {
  return `${projectName}#${issueNumber}:${reason}`;
}

export function collectAttentionItems(reports: RcaAgentReport[], issuesByProject: Map<string, IssueInfo[]>): AttentionItem[] {
  const out: AttentionItem[] = [];
  const seen = new Set<string>();
  for (const report of reports) {
    if (!report.project || !report.issueNumber) continue;
    const issue = (issuesByProject.get(report.project) ?? []).find((candidate) => candidate.Number === report.issueNumber);
    if (!issue?.attentionReason) continue;
    const id = attentionItemId(report.project, issue.Number, issue.attentionReason);
    if (seen.has(id)) continue;
    seen.add(id);
    out.push({
      id,
      projectName: report.project,
      issueNumber: issue.Number,
      title: issue.Title,
      reason: issue.attentionReason,
    });
  }
  return out;
}

export function countUnreadAttention(items: AttentionItem[], seenIds: string[]): number {
  const seen = new Set(seenIds);
  return items.filter((item) => !seen.has(item.id)).length;
}

export function useAttentionUnread(reports: RcaAgentReport[]) {
  const session = useSession();
  const storageKey = seenStorageKey(session.orgHandle, session.user.email);
  const [seen, setSeen] = useState(() => ({ storageKey, ids: readSeen(storageKey) }));
  // Account or org changed under a mounted bell: reload that scope's seen
  // state rather than carrying the previous one's in-memory set over. React
  // discards this render and re-runs it with the new state before committing.
  if (seen.storageKey !== storageKey) {
    setSeen({ storageKey, ids: readSeen(storageKey) });
  }
  const seenIds = seen.ids;

  const projectNames = useMemo(
    () => [...new Set(reports.map((report) => report.project).filter((name): name is string => Boolean(name)))],
    [reports],
  );

  // Polled on the bell's cadence: an issue's attention state changes without
  // a new alert report, so the alert poll alone would leave the badge stale.
  const queries = useQueries({
    queries: projectNames.map((projectName) => ({
      ...projectIssuesQueryOptions(projectName),
      refetchInterval: BELL_POLL_MS,
    })),
  });

  const issuesByProject = useMemo(() => {
    const byProject = new Map<string, IssueInfo[]>();
    projectNames.forEach((projectName, index) => {
      byProject.set(projectName, queries[index]?.data ?? []);
    });
    return byProject;
  }, [projectNames, queries]);

  // A project whose issue list never loaded has unknown attention, not none.
  // A failed background refetch keeps its last-known data and is not listed.
  const failedProjects = useMemo(
    () => projectNames.filter((_, index) => queries[index]?.isError && queries[index]?.data === undefined),
    [projectNames, queries],
  );

  const retryFailed = useCallback(() => {
    queries.forEach((query) => {
      if (query.isError) void query.refetch();
    });
  }, [queries]);

  const items = useMemo(() => collectAttentionItems(reports, issuesByProject), [reports, issuesByProject]);
  const unreadCount = useMemo(() => countUnreadAttention(items, seenIds), [items, seenIds]);

  const markAllSeen = useCallback(() => {
    const next = [...new Set([...seenIds, ...items.map((item) => item.id)])];
    setSeen({ storageKey, ids: next });
    writeSeen(storageKey, next);
  }, [items, seenIds, storageKey]);

  return { items, unreadCount, markAllSeen, failedProjects, retryFailed };
}
