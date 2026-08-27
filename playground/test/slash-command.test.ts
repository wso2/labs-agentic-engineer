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
 * The `/<skill>` chat grammar (`@aep/contracts/commands`). It yields FACTS —
 * which skill, which trailing text — never prompt wording, so what is pinned
 * here is the boundary between "this line is a flow" and "this line is chat".
 *
 * Getting that boundary wrong in the permissive direction eats a user's actual
 * message; getting it wrong the other way silently demotes a command to chat.
 * `aep-api` keeps its own copy of this grammar (internal/spec/start_command.go)
 * because it must classify the same lines in production.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { parseFlowCommand, parseStartCommand } from "@aep/contracts/commands";

// --- a flow -----------------------------------------------------------------

test("bare /<skill> names the skill and no text", () => {
  assert.deepEqual(parseFlowCommand("/spec"), { skill: "spec", text: "" });
  assert.deepEqual(parseFlowCommand("/design"), { skill: "design", text: "" });
});

test("/<skill> with follow-up text carries it", () => {
  assert.deepEqual(parseFlowCommand("/spec an expense tracker"), {
    skill: "spec",
    text: "an expense tracker",
  });
});

test("kebab-case skill tokens are allowed", () => {
  assert.deepEqual(parseFlowCommand("/architecture redo the edges"), {
    skill: "architecture",
    text: "redo the edges",
  });
});

test("surrounding + inner whitespace is normalized", () => {
  assert.deepEqual(parseFlowCommand("  /spec   an app  "), { skill: "spec", text: "an app" });
});

test("multi-line follow-up text is preserved", () => {
  assert.deepEqual(parseFlowCommand("/spec line one\nline two"), {
    skill: "spec",
    text: "line one\nline two",
  });
});

// --- stays literal chat (returns null) --------------------------------------

test("a plain chat line is not a command", () => {
  assert.equal(parseFlowCommand("please regenerate the design"), null);
});

test("a mid-message slash is literal", () => {
  assert.equal(parseFlowCommand("fix the /spec route please"), null);
});

test("a bare slash is literal", () => {
  assert.equal(parseFlowCommand("/"), null);
  assert.equal(parseFlowCommand("/ spec"), null);
});

test("a doubled slash escapes the command", () => {
  assert.equal(parseFlowCommand("//spec"), null);
});

test("a trailing-punctuation token is literal (token must end at whitespace/EOL)", () => {
  assert.equal(parseFlowCommand("/spec."), null);
  assert.equal(parseFlowCommand("/design?"), null);
});

test("uppercase tokens do not match the lowercase skill charset", () => {
  assert.equal(parseFlowCommand("/SPEC"), null);
});

test("empty / whitespace-only input is literal", () => {
  assert.equal(parseFlowCommand(""), null);
  assert.equal(parseFlowCommand("   "), null);
});

// --- /start -----------------------------------------------------------------

test("/start is recognised bare and with an inline idea", () => {
  assert.deepEqual(parseStartCommand("/start"), { inlineIdea: "" });
  assert.deepEqual(parseStartCommand("  /start  "), { inlineIdea: "" });
  assert.deepEqual(parseStartCommand("/start an expense tracker"), {
    inlineIdea: "an expense tracker",
  });
});

test("prose that merely mentions /start is not the command", () => {
  assert.equal(parseStartCommand("where do I /start with the design?"), null);
  assert.equal(parseStartCommand("/started"), null);
});

test("/register-external-resource is a flow command", () => {
  assert.deepEqual(parseFlowCommand("/register-external-resource Register Stripe"), {
    skill: "register-external-resource",
    text: "Register Stripe",
  });
});
