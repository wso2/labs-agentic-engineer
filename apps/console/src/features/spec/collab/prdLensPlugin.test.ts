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

// @vitest-environment jsdom

// The lens surface end to end, over a real PRD: markdown parsed by the SAME
// converter that seeds the collaborative document (`markdownToNode`), walked,
// located, and rendered as the controls the user clicks.
//
// jsdom carries no CSS, so the hover reveal of a line lens is not observable
// here — that is presentation. What is asserted is what the document offers,
// where, and what firing one sends.

import { afterEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { AgentInsertion, markdownToNode } from "@aep/collab-doc";
import { PrdLenses, refreshPrdLenses } from "./prdLensPlugin";
import { docBlocks } from "./docBlocks";
import { prdAffordances } from "../lib/prdLenses";

const PRD = `# Expenses — PRD

## Problem Statement

Employees file expenses on paper, and nobody can tell where one is.

## Actors

- Employee — files expenses and tracks their status
- Manager — approves what their reports file

## User Stories

1. As an Employee, I want to submit an expense with a receipt photo.
2. As a Manager, I want to approve or reject an expense.

## Product Decisions

- Sign-in: the org Google Workspace *assumed*
- Notifications go out by email

## Out of Scope

- Multi-currency expenses

## Open Questions

1. Which accounting system do we export to?
2. What is the approval limit? Deferred — the user will decide next quarter.
`;

let editor: Editor | null = null;

/** The console's binding, mutable so a test can flip it mid-run. */
const busy = { reason: "" };

/** Every block a Discuss lens handed to the aim surface, as [from, to]. */
let discussed: Array<[number, number]> = [];

/** Every prompted command the add-lenses asked the box to collect. */
let composed: Array<{ command: string; cta: string }> = [];

/** Mount the PRD in an editor carrying the lens extension. */
function mount(busyReason: string, run: (c: string) => void = () => {}): HTMLElement {
  busy.reason = busyReason;
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: [
      StarterKit.configure({ undoRedo: false }),
      Markdown,
      // In the schema for the same reason the editor carries it: an agent's
      // streamed write arrives marked.
      AgentInsertion,
      PrdLenses.configure({
        run,
        isBusy: () => busy.reason !== "",
        busyReason: () => busy.reason,
        discuss: (from, to) => discussed.push([from, to]),
        compose: (command, prompt) => composed.push({ command, cta: prompt.cta }),
      }),
    ],
    content: markdownToNode(PRD).toJSON(),
  });
  return element;
}

const lensButtons = (el: HTMLElement) =>
  Array.from(el.querySelectorAll<HTMLButtonElement>("button.prd-lens"));

/** The first lens reading `label` — for the PRD above, the one on its only flagged line. */
function byLabel(el: HTMLElement, label: string): HTMLButtonElement {
  const button = lensButtons(el).find((b) => b.textContent === label);
  if (!button) throw new Error(`no lens labelled ${label}`);
  return button;
}

afterEach(() => {
  editor?.destroy();
  editor = null;
  busy.reason = "";
  discussed = [];
  composed = [];
  document.body.innerHTML = "";
});

