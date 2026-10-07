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

import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { Box, Breadcrumbs, IconButton, InputBase, Tooltip, Typography } from "@wso2/oxygen-ui";
import { ArrowRight, PanelLeftClose } from "@wso2/oxygen-ui-icons-react";
import { useSession } from "../../../auth/SessionContext";
import { projectLabel, useProject } from "../../projects/api/queries";
import type { ComposeRequest } from "../../shell/chatPanel";
import { cardTitle, chatTopic, pageTitle, type ProjectCard, type ProjectPage } from "../../shell/scope";
import { useSpecFeature } from "../../spec/useSpecWorkspace";
import { turnScopeFor, type TurnScope } from "../turnScope";
import { chatViewFor, type ChatView } from "../chatView";
import { canSend, chatStoreFor, useProjectChat } from "../useProjectChat";
import { Thread } from "./Thread";

// The project's conversation, beside the main area: where the user is (the
// breadcrumb), the thread, and the composer. The Issues Page has its own agent
// and thread; everywhere else is the project's main chat. What a message is
// about follows from where the user is, and goes with every turn sent from here.

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
  const project = useProject(projectName);
  const segments = [
    orgHandle ?? "Organization",
    project.data ? projectLabel(project.data) : projectName,
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
}: {
  projectName: string;
  topic: string;
  note: string | null;
  scope: TurnScope;
  view: ChatView;
  composeRequest: ComposeRequest | null;
  onComposeApplied: (nonce: number) => void;
}) {
  const chat = useProjectChat(projectName, view);
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
            ...(status ? { "aria-describedby": "composer-status" } : {}),
          }}
          sx={{ flex: 1, fontSize: "0.875rem", py: 0.5 }}
        />
        <IconButton size="small" aria-label="Send message" disabled={!ready || !draft.trim()} onClick={send}>
          <ArrowRight size={16} />
        </IconButton>
      </Box>
      {status && (
        <Typography
          id="composer-status"
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

/** The chat panel: scope breadcrumb, the project's thread, and the composer. */
export function ChatPanel({
  projectName,
  page,
  card,
  specFile,
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
  /** The shell's pending ask to fill the composer, applied once per nonce. */
  composeRequest: ComposeRequest | null;
  /** Called with the request's nonce once a composer has applied it; the shell clears it. */
  onComposeApplied: (nonce: number) => void;
  onClose: () => void;
}) {
  const feature = useSpecFeature(projectName, card === "spec" ? specFile : null);
  const scope = turnScopeFor(card, feature);
  const view = chatViewFor(page, card);
  const { topic, note } = chatTopic(card, feature ? `${feature.id} ${feature.name}` : null, view);
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
        <Tooltip title="Hide agent chat">
          <IconButton size="small" aria-label="Hide agent chat" onClick={onClose}>
            <PanelLeftClose size={16} />
          </IconButton>
        </Tooltip>
      </Box>
      <Thread projectName={projectName} view={view} />
      <Composer
        projectName={projectName}
        topic={topic}
        note={note}
        scope={scope}
        view={view}
        composeRequest={composeRequest}
        onComposeApplied={onComposeApplied}
      />
    </Box>
  );
}
