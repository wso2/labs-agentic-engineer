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
import { chatTopic, shellScope } from "./scope";

describe("shellScope", () => {
  it("puts the Projects grid and New project at org level", () => {
    expect(shellScope({ routeId: "/", params: {} })).toEqual({ kind: "org", page: "projects" });
    expect(shellScope({ routeId: "/projects/new", params: {} })).toEqual({
      kind: "org",
      page: "new",
    });
  });

  it("reads the overview as a project with no card open", () => {
    expect(
      shellScope({ routeId: "/projects/$projectName", params: { projectName: "acme-expenses" } }),
    ).toEqual({ kind: "project", projectName: "acme-expenses", card: null, specFile: null });
  });

  it("reads each card route as its card over the project", () => {
    const routes = [
      ["/projects/$projectName/spec", "spec"],
      ["/projects/$projectName/design", "design"],
      ["/projects/$projectName/prototype", "prototype"],
      ["/projects/$projectName/builds/", "builds"],
      ["/projects/$projectName/builds/$version", "builds"],
    ] as const;
    for (const [routeId, card] of routes) {
      expect(shellScope({ routeId, params: { projectName: "acme-expenses" } })).toEqual({
        kind: "project",
        projectName: "acme-expenses",
        card,
        specFile: null,
      });
    }
  });

  it("reads the spec card's open file from its search, and only on the spec card", () => {
    const at = (routeId: string, search: { file?: unknown }) =>
      shellScope({ routeId, params: { projectName: "acme-expenses" }, search });

    expect(at("/projects/$projectName/spec", { file: "F2" })).toMatchObject({ card: "spec", specFile: "F2" });
    expect(at("/projects/$projectName/spec", {})).toMatchObject({ card: "spec", specFile: null });
    expect(at("/projects/$projectName/design", { file: "F2" })).toMatchObject({ card: "design", specFile: null });
  });

  it("treats any other route as org level with no page of its own", () => {
    expect(shellScope({ routeId: "/callback", params: {} })).toEqual({
      kind: "org",
      page: "other",
    });
  });
});

describe("chatTopic", () => {
  const product = { topic: "the whole product", note: null };

  it("talks about the design review on the design card and the prototypes beside it", () => {
    expect(chatTopic("design", null)).toEqual({ topic: "the design review", note: null });
    expect(chatTopic("prototype", null)).toEqual({ topic: "the design review", note: null });
  });

  it("narrows to the feature open in the spec card, and says where other changes go", () => {
    expect(chatTopic("spec", "Approvals")).toEqual({
      topic: "Approvals",
      note: "A change that reaches other features is made there too.",
    });
  });

  it("talks about the whole product on the product page, product-wide, the overview and builds", () => {
    expect(chatTopic("spec", null)).toEqual(product);
    expect(chatTopic(null, null)).toEqual(product);
    expect(chatTopic("builds", null)).toEqual(product);
  });
});