describe("the PRD's lens surface", () => {
  it("offers each command at the place it changes, in document order", () => {
    const el = mount("");
    expect(lensButtons(el).map((b) => b.textContent)).toEqual([
      "+ Actor",
      "Discuss",
      "Discuss",
      "+ Feature",
      "Go deeper",
      "Discuss",
      "Go deeper",
      "Discuss",
      "Agree",
      "Discuss",
      "Discuss",
      "Discuss",
      "Settle",
      "Settle",
      "Discuss",
      "Settle",
      "Discuss",
    ]);
  });

  it("carries the line the user clicked as the command's subject", () => {
    const doc = markdownToNode(PRD);
    const commands = prdAffordances(docBlocks(doc)).lenses.flatMap((l) =>
      l.kind === "command" ? [l.command] : [],
    );
    expect(commands).toEqual([
      "/actor",
      "/feature",
      "/expand As an Employee, I want to submit an expense with a receipt photo.",
      "/expand As a Manager, I want to approve or reject an expense.",
      "/settle",
      "/settle Which accounting system do we export to?",
      "/settle What is the approval limit? Deferred — the user will decide next quarter.",
    ]);
  });

  it("sends the command when a lens is clicked", () => {
    const run = vi.fn();
    const el = mount("", run);
    byLabel(el, "Settle").click();
    expect(run).toHaveBeenCalledWith("/settle");
  });

  // The add-lenses collect their subject first (#666): a bare `/actor` only
  // makes the agent ask what the lens could have asked on the spot. The click
  // opens the box; nothing is sent until the user answers it.
  it("opens the box to collect a subject for + Actor and + Feature, sending nothing", () => {
    const run = vi.fn();
    const el = mount("", run);
    byLabel(el, "+ Actor").click();
    byLabel(el, "+ Feature").click();
    expect(run).not.toHaveBeenCalled();
    expect(composed).toEqual([
      { command: "/actor", cta: "Add actor" },
      { command: "/feature", cta: "Add feature" },
    ]);
  });

  // Agree (#652) is a direct edit: no agent turn, one transaction, one undo.
  // What it does to the document is the whole contract, so it is asserted on
  // the markdown that results.
  describe("the verdict on an *assumed* run", () => {
    const decisions = () => {
      const md = editor!.getMarkdown();
      const start = md.indexOf("## Product Decisions");
      const end = md.indexOf("## Out of Scope");
      return md.slice(start, end).trim();
    };

    it("Agree strips the flag and the space that went with it, leaving the decision", () => {
      const run = vi.fn();
      const el = mount("", run);
      byLabel(el, "Agree").click();
      expect(decisions()).toBe(
        "## Product Decisions\n\n- Sign-in: the org Google Workspace\n- Notifications go out by email",
      );
      // The document is the signal; nothing was sent.
      expect(run).not.toHaveBeenCalled();
      // And with the flag gone, the line offers what any bullet does.
      expect(lensButtons(el).map((b) => b.textContent)).not.toContain("Agree");
    });

    // A widget whose key matches is REUSED, click handler and all, so the lens
    // it closed over carries positions from before the paragraph above grew.
    // The edit has to act on where the line is NOW.
    it("acts on the line where it is now, after an agent wrote above it", () => {
      const el = mount("");
      const button = byLabel(el, "Agree");
      editor!.commands.setContent(
        markdownToNode(PRD.replace("## Product Decisions\n", "## Product Decisions\n\nA new paragraph the agent streamed in first.\n")).toJSON(),
      );
      // The same DOM survived the rebuild — that is the trap.
      expect(byLabel(el, "Agree")).toBe(button);
      button.click();
      expect(decisions()).toContain("- Sign-in: the org Google Workspace\n");
      expect(decisions()).toContain("A new paragraph the agent streamed in first.");
    });
  });

  // Two bullets can read identically — an agent writes formulaic decisions —
  // and text was the only identity a clicked lens resolved by, so Agree on the
  // second stripped the FIRST's flag. The Nth duplicate stays the Nth.
  it("acts on the second of two identical bullets, not the first", () => {
    const el = mount("");
    editor!.commands.setContent(
      markdownToNode(
        "# Lunch — PRD\n\n## Product Decisions\n\n- Same words here *assumed*\n- Same words here *assumed*\n",
      ).toJSON(),
    );
    const agrees = lensButtons(el).filter((b) => b.textContent === "Agree");
    expect(agrees).toHaveLength(2);
    agrees[1]!.click();
    expect(editor!.getMarkdown().trim()).toBe(
      "# Lunch — PRD\n\n## Product Decisions\n\n- Same words here *assumed*\n- Same words here",
    );

    const discusses = lensButtons(el).filter((b) => b.textContent === "Discuss");
    discusses[1]!.click();
    const [from, to] = discussed.at(-1)!;
    // The block handed to the aim surface is the SECOND one.
    const second = docBlocks(editor!.state.doc).filter((b) => b.text.startsWith("Same words"))[1]!;
    expect([from, to]).toEqual([second.from, second.to]);
  });

  it("Discuss opens the aim surface on the block, without sending anything", () => {
    const run = vi.fn();
    const el = mount("", run);
    byLabel(el, "Discuss").click();
    expect(run).not.toHaveBeenCalled();
    const doc = editor!.state.doc;
    const [from, to] = discussed.at(-1)!;
    // The block handed over is the one the lens sat on — its text is the proof.
    expect(doc.textBetween(from, to)).toBe("Employee — files expenses and tracks their status");
  });

  it("marks the two kinds of unsettled apart, and a deferral apart from both", () => {
    const el = mount("");
    // The entry's own text, not the lens widget the decoration also wraps.
    const flagged = (kind: string) =>
      el.querySelector(`p.prd-flag--${kind}`)?.firstChild?.textContent;
    // An assumption is one word of an otherwise-settled decision…
    expect(el.querySelector("span.prd-flag--assumed")?.textContent).toBe("assumed");
    // …an open question is the whole entry, and a deferred one reads apart again.
    expect(flagged("question")).toBe("Which accounting system do we export to?");
    expect(flagged("deferred")).toBe(
      "What is the approval limit? Deferred — the user will decide next quarter.",
    );
  });

  it("goes inert, saying why, while the agent holds the turn — except Agree", () => {
    const run = vi.fn();
    const el = mount("An agent is still working", run);
    const buttons = lensButtons(el);
    const edits = buttons.filter((b) => b.textContent === "Agree");
    const rest = buttons.filter((b) => !edits.includes(b));
    expect(rest.every((b) => b.disabled)).toBe(true);
    expect(rest[0]!.title).toBe("An agent is still working");
    // A verdict needs no agent, so it never waits for one: the reviewer
    // reading flagged lines while the agent works is exactly who it is for.
    expect(edits).toHaveLength(1);
    expect(edits.every((b) => !b.disabled)).toBe(true);
    expect(edits[0]!.title).toBe("Keep this decision — drop the assumed flag");

    rest[0]!.click();
    expect(run).not.toHaveBeenCalled();
  });

  // A widget decoration whose key matches an existing one short-circuits
  // ProseMirror's comparison and REUSES the DOM — the factory never runs
  // again. Whether a lens is clickable is the one thing about it that changes
  // without the document changing, so it has to survive that reuse.
  it("comes back to life when the agent's turn ends, and dies when one starts", () => {
    const run = vi.fn();
    const el = mount("An agent is still working", run);
    expect(lensButtons(el).filter((b) => b.textContent === "+ Feature").every((b) => b.disabled)).toBe(true);

    busy.reason = "";
    refreshPrdLenses(editor!.view);
    const live = lensButtons(el);
    expect(live.every((b) => b.disabled)).toBe(false);
    expect(byLabel(el, "+ Feature").title).toBe("Add a feature to this PRD");
    byLabel(el, "Settle").click();
    expect(run).toHaveBeenCalledWith("/settle");

    busy.reason = "The agent is waiting on your answers";
    refreshPrdLenses(editor!.view);
    const inert = lensButtons(el).filter((b) => b.textContent !== "Agree");
    expect(inert.every((b) => b.disabled)).toBe(true);
    expect(byLabel(el, "+ Feature").title).toBe("The agent is waiting on your answers");
  });

  it("keeps a lens's DOM across a document change, so a hovered one cannot flicker", () => {
    const el = mount("");
    const before = lensButtons(el)[0]!;
    editor!.commands.setContent(
      markdownToNode(PRD + "\n3. And one more nobody has answered.\n").toJSON(),
    );
    expect(lensButtons(el)[0]).toBe(before);
  });

  it("follows the section as the agent streams a new entry into it", () => {
    const el = mount("");
    const before = lensButtons(el).length;
    editor!.commands.setContent(
      markdownToNode(PRD + "\n3. And one more nobody has answered.\n").toJSON(),
    );
    // A new open question brings its Settle and its Discuss.
    expect(lensButtons(el).length).toBe(before + 2);
    expect(el.querySelectorAll("p.prd-flag--question").length).toBe(2);
  });

  it("reads an *assumed* run that a marked agent write split in two", () => {
    const el = mount("");
    const at = { agent: "Spec Agent", at: "2026-08-20T00:00:00.000Z" };
    editor!.commands.setContent({
      type: "doc",
      content: [
        { type: "heading", attrs: { level: 2 }, content: [{ type: "text", text: "Product Decisions" }] },
        {
          type: "bulletList",
          content: [
            {
              type: "listItem",
              content: [
                {
                  type: "paragraph",
                  content: [
                    { type: "text", text: "Sign-in: the org Google Workspace " },
                    // Half the word carries the agent's insertion mark, so
                    // ProseMirror cannot merge the two text nodes.
                    {
                      type: "text",
                      marks: [{ type: "italic" }, { type: "agentInsertion", attrs: at }],
                      text: "assu",
                    },
                    { type: "text", marks: [{ type: "italic" }], text: "med" },
                  ],
                },
              ],
            },
          ],
        },
      ],
    });

    // The decoration covers the whole run; ProseMirror still paints it as one
    // span per distinct mark set, which is why this reads them together.
    const flagged = Array.from(el.querySelectorAll("span.prd-flag--assumed"))
      .map((n) => n.textContent)
      .join("");
    expect(flagged).toBe("assumed");
    expect(lensButtons(el).map((b) => b.textContent)).toEqual(["Agree", "Discuss"]);
  });

  it("offers no command on a document that is not a PRD, only a Discuss per bullet", () => {
    const doc = markdownToNode("# Notes\n\nJust prose, and a list:\n\n- one\n- two\n");
    const { lenses, flags } = prdAffordances(docBlocks(doc));
    expect(flags).toEqual([]);
    expect(lenses.map((l) => l.kind)).toEqual(["discuss", "discuss"]);
  });
});
