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
import { readDocFile, setDocFile } from "@aep/collab-doc";
import { acmeExpensesSpec } from "../../../mocks/fixtures/spec";
import { buildOffer } from "../../builds/model/picker";
import { specLeg } from "../../projects/model/track";
import { deriveWorkspace } from "../model/workspace";
import { docLines, type DocLine } from "./docLines";
import { applyAgentToolCall } from "./agentWrites";
import { seedSpecDoc } from "./specDoc";
import {
  answerBlockingQuestion,
  appendToSection,
  deleteLine,
  editFile,
  removeAssumedTag,
  settleAssumedLine,
} from "./specEdits";
import { FRESH_MS } from "./specLinesPlugin";
import { readSpecLines } from "./useSpecLines";

// Each edit runs through the Y.Doc (editFile) and is read back as the
// markdown the committer would write.

const PATH = "specs/requirements/features/F2-approvals.md";

function docWith(markdown: string): Y.Doc {
  const doc = new Y.Doc();
  setDocFile(doc, PATH, markdown);
  return doc;
}

type Tr = Parameters<Parameters<typeof editFile>[2]>[0];

/** Type at a position as the editor does: the text takes the marks there. */
function type(tr: Tr, pos: number, text: string) {
  tr.insert(pos, tr.doc.type.schema.text(text, tr.doc.resolve(pos).marks()));
}

/** Run an edit on the line whose text starts with `start`. */
function onLine(doc: Y.Doc, start: string, edit: (tr: Tr, line: DocLine) => void) {
  return editFile(doc, PATH, (tr) => {
    const line = docLines(tr.doc).find((l) => l.text.startsWith(start));
    if (!line) throw new Error(`no line starting "${start}"`);
    edit(tr, line);
  });
}

describe("settling an assumed line", () => {
  const md = "## Decisions\n\n- A claim is approved by the line manager.\n- A rejected claim goes back to the employee. *assumed*\n";

  it("keeps its words and drops the tag", () => {
    const doc = docWith(md);
    onLine(doc, "A rejected", (tr, line) => expect(removeAssumedTag(tr, line)).toBe(true));
    expect(readDocFile(doc, PATH)).toBe(
      "## Decisions\n\n- A claim is approved by the line manager.\n- A rejected claim goes back to the employee.",
    );
  });

  it("does nothing to a line with no tag", () => {
    const doc = docWith(md);
    expect(onLine(doc, "A claim", (tr, line) => expect(removeAssumedTag(tr, line)).toBe(false))).toBe(false);
  });

  it("removes the line, and the list with its last line", () => {
    const doc = docWith(md);
    onLine(doc, "A rejected", (tr, line) => deleteLine(tr, line));
    expect(readDocFile(doc, PATH)).toBe("## Decisions\n\n- A claim is approved by the line manager.");
    onLine(doc, "A claim", (tr, line) => deleteLine(tr, line));
    expect(readDocFile(doc, PATH)).toBe("## Decisions");
  });
});

describe("appendToSection", () => {
  it("adds a settled line at the end of the section's list", () => {
    const doc = docWith("## Decisions\n\n- One.\n\n## Out of Scope\n\n- Two.\n");
    editFile(doc, PATH, (tr) => appendToSection(tr, "Decisions", "Finance posts to one Xero organisation."));
    expect(readDocFile(doc, PATH)).toBe(
      "## Decisions\n\n- One.\n- Finance posts to one Xero organisation.\n\n## Out of Scope\n\n- Two.",
    );
  });

  it("starts the list, or the section, when there is none", () => {
    const doc = docWith("## Decisions\n\n## Out of Scope\n\n- Two.\n");
    editFile(doc, PATH, (tr) => appendToSection(tr, "Decisions", "One."));
    expect(readDocFile(doc, PATH)).toBe("## Decisions\n\n- One.\n\n## Out of Scope\n\n- Two.");
    const bare = docWith("# Payroll export\n");
    editFile(bare, PATH, (tr) => appendToSection(tr, "Decisions", "One."));
    expect(readDocFile(bare, PATH)).toBe("# Payroll export\n\n## Decisions\n\n- One.");
  });
});

