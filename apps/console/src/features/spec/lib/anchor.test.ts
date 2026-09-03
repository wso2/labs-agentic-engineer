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
import {
  EXCERPT_MAX,
  anchorFor,
  excerpt,
  headingPath,
  selectedBlocks,
} from "./anchor";
import type { DocBlock } from "./docBlocks";
import type { Anchor } from "./anchor";
import type { components } from "../../../generated/aep-api";

// Positions are contiguous and plausible rather than real ProseMirror offsets:
// every rule here reads `from`/`to` as opaque numbers, which is the point of
// keeping this module editor-free.
function build(
  specs: Array<[DocBlock["kind"], string, (number | undefined)?]>,
): DocBlock[] {
  let pos = 0;
  return specs.map(([kind, text, level]) => {
    const from = pos;
    const to = from + text.length + 2;
    pos = to;
    return {
      kind,
      text,
      emphasis: [],
      from,
      to,
      contentEnd: to - 1,
      ...(level === undefined ? {} : { level }),
    };
  });
}

const PRD = build([
  ["heading", "Solution", 2],
  ["paragraph", "A Slack bot collects lunch orders on a schedule.", undefined],
  ["heading", "Slack integration", 3],
  ["listItem", "The bot posts one message per round.", undefined],
  ["heading", "Open Questions", 2],
  ["listItem", "Which Slack workspace does the bot post to, and who owns the app registration for it?", undefined],
]);

describe("excerpt", () => {
  it("returns a short block whole", () => {
    expect(excerpt("Rounds close automatically.")).toBe("Rounds close automatically.");
  });

  it("collapses the hard wraps a markdown source carries", () => {
    expect(excerpt("All API access through\n  the generated client.")).toBe(
      "All API access through the generated client.",
    );
  });

  it("bounds a long block at a word boundary and never exceeds the max", () => {
    const long =
      "Which Slack workspace does the bot post to, and who owns the app registration for it?";
    const cut = excerpt(long);
    expect(cut.length).toBeLessThanOrEqual(EXCERPT_MAX);
    expect(long.startsWith(cut)).toBe(true);
    expect(cut.endsWith(" ")).toBe(false);
  });

  it("appends no ellipsis — the name is a string the agent searches for", () => {
    expect(excerpt("x".repeat(200))).not.toContain("…");
  });

  it("cuts mid-word rather than yielding a stub when one word fills the bound", () => {
    // The only space sits early, so honouring it would return three characters.
    const text = `abc ${"z".repeat(200)}`;
    expect(excerpt(text).length).toBe(EXCERPT_MAX);
  });

  it("drops trailing punctuation left dangling by the cut", () => {
    const cut = excerpt("The bot posts one message per round, and then it waits for replies", 36);
    expect(cut.endsWith(",")).toBe(false);
  });
});

describe("selectedBlocks", () => {
  it("takes the whole paragraph a partial drag touched", () => {
    const para = PRD[1]!;
    const nodes = selectedBlocks(PRD, para.from + 4, para.from + 9);
    expect(nodes).toEqual([para]);
  });

  it("takes every block a multi-block drag crosses", () => {
    const nodes = selectedBlocks(PRD, PRD[1]!.from + 2, PRD[3]!.from + 2);
    expect(nodes.map((b) => b.text)).toEqual([PRD[1]!.text, PRD[2]!.text, PRD[3]!.text]);
  });

  it("does not take the next block when the drag ends exactly on its start", () => {
    const nodes = selectedBlocks(PRD, PRD[1]!.from, PRD[2]!.from);
    expect(nodes).toEqual([PRD[1]!]);
  });

  it("expands a heading selected alone to the section it owns", () => {
    const nodes = selectedBlocks(PRD, PRD[2]!.from + 1, PRD[2]!.from + 3);
    expect(nodes.map((b) => b.text)).toEqual(["Slack integration", "The bot posts one message per round."]);
  });

  it("stops a section at the next heading of the same level", () => {
    const nodes = selectedBlocks(PRD, PRD[0]!.from + 1, PRD[0]!.from + 3);
    expect(nodes.map((b) => b.text)).not.toContain("Which Slack workspace does the bot post to, and who owns the app registration for it?");
    expect(nodes.at(-1)!.text).toBe("The bot posts one message per round.");
  });

  it("takes the containing block for a collapsed caret", () => {
    const nodes = selectedBlocks(PRD, PRD[1]!.from + 3, PRD[1]!.from + 3);
    expect(nodes).toEqual([PRD[1]!]);
  });

  it("selects one block, not two, for a caret at a shared boundary", () => {
    // A synthetic caret exactly at blockA.to === blockB.from — a real editor
    // never produces one, but this module is deliberately editor-free. Two
    // PARAGRAPHS, so no heading expansion muddies the count.
    const pair = build([
      ["paragraph", "The first paragraph."],
      ["paragraph", "The second paragraph."],
    ]);
    const nodes = selectedBlocks(pair, pair[0]!.to, pair[0]!.to);
    expect(nodes).toEqual([pair[1]]);
  });

  it("resolves to nothing past the end of the document", () => {
    const end = PRD.at(-1)!.to;
    expect(selectedBlocks(PRD, end + 10, end + 20)).toEqual([]);
  });
});

