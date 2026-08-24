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

import { afterEach, describe, expect, it } from "vitest";
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

  it("renders every lens disabled while an agent holds the turn", async () => {
    const { doc, view } = await mountPrd("An agent is still working");

    const lenses = Array.from(view.container.querySelectorAll<HTMLButtonElement>(".prd-lens"));
    expect(lenses.length).toBeGreaterThan(0);
    for (const lens of lenses) {
      expect(lens.disabled).toBe(true);
      expect(lens.title).toBe("An agent is still working");
    }

    doc.destroy();
  });
});
