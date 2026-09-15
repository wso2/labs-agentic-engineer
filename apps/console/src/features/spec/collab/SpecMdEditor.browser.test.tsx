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

// Real-browser test (Chromium via Playwright) for the streaming auto-scroll in
// SpecMdEditor. This can NOT run in jsdom: following the agent's edit is a
// claim about real layout (coordsAtPos, scrollHeight/clientHeight/scrollTop),
// none of which jsdom provides.
//
// The agent's collab writes are emulated faithfully: the real generation path
// is `services/agents` joining the room and calling `setDocFileAsAgent(doc,
// path, markdown, …)` (room-peer.ts). We call the very same @aep/collab-doc
// helper against a shared Y.Doc whose XmlFragment backs the editor, so the
// editor grows exactly as it does in production — no server, no cluster.

import { afterEach, describe, expect, it } from "vitest";
import { userEvent } from "@vitest/browser/context";
import { cleanup, render, waitFor } from "@testing-library/react";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setDocFileAsAgent } from "@aep/collab-doc";
import { SpecMdEditor } from "./SpecMdEditor";

const PATH = "requirements/prd.md";
const AGENT_META = { agent: "Spec Agent", at: "2026-07-12T00:00:00.000Z" };

// CollaborationCaret only reads `provider.awareness` (verified against the
// installed extension), so a real Awareness on the doc is a sufficient stand-in
// for a connected HocuspocusProvider.
function fakeProvider(doc: Y.Doc): HocuspocusProvider {
  return { awareness: new Awareness(doc) } as unknown as HocuspocusProvider;
}

// A growing PRD: `n` sections. Each call to setDocFileAsAgent reconciles the
// fragment to this markdown, so larger n ⇒ taller document (the stream).
function prd(n: number): string {
  let s = "# Product Requirements\n\n";
  for (let i = 1; i <= n; i++) {
    s +=
      `## Section ${i}\n\n` +
      `Requirement ${i}: the system shall do the thing described in this ` +
      `paragraph, which is long enough to take vertical space and force the ` +
      `document to overflow its scroll container.\n\n`;
  }
  return s;
}

const raf = () =>
  new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));

// Nearest scrollable ancestor of the editable element = the editor's document
// scroll region (the Box that owns `overflow: auto` in SpecMdEditor).
function scrollParentOf(el: HTMLElement): HTMLElement {
  let node: HTMLElement | null = el;
  while (node) {
    const oy = getComputedStyle(node).overflowY;
    if (oy === "auto" || oy === "scroll") return node;
    node = node.parentElement;
  }
  throw new Error("no scrollable ancestor found");
}

const bottomGap = (s: HTMLElement) =>
  s.scrollHeight - s.clientHeight - s.scrollTop;

// Mount the editor in a fixed-height flex column so its `flexGrow:1` scroll
// region gets a real, bounded height and can actually overflow.
async function mountEditor(
  doc: Y.Doc,
  agentStreaming: boolean,
  editable = true,
) {
  const fragment = doc.getXmlFragment(PATH);
  const view = render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ height: "320px", display: "flex", flexDirection: "column" }}>
        <SpecMdEditor
          fragment={fragment}
          provider={fakeProvider(doc)}
          self={{ name: "Tester", color: "#64b5f6" }}
          agentStreaming={agentStreaming}
          editable={editable}
        />
      </div>
    </OxygenUIThemeProvider>,
  );
  // The Tiptap editor mounts asynchronously (useEditor).
  const proseMirrorEl = await waitFor(() => {
    const el = view.container.querySelector<HTMLElement>(".ProseMirror");
    if (!el) throw new Error("editor not mounted yet");
    return el;
  });
  return { view, scrollEl: scrollParentOf(proseMirrorEl) };
}

// Emulate the agent streaming `n` sections in, one reconcile per step, giving
// the ResizeObserver/animation a frame between writes (as real token streaming
// would).
async function stream(doc: Y.Doc, sections: number) {
  for (let n = 1; n <= sections; n++) {
    setDocFileAsAgent(doc, PATH, prd(n), "test-agent", AGENT_META);
    await raf();
  }
}

afterEach(() => {
  cleanup();
});

