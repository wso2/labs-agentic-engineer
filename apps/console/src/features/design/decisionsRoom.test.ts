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
import { keepDecision, readDecisionLines } from "./decisionsRoom";
import { assumedCount, decisionTag, keptText, withoutAssumedTags } from "./model/decisions";
import { DECISIONS_PATH } from "./model/designFiles";

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

  it("leaves the file's Checked hash as it was", () => {
    const doc = docWith(DECISIONS);
    const before = withoutAssumedTags(readDocFile(doc, DECISIONS_PATH)!);
    for (const line of readDecisionLines(doc).filter((l) => decisionTag(l) !== null)) keepDecision(doc, line.text);
    const after = readDocFile(doc, DECISIONS_PATH)!;
    expect(after).not.toContain("assumed");
    expect(withoutAssumedTags(after)).toBe(before);
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
