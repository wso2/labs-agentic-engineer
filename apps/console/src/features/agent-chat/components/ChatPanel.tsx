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

import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { useNavigate } from "@tanstack/react-router";
import { Box, Breadcrumbs, IconButton, InputBase, Link, Tooltip, Typography } from "@wso2/oxygen-ui";
import { ArrowRight, PanelLeftClose } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";
import { projectLabel, useProject } from "../../projects/api/queries";
import type { ComposeRequest } from "../../shell/chatPanel";
import { chatTopic, type ProjectCard, type ProjectPage } from "../../shell/scope";
import { useSpecFeature } from "../../spec/useSpecWorkspace";
import type { ChatItem } from "../chatLog";
import { turnScopeFor, type TurnScope } from "../turnScope";
import { chatViewFor, type ChatView } from "../chatView";
import { useIssueThreadRemovedNote, useIssueThreadState, useOpenIssueThreads } from "../useIssueThread";
import { useOpenIssueChat, useOpenIssuesChat } from "../useOpenIssuesChat";
import { canSend, chatStoreFor, useProjectChat } from "../useProjectChat";
import { Thread } from "./Thread";
import { ThreadsMenu, type IssuesThread } from "./ThreadsMenu";

// The project's conversation, beside the main area: one thread at a time, the
// one of the page in view. Everywhere in a project that is the main chat; the
// Issues Page has its own agent and thread (the Issues chat), and an open
// issue's card has its own (`Issues › #N`). The thread takes the whole panel:
// the breadcrumb names it and links back up the path (project → main chat,
// Issues → Issues chat), and the threads menu lists them all. A closed issue
// has no chat: on its card the panel holds the main chat, and an issue found
// closed while its card is open leaves a line there. What a message is about
// follows from where the user is, and goes with every turn sent from here.

/** The project as the chat names it: its display name once read, its handle until then. */
function useProjectLabel(projectName: string): string {
  const project = useProject(projectName);
  return project.data ? projectLabel(project.data) : projectName;
}

const isSpoken = (item: ChatItem) => item.kind === "user" || item.kind === "agent";

/** A From Issues note in the main chat: the Issues branch was summed up there. */
const summarisesIssues = (item: ChatItem) =>
  item.kind === "note" && (item.actions ?? []).some((a) => a.kind === "open-issues");

/** One step of the breadcrumb: its name, and where it goes (none for the thread in view). */
interface Crumb {
  label: string;
  go?: () => void;
}

/** The thread in view as a path, `org › project › Issues › #N`; every step before it goes back up. */
function ThreadCrumb({ projectName, view, issueNumber }: { projectName: string; view: ChatView; issueNumber: number | null }) {
  const { orgHandle } = useSession();
  const label = useProjectLabel(projectName);
  const navigate = useNavigate();
  const crumbs: Crumb[] = [
    { label: orgHandle ?? "Organization", go: () => void navigate({ to: "/" }) },
    { label, go: () => void navigate({ to: "/projects/$projectName", params: { projectName } }) },
    ...(view !== "main"
      ? [{ label: "Issues", go: () => void navigate({ to: "/projects/$projectName/issues", params: { projectName } }) }]
      : []),
    ...(view === "issue" && issueNumber !== null ? [{ label: `#${issueNumber}` }] : []),
  ];
  const last = crumbs.length - 1;
  return (
    <Breadcrumbs
      aria-label="Chat thread"
      separator="›"
      sx={{
        flex: 1,
        minWidth: 0,
        fontSize: "0.8125rem",
        "& .MuiBreadcrumbs-ol": { flexWrap: "nowrap" },
        "& .MuiBreadcrumbs-li": { minWidth: 0 },
        "& .MuiBreadcrumbs-separator": { mx: 0.75 },
      }}
    >
      {crumbs.map((c, i) =>
        i === last ? (
          <Typography
            key={i}
            noWrap
            aria-current="location"
            sx={{ fontSize: "inherit", display: "block", fontWeight: 600, color: "text.primary" }}
          >
            {c.label}
          </Typography>
        ) : (
          <Link
            key={i}
            component="button"
            type="button"
            underline="hover"
            onClick={c.go}
            sx={{
              fontSize: "inherit",
              display: "block",
              maxWidth: "100%",
              overflow: "hidden",
              textOverflow: "ellipsis",
              whiteSpace: "nowrap",
              color: "text.primary",
              verticalAlign: "baseline",
            }}
          >
            {c.label}
          </Link>
        ),
      )}
    </Breadcrumbs>
  );
}