describe("SpecMdEditor streaming auto-scroll", () => {
  it("follows the tail while an agent streams (agentStreaming=true)", async () => {
    const doc = new Y.Doc();
    const { scrollEl } = await mountEditor(doc, true);

    await stream(doc, 40);

    // The document must actually overflow, else the assertion is vacuous.
    await waitFor(
      () => expect(scrollEl.scrollHeight - scrollEl.clientHeight).toBeGreaterThan(200),
      { timeout: 4000 },
    );
    // …and the view must have followed the growth to the bottom.
    await waitFor(() => expect(bottomGap(scrollEl)).toBeLessThan(8), {
      timeout: 4000,
      interval: 50,
    });
    expect(scrollEl.scrollTop).toBeGreaterThan(100);

    doc.destroy();
  });

  it("does NOT auto-scroll when no agent is streaming (agentStreaming=false)", async () => {
    const doc = new Y.Doc();
    const { scrollEl } = await mountEditor(doc, false);

    await stream(doc, 40);

    // Same overflow, but the view stays at the top — no tail-follow off-stream.
    await waitFor(
      () => expect(scrollEl.scrollHeight - scrollEl.clientHeight).toBeGreaterThan(200),
      { timeout: 4000 },
    );
    // Give any stray animation a chance to (wrongly) fire before asserting.
    await new Promise((r) => setTimeout(r, 400));
    expect(scrollEl.scrollTop).toBeLessThan(8);

    doc.destroy();
  });

  it("stops following once the user scrolls up mid-stream", async () => {
    const doc = new Y.Doc();
    const { scrollEl } = await mountEditor(doc, true);

    // Get it following at the bottom first.
    await stream(doc, 25);
    await waitFor(() => expect(bottomGap(scrollEl)).toBeLessThan(8), {
      timeout: 4000,
      interval: 50,
    });

    // User scrolls up — a real wheel-up event breaks the lock immediately.
    scrollEl.dispatchEvent(
      new WheelEvent("wheel", { deltaY: -200, bubbles: true }),
    );
    await raf();

    // More content streams in; the view must NOT snap back to the bottom.
    await stream(doc, 55);
    await new Promise((r) => setTimeout(r, 400));
    expect(bottomGap(scrollEl)).toBeGreaterThan(50);

    doc.destroy();
  });
});

// The follow is on the EDIT, not the tail (#666). Reported from use: the PRD
// jumped to the bottom whenever the agent said anything, taking the reader
// away from the passage they had just aimed the agent at.
describe("SpecMdEditor follows the agent's edit, not the tail", () => {
  /** A tall document with a named paragraph a third of the way down. */
  function tallWithTarget(targetText: string): string {
    let s = "# Product Requirements\n\n";
    for (let i = 1; i <= 40; i++) {
      if (i === 14) s += `## Target\n\n${targetText}\n\n`;
      s +=
        `## Section ${i}\n\n` +
        `Requirement ${i}: the system shall do the thing described in this ` +
        `paragraph, which is long enough to take vertical space.\n\n`;
    }
    return s;
  }

  it("scrolls to a paragraph the agent rewrote in the middle, and not to the bottom", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(doc, PATH, tallWithTarget("The old sentence."), "test-agent", AGENT_META);
    const { view, scrollEl } = await mountEditor(doc, true);
    await waitFor(() =>
      expect(scrollEl.scrollHeight - scrollEl.clientHeight).toBeGreaterThan(400),
    );
    expect(scrollEl.scrollTop).toBe(0);

    setDocFileAsAgent(
      doc,
      PATH,
      tallWithTarget("The agent rewrote this sentence in place."),
      "test-agent",
      AGENT_META,
    );

    await waitFor(() => {
      const target = Array.from(view.container.querySelectorAll("p")).find((p) =>
        p.textContent?.includes("rewrote this sentence"),
      )!;
      const t = target.getBoundingClientRect();
      const s = scrollEl.getBoundingClientRect();
      expect(t.top >= s.top && t.bottom <= s.bottom).toBe(true);
    });
    // In view — and nowhere near the bottom.
    expect(bottomGap(scrollEl)).toBeGreaterThan(200);

    doc.destroy();
  });

  it("does not move at all when the turn writes nothing — a Discuss", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(doc, PATH, tallWithTarget("Unchanged."), "test-agent", AGENT_META);
    const { scrollEl } = await mountEditor(doc, true);
    await waitFor(() =>
      expect(scrollEl.scrollHeight - scrollEl.clientHeight).toBeGreaterThan(400),
    );

    // The agent holds the turn and talks; the document is untouched.
    await new Promise((r) => setTimeout(r, 400));
    expect(scrollEl.scrollTop).toBe(0);

    doc.destroy();
  });
});

