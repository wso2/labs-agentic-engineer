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
import { acmeExpensesSpec } from "../../../mocks/fixtures/spec";
import { seedSpecDoc } from "../collab/specDoc";
import { readSpecLines } from "../collab/useSpecLines";
import { deriveFeatures } from "./features";

// The features are read off the documents: the fixture's features, as the
// mock's hand-written model states them, are what the files say.
describe("deriveFeatures", () => {
  const doc = new Y.Doc();
  seedSpecDoc(doc, acmeExpensesSpec);
  const lines = readSpecLines(doc);

  it("reads each feature's ID, name, file and purpose from its file", () => {
    const got = deriveFeatures(lines, {}, null).map(({ id, name, path, purpose }) => ({ id, name, path, purpose }));
    expect(got).toEqual(acmeExpensesSpec.features.map(({ id, name, path, purpose }) => ({ id, name, path, purpose })));
  });

  // F5 holds a story moved in from Approvals (F5.1 was F2.3), so by the rule the
  // build planner uses — a feature with stories has been interviewed — it is.
  it("says where each feature is: not interviewed, interviewing, interviewed, designed", () => {
    const stages = (designedFrom: Record<string, string>, interviewing: string | null) =>
      Object.fromEntries(deriveFeatures(lines, designedFrom, interviewing).map((f) => [f.id, f.stage]));
    expect(stages({ F1: "basis" }, "F4")).toMatchObject({
      F1: "Designed",
      F2: "Interviewed",
      F4: "Interviewing",
      F5: "Interviewed",
    });
    expect(stages({}, null)).toMatchObject({ F4: "Not interviewed" });
  });
});