describe("headingPath", () => {
  it("names the enclosing section, root-first", () => {
    expect(headingPath(PRD, 3)).toBe("Solution › Slack integration");
  });

  it("excludes a heading's own text from its context", () => {
    expect(headingPath(PRD, 2)).toBe("Solution");
  });

  it("is empty above the first heading", () => {
    const flat = build([["paragraph", "Preamble before any heading."]]);
    expect(headingPath(flat, 0)).toBe("");
  });

  it("pops back out when a sibling heading closes a deeper one", () => {
    expect(headingPath(PRD, 5)).toBe("Open Questions");
  });
});

describe("anchorFor", () => {
  it("names one selected block with its kind and section", () => {
    expect(anchorFor("specs/requirements/PRD.md", PRD, PRD[5]!.from + 2, PRD[5]!.from + 6)).toEqual({
      file: "specs/requirements/PRD.md",
      nodes: [
        {
          name: "Which Slack workspace does the bot post to, and who owns the app registration",
          kind: "list item",
          context: "Open Questions",
        },
      ],
    });
  });

  it("keeps document order across a multi-block selection", () => {
    const anchor = anchorFor("a.md", PRD, PRD[1]!.from + 1, PRD[3]!.from + 1);
    expect(anchor?.nodes.map((n) => n.kind)).toEqual(["paragraph", "heading", "list item"]);
  });

  it("omits context entirely rather than sending an empty one", () => {
    const flat = build([["paragraph", "No heading above this."]]);
    expect(anchorFor("a.md", flat, 0, 5)?.nodes[0]).not.toHaveProperty("context");
  });

  it("drops a blank block, which names nothing the agent could find", () => {
    const withBlank = build([
      ["paragraph", "Real text."],
      ["paragraph", "   "],
    ]);
    const anchor = anchorFor("a.md", withBlank, 0, withBlank[1]!.to);
    expect(anchor?.nodes).toHaveLength(1);
  });

  it("is null when the selection resolves to nothing", () => {
    expect(anchorFor("a.md", [], 0, 5)).toBeNull();
  });
});

// The console builds this shape; the contract carries it. They are two
// declarations of one thing, so a drift in either direction has to fail HERE —
// at compile time, in the module that owns the shape — rather than at the send
// site, where it would read as a call-signature problem.
describe("the wire shape", () => {
  it("stays interchangeable with the contract's TurnAnchor", () => {
    const fromWire: components["schemas"]["TurnAnchor"] = {
      file: "specs/requirements/PRD.md",
      nodes: [{ name: "Rounds close automatically.", kind: "paragraph", context: "Solution" }],
    };
    const local: Anchor = fromWire;
    const backToWire: components["schemas"]["TurnAnchor"] = local;
    expect(backToWire.nodes[0]?.kind).toBe("paragraph");
  });
});
