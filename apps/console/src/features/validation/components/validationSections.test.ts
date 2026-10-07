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
import type { FeatureResults } from "../../builds/model/validation";
import { validationSections } from "./ValidationByFeature";

const group = (id: string, notBuilt = false): FeatureResults => ({
  id,
  name: id,
  scenarios: [],
  passed: 0,
  judged: 0,
  runs: 0,
  notBuilt,
});

describe("a version's validation, in sections (B4)", () => {
  it("puts what the version built first, then what it re-checked, then what is not built yet", () => {
    const sections = validationSections([group("F1"), group("F2"), group("F3", true)], "v2", ["F2"]);
    expect(sections.map((s) => [s.title, s.groups.map((g) => g.id)])).toEqual([
      ["New in v2", ["F2"]],
      ["Already built", ["F1"]],
      ["Not built yet", ["F3"]],
    ]);
  });

  it("leaves out an empty section: a repair builds nothing new", () => {
    expect(validationSections([group("F1")], "v1.1", []).map((s) => s.title)).toEqual(["Already built"]);
  });
});
