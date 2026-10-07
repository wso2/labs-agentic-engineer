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

import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type RefObject } from "react";
import { Box, Breadcrumbs, IconButton, InputBase, Link, Tooltip, Typography } from "@wso2/oxygen-ui";
import { ArrowRight, PanelLeftClose } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";
import { projectLabel, useProject } from "../../projects/api/queries";
import type { ComposeRequest } from "../../shell/chatPanel";
import type { BranchState } from "../../shell/useChatControls";
import { cardTitle, chatTopic, pageTitle, type ProjectCard, type ProjectPage } from "../../shell/scope";
import { useSpecFeature } from "../../spec/useSpecWorkspace";
import type { ChatItem } from "../chatLog";
import { turnScopeFor, type TurnScope } from "../turnScope";
import { chatViewFor, type ChatView } from "../chatView";
import { useOpenIssuesChat } from "../useOpenIssuesChat";
import { canSend, chatStoreFor, useProjectChat } from "../useProjectChat";
import { BranchSheet } from "./BranchSheet";
import { Thread } from "./Thread";
import { ThreadsMenu, type IssuesThread } from "./ThreadsMenu";

// The project's conversation, beside the main area: where the user is (the
// breadcrumb), the threads menu, the main chat's thread and composer. The
// Issues Page has its own agent and thread, a branch of the main chat: a Start
// row above the main composer until it is started, then a sheet over the main
// chat, or a link at the end of its thread while minimised (the shell keeps
// which, `useChatControls`). What a message is about follows from where the
// user is, and goes with every turn sent from here.

/** The project as the chat names it: its display name once read, its handle until then. */
function useProjectLabel(projectName: string): string {
  const project = useProject(projectName);
  return project.data ? projectLabel(project.data) : projectName;
}

const isSpoken = (item: ChatItem) => item.kind === "user" || item.kind === "agent";

/** The main chat's last line, for the strip over the Issues sheet. */
function lastLine(items: ChatItem[]): string | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]!;
    if ((isSpoken(item) || item.kind === "note") && "text" in item && item.text.trim()) {
      return item.text.replace(/\s+/g, " ").trim();
    }
  }
  return null;
}

/** A From Issues note in the main chat: the Issues branch was summed up there. */
const summarisesIssues = (item: ChatItem) =>
  item.kind === "note" && (item.actions ?? []).some((a) => a.kind === "open-issues");

