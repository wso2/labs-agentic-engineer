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

import { describe, expect, it } from "vitest";
import type { ChatItem } from "../../agent-chat/chatLog";
import { prototypeNotes } from "./note";

const user = (id: string, text: string, state: "sent" | "failed" = "sent"): ChatItem => ({ kind: "user", id, text, state });
const agent = (id: string, text = "On it."): ChatItem => ({ kind: "agent", id, turnId: "history", text });
const wrote = (id: string, component: string, file = "prototype.tsx", state: "done" | "failed" = "done"): ChatItem => ({
  kind: "activity",
  id,
  turnId: "history",
  toolCallId: id,
  op: "add",
  path: `specs/design/components/${component}/${file}`,
  state,
});
const READY = new Set(["expense-web", "admin-web"]);

describe("prototypeNotes", () => {
  it("offers a component's review after the /prototype exchange that wrote it", () => {
    const items = [user("u1", "/prototype expense-web"), agent("a1"), wrote("w1", "expense-web", "prototype.json"), wrote("w2", "expense-web"), agent("a2", "Done.")];
    expect(prototypeNotes(items, READY, false)).toEqual([
      {
        afterId: "a2",
        text: "The expense-web prototype is ready to review.",
        actions: [{ kind: "open-prototype", label: "Open prototype", component: "expense-web" }],
      },
    ]);
  });

  it("offers the Prototype tab when an exchange made several", () => {
    const items = [user("u1", "/prototype"), wrote("w1", "expense-web"), wrote("w2", "admin-web")];
    expect(prototypeNotes(items, READY, false)[0]?.actions).toEqual([{ kind: "open-prototype", label: "Open prototypes", component: null }]);
  });

  it("says nothing for an exchange that wrote no prototype, whose writes were refused, or whose prototype is not valid now", () => {
    expect(prototypeNotes([user("u1", "/prototype expense-web"), agent("a1", "Up to date.")], READY, false)).toEqual([]);
    expect(prototypeNotes([user("u1", "/prototype expense-web"), wrote("w1", "expense-web", "prototype.tsx", "failed")], READY, false)).toEqual([]);
    expect(prototypeNotes([user("u1", "/prototype expense-web"), wrote("w1", "expense-web", "prototype.json")], new Set(), false)).toEqual([]);
  });

  it("says nothing for writes another kind of turn made", () => {
    expect(prototypeNotes([user("u1", "Tweak the prototype"), wrote("w1", "expense-web")], READY, false)).toEqual([]);
  });

  it("waits for the running exchange to end, and keeps every earlier one's", () => {
    const items = [user("u1", "/prototype expense-web"), wrote("w1", "expense-web"), user("u2", "Thanks", "failed"), user("u3", "/prototype expense-web"), wrote("w2", "expense-web")];
    expect(prototypeNotes(items, READY, true).map((n) => n.afterId)).toEqual(["w1"]);
    expect(prototypeNotes(items, READY, false).map((n) => n.afterId)).toEqual(["w1", "w2"]);
  });
});
