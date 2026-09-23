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
import { exampleSpecs } from "../../../examples/index.js";
import { genUiActions, genUiComponents } from "../../catalog/index.js";
import { jsonRenderCatalog } from "./catalog.js";
import { genUiSystemPrompt, validateGenUiSpec } from "./spec.js";

const leaf = (type: string, props: Record<string, unknown>) => ({
  root: "a",
  elements: { a: { type, props, children: [] } },
});

describe("jsonRenderCatalog", () => {
  it("carries every catalog component and action", () => {
    expect([...jsonRenderCatalog.componentNames].sort()).toEqual(
      Object.keys(genUiComponents).sort(),
    );
    expect([...jsonRenderCatalog.actionNames].sort()).toEqual(
      Object.keys(genUiActions).sort(),
    );
  });
});

describe("validateGenUiSpec", () => {
  it.each(Object.entries(exampleSpecs))("accepts the %s example", (_, spec) => {
    const result = validateGenUiSpec(spec);
    expect(result).toEqual({ ok: true, spec });
  });

  it("keeps action bindings on the returned spec", () => {
    const result = validateGenUiSpec(exampleSpecs["Dependency approval"]);
    expect(result.ok && result.spec.elements["approve"]).toHaveProperty(
      "on.press.action",
      "approveDependency",
    );
  });

  it("rejects a component type outside the catalog", () => {
    const result = validateGenUiSpec(leaf("Iframe", { src: "https://x" }));
    expect(result.ok).toBe(false);
  });

  it("rejects props that fail the component schema", () => {
    const result = validateGenUiSpec(leaf("Progress", { value: 250 }));
    expect(result.ok).toBe(false);
  });

  it("rejects a deployment URL that is not http(s)", () => {
    const result = validateGenUiSpec(
      leaf("Deployments", {
        environments: [
          { environment: "Development", state: "deployed", url: "javascript:alert(1)" },
        ],
      }),
    );
    expect(result.ok).toBe(false);
  });

  it("names the element and prop that failed", () => {
    const result = validateGenUiSpec(leaf("Progress", { value: 250 }));
    expect(!result.ok && result.issues).toEqual([
      "a.props.value: Too big: expected number to be <=100",
    ]);
  });

  it("accepts a $-expression where a prop value is expected", () => {
    const result = validateGenUiSpec(
      leaf("Progress", { value: { $state: "/delivery/percent" } }),
    );
    expect(result.ok).toBe(true);
  });

  it("rejects a component missing a required prop", () => {
    expect(validateGenUiSpec(leaf("Card", {})).ok).toBe(false);
  });

  it("rejects a child reference that does not resolve", () => {
    const result = validateGenUiSpec({
      root: "a",
      elements: { a: { type: "Card", props: { title: "T" }, children: ["ghost"] } },
    });
    expect(result).toMatchObject({ ok: false });
    expect(!result.ok && result.issues.join(" ")).toMatch(/ghost/);
  });

  it("rejects input that is not a spec at all", () => {
    expect(validateGenUiSpec("not a spec").ok).toBe(false);
  });
});

describe("genUiSystemPrompt", () => {
  it("describes every component, its events, and every action", () => {
    const prompt = genUiSystemPrompt();
    for (const name of Object.keys(genUiComponents)) expect(prompt).toContain(name);
    for (const name of Object.keys(genUiActions)) expect(prompt).toContain(name);
    expect(prompt).toContain("Events: press.");
  });

  it("appends surface-specific rules", () => {
    expect(genUiSystemPrompt({ customRules: ["Keep it to one card."] })).toContain(
      "Keep it to one card.",
    );
  });
});
