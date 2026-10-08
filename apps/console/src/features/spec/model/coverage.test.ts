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
import { documentTitle, withCoverage } from "./coverage";

describe("a document's coverage", () => {
  const doc = new Y.Doc();
  seedSpecDoc(doc, {
    files: {
      "specs/requirements/sources/rates-xlsx.md": `# Rates.xlsx

- Rates tab: Per diem in Sri Lanka is $40. → F1.3
- Rates tab: Mileage is paid at $0.30 a mile. → not used: mileage is not in this release
`,
    },
  });
  const lines = readSpecLines(doc);

  it("reads what each place says and where it landed, by the document's own name", () => {
    const [rates] = withCoverage([{ id: "Rates.xlsx.md", title: "Rates.xlsx.md", pages: 0, rows: [] }], lines);
    expect(rates).toEqual({
      id: "Rates.xlsx.md",
      title: "Rates.xlsx",
      pages: 0,
      rows: [
        { page: "Rates tab", says: "Per diem in Sri Lanka is $40.", landedIn: "F1.3" },
        { page: "Rates tab", says: "Mileage is paid at $0.30 a mile.", landedIn: null },
      ],
    });
  });

  it("finds a coverage file by its file name when it is titled with the document's own heading", () => {
    const titled = new Y.Doc();
    seedSpecDoc(titled, {
      files: { "specs/requirements/sources/acme-te-policy.md": "# Acme Travel & Expense Policy, version 3\n\n- 2. Receipts: A receipt above $25. → F1\n" },
    });
    const [policy] = withCoverage([{ id: "acme-te-policy.md", title: "acme-te-policy.md", pages: 0, rows: [] }], readSpecLines(titled));
    expect(policy?.rows).toEqual([{ page: "2. Receipts", says: "A receipt above $25.", landedIn: "F1" }]);
  });

  it("names a converted Office file by its own name, and leaves others as they are", () => {
    expect(documentTitle("Policy.docx.md")).toBe("Policy.docx");
    expect(documentTitle("notes.md")).toBe("notes.md");
  });
});
