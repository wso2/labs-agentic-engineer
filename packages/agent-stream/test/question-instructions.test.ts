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
 * The finish-valve serialization (issue #486): the instruction must keep the
 * decision-to-question link — answers already given stay attributed to their
 * question verbatim, unanswered questions are listed for recommended answers
 * tagged `*assumed*` — because the session summary's "N asked, M assumed"
 * split derives from exactly this text.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { buildFinishInstruction, FINISH_PREFIX } from "../src/contracts/sse-events.js";

test("leads with the finish marker", () => {
  const out = buildFinishInstruction([], []);
  assert.ok(out.startsWith(FINISH_PREFIX));
});

test("keeps given answers attributed to their questions", () => {
  const out = buildFinishInstruction(
    [
      { question: "Who owns a round?", selected: ["The opener"] },
      { question: "Any limits?", selected: [], freeText: "cap at 10" },
    ],
    [],
  );
  assert.match(out, /- "Who owns a round\?": The opener/);
  assert.match(out, /- "Any limits\?": cap at 10/);
  assert.match(out, /treat each as the user's decision/);
});

test("lists unanswered questions for *assumed* recommendations", () => {
  const out = buildFinishInstruction([], ["Retention period?", "Who can delete?"]);
  assert.match(out, /Unanswered — apply your recommended answer/);
  assert.match(out, /- "Retention period\?"/);
  assert.match(out, /- "Who can delete\?"/);
  assert.match(out, /\*assumed\*/);
});

test("mixed form carries both blocks, decisions first", () => {
  const out = buildFinishInstruction(
    [{ question: "Sign-in?", selected: ["Google"] }],
    ["Notifications?"],
  );
  const decided = out.indexOf('"Sign-in?"');
  const assumed = out.indexOf('"Notifications?"');
  assert.ok(decided >= 0 && assumed >= 0 && decided < assumed);
});

test("combines a selection with its free-text note", () => {
  const out = buildFinishInstruction(
    [{ question: "Q", selected: ["A", "B"], freeText: "note" }],
    [],
  );
  assert.match(out, /- "Q": A, B — note/);
});

test("empty form still instructs recommended answers for the remaining scope", () => {
  const out = buildFinishInstruction([], []);
  assert.match(out, /every remaining undecided area/);
  assert.match(out, /\*assumed\*/);
});