// Reported from use: pressing Undo threw the reader to the bottom. Two causes,
// both pinned here: the toolbar's focus chased a caret that a whole-document
// undo replace had clamped to the end, and the follow-the-edit treated the
// undo as the agent writing.
describe("undo keeps the reader's place", () => {
  it("does not scroll when the toolbar's Undo undoes an edit at the top", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(doc, PATH, prd(40), "test-agent", AGENT_META);
    const { view, scrollEl } = await mountEditor(doc, true);
    await waitFor(() =>
      expect(scrollEl.scrollHeight - scrollEl.clientHeight).toBeGreaterThan(400),
    );

    // A local edit near the top — the kind Agree makes.
    const editor = (view.container.querySelector(".ProseMirror") as HTMLElement & {
      editor?: unknown;
    });
    const paragraph = Array.from(view.container.querySelectorAll("p")).find((p) =>
      p.textContent?.includes("Requirement 1:"),
    )!;
    paragraph.scrollIntoView({ block: "center" });
    const before = scrollEl.scrollTop;

    // Type into that paragraph through the real editor surface.
    const range = document.createRange();
    range.setStart(paragraph.firstChild!, 0);
    range.collapse(true);
    (view.container.querySelector(".ProseMirror") as HTMLElement).focus();
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    await userEvent.keyboard("XX");
    expect(paragraph.textContent).toContain("XX");

    const undo = Array.from(view.container.querySelectorAll<HTMLButtonElement>("button")).find(
      (b) => b.getAttribute("aria-label")?.startsWith("Undo") || /undo/i.test(b.title ?? ""),
    )!;
    undo.click();

    await waitFor(() => expect(paragraph.textContent).not.toContain("XX"));
    // The reader stays where they were — nowhere near the bottom.
    expect(Math.abs(scrollEl.scrollTop - before)).toBeLessThan(120);
    void editor;
    doc.destroy();
  });
});

// A caller with ae:design-view but not ae:design must not be able to mutate
// the shared doc through this editor: `editable: false` on Tiptap only blocks
// DOM-originated input, not a programmatic `view.dispatch` — so this proves
// BOTH halves hold, the one ProseMirror gives for free and the one SpecMdEditor
// has to enforce itself by hiding every dispatch-capable control.
describe("SpecMdEditor is truly read-only without ae:design", () => {
  it("rejects typed input", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(doc, PATH, "# Product Requirements\n\nOriginal text.\n", "test-agent", AGENT_META);
    const { view } = await mountEditor(doc, false, false);

    const paragraph = Array.from(view.container.querySelectorAll("p")).find((p) =>
      p.textContent?.includes("Original text."),
    )!;
    const range = document.createRange();
    range.setStart(paragraph.firstChild!, 0);
    range.collapse(true);
    (view.container.querySelector(".ProseMirror") as HTMLElement).focus();
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);
    await userEvent.keyboard("XX");

    // Give a wrongly-accepted keystroke a beat to land before asserting absence.
    await new Promise((r) => setTimeout(r, 200));
    expect(paragraph.textContent).not.toContain("XX");
    expect(paragraph.textContent).toContain("Original text.");

    doc.destroy();
  });

  it("hides the toolbar, the bubble menu, and the accept/reject review bar", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(
      doc,
      PATH,
      "# Product Requirements\n\nSome agent-suggested text.\n",
      "test-agent",
      AGENT_META,
    );
    const { view } = await mountEditor(doc, false, false);

    // The toolbar's Bold button (present whenever SpecMdToolbar renders).
    expect(
      view.container.querySelector('[aria-label="Bold"], button[title*="Bold" i]'),
    ).toBeNull();
    // No dispatch-capable control anywhere in the mounted tree.
    expect(view.container.querySelector("button")).toBeNull();

    doc.destroy();
  });

  // The domain-model diagram is a ```mermaid code block (MermaidCodeBlock.tsx)
  // rendered by a Tiptap NodeView, which has its OWN render logic outside
  // SpecMdToolbar/BubbleMenu — reported separately because SpecMdEditor's
  // `editable` prop alone does not reach it; the node view has to read
  // `editor.isEditable` itself.
  it("hides the mermaid block's Edit control (the domain-model diagram)", async () => {
    const doc = new Y.Doc();
    setDocFileAsAgent(
      doc,
      PATH,
      [
        "# Design",
        "",
        "```mermaid",
        "erDiagram",
        "    USER ||--o{ ORDER : places",
        "```",
        "",
      ].join("\n"),
      "test-agent",
      AGENT_META,
    );
    const { view } = await mountEditor(doc, false, false);

    // `[data-testid="mermaid-preview"]` is always in the DOM (toggled by
    // `display`, not mounted/unmounted) — wait for the parsed SVG itself, or
    // this passes vacuously before mermaid has rendered anything (verified:
    // it did, against the pre-fix code, before this wait was added).
    await waitFor(() => {
      expect(
        view.container.querySelector('[data-testid="mermaid-preview"] svg'),
      ).not.toBeNull();
    });
    expect(
      Array.from(view.container.querySelectorAll("button")).some(
        (b) => b.textContent?.trim() === "Edit",
      ),
    ).toBe(false);

    doc.destroy();
  });
});
