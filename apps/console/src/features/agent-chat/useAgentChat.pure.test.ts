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

// The two pure decisions behind #562's chat fixes: how fast to look for a turn
// nobody in this browser started, and what to paint once one is found.

import { describe, expect, it } from "vitest";
import { foreignTurnPollDelay, userMessageForTurn } from "./useAgentChat";
import type { ChatMessage } from "./chatStore";
import type { TurnStatus } from "./api/turns";

function turn(over: Partial<TurnStatus> = {}): TurnStatus {
  return {
    turnId: "t1",
    conversationId: "c1",
    useCase: "general",
    status: "running",
    createdAt: "2026-08-22T08:00:00.000Z",
    updatedAt: "2026-08-22T08:00:00.000Z",
    ...over,
  };
}

const SOME_MESSAGE: ChatMessage = {
  id: "m1",
  role: "assistant",
  turnId: "t1",
  content: "working…",
};

describe("foreignTurnPollDelay", () => {
  // A freshly created project lands here: the platform fires `/start` after
  // `POST /projects` returns, so the panel opens before the turn exists and
  // the one mount check finds nothing. At the idle cadence the user watches an
  // empty pane for most of a minute at the exact moment the product is trying
  // to show it is working.
  it("looks again quickly while there is nothing on screen", () => {
    expect(foreignTurnPollDelay([], 0)).toBe(2_000);
  });

  // Once anything is rendering, the poll is a background refresh again and the
  // slow cadence is the right cost.
  it("settles once the log has something in it", () => {
    expect(foreignTurnPollDelay([SOME_MESSAGE], 0)).toBe(12_000);
  });

  // The fast cadence is for an ARRIVAL — the seconds between a panel opening
  // and the turn it waits for appearing. "Empty" is also the resting state of a
  // project nobody has talked to, and unbounded that would poll every 2s for as
  // long as the panel stayed open.
  it("settles even on an empty log once the arrival window has passed", () => {
    expect(foreignTurnPollDelay([], 7)).toBe(2_000);
    expect(foreignTurnPollDelay([], 8)).toBe(12_000);
  });
});

describe("userMessageForTurn", () => {
  it("paints the line the turn was started with", () => {
    expect(userMessageForTurn(turn({ instruction: "/start an expense tracker" }))).toMatchObject({
      role: "user",
      content: "/start an expense tracker",
      turnId: "t1",
      status: "in_flight",
    });
  });

  // Attribution is the half that was silently wrong before: with no author the
  // feed's fallback calls every unattributed row "You", so a teammate's turn —
  // and the platform's kickoff — read as the reader's own.
  it("carries the author so a teammate's turn reads as theirs", () => {
    const msg = userMessageForTurn(
      turn({
        instruction: "/design",
        authorId: "them@x.com",
        authorDisplayName: "Them",
      }),
    );
    expect(msg?.author).toEqual({ id: "them@x.com", displayName: "Them" });
  });

  // An M2M token journals no author rather than a bare subject.
  it("omits the author when the turn has no attributable human", () => {
    expect(userMessageForTurn(turn({ instruction: "/start" }))?.author).toBeUndefined();
  });

  // A row written before the display record existed. Painting an empty bubble
  // would be worse than painting nothing at all.
  it("paints nothing for a turn that carries no line", () => {
    expect(userMessageForTurn(turn())).toBeNull();
  });

  it("falls back to the id when only the display name is missing", () => {
    const msg = userMessageForTurn(turn({ instruction: "/design", authorId: "them@x.com" }));
    expect(msg?.author).toEqual({ id: "them@x.com", displayName: "them@x.com" });
  });
});
