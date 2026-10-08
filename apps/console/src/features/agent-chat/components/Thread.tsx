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

import { Fragment, useEffect, useMemo, useState, type ReactNode } from "react";
import { Box, Button, CircularProgress, Typography } from "@wso2/oxygen-ui";
import { CircleAlert, Sparkles } from "@wso2/oxygen-ui-icons-react";
import { visuallyHidden } from "../../../components/visuallyHidden";
import { usePrototypeNotes } from "../../prototype/usePrototypeNotes";
import { usePrototypeRequestsText } from "../../prototype/usePrototypeRequestsText";
import { useSpecModel } from "../../spec/useSpecWorkspace";
import { interviewWriteUp, openQuestionId, userLineText, type ChatItem } from "../chatLog";
import type { PrototypeFeedback } from "../turnScope";
import { chatStore, useProjectChat } from "../useProjectChat";
import { ActivityLine } from "./ActivityLine";
import { InterviewFollowUp } from "./InterviewFollowUp";
import { NoteActions } from "./NoteActions";
import { QuestionsPointer } from "./QuestionsPointer";

// The conversation, oldest first: what the user said, what the agent said,
// a compact line for each file it wrote, and its questions as cards. After an
// interview has written its feature, the walk and the next feature follow;
// after a prototype turn has written a valid prototype, Open prototype.

function AgentMark() {
  return (
    <Box
      aria-hidden
      sx={{
        width: 22,
        height: 22,
        flexShrink: 0,
        mt: 0.25,
        borderRadius: 1.5,
        display: "grid",
        placeItems: "center",
        color: "primary.main",
        bgcolor: "rgba(var(--oxygen-palette-primary-mainChannel) / 0.12)",
      }}
    >
      <Sparkles size={13} />
    </Box>
  );
}

/** A prototype review's message reads as its requests, not as the `/prototype` line it went over the wire as. */
function PrototypeRequests({ projectName, feedback }: { projectName: string; feedback: PrototypeFeedback }) {
  return <>{usePrototypeRequestsText(projectName, feedback)}</>;
}

function UserRow({ projectName, item }: { projectName: string; item: Extract<ChatItem, { kind: "user" }> }) {
  return (
    <Box sx={{ alignSelf: "flex-end", maxWidth: "88%", display: "flex", flexDirection: "column", alignItems: "flex-end" }}>
      <Box
        sx={{
          bgcolor: "var(--aep-shell-user-bubble)",
          borderRadius: 3,
          px: 1.5,
          py: 1,
          opacity: item.state === "sending" ? 0.7 : 1,
        }}
      >
        <Typography variant="body2" sx={{ whiteSpace: "pre-wrap" }}>
          {item.prototypeFeedback ? (
            <PrototypeRequests projectName={projectName} feedback={item.prototypeFeedback} />
          ) : (
            userLineText(item.text)
          )}
        </Typography>
      </Box>
      {item.state === "failed" && (
        <Typography variant="caption" color="error.main" sx={{ mt: 0.25 }}>
          Not sent
        </Typography>
      )}
    </Box>
  );
}

function AgentRow({ text }: { text: string }) {
  return (
    <Box sx={{ display: "flex", gap: 1.25 }}>
      <AgentMark />
      <Typography variant="body2" sx={{ flex: 1, minWidth: 0, whiteSpace: "pre-wrap" }}>
        <Box component="span" sx={visuallyHidden}>
          Agent:{" "}
        </Box>
        {text}
      </Typography>
    </Box>
  );
}

function ErrorRow({ text }: { text: string }) {
  return (
    <Box role="alert" sx={{ display: "flex", gap: 0.75, alignItems: "flex-start", pl: 4, color: "error.main" }}>
      <CircleAlert size={13} aria-hidden style={{ marginTop: 2, flexShrink: 0 }} />
      <Typography variant="caption">{text}</Typography>
    </Box>
  );
}

function Working() {
  return (
    <Box role="status" sx={{ display: "flex", gap: 1.25, alignItems: "center" }}>
      <AgentMark />
      <CircularProgress size={12} aria-hidden />
      <Typography variant="caption" color="text.secondary">
        Working…
      </Typography>
    </Box>
  );
}

function Centered({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        flex: 1,
        display: "flex",
        flexDirection: "column",
        gap: 1,
        alignItems: "center",
        justifyContent: "center",
        px: 3,
        textAlign: "center",
      }}
    >
      {children}
    </Box>
  );
}

