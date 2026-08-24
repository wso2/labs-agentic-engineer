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
import { PrdLenses, prdBlocks, refreshPrdLenses } from "./prdLensPlugin";
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
      }),
    ],
    content: markdownToNode(PRD).toJSON(),
  });
  return element;
}

const lensButtons = (el: HTMLElement) =>
  Array.from(el.querySelectorAll<HTMLButtonElement>("button.prd-lens"));

afterEach(() => {
  editor?.destroy();
  editor = null;
  busy.reason = "";
  document.body.innerHTML = "";
});

describe("the PRD's lens surface", () => {
  it("offers each command at the place it changes, in document order", () => {
    const el = mount("");
    expect(lensButtons(el).map((b) => b.textContent)).toEqual([
      "+ Actor",
      "+ Feature",
      "Go deeper",
      "Go deeper",
      "Settle",
      "Settle",
      "Settle",
      "Settle",
    ]);
  });

  it("carries the line the user clicked as the command's subject", () => {
    const doc = markdownToNode(PRD);
    expect(prdAffordances(prdBlocks(doc)).lenses.map((l) => l.command)).toEqual([
      "/actor",
      "/feature",
      "/expand As an Employee, I want to submit an expense with a receipt photo.",
      "/expand As a Manager, I want to approve or reject an expense.",
      "/settle Sign-in: the org Google Workspace assumed",
      "/settle",
      "/settle Which accounting system do we export to?",
      "/settle What is the approval limit? Deferred — the user will decide next quarter.",
    ]);
  });

  it("sends the command when a lens is clicked", () => {
    const run = vi.fn();
    const el = mount("", run);
    lensButtons(el)[1]!.click();
    expect(run).toHaveBeenCalledWith("/feature");
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

  it("goes inert, saying why, while the agent holds the turn", () => {
    const run = vi.fn();
    const el = mount("An agent is still working", run);
    const buttons = lensButtons(el);
    expect(buttons.every((b) => b.disabled)).toBe(true);
    expect(buttons[0]!.title).toBe("An agent is still working");

    buttons[0]!.click();
    expect(run).not.toHaveBeenCalled();
  });

  // A widget decoration whose key matches an existing one short-circuits
  // ProseMirror's comparison and REUSES the DOM — the factory never runs
  // again. Whether a lens is clickable is the one thing about it that changes
  // without the document changing, so it has to survive that reuse.
  it("comes back to life when the agent's turn ends, and dies when one starts", () => {
    const run = vi.fn();
    const el = mount("An agent is still working", run);
    expect(lensButtons(el).every((b) => b.disabled)).toBe(true);

    busy.reason = "";
    refreshPrdLenses(editor!.view);
    const live = lensButtons(el);
    expect(live.every((b) => b.disabled)).toBe(false);
    expect(live[1]!.title).toBe("Add a feature to this PRD");
    live[1]!.click();
    expect(run).toHaveBeenCalledWith("/feature");

    busy.reason = "The agent is waiting on your answers";
    refreshPrdLenses(editor!.view);
    const inert = lensButtons(el);
    expect(inert.every((b) => b.disabled)).toBe(true);
    expect(inert[1]!.title).toBe("The agent is waiting on your answers");
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
    expect(lensButtons(el).length).toBe(before + 1);
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
    expect(lensButtons(el).map((b) => b.textContent)).toEqual(["Settle"]);
  });

  it("offers nothing on a document that is not a PRD", () => {
    const doc = markdownToNode("# Notes\n\nJust prose, and a list:\n\n- one\n- two\n");
    expect(prdAffordances(prdBlocks(doc))).toEqual({ lenses: [], flags: [] });
  });
});
