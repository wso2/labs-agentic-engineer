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

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { chatViewFor } from "../agent-chat/chatView";
import { chatStoreFor } from "../agent-chat/useProjectChat";
import type { ChatPanelControls, ComposeRequest, ComposeTarget } from "./chatPanel";
import type { ShellScope } from "./scope";

// What the shell keeps for the chat panel: the request to fill a composer, and
// the branches' places in the panel. The panel always holds the project's
// main chat; on the Issues page the Issues chat is a branch of it, drawn as a
// sheet over it once started. It starts when the user asks for it (Start, New
// Issue, Create Issue, the threads menu, Reopen) or when a turn runs in it, and
// is put away again when the user leaves the Issues page: the main chat then
// sums the visit up (`useBranchSummary`). Kept per project, in this tab only.
// An open issue's own chat is a branch too, drawn over the main chat on the
// issue's card: it is up on arrival, can be minimised while the user stays on
// the card, and is up again on the next visit.

/** The Issues chat's place in the panel, for one project. */
export interface BranchState {
  /** Shown over the main chat, or as a link at its end while minimised. */
  started: boolean;
  /** Folded down to the link: the main chat is in front. */
  minimised: boolean;
}

const NOT_STARTED: BranchState = { started: false, minimised: false };
const OPEN: BranchState = { started: true, minimised: false };
const MINIMISED: BranchState = { started: true, minimised: true };

/** An issue's chat in a project, as one key. */
const issueKey = (projectName: string, issueNumber: number) => `${projectName}#${issueNumber}`;

export function useChatControls(scope: ShellScope, openChat: () => void) {
  const project = scope.kind === "project" ? scope : null;

  // The request lives here, not in the composer: the composer mounts when the
  // chat opens, after `compose` has been asked. It targets the chat the caller
  // names (a move to another page names where it is going), else the view and
  // project in focus when asked, and is cleared once a composer applies it.
  const [composeRequest, setComposeRequest] = useState<ComposeRequest | null>(null);
  const composeNonce = useRef(0);
  const inFocus = useRef<ComposeTarget | null>(null);
  inFocus.current = project
    ? {
        projectName: project.projectName,
        view: chatViewFor(project.page, project.card, project.issueNumber),
        ...(project.issueNumber !== null ? { issueNumber: project.issueNumber } : {}),
      }
    : null;
  const opener = useRef(openChat);
  opener.current = openChat;

  const [branches, setBranches] = useState<ReadonlyMap<string, BranchState>>(new Map());
  const setBranch = useCallback((projectName: string, next: (current: BranchState) => BranchState | null) => {
    setBranches((all) => {
      const current = all.get(projectName) ?? NOT_STARTED;
      const updated = next(current);
      if (updated === current) return all;
      const copy = new Map(all);
      if (updated && updated.started) copy.set(projectName, updated);
      else copy.delete(projectName);
      return copy;
    });
  }, []);

  // The issue in view (its card, or the Questions card answering its chat),
  // and which issue's chat the user minimised there (one at a time: leaving
  // the issue brings its chat back up for the next visit).
  const issueInView = project && project.issueNumber !== null ? issueKey(project.projectName, project.issueNumber) : null;
  const [minimisedIssue, setMinimisedIssue] = useState<string | null>(null);
  const raiseIssue = useCallback((key: string) => setMinimisedIssue((m) => (m === key ? null : m)), []);

  const controls = useMemo<ChatPanelControls>(
    () => ({
      open: () => opener.current(),
      compose: (text, explicit) => {
        const target = explicit ?? inFocus.current;
        if (target) {
          if (target.view === "issues") setBranch(target.projectName, () => OPEN);
          if (target.view === "issue" && target.issueNumber !== undefined) raiseIssue(issueKey(target.projectName, target.issueNumber));
          setComposeRequest({ text, ...target, nonce: ++composeNonce.current });
        }
        opener.current();
      },
      startBranch: (view, projectName, issueNumber) => {
        const name = projectName ?? inFocus.current?.projectName;
        if (!name) return;
        if (view === "issue") {
          const n = issueNumber ?? inFocus.current?.issueNumber;
          if (n !== undefined) raiseIssue(issueKey(name, n));
          return;
        }
        setBranch(name, () => OPEN);
      },
    }),
    [setBranch, raiseIssue],
  );
  const clearComposeRequest = useCallback(
    (nonce: number) => setComposeRequest((current) => (current?.nonce === nonce ? null : current)),
    [],
  );

  // The Issues page in view, whatever card is over it: its Questions card and
  // an issue's card keep the user on the page, so the branch stays.
  const issuesPage = project?.page === "issues" ? project.projectName : null;
  const previous = useRef(issuesPage);
  useEffect(() => {
    const left = previous.current;
    previous.current = issuesPage;
    if (left && left !== issuesPage) setBranch(left, () => null);
  }, [issuesPage, setBranch]);

  // A turn running in the Issues chat (one sent from elsewhere, or still going
  // when the user comes back) starts the branch, so it can be seen; one the
  // user minimised stays down.
  useEffect(() => {
    if (!issuesPage) return;
    const store = chatStoreFor("issues");
    const notice = () => {
      if (store.get(issuesPage).turn.phase === "idle") return;
      setBranch(issuesPage, (current) => (current.started ? current : OPEN));
    };
    notice();
    const stop = store.subscribe(issuesPage, notice);
    const unwatch = store.watch(issuesPage);
    return () => {
      stop();
      unwatch();
    };
  }, [issuesPage, setBranch]);

  const previousIssue = useRef(issueInView);
  useEffect(() => {
    if (previousIssue.current === issueInView) return;
    previousIssue.current = issueInView;
    setMinimisedIssue(null);
  }, [issueInView]);

  const branch = project ? (branches.get(project.projectName) ?? NOT_STARTED) : NOT_STARTED;
  /** The chat of the issue in view: up unless minimised there; null with no issue in view. */
  const issueBranch = issueInView ? (minimisedIssue === issueInView ? MINIMISED : OPEN) : null;
  const projectName = project?.projectName ?? null;
  // The branch in view: the issue's on its card, else the Issues chat.
  const minimiseBranch = useCallback(() => {
    if (issueInView) setMinimisedIssue(issueInView);
    else if (projectName) setBranch(projectName, (current) => (current.started ? MINIMISED : current));
  }, [issueInView, projectName, setBranch]);
  const startBranch = useCallback(() => {
    if (issueInView) raiseIssue(issueInView);
    else if (projectName) setBranch(projectName, () => OPEN);
  }, [issueInView, projectName, setBranch, raiseIssue]);

  return { controls, composeRequest, clearComposeRequest, branch, issueBranch, startBranch, minimiseBranch };
}
