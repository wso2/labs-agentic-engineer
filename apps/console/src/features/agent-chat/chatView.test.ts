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
import { chatViewFor, homePath, questionsPath, viewTurnBody, wireView } from "./chatView";

// A project has a chat per view: the Issues Page talks to its own agent on its
// own thread; everywhere else, including the Issue card over it, is the main chat.

describe("chatViewFor", () => {
  it("is the issues view on the Issues Page with no card open", () => {
    expect(chatViewFor("issues", null)).toBe("issues");
  });

  it("is the main chat on the Issue card, which no agent works on yet", () => {
    expect(chatViewFor("issues", "issue")).toBe("main");
  });

  it("is the issues view on the Questions card over the Issues Page, and the main chat on the one over the overview", () => {
    expect(chatViewFor("issues", "questions")).toBe("issues");
    expect(chatViewFor("overview", "questions")).toBe("main");
  });

  it("is the main chat on every other Page", () => {
    expect(chatViewFor("overview", null)).toBe("main");
    expect(chatViewFor("builds", null)).toBe("main");
    expect(chatViewFor("deploy", null)).toBe("main");
  });
});

describe("where a view's questions are answered, and where it closes back to", () => {
  it("is the overview's card for the main chat", () => {
    expect(questionsPath("main")).toBe("/projects/$projectName/questions");
    expect(homePath("main")).toBe("/projects/$projectName");
  });

  it("is the Issues Page's card for the issues view", () => {
    expect(questionsPath("issues")).toBe("/projects/$projectName/issues/questions");
    expect(homePath("issues")).toBe("/projects/$projectName/issues");
  });
});

describe("wireView", () => {
  it("names only the issues view: the main chat is the absence of a view", () => {
    expect(wireView("issues")).toBe("issues");
    expect(wireView("main")).toBeUndefined();
    expect(wireView(undefined)).toBeUndefined();
  });
});

describe("viewTurnBody", () => {
  const roomTurn = { instruction: "Go", collab: true, scope: { kind: "design-review" as const } };

  it("leaves a main-chat turn as it is", () => {
    expect(viewTurnBody("main", roomTurn)).toBe(roomTurn);
  });

  it("sends an issues turn as the words and the view alone: it has no spec room and no scope", () => {
    expect(viewTurnBody("issues", roomTurn)).toEqual({ instruction: "Go", view: "issues" });
  });
});
