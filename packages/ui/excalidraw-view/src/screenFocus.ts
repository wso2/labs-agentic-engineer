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


import type { ExcalidrawElement } from "@aep/excalidraw-dsl";

/**
 * Per-screen grouping over a compiled grid scene. The DSL compiler stamps
 * every element with `customData.screen`, which is what makes these questions
 * answerable without geometry guesswork: which elements are the first
 * screen's (focus it on open), and which elements belong to the screens the
 * compiler reported as changed (follow the agent's work).
 *
 * Deciding WHICH screens changed is not done here. The compiler knows the
 * screen structure and reports it (`compileWireframes` → `changedScreens`);
 * this module only maps names to elements and frames them.
 */

/**
 * The part of a compiled element this module reads. Every function here is
 * generic over it, so callers keep their own element type end-to-end (the
 * viewer passes `ExcalidrawElement`s straight through to `scrollToContent`)
 * while tests can pass a minimal stand-in.
 */
export type FocusableElement = Pick<ExcalidrawElement, "id" | "y" | "customData"> &
  Partial<Pick<ExcalidrawElement, "height">>;

function screenOf(el: FocusableElement): string | null {
  const s = el?.customData?.screen;
  return typeof s === "string" && s.length > 0 ? s : null;
}

/** The elements belonging to any of `names`, in scene order. */
export function elementsOfScreens<T extends FocusableElement>(
  elements: readonly T[],
  names: readonly string[],
): T[] {
  if (names.length === 0) return [];
  const want = new Set(names);
  return elements.filter((el) => {
    const s = screenOf(el);
    return s !== null && want.has(s);
  });
}

/**
 * The screen highest on the canvas — screens stack in one column, so this is
 * the first one the author declared. Null when nothing is tagged.
 */
export function firstScreenName(elements: readonly FocusableElement[]): string | null {
  let best: { name: string; y: number } | null = null;
  for (const el of elements) {
    const s = screenOf(el);
    if (s === null) continue;
    const y = typeof el.y === "number" ? el.y : Number.POSITIVE_INFINITY;
    if (best === null || y < best.y) best = { name: s, y };
  }
  return best?.name ?? null;
}

/**
 * What to bring into view when a wireframe opens: the whole first screen,
 * plus the top slice of the second so its title (and a sliver of frame) shows
 * beneath as the cue that there is more below. Fitting the first screen ALONE
 * only shows that cue by accident — a wide panel fits the screen at a zoom
 * whose height ends exactly at its bottom edge — so the peek band is part of
 * the target box on purpose. Falls back to the first screen when there is no
 * second, and to nothing when the scene carries no screen tags.
 */
export function openingFocusElements<T extends FocusableElement>(elements: readonly T[]): T[] {
  const first = firstScreenName(elements);
  if (!first) return [];
  const firstEls = elementsOfScreens(elements, [first]);

  // The second screen is the tagged screen whose topmost element is the
  // lowest one still ABOVE nothing else — i.e. the next in canvas order.
  const firstBottom = Math.max(...firstEls.map((e) => (e.y ?? 0) + (e.height ?? 0)));
  let second: { name: string; top: number } | null = null;
  for (const el of elements) {
    const s = screenOf(el);
    if (s === null || s === first) continue;
    const y = typeof el.y === "number" ? el.y : Number.POSITIVE_INFINITY;
    if (second === null || y < second.top) second = { name: s, top: y };
  }
  if (!second) return firstEls;

  // Peek band: from the second screen's top down to a fixed depth — enough
  // for its title block and the first ~10% of its frame.
  const PEEK = firstBottom - (firstEls.length ? Math.min(...firstEls.map((e) => e.y ?? 0)) : 0);
  const peekDepth = Math.max(80, PEEK * 0.12);
  // Only elements that END inside the band count: the second screen's frame
  // rectangle STARTS in the band but is a full screen tall, and letting it in
  // would fit both screens — the very thing this function exists to avoid.
  const bandBottom = second.top + peekDepth;
  const band = elementsOfScreens(elements, [second.name]).filter(
    (e) => typeof e.y === "number" && e.y + (e.height ?? 0) <= bandBottom,
  );
  // A zero-size marker at the band's bottom edge pins the fitted box to that
  // depth regardless of which second-screen elements happen to end inside it,
  // so a sliver of the second frame is always in view. It is only ever handed
  // to scrollToContent for its bounds — never added to the scene.
  const anchor = firstEls[0]!;
  const marker = { ...anchor, id: "__peek-marker", y: bandBottom, height: 0, width: 0 };
  return [...firstEls, ...band, marker];
}

/** The slice of Excalidraw's appState the viewport maths needs. */
export interface ViewportState {
  scrollX: number;
  scrollY: number;
  zoom: { value: number };
}

/**
 * Which screen the reader is looking at: the one whose frame contains the
 * centre of the viewport, or — when the centre falls in the gap between
 * frames — the nearest one. Inverts Excalidraw's `viewport = (scene + scroll)
 * × zoom` to recover the scene point under the viewport's middle. Null when
 * the scene carries no screen tags.
 */
export function screenAtViewportCenter(
  elements: readonly (FocusableElement & { x?: number; width?: number })[],
  view: ViewportState,
  viewportWidth: number,
  viewportHeight: number,
): string | null {
  const z = view.zoom.value || 1;
  const cx = viewportWidth / 2 / z - view.scrollX;
  const cy = viewportHeight / 2 / z - view.scrollY;

  // Each screen's frame as the union of its elements.
  const frames = new Map<string, { top: number; bottom: number }>();
  for (const el of elements) {
    const s = screenOf(el);
    if (s === null || typeof el.y !== "number") continue;
    const top = el.y;
    const bottom = el.y + (el.height ?? 0);
    const f = frames.get(s);
    if (!f) frames.set(s, { top, bottom });
    else {
      if (top < f.top) f.top = top;
      if (bottom > f.bottom) f.bottom = bottom;
    }
  }
  if (frames.size === 0) return null;

  let best: { name: string; distance: number } | null = null;
  for (const [name, f] of frames) {
    // Zero when the centre is inside the frame; otherwise the vertical gap.
    const distance = cy < f.top ? f.top - cy : cy > f.bottom ? cy - f.bottom : 0;
    if (best === null || distance < best.distance) best = { name, distance };
  }
  void cx; // screens stack in one column; horizontal position never disambiguates
  return best?.name ?? null;
}

/**
 * Given what the compiler says changed, which screens should the camera
 * actually move to — if any.
 *
 * - The reader's own screen is among the changed → hold still. The edit is
 *   under their nose; moving would only twitch the camera.
 * - A sweep is in progress → hold still. A sweep (the same change applied to
 *   every screen) lands one flush per screen, and at the first flush it is
 *   indistinguishable from a single edit — so the camera goes there. But a
 *   SECOND consecutive flush changing a DIFFERENT single screen is the sweep's
 *   signature; from then on the camera holds, rather than touring every
 *   screen and stranding the reader on the last one. The caller passes what
 *   the previous flush changed; a repeat of the SAME screen is one edit still
 *   being written and is followed.
 * - Otherwise → the changed screens.
 */
export function screensToFollow(
  changed: readonly string[],
  currentScreen: string | null,
  previouslyChanged: readonly string[] | null,
): string[] {
  if (changed.length === 0) return [];
  if (currentScreen !== null && changed.includes(currentScreen)) return [];
  const isSweep =
    previouslyChanged !== null &&
    previouslyChanged.length === 1 &&
    changed.length === 1 &&
    previouslyChanged[0] !== changed[0];
  if (isSweep) return [];
  return [...changed];
}
