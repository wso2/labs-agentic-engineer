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
import { findEnvironment, isLast, labelOf, moveEnvironment, stepsFor, type EnvironmentInfo } from "./environments";

const env = (over: Partial<EnvironmentInfo> & { name: string }): EnvironmentInfo => ({
  displayName: over.name,
  isProduction: false,
  validation: "off",
  position: 0,
  ...over,
});

describe("stepsFor", () => {
  it("gives a validating, non-final environment three steps, numbered in order", () => {
    const steps = stepsFor(env({ name: "development", validation: "on", position: 0, promotesTo: "staging" }));
    expect(steps.map((s) => [s.kind, s.index])).toEqual([
      ["deployment", 1],
      ["validation", 2],
      ["promote", 3],
    ]);
    expect(steps[2]?.promotesTo).toBe("staging");
  });

  it("drops the validation step when the environment does not validate", () => {
    const steps = stepsFor(env({ name: "staging", validation: "off", promotesTo: "uat" }));
    expect(steps.map((s) => [s.kind, s.index])).toEqual([
      ["deployment", 1],
      ["promote", 2],
    ]);
  });

  it("drops the promote step on the last environment — nothing follows it", () => {
    const steps = stepsFor(env({ name: "production", validation: "on", isProduction: true }));
    expect(steps.map((s) => [s.kind, s.index])).toEqual([
      ["deployment", 1],
      ["validation", 2],
    ]);
  });

  it("leaves a lone environment with one step", () => {
    expect(stepsFor(env({ name: "development" })).map((s) => s.kind)).toEqual(["deployment"]);
  });
});

describe("isLast", () => {
  it("is the absence of a promotion target, not a guess from isProduction", () => {
    expect(isLast(env({ name: "production", isProduction: true }))).toBe(true);
    // A production environment that still promotes onward is NOT last.
    expect(isLast(env({ name: "production", isProduction: true, promotesTo: "dr" }))).toBe(false);
  });
});

describe("labelOf", () => {
  it("uses the display name, and falls back to the raw name when the environment is unknown", () => {
    expect(labelOf(env({ name: "staging", displayName: "Staging" }), "staging")).toBe("Staging");
    expect(labelOf(undefined, "staging")).toBe("staging");
  });
});

describe("findEnvironment", () => {
  it("finds an environment by name, and answers undefined for one the pipeline does not have", () => {
    const list = [env({ name: "development" }), env({ name: "staging" })];
    expect(findEnvironment(list, "staging")?.name).toBe("staging");
    expect(findEnvironment(list, "uat")).toBeUndefined();
  });
});

describe("moveEnvironment", () => {
  const list = [env({ name: "a" }), env({ name: "b" }), env({ name: "c" })];

  it("moves a card forward and backward without mutating the input", () => {
    expect(moveEnvironment(list, 0, 2).map((e) => e.name)).toEqual(["b", "c", "a"]);
    expect(moveEnvironment(list, 2, 0).map((e) => e.name)).toEqual(["c", "a", "b"]);
    expect(list.map((e) => e.name)).toEqual(["a", "b", "c"]);
  });

  it("renumbers position and rewires promotesTo to the new neighbour", () => {
    const moved = moveEnvironment(list, 0, 2);
    expect(moved.map((e) => e.position)).toEqual([0, 1, 2]);
    expect(moved.map((e) => e.promotesTo)).toEqual(["c", "a", undefined]);
  });

  it("is a no-op for an unchanged or out-of-range move", () => {
    expect(moveEnvironment(list, 1, 1).map((e) => e.name)).toEqual(["a", "b", "c"]);
    expect(moveEnvironment(list, 5, 0).map((e) => e.name)).toEqual(["a", "b", "c"]);
  });
});
