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
import { describe, expect, it } from "vitest";
import { anchorAt, findAnchored, placePin, pointIn } from "./commentAnchor";

function surface(html: string): HTMLElement {
  const el = document.createElement("div");
  el.innerHTML = html;
  return el;
}

/** Lay an element out at a box (jsdom does no layout). */
function at(el: Element, box: { left: number; top: number; width: number; height: number }): void {
  el.getBoundingClientRect = () =>
    ({ ...box, x: box.left, y: box.top, right: box.left + box.width, bottom: box.top + box.height, toJSON: () => box }) as DOMRect;
}

describe("anchorAt", () => {
  it("anchors to a part by its data-anchor, from anywhere inside it", () => {
    const s = surface(`<section data-anchor="Claim"><dl><dd id="t">Priya Shah</dd></dl></section>`);
    const anchored = anchorAt(s.querySelector("#t"), s)!;
    expect(anchored.element).toBe(s.querySelector("section"));
    expect(anchored).toMatchObject({ label: "Claim", ref: { attr: "data-anchor", value: "Claim", nth: 0 } });
  });

  it("tells apart parts that share a name by their order", () => {
    const s = surface(`<li data-anchor="Step 2">Sam</li><h3 data-anchor="Step 2" id="h">Sam approves</h3>`);
    expect(anchorAt(s.querySelector("#h"), s)?.ref).toEqual({ attr: "data-anchor", value: "Step 2", nth: 1 });
  });

  it("anchors a diagram's node by its node id, even from its connection handle", () => {
    const s = surface(
      `<div data-id="expense-api"><div data-diagram-node-id="expense-api"><b>expense-api</b><i>service</i><div data-id="1-expense-api-top" id="h"></div></div></div>`,
    );
    expect(anchorAt(s.querySelector("#h"), s)).toMatchObject({
      label: "expense-api service",
      ref: { attr: "data-diagram-node-id", value: "expense-api", nth: 0 },
    });
  });

  it("anchors a part with no id by its words, and names it clipped", () => {
    const s = surface(`<p id="p">${"word ".repeat(30)}</p>`);
    const anchored = anchorAt(s.querySelector("#p"), s)!;
    expect(anchored.ref).toEqual({ tag: "p", words: "word ".repeat(30).trim(), nth: 0 });
    expect(anchored.label!.length).toBeLessThanOrEqual(60);
    expect(anchored.label!.endsWith("…")).toBe(true);
  });

  it("anchors a wordless part to its aria-label (a prototype hotspot, an icon's button)", () => {
    const s = surface(`<div role="button" aria-label="Go to Approved" id="hs"></div><button aria-label="Zoom in"><svg id="i"></svg></button>`);
    expect(anchorAt(s.querySelector("#hs"), s)).toMatchObject({ label: "Go to Approved", ref: { attr: "aria-label", value: "Go to Approved", nth: 0 } });
    expect(anchorAt(s.querySelector("#i"), s)?.label).toBe("Zoom in");
  });

  it("anchors to a canvas itself, with no name: its parts are pixels", () => {
    const s = surface(`<div aria-label="Drawing"><canvas></canvas><canvas id="c"></canvas></div>`);
    expect(anchorAt(s.querySelector("#c"), s)).toMatchObject({ label: null, ref: { tag: "canvas", words: "", nth: 1 } });
  });

  it("is none outside the artifact, or on its bare frame", () => {
    const s = surface(`<p>Words</p>`);
    expect(anchorAt(document.createElement("span"), s)).toBeNull();
    expect(anchorAt(s, s)).toBeNull();
  });
});

describe("placePin", () => {
  const frame = { left: 100, top: 50 };

  it("follows its element when the artifact is laid out anew", () => {
    const s = surface(`<div><div role="button" aria-label="Go to Approved"></div></div>`);
    const hotspot = s.querySelector("[aria-label]")!;
    at(hotspot, { left: 200, top: 300, width: 60, height: 20 });
    const anchored = anchorAt(hotspot, s)!;
    const anchor = { element: anchored.ref, ...pointIn(hotspot.getBoundingClientRect(), 230, 310) };
    expect(placePin(s, frame, anchor)).toEqual({ kind: "at", left: 130, top: 260 });

    // A design change redraws the screen: the button is a new element, lower down.
    s.firstElementChild!.innerHTML = `<table><tr><td>Hotel</td></tr></table><div role="button" aria-label="Go to Approved"></div>`;
    at(s.querySelector("td")!, { left: 200, top: 300, width: 300, height: 20 });
    at(s.querySelector("[aria-label]")!, { left: 200, top: 420, width: 60, height: 20 });
    expect(placePin(s, frame, anchor)).toEqual({ kind: "at", left: 130, top: 380 });
  });

  it("is gone when its element no longer exists, not placed on another", () => {
    const s = surface(`<div role="button" aria-label="Go to Approved"></div><div role="button" aria-label="Go to Rejected"></div>`);
    const anchor = { element: { attr: "aria-label", value: "Go to Approved", nth: 0 }, x: 0.5, y: 0.5 };
    s.querySelector('[aria-label="Go to Approved"]')!.remove();
    expect(placePin(s, frame, anchor)).toEqual({ kind: "gone" });
    expect(placePin(s, frame, { ...anchor, element: { tag: "p", words: "Reason", nth: 0 } })).toEqual({ kind: "gone" });
  });

  it("is hidden while its element is collapsed or outside a viewer's clipped area", () => {
    const s = surface(`<div id="clip" style="overflow: hidden"><p>Approve</p></div>`);
    const anchor = { element: { tag: "p", words: "Approve", nth: 0 }, x: 0.5, y: 0.5 };
    at(s.querySelector("p")!, { left: 0, top: 0, width: 0, height: 0 });
    expect(placePin(s, frame, anchor)).toEqual({ kind: "hidden" });
    at(s.querySelector("#clip")!, { left: 0, top: 0, width: 400, height: 300 });
    at(s.querySelector("p")!, { left: 0, top: 500, width: 100, height: 20 });
    expect(placePin(s, frame, anchor)).toEqual({ kind: "hidden" });
  });

  it("puts a comment on the artifact as a whole at its point on the artifact", () => {
    const s = surface(`<p>Words</p>`);
    at(s, { left: 100, top: 50, width: 400, height: 200 });
    expect(placePin(s, frame, { element: null, x: 0.5, y: 0.05 })).toEqual({ kind: "at", left: 200, top: 10 });
  });
});

describe("findAnchored", () => {
  it("finds the element by its name and order", () => {
    const s = surface(`<li data-anchor="Step 2">a</li><h3 data-anchor="Step 2">b</h3>`);
    expect(findAnchored(s, { attr: "data-anchor", value: "Step 2", nth: 1 })).toBe(s.querySelector("h3"));
    expect(findAnchored(s, { attr: "data-anchor", value: "Step 2", nth: 2 })).toBeNull();
  });
});

describe("pointIn", () => {
  it("is the point as fractions of the box, kept inside it", () => {
    const box = { left: 100, top: 50, width: 200, height: 100 };
    expect(pointIn(box, 150, 75)).toEqual({ x: 0.25, y: 0.25 });
    expect(pointIn(box, 400, 0)).toEqual({ x: 1, y: 0 });
  });
});