/** What the composer says under the field, when there is something to say. */
function composerNote(chat: ReturnType<typeof useProjectChat>): string | null {
  if (chat.status === "loading") return "Loading the conversation…";
  if (chat.turn.phase === "starting") return "Sending…";
  if (chat.turn.phase === "running") return "The agent is working. Your message can go when it's done.";
  return null;
}

function Composer({
  projectName,
  topic,
  note,
  scope,
  view,
  issueNumber,
  composeRequest,
  onComposeApplied,
}: {
  projectName: string;
  topic: string;
  note: string | null;
  scope: TurnScope;
  view: ChatView;
  /** The issue whose chat this is, in the issue view. */
  issueNumber?: number;
  composeRequest: ComposeRequest | null;
  onComposeApplied: (nonce: number) => void;
}) {
  const chat = useProjectChat(projectName, view, issueNumber);
  const statusId = useId();
  const [draft, setDraft] = useState("");
  const input = useRef<HTMLTextAreaElement | null>(null);
  // Each request applies once, and only here if it is for this project's view.
  const appliedNonce = useRef(0);
  const [pendingFocus, setPendingFocus] = useState(false);
  const request =
    composeRequest &&
    composeRequest.projectName === projectName &&
    composeRequest.view === view &&
    composeRequest.issueNumber === issueNumber
      ? composeRequest
      : null;
  useEffect(() => {
    if (!request || request.nonce === appliedNonce.current) return;
    appliedNonce.current = request.nonce;
    setDraft(request.text);
    setPendingFocus(true);
    onComposeApplied(request.nonce);
  }, [request, onComposeApplied]);
  // After the draft has committed, so the cursor lands past the new text, and
  // once the input is enabled: a disabled field takes no focus.
  const enabled = chat.status === "ready";
  useEffect(() => {
    if (!pendingFocus || !enabled) return;
    const el = input.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
    setPendingFocus(false);
  }, [pendingFocus, enabled]);
  const ready = canSend(chat);
  const status = composerNote(chat);

  const send = () => {
    const text = draft.trim();
    if (!text || !ready) return;
    setDraft("");
    void chatStoreFor(view, issueNumber).send(projectName, text, scope).then((sent) => {
      // Refused: the words come back, unless the user has typed again.
      if (!sent) setDraft((current) => current || text);
    });
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send();
    }
  };

  return (
    <Box sx={{ borderTop: 1, borderColor: "divider", px: 1.5, pt: 1.25, pb: 1.5 }}>
      <Typography variant="caption" color="text.secondary" component="p" sx={{ mb: 0.75, ml: 0.25 }}>
        Talking about{" "}
        <Box component="b" sx={{ color: "text.primary", fontWeight: 600 }}>
          {topic}
        </Box>
        .{note && ` ${note}`}
      </Typography>
      <Box
        sx={{
          display: "flex",
          alignItems: "flex-end",
          gap: 0.75,
          border: 1,
          borderColor: "divider",
          borderRadius: 2.5,
          bgcolor: "background.paper",
          py: 0.75,
          pr: 0.75,
          pl: 1.5,
        }}
      >
        <InputBase
          multiline
          maxRows={5}
          value={draft}
          inputRef={input}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={!enabled}
          placeholder={
            view === "issues"
              ? "Describe what's broken, or what you need…"
              : view === "issue"
                ? "Ask about this issue, or what to do with it…"
                : "Tell the agent what to change…"
          }
          inputProps={{
            "aria-label": "Message the agent",
            ...(status ? { "aria-describedby": statusId } : {}),
          }}
          sx={{ flex: 1, fontSize: "0.875rem", py: 0.5 }}
        />
        <IconButton size="small" aria-label="Send message" disabled={!ready || !draft.trim()} onClick={send}>
          <ArrowRight size={16} />
        </IconButton>
      </Box>
      {status && (
        <Typography
          id={statusId}
          role="status"
          variant="caption"
          color="text.secondary"
          component="p"
          sx={{ mt: 0.75, ml: 0.25 }}
        >
          {status}
        </Typography>
      )}
    </Box>
  );
}

