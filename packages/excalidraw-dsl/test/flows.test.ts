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
import { dslToExcalidraw, tryDslToPrototype, validateWireframeSyntax } from "../src/index.js";

const TWO_FLOWS = `screen Login "Sign in"
  button "Sign in" primary -> AdminQueue
screen AdminQueue "Admin: approval queue"
  button "Open" -> AuditDetail
screen AuditDetail "Admin: audit detail"
screen Orders "Customer: my orders"

flow "Admin path"
  Login
  AdminQueue
  AuditDetail

flow "Customer path"
  Login
  Orders
`;

function model(dsl: string) {
  const res = tryDslToPrototype(dsl);
  assert.ok(res.ok, `expected ok, got ${!res.ok ? res.error : ""}`);
  return res.model;
}

test("a duplicate screen name is rejected with the second declaration's line number", () => {
  // Two screens with one name merge into one fingerprint and one screenOrder
  // entry, and their screen-relative element ids collide — Excalidraw scenes
  // must not carry duplicate ids. Always an authoring bug; reject like a
  // duplicate flow name.
  const errs = validateWireframeSyntax(`screen One
  heading "A"
screen One
  heading "B"
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 3: /);
  assert.match(errs[0]!, /duplicate screen "One"/);
});

test("duplicate screen detection is case-insensitive, matching screen-name resolution", () => {
  const errs = validateWireframeSyntax(`screen Login
  heading "A"
screen LOGIN
  heading "B"
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 3: /);
});

test("a valid DSL with named flows passes the write gate", () => {
  assert.deepEqual(validateWireframeSyntax(TWO_FLOWS), []);
});

test("a flow naming a screen that does not exist is rejected with its line number", () => {
  const errs = validateWireframeSyntax(`screen Login "Sign in"

flow "Admin path"
  Login
  Typo
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 5: /);
  assert.match(errs[0]!, /unknown screen "Typo"/);
});

test("a flow may reference a screen declared later in the file", () => {
  assert.deepEqual(
    validateWireframeSyntax(`flow "Admin path"
  Login
screen Login "Sign in"
`),
    [],
  );
});

test("declaring the same flow name twice is rejected", () => {
  const errs = validateWireframeSyntax(`screen Login "Sign in"
screen Orders "Orders"

flow "Admin path"
  Login

flow "Admin path"
  Orders
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 7: /);
  assert.match(errs[0]!, /duplicate flow "Admin path"/);
});

test("a screen in no flow is not an error", () => {
  assert.deepEqual(
    validateWireframeSyntax(`screen Login "Sign in"
screen Stranded "Nobody lists me"

flow "Admin path"
  Login
`),
    [],
  );
});

test("the legacy unnamed flow block still parses and still reports nothing", () => {
  assert.deepEqual(
    validateWireframeSyntax(`screen Login "Sign in"
screen Dashboard "Home"

flow
  Login -> Dashboard
`),
    [],
  );
});

test("the model publishes each declared flow, in declaration order, entry screen first", () => {
  const m = model(TWO_FLOWS);
  assert.deepEqual(
    m.flows.map((f) => f.name),
    ["Admin path", "Customer path"],
  );
  assert.deepEqual(m.flows[0]!.screens, ["Login", "AdminQueue", "AuditDetail"]);
  assert.deepEqual(m.flows[1]!.screens, ["Login", "Orders"]);
});

test("a shared screen is compiled once and referenced by both flows", () => {
  const m = model(TWO_FLOWS);
  assert.equal(m.screens.filter((s) => s.name === "Login").length, 1);
  assert.ok(m.flows.every((f) => f.screens.includes("Login")));
});

test("a DSL with no named flows compiles to an empty flow list", () => {
  const m = model(`screen Login "Sign in"
screen Dashboard "Home"

flow
  Login -> Dashboard
`);
  assert.deepEqual(m.flows, []);
});

test("flow screen references resolve case-insensitively to the canonical name", () => {
  const m = model(`screen Login "Sign in"
screen AdminQueue "Queue"

flow "Admin path"
  login
  ADMINQUEUE
`);
  assert.deepEqual(m.flows[0]!.screens, ["Login", "AdminQueue"]);
});

test("a screen listed twice in one flow keeps its first position", () => {
  const m = model(`screen Login "Sign in"
screen AdminQueue "Queue"

flow "Admin path"
  Login
  AdminQueue
  Login
`);
  assert.deepEqual(m.flows[0]!.screens, ["Login", "AdminQueue"]);
});

function canvasTexts(dsl: string): string[] {
  const scene = JSON.parse(dslToExcalidraw("wireframes", dsl)) as {
    elements: Array<{ type: string; text?: string }>;
  };
  return scene.elements.filter((e) => e.type === "text").map((e) => e.text ?? "");
}

