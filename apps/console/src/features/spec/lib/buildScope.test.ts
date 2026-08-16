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
import { nextVersionLabel, parsePrdStories } from "./buildScope";

describe("buildScope display parsers", () => {
  it("reads the PRD's story numbers from the User Stories section", () => {
    const prd =
      "# PRD\n\n## User Stories\n\n" +
      "1. As a member, I want to browse, so that I can join.\n" +
      "2. As a member, I want to add my item, so that it is counted.\n" +
      "7. As a member, I want a Slack message, so that I don't miss it.\n";
    expect(parsePrdStories(prd)).toEqual([1, 2, 7]);
  });

  it("ignores numbered lines outside the User Stories section", () => {
    const prd =
      "## User Stories\n1. As a user, I want the thing, so that value.\n" +
      "## Out of Scope\n8. not a story.\n";
    expect(parsePrdStories(prd)).toEqual([1]);
  });

  it("skips a numbered item with a whitespace-only title, like the gate", () => {
    const prd = "## User Stories\n1. As a user, I want A, so that a.\n2.   \n";
    expect(parsePrdStories(prd)).toEqual([1]);
  });

  it("yields nothing for a PRD without a User Stories section", () => {
    expect(parsePrdStories("# PRD\n\njust prose")).toEqual([]);
  });

  it("predicts the next version label", () => {
    expect(nextVersionLabel("v3")).toBe("v4");
    expect(nextVersionLabel(undefined)).toBe("v1");
  });
});
