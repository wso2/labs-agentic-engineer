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
import { seedSpecDoc } from "../collab/specDoc";
import { readSpecLines } from "../collab/useSpecLines";
import { productWideItems, readRequirements } from "./requirements";

// The shared fixture both requirements readers are held to (the other is Go:
// services/aep-api/internal/platform/reqspec). Its markdown goes through the
// collab doc as the spec card seeds it, so this reads what the editor reads.
const FIXTURE = "../../../../../../packages/contracts/requirements/acme-expenses/";
const raw = import.meta.glob<string>("../../../../../../packages/contracts/requirements/acme-expenses/**/*.{md,json}", {
  query: "?raw",
  import: "default",
  eager: true,
});

function fixtureFiles(): Record<string, string> {
  return Object.fromEntries(
    Object.entries(raw)
      .filter(([path]) => path.endsWith(".md"))
      .map(([path, content]) => [`specs/requirements/${path.slice(FIXTURE.length)}`, content]),
  );
}

function linesOf(files: Record<string, string>) {
  const doc = new Y.Doc();
  seedSpecDoc(doc, { files });
  return readSpecLines(doc);
}

describe("readRequirements", () => {
  it("reads the shared Acme fixture as expected.json says", () => {
    const expected = JSON.parse(raw[`${FIXTURE}expected.json`]!);
    expect(readRequirements(linesOf(fixtureFiles()))).toEqual({ retiredFeatures: [], ...expected });
  });

  it("gives each product-wide item its reach", () => {
    expect(productWideItems(readRequirements(linesOf(fixtureFiles())))).toEqual([
      { id: "P1", appliesTo: "all" },
      { id: "P2", appliesTo: "all" },
      { id: "P3", appliesTo: ["F1", "F3"] },
      { id: "P4", appliesTo: "all" },
    ]);
  });

  it("reads a product-wide topic file and orders IDs numerically", () => {
    const requirements = readRequirements(
      linesOf({
        "specs/requirements/product-wide.md": "# Product-wide\n\n## Requirements\n\n- P2 Kept 7 years. Applies to: all.\n",
        "specs/requirements/product-wide/security.md": "# Security\n\n## Requirements\n\n- P10 Sessions end after 8 hours. Applies to: F1.\n",
        "specs/requirements/features/F10-audit.md": "# Audit\n\n## User Stories\n\n- F10.1 As an auditor, I export a year.\n",
        "specs/requirements/features/F2-approvals.md": "# Approvals\n\n## User Stories\n\n- F2.10 As a manager, I see ten.\n- F2.9 As a manager, I see nine.\n",
      }),
    );
    expect(requirements.productWide.map((p) => p.id)).toEqual(["P2", "P10"]);
    expect(requirements.features.map((f) => f.id)).toEqual(["F2", "F10"]);
  });
});