test("a screen in exactly one flow is labelled with that flow's name", () => {
  const texts = canvasTexts(TWO_FLOWS);
  assert.ok(texts.includes("Admin path · Screen 2"), texts.join(" | "));
  assert.ok(texts.includes("Customer path · Screen 4"), texts.join(" | "));
});

test("a screen in two or more flows is labelled Common", () => {
  const texts = canvasTexts(TWO_FLOWS);
  assert.ok(texts.includes("Common · Screen 1"), texts.join(" | "));
});

test("a screen in no flow keeps the bare screen-number marker", () => {
  const texts = canvasTexts(`screen Login "Sign in"
screen Stranded "Nobody lists me"

flow "Admin path"
  Login
`);
  assert.ok(texts.includes("Screen 2"), texts.join(" | "));
  assert.ok(!texts.some((t) => t.endsWith("· Screen 2")), "unassigned screen must carry no flow label");
});

test("a DSL with no named flows carries no flow label anywhere on the canvas", () => {
  const texts = canvasTexts(`screen Login "Sign in"
  button "Sign in" primary -> Dashboard
screen Dashboard "Home"
  navbar "App | Home"
`);
  assert.ok(texts.includes("Screen 1"), texts.join(" | "));
  assert.ok(texts.includes("Screen 2"), texts.join(" | "));
  // Target the flow-label marker format specifically ("... · Screen N" at the
  // end of a string), not any middle dot on the canvas: the `-> Dashboard`
  // arrow above legitimately draws its own "→ Screen 2 · Dashboard" nav
  // marker, which carries a middle dot of its own and must not trip this.
  assert.ok(
    !texts.some((t) => /· Screen \d+$/.test(t)),
    "no screen may gain a flow label",
  );
});

test("role and description keyword lines attach to the flow", () => {
  const m = model(`screen MyRisks "Owner: my risks"
screen NewRisk "Owner: log a risk"

flow "Log a risk"
  role "Risk owner"
  description "An owner records a new risk and tracks it"
  MyRisks
  NewRisk
`);
  assert.equal(m.flows[0]!.role, "Risk owner");
  assert.equal(m.flows[0]!.description, "An owner records a new risk and tracks it");
  assert.deepEqual(m.flows[0]!.screens, ["MyRisks", "NewRisk"]);
});

test("role and description are optional and independent", () => {
  const m = model(`screen A "a"
screen B "b"

flow "First"
  role "Admin"
  A

flow "Second"
  description "No role declared"
  B

flow "Third"
  A
  B
`);
  assert.equal(m.flows[0]!.role, "Admin");
  assert.equal(m.flows[0]!.description, undefined);
  assert.equal(m.flows[1]!.role, undefined);
  assert.equal(m.flows[1]!.description, "No role declared");
  assert.equal(m.flows[2]!.role, undefined);
  assert.equal(m.flows[2]!.description, undefined);
});

test("keyword lines may appear after screen references", () => {
  const m = model(`screen A "a"

flow "First"
  A
  role "Admin"
`);
  assert.equal(m.flows[0]!.role, "Admin");
  assert.deepEqual(m.flows[0]!.screens, ["A"]);
});

test("a duplicate role line is rejected with its line number", () => {
  const errs = validateWireframeSyntax(`screen A "a"

flow "First"
  role "Admin"
  role "Owner"
  A
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 5: /);
  assert.match(errs[0]!, /duplicate role/);
});

test("a duplicate description line is rejected with its line number", () => {
  const errs = validateWireframeSyntax(`screen A "a"

flow "First"
  description "one"
  description "two"
  A
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 5: /);
  assert.match(errs[0]!, /duplicate description/);
});

test("a mistyped keyword falls through to the unknown-flow-line error", () => {
  const errs = validateWireframeSyntax(`screen A "a"

flow "First"
  descripton "typo"
  A
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 4: /);
  assert.match(errs[0]!, /unknown flow line/);
});

test("a screen literally named role still resolves as a bare reference", () => {
  const m = model(`screen role "An unfortunate name"
screen A "a"

flow "First"
  role
  A
`);
  assert.deepEqual(m.flows[0]!.screens, ["role", "A"]);
  assert.equal(m.flows[0]!.role, undefined);
});

test("a named flow listing no screens is rejected with the header's line number", () => {
  const errs = validateWireframeSyntax(`screen A "a"

flow "Ghost"
  role "Nobody"
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 3: /);
  assert.match(errs[0]!, /lists no screens/);
});

test("a named flow holding only legacy edge lines is rejected as empty", () => {
  const errs = validateWireframeSyntax(`screen A "a"
screen B "b"

flow "Edges only"
  A -> B
`);
  assert.equal(errs.length, 1);
  assert.match(errs[0]!, /^line 4: /);
  assert.match(errs[0]!, /lists no screens/);
});

test("the tolerant compile keeps an empty flow rather than failing the model", () => {
  const m = model(`screen A "a"

flow "Ghost"
  role "Nobody"
`);
  assert.deepEqual(m.flows[0]!.screens, []);
});
