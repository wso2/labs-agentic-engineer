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
import { chatViewFor, homeLink, questionsLink, viewTurnBody, wireQuery } from "./chatView";

// A project has a chat per view: the Issues Page talks to its own agent on its
// own thread, and each issue's card to that issue's own; everywhere else is the
// main chat.

describe("chatViewFor", () => {
  it("is the issues view on the Issues Page with no card open", () => {
    expect(chatViewFor("issues", null)).toBe("issues");
  });

  it("is the issue's own view on an Issue card", () => {
    expect(chatViewFor("issues", "issue", 7)).toBe("issue");
  });

  it("is the issue's own view on the Questions card answering that issue's chat", () => {
    expect(chatViewFor("issues", "questions", 7)).toBe("issue");
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
    expect(questionsLink("shop", "main")).toEqual({ to: "/projects/$projectName/questions", params: { projectName: "shop" } });
    expect(homeLink("shop", "main")).toEqual({ to: "/projects/$projectName", params: { projectName: "shop" } });
  });

  it("is the Issues Page's card for the issues view", () => {
    expect(questionsLink("shop", "issues")).toEqual({ to: "/projects/$projectName/issues/questions", params: { projectName: "shop" } });
    expect(homeLink("shop", "issues")).toEqual({ to: "/projects/$projectName/issues", params: { projectName: "shop" } });
  });

  it("is the Issues Page's card, naming the issue, for an issue's chat, which closes back to the issue's card", () => {
    expect(questionsLink("shop", "issue", 7)).toEqual({
      to: "/projects/$projectName/issues/questions",
      params: { projectName: "shop" },
      search: { issue: 7 },
    });
    expect(homeLink("shop", "issue", 7)).toEqual({
      to: "/projects/$projectName/issues/$number",
      params: { projectName: "shop", number: "7" },
    });
  });
});

describe("wireQuery", () => {
  it("names the issues view alone: the main chat is the absence of a view", () => {
    expect(wireQuery("issues")).toEqual({ view: "issues" });
    expect(wireQuery("main")).toBeUndefined();
    expect(wireQuery(undefined)).toBeUndefined();
  });

  it("names an issue's chat by the view and its number", () => {
    expect(wireQuery("issue", 7)).toEqual({ view: "issue", issueNumber: 7 });
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

  it("sends an issue's turn as the words, the view and the issue's number", () => {
    expect(viewTurnBody("issue", roomTurn, 7)).toEqual({ instruction: "Go", view: "issue", issueNumber: 7 });
  });
});
