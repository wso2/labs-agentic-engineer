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

// Following a turn started away from the chat panel must ALWAYS settle, and
// must settle on THIS turn's end — not on whatever end the log hears next.

import { beforeEach, describe, expect, it, vi } from "vitest";
import { chatKeyFor, claimStreamFold, notifyTurnEnd } from "./chatStore";
import { followTurnToEnd, type FollowedTurnEnd } from "./followTurn";

const KEY = chatKeyFor("acme", "follow-proj");

const mockGetTurn = vi.fn();
vi.mock("./api/turns", async (importOriginal) => {
  const real = await importOriginal<typeof import("./api/turns")>();
  return { ...real, getTurn: (...a: unknown[]) => mockGetTurn(...a) };
});
let endOwnFold: () => void = () => {};
vi.mock("./runTurn", () => ({
  attachAndFoldTurn: () =>
    new Promise<void>((resolve) => {
      endOwnFold = resolve;
    }),
}));

function track(p: Promise<FollowedTurnEnd>) {
  const state: { end?: FollowedTurnEnd } = {};
  void p.then((end) => {
    state.end = end;
  });
  return state;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockGetTurn.mockResolvedValue({ status: "running" });
});

describe("followTurnToEnd", () => {
  it("ignores a late end of an earlier turn and settles on this turn's own end", async () => {
    const release = claimStreamFold(KEY);
    const followed = track(followTurnToEnd(KEY, "follow-proj", "turn-2"));
    notifyTurnEnd(KEY, "failed", "turn-1");
    await Promise.resolve();
    expect(followed.end).toBeUndefined();
    notifyTurnEnd(KEY, "completed", "turn-2");
    await vi.waitFor(() => expect(followed.end).toBe("completed"));
    release();
  });

  it("settles from the turn's status when a live fold ends without announcing this turn's end", async () => {
    mockGetTurn.mockResolvedValue({ status: "completed" });
    const release = claimStreamFold(KEY);
    const followed = track(followTurnToEnd(KEY, "follow-proj", "turn-3"));
    release();
    await vi.waitFor(() => expect(followed.end).toBe("completed"));
    expect(mockGetTurn).toHaveBeenCalledWith("follow-proj", "turn-3");
  });

  it("settles unknown when that fold ends and the turn still reads running", async () => {
    const release = claimStreamFold(KEY);
    const followed = track(followTurnToEnd(KEY, "follow-proj", "turn-4"));
    release();
    await vi.waitFor(() => expect(followed.end).toBe("unknown"));
  });

  it("with no live fold, folds the turn itself and settles on the end it announces", async () => {
    const followed = track(followTurnToEnd(KEY, "follow-proj", "turn-5"));
    notifyTurnEnd(KEY, "failed", "turn-5");
    endOwnFold();
    await vi.waitFor(() => expect(followed.end).toBe("failed"));
  });

  it("with no live fold, settles unknown when its own fold ends without a terminal", async () => {
    const followed = track(followTurnToEnd(KEY, "follow-proj", "turn-6"));
    endOwnFold();
    await vi.waitFor(() => expect(followed.end).toBe("unknown"));
  });
});
