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
import { markdownToNode } from "@aep/collab-doc";
import { EditorState, TextSelection, type Transaction } from "@tiptap/pm/state";
import { assumedLinesPlugin, assumedTransaction } from "./assumedLines";
import { docLines } from "./docLines";

// The editor's side of settling an assumed line, on a bare EditorState: the
// plugin's appended transaction (leaving an edited line) runs as it does in
// the editor.

const MD = "## Decisions\n\n- A claim is approved by the line manager.\n- A rejected claim goes back to the employee. *assumed*\n";

function setup() {
  const plugin = assumedLinesPlugin();
  let state = EditorState.create({ doc: markdownToNode(MD), plugins: [plugin] });
  const run = (tr: Transaction | null) => {
    if (!tr) throw new Error("no transaction");
    state = state.applyTransaction(tr).state;
  };
  const line = (start: string) => docLines(state.doc).find((l) => l.text.startsWith(start))!;
  return {
    run,
    line,
    act: (action: "keep" | "remove" | "edit") => run(assumedTransaction(state, line("A rejected").from + 1, action)),
    texts: () => docLines(state.doc).map((l) => l.text),
    byYou: () => plugin.getState(state)!.byYou.length,
    state: () => state,
  };
}

describe("acting on an assumed line", () => {
  it("Keep drops the tag and marks the line by you", () => {
    const t = setup();
    t.act("keep");
    expect(t.texts()).toContain("A rejected claim goes back to the employee.");
    expect(t.byYou()).toBe(1);
  });

  it("Remove deletes the line", () => {
    const t = setup();
    t.act("remove");
    expect(t.texts()).toEqual(["Decisions", "A claim is approved by the line manager."]);
  });

  it("I'll edit puts the caret after the words, before the tag", () => {
    const t = setup();
    t.act("edit");
    const line = t.line("A rejected");
    expect(t.state().selection.from).toBe(line.posAt("A rejected claim goes back to the employee.".length));
  });

  it("leaving the edited line changed drops the tag", () => {
    const t = setup();
    t.act("edit");
    t.run(t.state().tr.insertText(" Always."));
    t.run(t.state().tr.setSelection(TextSelection.create(t.state().doc, t.line("A claim").from + 1)));
    expect(t.texts()).toContain("A rejected claim goes back to the employee. Always.");
    expect(t.byYou()).toBe(1);
  });

  it("leaving it unchanged keeps it assumed", () => {
    const t = setup();
    t.act("edit");
    t.run(t.state().tr.setSelection(TextSelection.create(t.state().doc, t.line("A claim").from + 1)));
    expect(t.texts()).toContain("A rejected claim goes back to the employee. assumed");
    expect(t.byYou()).toBe(0);
  });
});
