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
import { deriveLiveWireframe, deriveWireframeScene, focusTargets } from "./deriveWireframe";

const dsl = `screen Home
  heading "Welcome"
  button "Go" primary`;

describe("deriveWireframeScene", () => {
  it("compiles a wireframes .dsl into an excalidraw scene with elements", () => {
    const json = deriveWireframeScene("design/components/web/wireframes.dsl", dsl);
    expect(json).not.toBeNull();
    const scene = JSON.parse(json!);
    expect(Array.isArray(scene.elements)).toBe(true);
    expect(scene.elements.length).toBeGreaterThan(0);
  });

  it("returns null (not throw) on DSL that does not compile", () => {
    expect(deriveWireframeScene("design/components/web/wireframes.dsl", "garbage {{{")).toBeNull();
  });

  it("returns null on empty input", () => {
    expect(deriveWireframeScene("x/wireframes.dsl", "")).toBeNull();
  });

  // Streaming: the collab writer flushes whole lines, so a mid-turn source is
  // a PREFIX of the final file. Every line-boundary prefix must compile to the
  // screens written so far — that is what draws the wireframe live.
  it("compiles every line-boundary prefix of a streaming source", () => {
    const full = `screen Catalog "Shoppers browse products"
  navbar "Shop"
  row
    heading "Browse products"
    right
    button "View cart" primary -> Cart
  table "Product | Price"
    row "Mug | $18"

screen Cart "Review and check out"
  navbar "Shop"
  heading "Your cart"
`;
    const lines = full.split("\n");
    for (let i = 1; i <= lines.length; i++) {
      const prefix = lines.slice(0, i).join("\n");
      if (prefix.trim().length === 0) continue;
      const json = deriveWireframeScene("x/wireframes.dsl", prefix);
      expect(json, `prefix of ${i} lines should compile`).not.toBeNull();
    }
    // The full source renders both screens.
    const scene = JSON.parse(deriveWireframeScene("x/wireframes.dsl", full)!);
    const texts = scene.elements.filter((e: { type: string }) => e.type === "text");
    expect(texts.some((t: { text?: string }) => t.text === "Catalog")).toBe(true);
    expect(texts.some((t: { text?: string }) => t.text === "Cart")).toBe(true);
  });
});

describe("deriveLiveWireframe", () => {
  const path = "design/components/web/wireframes.dsl";
  const two = `screen Home\n  heading "Welcome"\nscreen Cart\n  button "Pay"\n`;

  it("compiles and reports no changed screens when there is nothing to compare against", () => {
    const r = deriveLiveWireframe(path, two, null);
    expect(r).not.toBeNull();
    expect(r!.changedScreens).toEqual([]);
    expect(JSON.parse(r!.json).elements.length).toBeGreaterThan(0);
  });

  it("reports only the screen the edit touched, given the previous compile", () => {
    const first = deriveLiveWireframe(path, two, null);
    const second = deriveLiveWireframe(path, two.replace('button "Pay"', 'button "Pay" primary'), first);
    expect(second!.changedScreens).toEqual(["Cart"]);
  });

  it("returns null for an uncompilable source, leaving the caller's held compile in place", () => {
    const first = deriveLiveWireframe(path, two, null);
    expect(deriveLiveWireframe(path, "garbage {{{", first)).toBeNull();
    expect(deriveLiveWireframe(path, "", first)).toBeNull();
  });

  it("treats a domain-model DSL as a plain compile with no screens to report", () => {
    const r = deriveLiveWireframe("design/erd.dsl", `entity User\n  id: uuid\n`, null);
    expect(r).not.toBeNull();
    expect(r!.changedScreens).toEqual([]);
  });
});

describe("focusTargets", () => {
  const path = "design/components/web/wireframes.dsl";
  const three = `screen One\n  heading "A"\nscreen Two\n  button "Go"\nscreen Three\n  heading "C"\n`;
  const filler = Array.from({ length: 20 }, (_, i) => `  text "line ${i}"`).join("\n");

  it("follows a surgical edit live: one screen changed out of three is a target", () => {
    const base = deriveLiveWireframe(path, three, null)!;
    const edited = deriveLiveWireframe(path, three.replace('heading "A"', `heading "A"\n${filler}`), base)!;
    expect(focusTargets(edited, base)).toEqual(["One"]);
  });

  it("holds still mid-rewrite: a flush missing most screens is not a target", () => {
    // An agent rewriting the file emits a flush where Two and Three are not
    // yet written back. The compiler honestly reports all three as changed;
    // panning there would fit the whole board.
    const base = deriveLiveWireframe(path, three, null)!;
    const midRewrite = deriveLiveWireframe(path, `screen One\n  heading "A"\n${filler}\n`, base)!;
    expect(midRewrite.changedScreens).toEqual(["One", "Two", "Three"]);
    expect(focusTargets(midRewrite, base)).toEqual([]);
  });

  it("measures breadth against the larger compile, so a shrunken flush cannot look narrow", () => {
    // Mid-rewrite the new compile holds ONE screen; measured against itself,
    // 'One changed of one' would wrongly count as a narrow edit.
    const base = deriveLiveWireframe(path, three, null)!;
    const shrunken = deriveLiveWireframe(path, `screen One\n  heading "A"\n${filler}\n`, base)!;
    expect(Object.keys(shrunken.fingerprints)).toEqual(["One"]);
    expect(focusTargets(shrunken, base)).toEqual([]);
  });

  it("reports nothing when nothing changed", () => {
    const base = deriveLiveWireframe(path, three, null)!;
    const same = deriveLiveWireframe(path, three, base)!;
    expect(focusTargets(same, base)).toEqual([]);
  });
});
