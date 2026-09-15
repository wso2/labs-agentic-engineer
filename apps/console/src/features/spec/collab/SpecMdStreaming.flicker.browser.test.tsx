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

// Reproduction loop for the "flicker while requirements stream" bug (real
// Chromium — needs real layout + a live DOM). It drives the EXACT live write
// cadence: services/agents' StreamingDocWriter.flushLines writes
// `content.slice(0, lastNewline+1)` on every line boundary, each write going
// through @aep/collab-doc (full markdown reparse + y-prosemirror
// `updateYFragment`). We replicate that flush sequence against the real
// SpecMdEditor and measure how much the editor's DOM churns.
//
// Flicker == already-rendered inline DOM (esp. the `agent-insertion` highlight
// spans) getting torn down and re-created on each flush — a visible blink.
//
// Two runs isolate the cause:
//   - MARKED  (`setDocFileAsAgent`, the live agent path): a file created by
//     streaming is only "new"/mark-free on its FIRST chunk; chunks 2..N look
//     like edits and get review-highlight marks whose spans then churn.
//   - UNMARKED (`setDocFile`, control): same content, no marks — isolates how
//     much churn is inherent (tail text) vs mark-driven (the flicker).

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, waitFor } from "@testing-library/react";
import * as Y from "yjs";
import { Awareness } from "y-protocols/awareness";
import type { HocuspocusProvider } from "@hocuspocus/provider";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { setDocFile, setDocFileAsAgent } from "@aep/collab-doc";
import { SpecMdEditor } from "./SpecMdEditor";

const PATH = "requirements/prd.md";
// Faithful to the live agent path: room-peer.ts stamps a DISTINCT
// `at: new Date().toISOString()` on every flush, so each streamed chunk gets a
// different agentInsertion mark (adjacent chunks render as separate spans). A
// constant mark would instead collapse to one growing span and over-report
// span churn — a different failure than what ships.
const flushMeta = (n: number) => ({
  agent: "Spec Agent",
  at: `2026-07-12T00:00:${String(n % 60).padStart(2, "0")}.${String(n % 1000).padStart(3, "0")}Z`,
});

// Distinct text per line so identical text can only mean "same content
// re-rendered". Mixed block types (list, table) because those were suspected.
const DOC = [
  "# Product Requirements",
  "",
  "Intro paragraph one alpha.",
  "",
  "Intro paragraph two beta.",
  "",
  "## Section A Goals",
  "",
  "- bullet one gamma",
  "- bullet two delta",
  "- bullet three epsilon",
  "",
  "## Section B Data",
  "",
  "Paragraph under B zeta value.",
  "",
  "| Column One | Column Two |",
  "| --- | --- |",
  "| row one aaa | row one bbb |",
  "| row two ccc | row two ddd |",
  "",
  "## Section C Notes",
  "",
  "Closing paragraph eta omega.",
].join("\n");

type WriteFn = (doc: Y.Doc, path: string, content: string, flush: number) => void;

const writeMarked: WriteFn = (doc, path, content, flush) =>
  void setDocFileAsAgent(doc, path, content, "agent-stream", flushMeta(flush));
const writeUnmarked: WriteFn = (doc, path, content) =>
  setDocFile(doc, path, content, "agent-stream");

function fakeProvider(doc: Y.Doc): HocuspocusProvider {
  return { awareness: new Awareness(doc) } as unknown as HocuspocusProvider;
}

const raf = () =>
  new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));

async function mountEditor(doc: Y.Doc) {
  const fragment = doc.getXmlFragment(PATH);
  const view = render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div style={{ height: "600px", display: "flex", flexDirection: "column" }}>
        <SpecMdEditor
          fragment={fragment}
          provider={fakeProvider(doc)}
          self={{ name: "Tester", color: "#64b5f6" }}
          agentStreaming
          editable
        />
      </div>
    </OxygenUIThemeProvider>,
  );
  const pm = await waitFor(() => {
    const el = view.container.querySelector<HTMLElement>(".ProseMirror");
    if (!el) throw new Error("editor not mounted yet");
    return el;
  });
  return pm;
}

interface Metrics {
  flushes: number;
  finalBlocks: number;
  topLevelRemovals: number;
  rebuilt: { text: string; domNodes: number }[];
  allRemovals: number;
  allAdditions: number;
  agentSpanRemovals: number;
  agentSpanAdditions: number;
}

