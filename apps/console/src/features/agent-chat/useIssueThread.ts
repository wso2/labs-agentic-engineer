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

import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from "react";
import { useProjectIssues } from "../issues/api/issues";
import { forgetIssueClosed, issueMarkedClosed, onIssueClosedMark } from "./closedIssues";
import { chatStore, useIssueThreads, type IssueThreadActivity } from "./useProjectChat";

// An open issue has a chat of its own; a closed one has none (closing an issue
// removes its thread). Whether the issue in view is open is the issue list's
// word, unless the server has since refused its thread as closed (409
// `issue_closed`, `closedIssues`): that stands until the list shows it closed
// too. When an issue the user is on turns out closed, the main chat says its
// chat was removed.

/** Whether an issue's chat can be drawn: open; closed; or unknown until the list is read (or for an issue it lacks). */
export type IssueThreadState = "open" | "closed" | "unknown";

/** The issue's state for its chat; `issueNumber` null (no issue in view) is unknown. */
export function useIssueThreadState(projectName: string, issueNumber: number | null): IssueThreadState {
  const issues = useProjectIssues(projectName);
  const subscribe = useCallback((fn: () => void) => onIssueClosedMark(fn), []);
  const marked = useSyncExternalStore(subscribe, () => issueNumber !== null && issueMarkedClosed(projectName, issueNumber));
  const issue = issueNumber === null ? undefined : issues.data?.find((i) => i.Number === issueNumber);
  const listClosed = issue?.State === "closed";
  // The list has caught up: from now on it is the word, so a reopen shows.
  useEffect(() => {
    if (marked && listClosed && issueNumber !== null) forgetIssueClosed(projectName, issueNumber);
  }, [marked, listClosed, projectName, issueNumber]);
  if (marked || listClosed) return "closed";
  return issue ? "open" : "unknown";
}

/**
 * While an issue's card is open (`issueNumber`), tell the main chat when the
 * issue goes from open to closed: its chat was removed. A local line, never
 * sent to the agent; an issue already closed on arrival says nothing.
 */
export function useIssueThreadRemovedNote(projectName: string, issueNumber: number | null, state: IssueThreadState): void {
  const seen = useRef<{ key: string; state: IssueThreadState } | null>(null);
  useEffect(() => {
    const key = issueNumber === null ? null : `${projectName}#${issueNumber}`;
    const before = seen.current;
    seen.current = key ? { key, state } : null;
    if (key && before?.key === key && before.state === "open" && state === "closed") {
      chatStore.post(projectName, `Issue #${issueNumber} was closed; its chat was removed.`);
    }
  }, [projectName, issueNumber, state]);
}

/**
 * The project's issue chats that hold something (`useIssueThreads`), for the
 * open issues only: a closed issue's chat was removed with it.
 */
export function useOpenIssueThreads(projectName: string): IssueThreadActivity[] {
  const threads = useIssueThreads(projectName);
  const issues = useProjectIssues(projectName).data;
  const subscribe = useCallback((fn: () => void) => onIssueClosedMark(fn), []);
  // Which of them the server has refused as closed, as a string so it compares by value.
  const marked = useSyncExternalStore(subscribe, () =>
    threads
      .filter((t) => issueMarkedClosed(projectName, t.issueNumber))
      .map((t) => t.issueNumber)
      .join(","),
  );
  return useMemo(() => {
    const closed = new Set(marked.split(",").filter(Boolean).map(Number));
    return threads.filter(
      (t) => !closed.has(t.issueNumber) && issues?.find((i) => i.Number === t.issueNumber)?.State === "open",
    );
  }, [threads, issues, marked]);
}

