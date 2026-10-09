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

// The issues whose chat thread the server said is gone (409 `issue_closed`):
// closing an issue removes its thread, so the issue is closed, whatever the
// issue list read before says. GitHub's list can lag a close; a mark stands
// until the list itself shows the issue closed, and from then on the list is
// the word (`useIssueThreadState`). Kept in this tab only.

const marks = new Set<string>();
/** Told of each change: the issue, and whether it is now marked. */
type Listener = (projectName: string, issueNumber: number, marked: boolean) => void;
const listeners = new Set<Listener>();

const key = (projectName: string, issueNumber: number) => `${projectName}#${issueNumber}`;

/** The server said this issue's thread is gone: the issue is closed. */
export function markIssueClosed(projectName: string, issueNumber: number): void {
  marks.add(key(projectName, issueNumber));
  for (const fn of listeners) fn(projectName, issueNumber, true);
}

/** The issue list shows it closed now: the mark has done its work. */
export function forgetIssueClosed(projectName: string, issueNumber: number): void {
  if (!marks.delete(key(projectName, issueNumber))) return;
  for (const fn of listeners) fn(projectName, issueNumber, false);
}

export function issueMarkedClosed(projectName: string, issueNumber: number): boolean {
  return marks.has(key(projectName, issueNumber));
}

/** Be told when an issue is marked closed, or its mark goes; returns the unsubscribe. */
export function onIssueClosedMark(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
