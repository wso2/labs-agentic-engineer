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

// Real-browser test (Chromium via Playwright) for aiming the agent at part of a
// markdown document (#666).
//
// This can NOT run in jsdom. The rule under test is console ADR-0023 — a
// selection may only OFFER — and "offer" is a claim about real focus, a real
// caret and a real `window.getSelection()`, none of which jsdom models. The
// failure this guards against is precisely the one the prototype shipped with
// twice: an affordance that quietly takes the keyboard, so selecting text stops
// meaning "retype this".
//
// The provider is faked the same way as SpecMdEditor.browser.test.tsx — the
// caret extension only reads `provider.awareness`, so a real Awareness on the
// doc is enough, and no server is involved.

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import { userEvent } from "@vitest/browser/context";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setDocFileAsAgent } from "@aep/collab-doc";
import { SpecMdEditor } from "./SpecMdEditor";
import type { Anchor } from "../lib/anchor";

const PATH = "requirements/prd.md";

const PRD = `# Demo Shop — PRD

## Product Decisions

Rounds close automatically thirty minutes before the delivery slot.

## Open Questions

Which Slack workspace does the bot post to?
`;

function fakeProvider(doc: Y.Doc): HocuspocusProvider {
  return { awareness: new Awareness(doc) } as unknown as HocuspocusProvider;
}

async function mountEditor(busyReason = "") {
  const doc = new Y.Doc();
  setDocFileAsAgent(doc, PATH, PRD, "test-agent", {
    agent: "Spec Agent",
    at: "2026-08-30T00:00:00.000Z",
  });
  const send = vi.fn<
    (instruction: string, anchor: Anchor, intent: "change" | "discuss") => Promise<boolean>
  >(async () => true);
  const view = render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ height: "420px", display: "flex", flexDirection: "column" }}>
        <SpecMdEditor
          fragment={doc.getXmlFragment(PATH)}
          provider={fakeProvider(doc)}
          self={{ name: "Tester", color: "#64b5f6" }}
          agentStreaming={false}
          aim={{ path: PATH, send, busyReason }}
        />
      </div>
    </OxygenUIThemeProvider>,
  );
  const editable = await waitFor(() => {
    const el = view.container.querySelector<HTMLElement>(".ProseMirror");
    if (!el) throw new Error("editor not mounted yet");
    return el;
  });
  return { view, editable, send };
}

/** Select a whole paragraph the way a drag would leave it. */
function selectParagraph(editable: HTMLElement, text: string): HTMLElement {
  const node = Array.from(editable.querySelectorAll("p")).find((p) =>
    (p.textContent ?? "").includes(text),
  );
  if (!node) throw new Error(`no paragraph containing ${text}`);
  // Focus FIRST: ProseMirror reads the DOM selection from the browser's own
  // `selectionchange`, and ignores it while the view is not focused — which is
  // also why this cannot be a jsdom test.
  editable.focus();
  const range = document.createRange();
  range.selectNodeContents(node);
  const selection = window.getSelection()!;
  selection.removeAllRanges();
  selection.addRange(range);
  return node;
}

const chip = (view: { container: HTMLElement }) =>
  view.container.querySelector<HTMLElement>('[data-testid="aim-chip"]');

const box = (view: { container: HTMLElement }) =>
  view.container.querySelector<HTMLTextAreaElement>('[data-testid="aim-box"] textarea');

const washed = (view: { container: HTMLElement }) =>
  [...view.container.querySelectorAll(".aim-selected")].map((el) => el.textContent ?? "");

afterEach(() => {
  cleanup();
});

