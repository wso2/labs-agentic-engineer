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
  subscribeTurnEnd,
  type TurnEndStatus,
} from "./chatStore.js";
import { attachAndFoldTurn } from "./runTurn.js";

/**
 * How a turn started away from the chat panel ended, as far as this browser
 * could see: `unknown` when its stream was lost before a terminal and the
 * status poll found it still running.
 */
export type FollowedTurnEnd = TurnEndStatus | "unknown";

/**
 * Follow a turn dispatched from outside the chat panel (an aimed send #666, a
 * prototype review batch #817) to its end, folding its stream into the
 * project's chat log so the turn is recorded there like any other.
 *
 * The fold is claimed here unless one is already live for the log — two folds
 * of one turn would interleave its stream on top of itself — and either way
 * the end is read off the log's turn-end bus, which every fold announces on.
 * One turn runs per project at a time, so the next end on this log is this
 * turn's.
 */
export function followTurnToEnd(chatKey: string, projectName: string, turnId: string): Promise<FollowedTurnEnd> {
  return new Promise((resolve) => {
    let settled = false;
    const settle = (end: FollowedTurnEnd) => {
      if (settled) return;
      settled = true;
      unsubscribe();
      resolve(end);
    };
    const unsubscribe = subscribeTurnEnd(chatKey, settle);
    if (hasStreamFold(chatKey)) return;
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
