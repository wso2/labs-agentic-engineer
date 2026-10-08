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

import { strict as assert } from "node:assert";
import { test } from "node:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { listRequirementFiles } from "../src/project.js";

test("listRequirementFiles: product page, features in ID order, product-wide; references left out", () => {
  const dir = mkdtempSync(join(tmpdir(), "req-files-"));
  try {
    for (const rel of [
      "product-wide/security.md",
      "features/F10-audit-trail.md",
      "product-wide.md",
      "features/F2-approvals.md",
      "prd.md",
      "references/policy.md",
      "references/policy.pdf",
    ]) {
      const abs = join(dir, "specs/requirements", rel);
      mkdirSync(dirname(abs), { recursive: true });
      writeFileSync(abs, "# x\n");
    }
    assert.deepEqual(listRequirementFiles(dir), [
      "specs/requirements/prd.md",
      "specs/requirements/features/F2-approvals.md",
      "specs/requirements/features/F10-audit-trail.md",
      "specs/requirements/product-wide.md",
      "specs/requirements/product-wide/security.md",
    ]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("listRequirementFiles: no requirements directory yields nothing", () => {
  const dir = mkdtempSync(join(tmpdir(), "req-files-"));
  try {
    assert.deepEqual(listRequirementFiles(dir), []);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
