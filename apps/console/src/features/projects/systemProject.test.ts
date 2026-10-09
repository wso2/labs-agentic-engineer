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
import { isSystemProject, withoutSystemProjects } from "./systemProject";

describe("systemProject", () => {
  it("drops ae-system from a page and keeps the cursor", () => {
    expect(
      withoutSystemProjects({
        items: [{ name: "p1" }, { name: "ae-system" }],
        nextCursor: "c",
      }),
    ).toEqual({ items: [{ name: "p1" }], nextCursor: "c" });
  });

  it("matches only the exact system project name", () => {
    expect(isSystemProject("ae-system")).toBe(true);
    expect(isSystemProject("ae-system-2")).toBe(false);
  });
});
