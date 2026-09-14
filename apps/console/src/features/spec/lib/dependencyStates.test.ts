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
import type { components } from "../../../generated/aep-api";
import { computeDependencyStates, dependencyTodo } from "./dependencyStates";

type Dependency = components["schemas"]["Dependency"];

const ext = (over: Partial<Dependency>): Dependency => ({ kind: "external", name: "stripe", ...over });

describe("dependencyTodo — the one thing the user must do", () => {
  it("names the step by status and reason, in the user's words", () => {
    expect(dependencyTodo(ext({ status: "unresolved", reason: "needs-contract" }))).toBe("Needs a contract");
    expect(dependencyTodo(ext({ status: "unresolved", reason: "needs-acceptance" }))).toBe("Needs your acceptance");
    expect(dependencyTodo(ext({ status: "unresolved", reason: "needs-input" }))).toBe("Choose a provider");
  });
  it("is empty for a resolved dependency and for every other kind", () => {
    expect(dependencyTodo(ext({ status: "resolved", flags: ["assumed"] }))).toBe("");
    expect(dependencyTodo({ kind: "org-service", name: "billing", status: "unresolved" })).toBe("");
  });
});

describe("computeDependencyStates — one entry per external dependency", () => {
  it("folds the per-component rows by name and lists every consumer", () => {
    const states = computeDependencyStates([
      { componentName: "web", dependencies: [ext({ status: "resolved", flags: ["assumed", "sdk-only"] })] },
      { componentName: "api", dependencies: [ext({ status: "resolved", flags: ["assumed", "sdk-only"] }), { kind: "component", name: "web" }] },
    ]);
    expect(Object.keys(states)).toEqual(["stripe"]);
    expect(states["stripe"]!.usedBy).toEqual(["api", "web"]);
    expect(states["stripe"]!.blocking).toBe(false);
    expect(states["stripe"]!.flags).toEqual(["Assumed", "SDK only"]);
    expect(computeDependencyStates([{ componentName: "web", dependencies: [ext({ status: "resolved", flags: ["derived"] })] }])["stripe"]!.flags).toEqual(["Derived from docs"]);
  });

  it("marks a dependency the build gate would refuse as blocking, with its todo", () => {
    const states = computeDependencyStates([
      { componentName: "api", dependencies: [ext({ name: "mail", status: "unresolved", reason: "needs-input" })] },
    ]);
    expect(states["mail"]).toMatchObject({ blocking: true, todo: "Choose a provider", flags: [] });
  });
});