/**
 * Follow the conversation as it grows, as a chat does: while the reader is at
 * the bottom, new rows (streamed text, a card, the walk once the documents
 * are read) keep it there; once they scroll up to read, it stays put.
 */
function useStickToBottom() {
  const [node, setNode] = useState<HTMLElement | null>(null);
  useEffect(() => {
    if (!node) return;
    let atBottom = true;
    const onScroll = () => {
      atBottom = node.scrollHeight - node.scrollTop - node.clientHeight < 24;
    };
    const follow = () => {
      if (atBottom) node.scrollTop = node.scrollHeight;
    };
    follow();
    node.addEventListener("scroll", onScroll);
    const resized = new ResizeObserver(follow);
    const added = new MutationObserver(() => {
      follow();
      for (const child of node.children) resized.observe(child);
    });
    for (const child of node.children) resized.observe(child);
    added.observe(node, { childList: true, subtree: true, characterData: true });
    return () => {
      node.removeEventListener("scroll", onScroll);
      resized.disconnect();
      added.disconnect();
    };
  }, [node]);
  return setNode;
}

export function Thread({ projectName }: { projectName: string }) {
  const chat = useProjectChat(projectName);
  const features = useSpecModel(projectName).data?.features;
  const { items, turn } = chat;
  const running = turn.phase !== "idle";

  const openQuestion = openQuestionId(items);
  const featurePaths = useMemo(() => new Set((features ?? []).map((f) => f.path)), [features]);
  const followUp = running ? null : interviewWriteUp(items, featurePaths);
  const prototypeNotes = usePrototypeNotes(projectName, items, running);
  // Nothing from the agent yet since the last message: say it is working.
  const lastItem = items.at(-1);
  const waitingForOutput = running && (!lastItem || lastItem.kind === "user");

  const log = useStickToBottom();

  if (chat.status === "loading" && items.length === 0) {
    return (
      <Centered>
        <CircularProgress size={20} aria-label="Loading the conversation" />
      </Centered>
    );
  }
  if (chat.status === "error") {
    return (
      <Centered>
        <Box role="alert" sx={{ display: "contents" }}>
          <Typography variant="body2" color="text.secondary">
            {chat.error}
          </Typography>
          <Button size="small" variant="outlined" onClick={() => chatStore.retry(projectName)}>
            Try again
          </Button>
        </Box>
      </Centered>
    );
  }
  if (items.length === 0 && !running) {
    return (
      <Centered>
        <Typography variant="body2" color="text.secondary">
          No messages yet. The conversation about this project shows here.
        </Typography>
      </Centered>
    );
  }
  return (
    <Box
      ref={log}
      role="log"
      aria-label="Conversation"
      sx={{
        flex: 1,
        minHeight: 0,
        overflowY: "auto",
        // The containing block of the rows' visually hidden speaker names
        // (absolutely positioned): without it they escape this scroll box,
        // sit at their place in the whole log and make the page taller than
        // the viewport, so the document itself scrolls.
        position: "relative",
        px: 1.75,
        pt: 1.75,
        pb: 1,
        display: "flex",
        flexDirection: "column",
        gap: 1.5,
      }}
    >
      {items.map((item) => (
        <Fragment key={item.id}>
          {item.kind === "user" && <UserRow projectName={projectName} item={item} />}
          {(item.kind === "agent" || item.kind === "note") && <AgentRow text={item.text} />}
          {item.kind === "note" && item.actions && <NoteActions projectName={projectName} actions={item.actions} />}
          {item.kind === "activity" && <ActivityLine item={item} features={features ?? []} />}
          {item.kind === "error" && <ErrorRow text={item.text} />}
          {item.kind === "question" && (
            <Box sx={{ pl: 4 }}>
              <QuestionsPointer projectName={projectName} item={item} open={item.id === openQuestion} />
            </Box>
          )}
          {followUp?.afterId === item.id && <InterviewFollowUp projectName={projectName} path={followUp.path} />}
          {prototypeNotes.has(item.id) && (
            <>
              <AgentRow text={prototypeNotes.get(item.id)!.text} />
              <NoteActions projectName={projectName} actions={prototypeNotes.get(item.id)!.actions} />
            </>
          )}
        </Fragment>
      ))}
      {waitingForOutput && <Working />}
    </Box>
  );
}
