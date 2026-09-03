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

// The half of the lens surface jsdom cannot see: what is actually ON SCREEN.
//
// A section lens is always visible — it is how a command is discovered at all —
// while a line lens waits for its entry's hover, so a twenty-story list carries
// three visible controls rather than twenty-three. That split is CSS over real
// layout and a real pointer, which is what puts this test in the browser lane.
//
// The document is seeded through the production path (`setDocFile` over the
// file's Y.XmlFragment), so the editor renders the same markdown the collab
// server would hand it.

import { afterEach, describe, expect, it, vi } from "vitest";
import { userEvent } from "@vitest/browser/context";
import { cleanup, render, waitFor } from "@testing-library/react";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setDocFile } from "@aep/collab-doc";
import { SpecMdEditor } from "./SpecMdEditor";

const PATH = "requirements/prd.md";

const PRD = `# Expenses — PRD

## Actors

- Employee — files expenses and tracks their status

## User Stories

1. As an Employee, I want to submit an expense with a receipt photo.
2. As a Manager, I want to approve or reject an expense.

## Product Decisions

- Sign-in: the org Google Workspace *assumed*

## Open Questions

1. Which accounting system do we export to?
2. What is the approval limit? Deferred — the user will decide next quarter.
`;

// CollaborationCaret only reads `provider.awareness`, so a real Awareness on
// the doc stands in for a connected provider.
function fakeProvider(doc: Y.Doc): HocuspocusProvider {
  return { awareness: new Awareness(doc) } as unknown as HocuspocusProvider;
}

async function mountPrd(busyReason = "") {
  const doc = new Y.Doc();
  setDocFile(doc, PATH, PRD);
  const view = render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ height: "640px", display: "flex", flexDirection: "column" }}>
        <SpecMdEditor
          fragment={doc.getXmlFragment(PATH)}
          provider={fakeProvider(doc)}
          self={{ name: "Tester", color: "#64b5f6" }}
          agentStreaming={false}
          lenses={{ run: () => {}, busyReason }}
        />
      </div>
    </OxygenUIThemeProvider>,
  );
  await waitFor(() => {
    if (!view.container.querySelector(".prd-lens")) throw new Error("lenses not rendered yet");
  });
  return { doc, view };
}

const opacityOf = (el: Element) => Number(getComputedStyle(el).opacity);

/** The lens on the list entry whose text starts with `prefix`. */
function lensOn(container: HTMLElement, prefix: string): HTMLElement {
  const item = Array.from(container.querySelectorAll("li")).find((li) =>
    li.textContent?.startsWith(prefix),
  );
  if (!item) throw new Error(`no entry starting "${prefix}"`);
  const lens = item.querySelector<HTMLElement>(".prd-lens--line");
  if (!lens) throw new Error(`entry "${prefix}" carries no line lens`);
  return lens;
}

afterEach(() => {
  cleanup();
});

