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

import { useEffect, useRef, useState } from "react";
import {
  alpha,
  Avatar,
  Box,
  Chip,
  Collapse,
  Divider,
  IconButton,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from "@wso2/oxygen-ui";
import {
  Check,
  ChevronDown,
  ChevronRight,
  FileStack,
  Send,
  Sparkles,
  Wrench,
  X as XIcon,
} from "@wso2/oxygen-ui-icons-react";
import { useAgentChat } from "../useAgentChat";
import { answerQuestion, chatKeyFor, type ChatMessage } from "../chatStore";
import { groupChatItems, type ChatItem, type ToolMessage } from "../toolGrouping";
import { isAnswerable, serializeAnswer } from "../questionCards";
import { QuestionCard, type QuestionMessage } from "./QuestionCard";
import {
  buildDesignGenerationInstruction,
  buildSpecGenerationInstruction,
} from "@aep/contracts/prompts";
import { readCreatePrompt } from "../../projects/lib/promptStore";

export const AGENT_CHAT_PANEL_WIDTH = 380;

// The generation instruction for a one-shot signal: design is derived from the
// current requirements; requirements are seeded from the stored create prompt.
function instructionFor(
  signal: "requirements" | "design",
  org: string,
  projectName: string,
): string {
  return signal === "design"
    ? buildDesignGenerationInstruction()
    : buildSpecGenerationInstruction(readCreatePrompt(org, projectName));
}

// The project AI panel (#130): legacy console's ChatPanel experience on the
// new stack — narration + tool cards; the agent's FILE edits arrive through
// the live spec room (collab turns), not through this panel.

function opLabel(op: string, status: "streaming" | "done"): string {
  const active = status === "streaming";
  switch (op) {
    case "add":
      return active ? "Creating" : "Created";
    case "remove":
      return active ? "Deleting" : "Deleted";
    default:
      return active ? "Modifying" : "Modified";
  }
}

function leafName(path: string): string {
  return path.split("/").at(-1) ?? path;
}

// A small spinning ring shown while a tool's input is still streaming in.
function Spinner() {
  return (
    <Box
      sx={{
        width: 12,
        height: 12,
        borderRadius: "50%",
        border: "2px solid",
        borderColor: "divider",
        borderTopColor: "primary.main",
        animation: "agentChatSpin 0.7s linear infinite",
        "@keyframes agentChatSpin": { to: { transform: "rotate(360deg)" } },
      }}
    />
  );
}

function MessageRow({ msg }: { msg: ChatMessage }) {
  if (msg.role === "user") {
    return (
      <Box sx={{ display: "flex", justifyContent: "flex-end" }}>
        <Box
          sx={{
            maxWidth: "85%",
            px: 1.5,
            py: 1,
            borderRadius: 2,
            // Soft primary tint (chip-like), not the full brand color — a
            // solid primary bubble reads far too loud in a side panel.
            bgcolor: (theme) => alpha(theme.palette.primary.main, 0.12),
            color: "text.primary",
            whiteSpace: "pre-wrap",
            fontSize: "0.875rem",
            opacity: msg.status === "failed" ? 0.6 : 1,
          }}
        >
          {msg.content}
        </Box>
      </Box>
    );
  }
  if (msg.role === "assistant") {
    return (
      <Stack direction="row" spacing={1} sx={{ alignItems: "flex-start" }}>
        <Avatar sx={{ width: 24, height: 24, bgcolor: "primary.main" }}>
          <Sparkles size={14} />
        </Avatar>
        <Box
          sx={{
            maxWidth: "85%",
            px: 1.5,
            py: 1,
            borderRadius: 2,
            bgcolor: "action.hover",
            whiteSpace: "pre-wrap",
            fontSize: "0.875rem",
          }}
        >
          {msg.content}
        </Box>
      </Stack>
    );
  }
  if (msg.role === "error") {
    return (
      <Box
        data-testid="chat-error"
        sx={{
          px: 1.5,
          py: 1,
          borderRadius: 2,
          border: 1,
          borderColor: "error.main",
          color: "error.main",
          fontSize: "0.8125rem",
          whiteSpace: "pre-wrap",
        }}
      >
        {msg.content}
      </Box>
    );
  }
  return null; // tool messages render through <ToolGroup>, never here
}

// One tool-call row. `showFile` is dropped inside a group, where the shared
// filename already lives in the group header.
function ToolCardRow({
  msg,
  showFile = true,
}: {
  msg: ToolMessage;
  showFile?: boolean;
}) {
  return (
    <Stack
      data-testid="tool-card"
      direction="row"
      spacing={1}
      sx={{
        alignItems: "center",
        px: 1.5,
        py: 0.75,
        borderRadius: 1.5,
        border: 1,
        borderColor: !msg.ok && msg.status === "done" ? "error.main" : "divider",
        bgcolor: "background.paper",
      }}
    >
      {msg.status === "streaming" ? (
        <Spinner />
      ) : msg.ok ? (
        <Check size={14} color="var(--oxygen-palette-success-main, green)" />
      ) : (
        <XIcon size={14} color="var(--oxygen-palette-error-main, red)" />
      )}
      <Wrench size={14} />
      <Typography variant="caption" color="text.secondary">
        {opLabel(msg.op, msg.status)}
      </Typography>
      {showFile && (
        <Tooltip title={msg.path}>
          <Chip size="small" label={leafName(msg.path)} sx={{ maxWidth: 160 }} />
        </Tooltip>
      )}
      {!msg.ok && msg.errorText && (
        <Typography variant="caption" color="error" noWrap>
          {msg.errorText}
        </Typography>
      )}
    </Stack>
  );
}

// A run of same-file tool calls. A single call renders as one plain row; two or
// more collapse into a disclosure card (collapsed by default) — the header
// carries the filename + change count + an aggregate ok/error state, and the
// individual ops reveal on expand.
function ToolGroup({
  group,
  expanded,
  onToggle,
}: {
  group: Extract<ChatItem, { kind: "tool-group" }>;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { tools, path } = group;
  // A group always holds ≥1 tool; a lone call renders as a plain row.
  if (tools.length <= 1) {
    const only = tools[0];
    return only ? (
      <Box sx={{ ml: 4 }}>
        <ToolCardRow msg={only} />
      </Box>
    ) : null;
  }
  // The header mirrors the LAST op's state, not an aggregate: a burst of edits
  // to one file is judged by where the file ended up, so an intermediate
  // failure that a later op corrects (delete → recreate) doesn't mark the whole
  // group failed. `tools` always holds ≥2 here (a lone call returned early).
  const last = tools[tools.length - 1];
  const streaming = last?.status === "streaming";
  const failed = last?.status === "done" && !last.ok;
  return (
    <Box sx={{ ml: 4 }}>
      <Stack
        data-testid="tool-group"
        direction="row"
        spacing={1}
        role="button"
        aria-expanded={expanded}
        onClick={onToggle}
        sx={{
          alignItems: "center",
          px: 1.5,
          py: 0.75,
          borderRadius: 1.5,
          border: 1,
          borderColor: failed ? "error.main" : "divider",
          bgcolor: "background.paper",
          cursor: "pointer",
          "&:hover": { bgcolor: "action.hover" },
        }}
      >
        {streaming ? (
          <Spinner />
        ) : failed ? (
          <XIcon size={14} color="var(--oxygen-palette-error-main, red)" />
        ) : (
          <Check size={14} color="var(--oxygen-palette-success-main, green)" />
        )}
        <FileStack size={14} />
        <Tooltip title={path}>
          <Chip size="small" label={leafName(path)} sx={{ maxWidth: 160 }} />
        </Tooltip>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ flexGrow: 1 }}
        >
          {tools.length} changes
        </Typography>
        {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
      </Stack>
      <Collapse in={expanded} unmountOnExit>
        <Stack spacing={0.75} sx={{ mt: 0.75, pl: 1.5 }}>
          {tools.map((t) => (
            <ToolCardRow key={t.id} msg={t} showFile={false} />
          ))}
        </Stack>
      </Collapse>
    </Box>
  );
}

function ThinkingDots() {
  return (
    <Stack
      data-testid="thinking"
      direction="row"
      spacing={1}
      sx={{ alignItems: "center", ml: 4 }}
    >
      {[0, 1, 2].map((i) => (
        <Box
          key={i}
          sx={{
            width: 6,
            height: 6,
            borderRadius: "50%",
            bgcolor: "text.secondary",
            animation: "agentChatPulse 1.2s ease-in-out infinite",
            animationDelay: `${i * 0.2}s`,
            "@keyframes agentChatPulse": {
              "0%, 100%": { opacity: 0.25 },
              "50%": { opacity: 1 },
            },
          }}
        />
      ))}
    </Stack>
  );
}

export function AgentChatPanel({
  org,
  projectName,
  displayName,
  onClose,
  autoGenerate,
  onAutoGenerated,
}: {
  org: string;
  projectName: string;
  displayName?: string | undefined;
  onClose: () => void;
  /** Generation CTA (#150 spec / #159 design): auto-send the matching turn
   *  once — requirements seeded from the stored create prompt, design derived
   *  from the current requirements. */
  autoGenerate?: "requirements" | "design";
  /** Called after the auto-send fires, so the caller can clear the signal. */
  onAutoGenerated?: () => void;
}) {
  const { messages, isSending, send } = useAgentChat(org, projectName);
  const [draft, setDraft] = useState("");
  // Same-file tool runs render collapsed by default; a click flips membership.
  // Keyed by group id (its first tool's message id), stable as the run grows.
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(
    () => new Set(),
  );
  const toggleGroup = (id: string) =>
    setExpandedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const scrollRef = useRef<HTMLDivElement>(null);

  // Follow the tail while streaming / on new messages.
  useEffect(() => {
    const el = scrollRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [messages, isSending]);

  // One-shot generate (#150 spec / #159 design): fire exactly once per signal,
  // then re-arm when it clears so a LATER Generate click (e.g. Generate spec →
  // leave the chat open → Generate design) fires again. `send` is held in a ref
  // and kept out of the effect deps: its identity flips whenever `isSending`
  // changes, which would otherwise re-run this effect mid-turn — the reason the
  // fired-guard existed. Keying the effect purely on `autoGenerate` (a value
  // that goes undefined→signal→undefined as the caller sets/clears the param)
  // means one fire per click, and the guard reset on clear is StrictMode-safe.
  const sendRef = useRef(send);
  sendRef.current = send;
  const autoGenFiredRef = useRef(false);
  useEffect(() => {
    if (!autoGenerate) {
      autoGenFiredRef.current = false; // signal cleared — re-arm for next click
      return;
    }
    if (autoGenFiredRef.current) return;
    autoGenFiredRef.current = true;
    sendRef.current(instructionFor(autoGenerate, org, projectName));
    onAutoGenerated?.();
  }, [autoGenerate, org, projectName, onAutoGenerated]);

  const submit = () => {
    if (!draft.trim() || isSending) return;
    send(draft);
    setDraft("");
  };

  // ask_question cards (ADR-0012): a card is live only while unanswered and
  // not superseded by a later user message; the composer stays an equally
  // valid answer path, so `awaiting` only softens the placeholder.
  const chatKey = chatKeyFor(org, projectName);
  const answerable = (msg: QuestionMessage) => isAnswerable(messages, msg);
  const awaiting = !isSending && messages.some((m) => m.role === "question" && answerable(m));
  const answerCard = (msg: QuestionMessage) => (selected: string[], freeText?: string) => {
    answerQuestion(chatKey, msg.toolCallId, {
      selected,
      ...(freeText?.trim() ? { freeText: freeText.trim() } : {}),
    });
    send(serializeAnswer(msg.question, selected, freeText));
  };

  return (
    <Box
      sx={{
        width: AGENT_CHAT_PANEL_WIDTH,
        flexShrink: 0,
        height: "100%",
        display: "flex",
        flexDirection: "column",
        borderLeft: 1,
        borderColor: "divider",
        bgcolor: "background.paper",
      }}
    >
      {/* Header */}
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: "center", px: 2, py: 1.5 }}
      >
        <Sparkles size={18} />
        <Typography variant="body2" sx={{ fontWeight: 600, flexGrow: 1 }}>
          Agent Chat
        </Typography>
        <IconButton size="small" aria-label="Close agent chat" onClick={onClose}>
          <XIcon size={16} />
        </IconButton>
      </Stack>
      <Divider />

      {/* Messages */}
      <Box ref={scrollRef} sx={{ flexGrow: 1, overflow: "auto", p: 2 }}>
        {messages.length === 0 ? (
          <Stack
            spacing={1.5}
            sx={{ alignItems: "center", textAlign: "center", mt: 6, px: 2 }}
          >
            <Avatar sx={{ width: 48, height: 48, bgcolor: "primary.main" }}>
              <Sparkles size={24} />
            </Avatar>
            <Typography variant="subtitle2">Hi! I&apos;m your Agent.</Typography>
            <Typography variant="body2" color="text.secondary">
              Ask me to edit this project&apos;s spec — I join the shared
              workspace and you can watch the files change live.
            </Typography>
          </Stack>
        ) : (
          <Stack spacing={1.5}>
            {groupChatItems(messages).map((item) =>
              item.kind === "message" ? (
                item.message.role === "question" ? (
                  <QuestionCard
                    key={item.message.id}
                    msg={item.message}
                    answerable={answerable(item.message)}
                    busy={isSending}
                    onAnswer={answerCard(item.message)}
                  />
                ) : (
                  <MessageRow key={item.message.id} msg={item.message} />
                )
              ) : (
                <ToolGroup
                  key={item.id}
                  group={item}
                  expanded={expandedGroups.has(item.id)}
                  onToggle={() => toggleGroup(item.id)}
                />
              ),
            )}
            {isSending && <ThinkingDots />}
          </Stack>
        )}
      </Box>

      {/* Context + input */}
      <Divider />
      <Box sx={{ p: 1.5 }}>
        <Stack direction="row" spacing={1} sx={{ mb: 1 }}>
          <Chip size="small" variant="outlined" label={`project: ${displayName ?? projectName}`} />
        </Stack>
        <Stack direction="row" spacing={1} sx={{ alignItems: "flex-end" }}>
          <TextField
            fullWidth
            multiline
            maxRows={5}
            size="small"
            placeholder={
              awaiting
                ? "Answer the question above, or type a reply…"
                : "Ask the agent to edit the spec…"
            }
            value={draft}
            disabled={isSending}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <IconButton
            color="primary"
            aria-label="Send message"
            disabled={isSending || !draft.trim()}
            onClick={submit}
          >
            <Send size={18} />
          </IconButton>
        </Stack>
      </Box>
    </Box>
  );
}