describe("answering a blocking question", () => {
  const XERO = "Does finance post to one Xero organisation, or one per country?";
  const md = `## Decisions

- Each claim becomes one bill.

## Open Questions

1. ${XERO} *blocking*
   - Finance posts every claim to one Xero organisation.
   - Each country has its own Xero organisation.
2. Which currency do refunds use?
`;

  it("takes the entry and its options out of Open Questions and adds the answer to Decisions, in one transaction", () => {
    const doc = docWith(md);
    let transactions = 0;
    doc.on("afterTransaction", () => transactions++);
    expect(answerBlockingQuestion(doc, PATH, XERO, "Finance posts every claim to one Xero organisation.")).toBe(true);
    expect(transactions).toBe(1);
    expect(readDocFile(doc, PATH)).toBe(
      "## Decisions\n\n- Each claim becomes one bill.\n- Finance posts every claim to one Xero organisation.\n\n## Open Questions\n\n1. Which currency do refunds use?",
    );
  });

  it("takes the user's own words as the decision, and the list with its last entry", () => {
    const doc = docWith(`## Open Questions\n\n1. ${XERO} *blocking*\n`);
    answerBlockingQuestion(doc, PATH, XERO, "  One organisation per legal entity.  ");
    expect(readDocFile(doc, PATH)).toBe("## Open Questions\n\n## Decisions\n\n- One organisation per legal entity.");
  });

  it("writes nothing when the question is no longer in the file, or the answer is empty", () => {
    const doc = docWith(md);
    const before = readDocFile(doc, PATH);
    expect(answerBlockingQuestion(doc, PATH, "Some other question?", "Yes.")).toBe(false);
    expect(answerBlockingQuestion(doc, PATH, XERO, "  ")).toBe(false);
    expect(readDocFile(doc, PATH)).toBe(before);
  });
});

describe("Acme Expenses, acted on", () => {
  const f2 = acmeExpensesSpec.features.find((f) => f.id === "F2")!;
  const seeded = () => {
    const doc = new Y.Doc();
    seedSpecDoc(doc, acmeExpensesSpec);
    return doc;
  };
  const view = (doc: Y.Doc) => deriveWorkspace(acmeExpensesSpec, readSpecLines(doc));

  it("settling an assumed line is one fewer to confirm, in the chip and in Next up", () => {
    const doc = seeded();
    editFile(doc, f2.path, (tr) => {
      const line = docLines(tr.doc).find((l) => l.text.startsWith("A rejected claim"))!;
      removeAssumedTag(tr, line);
    });
    const after = view(doc);
    expect(after.features.find((f) => f.id === "F2")?.chips).toEqual([{ tone: "warning", label: "1 to confirm" }]);
    expect(after.nextUp.find((i) => i.kind === "confirm")?.label).toBe("Confirm 1 line in Approvals");
  });

  it("answering Payroll export's question unblocks it: chip, Next up, the track and the build picker", () => {
    const f3 = acmeExpensesSpec.features.find((f) => f.id === "F3")!;
    const offer = (doc: Y.Doc) =>
      buildOffer({
        features: view(doc).features,
        designedFrom: {},
        productWide: view(doc).productWide,
        lines: readSpecLines(doc),
        dependencies: [],
        comments: [],
        artifacts: [],
        builds: [],
      }).rows.find((r) => r.id === "F3");
    const doc = seeded();
    const before = view(doc);
    const question = before.features.find((f) => f.id === "F3")!.blocking[0]!;
    expect(before.features.find((f) => f.id === "F3")?.chips.map((c) => c.label)).toEqual(["blocked"]);
    expect(before.nextUp[0]).toMatchObject({ kind: "blocking", label: `Answer "${question.question}"` });
    expect(specLeg(before.features).summary).toBe("1 question to answer");
    expect(offer(doc)?.detail).toEqual(["blocked by a question"]);

    answerBlockingQuestion(doc, f3.path, question.question, question.options[0]!);
    const after = view(doc);
    expect(after.features.find((f) => f.id === "F3")?.blocking).toEqual([]);
    expect(after.features.find((f) => f.id === "F3")?.chips).toEqual([]);
    expect(after.nextUp.map((i) => i.kind)).not.toContain("blocking");
    expect(after.design.toDesign).toContain("F3");
    expect(specLeg(after.features).summary).toBe("2 lines to confirm");
    expect(offer(doc)?.detail).toEqual(["not designed yet"]);
    expect(readDocFile(doc, f3.path)).toContain(`- ${question.options[0]}`);
  });
});