describe("aiming the agent at a selection", () => {
  it("offers a chip on a selection and opens nothing", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Rounds close automatically");

    await waitFor(() => expect(chip(view)).toBeTruthy());
    // The whole decision, in one assertion: no input exists yet, so nothing can
    // have taken the keyboard.
    expect(box(view)).toBeNull();
    expect(document.activeElement).not.toBeInstanceOf(HTMLTextAreaElement);
  });

  it("still lets a selection be retyped, which is what an editor is for", async () => {
    const { view, editable } = await mountEditor();
    const paragraph = selectParagraph(editable, "Rounds close automatically");
    await waitFor(() => expect(chip(view)).toBeTruthy());

    await userEvent.keyboard("nope");

    // The typing replaced the selected text rather than landing in an ask box.
    expect(paragraph.textContent).toContain("nope");
    expect(paragraph.textContent).not.toContain("Rounds close automatically");
    expect(box(view)).toBeNull();
  });

  it("opens the box only when the chip is pressed, and focuses it then", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Which Slack workspace");
    const trigger = await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el as HTMLElement;
    });

    trigger.click();

    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });
    await waitFor(() => expect(document.activeElement).toBe(input));
  });

  it("sends the typed words with an anchor naming what was selected", async () => {
    const { view, editable, send } = await mountEditor();
    selectParagraph(editable, "Which Slack workspace");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });

    await userEvent.click(input);
    await userEvent.keyboard("name the workspace");
    const change = Array.from(view.container.querySelectorAll("button")).find(
      (b) => b.textContent?.trim() === "Change",
    )!;
    await userEvent.click(change);

    await waitFor(() => expect(send).toHaveBeenCalledOnce());
    const [instruction, anchor, intent] = send.mock.calls[0]!;
    expect(instruction).toBe("name the workspace");
    expect(intent).toBe("change");
    expect(anchor.file).toBe(PATH);
    expect(anchor.nodes[0]?.name).toContain("Which Slack workspace");
    expect(anchor.nodes[0]?.context).toBe("Demo Shop — PRD › Open Questions");
  });

  it("refuses both sends while an agent holds the turn, and says why", async () => {
    const reason = "An agent is still working — this is available once it finishes";
    const { view, editable } = await mountEditor(reason);
    selectParagraph(editable, "Which Slack workspace");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });
    await userEvent.click(input);
    await userEvent.keyboard("anything");

    const buttons = Array.from(view.container.querySelectorAll("button")).filter((b) =>
      ["Change", "Discuss"].includes(b.textContent?.trim() ?? ""),
    );
    expect(buttons).toHaveLength(2);
    for (const b of buttons) expect(b).toBeDisabled();
  });
});

// The gap the local-setup walk exposed. The browser stops painting the native
// selection the moment the editor loses focus — which is exactly what opening
// the box does — so without a decoration the user is typing an instruction
// about a passage they can no longer see.
describe("what the selection looks like while the box is open", () => {
  it("washes the blocks the agent would receive, and keeps it there once the box takes focus", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Rounds close automatically");

    await waitFor(() => expect(washed(view).some((t) => t.includes("Rounds close"))).toBe(true));

    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    await waitFor(() => expect(box(view)).not.toBeNull());

    // The native selection is gone by now — the wash is the only thing left
    // saying what this instruction is about.
    expect(window.getSelection()?.toString() ?? "").toBe("");
    expect(washed(view).some((t) => t.includes("Rounds close"))).toBe(true);
  });

  it("washes every block of a multi-block selection, not just the first", async () => {
    const { view, editable } = await mountEditor();
    const first = [...editable.querySelectorAll("p")].find((p) =>
      (p.textContent ?? "").includes("Rounds close automatically"),
    )!;
    const last = [...editable.querySelectorAll("p")].find((p) =>
      (p.textContent ?? "").includes("Which Slack workspace"),
    )!;
    editable.focus();
    const range = document.createRange();
    range.setStart(first.firstChild!, 0);
    range.setEnd(last.firstChild!, 5);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(range);

    await waitFor(() => expect(washed(view).length).toBeGreaterThan(1));
  });

  // Dismissing the box is not dismissing the SELECTION — the caret stays where
  // it was (ADR-0023), so the chip comes back and the wash stays with it. It
  // clears when the selection does.
  it("keeps the wash after the box is dismissed, and drops it when the selection goes", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Rounds close automatically");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });

    await userEvent.click(input);
    await userEvent.keyboard("{Escape}");

    await waitFor(() => expect(box(view)).toBeNull());
    expect(chip(view)).not.toBeNull();
    expect(washed(view).some((t) => t.includes("Rounds close"))).toBe(true);

    // Collapsing the selection is what ends the aim.
    editable.focus();
    const collapsed = document.createRange();
    const target = [...editable.querySelectorAll("p")][0]!;
    collapsed.setStart(target.firstChild!, 1);
    collapsed.collapse(true);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(collapsed);

    await waitFor(() => expect(washed(view)).toHaveLength(0));
  });
});

