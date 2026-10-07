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
import { editedBody, joinSkillMd, newSkillMd, splitFrontmatter, withDescription } from "./skillMd";

const FILE = `---
name: go
description: Go services, the house way.
metadata:
  aep:
    kind: org
---

# Go

Use the standard library.
`;

describe("splitFrontmatter", () => {
  it("parts the frontmatter from the body, which is kept exactly", () => {
    const { frontmatter, body } = splitFrontmatter(FILE);
    expect(frontmatter).toBe("name: go\ndescription: Go services, the house way.\nmetadata:\n  aep:\n    kind: org");
    expect(body).toBe("\n# Go\n\nUse the standard library.\n");
    expect(joinSkillMd(frontmatter!, body)).toBe(FILE);
  });

  it("reads a file with no frontmatter as all body", () => {
    expect(splitFrontmatter("# Just a body\n")).toEqual({ frontmatter: null, body: "# Just a body\n" });
  });
});

describe("withDescription", () => {
  it("replaces the description and nothing else", () => {
    const { frontmatter } = splitFrontmatter(FILE);
    expect(withDescription(frontmatter!, "Go services")).toBe("name: go\ndescription: Go services\nmetadata:\n  aep:\n    kind: org");
  });

  it("replaces a description that continues on indented lines", () => {
    const folded = "name: go\ndescription: >-\n  Go services,\n\n  the house way.\nlicense: MIT";
    expect(withDescription(folded, "Go")).toBe("name: go\ndescription: Go\nlicense: MIT");
  });

  it("quotes a description YAML would read as something else", () => {
    expect(withDescription("name: go\ndescription: x", "Use when: building Go")).toBe('name: go\ndescription: "Use when: building Go"');
    expect(withDescription("name: go\ndescription: x", "- a list?")).toBe('name: go\ndescription: "- a list?"');
  });

  it("adds a description after the name when there is none", () => {
    expect(withDescription("name: go\nlicense: MIT", "Go")).toBe("name: go\ndescription: Go\nlicense: MIT");
  });

  it("keeps the description to one line", () => {
    expect(withDescription("name: go", "Go\nservices")).toBe("name: go\ndescription: Go services");
  });
});

describe("a new skill's file", () => {
  it("names the skill, describes it and sets the body off with a blank line", () => {
    expect(newSkillMd("pdf-tools", "Read PDFs", "# PDF tools")).toBe("---\nname: pdf-tools\ndescription: Read PDFs\n---\n\n# PDF tools\n");
  });

  it("writes an edited body the same way", () => {
    expect(editedBody("# Go\n\nUse it.")).toBe("\n# Go\n\nUse it.\n");
    expect(editedBody("\n\n# Go\n")).toBe("\n# Go\n");
  });
});
