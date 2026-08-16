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

import { useMemo, useState } from "react";
import { Avatar, Box, Link, Stack, Typography, alpha } from "@wso2/oxygen-ui";
import { isStartInstruction, type FeedBlock } from "../feed";
import { summarizeUserMessage } from "../userMessageSummary";
import { MarkdownView } from "../../../components/MarkdownView";
import { START_READING_LINE, TurnBlock } from "./TurnBlock";
import { WorkingIndicator } from "./WorkingIndicator";

// The agent activity stream (task 3): a linear, author-attributed feed of user
// messages and the agent turns they trigger. No bubbles, no left/right
// alignment — every block is a full-width row.

function initialOf(name: string): string {
  return name.trim().charAt(0).toUpperCase() || "?";
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
  // Interview answers and the finish valve travel as plain-text instructions;
  // the thread reads them as one line, with the instruction itself one click
  // away. Nothing here changes what was sent.
  const machinery = useMemo(() => summarizeUserMessage(message.content), [message.content]);
  const [detailOpen, setDetailOpen] = useState(false);
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
        {machinery ? (
          <Stack spacing={0.5} sx={{ alignItems: "flex-start" }}>
            <Typography
              data-testid="user-message-summary"
              sx={{
                fontSize: "0.875rem",
                color: "text.primary",
                opacity: failed ? 0.6 : 1,
              }}
            >
              {machinery.summary}
            </Typography>
            <Link
              component="button"
              type="button"
              variant="caption"
              aria-expanded={detailOpen}
              onClick={() => setDetailOpen((v) => !v)}
              sx={{ fontWeight: 600 }}
            >
              {detailOpen ? "Hide details" : "Show details"}
            </Link>
            {detailOpen && (
              <Typography
                data-testid="user-message-detail"
                variant="caption"
                color="text.secondary"
                sx={{ whiteSpace: "pre-wrap", textAlign: "left" }}
              >
                {machinery.detail}
              </Typography>
            )}
          </Stack>
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
      </Box>
    </Box>
  );
}

export function MessageList({
  feed,
  expandedGroups,
  onToggleGroup,
  onOpenSpec,
  showWorkingTail,
}: {
  feed: FeedBlock[];
  expandedGroups: Set<string>;
  onToggleGroup: (id: string) => void;
  onOpenSpec: () => void;
  /** Show a tail "Working…" indicator when a turn is in flight but hasn't
   *  produced any content (and so has no running turn block of its own yet). */
  showWorkingTail: boolean;
}) {
  // A turn block only exists once the turn has produced something, so the
  // first seconds of the first run are just the user's `/start` and this tail.
  // Speak the same opening line here (#485 live-testing round 3) — when the
  // turn block does appear it leads with the identical line, so the thread
  // reads continuously rather than replacing one beat with another.
  const tail = feed[feed.length - 1];
  const startTail =
    tail?.kind === "user" && isStartInstruction(tail.message.content);

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
          />
        ),
      )}
      {showWorkingTail &&
        (startTail ? (
          <Stack spacing={1} data-testid="start-turn-tail">
            <MarkdownView>{START_READING_LINE}</MarkdownView>
            <WorkingIndicator />
          </Stack>
        ) : (
          <WorkingIndicator />
        ))}
    </Stack>
  );
}