function ScopeCrumb({
  projectName,
  page,
  card,
}: {
  projectName: string;
  page: ProjectPage;
  card: ProjectCard | null;
}) {
  const { orgHandle } = useSession();
  const label = useProjectLabel(projectName);
  const segments = [
    orgHandle ?? "Organization",
    label,
    // The overview is the project itself; any other Page is named.
    ...(page !== "overview" ? [pageTitle(page)] : []),
    ...(card ? [cardTitle(card)] : []),
  ];
  return (
    <Breadcrumbs
      aria-label="Chat scope"
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
      {segments.map((s, i) => (
        <Typography
          key={i}
          noWrap
          aria-current={i === segments.length - 1 ? "location" : undefined}
          sx={{
            fontSize: "inherit",
            display: "block",
            fontWeight: i === segments.length - 1 ? 600 : 400,
            color: "text.primary",
          }}
        >
          {s}
        </Typography>
      ))}
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
  composeRequest,
  onComposeApplied,
  focusSignal = null,
  focusActive = true,
  onFocusApplied,
}: {
  projectName: string;
  topic: string;
  note: string | null;
  scope: TurnScope;
  view: ChatView;
  composeRequest: ComposeRequest | null;
  onComposeApplied: (nonce: number) => void;
  /** A nonce that puts the cursor here: the user brought this chat to the front. Applied once. */
  focusSignal?: number | null;
  /** False while this composer is out of sight, so a signal waits for it. */
  focusActive?: boolean;
  /** Called with the signal's nonce once applied; the panel clears it. */
  onFocusApplied?: (nonce: number) => void;
}) {
  const chat = useProjectChat(projectName, view);
  const statusId = useId();
  const [draft, setDraft] = useState("");
  const input = useRef<HTMLTextAreaElement | null>(null);
  // Each request applies once, and only here if it is for this project's view.
  const appliedNonce = useRef(0);
  const [pendingFocus, setPendingFocus] = useState(false);
  const request =
    composeRequest && composeRequest.projectName === projectName && composeRequest.view === view ? composeRequest : null;
  useEffect(() => {
    if (!request || request.nonce === appliedNonce.current) return;
    appliedNonce.current = request.nonce;
    setDraft(request.text);
    setPendingFocus(true);
    onComposeApplied(request.nonce);
  }, [request, onComposeApplied]);
  const appliedFocus = useRef<number | null>(null);
  useEffect(() => {
    if (focusSignal === null || !focusActive || focusSignal === appliedFocus.current) return;
    appliedFocus.current = focusSignal;
    setPendingFocus(true);
    onFocusApplied?.(focusSignal);
  }, [focusSignal, focusActive, onFocusApplied]);
  // After the draft has committed, so the cursor lands past the new text, and
  // once the input is enabled: a disabled field takes no focus.
  const enabled = chat.status === "ready";
  useEffect(() => {
    if (!pendingFocus || !enabled || !focusActive) return;
    const el = input.current;
    if (!el) return;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
    setPendingFocus(false);
  }, [pendingFocus, enabled, focusActive]);
  const ready = canSend(chat);
  const status = composerNote(chat);

  const send = () => {
    const text = draft.trim();
    if (!text || !ready) return;
    setDraft("");
    void chatStoreFor(view).send(projectName, text, scope).then((sent) => {
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
          placeholder={view === "issues" ? "Describe what's broken, or what you need…" : "Tell the agent what to change…"}
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

/** Above the main composer on the Issues Page, until its chat is started. */
function StartRow({ onStart }: { onStart: () => void }) {
  return (
    <Typography
      data-testid="branch-start"
      variant="caption"
      color="text.secondary"
      component="p"
      sx={{ borderTop: 1, borderColor: "divider", px: 1.75, pt: 1 }}
    >
      Work on Issues here ·{" "}
      <Link component="button" type="button" variant="caption" onClick={onStart} sx={{ verticalAlign: "baseline" }}>
        Start
      </Link>
    </Typography>
  );
}

/** At the end of the main thread while the Issues chat is minimised. */
function BranchLink({ onOpen, linkRef }: { onOpen: () => void; linkRef: RefObject<HTMLButtonElement | null> }) {
  return (
    <Typography
      data-testid="branch-link"
      variant="caption"
      color="text.secondary"
      component="p"
      sx={{ px: 1.75, pb: 1 }}
    >
      ↳ Issues · its own chat ·{" "}
      <Link
        ref={linkRef}
        component="button"
        type="button"
        variant="caption"
        onClick={onOpen}
        sx={{ verticalAlign: "baseline" }}
      >
        Open ↑
      </Link>
    </Typography>
  );
}

/**
 * The chat panel: scope breadcrumb, threads menu, the project's main chat, and
 * on the Issues Page the Issues chat stacked on it.
 */
export function ChatPanel({
  projectName,
  page,
  card,
  specFile,
  composeRequest,
  onComposeApplied,
  branch,
  onStartBranch,
  onMinimiseBranch,
  onClose,
}: {
  projectName: string;
  /** The project Page in view, under any card. */
  page: ProjectPage;
  card: ProjectCard | null;
  /** The spec card's open file; a feature narrows what the chat is about. */
  specFile: string | null;
  /** The shell's pending ask to fill a composer, applied once per nonce. */
  composeRequest: ComposeRequest | null;
  /** Called with the request's nonce once a composer has applied it; the shell clears it. */
  onComposeApplied: (nonce: number) => void;
  /** The Issues chat's place in the panel, kept by the shell. */
  branch: BranchState;
  /** Start the Issues chat, or bring a minimised one back up. */
  onStartBranch: () => void;
  /** Fold the Issues chat down: the main chat comes to the front. */
  onMinimiseBranch: () => void;
  onClose: () => void;
}) {
  const feature = useSpecFeature(projectName, card === "spec" ? specFile : null);
  const scope = turnScopeFor(card, feature);
  const label = useProjectLabel(projectName);
  const main = useProjectChat(projectName, "main");
  const issues = useProjectChat(projectName, "issues");
  const openIssuesChat = useOpenIssuesChat(projectName);
  // The nonce of a pending ask to put the cursor in the sheet; the sheet's
  // composer clears it once it has applied it, so it never fires twice.
  const focusNonce = useRef(0);
  const [focusSheet, setFocusSheet] = useState<number | null>(null);
  const requestSheetFocus = () => setFocusSheet(++focusNonce.current);
  const sheetFocusApplied = useCallback((nonce: number) => setFocusSheet((n) => (n === nonce ? null : n)), []);
  // Minimising hides the strip button that had focus: focus goes to the link
  // that brings the sheet back, rather than falling to the page.
  const branchLink = useRef<HTMLButtonElement | null>(null);
  const [focusLink, setFocusLink] = useState(false);

  // `chatViewFor` names the chat this page has of its own; the main chat is
  // always here, and a page with its own (the Issues Page) stacks it on top.
  const branchHere = chatViewFor(page, card) === "issues";
  const sheetUp = branchHere && branch.started && !branch.minimised;
  // The sheet stays mounted, out of sight, while an issue's card is open on
  // the Issues page, so its draft survives; it is shown only where it belongs.
  const sheetMounted = page === "issues" && branch.started;
  const sheetHidden = !sheetUp;
  const minimise = () => {
    onMinimiseBranch();
    setFocusLink(true);
  };
  useEffect(() => {
    if (!focusLink || !branchHere || !branch.minimised) return;
    branchLink.current?.focus();
    setFocusLink(false);
  }, [focusLink, branchHere, branch.minimised]);
  const mainTopic = chatTopic(card, feature ? `${feature.id} ${feature.name}` : null, "main");
  const issuesTopic = chatTopic(card, null, "issues");
  const bringUp = () => {
    onStartBranch();
    requestSheetFocus();
  };

  const issuesCount = issues.items.filter(isSpoken).length;
  const issuesThread: IssuesThread | null =
    issues.items.length > 0 || branch.started
      ? {
          count: issuesCount,
          state: branch.started ? "open" : main.items.some(summarisesIssues) ? "summarised" : null,
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
        <ScopeCrumb projectName={projectName} page={page} card={card} />
        <ThreadsMenu
          projectLabel={label}
          mainCount={main.items.filter(isSpoken).length}
          issues={issuesThread}
          current={sheetUp ? "issues" : "main"}
          onMain={() => {
            if (sheetUp) minimise();
          }}
          onIssues={() => {
            requestSheetFocus();
            void openIssuesChat();
          }}
        />
        <Tooltip title="Hide agent chat">
          <IconButton size="small" aria-label="Hide agent chat" onClick={onClose}>
            <PanelLeftClose size={16} />
          </IconButton>
        </Tooltip>
      </Box>
      <Box sx={{ flex: 1, minHeight: 0, position: "relative", display: "flex", flexDirection: "column" }}>
        {/* Under the sheet the main chat is still drawn, but out of reach. */}
        <Box
          data-testid="main-chat"
          inert={sheetUp}
          sx={{ flex: 1, minHeight: 0, display: "flex", flexDirection: "column" }}
        >
          <Thread projectName={projectName} view="main" />
          {branchHere && branch.started && branch.minimised && <BranchLink onOpen={bringUp} linkRef={branchLink} />}
          {branchHere && !branch.started && <StartRow onStart={bringUp} />}
          <Composer
            projectName={projectName}
            topic={mainTopic.topic}
            note={mainTopic.note}
            scope={scope}
            view="main"
            composeRequest={composeRequest}
            onComposeApplied={onComposeApplied}
          />
        </Box>
        {sheetMounted && (
          <BranchSheet projectLabel={label} peek={lastLine(main.items)} hidden={sheetHidden} onMinimise={minimise}>
            <Thread projectName={projectName} view="issues" />
            <Composer
              projectName={projectName}
              topic={issuesTopic.topic}
              note={issuesTopic.note}
              scope={scope}
              view="issues"
              composeRequest={composeRequest}
              onComposeApplied={onComposeApplied}
              focusSignal={focusSheet}
              focusActive={sheetUp}
              onFocusApplied={sheetFocusApplied}
            />
          </BranchSheet>
        )}
      </Box>
    </Box>
  );
}
