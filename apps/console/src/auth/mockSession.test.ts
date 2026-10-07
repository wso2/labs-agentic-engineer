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

// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { isMockSignedOut, setMockSignedOut } from "./mockSession";

describe("mock sign-out", () => {
  afterEach(() => sessionStorage.clear());

  it("lasts across a reload of the tab until the developer signs in again", () => {
    setMockSignedOut(true);
    expect(isMockSignedOut()).toBe(true);

    setMockSignedOut(false);
    expect(isMockSignedOut()).toBe(false);
  });
});
