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
import { prototypeHash } from "@wso2/prototype-kit/feedback";
import { sha256Text } from "../../../lib/sha256";
import { artifactHashes, designItems, reviewLists } from "./designFiles";
import { expenseTrackerDesign } from "./expenseTrackerDesign.testing";

// A real design (the expense-tracker design the design eval wrote, as the
// room holds it, with a flow), with what Design makes now beside it: a
// prototype, an agent, a dependency, technical decisions.
const C = "specs/design/components/";
const files: Record<string, string> = {
  ...expenseTrackerDesign,
  [`${C}expense-webapp/prototype.json`]: '{ "name": "Expense Tracker" }',
  [`${C}expense-webapp/prototype.tsx`]: "export default 1;",
  [`${C}claims-assistant/design.json`]: JSON.stringify({ name: "claims-assistant", type: "ai-agent", stories: ["F1.1"] }),
  [`${C}claims-assistant/agent.afm.md`]: "# Claims assistant\n\nAnswers questions about a claim.\n",
  "specs/design/dependencies/xero/dependency.json": JSON.stringify({ name: "xero", resource: { name: "xero", description: "Payroll." } }),
  "specs/design/dependencies/xero/assumptions.md": "# Assumptions\n\n- Bills post in NZD.\n",
  "specs/design/decisions.md": "# Technical decisions\n\n- [data-model] Amounts in cents. *assumed* (F1)\n",
  "specs/design/review.json": '{ "version": 1, "artifacts": [] }\n',
};

describe("the Design card's rail, from the design's files", () => {
  const items = designItems(files);

  it("lists the files in the order Design makes them, each component's artifact under it", () => {
    expect(items.map((i) => [i.key, i.section, i.nested])).toEqual([
      ["architecture", null, false],
      ["data-model", null, false],
      ["roles", null, false],
      ["component-expense-webapp", null, false],
      ["prototype-expense-webapp", null, true],
      ["component-expense-api", null, false],
      ["contract-expense-api", null, true],
      ["component-claims-assistant", null, false],
      ["agent-claims-assistant", null, true],
      ["dependency-xero", "Dependencies", true],
      ["flow-claim-decision", "Flows", true],
      ["decisions", null, false],
      ["acceptance-F1", "Acceptance tests", true],
      ["acceptance-F2", "Acceptance tests", true],
      ["acceptance-F3", "Acceptance tests", true],
    ]);
  });

  it("keys each item by the path review.json names it by, the prototype by its manifest", () => {
    expect(items.find((i) => i.key === "prototype-expense-webapp")?.path).toBe(`${C}expense-webapp/prototype.json`);
    expect(items.find((i) => i.key === "contract-expense-api")?.path).toBe(`${C}expense-api/openapi.yaml`);
    expect(items.find((i) => i.key === "roles")?.path).toBe("specs/design/security.json");
  });

  it("lists nothing for wireframes or the review file itself", () => {
    expect(items.some((i) => i.path.endsWith(".dsl") || i.path.endsWith("review.json"))).toBe(false);
  });

  it("shows an item only once its file exists", () => {
    const early = designItems({ "specs/design/design.cell": files["specs/design/design.cell"]! });
    expect(early.map((i) => i.key)).toEqual(["architecture"]);
  });

  it("makes only the reviewed files checkable, in a business or a technical list", () => {
    const lists = reviewLists(items);
    expect(lists.business.map((i) => i.key)).toEqual(["prototype-expense-webapp", "agent-claims-assistant", "roles"]);
    expect(lists.technical.map((i) => i.key)).toEqual(["architecture", "data-model", "contract-expense-api", "flow-claim-decision", "decisions"]);
    expect(items.filter((i) => i.checkable).length).toBe(8);
    expect(items.find((i) => i.key === "acceptance-F1")?.checkable).toBe(false);
    expect(items.find((i) => i.key === "dependency-xero")?.checkable).toBe(false);
  });

  it("draws a dependency from its definition, then its assumptions", () => {
    const body = items.find((i) => i.key === "dependency-xero")?.body;
    expect(body?.kind === "dependency" && body.markdown).toBe("# xero\n\nPayroll.\n\n- Provider: not chosen yet\n\n# Assumptions\n\n- Bills post in NZD.\n");
  });

  it("hashes a file's text, and a prototype as the kit names its revision", () => {
    const hashes = artifactHashes(items);
    expect(hashes["specs/design/flows/claim-decision.md"]).toBe(sha256Text(files["specs/design/flows/claim-decision.md"]!));
    expect(hashes[`${C}expense-webapp/prototype.json`]).toBe(prototypeHash('{ "name": "Expense Tracker" }', "export default 1;"));
  });

  it("hashes the technical decisions without their assumed tags, so a Keep leaves them checked", () => {
    const kept = { ...files, "specs/design/decisions.md": "# Technical decisions\n\n- [data-model] Amounts in cents. (F1)\n" };
    const hash = (f: Record<string, string>) => artifactHashes(designItems(f))["specs/design/decisions.md"];
    expect(hash(kept)).toBe(hash(files));
  });
});
