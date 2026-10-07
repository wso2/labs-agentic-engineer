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

/**
 * Slash commands parsed in the pod (port of aep-api's
 * `A/spec/start_command.go`): a raw instruction becomes a `TurnSpec` and its
 * flow token. `/start` takes the idea typed inline, else the one the project
 * lookup read from the descriptor; `/start` and flow turns carry the
 * reference documents as snapshot paths; chat stays reference-free.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { startTurnSummary, turnSpecFor } from "../src/turns/start-spec.js";

const REFS = { references: ["z-notes.md", "brief.pdf"] };
const PATHS = ["specs/requirements/references/brief.pdf", "specs/requirements/references/z-notes.md"];

test("/start with no inline idea takes the idea from the lookup reply", () => {
  assert.deepEqual(turnSpecFor("/start", { idea: "A greeter that waves", ...REFS }), {
    spec: { kind: "start", idea: "A greeter that waves", references: PATHS },
    flow: "start",
  });
  assert.deepEqual(turnSpecFor("  /start  ", { idea: "  padded  ", references: [] }), {
    spec: { kind: "start", idea: "padded" },
    flow: "start",
  });
});

test("an inline /start idea wins over the lookup's", () => {
  assert.deepEqual(turnSpecFor("/start  A todo app\nwith tags ", { idea: "the descriptor idea", references: [] }), {
    spec: { kind: "start", idea: "A todo app\nwith tags" },
    flow: "start",
  });
});

test("/start with no idea anywhere carries none (the start skill asks)", () => {
  assert.deepEqual(turnSpecFor("/start", { references: [] }), { spec: { kind: "start" }, flow: "start" });
  assert.deepEqual(turnSpecFor("/start", { idea: "   ", references: [] }), { spec: { kind: "start" }, flow: "start" });
});

test("/design brief → flow design, with the references as snapshot paths", () => {
  assert.deepEqual(turnSpecFor("/design brief", REFS), {
    spec: { kind: "flow", skill: "design", text: "brief", references: PATHS },
    flow: "design",
  });
  assert.deepEqual(turnSpecFor("/feature", { references: [] }), {
    spec: { kind: "flow", skill: "feature" },
    flow: "feature",
  });
});

test("plain text is chat with no flow and no references, sent verbatim", () => {
  assert.deepEqual(turnSpecFor("  add a login page ", REFS), {
    spec: { kind: "chat", text: "  add a login page " },
    flow: "",
  });
});

test("only a narrow command shape is a command; everything else is chat", () => {
  for (const raw of ["//x", "/", "/ design", "hello /design", "/Design", "/design.", "/design!now", "/_x"]) {
    assert.deepEqual(turnSpecFor(raw, REFS), { spec: { kind: "chat", text: raw }, flow: "" }, raw);
  }
});

test("startTurnSummary appends the resolved idea to a bare /start only", () => {
  assert.equal(startTurnSummary("/start", { kind: "start", idea: "A greeter" }), "/start A greeter");
  assert.equal(startTurnSummary("  /start ", { kind: "start", idea: "A greeter" }), "/start A greeter");
  assert.equal(startTurnSummary("/start typed idea", { kind: "start", idea: "typed idea" }), "/start typed idea");
  assert.equal(startTurnSummary("/start", { kind: "start" }), "/start");
  assert.equal(startTurnSummary("/design x", { kind: "flow", skill: "design", text: "x" }), "/design x");
  assert.equal(startTurnSummary("hi", { kind: "chat", text: "hi" }), "hi");
});

test("/interview F<n> (main's interview command shape) is the interview flow with the feature as its text", () => {
  assert.deepEqual(turnSpecFor("/interview F3", REFS), {
    spec: { kind: "flow", skill: "interview", text: "F3", references: PATHS },
    flow: "interview",
  });
});

test("a review batch rides the /prototype flow it was sent with", () => {
  const batch = {
    prototypeHash: "0".repeat(64),
    component: "web",
    requests: [{ screenId: "s", roleId: "r", stateId: "st", elementIds: [], text: "t" }],
  };
  assert.deepEqual(turnSpecFor("/prototype web", { references: [] }, batch), {
    spec: { kind: "flow", skill: "prototype", text: "web", prototypeFeedback: batch },
    flow: "prototype",
  });
});
