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
 * `isTurnSpec` — the agents server's pre-stream 400 check. The point of the
 * guard is that a malformed body fails with a status code instead of composing
 * a nonsense instruction, so the cases that matter are the ones where a field
 * the kind DEPENDS on is missing or the wrong type.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { isPrototypeFeedback, isTurnSpec, PROTOTYPE_FEEDBACK_LIMITS } from "../src/contracts/sse-events.js";

test("accepts each well-formed kind", () => {
  assert.ok(isTurnSpec({ kind: "chat", text: "add a returns policy" }));
  assert.ok(isTurnSpec({ kind: "flow", skill: "design" }));
  assert.ok(isTurnSpec({ kind: "flow", skill: "amend", text: "add an actor" }));
  assert.ok(isTurnSpec({ kind: "start" }));
  assert.ok(isTurnSpec({ kind: "start", idea: "an expense tracker" }));
  // Reference documents ride as paths; a non-string list is a caller bug and
  // must be refused before it reaches prompt composition.
  assert.ok(isTurnSpec({ kind: "start", references: ["specs/requirements/references/rfp.pdf"] }));
  assert.ok(isTurnSpec({ kind: "flow", skill: "design", references: ["specs/requirements/references/sketch.png"] }));
  assert.equal(isTurnSpec({ kind: "flow", skill: "design", references: [7] }), false);
  assert.ok(isTurnSpec({ kind: "start", references: [] }));
  assert.equal(isTurnSpec({ kind: "start", references: [1, 2] }), false);
  assert.equal(isTurnSpec({ kind: "start", references: "rfp.pdf" }), false);
  assert.ok(isTurnSpec({ kind: "plan" }));
});

test("rejects a missing or unknown discriminant", () => {
  assert.equal(isTurnSpec(undefined), false);
  assert.equal(isTurnSpec(null), false);
  assert.equal(isTurnSpec("chat"), false);
  assert.equal(isTurnSpec({}), false);
  assert.equal(isTurnSpec({ kind: "generate" }), false);
});

test("rejects a kind whose required field is missing, blank, or mistyped", () => {
  assert.equal(isTurnSpec({ kind: "chat" }), false);
  assert.equal(isTurnSpec({ kind: "chat", text: "   " }), false);
  assert.equal(isTurnSpec({ kind: "chat", text: 42 }), false);
  assert.equal(isTurnSpec({ kind: "flow" }), false);
  assert.equal(isTurnSpec({ kind: "flow", skill: "" }), false);
  assert.equal(isTurnSpec({ kind: "flow", skill: "design", text: 1 }), false);
  assert.equal(isTurnSpec({ kind: "start", idea: 7 }), false);
});

test("plan scope and task context are validated when present", () => {
  assert.ok(
    isTurnSpec({
      kind: "plan",
      scope: { tag: "spec-v3", stories: [{ number: 1, title: "Sign in", covered: false }] },
      taskContext: [{ path: "tasks/1.md", body: "# Task" }],
    }),
  );
  // A story row without `covered` would silently plan over already-covered work.
  assert.equal(
    isTurnSpec({ kind: "plan", scope: { tag: "t", stories: [{ number: 1 }] } }),
    false,
  );
  assert.equal(isTurnSpec({ kind: "plan", scope: { tag: 2, stories: [] } }), false);
  assert.equal(isTurnSpec({ kind: "plan", scope: { tag: "t" } }), false);
  assert.equal(isTurnSpec({ kind: "plan", taskContext: [{ path: "tasks/1.md" }] }), false);
  assert.equal(isTurnSpec({ kind: "plan", taskContext: {} }), false);
});

test("a title is optional on a story row", () => {
  assert.ok(isTurnSpec({ kind: "plan", scope: { tag: "t", stories: [{ number: 3, covered: true }] } }));
});

test("unknown extra keys are tolerated", () => {
  assert.ok(isTurnSpec({ kind: "start", idea: "x", futureField: true }));
});

// --- prototype feedback (#817) ------------------------------------------------

const annotation = (over: Record<string, unknown> = {}) => ({
  id: "a1",
  prototypeSchemaVersion: 1,
  screenId: "screen.queue",
  flowId: null,
  stateId: "state.default",
  componentIds: ["queue.table"],
  request: "Show the submitter's department",
  ...over,
});

const feedbackFlow = (feedback: unknown, skill = "prototype") => ({ kind: "flow", skill, prototypeFeedback: feedback });

const feedback = (over: Record<string, unknown> = {}) => ({
  prototypePath: "specs/design/components/portal/prototype.json",
  annotations: [annotation()],
  ...over,
});

test("a /prototype flow may carry a well-formed feedback batch", () => {
  assert.ok(isTurnSpec(feedbackFlow(feedback())));
  // Whole-screen requests and a flow ID are both fine.
  assert.ok(isTurnSpec(feedbackFlow(feedback({ annotations: [annotation({ componentIds: [], flowId: "flow.approve" })] }))));
  assert.ok(isPrototypeFeedback(feedback()));
});

test("feedback rides only the prototype flow", () => {
  assert.equal(isTurnSpec(feedbackFlow(feedback(), "design")), false);
});

test("a malformed feedback batch is refused whole", () => {
  const bad = [
    feedback({ annotations: [] }),
    feedback({ annotations: Array.from({ length: PROTOTYPE_FEEDBACK_LIMITS.annotations + 1 }, (_, i) => annotation({ id: `a${i}` })) }),
    feedback({ annotations: [annotation(), annotation()] }), // duplicate annotation IDs
    feedback({ prototypePath: "specs/design/prototype.json" }),
    feedback({ prototypePath: "specs/design/components/a/b/prototype.json" }),
    feedback({ annotations: [annotation({ componentIds: Array.from({ length: 51 }, (_, i) => `c${i}`) })] }),
    feedback({ annotations: [annotation({ prototypeSchemaVersion: 2 })] }),
    feedback({ annotations: [annotation({ request: "   " })] }),
    feedback({ annotations: [annotation({ screenId: "" })] }),
    feedback({ annotations: [annotation({ flowId: "" })] }),
    feedback({ annotations: [annotation({ flowId: undefined })] }),
    feedback({ annotations: [annotation({ componentIds: ["ok", 3] })] }),
    { annotations: [annotation()] },
    null,
  ];
  for (const b of bad) {
    assert.equal(isPrototypeFeedback(b), false, JSON.stringify(b)?.slice(0, 120));
    assert.equal(isTurnSpec(feedbackFlow(b)), false);
  }
});
