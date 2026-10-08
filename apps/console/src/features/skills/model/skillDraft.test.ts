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
import { draftOf, draftProblems, isDirty, skillMdOf, type SavedSkill } from "./skillDraft";

const saved: SavedSkill = {
  name: "go",
  description: "Go services",
  skillMd: "---\nname: go\ndescription: Go services\nlicense: MIT\n---\n\n# Go\n\n| a | b |\n|---|---|\n| 1 | 2 |\n",
};

describe("the Skill card's draft", () => {
  it("is clean as it opens, and dirty once the description or the body changes", () => {
    const draft = draftOf(saved);
    expect(isDirty(draft, saved)).toBe(false);
    expect(isDirty({ ...draft, description: "Go services, the house way" }, saved)).toBe(true);
    expect(isDirty({ ...draft, body: "# Go\n" }, saved)).toBe(true);
  });

  it("is clean again when the description reads as it did", () => {
    expect(isDirty({ ...draftOf(saved), description: " Go services " }, saved)).toBe(false);
  });

  it("is dirty for a new skill once anything is typed", () => {
    const draft = draftOf(null);
    expect(isDirty(draft, null)).toBe(false);
    expect(isDirty({ ...draft, name: "pdf" }, null)).toBe(true);
    expect(isDirty({ ...draft, body: "# PDF" }, null)).toBe(true);
  });
});

describe("what Save writes", () => {
  it("keeps the body byte for byte when only the description changed", () => {
    expect(skillMdOf({ ...draftOf(saved), description: "Go, the house way" }, saved)).toBe(
      "---\nname: go\ndescription: Go, the house way\nlicense: MIT\n---\n\n# Go\n\n| a | b |\n|---|---|\n| 1 | 2 |\n",
    );
  });

  it("keeps the frontmatter when only the body changed", () => {
    expect(skillMdOf({ ...draftOf(saved), body: "# Go\n\nNew text." }, saved)).toBe(
      "---\nname: go\ndescription: Go services\nlicense: MIT\n---\n\n# Go\n\nNew text.\n",
    );
  });

  it("writes a new skill whole", () => {
    expect(skillMdOf({ name: " pdf ", description: "Read PDFs", body: null }, null)).toBe("---\nname: pdf\ndescription: Read PDFs\n---\n\n");
  });
});

describe("draftProblems", () => {
  it("asks a new skill for a name the platform takes", () => {
    const ok = { name: "pdf-tools", description: "Read PDFs", body: null };
    expect(draftProblems(ok, true)).toEqual({});
    expect(draftProblems({ ...ok, name: "" }, true).name).toBeDefined();
    expect(draftProblems({ ...ok, name: "PDF Tools" }, true).name).toBeDefined();
    expect(draftProblems({ ...ok, name: "pdf--tools" }, true).name).toBeDefined();
    expect(draftProblems({ ...ok, name: "new" }, true).name).toBeDefined();
    expect(draftProblems({ ...ok, name: "a".repeat(56) }, true).name).toBeDefined();
  });

  it("asks every skill for a description", () => {
    expect(draftProblems({ name: "go", description: "  ", body: null }, false)).toEqual({ description: "Say in one line what the skill is for." });
  });
});
