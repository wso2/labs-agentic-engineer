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
import { buildDependencyResolutionMessage } from "./dependencyResolutionMessage";
import type { components } from "../../../generated/aep-api";

type Dependency = components["schemas"]["Dependency"];

const ambiguousDep: Dependency = {
  kind: "external",
  name: "email-provider",
  description: "Transactional email for signup + reset flows.",
  status: "unresolved",
  reason: "needs-input",
  suggestions: [
    {
      name: "sendgrid-rest",
      style: "rest-api",
      description: "SendGrid v3 Web API",
    },
    {
      name: "resend-sdk",
      style: "sdk",
      description: "Resend Node SDK",
    },
  ],
};

const resolvedDep: Dependency = {
  kind: "external",
  name: "stripe",
  status: "resolved",
  style: "sdk",
  package: "npm:stripe@^17.0.0",
};

// The message is lean: the agent reads the dependency's own file from the
// snapshot and the playbook from its skills, so the seed only names the
// dependency (and, for a reconsider, the component whose choice is in
// question) plus the intent.
describe("buildDependencyResolutionMessage — lean seed message (#252 Task 17)", () => {
  it("resolve intent: runs the guided flow for the dependency — the skill command, nothing else", () => {
    const msg = buildDependencyResolutionMessage(
      "checkout-api",
      ambiguousDep,
      "resolve",
    );
    expect(msg).toBe("/resolve-dependency email-provider");
  });

  it("reconsider intent: names the dependency and component, asking to look at other options", () => {
    const msg = buildDependencyResolutionMessage(
      "checkout-api",
      resolvedDep,
      "reconsider",
    );
    expect(msg).toContain("stripe");
    expect(msg).toContain("checkout-api");
    expect(msg).toMatch(/reconsider/i);
    expect(msg).toMatch(/other options/i);
  });

  it("never embeds the dependency's JSON entry", () => {
    const resolveMsg = buildDependencyResolutionMessage(
      "checkout-api",
      ambiguousDep,
      "resolve",
    );
    const reconsiderMsg = buildDependencyResolutionMessage(
      "checkout-api",
      resolvedDep,
      "reconsider",
    );
    // The old shape fenced a JSON block and printed field names verbatim —
    // none of that survives the lean message.
    expect(resolveMsg).not.toContain("```");
    expect(resolveMsg).not.toContain(JSON.stringify(ambiguousDep));
    expect(resolveMsg).not.toContain("suggestions");
    expect(reconsiderMsg).not.toContain("```");
    expect(reconsiderMsg).not.toContain("style");
    expect(reconsiderMsg).not.toContain("package");
  });

  it("never embeds the resolution playbook", () => {
    const msg = buildDependencyResolutionMessage(
      "checkout-api",
      ambiguousDep,
      "resolve",
    );
    expect(msg).not.toContain("architecture");
    expect(msg).not.toContain("specPath");
    expect(msg).not.toContain("docsUrl");
    expect(msg).not.toMatch(/only this dependency/i);
  });

  it("degrades gracefully when the dependency carries no extra fields (status/reason absent)", () => {
    const bareDep: Dependency = { kind: "external", name: "github" };
    const msg = buildDependencyResolutionMessage("issue-sync", bareDep, "resolve");
    expect(msg).not.toContain("undefined");
    expect(msg).toBe("/resolve-dependency github");
  });
});
