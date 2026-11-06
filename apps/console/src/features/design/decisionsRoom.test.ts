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
import * as Y from "yjs";
import { readDocFile, setDocFile, setDocFileAsAgent } from "@aep/collab-doc";
import { keepDecision, keptMarkdown, readDecisionLines } from "./decisionsRoom";
import { assumedCount, decisionTag, DECISIONS_PATH, keptText } from "./model/decisions";

// The decisions file as the design skill writes it, through a Y.Doc as the
// room holds it, read back as the committer would write it.

const DECISIONS = `# Technical decisions

- [data-model] Amounts are stored in cents, never as decimals. *assumed* (F1, F2)
- [architecture] One service, expense-api, holds claims and approvals. (F1, F2)
- [contract] Pending approvals are listed oldest first. *assumed* (F2)
- [security] Only a manager reads the team's claims. *assumed*
`;

const CENTS = "[data-model] Amounts are stored in cents, never as decimals. assumed (F1, F2)";

function docWith(markdown: string): Y.Doc {
  const doc = new Y.Doc();
  setDocFile(doc, DECISIONS_PATH, markdown);
  return doc;
}

describe("the decisions' lines", () => {
  it("finds the tag before the features, and at the end of a line", () => {
    const lines = readDecisionLines(docWith(DECISIONS));
    expect(lines.map((l) => decisionTag(l) !== null)).toEqual([false, true, false, true, true]);
    expect(assumedCount(lines)).toBe(3);
  });

  it("reads a word in italics that is not the closing tag as words", () => {
    const lines = readDecisionLines(docWith("- [data-model] Store what is *assumed* by the client. (F1)\n"));
    expect(assumedCount(lines)).toBe(0);
  });

  it("is empty before Design writes the file", () => {
    expect(readDecisionLines(new Y.Doc())).toEqual([]);
  });

  it("reads a decision without its tag, as Change… quotes it", () => {
    const line = readDecisionLines(docWith(DECISIONS))[1]!;
    expect(line.text).toBe(CENTS);
    expect(keptText(line)).toBe("[data-model] Amounts are stored in cents, never as decimals. (F1, F2)");
  });
});

describe("keepDecision", () => {
  it("drops the tag and the space before it, and keeps the words", () => {
    const doc = docWith(DECISIONS);
    expect(keepDecision(doc, CENTS)).toBe(true);
    const after = readDocFile(doc, DECISIONS_PATH)!;
    expect(after).toContain("Amounts are stored in cents, never as decimals. (F1, F2)");
    expect(assumedCount(readDecisionLines(doc))).toBe(2);
  });

  it("does nothing when the decision was kept or rewritten meanwhile", () => {
    const doc = docWith(DECISIONS);
    expect(keepDecision(doc, CENTS)).toBe(true);
    expect(keepDecision(doc, CENTS)).toBe(false);
    expect(keepDecision(doc, "[data-model] Amounts are floats. assumed (F1)")).toBe(false);
  });

  it("keeps a run's concurrent write to another line of the file", () => {
    const doc = docWith(DECISIONS);
    const agent = new Y.Doc();
    Y.applyUpdate(agent, Y.encodeStateAsUpdate(doc));
    const written = readDocFile(agent, DECISIONS_PATH)!.replace("oldest first", "newest first");
    setDocFileAsAgent(agent, DECISIONS_PATH, written, "agent", { agent: "design", at: "2026-11-01T00:00:00Z" });
    expect(keepDecision(doc, CENTS)).toBe(true);
    Y.applyUpdate(doc, Y.encodeStateAsUpdate(agent));
    const merged = readDocFile(doc, DECISIONS_PATH)!;
    expect(merged).toContain("never as decimals. (F1, F2)");
    expect(merged).toContain("newest first. *assumed* (F2)");
  });
});

describe("keptMarkdown, what the Checked hash is taken of", () => {
  /** The file's hash input before and after one Keep of each assumed decision, in turn. */
  function eachKeep(markdown: string): { before: string; after: string[] } {
    const doc = docWith(markdown);
    const before = keptMarkdown(readDocFile(doc, DECISIONS_PATH)!);
    const after = readDecisionLines(doc)
      .filter((l) => decisionTag(l) !== null)
      .map((l) => {
        expect(keepDecision(doc, l.text)).toBe(true);
        return keptMarkdown(readDocFile(doc, DECISIONS_PATH)!);
      });
    expect(readDocFile(doc, DECISIONS_PATH)).not.toMatch(/assumed\* \(F/);
    return { before, after };
  }

  it("does not change with a Keep", () => {
    const { before, after } = eachKeep(DECISIONS);
    expect(after).toEqual([before, before, before]);
  });

  it("does not change with a Keep on a decision that wraps onto a second line", () => {
    const wrapped = "# Technical decisions\n\n- [data-model] Amounts are stored\n  in cents. *assumed* (F1)\n- [contract] Oldest  \n  first. *assumed* (F2)\n";
    const { before, after } = eachKeep(wrapped);
    expect(after).toEqual([before, before]);
  });

  it("keeps a tag that closes no decision as the file's words", () => {
    const markdown = "# Technical decisions\n\nA note. *assumed*\n\n```\n- a sample *assumed*\n~~~\n- still code *assumed*\n```\n\n- [data-model] Amounts in cents. *assumed* (F1)\n";
    const kept = keptMarkdown(markdown);
    expect(kept).toContain("A note. *assumed*");
    expect(kept).toContain("- a sample *assumed*");
    expect(kept).toContain("- still code *assumed*");
    expect(kept).toContain("Amounts in cents. (F1)");
  });
});
