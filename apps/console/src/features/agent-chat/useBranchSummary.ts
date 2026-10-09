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

import { useEffect, useRef } from "react";
import type { ShellScope } from "../shell/scope";
import type { ChatItem } from "./chatLog";
import { chatStore, chatStoreFor } from "./useProjectChat";

// The Issues page has a chat of its own, a branch of the main one. When the
// user leaves the page after talking there, the main chat sums the visit up,
// so the conversation they come back to knows what happened in between. An
// issue's card over the page is not leaving: the user is still on the page.

/** The last line, as one line, cut to this many characters. */
const LINE_MAX = 140;

const isSpoken = (item: ChatItem) => item.kind === "user" || item.kind === "agent";

/** The text on one line, cut with an ellipsis when it is longer than `LINE_MAX`. */
function oneLine(text: string): string {
  const line = text.replace(/\s+/g, " ").trim();
  return line.length > LINE_MAX ? `${line.slice(0, LINE_MAX - 1)}…` : line;
}

/**
 * The project whose Issues page the user is on, if any, whatever card is over
 * it: its Questions card and an issue's card keep the user on the page.
 */
function issuesProject(scope: ShellScope): string | null {
  return scope.kind === "project" && scope.page === "issues" ? scope.projectName : null;
}

/** A summary note posted, by project: its id, and the messages it sums up. */
type Posted = Map<string, { id: string; messages: number }>;

/**
 * Put the summary in the project's main chat. That chat may not be loaded yet
 * (a reload or a link straight onto Issues): reading its history replaces its
 * items, so the note waits for it, or the load would wipe it.
 */
async function summarise(posted: Posted, projectName: string, spoken: number, line: string): Promise<void> {
  await chatStore.open(projectName);
  const last = chatStore.get(projectName).items.at(-1);
  const before = posted.get(projectName);
  const reuse = last?.kind === "note" && before?.id === last.id ? before : null;
  const messages = spoken + (reuse?.messages ?? 0);
  const text = `From Issues · ${messages} ${messages === 1 ? "message" : "messages"}${line ? ` · ${line}` : ""}`;
  const actions = [{ kind: "open-issues" as const, label: "Reopen" }];
  if (reuse) {
    chatStore.replaceNote(projectName, reuse.id, text, actions);
    posted.set(projectName, { id: reuse.id, messages });
    return;
  }
  posted.set(projectName, { id: chatStore.post(projectName, text, actions), messages });
}

/**
 * Mounted once, in the shell. While the user is on the Issues page it notes how
 * many messages the Issues thread had when they arrived; when they leave the
 * page (for another page, or another project), a "From Issues · N messages · <the
 * agent's last line>" note with a Reopen goes into that project's main chat.
 * A visit that said nothing leaves nothing. The next visit rewords the note
 * while it is still the last thing in the main chat, adding its messages,
 * rather than stacking another.
 */
export function useBranchSummary(scope: ShellScope): void {
  const project = issuesProject(scope);
  const previous = useRef<string | null>(null);
  /** How many messages each project's Issues thread had on arrival; set once the thread is read. */
  const arrival = useRef(new Map<string, number>());
  /** The summary note last posted per project, and the messages it sums up. */
  const posted = useRef<Posted>(new Map());

  useEffect(() => {
    if (!project) return;
    const issues = chatStoreFor("issues");
    const notice = () => {
      const chat = issues.get(project);
      if (chat.status === "ready" && !arrival.current.has(project)) arrival.current.set(project, chat.items.filter(isSpoken).length);
    };
    notice();
    return issues.subscribe(project, notice);
  }, [project]);

  useEffect(() => {
    const left = previous.current;
    previous.current = project;
    if (!left || left === project) return;
    const since = arrival.current.get(left);
    arrival.current.delete(left);
    if (since === undefined) return;
    const spoken = chatStoreFor("issues").get(left).items.filter(isSpoken).slice(since);
    if (spoken.length === 0) return;
    const replies = spoken.flatMap((i) => (i.kind === "agent" && i.text.trim() !== "" ? [i.text] : []));
    const line = replies.length > 0 ? oneLine(replies[replies.length - 1]!) : "";

    void summarise(posted.current, left, spoken.length, line);
  }, [project]);
}

