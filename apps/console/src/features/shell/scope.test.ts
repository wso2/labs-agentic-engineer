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
import { cardOfRoute, chatTopic, pageOfCard, shellScope } from "./scope";

describe("shellScope", () => {
  const inProject = (routeId: string, search?: { file?: unknown }) =>
    shellScope({ routeId, params: { projectName: "acme-expenses" }, ...(search ? { search } : {}) });

  it("puts the Dashboard, the Projects grid and New project at org level", () => {
    expect(shellScope({ routeId: "/_dashboard/", params: {} })).toEqual({ kind: "org", page: "dashboard", card: null });
    expect(shellScope({ routeId: "/projects/", params: {} })).toEqual({ kind: "org", page: "projects", card: null });
    expect(shellScope({ routeId: "/projects/new", params: {} })).toEqual({
      kind: "org",
      page: "new",
      card: null,
    });
  });

  it("puts the Skills Page at org level", () => {
    expect(shellScope({ routeId: "/skills", params: {} })).toEqual({ kind: "org", page: "skills", card: null });
  });

  it("reads a Skill card, a saved one or a new one, as the org's card over Skills", () => {
    const skill = { kind: "org", page: "skills", card: "skill" };
    expect(shellScope({ routeId: "/skills/$name", params: {} })).toEqual(skill);
    expect(shellScope({ routeId: "/skills/new", params: {} })).toEqual(skill);
  });

  it("puts the Resources Page at org level, and a Resource card, saved or new, over it", () => {
    expect(shellScope({ routeId: "/resources", params: {} })).toEqual({ kind: "org", page: "resources", card: null });
    const resource = { kind: "org", page: "resources", card: "resource" };
    expect(shellScope({ routeId: "/resources/$name", params: {} })).toEqual(resource);
    expect(shellScope({ routeId: "/resources/new", params: {} })).toEqual(resource);
  });

  it("reads the Settings card as the org's card over the Dashboard", () => {
    expect(shellScope({ routeId: "/_dashboard/settings", params: {} })).toEqual({
      kind: "org",
      page: "dashboard",
      card: "settings",
    });
  });

  it("reads each project Page with no card open", () => {
    expect(inProject("/projects/$projectName/_overview/")).toEqual({
      kind: "project",
      projectName: "acme-expenses",
      page: "overview",
      card: null,
      specFile: null,
    });
    expect(inProject("/projects/$projectName/builds")).toMatchObject({ page: "builds", card: null });
    expect(inProject("/projects/$projectName/validations")).toMatchObject({ page: "validations", card: null });
    expect(inProject("/projects/$projectName/deploy")).toMatchObject({ page: "deploy", card: null });
    expect(inProject("/projects/$projectName/issues")).toMatchObject({ page: "issues", card: null });
  });

  it("reads each card route as its card over the Page that lists it", () => {
    const routes = [
      ["/projects/$projectName/_overview/spec", "spec", "overview"],
      ["/projects/$projectName/_overview/design", "design", "overview"],
      ["/projects/$projectName/_overview/prototype", "prototype", "overview"],
      ["/projects/$projectName/builds/$version", "build", "builds"],
      ["/projects/$projectName/validations/$version", "validation", "validations"],
      ["/projects/$projectName/deploy/$env/configure", "configure", "deploy"],
      ["/projects/$projectName/issues/$number", "issue", "issues"],
    ] as const;
    for (const [routeId, card, page] of routes) {
      expect(inProject(routeId)).toEqual({ kind: "project", projectName: "acme-expenses", page, card, specFile: null });
    }
  });

  it("reads an address the project does not have as its overview", () => {
    expect(inProject("/projects/$projectName")).toMatchObject({ page: "overview", card: null });
  });

  it("reads the spec card's open file from its search, and only on the spec card", () => {
    expect(inProject("/projects/$projectName/_overview/spec", { file: "F2" })).toMatchObject({ card: "spec", specFile: "F2" });
    expect(inProject("/projects/$projectName/_overview/spec", {})).toMatchObject({ card: "spec", specFile: null });
    expect(inProject("/projects/$projectName/_overview/design", { file: "F2" })).toMatchObject({
      card: "design",
      specFile: null,
    });
  });

  it("treats any other route as org level with no page of its own", () => {
    expect(shellScope({ routeId: "/callback", params: {} })).toEqual({
      kind: "org",
      page: "other",
      card: null,
    });
  });
});

describe("cards and the Pages they are over", () => {
  it("knows which routes draw a card", () => {
    expect(cardOfRoute("/projects/$projectName/deploy/$env/configure")).toBe("configure");
    expect(cardOfRoute("/projects/$projectName/builds/$version")).toBe("build");
    expect(cardOfRoute("/projects/$projectName/builds")).toBeNull();
    expect(cardOfRoute("/projects/$projectName/_overview/")).toBeNull();
    expect(cardOfRoute("/projects/$projectName/deploy")).toBeNull();
    expect(cardOfRoute("/projects/$projectName/issues/$number")).toBe("issue");
    expect(cardOfRoute("/projects/$projectName/issues")).toBeNull();
    expect(cardOfRoute("/_dashboard/settings")).toBe("settings");
    expect(cardOfRoute("/_dashboard/")).toBeNull();
    expect(cardOfRoute("/skills/$name")).toBe("skill");
    expect(cardOfRoute("/skills/new")).toBe("skill");
    expect(cardOfRoute("/skills")).toBeNull();
    expect(cardOfRoute("/resources/$name")).toBe("resource");
    expect(cardOfRoute("/resources")).toBeNull();
  });

  it("closes each card back to the Page it opened over", () => {
    expect(pageOfCard("spec")).toBe("overview");
    expect(pageOfCard("design")).toBe("overview");
    expect(pageOfCard("build")).toBe("builds");
    expect(pageOfCard("validation")).toBe("validations");
    expect(pageOfCard("configure")).toBe("deploy");
    expect(pageOfCard("issue")).toBe("issues");
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

  it("talks about the whole product on the product page, product-wide, the overview, a build and a validation", () => {
    expect(chatTopic("spec", null)).toEqual(product);
    expect(chatTopic(null, null)).toEqual(product);
    expect(chatTopic("build", null)).toEqual(product);
    expect(chatTopic("validation", null)).toEqual(product);
  });

  it("talks about the whole product on an environment's Configure card, which no agent can change yet", () => {
    expect(chatTopic("configure", null)).toEqual(product);
  });

  it("talks about the whole product on an Issue card, which no agent works on its own yet", () => {
    expect(chatTopic("issue", null)).toEqual(product);
  });
});
