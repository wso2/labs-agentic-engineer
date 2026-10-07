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
import { buildIdIndex, parseLine, resolveId, splitIds, type LineBlock } from "./ids";

const text = (s: string, span: string) => {
  const start = s.indexOf(span);
  return { start, end: start + span.length };
};

describe("parseLine", () => {
  it("reads a story's own ID at the start and leaves the words", () => {
    const parts = parseLine("F2.1 As a manager, I see my team's pending claims.");
    expect(parts.lead).toEqual({ id: "F2.1", start: 0, end: 4 });
    expect(parts.body).toBe("As a manager, I see my team's pending claims.");
    expect(parts.refs).toEqual([]);
  });

  it("reads a product-wide item's ID as the line's own", () => {
    expect(parseLine("P4 Staff sign in with company SSO.").lead?.id).toBe("P4");
  });

  it("treats a feature ID at the start as a reference: its home is its own file", () => {
    const parts = parseLine("F1 Submit expenses");
    expect(parts.lead).toBeNull();
    expect(parts.refs).toEqual([{ id: "F1", start: 0, end: 2 }]);
  });

  it("reads the ID a moved line replaces", () => {
    const line = "F5.1 (was F2.3) As a manager, I see the route.";
    const parts = parseLine(line);
    expect(parts.lead?.id).toBe("F5.1");
    expect(parts.was).toEqual({ id: "F2.3", ...text(line, "(was F2.3)") });
    expect(parts.refs).toEqual([]);
    expect(parts.body).toBe("As a manager, I see the route.");
  });

  it("finds every other ID as a reference, sentence punctuation aside", () => {
    const line = "- Amounts as P3 describes; claims go to F2, then F2.5.";
    expect(parseLine(line).refs.map((r) => [r.id, line.slice(r.start, r.end)])).toEqual([
      ["P3", "P3"],
      ["F2", "F2"],
      ["F2.5", "F2.5"],
    ]);
  });

  it("reads bracketed sources and keeps IDs inside them from being links", () => {
    const line = "A receipt is required above $25. [T&E policy p.4] [per F1]";
    const parts = parseLine(line);
    expect(parts.sources).toEqual([text(line, "[T&E policy p.4]"), text(line, "[per F1]")]);
    expect(parts.refs).toEqual([]);
    expect(parts.body).toBe("A receipt is required above $25.");
  });

  it("reads the italic assumed tag closing the line, and only there", () => {
    const line = "A rejected claim goes back to the employee. assumed";
    const tag = text(line, "assumed");
    expect(parseLine(line, [tag]).assumed).toEqual(tag);
    expect(parseLine(line, [tag]).body).toBe("A rejected claim goes back to the employee.");

    // Not italic: a word, not the tag.
    expect(parseLine(line).assumed).toBeNull();
    // Italic but mid-line: emphasis, not the tag.
    const mid = "What is assumed here stays.";
    expect(parseLine(mid, [text(mid, "assumed")]).assumed).toBeNull();
  });

  it("reads the italic blocking tag closing a question, and leaves the question's words", () => {
    const line = "Does finance post to one Xero organisation? blocking";
    const tag = text(line, "blocking");
    expect(parseLine(line, [tag])).toMatchObject({ blocking: tag, assumed: null, body: "Does finance post to one Xero organisation?" });
    expect(parseLine(line).blocking).toBeNull();
  });
});

const line = (s: string, emphasis: LineBlock["emphasis"] = []): LineBlock => ({ kind: "listItem", text: s, emphasis });

describe("buildIdIndex and resolveId", () => {
  const index = buildIdIndex(
    [{ id: "F2", name: "Approvals", purpose: "Managers approve claims." }],
    [
      { fileKey: "F2", lines: [line("F2.5 As finance, I give a second approval. [T&E policy p.7]")] },
      { fileKey: "F5", lines: [line("F5.1 (was F2.3) As a manager, I see the route.")] },
      { fileKey: "product-wide", lines: [line("P4 Staff sign in with company SSO.")] },
    ],
  );

  it("indexes each line by its ID with its words, and each feature by its name and purpose", () => {
    expect(resolveId(index, "F2.5")).toEqual({
      entry: { id: "F2.5", fileKey: "F2", text: "As finance, I give a second approval." },
      retiredFrom: null,
    });
    expect(resolveId(index, "P4")?.entry.fileKey).toBe("product-wide");
    expect(resolveId(index, "F2")?.entry).toEqual({
      id: "F2",
      fileKey: "F2",
      title: "Approvals",
      text: "Managers approve claims.",
    });
  });

  it("sends a retired ID to the line that replaced it", () => {
    expect(resolveId(index, "F2.3")).toEqual({
      entry: { id: "F5.1", fileKey: "F5", text: "As a manager, I see the route." },
      retiredFrom: "F2.3",
    });
  });

  it("knows nothing of an ID the spec does not have", () => {
    expect(resolveId(index, "F9.9")).toBeNull();
  });

  it("keeps the first home when an ID is written twice", () => {
    const twice = buildIdIndex([], [
      { fileKey: "F1", lines: [line("F1.1 First.")] },
      { fileKey: "F2", lines: [line("F1.1 Copy.")] },
    ]);
    expect(resolveId(twice, "F1.1")?.entry.fileKey).toBe("F1");
  });
});

describe("splitIds", () => {
  it("splits text into plain runs and IDs", () => {
    expect(splitIds("F2 Approvals, see F2.5")).toEqual([{ id: "F2" }, { text: " Approvals, see " }, { id: "F2.5" }]);
  });
});

describe("the clauses a line carries (skills/prd-contract)", () => {
  it("reads a story's Needs and leaves them out of its words, keeping the IDs as links", () => {
    const s = "F1.4 As an employee, I see my claims' status. [T&E Policy v3 · p.5] Needs: F2, F3.";
    const parts = parseLine(s);
    expect(parts.needs).toEqual(["F2", "F3"]);
    expect(parts.appliesTo).toBeNull();
    expect(parts.body).toBe("As an employee, I see my claims' status.");
    expect(parts.refs.map((r) => r.id)).toEqual(["F2", "F3"]);
  });

  it("reads a product-wide item's reach, before or after its source", () => {
    expect(parseLine("P1 Every edit is logged. Applies to: all.").appliesTo).toBe("all");
    const late = parseLine("P3 Amounts in cents. Applies to: F1, F3. [org default]");
    expect(late.appliesTo).toEqual(["F1", "F3"]);
    expect(late.body).toBe("Amounts in cents.");
  });
});

describe("a Retired section", () => {
  const heading = (s: string): LineBlock => ({ kind: "heading", level: 2, text: s, emphasis: [] });
  const index = buildIdIndex([], [
    {
      fileKey: "F2",
      lines: [heading("User Stories"), line("F2.1 As a manager, I approve."), heading("Retired"), line("F2.3 moved to F5.1"), line("F2.6 dropped")],
    },
    { fileKey: "F5", lines: [heading("User Stories"), line("F5.1 As a manager, I see the route.")] },
  ]);

  it("is never a live line, and a move sends the old ID to its replacement", () => {
    expect(resolveId(index, "F2.1")?.entry.fileKey).toBe("F2");
    expect(resolveId(index, "F2.3")).toMatchObject({ entry: { id: "F5.1" }, retiredFrom: "F2.3" });
    expect(resolveId(index, "F2.6")).toBeNull();
  });
});