describe("settling an assumed line from the chat", () => {
  const markdown = "# Approvals\n\n## Decisions\n\n- A rejected claim goes back to the employee. *assumed*\n- A claim is approved by the line manager.\n";
  const line = "A rejected claim goes back to the employee. assumed";

  it("keeps the words and drops the tag", () => {
    const doc = docWith(markdown);
    expect(settleAssumedLine(doc, PATH, line, "keep")).toBe(true);
    expect(readDocFile(doc, PATH)).toContain("- A rejected claim goes back to the employee.\n");
    expect(readDocFile(doc, PATH)).not.toContain("assumed");
  });

  it("removes the line", () => {
    const doc = docWith(markdown);
    expect(settleAssumedLine(doc, PATH, line, "remove")).toBe(true);
    expect(readDocFile(doc, PATH)).not.toContain("rejected claim");
    expect(readDocFile(doc, PATH)).toContain("line manager");
  });

  it("does nothing when the line was settled meanwhile", () => {
    const doc = docWith(markdown.replace(" *assumed*", ""));
    expect(settleAssumedLine(doc, PATH, line, "keep")).toBe(false);
  });
});

describe("the agent's file writes, applied to the local doc", () => {
  const stub = "# Approvals\n\n## Purpose\n\nManagers approve claims.\n";
  const edit = {
    type: "tool-result",
    toolName: "editFile",
    toolCallId: "w1",
    input: {
      path: PATH,
      // As the agent read it: the doc's markdown carries no trailing newline.
      oldString: "Managers approve claims.",
      newString: "Managers approve claims.\n\n## Decisions\n\n- Deputies approve on leave. *assumed*",
    },
    output: { ok: true, op: "edit", path: PATH },
  };

  it("applies an edit to the room path, as the agents service matched it", () => {
    const doc = docWith(stub);
    expect(applyAgentToolCall(doc, edit)).toBe(true);
    expect(readDocFile(doc, PATH)).toContain("- Deputies approve on leave. *assumed*");
  });

  it("marks what it wrote as the agent's, with when, so the words land with a fading wash", () => {
    const doc = new Y.Doc();
    seedSpecDoc(doc, acmeExpensesSpec);
    const f2 = acmeExpensesSpec.features.find((f) => f.id === "F2")!;
    const from = "- F2.2 As a manager, I approve or reject a claim with a reason.";
    const addressed = {
      type: "tool-result",
      toolName: "editFile",
      toolCallId: "c1",
      input: { path: f2.path, oldString: from, newString: from.replace(" with a reason.", " with a reason, after seeing its receipts.") },
    };
    expect(applyAgentToolCall(doc, addressed)).toBe(true);
    expect(readDocFile(doc, f2.path)).toContain("with a reason, after seeing its receipts.");
    onLine(doc, "F2.2", (_tr, line) => {
      expect(line.agentRuns.map((r) => line.text.slice(r.start, r.end)).join("")).toContain("after seeing its receipts");
      expect(Date.now() - Date.parse(line.agentRuns[0]!.by.at)).toBeLessThan(FRESH_MS);
    });
    // Only what it wrote: the rest of the file carries no mark.
    onLine(doc, "F2.1", (_tr, line) => expect(line.agentRuns).toEqual([]));
  });

  it("changes only what it wrote, so the user's typing at the same time survives", () => {
    const doc = docWith("# Approvals\n\n- Managers approve claims.\n- Finance pays.\n");
    const peer = new Y.Doc();
    Y.applyUpdate(peer, Y.encodeStateAsUpdate(doc));
    // The user types in one line while the agent's write to another lands.
    editFile(peer, PATH, (tr) => {
      const line = docLines(tr.doc).find((l) => l.text.startsWith("Finance"))!;
      type(tr, line.posAt(line.text.length - 1), " monthly");
    });
    const agentEdit = { ...edit, input: { path: PATH, oldString: "Managers approve", newString: "Line managers approve" } };
    expect(applyAgentToolCall(doc, agentEdit)).toBe(true);
    Y.applyUpdate(doc, Y.encodeStateAsUpdate(peer));
    expect(readDocFile(doc, PATH)).toBe("# Approvals\n\n- Line managers approve claims.\n- Finance pays monthly.");
  });

  it("ignores a tool that writes no file", () => {
    const doc = docWith(stub);
    expect(applyAgentToolCall(doc, { type: "tool-result", toolName: "ask_question", input: { question: "?" } })).toBe(false);
  });
});