// Both found on the local setup with a real pointer, after every earlier gate
// passed: eval-driven clicks never move focus the way a mouse does.
describe("the box and the mouse", () => {
  it("dismisses when the document is clicked, and the click keeps its caret", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Rounds close automatically");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    await waitFor(() => expect(box(view)).not.toBeNull());

    // The title, ABOVE the selection: the box hangs below it, and Playwright
    // rightly refuses to click anything the box covers.
    const elsewhere = editable.querySelector("h1")!;
    await userEvent.click(elsewhere);

    await waitFor(() => expect(box(view)).toBeNull());
    // Focus went where the click went — nothing pulled it back — and the click
    // kept its caret: collapsed, inside the clicked block.
    expect(document.activeElement).toBe(editable);
    const selection = window.getSelection();
    expect(selection?.isCollapsed).toBe(true);
    expect(elsewhere.contains(selection?.anchorNode ?? null)).toBe(true);
  });

  it("takes focus from a click on the input even when focus had gone elsewhere", async () => {
    const { view, editable } = await mountEditor();
    selectParagraph(editable, "Rounds close automatically");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });

    // Focus wanders off — a tab switch, a toolbar press — and comes back by
    // clicking the input, the way anyone would.
    editable.focus();
    expect(document.activeElement).toBe(editable);
    await userEvent.click(input);
    await userEvent.keyboard("shorter");

    expect(document.activeElement).toBe(input);
    expect(input.value).toBe("shorter");
    // And none of it landed in the document.
    expect(editable.textContent).not.toContain("shorter");
  });
});

// Reported from use, mid-turn: Enter bypassed the disabled buttons, the send
// was refused, and the box sat open and mute — the reason landed as an error
// row in a chat panel a quiet Change never opens.
describe("what a send that cannot go looks like", () => {
  it("Enter while an agent holds the turn sends nothing and keeps the words", async () => {
    const reason = "An agent is still working — this is available once it finishes";
    const { view, editable, send } = await mountEditor(reason);
    selectParagraph(editable, "Rounds close automatically");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });
    await userEvent.click(input);
    await userEvent.keyboard("shorter please{Enter}");

    expect(send).not.toHaveBeenCalled();
    expect(box(view)).not.toBeNull();
    expect(input.value).toBe("shorter please");
  });

});

// Reported twice from use as "Enter does not close the box". The dispatch is
// not instant — it resolves the repo and two snapshots before answering — and
// first the box waited on it mute, then it waited with a spinner. Neither was
// right: the message row is already in the log the moment the send starts, so
// the box has nothing left to say. Enter closes it, full stop; the rare
// refused dispatch surfaces in the log with the panel opened onto it.
describe("Enter is the end of the box", () => {
  it("closes immediately, before the dispatch has answered", async () => {
    const { view, editable, send } = await mountEditor();
    send.mockImplementation(() => new Promise<boolean>(() => {}));
    selectParagraph(editable, "Rounds close automatically");
    (await waitFor(() => {
      const el = chip(view);
      if (!el) throw new Error("no chip yet");
      return el;
    })).click();
    const input = await waitFor(() => {
      const el = box(view);
      if (!el) throw new Error("no box yet");
      return el;
    });
    await userEvent.click(input);
    await userEvent.keyboard("shorter please{Enter}");

    // The dispatch has NOT resolved — and the box is already gone.
    expect(box(view)).toBeNull();
    expect(send).toHaveBeenCalledOnce();
  });
});
