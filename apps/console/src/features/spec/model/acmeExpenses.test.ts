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
import * as Y from "yjs";
import { acmeExpensesSpec, triageAgentSpec, type MockSpecModel } from "../../../mocks/fixtures/spec";
import { seedSpecDoc } from "../collab/specDoc";
import { readSpecLines } from "../collab/useSpecLines";
import { resolveId } from "./ids";
import { deriveWorkspace } from "./workspace";

// The seeded project end to end: its markdown through the collab doc model
// (as the spec card seeds it) to what the workspace shows. Guards the fixture
// against drifting from what the screens are approved on.

function workspaceOf(model: MockSpecModel) {
  const doc = new Y.Doc();
  seedSpecDoc(doc, model);
  return { doc, workspace: deriveWorkspace(model, readSpecLines(doc)) };
}

describe("Acme Expenses, seeded", () => {
  const { workspace } = workspaceOf(acmeExpensesSpec);

  it("reads Approvals' two assumed lines as 2 to confirm, and Payroll export as blocked", () => {
    const chips = Object.fromEntries(workspace.features.map((f) => [f.id, f.chips.map((c) => c.label)]));
    expect(chips).toEqual({ F1: [], F2: ["2 to confirm"], F3: ["blocked"], F4: [], F5: [] });
  });

  it("indexes stories across files, product-wide items, and the retired F2.3", () => {
    expect(resolveId(workspace.index, "F2.5")?.entry).toMatchObject({ fileKey: "F2", text: expect.stringMatching(/^As finance, I give a second approval/) });
    expect(resolveId(workspace.index, "P4")?.entry).toMatchObject({ fileKey: "product-wide", text: "Staff sign in with company SSO." });
    expect(resolveId(workspace.index, "F2.3")).toMatchObject({ entry: { id: "F5.1", fileKey: "F5" }, retiredFrom: "F2.3" });
  });

  it("works out Next up from the documents", () => {
    expect(workspace.fog).toHaveLength(3);
    expect(workspace.nextUp.map((i) => i.kind)).toEqual(["blocking", "confirm", "interview", "interview", "design", "fog", "fog", "fog"]);
    expect(workspace.small).toBe(false);
  });

});

describe("Triage agent, seeded", () => {
  it("is a small product with nothing waiting on the user", () => {
    const { workspace } = workspaceOf(triageAgentSpec);
    expect(workspace.small).toBe(true);
    expect(workspace.fog).toEqual([]);
    expect(workspace.features.flatMap((f) => f.chips)).toEqual([]);
  });
});
