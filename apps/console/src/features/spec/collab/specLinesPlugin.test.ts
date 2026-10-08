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
import { yXmlFragmentToProseMirrorRootNode } from "y-prosemirror";
import { markdownToNode, setDocFile, setDocFileAsAgent } from "@aep/collab-doc";
import { parseLine } from "../model/ids";
import { blockingEntries } from "../model/questions";
import { docLines, type DocLine } from "./docLines";
import { specSchema } from "./specSchema";
import { FRESH_MS, lineMarks, type LineMark } from "./specLinesPlugin";

// Decoration ranges, on a document parsed by the same pipeline the room uses:
// what each range covers is checked by the text it spans.

function marksOf(markdown: string) {
  const doc = markdownToNode(markdown);
  const spanned = (m: LineMark) => ("at" in m ? "" : doc.textBetween(m.from, m.to, "\n"));
  const lines = docLines(doc);
  const blocking = new Set(blockingEntries(lines).map((e) => e.line));
  return lines.flatMap((line) =>
    lineMarks(line, parseLine(line.text, line.emphasis), blocking.has(line)).map((m) => ({ ...m, text: spanned(m) })),
  );
}

describe("lineMarks", () => {
  it("draws a story's ID, its sources, and its references over exactly their text", () => {
    const marks = marksOf("- F2.5 As finance, I approve after F2.1. [T&E policy p.7]\n");
    expect(marks.map((m) => [m.kind, m.text])).toEqual([
      ["line", "F2.5 As finance, I approve after F2.1. [T&E policy p.7]"],
      ["sid", "F2.5"],
      ["source", "[T&E policy p.7]"],
      ["ref", "F2.1"],
    ]);
    expect(marks[0]).toMatchObject({ kind: "line", lineId: "F2.5", assumed: false });
  });

  it("highlights an assumed line, marks the tag itself, and offers its actions after it", () => {
    const marks = marksOf("- A rejected claim goes back to the employee. *assumed*\n");
    expect(marks.map((m) => [m.kind, m.text])).toEqual([
      ["line", "A rejected claim goes back to the employee. assumed"],
      ["assumed", "assumed"],
      ["actions", ""],
    ]);
    expect(marks[0]).toMatchObject({ lineId: null, assumed: true });
    expect(marks[2]).toMatchObject({ body: "A rejected claim goes back to the employee." });
  });

  it("highlights a blocking question with its options, and marks the tag itself", () => {
    const marks = marksOf("## Open Questions\n\n1. One Xero organisation? *blocking*\n   - One for every claim.\n   - One per country.\n");
    expect(marks.map((m) => [m.kind, m.text])).toEqual([
      ["line", "One Xero organisation? blocking\nOne for every claim.\nOne per country."],
      ["blocking", "blocking"],
    ]);
    expect(marks[0]).toMatchObject({ blocking: true, assumed: false });
  });

  it("draws a blocking tag outside Open Questions as nothing but emphasis", () => {
    expect(marksOf("## Decisions\n\n- Claims go to Xero. *blocking*\n")).toEqual([]);
  });

  it("washes what the agent has just written, and nothing once it is older than FRESH_MS", () => {
    const doc = new Y.Doc();
    setDocFile(doc, "f.md", "- F2.5 As finance, I approve.\n");
    const at = "2026-09-30T12:00:00.000Z";
    setDocFileAsAgent(doc, "f.md", "- F2.5 As finance, I approve.\n- F2.6 As an auditor, I read it. *assumed*\n", "test", {
      agent: "Spec Agent",
      at,
    });
    const pm = yXmlFragmentToProseMirrorRootNode(doc.getXmlFragment("f.md"), specSchema);
    const [settled, written] = docLines(pm);
    const writtenAt = Date.parse(at);
    const fresh = (line: DocLine, now: number) =>
      lineMarks(line, parseLine(line.text, line.emphasis), false, now).filter((m) => m.kind === "fresh");
    expect(fresh(settled!, writtenAt + 1000)).toEqual([]);
    expect(fresh(written!, writtenAt + 1000)).toEqual([{ kind: "fresh", from: written!.posAt(0), to: written!.posAt(written!.text.length) }]);
    expect(fresh(written!, writtenAt + FRESH_MS)).toEqual([]);
    // Written, not proposed: an assumed line the agent wrote offers its actions at once.
    expect(lineMarks(written!, parseLine(written!.text, written!.emphasis)).map((m) => m.kind)).toContain("actions");
  });

  it("draws a Needs or Applies to clause as a quiet tag", () => {
    const doc = markdownToNode("- P1 Every edit is logged. Applies to: all.\n");
    const [line] = docLines(doc);
    const marks = lineMarks(line!, parseLine(line!.text, line!.emphasis));
    const clause = marks.find((m) => m.kind === "clause");
    expect(clause).toBeDefined();
    expect(doc.textBetween((clause as { from: number }).from, (clause as { to: number }).to)).toBe("Applies to: all.");
  });

  it("covers the whole list item for the line, and marks a moved line's old ID", () => {
    const doc = markdownToNode("- F5.1 (was F2.3) As a manager, I see the route.\n");
    const [line] = docLines(doc);
    const item = doc.firstChild!.firstChild!;
    expect(item.type.name).toBe("listItem");
    const marks = lineMarks(line!, parseLine(line!.text, line!.emphasis));
    expect(marks[0]).toMatchObject({ kind: "line", from: line!.lineFrom, to: line!.lineFrom + item.nodeSize });
    const was = marks.find((m) => m.kind === "was");
    expect(was && "from" in was && doc.textBetween(was.from, was.to, "\n")).toBe("(was F2.3)");
  });

  it("maps offsets past bold and links to the right positions", () => {
    const marks = marksOf("Rules such as **the audit log** ([P1](product-wide.md)) and P4 apply.\n");
    expect(marks.filter((m) => m.kind === "ref").map((m) => m.text)).toEqual(["P1", "P4"]);
  });
});
