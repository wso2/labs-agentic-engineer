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
import { toRepoPath, toSpecEntries, toSpecEntry } from "./mapping";

describe("toSpecEntry", () => {
  it("strips the specs/ prefix and derives the group from the folder", () => {
    expect(
      toSpecEntry({ path: "specs/requirements/prd.md", sha: "a1" }),
    ).toEqual({ path: "requirements/prd.md", sha: "a1", group: "requirements" });
    expect(
      toSpecEntry({ path: "specs/design/architecture.md", sha: "b2" }),
    ).toEqual({ path: "design/architecture.md", sha: "b2", group: "designs" });
    expect(
      toSpecEntry({ path: "specs/validation/plan.md", sha: "c3" }),
    ).toEqual({ path: "validation/plan.md", sha: "c3", group: "validation" });
  });

  it("keeps nested paths intact", () => {
    expect(
      toSpecEntry({ path: "specs/design/components/orders/design.json", sha: "d4" }),
    ).toEqual({
      path: "design/components/orders/design.json",
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
});

describe("toSpecEntries", () => {
  it("maps and drops hidden files in one pass", () => {
    expect(
      toSpecEntries([
        { path: "specs/requirements/prd.md", sha: "a1" },
        { path: "specs/notes.md", sha: "x" },
        { path: "specs/validation/plan.md", sha: "c3" },
      ]).map((e) => e.path),
    ).toEqual(["requirements/prd.md", "validation/plan.md"]);
  });
});

describe("toRepoPath", () => {
  it("round-trips with toSpecEntry", () => {
    expect(toRepoPath("requirements/prd.md")).toBe("specs/requirements/prd.md");
  });
});