// Mount, stream DOC line-by-line via `write`, and measure DOM churn.
async function runStream(write: WriteFn): Promise<Metrics> {
  const doc = new Y.Doc();
  const pm = await mountEditor(doc);

  let nextId = 1;
  const idOf = new WeakMap<Element, number>();
  const tagId = (el: Element) => {
    let id = idOf.get(el);
    if (!id) {
      id = nextId++;
      idOf.set(el, id);
    }
    return id;
  };
  const snapshots: { id: number; text: string }[][] = [];
  const snap = () =>
    snapshots.push(
      [...pm.children].map((el) => ({
        id: tagId(el),
        text: (el.textContent ?? "").trim(),
      })),
    );

  let topLevelRemovals = 0;
  let allRemovals = 0;
  let allAdditions = 0;
  let agentSpanRemovals = 0;
  let agentSpanAdditions = 0;
  const isAgentSpan = (n: Node) =>
    n.nodeType === 1 && (n as Element).classList?.contains("agent-insertion");
  const mo = new MutationObserver((records) => {
    for (const r of records) {
      if (r.target === pm) topLevelRemovals += r.removedNodes.length;
      allRemovals += r.removedNodes.length;
      allAdditions += r.addedNodes.length;
      r.removedNodes.forEach((n) => isAgentSpan(n) && agentSpanRemovals++);
      r.addedNodes.forEach((n) => isAgentSpan(n) && agentSpanAdditions++);
    }
  });
  mo.observe(pm, { childList: true, subtree: true, characterData: true });

  // Exact StreamingDocWriter cadence: flush up to the last newline each step,
  // then a final flush of the trailing partial line (tool-input-end).
  let flushedLen = 0;
  let acc = "";
  let flush = 0;
  const lines = DOC.split("\n");
  for (let i = 0; i < lines.length; i++) {
    acc += lines[i] + (i < lines.length - 1 ? "\n" : "");
    const boundary = acc.lastIndexOf("\n") + 1;
    if (boundary > flushedLen) {
      write(doc, PATH, acc.slice(0, boundary), flush++);
      flushedLen = boundary;
      await raf();
      snap();
    }
  }
  if (acc.length > flushedLen) {
    write(doc, PATH, acc, flush++);
    await raf();
    snap();
  }
  mo.disconnect();

  const idsByText = new Map<string, Set<number>>();
  for (const s of snapshots) {
    for (const b of s) {
      if (!b.text) continue;
      if (!idsByText.has(b.text)) idsByText.set(b.text, new Set());
      idsByText.get(b.text)!.add(b.id);
    }
  }
  const rebuilt = [...idsByText.entries()]
    .filter(([, ids]) => ids.size > 1)
    .map(([text, ids]) => ({ text, domNodes: ids.size }));

  expect(pm.textContent).toContain("Closing paragraph eta omega.");
  doc.destroy();
  return {
    flushes: snapshots.length,
    finalBlocks: snapshots[snapshots.length - 1]?.length ?? 0,
    topLevelRemovals,
    rebuilt,
    allRemovals,
    allAdditions,
    agentSpanRemovals,
    agentSpanAdditions,
  };
}

afterEach(() => cleanup());

describe("SpecMdEditor streaming flicker", () => {
  // Characterization of the DOM-level cause of the streaming flicker. This is
  // NOT the fix gate — the fix lives in services/agents, which now writes
  // streamed new files UNMARKED (addFile is accept-by-default; see
  // doc-bundle.test.ts / streaming-add.test.ts). This test pins WHY that matters:
  // routing streamed content through the MARKED write re-renders the highlighted
  // tail on every line flush, churning the inline DOM many× an unmarked write.
  // If a future change sends streamed new files back through the marked path,
  // the churn (and the visible flicker) returns and this goes red.
  it("marking streamed content churns the inline DOM far more than an unmarked write", async () => {
    const marked = await runStream(writeMarked);
    const unmarked = await runStream(writeUnmarked);

    // Neither path rebuilds an already-rendered block (the churn is inline).
    expect(marked.rebuilt).toEqual([]);
    expect(unmarked.rebuilt).toEqual([]);

    // The unmarked stream (what the fixed agent path now does for a new file) is
    // essentially append-only.
    expect(unmarked.allRemovals).toBeLessThan(30);
    // Marking it — as chunks 2..N of a streamed new file used to be — churns the
    // tail heavily: many× the unmarked baseline. That excess IS the flicker.
    expect(marked.allRemovals).toBeGreaterThan(unmarked.allRemovals * 5);
  });
});
