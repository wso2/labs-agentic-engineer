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

import {
  addMessage,
  claimStreamFold,
  hasStreamFold,
  setTurnStatus,
  subscribeLocalTurnActivity,
  subscribeTurnEnd,
  type TurnEndStatus,
} from "./chatStore.js";
import { getTurn } from "./api/turns.js";
import { attachAndFoldTurn } from "./runTurn.js";

/**
 * How a turn started away from the chat panel ended, as far as this browser
 * could see: `unknown` when its stream was lost before a terminal and the
 * status read found it still running (or could not be made).
 */
export type FollowedTurnEnd = TurnEndStatus | "unknown";

/**
 * Follow a turn dispatched from outside the chat panel (an aimed send #666, a
 * prototype review batch #817) to its end, folding its stream into the
 * project's chat log so the turn is recorded there like any other.
 *
 * The fold is claimed here unless one is already live for the log — two folds
 * of one turn would interleave its stream on top of itself — and either way
 * the end is read off the log's turn-end bus, which every fold announces on,
 * matched by turn id: a late end of an earlier turn is not this one's.
 *
 * Always settles. A borrowed fold can end without announcing this turn (the
 * panel unmounted mid-stream, or it was folding an earlier turn); when the
 * log's last fold goes, the turn's status is read once instead.
 */
export function followTurnToEnd(chatKey: string, projectName: string, turnId: string): Promise<FollowedTurnEnd> {
  return new Promise((resolve) => {
    let settled = false;
    const cleanups: Array<() => void> = [];
    const settle = (end: FollowedTurnEnd) => {
      if (settled) return;
      settled = true;
      for (const cleanup of cleanups) cleanup();
      resolve(end);
    };
    cleanups.push(
      subscribeTurnEnd(chatKey, (status, endedTurnId) => {
        if (endedTurnId === turnId) settle(status);
      }),
    );
    if (hasStreamFold(chatKey)) {
      cleanups.push(
        subscribeLocalTurnActivity(chatKey, () => {
          if (settled || hasStreamFold(chatKey)) return;
          void readEnd(projectName, turnId).then(settle);
        }),
      );
      return;
    }
    const releaseFold = claimStreamFold(chatKey);
    void attachAndFoldTurn(chatKey, projectName, turnId, new AbortController().signal)
      .catch(() => {
        setTurnStatus(chatKey, turnId, "failed");
        addMessage(chatKey, {
          role: "error",
          content: "Lost the agent's stream — open the panel to re-attach.",
        });
      })
      .finally(() => {
        releaseFold();
        // A fold that ends without announcing an end never saw a terminal.
        settle("unknown");
      });
  });
}

/** The turn's end by one authoritative status read; `unknown` while it still
 *  runs, or when the read fails. */
async function readEnd(projectName: string, turnId: string): Promise<FollowedTurnEnd> {
  try {
    const turn = await getTurn(projectName, turnId);
    return turn?.status === "completed" || turn?.status === "failed" ? turn.status : "unknown";
  } catch {
    return "unknown";
  }
}
