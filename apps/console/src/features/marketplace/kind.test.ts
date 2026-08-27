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
import type { components } from "../../generated/aep-api";
import { isRegisteredExternal } from "./kind";

type ExternalResourceDTO = components["schemas"]["ExternalResourceDTO"];

function resource(
  overrides: Partial<ExternalResourceDTO> = {},
): ExternalResourceDTO {
  return {
    name: "stripe",
    config: [],
    consumers: [],
    ...overrides,
  };
}

describe("isRegisteredExternal", () => {
  it("is true when envCells is a non-empty array", () => {
    expect(
      isRegisteredExternal(
        resource({
          envCells: [
            { environment: "development", key: "api_key", status: "configured" },
          ],
        }),
      ),
    ).toBe(true);
  });

  it("is false when envCells is empty", () => {
    expect(isRegisteredExternal(resource({ envCells: [] }))).toBe(false);
  });

  it("is false when envCells is omitted", () => {
    expect(isRegisteredExternal(resource())).toBe(false);
  });
});
