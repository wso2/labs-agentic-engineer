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
import { buildAnswerInstruction, type AskQuestionInput } from "@aep/agent-stream";
import { scriptIssueTurn, type MockIssue } from "./issueAgent";

// The mock agent of one issue: it reads the issue, drafts the change the user
// asks for, shows it as the confirm option's description on a question card
// (as the agents service's gate requires), and makes it only on that answer.

type Part = { type: string; toolName?: string; input?: unknown; output?: unknown; delta?: string };

const ISSUE: MockIssue = { number: 11, title: "Should receipts accept PDFs?", body: "Finance asked.", state: "open" };

function calls(turn: { frames: { part: Part }[] }, toolName: string): Part[] {
  return turn.frames.map((f) => f.part).filter((p) => p.type === "tool-call" && p.toolName === toolName);
}

function said(turn: { frames: { part: Part }[] }): string {
  return turn.frames.map((f) => (f.part.type === "text-delta" ? (f.part.delta ?? "") : "")).join("");
}

function asked(turn: { frames: { part: Part }[] }): AskQuestionInput {
  const asks = calls(turn, "ask_question");
  expect(asks).toHaveLength(1);
  return asks[0]!.input as AskQuestionInput;
}

const confirm = (input: AskQuestionInput, label: string) => input.options.find((o) => o.label === label)!;

describe("the mock agent of an issue", () => {
  it("reads the issue and answers a question about it, changing nothing", () => {
    const turn = scriptIssueTurn("What is this about?", ISSUE);
    expect(calls(turn, "get_issue")).toHaveLength(1);
    expect(said(turn)).toContain("Should receipts accept PDFs?");
    expect(calls(turn, "ask_question")).toHaveLength(0);
    expect(turn.change).toBeUndefined();
  });

  it("drafts a comment and asks Post this comment?, the comment as Post it's description; posts it on Post it", () => {
    const request = "Comment that PDFs are accepted from v2";
    const question = asked(scriptIssueTurn(request, ISSUE));
    expect(question.question).toBe("Post this comment?");
    expect(question.options.map((o) => o.label)).toEqual(["Post it", "Not now"]);
    const body = confirm(question, "Post it").description!;
    expect(body).toContain("PDFs are accepted from v2");

    const turn = scriptIssueTurn(buildAnswerInstruction("Post this comment?", ["Post it"]), ISSUE, [request]);
    expect(calls(turn, "comment_issue").map((c) => c.input)).toEqual([{ body }]);
    expect(turn.change).toEqual({ kind: "comment", body });
  });

  it("asks Close this issue?, the reason as Close it's description; closes it on Close it", () => {
    const request = "Close it, finance no longer needs PDFs";
    const question = asked(scriptIssueTurn(request, ISSUE));
    expect(question.question).toBe("Close this issue?");
    const reason = confirm(question, "Close it").description!;
    expect(reason).toMatch(/finance no longer needs PDFs/i);

    const turn = scriptIssueTurn(buildAnswerInstruction("Close this issue?", ["Close it"]), ISSUE, [request]);
    expect(calls(turn, "close_issue").map((c) => c.input)).toEqual([{ reason }]);
    expect(turn.change).toEqual({ kind: "close", reason });
  });

  it("asks Apply this edit?, the new title as Apply it's description", () => {
    const question = asked(scriptIssueTurn("Rename it to Accept PDF receipts", ISSUE));
    expect(question.question).toBe("Apply this edit?");
    expect(confirm(question, "Apply it").description).toBe("Title: Accept PDF receipts");
    const turn = scriptIssueTurn(buildAnswerInstruction("Apply this edit?", ["Apply it"]), ISSUE, ["Rename it to Accept PDF receipts"]);
    expect(calls(turn, "edit_issue").map((c) => c.input)).toEqual([{ title: "Accept PDF receipts" }]);
    expect(turn.change).toEqual({ kind: "edit", title: "Accept PDF receipts" });
  });

  it("asks Hand this to the coding agent?, the component as Hand it over's description", () => {
    const turn = scriptIssueTurn("Hand it to the coding agent", ISSUE);
    expect(calls(turn, "list_components")).toHaveLength(1);
    const question = asked(turn);
    expect(question.question).toBe("Hand this to the coding agent?");
    expect(confirm(question, "Hand it over").description).toMatch(/^Component: /);
  });

  it("changes nothing on Not now", () => {
    const turn = scriptIssueTurn(buildAnswerInstruction("Post this comment?", ["Not now"]), ISSUE, ["Comment that it works"]);
    expect(turn.change).toBeUndefined();
    expect(calls(turn, "comment_issue")).toHaveLength(0);
  });
});
