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
import { prdAffordances, type PrdBlock } from "./prdLenses";

// Blocks as the doc walker emits them. Positions are synthetic but ordered and
// non-overlapping, which is all the locator uses them for.
let cursor = 0;
function block(
  kind: PrdBlock["kind"],
  text: string,
  extra: Partial<PrdBlock> = {},
): PrdBlock {
  const from = cursor;
  const to = from + text.length + 2;
  cursor = to;
  return { kind, text, emphasis: [], from, to, contentEnd: to - 1, ...extra };
}

function heading(text: string): PrdBlock {
  return block("heading", text, { level: 2 });
}

/** An entry carrying an `*assumed*` run, positioned inside its own block. */
function assumed(kind: PrdBlock["kind"], text: string): PrdBlock {
  const b = block(kind, `${text} assumed`);
  return { ...b, emphasis: [{ text: "assumed", from: b.to - 9, to: b.to - 1 }] };
}

function fresh(build: () => PrdBlock[]): PrdBlock[] {
  cursor = 0;
  return build();
}

const commands = (blocks: PrdBlock[]) =>
  prdAffordances(blocks).lenses.map((l) => l.command);

describe("prdAffordances — the PRD's own launchers", () => {
  it("puts an add-lens on Actors and on the story list", () => {
    const blocks = fresh(() => [
      heading("Actors"),
      block("listItem", "Employee — files expenses"),
      heading("User Stories"),
      block("listItem", "As an Employee, I want to file an expense."),
    ]);
    const { lenses } = prdAffordances(blocks);
    const sections = lenses.filter((l) => l.placement === "section");
    expect(sections.map((l) => l.command)).toEqual(["/actor", "/feature"]);
    expect(sections[0]!.at).toBe(blocks[0]!.contentEnd);
    expect(sections[1]!.at).toBe(blocks[2]!.contentEnd);
  });

  it("gives every story its own /expand, carrying the story as the subject", () => {
    const blocks = fresh(() => [
      heading("User Stories"),
      block("listItem", "As an Employee, I want to file an expense."),
      block("listItem", "As a Manager, I want to approve one."),
    ]);
    const lines = prdAffordances(blocks).lenses.filter((l) => l.placement === "line");
    expect(lines.map((l) => l.command)).toEqual([
      "/expand As an Employee, I want to file an expense.",
      "/expand As a Manager, I want to approve one.",
    ]);
  });

  it("flags each open question and offers /settle over the point", () => {
    const blocks = fresh(() => [
      heading("Open Questions"),
      block("listItem", "Which Slack workspace?"),
      block("listItem", "Personal or team budget? Deferred — the user said later."),
    ]);
    const { lenses, flags } = prdAffordances(blocks);
    expect(flags).toEqual([
      { kind: "question", from: blocks[1]!.from, to: blocks[1]!.to },
      { kind: "deferred", from: blocks[2]!.from, to: blocks[2]!.to },
    ]);
    // A deferred question is settled-for-now, not closed: it keeps its lens.
    expect(lenses.filter((l) => l.placement === "line").map((l) => l.command)).toEqual([
      "/settle Which Slack workspace?",
      "/settle Personal or team budget? Deferred — the user said later.",
    ]);
  });

  it("puts a section lens on Open Questions only while it holds entries", () => {
    const populated = fresh(() => [heading("Open Questions"), block("listItem", "Which vendor?")]);
    expect(commands(populated)).toContain("/settle");

    const empty = fresh(() => [heading("Open Questions"), heading("Further Notes")]);
    expect(commands(empty)).toEqual([]);
  });

  it("flags an *assumed* run wherever it sits, and settles that point", () => {
    const blocks = fresh(() => [
      heading("Product Decisions"),
      assumed("listItem", "Sign-in via Google —"),
      block("listItem", "Notifications by email"),
    ]);
    const { lenses, flags } = prdAffordances(blocks);
    const run = blocks[1]!.emphasis[0]!;
    // The flag sits on the run, not the whole line — an assumption is one word
    // of a decision that is otherwise settled.
    expect(flags).toEqual([{ kind: "assumed", from: run.from, to: run.to }]);
    expect(lenses.map((l) => l.command)).toEqual([
      "/settle Sign-in via Google — assumed",
    ]);
  });

  it("reads emphasis that is not the assumed flag as ordinary prose", () => {
    const b = fresh(() => [heading("Product Decisions"), block("listItem", "Uses Stripe")])[1]!;
    const withEmphasis: PrdBlock = {
      ...b,
      emphasis: [{ text: "Stripe", from: b.from + 6, to: b.to - 1 }],
    };
    expect(prdAffordances([withEmphasis]).flags).toEqual([]);
  });

  it("collapses a wrapped subject onto one line", () => {
    const blocks = fresh(() => [
      heading("User Stories"),
      block("listItem", "As an Employee,\n  I want\tto file an expense."),
    ]);
    expect(commands(blocks)).toContain("/expand As an Employee, I want to file an expense.");
  });

  it("ignores list items that belong to no lens-bearing section", () => {
    const blocks = fresh(() => [
      heading("Out of Scope"),
      block("listItem", "Multi-currency"),
      block("paragraph", "Prose under no heading at all."),
    ]);
    expect(prdAffordances(blocks)).toEqual({ lenses: [], flags: [] });
  });

  it("stops a section at the next heading of any depth", () => {
    const blocks = fresh(() => [
      heading("User Stories"),
      block("listItem", "As an Employee, I want to file an expense."),
      block("heading", "Later", { level: 3 }),
      block("listItem", "Not a story."),
    ]);
    expect(commands(blocks)).toEqual([
      "/feature",
      "/expand As an Employee, I want to file an expense.",
    ]);
  });

  it("finds nothing in a document that has no PRD sections", () => {
    const blocks = fresh(() => [block("paragraph", "# Notes"), block("paragraph", "Nothing here.")]);
    expect(prdAffordances(blocks)).toEqual({ lenses: [], flags: [] });
  });
});
