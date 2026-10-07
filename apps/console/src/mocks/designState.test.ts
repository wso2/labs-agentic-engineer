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

// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { turnBody } from "../features/agent-chat/turnScope";

// A reload keeps the conversation (chatServer.ts, in sessionStorage) and loads
// every mock module afresh. The design must still say what the chat says the
// agent did: were it to start over, the same features would be offered for
// design again, and Design would run a second time into the same thread.

const PROJECT = "acme-expenses";

/** The mock server as a page load finds it: fresh modules over the stored conversation. */
async function loadPage() {
  vi.resetModules();
  const conversation = await import("./handlers/conversation");
  const spec = await import("./specState");
  return { startMockTurn: conversation.startMockTurn, specView: spec.specView };
}

/** Everything a turn says in the chat, as one text. */
function said(turn: { frames: { part: { type: string; delta?: string } }[] }): string {
  return turn.frames.map((f) => (f.part.type === "text-delta" ? (f.part.delta ?? "") : "")).join("");
}

describe("the design review across a reload", () => {
  beforeEach(() => {
    sessionStorage.clear();
    vi.useFakeTimers({ now: new Date("2026-09-30T10:00:00Z") });
  });
  afterEach(() => vi.useRealTimers());

  it("keeps what a finished design turn did, so Design is not offered, or run, again", async () => {
    const first = await loadPage();
    const design = first.startMockTurn(PROJECT, turnBody("/design F1 F2", { kind: "design" }));
    expect(said(design)).toContain("Designed Approvals");
    vi.setSystemTime(Date.now() + 60_000);

    const reloaded = await loadPage();
    const stages = reloaded.specView(PROJECT).features.map((f) => [f.id, f.stage]);
    expect(stages).toEqual(expect.arrayContaining([["F1", "Designed"], ["F2", "Designed"]]));
    const again = reloaded.startMockTurn(PROJECT, turnBody("/design F1 F2", { kind: "design" }));
    expect(said(again)).toBe("The design is up to date with the spec. Nothing to design.");
  });
});
