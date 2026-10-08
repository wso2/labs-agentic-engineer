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
import type { ColumnComponent, EnvironmentColumn } from "./pipeline";
import { agentLaunch, componentTry, testLogins, tryItAppUrl, tryItKind } from "./tryIt";

type ProjectRolesView = components["schemas"]["ProjectRolesView"];

const component = (over: Partial<ColumnComponent>): ColumnComponent => ({
  name: "expense-agent",
  displayName: "Expense agent",
  type: "ai-agent",
  deployment: {},
  kind: "ready",
  url: "https://expense-agent.example",
  ...over,
});

const view: ProjectRolesView = {
  directoryAvailable: true,
  signIn: { issuer: "https://idp.example", clientId: "acme-public" },
  resourceServer: "https://api.acme.example",
  testUsers: [
    { username: "jane", owned: true, exists: true, supplied: false, roles: ["Employee"], scopes: ["expenses:write"] },
    { username: "priya", owned: true, exists: true, supplied: false, roles: ["Approver", "Approver"], scopes: ["expenses:approve"] },
    { username: "someone-else", owned: false, exists: true, supplied: false },
  ],
};

describe("tryItKind", () => {
  it("tries a serving component by its type", () => {
    expect(tryItKind(component({ type: "web-application" }))).toBe("web-app");
    expect(tryItKind(component({}))).toBe("agent");
    expect(tryItKind(component({ type: "service" }))).toBe("service");
  });

  it("offers nothing for a component that is not serving", () => {
    expect(tryItKind(component({ kind: "converging" }))).toBeNull();
    expect(tryItKind(component({ url: null }))).toBeNull();
  });
});

describe("testLogins", () => {
  it("offers only the accounts the platform holds the password for", () => {
    expect(testLogins(view)).toEqual([
      { username: "jane", roles: ["Employee"], scopes: ["expenses:write"] },
      { username: "priya", roles: ["Approver"], scopes: ["expenses:approve"] },
    ]);
  });
});

describe("agentLaunch", () => {
  it("opens the agent in the test app, signed in on the project's identity provider with every test user's grants", () => {
    const launch = agentLaunch("acme-expenses", component({}), view);
    expect(launch).toMatchObject({
      issuer: "https://idp.example",
      clientId: "acme-public",
      resource: "https://api.acme.example",
      endpoint: "https://expense-agent.example",
      scopes: ["openid", "profile", "email", "expenses:write", "expenses:approve"],
    });
    expect(tryItAppUrl("http://tryit.example/", launch!)).toMatch(/^http:\/\/tryit\.example\/#\/agent\?project=acme-expenses&component=expense-agent&/);
  });

  it("has no launch without a sign-in client or a test user to sign in as", () => {
    const withoutSignIn: ProjectRolesView = { directoryAvailable: true, testUsers: view.testUsers ?? null };
    expect(agentLaunch("acme-expenses", component({}), withoutSignIn)).toBeNull();
    expect(agentLaunch("acme-expenses", component({}), { ...view, testUsers: [] })).toBeNull();
  });
});

describe("componentTry", () => {
  const column = (name: string, components: ColumnComponent[]): EnvironmentColumn => ({
    name,
    label: name[0]!.toUpperCase() + name.slice(1),
    entry: name === "development",
    version: null,
    running: components.length > 0,
    state: { label: "Running", tone: "success" },
    components,
    dependencies: null,
    next: null,
  });
  const app = (over: Partial<ColumnComponent> = {}) =>
    component({ name: "expense-app", displayName: "Expense app", type: "web-application", ...over });

  it("opens on the first environment it serves in, where its newest version runs, and offers the others", () => {
    const columns = [column("development", [app()]), column("staging", [app()]), column("production", [app()])];
    expect(componentTry(columns, "expense-app")).toEqual({
      kind: "serving",
      environments: [
        { name: "development", label: "Development" },
        { name: "staging", label: "Staging" },
        { name: "production", label: "Production" },
      ],
    });
  });

  it("skips an environment where it is not serving", () => {
    const columns = [column("development", [app({ kind: "converging" })]), column("staging", [app()])];
    expect(componentTry(columns, "expense-app")).toEqual({ kind: "serving", environments: [{ name: "staging", label: "Staging" }] });
  });

  it("says where it is deploying or failed when it serves nowhere, a failure first", () => {
    expect(componentTry([column("development", [app({ kind: "converging" })])], "expense-app")).toEqual({
      kind: "deploying",
      environment: { name: "development", label: "Development" },
    });
    const columns = [column("development", [app({ kind: "converging" })]), column("staging", [app({ kind: "failed" })])];
    expect(componentTry(columns, "expense-app")).toEqual({ kind: "failed", environment: { name: "staging", label: "Staging" } });
  });

  it("says it runs where it is ready but nothing answers to try", () => {
    expect(componentTry([column("development", [app({ url: null })])], "expense-app")).toEqual({
      kind: "running",
      environment: { name: "development", label: "Development" },
    });
  });

  it("says a component deployed nowhere is not deployed yet", () => {
    const notDeployed = app({ deployment: undefined, kind: "not-deployed", url: null });
    expect(componentTry([column("development", [notDeployed]), column("staging", [])], "expense-app")).toEqual({
      kind: "not-deployed",
    });
  });
});
