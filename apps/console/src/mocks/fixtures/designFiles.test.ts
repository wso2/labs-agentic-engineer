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
import { FileBundle } from "@aep/agent-stream";
import { designItems } from "../../features/design/model/designFiles";
import { dependencyOf, designCatalog } from "./design";
import { designRoomFiles, reviseDecision } from "./designFiles";
import { acmeExpensesSpec } from "./spec";

// What a mock design turn leaves in the room has to get through the design
// agent's write gates, as the turn streams it: a refused write never lands in
// the mock's room, and the Design card would miss it.

const designed = acmeExpensesSpec.features.filter((f) => f.id === "F1" || f.id === "F2");
const drafts = designCatalog("acme-expenses", designed, new Map());
const dependencies = acmeExpensesSpec.features.flatMap((f) => dependencyOf("acme-expenses", f) ?? []);

describe("the files a mock design writes into the room", () => {
  const files = designRoomFiles("acme-expenses", drafts, dependencies);

  it("pass every write gate, one addFile at a time in the order they are streamed", () => {
    const bundle = new FileBundle(acmeExpensesSpec.files);
    for (const [path, content] of Object.entries(files)) {
      const result = bundle.addFile(path, content);
      expect(result.ok ? "applied" : JSON.stringify(result), path).toBe("applied");
    }
  });

  it("make Acme's whole rail", () => {
    expect(designItems(files).map((i) => i.key)).toEqual([
      "architecture",
      "data-model",
      "roles",
      "component-expense-web",
      "prototype-expense-web",
      "component-expense-api",
      "contract-expense-api",
      "dependency-xero",
      "decisions",
      "acceptance-F1",
      "acceptance-F2",
    ]);
  });
});

describe("reviseDecision", () => {
  // As the room writes the file back: the tag's brackets escaped.
  const markdown = `# Technical decisions

- \\[data-model\\] Amounts are stored in cents, never as decimals. *assumed* (F1, F2)
- \\[contract\\] Pending approvals are listed oldest first. *assumed* (F2)
`;

  it("rewrites the quoted decision in the comment's words, without the tag", () => {
    const comment = "> [data-model] Amounts are stored in cents, never as decimals. (F1, F2)\n\nStore amounts as decimals with two places.";
    expect(reviseDecision(markdown, comment)).toEqual({
      from: "- \\[data-model\\] Amounts are stored in cents, never as decimals. *assumed* (F1, F2)",
      to: "- \\[data-model\\] Store amounts as decimals with two places. (F1, F2)",
      words: "Store amounts as decimals with two places.",
    });
  });

  it("leaves a comment that quotes no decision, or asks for nothing, to the agent's reply", () => {
    expect(reviseDecision(markdown, "Why cents?")).toBeNull();
    expect(reviseDecision(markdown, "> [data-model] Amounts are floats. (F1)\n\nUse decimals.")).toBeNull();
    expect(reviseDecision(markdown, "> [contract] Pending approvals are listed oldest first. (F2)\n\n")).toBeNull();
  });
});