/**
 * The chat panel: the breadcrumb of the thread in view, the threads menu, and
 * that thread with its composer: the main chat, the Issues chat on the Issues
 * Page, or an open issue's chat on its card.
 */
export function ChatPanel({
  projectName,
  page,
  card,
  specFile,
  issueNumber,
  composeRequest,
  onComposeApplied,
  onClose,
}: {
  projectName: string;
  /** The project Page in view, under any card. */
  page: ProjectPage;
  card: ProjectCard | null;
  /** The spec card's open file; a feature narrows what the chat is about. */
  specFile: string | null;
  /** The issue in view: its card, or the Questions card answering its chat; null elsewhere. */
  issueNumber: number | null;
  /** The shell's pending ask to fill a composer, applied once per nonce. */
  composeRequest: ComposeRequest | null;
  /** Called with the request's nonce once a composer has applied it; the shell clears it. */
  onComposeApplied: (nonce: number) => void;
  onClose: () => void;
}) {
  const feature = useSpecFeature(projectName, card === "spec" ? specFile : null);
  const scope = turnScopeFor(card, feature);
  const label = useProjectLabel(projectName);
  const navigate = useNavigate();
  const main = useProjectChat(projectName, "main");
  const issues = useProjectChat(projectName, "issues");
  const openIssuesChat = useOpenIssuesChat(projectName);
  const openIssueChat = useOpenIssueChat(projectName);
  const issueThreads = useOpenIssueThreads(projectName);
  // An issue's chat is drawn only while the issue is known open; one found
  // closed while its card is open leaves a line in the main chat.
  const issueState = useIssueThreadState(projectName, issueNumber);
  useIssueThreadRemovedNote(projectName, issueNumber, issueState);

  // The page's own thread; an issue's card without an open issue holds the main chat.
  const pageView = chatViewFor(page, card, issueNumber);
  const view: ChatView = pageView === "issue" && issueState !== "open" ? "main" : pageView;
  const threadIssue = view === "issue" ? (issueNumber ?? undefined) : undefined;
  const ofIssue = threadIssue !== undefined ? { issueNumber: threadIssue } : {};
  const topic = chatTopic(card, feature ? `${feature.id} ${feature.name}` : null, view, threadIssue);

  const issuesThread: IssuesThread | null =
    issues.items.length > 0 || view === "issues"
      ? {
          count: issues.items.filter(isSpoken).length,
          state: page === "issues" ? "open" : main.items.some(summarisesIssues) ? "summarised" : null,
        }
      : null;

  return (
    <Box
      component="aside"
      aria-label="Agent chat"
      sx={{ height: "100%", display: "flex", flexDirection: "column", minWidth: 0 }}
    >
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: 1,
          pl: 1.75,
          pr: 1.5,
          py: 1.25,
          borderBottom: 1,
          borderColor: "divider",
        }}
      >
        <ThreadCrumb projectName={projectName} view={view} issueNumber={threadIssue ?? null} />
        <ThreadsMenu
          projectLabel={label}
          mainCount={main.items.filter(isSpoken).length}
          issues={issuesThread}
          issueThreads={issueThreads}
          current={view === "issue" && threadIssue !== undefined ? threadIssue : view === "issues" ? "issues" : "main"}
          onMain={() => void navigate({ to: "/projects/$projectName", params: { projectName } })}
          onIssues={() => void openIssuesChat()}
          onIssue={(n) => void openIssueChat(n)}
        />
        <Tooltip title="Hide agent chat">
          <IconButton size="small" aria-label="Hide agent chat" onClick={onClose}>
            <PanelLeftClose size={16} />
          </IconButton>
        </Tooltip>
      </Box>
      {/* Keyed by thread: a draft belongs to the thread it was typed in. */}
      <Box
        key={`${view}#${threadIssue ?? ""}`}
        component="section"
        aria-label={view === "issue" ? `Issue #${threadIssue} chat` : view === "issues" ? "Issues chat" : "Main chat"}
        sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
      >
        <Thread projectName={projectName} view={view} {...ofIssue} />
        <Composer
          projectName={projectName}
          topic={topic.topic}
          note={topic.note}
          scope={scope}
          view={view}
          {...ofIssue}
          composeRequest={composeRequest}
          onComposeApplied={onComposeApplied}
        />
      </Box>
    </Box>
  );
}
