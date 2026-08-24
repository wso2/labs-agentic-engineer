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

// A PRD's references to its feature docs, end to end: markdown parsed by the
// same converter that seeds the collaborative document, then clicked.

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Editor } from "@tiptap/core";
import StarterKit from "@tiptap/starter-kit";
import { Markdown } from "@tiptap/markdown";
import { markdownToNode } from "@aep/collab-doc";
import { SpecLinks, refreshSpecLinks, type SpecLinkBinding } from "./specLinkPlugin";

const PRD = "specs/requirements/prd.md";
const RECEIPTS = "specs/requirements/features/receipts.md";

const MARKDOWN = `# Expenses — PRD

## User Stories

1. As an Employee, I want to submit an expense — depth in [Receipt capture](features/receipts.md).
2. As a Manager, I want to approve one — depth in [Approvals](features/approvals.md).

## Further Notes

The rules live at [wso2.com](https://wso2.com/policy.md).
`;

let editor: Editor | null = null;
let binding: SpecLinkBinding;

function mount(knownPaths: string[], open = vi.fn()): HTMLElement {
  binding = { path: PRD, knownPaths, open };
  const element = document.createElement("div");
  document.body.appendChild(element);
  editor = new Editor({
    element,
    extensions: [
      StarterKit.configure({ undoRedo: false }),
      Markdown,
      SpecLinks.configure({ binding: () => binding }),
    ],
    content: markdownToNode(MARKDOWN).toJSON(),
  });
  return element;
}

/** The position ProseMirror reports for a click on the given text. */
function positionOf(text: string): number {
  const doc = editor!.state.doc;
  let at = -1;
  doc.descendants((node, pos) => {
    if (node.isText && node.text === text) at = pos + 1;
    return at === -1;
  });
  if (at === -1) throw new Error(`no text node "${text}"`);
  return at;
}

/**
 * Click the editor the way ProseMirror does: `someProp` walks the plugins in
 * their real order and hands the click to the FIRST one that claims it.
 *
 * The event carries the anchor as its target, which is the part that matters —
 * StarterKit's Link registers a `handleClick` too, and it bails out early on a
 * targetless event. A synthetic event with no target therefore never reaches
 * Link's handler, and a test built on one cannot see which plugin wins.
 */
function clickOn(text: string, button = 0): boolean {
  const anchor = Array.from(document.querySelectorAll("a")).find(
    (a) => a.textContent === text,
  );
  const event = new MouseEvent("click", { bubbles: true, button });
  Object.defineProperty(event, "target", {
    value: anchor ?? editor!.view.dom,
    configurable: true,
  });
  return Boolean(
    editor!.view.someProp("handleClick", (f) =>
      f(editor!.view, positionOf(text), event),
    ),
  );
}

const linked = (el: HTMLElement) =>
  Array.from(el.querySelectorAll(".spec-link")).map((n) => n.textContent);

beforeEach(() => {
  // Link's `openOnClick` reaches the browser through this; jsdom has no real
  // implementation, and every assertion below cares whether it was reached.
  vi.spyOn(window, "open").mockReturnValue(null);
});

afterEach(() => {
  editor?.destroy();
  editor = null;
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("references between spec documents", () => {
  it("marks only the references the project can actually open", () => {
    // `approvals.md` is named but not written yet, so it stays plain text.
    const el = mount([PRD, RECEIPTS]);
    expect(linked(el)).toEqual(["Receipt capture"]);
  });

  it("selects the referenced document instead of leaving the app", () => {
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    expect(clickOn("Receipt capture")).toBe(true);
    expect(open).toHaveBeenCalledWith(RECEIPTS);
  });

  // StarterKit bundles Link at priority 1000 with `openOnClick`, and it
  // registers a `handleClick` of its own. Whichever plugin sorts first takes
  // the click, so these pin the ordering rather than the resolver.
  it("wins the click from StarterKit's Link instead of opening a tab", () => {
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    expect(clickOn("Receipt capture")).toBe(true);
    expect(open).toHaveBeenCalledWith(RECEIPTS);
    expect(window.open).not.toHaveBeenCalled();
  });

  it("leaves an external link to the editor's own default", () => {
    // Declined here, so the click falls through to Link — which is what
    // "the editor's own default" means for a link that is genuinely elsewhere.
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    expect(clickOn("wso2.com")).toBe(true);
    expect(open).not.toHaveBeenCalled();
    expect(window.open).toHaveBeenCalledWith("https://wso2.com/policy.md", "_blank");
  });

  it("swallows a reference to a document nobody has written", () => {
    // Inert, not external: `features/approvals.md` names a repo path, so
    // letting Link have the click would open a tab the console cannot serve.
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    expect(clickOn("Approvals")).toBe(true);
    expect(open).not.toHaveBeenCalled();
    expect(window.open).not.toHaveBeenCalled();
  });

  it("ignores a click that is not on the anchor", () => {
    // The Link mark is inclusive, so the position at the end of a story line
    // still reports the link that line ends with — and `prd-contract` puts the
    // feature reference exactly there. Clicking beside it must do nothing.
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    const event = new MouseEvent("click", { bubbles: true, button: 0 });
    Object.defineProperty(event, "target", { value: editor!.view.dom });
    const handled = editor!.view.someProp("handleClick", (f) =>
      f(editor!.view, positionOf("Receipt capture"), event),
    );
    expect(Boolean(handled)).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });

  it("ignores a non-primary button", () => {
    const open = vi.fn();
    mount([PRD, RECEIPTS], open);
    expect(clickOn("Receipt capture", 2)).toBe(false);
    expect(open).not.toHaveBeenCalled();
  });

  it("comes alive when the agent writes the file it names", () => {
    const el = mount([PRD, RECEIPTS]);
    expect(linked(el)).toEqual(["Receipt capture"]);

    binding = { ...binding, knownPaths: [PRD, RECEIPTS, "specs/requirements/features/approvals.md"] };
    refreshSpecLinks(editor!.view);

    expect(linked(el)).toEqual(["Receipt capture", "Approvals"]);
    expect(clickOn("Approvals")).toBe(true);
  });
});
