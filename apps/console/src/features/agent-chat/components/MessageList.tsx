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

import { Avatar, Box, Chip, Stack, Typography, alpha } from "@wso2/oxygen-ui";
import { Paperclip } from "@wso2/oxygen-ui-icons-react";
import { START_COMMAND } from "@aep/contracts/commands";
import type { FeedBlock } from "../feed";
import { startLineOf } from "../startLine";
import { AnchorTag } from "./AnchorTag";
import { TurnBlock } from "./TurnBlock";
import { WorkingIndicator } from "./WorkingIndicator";

// The agent activity stream (task 3): a linear, author-attributed feed of user
// messages and the agent turns they trigger. No bubbles, no left/right
// alignment — every block is a full-width row.

function initialOf(name: string): string {
  return name.trim().charAt(0).toUpperCase() || "?";
}

// How many lines of the kickoff's idea the transcript shows before cropping.
// Two: one is not enough to recognise a paragraph you wrote, and the whole
// point is recognition — the document itself is the PRD, not this.
const IDEA_CLAMP_LINES = 2;

/**
 * The kickoff line (#562/#528): `/start` beside the user's own words.
 *
 * The command is set apart so the idea reads as prose rather than as part of a
 * string the user is being shown they "typed" — they did not; the platform
 * fired it at project creation, and this line's whole job is to show the agent
 * working from what they wrote.
 *
 * Nothing is ADDED to the line, not even a connective. The same shape also
 * renders a `/start <idea>` a user really did type, and the console cannot tell
 * the two apart — the server journals a typed command verbatim — so a joining
 * word here would put a word the user never wrote inside a message attributed
 * to them. Setting the command apart already does the work a join would.
 *
 * The crop is CSS. A clamped element keeps its full text for selection, copy
 * and screen readers, and re-measures when the user drags the panel wider —
 * none of which a truncated string does.
 */
function StartLine({ idea }: { idea: string }) {
  return (
    <Typography
      component="span"
      sx={{ fontSize: "0.875rem", color: "text.primary", display: "block" }}
    >
      <Box
        component="span"
        sx={{ fontFamily: "monospace", fontWeight: 600, mr: 0.75 }}
      >
        {START_COMMAND}
      </Box>
      {idea && (
        <Box
          component="span"
          title={idea}
          sx={{
            display: "-webkit-box",
            WebkitBoxOrient: "vertical",
            WebkitLineClamp: IDEA_CLAMP_LINES,
            overflow: "hidden",
            color: "text.secondary",
          }}
        >
          {idea}
        </Box>
      )}
    </Typography>
  );
}

function timeOf(createdAt: number | undefined): string | null {
  if (!createdAt) return null;
  return new Date(createdAt).toLocaleTimeString([], {
    hour: "2-digit",
    minute: "2-digit",
  });
}

function UserBlock({ block }: { block: Extract<FeedBlock, { kind: "user" }> }) {
  const { message, attribution } = block;
  const isOwn = attribution.isOwn;
  const time = timeOf(message.createdAt);
  const failed = message.status === "failed";
  const startLine = startLineOf(message.content);
  return (
    // Your own messages align to the right, teammates to the left — the
    // familiar chat convention that makes "who said this" scannable at a
    // glance. The author row mirrors so the avatar sits on the outer edge.
    <Box
      data-testid="user-message"
      sx={{
        display: "flex",
        flexDirection: "column",
        alignItems: isOwn ? "flex-end" : "flex-start",
      }}
    >
      <Stack
        direction={isOwn ? "row-reverse" : "row"}
        spacing={1}
        sx={{ alignItems: "center", mb: 0.5 }}
      >
        <Avatar
          sx={{
            width: 22,
            height: 22,
            fontSize: "0.7rem",
            bgcolor: isOwn ? "primary.main" : "info.main",
          }}
        >
          {initialOf(attribution.displayName)}
        </Avatar>
        <Typography variant="caption" sx={{ fontWeight: 600 }}>
          {attribution.displayName}
        </Typography>
        {time && (
          <Typography variant="caption" color="text.secondary">
            {time}
          </Typography>
        )}
      </Stack>
      {/* Human messages get a subtle bubble so a person's words read as
          distinct turns in the thread (own vs teammate tinted differently);
          agent turns stay bubble-less as a flat activity stream. */}
      <Box
        sx={{
          maxWidth: "85%",
          px: 1.5,
          py: 1,
          borderRadius: 2,
          textAlign: "left",
          bgcolor: (theme) =>
            isOwn
              ? alpha(theme.palette.primary.main, 0.08)
              : theme.palette.action.hover,
        }}
      >
        {/* What this message was aimed at (#666) — ABOVE the words, because the
            anchor is the subject of the sentence rather than an addendum to it.
            Frozen: never re-checked against the current document (ADR-0024). */}
        {message.anchor && <AnchorTag anchor={message.anchor} />}
        {startLine ? (
          <Box sx={{ opacity: failed ? 0.6 : 1 }}>
            <StartLine idea={startLine.idea} />
          </Box>
        ) : (
          <Typography
            sx={{
              whiteSpace: "pre-wrap",
              fontSize: "0.875rem",
              color: "text.primary",
              opacity: failed ? 0.6 : 1,
            }}
          >
            {message.content}
          </Typography>
        )}
        {/* What went up with this message (#428). Names only — the bytes are
            conversation-scoped model content the platform never stores
            (ADR-0019), so a chip is a record, not a download link. Wraps rather
            than scrolls: a sent message is history and may be any height, unlike
            the composer, which must not grow. */}
        {message.attachments && message.attachments.length > 0 && (
          <Stack
            direction="row"
            spacing={0.5}
            useFlexGap
            sx={{ flexWrap: "wrap", mt: 1 }}
            data-testid="user-message-attachments"
          >
            {message.attachments.map((name) => (
              <Chip
                key={name}
                size="small"
                variant="outlined"
                icon={<Paperclip size={12} />}
                label={name}
                title={name}
                sx={{
                  maxWidth: "100%",
                  opacity: failed ? 0.6 : 1,
                  "& .MuiChip-icon": { ml: 0.75, mr: -0.25, flexShrink: 0 },
                  "& .MuiChip-label": { overflow: "hidden", textOverflow: "ellipsis" },
                }}
              />
            ))}
          </Stack>
        )}
      </Box>
    </Box>
  );
}

export function MessageList({
  feed,
  expandedGroups,
  onToggleGroup,
  onOpenSpec,
  showSpecLink = true,
  showWorkingTail,
}: {
  feed: FeedBlock[];
  expandedGroups: Set<string>;
  onToggleGroup: (id: string) => void;
  onOpenSpec: () => void;
  showSpecLink?: boolean;
  /** Show a tail "Working…" indicator when a turn is in flight but hasn't
   *  produced any content (and so has no running turn block of its own yet). */
  showWorkingTail: boolean;
}) {
    // No dividers between blocks: each block's author header ("You" / "✦
    // Agent") plus the spacing already separates turns; hard rules made the
    // feed read like a table rather than a chat.
  return (
    <Stack spacing={3}>
      {feed.map((block) =>
        block.kind === "user" ? (
          <UserBlock key={block.id} block={block} />
        ) : (
          <TurnBlock
            key={block.id}
            turn={block}
            expandedGroups={expandedGroups}
            onToggleGroup={onToggleGroup}
            onOpenSpec={onOpenSpec}
            showSpecLink={showSpecLink}
          />
        ),
      )}
      {showWorkingTail && <WorkingIndicator />}
    </Stack>
  );
}
