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

import type { CommentAnchor, ElementRef } from "./api/designModel";

// Comments are anchored to the artifact's elements. The artifacts are drawn
// by viewers this app does not own (packages/ui, used unchanged), so a
// comment cannot be written into their markup: it keeps how to find its
// element again in what they render, and its pin is placed from that
// element's box whenever the artifact is laid out. An element that is no
// longer there leaves the comment unanchored, never pinned somewhere else.
//
// What names an element, first found wins:
//  - an id its markup gives it, the nearest of each kind in this order: this
//    app's own views mark their parts `data-anchor`; the cell diagram marks
//    its nodes `data-diagram-node-id` (so a click on a node's connection
//    handle is still the node), and React Flow, under it, its edges `data-id`;
//  - its own words: the contract, design and acceptance views carry no ids,
//    so an operation or a scenario is known by what it says;
//  - an `aria-label`: the prototype's hotspots ("Go to Approved"), an icon's
//    button;
//  - a canvas is itself: it draws its parts in pixels, so the prototype's
//    screens (Excalidraw) are one element, and a comment elsewhere on them
//    is a point on the canvas.
// Several elements can share a name (a flow step is marked in each of the
// flow's lists): `nth` says which one, in document order.

const IDS = ["data-anchor", "data-diagram-node-id", "data-id"] as const;

const MAX_LABEL = 60;

/** The overflow values that cut off what lies outside the box. */
const CLIPS = /\b(hidden|clip|auto|scroll)\b/;

/** An element's words: its text, each run apart (a node's name and its kind are two words, not one). */
function wordsOf(el: Element): string {
  const runs: string[] = [];
  const walker = el.ownerDocument.createTreeWalker(el, NodeFilter.SHOW_TEXT);
  for (let node = walker.nextNode(); node; node = walker.nextNode()) runs.push(node.nodeValue ?? "");
  return runs.join(" ").replace(/\s+/g, " ").trim();
}

function clip(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > MAX_LABEL ? `${flat.slice(0, MAX_LABEL - 1).trimEnd()}…` : flat;
}

type Name = { attr: string; value: string } | { tag: string; words: string };

/** Every element under `root` with that name, in document order. */
function named(root: Element, name: Name): Element[] {
  if ("attr" in name) {
    return [...root.querySelectorAll(`[${name.attr}]`)].filter((el) => el.getAttribute(name.attr) === name.value);
  }
  return [...root.querySelectorAll(name.tag)].filter((el) => wordsOf(el) === name.words);
}

/** The element a click in the artifact is about, how to find it again, and what it is called. */
export interface Anchored {
  element: Element;
  ref: ElementRef;
  /** Null when it has no name a person could read (a canvas). */
  label: string | null;
}

function anchored(root: Element, element: Element, name: Name, label: string | null): Anchored {
  return { element, ref: { ...name, nth: Math.max(0, named(root, name).indexOf(element)) }, label: label && clip(label) };
}

/**
 * The element under `target` a comment anchors to, looking up from it to
 * (not past) `root`, the artifact. Null when the target is not in the
 * artifact, or is only its bare frame: the comment is then on the artifact
 * as a whole.
 */
export function anchorAt(target: Element | null, root: Element): Anchored | null {
  if (!target || target === root || !root.contains(target)) return null;
  if (target.tagName === "CANVAS") return anchored(root, target, { tag: "canvas", words: "" }, null);
  const up: Element[] = [];
  for (let node: Element | null = target; node && node !== root; node = node.parentElement) up.push(node);

  for (const attr of IDS) {
    for (const node of up) {
      const value = node.getAttribute(attr)?.trim();
      if (!value) continue;
      const label = attr === "data-anchor" ? value : (node.getAttribute("aria-label") ?? (wordsOf(node) || value));
      return anchored(root, node, { attr, value }, label);
    }
  }
  const words = wordsOf(target);
  if (words) return anchored(root, target, { tag: target.tagName.toLowerCase(), words }, words);
  for (const node of up) {
    const value = node.getAttribute("aria-label")?.trim();
    if (value) return anchored(root, node, { attr: "aria-label", value }, value);
  }
  // Wordless and unnamed (a gap between parts): the nearest part with words around it.
  const around = up.find((node) => wordsOf(node));
  return around ? anchored(root, around, { tag: around.tagName.toLowerCase(), words: wordsOf(around) }, wordsOf(around)) : null;
}

/** A comment's element in the artifact as it is drawn now, or null when it is gone. */
export function findAnchored(root: Element, ref: ElementRef): Element | null {
  const { nth, ...name } = ref;
  return named(root, name)[nth] ?? null;
}

/** A client point as fractions of a box, kept inside it. */
export function pointIn(
  box: { left: number; top: number; width: number; height: number },
  clientX: number,
  clientY: number,
): { x: number; y: number } {
  const clamp = (v: number) => Math.min(1, Math.max(0, v));
  return {
    x: box.width > 0 ? clamp((clientX - box.left) / box.width) : 0,
    y: box.height > 0 ? clamp((clientY - box.top) / box.height) : 0,
  };
}

/**
 * Where a pin goes now: at a point relative to `frame` (the box the pins are
 * drawn in); hidden while its element is there but not shown (collapsed, or
 * scrolled or panned out of a viewer's clipped area); gone when its element
 * no longer exists.
 */
export type PinPlace = { kind: "at"; left: number; top: number } | { kind: "hidden" } | { kind: "gone" };

export function placePin(
  root: Element,
  frame: { left: number; top: number },
  anchor: Pick<CommentAnchor, "element" | "x" | "y">,
): PinPlace {
  const element = anchor.element ? findAnchored(root, anchor.element) : root;
  if (!element) return { kind: "gone" };
  const box = element.getBoundingClientRect();
  if (box.width === 0 && box.height === 0) return { kind: "hidden" };
  const x = box.left + anchor.x * box.width;
  const y = box.top + anchor.y * box.height;
  for (let node = element.parentElement; node && node !== root; node = node.parentElement) {
    const style = getComputedStyle(node);
    if (!CLIPS.test(`${style.overflow} ${style.overflowX} ${style.overflowY}`)) continue;
    const shown = node.getBoundingClientRect();
    if (x < shown.left || x > shown.right || y < shown.top || y > shown.bottom) return { kind: "hidden" };
  }
  return { kind: "at", left: x - frame.left, top: y - frame.top };
}