describe("the PRD's lens surface, on screen", () => {
  it("shows the section lenses at rest and holds the line lenses back", async () => {
    const { doc, view } = await mountPrd();

    const sections = view.container.querySelectorAll(".prd-lens--section");
    expect(sections.length).toBe(3);
    for (const lens of sections) expect(opacityOf(lens)).toBe(1);

    for (const lens of view.container.querySelectorAll(".prd-lens--line")) {
      expect(opacityOf(lens)).toBe(0);
    }

    doc.destroy();
  });

  it("reveals a line's lens when the pointer is on that line, and only that one", async () => {
    const { doc, view } = await mountPrd();
    const story = lensOn(view.container, "As an Employee");
    const other = lensOn(view.container, "As a Manager");

    // CSS `:hover` follows the real pointer, not a dispatched event, so the
    // pointer is what moves.
    await userEvent.hover(story.closest("li")!);

    await waitFor(() => expect(opacityOf(story)).toBe(1));
    expect(opacityOf(other)).toBe(0);

    doc.destroy();
  });

  it("renders every lens disabled while an agent holds the turn, except the verdicts", async () => {
    const { doc, view } = await mountPrd("An agent is still working");

    const lenses = Array.from(view.container.querySelectorAll<HTMLButtonElement>(".prd-lens"));
    expect(lenses.length).toBeGreaterThan(0);
    for (const lens of lenses) {
      if (lens.classList.contains("prd-lens--edit")) {
        expect(lens.disabled).toBe(false);
        continue;
      }
      expect(lens.disabled).toBe(true);
      expect(lens.title).toBe("An agent is still working");
    }

    doc.destroy();
  });

  // Reported from use: the moment a turn disabled the line lenses, every one of
  // them appeared at once — `.prd-lens:disabled`'s inert dimming outweighed the
  // line lens's hidden state. Hidden is hidden, inert or not.
  it("keeps the line lenses hidden while they are inert", async () => {
    const { doc, view } = await mountPrd("An agent is still working");

    for (const lens of view.container.querySelectorAll(".prd-lens--line")) {
      expect(opacityOf(lens)).toBe(0);
    }
    // The section lenses stay on show — dimmed is how they say inert.
    for (const lens of view.container.querySelectorAll(".prd-lens--section")) {
      expect(opacityOf(lens)).toBeCloseTo(0.4, 5);
    }

    doc.destroy();
  });

  // The Discuss lens (#652) and the selection surface (#666) are one path: the
  // lens leaves the block selected, exactly as a drag would have, and opens
  // the same box. What Enter sends is the one difference, and it is asserted
  // through the binding rather than the keyboard's label.
  it("Discuss opens the aim box on its line, with Enter sending Discuss", async () => {
    const sends: Array<{ intent: string; name: string }> = [];
    const doc = new Y.Doc();
    setDocFile(doc, PATH, PRD);
    const view = render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <div style={{ height: "640px", display: "flex", flexDirection: "column" }}>
          <SpecMdEditor
            fragment={doc.getXmlFragment(PATH)}
            provider={fakeProvider(doc)}
            self={{ name: "Tester", color: "#64b5f6" }}
            agentStreaming={false}
            lenses={{ run: () => {}, busyReason: "" }}
            aim={{
              path: PATH,
              busyReason: "",
              send: async (_instruction, anchor, intent) => {
                sends.push({ intent, name: anchor.nodes[0]?.name ?? "" });
                return true;
              },
            }}
          />
        </div>
      </OxygenUIThemeProvider>,
    );
    await waitFor(() => {
      if (!view.container.querySelector(".prd-lens")) throw new Error("lenses not rendered yet");
    });

    const story = Array.from(view.container.querySelectorAll("li")).find((li) =>
      li.textContent?.startsWith("As a Manager"),
    )!;
    const discuss = Array.from(story.querySelectorAll<HTMLButtonElement>(".prd-lens")).find(
      (b) => b.textContent === "Discuss",
    )!;
    discuss.click();

    const input = await waitFor(() => {
      const el = view.container.querySelector<HTMLTextAreaElement>('[data-testid="aim-box"] textarea');
      if (!el) throw new Error("no box yet");
      return el;
    });
    await waitFor(() => expect(document.activeElement).toBe(input));
    // The wash is on the line the lens sat on — the selection is the block.
    expect(
      Array.from(view.container.querySelectorAll(".aim-selected")).some((el) =>
        el.textContent?.includes("As a Manager"),
      ),
    ).toBe(true);

    await userEvent.keyboard("why a day?{Enter}");

    await waitFor(() => expect(sends).toHaveLength(1));
    expect(sends[0]!.intent).toBe("discuss");
    expect(sends[0]!.name).toContain("As a Manager, I want to approve or reject an expense.");

    doc.destroy();
  });
});

// "Review them first" (#666) has to land ON the first flagged line, and
// whether an element ended up in the viewport is real layout — the browser
// lane again.
describe("revealing the first unsettled line", () => {
  it("scrolls the first flagged line into view when asked", async () => {
    // A tall document: many plain stories above the flagged decision, so the
    // flag starts well below the fold of a 320px editor.
    const tall =
      "# Expenses — PRD\n\n## User Stories\n\n" +
      Array.from({ length: 40 }, (_, i) => `${i + 1}. As an Employee, I want story number ${i + 1}.`).join("\n") +
      "\n\n## Product Decisions\n\n- Sign-in: the org Google Workspace *assumed*\n";
    const doc = new Y.Doc();
    setDocFile(doc, PATH, tall);
    // Built ONCE: the editor is keyed on (fragment, provider), and a provider
    // made inside the harness would remount it on every rerender.
    const fragment = doc.getXmlFragment(PATH);
    const provider = fakeProvider(doc);
    const Harness = ({ reveal }: { reveal: number }) => (
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <div style={{ height: "320px", display: "flex", flexDirection: "column" }}>
          <SpecMdEditor
            fragment={fragment}
            provider={provider}
            self={{ name: "Tester", color: "#64b5f6" }}
            agentStreaming={false}
            lenses={{ run: () => {}, busyReason: "" }}
            revealUnsettled={reveal}
          />
        </div>
      </OxygenUIThemeProvider>
    );
    const view = render(<Harness reveal={0} />);
    const flag = await waitFor(() => {
      const el = view.container.querySelector<HTMLElement>(".prd-flag--assumed");
      if (!el) throw new Error("no flag yet");
      return el;
    });
    const scroller = flag.closest<HTMLElement>("[data-aim-scroll]")!;
    // Re-queried every time: the flag is a decoration span, and ProseMirror
    // re-renders it, so a held reference goes stale mid-scroll.
    const inView = () => {
      const f = view.container.querySelector<HTMLElement>(".prd-flag--assumed")!.getBoundingClientRect();
      const s = scroller.getBoundingClientRect();
      return f.top >= s.top && f.bottom <= s.bottom;
    };
    expect(inView()).toBe(false);

    view.rerender(<Harness reveal={1} />);

    await waitFor(() => expect(inView()).toBe(true), { timeout: 3000 });
    doc.destroy();
  });
});

