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

import { test } from "node:test";
import assert from "node:assert/strict";
import type { AskQuestionOption, StreamPart } from "@aep/agent-stream";
import { pendingQuestions } from "../src/engine/questions.js";
import { parseSelection, renderSessionHeader } from "../src/tui/questions.js";

const OPTIONS: AskQuestionOption[] = [
  { label: "Individual consumers", recommended: true },
  { label: "Enterprise teams", description: "B2B" },
  { label: "Both" },
];

function toolCall(toolName: string, input: unknown): StreamPart {
  return { type: "tool-call", toolCallId: "c1", toolName, input } as unknown as StreamPart;
}

// --- pendingQuestions --------------------------------------------------------

test("pendingQuestions: an ask_question call becomes a one-element list", () => {
  const pending = pendingQuestions([
    toolCall("editFile", { path: "x" }),
    toolCall("ask_question", { question: "Who?", options: OPTIONS }),
  ]);
  assert.ok(pending);
  assert.equal(pending.batch, false);
  assert.equal(pending.questions.length, 1);
  assert.equal(pending.questions[0]?.question, "Who?");
});

test("pendingQuestions: an ask_questions call unwraps the form's questions", () => {
  const pending = pendingQuestions([
    toolCall("ask_questions", { questions: [{ question: "A", options: OPTIONS }, { question: "B", options: OPTIONS }] }),
  ]);
  assert.ok(pending);
  assert.equal(pending.batch, true);
  assert.equal(pending.questions.length, 2);
});

test("pendingQuestions: no question tool-call ⇒ undefined (ordinary turn)", () => {
  assert.equal(pendingQuestions([toolCall("editFile", { path: "x" })]), undefined);
});

// --- grilling sessions (#486) ------------------------------------------------
// The `session` checklist is what distinguishes a session ROUND from a one-form
// interview. Dropping it here would make a session indistinguishable from a
// single form in the playground and in every eval transcript — the exact
// failure the reachability round was opened for.

test("pendingQuestions: a session round carries its area checklist through", () => {
  const pending = pendingQuestions([
    toolCall("ask_questions", {
      session: {
        title: "Voting & nominations",
        areas: [
          { name: "Eligibility", state: "now" },
          { name: "Quorum", state: "todo" },
        ],
      },
      questions: [{ question: "Who may vote?", options: OPTIONS }],
    }),
  ]);
  assert.ok(pending);
  assert.equal(pending.session?.title, "Voting & nominations");
  assert.deepEqual(pending.session?.areas, [
    { name: "Eligibility", state: "now" },
    { name: "Quorum", state: "todo" },
  ]);
});

test("pendingQuestions: a one-form interview has no session", () => {
  const pending = pendingQuestions([
    toolCall("ask_questions", { questions: [{ question: "A", options: OPTIONS }] }),
  ]);
  assert.equal(pending?.session, undefined);
});

test("pendingQuestions: a malformed checklist costs the header, never the questions", () => {
  const pending = pendingQuestions([
    toolCall("ask_questions", {
      session: { title: "Broken" },
      questions: [{ question: "A", options: OPTIONS }],
    }),
  ]);
  assert.equal(pending?.questions.length, 1);
  assert.equal(pending?.session, undefined);
});

test("renderSessionHeader: every area shows, marked by state", () => {
  const out = renderSessionHeader({
    title: "Voting & nominations",
    areas: [
      { name: "Eligibility", state: "done" },
      { name: "Quorum", state: "now" },
      { name: "Nominee limits", state: "todo" },
    ],
  });
  assert.match(out, /Grilling session — Voting & nominations/);
  assert.match(out, /✔ Eligibility/);
  assert.match(out, /▸ Quorum/);
  assert.match(out, /○ Nominee limits/);
});

test("renderSessionHeader: a titleless session still announces itself", () => {
  const out = renderSessionHeader({ areas: [{ name: "Reviews", state: "now" }] });
  assert.match(out, /Grilling session\n/);
  assert.match(out, /▸ Reviews/);
});

// --- parseSelection ----------------------------------------------------------

test("parseSelection: a single number picks that option's label", () => {
  assert.deepEqual(parseSelection("1", OPTIONS), { selected: ["Individual consumers"] });
});

test("parseSelection: comma/space lists pick several (multiSelect), de-duped", () => {
  assert.deepEqual(parseSelection("1, 2", OPTIONS), {
    selected: ["Individual consumers", "Enterprise teams"],
  });
  assert.deepEqual(parseSelection("2 2", OPTIONS), { selected: ["Enterprise teams"] });
});

test("parseSelection: a trailing em-dash note rides alongside the picks", () => {
  assert.deepEqual(parseSelection("1 — mobile-first", OPTIONS), {
    selected: ["Individual consumers"],
    freeText: "mobile-first",
  });
  // double-hyphen is accepted too (terminals without an em-dash key)
  assert.deepEqual(parseSelection("3 -- both markets", OPTIONS), {
    selected: ["Both"],
    freeText: "both markets",
  });
});

test("parseSelection: non-numeric input is taken verbatim as free text (the Other path)", () => {
  assert.deepEqual(parseSelection("hobbyists and students", OPTIONS), {
    selected: [],
    freeText: "hobbyists and students",
  });
});

test("parseSelection: an out-of-range number is not a clean pick → free text", () => {
  assert.deepEqual(parseSelection("9", OPTIONS), { selected: [], freeText: "9" });
});
