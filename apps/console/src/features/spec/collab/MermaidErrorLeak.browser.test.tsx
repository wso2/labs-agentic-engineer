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

// A mermaid block that does not parse must leave NOTHING behind in the page.
//
// Needs real mermaid and a real DOM, so it lives in the browser lane: the unit
// lane injects a fake renderer (jsdom cannot measure SVG text), and a fake can
// never reproduce this — the leak is mermaid's own DOM write, not ours.
//
// The bug: `mermaid.render(id, src)` is called with no container, so mermaid
// builds its scratch element under <body>. On a parse error it renders its
// "Syntax error in text" bomb into that element and then throws, skipping the
// cleanup that only runs on the success return. The bomb lands outside React's
// tree, so nothing removes it — it draws below the whole app and survives
// navigation.
//
// Failures are ROUTINE, which is what made this visible in use: the node view
// re-renders on every flush of the agent's stream, so a half-written diagram is
// parsed and fails repeatedly before the block closes. A live session stacked
// four bombs under the footer.

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setDocFile } from "@aep/collab-doc";
import { SpecMdEditor } from "./SpecMdEditor";

const PATH = "design/design.md";

// The shape a stream actually produces: `sequenceDiagram` is written, its
// participants are not. A valid prefix of an invalid document — what every
// intermediate flush looks like, and measured to leak without the fix.
const PARTIAL = ["# Design", "", "```mermaid", "sequenceDiagram", "    parti", "```"].join("\n");

function fakeProvider(doc: Y.Doc): HocuspocusProvider {
  return { awareness: new Awareness(doc) } as unknown as HocuspocusProvider;
}

async function mountWith(content: string) {
  const doc = new Y.Doc();
  setDocFile(doc, PATH, content, "agent-stream");
  const view = render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ height: "600px", display: "flex", flexDirection: "column" }}>
        <SpecMdEditor
          fragment={doc.getXmlFragment(PATH)}
          provider={fakeProvider(doc)}
          self={{ name: "Tester", color: "#64b5f6" }}
          agentStreaming
          editable
        />
      </div>
    </OxygenUIThemeProvider>,
  );
  await waitFor(() => {
    const el = view.container.querySelector<HTMLElement>(".ProseMirror");
    if (!el) throw new Error("editor not mounted yet");
    return el;
  });
  return view;
}

/** Mermaid's scratch element is `d` + the render id (`spec-mermaid-<n>`). */
const orphans = () => document.querySelectorAll('body > [id^="dspec-mermaid-"]');

/**
 * Let the render finish before looking.
 *
 * Deliberately NOT `waitFor`: the assertion here is an ABSENCE, and `waitFor`
 * resolves on its first passing poll — which happens at t≈0, before the async,
 * queued render has had a chance to leak anything. That yields a test which
 * passes whether or not the bug is present (verified: it did). Measured with
 * the fix removed, the orphan appears well inside this window; with the fix the
 * count stays 0 throughout.
 */
const settle = () => new Promise((r) => setTimeout(r, 1500));

afterEach(cleanup);

describe("a mermaid block that fails to parse", () => {
  it("leaves no error graphic in the page body", async () => {
    await mountWith(PARTIAL);
    await settle();

    expect(orphans().length).toBe(0);
    expect(document.body.textContent ?? "").not.toContain("Syntax error in text");
  });

  it("does not accumulate one graphic per streamed flush", async () => {
    // Each mount is one more failed render — what a stream does on every flush.
    for (let i = 0; i < 4; i++) {
      await mountWith(PARTIAL);
      await settle();
      cleanup();
    }

    expect(orphans().length).toBe(0);
    expect(document.body.textContent ?? "").not.toContain("Syntax error in text");
  });
});