// The add-lenses collect their subject first (#666): + Feature opens the box,
// the user types the idea, and the send is the command PLUS their words —
// `/feature manager should approve` — through the same run path the lens
// always used, so the panel opens and the line reads as theirs.
describe("the add-lenses ask before they fire", () => {
  it("+ Feature opens the box and Enter sends the command with the typed subject", async () => {
    const run = vi.fn();
    const doc = new Y.Doc();
    setDocFile(doc, PATH, PRD);
    const view = render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <div style={{ height: "640px", display: "flex", flexDirection: "column" }}>
          <SpecMdEditor
            fragment={doc.getXmlFragment(PATH)}
            provider={fakeProvider(doc)}
            self={{ name: "Tester", color: "#64b5f6" }}
            agentStreaming={false}
            lenses={{ run, busyReason: "" }}
            aim={{ path: PATH, busyReason: "", send: async () => true }}
          />
        </div>
      </OxygenUIThemeProvider>,
    );
    const lens = await waitFor(() => {
      const el = Array.from(view.container.querySelectorAll<HTMLButtonElement>(".prd-lens")).find(
        (b) => b.textContent === "+ Feature",
      );
      if (!el) throw new Error("no + Feature lens yet");
      return el;
    });

    lens.click();

    const input = await waitFor(() => {
      const el = view.container.querySelector<HTMLTextAreaElement>('[data-testid="aim-box"] textarea');
      if (!el) throw new Error("no box yet");
      return el;
    });
    expect(input.placeholder).toBe("Describe the feature in your own words…");
    // Nothing was sent by the click itself.
    expect(run).not.toHaveBeenCalled();
    // No wash either: the command aims at nothing.
    expect(view.container.querySelectorAll(".aim-selected")).toHaveLength(0);

    await userEvent.click(input);
    await userEvent.keyboard("manager should approve{Enter}");

    expect(run).toHaveBeenCalledWith("/feature manager should approve");
    // The box closed with the send.
    await waitFor(() =>
      expect(view.container.querySelector('[data-testid="aim-box"]')).toBeNull(),
    );

    doc.destroy();
  });

  it("the CTA sends the bare command when nothing is typed — the pre-prompt behaviour", async () => {
    const run = vi.fn();
    const doc = new Y.Doc();
    setDocFile(doc, PATH, PRD);
    const view = render(
      <OxygenUIThemeProvider theme={OxygenTheme}>
        <div style={{ height: "640px", display: "flex", flexDirection: "column" }}>
          <SpecMdEditor
            fragment={doc.getXmlFragment(PATH)}
            provider={fakeProvider(doc)}
            self={{ name: "Tester", color: "#64b5f6" }}
            agentStreaming={false}
            lenses={{ run, busyReason: "" }}
            aim={{ path: PATH, busyReason: "", send: async () => true }}
          />
        </div>
      </OxygenUIThemeProvider>,
    );
    const lens = await waitFor(() => {
      const el = Array.from(view.container.querySelectorAll<HTMLButtonElement>(".prd-lens")).find(
        (b) => b.textContent === "+ Actor",
      );
      if (!el) throw new Error("no + Actor lens yet");
      return el;
    });
    lens.click();
    const cta = await waitFor(() => {
      const el = Array.from(view.container.querySelectorAll<HTMLButtonElement>("button")).find(
        (b) => b.textContent?.trim() === "Add actor",
      );
      if (!el) throw new Error("no CTA yet");
      return el;
    });

    await userEvent.click(cta);

    expect(run).toHaveBeenCalledWith("/actor");
    doc.destroy();
  });
});
