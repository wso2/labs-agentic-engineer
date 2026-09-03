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
import { composeInstruction, aimNote } from "../src/prompts/turn.js";
import { isTurnAim, type TurnAim } from "@aep/agent-stream";
import { projectDisplayHistory } from "../src/conversation/display-history.js";
import type { Conversation } from "../src/store/conversation-store.js";

// An anchored turn (console #666): the user pointed at part of a spec document
// before typing. What is proven here is that the anchor becomes WORDS in this
// service and nowhere else — the console sends the user's own text plus a fact,
// and the preamble is composed here, because a preamble written client-side
// would have to ride `instruction` and would then appear in the transcript as
// something the user said and did not.

const AIM: TurnAim = {
  anchor: {
    file: "specs/requirements/PRD.md",
    nodes: [
      {
        name: "Rounds close automatically thirty minutes before the delivery slot",
        kind: "paragraph",
        context: "Product Decisions",
      },
      { name: "The bot posts one message per round", kind: "list item" },
    ],
  },
  intent: "change",
};

test("an unaimed turn is byte-identical to one from before aiming existed", () => {
  const turn = { kind: "chat", text: "tidy the requirements" } as const;
  assert.equal(composeInstruction(turn), composeInstruction(turn, {}));
  assert.equal(aimNote(undefined), "");
});

test("the aim leads the instruction, so the model knows what it is pointed at first", () => {
  const composed = composeInstruction({ kind: "chat", text: "make these shorter" }, { aim: AIM });
  assert.ok(composed.indexOf("selected part of a document") < composed.indexOf("make these shorter"));
});

test("every selected node is named, with its kind and where it sits", () => {
  const note = aimNote(AIM);
  assert.match(note, /specs\/requirements\/PRD\.md/);
  assert.match(note, /- the paragraph "Rounds close automatically thirty minutes before the delivery slot" \(under Product Decisions\)/);
  // A name that stands alone carries no context, and must not grow an empty one.
  assert.match(note, /- the list item "The bot posts one message per round"\n/);
  assert.doesNotMatch(note, /\(under \)/);
});

// The decision this test exists for (console ADR-0024). The names are LOCATORS,
// not content: the agent is a live peer in the same room, so the document may
// have moved since the user selected. An agent that guesses which paragraph was
// meant is the one failure the whole shape exists to avoid.
test("the note says to resolve against the current document, and to ask on a miss", () => {
  const note = aimNote(AIM);
  assert.match(note, /as it stands now/);
  assert.match(note, /never guess/);
});

// The fence (#654): the selection is the SUBJECT, not a cage. A spec has
// legitimate ripples, so a change may touch what it makes wrong — and nothing
// else. When the right fix lies elsewhere, the agent makes it there and says
// so, because the user asked for a change, not a conversation — Discuss is
// the asking path.
test("a change is fenced to its subject, with consistency allowed and tidying forbidden", () => {
  const note = aimNote(AIM);
  assert.match(note, /the subject, not a cage/);
  assert.match(note, /consistency, never improvement/);
  assert.match(note, /make it there and say so plainly/);
});

test("a discuss carries no fence — it edits nothing to fence", () => {
  assert.doesNotMatch(aimNote({ ...AIM, intent: "discuss" }), /not a cage/);
});

test("discuss and change differ, and only in the wording", () => {
  const change = aimNote(AIM);
  const discuss = aimNote({ ...AIM, intent: "discuss" });
  assert.notEqual(change, discuss);
  assert.match(discuss, /do not edit the document in this turn/i);
  assert.doesNotMatch(change, /do not edit the document in this turn/i);
  // The selection itself reads the same either way — the intent changes what to
  // do with it, never what was pointed at.
  for (const node of AIM.anchor.nodes) {
    assert.ok(change.includes(node.name));
    assert.ok(discuss.includes(node.name));
  }
});

// The guard is what stands between an untrusted body and a preamble that points
// the agent at the wrong scope quietly. A half-read aim is worse than none.
// The caps (CodeRabbit on #670): the anchor locates and never carries, so a
// hand-built request must not smuggle a prompt payload through the locator
// fields the console keeps short by construction.
test("an aim past the size ceilings is rejected", () => {
  const long = (n: number) => "x".repeat(n);
  assert.ok(!isTurnAim({ ...AIM, anchor: { ...AIM.anchor, file: long(513) } }));
  assert.ok(
    !isTurnAim({
      ...AIM,
      anchor: { file: "a.md", nodes: [{ name: long(201), kind: "paragraph" }] },
    }),
  );
  assert.ok(
    !isTurnAim({
      ...AIM,
      anchor: { file: "a.md", nodes: [{ name: "n", kind: long(65) }] },
    }),
  );
  assert.ok(
    !isTurnAim({
      ...AIM,
      anchor: { file: "a.md", nodes: [{ name: "n", kind: "paragraph", context: long(513) }] },
    }),
  );
  assert.ok(
    !isTurnAim({
      ...AIM,
      anchor: {
        file: "a.md",
        nodes: Array.from({ length: 51 }, () => ({ name: "n", kind: "paragraph" })),
      },
    }),
  );
  // At the ceiling is fine — the cap is a ceiling, not a headroom rule.
  assert.ok(
    isTurnAim({
      ...AIM,
      anchor: { file: "a.md", nodes: [{ name: long(200), kind: "paragraph" }] },
    }),
  );
});

test("a malformed aim is rejected whole", () => {
  assert.ok(isTurnAim(AIM));
  assert.ok(!isTurnAim({ ...AIM, intent: "rewrite" }));
  assert.ok(!isTurnAim({ anchor: AIM.anchor }));
  assert.ok(!isTurnAim({ ...AIM, anchor: { file: "", nodes: AIM.anchor.nodes } }));
  assert.ok(!isTurnAim({ ...AIM, anchor: { file: "a.md", nodes: [] } }));
  assert.ok(
    !isTurnAim({ ...AIM, anchor: { file: "a.md", nodes: [{ name: "n" }] } }),
    "a node with no kind",
  );
});

// Without this a reload leaves "make these shorter" with nothing saying what
// "these" was — a record of the words but not the target.
test("the journalled anchor is served back on the display read", () => {
  const conv = {
    messages: [
      { role: "user", content: "composed prompt the browser must never see" },
      { role: "assistant", content: "done" },
    ],
    turns: [
      {
        turnId: "t-1",
        text: "make these shorter",
        anchor: AIM.anchor,
        messageIndex: 0,
        createdAt: new Date(),
      },
    ],
  } as unknown as Conversation;

  const [user] = projectDisplayHistory(conv);
  assert.equal(user?.role, "user");
  assert.equal((user as { content: string }).content, "make these shorter");
  assert.deepEqual((user as { anchor?: unknown }).anchor, AIM.anchor);
});

test("an un-anchored journal entry grows no anchor field", () => {
  const conv = {
    messages: [{ role: "user", content: "composed" }],
    turns: [{ turnId: "t-1", text: "hello", messageIndex: 0, createdAt: new Date() }],
  } as unknown as Conversation;
  assert.ok(!("anchor" in (projectDisplayHistory(conv)[0] as object)));
});
