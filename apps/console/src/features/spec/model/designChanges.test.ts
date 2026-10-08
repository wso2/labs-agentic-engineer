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
import type { DesignSpecChange } from "../api/specModel";
import { designChangedLines, specTabDot } from "./designChanges";

const change: DesignSpecChange = { lineId: "F2.2", featureId: "F2", comment: 2, seen: false };

describe("spec lines changed by design feedback", () => {
  it("dots the Spec tab until the user has seen every change", () => {
    expect(specTabDot([change])).toBe(true);
    expect(specTabDot([{ ...change, seen: true }])).toBe(false);
    expect(specTabDot([])).toBe(false);
  });

  it("marks each changed line in its own feature, with the comment that changed it", () => {
    const changes = [change, { ...change, lineId: "F1.3", featureId: "F1", comment: 4, seen: true }];
    expect([...designChangedLines(changes, "F2")]).toEqual([["F2.2", "changed by design comment 2"]]);
    expect([...designChangedLines(changes, "F1")]).toEqual([["F1.3", "changed by design comment 4"]]);
    expect(designChangedLines(changes, "F3").size).toBe(0);
  });
});
