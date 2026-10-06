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
import { buildAnswerInstruction } from "@aep/agent-stream";
import { scriptIssuesTurn } from "./issuesAgent";

// The mock Issues agent: it classifies a report, drafts the issue, asks "File
// this issue?", and files on the answer. A vague report gets the kind question
// first. It mirrors the real agent's tools and never speaks of the classifier.

type Part = { type: string; toolName?: string; input?: unknown; delta?: string };

function calls(turn: { frames: { part: Part }[] }, toolName: string): Part[] {
  return turn.frames.map((f) => f.part).filter((p) => p.type === "tool-call" && p.toolName === toolName);
}

function said(turn: { frames: { part: Part }[] }): string {
  return turn.frames.map((f) => (f.part.type === "text-delta" ? (f.part.delta ?? "") : "")).join("");
}

const REPORT = "The Save button on the expense form does nothing";
const FILE_IT = buildAnswerInstruction("File this issue?", ["File it"]);
const KIND_BUG = buildAnswerInstruction("What kind of issue is this?", ["Bug"]);

describe("a report to the mock Issues agent", () => {
  it("is classified, searched against, drafted, and confirmed before filing", () => {
    const turn = scriptIssuesTurn(REPORT, 15);
    expect(calls(turn, "classify_report")).toHaveLength(1);
    expect(calls(turn, "search_issues")).toHaveLength(1);
    const asks = calls(turn, "ask_question");
    expect(asks).toHaveLength(1);
    const input = asks[0]!.input as { question: string; options: { label: string; recommended?: boolean }[] };
    expect(input.question).toBe("File this issue?");
    expect(input.options.map((o) => o.label)).toEqual(["File it", "Change it"]);
    expect(input.options[0]!.recommended).toBe(true);
    expect(turn.filed).toBeUndefined();
    expect(turn.display).toBe(REPORT);
  });

  it("drafts a bug from a confident report, without the classifier's name or scores", () => {
    const text = said(scriptIssuesTurn(REPORT, 15));
    expect(text).toMatch(/bug/i);
    expect(text).toContain("Save button on the expense form does nothing");
    expect(text).not.toMatch(/jev|confidence|0\.\d/i);
  });

  it("asks which kind it is when the report is vague", () => {
    const turn = scriptIssuesTurn("the export is kind of annoying", 15);
    expect(calls(turn, "classify_report")).toHaveLength(1);
    const asks = calls(turn, "ask_question");
    expect(asks).toHaveLength(1);
    const input = asks[0]!.input as { question: string; options: { label: string }[] };
    expect(input.question).toBe("What kind of issue is this?");
    expect(input.options.map((o) => o.label)).toEqual(["Bug", "Feature request", "Improvement"]);
    expect(calls(turn, "search_issues")).toHaveLength(0);
    expect(turn.filed).toBeUndefined();
  });

  it("drafts a feature request from a wish", () => {
    const turn = scriptIssuesTurn("Please add dark mode to the dashboard", 15);
    expect(calls(turn, "ask_question")).toHaveLength(1);
    const classified = turn.frames.find((f) => f.part.type === "tool-result" && (f.part as Part).toolName === "classify_report")!.part as unknown as {
      output: { kind: string; needsClarification: boolean };
    };
    expect(classified.output).toMatchObject({ kind: "feature", needsClarification: false });
  });
});

describe("the answers to the mock Issues agent's questions", () => {
  it("files the drafted bug on File it, and says its number", () => {
    const turn = scriptIssuesTurn(FILE_IT, 15, [REPORT]);
    expect(turn.filed).toMatchObject({ kind: "bug", title: expect.stringContaining("Save button") });
    expect(turn.filed!.body).toContain(REPORT);
    expect(calls(turn, "create_issue")).toHaveLength(1);
    expect(said(turn)).toContain("#15");
    expect(calls(turn, "ask_question")).toHaveLength(0);
  });

  it("files a feature request as kind feature", () => {
    const turn = scriptIssuesTurn(FILE_IT, 16, ["Please add dark mode to the dashboard"]);
    expect(turn.filed!.kind).toBe("feature");
    expect(said(turn)).toContain("#16");
  });

  it("files as a bug when there is nothing earlier to draft from", () => {
    expect(scriptIssuesTurn(FILE_IT, 15).filed!.kind).toBe("bug");
  });

  it("files nothing on Change it, and asks what to change", () => {
    const turn = scriptIssuesTurn(buildAnswerInstruction("File this issue?", ["Change it"]), 15, [REPORT]);
    expect(turn.filed).toBeUndefined();
    expect(calls(turn, "create_issue")).toHaveLength(0);
    expect(said(turn)).toMatch(/change/i);
  });

  it("drafts with the kind the user chose, then asks to file", () => {
    const turn = scriptIssuesTurn(KIND_BUG, 15, ["the export is kind of annoying"]);
    expect(turn.filed).toBeUndefined();
    const asks = calls(turn, "ask_question");
    expect((asks[0]!.input as { question: string }).question).toBe("File this issue?");
    const filing = scriptIssuesTurn(FILE_IT, 15, ["the export is kind of annoying", KIND_BUG]);
    expect(filing.filed!.kind).toBe("bug");
  });

  it("persists its tool calls in the reply for the history", () => {
    const turn = scriptIssuesTurn(REPORT, 15);
    const assistant = turn.reply[0]!.content as { type: string; toolName?: string }[];
    expect(assistant.filter((p) => p.type === "tool-call").map((p) => p.toolName)).toEqual(["classify_report", "search_issues", "ask_question"]);
  });
});
