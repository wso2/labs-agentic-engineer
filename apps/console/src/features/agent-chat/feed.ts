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

// Derived view model for the agent activity stream (#130 follow-up: task 3).
// Folds the flat, append-only chat log into author-attributed blocks: a user
// message, then the agent turn it triggered (narration + activity steps +
// lifecycle). Pure and independently testable — no rendering, no store access.

import type { ChatMessage } from "./chatStore";
import { groupChatItems, type ChatItem } from "./toolGrouping";

export type UserMessage = Extract<ChatMessage, { role: "user" }>;

/** Who a block belongs to, resolved against the signed-in user. */
export interface Attribution {
  /** "You" for the current user (or an author-less legacy message); a
   *  teammate's real display name otherwise. */
  displayName: string;
  isOwn: boolean;
}

/** A participant in the log (for the header avatar cluster). */
export interface Participant extends Attribution {
  id: string;
}

export type TurnLifecycle = "running" | "committed" | "failed";

export type FeedBlock =
  | { kind: "user"; id: string; message: UserMessage; attribution: Attribution }
  | {
      kind: "turn";
      id: string;
      /** The live turn id, when known (absent on rehydrated history turns). */
      turnId: string | undefined;
      attribution: Attribution;
      items: ChatItem[];
      status: TurnLifecycle;
      /** The turn was initiated by `/start` — the first-run interview. Drives
       *  the console-side transition line before its question banner. */
      startTurn: boolean;
    };

export interface BuildFeedOptions {
  /** Stable id of the signed-in user (email — see currentUser.ts). */
  currentUserId: string;
  /** The turn currently streaming, if any — the authoritative "running" signal
   *  for a teammate's re-attached turn, whose triggering message is rehydrated
   *  (status "completed") and so can't be told apart by status alone. */
  activeTurnId?: string | undefined;
}

function attributionFor(
  initiator: UserMessage | undefined,
  currentUserId: string,
): Attribution {
  const author = initiator?.author;
  // No author == a log written before attribution existed: treat as the
  // signed-in user for display, same fallback the header uses.
  if (!author) return { displayName: "You", isOwn: true };
  const isOwn = author.id === currentUserId;
  return { displayName: isOwn ? "You" : author.displayName, isOwn };
}

function turnStatusFor(
  turnId: string | undefined,
  initiator: UserMessage | undefined,
  activeTurnId: string | undefined,
): TurnLifecycle {
  if (turnId && turnId === activeTurnId) return "running";
  if (initiator?.status === "in_flight") return "running";
  if (initiator?.status === "failed") return "failed";
  return "committed";
}

/**
 * Fold the flat message log into author-attributed feed blocks. A user message
 * opens a block and closes any open agent turn; the agent messages that follow
 * (narration, tool cards, errors) collect into that turn until the next user
 * message. Tool runs are merged with the shared `groupChatItems`.
 *
 * Attribution links a turn to its initiator by `turnId` when present (live
 * messages), falling back to the nearest preceding user message for rehydrated
 * history (whose user messages carry no `turnId`).
 */
export function buildFeed(
  messages: ChatMessage[],
  { currentUserId, activeTurnId }: BuildFeedOptions,
): FeedBlock[] {
  const userByTurnId = new Map<string, UserMessage>();
  for (const m of messages) {
    if (m.role === "user" && m.turnId) userByTurnId.set(m.turnId, m);
  }

  const blocks: FeedBlock[] = [];
  let lastUser: UserMessage | undefined;
  let open:
    | { block: Extract<FeedBlock, { kind: "turn" }>; raw: ChatMessage[]; initiator: UserMessage | undefined }
    | null = null;

  const closeTurn = () => {
    if (!open) return;
    open.block.items = groupChatItems(open.raw);
    open.block.status = turnStatusFor(open.block.turnId, open.initiator, activeTurnId);
    open = null;
  };

  for (const message of messages) {
    if (message.role === "user") {
      closeTurn();
      lastUser = message;
      blocks.push({
        kind: "user",
        id: message.id,
        message,
        attribution: attributionFor(message, currentUserId),
      });
      continue;
    }
    if (!open) {
      const turnId = "turnId" in message ? message.turnId : undefined;
      const initiator = (turnId && userByTurnId.get(turnId)) || lastUser;
      const block: Extract<FeedBlock, { kind: "turn" }> = {
        kind: "turn",
        id: `turn-${message.id}`,
        turnId,
        attribution: attributionFor(initiator, currentUserId),
        items: [],
        status: "committed",
        // Word-boundary, not startsWith: "/start a rota planner" is a start
        // turn, "/startle" would not be. Rehydrate keeps the verbatim command
        // (#463), so the flag survives a fresh browser.
        startTurn: /^\/start\b/.test(initiator?.content.trim() ?? ""),
      };
      blocks.push(block);
      open = { block, raw: [], initiator };
    }
    open.raw.push(message);
  }
  closeTurn();

  return blocks;
}

/**
 * Distinct authors present in the log, current user first (as "You"). Powers
 * the header avatar cluster — static, no live presence.
 */
export function participantsOf(
  messages: ChatMessage[],
  currentUserId: string,
): Participant[] {
  const seen = new Map<string, Participant>();
  for (const m of messages) {
    if (m.role !== "user") continue;
    const author = m.author;
    const id = author?.id ?? currentUserId;
    if (seen.has(id)) continue;
    const isOwn = id === currentUserId;
    seen.set(id, {
      id,
      isOwn,
      displayName: isOwn ? "You" : (author?.displayName ?? "You"),
    });
  }
  return [...seen.values()].sort((a, b) => Number(b.isOwn) - Number(a.isOwn));
}
