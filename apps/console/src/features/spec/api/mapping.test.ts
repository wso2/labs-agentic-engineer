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
import { toSpecEntries, toSpecEntry } from "./mapping";

describe("toSpecEntry", () => {
  it("keeps the full specs/ path and derives the group from the folder", () => {
    expect(
      toSpecEntry({ path: "specs/requirements/prd.md", sha: "a1" }),
    ).toEqual({ path: "specs/requirements/prd.md", sha: "a1", group: "requirements" });
    expect(
      toSpecEntry({ path: "specs/design/architecture.md", sha: "b2" }),
    ).toEqual({ path: "specs/design/architecture.md", sha: "b2", group: "designs" });
    expect(
      toSpecEntry({ path: "specs/validation/plan.md", sha: "c3" }),
    ).toEqual({ path: "specs/validation/plan.md", sha: "c3", group: "validation" });
  });

  // References are transient turn inputs, never committed (ADR-0017), so no
  // project created from here on has them in git at all. The guard exists for
  // the ones created under the feature's v1, which DID commit them: without it
  // they fall through to the `requirements` group, become selectable, and pour
  // a PDF's bytes into the editor pane.
  it("hides reference documents from the spec view entirely (#383)", () => {
    expect(
      toSpecEntry({ path: "specs/requirements/references/prd.pdf", sha: "e5" }),
    ).toBeNull();
    expect(
      toSpecEntry({ path: "specs/requirements/references/notes.md", sha: "e6" }),
    ).toBeNull();
    // Only the references folder itself — a requirements file in any other
    // subfolder stays a requirement.
    expect(
      toSpecEntry({ path: "specs/requirements/drafts/old.md", sha: "f6" }),
    ).toEqual({
      path: "specs/requirements/drafts/old.md",
      sha: "f6",
      group: "requirements",
    });
  });

  it("keeps nested paths intact", () => {
    expect(
      toSpecEntry({ path: "specs/design/components/orders/design.json", sha: "d4" }),
    ).toEqual({
      path: "specs/design/components/orders/design.json",
      sha: "d4",
      group: "designs",
    });
  });

  it("hides files outside the known folders (#113 decision 3)", () => {
    expect(toSpecEntry({ path: "specs/notes.md", sha: "x" })).toBeNull();
    expect(toSpecEntry({ path: "specs/scratch/todo.md", sha: "x" })).toBeNull();
    expect(toSpecEntry({ path: "README.md", sha: "x" })).toBeNull();
    // A file literally named like a group folder has no content path.
    expect(toSpecEntry({ path: "specs/requirements", sha: "x" })).toBeNull();
  });

  // A trailing slash names a directory, and its empty last segment clears the
  // length check — it must not become a selectable entry with no file name.
  it("hides directory-like paths", () => {
    expect(
      toSpecEntry({ path: "specs/requirements/references/", sha: "x" }),
    ).toBeNull();
    expect(toSpecEntry({ path: "specs/requirements/", sha: "x" })).toBeNull();
    expect(toSpecEntry({ path: "specs/design/components/", sha: "x" })).toBeNull();
  });
});

describe("toSpecEntries", () => {
  it("maps and drops hidden files in one pass", () => {
    expect(
      toSpecEntries([
        { path: "specs/requirements/prd.md", sha: "a1" },
        { path: "specs/notes.md", sha: "x" },
        { path: "specs/validation/plan.md", sha: "c3" },
      ]).map((e) => e.path),
    ).toEqual(["specs/requirements/prd.md", "specs/validation/plan.md"]);
  });
});
